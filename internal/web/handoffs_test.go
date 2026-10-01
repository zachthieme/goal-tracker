package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner starts a Handoff from their Goal page; the proposed new Owner sees it
// in their inbox and accepts it, and only then does the Goal page show the new
// Owner.
func TestHandoffStartAcceptFlow(t *testing.T) {
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

	// The Reassign is recorded in the Goal's Ownership history.
	ownership := pageElement(t, childPage, "section", "goal-ownership-history")
	if !strings.Contains(ownership, "Ownership history (1)") {
		t.Errorf("Ownership history should count the Reassign: %s", ownership)
	}
	entry := between(t, ownership, `data-testid="ownership-change"`, "</li>")
	for _, part := range []string{shownAs("sam@example.com", "sam"), shownAs("pat@example.com", "pat"), "reassigned by an Admin", "started by " + shownAs("boss@example.com", "boss")} {
		if !strings.Contains(entry, part) {
			t.Errorf("Reassign entry lacks %q: %s", part, entry)
		}
	}
	_ = pat
}

// Each pending Handoff is a card with Accept as the primary action and a Reject
// that asks for confirmation first.
func TestPendingHandoffRowsConfirmReject(t *testing.T) {
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
	if !strings.Contains(openTag(row), `class="card`) {
		t.Errorf("the pending Handoff isn't a card: %s", openTag(row))
	}
	assertAcceptReject(t, row, fmt.Sprintf("/handoffs/%d", ho.ID))
}

// Every Handoff is kept with its outcome and listed oldest first under the Goal
// page's collapsed Ownership history: one rejected by the new Owner, one
// cancelled when its new Owner departed, and one the new Owner accepted after
// the Owner who started it departed — which ends the Goal's Ownerless state.
func TestOwnershipHistoryKeepsEveryHandoffOutcome(t *testing.T) {
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
		if resp := postForm(t, samClient, goalURL+"/handoff", url.Values{"to_email": {email}}); resp.StatusCode != http.StatusOK {
			t.Fatalf("start handoff to %s: status %d", email, resp.StatusCode)
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
	if resp := postForm(t, signInClient(t, ts.URL, mel.Email), fmt.Sprintf("%s/handoffs/%d/reject", ts.URL, rejected), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reject: status %d", resp.StatusCode)
	}

	// Pat departs before deciding, which cancels the Handoff to Pat.
	cancelled := handTo(pat.Email)
	if resp := postForm(t, bossClient, fmt.Sprintf("%s/accounts/%d/depart", ts.URL, pat.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("depart Pat: status %d", resp.StatusCode)
	}
	// Pat can no longer sign in to accept it, and the Handoff stays cancelled.
	if _, err := h.Service.AcceptHandoff(context.Background(), cancelled, pat.ID, nil); err == nil {
		t.Errorf("a cancelled Handoff was accepted")
	}

	// Sam departs with a Handoff to Lee pending; Lee still accepts it.
	handTo(lee.Email)
	if resp := postForm(t, bossClient, fmt.Sprintf("%s/accounts/%d/depart", ts.URL, sam.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("depart Sam: status %d", resp.StatusCode)
	}
	leeClient := signInClient(t, ts.URL, lee.Email)
	inbox := getBody(t, leeClient, ts.URL+"/handoffs")
	ho := between(t, inbox, `data-testid="pending-handoff"`, "")
	if resp := postForm(t, leeClient, ts.URL+between(t, ho, `/handoffs/`, `/accept`)+"/accept", url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("accept: status %d", resp.StatusCode)
	}

	page := getBody(t, bossClient, goalURL)
	if strings.Contains(page, `data-testid="goal-ownerless"`) {
		t.Errorf("Goal still Ownerless after Lee accepted the Handoff")
	}
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("lee@example.com", "lee")) {
		t.Errorf("Goal page should show Lee as Owner")
	}

	section := pageElement(t, between(t, page, `data-testid="goal-history"`, "</aside>"), "section", "goal-ownership-history")
	details := between(t, section, "<details", "</summary>")
	if strings.Contains(openTag(details), "open") {
		t.Errorf("Ownership history is not collapsed: %s", openTag(details))
	}
	if !strings.Contains(details, "Ownership history (3)") {
		t.Errorf("Ownership history summary lacks its count: %s", details)
	}
	entries := strings.Split(section, `data-testid="ownership-change"`)[1:]
	want := []struct{ to, outcome, at string }{
		{shownAs("mel@example.com", "mel"), "rejected", "2026-01-02 15:04"},
		{shownAs("pat@example.com", "pat"), "cancelled", "2026-01-02 16:04"},
		{shownAs("lee@example.com", "lee"), "accepted", "2026-01-02 17:04"},
	}
	if len(entries) != len(want) {
		t.Fatalf("Ownership history has %d entries, want %d:\n%s", len(entries), len(want), section)
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

	if resp := postForm(t, bossClient, fmt.Sprintf("%s/accounts/%d/return", ts.URL, sam.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mark returned: status %d", resp.StatusCode)
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
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("returned Delegate's Check-in: status %d", resp.StatusCode)
	}
	if latest, ok, err := h.Service.LatestCheckin(t.Context(), delegated.ID); err != nil || !ok || latest.Author.ID != sam.ID {
		t.Errorf("LatestCheckin = %+v, %v, %v; want Sam's Check-in as Delegate", latest, ok, err)
	}
}

// Only an Admin may mark someone returned: anyone else is refused with 403 and
// the person stays Departed.
func TestNonAdminCannotMarkReturned(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	resp := postForm(t, signInClient(t, ts.URL, "pat@example.com"), fmt.Sprintf("%s/accounts/%d/return", ts.URL, sam.ID), url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin mark returned: status %d, want 403", resp.StatusCode)
	}
	if acc, err := h.Service.Account(t.Context(), sam.ID); err != nil || !acc.Departed {
		t.Errorf("Sam after a refused return = %+v, %v; want still Departed", acc, err)
	}
}

// The accept form on the pending Handoffs page lists the Goal's Delegates who
// aren't Departed, each with a keep checkbox checked by default; accepting with
// one unchecked removes exactly that Delegate, as a plain form POST, and the
// Departed Delegate stays (CONTEXT.md: Delegate).
func TestAcceptHandoffFormChoosesDelegatesToKeep(t *testing.T) {
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
	if strings.Contains(form, fmt.Sprintf(`value="%d"`, dee.ID)) || strings.Contains(form, "dee@example.com") {
		t.Errorf("accept form offers the Departed Delegate:\n%s", form)
	}

	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{
		"keep": {fmt.Sprint(ann.ID)},
	})
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
	if resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{
		"keep": {fmt.Sprint(ann.ID), fmt.Sprint(bob.ID)},
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("accept handoff: status %d", resp.StatusCode)
	}
	if got := delegateEmails(t, h, kept.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after accepting with every box checked = %v, want %v", got, want)
	}

	ho, err = h.Service.StartHandoffByEmail(context.Background(), rejected.ID, pat.Email, sam.ID)
	if err != nil {
		t.Fatalf("StartHandoff: %v", err)
	}
	if resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/reject", ts.URL, ho.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reject handoff: status %d", resp.StatusCode)
	}
	if got := delegateEmails(t, h, rejected.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after reject = %v, want %v", got, want)
	}
}

// Home's Waiting on you offers the same choice: a Handoff's accept form there
// lists the Delegates to keep, each checked by default.
func TestHomeHandoffAcceptListsDelegatesToKeep(t *testing.T) {
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

	home := getBody(t, signInClient(t, ts.URL, pat.Email), ts.URL+"/")
	row := between(t, home, `data-testid="home-pending-handoff"`, "</li>")
	form := between(t, row, fmt.Sprintf(`action="/handoffs/%d/accept"`, ho.ID), "</form>")
	if box := fmt.Sprintf(`<input type="checkbox" name="keep" value="%d" checked>`, ann.ID); !strings.Contains(form, box) {
		t.Errorf("Home's accept form lacks Ann's keep checkbox (%s):\n%s", box, form)
	}
}

// An Admin Reassign keeps every Delegate, present or Departed.
func TestReassignKeepsDelegates(t *testing.T) {
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

	if resp := postForm(t, signInClient(t, ts.URL, boss.Email), fmt.Sprintf("%s/goals/%d/reassign", ts.URL, goal.ID), url.Values{
		"email": {"pat@example.com"},
	}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reassign: status %d", resp.StatusCode)
	}

	want := []string{"ann@example.com", "dee@example.com"}
	if got := delegateEmails(t, h, goal.ID); !slices.Equal(got, want) {
		t.Errorf("Delegates after reassign = %v, want %v", got, want)
	}
}

// When accepting fails part-way, the request fails and nothing changes: the
// Owner, the Delegates and the pending Handoff all stay as they were.
func TestFailedAcceptChangesNothing(t *testing.T) {
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
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_accept BEFORE UPDATE OF status ON handoffs
		WHEN NEW.status = 'accepted' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	patClient := signInClient(t, ts.URL, pat.Email)

	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{})
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

	if resp := postForm(t, bossClient, goalURL+"/reassign", url.Values{"email": {"cal@example.com"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("reassign: status %d", resp.StatusCode)
	}

	if inbox := getBody(t, patClient, ts.URL+"/handoffs"); !strings.Contains(inbox, "No pending handoffs") {
		t.Errorf("Pat's inbox still offers the cancelled Handoff:\n%s", inbox)
	}
	resp := postForm(t, patClient, fmt.Sprintf("%s/handoffs/%d/accept", ts.URL, ho.ID), url.Values{})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("accept the cancelled Handoff: status %d, want 404", resp.StatusCode)
	}

	page := getBody(t, bossClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-owner">`+shownAs("cal@example.com", "cal")) {
		t.Errorf("Goal page should show Cal as Owner after Pat's refused accept")
	}
	section := pageElement(t, page, "section", "goal-ownership-history")
	entries := strings.Split(section, `data-testid="ownership-change"`)[1:]
	want := []struct{ to, outcome string }{
		{shownAs("pat@example.com", "pat"), "cancelled"},
		{shownAs("cal@example.com", "cal"), "reassigned by an Admin"},
	}
	if len(entries) != len(want) {
		t.Fatalf("Ownership history has %d entries, want %d:\n%s", len(entries), len(want), section)
	}
	for i, w := range want {
		for _, part := range []string{shownAs("sam@example.com", "sam"), w.to, w.outcome} {
			if !strings.Contains(entries[i], part) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
		}
	}
}

// A pending Handoff's accept and reject forms sit inline, and the accept form's
// keep-Delegates fieldset stacks its boxes, through classes rather than style
// attributes.
func TestPendingHandoffLayoutComesFromClasses(t *testing.T) {
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

	for _, path := range []string{"/handoffs", "/home"} {
		page := getBody(t, client, ts.URL+path)
		for _, action := range []string{"accept", "reject"} {
			form := tagAround(t, page, fmt.Sprintf(`action="/handoffs/%d/%s"`, ho.ID, action))
			assertStyledBy(t, form, css, "inline-form", "display:inline")
		}
		keep := tagAround(t, page, `data-testid="handoff-keep-delegates"`)
		assertStyledBy(t, keep, page, "handoff-keep", "border:0;padding:0;margin:0 0 8px;display:flex;flex-direction:column;gap:4px")
	}
}

// assertStyledBy checks the element's opening tag carries no style attribute
// but the class, and that rules declare exactly want for that class.
func assertStyledBy(t *testing.T, tag, rules, class, want string) {
	t.Helper()
	if strings.Contains(tag, " style=") {
		t.Errorf("element carries a style attribute: %s", tag)
	}
	if !slices.Contains(strings.Fields(attr(tag, "class")), class) {
		t.Errorf("element lacks class %s: %s", class, tag)
	}
	if got := cssRule(t, rules, "."+class); got != want {
		t.Errorf(".%s declares %q, want %q", class, got, want)
	}
}

// tagAround returns the whole opening tag holding marker, its attributes in
// whatever order they render.
func tagAround(t *testing.T, page, marker string) string {
	t.Helper()
	at := strings.Index(page, marker)
	if at < 0 {
		t.Fatalf("page has no %s", marker)
	}
	start := strings.LastIndex(page[:at], "<")
	end := strings.Index(page[at:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("%s is not inside a tag", marker)
	}
	return page[start : at+end+1]
}

// attr returns the value of the named attribute in an opening tag, or "".
func attr(tag, name string) string {
	m := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}
