package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	nethtml "golang.org/x/net/html"
)

// An Owner starts a Handoff from their Goal page; the proposed new Owner sees it
// in their inbox and accepts it, and only then does the Goal page show the new
// Owner.
func TestHandoffStartAcceptFlow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	samClient := signInClient(t, ts.URL, "sam@example.com")
	patClient := signInClient(t, ts.URL, "pat@example.com")

	// Sam hands the Goal to Pat by email.
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/handoff", ts.URL, goal.ID), url.Values{
		"to_email": {"pat@example.com"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start handoff: status %d", resp.StatusCode)
	}

	// Ownership has not moved: the Goal page still shows Sam.
	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(page, "sam@example.com") {
		t.Errorf("Goal page should still show Sam before acceptance; body:\n%s", page)
	}

	// Pat sees the pending Handoff in their inbox.
	inbox := getBody(t, patClient, ts.URL+"/handoffs")
	if !strings.Contains(inbox, "Reduce outages") {
		t.Errorf("Pat's handoff inbox missing the Goal; body:\n%s", inbox)
	}

	// Pat accepts it.
	pending, err := h.Service.PendingHandoffs(context.Background(), pat.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingHandoffs = %+v, %v", pending, err)
	}
	_ = resp.Body.Close()
	resp = postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, pending[0].ID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept handoff: status %d", resp.StatusCode)
	}

	// Now the Goal page shows Pat as Owner, and Pat's inbox is empty.
	page = getBody(t, patClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("pat@example.com", "pat")) {
		t.Errorf("Goal page should show Pat as Owner after acceptance; body:\n%s", page)
	}
	if inbox := getBody(t, patClient, ts.URL+"/handoffs"); !strings.Contains(inbox, "No pending handoffs") {
		t.Errorf("inbox not empty after accept; body:\n%s", inbox)
	}
}

// An Admin marks a Goal's departed Owner; the Goal shows Ownerless prominently,
// the Ownerless child is surfaced on its parent's page, and the Admin reassigns
// it to a present Owner, clearing Ownerless and recording the Reassign in the
// Goal's Ownership history.
func TestDepartedGoalOwnerlessSurfacedAndReassigned(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)

	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	parent := h.CreateGoal(boss, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	// Link the child under the Admin's parent so it appears on the parent page.
	link := h.RequestLink(sam, child, parent, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, boss.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	bossClient := signInClient(t, ts.URL, "boss@example.com")

	// Admin marks Sam departed.
	resp := postForm(t, bossClient, fmt.Sprintf("%s/accounts/%d/depart", ts.URL, sam.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark departed: status %d", resp.StatusCode)
	}

	// The child Goal page shows Ownerless prominently.
	childPage := getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if !strings.Contains(childPage, `data-testid="goal-ownerless"`) {
		t.Errorf("Ownerless Goal page missing the Ownerless banner; body:\n%s", childPage)
	}

	// The parent Owner's page surfaces the Ownerless child.
	parentPage := getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID))
	if !strings.Contains(parentPage, `data-testid="ownerless"`) {
		t.Errorf("parent page does not surface the Ownerless child; body:\n%s", parentPage)
	}
	_ = resp.Body.Close()

	// Admin reassigns the Ownerless child to Pat.
	resp = postForm(t, bossClient, fmt.Sprintf("%s/goals/%d/reassign", ts.URL, child.ID), url.Values{
		"email": {"pat@example.com"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reassign: status %d", resp.StatusCode)
	}
	childPage = getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if strings.Contains(childPage, `data-testid="goal-ownerless"`) {
		t.Errorf("Goal still shows Ownerless after reassignment; body:\n%s", childPage)
	}
	if !strings.Contains(childPage, `data-testid="goal-owner">`+shownAs("pat@example.com", "pat")) {
		t.Errorf("Goal page should show Pat as Owner after reassignment; body:\n%s", childPage)
	}

	// The Reassign is on the Goal's History timeline, under Ownership.
	if chip := historyChips(t, historyBlock(t, childPage))["Ownership"]; chip.count != "1" {
		t.Errorf("the Ownership chip counts %q, want the one Reassign", chip.count)
	}
	entries := ownershipEntries(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, child.ID))
	if len(entries) != 1 {
		t.Fatalf("History lists %d ownership changes, want the one Reassign", len(entries))
	}
	entry := entries[0]
	if !strings.Contains(entry, ">Reassign<") {
		t.Errorf("the Reassign entry isn't labelled Reassign: %s", entry)
	}
	for _, part := range []string{shownAs("sam@example.com", "sam"), shownAs("pat@example.com", "pat"), "reassigned by an Admin", "started by " + shownAs("boss@example.com", "boss")} {
		if !strings.Contains(entry, part) {
			t.Errorf("Reassign entry lacks %q: %s", part, entry)
		}
	}
	_ = pat
}

// Each pending Handoff is a hairline row with Accept as the primary action and
// a Reject that rejects it at once, with no confirmation (#56). The page sits
// in the flat frame: its name in the header strip, and no card.
func TestPendingHandoffRowsRejectAtOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/handoffs")

	row := between(t, page, `data-testid="pending-handoff"`, "")
	assertInFlatFrame(t, page, "Pending handoffs")
	base := fmt.Sprintf("/handoffs/%d", ho.ID)
	assertAcceptReject(t, row, base)

	client := signInClient(t, ts.URL, "pat@example.com")
	if page := postFormBody(t, client, ts.URL+base+"/reject", url.Values{}); !strings.Contains(page, `data-testid="no-pending-handoffs"`) {
		t.Errorf("the Handoff is still pending after Reject:\n%s", page)
	}
}

// Every Handoff is kept with its outcome and listed newest first on the Goal
// page's History timeline under the Ownership chip: one rejected by the new Owner, one
// cancelled when its new Owner departed, and one the new Owner accepted after
// the Owner who started it departed — which ends the Goal's Ownerless state.
func TestOwnershipHistoryKeepsEveryHandoffOutcome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	mel := h.SignIn("mel@example.com")
	pat := h.SignIn("pat@example.com")
	lee := h.SignIn("lee@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	bossClient := signInClient(t, ts.URL, "boss@example.com")
	handTo := func(email string) int64 {
		t.Helper()
		if status := postFormStatus(t, samClient, goalURL+"/handoff", url.Values{"to_email": {email}}); status != http.StatusOK {
			t.Fatalf("start handoff to %s: status %d", email, status)
		}
		history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
		if err != nil || len(history) == 0 {
			t.Fatalf("OwnershipHistory = %+v, %v", history, err)
		}
		h.Clock.Advance(time.Hour)
		return history[len(history)-1].ID
	}

	// Mel rejects the first Handoff; the Goal stays Sam's.
	rejected := handTo(mel.Email)
	if status := postFormStatus(t, signInClient(t, ts.URL, mel.Email), fmt.Sprintf("%s/handoffs/%d/reject", ts.URL, rejected), url.Values{}); status != http.StatusOK {
		t.Fatalf("reject: status %d", status)
	}

	// Pat departs before deciding, which cancels the Handoff to Pat.
	cancelled := handTo(pat.Email)
	if status := postFormStatus(t, bossClient, fmt.Sprintf("%s/accounts/%d/depart", ts.URL, pat.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("depart Pat: status %d", status)
	}
	// Pat can no longer sign in to accept it, and the Handoff stays cancelled.
	if _, err := h.Service.AcceptHandoff(context.Background(), cancelled, pat.ID, nil); err == nil {
		t.Errorf("a cancelled Handoff was accepted")
	}

	// Sam departs with a Handoff to Lee pending; Lee still accepts it.
	handTo(lee.Email)
	if status := postFormStatus(t, bossClient, fmt.Sprintf("%s/accounts/%d/depart", ts.URL, sam.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("depart Sam: status %d", status)
	}
	leeClient := signInClient(t, ts.URL, lee.Email)
	inbox := getBody(t, leeClient, ts.URL+"/handoffs")
	ho := between(t, inbox, `data-testid="pending-handoff"`, "")
	if status := postFormStatus(t, leeClient, ts.URL+between(t, ho, `/handoffs/`, `/accept`)+"/accept", url.Values{}); status != http.StatusOK {
		t.Fatalf("accept: status %d", status)
	}

	page := getBody(t, bossClient, goalURL)
	if strings.Contains(page, `data-testid="goal-ownerless"`) {
		t.Errorf("Goal still Ownerless after Lee accepted the Handoff")
	}
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("lee@example.com", "lee")) {
		t.Errorf("Goal page should show Lee as Owner")
	}

	if chip := historyChips(t, historyBlock(t, page))["Ownership"]; chip.count != "3" {
		t.Errorf("the Ownership chip counts %q, want 3", chip.count)
	}
	entries := ownershipEntries(t, bossClient, goalURL)
	want := []struct{ to, outcome, at string }{
		{shownAs("lee@example.com", "lee"), "accepted", "Jan 2, 17:04"},
		{shownAs("pat@example.com", "pat"), "— cancelled by " + shownAs("boss@example.com", "boss"), "Jan 2, 16:04"},
		{shownAs("mel@example.com", "mel"), "rejected", "Jan 2, 15:04"},
	}
	if len(entries) != len(want) {
		t.Fatalf("History lists %d ownership changes, want %d:\n%s", len(entries), len(want), strings.Join(entries, "\n"))
	}
	for i, w := range want {
		for _, part := range []string{shownAs("sam@example.com", "sam"), w.to, w.outcome, w.at, "started by " + shownAs("sam@example.com", "sam")} {
			if !strings.Contains(entries[i], part) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
		}
	}
}

// An Admin marks a Departed Owner returned: their remaining Goal is no longer
// Ownerless, they can sign in again and check in as a Delegate, and a Goal
// reassigned while they were away stays with its new Owner (CONTEXT.md:
// Departed).
func TestAdminMarksDepartedOwnerReturned(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	kept := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	moved := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	delegated := h.ActiveGoal(pat, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(pat, sam, delegated.ID)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if _, err := h.Service.ReassignGoal(t.Context(), boss.ID, moved.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}
	bossClient := signInClient(t, ts.URL, "boss@example.com")

	if status := postFormStatus(t, bossClient, fmt.Sprintf("%s/accounts/%d/return", ts.URL, sam.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("mark returned: status %d", status)
	}

	page := getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, kept.ID))
	if strings.Contains(page, `data-testid="goal-ownerless"`) {
		t.Errorf("Sam's Goal is still Ownerless after Sam returned")
	}
	page = getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, moved.ID))
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("pat@example.com", "pat")) {
		t.Errorf("the Goal reassigned to Pat while Sam was away no longer shows Pat as Owner")
	}

	samClient := signInClient(t, ts.URL, "sam@example.com")
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, delegated.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Checked in for Pat."},
	})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("returned Delegate's Check-in: status %d", resp.StatusCode)
	}
	if latest, ok, err := h.Service.LatestCheckin(t.Context(), delegated.ID); err != nil || !ok || latest.Author.ID != sam.ID {
		t.Errorf("LatestCheckin = %+v, %v, %v; want Sam's Check-in as Delegate", latest, ok, err)
	}
}

// A Hand off or Reassign from someone who may not, naming a person who has an
// Account, is refused on the refusal page, 403, saying who may, with a way
// Back to the Goal.
func TestHandoffAndReassignByOneWhoMayNotAnswerTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	leo := h.SignIn("leo@example.com")
	h.SignIn("pat@example.com")
	owned := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ownerless := h.CreateGoal(leo, "Migrate displays", "Displays fail often.")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, leo.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, c := range []struct {
		name, path, field, heading, reason string
		goal                               domain.Goal
	}{
		{"Hand off", "handoff", "to_email", "Can't hand off Goal", "Only the Goal's Owner or an Admin may start a Handoff.", owned},
		{"Reassign", "reassign", "email", "Can't reassign Goal", "Only an Admin may do this.", ownerless},
	} {
		resp := postForm(t, pat, fmt.Sprintf("%s/goals/%d/%s", ts.URL, c.goal.ID, c.path), url.Values{c.field: {"pat@example.com"}})
		page := parsePage(t, readBody(t, resp))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s by someone who may not: status %d, want 403", c.name, resp.StatusCode)
		}
		assertRefusalPage(t, page, c.heading, c.reason, fmt.Sprintf("/goals/%d", c.goal.ID))
	}
	if pending, _ := h.Service.PendingHandoffs(t.Context(), h.SignIn("pat@example.com").ID); len(pending) != 0 {
		t.Errorf("pending Handoffs to Pat = %+v, want none", pending)
	}
}

// An Admin's Hand off of an Ownerless Goal, or Reassign of an owned one, is
// refused where the Goal page offers no such form, so the refusal page says
// why, 422, headed by what was refused, with a way Back to the Goal.
func TestHandoffAndReassignTheGoalPageDoesNotOfferAnswerTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	leo := h.SignIn("leo@example.com")
	h.SignIn("pat@example.com")
	owned := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ownerless := h.CreateGoal(leo, "Migrate displays", "Displays fail often.")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, leo.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	bossClient := signInClient(t, ts.URL, "boss@example.com")

	for _, c := range []struct {
		name, path, field, heading, reason string
		goal                               domain.Goal
	}{
		{"Hand off of an Ownerless Goal", "handoff", "to_email", "Can't hand off Goal", "This Goal is Ownerless: reassign it instead.", ownerless},
		{"Reassign of an owned Goal", "reassign", "email", "Can't reassign Goal", "Only an Ownerless Goal is reassigned; hand off a Goal with a present Owner.", owned},
	} {
		resp := postForm(t, bossClient, fmt.Sprintf("%s/goals/%d/%s", ts.URL, c.goal.ID, c.path), url.Values{c.field: {"pat@example.com"}})
		page := parsePage(t, readBody(t, resp))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("an Admin's %s: status %d, want 422", c.name, resp.StatusCode)
		}
		assertRefusalPage(t, page, c.heading, c.reason, fmt.Sprintf("/goals/%d", c.goal.ID))
	}
}

// A Depart or Return that is refused answers the refusal page, linking Back to
// Home: 403 for someone who isn't an Admin, and 422 for an Admin's that can't
// be done to that person. A path that names no Account, whether it isn't a
// number or is one no Account has, is the Not found page.
func TestRefusedDepartAndReturnAnswerTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	leo := h.SignIn("leo@example.com")
	h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, leo.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	pat := signInClient(t, ts.URL, "pat@example.com")
	bossClient := signInClient(t, ts.URL, "boss@example.com")

	for _, c := range []struct {
		name    string
		client  *http.Client
		path    string
		status  int
		heading string
		reason  string
	}{
		{"a Depart by a non-Admin", pat, fmt.Sprintf("/accounts/%d/depart", sam.ID), http.StatusForbidden, "Can't mark departed", "Only an Admin may do this."},
		{"a Return by a non-Admin", pat, fmt.Sprintf("/accounts/%d/return", leo.ID), http.StatusForbidden, "Can't mark returned", "Only an Admin may do this."},
		{"a Depart of someone Departed", bossClient, fmt.Sprintf("/accounts/%d/depart", leo.ID), http.StatusUnprocessableEntity, "Can't mark departed", "Leo has already left the company."},
		{"a Return of someone not Departed", bossClient, fmt.Sprintf("/accounts/%d/return", sam.ID), http.StatusUnprocessableEntity, "Can't mark returned", "Sam hasn't left the company."},
	} {
		resp := postForm(t, c.client, ts.URL+c.path, url.Values{})
		page := parsePage(t, readBody(t, resp))
		_ = resp.Body.Close()
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		assertRefusalPage(t, page, c.heading, c.reason, "/home")
	}
	for _, action := range []string{"depart", "return"} {
		assertNotFoundPageFor(t, bossClient, http.MethodPost, ts.URL+"/accounts/sam/"+action, "/home")
		assertNotFoundPageFor(t, bossClient, http.MethodPost, ts.URL+"/accounts/999999/"+action, "/home")
	}
	if acc, err := h.Service.Account(t.Context(), sam.ID); err != nil || acc.Departed {
		t.Errorf("Sam after a refused Depart = %+v, %v; want still present", acc, err)
	}
	if acc, err := h.Service.Account(t.Context(), leo.ID); err != nil || !acc.Departed {
		t.Errorf("Leo after a refused Return = %+v, %v; want still Departed", acc, err)
	}
}

// Only an Admin may mark someone returned: anyone else is refused with 403 and
// the person stays Departed.
func TestNonAdminCannotMarkReturned(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	resp := postForm(t, signInClient(t, ts.URL, "pat@example.com"), fmt.Sprintf("%s/accounts/%d/return", ts.URL, sam.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin mark returned: status %d, want 403", resp.StatusCode)
	}
	if acc, err := h.Service.Account(t.Context(), sam.ID); err != nil || !acc.Departed {
		t.Errorf("Sam after a refused return = %+v, %v; want still Departed", acc, err)
	}
}

// Marking someone Departed or returned sends the Admin back to the page the
// form's return names, but only a path on this app: none, or one a browser
// would read as another site, goes to /goals. The Referer is never read, so
// one naming another page changes nothing.
func TestDepartAndReturnRedirectBackOnlyWithinTheApp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	h.SignIn("boss@example.com")
	bossClient := noRedirects(signInClient(t, ts.URL, "boss@example.com"))

	for i, tc := range []struct {
		name, back, want string
	}{
		{"the Admin page", "/admin", "/admin"},
		{"a Goal page with a query", "/goals/5?x=1", "/goals/5?x=1"},
		{"no return", "", "/goals"},
		{"another origin", "https://evil.example/goals/5", "/goals"},
		{"a path starting //", "//evil.example/x", "/goals"},
		{"a path with a backslash", `/\evil.example`, "/goals"},
		{"a path with a newline", "/goals/5\n", "/goals"},
	} {
		who := h.SignIn(fmt.Sprintf("leaver%d@example.com", i))
		depart := fmt.Sprintf("%s/accounts/%d/depart", ts.URL, who.ID)
		if got := markAccount(t, bossClient, depart, tc.back); got != tc.want {
			t.Errorf("depart with %s (return %q) redirected to %q, want %q", tc.name, tc.back, got, tc.want)
		}
		ret := fmt.Sprintf("%s/accounts/%d/return", ts.URL, who.ID)
		if got := markAccount(t, bossClient, ret, tc.back); got != tc.want {
			t.Errorf("return with %s (return %q) redirected to %q, want %q", tc.name, tc.back, got, tc.want)
		}
	}
}

// Marking a Goal's Owner departed or returned from the Goal page's People
// block comes back to that Goal's plain page, not the one with the form open.
func TestDepartAndReturnFromTheGoalPageComeBackToIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	client := noRedirects(signInClient(t, ts.URL, "ada@example.com"))

	for _, tc := range []struct {
		name, open, form string
		goal             domain.Goal
	}{
		{"Mark owner departed", "depart", "depart-owner", goal},
		{"Mark returned", "return", "return-owner", orphan},
	} {
		page := getPage(t, client, fmt.Sprintf("%s/goals/%d?open=%s", ts.URL, tc.goal.ID, tc.open))
		form := findByTestID(t, page, tc.form)
		resp := postForm(t, client, ts.URL+attrOf(form, "action"), formSubmission(form))
		_ = readBody(t, resp)
		_ = resp.Body.Close()
		if got, want := resp.Header.Get("Location"), fmt.Sprintf("/goals/%d", tc.goal.ID); resp.StatusCode != http.StatusSeeOther || got != want {
			t.Errorf("%s: status %d to %q, want 303 to %q", tc.name, resp.StatusCode, got, want)
		}
	}
}

// markAccount posts a Depart or Return to rawURL as client, with back as the
// form's return (none when empty) and a Referer naming another page, and
// returns where the response redirects.
func markAccount(t *testing.T, client *http.Client, rawURL, back string) string {
	t.Helper()
	form := url.Values{}
	if back != "" {
		form.Set("return", back)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://app.example/referer-page")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s: status %d, want 303", rawURL, resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// The accept form on the pending Handoffs page lists the Goal's Delegates who
// aren't Departed, each with a keep checkbox checked by default; accepting with
// one unchecked removes exactly that Delegate, as a plain form POST, and the
// Departed Delegate stays (CONTEXT.md: Delegate).
func TestAcceptHandoffFormChoosesDelegatesToKeep(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com") // kept
	bob := h.SignIn("bob@example.com") // unchecked
	dee := h.SignIn("dee@example.com") // Departed
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	for _, d := range []domain.Account{ann, bob, dee} {
		h.AddDelegate(sam, d, goal.ID)
	}
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, dee.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	patClient := signInClient(t, ts.URL, pat.Email)

	page := getBody(t, patClient, ts.URL+"/handoffs")
	form := between(t, page, fmt.Sprintf(`action="/handoffs/%d/accept"`, ho.ID), "</form>")
	for _, d := range []domain.Account{ann, bob} {
		if box := fmt.Sprintf(`<input type="checkbox" name="keep" value="%d" checked>`, d.ID); !strings.Contains(form, box) {
			t.Errorf("Delegate %s lacks a keep checkbox checked by default (%s):\n%s", d.Email, box, form)
		}
		if !strings.Contains(form, shownPlainAs(d.Email, strings.TrimSuffix(d.Email, "@example.com"))) {
			t.Errorf("accept form doesn't name Delegate %s:\n%s", d.Email, form)
		}
	}
	if nested := nestedControls(form); len(nested) > 0 {
		t.Errorf("accept form nests a control inside another: %q", nested)
	}
	if strings.Contains(form, fmt.Sprintf(`value="%d"`, dee.ID)) || strings.Contains(form, "dee@example.com") {
		t.Errorf("accept form offers the Departed Delegate:\n%s", form)
	}

	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{
		"keep": {fmt.Sprint(ann.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept handoff: status %d", resp.StatusCode)
	}

	goalPage := getBody(t, patClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(goalPage, `data-testid="goal-owner">`+shownAs("pat@example.com", "pat")) {
		t.Errorf("Goal page should show Pat as Owner after acceptance")
	}
	delegates := pageElement(t, goalPage, "ul", "delegate-list")
	if !strings.Contains(delegates, shownAs("ann@example.com", "ann")) {
		t.Errorf("kept Delegate Ann is gone: %s", delegates)
	}
	if strings.Contains(delegates, "bob@example.com") {
		t.Errorf("unchecked Delegate Bob is still a Delegate: %s", delegates)
	}
	if !strings.Contains(delegates, shownAs("dee@example.com", "dee")) || !strings.Contains(delegates, `data-testid="delegate-departed"`) {
		t.Errorf("Departed Delegate Dee isn't still shown as departed: %s", delegates)
	}
}

// delegateEmails returns the emails of the Goal's Delegates, failing the test on
// error.
func delegateEmails(t *testing.T, h *testsupport.Harness, goalID int64) []string {
	t.Helper()
	ds, err := h.Service.ListDelegates(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Email)
	}
	return out
}

// Posting the accept form with every box still checked keeps every Delegate; a
// Reject touches none of them.
func TestAcceptHandoffKeepingEveryBoxAndRejectLeaveDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	bob := h.SignIn("bob@example.com")
	kept := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	rejected := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	for _, g := range []int64{kept.ID, rejected.ID} {
		h.AddDelegate(sam, ann, g)
		h.AddDelegate(sam, bob, g)
	}
	patClient := signInClient(t, ts.URL, pat.Email)
	want := []string{"ann@example.com", "bob@example.com"}

	ho, err := h.Service.StartHandoffByEmail(context.Background(), kept.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if status := postFormStatus(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{
		"keep": {fmt.Sprint(ann.ID), fmt.Sprint(bob.ID)},
	}); status != http.StatusOK {
		t.Fatalf("accept handoff: status %d", status)
	}
	if got := delegateEmails(t, h, kept.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after accepting with every box checked = %v, want %v", got, want)
	}

	ho, err = h.Service.StartHandoffByEmail(context.Background(), rejected.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if status := postFormStatus(t, patClient, fmt.Sprintf("%s/handoffs/%d/reject", ts.URL, ho.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("reject handoff: status %d", status)
	}
	if got := delegateEmails(t, h, rejected.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after reject = %v, want %v", got, want)
	}
}

// Home's Requests leads to the same choice: a Handoff's Review goes to the
// accept form listing the Delegates to keep, each checked by default.
func TestHomeHandoffReviewListsDelegatesToKeep(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	client := signInClient(t, ts.URL, pat.Email)

	home := getBody(t, client, ts.URL+"/")
	row := between(t, home, `data-testid="home-request"`, "</li>")
	review := attr(tagAround(t, row, `data-testid="home-request-review"`), "href")
	form := between(t, getBody(t, client, ts.URL+review), fmt.Sprintf(`action="/handoffs/%d/accept"`, ho.ID), "</form>")
	if box := fmt.Sprintf(`<input type="checkbox" name="keep" value="%d" checked>`, ann.ID); !strings.Contains(form, box) {
		t.Errorf("the accept form Home's Review leads to lacks Ann's keep checkbox (%s):\n%s", box, form)
	}
}

// An Admin Reassign keeps every Delegate, present or Departed.
func TestReassignKeepsDelegates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	dee := h.SignIn("dee@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	h.AddDelegate(sam, dee, goal.ID)
	for _, gone := range []int64{sam.ID, dee.ID} {
		if err := h.Service.MarkDeparted(context.Background(), boss.ID, gone); err != nil {
			t.Fatalf("MarkDeparted: %v", err)
		}
	}

	if status := postFormStatus(t, signInClient(t, ts.URL, boss.Email), fmt.Sprintf("%s/goals/%d/reassign", ts.URL, goal.ID), url.Values{
		"email": {"pat@example.com"},
	}); status != http.StatusOK {
		t.Fatalf("reassign: status %d", status)
	}

	want := []string{"ann@example.com", "dee@example.com"}
	if got := delegateEmails(t, h, goal.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after reassign = %v, want %v", got, want)
	}
}

// When accepting fails part-way, the request fails and nothing changes: the
// Owner, the Delegates and the pending Handoff all stay as they were.
func TestFailedAcceptChangesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	// Fail the last step, after ownership has moved and Ann has been removed.
	h.FailWrites(testsupport.Update, "handoffs")
	patClient := signInClient(t, ts.URL, pat.Email)

	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("failed accept: status %d, want 500", resp.StatusCode)
	}

	page := getBody(t, patClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("sam@example.com", "sam")) {
		t.Errorf("Goal page should still show Sam as Owner after a failed accept")
	}
	if got := delegateEmails(t, h, goal.ID); !slices.Equal(got, []string{"ann@example.com"}) {
		t.Errorf("Delegates after a failed accept = %v, want Ann still there", got)
	}
	if inbox := getBody(t, patClient, ts.URL+"/handoffs"); !strings.Contains(inbox, fmt.Sprintf(`action="/handoffs/%d/accept"`, ho.ID)) {
		t.Errorf("the Handoff isn't still pending after a failed accept:\n%s", inbox)
	}
}

// A Handoff the Owner started before departing is cancelled when an Admin
// reassigns the Goal: its recipient's inbox no longer offers it, accepting it is
// refused, the Goal stays with the Owner the Admin chose, and the Ownership
// history shows the Handoff cancelled before the Reassign.
func TestReassignCancelsTheDepartedOwnersPendingHandoff(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	h.SignIn("cal@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	bossClient := signInClient(t, ts.URL, boss.Email)
	patClient := signInClient(t, ts.URL, pat.Email)

	if status := postFormStatus(t, bossClient, goalURL+"/reassign", url.Values{"email": {"cal@example.com"}}); status != http.StatusOK {
		t.Fatalf("reassign: status %d", status)
	}

	if inbox := getBody(t, patClient, ts.URL+"/handoffs"); !strings.Contains(inbox, "No pending handoffs") {
		t.Errorf("Pat's inbox still offers the cancelled Handoff:\n%s", inbox)
	}
	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	refused := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("accept the cancelled Handoff: status %d, want 422", resp.StatusCode)
	}
	assertRefusalPage(t, refused, "Can't accept Handoff", "This Handoff was cancelled.", "/handoffs")

	page := getBody(t, bossClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("cal@example.com", "cal")) {
		t.Errorf("Goal page should show Cal as Owner after Pat's refused accept")
	}
	entries := ownershipEntries(t, bossClient, goalURL)
	want := []struct{ to, outcome string }{
		{shownAs("cal@example.com", "cal"), "reassigned by an Admin"},
		{shownAs("pat@example.com", "pat"), "— cancelled by " + shownAs("boss@example.com", "boss")},
	}
	if len(entries) != len(want) {
		t.Fatalf("History lists %d ownership changes, want %d:\n%s", len(entries), len(want), strings.Join(entries, "\n"))
	}
	for i, w := range want {
		for _, part := range []string{shownAs("sam@example.com", "sam"), w.to, w.outcome} {
			if !strings.Contains(entries[i], part) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
		}
	}
}

// Accepting a Handoff a second time, as a stale second click would, or
// rejecting one already accepted, is refused with a page inside the app
// saying how it was decided, linking back to the page it came from (#551).
func TestADecisionOnADecidedHandoffSaysHowItWasDecided(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := h.StartHandoff(sam, goal.ID, pat)
	patClient := signInClient(t, ts.URL, pat.Email)
	if status := postFormStatus(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{}); status != http.StatusOK {
		t.Fatalf("first accept: status %d", status)
	}

	decisions := []struct {
		decision, heading string
		form              url.Values
		back              string
	}{
		{"accept", "Can't accept Handoff", url.Values{}, "/handoffs"},
		{"accept", "Can't accept Handoff", url.Values{"return": {"/home"}}, "/home"},
		{"reject", "Can't reject Handoff", url.Values{}, "/handoffs"},
		{"reject", "Can't reject Handoff", url.Values{"return": {"/home"}}, "/home"},
	}
	for _, d := range decisions {
		resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/%s", ts.URL, ho.ID, d.decision), d.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s %v: status %d, want %d", d.decision, d.form, resp.StatusCode, http.StatusUnprocessableEntity)
		}
		assertRefusalPage(t, page, d.heading, "This Handoff was already accepted.", d.back)
	}
	if g, _ := h.Service.ViewGoal(context.Background(), goal.ID); g.Owner.ID != pat.ID {
		t.Errorf("Owner = %d after the refused decisions, want Pat (%d) still", g.Owner.ID, pat.ID)
	}
}

// Accepting, rejecting or cancelling a Handoff that doesn't exist is the Not
// found page.
func TestADecisionOnAMissingHandoffShowsNotFoundPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("pat@example.com")
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, decision := range []string{"accept", "reject", "cancel"} {
		assertNotFoundPageFor(t, pat, http.MethodPost, fmt.Sprintf("%s/handoffs/999999/%s", ts.URL, decision), "/home")
	}
}

// A pending Handoff's accept and reject forms sit inline, and the accept form's
// keep-Delegates fieldset stacks its boxes, through classes rather than style
// attributes. Home offers only the Reject form for such a Handoff, beside
// Review, and it sits inline the same way.
func TestPendingHandoffLayoutComesFromClasses(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	ann := h.SignIn("ann@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, ann, goal.ID)
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	client := signInClient(t, ts.URL, pat.Email)
	css := getBody(t, client, ts.URL+"/static/app.css")

	page := getBody(t, client, ts.URL+"/handoffs")
	for _, action := range []string{"accept", "reject"} {
		form := tagAround(t, page, fmt.Sprintf(`action="/handoffs/%d/%s"`, ho.ID, action))
		assertStyledBy(t, form, css, "inline-form", "display:inline")
	}
	keep := tagAround(t, page, `data-testid="handoff-keep-delegates"`)
	assertStyledBy(t, keep, page, "handoff-keep", "border:0;padding:0;margin:0 0 8px;display:flex;flex-direction:column;gap:4px")

	home := getBody(t, client, ts.URL+"/home")
	assertStyledBy(t, tagAround(t, home, fmt.Sprintf(`action="/handoffs/%d/reject"`, ho.ID)), css, "inline-form", "display:inline")
}

// rejectHandoff arranges Sam handing his Goal to Pat and Pat rejecting the
// Handoff from the page at from ("/handoffs" or "/home"), returning the page
// Pat lands on.
func rejectHandoff(t *testing.T, h *testsupport.Harness, ts *httptest.Server, from string) (goal domain.Goal, ho domain.Handoff, pat *http.Client, landed string) {
	t.Helper()
	sam := h.SignIn("sam@example.com")
	patAcc := h.SignIn("pat@example.com")
	goal = h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, patAcc.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	pat = signInClient(t, ts.URL, "pat@example.com")
	form := url.Values{}
	if from == "/home" {
		action := fmt.Sprintf("/handoffs/%d/reject", ho.ID)
		requests := findByTestID(t, getPage(t, pat, ts.URL+"/home"), "home-requests")
		rejects := findAll(requests, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == action })
		if len(rejects) != 1 {
			t.Fatalf("Home offers %d Rejects posting to %s, want 1", len(rejects), action)
		}
		form = formSubmission(rejects[0])
		if got := form.Get("return"); got != "/home" {
			t.Fatalf("Home's Reject returns to %q, want /home: %s", got, renderNode(rejects[0]))
		}
	}
	resp := postForm(t, pat, fmt.Sprintf("%s/handoffs/%d/reject", ts.URL, ho.ID), form)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject: status %d", resp.StatusCode)
	}
	if resp.Request.URL.Path != from {
		t.Errorf("reject landed on %s, want %s, where it came from", resp.Request.URL.Path, from)
	}
	return goal, ho, pat, readBody(t, resp)
}

// After rejecting a Handoff, from the pending page or from Home, the page
// shown next carries a toast saying so with an Undo button, a plain form post;
// the toast isn't shown on a later visit, and Undo returns to the same page
// with the Handoff pending again, shown so in the Goal's ownership history.
func TestRejectingAHandoffOffersUndoOnce(t *testing.T) {
	t.Parallel()

	for _, from := range []string{"/handoffs", "/home"} {
		t.Run(from, func(t *testing.T) {
			h := testsupport.New(t)
			ts := newServer(t, h)
			goal, ho, pat, landed := rejectHandoff(t, h, ts, from)

			toast := pageElement(t, landed, "aside", "toast")
			for _, want := range []string{"Reduce outages", "sam", `method="post"`, "Undo"} {
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
			if page := readBody(t, resp); !strings.Contains(page, navTo(goal.ID)) {
				t.Errorf("the page doesn't list the restored Handoff:\n%s", page)
			}
			history, _ := h.Service.OwnershipHistory(context.Background(), goal.ID)
			if len(history) != 1 || history[0].ID != ho.ID || history[0].Status != domain.HandoffPending {
				t.Errorf("history = %+v, want the one Handoff, pending", history)
			}
		})
	}
}

// Undo for a rejected Handoff is refused for anyone but its rejecter, and
// refused a second time; a crafted Undo of a Handoff that was never rejected
// is refused too, and none of them changes the Handoff.
func TestUndoingAHandoffRejectionIsRefusedWhenItCantBeForged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, _, pat, landed := rejectHandoff(t, h, ts, "/handoffs")
	action := undoAction(t, landed)
	sam := signInClient(t, ts.URL, "sam@example.com")
	status := func() string {
		history, err := h.Service.OwnershipHistory(context.Background(), goal.ID)
		if err != nil || len(history) != 1 {
			t.Fatalf("OwnershipHistory = %+v, %v; want the one Handoff", history, err)
		}
		return history[0].Status
	}

	resp := postForm(t, sam, ts.URL+action, url.Values{})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Undo by the Owner: status %d, want 403", resp.StatusCode)
	}
	if got := status(); got != domain.HandoffRejected {
		t.Fatalf("status = %q after a refused Undo, want rejected", got)
	}

	other := h.CreateGoal(h.SignIn("sam@example.com"), "Migrate displays", "Displays fail often.")
	pending, err := h.Service.StartHandoffByEmail(context.Background(), other.ID, "pat@example.com", h.SignIn("sam@example.com").ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	resp = postForm(t, pat, fmt.Sprintf("%s/handoffs/%d/restore", ts.URL, pending.ID), url.Values{})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(body), "isn't rejected") {
		t.Errorf("Undo of a Handoff never rejected: status %d %q, want 422 saying it isn't rejected", resp.StatusCode, body)
	}
	resp = postForm(t, pat, ts.URL+"/handoffs/4242/restore", url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Undo of no Handoff: status %d, want 404", resp.StatusCode)
	}

	if status := postFormStatus(t, pat, ts.URL+action, toastFields(t, landed)); status != http.StatusOK {
		t.Fatalf("Undo by the rejecter: status %d", status)
	}
	resp = postForm(t, pat, ts.URL+action, toastFields(t, landed))
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(body), "isn't rejected") {
		t.Errorf("second Undo: status %d %q, want 422", resp.StatusCode, body)
	}
	if got := status(); got != domain.HandoffPending {
		t.Errorf("status = %q, want pending after the one Undo", got)
	}
}

// Undo is refused with a message, changing nothing, once the Goal has another
// pending Handoff, its Owner has changed, or the proposed Owner has Departed.
func TestUndoingAHandoffRejectionIsRefusedWhenItNoLongerFits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		since func(t *testing.T, h *testsupport.Harness, goal domain.Goal)
		says  string
	}{
		{"another pending", func(t *testing.T, h *testsupport.Harness, goal domain.Goal) {
			h.SignIn("mel@example.com")
			if _, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, "mel@example.com", goal.Owner.ID); err != nil {
				t.Fatalf("StartHandoff: %v", err)
			}
		}, "Another Handoff"},
		{"Owner changed", func(t *testing.T, h *testsupport.Harness, goal domain.Goal) {
			mel := h.SignIn("mel@example.com")
			ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, mel.Email, goal.Owner.ID)
			if err != nil {
				t.Fatalf("StartHandoff: %v", err)
			}
			if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, mel.ID, nil); err != nil {
				t.Fatalf("AcceptHandoff: %v", err)
			}
		}, "changed hands"},
		{"proposed Owner Departed", func(t *testing.T, h *testsupport.Harness, goal domain.Goal) {
			boss := h.SignIn("boss@example.com")
			if err := h.Service.MarkDeparted(context.Background(), boss.ID, h.SignIn("pat@example.com").ID); err != nil {
				t.Fatalf("MarkDeparted: %v", err)
			}
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			ts := newServer(t, h)
			goal, ho, pat, landed := rejectHandoff(t, h, ts, "/handoffs")
			action := undoAction(t, landed)
			tc.since(t, h, goal)

			resp := postForm(t, pat, ts.URL+action, toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
			body := readBody(t, resp)
			if tc.says == "" {
				// A Departed person's session ends with their departure, so
				// their Undo is sent to sign in; the domain refuses it too.
				if resp.Request.URL.Path != "/signin" {
					t.Errorf("Undo by a Departed person landed on %s, want /signin", resp.Request.URL.Path)
				}
			} else if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, tc.says) {
				t.Errorf("Undo: status %d %q, want 422 saying %q", resp.StatusCode, body, tc.says)
			}
			history, _ := h.Service.OwnershipHistory(context.Background(), goal.ID)
			if history[0].ID != ho.ID || history[0].Status != domain.HandoffRejected {
				t.Errorf("history = %+v, want the Handoff still rejected", history)
			}
		})
	}
}

// A Handoff from Sam to Pat is rejected; the Goal then passes from Sam to Mel
// and back to Sam. Pat's Undo from the toast is refused with a message, so the
// old Handoff doesn't come back for Pat to take the Goal.
func TestAHandoffRejectionUndoCantReviveItAfterLaterHandoffs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, ho, pat, landed := rejectHandoff(t, h, ts, "/handoffs")
	ctx := context.Background()
	sam, mel := h.SignIn("sam@example.com"), h.SignIn("mel@example.com")
	toMel, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: mel.ID, ActorID: sam.ID})
	if err != nil {
		t.Fatalf("StartHandoff to Mel: %v", err)
	}
	if _, err := h.Service.AcceptHandoff(ctx, toMel.ID, mel.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff to Mel: %v", err)
	}
	back, err := h.Service.StartHandoff(ctx, domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: sam.ID, ActorID: mel.ID})
	if err != nil {
		t.Fatalf("StartHandoff back to Sam: %v", err)
	}
	if _, err := h.Service.AcceptHandoff(ctx, back.ID, sam.ID, nil); err != nil {
		t.Fatalf("AcceptHandoff back to Sam: %v", err)
	}

	resp := postForm(t, pat, ts.URL+undoAction(t, landed), toastFields(t, landed))
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "handed off again") {
		t.Errorf("revived Undo: status %d %q, want 422 saying the Goal was handed off again", resp.StatusCode, body)
	}
	history, _ := h.Service.OwnershipHistory(ctx, goal.ID)
	if history[0].ID != ho.ID || history[0].Status != domain.HandoffRejected {
		t.Errorf("history = %+v, want the first Handoff still rejected", history)
	}
}

// A Handoff rejected before Undo tokens existed has none, so its Undo is
// refused with 403 and it stays rejected.
func TestAHandoffRejectedBeforeUndoTokensCantBeUndoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam, patAcc := h.SignIn("sam@example.com"), h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho, err := h.Service.StartHandoff(context.Background(), domain.StartHandoffInput{GoalID: goal.ID, ToOwnerID: patAcc.ID, ActorID: sam.ID})
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	// Rejected as it was before, leaving nothing but its status.
	if _, err := h.DB.ExecContext(t.Context(), `UPDATE handoffs SET status = 'rejected' WHERE id = ?`, ho.ID); err != nil {
		t.Fatalf("reject as before: %v", err)
	}
	pat := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, pat, fmt.Sprintf("%s/handoffs/%d/restore", ts.URL, ho.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Undo of an old rejection: status %d %q, want 403", resp.StatusCode, body)
	}
	if pending, _ := h.Service.PendingHandoffs(context.Background(), patAcc.ID); len(pending) != 0 {
		t.Errorf("pending = %+v, want none", pending)
	}
}

// A Goal with a pending Handoff says so under its Owner, to every viewer:
// "Handoff to <Name> pending". Whoever started it and an Admin are offered
// Cancel handoff beside it; a reader and the proposed new Owner aren't. A
// Goal with none pending says nothing of one.
func TestTheGoalPageShowsItsPendingHandoff(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	h.SignIn("rita@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	quiet := h.CreateGoal(sam, "Expand to the EU", "Customers asked.")
	h.StartHandoff(sam, goal.ID, pat)
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	for _, c := range []struct {
		who, email string
		cancels    bool
	}{
		{"the Owner who started it", "sam@example.com", true},
		{"an Admin", "boss@example.com", true},
		{"a reader", "rita@example.com", false},
		{"the proposed new Owner", "pat@example.com", false},
	} {
		page := parsePage(t, getBody(t, signInClient(t, ts.URL, c.email), goalURL))
		line := findByTestID(t, page, "goal-pending-handoff")
		if got := textOf(line); !strings.HasPrefix(got, "Handoff to Pat Lee pending") {
			t.Errorf("%s: the pending line reads %q, want it to start %q", c.who, got, "Handoff to Pat Lee pending")
		}
		if got := buttonNamed(line, "Cancel handoff") != nil; got != c.cancels {
			t.Errorf("%s: offered Cancel handoff = %v, want %v", c.who, got, c.cancels)
		}
	}

	page := parsePage(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, quiet.ID)))
	if hasTestID(page, "goal-pending-handoff") {
		t.Errorf("a Goal with no pending Handoff shows one:\n%s", textOf(findByTestID(t, page, "goal-pending-handoff")))
	}
}

// Cancel handoff on the Goal page cancels it and comes back to the Goal, which
// no longer shows it pending; its History names who cancelled it.
func TestCancellingAHandoffFromTheGoalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignInNamed("sam@example.com", "Sam Ito")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := h.StartHandoff(sam, goal.ID, pat)
	goalPath := fmt.Sprintf("/goals/%d", goal.ID)
	goalURL := ts.URL + goalPath
	client := signInClient(t, ts.URL, sam.Email)

	line := findByTestID(t, parsePage(t, getBody(t, client, goalURL)), "goal-pending-handoff")
	form := findAll(line, func(n *nethtml.Node) bool { return n.Data == "form" })
	if len(form) != 1 || attrOf(form[0], "action") != fmt.Sprintf("/handoffs/%d/cancel", ho.ID) || !strings.Contains(attrOf(form[0], "class"), "inline-form") {
		t.Fatalf("the pending line's forms = %d, want one inline form posting to /handoffs/%d/cancel:\n%s", len(form), ho.ID, renderNode(line))
	}
	if got := formField(t, form[0], "return").Value; got != goalPath {
		t.Errorf("Cancel handoff returns to %q, want %q", got, goalPath)
	}

	resp := postForm(t, client, fmt.Sprintf("%s/handoffs/%d/cancel", ts.URL, ho.ID), url.Values{"return": {goalPath}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != goalPath {
		t.Fatalf("cancel: status %d at %s, want 200 at the Goal page", resp.StatusCode, resp.Request.URL.Path)
	}
	if page := parsePage(t, readBody(t, resp)); hasTestID(page, "goal-pending-handoff") {
		t.Errorf("the Goal page still shows the cancelled Handoff pending")
	}
	entries := ownershipEntries(t, client, goalURL)
	if len(entries) != 1 || !strings.Contains(entries[0], "— cancelled by "+shownAs(sam.Email, "Sam Ito")) {
		t.Errorf("History's ownership entries = %q, want the one Handoff cancelled by Sam", entries)
	}
}

// Someone who may not cancel a Handoff, posting its Cancel anyway, gets the
// refusal page saying who may, and the Handoff stays pending.
func TestCancellingAHandoffByOneWhoMayNotAnswersTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	h.SignIn("rita@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ho := h.StartHandoff(sam, goal.ID, pat)

	resp := postForm(t, signInClient(t, ts.URL, "rita@example.com"), fmt.Sprintf("%s/handoffs/%d/cancel", ts.URL, ho.ID), url.Values{})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cancel by a reader: status %d, want 403", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, readBody(t, resp)), "Can't cancel Handoff", "Only the person who started a Handoff or an Admin may cancel it.", "/handoffs")
	if pending, _ := h.Service.PendingHandoffs(t.Context(), pat.ID); len(pending) != 1 {
		t.Errorf("pending for Pat after the refused cancel = %+v, want the one Handoff", pending)
	}
}

// A second Hand off while one is pending goes back to the Goal page with the
// form open, saying whom the pending one is to and how to clear it.
func TestASecondHandOffSaysWhichHandoffIsPending(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	h.SignIn("mel@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.StartHandoff(sam, goal.ID, pat)

	client := signInClient(t, ts.URL, sam.Email)
	back := fmt.Sprintf("/goals/%d", goal.ID)
	postRefusedBack(t, client, ts.URL+back+"/handoff", url.Values{"to_email": {"mel@example.com"}}, back)
	page := getPage(t, client, ts.URL+back)
	form := findByTestID(t, page, "handoff-goal")
	if form == nil {
		t.Fatal("the Hand off form isn't open")
	}
	if got, want := textOf(findByTestID(t, page, "form-error")), "A Handoff to Pat Lee is already pending; cancel it to start another."; got != want {
		t.Errorf("form-error = %q, want %q", got, want)
	}
	if buttonNamed(findByTestID(t, page, "goal-pending-handoff"), "Cancel handoff") == nil {
		t.Error("the page doesn't offer Cancel handoff for the pending Handoff")
	}
}

// Pending handoffs lists, after the Handoffs to the viewer, those they
// started that wait on the new Owner, each linking its Goal and offering
// Cancel handoff, which comes back to the page without it. Someone who
// started none sees no such list.
func TestPendingHandoffsListsTheHandoffsYouStarted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.StartHandoff(sam, goal.ID, pat)
	samClient := signInClient(t, ts.URL, sam.Email)

	page := parsePage(t, getBody(t, samClient, ts.URL+"/handoffs"))
	list := findByTestID(t, page, "started-handoffs")
	if heading := headingNamed(page, "Handoffs you started"); heading == nil {
		t.Error("Pending handoffs has no Handoffs you started heading")
	}
	rows := findAllByTestID(list, "started-handoff")
	if len(rows) != 1 {
		t.Fatalf("started-handoffs lists %d rows, want 1:\n%s", len(rows), renderNode(list))
	}
	row := rows[0]
	if got, want := textOf(row), "Reduce outages — waiting for Pat Lee to accept Cancel handoff"; got != want {
		t.Errorf("the row reads %q, want %q", got, want)
	}
	if linkTo(row, fmt.Sprintf("/goals/%d", goal.ID)) == nil {
		t.Errorf("the row doesn't link its Goal:\n%s", renderNode(row))
	}
	if buttonNamed(row, "Cancel handoff") == nil {
		t.Fatalf("the row doesn't offer Cancel handoff:\n%s", renderNode(row))
	}
	form := findAll(row, func(n *nethtml.Node) bool { return n.Data == "form" })[0]
	if got := formField(t, form, "return").Value; got != "/handoffs" {
		t.Errorf("Cancel handoff returns to %q, want /handoffs", got)
	}

	resp := postForm(t, samClient, ts.URL+attrOf(form, "action"), url.Values{"return": {"/handoffs"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/handoffs" {
		t.Fatalf("cancel: status %d at %s, want 200 at /handoffs", resp.StatusCode, resp.Request.URL.Path)
	}
	if hasTestID(parsePage(t, readBody(t, resp)), "started-handoff") {
		t.Error("Pending handoffs still lists the cancelled Handoff")
	}

	if hasTestID(parsePage(t, getBody(t, signInClient(t, ts.URL, pat.Email), ts.URL+"/handoffs")), "started-handoffs") {
		t.Error("Pending handoffs lists Handoffs you started for someone who started none")
	}
}
