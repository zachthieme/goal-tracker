package domain_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// followUpsFor reads the Follow-ups page's loader for viewer, failing the
// test on error.
func followUpsFor(t *testing.T, h *testsupport.Harness, viewer domain.Account) domain.FollowUps {
	t.Helper()
	f, err := h.Service.FollowUps(context.Background(), viewer.ID)
	if err != nil {
		t.Fatalf("FollowUps for %s: %v", viewer.Email, err)
	}
	return f
}

// followUpKinds lists a row's signal kinds in order.
func followUpKinds(r domain.FollowUpRow) []domain.FollowUpKind {
	var out []domain.FollowUpKind
	for _, sig := range r.Signals {
		out = append(out, sig.Kind)
	}
	return out
}

// A Goal that is both Check-in overdue and Unlinked is one row carrying both
// signals, the Check-in overdue one counting the days since its last update.
func TestFollowUpsMergeAGoalsSignalsIntoOneRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	rows := followUpsFor(t, h, sam).Rows

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Goal.ID != silent.ID || r.Health != "" {
		t.Errorf("row = %q Health %q, want %q with no Health", r.Goal.Title, r.Health, silent.Title)
	}
	want := []domain.FollowUpSignal{{Kind: "checkin-overdue", Days: 10}, {Kind: "unlinked"}}
	if len(r.Signals) != len(want) {
		t.Fatalf("signals = %v, want checkin-overdue and unlinked", followUpKinds(r))
	}
	for i, w := range want {
		if got := r.Signals[i]; got.Kind != w.Kind || got.Days != w.Days || got.Related != nil || got.Person != nil {
			t.Errorf("signal %d = %+v, want %+v", i, got, w)
		}
	}
}

// followUpRowOf finds goal's row, failing the test when it has none.
func followUpRowOf(t *testing.T, rows []domain.FollowUpRow, goal domain.Goal) domain.FollowUpRow {
	t.Helper()
	for _, r := range rows {
		if r.Goal.ID == goal.ID {
			return r
		}
	}
	t.Fatalf("no row for %q in %+v", goal.Title, rows)
	return domain.FollowUpRow{}
}

// followUpSignalOf finds the row's signal of kind, failing the test when it
// has none.
func followUpSignalOf(t *testing.T, r domain.FollowUpRow, kind domain.FollowUpKind) domain.FollowUpSignal {
	t.Helper()
	sig, ok := r.Signal(kind)
	if !ok {
		t.Fatalf("%q has no %s signal: %v", r.Goal.Title, kind, followUpKinds(r))
	}
	return sig
}

// A row carries its Goal's latest Health, and each signal says how bad it is:
// a Path to Green counts the days since its target date, the first day after
// it being 1; a schedule conflict counts the days the child's delivery date
// falls after its parent's, and names the parent, as a halted parent does. A
// parent is flagged for its own Health, never for its children's signals.
func TestFollowUpsCountDaysAndNameParents(t *testing.T) {
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
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: paused.ID, AuthorID: sam.ID, Status: "Pausing.",
		Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch)
	h.Checkin(sam, launch.ID, domain.HealthYellow, "Slipping.", "Add a team.", testsupport.Epoch.AddDate(0, 1, 0))
	h.Clock.Advance(3 * day)

	rows := followUpsFor(t, h, sam).Rows

	if r := followUpRowOf(t, rows, stalled); r.Health != domain.HealthRed || followUpSignalOf(t, r, "path-overdue").Days != 3 {
		t.Errorf("stalled row = Health %q, path-overdue %+v; want Red, 3 days", r.Health, followUpSignalOf(t, r, "path-overdue"))
	}
	conflict := followUpSignalOf(t, followUpRowOf(t, rows, late), "schedule-conflicts")
	if conflict.Days != 30 || conflict.Related == nil || conflict.Related.ID != launch.ID {
		t.Errorf("schedule conflict = %+v, want 30 days after %q", conflict, launch.Title)
	}
	halted := followUpSignalOf(t, followUpRowOf(t, rows, underPaused), "halted-parents")
	if halted.Days != 0 || halted.Related == nil || halted.Related.ID != paused.ID {
		t.Errorf("halted parent = %+v, want %q", halted, paused.Title)
	}
	// launch is a parent with nothing wrong in its schedule, but its Yellow
	// Check-in puts it under Not Green, and its Path to Green isn't due yet.
	if got := followUpKinds(followUpRowOf(t, rows, launch)); !slices.Equal(got, []domain.FollowUpKind{"yellow"}) {
		t.Errorf("%q flagged for %v, want only yellow", launch.Title, got)
	}
	for _, r := range rows {
		if r.Goal.ID == paused.ID {
			t.Errorf("%q, an On Hold parent, is flagged: %v", paused.Title, followUpKinds(r))
		}
	}
}

// A date-only value counts its days as the date it is, wherever its reader
// is: behind UTC, on the evening of 8 March, when it is already 9 March in
// UTC, a Path to Green due 5 March, judged in its Owner's Timezone, and an
// Action Item due 5 March, judged in the org's, are each 3 days overdue, not
// 4.
func TestFollowUpsCountDaysOverdueAsDatesBehindUTC(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t, "ada@example.com")
	svc := domain.NewService(h.DB, h.Clock, h.Email, []string{"ada@example.com"}, domain.WithTimezone(la))
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SetTimezone(sam, "America/Los_Angeles")
	h.Clock.Set(time.Date(2026, 3, 2, 9, 0, 0, 0, la))
	march5 := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
	stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", march5)
	followed := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Followed up", "It matters."))
	h.Checkin(sam, followed.ID, domain.HealthGreen, "On track.", "", time.Time{})
	report := h.SaveReportDefinition(ada, domain.SaveReportDefinitionInput{
		Name: "Weekly",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{
			Attribute: domain.RuleLifecycle,
			Op:        domain.RuleIsAnyOf,
			Values:    []string{domain.LifecycleActive},
		}},
	})
	pub := h.PublishReport(ada, report)
	if _, err := h.Service.RaiseActionItem(context.Background(), ada.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: followed.ID, Text: "Name the second vendor.", OwnerID: sam.ID, DueDate: march5,
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	h.Clock.Set(time.Date(2026, 3, 8, 20, 0, 0, 0, la))

	f, err := svc.FollowUps(context.Background(), sam.ID)
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}

	if got := followUpSignalOf(t, followUpRowOf(t, f.Rows, stalled), "path-overdue").Days; got != 3 {
		t.Errorf("Path to Green due 5 March is %d days overdue on 8 March in Los Angeles; want 3", got)
	}
	if got := followUpSignalOf(t, followUpRowOf(t, f.Rows, followed), "action-item-overdue").Days; got != 3 {
		t.Errorf("Action Item due 5 March is %d days overdue on 8 March in Los Angeles; want 3", got)
	}
}

// The rows come most urgent first, key by key: Active Goals before the
// Proposed and On Hold ones; then Ownerless first; then Red, Yellow, no
// Health, Green; then a Path to Green overdue first, longest overdue first;
// then a Milestone overdue first; then Check-in overdue first; then by title,
// ignoring case. An On Hold row has no Health: its latest Check-in, which
// took it out of Active, set none.
func TestFollowUpsPutTheMostUrgentGoalFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	olga := h.SignIn("olga@example.com")
	monthly := func(owner domain.Account, g domain.Goal) {
		t.Helper()
		if _, err := h.Service.SetCadence(ctx, owner.ID, g.ID, 30); err != nil {
			t.Fatalf("SetCadence(%q): %v", g.Title, err)
		}
	}
	topLevel := func(owner domain.Account, title string) domain.Goal {
		t.Helper()
		return h.MarkTopLevel(ada, h.ActiveGoal(owner, title, "It matters."))
	}

	milestone, _ := h.GreenGoalWithOverdueMilestone(sam, "Green milestone")
	milestone = h.MarkTopLevel(ada, milestone)
	monthly(sam, milestone)
	now := h.Clock.Now()
	redLong := topLevel(sam, "red long overdue")
	h.Checkin(sam, redLong.ID, domain.HealthRed, "Blocked.", "Escalate.", now.AddDate(0, 0, 1))
	monthly(sam, redLong)
	red := topLevel(sam, "Red overdue")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", now.AddDate(0, 0, 3))
	monthly(sam, red)
	yellow := topLevel(sam, "Yellow")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", now.AddDate(0, 0, 30))
	monthly(sam, yellow)
	quiet := topLevel(sam, "Quiet")
	orphan := topLevel(olga, "Zzz orphan")
	h.Checkin(olga, orphan.ID, domain.HealthGreen, "On track.", "", time.Time{})
	monthly(olga, orphan)
	h.MarkDeparted(ada, olga)
	paused := topLevel(sam, "Paused")
	underPaused := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	h.Checkin(sam, underPaused.ID, domain.HealthGreen, "On track.", "", time.Time{})
	monthly(sam, underPaused)
	shelved := h.ActiveChildOf(sam, paused, "Aaa shelved", "Feeds paused.")
	for _, g := range []domain.Goal{shelved, paused} {
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID: g.ID, AuthorID: sam.ID, Status: "Pausing.",
			Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin On Hold %q: %v", g.Title, err)
		}
	}
	unlinked := h.ActiveGoal(sam, "Unlinked green", "It matters.")
	h.Checkin(sam, unlinked.ID, domain.HealthGreen, "On track.", "", time.Time{})
	monthly(sam, unlinked)
	h.Clock.Advance(8 * day)

	rows := followUpsFor(t, h, sam).Rows

	want := []domain.Goal{orphan, redLong, red, yellow, quiet, milestone, underPaused, unlinked, shelved}
	var got, wantTitles []string
	for _, r := range rows {
		got = append(got, r.Goal.Title)
	}
	for _, g := range want {
		wantTitles = append(wantTitles, g.Title)
	}
	if !slices.Equal(got, wantTitles) {
		t.Fatalf("rows:\n got %q\nwant %q", got, wantTitles)
	}
	if long, short := followUpSignalOf(t, rows[1], "path-overdue").Days, followUpSignalOf(t, rows[2], "path-overdue").Days; long != 7 || short != 5 {
		t.Errorf("Paths to Green overdue %d and %d days; want 7 and 5", long, short)
	}
	if r := rows[len(rows)-1]; r.Health != "" || !slices.Equal(followUpKinds(r), []domain.FollowUpKind{"halted-parents"}) {
		t.Errorf("On Hold row = Health %q, signals %v; want none, halted-parents", r.Health, followUpKinds(r))
	}
}

// The loader says what each row's Fix turns on: the Goals the viewer is a
// Delegate on, who nudged a row's Goal today, and whether an Ownerless Goal
// has nobody left to check in on it, none of its Delegates still in the
// company.
func TestFollowUpsSayWhatTheFixesTurnOn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	olga := h.SignIn("olga@example.com")
	rita := h.SignIn("rita@example.com")
	dan := h.SignIn("dan@example.com")
	morgan := h.SignIn("morgan@example.com")
	topLevel := func(owner domain.Account, title string) domain.Goal {
		t.Helper()
		return h.MarkTopLevel(ada, h.ActiveGoal(owner, title, "It matters."))
	}
	lapsed := topLevel(sam, "Lapsed")
	h.AddDelegate(sam, rita, lapsed.ID)
	covered := topLevel(olga, "Covered")
	h.AddDelegate(olga, rita, covered.ID)
	alone := topLevel(olga, "Alone")
	abandoned := topLevel(olga, "Abandoned")
	h.AddDelegate(olga, dan, abandoned.ID)
	h.MarkDeparted(ada, olga)
	h.MarkDeparted(ada, dan)
	h.Clock.Advance(10 * day)
	nudge := h.Nudge(morgan, lapsed.ID)

	f := followUpsFor(t, h, rita)

	if !f.Delegated[lapsed.ID] || !f.Delegated[covered.ID] || len(f.Delegated) != 2 {
		t.Errorf("Delegated = %v, want %q and %q", f.Delegated, lapsed.Title, covered.Title)
	}
	if r := followUpRowOf(t, f.Rows, lapsed); r.NudgedToday == nil || r.NudgedToday.Sender.ID != morgan.ID || r.NudgedToday.ID != nudge.ID {
		t.Errorf("%q nudged today = %+v, want by %s", lapsed.Title, r.NudgedToday, morgan.Email)
	}
	if r := followUpRowOf(t, f.Rows, covered); r.NudgedToday != nil {
		t.Errorf("%q nudged today = %+v, want nobody", covered.Title, r.NudgedToday)
	}
	for _, c := range []struct {
		goal        domain.Goal
		unreachable bool
	}{{lapsed, false}, {covered, false}, {alone, true}, {abandoned, true}} {
		if got := followUpRowOf(t, f.Rows, c.goal).Unreachable; got != c.unreachable {
			t.Errorf("%q unreachable = %v, want %v", c.goal.Title, got, c.unreachable)
		}
	}
}

// Each row carries what its viewer may do with its Goal, the Goal page's
// Permissions for them but No change, which Follow-ups doesn't offer, and each
// Schedule conflict what they may do with its parent; whether they own the
// Goal; and whether they may nudge it, as Service.Nudge would judge a Goal
// nobody has nudged today that isn't a parent in a Schedule conflict: for the
// Owner, a Delegate, an Admin and a reader alike.
func TestFollowUpsRowsCarryTheirViewersPermissions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	rita := h.SignIn("rita@example.com")
	pat := h.SignIn("pat@example.com")
	olga := h.SignIn("olga@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(pat, "Launch", june))
	h.AddDelegate(pat, morgan, launch.ID)
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 0, 30))
	h.AddDelegate(sam, morgan, late.ID)
	link := h.RequestLink(sam, late, launch, "")
	if _, err := h.Service.AcceptLink(ctx, link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	lapsed := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Lapsed", "It matters."))
	h.AddDelegate(sam, morgan, lapsed.ID)
	alone := h.MarkTopLevel(ada, h.ActiveGoal(olga, "Alone", "It matters."))
	blocked := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Blocked", "It matters."))
	h.MarkDeparted(ada, olga)
	h.Clock.Advance(10 * day)
	// Only Lapsed and Alone are Check-in overdue: Late piece is flagged for its
	// Schedule conflict alone, Launch not at all, and Blocked for its Health.
	h.Checkin(pat, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(sam, late.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(sam, blocked.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0))

	// permissionsOn are the Goal page's Permissions for viewer on g, but No
	// change.
	permissionsOn := func(viewer domain.Account, g domain.Goal) domain.GoalPermissions {
		t.Helper()
		p, err := h.Service.GoalPage(ctx, viewer.ID, g.ID)
		if err != nil {
			t.Fatalf("GoalPage for %s on %q: %v", viewer.Email, g.Title, err)
		}
		perms := p.Permissions
		perms.NoChange = false
		return perms
	}
	nudges := map[bool]int{}
	for _, viewer := range []domain.Account{sam, morgan, ada, rita} {
		rows := followUpsFor(t, h, viewer).Rows
		for _, g := range []domain.Goal{late, lapsed, alone, blocked} {
			followUpRowOf(t, rows, g)
		}
		for _, r := range rows {
			if r.Goal.ID == launch.ID {
				t.Fatalf("%q, a Schedule conflict's parent, is flagged: %v", launch.Title, followUpKinds(r))
			}
			if r.Permissions.NoChange {
				t.Errorf("%s's row for %q offers No change", viewer.Email, r.Goal.Title)
			}
			if want := permissionsOn(viewer, r.Goal); !reflect.DeepEqual(r.Permissions, want) {
				t.Errorf("%s's row for %q has Permissions %+v, want the Goal page's %+v", viewer.Email, r.Goal.Title, r.Permissions, want)
			}
			for _, sig := range r.Signals {
				if sig.Kind != "schedule-conflicts" {
					continue
				}
				if want := permissionsOn(viewer, *sig.Related); !reflect.DeepEqual(sig.RelatedPermissions, want) {
					t.Errorf("%s's row for %q has RelatedPermissions on %q %+v, want the Goal page's %+v", viewer.Email, r.Goal.Title, sig.Related.Title, sig.RelatedPermissions, want)
				}
			}
			if owned := r.Goal.Owner.ID == viewer.ID; r.OwnedByViewer != owned {
				t.Errorf("%s's row for %q: OwnedByViewer = %t, want %t", viewer.Email, r.Goal.Title, r.OwnedByViewer, owned)
			}
			var nudged error
			if err := h.Service.WithinTx(ctx, func(tx *domain.Service) error {
				_, nudged = tx.Nudge(ctx, viewer.ID, r.Goal.ID)
				return errRolledBack
			}); !errors.Is(err, errRolledBack) {
				t.Fatalf("Nudge by %s on %q: the transaction ended %v", viewer.Email, r.Goal.Title, err)
			}
			if refused := errors.Is(nudged, domain.ErrNotAuthorized) || errors.Is(nudged, domain.ErrValidation); nudged != nil && !refused {
				t.Fatalf("Nudge by %s on %q: %v", viewer.Email, r.Goal.Title, nudged)
			}
			if r.Nudge != (nudged == nil) {
				t.Errorf("%s's row for %q: Nudge = %t, but Service.Nudge answers %v", viewer.Email, r.Goal.Title, r.Nudge, nudged)
			}
			nudges[r.Nudge]++
		}
	}
	if nudges[true] == 0 || nudges[false] == 0 {
		t.Errorf("rows offering a Nudge: %d, not offering one: %d; want some of each", nudges[true], nudges[false])
	}
}

// A Goal waits on a Check-in when it is Check-in overdue, past its Path to
// Green or has a Milestone overdue, its Freshness and its Follow-ups row
// agreeing, and only then: being Red, an overdue Action Item or a Schedule
// conflict isn't waiting on one (CONTEXT.md: Nudge).
func TestFollowUpsSayWhatWaitsOnACheckin(t *testing.T) {
	t.Parallel()

	wantKinds := []domain.FollowUpKind{domain.FollowUpCheckinOverdue, domain.FollowUpPathOverdue, domain.FollowUpMilestoneOverdue}
	if got := domain.WaitsOnCheckinKinds(); !slices.Equal(got, wantKinds) {
		t.Errorf("WaitsOnCheckinKinds() = %v, want %v", got, wantKinds)
	}
	// Each call answers its own copy: changing one leaves the next whole.
	changed := domain.WaitsOnCheckinKinds()
	changed[0] = domain.FollowUpRed
	if got := domain.WaitsOnCheckinKinds(); !slices.Equal(got, wantKinds) {
		t.Errorf("after changing one answer, WaitsOnCheckinKinds() = %v, want %v", got, wantKinds)
	}

	for _, tc := range []struct {
		name      string
		freshness domain.Freshness
		want      bool
	}{
		{"Check-in overdue", domain.Freshness{CheckinOverdue: true}, true},
		{"Path to Green overdue", domain.Freshness{PathToGreenOverdue: true}, true},
		{"Milestone overdue", domain.Freshness{MilestoneOverdue: true}, true},
		{"none", domain.Freshness{DaysSince: 3, CadenceDays: 7}, false},
	} {
		if got := tc.freshness.WaitsOnCheckin(); got != tc.want {
			t.Errorf("Freshness %s: WaitsOnCheckin() = %t, want %t", tc.name, got, tc.want)
		}
	}

	for _, kind := range wantKinds {
		r := domain.FollowUpRow{Signals: []domain.FollowUpSignal{{Kind: domain.FollowUpUnlinked}, {Kind: kind, Days: 2}}}
		if !r.WaitsOnCheckin() {
			t.Errorf("row with %s: WaitsOnCheckin() = false, want true", kind)
		}
	}
	for _, kind := range []domain.FollowUpKind{domain.FollowUpRed, domain.FollowUpActionItemOverdue, domain.FollowUpScheduleConflicts} {
		r := domain.FollowUpRow{Signals: []domain.FollowUpSignal{{Kind: kind, Days: 2}}}
		if r.WaitsOnCheckin() {
			t.Errorf("row with only %s: WaitsOnCheckin() = true, want false", kind)
		}
	}
}
