package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Yellow Check-in submitted without a Path to Green comes back with the
// validation error rendered in the form, next to the Path to Green field, so the
// Owner can fix it in place (htmx). This is the validation-error smoke test.
func TestSmokeCheckinValidationErrorReachesForm(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Submit Yellow with a status but no Path to Green, as htmx would.
	body, status := postFormHX(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthYellow},
		"status": {"Slipping a little."},
	})
	if status != http.StatusOK {
		t.Fatalf("htmx validation response status = %d, want 200 so htmx swaps the form", status)
	}
	if !strings.Contains(body, `data-testid="checkin-error"`) {
		t.Errorf("response missing the inline error; body:\n%s", body)
	}
	if !strings.Contains(body, "Path to Green") {
		t.Errorf("error does not mention the Path to Green; body:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="checkin-form"`) {
		t.Errorf("error response is not the re-rendered form; body:\n%s", body)
	}

	// Nothing was recorded.
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the validation error: %d", len(history))
	}
}

// The one-click "no change" button records a Check-in repeating the previous
// values. This is the no-change smoke test.
func TestSmokeNoChangeCheckinButton(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	// Seed a first Check-in through the form (the happy path).
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit first check-in: status %d", resp.StatusCode)
	}

	// Click "no change".
	resp = postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, goal.ID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-change: status %d", resp.StatusCode)
	}

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (the seeded Check-in and its no-change repeat)", len(history))
	}
	if history[0].Health != domain.HealthGreen || history[0].Status != "On track." {
		t.Errorf("no-change did not repeat the previous values: %+v", history[0])
	}

	// The Goal page shows the current Health from the latest Check-in.
	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(page, `data-testid="goal-health">Green`) {
		t.Errorf("Goal page missing current Health; body:\n%s", page)
	}
}

// An Owner records a Metric reading and a Highlight in a Check-in through the
// web form: both are persisted, and the Goal page then shows the Metric's trend
// value against its target and the Highlight. This is the readings/Highlight
// smoke test (ticket #6).
func TestSmokeCheckinRecordsReadingAndHighlight(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	metric, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "p95 checkout latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: h.Clock.Now().AddDate(0, 6, 0),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	samClient := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":                             {domain.HealthGreen},
		"status":                             {"On track."},
		fmt.Sprintf("reading_%d", metric.ID): {"850"},
		"highlight_kind":                     {domain.HighlightAccomplishment},
		"highlight_note":                     {"Shipped the caching layer."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d", resp.StatusCode)
	}

	readings, err := h.Service.ListMetricReadings(context.Background(), metric.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 1 || readings[0].Value != 850 {
		t.Fatalf("readings = %+v, want one reading of 850", readings)
	}
	highlights, err := h.Service.ListHighlightsByGoal(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListHighlightsByGoal: %v", err)
	}
	if len(highlights) != 1 || highlights[0].Note != "Shipped the caching layer." {
		t.Fatalf("highlights = %+v, want the Accomplishment just recorded", highlights)
	}

	page := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	for _, want := range []string{"850", "Shipped the caching layer.", domain.HighlightAccomplishment} {
		if !strings.Contains(page, want) {
			t.Errorf("Goal page missing %q; body:\n%s", want, page)
		}
	}
}

// postFormHX posts a form with the HX-Request header set, as htmx does, and
// returns the body and status without following redirects or asserting 200.
func postFormHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b), resp.StatusCode
}
