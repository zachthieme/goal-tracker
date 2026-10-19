package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An exception block opens with what changed in its Health since the
// baseline; a Goal Red then and Red now changed nothing (CONTEXT.md: Report).
func TestReportBlockSaysHowItsHealthChanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		before, after string
		changes       []string
	}{
		{name: "Green to Red", before: domain.HealthGreen, after: domain.HealthRed, changes: []string{"Turned Red"}},
		{name: "Yellow to Red", before: domain.HealthYellow, after: domain.HealthRed, changes: []string{"Turned Red"}},
		{name: "Green to Yellow", before: domain.HealthGreen, after: domain.HealthYellow, changes: []string{"Turned Yellow"}},
		{name: "Red to Yellow", before: domain.HealthRed, after: domain.HealthYellow, changes: []string{"Improved to Yellow"}},
		{name: "Yellow to Green", before: domain.HealthYellow, after: domain.HealthGreen, changes: []string{"Back to Green"}},
		{name: "stays Red", before: domain.HealthRed, after: domain.HealthRed, changes: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			checkinHealth(h, boss, g, tc.before)
			settle(h)
			checkinHealth(h, boss, g, tc.after)

			r := draftReport(t, h, boss, time.Time{}, g)

			if len(r.Exceptions) != 1 {
				t.Fatalf("exception blocks %v, want [%d]", blockIDs(r), g.ID)
			}
			if got := r.Exceptions[0].Changes; !slices.Equal(got, tc.changes) {
				t.Errorf("changes %q, want %q", got, tc.changes)
			}
		})
	}
}

// checkinHealth checks g in at health, with a Path to Green a month out when
// health needs one.
func checkinHealth(h *testsupport.Harness, author domain.Account, g domain.Goal, health string) {
	h.T.Helper()
	if health == domain.HealthGreen {
		h.Checkin(author, g.ID, health, "On track.", "", time.Time{})
		return
	}
	h.Checkin(author, g.ID, health, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0))
}

// A delivery date that moved since the baseline says by how much, net of
// every move since: a slip before the baseline doesn't count, and a short
// move reads in days, a longer one in weeks (CONTEXT.md: Date Slip).
func TestReportBlockSaysHowFarItsDeliveryDateMoved(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// moves are the days the delivery date moves by since the baseline,
		// each in its own Check-in, after a 14-day slip before it.
		moves   []int
		changes []string
	}{
		{name: "slipped three weeks in two moves", moves: []int{10, 11}, changes: []string{"Delivery date slipped 3 weeks"}},
		{name: "slipped 17 days", moves: []int{17}, changes: []string{"Delivery date slipped 2 weeks"}},
		{name: "slipped 13 days", moves: []int{13}, changes: []string{"Delivery date slipped 13 days"}},
		{name: "slipped a day", moves: []int{1}, changes: []string{"Delivery date slipped 1 day"}},
		{name: "pulled in 5 days", moves: []int{-5}, changes: []string{"Delivery date moved 5 days earlier"}},
		{name: "moved and back", moves: []int{7, -7}, changes: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			due := g.DeliveryDate.AddDate(0, 0, 14)
			slipDelivery(h, boss, g, due)
			settle(h)
			for _, days := range tc.moves {
				h.Clock.Advance(time.Hour)
				due = due.AddDate(0, 0, days)
				slipDelivery(h, boss, g, due)
			}

			r := draftReport(t, h, boss, time.Time{}, g)

			if len(r.Exceptions) != 1 {
				t.Fatalf("exception blocks %v, want [%d]", blockIDs(r), g.ID)
			}
			if got := r.Exceptions[0].Changes; !slices.Equal(got, tc.changes) {
				t.Errorf("changes %q, want %q", got, tc.changes)
			}
		})
	}
}

// A block says how many of its Milestones slipped since the baseline, that
// its Goal moved Lifecycle since, and that its Path to Green's date passed
// since; a Path to Green whose date passed before the baseline is standing
// trouble, not a change (CONTEXT.md: Report).
func TestReportBlockSaysWhatElseChanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	slipMilestones := func(h *testsupport.Harness, boss domain.Account, g domain.Goal, changes ...domain.MilestoneChangeInput) {
		h.T.Helper()
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID: g.ID, AuthorID: boss.ID, Health: domain.HealthGreen, Status: "On track.", Milestones: changes,
		}); err != nil {
			h.T.Fatalf("SubmitCheckin Milestones: %v", err)
		}
	}
	cases := []struct {
		name string
		// before runs at Epoch, before the default baseline (2026-01-12), and
		// since once the clock has settled on 2026-02-11.
		before, since func(h *testsupport.Harness, boss domain.Account, g domain.Goal)
		changes       []string
	}{
		{
			name: "two Milestones slipped",
			before: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				addDatedMilestone(h, g, "GA", date(2026, 6, 1))
				h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				slipMilestones(h, boss, g,
					domain.MilestoneChangeInput{MilestoneID: milestoneNamed(h, g, "Beta").ID, TargetDate: date(2026, 4, 20), DateReason: "Late."},
					domain.MilestoneChangeInput{MilestoneID: milestoneNamed(h, g, "GA").ID, TargetDate: date(2026, 6, 20), DateReason: "Late."})
			},
			changes: []string{"2 Milestones slipped"},
		},
		{
			name: "one Milestone slipped twice",
			before: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
			},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				beta := milestoneNamed(h, g, "Beta")
				slipMilestones(h, boss, g, domain.MilestoneChangeInput{MilestoneID: beta.ID, TargetDate: date(2026, 4, 20), DateReason: "Late."})
				h.Clock.Advance(time.Hour)
				slipMilestones(h, boss, g, domain.MilestoneChangeInput{MilestoneID: beta.ID, TargetDate: date(2026, 4, 27), DateReason: "Later."})
			},
			changes: []string{"1 Milestone slipped"},
		},
		{
			name:   "put On Hold",
			before: func(*testsupport.Harness, domain.Account, domain.Goal) {},
			since: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
					GoalID: g.ID, AuthorID: boss.ID, Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Reorg.",
				}); err != nil {
					h.T.Fatalf("SubmitCheckin On Hold: %v", err)
				}
			},
			changes: []string{"Moved to On Hold"},
		},
		{
			name: "Path to Green date passed since the baseline",
			before: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				h.Checkin(boss, g.ID, domain.HealthYellow, "Slipping.", "Add staff.", date(2026, 1, 22))
			},
			since:   func(*testsupport.Harness, domain.Account, domain.Goal) {},
			changes: []string{"Path to Green date passed"},
		},
		{
			name: "Path to Green date passed before the baseline",
			before: func(h *testsupport.Harness, boss domain.Account, g domain.Goal) {
				h.Checkin(boss, g.ID, domain.HealthYellow, "Slipping.", "Add staff.", date(2026, 1, 7))
			},
			since:   func(*testsupport.Harness, domain.Account, domain.Goal) {},
			changes: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
			tc.before(h, boss, g)
			settle(h)
			tc.since(h, boss, g)

			r := draftReport(t, h, boss, time.Time{}, g)

			if len(r.Exceptions) != 1 {
				t.Fatalf("exception blocks %v, want [%d]", blockIDs(r), g.ID)
			}
			if got := r.Exceptions[0].Changes; !slices.Equal(got, tc.changes) {
				t.Errorf("changes %q, want %q", got, tc.changes)
			}
		})
	}
}

// A Report's exceptions come worst first, key by key: Ownerless, then Red,
// Yellow, no Health, Green; then a Path to Green overdue; then Stale; then
// those that changed since the baseline; then the soonest delivery date. The
// order is the same whatever order the Goals were selected in (CONTEXT.md:
// Report).
func TestReportPutsItsExceptionsWorstFirst(t *testing.T) {
	t.Parallel()

	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	type goalCase struct {
		title     string
		ownerless bool
		due       time.Time
		// before is the Health checked in at Epoch, before the default
		// baseline, and after the one checked in once the clock has settled;
		// none when empty.
		before, after string
		// pathTarget is the Path to Green's date checked in before, when set.
		pathTarget time.Time
	}
	want := []goalCase{
		{title: "Ownerless", ownerless: true, due: date(2026, 7, 2), before: domain.HealthGreen, after: domain.HealthGreen},
		{title: "Turned Red", due: date(2026, 10, 1), before: domain.HealthGreen, after: domain.HealthRed},
		{title: "Red due sooner", due: date(2026, 7, 1), before: domain.HealthRed, after: domain.HealthRed},
		{title: "Red due later", due: date(2026, 9, 1), before: domain.HealthRed, after: domain.HealthRed},
		{title: "Yellow overdue", due: date(2026, 7, 2), before: domain.HealthYellow, pathTarget: date(2026, 1, 7)},
		{title: "Yellow", due: date(2026, 7, 2), before: domain.HealthYellow, after: domain.HealthYellow},
		{title: "Back to Green", due: date(2026, 7, 2), before: domain.HealthYellow, after: domain.HealthGreen},
	}
	reversed := slices.Clone(want)
	slices.Reverse(reversed)
	for name, created := range map[string][]goalCase{
		"created worst first": want,
		"created best first":  reversed,
	} {
		t.Run(name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			sam := h.SignIn("sam@example.com")
			goals := make([]domain.Goal, len(created))
			for i, gc := range created {
				owner := boss
				if gc.ownerless {
					owner = sam
				}
				goals[i] = activeGoalDue(h, owner, gc.title, gc.due)
				switch {
				case !gc.pathTarget.IsZero():
					h.Checkin(owner, goals[i].ID, gc.before, "Off track.", "Add staff.", gc.pathTarget)
				case gc.before != "":
					checkinHealth(h, owner, goals[i], gc.before)
				}
			}
			settle(h)
			for i, gc := range created {
				if gc.after != "" {
					checkinHealth(h, goals[i].Owner, goals[i], gc.after)
				}
				if gc.ownerless {
					if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
						t.Fatalf("MarkDeparted: %v", err)
					}
				}
			}

			r := draftReport(t, h, boss, time.Time{}, goals...)

			var got, titles []string
			for _, b := range r.Exceptions {
				got = append(got, b.Goal.Title)
			}
			for _, gc := range want {
				titles = append(titles, gc.title)
			}
			if !slices.Equal(got, titles) {
				t.Errorf("exceptions in order\n got %q\nwant %q", got, titles)
			}
		})
	}
}

// activeGoalDue creates an Active Goal owned by owner, due on due, with a
// Milestone a month before it, so neither is overdue once the clock settles.
func activeGoalDue(h *testsupport.Harness, owner domain.Account, title string, due time.Time) domain.Goal {
	h.T.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, title, title+" matters.")
	if _, err := h.Service.MarkGoalDated(ctx, g.ID, due); err != nil {
		h.T.Fatalf("MarkGoalDated: %v", err)
	}
	addDatedMilestone(h, g, "Beta", due.AddDate(0, -1, 0))
	active, err := h.Service.ActivateGoal(ctx, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}
