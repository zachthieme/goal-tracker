package domain_test

import (
	"context"
	"slices"
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
	t.Parallel()

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
	t.Parallel()

	cases := []struct {
		name string
		// arrange builds the Goal under test at Epoch, before the default
		// baseline; nil when since builds it.
		arrange func(h *testsupport.Harness, boss domain.Account) domain.Goal
		// since runs once the clock has settled 40 days on, after the baseline,
		// and returns the Goal under test.
		since func(h *testsupport.Harness, boss domain.Account, g domain.Goal) domain.Goal
		// badges are the block's badges.
		badges []string
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
			badges: nil,
		},
		{
			// Checked in Green at Epoch, then nothing for 40 days.
			name: "Stale",
			arrange: func(h *testsupport.Harness, boss domain.Account) domain.Goal {
				g := h.ActiveGoal(boss, "Stale", "why")
				h.Checkin(boss, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
				return g
			},
			since:  func(_ *testsupport.Harness, _ domain.Account, g domain.Goal) domain.Goal { return g },
			badges: []string{domain.BadgeStale},
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
			badges: []string{domain.BadgeOwnerless},
		},
		{
			// Proposed since the baseline: no Health yet.
			name: "created",
			since: func(h *testsupport.Harness, boss domain.Account, _ domain.Goal) domain.Goal {
				return h.CreateGoal(boss, "Created", "why")
			},
			badges: []string{domain.BadgeNew},
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
			badges: []string{domain.BadgeNewDate},
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
			badges: []string{domain.BadgeOnHold},
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
			badges: nil,
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
			if len(r.Exceptions) == 1 && !slices.Equal(r.Exceptions[0].Badges, tc.badges) {
				t.Errorf("badges %v, want %v", r.Exceptions[0].Badges, tc.badges)
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
	t.Parallel()

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

// An exception block carries the whole MBR treatment: the due date with its
// struck-through history, Health, badges, So What, latest status, Path to
// Green, the Milestones marked New/Done/Removed with their struck dates, the
// Metrics against target, and the Rolled-up Health with the Owner's
// explanation (CONTEXT.md: Report, Date Slip, Rolled-up Health).
func TestReportBlockShowsTheMBRTreatment(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	date := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	// Due 2026-07-02 with Milestone Beta on 2026-04-02 (ActiveGoal's defaults).
	g := h.ActiveGoal(boss, "Launch in EU", "EU shoppers can't pay in euros.")
	child := h.ActiveChildOf(boss, g, "Localize checkout", "why")
	revenue, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
		GoalID: g.ID, Name: "EU revenue", Unit: "$M", Direction: domain.MetricUp,
		Baseline: 10, Target: 20, TargetDate: date(2026, 12, 31),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	alpha := addDatedMilestone(h, g, "Alpha", date(2026, 3, 1))
	docs := addDatedMilestone(h, g, "Docs", date(2026, 3, 15))
	beta := milestoneNamed(h, g, "Beta")

	// Before the baseline the delivery date slipped once.
	h.Clock.Advance(day)
	slipDelivery(h, boss, g, date(2026, 7, 16))

	settle(h)
	h.Checkin(boss, child.ID, domain.HealthRed, "Payments vendor down.", "Switch vendor.", date(2026, 3, 30))
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:             g.ID,
		AuthorID:           boss.ID,
		Health:             domain.HealthYellow,
		Status:             "Vendor is late again.",
		PathToGreen:        "Second vendor in parallel.",
		PathTargetDate:     date(2026, 4, 1),
		Explanation:        "Checkout's Red is contained to one market.",
		DeliveryDate:       date(2026, 8, 3),
		DeliveryDateReason: "Vendor slipped again.",
		Readings:           []domain.MetricReadingInput{{MetricID: revenue.ID, Value: 14}},
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: alpha.ID, Status: domain.MilestoneDone},
			{MilestoneID: docs.ID, Status: domain.MilestoneRemoved, RemovedReason: "Folded into Beta."},
			{MilestoneID: beta.ID, TargetDate: date(2026, 5, 4), DateReason: "Waiting on vendor."},
		},
		NewMilestones: []domain.NewMilestoneInput{{Name: "GA", TargetDate: date(2026, 7, 20)}},
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	r := draftReport(t, h, boss, time.Time{}, g)
	if len(r.Exceptions) != 1 {
		t.Fatalf("exception blocks %v, want [%d]", blockIDs(r), g.ID)
	}
	b := r.Exceptions[0]

	if b.Goal.Title != "Launch in EU" || b.Goal.SoWhat != "EU shoppers can't pay in euros." {
		t.Errorf("title and So What %q / %q", b.Goal.Title, b.Goal.SoWhat)
	}
	if !b.Goal.DeliveryDate.Equal(date(2026, 8, 3)) {
		t.Errorf("due date %v, want 2026-08-03", b.Goal.DeliveryDate)
	}
	if want := []time.Time{date(2026, 7, 2), date(2026, 7, 16)}; !slices.EqualFunc(b.PriorDueDates, want, time.Time.Equal) {
		t.Errorf("struck-through due dates %v, want %v", b.PriorDueDates, want)
	}
	if b.Health != domain.HealthYellow {
		t.Errorf("Health %q, want Yellow", b.Health)
	}
	if want := []string{domain.BadgeNewDate}; !slices.Equal(b.Badges, want) {
		t.Errorf("badges %v, want %v", b.Badges, want)
	}
	if b.Status != "Vendor is late again." {
		t.Errorf("latest status %q", b.Status)
	}
	if b.PathToGreen != "Second vendor in parallel." || !b.PathTargetDate.Equal(date(2026, 4, 1)) {
		t.Errorf("Path to Green %q by %v", b.PathToGreen, b.PathTargetDate)
	}

	type ms struct {
		name   string
		date   time.Time
		prior  []time.Time
		status string
		new    bool
	}
	var got []ms
	for _, m := range b.Milestones {
		got = append(got, ms{m.Milestone.Name, m.Milestone.TargetDate, m.PriorDates, m.Milestone.Status, m.New})
	}
	want := []ms{
		{"Alpha", date(2026, 3, 1), nil, domain.MilestoneDone, false},
		{"Docs", date(2026, 3, 15), nil, domain.MilestoneRemoved, false},
		{"Beta", date(2026, 5, 4), []time.Time{date(2026, 4, 2)}, domain.MilestonePlanned, false},
		{"GA", date(2026, 7, 20), nil, domain.MilestonePlanned, true},
	}
	if !slices.EqualFunc(got, want, func(a, b ms) bool {
		return a.name == b.name && a.date.Equal(b.date) && slices.EqualFunc(a.prior, b.prior, time.Time.Equal) &&
			a.status == b.status && a.new == b.new
	}) {
		t.Errorf("Milestones\n got %+v\nwant %+v", got, want)
	}

	if len(b.Metrics) != 1 {
		t.Fatalf("Metrics %+v, want EU revenue", b.Metrics)
	}
	if m := b.Metrics[0]; m.Metric.Name != "EU revenue" || !m.Read || m.Current != 14 || m.Metric.Target != 20 {
		t.Errorf("Metric against target %+v, want EU revenue at 14 of 20", m)
	}

	if !b.RolledUp.Present || b.RolledUp.Health != domain.HealthRed {
		t.Errorf("Rolled-up Health %+v, want Red", b.RolledUp)
	}
	if b.Explanation != "Checkout's Red is contained to one market." {
		t.Errorf("Rolled-up Health explanation %q", b.Explanation)
	}
}

// addDatedMilestone adds a Milestone to g, failing the test on error.
func addDatedMilestone(h *testsupport.Harness, g domain.Goal, name string, date time.Time) domain.Milestone {
	h.T.Helper()
	m, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{GoalID: g.ID, Name: name, TargetDate: date})
	if err != nil {
		h.T.Fatalf("AddMilestone: %v", err)
	}
	return m
}

// milestoneNamed returns g's Milestone with the given name, failing the test
// when there is none.
func milestoneNamed(h *testsupport.Harness, g domain.Goal, name string) domain.Milestone {
	h.T.Helper()
	ms, err := h.Service.ListMilestones(context.Background(), g.ID)
	if err != nil {
		h.T.Fatalf("ListMilestones: %v", err)
	}
	for _, m := range ms {
		if m.Name == name {
			return m
		}
	}
	h.T.Fatalf("no Milestone %q", name)
	return domain.Milestone{}
}
