package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A Goal's Owner links it under another Goal they own; the link is accepted
// immediately and both Goal pages show the connection for navigation, and the
// link can then be removed.
func TestLinkGoalsAutoAcceptNavigateAndRemove(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Displays fail often.")
	client := signInClient(t, ts.URL, "sam@example.com")

	// Request the link; owning both Goals, it auto-accepts.
	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/links", ts.URL, child.ID), url.Values{
		"parent_id": {fmt.Sprint(parent.ID)},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request link: status %d", resp.StatusCode)
	}
	childBody := readBody(t, resp)
	if !strings.Contains(childBody, navTo(parent.ID)) {
		t.Errorf("child page does not link its parent for navigation; body:\n%s", childBody)
	}

	// The parent page shows the child.
	parentBody := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID))
	if !strings.Contains(parentBody, navTo(child.ID)) {
		t.Errorf("parent page does not link its child for navigation; body:\n%s", parentBody)
	}

	// Remove the link; the parent no longer appears on the child page.
	links, err := h.Service.ParentLinks(context.Background(), child.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("ParentLinks = %+v, %v", links, err)
	}
	resp = postForm(t, client, fmt.Sprintf("%s/links/%d/remove", ts.URL, links[0].LinkID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove link: status %d", resp.StatusCode)
	}
	childBody = getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if strings.Contains(childBody, navTo(parent.ID)) {
		t.Errorf("child page still links the removed parent; body:\n%s", childBody)
	}
}

// When the parent is owned by someone else, the request waits Pending; the
// parent's Owner sees it in their inbox and accepts it into the graph.
func TestLinkRequestPendsThenParentOwnerAcceptsAcrossOwners(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")

	patClient := signInClient(t, ts.URL, "pat@example.com")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Sam requests the link with a note.
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/links", ts.URL, child.ID), url.Values{
		"parent_id": {fmt.Sprint(parent.ID)},
		"note":      {"displays cause a third of outages"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request link: status %d", resp.StatusCode)
	}
	// Not yet linked on Sam's Goal page.
	if body := readBody(t, resp); strings.Contains(body, navTo(parent.ID)) {
		t.Errorf("parent linked before acceptance; body:\n%s", body)
	}

	// Pat sees the pending request, with the note.
	inbox := getBody(t, patClient, ts.URL+"/links")
	for _, want := range []string{"Migrate displays", "Reduce outages", "displays cause a third of outages"} {
		if !strings.Contains(inbox, want) {
			t.Errorf("pending inbox missing %q; body:\n%s", want, inbox)
		}
	}

	// Pat accepts it.
	pending, err := h.Service.PendingLinkRequests(context.Background(), pat.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingLinkRequests = %+v, %v", pending, err)
	}
	resp = postForm(t, patClient, fmt.Sprintf("%s/links/%d/accept", ts.URL, pending[0].ID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept link: status %d", resp.StatusCode)
	}

	// Now Sam's Goal page shows the parent, and Pat's inbox is empty.
	childBody := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if !strings.Contains(childBody, navTo(parent.ID)) {
		t.Errorf("child page does not link the accepted parent; body:\n%s", childBody)
	}
	if inbox := getBody(t, patClient, ts.URL+"/links"); !strings.Contains(inbox, "No pending requests") {
		t.Errorf("inbox not empty after accept; body:\n%s", inbox)
	}
}

// signInClient returns an HTTP client with its own cookie jar, signed in as
// emailAddr.
func signInClient(t *testing.T, baseURL, emailAddr string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := postForm(t, client, baseURL+"/signin", url.Values{"email": {emailAddr}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign in %q: status %d", emailAddr, resp.StatusCode)
	}
	_ = readBody(t, resp)
	return client
}

// navTo is the anchor a parents/children navigation list renders for a Goal —
// distinct from the request form's <option value="id">, so it tells a real link
// apart from a mere candidate.
func navTo(goalID int64) string {
	return fmt.Sprintf(`href="/goals/%d"`, goalID)
}

func getBody(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

func newServer(t *testing.T, h *testsupport.Harness) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)
	return ts
}

// Each pending link request is a card with Accept as the primary action and a
// Reject that asks for confirmation first.
func TestPendingLinkRowsConfirmReject(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	link := h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(pat, "Reduce outages", "Outages cost trust."), "")

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/links")

	row := page[strings.Index(page, `data-testid="pending-link"`):]
	if !strings.Contains(openTag(row), `class="card`) {
		t.Errorf("the pending link isn't a card: %s", openTag(row))
	}
	assertAcceptReject(t, row, fmt.Sprintf("/links/%d", link.ID))
}

// assertAcceptReject checks a pending request's row decides it at base with
// Accept as the primary button and a Reject that asks for confirmation.
func assertAcceptReject(t *testing.T, row, base string) {
	t.Helper()
	accept := strings.Index(row, `action="`+base+`/accept"`)
	reject := strings.Index(row, `action="`+base+`/reject"`)
	if accept < 0 || reject < 0 {
		t.Fatalf("the row lacks Accept or Reject for %s:\n%s", base, row)
	}
	if button := row[accept:]; !strings.Contains(button[:strings.Index(button, "</form>")], `class="btn primary`) {
		t.Errorf("Accept isn't the primary button:\n%s", button)
	}
	if !strings.Contains(openTag(row[reject:]), `onsubmit="return confirm(`) {
		t.Errorf("Reject doesn't ask for confirmation: %s", openTag(row[reject:]))
	}
}
