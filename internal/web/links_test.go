package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
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
	if parents := pageElement(t, childBody, "section", "goal-parents"); !strings.Contains(parents, navTo(parent.ID)) {
		t.Errorf("child page does not link the accepted parent:\n%s", parents)
	}
	if inbox := getBody(t, patClient, ts.URL+"/links"); !strings.Contains(inbox, "No pending requests") {
		t.Errorf("inbox not empty after accept; body:\n%s", inbox)
	}
}

// Each pending link request is a hairline row with Accept as the primary
// action and a Reject that rejects it at once, with no confirmation (#56). The
// page sits in the flat frame: its name in the header strip, and no card.
func TestPendingLinkRowsRejectAtOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	link := h.RequestLink(sam, h.CreateGoal(sam, "Migrate displays", "Displays fail often."), h.CreateGoal(pat, "Reduce outages", "Outages cost trust."), "")

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/links")

	row := between(t, page, `data-testid="pending-link"`, "")
	assertInFlatFrame(t, page, "Pending links")
	base := fmt.Sprintf("/links/%d", link.ID)
	assertAcceptReject(t, row, base)

	client := signInClient(t, ts.URL, "pat@example.com")
	if page := postFormBody(t, client, ts.URL+base+"/reject", url.Values{}); !strings.Contains(page, `data-testid="no-pending-links"`) {
		t.Errorf("the link request is still pending after Reject:\n%s", page)
	}
}

// A pending link's note is set apart by its indent and a 1px neutral rule, not
// a coloured side stripe (#67).
func TestPendingLinkNoteIsSetApartByANeutralRule(t *testing.T) {
	t.Parallel()

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
// Accept as the primary button and a button named Reject that submits at once.
func assertAcceptReject(t *testing.T, row, base string) {
	t.Helper()
	if accept := between(t, row, `action="`+base+`/accept"`, "</form>"); !strings.Contains(accept, `class="btn primary`) {
		t.Errorf("Accept isn't the primary button:\n%s", accept)
	}
	rejects := findAll(parsePage(t, row), func(n *html.Node) bool { return n.Data == "form" && attrOf(n, "action") == base+"/reject" })
	if len(rejects) != 1 || buttonNamed(rejects[0], "Reject") == nil {
		t.Errorf("the row offers no button named Reject posting to %s/reject:\n%s", base, row)
	}
	assertSubmitsAtOnce(t, "Reject", row, base+"/reject")
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
	defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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

			if later := getBody(t, pat, ts.URL+from); strings.Contains(later, `data-testid="toast"`) {
				t.Errorf("toast shown again on a later visit:\n%s", later)
			}

			resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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
		child, parent, pat, landed := rejectRequest(t, h, ts, "/links")
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
		child, parent, pat, landed := rejectRequest(t, h, ts, "/links")
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
	pageElement(t, body, "a", "nav-home")
	if got, want := pageElement(t, body, "p", "link-refused"), "Only an Owner of the linked Goals may remove the link."; !strings.Contains(got, want) {
		t.Errorf("link-refused = %s, want %q", got, want)
	}
	if want := fmt.Sprintf(`<a href="/goals/%d">Back</a>`, parent.ID); !strings.Contains(body, want) {
		t.Errorf("refusal page has no %s:\n%s", want, body)
	}
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
// authorized:" prefix, and linking back to the pending links (#546).
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
	assertLinkRefusedPage(t, page, "Can't accept link", "Only the parent's Owner may accept a link.", "/links")
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused accept, want none", got)
	}
}

// A link accept refused for making a cycle says so in a sentence a person can
// read, naming no Goal by its ID (#544), on a page inside the app linking back
// to the pending links (#546).
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
	assertLinkRefusedPage(t, page, "Can't accept link", "Linking these Goals would make a cycle: the parent already contributes to the child.", "/links")
	for _, id := range []int64{parent.ID, child.ID} {
		if reason := textOf(findByTestID(t, page, "link-refused")); strings.Contains(reason, fmt.Sprint(id)) {
			t.Errorf("link-refused %q names Goal %d by its ID", reason, id)
		}
	}
	if got := h.ParentsOf(child); len(got) != 0 {
		t.Errorf("ParentsOf(child) = %+v after a refused accept, want none", got)
	}
}

// A link request refused for making a cycle says so in the form, in the
// sentence Suggest a parent uses, naming no Goal by its ID (#546).
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
	if want := "That Goal already contributes to this one, so linking them would make a cycle."; got != want {
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
		{pat, "decline", url.Values{"from": {"home"}}, "Can't decline suggestion", "Only the Goal's Owner may decide a parent suggestion.", "/home"},
		{pat, "accept", url.Values{"from": {"home"}}, "Can't accept suggestion", "Only the Goal's Owner may decide a parent suggestion.", "/home"},
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
		assertLinkRefusedPage(t, page, c.heading, c.reason, c.back)
	}
	if got, _ := h.Service.ParentSuggestion(context.Background(), sug.ID); got.Status != domain.SuggestionOpen {
		t.Errorf("Status = %q after refused actions, want open", got.Status)
	}
}

// A decision on a link that isn't there answers with the app's Not found page.
func TestALinkDecisionOnNoLinkIsTheNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, decision := range []string{"accept", "reject"} {
		resp := postForm(t, pat, fmt.Sprintf("%s/links/999999/%s", ts.URL, decision), url.Values{})
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", decision, resp.StatusCode)
		}
		if !hasTestID(page, "not-found") {
			t.Errorf("%s: not the Not found page:\n%s", decision, renderNode(page))
		}
	}
}

// assertLinkRefusedPage checks page is a refused link action's page inside
// the app: heading names what was refused, reason says why, and Back links to
// back.
func assertLinkRefusedPage(t *testing.T, page *html.Node, heading, reason, back string) {
	t.Helper()
	if !hasTestID(page, "nav-home") {
		t.Errorf("refusal page is not inside the app's layout:\n%s", renderNode(page))
	}
	if headingNamed(page, heading) == nil {
		t.Errorf("refusal page has no heading %q:\n%s", heading, renderNode(page))
	}
	if got := textOf(findByTestID(t, page, "link-refused")); got != reason {
		t.Errorf("link-refused = %q, want %q", got, reason)
	}
	if backs := findAll(page, func(n *html.Node) bool {
		return n.Data == "a" && attrOf(n, "href") == back && textOf(n) == "Back"
	}); len(backs) == 0 {
		t.Errorf("refusal page has no Back link to %s:\n%s", back, renderNode(page))
	}
}
