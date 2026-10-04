package web_test

import (
	"html"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// riskSection returns the rows of the Risks page's section for one problem
// type.
func riskSection(t *testing.T, page, anchor string) string {
	t.Helper()
	section := pageElement(t, page, "section", "risks-"+anchor)
	if !strings.Contains(openTag(section), `id="`+anchor+`"`) {
		t.Errorf("section risks-%s has no #%s anchor: %s", anchor, anchor, openTag(section))
	}
	rows := strings.Index(section, "<tbody")
	if rows < 0 {
		t.Fatalf("section risks-%s has no rows:\n%s", anchor, section)
	}
	return section[rows:]
}

// The Risks page lists each Stale Goal in its own section, with its Owner and
// how long it has gone without a Check-in. A fresh Goal isn't listed.
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

	stale := riskSection(t, page, "stale")
	for _, want := range []string{navTo(silent.ID), "sam@example.com", "no Check-in for 10 days on a 7-day cadence"} {
		if !strings.Contains(stale, want) {
			t.Errorf("Stale section lacks %s:\n%s", want, stale)
		}
	}
	if strings.Contains(stale, navTo(fresh.ID)) {
		t.Errorf("Stale section lists %q, which checked in today:\n%s", fresh.Title, stale)
	}
}

// A Risks page section with Goals renders its heading, explanation and table;
// one without renders none of them. A single "Nothing in" line, after the
// sections, names the empty ones in the page's order, each carrying its
// section's anchor so a link to it still lands somewhere.
func TestRisksPageCollapsesEmptySections(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Silent work", "It matters.") // Stale and Unaligned
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	lastSection := 0
	for heading, explain := range map[string]string{
		"stale":     "Active Goals whose last Check-in",
		"unaligned": "Active Goals that contribute to no other Goal",
	} {
		section := pageElement(t, page, "section", "risks-"+heading)
		for _, want := range []string{"<h2>", explain, "<table>", "<tbody>"} {
			if !strings.Contains(section, want) {
				t.Errorf("section risks-%s lacks %s:\n%s", heading, want, section)
			}
		}
		lastSection = max(lastSection, strings.Index(page, `data-testid="risks-`+heading+`"`))
	}
	empty := []struct{ anchor, name string }{
		{"path-overdue", "Path to Green overdue"},
		{"ownerless", "Ownerless"},
		{"schedule-conflicts", "Schedule conflicts"},
		{"halted-parents", "Parent On Hold or Cancelled"},
	}
	for _, e := range empty {
		if strings.Contains(page, `data-testid="risks-`+e.anchor+`"`) {
			t.Errorf("empty section %s still renders", e.anchor)
		}
	}
	line := pageElement(t, page, "p", "risks-nothing-in")
	if !strings.HasPrefix(line[strings.Index(line, ">")+1:], "Nothing in: ") {
		t.Errorf("collapsed line does not open \"Nothing in: \": %s", line)
	}
	at := 0
	for _, e := range empty {
		i := strings.Index(line, `id="`+e.anchor+`"`)
		if i < 0 || i < at || !strings.HasPrefix(line[i+strings.Index(line[i:], ">")+1:], e.name+"<") {
			t.Errorf("Nothing in line lacks %s, anchored #%s, in order: %s", e.name, e.anchor, line)
		}
		at = i
	}
	for _, full := range []string{"stale", "unaligned"} {
		if strings.Contains(line, `id="`+full+`"`) {
			t.Errorf("Nothing in line names %s, which has Goals: %s", full, line)
		}
	}
	if strings.Count(page, `data-testid="risks-nothing-in"`) != 1 || strings.Index(page, `data-testid="risks-nothing-in"`) < lastSection {
		t.Errorf("the Nothing in line is not one line after the sections")
	}
	if strings.Contains(page, `data-testid="risks-all-clear"`) {
		t.Errorf("page is all clear with Goals flagged")
	}
}

// The Risks page lists each Goal still not Green past its Path to Green's
// target date, saying which date it missed.
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

	overdue := riskSection(t, page, "path-overdue")
	for _, want := range []string{navTo(stalled.ID), "sam@example.com", "meant to be back to Green by 2026-01-05"} {
		if !strings.Contains(overdue, want) {
			t.Errorf("Path to Green overdue section lacks %s:\n%s", want, overdue)
		}
	}
	if strings.Contains(overdue, navTo(green.ID)) {
		t.Errorf("Path to Green overdue section lists Green %q:\n%s", green.Title, overdue)
	}
}

// The Risks page lists each Active Goal whose Owner has left the org. A
// Proposed Goal of the same departed Owner isn't listed, nor is an Active Goal
// with a present Owner.
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

	ownerless := riskSection(t, page, "ownerless")
	for _, want := range []string{navTo(orphaned.ID), "sam@example.com", "has left the org"} {
		if !strings.Contains(ownerless, want) {
			t.Errorf("Ownerless section lacks %s:\n%s", want, ownerless)
		}
	}
	for _, g := range []domain.Goal{proposed, owned} {
		if strings.Contains(ownerless, navTo(g.ID)) {
			t.Errorf("Ownerless section lists %q:\n%s", g.Title, ownerless)
		}
	}
}

// The Risks page lists each Unaligned Goal. A Top-level Goal and a Goal that
// contributes to it aren't Unaligned.
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

	unaligned := riskSection(t, page, "unaligned")
	for _, want := range []string{navTo(loner.ID), "sam@example.com", "contributes to no other Goal"} {
		if !strings.Contains(unaligned, want) {
			t.Errorf("Unaligned section lacks %s:\n%s", want, unaligned)
		}
	}
	for _, g := range []domain.Goal{top, aligned} {
		if strings.Contains(unaligned, navTo(g.ID)) {
			t.Errorf("Unaligned section lists %q:\n%s", g.Title, unaligned)
		}
	}
}

// The Risks page lists each Goal due later than a Goal it contributes to,
// naming the parent and both delivery dates.
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	conflicts := riskSection(t, page, "schedule-conflicts")
	for _, want := range []string{navTo(late.ID), "sam@example.com", "2026-07-01", navTo(launch.ID), "2026-06-01"} {
		if !strings.Contains(conflicts, want) {
			t.Errorf("Schedule conflicts section lacks %s:\n%s", want, conflicts)
		}
	}
	if strings.Contains(conflicts, navTo(early.ID)) {
		t.Errorf("Schedule conflicts section lists %q, due before its parent:\n%s", early.Title, conflicts)
	}
}

// The Risks page lists each Goal contributing to a Goal that is On Hold or
// Cancelled, naming the parent and its Lifecycle.
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
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:          paused.ID,
		AuthorID:        sam.ID,
		Status:          "Pausing.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	halted := riskSection(t, page, "halted-parents")
	for _, want := range []string{navTo(underPaused.ID), "sam@example.com", navTo(paused.ID), "On Hold"} {
		if !strings.Contains(halted, want) {
			t.Errorf("Parent On Hold or Cancelled section lacks %s:\n%s", want, halted)
		}
	}
	if strings.Contains(halted, navTo(underLive.ID)) {
		t.Errorf("Parent On Hold or Cancelled section lists %q, whose parent is Active:\n%s", underLive.Title, halted)
	}
}

// With nothing flagged, the Risks page says so in one all-clear sentence
// instead of six empty sections, and that sentence still carries every
// section's anchor, in order. Each group card is muted, links nowhere, and
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
	clear := pageElement(t, page, "p", "risks-all-clear")
	if !strings.Contains(clear, "All clear") || strings.Count(clear, ".") != 1 {
		t.Errorf("all-clear is not one sentence: %s", clear)
	}
	if strings.Contains(page, `data-testid="risks-nothing-in"`) {
		t.Errorf("page has a Nothing in line as well as the all-clear sentence")
	}
	lastAnchor := -1
	for _, anchor := range []string{"stale", "path-overdue", "ownerless", "unaligned", "schedule-conflicts", "halted-parents"} {
		if strings.Contains(page, `data-testid="risks-`+anchor+`"`) {
			t.Errorf("empty section %s still renders", anchor)
		}
		anchorAt := strings.Index(clear, `id="`+anchor+`"`)
		if anchorAt < 0 {
			t.Errorf("all-clear sentence lacks the #%s anchor: %s", anchor, clear)
		}
		if anchorAt < lastAnchor {
			t.Errorf("%s is out of order in the all-clear sentence", anchor)
		}
		lastAnchor = anchorAt
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
// no script: the card is current, the cards still count every group, and Show
// all clears the filter. An unknown group shows everything.
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

	if stale := riskSection(t, page, "stale"); !strings.Contains(stale, navTo(silent.ID)) {
		t.Errorf("filtered to owner, the Stale section lacks %q:\n%s", silent.Title, stale)
	}
	for _, hidden := range []string{"unaligned", "ownerless"} {
		if strings.Contains(page, `data-testid="risks-`+hidden+`"`) {
			t.Errorf("filtered to owner, the page still shows the %s section", hidden)
		}
	}
	if strings.Contains(page, navTo(orphaned.ID)) {
		t.Errorf("filtered to owner, the page lists %q, flagged only outside it", orphaned.Title)
	}
	line := pageElement(t, page, "p", "risks-nothing-in")
	if !strings.Contains(line, `id="path-overdue"`) || strings.Contains(line, `id="halted-parents"`) {
		t.Errorf("filtered to owner, the Nothing in line doesn't name just the group's empty signals: %s", line)
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

	for _, shown := range []string{"stale", "unaligned", "ownerless"} {
		if !strings.Contains(page, `data-testid="risks-`+shown+`"`) {
			t.Errorf("an unknown group hides the %s section", shown)
		}
	}
	for _, key := range []string{"owner", "plan", "admin"} {
		if card := pageElement(t, page, "a", "risks-group-"+key); strings.Contains(openTag(card), "aria-current") {
			t.Errorf("an unknown group marks the %s card current: %s", key, openTag(card))
		}
	}
	if strings.Contains(page, `data-testid="risks-show-all"`) {
		t.Errorf("an unknown group offers Show all")
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
