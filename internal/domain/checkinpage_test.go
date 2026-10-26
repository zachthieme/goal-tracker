package domain_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Check-in page's loader refuses a Goal that doesn't exist with
// ErrNotFound, as ViewGoal does (ADR 0010).
func TestCheckinPageRefusesAMissingGoal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")

	if _, err := h.Service.CheckinPage(ctx, sam.ID, g.ID+1000); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("CheckinPage of a missing Goal: err = %v, want ErrNotFound", err)
	}
}

// The Check-in page's loader lets check in whoever SubmitCheckin would let
// write a Check-in: the Goal's Owner and a Delegate, but not an Admin, a
// reader with no rights, a Departed Delegate or a viewer with no Account. Only
// those who may check in see the pending Draft Highlights.
func TestCheckinPagePermitsTheCheckinAuthorsOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	gone := h.SignIn("gone@example.com")
	rita := h.SignIn("rita@example.com")
	admin := h.SignIn("admin@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, morgan, g.ID)
	h.AddDelegate(sam, gone, g.ID)
	h.MarkDeparted(admin, gone)
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: morgan.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}

	for _, v := range []struct {
		name    string
		viewer  int64
		checkIn bool
	}{
		{"the Owner", sam.ID, true},
		{"a Delegate", morgan.ID, true},
		{"a Departed Delegate", gone.ID, false},
		{"an Admin", admin.ID, false},
		{"a reader", rita.ID, false},
		{"a viewer with no Account", sam.ID + 1000, false},
	} {
		p, err := h.Service.CheckinPage(ctx, v.viewer, g.ID)
		if err != nil {
			t.Fatalf("CheckinPage for %s: %v", v.name, err)
		}
		if p.CanCheckin != v.checkIn {
			t.Errorf("CanCheckin for %s = %t, want %t", v.name, p.CanCheckin, v.checkIn)
		}
		if want := map[bool]int{true: 1}[v.checkIn]; len(p.DraftHighlights) != want {
			t.Errorf("%s sees %d Draft Highlights, want %d", v.name, len(p.DraftHighlights), want)
		}
		if p.Goal.Title != "Ship search" {
			t.Errorf("CheckinPage's Goal for %s = %q, want Ship search", v.name, p.Goal.Title)
		}
	}
}

// What the Check-in page shows of a Goal with a parent, children, Check-ins,
// an overdue Milestone, an off-target Metric, an open Ask for help and a
// pending Draft Highlight is what the Goal page's loader gives for it: the
// Check-in page's loader reads less, not differently (#560). Its children's
// link Health is theirs alone, as the page lists no parents.
func TestCheckinPageAgreesWithTheGoalPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	mia := h.SignIn("mia@example.com")
	// sam@'s Manager left, so the Ask for help goes past them to morgan@.
	h.SetManager(sam, mia)
	h.SetManager(mia, morgan)
	h.MarkDeparted(admin, mia)
	parent := h.ActiveGoal(sam, "Grow revenue", "The company needs it.")
	g := h.ActiveChildOf(sam, parent, "Ship search", "People can't find things.")
	child := h.ActiveChildOf(sam, g, "Index the catalogue", "Search needs it.")
	h.ActiveChildOf(sam, g, "Tune ranking", "Search needs it.")
	h.AddDelegate(sam, morgan, g.ID)
	metric, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
		GoalID: g.ID, Name: "Searches", Unit: "a day", Direction: domain.MetricUp,
		Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 0, 5),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, sam.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 0, 10),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	h.Checkin(sam, child.ID, domain.HealthYellow, "Slow going.", "Add an indexer.", h.Clock.Now().AddDate(0, 0, 30))
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthRed, Status: "Blocked on indexing.",
		PathToGreen: "Index the catalogue first.", PathTargetDate: h.Clock.Now().AddDate(0, 0, 30),
		Explanation: "The index is the blocker, not the child's pace.", AsksForHelp: true,
		Readings: []domain.MetricReadingInput{{MetricID: metric.ID, Value: 40}},
	}); err != nil {
		t.Fatalf("SubmitCheckin Red: %v", err)
	}
	if _, err := h.Service.LogDraftHighlight(ctx, domain.LogDraftHighlightInput{
		GoalID: g.ID, AuthorID: morgan.ID, Kind: domain.HighlightInsight, Note: "Search is slow on mobile.",
	}); err != nil {
		t.Fatalf("LogDraftHighlight: %v", err)
	}
	h.Clock.Advance(12 * 24 * time.Hour) // past Beta's date and the Metric's

	for _, viewer := range []domain.Account{sam, morgan} {
		gp, err := h.Service.GoalPage(ctx, viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage: %v", err)
		}
		cp, err := h.Service.CheckinPage(ctx, viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("CheckinPage: %v", err)
		}
		if gp.Ask == nil || gp.OverdueMilestone == nil || gp.NoChangeRefusal == nil || len(gp.DraftHighlights) == 0 || !gp.RunOffGreen.Running() || !gp.RolledUp.Present {
			t.Fatalf("the fixture doesn't give the Goal page everything to compare: %+v", gp)
		}
		sameAsGoalPage(t, "Goal", cp.Goal, gp.Goal)
		sameAsGoalPage(t, "Children", cp.Children, gp.Children)
		childHealth := map[int64]string{}
		for _, c := range gp.Children {
			if health, ok := gp.LinkHealth[c.Goal.ID]; ok {
				childHealth[c.Goal.ID] = health
			}
		}
		sameAsGoalPage(t, "children's link Health", cp.LinkHealth, childHealth)
		sameAsGoalPage(t, "Milestones", cp.Milestones, gp.Milestones)
		sameAsGoalPage(t, "OverdueMilestone", cp.OverdueMilestone, gp.OverdueMilestone)
		sameAsGoalPage(t, "Metrics", cp.Metrics, gp.Metrics)
		sameAsGoalPage(t, "Readings", cp.Readings, gp.Readings)
		sameAsGoalPage(t, "LatestCheckin", cp.LatestCheckin, gp.LatestCheckin)
		sameAsGoalPage(t, "RolledUp", cp.RolledUp, gp.RolledUp)
		sameAsGoalPage(t, "RunOffGreen", cp.RunOffGreen, gp.RunOffGreen)
		sameAsGoalPage(t, "Ask", cp.Ask, gp.Ask)
		sameAsGoalPage(t, "AskAddressee", cp.AskAddressee, gp.AskAddressee)
		sameAsGoalPage(t, "DraftHighlights", cp.DraftHighlights, gp.DraftHighlights)
		sameAsGoalPage(t, "CanCheckin", cp.CanCheckin, gp.CanCheckin)
		sameAsGoalPage(t, "NoChangeRefusal", cp.NoChangeRefusal, gp.NoChangeRefusal)
		sameAsGoalPage(t, "Now", cp.Now, gp.Now)
		sameAsGoalPage(t, "Timezone", cp.Timezone, gp.Timezone)
	}
}

// sameAsGoalPage fails the test unless the Check-in page's loader's got is
// what the Goal page's loader gave, want.
func sameAsGoalPage[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CheckinPage's %s = %+v, want GoalPage's %+v", what, got, want)
	}
}

// The Check-in page judges which Metrics a Green would leave off target, by
// the readings typed or, where none is, each Metric's latest, as
// OffTargetMetrics does, without reading anything more.
func TestCheckinPageJudgesOffTargetAsOffTargetMetricsDoes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	var metrics []domain.Metric
	for _, name := range []string{"Searches", "Clicks"} {
		m, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
			GoalID: g.ID, Name: name, Unit: "a day", Direction: domain.MetricUp,
			Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 0, 5),
		})
		if err != nil {
			t.Fatalf("AddMetric %s: %v", name, err)
		}
		metrics = append(metrics, m)
	}
	h.Clock.Advance(3 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.",
		Readings: []domain.MetricReadingInput{{MetricID: metrics[0].ID, Value: 40}, {MetricID: metrics[1].ID, Value: 50}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	h.Clock.Advance(3 * 24 * time.Hour) // past both Metrics' target date

	p, err := h.Service.CheckinPage(ctx, sam.ID, g.ID)
	if err != nil {
		t.Fatalf("CheckinPage: %v", err)
	}
	for _, typed := range [][]domain.MetricReadingInput{
		nil,
		{{MetricID: metrics[0].ID, Value: 120}},
		{{MetricID: metrics[0].ID, Value: 120}, {MetricID: metrics[1].ID, Value: 100}},
	} {
		want, err := h.Service.OffTargetMetrics(ctx, g.ID, typed)
		if err != nil {
			t.Fatalf("OffTargetMetrics: %v", err)
		}
		if got := p.OffTargetMetrics(typed); !reflect.DeepEqual(got, want) {
			t.Errorf("CheckinPage's OffTargetMetrics(%+v) = %+v, want %+v", typed, got, want)
		}
	}
}
