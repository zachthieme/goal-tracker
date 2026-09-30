package web_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// riskSection returns the rows of the Risks page's section for one problem
// type, checking that its summary tile counts want Goals and links down to it,
// and that an empty section says so.
func riskSection(t *testing.T, page, anchor, tileID string, want int) string {
	t.Helper()
	tile := pageElement(t, page, "a", tileID)
	if !strings.Contains(tile, `href="#`+anchor+`"`) {
		t.Errorf("summary tile %s does not link to #%s: %s", tileID, anchor, tile)
	}
	if !strings.Contains(tile, `<span class="num">`+strconv.Itoa(want)+`</span>`) {
		t.Errorf("summary tile %s does not count %d: %s", tileID, want, tile)
	}
	section := pageElement(t, page, "section", "risks-"+anchor)
	if !strings.Contains(openTag(section), `id="`+anchor+`"`) {
		t.Errorf("section risks-%s has no #%s anchor: %s", anchor, anchor, openTag(section))
	}
	rows := strings.Index(section, "<tbody")
	if rows < 0 {
		if want > 0 || !strings.Contains(section, `data-testid="risks-`+anchor+`-empty"`) {
			t.Errorf("section risks-%s has neither rows nor its empty state:\n%s", anchor, section)
		}
		return ""
	}
	return section[rows:]
}

// The Risks page lists each Stale Goal in its own section, with its Owner and
// how long it has gone without a Check-in; the summary tile counts it and links
// down to the section. A fresh Goal isn't listed.
func TestRisksPageListsStaleGoals(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	stale := riskSection(t, page, "stale", "risks-tile-stale", 1)
	for _, want := range []string{navTo(silent.ID), "sam@example.com", "no Check-in for 10 days on a 7-day cadence"} {
		if !strings.Contains(stale, want) {
			t.Errorf("Stale section lacks %s:\n%s", want, stale)
		}
	}
	if strings.Contains(stale, navTo(fresh.ID)) {
		t.Errorf("Stale section lists %q, which checked in today:\n%s", fresh.Title, stale)
	}
}

// The Risks page lists each Goal still not Green past its Path to Green's
// target date, saying which date it missed.
func TestRisksPageListsOverduePathsToGreen(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))
	green := h.ActiveGoal(sam, "On track", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Clock.Advance(5 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	overdue := riskSection(t, page, "path-overdue", "risks-tile-path-overdue", 1)
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

	ownerless := riskSection(t, page, "ownerless", "risks-tile-ownerless", 1)
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
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	top := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Grow revenue", "It pays for everything."))
	aligned := h.ActiveChildOf(sam, top, "Ship search", "People can't find things.")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	unaligned := riskSection(t, page, "unaligned", "risks-tile-unaligned", 1)
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

	conflicts := riskSection(t, page, "schedule-conflicts", "risks-tile-schedule-conflicts", 1)
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

	halted := riskSection(t, page, "halted-parents", "risks-tile-halted-parents", 1)
	for _, want := range []string{navTo(underPaused.ID), "sam@example.com", navTo(paused.ID), "On Hold"} {
		if !strings.Contains(halted, want) {
			t.Errorf("Parent On Hold or Cancelled section lacks %s:\n%s", want, halted)
		}
	}
	if strings.Contains(halted, navTo(underLive.ID)) {
		t.Errorf("Parent On Hold or Cancelled section lists %q, whose parent is Active:\n%s", underLive.Title, halted)
	}
}

// With nothing flagged, every section of the Risks page says so, and every
// summary tile counts zero. The tiles and sections run in the same order.
func TestRisksPageShowsEmptyStates(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks")

	lastTile, lastSection := -1, -1
	for _, anchor := range []string{"stale", "path-overdue", "ownerless", "unaligned", "schedule-conflicts", "halted-parents"} {
		if rows := riskSection(t, page, anchor, "risks-tile-"+anchor, 0); rows != "" {
			t.Errorf("empty section %s has rows:\n%s", anchor, rows)
		}
		tile := strings.Index(page, `data-testid="risks-tile-`+anchor+`"`)
		section := strings.Index(page, `data-testid="risks-`+anchor+`"`)
		if tile < lastTile || section < lastSection {
			t.Errorf("%s is out of order among the tiles or sections", anchor)
		}
		lastTile, lastSection = tile, section
	}
}
