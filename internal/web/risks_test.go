package web_test

import (
	"html"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// riskRows returns every row of the Risks page's table.
func riskRows(page string) []string {
	var rows []string
	for rest := page; ; {
		start := strings.Index(rest, `<tr data-testid="risk-row"`)
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

// riskRowOf returns the Risks page's one row for g, failing the test unless
// the table holds exactly one row linking to it.
func riskRowOf(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	var found []string
	for _, row := range riskRows(page) {
		if isRiskRowOf(row, g) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Risks table has %d rows for %q, want 1:\n%s", len(found), g.Title, page)
	}
	return found[0]
}

// hasRiskRow reports whether the Risks page's table has a row for g.
func hasRiskRow(page string, g domain.Goal) bool {
	return slices.ContainsFunc(riskRows(page), func(row string) bool { return isRiskRowOf(row, g) })
}

// isRiskRowOf reports whether a Risks table row is g's: its Goal cell links
// to g.
func isRiskRowOf(row string, g domain.Goal) bool {
	return strings.Contains(row, `<td data-testid="risk-goal">`+`<a `+navTo(g.ID)+`>`)
}

// riskChip returns a row's chip for one signal kind.
func riskChip(t *testing.T, row, kind string) string {
	t.Helper()
	start := strings.Index(row, `<span data-testid="risk-signal" data-kind="`+kind+`"`)
	if start < 0 {
		t.Fatalf("row has no %s chip:\n%s", kind, row)
	}
	end := strings.Index(row[start:], "</span>")
	if end < 0 {
		t.Fatalf("the %s chip is not closed:\n%s", kind, row)
	}
	return row[start : start+end]
}

// The Risks page lists each Stale Goal once, with its Owner, "You" for the
// viewer's own, and a Stale chip saying how long it has gone without a
// Check-in against its cadence. A fresh Goal isn't Stale.
func TestRisksPageListsStaleGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	row := riskRowOf(t, page, silent)
	if chip := riskChip(t, row, "stale"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Stale · 10d on 7d") {
		t.Errorf("Stale chip is not a Stale badge saying 10d on 7d: %s", chip)
	}
	if owner := pageElement(t, row, "td", "risk-owner"); !strings.HasSuffix(owner, ">You") || strings.Contains(owner, "sam@example.com") {
		t.Errorf("the viewer's own Goal isn't Owned by You: %s", owner)
	}
	if row := riskRowOf(t, page, fresh); strings.Contains(row, `data-kind="stale"`) {
		t.Errorf("%q, which checked in today, is Stale:\n%s", fresh.Title, row)
	}
}

// The Risks page lists each Goal still not Green past its Path to Green's
// target date with its Health and a chip saying how many days overdue it is.
// A Green Goal isn't overdue.
func TestRisksPageListsOverduePathsToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	green := h.ActiveGoal(sam, "On track", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Clock.Advance(5 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	row := riskRowOf(t, page, stalled)
	if chip := riskChip(t, row, "path-overdue"); !strings.Contains(openTag(chip), `class="badge st"`) || !strings.HasSuffix(chip, ">Path to Green 2d overdue") {
		t.Errorf("overdue chip is not a Stale-colored badge saying 2d overdue: %s", chip)
	}
	if health := pageElement(t, row, "span", "risk-health"); !strings.Contains(openTag(health), `class="badge r"`) {
		t.Errorf("row doesn't show the Goal's Red Health: %s", health)
	}
	if row := riskRowOf(t, page, green); strings.Contains(row, `data-kind="path-overdue"`) {
		t.Errorf("Green %q is overdue:\n%s", green.Title, row)
	}
}

// The Risks page lists each Active Goal whose Owner has left the org with an
// Ownerless chip as prominent as Red, and the departed Owner, who isn't the
// viewer. A Proposed Goal of the same departed Owner isn't listed, nor is an
// Active Goal with a present Owner Ownerless.
func TestRisksPageListsOwnerlessGoals(t *testing.T) {
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

	page := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/risks")

	row := riskRowOf(t, page, orphaned)
	if chip := riskChip(t, row, "ownerless"); !strings.Contains(openTag(chip), `class="badge ol"`) || !strings.HasSuffix(chip, ">Ownerless") {
		t.Errorf("Ownerless chip is not an Ownerless badge: %s", chip)
	}
	if owner := pageElement(t, row, "td", "risk-owner"); !strings.Contains(owner, `title="sam@example.com"`) || strings.Contains(owner, "You") {
		t.Errorf("row doesn't show the departed Owner: %s", owner)
	}
	if hasRiskRow(page, proposed) {
		t.Errorf("the Risks table lists Proposed %q", proposed.Title)
	}
	if row := riskRowOf(t, page, owned); strings.Contains(row, `data-kind="ownerless"`) {
		t.Errorf("%q, whose Owner is present, is Ownerless:\n%s", owned.Title, row)
	}
}

// The Risks page chips each Unaligned Goal with an outlined chip saying why.
// A Top-level Goal and a Goal that contributes to it aren't Unaligned.
func TestRisksPageListsUnalignedGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	aligned := h.ActiveChildOf(sam, top, "Ship search", "People can't find things.")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	row := riskRowOf(t, page, loner)
	if chip := riskChip(t, row, "unaligned"); !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.HasSuffix(chip, ">Unaligned · no parent Goal, not Top-level") {
		t.Errorf("Unaligned chip is not an outlined chip saying why: %s", chip)
	}
	rule, _, _ := strings.Cut(page[strings.Index(page, ".rk-tag{")+1:], "}")
	if !strings.Contains(rule, "border-radius:var(--radius-sm)") || !strings.Contains(rule, "border:1px solid var(--color-border-strong)") {
		t.Errorf("the outlined chip isn't outlined with the small radius: %s", rule)
	}
	for _, g := range []domain.Goal{top, aligned} {
		if hasRiskRow(page, g) {
			t.Errorf("the Risks table lists %q", g.Title)
		}
	}
}

// The Risks page chips each Goal due later than a Goal it contributes to with
// its delivery date, how many days after its parent's that is, and a link to
// the parent. A date in another year carries its year.
func TestRisksPageListsScheduleConflicts(t *testing.T) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	for _, c := range []struct {
		child, parent domain.Goal
		says          string
	}{
		{late, launch, ">Schedule conflict · due Jul 1, 30 days after its parent <a "},
		{slipped, yearEnd, ">Schedule conflict · due Jan 10, 2027, 21 days after its parent <a "},
	} {
		chip := riskChip(t, riskRowOf(t, page, c.child), "schedule-conflicts")
		if !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.Contains(chip, c.says) {
			t.Errorf("%q's chip is not an outlined chip saying %q: %s", c.child.Title, c.says, chip)
		}
		if !strings.HasSuffix(chip, navTo(c.parent.ID)+">"+html.EscapeString(c.parent.Title)+"</a>") {
			t.Errorf("%q's chip doesn't link to its parent %q: %s", c.child.Title, c.parent.Title, chip)
		}
	}
	if hasRiskRow(page, early) && strings.Contains(riskRowOf(t, page, early), `data-kind="schedule-conflicts"`) {
		t.Errorf("%q, due before its parent, is a schedule conflict", early.Title)
	}
}

// The Risks page chips each Goal contributing to a Goal that is On Hold or
// Cancelled with its parent's Lifecycle and a link to the parent. A Goal under
// an Active parent isn't chipped.
func TestRisksPageListsGoalsUnderHaltedParents(t *testing.T) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	for _, c := range []struct {
		child, parent domain.Goal
		says          string
	}{
		{underPaused, paused, ">Parent On Hold · <a "},
		{underDropped, dropped, ">Parent Cancelled · <a "},
	} {
		chip := riskChip(t, riskRowOf(t, page, c.child), "halted-parents")
		if !strings.Contains(openTag(chip), `class="rk-tag"`) || !strings.Contains(chip, c.says) {
			t.Errorf("%q's chip is not an outlined chip saying %q: %s", c.child.Title, c.says, chip)
		}
		if !strings.HasSuffix(chip, navTo(c.parent.ID)+">"+c.parent.Title+"</a>") {
			t.Errorf("%q's chip doesn't link to its parent %q: %s", c.child.Title, c.parent.Title, chip)
		}
	}
	if hasRiskRow(page, underLive) {
		t.Errorf("the Risks table lists %q, whose parent is Active", underLive.Title)
	}
}

// The Risks page lists a Goal flagged by two signals once, in one table
// inside a card that scrolls sideways on a narrow screen: one row with both
// chips, a neutral dash for no Health, and one Fix: Check in for its Owner,
// though it is Unaligned too.
func TestRisksPageListsEachFlaggedGoalOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	if n := strings.Count(page, `<table id="risks-table"`); n != 1 {
		t.Fatalf("page has %d risks tables, want 1", n)
	}
	if !strings.Contains(page, `<div class="card"><table id="risks-table"`) {
		t.Errorf("the risks table doesn't sit straight inside a card")
	}
	if rows := riskRows(page); len(rows) != 1 {
		t.Fatalf("the Risks table has %d rows, want 1", len(rows))
	}
	row := riskRowOf(t, page, silent)
	if n := strings.Count(row, `data-testid="risk-signal"`); n != 2 {
		t.Errorf("row has %d chips, want 2:\n%s", n, row)
	}
	for _, kind := range []string{"stale", "unaligned"} {
		riskChip(t, row, kind)
	}
	if health := row[strings.Index(row, `data-testid="risk-health"`):strings.Index(row, `data-testid="risk-goal"`)]; !strings.Contains(health, `class="badge lc"`) || !strings.Contains(health, "—") {
		t.Errorf("row doesn't show a neutral dash for no Health: %s", health)
	}
	if label, href, _ := riskFix(t, row); label != "Check in" || href != "/goals/"+strconv.FormatInt(silent.ID, 10)+"/checkin" {
		t.Errorf("the Owner's Stale and Unaligned Goal's Fix is %q → %s, want Check in", label, href)
	}
	for _, gone := range []string{"risks-nothing-in", `class="rk-empty"`, `data-testid="risks-stale"`, "Why it's flagged"} {
		if strings.Contains(page, gone) {
			t.Errorf("page still has %s", gone)
		}
	}
}

// With nothing flagged, the Risks page says so in one all-clear sentence
// instead of an empty table. Each group card is muted, links nowhere, and
// says why it is empty.
func TestRisksPageIsAllClearWithNothingFlagged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	if n := strings.Count(page, `data-testid="risks-all-clear"`); n != 1 {
		t.Fatalf("page has %d all-clear sentences, want 1", n)
	}
	if clear := pageElement(t, page, "p", "risks-all-clear"); !strings.HasSuffix(clear, ">All clear: nothing needs attention.") {
		t.Errorf("all-clear doesn't say nothing needs attention: %s", clear)
	}
	if strings.Contains(page, "<table") {
		t.Errorf("page renders a table with nothing flagged")
	}
	if head := pageElement(t, page, "p", "risks-attention"); !strings.Contains(head, "<strong>0</strong> Goals need attention.") {
		t.Errorf("header does not count 0 Goals: %s", head)
	}
	for key, line := range map[string]string{
		"owner": "Every Owner is up to date.",
		"plan":  "Every plan fits.",
		"admin": "Nothing needs an Admin.",
	} {
		if strings.Contains(page, `<a data-testid="risks-group-`+key+`"`) {
			t.Errorf("the empty %s card is a link", key)
		}
		card := pageElement(t, page, "div", "risks-group-"+key)
		if !strings.Contains(openTag(card), "rk-zero") || !strings.Contains(card, line) || strings.Contains(card, "href") {
			t.Errorf("the empty %s card isn't muted, saying %q, and linking nowhere: %s", key, line, card)
		}
	}
	if !strings.Contains(page, ".rk-zero{border:1px dashed var(--color-border-strong);color:var(--color-ink-muted)}") {
		t.Errorf("an empty card doesn't take a dashed edge and muted text")
	}
	if strings.Contains(page, "risks-tile-") {
		t.Errorf("the page still has the per-signal summary tiles")
	}
	if risks := pageElement(t, page, "a", "nav-risks"); strings.Contains(risks, "count") {
		t.Errorf("Risks shows a count with nothing flagged: %s", risks)
	}
}

// The Risks page heads with how many Goals need attention, each once, then a
// card per group of signals, by who acts: each counts the Goals it holds once,
// chips each of its signals with how many Goals it flags, and links to the
// page filtered to the group.
func TestRisksPageSummarizesGroupsByWhoActs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unaligned
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	if head := pageElement(t, page, "p", "risks-attention"); !strings.Contains(head, "<strong>3</strong> Goals need attention.") {
		t.Errorf("header does not count the 3 flagged Goals: %s", head)
	}
	last := -1
	for _, g := range []struct {
		key, name string
		count     int
		chips     []string
	}{
		{"owner", "Owner needs to update", 1, []string{"Stale · 1"}},
		{"plan", "Plan doesn't fit", 3, []string{"Unaligned · 2", "Schedule conflicts · 1"}},
		{"admin", "Needs an Admin", 1, []string{"Ownerless · 1"}},
	} {
		card := pageElement(t, page, "a", "risks-group-"+g.key)
		for _, want := range append([]string{
			`href="/risks?group=` + g.key + `"`,
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
		at := strings.Index(page, `data-testid="risks-group-`+g.key+`"`)
		if at < last {
			t.Errorf("%s card is out of order", g.key)
		}
		last = at
	}
	if strings.Contains(page, "Show all") {
		t.Errorf("page offers Show all with no filter")
	}
}

// A group card's address filters the page to the signals in that group, with
// no script: the table lists only the Goals they flag, chipped with them
// alone, the card is current, the cards still count every group, and Show all
// clears the filter. An unknown group shows everything.
func TestRisksPageFiltersToAGroup(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)
	orphaned := h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unaligned
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/risks?group=owner")

	row := riskRowOf(t, page, silent)
	riskChip(t, row, "stale")
	if strings.Contains(row, `data-kind="unaligned"`) {
		t.Errorf("filtered to owner, %q still has its Unaligned chip:\n%s", silent.Title, row)
	}
	if hasRiskRow(page, orphaned) {
		t.Errorf("filtered to owner, the page lists %q, flagged only outside it", orphaned.Title)
	}
	if head := pageElement(t, page, "p", "risks-attention"); !strings.Contains(head, "<strong>2</strong>") {
		t.Errorf("filtered to owner, the header doesn't still count both flagged Goals: %s", head)
	}
	for key, want := range map[string]string{"owner": "1", "plan": "2", "admin": "1"} {
		card := pageElement(t, page, "a", "risks-group-"+key)
		if !strings.Contains(card, `<span class="num">`+want+`</span>`) {
			t.Errorf("filtered to owner, the %s card doesn't count %s: %s", key, want, card)
		}
		if current := strings.Contains(openTag(card), `aria-current="page"`); current != (key == "owner") {
			t.Errorf("filtered to owner, the %s card's aria-current is %v: %s", key, current, openTag(card))
		}
	}
	if all := pageElement(t, page, "a", "risks-show-all"); !strings.Contains(all, `href="/risks"`) || !strings.Contains(all, "Show all") {
		t.Errorf("Show all doesn't clear the filter: %s", all)
	}

	page = getBody(t, client, ts.URL+"/risks?group=bogus")

	riskChip(t, riskRowOf(t, page, silent), "unaligned")
	riskChip(t, riskRowOf(t, page, orphaned), "ownerless")
	for _, key := range []string{"owner", "plan", "admin"} {
		if card := pageElement(t, page, "a", "risks-group-"+key); strings.Contains(openTag(card), "aria-current") {
			t.Errorf("an unknown group marks the %s card current: %s", key, openTag(card))
		}
	}
	if strings.Contains(page, `data-testid="risks-show-all"`) {
		t.Errorf("an unknown group offers Show all")
	}
}

// Filtered to a group with no Goals while others have some, the Risks page
// says what the group's card says instead of an empty table, and isn't all
// clear.
func TestRisksPageFilteredToAnEmptyGroupSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks?group=admin")

	if line := pageElement(t, page, "p", "risks-group-blank"); !strings.HasSuffix(line, ">Nothing needs an Admin.") {
		t.Errorf("filtered to an empty group, the page doesn't say so: %s", line)
	}
	for _, gone := range []string{"<table", `data-testid="risks-all-clear"`} {
		if strings.Contains(page, gone) {
			t.Errorf("filtered to an empty group, the page has %s", gone)
		}
	}
}

// Every page's top bar has Risks after Goals, counting the Goals the Risks page
// flags, each once however many sections list it, for everyone. The nav doesn't
// link to the Graph or Freshness signals pages it replaces.
func TestNavCountsFlaggedGoalsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)
	h.Checkin(sam, launch.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveGoal(kim, "Orphaned work", "It matters.") // Ownerless and Unaligned
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0)) // Schedule conflict
	h.RequestLink(sam, late, launch, "")

	for _, who := range []string{"sam@example.com", "ada@example.com"} {
		client := signInClient(t, ts.URL, who)
		for _, path := range []string{"/home", "/goals", "/risks"} {
			page := getBody(t, client, ts.URL+path)
			nav := page[strings.Index(page, "<nav"):strings.Index(page, "</nav>")]
			risks := pageElement(t, page, "a", "nav-risks")
			if !strings.Contains(risks, `href="/risks"`) || !strings.Contains(risks, `<span class="count">3</span>`) {
				t.Errorf("%s on %s: Risks does not count the 3 flagged Goals: %s", who, path, risks)
			}
			at := strings.Index(nav, `data-testid="nav-risks"`)
			for _, before := range []string{"nav-home", "nav-goals"} {
				if i := strings.Index(nav, `data-testid="`+before+`"`); i > at {
					t.Errorf("%s on %s: Risks comes before %s:\n%s", who, path, before, nav)
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

// A Goal's page shows its freshness and graph signals as alert banners, Stale
// in its own color, and the Goal list marks a Stale or overdue row with a chip
// in the same color saying how long. The data-testids other tests find them by
// don't change.
func TestRiskFlagsUseAlertBannersAndChips(t *testing.T) {
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
		"goal-stale":             `class="alert st"`,
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
	if el := pageElement(t, parentPage, "p", "goal-unaligned"); !strings.Contains(openTag(el), `class="alert lc"`) {
		t.Errorf("Unaligned banner is not an alert: %s", openTag(el))
	}
	stalledPage := getBody(t, client, goalPageURL(ts.URL, stalled))
	if el := pageElement(t, stalledPage, "p", "goal-path-overdue"); !strings.Contains(openTag(el), `class="alert st"`) {
		t.Errorf("Path to Green overdue banner is not an alert: %s", openTag(el))
	}

	list := getBody(t, client, ts.URL+"/goals")
	if chip := pageElement(t, list, "span", "stale"); !strings.Contains(chip, `class="badge st"`) || !strings.Contains(chip, "Stale · 16 days") {
		t.Errorf("Stale row chip is not a Stale badge saying how long: %s", chip)
	}
	if chip := pageElement(t, list, "span", "path-overdue"); !strings.Contains(chip, `class="badge st"`) || !strings.Contains(chip, "Path to Green overdue") {
		t.Errorf("overdue row chip is not a Stale-colored badge: %s", chip)
	}

	adaPage := getBody(t, signInClient(t, ts.URL, "ada@example.com"), goalPageURL(ts.URL, silent)+"?open=top-level")
	if control := pageElement(t, adaPage, "section", "mark-top-level"); !strings.Contains(control, `<button type="submit" class="btn" `) {
		t.Errorf("Top-level control is not a plain button: %s", control)
	}
}

// goalPageURL is the address of g's page on the server at base.
func goalPageURL(base string, g domain.Goal) string {
	return base + "/goals/" + strconv.FormatInt(g.ID, 10)
}

// riskFix returns a row's one Fix: its label, its address, and whether it is
// the primary button, failing the test unless the fix cell holds exactly one
// link.
func riskFix(t *testing.T, row string) (label, href string, primary bool) {
	t.Helper()
	cell := pageElement(t, row, "td", "risk-fix")
	if n := strings.Count(cell, "<a "); n != 1 {
		t.Fatalf("fix cell has %d links, want 1: %s", n, cell)
	}
	link := cell[strings.Index(cell, "<a "):]
	tag := openTag(link)
	_, rest, _ := strings.Cut(tag, `href="`)
	href, _, _ = strings.Cut(rest, `"`)
	label, _, _ = strings.Cut(link[len(tag)+1:], "</a>")
	return label, html.UnescapeString(href), strings.Contains(tag, "primary")
}

// A stale Goal's Fix is Check in, the primary button, for its Owner and its
// Delegates, who can check in; anyone else gets Open Goal.
func TestRiskFixOnAStaleGoalIsCheckInForWhoCanCheckIn(t *testing.T) {
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
		{"kim@example.com", "Open Goal", "/goals/" + strconv.FormatInt(silent.ID, 10), false},
		{"ada@example.com", "Open Goal", "/goals/" + strconv.FormatInt(silent.ID, 10), false},
	} {
		page := getBody(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/risks")
		label, href, primary := riskFix(t, riskRowOf(t, page, silent))
		if label != c.label || href != c.href || primary != c.primary {
			t.Errorf("%s sees Fix %q → %s (primary %v), want %q → %s (primary %v)", c.viewer, label, href, primary, c.label, c.href, c.primary)
		}
	}
}

// An Ownerless Goal's Fix for an Admin is Reassign, opening the Goal page's
// reassign form; a Delegate on it, who can still check in, gets Check in while
// it is Stale.
func TestRiskFixOnAnOwnerlessGoalIsReassignForAnAdmin(t *testing.T) {
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
		page := getBody(t, client, ts.URL+"/risks")
		row := riskRowOf(t, page, orphaned)
		riskChip(t, row, "ownerless")
		riskChip(t, row, "stale")
		label, href, _ := riskFix(t, row)
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Reassign" && !strings.Contains(getBody(t, client, ts.URL+href), `data-testid="reassign-goal"`) {
			t.Errorf("Reassign doesn't open the reassign form")
		}
	}
}

// An Unaligned Goal's Fix for its Owner is Link to a parent, opening the Goal
// page's parent-link form; a Delegate, who can't link it, gets Open Goal.
func TestRiskFixOnAnUnalignedGoalIsLinkToAParentForItsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	h.AddDelegate(sam, dee, loner.ID)

	goal := "/goals/" + strconv.FormatInt(loner.ID, 10)
	for _, c := range []struct {
		viewer, label, href string
	}{
		{"sam@example.com", "Link to a parent", goal + "?open=parent-link"},
		{"dee@example.com", "Open Goal", goal},
	} {
		client := signInClient(t, ts.URL, c.viewer)
		page := getBody(t, client, ts.URL+"/risks")
		label, href, _ := riskFix(t, riskRowOf(t, page, loner))
		if label != c.label || href != c.href {
			t.Fatalf("%s sees Fix %q → %s, want %q → %s", c.viewer, label, href, c.label, c.href)
		}
		if label == "Link to a parent" && !strings.Contains(getBody(t, client, ts.URL+href), `data-testid="request-link"`) {
			t.Errorf("Link to a parent doesn't open the parent-link form")
		}
	}
}
