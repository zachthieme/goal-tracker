package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(page, `data-testid="goal-owner">pat@example.com`) {
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
	if !strings.Contains(childPage, `data-testid="goal-owner">pat@example.com`) {
		t.Errorf("Goal page should show Pat as Owner after reassignment; body:\n%s", childPage)
	}

	// The Reassign is recorded in the Goal's Ownership history.
	ownership := pageElement(t, childPage, "section", "goal-ownership-history")
	if !strings.Contains(ownership, "Ownership history (1)") {
		t.Errorf("Ownership history should count the Reassign: %s", ownership)
	}
	entry := between(t, ownership, `data-testid="ownership-change"`, "</li>")
	for _, part := range []string{"sam@example.com", "pat@example.com", "reassigned by an Admin", "started by boss@example.com"} {
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
	if _, err := h.Service.AcceptHandoff(context.Background(), cancelled, pat.ID); err == nil {
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
	if !strings.Contains(page, `data-testid="goal-owner">lee@example.com`) {
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
		{"mel@example.com", "rejected", "2026-01-02 15:04"},
		{"pat@example.com", "cancelled", "2026-01-02 16:04"},
		{"lee@example.com", "accepted", "2026-01-02 17:04"},
	}
	if len(entries) != len(want) {
		t.Fatalf("Ownership history has %d entries, want %d:\n%s", len(entries), len(want), section)
	}
	for i, w := range want {
		for _, part := range []string{"sam@example.com", w.to, w.outcome, w.at, "started by sam@example.com"} {
			if !strings.Contains(entries[i], part) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
		}
	}
}
