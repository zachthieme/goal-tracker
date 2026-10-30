package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A Goal's Owner links it under another Goal they own; the link is accepted
// immediately and both Goal pages show the connection for navigation, and the
// link can then be removed.
func TestLinkGoalsAutoAcceptNavigateAndRemove(t *testing.T) {
	t.Parallel()

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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request link: status %d", resp.StatusCode)
	}
	if parents := pageElement(t, readBody(t, resp), "section", "goal-parents"); !strings.Contains(parents, navTo(parent.ID)) {
		t.Errorf("child page does not link its parent for navigation:\n%s", parents)
	}

	// The parent page shows the child.
	parentBody := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID))
	if children := pageElement(t, parentBody, "section", "goal-children"); !strings.Contains(children, navTo(child.ID)) {
		t.Errorf("parent page does not link its child for navigation:\n%s", children)
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
	// (Its History still names the parent the link was removed from.)
	childBody := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if parents := pageElement(t, childBody, "section", "goal-parents"); strings.Contains(parents, navTo(parent.ID)) {
		t.Errorf("child page still lists the removed parent:\n%s", parents)
	}
}

// When the parent is owned by someone else, the request waits Pending; the
// parent's Owner sees it in their inbox and accepts it into the graph.
func TestLinkRequestPendsThenParentOwnerAcceptsAcrossOwners(t *testing.T) {
	t.Parallel()

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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request link: status %d", resp.StatusCode)
	}
	// Not yet linked on Sam's Goal page, though its History names the request.
	if parents := pageElement(t, readBody(t, resp), "section", "goal-parents"); strings.Contains(parents, navTo(parent.ID)) {
		t.Errorf("parent linked before acceptance:\n%s", parents)
	}

	// Pat sees the pending request on Home, with the note.
	requests := textOf(findByTestID(t, getPage(t, patClient, ts.URL+"/home"), "home-requests"))
	for _, want := range []string{"Migrate displays", "Reduce outages", "displays cause a third of outages"} {
		if !strings.Contains(requests, want) {
			t.Errorf("Home's Requests miss %q; they read %q", want, requests)
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

	// Now Sam's Goal page shows the parent, and Home has no Requests for Pat.
	childBody := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if parents := pageElement(t, childBody, "section", "goal-parents"); !strings.Contains(parents, navTo(parent.ID)) {
		t.Errorf("child page does not link the accepted parent:\n%s", parents)
	}
	if home := getPage(t, patClient, ts.URL+"/home"); hasTestID(home, "home-requests") {
		t.Errorf("Home still lists Requests after accept:\n%s", renderNode(findByTestID(t, home, "home-requests")))
	}
}

// assertAcceptReject checks a pending request's row decides it at base with
// Accept as the primary button and, after it, a button named Reject that
// submits at once.
func assertAcceptReject(t *testing.T, row, base string) {
	t.Helper()
	if accept := between(t, row, `action="`+base+`/accept"`, "</form>"); !strings.Contains(accept, `class="btn primary`) {
		t.Errorf("Accept isn't the primary button:\n%s", accept)
	}
	parsed := parsePage(t, row)
	rejects := findAll(parsed, func(n *html.Node) bool { return n.Data == "form" && attrOf(n, "action") == base+"/reject" })
	if len(rejects) != 1 || buttonNamed(rejects[0], "Reject") == nil {
		t.Errorf("the row offers no button named Reject posting to %s/reject:\n%s", base, row)
	} else if accept := buttonNamed(parsed, "Accept"); accept == nil || !precedes(accept, buttonNamed(rejects[0], "Reject")) {
		t.Errorf("the row does not put Accept before Reject:\n%s", row)
	}
	assertSubmitsAtOnce(t, "Reject", row, base+"/reject")
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
	defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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

	resp := postForm(t, sam, ts.URL+action, toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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
	defer func() { _ = resp.Body.Close() }()
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", parent.ID) {
		t.Errorf("remove landed on %s, want the parent's Goal page", resp.Request.URL.Path)
	}
	pageElement(t, readBody(t, resp), "aside", "toast")
}

// Undo for a link is refused for anyone but its remover, and refused a second
// time; a crafted Undo of a removal that never happened is refused too, and
// none of them creates a link.
func TestUndoingALinkRemovalIsRefusedWhenItCantBeForged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, _, sam, landed := removeAcrossOwners(t, h, ts)
	action := undoAction(t, landed)
	pat := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Undo by the parent's Owner: status %d, want 403", resp.StatusCode)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Fatalf("ParentsOf(child) = %+v after a refused Undo, want none", got)
	}

	if status := postFormStatus(t, sam, ts.URL+action, toastFields(t, landed)); status != http.StatusOK {
		t.Fatalf("Undo by the remover: status %d", status)
	}
	links, err := h.Service.ParentLinks(context.Background(), child.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("ParentLinks = %+v, %v", links, err)
	}
	if status := postFormStatus(t, sam, fmt.Sprintf("%s/links/%d/remove", ts.URL, links[0].LinkID), url.Values{}); status != http.StatusOK {
		t.Fatalf("remove again: status %d", status)
	}
	resp = postForm(t, sam, ts.URL+action, toastFields(t, landed))
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
	t.Parallel()

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

	resp := postForm(t, sam, ts.URL+action, toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, _, _, landed := removeAcrossOwners(t, h, ts)
	action := undoAction(t, landed)
	id := strings.TrimSuffix(strings.TrimPrefix(action, "/link-removals/"), "/undo")
	token := toastFields(t, landed).Get("undo")

	pat := signInClient(t, ts.URL, "pat@example.com")
	page := fmt.Sprintf("%s/goals/%d", ts.URL, child.ID)
	u, err := url.Parse(page)
	if err != nil {
		t.Fatal(err)
	}
	pat.Jar.SetCookies(u, []*http.Cookie{{Name: "gt_undo", Value: "link-removal:" + id + ":" + token, Path: u.Path}})
	if body := getBody(t, pat, page); strings.Contains(body, `data-testid="toast"`) {
		t.Errorf("someone else's removal offered as a toast:\n%s", body)
	}
}

// rejectRequest arranges Sam's request, with a note, for his child Goal to
// contribute to Pat's parent, and Pat rejecting it from the page at from
// ("/home"), returning the page Pat lands on.
func rejectRequest(t *testing.T, h *testsupport.Harness, ts *httptest.Server, from string) (child, parent domain.Goal, pat *http.Client, landed string) {
	t.Helper()
	patAcc := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent = h.CreateGoal(patAcc, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "displays cause outages")
	pat = signInClient(t, ts.URL, "pat@example.com")
	action := fmt.Sprintf("/links/%d/reject", link.ID)
	rejects := findAll(getPage(t, pat, ts.URL+from), func(n *html.Node) bool { return n.Data == "form" && attrOf(n, "action") == action })
	if len(rejects) != 1 {
		t.Fatalf("%s offers %d Rejects posting to %s, want 1", from, len(rejects), action)
	}
	form := formSubmission(rejects[0])
	if got := form.Get("return"); got != from {
		t.Fatalf("%s's Reject returns to %q, want %s: %s", from, got, from, renderNode(rejects[0]))
	}
	resp := postForm(t, pat, fmt.Sprintf("%s/links/%d/reject", ts.URL, link.ID), form)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject: status %d", resp.StatusCode)
	}
	if resp.Request.URL.Path != from {
		t.Errorf("reject landed on %s, want %s, where it came from", resp.Request.URL.Path, from)
	}
	return child, parent, pat, readBody(t, resp)
}

// After rejecting a link request from Home, Home carries a toast saying so
// with an Undo button, a plain form post; the toast isn't shown on a later
// visit, and Undo returns to Home with the request pending again, note and
// all.
func TestRejectingALinkRequestOffersUndoOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, pat, landed := rejectRequest(t, h, ts, "/home")

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

	if later := getBody(t, pat, ts.URL+"/home"); strings.Contains(later, `data-testid="toast"`) {
		t.Errorf("toast shown again on a later visit:\n%s", later)
	}

	resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo: status %d", resp.StatusCode)
	}
	if resp.Request.URL.Path != "/home" {
		t.Errorf("undo landed on %s, want /home", resp.Request.URL.Path)
	}
	if page := readBody(t, resp); !strings.Contains(page, navTo(child.ID)) {
		t.Errorf("the page doesn't list the restored request:\n%s", page)
	}
	patAcc := h.SignIn("pat@example.com")
	pending, _ := h.Service.PendingLinkRequests(context.Background(), patAcc.ID)
	if len(pending) != 1 || pending[0].Child.ID != child.ID || pending[0].Parent.ID != parent.ID || pending[0].Note != "displays cause outages" {
		t.Errorf("pending = %+v, want the request back with its note", pending)
	}
}

// Undo for a rejected request is refused for anyone but its rejecter, and
// refused a second time; a crafted Undo of a rejection that never happened is
// refused too, and none of them adds a request.
func TestUndoingALinkRejectionIsRefusedWhenItCantBeForged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	_, _, pat, landed := rejectRequest(t, h, ts, "/home")
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

	resp := postForm(t, sam, ts.URL+action, toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
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

	if status := postFormStatus(t, pat, ts.URL+action, toastFields(t, landed)); status != http.StatusOK {
		t.Fatalf("Undo by the rejecter: status %d", status)
	}
	resp = postForm(t, pat, ts.URL+action, toastFields(t, landed))
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
	t.Parallel()

	t.Run("requested again", func(t *testing.T) {
		h := testsupport.New(t)
		ts := newServer(t, h)
		child, parent, pat, landed := rejectRequest(t, h, ts, "/home")
		action := undoAction(t, landed)
		h.RequestLink(h.SignIn("sam@example.com"), child, parent, "second try")

		resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
		defer func() { _ = resp.Body.Close() }()
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
		child, parent, pat, landed := rejectRequest(t, h, ts, "/home")
		action := undoAction(t, landed)
		patAcc, sam := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
		// Pat links the other way round, and Sam accepts.
		back := h.RequestLink(patAcc, parent, child, "")
		if _, err := h.Service.AcceptLink(context.Background(), back.ID, sam.ID); err != nil {
			t.Fatalf("AcceptLink: %v", err)
		}

		resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusConflict || !strings.Contains(body, "cycle") {
			t.Errorf("Undo: status %d %q, want 409 naming the cycle", resp.StatusCode, body)
		}
		if pending, _ := h.Service.PendingLinkRequests(context.Background(), patAcc.ID); len(pending) != 0 {
			t.Errorf("pending = %+v, want none after a refused Undo", pending)
		}
	})
}

// Sam removes a link; it is requested and accepted again, and Pat, the
// parent's Owner, removes it. Sam's Undo from the first toast is refused with
// a message, and the link stays removed.
func TestALinkRemovalUndoCantBeReplayedOverALaterRemoval(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, sam, landed := removeAcrossOwners(t, h, ts)
	pat, samAcc := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
	again := h.RequestLink(samAcc, child, parent, "")
	if _, err := h.Service.AcceptLink(context.Background(), again.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if _, err := h.Service.RemoveLink(context.Background(), again.ID, pat.ID); err != nil {
		t.Fatalf("RemoveLink by Pat: %v", err)
	}

	resp := postForm(t, sam, ts.URL+undoAction(t, landed), toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "requested again") {
		t.Errorf("replayed Undo: status %d %q, want 422 saying the link was requested again", resp.StatusCode, body)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a replayed Undo, want none", got)
	}
}

// linkedAcrossOwners links Sam's child Goal under Pat's parent Goal, accepted.
func linkedAcrossOwners(t *testing.T, h *testsupport.Harness) (child, parent domain.Goal, link domain.Link) {
	t.Helper()
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent = h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child = h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link = h.RequestLink(sam, child, parent, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	return child, parent, link
}

// Remove shows on a link only to an Owner of either linked Goal, the people
// allowed to remove it; anyone else still sees the linked Goal, without it.
func TestRemoveShowsOnALinkOnlyForAnOwnerOfEitherGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, link := linkedAcrossOwners(t, h)
	h.SignIn("lee@example.com")
	removeForm := fmt.Sprintf(`action="/links/%d/remove"`, link.ID)

	pages := []struct {
		who, section string
		goal, linked domain.Goal
		canRemove    bool
	}{
		{"sam@example.com", "goal-parents", child, parent, true},
		{"pat@example.com", "goal-children", parent, child, true},
		{"pat@example.com", "goal-parents", child, parent, true},
		{"sam@example.com", "goal-children", parent, child, true},
		{"lee@example.com", "goal-parents", child, parent, false},
		{"lee@example.com", "goal-children", parent, child, false},
	}
	for _, p := range pages {
		client := signInClient(t, ts.URL, p.who)
		links := pageElement(t, getBody(t, client, goalPageURL(ts.URL, p.goal)), "section", p.section)
		if !strings.Contains(links, navTo(p.linked.ID)) {
			t.Errorf("%s on Goal %d: the link to Goal %d is missing:\n%s", p.who, p.goal.ID, p.linked.ID, links)
		}
		if got := strings.Contains(links, removeForm); got != p.canRemove {
			t.Errorf("%s on Goal %d: Remove shown = %v, want %v:\n%s", p.who, p.goal.ID, got, p.canRemove, links)
		}
	}
}

// A removal by someone who owns neither linked Goal is refused with a page
// inside the app saying why, linking back to the Goal page it came from, and
// the link stays.
func TestRefusedLinkRemovalShowsAPageSayingWhy(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	child, parent, link := linkedAcrossOwners(t, h)
	h.SignIn("lee@example.com")
	lee := signInClient(t, ts.URL, "lee@example.com")

	resp := postForm(t, lee, fmt.Sprintf("%s/links/%d/remove", ts.URL, link.ID), url.Values{"goal_id": {fmt.Sprint(parent.ID)}})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("remove by a non-Owner: status %d, want 403", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't remove link", "Only an Owner of the linked Goals may remove the link.", fmt.Sprintf("/goals/%d", parent.ID))
	if strings.Contains(body, "not authorized:") {
		t.Errorf("refusal page shows the raw error:\n%s", body)
	}
	if got := h.ParentsOf(child); len(got) != 1 || got[0].ID != parent.ID {
		t.Errorf("ParentsOf(child) = %+v after a refused removal, want the parent", got)
	}
}

// A refused removal posted without a Goal to go back to, or with one that
// isn't a number, links Back to Home rather than to a Goal page that isn't
// there.
func TestRefusedLinkRemovalWithoutAGoalLinksBackHome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	_, _, link := linkedAcrossOwners(t, h)
	h.SignIn("lee@example.com")
	lee := signInClient(t, ts.URL, "lee@example.com")

	for name, form := range map[string]url.Values{
		"no goal_id":  {},
		"goal_id=abc": {"goal_id": {"abc"}},
	} {
		resp := postForm(t, lee, fmt.Sprintf("%s/links/%d/remove", ts.URL, link.ID), form)
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
		if want := `<a href="/home">Back</a>`; !strings.Contains(body, want) {
			t.Errorf("%s: refusal page has no %s:\n%s", name, want, body)
		}
	}
}

// Only the parent Goal's Owner may decide a link request: anyone else's
// Accept or Reject, the requester's included, is refused as Forbidden, and a
// decision on a link that isn't there is Not found. Neither changes the
// request, which still waits Pending.
func TestARefusedLinkDecisionLeavesTheRequestPending(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	h.SignIn("lee@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")
	const missing = 999999

	decisions := []struct {
		who    string
		linkID int64
		want   int
	}{
		{"lee@example.com", link.ID, http.StatusForbidden},
		{"sam@example.com", link.ID, http.StatusForbidden},
		{"pat@example.com", missing, http.StatusNotFound},
	}
	for _, d := range decisions {
		client := signInClient(t, ts.URL, d.who)
		for _, decision := range []string{"accept", "reject"} {
			resp := postForm(t, client, fmt.Sprintf("%s/links/%d/%s", ts.URL, d.linkID, decision), url.Values{})
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			if resp.StatusCode != d.want {
				t.Errorf("%s by %s on link %d: status %d, want %d", decision, d.who, d.linkID, resp.StatusCode, d.want)
			}
		}
	}

	pending, err := h.Service.PendingLinkRequests(context.Background(), pat.ID)
	if err != nil || len(pending) != 1 || pending[0].ID != link.ID {
		t.Errorf("PendingLinkRequests(pat) = %+v, %v after refused decisions; want link %d still pending", pending, err, link.ID)
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after refused decisions, want none", got)
	}
}

// A link accept by someone other than the parent's Owner is refused with a
// page inside the app giving the reason alone, not the error's "not
// authorized:" prefix, and linking back to Home, where the request is
// decided (#546, #677).
func TestARefusedLinkAcceptAnswersWithTheReason(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")

	resp := postForm(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/links/%d/accept", ts.URL, link.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	assertRefusalPage(t, page, "Can't accept link", "Only the parent's Owner may accept a link.", "/home")
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused accept, want none", got)
	}
}

// A link accept refused for making a cycle says so in a sentence a person can
// read, naming no Goal by its ID (#544), on a page inside the app linking back
// to Home, where the request is decided (#546, #677).
func TestALinkAcceptThatWouldMakeACycleSaysSoWithoutIDs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")
	// Pat links the other way round and Sam accepts, so accepting Sam's request
	// now would close a cycle.
	other := h.RequestLink(pat, parent, child, "")
	if _, err := h.Service.AcceptLink(context.Background(), other.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	resp := postForm(t, signInClient(t, ts.URL, "pat@example.com"), fmt.Sprintf("%s/links/%d/accept", ts.URL, link.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	assertRefusalPage(t, page, "Can't accept link", "Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”.", "/home")
	for _, id := range []int64{parent.ID, child.ID} {
		if reason := textOf(findByTestID(t, page, "refused-reason")); strings.Contains(reason, fmt.Sprint(id)) {
			t.Errorf("refused-reason %q names Goal %d by its ID", reason, id)
		}
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused accept, want none", got)
	}
}

// A link request refused for making a cycle says so in the form, in the
// sentence every cycle's refusal uses, naming the Goals by title, not ID
// (#546, #645).
func TestALinkRequestThatWouldMakeACycleSaysSoWithoutIDs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com") // owns the child
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	// Pat links the parent under the child and Sam accepts, so the child
	// asking to contribute to the parent would close a cycle.
	other := h.RequestLink(pat, parent, child, "")
	if _, err := h.Service.AcceptLink(context.Background(), other.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	resp := postForm(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/links", ts.URL, child.ID), url.Values{
		"parent_id": {fmt.Sprint(parent.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	got := textOf(findByTestID(t, page, "form-error"))
	if want := "Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”."; got != want {
		t.Errorf("form-error = %q, want %q", got, want)
	}
	for _, id := range []int64{parent.ID, child.ID} {
		if strings.Contains(got, fmt.Sprint(id)) {
			t.Errorf("form-error %q names Goal %d by its ID", got, id)
		}
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused request, want none", got)
	}
}

// A refused Parent suggestion decision or withdrawal is a page inside the app
// saying why, linking back to the page it came from: Home when its from says
// so, the Goal page otherwise. The suggestion stays open (#546).
func TestARefusedSuggestionActionShowsAPageLinkingBack(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, _, sug := suggestionScene(t, h)
	pat := signInClient(t, ts.URL, "pat@example.com") // made the suggestion
	sam := signInClient(t, ts.URL, "sam@example.com") // owns the Goal

	refusals := []struct {
		client  *http.Client
		action  string
		form    url.Values
		heading string
		reason  string
		back    string
	}{
		{pat, "decline", url.Values{"return": {"/home"}}, "Can't decline suggestion", "Only the Goal's Owner may decide a parent suggestion.", "/home"},
		{pat, "accept", url.Values{"return": {"/home"}}, "Can't accept suggestion", "Only the Goal's Owner may decide a parent suggestion.", "/home"},
		{pat, "decline", url.Values{}, "Can't decline suggestion", "Only the Goal's Owner may decide a parent suggestion.", fmt.Sprintf("/goals/%d", goal.ID)},
		{sam, "withdraw", url.Values{}, "Can't withdraw suggestion", "Only the person who made a suggestion may withdraw it.", fmt.Sprintf("/goals/%d", goal.ID)},
	}
	for _, c := range refusals {
		resp := postForm(t, c.client, fmt.Sprintf("%s/parent-suggestions/%d/%s", ts.URL, sug.ID, c.action), c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %v: status %d, want 403", c.action, c.form, resp.StatusCode)
		}
		assertRefusalPage(t, page, c.heading, c.reason, c.back)
	}
	if got, _ := h.Service.ParentSuggestion(context.Background(), sug.ID); got.Status != domain.SuggestionOpen {
		t.Errorf("Status = %q after refused actions, want open", got.Status)
	}
}

// A decision on a link that isn't there, never made or since rejected,
// answers with the app's Not found page.
func TestALinkDecisionOnNoLinkIsTheNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	patAcc := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	rejected := h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(patAcc, "Reduce outages", "Outages cost trust."), "")
	if _, err := h.Service.RejectLink(context.Background(), rejected.ID, patAcc.ID); err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, linkID := range []int64{999999, rejected.ID} {
		for _, decision := range []string{"accept", "reject"} {
			resp := postForm(t, pat, fmt.Sprintf("%s/links/%d/%s", ts.URL, linkID, decision), url.Values{})
			defer func() { _ = resp.Body.Close() }()
			page := parsePage(t, readBody(t, resp))
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s link %d: status %d, want 404", decision, linkID, resp.StatusCode)
			}
			if !hasTestID(page, "not-found") {
				t.Errorf("%s link %d: not the Not found page:\n%s", decision, linkID, renderNode(page))
			}
		}
	}
}

// Accepting a link request a second time, as a stale second click would, or
// rejecting one already accepted, is refused with a page inside the app
// saying it was already accepted, linking back to the page it came from (Home
// without one), and leaves the link in place (#549).
func TestADecisionOnAnAcceptedLinkSaysItWasAccepted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	patAcc := h.SignIn("pat@example.com") // owns the parent
	sam := h.SignIn("sam@example.com")    // owns the child
	parent := h.CreateGoal(patAcc, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")
	pat := signInClient(t, ts.URL, "pat@example.com")
	if status := postFormStatus(t, pat, fmt.Sprintf("%s/links/%d/accept", ts.URL, link.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("first accept: status %d", status)
	}

	decisions := []struct {
		decision, heading string
		form              url.Values
		back              string
	}{
		{"accept", "Can't accept link", url.Values{}, "/home"},
		{"accept", "Can't accept link", url.Values{"return": {"/home"}}, "/home"},
		{"reject", "Can't reject link", url.Values{}, "/home"},
		{"reject", "Can't reject link", url.Values{"return": {"/home"}}, "/home"},
	}
	for _, d := range decisions {
		resp := postForm(t, pat, fmt.Sprintf("%s/links/%d/%s", ts.URL, link.ID, d.decision), d.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s %v: status %d, want %d", d.decision, d.form, resp.StatusCode, http.StatusUnprocessableEntity)
		}
		assertRefusalPage(t, page, d.heading, "This link request has already been accepted.", d.back)
	}
	if got := h.ParentsOf(child); len(got) != 1 || got[0].ID != parent.ID {
		t.Errorf("ParentsOf(child) = %+v after the refused decisions, want %q still", got, parent.Title)
	}
}

// homeLinkAcceptForm is the Accept form on Pat's Home for the pending link
// request putting child under a parent of Pat's, failing the test unless
// there is one.
func homeLinkAcceptForm(t *testing.T, pat *http.Client, baseURL string, child domain.Goal, link domain.Link) *html.Node {
	t.Helper()
	row := homeRowIn(t, findByTestID(t, getPage(t, pat, baseURL+"/home"), "home-requests"), child)
	accept := homeFormTo(row, fmt.Sprintf("/links/%d/accept", link.ID))
	if accept == nil || buttonNamed(accept, "Accept") == nil {
		t.Fatalf("Home's row for %q has no Accept for link %d:\n%s", child.Title, link.ID, renderNode(row))
	}
	return accept
}

// Accepting a link request on Home returns to Home, and an Accept refused
// there, for making a cycle, links Back to Home; one posted with no return
// lands on Home too (#549, #677).
func TestALinkAcceptReturnsToThePageItCameFrom(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	patAcc := h.SignIn("pat@example.com") // owns the parents
	sam := h.SignIn("sam@example.com")    // owns the children
	pat := signInClient(t, ts.URL, "pat@example.com")

	parent := h.CreateGoal(patAcc, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")
	resp := postForm(t, pat, ts.URL+fmt.Sprintf("/links/%d/accept", link.ID), formSubmission(homeLinkAcceptForm(t, pat, ts.URL, child, link)))
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Errorf("Accept on Home: status %d at %s, want 200 at /home", resp.StatusCode, resp.Request.URL.Path)
	}
	if got := h.ParentsOf(child); len(got) != 1 || got[0].ID != parent.ID {
		t.Errorf("ParentsOf(child) = %+v after Accept on Home, want %q", got, parent.Title)
	}

	// Pat links a Goal of Sam's under one of Pat's own and Sam accepts, so
	// accepting Sam's request the other way round would close a cycle.
	upper := h.CreateGoal(patAcc, "Cut costs", "Spend less.")
	lower := h.CreateGoal(sam, "Renegotiate contracts", "Vendors overcharge.")
	cyclic := h.RequestLink(sam, lower, upper, "")
	other := h.RequestLink(patAcc, upper, lower, "")
	if _, err := h.Service.AcceptLink(context.Background(), other.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	resp = postForm(t, pat, ts.URL+fmt.Sprintf("/links/%d/accept", cyclic.ID), formSubmission(homeLinkAcceptForm(t, pat, ts.URL, lower, cyclic)))
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("refused Accept on Home: status %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	assertRefusalPage(t, page, "Can't accept link", "Linking these Goals would make a cycle: “Cut costs” already contributes to “Renegotiate contracts”.", "/home")

	third := h.CreateGoal(sam, "Replace cabling", "Cables fray.")
	noReturn := h.RequestLink(sam, third, parent, "")
	resp = postForm(t, pat, ts.URL+fmt.Sprintf("/links/%d/accept", noReturn.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Errorf("Accept without from: status %d at %s, want 200 at /home", resp.StatusCode, resp.Request.URL.Path)
	}
}

// The Goal page's Link to a parent Goal picks the parent with the Parent Goal
// picker, opening with nothing picked: its search, the open form's focus,
// asks for the Goals the Goal could contribute to, which leave out those it
// already does (#656).
func TestLinkToAParentGoalOpensWithNoParentPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	child := h.CreateGoal(sam, "Site speed", "Shoppers abandon slow pages.")
	linked := h.CreateGoal(sam, "Site revenue", "The business needs it.")
	free := h.CreateGoal(sam, "Site trust", "Shoppers doubt us.")
	h.RequestLink(sam, child, linked, "")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, goalPageURL(ts.URL, child)+"?open=parent-link")
	picker := assertParentPickerOpensEmpty(t, findByTestID(t, page, "request-link"))
	search := findAll(picker, func(n *html.Node) bool { return attrOf(n, "id") == "parent-search" })[0]
	if want := fmt.Sprintf("/goals/search?goal=%d", child.ID); attrOf(search, "hx-get") != want {
		t.Errorf("the picker searches %q, want %q", attrOf(search, "hx-get"), want)
	}
	if !hasAttr(search, "autofocus") {
		t.Errorf("the picker's search doesn't take the open form's focus")
	}
	results := findAllByTestID(getPage(t, client, ts.URL+attrOf(search, "hx-get")+"&q=site"), "parent-result")
	if len(results) != 1 || !strings.Contains(textOf(results[0]), free.Title) {
		t.Errorf("the search found %q, want only %q", textsOf(results), free.Title)
	}
}

// Request link pressed with no parent picked links nothing: the Goal page
// comes back, 422, with the form open and the reason beside its picker,
// which marks its controls refused, the note as typed and still nothing
// picked (#656).
func TestRequestingALinkWithNoParentPickedIsRefusedBesideThePicker(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	child := h.CreateGoal(sam, "Site speed", "Shoppers abandon slow pages.")
	h.CreateGoal(sam, "Site revenue", "The business needs it.")
	client := signInClient(t, ts.URL, "sam@example.com")

	for name, sent := range map[string]url.Values{
		"no parent_id":    {"note": {"Because."}},
		"empty parent_id": {"parent_id": {""}, "note": {"Because."}},
		"not an id":       {"parent_id": {"soon"}, "note": {"Because."}},
	} {
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/links", ts.URL, child.ID), sent)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, resp.StatusCode)
		}
		if hasTestID(page, "form-error") {
			t.Errorf("%s: the refusal is above the form, not beside the picker: %s", name, textOf(findByTestID(t, page, "form-error")))
		}
		form := findByTestID(t, page, "request-link")
		assertPickerRefused(t, assertParentPickerOpensEmpty(t, form), "A parent Goal is required.")
		if note := formField(t, form, "note").Value; note != "Because." {
			t.Errorf("%s: the note comes back %q, want it as typed", name, note)
		}
	}
	if parents := h.ParentsOf(child); len(parents) != 0 {
		t.Errorf("the Goal contributes to %+v, want none", parents)
	}
}

// A link request refused for its parent, missing or making a cycle, keeps its
// status, 422 or 409, whether or not the Goal page offers the viewer the link
// form: offered, the reason is beside the form, a missing parent beside its
// picker (#656); not offered, it is the refusal page, linking Back to the
// Goal (#573).
func TestARefusedLinkRequestKeepsItsStatusWhenTheFormIsNotOffered(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service)
	srv.HandleRefusedLinkCycleForTest("POST /link-cycle-test/{id}")
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	sam := h.SignIn("sam@example.com") // owns the child
	h.SignIn("pat@example.com")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	back := fmt.Sprintf("/goals/%d", child.ID)

	for _, c := range []struct {
		name, who, path string
		form            url.Values
		status          int
		reason          string
		offered         bool
		// shownIn is where an offered form says why: beside the picker
		// (input-error) or above the form (form-error).
		shownIn string
	}{
		{"missing parent, offered", "sam@example.com", back + "/links", url.Values{"parent_id": {""}}, http.StatusUnprocessableEntity, "A parent Goal is required.", true, "input-error"},
		{"missing parent, not offered", "pat@example.com", back + "/links", url.Values{"parent_id": {""}}, http.StatusUnprocessableEntity, "A parent Goal is required.", false, ""},
		{"cycle, offered", "sam@example.com", fmt.Sprintf("/link-cycle-test/%d", child.ID), nil, http.StatusConflict,
			"Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”.", true, "form-error"},
		{"cycle, not offered", "pat@example.com", fmt.Sprintf("/link-cycle-test/%d", child.ID), nil, http.StatusConflict,
			"Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”.", false, ""},
	} {
		resp := postForm(t, signInClient(t, ts.URL, c.who), ts.URL+c.path, c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		if !c.offered {
			assertRefusalPage(t, page, "Can't change Goal", c.reason, back)
			continue
		}
		if got := textOf(findByTestID(t, page, c.shownIn)); got != c.reason {
			t.Errorf("%s: %s = %q, want %q", c.name, c.shownIn, got, c.reason)
		}
	}
}
