package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// TestSmokeDelegateChecksInOnOwnersGoal is the Delegate HTTP smoke test: an Owner
// authorizes a Delegate on the Goal page, the Delegate finds the Goal on their
// "Delegated to me" page, and checks in on it — the Check-in recording the
// Delegate as author and the Owner it was written for (CONTEXT.md: Delegate).
func TestSmokeDelegateChecksInOnOwnersGoal(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com") // the Delegate's account must exist to be added by email
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	samClient := signInClient(t, ts.URL, "sam@example.com")
	tpmClient := signInClient(t, ts.URL, "tpm@example.com")

	// The Owner authorizes the Delegate on the Goal page.
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/delegates", ts.URL, goal.ID), url.Values{
		"email": {"tpm@example.com"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add delegate: status %d", resp.StatusCode)
	}
	goalBody := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(goalBody, "tpm@example.com") || !strings.Contains(goalBody, `data-testid="delegate"`) {
		t.Errorf("goal page does not list the Delegate; body:\n%s", goalBody)
	}

	// The Delegate's page lists the Goal they may check in on.
	delegateBody := getBody(t, tpmClient, ts.URL+"/delegates")
	if !strings.Contains(delegateBody, "Reduce outages") {
		t.Errorf("delegate page does not list the delegated Goal; body:\n%s", delegateBody)
	}
	if !strings.Contains(delegateBody, `data-testid="checkin-form"`) {
		t.Errorf("delegate page does not offer a Check-in form; body:\n%s", delegateBody)
	}

	// The Delegate checks in on the Goal.
	resp = postForm(t, tpmClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track — checked in for Sam."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delegate check-in: status %d", resp.StatusCode)
	}

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	if history[0].Author.ID != tpm.ID {
		t.Errorf("Check-in Author = %d, want the Delegate %d", history[0].Author.ID, tpm.ID)
	}
	if history[0].Owner.ID != sam.ID {
		t.Errorf("Check-in Owner = %d, want the Owner %d", history[0].Owner.ID, sam.ID)
	}
}

// A person who is neither the Owner nor a Delegate cannot submit a Check-in on
// someone else's Goal through the web (acceptance: Non-Delegates can't submit).
func TestSmokeNonDelegateCheckinForbidden(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	melClient := signInClient(t, ts.URL, "mel@example.com")
	resp := postForm(t, melClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Meddling."},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Delegate check-in status = %d, want 403", resp.StatusCode)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded by a non-Delegate: %d", len(history))
	}
}

// Only the Owner may authorize a Delegate through the web; another user's attempt
// is refused (CONTEXT.md: a person an Owner authorizes).
func TestSmokeNonOwnerCannotAddDelegate(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	melClient := signInClient(t, ts.URL, "mel@example.com")
	resp := postForm(t, melClient, fmt.Sprintf("%s/goals/%d/delegates", ts.URL, goal.ID), url.Values{
		"email": {"tpm@example.com"},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Owner add delegate status = %d, want 403", resp.StatusCode)
	}
	if delegates, _ := h.Service.ListDelegates(context.Background(), goal.ID); len(delegates) != 0 {
		t.Errorf("a Delegate was added by a non-Owner: %+v", delegates)
	}
}
