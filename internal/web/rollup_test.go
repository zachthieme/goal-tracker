package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

var pathDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// The Goal page and the Check-in form show the Rolled-up Health — the worst
// Owner-set Health among the Goal's Active children — next to the Owner-set
// Health (ADR-0003).
func TestSmokeGoalPageShowsRolledUpHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	// The parent Owner checks in Green, explaining the difference.
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:      parent.ID,
		AuthorID:    sam.ID,
		Health:      domain.HealthGreen,
		Status:      "Fine at this level.",
		Explanation: "The Red child is a stretch item.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}

	samClient := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID))
	if !strings.Contains(page, `data-testid="goal-health">Green`) {
		t.Errorf("Goal page missing Owner-set Health; body:\n%s", page)
	}
	if !strings.Contains(page, `data-testid="goal-rollup-health">Red`) {
		t.Errorf("Goal page missing Rolled-up Health next to it; body:\n%s", page)
	}
}

// Submitting a Health that differs from the Rolled-up Health without an
// explanation comes back with the validation error rendered in the form (htmx),
// and records nothing.
func TestSmokeCheckinDifferingFromRollupNeedsExplanation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Parent", "It matters.")
	redChild := h.ActiveChildOf(sam, parent, "Red work", "Red so what.")
	h.Checkin(sam, redChild.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, parent.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Looks fine."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200", status)
	}
	if !strings.Contains(body, `data-testid="checkin-error"`) {
		t.Errorf("response missing the inline error; body:\n%s", body)
	}
	if !strings.Contains(body, "Rolled-up Health") {
		t.Errorf("error does not mention the Rolled-up Health; body:\n%s", body)
	}

	// With the explanation, it is accepted.
	_, status = postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, parent.ID), url.Values{
		"health":      {domain.HealthGreen},
		"status":      {"Looks fine."},
		"explanation": {"Stretch child, does not gate delivery."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx success response status = %d, want 200", status)
	}
	latest, ok, err := h.Service.LatestCheckin(t.Context(), parent.ID)
	if err != nil || !ok {
		t.Fatalf("LatestCheckin: %v ok=%v", err, ok)
	}
	if latest.Explanation != "Stretch child, does not gate delivery." {
		t.Errorf("Explanation not recorded via the form: %q", latest.Explanation)
	}
}
