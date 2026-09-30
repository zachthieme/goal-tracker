package web_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A signed-in person saves a Report Definition of a root Goal to a depth and is
// shown a live draft listing the selected Goals one line each, with each Goal's
// title, Owner, Health, and due date (CONTEXT.md: Report Definition).
func TestSaveReportDefinitionAndSeeDraftOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")

	a := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	b := h.ActiveChildOf(boss, a, "Launch in EU", "Expand the market.")
	c := h.ActiveChildOf(boss, a, "Cut churn", "Keep customers.")
	h.Checkin(boss, b.ID, domain.HealthYellow, "slipping", "add staff", h.Clock.Now().AddDate(0, 1, 0))

	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	// The create form offers the Goals as roots.
	form := getBody(t, client, ts.URL+"/reports")
	if !strings.Contains(form, "Grow revenue") {
		t.Fatalf("reports page does not offer Goals as roots; body:\n%s", form)
	}

	resp := postForm(t, client, ts.URL+"/reports", url.Values{
		"name":         {"EU MBR"},
		"introduction": {"Quarterly business review."},
		"root":         {strconv.FormatInt(a.ID, 10)},
		"depth":        {"1"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save report: status %d", resp.StatusCode)
	}
	draft := readBody(t, resp)
	for _, want := range []string{
		"EU MBR",
		"Quarterly business review.",
		"Grow revenue", "Launch in EU", "Cut churn", // selected Goals
		"boss@example.com",  // Owner
		domain.HealthYellow, // B's Health
	} {
		if !strings.Contains(draft, want) {
			t.Errorf("live draft missing %q; body:\n%s", want, draft)
		}
	}
	// Each selected Goal is one line.
	if got := strings.Count(draft, `data-testid="selected-goal"`); got != 3 {
		t.Errorf("draft lists %d selected Goals, want 3; body:\n%s", got, draft)
	}

	// The saved definition shows up on the reports list.
	list := getBody(t, client, ts.URL+"/reports")
	if !strings.Contains(list, "EU MBR") {
		t.Errorf("reports list missing the saved definition; body:\n%s", list)
	}
	_ = c
}

// A Report Definition that selects nothing (no roots, no filters) is rejected.
func TestSaveReportDefinitionRejectsEmptySelection(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports", url.Values{"name": {"Selects nothing"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty selection: status %d, want 422", resp.StatusCode)
	}
}
