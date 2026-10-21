package web_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// queryCountAdmin is the Admin the query-count fixture signs in as.
const queryCountAdmin = "admin@example.com"

// queryCountFixture is a company of n Goals, cycling through every kind the Goal
// list and a Report show differently, and a Report Definition over them.
type queryCountFixture struct {
	report domain.ReportDefinition
}

// seedQueryCountFixture arranges n Goals over three Owners. Every tenth Goal is
// of the same kind, so ten or more Goals hold every kind: Proposed; Active with
// no Check-in; Green; Yellow and Red, each with a Path to Green, the Red one
// with its delivery date slipped; Check-in overdue on a daily cadence; Green
// with an overdue Milestone; On Hold; Done; and Cancelled. Some carry a Team or
// a Budget, which an Admin then marks required, so some Active Goals are
// Incomplete. The Report selects the Active and On Hold Goals by Lifecycle,
// showing Budget, so the Done and Cancelled ones come in by changing
// Lifecycle since its baseline.
func seedQueryCountFixture(t *testing.T, h *testsupport.Harness, n int) queryCountFixture {
	t.Helper()
	ctx := context.Background()
	admin := h.SignInNamed(queryCountAdmin, "Ada Admin")
	owners := []domain.Account{
		admin,
		h.SignInNamed("priya@example.com", "Priya Raman"),
		h.SignInNamed("sam@example.com", "Sam Lee"),
	}
	team := h.CreateDimension(admin, "Team", "Platform", "Growth")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "USD")

	path := func(days int) time.Time { return h.Clock.Now().AddDate(0, 0, days) }
	for i := range n {
		owner := owners[i%len(owners)]
		title := fmt.Sprintf("Goal %02d", i)
		var g domain.Goal
		switch i % 10 {
		case 0:
			g = h.CreateGoal(owner, title, "It matters.")
		case 1:
			g = h.ActiveGoal(owner, title, "It matters.")
		case 2:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.Checkin(owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
		case 3:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.Checkin(owner, g.ID, domain.HealthYellow, "A little behind.", "Add a reviewer.", path(14))
		case 4:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
				GoalID:             g.ID,
				AuthorID:           owner.ID,
				Health:             domain.HealthRed,
				Status:             "Blocked on the vendor.",
				PathToGreen:        "Find another vendor.",
				PathTargetDate:     path(3),
				DeliveryDate:       h.Clock.Now().AddDate(0, 7, 0),
				DeliveryDateReason: "The vendor slipped.",
			}); err != nil {
				t.Fatalf("SubmitCheckin slipping the delivery date: %v", err)
			}
		case 5:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.SetCadence(ctx, owner.ID, g.ID, 1); err != nil {
				t.Fatalf("SetCadence: %v", err)
			}
			h.Checkin(owner, g.ID, domain.HealthGreen, "Daily so far.", "", time.Time{})
		case 6:
			g = h.ActiveGoal(owner, title, "It matters.")
			if _, err := h.Service.AddMilestone(ctx, owner.ID, domain.AddMilestoneInput{
				GoalID: g.ID, Name: "Pilot", TargetDate: path(2),
			}); err != nil {
				t.Fatalf("AddMilestone: %v", err)
			}
			h.Checkin(owner, g.ID, domain.HealthGreen, "Pilot lined up.", "", time.Time{})
		case 7:
			g = h.OnHoldGoal(owner, title, "It matters.", "Waiting on budget.")
		case 8:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleDone)
		case 9:
			g = h.ActiveGoal(owner, title, "It matters.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleCancelled)
		}
		if i%3 == 0 {
			h.AssignGoalValue(g, team.Values[i%2])
		}
		if i%4 == 0 {
			h.SetGoalField(owner, g, budget, strconv.Itoa(1000*(i+1)))
		}
		h.Clock.Advance(time.Hour)
	}
	h.SetDimensionRequired(admin, team, true)
	h.SetFieldRequired(admin, budget, true)
	// Days on, the daily Goals are Check-in overdue and the Pilots overdue.
	h.Clock.Advance(5 * 24 * time.Hour)

	report := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{
		Name: "Weekly",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{
			Attribute: domain.RuleLifecycle,
			Op:        domain.RuleIsAnyOf,
			Values:    []string{domain.LifecycleActive, domain.LifecycleOnHold},
		}},
		FieldIDs: []int64{budget.ID},
	})
	return queryCountFixture{report: report}
}

// assertGolden compares got with the golden file testdata/name, rewriting it
// instead when UPDATE_GOLDEN is set.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (UPDATE_GOLDEN=1 writes it): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from what was rendered; the page changed for the seeded data", path)
	}
}

// The Goal list and a Report render the same for the seeded Goals as they did
// before their reads were batched (#411).
func TestGoalListAndReportRenderAsGolden(t *testing.T) {
	h := testsupport.New(t, queryCountAdmin)
	fx := seedQueryCountFixture(t, h, 50)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, queryCountAdmin)

	assertGolden(t, "goal-list.golden.html", getBody(t, client, ts.URL+"/goals?org=all"))
	assertGolden(t, "report.golden.html", getBody(t, client, fmt.Sprintf("%s/reports/%d", ts.URL, fx.report.ID)))
}
