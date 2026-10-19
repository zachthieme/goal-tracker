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
// health needs one. A month out from a later day moves the back-to-Green date
// later, so it gives the reason that slip needs.
func checkinHealth(h *testsupport.Harness, author domain.Account, g domain.Goal, health string) {
	h.T.Helper()
	if health == domain.HealthGreen {
		h.Checkin(author, g.ID, health, "On track.", "", time.Time{})
		return
	}
	h.CheckinMovingPath(author, g.ID, health, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 1, 0), "Staff arrive later.")
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

// reportExceptionTitles lists the titles of a Report's exceptions, in order.
func reportExceptionTitles(r domain.Report) []string {
	titles := make([]string, 0, len(r.Exceptions))
	for _, b := range r.Exceptions {
		titles = append(titles, b.Goal.Title)
	}
	return titles
}

// A Yellow exception comes before one with no Health, even where its title
// sorts after it (CONTEXT.md: Report).
func TestReportPutsYellowBeforeNoHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	noHealth := activeGoalDue(h, boss, "A no Health", due)
	yellow := activeGoalDue(h, boss, "Z Yellow", due)
	// The Path to Green is six months out, so it isn't overdue once settled.
	h.Checkin(boss, yellow.ID, domain.HealthYellow, "Off track.", "Add staff.", h.Clock.Now().AddDate(0, 6, 0))
	settle(h)

	r := draftReport(t, h, boss, time.Time{}, noHealth, yellow)

	if got, want := reportExceptionTitles(r), []string{"Z Yellow", "A no Health"}; !slices.Equal(got, want) {
		t.Errorf("exceptions in order\n got %q\nwant %q", got, want)
	}
}

// A Stale exception comes before an otherwise equal fresh one, even where its
// title sorts after it (CONTEXT.md: Report).
func TestReportPutsAStaleExceptionBeforeAFreshOne(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	stale := activeGoalDue(h, boss, "Z stale", due)
	fresh := activeGoalDue(h, boss, "A fresh", due)
	target := h.Clock.Now().AddDate(0, 6, 0)
	h.Checkin(boss, stale.ID, domain.HealthYellow, "Off track.", "Add staff.", target)
	h.Checkin(boss, fresh.ID, domain.HealthYellow, "Off track.", "Add staff.", target)
	h.Clock.Advance(10 * day)
	h.Checkin(boss, fresh.ID, domain.HealthYellow, "Off track.", "Add staff.", target)
	h.Clock.Advance(day)

	// Against now, neither Goal changed since the baseline; "Z stale" was
	// last checked in 11 days ago on a 7-day cadence, "A fresh" one day ago.
	r := draftReport(t, h, boss, h.Clock.Now(), stale, fresh)

	if got, want := reportExceptionTitles(r), []string{"Z stale", "A fresh"}; !slices.Equal(got, want) {
		t.Fatalf("exceptions in order\n got %q\nwant %q", got, want)
	}
	for _, b := range r.Exceptions {
		if got, want := slices.Contains(b.Badges, domain.BadgeStale), b.Goal.ID == stale.ID; got != want {
			t.Errorf("%q Stale = %v, want %v (badges %q)", b.Goal.Title, got, want, b.Badges)
		}
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

// A Report counts what moved since its baseline across its Goals, each Goal
// in every count it fits: a Goal created since is new, and activating it is
// no Lifecycle change of its own (CONTEXT.md: Report).
func TestReportCountsWhatMovedSinceTheBaseline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	turnedRed := h.ActiveGoal(boss, "Turned Red", "why")
	backToGreen := h.ActiveGoal(boss, "Back to Green", "why")
	slipped := h.ActiveGoal(boss, "Slipped", "why")
	checkinHealth(h, boss, turnedRed, domain.HealthGreen)
	checkinHealth(h, boss, backToGreen, domain.HealthYellow)
	settle(h)
	checkinHealth(h, boss, turnedRed, domain.HealthRed)
	checkinHealth(h, boss, backToGreen, domain.HealthGreen)
	slipDelivery(h, boss, slipped, slipped.DeliveryDate.AddDate(0, 0, 21))
	created := h.ActiveGoal(boss, "Created", "why")

	r := draftReport(t, h, boss, time.Time{}, turnedRed, backToGreen, slipped, created)

	if r.Movement == nil {
		t.Fatal("no movement, want the draft's")
	}
	if got, want := *r.Movement, (domain.ReportMovement{TurnedRed: 1, BackToGreen: 1, Slipped: 1, New: 1}); got != want {
		t.Errorf("movement %+v, want %+v", got, want)
	}
	if got, want := r.Movement.Parts(), []string{"1 turned Red", "1 back to Green", "1 slipped", "1 new"}; !slices.Equal(got, want) {
		t.Errorf("movement parts %q, want %q", got, want)
	}
	for _, b := range r.Exceptions {
		if b.Goal.ID == created.ID && slices.Contains(b.Changes, "Moved to Active") {
			t.Errorf("new Goal's changes %q, want no Lifecycle move beside its New badge", b.Changes)
		}
	}
}

// Against the previous publication a Report counts the Goals that entered it,
// a new one only as new, and those that left. Against a date the reader
// chose it counts neither, as which Goals entered or left is read against
// the latest publication, not that date. A delivery date pulled in is no
// slip.
func TestReportCountsGoalsThatEnteredOrLeftAgainstThePreviousPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Stays in", "why")
	entered := h.ActiveGoal(boss, "Recovers", "why")
	left := h.ActiveGoal(boss, "Turns Red", "why")
	pulledIn := h.ActiveGoal(boss, "Pulled in", "why")
	settle(h)
	checkinHealth(h, boss, entered, domain.HealthRed)
	checkinHealth(h, boss, left, domain.HealthGreen)
	checkinHealth(h, boss, pulledIn, domain.HealthGreen)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:  "MBR",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{namedRule(domain.RuleHealth, domain.RuleIsNot, domain.HealthRed)},
	})
	h.PublishReport(boss, def)
	h.Clock.Advance(day)
	checkinHealth(h, boss, entered, domain.HealthGreen)
	checkinHealth(h, boss, left, domain.HealthRed)
	slipDelivery(h, boss, pulledIn, pulledIn.DeliveryDate.AddDate(0, 0, -5))
	h.ActiveGoal(boss, "Created", "why")

	for name, tc := range map[string]struct {
		baseline time.Time
		want     domain.ReportMovement
	}{
		"against the previous publication": {
			want: domain.ReportMovement{TurnedYellow: 1, BackToGreen: 1, New: 1, Entered: 1, Left: 1},
		},
		"against a chosen date": {
			baseline: orgDay(h.Clock.Now()),
			want:     domain.ReportMovement{TurnedYellow: 1, BackToGreen: 1, New: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := h.Service.DraftReport(ctx, def, tc.baseline)
			if err != nil {
				t.Fatalf("DraftReport: %v", err)
			}
			if len(r.MembershipChanges) != 3 {
				t.Errorf("membership changes %v, want the new Goal's, and those of the Goal that entered and the one that left", byGoal(t, r.MembershipChanges))
			}
			if r.Movement == nil {
				t.Fatal("no movement, want the draft's")
			}
			if got := *r.Movement; got != tc.want {
				t.Errorf("movement %+v, want %+v", got, tc.want)
			}
		})
	}
}

// orgDay is the calendar date of t, as a reader picks a baseline.
func orgDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// A Report where nothing moved since the baseline still counts it, as none.
func TestReportWithNothingMovedCountsNone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Stays Red", "why")
	checkinHealth(h, boss, g, domain.HealthRed)
	settle(h)
	checkinHealth(h, boss, g, domain.HealthRed)

	r := draftReport(t, h, boss, time.Time{}, g)

	if r.Movement == nil {
		t.Fatal("no movement, want the draft's")
	}
	if got := r.Movement.Parts(); len(got) != 0 {
		t.Errorf("movement parts %q, want none", got)
	}
}

// The movement's parts read each count that isn't zero, in a fixed order.
func TestReportMovementPartsReadEachCount(t *testing.T) {
	t.Parallel()

	m := domain.ReportMovement{TurnedRed: 1, TurnedYellow: 2, ImprovedToYellow: 3, BackToGreen: 4, Slipped: 5, ChangedLifecycle: 6, New: 7, Entered: 8, Left: 9}
	want := []string{
		"1 turned Red", "2 turned Yellow", "3 improved to Yellow", "4 back to Green", "5 slipped",
		"6 changed Lifecycle", "7 new", "8 entered", "9 left",
	}
	if got := m.Parts(); !slices.Equal(got, want) {
		t.Errorf("parts\n got %q\nwant %q", got, want)
	}
	if got := (domain.ReportMovement{Left: 1}).Parts(); !slices.Equal(got, []string{"1 left"}) {
		t.Errorf("parts of one Goal that left %q, want [1 left]", got)
	}
}
