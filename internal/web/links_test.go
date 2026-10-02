package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
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
// Reject that rejects it at once, with no confirmation (#56).
func TestPendingLinkRowsRejectAtOnce(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	link := h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(pat, "Reduce outages", "Outages cost trust."), "")

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/links")

	row := between(t, page, `data-testid="pending-link"`, "")
	if !strings.Contains(openTag(row), `class="card`) {
		t.Errorf("the pending link isn't a card: %s", openTag(row))
	}
	base := fmt.Sprintf("/links/%d", link.ID)
	assertAcceptReject(t, row, base)

	client := signInClient(t, ts.URL, "pat@example.com")
	if page := readBody(t, postForm(t, client, ts.URL+base+"/reject", url.Values{})); !strings.Contains(page, `data-testid="no-pending-links"`) {
		t.Errorf("the link request is still pending after Reject:\n%s", page)
	}
}

// A pending link's note is set apart by its indent and a 1px neutral rule, not
// a coloured side stripe (#67).
func TestPendingLinkNoteIsSetApartByANeutralRule(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(pat, "Reduce outages", "Outages cost trust."), "Displays drive most outages.")

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/links")

	pageElement(t, page, "blockquote", "pending-link-note")
	assertNeutralRule(t, cssRule(t, page, ".pd-note"))
}

// assertNeutralRule checks a rule indents its element 12px from a 1px left
// rule in --color-border, the neutral alternative to a coloured side stripe.
func assertNeutralRule(t *testing.T, rule string) {
	t.Helper()
	for _, want := range []string{"padding-left:12px", "border-left:1px solid var(--color-border)"} {
		if !strings.Contains(rule, want) {
			t.Errorf("rule lacks %s; rule: %s", want, rule)
		}
	}
}

// assertAcceptReject checks a pending request's row decides it at base with
// Accept as the primary button and a Reject that submits at once.
func assertAcceptReject(t *testing.T, row, base string) {
	t.Helper()
	if accept := between(t, row, `action="`+base+`/accept"`, "</form>"); !strings.Contains(accept, `class="btn primary`) {
		t.Errorf("Accept isn't the primary button:\n%s", accept)
	}
	assertSubmitsAtOnce(t, "Reject", tagAround(t, row, `action="`+base+`/reject"`))
}

// assertSubmitsAtOnce checks a form's opening tag sends it without a browser
// dialog asking for confirmation first (#56).
func assertSubmitsAtOnce(t *testing.T, what, form string) {
	t.Helper()
	if strings.Contains(form, "onsubmit=") || strings.Contains(form, "confirm(") {
		t.Errorf("%s asks for confirmation: %s", what, form)
	}
}

// assertConfirms checks a form's opening tag asks for confirmation in a browser
// dialog before sending it.
func assertConfirms(t *testing.T, what, form string) {
	t.Helper()
	if !strings.Contains(form, `onsubmit="return confirm(`) {
		t.Errorf("%s doesn't ask for confirmation: %s", what, form)
	}
}

// between returns s from the first start up to the first end after it, or to
// the end of s when end is "", failing the test if either is missing.
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	at := strings.Index(s, start)
	if at < 0 {
		t.Fatalf("no %s in:\n%s", start, s)
	}
	s = s[at:]
	if end == "" {
		return s
	}
	upTo := strings.Index(s, end)
	if upTo < 0 {
		t.Fatalf("no %s after %s in:\n%s", end, start, s)
	}
	return s[:upTo]
}

// undoAction pulls the Undo form's action out of a page's toast, failing when
// the page carries no toast.
func undoAction(t *testing.T, page string) string {
	t.Helper()
	toast := pageElement(t, page, "aside", "toast")
	m := regexp.MustCompile(`<form[^>]*action="([^"]+)"`).FindStringSubmatch(toast)
	if m == nil {
		t.Fatalf("the toast has no Undo form:\n%s", toast)
	}
	return m[1]
}

// removeAcrossOwners arranges Sam's child Goal accepted under Pat's parent and
// Sam removing that link from the child's Goal page, returning the page Sam
// lands on.
func removeAcrossOwners(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (child, parent domain.Goal, sam *http.Client, landed string) {
	t.Helper()
	pat := h.SignIn("pat@example.com")
	samAcc := h.SignIn("sam@example.com")
	parent = h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(samAcc, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(samAcc, child, parent, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	sam = signInClient(t, ts.URL, "sam@example.com")
	resp := postForm(t, sam, fmt.Sprintf("%s/links/%d/remove", ts.URL, link.ID), url.Values{
		"goal_id": {fmt.Sprint(child.ID)},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove link: status %d", resp.StatusCode)
	}
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", child.ID) {
		t.Errorf("remove landed on %s, want the Goal page it came from", resp.Request.URL.Path)
	}
	return child, parent, sam, readBody(t, resp)
}

// After removing a link, the Goal page carries a toast saying so with an Undo
// button, a plain form post; Undo puts the link back as accepted without
// asking the parent's Owner, and the toast isn't shown on a later visit.
func TestRemovingALinkOffersUndoOnce(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, sam, landed := removeAcrossOwners(t, h, ts)

	toast := pageElement(t, landed, "aside", "toast")
	for _, want := range []string{"Reduce outages", `method="post"`, "Undo"} {
		if !strings.Contains(toast, want) {
			t.Errorf("toast missing %q:\n%s", want, toast)
		}
	}
	if strings.Contains(toast, "<script") {
		t.Errorf("toast uses script:\n%s", toast)
	}
	action := undoAction(t, landed)

	later := getBody(t, sam, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if strings.Contains(later, `data-testid="toast"`) {
		t.Errorf("toast shown again on a later visit:\n%s", later)
	}

	resp := postForm(t, sam, ts.URL+action, url.Values{"goal_id": {fmt.Sprint(child.ID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo: status %d", resp.StatusCode)
	}
	if page := readBody(t, resp); !strings.Contains(page, navTo(parent.ID)) {
		t.Errorf("the Goal page doesn't link the restored parent:\n%s", page)
	}
	if got := h.ParentsOf(child); len(got) != 1 || got[0].ID != parent.ID {
		t.Errorf("ParentsOf(child) = %+v, want the restored parent", got)
	}
}

// A Remove button says which Goal page it sits on, so removing a link from the
// parent's page returns there, toast and all, not to the child's.
func TestRemoveReturnsToTheGoalPageItCameFrom(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	owner := h.SignIn("sam@example.com")
	parent := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(owner, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(owner, child, parent, "")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID))
	form := between(t, page, fmt.Sprintf(`action="/links/%d/remove"`, link.ID), "</form>")
	if want := fmt.Sprintf(`name="goal_id" value="%d"`, parent.ID); !strings.Contains(form, want) {
		t.Fatalf("the Remove form doesn't post %s:\n%s", want, form)
	}

	resp := postForm(t, client, fmt.Sprintf("%s/links/%d/remove", ts.URL, link.ID), url.Values{"goal_id": {fmt.Sprint(parent.ID)}})
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", parent.ID) {
		t.Errorf("remove landed on %s, want the parent's Goal page", resp.Request.URL.Path)
	}
	pageElement(t, readBody(t, resp), "aside", "toast")
}

// Undo for a link is refused for anyone but its remover, and refused a second
// time; a crafted Undo of a removal that never happened is refused too, and
// none of them creates a link.
func TestUndoingALinkRemovalIsRefusedWhenItCantBeForged(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	child, _, sam, landed := removeAcrossOwners(t, h, ts)
	action := undoAction(t, landed)
	pat := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, pat, ts.URL+action, url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Undo by the parent's Owner: status %d, want 403", resp.StatusCode)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Fatalf("ParentsOf(child) = %+v after a refused Undo, want none", got)
	}

	if resp := postForm(t, sam, ts.URL+action, url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("Undo by the remover: status %d", resp.StatusCode)
	}
	links, err := h.Service.ParentLinks(context.Background(), child.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("ParentLinks = %+v, %v", links, err)
	}
	if resp := postForm(t, sam, fmt.Sprintf("%s/links/%d/remove", ts.URL, links[0].LinkID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("remove again: status %d", resp.StatusCode)
	}
	resp = postForm(t, sam, ts.URL+action, url.Values{})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "already been undone") {
		t.Errorf("second Undo: status %d %q, want 422 saying it was already undone", resp.StatusCode, body)
	}

	resp = postForm(t, sam, ts.URL+"/link-removals/4242/undo", url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Undo of a removal that never happened: status %d, want 404", resp.StatusCode)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after refused Undos, want none", got)
	}
}

// Undo that would now close a cycle is refused with a message, and nothing
// changes.
func TestUndoingALinkRemovalIsRefusedWhenItWouldCreateACycle(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, sam, landed := removeAcrossOwners(t, h, ts)
	action := undoAction(t, landed)
	pat := h.SignIn("pat@example.com")
	h.RequestLink(pat, parent, child, "") // Pat links the other way round...
	samAcc := h.SignIn("sam@example.com")
	pending, err := h.Service.PendingLinkRequests(context.Background(), samAcc.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingLinkRequests = %+v, %v", pending, err)
	}
	if _, err := h.Service.AcceptLink(context.Background(), pending[0].ID, samAcc.ID); err != nil { // ...and Sam accepts
		t.Fatalf("AcceptLink: %v", err)
	}

	resp := postForm(t, sam, ts.URL+action, url.Values{})
	if body := readBody(t, resp); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "cycle") {
		t.Errorf("Undo closing a cycle: status %d %q, want 409 naming the cycle", resp.StatusCode, body)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused Undo, want none", got)
	}
}

// Only the person who removed a link is offered its Undo: the same offer
// carried to someone else's page shows no toast.
func TestTheLinkRemovalToastIsOnlyForTheRemover(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	child, _, _, landed := removeAcrossOwners(t, h, ts)
	action := undoAction(t, landed)
	id := strings.TrimSuffix(strings.TrimPrefix(action, "/link-removals/"), "/undo")

	pat := signInClient(t, ts.URL, "pat@example.com")
	page := fmt.Sprintf("%s/goals/%d", ts.URL, child.ID)
	u, err := url.Parse(page)
	if err != nil {
		t.Fatal(err)
	}
	pat.Jar.SetCookies(u, []*http.Cookie{{Name: "gt_undo", Value: "link-removal:" + id, Path: u.Path}})
	if body := getBody(t, pat, page); strings.Contains(body, `data-testid="toast"`) {
		t.Errorf("someone else's removal offered as a toast:\n%s", body)
	}
}

// rejectRequest arranges Sam's request, with a note, for his child Goal to
// contribute to Pat's parent, and Pat rejecting it from the page at from
// ("/links" or "/home"), returning the page Pat lands on.
func rejectRequest(t *testing.T, h *testsupport.Harness, ts *httptest.Server, from string) (child, parent domain.Goal, pat *http.Client, landed string) {
	t.Helper()
	patAcc := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent = h.CreateGoal(patAcc, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "displays cause outages")
	pat = signInClient(t, ts.URL, "pat@example.com")
	form := url.Values{}
	if from == "/home" {
		row := homeRow(t, pageElement(t, getBody(t, pat, ts.URL+"/home"), "ul", "home-requests"), child)
		reject := between(t, row, fmt.Sprintf(`action="/links/%d/reject"`, link.ID), "</form>")
		if !strings.Contains(reject, `name="from" value="home"`) {
			t.Fatalf("Home's Reject doesn't say it came from Home:\n%s", reject)
		}
		form.Set("from", "home")
	}
	resp := postForm(t, pat, fmt.Sprintf("%s/links/%d/reject", ts.URL, link.ID), form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject: status %d", resp.StatusCode)
	}
	if resp.Request.URL.Path != from {
		t.Errorf("reject landed on %s, want %s, where it came from", resp.Request.URL.Path, from)
	}
	return child, parent, pat, readBody(t, resp)
}

// After rejecting a link request, from the pending page or from Home, the page
// shown next carries a toast saying so with an Undo button, a plain form post;
// the toast isn't shown on a later visit, and Undo returns to the same page
// with the request pending again, note and all.
func TestRejectingALinkRequestOffersUndoOnce(t *testing.T) {
	for _, from := range []string{"/links", "/home"} {
		t.Run(from, func(t *testing.T) {
			h := testsupport.New(t)
			ts := newServer(t, h)
			child, parent, pat, landed := rejectRequest(t, h, ts, from)

			toast := pageElement(t, landed, "aside", "toast")
			for _, want := range []string{"Migrate displays", "Reduce outages", `method="post"`, "Undo"} {
				if !strings.Contains(toast, want) {
					t.Errorf("toast missing %q:\n%s", want, toast)
				}
			}
			if strings.Contains(toast, "<script") {
				t.Errorf("toast uses script:\n%s", toast)
			}
			action := undoAction(t, landed)
			fields := url.Values{}
			for _, m := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(toast, -1) {
				fields.Set(m[1], m[2])
			}

			if later := getBody(t, pat, ts.URL+from); strings.Contains(later, `data-testid="toast"`) {
				t.Errorf("toast shown again on a later visit:\n%s", later)
			}

			resp := postForm(t, pat, ts.URL+action, fields)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("undo: status %d", resp.StatusCode)
			}
			if resp.Request.URL.Path != from {
				t.Errorf("undo landed on %s, want %s", resp.Request.URL.Path, from)
			}
			if page := readBody(t, resp); !strings.Contains(page, navTo(child.ID)) {
				t.Errorf("the page doesn't list the restored request:\n%s", page)
			}
			patAcc := h.SignIn("pat@example.com")
			pending, _ := h.Service.PendingLinkRequests(context.Background(), patAcc.ID)
			if len(pending) != 1 || pending[0].Child.ID != child.ID || pending[0].Parent.ID != parent.ID || pending[0].Note != "displays cause outages" {
				t.Errorf("pending = %+v, want the request back with its note", pending)
			}
		})
	}
}

// Undo for a rejected request is refused for anyone but its rejecter, and
// refused a second time; a crafted Undo of a rejection that never happened is
// refused too, and none of them adds a request.
func TestUndoingALinkRejectionIsRefusedWhenItCantBeForged(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	_, _, pat, landed := rejectRequest(t, h, ts, "/links")
	action := undoAction(t, landed)
	sam := signInClient(t, ts.URL, "sam@example.com")
	patAcc := h.SignIn("pat@example.com")
	pendingCount := func() int {
		pending, err := h.Service.PendingLinkRequests(context.Background(), patAcc.ID)
		if err != nil {
			t.Fatalf("PendingLinkRequests: %v", err)
		}
		return len(pending)
	}

	resp := postForm(t, sam, ts.URL+action, url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Undo by the requester: status %d, want 403", resp.StatusCode)
	}
	if n := pendingCount(); n != 0 {
		t.Fatalf("pending = %d after a refused Undo, want 0", n)
	}

	resp = postForm(t, pat, ts.URL+"/link-rejections/4242/undo", url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Undo of a rejection that never happened: status %d, want 404", resp.StatusCode)
	}
	if n := pendingCount(); n != 0 {
		t.Fatalf("pending = %d after a crafted Undo, want 0", n)
	}

	if resp := postForm(t, pat, ts.URL+action, url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("Undo by the rejecter: status %d", resp.StatusCode)
	}
	resp = postForm(t, pat, ts.URL+action, url.Values{})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "already been undone") {
		t.Errorf("second Undo: status %d %q, want 422 saying it was already undone", resp.StatusCode, body)
	}
	if n := pendingCount(); n != 1 {
		t.Errorf("pending = %d, want just the one restored request", n)
	}
}

// Undo is refused with a message, changing nothing, once the same link has
// been requested again, or when it would now close a cycle.
func TestUndoingALinkRejectionIsRefusedWhenItNoLongerFits(t *testing.T) {
	t.Run("requested again", func(t *testing.T) {
		h := testsupport.New(t)
		ts := newServer(t, h)
		child, parent, pat, landed := rejectRequest(t, h, ts, "/links")
		action := undoAction(t, landed)
		h.RequestLink(h.SignIn("sam@example.com"), child, parent, "second try")

		resp := postForm(t, pat, ts.URL+action, url.Values{})
		if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "requested again") {
			t.Errorf("Undo: status %d %q, want 422 saying it was requested again", resp.StatusCode, body)
		}
		pending, _ := h.Service.PendingLinkRequests(context.Background(), h.SignIn("pat@example.com").ID)
		if len(pending) != 1 || pending[0].Note != "second try" {
			t.Errorf("pending = %+v, want just the new request", pending)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		h := testsupport.New(t)
		ts := newServer(t, h)
		child, parent, pat, landed := rejectRequest(t, h, ts, "/links")
		action := undoAction(t, landed)
		patAcc, sam := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
		back := h.RequestLink(patAcc, parent, child, "") // Pat links the other way round...
		if _, err := h.Service.AcceptLink(context.Background(), back.ID, sam.ID); err != nil { // ...and Sam accepts
			t.Fatalf("AcceptLink: %v", err)
		}

		resp := postForm(t, pat, ts.URL+action, url.Values{})
		if body := readBody(t, resp); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "cycle") {
			t.Errorf("Undo: status %d %q, want 409 naming the cycle", resp.StatusCode, body)
		}
		if pending, _ := h.Service.PendingLinkRequests(context.Background(), patAcc.ID); len(pending) != 0 {
			t.Errorf("pending = %+v, want none after a refused Undo", pending)
		}
	})
}
