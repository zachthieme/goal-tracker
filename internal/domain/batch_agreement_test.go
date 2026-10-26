package domain_test

import (
	"context"
	"reflect"
	"slices"
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
// required, so the Active Goals without it are Incomplete. The Goal carrying
// Team also carries an Area, two Metrics with readings and a second Milestone,
// each listed out of the order it was added, and contributes to two parents;
// the daily Goal has a Metric, a Retired Team value and a Pending parent.
func seedBatchAgreementGoals(t *testing.T, h *testsupport.Harness) []domain.Goal {
	t.Helper()
	ctx := context.Background()
	admin := h.SignInNamed("admin@example.com", "Ada Admin")
	priya := h.SignInNamed("priya@example.com", "Priya Raman")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	h.SetTimezone(priya, "America/New_York")
	h.SetTimezone(sam, "Asia/Tokyo")
	team := h.CreateDimension(admin, "Team", "Platform", "Growth")
	area := h.CreateDimension(admin, "Area", "Search", "Billing")

	// It moves the clock to just before its Milestone, so it goes first.
	h.GreenGoalWithOverdueMilestone(priya, "Green with an overdue Milestone")
	h.CreateGoal(sam, "Proposed", "It matters.")
	unchecked := h.ActiveGoal(priya, "Active with no Check-in", "It matters.")

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
	batchAgreementMetric(t, h, sam, daily, "Deploys", h.Clock.Now().AddDate(0, 1, 0))
	// Daily carries Growth, which is then Retired: it stays readable.
	h.AssignGoalValue(daily, team.Values[1])
	if err := h.Service.RetireDimensionValue(ctx, admin.ID, team.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue Growth: %v", err)
	}
	h.Checkin(sam, daily.ID, domain.HealthGreen, "Daily so far.", "", time.Time{})

	carried := h.ActiveGoal(priya, "Carries Team", "It matters.")
	h.AssignGoalValue(carried, team.Values[0])
	h.AssignGoalValue(carried, area.Values[0])
	// Alpha, added after Beta, is due before it, and the Metrics are added
	// latest due first: each lists in date order, not the order it was added.
	if _, err := h.Service.AddMilestoneAsAuthor(ctx, priya.ID, domain.AddMilestoneInput{
		GoalID:     carried.ID,
		Name:       "Alpha",
		TargetDate: h.Clock.Now().AddDate(0, 1, 0),
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor Alpha: %v", err)
	}
	latency := batchAgreementMetric(t, h, priya, carried, "Latency", h.Clock.Now().AddDate(0, 2, 0))
	uptime := batchAgreementMetric(t, h, priya, carried, "Uptime", h.Clock.Now().AddDate(0, 1, 0))
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   carried.ID,
		AuthorID: priya.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
		Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 120}, {MetricID: uptime.ID, Value: 99.5}},
	}); err != nil {
		t.Fatalf("SubmitCheckin with Metric readings: %v", err)
	}

	h.OnHoldGoal(sam, "On Hold", "It matters.", "Waiting on budget.")
	done := h.ActiveGoal(priya, "Done", "It matters.")
	h.EndGoalInCheckin(priya, done.ID, domain.LifecycleDone)

	// Carries Team contributes to two Goals, its Owner's own, so each link
	// is accepted at once; Daily has only asked to contribute to one.
	h.RequestLink(priya, carried, unchecked, "")
	h.RequestLink(priya, carried, slipped, "")
	h.RequestLink(sam, daily, unchecked, "")

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

// batchAgreementMetric adds a Metric named name, due on targetDate, to goal
// as its Owner, failing the test on error.
func batchAgreementMetric(t *testing.T, h *testsupport.Harness, owner domain.Account, goal domain.Goal, name string, targetDate time.Time) domain.Metric {
	t.Helper()
	m, err := h.Service.AddMetric(context.Background(), owner.ID, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       name,
		Unit:       "count",
		Direction:  domain.MetricUp,
		Baseline:   0,
		Target:     100,
		TargetDate: targetDate,
	})
	if err != nil {
		t.Fatalf("AddMetric %q: %v", name, err)
	}
	return m
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

// batchAgreementOutOfIDOrder reports whether the n rows idAt reads are out of
// the order of their IDs, so a read that listed them by ID instead would show.
func batchAgreementOutOfIDOrder(n int, idAt func(i int) int64) bool {
	for i := 1; i < n; i++ {
		if idAt(i) < idAt(i-1) {
			return true
		}
	}
	return false
}

// batchAgreementCount is 1 for true and 0 for false.
func batchAgreementCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// The batch reads the Goal list and the CSV download read with return, for
// every Goal, what its single-Goal read returns for that Goal alone:
// RunOffGreenByGoal as RunOffGreen (#449) and MetricsByGoal as ListMetrics,
// MilestonesByGoal as ListMilestones, ParentsByGoal as ParentsOf and
// GoalValuesByGoal as GoalValues (#450).
func TestListingBatchReadsAgreeWithTheirSingleGoalReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	goals := seedBatchAgreementGoals(t, h)
	ids := batchAgreementIDs(goals)

	runs, err := h.Service.RunOffGreenByGoal(ctx, goals)
	if err != nil {
		t.Fatalf("RunOffGreenByGoal: %v", err)
	}
	metrics, err := h.Service.MetricsByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("MetricsByGoal: %v", err)
	}
	milestones, err := h.Service.MilestonesByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("MilestonesByGoal: %v", err)
	}
	parents, err := h.Service.ParentsByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("ParentsByGoal: %v", err)
	}
	values, err := h.Service.GoalValuesByGoal(ctx)
	if err != nil {
		t.Fatalf("GoalValuesByGoal: %v", err)
	}

	var offGreen, withMetrics, metricsOutOfIDOrder, withMilestones, milestonesOutOfIDOrder, withParents, withSeveralParents int
	var withValues, valuesOutOfIDOrder, withRetiredValue int
	for _, g := range goals {
		r, err := h.Service.RunOffGreen(ctx, g.ID)
		if err != nil {
			t.Fatalf("RunOffGreen(%q): %v", g.Title, err)
		}
		if batch, ok := runs[g.ID]; !ok || !reflect.DeepEqual(batch, r) {
			t.Errorf("RunOffGreenByGoal[%q] = %+v, %t; RunOffGreen = %+v", g.Title, batch, ok, r)
		}
		m, err := h.Service.ListMetrics(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListMetrics(%q): %v", g.Title, err)
		}
		// A Goal with none has no entry in the batch, and an empty list alone.
		if batch := metrics[g.ID]; len(batch) != len(m) || len(m) > 0 && !reflect.DeepEqual(batch, m) {
			t.Errorf("MetricsByGoal[%q] = %+v; ListMetrics = %+v", g.Title, batch, m)
		}
		ms, err := h.Service.ListMilestones(ctx, g.ID)
		if err != nil {
			t.Fatalf("ListMilestones(%q): %v", g.Title, err)
		}
		if batch := milestones[g.ID]; len(batch) != len(ms) || len(ms) > 0 && !reflect.DeepEqual(batch, ms) {
			t.Errorf("MilestonesByGoal[%q] = %+v; ListMilestones = %+v", g.Title, batch, ms)
		}
		p, err := h.Service.ParentsOf(ctx, g.ID)
		if err != nil {
			t.Fatalf("ParentsOf(%q): %v", g.Title, err)
		}
		if batch := parents[g.ID]; len(batch) != len(p) || len(p) > 0 && !reflect.DeepEqual(batch, p) {
			t.Errorf("ParentsByGoal[%q] = %+v; ParentsOf = %+v", g.Title, batch, p)
		}
		v, err := h.Service.GoalValues(ctx, g.ID)
		if err != nil {
			t.Fatalf("GoalValues(%q): %v", g.Title, err)
		}
		if batch := values[g.ID]; len(batch) != len(v) || len(v) > 0 && !reflect.DeepEqual(batch, v) {
			t.Errorf("GoalValuesByGoal[%q] = %+v; GoalValues = %+v", g.Title, batch, v)
		}

		offGreen += batchAgreementCount(r.Running())
		withMetrics += batchAgreementCount(len(m) > 0)
		metricsOutOfIDOrder += batchAgreementCount(batchAgreementOutOfIDOrder(len(m), func(i int) int64 { return m[i].ID }))
		withMilestones += batchAgreementCount(len(ms) > 0)
		milestonesOutOfIDOrder += batchAgreementCount(batchAgreementOutOfIDOrder(len(ms), func(i int) int64 { return ms[i].ID }))
		withParents += batchAgreementCount(len(p) > 0)
		withSeveralParents += batchAgreementCount(len(p) > 1)
		withValues += batchAgreementCount(len(v) > 0)
		valuesOutOfIDOrder += batchAgreementCount(batchAgreementOutOfIDOrder(len(v), func(i int) int64 { return v[i].ID }))
		withRetiredValue += batchAgreementCount(slices.ContainsFunc(v, func(dv domain.DimensionValue) bool { return dv.Retired }))
	}
	// The agreement means something only over Goals that differ.
	for what, n := range map[string]int{
		"off Green":     offGreen,
		"with a Metric": withMetrics,
		"with Metrics out of the order they were added": metricsOutOfIDOrder,
		"with a Milestone": withMilestones,
		"with Milestones out of the order they were added": milestonesOutOfIDOrder,
		"with a parent":                                  withParents,
		"with several parents":                           withSeveralParents,
		"with a Dimension value":                         withValues,
		"with values out of the order they were defined": valuesOutOfIDOrder,
		"with a Retired value":                           withRetiredValue,
	} {
		if n == 0 || n == len(goals) {
			t.Errorf("%d of the %d Goals are %s; want some and not all", n, len(goals), what)
		}
	}
}
