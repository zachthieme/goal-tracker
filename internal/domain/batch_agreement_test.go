package domain_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// seedBatchAgreementGoals arranges Goals of every kind the per-Goal reads tell
// apart, over two Owners in different Timezones, and returns every Goal as
// ListGoals reads it: Proposed; Active with no Check-in; Green with an overdue
// Milestone; Yellow whose back-to-Green date slipped and is now overdue; Red
// with its delivery date and a Milestone's date slipped; Check-in overdue on a
// daily cadence; Active carrying the required Team; On Hold; and Done. Team is
// required, so the Active Goals without it are Incomplete.
func seedBatchAgreementGoals(t *testing.T, h *testsupport.Harness) []domain.Goal {
	t.Helper()
	ctx := context.Background()
	admin := h.SignInNamed("admin@example.com", "Ada Admin")
	priya := h.SignInNamed("priya@example.com", "Priya Raman")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	h.SetTimezone(priya, "America/New_York")
	h.SetTimezone(sam, "Asia/Tokyo")
	team := h.CreateDimension(admin, "Team", "Platform", "Growth")

	// It moves the clock to just before its Milestone, so it goes first.
	h.GreenGoalWithOverdueMilestone(priya, "Green with an overdue Milestone")
	h.CreateGoal(sam, "Proposed", "It matters.")
	h.ActiveGoal(priya, "Active with no Check-in", "It matters.")

	slippedPath := h.ActiveGoal(sam, "Back-to-Green date slipped", "It matters.")
	h.Checkin(sam, slippedPath.ID, domain.HealthYellow, "Behind.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 2))
	h.Clock.Advance(time.Hour)
	h.CheckinMovingPath(sam, slippedPath.ID, domain.HealthYellow, "Still behind.", "Add two reviewers.", h.Clock.Now().AddDate(0, 0, 3), "The reviewer left.")

	slipped := h.ActiveGoal(priya, "Delivery and Milestone slipped", "It matters.")
	milestones, err := h.Service.ListMilestones(ctx, slipped.ID)
	if err != nil || len(milestones) != 1 {
		t.Fatalf("ListMilestones = %+v, %v; want just Beta", milestones, err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:             slipped.ID,
		AuthorID:           priya.ID,
		Health:             domain.HealthRed,
		Status:             "Blocked on the vendor.",
		PathToGreen:        "Find another vendor.",
		PathTargetDate:     h.Clock.Now().AddDate(0, 0, 20),
		DeliveryDate:       h.Clock.Now().AddDate(0, 9, 0),
		DeliveryDateReason: "The vendor slipped.",
		Milestones: []domain.MilestoneChangeInput{{
			MilestoneID: milestones[0].ID,
			TargetDate:  milestones[0].TargetDate.AddDate(0, 1, 0),
			DateReason:  "The vendor slipped.",
			Status:      domain.MilestonePlanned,
		}},
	}); err != nil {
		t.Fatalf("SubmitCheckin slipping the delivery date and Beta: %v", err)
	}

	daily := h.ActiveGoal(sam, "Daily", "It matters.")
	if _, err := h.Service.SetCadence(ctx, sam.ID, daily.ID, 1); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Checkin(sam, daily.ID, domain.HealthGreen, "Daily so far.", "", time.Time{})

	carried := h.ActiveGoal(priya, "Carries Team", "It matters.")
	h.AssignGoalValue(carried, team.Values[0])
	h.Checkin(priya, carried.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.OnHoldGoal(sam, "On Hold", "It matters.", "Waiting on budget.")
	done := h.ActiveGoal(priya, "Done", "It matters.")
	h.EndGoalInCheckin(priya, done.ID, domain.LifecycleDone)

	h.SetDimensionRequired(admin, team, true)
	// Days on, the daily Goal is Check-in overdue and the slipped back-to-Green
	// date has passed.
	h.Clock.Advance(4 * 24 * time.Hour)

	goals, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	return goals
}

// batchAgreementIDs is the IDs of goals, in order.
func batchAgreementIDs(goals []domain.Goal) []int64 {
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	return ids
}

// Each batch read returns, for every Goal, what its single-Goal read returns
// for that Goal alone: FreshnessByGoal as Freshness, IncompleteByGoal as
// Incomplete, LatestCheckinsByGoal as LatestCheckin and DateSlipsByGoal as
// ListDateSlips (#411, #451).
func TestBatchReadsAgreeWithTheirSingleGoalReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	goals := seedBatchAgreementGoals(t, h)
	ids := batchAgreementIDs(goals)

	freshness, err := h.Service.FreshnessByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("FreshnessByGoal: %v", err)
	}
	incomplete, err := h.Service.IncompleteByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("IncompleteByGoal: %v", err)
	}
	latest, err := h.Service.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("LatestCheckinsByGoal: %v", err)
	}
	slips, err := h.Service.DateSlipsByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("DateSlipsByGoal: %v", err)
	}

	var checkinOverdue, pathOverdue, milestoneOverdue, isIncomplete, uncheckedIn, slipped int
	for _, g := range goals {
		f, err := h.Service.Freshness(ctx, g.ID)
		if err != nil {
			t.Fatalf("Freshness(%q): %v", g.Title, err)
		}
		if !reflect.DeepEqual(freshness[g.ID], f) {
			t.Errorf("FreshnessByGoal[%q] = %+v; Freshness = %+v", g.Title, freshness[g.ID], f)
		}
		missing, err := h.Service.Incomplete(ctx, g)
		if err != nil {
			t.Fatalf("Incomplete(%q): %v", g.Title, err)
		}
		if !reflect.DeepEqual(incomplete[g.ID], missing) {
			t.Errorf("IncompleteByGoal[%q] = %q; Incomplete = %q", g.Title, incomplete[g.ID], missing)
		}
		c, ok, err := h.Service.LatestCheckin(ctx, g.ID)
		if err != nil {
			t.Fatalf("LatestCheckin(%q): %v", g.Title, err)
		}
		if batch, batchOK := latest[g.ID]; batchOK != ok || !reflect.DeepEqual(batch, c) {
			t.Errorf("LatestCheckinsByGoal[%q] = %+v, %t; LatestCheckin = %+v, %t", g.Title, batch, batchOK, c, ok)
		}
		s, err := h.Service.ListDateSlips(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListDateSlips(%q): %v", g.Title, err)
		}
		// A Goal with none has no entry in the batch, and an empty list alone.
		if batch := slips[g.ID]; len(batch) != len(s) || len(s) > 0 && !reflect.DeepEqual(batch, s) {
			t.Errorf("DateSlipsByGoal[%q] = %+v; ListDateSlips = %+v", g.Title, batch, s)
		}

		checkinOverdue += batchAgreementCount(f.CheckinOverdue)
		pathOverdue += batchAgreementCount(f.PathToGreenOverdue)
		milestoneOverdue += batchAgreementCount(f.MilestoneOverdue)
		isIncomplete += batchAgreementCount(len(missing) > 0)
		uncheckedIn += batchAgreementCount(!ok)
		slipped += batchAgreementCount(len(s) > 0)
	}
	// The agreement means something only over Goals that differ.
	for what, n := range map[string]int{
		"Check-in overdue": checkinOverdue, "Path to Green overdue": pathOverdue,
		"Milestone overdue": milestoneOverdue, "Incomplete": isIncomplete,
		"with no Check-in": uncheckedIn, "with a Date Slip": slipped,
	} {
		if n == 0 || n == len(goals) {
			t.Errorf("%d of the %d Goals are %s; want some and not all", n, len(goals), what)
		}
	}
}

// batchAgreementCount is 1 for true and 0 for false.
func batchAgreementCount(b bool) int {
	if b {
		return 1
	}
	return 0
}
