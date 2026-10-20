package web

import (
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const day = 24 * time.Hour

// rowsFor reads the Risks page's rows the way the page does: the signals
// loadRisks finds, merged by riskRows.
func rowsFor(t *testing.T, h *testsupport.Harness) []riskGoalRow {
	t.Helper()
	s := NewServer(h.Service)
	v, err := s.loadRisks(t.Context())
	if err != nil {
		t.Fatalf("loadRisks: %v", err)
	}
	rows, err := s.riskRows(t.Context(), v)
	if err != nil {
		t.Fatalf("riskRows: %v", err)
	}
	return rows
}

// kinds lists a row's signal kinds in order.
func kinds(r riskGoalRow) []string {
	var out []string
	for _, sig := range r.Signals {
		out = append(out, sig.Kind)
	}
	return out
}

// A Goal that is both Stale and Unlinked is one row carrying both signals,
// the Stale one counting the days since its last update.
func TestRiskRowsMergeAGoalsSignals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	rows := rowsFor(t, h)

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Goal.ID != silent.ID || r.Health != "" {
		t.Errorf("row = %q Health %q, want %q with no Health", r.Goal.Title, r.Health, silent.Title)
	}
	want := []riskSignal{{Kind: "checkin-overdue", Days: 10}, {Kind: "unlinked"}}
	if len(r.Signals) != len(want) {
		t.Fatalf("signals = %v, want stale and unlinked", kinds(r))
	}
	for i, w := range want {
		if got := r.Signals[i]; got.Kind != w.Kind || got.Days != w.Days || got.Related != nil {
			t.Errorf("signal %d = %+v, want %+v", i, got, w)
		}
	}
}

// rowOf finds goal's row, failing the test when it has none.
func rowOf(t *testing.T, rows []riskGoalRow, goal domain.Goal) riskGoalRow {
	t.Helper()
	for _, r := range rows {
		if r.Goal.ID == goal.ID {
			return r
		}
	}
	t.Fatalf("no row for %q in %+v", goal.Title, rows)
	return riskGoalRow{}
}

// signalOf finds the row's signal of kind, failing the test when it has none.
func signalOf(t *testing.T, r riskGoalRow, kind string) riskSignal {
	t.Helper()
	for _, sig := range r.Signals {
		if sig.Kind == kind {
			return sig
		}
	}
	t.Fatalf("%q has no %s signal: %v", r.Goal.Title, kind, kinds(r))
	return riskSignal{}
}

// A row carries its Goal's latest Health, and each signal says how bad it is:
// a Path to Green counts the days since its target date, the first day after
// it being 1; a schedule conflict counts the days the child's delivery date
// falls after its parent's, and names the parent, as a halted parent does. A
// parent is flagged for its own Health, never for its children's signals.
func TestRiskRowsCountDaysAndNameParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 0, 30))
	h.RequestLink(sam, late, launch, "")
	paused := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Paused outcome", "It mattered."))
	underPaused := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID: paused.ID, AuthorID: sam.ID, Status: "Pausing.",
		Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch)
	h.Checkin(sam, launch.ID, domain.HealthYellow, "Slipping.", "Add a team.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(3 * day)

	rows := rowsFor(t, h)

	if r := rowOf(t, rows, stalled); r.Health != domain.HealthRed || signalOf(t, r, "path-overdue").Days != 3 {
		t.Errorf("stalled row = Health %q, path-overdue %+v; want Red, 3 days", r.Health, signalOf(t, r, "path-overdue"))
	}
	conflict := signalOf(t, rowOf(t, rows, late), "schedule-conflicts")
	if conflict.Days != 30 || conflict.Related == nil || conflict.Related.ID != launch.ID {
		t.Errorf("schedule conflict = %+v, want 30 days after %q", conflict, launch.Title)
	}
	halted := signalOf(t, rowOf(t, rows, underPaused), "halted-parents")
	if halted.Days != 0 || halted.Related == nil || halted.Related.ID != paused.ID {
		t.Errorf("halted parent = %+v, want %q", halted, paused.Title)
	}
	// launch is a parent with nothing wrong in its schedule, but its Yellow
	// Check-in puts it under Not Green, and its Path to Green isn't due yet.
	if got := kinds(rowOf(t, rows, launch)); !slices.Equal(got, []string{"yellow"}) {
		t.Errorf("%q flagged for %v, want only yellow", launch.Title, got)
	}
	for _, r := range rows {
		if r.Goal.ID == paused.ID {
			t.Errorf("%q, an On Hold parent, is flagged: %v", paused.Title, kinds(r))
		}
	}
}

// row builds a riskGoalRow for the pure tests: an Active Goal titled title on
// a cadence of days.
func row(title string, cadence int, health string, signals ...riskSignal) riskGoalRow {
	return riskGoalRow{Goal: domain.Goal{Title: title, CadenceDays: cadence, Lifecycle: domain.LifecycleActive}, Health: health, Signals: signals}
}

// Rows sort worst first, key by key: Active Goals before the Proposed and On
// Hold ones; then Ownerless first; then Red, Yellow, no Health, Green; then a
// Path to Green overdue first, longest overdue first; then Stale first,
// furthest past its cadence first; then more kinds of signal first; then by
// title, ignoring case. A Milestone overdue and an overdue Action Item rank
// between the Path to Green and Stale keys (see the tests below).
func TestSortRiskRowsPutsTheWorstFirst(t *testing.T) {
	t.Parallel()

	stale := func(days int) riskSignal { return riskSignal{Kind: "checkin-overdue", Days: days} }
	overdue := func(days int) riskSignal { return riskSignal{Kind: "path-overdue", Days: days} }
	ownerless := riskSignal{Kind: "ownerless"}
	unlinked := riskSignal{Kind: "unlinked"}
	conflict := riskSignal{Kind: "schedule-conflicts", Days: 30, Related: &domain.Goal{Title: "Launch"}}
	halted := riskSignal{Kind: "halted-parents", Related: &domain.Goal{Title: "Paused"}}
	want := []riskGoalRow{
		row("Orphan red", 7, domain.HealthRed, ownerless),
		row("Orphan green", 7, domain.HealthGreen, ownerless, stale(30)),
		row("Red overdue", 7, domain.HealthRed, overdue(1)),
		row("Red quiet", 7, domain.HealthRed, unlinked, conflict, halted),
		row("Yellow long overdue", 7, domain.HealthYellow, stale(8), overdue(5)),
		row("Yellow overdue", 7, domain.HealthYellow, stale(40), overdue(2)),
		row("Yellow stale", 7, domain.HealthYellow, stale(10)),
		row("Yellow plan", 7, domain.HealthYellow, conflict),
		row("None far past", 3, "", stale(12)),
		row("None long silent", 14, "", stale(20)),
		row("Beta halted", 7, "", halted, unlinked),
		row("alpha conflict", 7, "", conflict),
		row("Green stale", 7, domain.HealthGreen, stale(9)),
		row("Alpha", 30, domain.HealthGreen, unlinked),
		row("Beta", 7, domain.HealthGreen, unlinked),
		lifecycle(row("Aardvark proposal", 7, "", conflict), domain.LifecycleProposed),
		lifecycle(row("Abandoned plan", 7, "", halted), domain.LifecycleOnHold),
	}
	rows := slices.Clone(want)
	slices.Reverse(rows)

	sortRiskRows(rows)

	var got, wantTitles []string
	for i := range want {
		got = append(got, rows[i].Goal.Title)
		wantTitles = append(wantTitles, want[i].Goal.Title)
	}
	if !slices.Equal(got, wantTitles) {
		t.Errorf("sorted rows:\n got %q\nwant %q", got, wantTitles)
	}
}

// lifecycle is r with its Goal in lifecycle l.
func lifecycle(r riskGoalRow, l string) riskGoalRow {
	r.Goal.Lifecycle = l
	return r
}

// Among rows no ranked signal tells apart, the one with more kinds of signal
// comes first, whatever the titles, and then the title decides.
func TestSortRiskRowsPutsMoreKindsOfSignalFirst(t *testing.T) {
	t.Parallel()

	unlinked := riskSignal{Kind: "unlinked"}
	conflict := riskSignal{Kind: "schedule-conflicts", Days: 30, Related: &domain.Goal{Title: "Launch"}}
	want := []string{"Zebra", "Apple", "Mango"}
	rows := []riskGoalRow{
		row("Mango", 7, domain.HealthGreen, unlinked),
		row("Apple", 7, domain.HealthGreen, unlinked),
		row("Zebra", 7, domain.HealthGreen, unlinked, conflict),
	}

	sortRiskRows(rows)

	var got []string
	for _, r := range rows {
		got = append(got, r.Goal.Title)
	}
	if !slices.Equal(got, want) {
		t.Errorf("sorted rows:\n got %q\nwant %q", got, want)
	}
}

// Among Goals of the same Health, an overdue Action Item ranks a row below a
// Path to Green overdue and above Stale, the longest overdue first.
func TestSortRiskRowsRanksOverdueActionItemsBetweenPathsAndCheckinOverdue(t *testing.T) {
	t.Parallel()

	itemOverdue := func(days int) riskSignal { return riskSignal{Kind: "action-item-overdue", Days: days} }
	want := []string{"Path overdue", "Item long overdue", "Item overdue", "Stale"}
	rows := []riskGoalRow{
		row("Stale", 7, domain.HealthYellow, riskSignal{Kind: "checkin-overdue", Days: 30}),
		row("Item overdue", 7, domain.HealthYellow, itemOverdue(2)),
		row("Item long overdue", 7, domain.HealthYellow, itemOverdue(9)),
		row("Path overdue", 7, domain.HealthYellow, riskSignal{Kind: "path-overdue", Days: 1}),
	}

	sortRiskRows(rows)

	var got []string
	for _, r := range rows {
		got = append(got, r.Goal.Title)
	}
	if !slices.Equal(got, want) {
		t.Errorf("sorted rows:\n got %q\nwant %q", got, want)
	}
}

// Among Green Goals, a Milestone overdue ranks a row above an overdue Action
// Item and Stale, the longest overdue first: it takes the place a Path to
// Green overdue holds among Goals that aren't Green.
func TestSortRiskRowsRanksMilestoneOverdueAfterPaths(t *testing.T) {
	t.Parallel()

	milestoneOverdue := func(days int) riskSignal { return riskSignal{Kind: "milestone-overdue", Days: days} }
	want := []string{"Milestone long overdue", "Milestone overdue", "Item overdue", "Stale"}
	rows := []riskGoalRow{
		row("Stale", 7, domain.HealthGreen, riskSignal{Kind: "checkin-overdue", Days: 30}),
		row("Item overdue", 7, domain.HealthGreen, riskSignal{Kind: "action-item-overdue", Days: 9}),
		row("Milestone overdue", 7, domain.HealthGreen, milestoneOverdue(1)),
		row("Milestone long overdue", 7, domain.HealthGreen, milestoneOverdue(6)),
	}

	sortRiskRows(rows)

	var got []string
	for _, r := range rows {
		got = append(got, r.Goal.Title)
	}
	if !slices.Equal(got, want) {
		t.Errorf("sorted rows:\n got %q\nwant %q", got, want)
	}
}

// The Risks page groups the signals by who acts on them, and each group counts
// the Goals it holds once, however many of its signals flag a Goal.
func TestRiskGroupsCountUniqueGoals(t *testing.T) {
	t.Parallel()

	sig := func(kind string) riskSignal { return riskSignal{Kind: kind} }
	rows := []riskGoalRow{
		row("Stale and overdue", 7, domain.HealthRed, sig("checkin-overdue"), sig("path-overdue")),
		row("Stale and unlinked", 7, "", sig("checkin-overdue"), sig("unlinked")),
		row("Two conflicts", 7, domain.HealthGreen, sig("schedule-conflicts"), sig("schedule-conflicts")),
		row("Orphan under two halted parents", 7, "", sig("ownerless"), sig("halted-parents"), sig("halted-parents")),
		row("Under a halted parent", 7, domain.HealthGreen, sig("halted-parents")),
		row("Unlinked", 7, domain.HealthGreen, sig("unlinked")),
		row("Stale with an overdue Action Item", 7, domain.HealthYellow, sig("checkin-overdue"), sig("action-item-overdue")),
		row("Overdue Action Item", 7, domain.HealthGreen, sig("action-item-overdue")),
		row("Red, asking and stale", 7, domain.HealthRed, sig("red"), sig("asks-for-help"), sig("checkin-overdue")),
		row("Yellow only", 7, domain.HealthYellow, sig("yellow")),
	}

	want := []riskGroup{
		{Key: "health", Name: "Not Green", Count: 2},
		{Key: "owner", Name: "Waiting on a Check-in", Count: 4},
		{Key: "followup", Name: "Action Items overdue", Count: 2},
		{Key: "plan", Name: "Placement and dates", Count: 3},
		{Key: "admin", Name: "Needs an Admin", Count: 2},
	}
	if got := riskGroups(rows); !slices.Equal(got, want) {
		t.Errorf("riskGroups:\n got %+v\nwant %+v", got, want)
	}
}
