package web_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// riskSection returns the Risks page's section for one problem type, checking
// that its summary tile counts want Goals and links down to it.
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
	return section
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
