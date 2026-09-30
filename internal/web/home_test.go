package web_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const day = 24 * time.Hour

// setCadence sets how often a Check-in is expected on g, failing the test on
// error.
func setCadence(t *testing.T, h *testsupport.Harness, g domain.Goal, days int) {
	t.Helper()
	if _, err := h.Service.SetCadence(context.Background(), g.ID, days); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
}

// homeRow returns the row of a Home section that links to g, failing the test
// when the section has none.
func homeRow(t *testing.T, section string, g domain.Goal) string {
	t.Helper()
	at := strings.Index(section, navTo(g.ID))
	if at < 0 {
		t.Fatalf("section has no row for %q:\n%s", g.Title, section)
	}
	start := strings.LastIndex(section[:at], "<li")
	end := strings.Index(section[at:], "</li>")
	return section[start : at+end]
}

// A Stale Goal the viewer Owns is listed under "Check in on these", saying how
// long since its last Check-in against its cadence, flagged Stale, with a
// one-click No change and a Check in link. Fresh Goals and other people's
// Stale Goals aren't listed.
func TestHomeListsStaleGoalToCheckInOn(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	stale := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, stale, 14)
	h.Checkin(sam, stale.ID, domain.HealthYellow, "Slipping.", "Cut scope.", testsupport.Epoch.AddDate(0, 1, 0))
	others := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	h.Clock.Advance(16 * day)
	fresh := h.ActiveGoal(sam, "Fresh work", "It matters.")

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := pageElement(t, page, "ul", "home-due")
	row := homeRow(t, due, stale)
	for _, want := range []string{
		"Last check-in 16 days ago on a 14-day cadence",
		`data-testid="home-due-stale"`,
		`data-testid="home-due-health"`,
		fmt.Sprintf(`action="/goals/%d/checkins/no-change"`, stale.ID),
		`data-testid="home-due-checkin"`,
	} {
		if !strings.Contains(row, want) {
			t.Errorf("Stale Goal's row lacks %s:\n%s", want, row)
		}
	}
	for _, g := range []domain.Goal{fresh, others} {
		if strings.Contains(due, navTo(g.ID)) {
			t.Errorf("Check in on these lists %q, which isn't due for sam:\n%s", g.Title, due)
		}
	}
}

// A Goal that would go Stale before next week's reminder is listed too, by the
// same rule as the reminder email, but not flagged Stale. A Goal with a week
// to spare isn't listed, and a Goal with no Check-in to repeat offers no No
// change.
func TestHomeListsGoalDueBeforeNextReminder(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dueSoon := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	setCadence(t, h, dueSoon, 14)
	h.Checkin(sam, dueSoon.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(6 * day)
	spare := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	setCadence(t, h, spare, 14)
	h.Checkin(sam, spare.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(2 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	due := pageElement(t, page, "ul", "home-due")
	row := homeRow(t, due, dueSoon)
	if !strings.Contains(row, "Last check-in 8 days ago on a 14-day cadence") {
		t.Errorf("due-soon Goal's row does not say why it's listed:\n%s", row)
	}
	if strings.Contains(row, `data-testid="home-due-stale"`) {
		t.Errorf("due-soon Goal is flagged Stale before it is:\n%s", row)
	}
	if strings.Contains(due, navTo(spare.ID)) {
		t.Errorf("Check in on these lists %q, which has a week to spare:\n%s", spare.Title, due)
	}
}

// A Goal never checked in on has no previous Check-in to repeat, so its row
// offers only Check in, not No change.
func TestHomeOffersNoChangeOnlyWithAPreviousCheckin(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")

	row := homeRow(t, pageElement(t, page, "ul", "home-due"), silent)
	if strings.Contains(row, "no-change") {
		t.Errorf("Goal with no Check-in offers No change:\n%s", row)
	}
	if !strings.Contains(row, `data-testid="home-due-checkin"`) {
		t.Errorf("Goal with no Check-in offers no Check in link:\n%s", row)
	}
}

// A Delegate checks in for the Owner, so a due Goal they're a Delegate on is
// listed to check in on, saying whose it is, and the sidebar lists the Goals
// delegated to them. Someone with none delegated sees no such section.
func TestHomeListsDelegatedGoals(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "dee@example.com"), ts.URL+"/home")

	row := homeRow(t, pageElement(t, page, "ul", "home-due"), g)
	if !strings.Contains(row, "Delegated to you by sam@example.com") {
		t.Errorf("delegated Goal's row does not say whose it is:\n%s", row)
	}
	delegated := pageElement(t, page, "section", "home-delegated")
	if !strings.Contains(delegated, navTo(g.ID)) || !strings.Contains(delegated, `href="/delegates"`) {
		t.Errorf("sidebar does not list the delegated Goal and link to /delegates:\n%s", delegated)
	}

	samPage := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/home")
	if strings.Contains(samPage, `data-testid="home-delegated"`) {
		t.Errorf("sam has nothing delegated but sees a Delegated to you section")
	}
}
