package domain

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// The Goal part checks, each called directly with no database: every case
// asserts the inputs refused and the sentences they are refused with, each
// as "input: sentence", in order.

// partProblemLines are problems as "input: sentence" lines, for comparing.
func partProblemLines(problems []*InputError) []string {
	var out []string
	for _, p := range problems {
		out = append(out, fmt.Sprintf("%s: %s", p.Input, p.Message))
	}
	return out
}

func checkPartProblems(t *testing.T, got []*InputError, want []string) {
	t.Helper()
	if lines := partProblemLines(got); !slices.Equal(lines, want) {
		t.Errorf("problems = %q, want %q", lines, want)
	}
}

var partDate = time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC)

func TestMilestoneProblems(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mName    string
		date     time.Time
		problems []string
	}{
		{"a name and a date", "Beta", partDate, nil},
		{"a blank name", "  ", partDate, []string{"name: a Milestone needs a name"}},
		{"no date", "Beta", time.Time{}, []string{"date: a Milestone needs a date"}},
		{"neither", "", time.Time{}, []string{"name: a Milestone needs a name", "date: a Milestone needs a date"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, milestoneProblems(tc.mName, tc.date, "name", "date"), tc.problems)
		})
	}
}

func TestMetricProblems(t *testing.T) {
	t.Parallel()

	input := func(part string) string { return "m." + part }
	cases := []struct {
		name                   string
		mName, unit, direction string
		targetDate             time.Time
		problems               []string
	}{
		{"a whole Metric", "p95 latency", "ms", MetricDown, partDate, nil},
		{"a blank name", " ", "ms", MetricUp, partDate, []string{"m.name: a Metric needs a name"}},
		{"a blank unit", "p95 latency", " ", MetricUp, partDate, []string{"m.unit: a Metric needs a unit"}},
		{"no target date", "p95 latency", "ms", MetricUp, time.Time{}, []string{"m.target_date: a Metric needs a target date"}},
		{"a direction neither up nor down", "p95 latency", "ms", "sideways", partDate, []string{`m.direction: a Metric needs a direction of "up" or "down"`}},
		{"nothing at all", "", "", "", time.Time{}, []string{
			"m.name: a Metric needs a name",
			"m.unit: a Metric needs a unit",
			`m.direction: a Metric needs a direction of "up" or "down"`,
			"m.target_date: a Metric needs a target date",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, metricProblems(tc.mName, tc.unit, tc.direction, tc.targetDate, input), tc.problems)
		})
	}
}
