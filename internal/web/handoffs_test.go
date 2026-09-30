package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
// it to a present Owner, clearing Ownerless.
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
	_ = pat
}
