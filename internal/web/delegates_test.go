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
	if !strings.Contains(delegateBody, fmt.Sprintf(`href="/goals/%d/checkin"`, goal.ID)) {
		t.Errorf("delegate page does not link to the Check-in page; body:\n%s", delegateBody)
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

// The Delegate's page shows each delegated Goal as a card with its Health, its
// Owner, when it was last checked in on, and a Check in link to the Check-in
// page, rather than embedding the whole form.
func TestDelegatePageShowsGoalCards(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	checked := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, checked.ID)
	h.Checkin(tpm, checked.ID, domain.HealthYellow, "Wobbling.", "Add a second on-call.", testsupport.Epoch.AddDate(0, 2, 0))
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(sam, tpm, proposed.ID)
	h.Clock.Advance(3 * day)

	page := getBody(t, signInClient(t, ts.URL, "tpm@example.com"), ts.URL+"/delegates")
	if strings.Contains(page, `data-testid="checkin-form"`) {
		t.Errorf("the Delegate page still embeds the Check-in form")
	}
	card := delegatedCard(t, page, checked)
	if !strings.Contains(openTag(card), "card") {
		t.Errorf("a delegated Goal is not a card: %s", openTag(card))
	}
	for _, want := range []string{
		"Reduce outages",
		`class="badge y"`,
		`data-testid="delegated-goal-health">Yellow<`,
		`data-testid="delegated-goal-owner">sam@example.com<`,
		"3 days ago",
		fmt.Sprintf(`href="/goals/%d/checkin"`, checked.ID),
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %s:\n%s", want, card)
		}
	}

	rest := delegatedCard(t, page, proposed)
	if strings.Contains(rest, fmt.Sprintf(`href="/goals/%d/checkin"`, proposed.ID)) {
		t.Errorf("a Proposed Goal offers Check in: %s", rest)
	}
	if !strings.Contains(rest, `data-testid="delegated-goal-no-health"`) || !strings.Contains(rest, `data-testid="delegated-goal-not-active"`) {
		t.Errorf("a Proposed Goal's card doesn't say it has no Check-ins and can't be checked in on: %s", rest)
	}
}

// delegatedCard returns the Delegate page's card for g, from its opening <li>
// to its close.
func delegatedCard(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	at := strings.Index(page, navTo(g.ID))
	if at < 0 {
		t.Fatalf("the Delegate page doesn't list %s:\n%s", g.Title, page)
	}
	start := strings.LastIndex(page[:at], `<li data-testid="delegated-goal"`)
	if start < 0 {
		t.Fatalf("%s isn't in a delegated-goal card", g.Title)
	}
	return between(t, page[start:], "", "</li>")
}
