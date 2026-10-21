package web_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// followUpRows returns every row of the Follow-ups page's table.
func followUpRows(page string) []string {
	var rows []string
	for rest := page; ; {
		start := strings.Index(rest, `<tr data-testid="follow-up-row"`)
		if start < 0 {
			return rows
		}
		end := strings.Index(rest[start:], "</tr>")
		if end < 0 {
			return append(rows, rest[start:])
		}
		rows = append(rows, rest[start:start+end])
		rest = rest[start+end:]
	}
}

// followUpRowOf returns the Follow-ups page's one row for g, failing the test unless
// the table holds exactly one row linking to it.
func followUpRowOf(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	var found []string
	for _, row := range followUpRows(page) {
		if isFollowUpRowOf(row, g) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Follow-ups table has %d rows for %q, want 1:\n%s", len(found), g.Title, page)
	}
	return found[0]
}

// hasFollowUpRow reports whether the Follow-ups page's table has a row for g.
func hasFollowUpRow(page string, g domain.Goal) bool {
	return slices.ContainsFunc(followUpRows(page), func(row string) bool { return isFollowUpRowOf(row, g) })
}

// isFollowUpRowOf reports whether a Follow-ups table row is g's: its Goal cell links
// to g.
func isFollowUpRowOf(row string, g domain.Goal) bool {
	return strings.Contains(row, `<td data-testid="follow-up-goal" class="rk-goal">`+`<a `+navTo(g.ID)+`>`)
}

// followUpChip returns a row's chip for one signal kind.
func followUpChip(t *testing.T, row, kind string) string {
	t.Helper()
	start := strings.Index(row, `<span data-testid="follow-up-signal" data-kind="`+kind+`"`)
	if start < 0 {
		t.Fatalf("row has no %s chip:\n%s", kind, row)
	}
	end := strings.Index(row[start:], "</span>")
	if end < 0 {
		t.Fatalf("the %s chip is not closed:\n%s", kind, row)
	}
	return row[start : start+end]
}

// The Follow-ups page is served at /follow-ups, headed and titled Follow-ups,
// with its nav item marked current. The page's old address, under its old
// name, Risks, is gone: no redirect, just Not found.
func TestFollowUpsPageIsAtFollowUps(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups")
	if !strings.Contains(page, "<title>Follow-ups · Goal Tracker</title>") || !strings.Contains(page, "<h1>Follow-ups</h1>") {
		t.Errorf("/follow-ups isn't titled and headed Follow-ups:\n%s", page)
	}
	nav := pageElement(t, page, "a", "nav-follow-ups")
	if !strings.Contains(nav, `href="/follow-ups"`) || !strings.Contains(nav, `aria-current="page"`) || !strings.HasSuffix(nav, ">Follow-ups") {
		t.Errorf("the nav's Follow-ups item isn't a current link to /follow-ups: %s", nav)
	}
	assertNotFoundPageFor(t, client, http.MethodGet, ts.URL+"/"+strings.ToLower("Risks"), "/home")
}

// The Follow-ups page lists each Check-in overdue Goal once, with its Owner, "You"
// for the viewer's own, and a Check-in overdue chip saying how long it has gone
// without a Check-in against its cadence. A fresh Goal isn't Check-in overdue.
func TestFollowUpsPageListsCheckinOverdueGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, silent)
	if chip := followUpChip(t, row, "checkin-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Check-in overdue · 10 days, checks in weekly</button>") {
		t.Errorf("Check-in overdue chip is not a Check-in overdue badge saying 10 days, checks in weekly: %s", chip)
	}
	if owner := pageElement(t, row, "td", "follow-up-owner"); !strings.HasSuffix(owner, ">You") || strings.Contains(owner, "sam@example.com") {
		t.Errorf("the viewer's own Goal isn't Owned by You: %s", owner)
	}
	if row := followUpRowOf(t, page, fresh); strings.Contains(row, `data-kind="checkin-overdue"`) {
		t.Errorf("%q, which checked in today, is Check-in overdue:\n%s", fresh.Title, row)
	}
}

// A Check-in overdue chip says in words how long its Goal has gone without a
// Check-in and how often it checks in, naming its cadence as the New goal
// form's chips do, and counting the days of any other.
func TestFollowUpsCheckinOverdueChipSaysItsCadenceInWords(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	want := map[int]string{
		7:  "Check-in overdue · 42 days, checks in weekly",
		14: "Check-in overdue · 42 days, checks in every 2 weeks",
		30: "Check-in overdue · 42 days, checks in monthly",
		3:  "Check-in overdue · 42 days, checks in every 3 days",
	}
	goals := map[int]domain.Goal{}
	for cadence := range want {
		g := h.ActiveGoal(sam, "Every "+strconv.Itoa(cadence)+" days", "It matters.")
		if _, err := h.Service.SetCadence(t.Context(), g.ID, cadence); err != nil {
			t.Fatalf("SetCadence: %v", err)
		}
		goals[cadence] = g
	}
	h.Clock.Advance(42 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for cadence, g := range goals {
		chip := followUpChip(t, followUpRowOf(t, page, g), "checkin-overdue")
		if !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">"+want[cadence]+"</button>") {
			t.Errorf("on a %d-day cadence, the Check-in overdue chip doesn't read %q: %s", cadence, want[cadence], chip)
		}
	}
}

// The Follow-ups page lists each Goal still not Green past its Path to Green's
// target date with its Health and a chip saying how many days overdue it is.
// A Green Goal isn't overdue.
func TestFollowUpsPageListsOverduePathsToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	green := h.ActiveGoal(sam, "On track", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Clock.Advance(5 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, stalled)
	if chip := followUpChip(t, row, "path-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Path to Green 2 days overdue</button>") {
		t.Errorf("overdue chip is not a Check-in overdue-colored badge saying 2 days overdue: %s", chip)
	}
	if health := pageElement(t, row, "span", "follow-up-health"); !strings.Contains(openTag(health), `class="badge r"`) {
		t.Errorf("row doesn't show the Goal's Red Health: %s", health)
	}
	if row := followUpRowOf(t, page, green); strings.Contains(row, `data-kind="path-overdue"`) {
		t.Errorf("Green %q is overdue:\n%s", green.Title, row)
	}
}

// A Path to Green overdue chip counts its days in the Goal's Owner's Timezone,
// as the flag is judged (CONTEXT.md: Timezone): just past midnight in Tokyo it
// is a day further overdue than for an Owner with no Timezone, counted in the
// company's UTC.
func TestFollowUpsPathToGreenDaysAreCountedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	target := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	tokyo := h.ActiveGoal(kenji, "Tokyo recovery", "It matters.")
	h.Checkin(kenji, tokyo.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	plain := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, plain.ID, domain.HealthRed, "Blocked.", "Escalate.", target)
	h.Clock.Set(time.Date(2026, 1, 7, 15, 4, 0, 0, time.UTC)) // Jan 8, 12:04am in Tokyo

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	if chip := followUpChip(t, followUpRowOf(t, page, tokyo), "path-overdue"); !strings.HasSuffix(chip, ">Path to Green 3 days overdue</button>") {
		t.Errorf("overdue chip of an Owner in Tokyo doesn't say 3 days overdue: %s", chip)
	}
	if chip := followUpChip(t, followUpRowOf(t, page, plain), "path-overdue"); !strings.HasSuffix(chip, ">Path to Green 2 days overdue</button>") {
		t.Errorf("overdue chip of an Owner with no Timezone doesn't say 2 days overdue: %s", chip)
	}
}

// A Milestone overdue chip counts its days in the Goal's Owner's Timezone, as
// the flag is judged (CONTEXT.md: Timezone): at 1am in Tokyo it is a day
// further overdue than once the Owner has no Timezone, counted in the
// company's UTC.
func TestFollowUpsMilestoneOverdueDaysAreCountedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	g, beta := h.GreenGoalWithOverdueMilestone(kenji, "Ship search")
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1).Add(16 * time.Hour)) // Beta + 2 days, 1am in Tokyo
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")
	if chip := followUpChip(t, followUpRowOf(t, page, g), "milestone-overdue"); !strings.HasSuffix(chip, ">Milestone 2 days overdue</button>") {
		t.Errorf("Milestone overdue chip of an Owner in Tokyo doesn't say 2 days overdue: %s", chip)
	}

	h.SetTimezone(kenji, "")
	page = getBody(t, client, ts.URL+"/follow-ups?org=all")
	if chip := followUpChip(t, followUpRowOf(t, page, g), "milestone-overdue"); !strings.HasSuffix(chip, ">Milestone 1 day overdue</button>") {
		t.Errorf("Milestone overdue chip of an Owner with no Timezone doesn't say 1 day overdue: %s", chip)
	}
}

// A Path to Green overdue chip says in words how many days overdue it is.
func TestFollowUpsPathToGreenChipSaysItsDaysInWords(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	start := h.Clock.Now()
	want := map[int]string{12: "Path to Green 12 days overdue", 1: "Path to Green 1 day overdue"}
	goals := map[int]domain.Goal{}
	for overdue := range want {
		g := h.ActiveGoal(sam, strconv.Itoa(overdue)+" days overdue", "It matters.")
		h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", start.AddDate(0, 0, 1-overdue))
		goals[overdue] = g
	}
	h.Clock.Advance(day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for overdue, g := range goals {
		if chip := followUpChip(t, followUpRowOf(t, page, g), "path-overdue"); !strings.HasSuffix(chip, ">"+want[overdue]+"</button>") {
			t.Errorf("%d days overdue, the chip doesn't read %q: %s", overdue, want[overdue], chip)
		}
	}
}

// The Follow-ups page lists each Active Goal whose Owner has left the company with an
// Ownerless chip as prominent as Red, and the departed Owner, who isn't the
// viewer. A Proposed Goal of the same departed Owner isn't listed, nor is an
// Active Goal with a present Owner Ownerless.
func TestFollowUpsPageListsOwnerlessGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	orphaned := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	proposed := h.CreateGoal(sam, "Someday", "Maybe.")
	owned := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/follow-ups?org=all")

	row := followUpRowOf(t, page, orphaned)
	chip := followUpChip(t, row, "ownerless")
	if !strings.Contains(openTag(chip), `class="badge ol"`) || !strings.HasSuffix(chip, ">Ownerless</button>") {
		t.Errorf("Ownerless chip is not an Ownerless badge: %s", chip)
	}
	if owner := pageElement(t, row, "td", "follow-up-owner"); !strings.Contains(owner, `title="sam@example.com"`) || strings.Contains(owner, "You") {
		t.Errorf("row doesn't show the departed Owner: %s", owner)
	}
	if hasFollowUpRow(page, proposed) {
		t.Errorf("the Follow-ups table lists Proposed %q", proposed.Title)
	}
	if row := followUpRowOf(t, page, owned); strings.Contains(row, `data-kind="ownerless"`) {
		t.Errorf("%q, whose Owner is present, is Ownerless:\n%s", owned.Title, row)
	}
}

// The Follow-ups page chips each Unlinked Goal with an outlined chip saying why.
// A Top-level Goal and a Goal that contributes to it aren't Unlinked.
func TestFollowUpsPageListsUnlinkedGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	aligned := h.ActiveChildOf(sam, top, "Ship search", "People can't find things.")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, loner)
	if chip := followUpChip(t, row, "unlinked"); !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.HasSuffix(chip, ">Unlinked · no parent Goal, not Top-level</button>") {
		t.Errorf("Unlinked chip is not an outlined chip saying why: %s", chip)
	}
	rule, _, _ := strings.Cut(page[strings.Index(page, ".rk-tag{")+1:], "}")
	if !strings.Contains(rule, "border-radius:var(--radius-sm)") || !strings.Contains(rule, "border:1px solid var(--color-border-strong)") {
		t.Errorf("the outlined chip isn't outlined with the small radius: %s", rule)
	}
	for _, g := range []domain.Goal{top, aligned} {
		if hasFollowUpRow(page, g) {
			t.Errorf("the Follow-ups table lists %q", g.Title)
		}
	}
}

// The Follow-ups page chips each Goal due later than a Goal it contributes to with
// its delivery date, how many days after its parent's that is, and a link to
// the parent. A date in another year carries its year.
func TestFollowUpsPageListsScheduleConflicts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	early := h.ActiveGoalDue(sam, "Early piece", june.AddDate(0, -1, 0))
	h.RequestLink(sam, early, launch, "")
	yearEnd := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Year-end close", time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)))
	slipped := h.ActiveGoalDue(sam, "Slipped piece", time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC))
	h.RequestLink(sam, slipped, yearEnd, "")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		child, parent domain.Goal
		says          string
	}{
		{late, launch, ">Schedule conflict</button> · due Jul 1, 30 days after its parent <a "},
		{slipped, yearEnd, ">Schedule conflict</button> · due Jan 10, 2027, 21 days after its parent <a "},
	} {
		chip := followUpChip(t, followUpRowOf(t, page, c.child), "schedule-conflicts")
		if !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.Contains(chip, c.says) {
			t.Errorf("%q's chip is not an outlined chip saying %q: %s", c.child.Title, c.says, chip)
		}
		if !strings.HasSuffix(chip, navTo(c.parent.ID)+">"+html.EscapeString(c.parent.Title)+"</a>") {
			t.Errorf("%q's chip doesn't link to its parent %q: %s", c.child.Title, c.parent.Title, chip)
		}
	}
	if hasFollowUpRow(page, early) && strings.Contains(followUpRowOf(t, page, early), `data-kind="schedule-conflicts"`) {
		t.Errorf("%q, due before its parent, is a schedule conflict", early.Title)
	}
}

// The Follow-ups page chips each Goal contributing to a Goal that is On Hold or
// Cancelled with its parent's Lifecycle and a link to the parent. A Goal under
// an Active parent isn't chipped.
func TestFollowUpsPageListsGoalsUnderHaltedParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	live := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Live outcome", "It matters."))
	underLive := h.ActiveChildOf(sam, live, "Under live", "Feeds live.")
	paused := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Paused outcome", "It mattered."))
	underPaused := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	dropped := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Dropped outcome", "It mattered."))
	underDropped := h.ActiveChildOf(sam, dropped, "Under dropped", "Feeds dropped.")
	for parent, lifecycle := range map[int64]string{paused.ID: domain.LifecycleOnHold, dropped.ID: domain.LifecycleCancelled} {
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID:          parent,
			AuthorID:        sam.ID,
			Status:          "Stopping.",
			Lifecycle:       lifecycle,
			LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin %s: %v", lifecycle, err)
		}
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		child, parent domain.Goal
		says          string
	}{
		{underPaused, paused, ">Parent On Hold</button> · <a "},
		{underDropped, dropped, ">Parent Cancelled</button> · <a "},
	} {
		chip := followUpChip(t, followUpRowOf(t, page, c.child), "halted-parents")
		if !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.Contains(chip, c.says) {
			t.Errorf("%q's chip is not an outlined chip saying %q: %s", c.child.Title, c.says, chip)
		}
		if !strings.HasSuffix(chip, navTo(c.parent.ID)+">"+c.parent.Title+"</a>") {
			t.Errorf("%q's chip doesn't link to its parent %q: %s", c.child.Title, c.parent.Title, chip)
		}
	}
	if hasFollowUpRow(page, underLive) {
		t.Errorf("the Follow-ups table lists %q, whose parent is Active", underLive.Title)
	}
}

// The Follow-ups page lists a Goal flagged by two signals once, in one table
// inside a card that scrolls sideways on a narrow screen: one row with both
// chips, a neutral dash for no Health, and one Fix: Check in for its Owner,
// though it is Unlinked too.
func TestFollowUpsPageListsEachFlaggedGoalOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if n := strings.Count(page, `<table id="follow-ups-table"`); n != 1 {
		t.Fatalf("page has %d Follow-ups tables, want 1", n)
	}
	if !strings.Contains(page, `<div class="card"><table id="follow-ups-table"`) {
		t.Errorf("the Follow-ups table doesn't sit straight inside a card")
	}
	if want := `<tr><th>Health</th><th>Goal</th><th>Owner</th><th aria-label="Next step"></th></tr>`; !strings.Contains(page, want) {
		t.Errorf("the Follow-ups table's head isn't Health, Goal, Owner and a Next step column:\n%s", page)
	}
	if rows := followUpRows(page); len(rows) != 1 {
		t.Fatalf("the Follow-ups table has %d rows, want 1", len(rows))
	}
	row := followUpRowOf(t, page, silent)
	if n := strings.Count(row, `data-testid="follow-up-signal"`); n != 2 {
		t.Errorf("row has %d chips, want 2:\n%s", n, row)
	}
	for _, kind := range []string{"checkin-overdue", "unlinked"} {
		followUpChip(t, row, kind)
	}
	if health := row[strings.Index(row, `data-testid="follow-up-health"`):strings.Index(row, `data-testid="follow-up-goal"`)]; !strings.Contains(health, `class="badge lc"`) || !strings.Contains(health, "—") {
		t.Errorf("row doesn't show a neutral dash for no Health: %s", health)
	}
	if label, href, _ := followUpFix(t, row); label != "Check in" || href != "/goals/"+strconv.FormatInt(silent.ID, 10)+"/checkin" {
		t.Errorf("the Owner's Check-in overdue and Unlinked Goal's Fix is %q → %s, want Check in", label, href)
	}
	for _, gone := range []string{"follow-ups-nothing-in", `class="rk-empty"`, `data-testid="follow-ups-checkin-overdue"`, "Why it's flagged"} {
		if strings.Contains(page, gone) {
			t.Errorf("page still has %s", gone)
		}
	}
}

// With nothing flagged, the Follow-ups page heads with no count and says so in one
// line, with no table, no group cards and nothing to clear.
func TestFollowUpsPageWithNothingFlaggedSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	if n := strings.Count(page, `data-testid="follow-ups-empty"`); n != 1 {
		t.Fatalf("page has %d empty lines, want 1", n)
	}
	if empty := strings.TrimSpace(pageElement(t, page, "p", "follow-ups-empty")); !strings.HasSuffix(empty, ">No Goals need attention.") {
		t.Errorf("the empty line doesn't say no Goals need attention: %s", empty)
	}
	for _, gone := range []string{"<table", `data-testid="follow-ups-attention"`, `data-testid="follow-ups-groups"`, `data-testid="follow-ups-group-`, "Show everything", "follow-ups-all-clear", "follow-ups-tile-"} {
		if strings.Contains(page, gone) {
			t.Errorf("with nothing flagged, the page has %s", gone)
		}
	}
	if followUps := pageElement(t, page, "a", "nav-follow-ups"); strings.Contains(followUps, "count") {
		t.Errorf("Follow-ups shows a count with nothing flagged: %s", followUps)
	}
}

// A group card holding no Goals while another holds some is muted, links
// nowhere, and says why it is empty.
func TestFollowUpsPageMutesAGroupWithNoGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if strings.Contains(page, `<a data-testid="follow-ups-group-admin"`) {
		t.Errorf("the empty admin card is a link")
	}
	card := pageElement(t, page, "div", "follow-ups-group-admin")
	if !strings.Contains(openTag(card), "rk-zero") || !strings.Contains(card, "Nothing needs an Admin.") || strings.Contains(card, "href") {
		t.Errorf("the empty admin card isn't muted, saying nothing needs an Admin, and linking nowhere: %s", card)
	}
	if !strings.Contains(page, ".rk-zero{border:1px dashed var(--color-border-strong);color:var(--color-ink-muted)}") {
		t.Errorf("an empty card doesn't take a dashed edge and muted text")
	}
}

// The Follow-ups page heads with how many Goals there are to review, most urgent
// first, in the singular for one Goal, and no longer says what's going wrong.
func TestFollowUpsPageHeadsWithHowManyGoalsToReview(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "><strong>1</strong> Goal to review, most urgent first.") {
		t.Errorf("header doesn't say 1 Goal to review, most urgent first: %s", head)
	}
	if strings.Contains(html.UnescapeString(page), "What's going wrong") {
		t.Errorf("page still says what's going wrong")
	}

	h.ActiveGoal(sam, "More silent work", "It matters.")
	h.Clock.Advance(10 * day)
	page = getBody(t, client, ts.URL+"/follow-ups?org=all")

	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "><strong>2</strong> Goals to review, most urgent first.") {
		t.Errorf("header doesn't say 2 Goals to review, most urgent first: %s", head)
	}
}

// Beside its count line, the Follow-ups page has a "What each signal means"
// button opening, through a native popover and no script, every signal's
// definition, once each, in the page's order. No list of them waits at the
// page's foot.
func TestFollowUpsPageDefinesEachSignalBesideItsCount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	head := between(t, page, `<div class="rk-head">`, "</div>")
	if !strings.Contains(head, `data-testid="follow-ups-attention"`) {
		t.Fatalf("the page's head has no count line:\n%s", head)
	}
	at := strings.Index(head, ">What each signal means</button>")
	if at < 0 {
		t.Fatalf("no What each signal means button beside the count line:\n%s", head)
	}
	button := head[strings.LastIndex(head[:at], "<button"):at]
	if attr(button, "type") != "button" || attr(button, "popovertarget") == "" {
		t.Fatalf("What each signal means isn't a button opening a popover: %s", button)
	}
	list := html.UnescapeString(followUpPopover(t, page, attr(button, "popovertarget")))
	var want strings.Builder
	for _, d := range web.FollowUpSignalDefinitions {
		want.WriteString("<dt>" + d.Name + "</dt><dd>" + d.Means + "</dd>")
	}
	if got := strings.TrimSpace(list); got != "<dl>"+want.String()+"</dl>" {
		t.Errorf("the list doesn't define each signal once, in the page's order:\n%s\nwant\n<dl>%s</dl>", got, want.String())
	}
	for _, gone := range []string{`data-testid="follow-ups-definitions"`, "What each risk means"} {
		if strings.Contains(page, gone) {
			t.Errorf("the page still has its old list of definitions, %s", gone)
		}
	}
}

// The Follow-ups page heads with how many Goals need attention, each once, then a
// card per group of signals, Not Green first, then by who acts: each counts the Goals it holds once,
// chips each of its signals with how many Goals it flags, and links to the
// page filtered to the group.
func TestFollowUpsPageSummarizesGroupsByWhoActs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue, Unlinked and an overdue Action Item
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	raiseFollowUpActionItem(t, h, ada, silent, sam, "Break the silence.", -1)
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")
	stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters.")) // Red
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", june)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.Contains(head, "<strong>4</strong> Goals to review,") {
		t.Errorf("header does not count the 4 flagged Goals: %s", head)
	}
	last := -1
	for _, g := range []struct {
		key, name, href string
		count           int
		chips           []string
	}{
		{"health", "Not Green", "/follow-ups?group=health&amp;org=all", 1, []string{"Red · 1"}},
		{"owner", "Waiting on a Check-in", "/follow-ups?group=owner&amp;org=all", 1, []string{"Check-in overdue · 1"}},
		{"followup", "Action Items overdue", "/follow-ups?group=followup&amp;org=all", 1, []string{"Action Item overdue · 1"}},
		{"plan", "Placement and dates", "/follow-ups?group=plan&amp;org=all", 3, []string{"Unlinked · 2", "Schedule conflicts · 1"}},
		{"admin", "Needs an Admin", "/follow-ups?group=admin&amp;org=all", 1, []string{"Ownerless · 1"}},
	} {
		card := pageElement(t, page, "a", "follow-ups-group-"+g.key)
		for _, want := range append([]string{
			`href="` + g.href + `"`,
			html.EscapeString(g.name),
			`<span class="num">` + strconv.Itoa(g.count) + `</span>`,
		}, g.chips...) {
			if !strings.Contains(card, want) {
				t.Errorf("%s card lacks %s: %s", g.key, want, card)
			}
		}
		if strings.Contains(openTag(card), "aria-current") {
			t.Errorf("%s card is current with no filter: %s", g.key, openTag(card))
		}
		at := strings.Index(page, `data-testid="follow-ups-group-`+g.key+`"`)
		if at < last {
			t.Errorf("%s card is out of order", g.key)
		}
		last = at
	}
	for _, c := range []struct{ group, kind, class string }{
		{"health", "red", `class="badge r"`},
		{"owner", "checkin-overdue", `class="badge st"`},
		{"followup", "action-item-overdue", `class="badge st"`},
		{"plan", "unlinked", `class="rk-tag"`},
		{"plan", "schedule-conflicts", `class="rk-tag"`},
		{"admin", "ownerless", `class="badge ol"`},
	} {
		card := pageElement(t, page, "a", "follow-ups-group-"+c.group)
		if chip := followUpChip(t, card, c.kind); !strings.Contains(openTag(chip), c.class) {
			t.Errorf("the %s card's %s chip isn't drawn like the table's %s: %s", c.group, c.kind, c.class, chip)
		}
	}
	if strings.Contains(page, "Show all") {
		t.Errorf("page offers Show all with no filter")
	}
}

// A group card's address filters the page to the signals in that group, with
// no script: the table lists only the Goals they flag, chipped with them
// alone, the card is current under its name, the cards still count every group, and Show all
// clears the filter. An unknown group shows everything.
func TestFollowUpsPageFiltersToAGroup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	orphaned := h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?group=owner&org=all")

	row := followUpRowOf(t, page, silent)
	followUpChip(t, row, "checkin-overdue")
	if strings.Contains(row, `data-kind="unlinked"`) {
		t.Errorf("filtered to owner, %q still has its Unlinked chip:\n%s", silent.Title, row)
	}
	if hasFollowUpRow(page, orphaned) {
		t.Errorf("filtered to owner, the page lists %q, flagged only outside it", orphaned.Title)
	}
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.Contains(head, "<strong>2</strong>") {
		t.Errorf("filtered to owner, the header doesn't still count both flagged Goals: %s", head)
	}
	for key, want := range map[string]string{"owner": "1", "plan": "2", "admin": "1"} {
		card := pageElement(t, page, "a", "follow-ups-group-"+key)
		if !strings.Contains(card, `<span class="num">`+want+`</span>`) {
			t.Errorf("filtered to owner, the %s card doesn't count %s: %s", key, want, card)
		}
		if current := strings.Contains(openTag(card), `aria-current="page"`); current != (key == "owner") {
			t.Errorf("filtered to owner, the %s card's aria-current is %v: %s", key, current, openTag(card))
		}
	}
	if all := pageElement(t, page, "a", "follow-ups-show-all"); !strings.Contains(all, `href="/follow-ups?org=all"`) || !strings.Contains(all, "Show all") {
		t.Errorf("Show all doesn't clear the filter: %s", all)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-owner"); !strings.Contains(card, "Waiting on a Check-in") {
		t.Errorf("filtered to owner, the current card isn't Waiting on a Check-in: %s", card)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?group=plan&org=all")

	for _, g := range []domain.Goal{silent, orphaned} {
		row := followUpRowOf(t, page, g)
		followUpChip(t, row, "unlinked")
		for _, other := range []string{"checkin-overdue", "ownerless"} {
			if strings.Contains(row, `data-kind="`+other+`"`) {
				t.Errorf("filtered to plan, %q still has its %s chip:\n%s", g.Title, other, row)
			}
		}
	}
	for _, key := range []string{"owner", "plan", "admin"} {
		card := pageElement(t, page, "a", "follow-ups-group-"+key)
		if current := strings.Contains(openTag(card), `aria-current="page"`); current != (key == "plan") {
			t.Errorf("filtered to plan, the %s card's aria-current is %v: %s", key, current, openTag(card))
		}
	}
	if card := pageElement(t, page, "a", "follow-ups-group-plan"); !strings.Contains(card, "Placement and dates") {
		t.Errorf("filtered to plan, the current card isn't Placement and dates: %s", card)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?group=bogus&org=all")

	followUpChip(t, followUpRowOf(t, page, silent), "unlinked")
	followUpChip(t, followUpRowOf(t, page, orphaned), "ownerless")
	for _, key := range []string{"owner", "plan", "admin"} {
		if card := pageElement(t, page, "a", "follow-ups-group-"+key); strings.Contains(openTag(card), "aria-current") {
			t.Errorf("an unknown group marks the %s card current: %s", key, openTag(card))
		}
	}
	if strings.Contains(page, `data-testid="follow-ups-show-all"`) {
		t.Errorf("an unknown group offers Show all")
	}
}

// Scoped and filtered to nothing while other Goals are flagged, the Follow-ups page
// says where nothing needs attention, Mine then the value then the group, and
// links to everything, Everyone's, with no count, table or cards. Needs an
// Admin, company-wide, says nothing needs an Admin in the value alone, and
// has nowhere wider to link.
func TestFollowUpsPageFilteredToNothingSaysWhere(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	own := h.ActiveGoal(sam, "Own work", "It matters.") // Unlinked only
	h.ActiveGoal(kim, "Kim's work", "It matters.")      // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.Checkin(sam, own.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	for address, want := range map[string]string{
		"/follow-ups?org=mine&group=owner":                                                  "No Goals need attention in Mine, Waiting on a Check-in.",
		"/follow-ups?group=admin&value=" + strconv.FormatInt(platform.ID, 10) + "&org=mine": "Nothing needs an Admin in Team Platform.",
		"/follow-ups?org=all&value=" + strconv.FormatInt(platform.ID, 10):                   "No Goals need attention in Team Platform.",
	} {
		page := getBody(t, client, ts.URL+address)

		empty := html.UnescapeString(pageElement(t, page, "p", "follow-ups-empty"))
		if strings.HasPrefix(address, "/follow-ups?group=admin") {
			if !strings.HasSuffix(strings.TrimSpace(empty), ">"+want) {
				t.Errorf("at %s, the empty line doesn't say only %q: %s", address, want, empty)
			}
		} else if !strings.Contains(empty, ">"+want+" ") {
			t.Errorf("at %s, the empty line doesn't say %q: %s", address, want, empty)
		} else if link := between(t, empty, "<a ", ""); attr(link, "href") != "/follow-ups?org=all" || !strings.HasSuffix(link, ">Show everything</a>") {
			t.Errorf("at %s, the empty line doesn't end linking to everything: %s", address, empty)
		}
		for _, gone := range []string{"<table", `data-testid="follow-ups-attention"`, `data-testid="follow-ups-groups"`, `data-testid="follow-ups-group-`, "follow-ups-show-all", "follow-ups-group-blank"} {
			if strings.Contains(page, gone) {
				t.Errorf("at %s, the page has %s", address, gone)
			}
		}
	}
}

// Every page's top bar has Follow-ups after Goals, counting the Goals the Follow-ups page
// flags in the viewer's default scope, each once however many sections list
// it: Ada's org's 3 for Ada, who manages Sam and Kim, Sam's own 2 for Sam. The
// nav doesn't link to the Graph or Freshness signals pages it replaces.
func TestNavCountsFlaggedGoalsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.SetManager(sam, ada)
	h.SetManager(kim, ada)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")

	for who, want := range map[string]string{"sam@example.com": "2", "ada@example.com": "3"} {
		client := signInClient(t, ts.URL, who)
		for _, path := range []string{"/home", "/goals", "/follow-ups"} {
			page := getBody(t, client, ts.URL+path)
			nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
			followUps := pageElement(t, page, "a", "nav-follow-ups")
			if !strings.Contains(followUps, `href="/follow-ups"`) || !strings.Contains(followUps, `<span class="count">`+want+`</span>`) {
				t.Errorf("%s on %s: Follow-ups does not count the %s flagged Goals: %s", who, path, want, followUps)
			}
			at := strings.Index(nav, `data-testid="nav-follow-ups"`)
			for _, before := range []string{"nav-home", "nav-goals"} {
				if i := strings.Index(nav, `data-testid="`+before+`"`); i > at {
					t.Errorf("%s on %s: Follow-ups comes before %s:\n%s", who, path, before, nav)
				}
			}
			for _, gone := range []string{`href="/signals"`, `href="/freshness"`} {
				if strings.Contains(nav, gone) {
					t.Errorf("%s on %s: the top bar still links %s", who, path, gone)
				}
			}
		}
	}
}

// A Goal's page shows its freshness and graph signals as alert banners,
// Check-in overdue in its own color, and the Goal list marks a Check-in overdue
// or overdue row with a chip in the same color saying how long. The
// data-testids other tests find them by don't change.
func TestFollowUpFlagsUseAlertBannersAndChips(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	silent := h.ActiveGoalDue(sam, "Silent work", june.AddDate(0, 1, 0))
	h.Clock.Advance(16 * day)
	parent := h.ActiveGoalDue(sam, "Launch", june)
	h.RequestLink(sam, silent, parent, "")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", testsupport.Epoch)

	client := signInClient(t, ts.URL, "sam@example.com")
	silentPage := getBody(t, client, goalPageURL(ts.URL, silent))
	for testID, class := range map[string]string{
		"goal-checkin-overdue":   `class="alert st"`,
		"goal-schedule-conflict": `class="alert lc"`,
	} {
		if el := pageElement(t, silentPage, "p", testID); !strings.Contains(openTag(el), class) {
			t.Errorf("%s banner is not %s: %s", testID, class, openTag(el))
		}
	}
	if !strings.Contains(silentPage, `data-testid="goal-signals"`) {
		t.Errorf("Goal page lost its goal-signals group")
	}
	parentPage := getBody(t, client, goalPageURL(ts.URL, parent))
	if el := pageElement(t, parentPage, "p", "goal-unlinked"); !strings.Contains(openTag(el), `class="alert lc"`) {
		t.Errorf("Unlinked banner is not an alert: %s", openTag(el))
	}
	stalledPage := getBody(t, client, goalPageURL(ts.URL, stalled))
	if el := pageElement(t, stalledPage, "p", "goal-path-overdue"); !strings.Contains(openTag(el), `class="alert st"`) {
		t.Errorf("Path to Green overdue banner is not an alert: %s", openTag(el))
	}

	list := getBody(t, client, ts.URL+"/goals")
	if chip := pageElement(t, list, "span", "checkin-overdue"); !strings.Contains(chip, `class="badge st"`) || !strings.Contains(chip, "Check-in overdue · 16 days") {
		t.Errorf("Check-in overdue row chip is not a Check-in overdue badge saying how long: %s", chip)
	}
	if chip := pageElement(t, list, "span", "path-overdue"); !strings.Contains(chip, `class="badge st"`) || !strings.Contains(chip, "Path to Green overdue") {
		t.Errorf("overdue row chip is not a Check-in overdue-colored badge: %s", chip)
	}

	adaPage := getBody(t, signInClient(t, ts.URL, "ada@example.com"), goalPageURL(ts.URL, silent)+"?open=top-level")
	if control := pageElement(t, adaPage, "section", "mark-top-level"); !strings.Contains(control, `<button type="submit" class="btn" `) {
		t.Errorf("Top-level control is not a plain button: %s", control)
	}
}

// followUpFix returns a row's one Fix: its label, its address (a form's action,
// for a Fix that posts, and "" for a disabled one), and whether it is the
// primary button, failing the test unless the fix cell holds exactly one link,
// one form or one button.
func followUpFix(t *testing.T, row string) (label, href string, primary bool) {
	t.Helper()
	cell := pageElement(t, row, "td", "follow-up-fix")
	links, forms, buttons := strings.Count(cell, "<a "), strings.Count(cell, "<form "), strings.Count(cell, "<button ")
	if links+buttons != 1 || forms > buttons {
		t.Fatalf("fix cell has %d links, %d forms and %d buttons, want one Fix: %s", links, forms, buttons, cell)
	}
	if buttons == 1 {
		_, button, _ := strings.Cut(cell, "<button ")
		tag := openTag("<button " + button)
		label, _, _ = strings.Cut(button[len(tag)-len("<button ")+1:], "</button>")
		if _, form, ok := strings.Cut(cell, "<form "); ok {
			href = html.UnescapeString(attr(openTag("<form "+form)+">", "action"))
		}
		return label, href, strings.Contains(tag, "primary")
	}
	_, link, _ := strings.Cut(cell, "<a ")
	link = "<a " + link
	tag := openTag(link)
	_, rest, _ := strings.Cut(tag, `href="`)
	href, _, _ = strings.Cut(rest, `"`)
	label, _, _ = strings.Cut(link[len(tag)+1:], "</a>")
	return label, html.UnescapeString(href), strings.Contains(tag, "primary")
}

// A Check-in overdue Goal's Fix is Check in, the primary button, for its Owner
// and its Delegates, who can check in; anyone else, an Admin included, gets
// Nudge.
func TestFollowUpFixOnACheckinOverdueGoalIsCheckInForWhoCanCheckIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("kim@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	silent := h.ActiveChildOf(sam, top, "Silent work", "It matters.")
	h.AddDelegate(sam, dee, silent.ID)
	h.Clock.Advance(10 * day)
	h.Checkin(sam, top.ID, domain.HealthGreen, "On track.", "", time.Time{})

	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"sam@example.com", "Check in", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/checkin", true},
		{"dee@example.com", "Check in", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/checkin", true},
		{"kim@example.com", "Nudge", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge", false},
		{"ada@example.com", "Nudge", "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge", false},
	} {
		page := getBody(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all")
		label, href, primary := followUpFix(t, followUpRowOf(t, page, silent))
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
	}
}

// An Ownerless Goal's Fix for an Admin is Reassign, opening the Goal page's
// reassign form; a Delegate on it, who can still check in, gets Check in while
// it is Check-in overdue.
func TestFollowUpFixOnAnOwnerlessGoalIsReassignForAnAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(ada, "Grow revenue", "It pays for everything."))
	orphaned := h.ActiveChildOf(sam, top, "Migrate displays", "Displays fail often.")
	h.AddDelegate(sam, dee, orphaned.ID)
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)

	goal := "/goals/" + strconv.FormatInt(orphaned.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
	}{
		{"ada@example.com", "Reassign", goal + "?open=reassign"},
		{"dee@example.com", "Check in", goal + "/checkin"},
	} {
		client := signInClient(t, ts.URL, c.viewer)
		page := getBody(t, client, ts.URL+"/follow-ups?org=all")
		row := followUpRowOf(t, page, orphaned)
		followUpChip(t, row, "ownerless")
		followUpChip(t, row, "checkin-overdue")
		label, href, _ := followUpFix(t, row)
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Reassign" && !strings.Contains(getBody(t, client, ts.URL+href), `data-testid="reassign-goal"`) {
			t.Errorf("Reassign doesn't open the reassign form")
		}
	}
}

// An Unlinked Goal's Fix for its Owner is Link to a parent, opening the Goal
// page's parent-link form; anyone else, a Delegate included, who can't link
// it, gets Suggest a parent, opening the Suggest a parent form.
func TestFollowUpFixOnAnUnlinkedGoalIsLinkToAParentForItsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("kim@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	h.AddDelegate(sam, dee, loner.ID)

	goal := "/goals/" + strconv.FormatInt(loner.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
	}{
		{"sam@example.com", "Link to a parent", goal + "?open=parent-link"},
		{"dee@example.com", "Suggest a parent", goal + "/suggest-parent"},
		{"kim@example.com", "Suggest a parent", goal + "/suggest-parent"},
	} {
		client := signInClient(t, ts.URL, c.viewer)
		page := getBody(t, client, ts.URL+"/follow-ups?org=all")
		label, href, _ := followUpFix(t, followUpRowOf(t, page, loner))
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Link to a parent" && !strings.Contains(getBody(t, client, ts.URL+href), `data-testid="request-link"`) {
			t.Errorf("Link to a parent doesn't open the parent-link form")
		}
		if label == "Suggest a parent" && !strings.Contains(getBody(t, client, ts.URL+href), `data-testid="suggest-parent"`) {
			t.Errorf("Suggest a parent doesn't open the Suggest a parent form")
		}
	}
}

// A Goal due after its parent gets Check in, its dates open, for its Owner,
// even when that parent is On Hold too; a Goal under halted parents only gets
// Open parent, the halted parent first by title. Every row has exactly one Fix.
func TestFollowUpFixOnAStructuralSignal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	zeta := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Zeta outcome", "It mattered."))
	alpha := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Alpha outcome", "It mattered."))
	under := h.ActiveChildOf(sam, zeta, "Under both", "Feeds both.")
	h.RequestLink(sam, under, alpha, "")
	for _, parent := range []domain.Goal{launch, zeta, alpha} {
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID:          parent.ID,
			AuthorID:        sam.ID,
			Status:          "Stopping.",
			Lifecycle:       domain.LifecycleOnHold,
			LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin %q On Hold: %v", parent.Title, err)
		}
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	for _, c := range []struct {
		goal        domain.Goal
		kinds       []string
		label, href string
	}{
		{late, []string{"schedule-conflicts", "halted-parents"}, "Check in", "/goals/" + strconv.FormatInt(late.ID, 10) + "/checkin?open=dates"},
		{under, []string{"halted-parents"}, "Open parent", "/goals/" + strconv.FormatInt(alpha.ID, 10)},
	} {
		row := followUpRowOf(t, page, c.goal)
		for _, kind := range c.kinds {
			followUpChip(t, row, kind)
		}
		if label, href, _ := followUpFix(t, row); label != c.label || href != c.href {
			t.Errorf("%q's Fix is %q → %s, want %q → %s", c.goal.Title, label, href, c.label, c.href)
		}
	}
	for _, row := range followUpRows(page) {
		followUpFix(t, row) // every row has exactly one Fix
	}
}

// A Schedule conflict's Fix is Check in, its dates open, for the Owner or a
// Delegate of either Goal, on their own Goal, the child first; anyone else
// gets Nudge on the child, the row's Goal, and once it's nudged, a disabled
// "Nudged today by" whoever did (CONTEXT.md: Schedule conflict, Nudge).
func TestFollowUpFixOnAScheduleConflict(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	dee := h.SignIn("dee@example.com")
	lee := h.SignIn("lee@example.com")
	h.SignInNamed("pat@example.com", "Pat Okafor")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(kim, "Late piece", june.AddDate(0, 1, 0))
	link := h.RequestLink(kim, late, launch, "")
	if _, err := h.Service.AcceptLink(t.Context(), link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	h.AddDelegate(sam, dee, launch.ID)
	h.AddDelegate(kim, lee, late.ID)

	lateCheckin := "/goals/" + strconv.FormatInt(late.ID, 10) + "/checkin?open=dates"
	launchCheckin := "/goals/" + strconv.FormatInt(launch.ID, 10) + "/checkin?open=dates"
	nudge := "/goals/" + strconv.FormatInt(late.ID, 10) + "/nudge"
	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"kim@example.com", "Check in", lateCheckin, true},
		{"lee@example.com", "Check in", lateCheckin, true},
		{"sam@example.com", "Check in", launchCheckin, true},
		{"dee@example.com", "Check in", launchCheckin, true},
		{"pat@example.com", "Nudge", nudge, false},
	} {
		page := getBody(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all")
		row := followUpRowOf(t, page, late)
		followUpChip(t, row, "schedule-conflicts")
		label, href, primary := followUpFix(t, row)
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
		if c.label == "Nudge" {
			nudgeForm(t, row)
		}
	}

	checkin := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+lateCheckin)
	if !strings.Contains(openTag(pageElement(t, checkin, "details", "checkin-dates-section")), " open") {
		t.Errorf("kim's Check in doesn't open the dates section")
	}

	pat := signInClient(t, ts.URL, "pat@example.com")
	resp := postForm(t, noRedirects(pat), ts.URL+nudge, url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/follow-ups" {
		t.Fatalf("nudging got %d → %q, want 303 → /follow-ups", resp.StatusCode, resp.Header.Get("Location"))
	}
	cell := pageElement(t, followUpRowOf(t, getBody(t, pat, ts.URL+"/follow-ups?org=all"), late), "td", "follow-up-fix")
	if !strings.Contains(cell, "disabled") || !strings.Contains(cell, "Nudged today by Pat Okafor") || strings.Contains(cell, "<form") {
		t.Errorf("pat sees Fix %s, want a disabled Nudged today by Pat Okafor", cell)
	}
}

// A Proposed child in a Schedule conflict can't be checked in on or nudged,
// so its Owner and anyone else get Open Goal, its page, where its Owner
// finishes defining it; the parent's Owner still gets Check in on the parent.
func TestFollowUpFixOnAScheduleConflictWithAProposedChild(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.SignIn("pat@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	proposed := h.CreateGoal(kim, "Late plan", "It matters.")
	if _, err := h.Service.MarkGoalDated(t.Context(), proposed.ID, june.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	link := h.RequestLink(kim, proposed, launch, "")
	if _, err := h.Service.AcceptLink(t.Context(), link.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	page := "/goals/" + strconv.FormatInt(proposed.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"kim@example.com", "Open Goal", page, false},
		{"pat@example.com", "Open Goal", page, false},
		{"sam@example.com", "Check in", "/goals/" + strconv.FormatInt(launch.ID, 10) + "/checkin?open=dates", true},
	} {
		row := followUpRowOf(t, getBody(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all"), proposed)
		followUpChip(t, row, "schedule-conflicts")
		label, href, primary := followUpFix(t, row)
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
		if strings.Contains(pageElement(t, row, "td", "follow-up-fix"), "<form") {
			t.Errorf("%s is offered a Nudge form on a Proposed Goal", c.viewer)
		}
	}
}

// Scoped to Mine, by ?org=mine, the Follow-ups page keeps only the Goals the viewer Owns or is a Delegate
// on, and its count line names Mine and counts those alone, as do its cards,
// while the top bar still counts every flagged Goal. The Scope select shows
// the current scope; with no Managers it offers only Everyone and Mine.
func TestFollowUpsPageScopedToMine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	own := h.ActiveGoal(sam, "Own work", "It matters.")
	delegated := h.ActiveGoal(kim, "Delegated work", "It matters.")
	h.AddDelegate(kim, sam, delegated.ID)
	other := h.ActiveGoal(kim, "Other work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=mine")

	followUpRowOf(t, page, own)
	followUpRowOf(t, page, delegated)
	if hasFollowUpRow(page, other) {
		t.Errorf("the page lists %q, which sam neither Owns nor is a Delegate on", other.Title)
	}
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "<strong>2</strong> Goals to review in Mine, most urgent first.") {
		t.Errorf("the count line doesn't count sam's 2 Goals in Mine: %s", head)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-owner"); !strings.Contains(card, `<span class="num">2</span>`) || !strings.Contains(card, "Check-in overdue · 2") {
		t.Errorf("the owner card doesn't count sam's 2 Goals: %s", card)
	}
	if followUps := pageElement(t, page, "a", "nav-follow-ups"); !strings.Contains(followUps, `<span class="count">2</span>`) {
		t.Errorf("the top bar's Follow-ups count isn't sam's default, Mine's 2: %s", followUps)
	}
	choices, value, label := scopeChoices(scopeSelect(t, page, "follow-ups-scope"))
	if value != "mine" || label != "Mine" {
		t.Errorf("the Scope select shows %q %q chosen, want Mine", value, label)
	}
	if want := []string{"Everyone", "Mine"}; !slices.Equal(choices, want) {
		t.Errorf("without Managers the Scope select offers %q, want %q", choices, want)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?org=all")

	followUpRowOf(t, page, other)
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "<strong>3</strong> Goals to review, most urgent first.") {
		t.Errorf("for Everyone, the count line doesn't count all 3 Goals: %s", head)
	}
	if _, value, label := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != "all" || label != "Everyone" {
		t.Errorf("for Everyone, the Scope select shows %q %q chosen, want Everyone", value, label)
	}
	if followUps := pageElement(t, page, "a", "nav-follow-ups"); !strings.Contains(followUps, `<span class="count">2</span>`) {
		t.Errorf("for Everyone, the top bar's Follow-ups count follows the page's scope: %s", followUps)
	}
}

// followUpsValueSelects returns each of the Follow-ups page's Dimension value selects,
// in order.
func followUpsValueSelects(page string) []string {
	return regexp.MustCompile(`(?s)<select data-testid="follow-ups-value".*?</select>`).FindAllString(page, -1)
}

// followUpsValueSelect returns the Follow-ups page's value select for the Dimension
// named dim, failing the test unless there is exactly one.
func followUpsValueSelect(t *testing.T, page, dim string) string {
	t.Helper()
	var found []string
	for _, sel := range followUpsValueSelects(page) {
		if attr(openTag(sel), "data-dimension") == dim {
			found = append(found, sel)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Follow-ups page has %d value selects for %s, want 1:\n%s", len(found), dim, page)
	}
	return found[0]
}

// The Follow-ups page has a value select per offered Dimension, in the Dimensions'
// order, by name, each named value and labelled by its Dimension's name, offering
// "<Dimension>: any", which scopes nothing, then the Dimension's values, a
// Retired one saying so.
func TestFollowUpsPageHasAValueSelectPerDimension(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	h.CreateDimension(boss, "Quarter", "Q3")
	if err := h.Service.RetireDimensionValue(t.Context(), boss.ID, team.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	sels := followUpsValueSelects(page)
	var dims []string
	for _, sel := range sels {
		dims = append(dims, attr(openTag(sel), "data-dimension"))
	}
	if !slices.Equal(dims, []string{"Quarter", "Team"}) {
		t.Fatalf("the value selects are for %q, want Quarter then Team", dims)
	}
	for dim, want := range map[string][]string{"Team": {"Team: any", "Platform", "Growth (retired)"}, "Quarter": {"Quarter: any", "Q3"}} {
		sel := followUpsValueSelect(t, page, dim)
		if name := attr(openTag(sel), "name"); name != "value" {
			t.Errorf("the %s select is named %q, want value", dim, name)
		}
		if label := attr(openTag(sel), "aria-label"); label != dim {
			t.Errorf("the %s select is labelled %q", dim, label)
		}
		choices, value, _ := scopeChoices(sel)
		if !slices.Equal(choices, want) {
			t.Errorf("the %s select offers %q, want %q", dim, choices, want)
		}
		if first := regexp.MustCompile(`<option[^>]*>`).FindString(sel); attr(first, "value") != "" || value != "" {
			t.Errorf("unscoped, the %s select's any option doesn't post nothing, chosen: %s", dim, sel)
		}
	}
}

// Scoped to a Dimension value (?value=), the Follow-ups page keeps only the Goals
// that have it, counting those alone, and its Dimension's select offers
// "Team: any" then the Dimension's values, the chosen one selected. A value of
// a Retired Dimension, which has no select, or no value at all, scopes
// nothing.
func TestFollowUpsPageScopedToADimensionValue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	region := h.CreateDimension(boss, "Region", "EMEA")
	platform, growth, emea := team.Values[0], team.Values[1], region.Values[0]
	onPlatform := h.ActiveGoal(sam, "Platform work", "It matters.")
	h.AssignGoalValue(onPlatform, platform)
	onGrowth := h.ActiveGoal(sam, "Growth work", "It matters.")
	h.AssignGoalValue(onGrowth, growth)
	h.AssignGoalValue(onGrowth, emea)
	untagged := h.ActiveGoal(sam, "Untagged work", "It matters.")
	if err := h.Service.RetireDimension(t.Context(), boss.ID, region.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	id := func(v domain.DimensionValue) string { return strconv.FormatInt(v.ID, 10) }

	page := getBody(t, client, ts.URL+"/follow-ups?org=all&value="+id(platform))

	followUpRowOf(t, page, onPlatform)
	for _, g := range []domain.Goal{onGrowth, untagged} {
		if hasFollowUpRow(page, g) {
			t.Errorf("scoped to Platform, the page lists %q", g.Title)
		}
	}
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.Contains(head, "<strong>1</strong> Goal to review,") {
		t.Errorf("scoped to Platform, the header doesn't count its 1 Goal: %s", head)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-owner"); !strings.Contains(card, `<span class="num">1</span>`) {
		t.Errorf("scoped to Platform, the owner card doesn't count its 1 Goal: %s", card)
	}
	if followUps := pageElement(t, page, "a", "nav-follow-ups"); !strings.Contains(followUps, `<span class="count">3</span>`) {
		t.Errorf("scoped to Platform, the top bar's Follow-ups count follows scope: %s", followUps)
	}
	if sels := followUpsValueSelects(page); len(sels) != 1 {
		t.Errorf("with Team the one offered Dimension, the page has %d value selects, want 1", len(sels))
	}
	if choices, value, label := scopeChoices(followUpsValueSelect(t, page, "Team")); !slices.Equal(choices, []string{"Team: any", "Platform", "Growth"}) || value != id(platform) || label != "Platform" {
		t.Errorf("scoped to Platform, the Team select offers %q with %q %q chosen, want Team: any, Platform, Growth with Platform", choices, value, label)
	}
	values := formValues(between(t, page, `data-testid="follow-ups-filters"`, "</form>"))
	if values.Set("org", "mine"); values.Encode() != "org=mine&value="+id(platform) {
		t.Errorf("choosing Mine doesn't keep the value scope: %s", values.Encode())
	}

	for _, ignored := range []string{id(emea), "bogus"} {
		page = getBody(t, client, ts.URL+"/follow-ups?org=all&value="+ignored)
		for _, g := range []domain.Goal{onPlatform, onGrowth, untagged} {
			followUpRowOf(t, page, g)
		}
	}
}

// Scoped to values of two Dimensions, the Follow-ups page keeps only the flagged
// Goals carrying both, as the Goal list does; scoped to one, those carrying
// it. Of two values in one Dimension it keeps the first, which its select
// shows chosen, and a value naming no offered one scopes nothing.
func TestFollowUpsPageScopedToValuesOfSeveralDimensions(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth")
	quarter := h.CreateDimension(boss, "Quarter", "Q3")
	platform, growth, q3 := team.Values[0], team.Values[1], quarter.Values[0]
	both := h.ActiveGoal(sam, "Platform in Q3", "It matters.")
	h.AssignGoalValue(both, platform)
	h.AssignGoalValue(both, q3)
	platformOnly := h.ActiveGoal(sam, "Platform any time", "It matters.")
	h.AssignGoalValue(platformOnly, platform)
	growthInQ3 := h.ActiveGoal(sam, "Growth in Q3", "It matters.")
	h.AssignGoalValue(growthInQ3, growth)
	h.AssignGoalValue(growthInQ3, q3)
	untagged := h.ActiveGoal(sam, "Untagged work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	id := func(v domain.DimensionValue) string { return strconv.FormatInt(v.ID, 10) }
	all := []domain.Goal{both, platformOnly, growthInQ3, untagged}

	for query, want := range map[string][]domain.Goal{
		"value=" + id(platform) + "&value=" + id(q3):     {both},
		"value=" + id(q3) + "&value=" + id(platform):     {both},
		"value=" + id(platform):                          {both, platformOnly},
		"value=&value=" + id(q3):                         {both, growthInQ3},
		"value=" + id(platform) + "&value=" + id(growth): {both, platformOnly},
		"value=bogus&value=" + id(growth) + "&value=999": {growthInQ3},
	} {
		page := getBody(t, client, ts.URL+"/follow-ups?"+query)
		for _, g := range all {
			if listed := hasFollowUpRow(page, g); listed != slices.Contains(want, g) {
				t.Errorf("at ?%s, %q listed is %v", query, g.Title, listed)
			}
		}
	}

	page := getBody(t, client, ts.URL+"/follow-ups?value="+id(platform)+"&value="+id(growth))

	if _, value, label := scopeChoices(followUpsValueSelect(t, page, "Team")); value != id(platform) || label != "Platform" {
		t.Errorf("of Platform then Growth, the Team select shows %q %q chosen, want Platform", value, label)
	}
	if _, value, label := scopeChoices(followUpsValueSelect(t, page, "Quarter")); value != "" || label != "Quarter: any" {
		t.Errorf("with no Quarter chosen, the Quarter select shows %q %q chosen, want Quarter: any", value, label)
	}
}

// Scoped to values of two Dimensions, the group cards and Show all keep every
// value, and with nothing flagged there the empty line names each value with
// its Dimension, in the Dimensions' order.
func TestFollowUpsPageKeepsEveryValueInItsAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	q3 := h.CreateDimension(boss, "Quarter", "Q3").Values[0]
	g := h.ActiveGoal(sam, "Platform in Q3", "It matters.")
	h.AssignGoalValue(g, platform)
	h.AssignGoalValue(g, q3)
	h.ActiveGoal(sam, "Untagged work", "It matters.")
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")
	scope := "value=" + strconv.FormatInt(q3.ID, 10) + "&value=" + strconv.FormatInt(platform.ID, 10)

	page := getBody(t, client, ts.URL+"/follow-ups?value="+strconv.FormatInt(platform.ID, 10)+"&value="+strconv.FormatInt(q3.ID, 10))

	href := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-group-owner")), "href"))
	if href != "/follow-ups?group=owner&"+scope {
		t.Fatalf("the owner card's link %q doesn't keep both values", href)
	}
	page = getBody(t, client, ts.URL+href)
	if all := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-show-all")), "href")); all != "/follow-ups?"+scope {
		t.Errorf("Show all %q doesn't keep both values", all)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?group=admin&"+scope)

	if empty := html.UnescapeString(pageElement(t, page, "p", "follow-ups-empty")); !strings.Contains(empty, ">Nothing needs an Admin in Quarter Q3, Team Platform.") {
		t.Errorf("the empty line doesn't name both values with their Dimensions: %s", empty)
	}
}

// With no Dimension offered, the Follow-ups page has no value select, and a value
// in its address scopes nothing.
func TestFollowUpsPageWithNoDimensionsHasNoValueSelect(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform")
	g := h.ActiveGoal(sam, "Platform work", "It matters.")
	h.AssignGoalValue(g, team.Values[0])
	other := h.ActiveGoal(sam, "Other work", "It matters.")
	if err := h.Service.RetireDimension(t.Context(), boss.ID, team.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?value="+strconv.FormatInt(team.Values[0].ID, 10))

	if strings.Contains(page, `data-testid="follow-ups-value"`) {
		t.Errorf("with no Dimension offered, the page has a value select")
	}
	followUpRowOf(t, page, g)
	followUpRowOf(t, page, other)
}

// Scoped to Mine and a value, the group cards and Show all keep the scope, so
// clicking a card narrows the scoped page to the group: the table lists only
// the Goals the scope and the group both keep, while the cards count every
// group within the scope.
func TestFollowUpsPageGroupsWithinScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	ownCheckinOverdue := h.MarkTopLevel(boss, h.ActiveGoal(sam, "Own overdue", "It matters.")) // Check-in overdue only
	h.AssignGoalValue(ownCheckinOverdue, platform)
	ownUnlinked := h.ActiveGoal(sam, "Own unlinked", "It matters.") // Unlinked only
	h.AssignGoalValue(ownUnlinked, platform)
	ownUntagged := h.ActiveGoal(sam, "Own untagged", "It matters.")
	kims := h.ActiveGoal(kim, "Kim's platform work", "It matters.")
	h.AssignGoalValue(kims, platform)
	h.Clock.Advance(10 * day)
	h.Checkin(sam, ownUnlinked.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")
	scope := "org=mine&value=" + strconv.FormatInt(platform.ID, 10)

	page := getBody(t, client, ts.URL+"/follow-ups?"+scope)

	href := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-group-owner")), "href"))
	if href != "/follow-ups?group=owner&"+scope {
		t.Fatalf("the owner card's link %q doesn't keep the scope", href)
	}

	page = getBody(t, client, ts.URL+href)

	followUpRowOf(t, page, ownCheckinOverdue)
	for _, g := range []domain.Goal{ownUnlinked, ownUntagged, kims} {
		if hasFollowUpRow(page, g) {
			t.Errorf("scoped and filtered to owner, the page lists %q", g.Title)
		}
	}
	for key, want := range map[string]string{"owner": "1", "plan": "1"} {
		if card := pageElement(t, page, "a", "follow-ups-group-"+key); !strings.Contains(card, `<span class="num">`+want+`</span>`) {
			t.Errorf("scoped and filtered to owner, the %s card doesn't count %s: %s", key, want, card)
		}
	}
	if all := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-show-all")), "href")); all != "/follow-ups?"+scope {
		t.Errorf("Show all %q doesn't keep the scope", all)
	}
	values := formValues(between(t, page, `data-testid="follow-ups-filters"`, "</form>"))
	if values.Set("org", "all"); values.Get("group") != "owner" || values.Get("value") != strconv.FormatInt(platform.ID, 10) {
		t.Errorf("choosing Everyone doesn't keep the group and value: %v", values)
	}
}

// The scope form applies itself under htmx as the Goal list's filter bar does:
// a change GETs the page and swaps in only #follow-ups-body, which holds the header
// count, the cards and the table but not the form, and pushes the address,
// while the page opts out of htmx's history snapshot so Back loads the earlier
// address afresh. The form carries the page's group and its Scope, Mine here
// as ?org=mine, so a value chosen while filtered keeps both, and without
// JavaScript it is a plain GET with an Apply button in <noscript>.
func TestFollowUpsScopeFormAppliesItself(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	platform := h.CreateDimension(boss, "Team", "Platform").Values[0]
	own := h.ActiveGoal(sam, "Own platform work", "It matters.")
	h.AssignGoalValue(own, platform)
	h.ActiveGoal(sam, "Own untagged", "It matters.")
	kims := h.ActiveGoal(kim, "Kim's platform work", "It matters.")
	h.AssignGoalValue(kims, platform)
	h.Clock.Advance(10 * day)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?group=owner&org=mine")

	form := tagAround(t, page, `data-testid="follow-ups-filters"`)
	for name, want := range map[string]string{
		"method":      "get",
		"action":      "/follow-ups",
		"hx-get":      "/follow-ups",
		"hx-trigger":  "submit, change",
		"hx-target":   "#follow-ups-body",
		"hx-select":   "#follow-ups-body",
		"hx-swap":     "outerHTML",
		"hx-push-url": "true",
		"hx-sync":     "this:replace",
		"hx-history":  "false",
	} {
		if got := html.UnescapeString(attr(form, name)); got != want {
			t.Errorf("scope form %s = %q, want %q", name, got, want)
		}
	}
	filters := between(t, page, `data-testid="follow-ups-filters"`, "</form>")
	if noscript := between(t, filters, "<noscript>", "</noscript>"); !strings.Contains(noscript, `<button type="submit" class="btn">Apply</button>`) || strings.Count(filters, "Apply") != 1 {
		t.Errorf("the scope form's Apply isn't only in <noscript>:\n%s", filters)
	}
	body := between(t, page, `<div id="follow-ups-body"`, "</main>")
	for _, in := range []string{`data-testid="follow-ups-attention"`, `data-testid="follow-ups-groups"`, `data-testid="follow-ups-table"`} {
		if !strings.Contains(body, in) {
			t.Errorf("#follow-ups-body lacks %s", in)
		}
	}
	if strings.Contains(body, `data-testid="follow-ups-filters"`) || strings.Contains(body, `aria-label="Scope"`) {
		t.Errorf("#follow-ups-body holds the scope form or its Scope, which a swap would reset")
	}

	values := formValues(filters)
	values.Set("value", strconv.FormatInt(platform.ID, 10))
	address := attr(form, "hx-get") + "?" + values.Encode()
	if want := "/follow-ups?group=owner&org=mine&value=" + strconv.FormatInt(platform.ID, 10); address != want {
		t.Fatalf("choosing Platform GETs %s, want %s", address, want)
	}

	page = getBody(t, client, ts.URL+address)

	followUpRowOf(t, page, own)
	if rows := followUpRows(page); len(rows) != 1 {
		t.Errorf("scoped to Mine and Platform and filtered to owner, the table has %d rows, want 1", len(rows))
	}
}

// A Health chip on the Follow-ups page shows one marker, its dot: the shape
// healthBadge also emits is for the print page, so the page hides it on screen.
func TestFollowUpsPageHidesHealthShapesOnScreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if row := followUpRowOf(t, page, stalled); !strings.Contains(row, `class="shape"`) {
		t.Fatalf("the row's Health chip has no shape, so this test checks nothing:\n%s", row)
	}
	if rule := cssRule(t, page, ".shape"); !strings.Contains(rule, "display:none") {
		t.Errorf("the Follow-ups page shows .shape{%s} on screen beside each Health chip's dot", rule)
	}
}

// An Owner's name stays on one line at desktop width rather than wrapping
// onto two in the table's narrow Owner column.
func TestFollowUpsPageKeepsOwnersOnOneLine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if owner := pageElement(t, followUpRowOf(t, page, silent), "td", "follow-up-owner"); !strings.Contains(openTag(owner), `class="rk-owner"`) {
		t.Fatalf("the Owner cell has no rk-owner class to style: %s", openTag(owner))
	}
	if rule := cssRule(t, page, ".rk-table .rk-owner"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("the Owner cell .rk-table .rk-owner{%s} wraps", rule)
	}
}

// A Not Green row's Health badge and its Run off Green stay on one line
// however the table squeezes the column, so "· 10 wks" never drops its "wks"
// under the badge.
func TestFollowUpsPageKeepsHealthAndItsRunOnOneLine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	yellow := h.ActiveGoal(sam, "Launch in EU", "Expand the market.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 1, 0))
	h.Clock.Advance(3 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if cell := between(t, followUpRowOf(t, page, yellow), `<td class="rk-health">`, "</td>"); !strings.Contains(cell, `data-testid="follow-up-run-off-green"`) {
		t.Fatalf("the rk-health cell doesn't hold the Run off Green: %s", cell)
	}
	if rule := cssRule(t, page, ".rk-table .rk-health"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("the Health cell .rk-table .rk-health{%s} wraps", rule)
	}
}

// Below 600px each Follow-ups row stacks as a card rather than hiding its Fix off
// to the side of a sideways scroller: the Goal and its chips first, then its
// Health and Owner on one line, then its Fix. The cells run Health, Goal,
// Owner, Fix in the HTML, so the Goal cell is ordered first.
func TestFollowUpsRowsStackAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, silent)
	for _, class := range []string{"rk-health", "rk-goal", "rk-owner", "rk-fix"} {
		if !strings.Contains(row, `class="`+class+`"`) {
			t.Errorf("the row has no %s cell to style:\n%s", class, row)
		}
	}
	phone := between(t, page, "@media (max-width:600px){", "}}") + "}"
	for selector, want := range map[string]string{
		".rk-table thead":    "display:none",
		".rk-table tr":       "display:grid",
		".rk-table td":       "display:block",
		".rk-table .rk-goal": "order:-1",
		".rk-table .rk-fix":  "grid-column:1/-1",
	} {
		if rule := cssRule(t, phone, selector); !strings.Contains(rule, want) {
			t.Errorf("at 600px %s{%s} lacks %s", selector, rule, want)
		}
	}
	if rule := cssRule(t, phone, ".rk-table .rk-goal"); !strings.Contains(rule, "grid-column:1/-1") {
		t.Errorf("at 600px the Goal cell .rk-table .rk-goal{%s} shares its line with Health rather than heading the card", rule)
	}
}

// The Follow-ups filters wrap, as the Goal list's filter bar does, and never grow
// past their column: a Scope select listing Other orgs by their long labels
// shrinks within the form rather than pushing the Team select off a phone's
// screen (#268).
func TestFollowUpsFiltersWrapAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "vera@example.com"), ts.URL+"/follow-ups")
	choices, _, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope"))
	if !slices.ContainsFunc(choices, func(c string) bool { return strings.HasPrefix(c, "Other orgs: ") }) {
		t.Fatalf("the Scope select lists no Other orgs: %q", choices)
	}
	if form := tagAround(t, page, `data-testid="follow-ups-filters"`); !hasClass(form, "rk-filters") {
		t.Fatalf("the filters form isn't .rk-filters: %s", form)
	}
	rule := cssRule(t, page, ".rk-filters")
	for _, want := range []string{"flex-wrap:wrap", "min-width:0"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".rk-filters{%s} lacks %s, so its selects run off a phone's screen", rule, want)
		}
	}
}

// orgScopedFollowUpTitles names the Goals the Follow-ups table lists, in order, by matching each
// row against the Goals it could be.
func orgScopedFollowUpTitles(page string, goals ...domain.Goal) []string {
	var titles []string
	for _, row := range followUpRows(page) {
		for _, g := range goals {
			if isFollowUpRowOf(row, g) {
				titles = append(titles, g.Title)
			}
		}
	}
	slices.Sort(titles)
	return titles
}

// With no scope in its address the Follow-ups page opens on the viewer's default:
// My org for someone with anyone under them, Mine for everyone else, each
// shown chosen and named in the count line. An explicit Scope is never
// overridden: ?org=all is Everyone, ?org=mine Mine and ?org=<id> that
// person's Org, while a malformed ?org=, or one naming no one, gives the
// default, as does the old ?mine=1, ignored like any unknown parameter.
func TestFollowUpsPageOpensOnTheViewersDefaultScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	maxID := strconv.FormatInt(org.Max.ID, 10)

	for _, viewer := range []struct {
		email, defaultValue, defaultLine string
		defaultTitles                    []string
		mine                             string
	}{
		{"mia@example.com", strconv.FormatInt(org.Mia.ID, 10), "<strong>2</strong> Goals to review in My org, most urgent first.", []string{"Mia's goal", "Pia's goal"}, "Mia's goal"},
		{"pia@example.com", "mine", "<strong>1</strong> Goal to review in Mine, most urgent first.", []string{"Pia's goal"}, "Pia's goal"},
	} {
		client := signInClient(t, ts.URL, viewer.email)
		page := getBody(t, client, ts.URL+"/follow-ups")
		if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, viewer.defaultLine) {
			t.Errorf("%s's default: the count line = %s, want %q", viewer.email, head, viewer.defaultLine)
		}
		for _, c := range []struct {
			query, value string
			titles       []string
		}{
			{"", viewer.defaultValue, viewer.defaultTitles},
			{"?org=all", "all", []string{"Max's goal", "Mia's goal", "Oz's goal", "Pax's goal", "Pia's goal", "Vera's goal"}},
			{"?org=mine", "mine", []string{viewer.mine}},
			{"?org=" + maxID, maxID, []string{"Max's goal", "Pax's goal"}},
			{"?org=abc", viewer.defaultValue, viewer.defaultTitles},
			{"?org=999999", viewer.defaultValue, viewer.defaultTitles},
			{"?mine=1", viewer.defaultValue, viewer.defaultTitles},
		} {
			page := getBody(t, client, ts.URL+"/follow-ups"+c.query)
			if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, c.titles) {
				t.Errorf("%s, /follow-ups%s: rows = %q, want %q", viewer.email, c.query, got, c.titles)
			}
			if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != c.value {
				t.Errorf("%s, /follow-ups%s: the Scope select shows %q chosen, want %q", viewer.email, c.query, value, c.value)
			}
		}
	}
}

// Everyone is an explicit ?org=all on the Follow-ups page: the Scope select
// submits it, Show everything links to it, and a page showing Everyone keeps
// it in its group cards' links and Show all, since a bare /follow-ups is the
// viewer's default.
func TestFollowUpsPageNamesEveryoneAsOrgAll(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "mia@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")
	if sel := scopeSelect(t, page, "follow-ups-scope"); !strings.Contains(sel, `<option value="all" selected>Everyone</option>`) {
		t.Errorf("the Scope select's Everyone doesn't submit org=all: %s", sel)
	}
	if href := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-group-owner")), "href")); href != "/follow-ups?group=owner&org=all" {
		t.Errorf("on Everyone, the owner card links to %q, want it to keep org=all", href)
	}
	page = getBody(t, client, ts.URL+"/follow-ups?group=owner&org=all")
	if href := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-show-all")), "href")); href != "/follow-ups?org=all" {
		t.Errorf("on Everyone, Show all links to %q, want it to keep org=all", href)
	}

	lees := signInClient(t, ts.URL, lee.Email)
	empty := pageElement(t, getBody(t, lees, ts.URL+"/follow-ups"), "p", "follow-ups-empty")
	if !strings.Contains(empty, "No Goals need attention in Mine.") || !strings.Contains(empty, `<a href="/follow-ups?org=all">Show everything</a>`) {
		t.Errorf("an empty default doesn't say so with Show everything to Everyone: %s", empty)
	}
}

// An Admin's job in Needs an Admin is company-wide: that group lists every
// Ownerless and halted-parent Goal whatever the page's scope, which stays the
// one its address names, the viewer's default with none, for an Admin with no
// one under them too. Its card counts the company's, keeping the page's scope
// and values in its link, so its count and its table agree, and Show all and
// the other cards from there come back to the page's scope.
func TestFollowUpsNeedsAnAdminIsCompanyWide(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Core", "Edge")
	core, edge := team.Values[0], team.Values[1]
	h.AssignGoalValue(org.PaxGoal, core)
	h.AssignGoalValue(org.MiaGoal, core)
	h.AssignGoalValue(org.PiaGoal, edge)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, org.Pax.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	admin := signInClient(t, ts.URL, "boss@example.com")
	page := getBody(t, admin, ts.URL+"/follow-ups?group=admin")
	if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, []string{"Pax's goal"}) {
		t.Errorf("Needs an Admin for an Admin with no one under them: rows = %q, want Pax's Ownerless Goal", got)
	}
	if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != "mine" {
		t.Errorf("Needs an Admin with no scope shows %q chosen, want the Admin's default, Mine", value)
	}
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "><strong>1</strong> Goal waiting on an Admin, most urgent first.") {
		t.Errorf("Needs an Admin's header doesn't say 1 Goal needs an Admin across the company: %s", head)
	}

	mia := signInClient(t, ts.URL, "mia@example.com")
	coreID := strconv.FormatInt(core.ID, 10)
	for _, c := range []struct{ query, count, href string }{
		{"", "1", "/follow-ups?group=admin"},
		{"?org=mine", "1", "/follow-ups?group=admin&org=mine"},
		{"?org=all", "1", "/follow-ups?group=admin&org=all"},
		{"?value=" + coreID, "1", "/follow-ups?group=admin&value=" + coreID},
	} {
		card := pageElement(t, getBody(t, mia, ts.URL+"/follow-ups"+c.query), "a", "follow-ups-group-admin")
		if !strings.Contains(card, `<span class="num">`+c.count+`</span>`) || !strings.Contains(card, "Ownerless · 1") {
			t.Errorf("/follow-ups%s: the Needs an Admin card doesn't count the company's 1: %s", c.query, card)
		}
		if href := html.UnescapeString(attr(openTag(card), "href")); href != c.href {
			t.Errorf("/follow-ups%s: the Needs an Admin card links to %q, want %q", c.query, href, c.href)
		}
	}
	page = getBody(t, mia, ts.URL+"/follow-ups?group=admin&org=mine")
	if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, []string{"Pax's goal"}) {
		t.Errorf("Needs an Admin in Mia's Mine: rows = %q, want Pax's Ownerless Goal", got)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-admin"); !strings.Contains(card, `<span class="num">1</span>`) {
		t.Errorf("Needs an Admin in Mia's Mine: the card doesn't count 1: %s", card)
	}
	page = getBody(t, mia, ts.URL+"/follow-ups?value="+strconv.FormatInt(edge.ID, 10))
	if card := pageElement(t, page, "div", "follow-ups-group-admin"); !strings.Contains(card, "Nothing needs an Admin.") {
		t.Errorf("the Needs an Admin card doesn't keep the page's values: %s", card)
	}

	miaID := strconv.FormatInt(org.Mia.ID, 10)
	page = getBody(t, mia, ts.URL+"/follow-ups?group=admin&org="+miaID)
	if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != miaID {
		t.Errorf("Needs an Admin in Mia's org shows %q chosen, want %s", value, miaID)
	}
	if all := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-show-all")), "href")); all != "/follow-ups?org="+miaID {
		t.Errorf("Show all from Needs an Admin links to %q, want it to keep Mia's org", all)
	}
	if href := html.UnescapeString(attr(openTag(pageElement(t, page, "a", "follow-ups-group-owner")), "href")); href != "/follow-ups?group=owner&org="+miaID {
		t.Errorf("the owner card from Needs an Admin links to %q, want it to keep Mia's org", href)
	}
}

// With nothing for an Admin anywhere, Needs an Admin says so in one line, with
// nowhere wider to link, whatever the page's scope.
func TestFollowUpsNeedsAnAdminWithNothingSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, query := range []string{"", "&org=mine", "&org=all"} {
		page := getBody(t, client, ts.URL+"/follow-ups?group=admin"+query)
		if empty := strings.TrimSpace(pageElement(t, page, "p", "follow-ups-empty")); !strings.HasSuffix(empty, ">Nothing needs an Admin.") {
			t.Errorf("group=admin%s: the empty line doesn't say only that nothing needs an Admin: %s", query, empty)
		}
		if strings.Contains(page, "Show everything") {
			t.Errorf("group=admin%s: the empty line offers Show everything", query)
		}
	}
}

// An Admin whose own scope has nothing flagged still sees, after the empty
// line, the Needs an Admin card counting the company's, the other cards blank;
// anyone else sees only the empty line.
func TestFollowUpsShowsAnAdminTheirCardOnAnEmptyScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Orphaned work", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/follow-ups")
	empty := pageElement(t, page, "p", "follow-ups-empty")
	if !strings.Contains(empty, "No Goals need attention in Mine.") {
		t.Errorf("the Admin's empty Mine doesn't say so: %s", empty)
	}
	card := pageElement(t, page, "a", "follow-ups-group-admin")
	if !strings.Contains(card, `<span class="num">1</span>`) || !strings.Contains(card, "Ownerless · 1") {
		t.Errorf("the Admin's Needs an Admin card doesn't count the company's 1: %s", card)
	}
	if strings.Index(page, `data-testid="follow-ups-empty"`) > strings.Index(page, `data-testid="follow-ups-groups"`) {
		t.Errorf("the cards come before the empty line")
	}
	for key, blank := range map[string]string{"owner": "Nothing is waiting on a Check-in.", "followup": "No Action Item is overdue.", "plan": "Every Goal is placed and its dates fit."} {
		if card := pageElement(t, page, "div", "follow-ups-group-"+key); !strings.Contains(card, blank) {
			t.Errorf("the Admin's %s card isn't blank: %s", key, card)
		}
	}
	for _, gone := range []string{"<table", `data-testid="follow-ups-attention"`, "follow-ups-show-all"} {
		if strings.Contains(page, gone) {
			t.Errorf("the Admin's empty Mine has %s", gone)
		}
	}

	page = getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/follow-ups")
	pageElement(t, page, "p", "follow-ups-empty")
	for _, gone := range []string{`data-testid="follow-ups-groups"`, `data-testid="follow-ups-group-`} {
		if strings.Contains(page, gone) {
			t.Errorf("a non-Admin's empty Mine has %s", gone)
		}
	}
}

// The top bar's Follow-ups count is the viewer's default scope, as many as a bare
// /follow-ups lists: My org's for a manager, Mine's, Delegate Goals included, for
// someone with no one under them, an Admin among them.
func TestTopBarFollowUpsCountIsTheViewersDefaultScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Boss's goal", "It matters.")
	h.AddDelegate(org.Oz, boss, org.OzGoal.ID)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	for _, c := range []struct {
		email string
		count int
	}{
		{"vera@example.com", 5},
		{"mia@example.com", 2},
		{"pia@example.com", 1},
		{"boss@example.com", 2},
	} {
		client := signInClient(t, ts.URL, c.email)
		page := getBody(t, client, ts.URL+"/follow-ups")
		if rows := len(followUpRows(page)); rows != c.count {
			t.Errorf("%s: a bare /follow-ups lists %d rows, want %d", c.email, rows, c.count)
		}
		for _, path := range []string{"/home", "/goals", "/follow-ups"} {
			nav := pageElement(t, getBody(t, client, ts.URL+path), "a", "nav-follow-ups")
			if want := `<span class="count">` + strconv.Itoa(c.count) + `</span>`; !strings.Contains(nav, want) {
				t.Errorf("%s on %s: the nav's Follow-ups count isn't the %d a bare /follow-ups lists: %s", c.email, path, c.count, nav)
			}
		}
	}
}

// Scoped to My org, the Follow-ups page keeps the Goals whose Owner is in the
// viewer's org, their own included: for a VP every level below them, for a
// manager only their part, and its count line names it. The header form's one
// Scope select, its first control, offers Everyone, Mine, My org, then each
// other person's org under Other orgs; My org only to a viewer with anyone
// under them. There is no scope toggle beside the count.
func TestFollowUpsPageScopedToMyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Team", "Platform")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)

	vera := signInClient(t, ts.URL, "vera@example.com")
	page := getBody(t, vera, ts.URL+"/follow-ups?org=all")
	sel := scopeSelect(t, page, "follow-ups-scope")
	choices, value, label := scopeChoices(sel)
	if want := []string{
		"Everyone", "Mine", "My org",
		"Other orgs: Max Marsh (max@example.com)", "Other orgs: Mia Moss (mia@example.com)", "Other orgs: Oz Ortiz (oz@example.com)",
		"Other orgs: Pax Penn (pax@example.com)", "Other orgs: Pia Park (pia@example.com)",
	}; !slices.Equal(choices, want) {
		t.Errorf("the Scope select offers %q, want %q", choices, want)
	}
	if value != "all" || label != "Everyone" {
		t.Errorf("for Everyone, the Scope select shows %q %q chosen, want Everyone", value, label)
	}
	filters := between(t, page, `data-testid="follow-ups-filters"`, "</form>")
	if first := regexp.MustCompile(`<select data-testid="([^"]*)"`).FindAllStringSubmatch(filters, -1); len(first) != 2 || first[0][1] != "follow-ups-scope" || first[1][1] != "follow-ups-value" {
		t.Errorf("the Scope select isn't the header form's first control, ahead of the value select:\n%s", filters)
	}
	if strings.Contains(page, "rk-scope") || strings.Contains(page, "follow-ups-scope-") {
		t.Errorf("the Follow-ups page still has a scope toggle")
	}
	if head := html.UnescapeString(pageElement(t, page, "p", "follow-ups-attention")); !strings.HasSuffix(head, "</strong> Goals to review, most urgent first.") {
		t.Errorf("for Everyone, the count line changed: %s", head)
	}

	page = getBody(t, vera, scopeOrgURL(ts.URL, "/follow-ups", org.Vera))
	if got, want := orgScopedFollowUpTitles(page, org.goals()...), []string{"Max's goal", "Mia's goal", "Pax's goal", "Pia's goal", "Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Vera's My org: rows = %q, want %q", got, want)
	}
	if head := pageElement(t, page, "p", "follow-ups-attention"); !strings.HasSuffix(head, "<strong>5</strong> Goals to review in My org, most urgent first.") {
		t.Errorf("Vera's My org: the count line doesn't count 5 Goals in My org: %s", head)
	}
	if _, value, label := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != strconv.FormatInt(org.Vera.ID, 10) || label != "My org" {
		t.Errorf("scoped to My org, the Scope select shows %q %q chosen", value, label)
	}

	mia := signInClient(t, ts.URL, "mia@example.com")
	page = getBody(t, mia, scopeOrgURL(ts.URL, "/follow-ups", org.Mia))
	if got, want := orgScopedFollowUpTitles(page, org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia's My org: rows = %q, want %q", got, want)
	}

	pia := signInClient(t, ts.URL, "pia@example.com")
	page = getBody(t, pia, ts.URL+"/follow-ups")
	if choices, _, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); slices.Contains(choices, "My org") {
		t.Errorf("Pia, with no one under her, is offered My org: %q", choices)
	}
}

// Anyone may scope the Follow-ups page to another person's org, picked by Name
// with their email, and the page names whose org it is. A Goal is in an org
// by its Owner: a Delegate in the org doesn't bring in a Goal owned outside
// it, and an Ownerless Goal stays in the org its Departed Owner was in.
func TestFollowUpsPageScopedToAnotherPersonsOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.AddDelegate(org.Oz, org.Pax, org.OzGoal.ID)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, org.Pax.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	pia := signInClient(t, ts.URL, "pia@example.com")

	page := getBody(t, pia, ts.URL+"/follow-ups")
	orgSel := scopeSelect(t, page, "follow-ups-scope")
	if !strings.Contains(orgSel, `<option value="`+strconv.FormatInt(org.Max.ID, 10)+`">Max Marsh (max@example.com)</option>`) {
		t.Errorf("the Scope select doesn't offer Max by Name and email: %s", orgSel)
	}

	page = getBody(t, pia, scopeOrgURL(ts.URL, "/follow-ups", org.Max))
	if got, want := orgScopedFollowUpTitles(page, org.goals()...), []string{"Max's goal", "Pax's goal"}; !slices.Equal(got, want) {
		t.Errorf("Max's org: rows = %q, want %q (Pax's Ownerless Goal, not Oz's that Pax is a Delegate on)", got, want)
	}
	if head := html.UnescapeString(pageElement(t, page, "p", "follow-ups-attention")); !strings.HasSuffix(head, "<strong>2</strong> Goals to review in Max Marsh's org, most urgent first.") {
		t.Errorf("the count line doesn't name Max's org as its scope: %s", head)
	}
	orgSel = scopeSelect(t, page, "follow-ups-scope")
	if !strings.Contains(orgSel, `<option value="`+strconv.FormatInt(org.Max.ID, 10)+`" selected>Max Marsh (max@example.com)</option>`) || strings.Count(orgSel, "selected") != 1 {
		t.Errorf("Max's org isn't the one chosen scope: %s", orgSel)
	}
}

// Whether the Follow-ups page offers Orgs is the company's answer, not the
// viewer's: with Vera managing Mia, Oz, who has no Manager and no one under
// them, can open a link to Vera's org and sees it scoped and chosen.
func TestFollowUpsPageScopesAnOrgLinkForAViewerOutsideAnyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	vera := h.SignInNamed("vera@example.com", "Vera Vance")
	mia := h.SignInNamed("mia@example.com", "Mia Moss")
	oz := h.SignInNamed("oz@example.com", "Oz Ortiz")
	h.SetManager(mia, vera)
	veraGoal := h.ActiveGoal(vera, "Vera's goal", "It matters.")
	ozGoal := h.ActiveGoal(oz, "Oz's goal", "It matters.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")

	if got := orgScopedFollowUpTitles(getBody(t, client, ts.URL+"/follow-ups?org=all"), veraGoal, ozGoal); len(got) != 2 {
		t.Fatalf("for Everyone, rows = %q, want both Check-in overdue Goals", got)
	}
	page := getBody(t, client, scopeOrgURL(ts.URL, "/follow-ups", vera))
	if got, want := orgScopedFollowUpTitles(page, veraGoal, ozGoal), []string{"Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Oz in Vera's org: rows = %q, want %q", got, want)
	}
	if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != strconv.FormatInt(vera.ID, 10) {
		t.Errorf("Oz in Vera's org: the Scope select shows %q chosen, want %d", value, vera.ID)
	}
}

// The org lives in the Follow-ups page's address, so its group cards, Show all
// and the scope form keep it, and a reload shows it again; the form keeps no
// Mine of its own, so choosing Everyone from there lists every flagged Goal.
// A ?org= naming no one, or malformed, gives the viewer's default, Mine for
// Oz, the old ?mine=1 beside it changing nothing, and an org with nothing
// needing attention says so.
func TestFollowUpsPageKeepsTheOrgInItsAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.SetManager(lee, org.Vera)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")
	orgID := strconv.FormatInt(org.Mia.ID, 10)

	page := getBody(t, client, ts.URL+"/follow-ups?org="+orgID)
	filters := between(t, page, `data-testid="follow-ups-filters"`, "</form>")
	if strings.Contains(filters, `name="mine"`) {
		t.Errorf("the scope form carries a mine field:\n%s", filters)
	}
	if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != orgID {
		t.Errorf("Mia's org: the Scope select shows %q chosen, want %s", value, orgID)
	}
	values := formValues(filters)
	values.Set("org", "all")
	if got := orgScopedFollowUpTitles(getBody(t, client, ts.URL+"/follow-ups?"+values.Encode()), org.goals()...); len(got) != 6 {
		t.Errorf("choosing Everyone from Mia's org: rows = %q, want all 6", got)
	}
	card := pageElement(t, page, "a", "follow-ups-group-owner")
	address := html.UnescapeString(attr(openTag(card), "href"))
	if want := "/follow-ups?group=owner&org=" + orgID; address != want {
		t.Fatalf("the owner card links to %s, want %s", address, want)
	}

	page = getBody(t, client, ts.URL+address)
	if got, want := orgScopedFollowUpTitles(page, org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("grouped in Mia's org: rows = %q, want %q", got, want)
	}
	if all := pageElement(t, page, "a", "follow-ups-show-all"); attr(openTag(all), "href") != "/follow-ups?org="+orgID {
		t.Errorf("Show all drops the org: %s", all)
	}
	filters = between(t, page, `data-testid="follow-ups-filters"`, "</form>")
	if values := formValues(filters); values.Get("org") != orgID || values.Get("group") != "owner" {
		t.Errorf("the scope form doesn't carry the org and group: %v", values)
	}

	for _, raw := range []string{"abc", "999999", ""} {
		page := getBody(t, client, ts.URL+"/follow-ups?org="+raw)
		if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%q: rows = %q, want Oz's default, Mine", raw, got)
		}
		if _, value, label := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != "mine" || label != "Mine" {
			t.Errorf("org=%q: the Scope select shows %q %q chosen, want Oz's default, Mine", raw, value, label)
		}
		page = getBody(t, client, ts.URL+"/follow-ups?mine=1&org="+raw)
		if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%q beside the old Mine parameter: rows = %q, want Oz's default, Mine", raw, got)
		}
	}

	page = getBody(t, client, scopeOrgURL(ts.URL, "/follow-ups", lee))
	if empty := pageElement(t, page, "p", "follow-ups-empty"); !strings.Contains(empty, "No Goals need attention in Lee Lund&#39;s org.") {
		t.Errorf("an empty org doesn't say so: %s", empty)
	}
}

// The scope parameter is ?org=; the old ?chain= is no scope at all, ignored
// like any unknown parameter, so the page is on the viewer's default, Vera's
// My org, and not Mia's.
func TestFollowUpsPageIgnoresTheOldChainParameter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	vera := signInClient(t, ts.URL, "vera@example.com")

	unscoped := orgScopedFollowUpTitles(getBody(t, vera, ts.URL+"/follow-ups"), org.goals()...)
	page := getBody(t, vera, ts.URL+"/follow-ups?chain="+strconv.FormatInt(org.Mia.ID, 10))
	if got := orgScopedFollowUpTitles(page, org.goals()...); !slices.Equal(got, unscoped) || len(got) != 5 {
		t.Errorf("?chain=: rows = %q, want the unscoped %q", got, unscoped)
	}
	if sel := scopeSelect(t, page, "follow-ups-scope"); !strings.Contains(sel, `<option value="`+strconv.FormatInt(org.Vera.ID, 10)+`" selected>My org</option>`) || strings.Count(sel, "selected") != 1 {
		t.Errorf("?chain=: Vera's default isn't the one chosen scope: %s", sel)
	}
}

// With no Managers anywhere, as when no directory is synced, the Follow-ups page's
// Scope select offers no org, only Everyone and Mine, and a ?org= link names
// no one, so gives the viewer's default, Mine.
func TestFollowUpsPageOffersNoOrgWithoutManagers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Sam's", "It matters.")
	h.ActiveGoal(kim, "Kim's", "It matters.")
	h.Clock.Advance(10 * day)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org="+strconv.FormatInt(kim.ID, 10))
	if choices, _, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); !slices.Equal(choices, []string{"Everyone", "Mine"}) {
		t.Errorf("without Managers the Scope select offers %q, want only Everyone and Mine", choices)
	}
	if rows := followUpRows(page); len(rows) != 1 {
		t.Errorf("without Managers a ?org= scoped the page to %d Goals, want Mine's 1", len(rows))
	}
	if _, value, _ := scopeChoices(scopeSelect(t, page, "follow-ups-scope")); value != "mine" {
		t.Errorf("without Managers a ?org= chose %q, want the default Mine", value)
	}
}

// A Follow-ups schedule-conflict chip says the delivery date the way the Goal page
// it links to does, in this year and the next (ticket #276).
func TestFollowUpsScheduleConflictDateReadsAsTheGoalPageDoes(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	for _, c := range []struct {
		parentDue, due time.Time
		shown          string
	}{
		{time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC), "Dec 28"},
		{time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC), "Jan 10, 2027"},
	} {
		parent := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Parent due "+c.shown, c.parentDue))
		child := h.ActiveGoalDue(sam, "Child due "+c.shown, c.due)
		h.RequestLink(sam, child, parent, "")

		chip := followUpChip(t, followUpRowOf(t, getBody(t, client, ts.URL+"/follow-ups"), child), "schedule-conflicts")
		if !strings.Contains(chip, "due "+c.shown+",") {
			t.Errorf("the chip doesn't say due %s: %s", c.shown, chip)
		}
		delivery := pageElement(t, getBody(t, client, goalPageURL(ts.URL, child)), "span", "goal-delivery-date")
		if !strings.Contains(delivery, "<strong>"+c.shown+"</strong>") {
			t.Errorf("the Goal page doesn't show its delivery date as %s, as its Follow-ups chip does: %s", c.shown, delivery)
		}
	}
}

// followUpSignalKinds lists the kinds of a row's signal chips, in order.
func followUpSignalKinds(row string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`data-testid="follow-up-signal" data-kind="([^"]+)"`).FindAllStringSubmatch(row, -1) {
		out = append(out, m[1])
	}
	return out
}

// raiseFollowUpActionItem has author, who saves and publishes a Report covering
// goal, raise an Action Item about it, owned by owner and due dueInDays from
// today, so a negative dueInDays is already overdue.
func raiseFollowUpActionItem(t *testing.T, h *testsupport.Harness, author domain.Account, goal domain.Goal, owner domain.Account, text string, dueInDays int) domain.ActionItem {
	t.Helper()
	pub := h.PublishReport(author, h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{
		Name: "MBR · " + text, Mode: domain.ReportModePicked, Picked: []int64{goal.ID},
	}))
	item, err := h.Service.RaiseActionItem(t.Context(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: goal.ID, Text: text, OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, dueInDays),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem %q: %v", text, err)
	}
	return item
}

// A Green Goal with an Action Item two days past its due date is on the Follow-ups
// page with a chip, drawn like Path to Green overdue's, saying how long, and
// its Health is still Green: an overdue Action Item doesn't change a Goal's
// Health.
func TestFollowUpsPageListsGoalsWithOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, launch)
	if chip := followUpChip(t, row, "action-item-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Action Item 2 days overdue</button>") {
		t.Errorf("overdue Action Item chip is not a Check-in overdue-colored badge saying 2 days overdue: %s", chip)
	}
	if health := pageElement(t, row, "span", "follow-up-health"); !strings.Contains(openTag(health), `class="badge g"`) {
		t.Errorf("row doesn't show the Goal's Green Health: %s", health)
	}
}

// A row flagged only for an overdue Action Item has one Fix, See Action
// Items, linking to its Goal page's Action Items.
func TestFollowUpFixOnAnOverdueActionItemIsSeeActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	for _, who := range []string{"sam@example.com", "ada@example.com"} {
		page := getBody(t, signInClient(t, ts.URL, who), ts.URL+"/follow-ups?org=all")

		row := followUpRowOf(t, page, launch)
		if got := followUpSignalKinds(row); !slices.Equal(got, []string{"action-item-overdue"}) {
			t.Fatalf("%s: row's signals = %q, want only action-item-overdue", who, got)
		}
		label, href, primary := followUpFix(t, row)
		if want := "/goals/" + strconv.FormatInt(launch.ID, 10) + "#action-items"; label != "See Action Items" || href != want || primary {
			t.Errorf("%s: Fix = %q → %q (primary %v), want See Action Items → %q", who, label, href, primary, want)
		}
	}
}

// Action Items overdue is a group of its own, its card after Owner needs to
// update's: filtered to it, the page lists only the Goals an overdue Action
// Item flags. With none, its card is muted and says no Action Item is
// overdue.
func TestFollowUpsPageGroupsOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	if card := pageElement(t, page, "div", "follow-ups-group-followup"); !strings.Contains(openTag(card), "rk-zero") || !strings.Contains(card, "No Action Item is overdue.") {
		t.Errorf("with no overdue Action Item, the followup card isn't muted saying so: %s", card)
	}

	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)
	page = getBody(t, client, ts.URL+"/follow-ups?org=all")

	card := pageElement(t, page, "a", "follow-ups-group-followup")
	for _, want := range []string{`href="/follow-ups?group=followup&amp;org=all"`, "Action Items overdue", `<span class="num">1</span>`, "Action Item overdue · 1"} {
		if !strings.Contains(card, want) {
			t.Errorf("followup card lacks %s: %s", want, card)
		}
	}
	if chip := followUpChip(t, card, "action-item-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) {
		t.Errorf("the followup card's chip isn't drawn like the table's: %s", chip)
	}
	if order := []int{
		strings.Index(page, `data-testid="follow-ups-group-owner"`),
		strings.Index(page, `data-testid="follow-ups-group-followup"`),
		strings.Index(page, `data-testid="follow-ups-group-plan"`),
	}; !slices.IsSorted(order) {
		t.Errorf("the followup card isn't between the owner and plan cards: %v", order)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?group=followup&org=all")

	followUpChip(t, followUpRowOf(t, page, launch), "action-item-overdue")
	if hasFollowUpRow(page, silent) {
		t.Errorf("filtered to followup, the page lists %q, flagged only outside it", silent.Title)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-followup"); !strings.Contains(openTag(card), `aria-current="page"`) {
		t.Errorf("filtered to followup, its card isn't current: %s", openTag(card))
	}
}

// The Follow-ups page's five group cards sit five across at the page's full width,
// two across where the page is narrower, and stack below 900px.
func TestFollowUpsGroupCardsFitFiveAcross(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	if n := strings.Count(page, `data-testid="follow-ups-group-`); n != 5 {
		t.Errorf("the page has %d group cards, want 5", n)
	}
	css := between(t, page, "<style>", "</style>")
	for query, want := range map[string]string{
		"":                          "grid-template-columns:repeat(5,minmax(0,1fr))",
		"@media (max-width:1151px)": "grid-template-columns:repeat(2,minmax(0,1fr))",
		"@media (max-width:900px)":  "grid-template-columns:minmax(0,1fr)",
	} {
		block := css
		if query != "" {
			block = mediaBlock(t, css, query)
		}
		if rule, _ := ruleFor(block, ".rk-cards"); !strings.Contains(rule, want) {
			t.Errorf("%q .rk-cards{%s} lacks %s", query, rule, want)
		}
	}
}

// A Goal flagged only for an overdue Action Item counts in the top bar's
// Follow-ups count, as it does on the page.
func TestTopBarFollowUpsCountsOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	launch := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Launch in EU", "Expand the market."))
	client := signInClient(t, ts.URL, "sam@example.com")

	if nav := pageElement(t, getBody(t, client, ts.URL+"/home"), "a", "nav-follow-ups"); !strings.Contains(nav, `<span class="count">1</span>`) {
		t.Fatalf("before the Action Item is overdue, the nav's Follow-ups count isn't 1: %s", nav)
	}
	raiseFollowUpActionItem(t, h, ada, launch, sam, "Book the legal review.", -2)

	if nav := pageElement(t, getBody(t, client, ts.URL+"/home"), "a", "nav-follow-ups"); !strings.Contains(nav, `<span class="count">2</span>`) {
		t.Errorf("with an overdue Action Item, the nav's Follow-ups count isn't 2: %s", nav)
	}
}

// The page's scope narrows overdue Action Items like any other row: under
// Mine, a Goal the viewer doesn't Own isn't listed for one, however much the
// viewer owns the item; under Everyone it is.
func TestFollowUpsPageScopesOverdueActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	own := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Own work", "It matters."))
	kims := h.MarkTopLevel(ada, h.ActiveGoal(kim, "Kim's work", "It matters."))
	raiseFollowUpActionItem(t, h, ada, own, kim, "Chase the vendor.", -1)
	raiseFollowUpActionItem(t, h, ada, kims, sam, "Send Kim the numbers.", -3)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=mine")

	followUpChip(t, followUpRowOf(t, page, own), "action-item-overdue")
	if hasFollowUpRow(page, kims) {
		t.Errorf("under Mine, the page lists %q, which Sam doesn't Own", kims.Title)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-followup"); !strings.Contains(card, `<span class="num">1</span>`) {
		t.Errorf("under Mine, the followup card doesn't count Sam's 1: %s", card)
	}

	page = getBody(t, client, ts.URL+"/follow-ups?org=all")

	if chip := followUpChip(t, followUpRowOf(t, page, kims), "action-item-overdue"); !strings.HasSuffix(chip, ">Action Item 3 days overdue</button>") {
		t.Errorf("under Everyone, %q's chip doesn't say 3 days overdue: %s", kims.Title, chip)
	}
	followUpChip(t, followUpRowOf(t, page, own), "action-item-overdue")
}

// An Action Item due today, a closed one past its due date, one about the
// whole Report, and one past its due date on a Done Goal flag nothing.
func TestFollowUpsPageIgnoresActionItemsNotOverdueAboutAnActiveGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	goal := func(title string) domain.Goal {
		g := h.MarkTopLevel(ada, h.ActiveGoal(sam, title, "It matters."))
		h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
		return g
	}
	dueToday := goal("Due today")
	raiseFollowUpActionItem(t, h, ada, dueToday, sam, "Do it today.", 0)
	closed := goal("Closed item")
	item := raiseFollowUpActionItem(t, h, ada, closed, sam, "Done already.", -4)
	if _, err := h.Service.CloseActionItem(t.Context(), sam.ID, item.ID, "Handled."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	whole := goal("In a Report")
	pub := h.PublishReport(ada, h.SaveReportDefinition(ada, domain.SaveReportDefinitionInput{
		Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{whole.ID},
	}))
	if _, err := h.Service.RaiseActionItem(t.Context(), ada.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Move the QBR.", OwnerID: sam.ID, DueDate: h.Clock.Now().AddDate(0, 0, -5),
	}); err != nil {
		t.Fatalf("RaiseActionItem about the whole Report: %v", err)
	}
	done := goal("Done goal")
	raiseFollowUpActionItem(t, h, ada, done, sam, "Write the retro.", -2)
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	for _, g := range []domain.Goal{dueToday, closed, whole, done} {
		if hasFollowUpRow(page, g) {
			t.Errorf("the page lists %q:\n%s", g.Title, followUpRowOf(t, page, g))
		}
	}
	if !strings.Contains(page, `data-testid="follow-ups-empty"`) {
		t.Errorf("with no overdue Action Item about an Active Goal, the page isn't empty")
	}
	if nav := pageElement(t, page, "a", "nav-follow-ups"); strings.Contains(nav, "count") {
		t.Errorf("the nav counts a Follow-up: %s", nav)
	}
}

// Not Green is the Follow-ups page's first card: it holds every Active Goal whose
// latest Check-in is Red or Yellow, chipped like their Health badges, and the
// top bar counts them. A Red or Yellow row takes no chip of its own, as its
// Health badge says so already, and its Fix opens the Goal. A Green Goal with
// nothing else wrong isn't listed.
func TestFollowUpsPageListsRedAndYellowGoalsUnderNotGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	later := h.Clock.Now().AddDate(0, 1, 0)
	red := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", later)
	yellow := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Slipping launch", "It matters."))
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	green := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Steady work", "It matters."))
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	if first := strings.Index(page, `data-testid="follow-ups-group-`); first < 0 || !strings.HasPrefix(page[first:], `data-testid="follow-ups-group-health"`) {
		t.Errorf("the Not Green card isn't the first card:\n%s", page)
	}
	card := pageElement(t, page, "a", "follow-ups-group-health")
	if !strings.Contains(card, "Not Green") || !strings.Contains(card, `<span class="num">2</span>`) {
		t.Errorf("the Not Green card doesn't count 2 Goals: %s", card)
	}
	for kind, want := range map[string]struct{ class, text string }{
		"red":    {`class="badge r"`, ">Red · 1"},
		"yellow": {`class="badge y"`, ">Yellow · 1"},
	} {
		if chip := followUpChip(t, card, kind); !strings.Contains(openTag(chip), want.class) || !strings.HasSuffix(chip, want.text) {
			t.Errorf("the Not Green card's %s chip isn't %s saying %s: %s", kind, want.class, want.text, chip)
		}
	}
	for _, g := range []domain.Goal{red, yellow} {
		row := followUpRowOf(t, page, g)
		if strings.Contains(row, `data-testid="follow-up-signal"`) {
			t.Errorf("%q's row chips its Health:\n%s", g.Title, row)
		}
		if label, href, _ := followUpFix(t, row); label != "Open Goal" || href != "/goals/"+strconv.FormatInt(g.ID, 10) {
			t.Errorf("%q's Fix = %q %q, want Open Goal", g.Title, label, href)
		}
	}
	if hasFollowUpRow(page, green) {
		t.Errorf("%q, Green with nothing wrong, is listed:\n%s", green.Title, followUpRowOf(t, page, green))
	}

	filtered := getBody(t, client, ts.URL+"/follow-ups?group=health&org=all")
	if rows := followUpRows(filtered); len(rows) != 2 || !hasFollowUpRow(filtered, red) || !hasFollowUpRow(filtered, yellow) {
		t.Errorf("Not Green lists %d rows, want only %q and %q:\n%s", len(rows), red.Title, yellow.Title, filtered)
	}

	if nav := pageElement(t, getBody(t, client, ts.URL+"/home"), "a", "nav-follow-ups"); !strings.Contains(nav, `<span class="count">2</span>`) {
		t.Errorf("the nav's Follow-ups count doesn't count the Red and Yellow Goals: %s", nav)
	}
}

// A Not Green row reads its Goal's Run off Green beside its Health ("Red · 2
// wks"), on the whole page and in the Not Green group; a Green Goal listed for
// another signal, here Check-in overdue, shows none (CONTEXT.md: Run off
// Green).
func TestFollowUpsNotGreenRowsShowTheRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	red := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	checkinOverdue := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	// Red on Mondays 5 and 12 January; the Green Check-in on the 5th is
	// Check-in overdue by Wednesday the 14th, when the page is read.
	h.Clock.Set(time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC))
	h.Checkin(ada, checkinOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(ada, red.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0))
	h.Clock.Set(time.Date(2026, time.January, 12, 9, 0, 0, 0, time.UTC))
	// A month out from a week later moves the back-to-Green date later.
	h.CheckinMovingPath(ada, red.ID, domain.HealthRed, "Still blocked.", "Escalate.", h.Clock.Now().AddDate(0, 1, 0), "Escalation takes longer.")
	h.Clock.Set(time.Date(2026, time.January, 14, 9, 0, 0, 0, time.UTC))
	client := signInClient(t, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")
	for name, body := range map[string]string{
		"the Follow-ups page": page,
		"Not Green":           getBody(t, client, ts.URL+"/follow-ups?group=health&org=all"),
	} {
		row := followUpRowOf(t, body, red)
		if got := elementTexts(row, "span", "follow-up-run-off-green"); !slices.Equal(got, []string{"· 2 wks"}) {
			t.Errorf("on %s, %q's row reads its Run off Green %q, want \"· 2 wks\":\n%s", name, red.Title, got, row)
		}
		health, run := strings.Index(row, `data-testid="follow-up-health"`), strings.Index(row, `data-testid="follow-up-run-off-green"`)
		if health < 0 || run < health || strings.Contains(row[health:max(run, health)], "</td>") {
			t.Errorf("on %s, %q's Run off Green isn't right after its Health badge:\n%s", name, red.Title, row)
		}
	}
	if row := followUpRowOf(t, page, checkinOverdue); strings.Contains(row, `data-testid="follow-up-run-off-green"`) {
		t.Errorf("%q, Green and Check-in overdue, shows a Run off Green:\n%s", checkinOverdue.Title, row)
	}
}

// A Proposed Goal under Placement and dates is listed, but after every Active
// Goal: nothing has gone wrong with it yet. Lacking a Health doesn't rank it
// above a Green Goal that has gone Check-in overdue.
func TestFollowUpsPageListsGoalsThatArentActiveLast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	checkinOverdue := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Zero-downtime deploys", "It matters."))
	h.Checkin(sam, checkinOverdue.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(10 * day)
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	proposed := h.CreateGoal(sam, "Article backlog", "It matters.")
	if _, err := h.Service.MarkGoalDated(t.Context(), proposed.ID, june.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	h.RequestLink(sam, proposed, launch, "")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

	followUpChip(t, followUpRowOf(t, page, checkinOverdue), "checkin-overdue")
	followUpChip(t, followUpRowOf(t, page, proposed), "schedule-conflicts")
	var order []string
	for _, row := range followUpRows(page) {
		for _, g := range []domain.Goal{checkinOverdue, proposed} {
			if isFollowUpRowOf(row, g) {
				order = append(order, g.Title)
			}
		}
	}
	if want := []string{checkinOverdue.Title, proposed.Title}; !slices.Equal(order, want) {
		t.Errorf("rows in order %q, want %q", order, want)
	}
}

// A Goal with an open Ask for help carries an "Asks for help" chip in Not
// Green naming whom it asks, or saying no one is above its Owner; the Not
// Green card counts the asks. A Yellow Goal that doesn't ask has no such
// chip, and an asking Goal is still counted once.
func TestFollowUpsPageMarksAsksForHelpUnderNotGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	mia := h.SignInNamed("mia@example.com", "Mia Chen")
	lee := h.SignIn("lee@example.com")
	h.SetManager(sam, mia)
	later := h.Clock.Now().AddDate(0, 1, 0)
	red := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.AskForHelp(sam, red.ID, domain.HealthRed, "Blocked.", "Need budget.", later)
	alone := h.MarkTopLevel(ada, h.ActiveGoal(lee, "Lonely launch", "It matters."))
	h.AskForHelp(lee, alone.ID, domain.HealthYellow, "Slipping.", "Need a reviewer.", later)
	yellow := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Slipping launch", "It matters."))
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	for g, want := range map[*domain.Goal]string{
		&red:   `<span data-testid="follow-up-signal" data-kind="asks-for-help" class="rk-tag"><button type="button" class="rk-tip" popovertarget="follow-up-means-asks-for-help" aria-describedby="follow-up-means-asks-for-help">Asks for help</button> of ` + shownAs(mia.Email, "Mia Chen") + `</span>`,
		&alone: `<span data-testid="follow-up-signal" data-kind="asks-for-help" class="rk-tag"><button type="button" class="rk-tip" popovertarget="follow-up-means-asks-for-help" aria-describedby="follow-up-means-asks-for-help">Asks for help · no one above the Owner</button></span>`,
	} {
		if row := followUpRowOf(t, page, *g); strings.Count(row, `data-kind="asks-for-help"`) != 1 || !strings.Contains(row, want) {
			t.Errorf("%q's row doesn't carry one chip %s:\n%s", g.Title, want, row)
		}
	}
	if row := followUpRowOf(t, page, yellow); strings.Contains(row, `data-kind="asks-for-help"`) {
		t.Errorf("%q, which doesn't ask, carries an Asks for help chip:\n%s", yellow.Title, row)
	}
	card := pageElement(t, page, "a", "follow-ups-group-health")
	if chip := followUpChip(t, card, "asks-for-help"); !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.HasSuffix(chip, ">Asks for help · 2") {
		t.Errorf("the Not Green card's asks chip isn't an rk-tag saying Asks for help · 2: %s", chip)
	}
	if !strings.Contains(card, `<span class="num">3</span>`) {
		t.Errorf("the Not Green card doesn't count 3 Goals: %s", card)
	}

	filtered := getBody(t, client, ts.URL+"/follow-ups?group=health&org=all")
	if rows := followUpRows(filtered); len(rows) != 3 || !hasFollowUpRow(filtered, red) || !hasFollowUpRow(filtered, alone) || !hasFollowUpRow(filtered, yellow) {
		t.Errorf("Not Green lists %d rows, want %q, %q and %q:\n%s", len(rows), red.Title, alone.Title, yellow.Title, filtered)
	}

	// The top bar counts sam's default scope, their own two Goals, each once.
	if nav := pageElement(t, getBody(t, client, ts.URL+"/home"), "a", "nav-follow-ups"); !strings.Contains(nav, `<span class="count">2</span>`) {
		t.Errorf("the nav's Follow-ups count doesn't count each of sam's Goals once: %s", nav)
	}
}

// An ask is cleared from Follow-ups when its Owner checks in again without it,
// though the Goal stays Not Green.
func TestFollowUpsPageDropsAnAskTheOwnerUnmarks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SetManager(sam, h.SignIn("mia@example.com"))
	later := h.Clock.Now().AddDate(0, 1, 0)
	goal := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters."))
	h.AskForHelp(sam, goal.ID, domain.HealthRed, "Blocked.", "Need budget.", later)
	client := signInClient(t, ts.URL, "sam@example.com")
	if row := followUpRowOf(t, getBody(t, client, ts.URL+"/follow-ups?org=all"), goal); !strings.Contains(row, `data-kind="asks-for-help"`) {
		t.Fatalf("the asking Goal's row has no Asks for help chip:\n%s", row)
	}

	h.Checkin(sam, goal.ID, domain.HealthYellow, "Better.", "Keep going.", later)

	if row := followUpRowOf(t, getBody(t, client, ts.URL+"/follow-ups?org=all"), goal); strings.Contains(row, `data-kind="asks-for-help"`) {
		t.Errorf("the row still carries the Asks for help chip after the Owner unmarked it:\n%s", row)
	}
}

// A Goal whose latest Check-in is Green while a Milestone is overdue is listed
// under Waiting on a Check-in with a Milestone overdue chip in Check-in
// overdue's style, counted on its card and in the top bar. Its Fix is Check in
// for its Owner, and Nudge for anyone else, as its Owner answers it in a
// Check-in (CONTEXT.md: Nudge).
func TestFollowUpsPageListsGreenGoalsPastAMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	g, _ := h.GreenGoalWithOverdueMilestone(sam, "Ship search")
	g = h.MarkTopLevel(ada, g)
	goal := "/goals/" + strconv.FormatInt(g.ID, 10)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	row := followUpRowOf(t, page, g)
	if chip := followUpChip(t, row, "milestone-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Milestone 1 day overdue</button>") {
		t.Errorf("Milestone overdue chip is not a Check-in overdue-colored badge saying 1 day overdue: %s", chip)
	}
	if health := pageElement(t, row, "span", "follow-up-health"); !strings.Contains(openTag(health), `class="badge g"`) {
		t.Errorf("row doesn't show the Owner's Green: %s", health)
	}
	card := pageElement(t, page, "a", "follow-ups-group-owner")
	if !strings.Contains(card, `<span class="num">1</span>`) || !strings.Contains(card, "Milestone overdue · 1") {
		t.Errorf("Waiting on a Check-in card doesn't count the Goal with a Milestone overdue · 1 chip: %s", card)
	}
	if chip := followUpChip(t, card, "milestone-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) {
		t.Errorf("the card's Milestone overdue chip isn't drawn like the table's: %s", chip)
	}
	if followUps := pageElement(t, page, "a", "nav-follow-ups"); !strings.Contains(followUps, `<span class="count">1</span>`) {
		t.Errorf("the top bar's Follow-ups doesn't count the Goal: %s", followUps)
	}

	for _, c := range []struct {
		viewer, label, href string
		primary             bool
	}{
		{"sam@example.com", "Check in", goal + "/checkin", true},
		{"kim@example.com", "Nudge", goal + "/nudge", false},
	} {
		page := getBody(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/follow-ups?org=all")
		label, href, primary := followUpFix(t, followUpRowOf(t, page, g))
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
	}
}

// A Not Green row whose back-to-Green date slipped within its Run off Green
// tags the slip as a Milestone's slipped date reads: the current date, the one
// it last slipped from struck through, and the count once it slipped more than
// once. A row whose date never slipped stays quiet, and the tag isn't a signal:
// the Not Green card counts the Goals as before.
func TestFollowUpsNotGreenRowsTagASlippedBackToGreenDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	slipped := h.ActiveGoal(ada, "Launch in EU", "Expand the market.")
	steady := h.ActiveGoal(ada, "Cut churn", "Keep customers.")
	h.Clock.Set(time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC))
	h.Checkin(ada, slipped.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, time.February, 6, 0, 0, 0, 0, time.UTC))
	h.Checkin(ada, steady.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, time.February, 6, 0, 0, 0, 0, time.UTC))
	h.Clock.Set(time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC))
	h.CheckinMovingPath(ada, slipped.ID, domain.HealthRed, "Still blocked.", "Escalate.", time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC), "Escalation takes longer.")
	h.Clock.Set(time.Date(2026, time.January, 7, 9, 0, 0, 0, time.UTC))
	h.CheckinMovingPath(ada, slipped.ID, domain.HealthRed, "Still blocked.", "Escalate.", time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC), "The approver is away.")
	client := signInClient(t, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/follow-ups?org=all")

	row := followUpRowOf(t, page, slipped)
	tag := between(t, row, `<span data-testid="follow-up-back-to-green"`, `</td>`)
	text := strings.Join(strings.Fields(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(tag, ""))), " ")
	if want := "Back to Green Mar 3 Feb 13 (2)"; text != want {
		t.Errorf("%q's row tags its back-to-Green date %q, want %q:\n%s", slipped.Title, text, want, row)
	}
	if !strings.Contains(tag, "<del>Feb 13</del>") || !strings.Contains(tag, `<span data-testid="follow-up-back-to-green-slips">(2)</span>`) {
		t.Errorf("%q's tag doesn't strike Feb 13 through and count 2 slips:\n%s", slipped.Title, tag)
	}
	if !strings.Contains(between(t, row, `class="rk-chips"`, `</td>`), `data-testid="follow-up-back-to-green"`) {
		t.Errorf("%q's tag isn't among its chips:\n%s", slipped.Title, row)
	}
	if row := followUpRowOf(t, page, steady); strings.Contains(row, `data-testid="follow-up-back-to-green"`) {
		t.Errorf("%q, whose back-to-Green date never slipped, tags one:\n%s", steady.Title, row)
	}
	if card := pageElement(t, page, "a", "follow-ups-group-health"); !strings.Contains(card, `<span class="num">2</span>`) {
		t.Errorf("the Not Green card doesn't count its 2 Goals: %s", card)
	}
}

// followUpPopover returns the content of the page's native popover with id,
// failing the test unless one is there.
func followUpPopover(t *testing.T, page, id string) string {
	t.Helper()
	tag := tagAround(t, page, ` id="`+id+`"`)
	if !regexp.MustCompile(`\spopover(="auto")?[\s>]`).MatchString(tag) {
		t.Fatalf("#%s is not a native popover: %s", id, tag)
	}
	name := strings.Fields(tag[1:])[0]
	return between(t, page[strings.Index(page, tag)+len(tag):], "", "</"+name+">")
}

// Each signal chip in the Follow-ups table names its signal with a button that,
// with no script, opens a popover saying what the signal means, so a tap
// explains it where it's shown. Chips holding a link or a person name the
// signal with the button and keep the link outside it. Every signal but Red
// and Yellow, which the Health badge says, takes a chip, and each is checked.
func TestFollowUpsSignalChipsOpenWhatTheyMean(t *testing.T) {
	t.Parallel()

	checked := map[string]bool{}
	check := func(t *testing.T, page string, g domain.Goal, kinds ...string) {
		t.Helper()
		row := followUpRowOf(t, page, g)
		for _, kind := range kinds {
			checked[kind] = true
			chip := followUpChip(t, row, kind)
			at := strings.Index(chip, "<button")
			if at < 0 {
				t.Errorf("%q's %s chip has no button:\n%s", g.Title, kind, chip)
				continue
			}
			button := chip[at:]
			if attr(openTag(button), "type") != "button" {
				t.Errorf("%q's %s button isn't type=button: %s", g.Title, kind, openTag(button))
			}
			end := strings.Index(button, "</button>")
			if end < 0 || strings.TrimSpace(button[len(openTag(button))+1:end]) == "" {
				t.Errorf("%q's %s button has no name: %s", g.Title, kind, button)
			}
			target := attr(openTag(button), "popovertarget")
			if target == "" {
				t.Errorf("%q's %s button opens no popover: %s", g.Title, kind, openTag(button))
				continue
			}
			def := followUpSignalDefinition(t, kind)
			if pop, want := html.UnescapeString(followUpPopover(t, page, target)), "<strong>"+def.Name+"</strong> "+def.Means; pop != want {
				t.Errorf("%q's %s popover = %q, want %q", g.Title, kind, pop, want)
			}
		}
	}

	t.Run("by who acts", func(t *testing.T) {
		h := testsupport.New(t, "ada@example.com")
		ts := newServer(t, h)
		ada := h.SignIn("ada@example.com")
		sam := h.SignIn("sam@example.com")
		kim := h.SignIn("kim@example.com")
		mia := h.SignInNamed("mia@example.com", "Mia Chen")
		h.SetManager(sam, mia)
		june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
		silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue, Unlinked and an overdue Action Item
		h.Clock.Advance(10 * day)
		h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
		raiseFollowUpActionItem(t, h, ada, silent, sam, "Break the silence.", -1)
		orphaned := h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
		if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
			t.Fatalf("MarkDeparted: %v", err)
		}
		late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
		h.RequestLink(sam, late, launch, "")
		stalled := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Stalled recovery", "It matters.")) // Asks for help
		h.AskForHelp(sam, stalled.ID, domain.HealthRed, "Blocked.", "Need budget.", june)

		page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups?org=all")

		check(t, page, silent, "checkin-overdue", "action-item-overdue", "unlinked")
		check(t, page, orphaned, "ownerless", "unlinked")
		check(t, page, late, "schedule-conflicts")
		check(t, page, stalled, "asks-for-help")
	})

	t.Run("by what is late or halted", func(t *testing.T) {
		h := testsupport.New(t)
		ts := newServer(t, h)
		sam := h.SignIn("sam@example.com")
		passed, _ := h.GreenGoalWithOverdueMilestone(sam, "Ship search") // Milestone overdue
		stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")  // Path to Green overdue
		h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 0, 1))
		paused := h.ActiveGoal(sam, "Paused outcome", "It mattered.")
		under := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.") // Parent On Hold
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID: paused.ID, AuthorID: sam.ID, Status: "Stopping.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
		}); err != nil {
			t.Fatalf("SubmitCheckin On Hold: %v", err)
		}
		h.Clock.Advance(3 * day)

		page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

		check(t, page, passed, "milestone-overdue")
		check(t, page, stalled, "path-overdue")
		check(t, page, under, "halted-parents")
	})

	for _, d := range web.FollowUpSignalDefinitions {
		if d.Kind != "red" && d.Kind != "yellow" && !checked[d.Kind] {
			t.Errorf("no %s chip is checked for what it means", d.Kind)
		}
	}
}

// followUpSignalDefinition returns the Follow-ups page's definition of the
// signal of kind.
func followUpSignalDefinition(t *testing.T, kind string) web.FollowUpSignalDefinition {
	t.Helper()
	for _, d := range web.FollowUpSignalDefinitions {
		if d.Kind == kind {
			return d
		}
	}
	t.Fatalf("no signal of kind %q", kind)
	return web.FollowUpSignalDefinition{}
}

// A group card stays a link holding no button, though the table's chips for
// the same signals open what they mean: a button can't sit inside a link.
func TestFollowUpsGroupCardsHoldNoButton(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Check-in overdue and Unlinked
	h.Clock.Advance(10 * day)
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unlinked
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/follow-ups?org=all")

	for _, key := range []string{"owner", "plan", "admin"} {
		card := pageElement(t, page, "a", "follow-ups-group-"+key)
		followUpChip(t, card, map[string]string{"owner": "checkin-overdue", "plan": "unlinked", "admin": "ownerless"}[key])
		if strings.Contains(card, "<button") {
			t.Errorf("the %s card holds a button:\n%s", key, card)
		}
	}
}

// An open popover saying what a signal means fits a 390px phone: it is never
// wider than the window less a margin, scrolls rather than grows past its
// height, and the full list stacks each name over its meaning below 600px.
func TestFollowUpsSignalPopoversFitAPhoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/follow-ups")

	tip := attr(openTag(between(t, followUpChip(t, followUpRowOf(t, page, silent), "checkin-overdue"), "<button", "")), "popovertarget")
	for _, id := range []string{tip, "follow-ups-meanings"} {
		if pop := tagAround(t, page, ` id="`+id+`"`); !hasClass(pop, "rk-pop") {
			t.Errorf("#%s isn't an .rk-pop: %s", id, pop)
		}
	}
	rule := cssRule(t, page, ".rk-pop")
	for _, want := range []string{"box-sizing:border-box", "max-width:min(360px,calc(100vw - 32px))", "max-height:calc(100dvh - 32px)", "overflow:auto"} {
		if !strings.Contains(rule, want) {
			t.Errorf(".rk-pop{%s} lacks %s, so it can run off a phone's screen", rule, want)
		}
	}
	phone := between(t, page, "@media (max-width:600px){", "}}") + "}"
	if rule, _ := ruleFor(phone, ".rk-defs dl"); !strings.Contains(rule, "grid-template-columns:minmax(0,1fr)") {
		t.Errorf("at 600px .rk-defs dl{%s} keeps its names beside their meanings", rule)
	}
}
