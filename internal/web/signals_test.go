package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin marks a Goal Top-level from its page and unmarks it again; the Goal
// page shows the mark. The Goal's Owner, not being an Admin, is refused.
func TestAdminMarksAndUnmarksTopLevelOverHTTP(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, g.ID)

	adaClient := signInClient(t, ts.URL, "ada@example.com")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	if page := getBody(t, adaClient, goalURL); !strings.Contains(page, `data-testid="mark-top-level"`) {
		t.Fatalf("Admin's Goal page offers no Top-level control; body:\n%s", page)
	}
	if page := getBody(t, samClient, goalURL); strings.Contains(page, `data-testid="mark-top-level"`) {
		t.Errorf("non-Admin's Goal page offers the Top-level control")
	}

	resp := postForm(t, samClient, goalURL+"/top-level", url.Values{"top_level": {"true"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Owner marking Top-level: status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	resp = postForm(t, adaClient, goalURL+"/top-level", url.Values{"top_level": {"true"}})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Admin marking Top-level: status %d; body:\n%s", resp.StatusCode, page)
	}
	if !strings.Contains(page, `data-testid="goal-top-level"`) {
		t.Errorf("Goal page does not show the Top-level mark; body:\n%s", page)
	}

	resp = postForm(t, adaClient, goalURL+"/top-level", url.Values{"top_level": {"false"}})
	page = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Admin unmarking Top-level: status %d; body:\n%s", resp.StatusCode, page)
	}
	if strings.Contains(page, `data-testid="goal-top-level"`) {
		t.Errorf("Goal page still shows the Top-level mark after unmarking; body:\n%s", page)
	}
}

// The Graph signals page lists the risks the graph flags on its own: Unaligned
// Goals, children due later than their parents, and children of a parent that
// is On Hold or Cancelled. The Goal list links to it so leadership can find it.
func TestGraphSignalsPageListsRisks(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	paused := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Paused outcome", "It mattered."))
	orphaned := h.ActiveChildOf(sam, paused, "Under paused", "Feeds paused.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:          paused.ID,
		AuthorID:        sam.ID,
		Status:          "Pausing.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}

	client := signInClient(t, ts.URL, "sam@example.com")
	if list := getBody(t, client, ts.URL+"/goals"); !strings.Contains(list, `href="/signals"`) {
		t.Errorf("Goal list does not link to the Graph signals page; body:\n%s", list)
	}
	page := getBody(t, client, ts.URL+"/signals")

	unaligned := pageElement(t, page, "ul", "unaligned-goals")
	if !strings.Contains(unaligned, navTo(loner.ID)) {
		t.Errorf("Unaligned list missing %q; section:\n%s", loner.Title, unaligned)
	}
	for _, g := range []domain.Goal{launch, late, paused, orphaned} {
		if strings.Contains(unaligned, navTo(g.ID)) {
			t.Errorf("Unaligned list includes %q, which is Top-level or aligned", g.Title)
		}
	}

	conflicts := pageElement(t, page, "ul", "schedule-conflicts")
	if !strings.Contains(conflicts, navTo(late.ID)) || !strings.Contains(conflicts, navTo(launch.ID)) {
		t.Errorf("schedule conflicts do not name both %q and %q; section:\n%s", late.Title, launch.Title, conflicts)
	}

	halted := pageElement(t, page, "ul", "halted-parents")
	if !strings.Contains(halted, navTo(orphaned.ID)) || !strings.Contains(halted, "On Hold") {
		t.Errorf("halted parents do not flag %q under an On Hold parent; section:\n%s", orphaned.Title, halted)
	}
}

// Each Goal's page flags the graph signals that touch it: a schedule conflict
// shows on both the child and the parent, a halted parent shows on the child,
// and an Unaligned Goal says so.
func TestGoalPageFlagsItsGraphSignals(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	june := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	launch := h.MarkTopLevel(ada, h.ActiveGoalDue(sam, "Launch", june))
	late := h.ActiveGoalDue(sam, "Late piece", june.AddDate(0, 1, 0))
	h.RequestLink(sam, late, launch, "")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:          launch.ID,
		AuthorID:        sam.ID,
		Status:          "Pausing.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}

	client := signInClient(t, ts.URL, "sam@example.com")
	pageOf := func(g domain.Goal) string {
		return getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))
	}

	childPage := pageOf(late)
	if !strings.Contains(childPage, `data-testid="goal-schedule-conflict"`) {
		t.Errorf("child page does not flag its schedule conflict; body:\n%s", childPage)
	}
	if !strings.Contains(childPage, `data-testid="goal-halted-parent"`) {
		t.Errorf("child page does not flag its On Hold parent; body:\n%s", childPage)
	}
	if strings.Contains(childPage, `data-testid="goal-unaligned"`) {
		t.Errorf("aligned child page is flagged Unaligned")
	}

	parentPage := pageOf(launch)
	if !strings.Contains(parentPage, `data-testid="goal-schedule-conflict"`) {
		t.Errorf("parent page does not flag the schedule conflict; body:\n%s", parentPage)
	}
	if strings.Contains(parentPage, `data-testid="goal-halted-parent"`) {
		t.Errorf("the On Hold parent itself is flagged as having a halted parent")
	}

	lonerPage := pageOf(loner)
	if !strings.Contains(lonerPage, `data-testid="goal-unaligned"`) {
		t.Errorf("Unaligned Goal's page does not say so; body:\n%s", lonerPage)
	}
	if !strings.Contains(lonerPage, `data-testid="goal-signals"`) {
		t.Errorf("Unaligned Goal's page has no signals section")
	}
	if strings.Contains(pageOf(h.ActiveChildOf(sam, loner, "Quiet child", "Fine.")), `data-testid="goal-signals"`) {
		t.Errorf("a Goal with no signals still shows a signals section")
	}
}

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
