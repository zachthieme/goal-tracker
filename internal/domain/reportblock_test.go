package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// settle moves the clock 40 days past the harness Epoch, so Goals arranged at
// Epoch were created and activated before the default baseline (30 days ago).
func settle(h *testsupport.Harness) {
	h.Clock.Advance(40 * day)
}

// A Red Goal gets the full MBR block; an unchanged Green Goal gets one line
// (CONTEXT.md: Report).
func TestReportRedIsExceptionAndUnchangedGreenIsOneLine(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	settle(h)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	r := draftReport(t, h, boss, time.Time{}, red, green)

	if got, want := blockIDs(r), []int64{red.ID}; !sameSet(got, want) {
		t.Errorf("exception blocks %v, want %v", got, want)
	}
	if got, want := lineIDs(r), []int64{green.ID}; !sameSet(got, want) {
		t.Errorf("one-line Goals %v, want %v", got, want)
	}
	if got := r.Exceptions[0].Health; got != domain.HealthRed {
		t.Errorf("block Health %q, want Red", got)
	}
}

// draftReport saves a Report Definition over goals (depth 0, so exactly those
// Goals) and drafts its Report against baseline, failing the test on error. A
// zero baseline takes the default.
func draftReport(t *testing.T, h *testsupport.Harness, actor domain.Account, baseline time.Time, goals ...domain.Goal) domain.Report {
	t.Helper()
	roots := make([]int64, 0, len(goals))
	for _, g := range goals {
		roots = append(roots, g.ID)
	}
	def := h.SaveReportDefinition(actor, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: roots})
	r, err := h.Service.DraftReport(context.Background(), def, baseline)
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	return r
}

// blockIDs lists the Goals a Report gives a full exception block.
func blockIDs(r domain.Report) []int64 {
	ids := make([]int64, 0, len(r.Exceptions))
	for _, b := range r.Exceptions {
		ids = append(ids, b.Goal.ID)
	}
	return ids
}

// lineIDs lists the Goals a Report gives one line each.
func lineIDs(r domain.Report) []int64 {
	return selectedIDs(r.Lines)
}

// Each exception trigger alone earns a Goal the full block, beside an unchanged
// Green control that stays one line (CONTEXT.md: Report).
func TestReportExceptionTriggers(t *testing.T) {
	cases := []struct {
		name string
		// arrange builds the Goal under test at Epoch, before the default
		// baseline; nil when since builds it.
		arrange func(h *testsupport.Harness, boss domain.Account) domain.Goal
		// since runs once the clock has settled 40 days on, after the baseline,
		// and returns the Goal under test.
		since func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal
	}{
		{
			name: "Yellow",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				return h.ActiveGoal(boss, "Yellow", "why")
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal {
				h.Checkin(boss, g.ID, domain.HealthYellow, "Slipping.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
				return g
			},
		},
		{
			// Checked in Green at Epoch, then nothing for 40 days.
			name: "Stale",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				g := h.ActiveGoal(boss, "Stale", "why")
				h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
				return g
			},
			since: func(_ *testsupport.Harness, _ domain.Account, g domain.Goal) domain.Goal { return g },
		},
		{
			// Checked in Green, then its Owner leaves the org.
			name: "Ownerless",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				return h.ActiveGoal(h.SignIn("sam@example.com"), "Ownerless", "why")
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal {
				h.Checkin(g.Owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
				if err := h.Service.MarkDeparted(context.Background(), boss.ID, g.Owner.ID); err != nil {
					h.T.Fatalf("MarkDeparted: %v", err)
				}
				return g
			},
		},
		{
			// Proposed since the baseline: no Health yet.
			name: "created",
			since: func(h *testsupport.Harness, boss domain.Account, _ domain.Goal) domain.Goal {
				return h.CreateGoal(boss, "Created", "why")
			},
		},
		{
			// The delivery date slipped since the baseline; the Goal is Green
			// again by now.
			name: "slipped",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				return h.ActiveGoal(boss, "Slipped", "why")
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal {
				slipDelivery(h, boss, g, g.DeliveryDate.AddDate(0, 0, 14))
				h.Checkin(boss, g.ID, domain.HealthGreen, "Back on track.", "", time.Time{})
				return g
			},
		},
		{
			// Put On Hold since the baseline: On Hold is never Stale and has no
			// Health.
			name: "put On Hold",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				return h.ActiveGoal(boss, "On Hold", "why")
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal {
				if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
					GoalID:          g.ID,
					AuthorID:        boss.ID,
					Status:          "Pausing.",
					Lifecycle:       domain.LifecycleOnHold,
					LifecycleReason: "Reorg.",
				}); err != nil {
					h.T.Fatalf("SubmitCheckin On Hold: %v", err)
				}
				return g
			},
		},
		{
			// Proposed at Epoch, activated since the baseline.
			name: "activated",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				g := h.CreateGoal(boss, "Activated", "why")
				if _, err := h.Service.MarkGoalOngoing(context.Background(), g.ID); err != nil {
					h.T.Fatalf("MarkGoalOngoing: %v", err)
				}
				if _, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
					GoalID: g.ID, Name: "NPS", Unit: "points", Direction: domain.MetricUp,
					Baseline: 10, Target: 40, TargetDate: h.Clock.Now().AddDate(1, 0, 0),
				}); err != nil {
					h.T.Fatalf("AddMetric: %v", err)
				}
				return g
			},
			since: func(h *testsupport.Harness, _ domain.Account, g domain.Goal) domain.Goal {
				if _, err := h.Service.ActivateGoal(context.Background(), g.ID); err != nil {
					h.T.Fatalf("ActivateGoal: %v", err)
				}
				return g
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			var g domain.Goal
			if tc.arrange != nil {
				g = tc.arrange(h, boss)
			}
			control := h.ActiveGoal(boss, "Control", "why")
			settle(h)
			h.Checkin(boss, control.ID, domain.HealthGreen, "On track.", "", time.Time{})
			g = tc.since(h, boss, g)

			r := draftReport(t, h, boss, time.Time{}, g, control)

			if got, want := blockIDs(r), []int64{g.ID}; !sameSet(got, want) {
				t.Errorf("exception blocks %v, want %v", got, want)
			}
			if got, want := lineIDs(r), []int64{control.ID}; !sameSet(got, want) {
				t.Errorf("one-line Goals %v, want %v", got, want)
			}
		})
	}
}

// slipDelivery moves g's delivery date to newDate in a Yellow Check-in,
// recording a Date Slip, failing the test on error.
func slipDelivery(h *testsupport.Harness, owner domain.Account, g domain.Goal, newDate time.Time) {
	h.T.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             g.ID,
		AuthorID:           owner.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late.",
		PathToGreen:        "Chase the vendor.",
		PathTargetDate:     h.Clock.Now().AddDate(0, 0, 14),
		DeliveryDate:       newDate,
		DeliveryDateReason: "Vendor slipped.",
	}); err != nil {
		h.T.Fatalf("SubmitCheckin slip: %v", err)
	}
}

// Changes are read against the baseline: a Goal created, slipped, and paused
// and resumed before it is unchanged and takes one line, but a baseline the
// reader sets earlier catches those changes and makes it an exception.
func TestReportReadsChangesAgainstBaseline(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.Clock.Advance(day)
	slipDelivery(h, boss, g, g.DeliveryDate.AddDate(0, 0, 14))
	for _, move := range []domain.SubmitCheckinInput{
		{Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Reorg."},
		{Status: "Resuming.", Lifecycle: domain.LifecycleActive, Health: domain.HealthGreen},
	} {
		move.GoalID, move.AuthorID = g.ID, boss.ID
		if _, err := h.Service.SubmitCheckin(ctx, move); err != nil {
			t.Fatalf("SubmitCheckin %s: %v", move.Lifecycle, err)
		}
	}
	settle(h)
	h.Checkin(boss, g.ID, domain.HealthGreen, "Back on track.", "", time.Time{})

	r := draftReport(t, h, boss, time.Time{}, g)
	if want := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC); !r.Baseline.Equal(want) {
		t.Errorf("default baseline %v, want %v (30 days before today)", r.Baseline, want)
	}
	if got, want := lineIDs(r), []int64{g.ID}; !sameSet(got, want) {
		t.Errorf("with the default baseline, one-line Goals %v, want %v", got, want)
	}

	chosen := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) // the day of the slip
	r, err := h.Service.DraftReport(ctx, r.Definition, chosen)
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if !r.Baseline.Equal(chosen) {
		t.Errorf("baseline %v, want the reader's %v", r.Baseline, chosen)
	}
	if got, want := blockIDs(r), []int64{g.ID}; !sameSet(got, want) {
		t.Errorf("with an earlier baseline, exception blocks %v, want %v", got, want)
	}
}
