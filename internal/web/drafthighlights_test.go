package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Owner and a Delegate see the Goal's pending Draft Highlights on its
// page, oldest first, each with its kind, note, who logged it and when;
// anyone else sees no Draft Highlights block at all.
func TestGoalPageShowsPendingDraftHighlightsOnlyToOwnerAndDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignInNamed("sam@example.com", "Sam Rivera")
	dee := h.SignInNamed("dee@example.com", "Dee Okafor")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	h.LogDraftHighlight(sam, goal.ID, domain.HighlightAccomplishment, "Cut paging noise by half.")
	h.Clock.Advance(26 * time.Hour)
	h.LogDraftHighlight(dee, goal.ID, "", "Vendor may raise prices.")

	want := []string{
		"Accomplishment Cut paging noise by half. Sam Rivera sam@example.com · Fri 2 Jan 15:04 Delete",
		"No kind Vendor may raise prices. Dee Okafor dee@example.com · Sat 3 Jan 17:04 Delete",
	}
	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		page := getBody(t, signInClient(t, ts.URL, who), goalPageURL(ts.URL, goal))
		if got := elementTexts(page, "li", "draft-highlight"); !slices.Equal(got, want) {
			t.Errorf("%s sees Draft Highlights %q, want %q", who, got, want)
		}
		if !strings.Contains(page, `data-testid="log-draft-highlight"`) {
			t.Errorf("%s isn't offered the form to log one", who)
		}
	}

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), goalPageURL(ts.URL, goal))
	for _, leak := range []string{`data-testid="goal-draft-highlights"`, `data-testid="log-draft-highlight"`, "Cut paging noise", "Vendor may raise"} {
		if strings.Contains(page, leak) {
			t.Errorf("someone else's Goal page shows %s", leak)
		}
	}
}

// The Owner logs a Draft Highlight with a kind and a Delegate one without,
// each landing back on the Goal page; a blank note is refused there, saying
// why, and logs nothing.
func TestLogDraftHighlightFromTheGoalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	logURL := fmt.Sprintf("%s/goals/%d/draft-highlights", ts.URL, goal.ID)

	for _, tc := range []struct{ who, kind, note string }{
		{"sam@example.com", domain.HighlightInsight, "Retries mask the root cause."},
		{"dee@example.com", "", "Vendor may raise prices."},
	} {
		resp := postForm(t, noRedirects(signInClient(t, ts.URL, tc.who)), logURL, url.Values{"kind": {tc.kind}, "note": {tc.note}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != fmt.Sprintf("/goals/%d", goal.ID) {
			t.Errorf("%s logging: status %d to %q, want 303 to the Goal page", tc.who, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); !slices.Equal(got, []string{"Retries mask the root cause.", "Vendor may raise prices."}) {
		t.Errorf("pending = %q, want both logged", got)
	}

	resp := postForm(t, signInClient(t, ts.URL, "sam@example.com"), logURL, url.Values{"kind": {domain.HighlightMiss}, "note": {"  "}})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("blank note: status %d, want 422", resp.StatusCode)
	}
	section := between(t, page, `data-testid="goal-draft-highlights"`, "</section>")
	if !strings.Contains(section, "A Draft Highlight needs a note.") {
		t.Errorf("the refusal isn't said in the Draft Highlights block:\n%s", section)
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); len(got) != 2 {
		t.Errorf("pending = %q, want the blank one not logged", got)
	}
}

// Anyone but the Owner and Delegates is refused (403) logging or deleting a
// Draft Highlight, and nothing changes.
func TestDraftHighlightRoutesRefuseAnyoneElse(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	h.SignIn("ada@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	draft := h.LogDraftHighlight(sam, goal.ID, domain.HighlightInsight, "Retries mask the root cause.")

	for _, who := range []string{"pat@example.com", "ada@example.com"} {
		client := noRedirects(signInClient(t, ts.URL, who))
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/draft-highlights", ts.URL, goal.ID), url.Values{"note": {"Mine."}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s logging: status %d, want 403", who, resp.StatusCode)
		}
		resp = postForm(t, client, fmt.Sprintf("%s/draft-highlights/%d/delete", ts.URL, draft.ID), url.Values{})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s deleting: status %d, want 403", who, resp.StatusCode)
		}
	}
	if got := pendingDraftNotes(t, h, sam, goal.ID); !slices.Equal(got, []string{"Retries mask the root cause."}) {
		t.Errorf("pending = %q, want only the Owner's, untouched", got)
	}
}

// A Done or Cancelled Goal takes no Draft Highlights: its page offers no form
// to log one, and a log sent anyway is refused.
func TestLogDraftHighlightRefusedOnAnEndedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	for _, lifecycle := range []string{domain.LifecycleDone, domain.LifecycleCancelled} {
		goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
		h.EndGoalInCheckin(sam, goal.ID, lifecycle)

		if page := getBody(t, samClient, goalPageURL(ts.URL, goal)); strings.Contains(page, `data-testid="log-draft-highlight"`) {
			t.Errorf("a %s Goal's page offers the form to log a Draft Highlight", lifecycle)
		}
		resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/draft-highlights", ts.URL, goal.ID), url.Values{"note": {"Too late."}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("logging on a %s Goal: status %d, want 422", lifecycle, resp.StatusCode)
		}
		if got := pendingDraftNotes(t, h, sam, goal.ID); len(got) != 0 {
			t.Errorf("a %s Goal has pending %q, want none", lifecycle, got)
		}
	}
}

// Deleting a pending Draft Highlight from the Goal page lands back on it with
// the Draft Highlight gone.
func TestDeleteDraftHighlightFromTheGoalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	draft := h.LogDraftHighlight(sam, goal.ID, domain.HighlightMiss, "Runbook was late.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, samClient, goalPageURL(ts.URL, goal))
	form := tagAround(t, page, `data-testid="delete-draft-highlight"`)
	if action := attr(form, "action"); action != fmt.Sprintf("/draft-highlights/%d/delete", draft.ID) {
		t.Fatalf("Delete posts to %q, want the Draft Highlight's delete route", action)
	}

	resp := postForm(t, noRedirects(samClient), ts.URL+attr(form, "action"), url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != fmt.Sprintf("/goals/%d", goal.ID) {
		t.Errorf("delete: status %d to %q, want 303 to the Goal page", resp.StatusCode, resp.Header.Get("Location"))
	}
	page = getBody(t, samClient, goalPageURL(ts.URL, goal))
	if strings.Contains(page, "Runbook was late.") {
		t.Errorf("the Goal page still shows the deleted Draft Highlight")
	}
	if !strings.Contains(page, `data-testid="no-draft-highlights"`) {
		t.Errorf("the Goal page doesn't say no Draft Highlights are waiting")
	}
}
