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

func TestFieldValueProblems(t *testing.T) {
	t.Parallel()

	headcount := Field{ID: 7, Name: "Headcount", Type: FieldNumber}
	retired := Field{ID: 8, Name: "Budget", Type: FieldNumber, Retired: true}
	launch := Field{ID: 9, Name: "Launch", Type: FieldDate}
	cases := []struct {
		name     string
		field    Field
		value    string
		problems []string
	}{
		{"a number in a number Field", headcount, "3", nil},
		{"a blank value, which clears it", headcount, "", nil},
		{"a word in a number Field", headcount, "three", []string{`f: Headcount takes a number, and "three" isn't one`}},
		{"a value that isn't a date in a date Field", launch, "soon", []string{`f: Launch takes a date as YYYY-MM-DD, and "soon" isn't one`}},
		{"a value in a Retired Field", retired, "3", []string{"f: Budget is retired, so it can't be set"}},
		{"a blank value in a Retired Field", retired, "", []string{"f: Budget is retired, so it can't be set"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, fieldValueProblems(tc.field, tc.value, "f"), tc.problems)
		})
	}
}

func TestParentLinkProblems(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		childID      int64
		parentID     int64
		parentExists bool
		problems     []string
	}{
		{"another Goal", 1, 2, true, nil},
		{"a Goal not yet saved, to an existing parent", 0, 2, true, nil},
		{"the Goal itself", 1, 1, true, []string{"p: a Goal cannot contribute to itself"}},
		{"a parent that doesn't exist", 1, 2, false, []string{"p: that parent Goal does not exist"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, parentLinkProblems(tc.childID, tc.parentID, tc.parentExists, "p"), tc.problems)
		})
	}
}

func TestChosenValueProblems(t *testing.T) {
	t.Parallel()

	team := Dimension{ID: 3, Name: "Team"}
	legacy := Dimension{ID: 4, Name: "Legacy", Retired: true}
	platform := DimensionValue{ID: 30, DimensionID: team.ID, Value: "Platform"}
	ops := DimensionValue{ID: 31, DimensionID: team.ID, Value: "Ops", Retired: true}
	old := DimensionValue{ID: 40, DimensionID: legacy.ID, Value: "Old"}
	cases := []struct {
		name     string
		val      DimensionValue
		dim      Dimension
		carried  bool
		problems []string
	}{
		{"a value offered", platform, team, false, nil},
		{"a Retired value", ops, team, false, []string{"v: Ops is retired and cannot be newly assigned"}},
		{"a value in a Retired Dimension", old, legacy, false, []string{"v: Legacy is retired, so its values can't be newly assigned"}},
		{"a Retired value the Goal carries", ops, team, true, nil},
		{"a value in a Retired Dimension the Goal carries", old, legacy, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, chosenValueProblems(tc.val, tc.dim, tc.carried, "v"), tc.problems)
		})
	}
}

func TestTypedValueProblems(t *testing.T) {
	t.Parallel()

	customer := Dimension{ID: 5, Name: "Customer", List: ListExtendable}
	legacy := Dimension{ID: 6, Name: "Legacy", List: ListExtendable, Retired: true}
	acme := DimensionValue{ID: 50, DimensionID: customer.ID, Value: "Acme"}
	oldCo := DimensionValue{ID: 51, DimensionID: customer.ID, Value: "Old Co", Retired: true}
	none := DimensionValue{}
	cases := []struct {
		name     string
		dim      Dimension
		typed    string
		match    DimensionValue
		matched  bool
		problems []string
	}{
		{"a new value", customer, "Globex", none, false, nil},
		{"a value already on the list", customer, "acme ", acme, true, nil},
		{"a value matching a Retired value", customer, "old co", oldCo, true, []string{"n: Old Co is retired, so it can't be added or newly assigned"}},
		{"a new value with a semicolon", customer, "Globex; Initech", none, false, []string{"n: Globex; Initech: a value can't contain a semicolon, because the import format uses it to separate values"}},
		{"a value in a Retired Dimension", legacy, "Globex", none, false, []string{"n: Legacy is retired, so its values can't be newly assigned"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkPartProblems(t, typedValueProblems(tc.dim, tc.typed, tc.match, tc.matched, "n"), tc.problems)
		})
	}
}
