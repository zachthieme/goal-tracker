package web_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A No change runs as many queries on a Green Goal with five Metrics read as on
// one with a single Metric: its verdict reads every Metric's readings at once,
// not once per Metric (#558).
func TestNoChangeQueriesDontGrowWithMetrics(t *testing.T) {
	t.Parallel()
	countFor := func(n int) int {
		ctx := context.Background()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
		var readings []domain.MetricReadingInput
		for i := range n {
			m, err := h.Service.AddMetric(ctx, sam.ID, domain.AddMetricInput{
				GoalID: g.ID, Name: fmt.Sprintf("Metric %d", i), Unit: "a day", Direction: domain.MetricUp,
				Baseline: 10, Target: 100, TargetDate: h.Clock.Now().AddDate(0, 2, 0),
			})
			if err != nil {
				t.Fatalf("AddMetric: %v", err)
			}
			readings = append(readings, domain.MetricReadingInput{MetricID: m.ID, Value: 40})
		}
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track.", Readings: readings,
		}); err != nil {
			t.Fatalf("SubmitCheckin: %v", err)
		}
		ts, _, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, "sam@example.com")
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		queries.take()
		resp, _ := postNoChange(t, client, fmt.Sprintf("%s/goals/%d/checkins/no-change", ts.URL, g.ID), false)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("No change with %d Metrics: status %d, want 303", n, resp.StatusCode)
		}
		return len(queries.take())
	}
	if one, five := countFor(1), countFor(5); one != five {
		t.Errorf("a No change ran %d queries with 1 Metric and %d with 5; want the same", one, five)
	}
}
