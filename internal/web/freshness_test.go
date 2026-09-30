package web_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Freshness signals page is the Stale list: every Stale Goal with how long
// since its last update, and every Goal whose Path to Green is overdue. The
// Goal list links to it, and fresh Goals aren't listed.
func TestFreshnessPageListsStaleGoalsAndOverduePaths(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	if list := getBody(t, client, ts.URL+"/goals"); !strings.Contains(list, `href="/freshness"`) {
		t.Errorf("Goal list does not link to the Freshness signals page; body:\n%s", list)
	}
	page := getBody(t, client, ts.URL+"/freshness")

	stale := pageElement(t, page, "ul", "stale-goals")
	if !strings.Contains(stale, navTo(silent.ID)) || !strings.Contains(stale, "10 days") {
		t.Errorf("Stale list does not show %q silent for 10 days; section:\n%s", silent.Title, stale)
	}
	for _, g := range []domain.Goal{fresh, stalled} {
		if strings.Contains(stale, navTo(g.ID)) {
			t.Errorf("Stale list includes %q, which checked in today", g.Title)
		}
	}

	overdue := pageElement(t, page, "ul", "overdue-paths")
	if !strings.Contains(overdue, navTo(stalled.ID)) || !strings.Contains(overdue, "2026-01-05") {
		t.Errorf("overdue Paths to Green do not show %q due back at Green by 2026-01-05; section:\n%s", stalled.Title, overdue)
	}
	if strings.Contains(overdue, navTo(silent.ID)) || strings.Contains(overdue, navTo(fresh.ID)) {
		t.Errorf("overdue Paths to Green list a Goal with no Path to Green; section:\n%s", overdue)
	}
}

// A Goal's page flags it when it is Stale or its Path to Green is overdue, and
// beside the Rolled-up Health says how many Active children are Stale without
// changing the color. A fresh Goal shows no freshness flag.
func TestGoalPageFlagsFreshness(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Org outcome", "It matters.")
	h.ActiveChildOf(sam, parent, "Silent child", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveChildOf(sam, parent, "Fresh child", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthYellow, "Slipping.", "Cut scope.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	pageOf := func(g domain.Goal) string {
		return getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))
	}

	parentPage := pageOf(parent)
	if !strings.Contains(parentPage, `data-testid="goal-stale"`) {
		t.Errorf("Stale Goal's page does not flag it; body:\n%s", parentPage)
	}
	if !strings.Contains(parentPage, `data-testid="goal-rollup-health">Green`) {
		t.Errorf("Stale children changed the Rolled-up Health color; body:\n%s", parentPage)
	}
	if !strings.Contains(parentPage, `data-testid="goal-rollup-stale">1 of 2 Stale`) {
		t.Errorf("Rolled-up Health does not say 1 of 2 children are Stale; body:\n%s", parentPage)
	}

	stalledPage := pageOf(stalled)
	if !strings.Contains(stalledPage, `data-testid="goal-path-overdue"`) || !strings.Contains(stalledPage, "2026-01-05") {
		t.Errorf("page of a Goal past its Path to Green target does not flag it; body:\n%s", stalledPage)
	}

	freshPage := pageOf(fresh)
	if strings.Contains(freshPage, `data-testid="goal-stale"`) || strings.Contains(freshPage, `data-testid="goal-path-overdue"`) {
		t.Errorf("fresh Green Goal's page carries a freshness flag")
	}
}

// The Goal list marks a Stale Goal and a Goal whose Path to Green is overdue on
// its own row, as it marks an Ownerless one, so neither hides until someone
// opens the Goal. A fresh Goal's row carries no mark.
func TestGoalListMarksStaleAndOverdueGoals(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * 24 * time.Hour)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC))

	client := signInClient(t, ts.URL, "sam@example.com")
	list := getBody(t, client, ts.URL+"/goals")
	row := func(g domain.Goal) string {
		at := strings.Index(list, navTo(g.ID))
		if at < 0 {
			t.Fatalf("Goal list has no row for %q; body:\n%s", g.Title, list)
		}
		start := strings.LastIndex(list[:at], "<tr")
		end := strings.Index(list[at:], "</tr>")
		return list[start : at+end]
	}

	if r := row(silent); !strings.Contains(r, `data-testid="stale"`) {
		t.Errorf("Stale Goal's row is not marked Stale; row:\n%s", r)
	}
	if r := row(stalled); !strings.Contains(r, `data-testid="path-overdue"`) || strings.Contains(r, `data-testid="stale"`) {
		t.Errorf("row of a Goal past its Path to Green target is not marked overdue (and only that); row:\n%s", r)
	}
	if r := row(fresh); strings.Contains(r, `data-testid="stale"`) || strings.Contains(r, `data-testid="path-overdue"`) {
		t.Errorf("fresh Goal's row carries a freshness mark; row:\n%s", r)
	}
}
