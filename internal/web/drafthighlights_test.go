package web_test

import (
	"fmt"
	"html"
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
// page, oldest first, each with its kind, note, who logged it and when and a
// Delete, with the log form closed behind its link; anyone else sees no Draft
// Highlights block at all.
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
		"Accomplishment Cut paging noise by half. Sam Rivera sam@example.com · Jan 2, 15:04 Delete",
		"No kind Vendor may raise prices. Dee Okafor dee@example.com · Jan 3, 17:04 Delete",
	}
	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		page := getBody(t, signInClient(t, ts.URL, who), goalPageURL(ts.URL, goal))
		if got := elementTexts(page, "li", "draft-highlight"); !slices.Equal(got, want) {
			t.Errorf("%s sees Draft Highlights %q, want %q", who, got, want)
		}
		if got := strings.Count(page, `data-testid="delete-draft-highlight"`); got != 2 {
			t.Errorf("%s sees %d Deletes, want one per Draft Highlight", who, got)
		}
		if !strings.Contains(page, `data-testid="open-draft-highlight"`) {
			t.Errorf("%s isn't offered the link to log one", who)
		}
		if strings.Contains(page, `data-testid="log-draft-highlight"`) {
			t.Errorf("%s sees the log form open on a plain visit", who)
		}
	}

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), goalPageURL(ts.URL, goal))
	for _, leak := range []string{`data-testid="goal-draft-highlights"`, `data-testid="open-draft-highlight"`, `data-testid="log-draft-highlight"`, "Cut paging noise", "Vendor may raise"} {
		if strings.Contains(page, leak) {
			t.Errorf("someone else's Goal page shows %s", leak)
		}
	}
}

// With none pending, the Owner's and a Delegate's Draft Highlights block is
// just its heading and the "+ Log a highlight" link: no form, no help text and
// no line saying none are waiting.
func TestEmptyDraftHighlightsBlockIsItsHeadingAndLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)

	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		block := pageElement(t, getBody(t, signInClient(t, ts.URL, who), goalPageURL(ts.URL, goal)), "section", "goal-draft-highlights")
		if !strings.Contains(block, "<h2>Draft Highlights</h2>") {
			t.Errorf("%s: the block has no Draft Highlights heading: %s", who, block)
		}
		link := tagAround(t, block, `data-testid="open-draft-highlight"`)
		if href := html.UnescapeString(attr(link, "href")); href != fmt.Sprintf("/goals/%d?open=draft-highlight", goal.ID) {
			t.Errorf("%s: the link leads to %q, want the form open in place", who, href)
		}
		if name := attr(link, "aria-label"); !strings.Contains(name, "Log a highlight") {
			t.Errorf("%s: the link's accessible name %q lacks its visible text", who, name)
		}
		if !strings.Contains(block, ">+ Log a highlight</a>") {
			t.Errorf("%s: the link doesn't read \"+ Log a highlight\": %s", who, link)
		}
		for _, gone := range []string{`data-testid="log-draft-highlight"`, `data-testid="no-draft-highlights"`, "Log what's worth reporting", `data-testid="draft-highlight"`} {
			if strings.Contains(block, gone) {
				t.Errorf("%s: the closed, empty block shows %s: %s", who, gone, block)
			}
		}
	}
}

// Following "+ Log a highlight" opens the log form in place in the Draft
// Highlights block, with its help text, a Kind and a focused Note, and a
// Cancel back to the plain page; the link is hidden while it is open.
func TestLogADraftHighlightLinkOpensTheFormInPlace(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)

	for _, who := range []string{"sam@example.com", "dee@example.com"} {
		client := signInClient(t, ts.URL, who)
		link := tagAround(t, getBody(t, client, goalPageURL(ts.URL, goal)), `data-testid="open-draft-highlight"`)
		page := getBody(t, client, ts.URL+html.UnescapeString(attr(link, "href")))
		open := openForm(t, page, "draft-highlight")
		assertOpenIn(t, page, "draft-highlight", `data-testid="goal-draft-highlights"`, `data-testid="goal-history"`)
		for _, want := range []string{
			"Log what's worth reporting as it happens.",
			`data-testid="log-draft-highlight"`,
			fmt.Sprintf(`action="/goals/%d/draft-highlights"`, goal.ID),
			`<select name="kind">`,
			`<textarea name="note" required autofocus>`,
			"Log Draft Highlight</button>",
		} {
			if !strings.Contains(open, want) {
				t.Errorf("%s: the open form lacks %s: %s", who, want, open)
			}
		}
		if cancel := tagAround(t, open, `data-testid="cancel-form"`); attr(cancel, "href") != fmt.Sprintf("/goals/%d", goal.ID) {
			t.Errorf("%s: Cancel leads to %q, want the plain Goal page", who, attr(cancel, "href"))
		}
		if strings.Contains(page, `data-testid="open-draft-highlight"`) {
			t.Errorf("%s: the link still shows while the form is open", who)
		}
	}
}

// The Owner logs a Draft Highlight with a kind and a Delegate one without,
// each landing back on the Goal page with the form closed and it listed; a
// blank note is refused there, the form open again saying why once and
// keeping the Kind picked, and logs nothing.
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
		client := signInClient(t, ts.URL, tc.who)
		resp := postForm(t, noRedirects(client), logURL, url.Values{"kind": {tc.kind}, "note": {tc.note}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != fmt.Sprintf("/goals/%d", goal.ID) {
			t.Errorf("%s logging: status %d to %q, want 303 to the Goal page", tc.who, resp.StatusCode, resp.Header.Get("Location"))
		}
		page := getBody(t, client, ts.URL+resp.Header.Get("Location"))
		if n := openForms(page); n != 0 {
			t.Errorf("%s: the page logged to shows %d forms open, want the form closed", tc.who, n)
		}
		if got := elementTexts(page, "li", "draft-highlight"); !slices.ContainsFunc(got, func(d string) bool { return strings.Contains(d, tc.note) }) {
			t.Errorf("%s: the page lists %q, want %q among them", tc.who, got, tc.note)
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
	open := openForm(t, page, "draft-highlight")
	assertOpenIn(t, page, "draft-highlight", `data-testid="goal-draft-highlights"`, `data-testid="goal-history"`)
	if got := strings.Count(page, "A Draft Highlight needs a note."); got != 1 {
		t.Errorf("the refusal shows %d times, want once", got)
	}
	if alert := elementTexts(open, "p", "form-error"); !slices.Equal(alert, []string{"A Draft Highlight needs a note."}) {
		t.Errorf("the open form's alert reads %q, want the refusal", alert)
	}
	if !strings.Contains(open, fmt.Sprintf(`<option value="%s" selected>`, domain.HighlightMiss)) {
		t.Errorf("the refused form lost the Kind picked: %s", open)
	}
	if strings.Contains(page, `data-testid="open-draft-highlight"`) {
		t.Errorf("the link shows beside the refused form")
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

// A Done or Cancelled Goal takes no Draft Highlights: its page offers no link
// to log one, ?open=draft-highlight opens no form, and a log sent anyway is
// refused. Its Draft Highlights block still lists any left pending, each with
// its Delete, and is left out when none are.
func TestLogDraftHighlightRefusedOnAnEndedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	for _, lifecycle := range []string{domain.LifecycleDone, domain.LifecycleCancelled} {
		goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
		h.EndGoalInCheckin(sam, goal.ID, lifecycle)
		leftOver := h.ActiveGoal(sam, "Migrate billing", "Billing is brittle.")
		h.LogDraftHighlight(sam, leftOver.ID, domain.HighlightMiss, "Runbook was late.")
		h.EndGoalInCheckin(sam, leftOver.ID, lifecycle)

		for _, g := range []domain.Goal{goal, leftOver} {
			page := getBody(t, samClient, goalPageURL(ts.URL, g)+"?open=draft-highlight")
			for _, offered := range []string{`data-testid="open-draft-highlight"`, `data-testid="log-draft-highlight"`, `data-open-form="draft-highlight"`} {
				if strings.Contains(page, offered) {
					t.Errorf("a %s Goal's page, asked to open the form, shows %s", lifecycle, offered)
				}
			}
		}

		if page := getBody(t, samClient, goalPageURL(ts.URL, goal)); strings.Contains(page, `data-testid="goal-draft-highlights"`) {
			t.Errorf("a %s Goal with none pending shows the Draft Highlights block", lifecycle)
		}
		block := pageElement(t, getBody(t, samClient, goalPageURL(ts.URL, leftOver)), "section", "goal-draft-highlights")
		if got := elementTexts(block, "li", "draft-highlight"); len(got) != 1 || !strings.Contains(got[0], "Runbook was late.") || !strings.Contains(block, `data-testid="delete-draft-highlight"`) {
			t.Errorf("a %s Goal's block lists %q, want the one left pending with its Delete", lifecycle, got)
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
// the Draft Highlight gone and none listed.
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
	if got := elementTexts(page, "li", "draft-highlight"); len(got) != 0 {
		t.Errorf("the Goal page lists Draft Highlights %q, want none", got)
	}
}
