package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin defines a Dimension with a fixed list of values; a non-Admin cannot
// (CONTEXT.md: Admin defines Dimensions).
func TestAdminCreatesDimensionWithValues(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")

	dim, err := h.Service.CreateDimension(context.Background(), boss.ID, "Pillar", []string{"Growth", "Reliability"})
	if err != nil {
		t.Fatalf("CreateDimension: %v", err)
	}
	if dim.Name != "Pillar" {
		t.Errorf("Name = %q, want %q", dim.Name, "Pillar")
	}
	if len(dim.Values) != 2 {
		t.Fatalf("Values = %+v, want 2", dim.Values)
	}
	if dim.Values[0].Value != "Growth" || dim.Values[1].Value != "Reliability" {
		t.Errorf("Values = %+v, want Growth then Reliability", dim.Values)
	}

	if _, err := h.Service.CreateDimension(context.Background(), sam.ID, "Quarter", []string{"Q1"}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin CreateDimension err = %v, want ErrNotAuthorized", err)
	}
}

// An Admin adds a value, renames a value (its identity is kept), and retires a
// value (which stays listed, flagged retired); a non-Admin may do none of these
// (CONTEXT.md: Admins add, rename, and retire values).
func TestAdminManagesDimensionValues(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	dim, err := h.Service.CreateDimension(ctx, boss.ID, "Pillar", []string{"Growth"})
	if err != nil {
		t.Fatalf("CreateDimension: %v", err)
	}
	growthID := dim.Values[0].ID

	added, err := h.Service.AddDimensionValue(ctx, boss.ID, dim.ID, "Reliability")
	if err != nil {
		t.Fatalf("AddDimensionValue: %v", err)
	}
	renamed, err := h.Service.RenameDimensionValue(ctx, boss.ID, growthID, "Expansion")
	if err != nil {
		t.Fatalf("RenameDimensionValue: %v", err)
	}
	if renamed.ID != growthID {
		t.Errorf("rename changed the value's identity: got %d, want %d", renamed.ID, growthID)
	}
	if renamed.Value != "Expansion" {
		t.Errorf("renamed value = %q, want %q", renamed.Value, "Expansion")
	}
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, added.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if len(dims) != 1 {
		t.Fatalf("ListDimensions = %+v, want 1", dims)
	}
	byID := map[int64]domain.DimensionValue{}
	for _, v := range dims[0].Values {
		byID[v.ID] = v
	}
	if got := byID[growthID]; got.Value != "Expansion" || got.Retired {
		t.Errorf("renamed value in list = %+v, want Expansion not retired", got)
	}
	if got := byID[added.ID]; !got.Retired {
		t.Errorf("retired value in list = %+v, want Retired true", got)
	}

	if _, err := h.Service.AddDimensionValue(ctx, sam.ID, dim.ID, "Nope"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin AddDimensionValue err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.RenameDimensionValue(ctx, sam.ID, growthID, "Nope"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RenameDimensionValue err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RetireDimensionValue(ctx, sam.ID, growthID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RetireDimensionValue err = %v, want ErrNotAuthorized", err)
	}
}

// An Owner assigns a Dimension value to their Goal; assigning a second value in
// the same Dimension replaces the first (a Goal carries one value per Dimension),
// while a value in another Dimension coexists (CONTEXT.md: Owners assign
// Dimension values to their Goals).
func TestAssignDimensionValueReplacesWithinDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	quarter := h.CreateDimension(boss, "Quarter", "Q1")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	if err := h.Service.AssignGoalValue(ctx, goal.ID, pillar.Values[0].ID); err != nil {
		t.Fatalf("AssignGoalValue Growth: %v", err)
	}
	values, err := h.Service.GoalValues(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Growth" {
		t.Fatalf("after first assign, values = %+v, want [Growth]", values)
	}

	// Assigning Reliability in the same Dimension replaces Growth.
	if err := h.Service.AssignGoalValue(ctx, goal.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("AssignGoalValue Reliability: %v", err)
	}
	// A value in another Dimension coexists.
	if err := h.Service.AssignGoalValue(ctx, goal.ID, quarter.Values[0].ID); err != nil {
		t.Fatalf("AssignGoalValue Q1: %v", err)
	}
	values, err = h.Service.GoalValues(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	got := map[string]bool{}
	for _, v := range values {
		got[v.Value] = true
	}
	if got["Growth"] {
		t.Errorf("Growth still assigned after replacement; values = %+v", values)
	}
	if !got["Reliability"] || !got["Q1"] {
		t.Errorf("values = %+v, want Reliability and Q1", values)
	}
}

// A retired value stays readable on a Goal that already carries it, but is not
// offered for a new assignment (CONTEXT.md: retired values stay readable).
func TestRetiredValueStaysReadableButNotAssignable(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	growth := pillar.Values[0]
	assigned := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	fresh := h.CreateGoal(sam, "Ship faster", "Slow ships lose deals.")
	h.AssignGoalValue(assigned, growth)

	if err := h.Service.RetireDimensionValue(ctx, boss.ID, growth.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	// Still readable on the Goal that carried it before retirement.
	values, err := h.Service.GoalValues(ctx, assigned.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Growth" || !values[0].Retired {
		t.Errorf("values = %+v, want the retired Growth still readable", values)
	}

	// Not offered for a new assignment.
	if err := h.Service.AssignGoalValue(ctx, fresh.ID, growth.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("assigning a retired value err = %v, want ErrValidation", err)
	}
}

// The Goal list filters and groups by Dimension values: filtering ANDs across
// Dimensions and ORs within one, and grouping buckets each Goal under its value
// (with an unassigned bucket) (CONTEXT.md: filter and group Goals).
func TestFilterAndGroupGoalsByDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	quarter := h.CreateDimension(boss, "Quarter", "Q1", "Q2")
	growth, reliability := pillar.Values[0], pillar.Values[1]
	q1 := quarter.Values[0]

	goalA := h.CreateGoal(sam, "Alpha", "A matters.")
	goalB := h.CreateGoal(sam, "Bravo", "B matters.")
	goalC := h.CreateGoal(sam, "Charlie", "C matters.")
	h.AssignGoalValue(goalA, growth)
	h.AssignGoalValue(goalA, q1)
	h.AssignGoalValue(goalB, reliability)
	h.AssignGoalValue(goalB, q1)
	_ = goalC // goalC has no values; it appears in the unassigned group.

	all, err := h.Service.ListGoalsWithValues(ctx)
	if err != nil {
		t.Fatalf("ListGoalsWithValues: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListGoalsWithValues = %d goals, want 3", len(all))
	}

	// OR within a Dimension: Growth or Reliability keeps A and B (newest first).
	within := domain.FilterGoals(all, map[int64][]int64{pillar.ID: {growth.ID, reliability.ID}})
	if titles := goalTitles(within); !equalStrings(titles, []string{"Bravo", "Alpha"}) {
		t.Errorf("OR-within filter = %v, want [Bravo Alpha]", titles)
	}

	// AND across Dimensions: Growth and Q1 keeps only A (B has Reliability).
	across := domain.FilterGoals(all, map[int64][]int64{pillar.ID: {growth.ID}, quarter.ID: {q1.ID}})
	if titles := goalTitles(across); !equalStrings(titles, []string{"Alpha"}) {
		t.Errorf("AND-across filter = %v, want [Alpha]", titles)
	}

	// Grouping by Pillar buckets A under Growth, B under Reliability, C unassigned.
	groups := domain.GroupGoalsByDimension(all, pillar)
	if len(groups) != 3 {
		t.Fatalf("groups = %+v, want 3 (Growth, Reliability, unassigned)", groups)
	}
	if groups[0].Value == nil || groups[0].Value.Value != "Growth" || !equalStrings(goalTitles(groups[0].Goals), []string{"Alpha"}) {
		t.Errorf("group[0] = %+v, want Growth -> [Alpha]", groups[0])
	}
	if groups[1].Value == nil || groups[1].Value.Value != "Reliability" || !equalStrings(goalTitles(groups[1].Goals), []string{"Bravo"}) {
		t.Errorf("group[1] = %+v, want Reliability -> [Bravo]", groups[1])
	}
	if groups[2].Value != nil || !equalStrings(goalTitles(groups[2].Goals), []string{"Charlie"}) {
		t.Errorf("group[2] = %+v, want unassigned -> [Charlie]", groups[2])
	}
}

func goalTitles(goals []domain.GoalWithValues) []string {
	out := make([]string, 0, len(goals))
	for _, g := range goals {
		out = append(out, g.Goal.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
