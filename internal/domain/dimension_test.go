package domain_test

import (
	"context"
	"errors"
	"strings"
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

	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, pillar.Values[0].ID); err != nil {
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
	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("AssignGoalValue Reliability: %v", err)
	}
	// A value in another Dimension coexists.
	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, quarter.Values[0].ID); err != nil {
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

// Only the Goal's Owner, a Delegate or an Admin may assign its Dimension
// values; anyone else is refused and the Goal's value is left as it was
// (CONTEXT.md: Owners assign Dimension values to their Goals).
func TestOnlyOwnerOrAdminAssignsDimensionValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	other := h.SignIn("other@example.com")
	ctx := context.Background()

	quarter := h.CreateDimension(boss, "Quarter", "Q2", "Q3", "Q4")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, quarter.Values[2])

	if err := h.Service.AssignGoalValue(ctx, other.ID, goal.ID, quarter.Values[1].ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Owner AssignGoalValue err = %v, want ErrNotAuthorized", err)
	}
	values, err := h.Service.GoalValues(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Q4" {
		t.Errorf("after refused assign, values = %+v, want Q4 unchanged", values)
	}

	if err := h.Service.AssignGoalValue(ctx, boss.ID, goal.ID, quarter.Values[0].ID); err != nil {
		t.Fatalf("Admin AssignGoalValue: %v", err)
	}
	values, err = h.Service.GoalValues(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Q2" {
		t.Errorf("after Admin assign, values = %+v, want Q2", values)
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
	if err := h.Service.AssignGoalValue(ctx, sam.ID, fresh.ID, growth.ID); !errors.Is(err, domain.ErrValidation) {
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

// A Dimension takes one value per Goal until an Admin switches it to several;
// a non-Admin cannot switch it (CONTEXT.md: Dimension — the Admin decides
// whether a Goal takes one value or several).
func TestAdminSwitchesDimensionToSeveralValues(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	if pillar.Selection != domain.SelectionOne {
		t.Fatalf("new Dimension Selection = %q, want %q", pillar.Selection, domain.SelectionOne)
	}

	if err := h.Service.SetDimensionSelection(ctx, sam.ID, pillar.ID, domain.SelectionSeveral); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin SetDimensionSelection err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.SetDimensionSelection(ctx, boss.ID, pillar.ID, "many"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("unknown selection err = %v, want ErrValidation", err)
	}
	if err := h.Service.SetDimensionSelection(ctx, boss.ID, pillar.ID, domain.SelectionSeveral); err != nil {
		t.Fatalf("SetDimensionSelection several: %v", err)
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if len(dims) != 1 || dims[0].Selection != domain.SelectionSeveral {
		t.Errorf("ListDimensions = %+v, want Pillar taking several values", dims)
	}
}

// In a several-values Dimension an Owner gives a Goal two values, and setting
// the Goal's values again with one of them removes the other; a one-value
// Dimension refuses two (CONTEXT.md: Dimension).
func TestOwnerSetsSeveralValuesInDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	other := h.SignIn("other@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra", "Web")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	core, infra := teams.Values[0], teams.Values[1]
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, []int64{core.ID, infra.ID}); err != nil {
		t.Fatalf("SetGoalValues Core+Infra: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Core", "Infra"}) {
		t.Fatalf("values = %v, want [Core Infra]", got)
	}

	if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, []int64{infra.ID}); err != nil {
		t.Fatalf("SetGoalValues Infra: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Infra"}) {
		t.Errorf("after removing Core, values = %v, want [Infra]", got)
	}

	// Assigning a single value adds it alongside the Goal's others.
	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, core.ID); err != nil {
		t.Fatalf("AssignGoalValue Core: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Core", "Infra"}) {
		t.Errorf("after assigning Core, values = %v, want [Core Infra]", got)
	}

	// Two values in a one-value Dimension are refused.
	if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, pillar.ID, []int64{pillar.Values[0].ID, pillar.Values[1].ID}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("two values in a one-value Dimension err = %v, want ErrValidation", err)
	}
	// A value from another Dimension is refused.
	if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, []int64{pillar.Values[0].ID}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("value from another Dimension err = %v, want ErrValidation", err)
	}
	// Only the Owner, a Delegate or an Admin may set them.
	if err := h.Service.SetGoalValues(ctx, other.ID, goal.ID, teams.ID, nil); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Owner SetGoalValues err = %v, want ErrNotAuthorized", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Core", "Infra"}) {
		t.Errorf("after refused sets, values = %v, want [Core Infra]", got)
	}

	// Setting none clears the Dimension.
	if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, nil); err != nil {
		t.Fatalf("SetGoalValues none: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); len(got) != 0 {
		t.Errorf("after clearing, values = %v, want none", got)
	}
}

// A retired value the Goal already carries can be kept when its values are set
// again, but can't be newly given to it (CONTEXT.md: Retired).
func TestSetGoalValuesKeepsButNeverAddsRetiredValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	core, infra := teams.Values[0], teams.Values[1]
	carrier := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	fresh := h.CreateGoal(sam, "Ship faster", "Slow ships lose deals.")
	h.AssignGoalValue(carrier, core)
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, core.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	if err := h.Service.SetGoalValues(ctx, sam.ID, carrier.ID, teams.ID, []int64{core.ID, infra.ID}); err != nil {
		t.Fatalf("keeping a carried retired value: %v", err)
	}
	if got := goalValueNames(t, h, carrier.ID); !equalStrings(got, []string{"Core", "Infra"}) {
		t.Errorf("values = %v, want [Core Infra]", got)
	}
	if err := h.Service.SetGoalValues(ctx, sam.ID, fresh.ID, teams.ID, []int64{core.ID}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("newly giving a retired value err = %v, want ErrValidation", err)
	}
}

func goalValueNames(t *testing.T, h *testsupport.Harness, goalID int64) []string {
	t.Helper()
	values, err := h.Service.GoalValues(context.Background(), goalID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.Value)
	}
	return out
}

// Grouped by a several-values Dimension, a Goal with two values appears under
// each of them (ADR 0005: group counts can add up to more than the Goals).
func TestGroupBySeveralValuesDimensionListsGoalUnderEachValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	core, infra := teams.Values[0], teams.Values[1]
	shared := h.CreateGoal(sam, "Alpha", "A matters.")
	solo := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(shared, core)
	h.AssignGoalValue(shared, infra)
	h.AssignGoalValue(solo, infra)

	all, err := h.Service.ListGoalsWithValues(ctx)
	if err != nil {
		t.Fatalf("ListGoalsWithValues: %v", err)
	}
	groups := domain.GroupGoalsByDimension(all, teams)
	if len(groups) != 2 {
		t.Fatalf("groups = %+v, want Core and Infra", groups)
	}
	if groups[0].Value == nil || groups[0].Value.Value != "Core" || !equalStrings(goalTitles(groups[0].Goals), []string{"Alpha"}) {
		t.Errorf("group[0] = %+v, want Core -> [Alpha]", groups[0])
	}
	if groups[1].Value == nil || groups[1].Value.Value != "Infra" || !equalStrings(goalTitles(groups[1].Goals), []string{"Bravo", "Alpha"}) {
		t.Errorf("group[1] = %+v, want Infra -> [Bravo Alpha]", groups[1])
	}
}

// Switching a Dimension from several values back to one is refused while any
// Goal carries more than one value in it, and the refusal names those Goals; it
// succeeds once none does.
func TestSwitchingToOneValueRefusedWhileGoalsCarrySeveral(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra", "Web")
	core, infra, web := teams.Values[0], teams.Values[1], teams.Values[2]
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	bravo := h.CreateGoal(sam, "Bravo", "B matters.")
	charlie := h.CreateGoal(sam, "Charlie", "C matters.")
	h.AssignGoalValue(alpha, core)
	h.AssignGoalValue(alpha, infra)
	h.AssignGoalValue(bravo, infra)
	h.AssignGoalValue(bravo, web)
	h.AssignGoalValue(charlie, web)

	err := h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionOne)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("switch to one err = %v, want ErrValidation", err)
	}
	var several *domain.SeveralValuesError
	if !errors.As(err, &several) {
		t.Fatalf("switch to one err = %T %v, want *SeveralValuesError", err, err)
	}
	var titles []string
	for _, g := range several.Goals {
		titles = append(titles, g.Title)
	}
	if !equalStrings(titles, []string{"Alpha", "Bravo"}) {
		t.Errorf("refusal names %v, want [Alpha Bravo]", titles)
	}
	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if dims[0].Selection != domain.SelectionSeveral {
		t.Errorf("after refusal Selection = %q, want several", dims[0].Selection)
	}

	if err := h.Service.SetGoalValues(ctx, sam.ID, alpha.ID, teams.ID, []int64{core.ID}); err != nil {
		t.Fatalf("SetGoalValues Alpha: %v", err)
	}
	if err := h.Service.SetGoalValues(ctx, sam.ID, bravo.ID, teams.ID, []int64{web.ID}); err != nil {
		t.Fatalf("SetGoalValues Bravo: %v", err)
	}
	if err := h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionOne); err != nil {
		t.Fatalf("switch to one once no Goal carries several: %v", err)
	}
	if got := goalValueNames(t, h, charlie.ID); !equalStrings(got, []string{"Web"}) {
		t.Errorf("Charlie values = %v, want [Web] kept", got)
	}
}

// A Report Definition filtering on a value selects a Goal carrying that value
// among several in the Dimension (filtering is unchanged: values OR'd within a
// Dimension).
func TestReportFilterSelectsGoalCarryingValueAmongSeveral(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra", "Web")
	core, infra, web := teams.Values[0], teams.Values[1], teams.Values[2]
	shared := h.CreateGoal(sam, "Alpha", "A matters.")
	other := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(shared, core)
	h.AssignGoalValue(shared, infra)
	h.AssignGoalValue(other, web)

	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:              "Infra report",
		DimensionValueIDs: []int64{infra.ID},
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition: %v", err)
	}
	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals: %v", err)
	}
	if got, want := selectedIDs(selected), []int64{shared.ID}; !sameSet(got, want) {
		t.Errorf("Infra filter selected %v, want %v", got, want)
	}
}

// A new Dimension's list is Fixed; an Admin marks it Extendable and switches it
// back, and a non-Admin can do neither (CONTEXT.md: Fixed, Extendable).
func TestAdminSwitchesDimensionBetweenFixedAndExtendable(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateDimension(boss, "Customer", "Acme")
	if customer.Extendable() {
		t.Fatalf("a new Dimension is Extendable, want Fixed")
	}

	if err := h.Service.SetDimensionList(ctx, boss.ID, customer.ID, domain.ListExtendable); err != nil {
		t.Fatalf("SetDimensionList Extendable: %v", err)
	}
	if got := dimensionNamed(t, h, "Customer"); !got.Extendable() {
		t.Errorf("Customer List = %q after marking it Extendable", got.List)
	}

	if err := h.Service.SetDimensionList(ctx, sam.ID, customer.ID, domain.ListFixed); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin SetDimensionList err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.SetDimensionList(ctx, boss.ID, customer.ID, "open"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("unknown list kind err = %v, want ErrValidation", err)
	}

	if err := h.Service.SetDimensionList(ctx, boss.ID, customer.ID, domain.ListFixed); err != nil {
		t.Fatalf("SetDimensionList Fixed: %v", err)
	}
	if got := dimensionNamed(t, h, "Customer"); got.Extendable() {
		t.Errorf("Customer List = %q after switching back to Fixed", got.List)
	}
}

func dimensionNamed(t *testing.T, h *testsupport.Harness, name string) domain.Dimension {
	t.Helper()
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no Dimension named %q", name)
	return domain.Dimension{}
}

// A Goal's Delegate sets its values like its Owner, in one-value and
// several-values Dimensions alike; a Contributor can't (CONTEXT.md: Delegate,
// Contributor).
func TestDelegateSetsGoalValuesButContributorCannot(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	cory := h.SignIn("cory@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	if err := h.Service.AddContributorByEmail(ctx, goal.ID, "cory@example.com"); err != nil {
		t.Fatalf("AddContributorByEmail: %v", err)
	}

	if err := h.Service.AssignGoalValue(ctx, dee.ID, goal.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("Delegate AssignGoalValue: %v", err)
	}
	if err := h.Service.SetGoalValues(ctx, dee.ID, goal.ID, teams.ID, []int64{teams.Values[0].ID, teams.Values[1].ID}); err != nil {
		t.Fatalf("Delegate SetGoalValues: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Reliability", "Core", "Infra"}) {
		t.Fatalf("values = %v, want [Reliability Core Infra]", got)
	}

	if err := h.Service.AssignGoalValue(ctx, cory.ID, goal.ID, pillar.Values[0].ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Contributor AssignGoalValue err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.SetGoalValues(ctx, cory.ID, goal.ID, teams.ID, nil); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Contributor SetGoalValues err = %v, want ErrNotAuthorized", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Reliability", "Core", "Infra"}) {
		t.Errorf("after the Contributor's refused sets, values = %v, want them unchanged", got)
	}
}

// On an Extendable Dimension an Owner names a new value: it joins the list and
// the Goal carries it in one step. In a one-value Dimension it replaces the
// Goal's value; in a several-values one it joins the Goal's others (CONTEXT.md:
// Extendable).
func TestOwnerAddsNewValueToExtendableDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	partners := h.CreateExtendableDimension(boss, "Partner", "Initech")
	h.SetDimensionSelection(boss, partners, domain.SelectionSeveral)
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, customer.Values[0])

	globex, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "  Globex ")
	if err != nil {
		t.Fatalf("AssignGoalValueByName Globex: %v", err)
	}
	if globex.Value != "Globex" || globex.DimensionID != customer.ID {
		t.Errorf("added value = %+v, want Globex in Customer", globex)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, partners.ID, "Umbrella"); err != nil {
		t.Fatalf("AssignGoalValueByName Umbrella: %v", err)
	}
	h.AssignGoalValue(goal, partners.Values[0])

	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Globex", "Initech", "Umbrella"}) {
		t.Errorf("values = %v, want [Globex Initech Umbrella]", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex]", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Partner").Values); !equalStrings(got, []string{"Initech", "Umbrella"}) {
		t.Errorf("Partner list = %v, want [Initech Umbrella]", got)
	}

	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "  "); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("blank value err = %v, want ErrValidation", err)
	}
}

func valueNames(values []domain.DimensionValue) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, v.Value)
	}
	return out
}

// On a Fixed Dimension only an Admin adds a value, so an Owner naming a new one
// is refused and nothing changes; an Admin's goes through. Switching an
// Extendable list back to Fixed stops further additions but keeps the values
// already added (CONTEXT.md: Fixed).
func TestNewValueOnFixedDimensionRefusedForNonAdmin(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "Globex"); err != nil {
		t.Fatalf("Owner adds Globex while Extendable: %v", err)
	}
	if err := h.Service.SetDimensionList(ctx, boss.ID, customer.ID, domain.ListFixed); err != nil {
		t.Fatalf("SetDimensionList Fixed: %v", err)
	}

	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "Initech"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Owner adding to a Fixed list err = %v, want ErrNotAuthorized", err)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex] kept and nothing added", got)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Globex"}) {
		t.Errorf("values = %v, want [Globex] unchanged", got)
	}

	if _, err := h.Service.AssignGoalValueByName(ctx, boss.ID, goal.ID, customer.ID, "Initech"); err != nil {
		t.Fatalf("Admin adding to a Fixed list: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Initech"}) {
		t.Errorf("values = %v, want [Initech]", got)
	}
}

// A named value matches an existing one whatever its case or surrounding
// spaces, so "ACME " sets Acme and adds nothing — on a Fixed list too, where
// choosing an existing value needs no Admin. A match on a Retired value is
// refused, saying it is retired (CONTEXT.md: Extendable, Retired).
func TestNamedValueMatchesExistingWhateverItsCase(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateExtendableDimension(boss, "Customer", "Acme", "Hooli")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	got, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "ACME ")
	if err != nil {
		t.Fatalf("AssignGoalValueByName ACME: %v", err)
	}
	if got.ID != customer.Values[0].ID {
		t.Errorf("ACME set %+v, want the existing Acme", got)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, teams.ID, " core"); err != nil {
		t.Fatalf("Owner naming an existing value on a Fixed list: %v", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Acme", "Core"}) {
		t.Errorf("values = %v, want [Acme Core]", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Hooli"}) {
		t.Errorf("Customer list = %v, want [Acme Hooli] with nothing created", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Team").Values); !equalStrings(got, []string{"Core", "Infra"}) {
		t.Errorf("Team list = %v, want [Core Infra] with nothing created", got)
	}

	if err := h.Service.RetireDimensionValue(ctx, boss.ID, customer.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	_, err = h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "hooli")
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "Hooli is retired") {
		t.Errorf("naming a Retired value err = %v, want an ErrValidation saying Hooli is retired", err)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Hooli"}) {
		t.Errorf("Customer list = %v, want [Acme Hooli] with nothing created", got)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Acme", "Core"}) {
		t.Errorf("values = %v, want [Acme Core] unchanged", got)
	}
}

// An Admin adding a value on the Dimensions page that matches an existing one
// whatever its case adds nothing, and one matching a Retired value is refused,
// saying it is retired (CONTEXT.md: Retired).
func TestAdminAddingNearDuplicateValueAddsNothing(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Legacy")
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	got, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, " growth ")
	if err != nil {
		t.Fatalf("AddDimensionValue growth: %v", err)
	}
	if got.ID != pillar.Values[0].ID {
		t.Errorf("adding growth returned %+v, want the existing Growth", got)
	}
	if _, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, "LEGACY"); !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "Legacy is retired") {
		t.Errorf("adding LEGACY err = %v, want an ErrValidation saying Legacy is retired", err)
	}
	if got := valueNames(dimensionNamed(t, h, "Pillar").Values); !equalStrings(got, []string{"Growth", "Legacy"}) {
		t.Errorf("Pillar list = %v, want [Growth Legacy] with nothing added", got)
	}

	quarter := h.CreateDimension(boss, "Quarter", "Q1", " q1", "Q2")
	if got := valueNames(quarter.Values); !equalStrings(got, []string{"Q1", "Q2"}) {
		t.Errorf("Quarter defined with Q1 and q1 = %v, want [Q1 Q2]", got)
	}
}

// A Delegate adds a new value to an Extendable list from the Goal, like its
// Owner; a Contributor can't, and nothing is added (CONTEXT.md: Delegate,
// Extendable).
func TestDelegateAddsToExtendableListButContributorCannot(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	cory := h.SignIn("cory@example.com")
	ctx := context.Background()

	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	if err := h.Service.AddContributorByEmail(ctx, goal.ID, "cory@example.com"); err != nil {
		t.Fatalf("AddContributorByEmail: %v", err)
	}

	if _, err := h.Service.AssignGoalValueByName(ctx, dee.ID, goal.ID, customer.ID, "Globex"); err != nil {
		t.Fatalf("Delegate AssignGoalValueByName: %v", err)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, cory.ID, goal.ID, customer.ID, "Initech"); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Contributor AssignGoalValueByName err = %v, want ErrNotAuthorized", err)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Globex"}) {
		t.Errorf("values = %v, want [Globex]", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex]", got)
	}
}

// A value added to a Dimension lands at the end of its list, in the order
// added, whoever adds it: the Admin's own values keep the order they were
// typed, and an Owner's addition to an Extendable list comes last.
func TestNewValuesGoLastInOrderAdded(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateExtendableDimension(boss, "Customer", "Umbrella", "Initech")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	if _, err := h.Service.AddDimensionValue(ctx, boss.ID, customer.ID, "Globex"); err != nil {
		t.Fatalf("AddDimensionValue: %v", err)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "Acme"); err != nil {
		t.Fatalf("AssignGoalValueByName: %v", err)
	}

	want := []string{"Umbrella", "Initech", "Globex", "Acme"}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, want) {
		t.Errorf("Customer list = %v, want %v", got, want)
	}
}

// An Admin moves a value up or down its Dimension's list, and sorts the list
// alphabetically whatever the values' case; moving the first value up or the
// last down leaves the list as it is. A non-Admin may do none of these.
func TestAdminReordersDimensionValues(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Reliability", "growth", "Efficiency")
	reliability, growth, efficiency := pillar.Values[0], pillar.Values[1], pillar.Values[2]
	order := func() []string { return valueNames(dimensionNamed(t, h, "Pillar").Values) }

	if err := h.Service.MoveDimensionValue(ctx, boss.ID, efficiency.ID, domain.MoveUp); err != nil {
		t.Fatalf("MoveDimensionValue up: %v", err)
	}
	if got, want := order(), []string{"Reliability", "Efficiency", "growth"}; !equalStrings(got, want) {
		t.Errorf("after moving Efficiency up = %v, want %v", got, want)
	}
	if err := h.Service.MoveDimensionValue(ctx, boss.ID, reliability.ID, domain.MoveDown); err != nil {
		t.Fatalf("MoveDimensionValue down: %v", err)
	}
	if got, want := order(), []string{"Efficiency", "Reliability", "growth"}; !equalStrings(got, want) {
		t.Errorf("after moving Reliability down = %v, want %v", got, want)
	}
	if err := h.Service.MoveDimensionValue(ctx, boss.ID, efficiency.ID, domain.MoveUp); err != nil {
		t.Fatalf("MoveDimensionValue first up: %v", err)
	}
	if err := h.Service.MoveDimensionValue(ctx, boss.ID, growth.ID, domain.MoveDown); err != nil {
		t.Fatalf("MoveDimensionValue last down: %v", err)
	}
	if got, want := order(), []string{"Efficiency", "Reliability", "growth"}; !equalStrings(got, want) {
		t.Errorf("after moving past the ends = %v, want %v unchanged", got, want)
	}
	if err := h.Service.MoveDimensionValue(ctx, boss.ID, growth.ID, "sideways"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("move sideways err = %v, want ErrValidation", err)
	}

	if err := h.Service.SortDimensionValues(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("SortDimensionValues: %v", err)
	}
	if got, want := order(), []string{"Efficiency", "growth", "Reliability"}; !equalStrings(got, want) {
		t.Errorf("after sorting = %v, want %v", got, want)
	}

	if err := h.Service.MoveDimensionValue(ctx, sam.ID, efficiency.ID, domain.MoveDown); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin move err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.MoveDimensionValue(ctx, boss.ID, 9999, domain.MoveDown); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("move of a missing value err = %v, want ErrValidation", err)
	}
	if err := h.Service.SortDimensionValues(ctx, sam.ID, pillar.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin sort err = %v, want ErrNotAuthorized", err)
	}
}

// An Admin merges one value into another in the same Dimension: every Goal
// carrying the merged value carries the target instead, once if it had both,
// every Report Definition filter on it points at the target, and the merged
// value is gone from the list.
func TestAdminMergesDimensionValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateSeveralValuesDimension(boss, "Customer", "Acme", "ACME Corp", "Globex")
	acme, acmeCorp, globex := customer.Values[0], customer.Values[1], customer.Values[2]
	onlyMerged := h.CreateGoal(sam, "Alpha", "A matters.")
	both := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(onlyMerged, acmeCorp)
	h.AssignGoalValue(onlyMerged, globex)
	h.AssignGoalValue(both, acme)
	h.AssignGoalValue(both, acmeCorp)
	onMerged := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Acme Corp", DimensionValueIDs: []int64{acmeCorp.ID}})
	onBoth := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Acmes", DimensionValueIDs: []int64{acme.ID, acmeCorp.ID, globex.ID}})

	if err := h.Service.MergeDimensionValue(ctx, boss.ID, acmeCorp.ID, acme.ID); err != nil {
		t.Fatalf("MergeDimensionValue: %v", err)
	}

	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex]", got)
	}
	if got := goalValueNames(t, h, onlyMerged.ID); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Alpha's values = %v, want [Acme Globex]", got)
	}
	if got := goalValueNames(t, h, both.ID); !equalStrings(got, []string{"Acme"}) {
		t.Errorf("Bravo's values = %v, want [Acme] once", got)
	}
	for _, c := range []struct {
		def  domain.ReportDefinition
		want []int64
	}{{onMerged, []int64{acme.ID}}, {onBoth, []int64{acme.ID, globex.ID}}} {
		def, err := h.Service.GetReportDefinition(ctx, c.def.ID)
		if err != nil {
			t.Fatalf("GetReportDefinition: %v", err)
		}
		if !sameSet(def.DimensionValueIDs, c.want) || len(def.DimensionValueIDs) != len(c.want) {
			t.Errorf("%s filters = %v, want %v", def.Name, def.DimensionValueIDs, c.want)
		}
	}
}

// Merging is refused across Dimensions, into the value itself, and for
// anyone but an Admin; a refused merge changes nothing.
func TestMergeDimensionValueRefusals(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateDimension(boss, "Customer", "Acme", "Globex")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	acme, globex, growth := customer.Values[0], customer.Values[1], pillar.Values[0]
	goal := h.CreateGoal(sam, "Alpha", "A matters.")
	h.AssignGoalValue(goal, acme)

	if err := h.Service.MergeDimensionValue(ctx, boss.ID, acme.ID, growth.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("merge across Dimensions err = %v, want ErrValidation", err)
	}
	if err := h.Service.MergeDimensionValue(ctx, boss.ID, acme.ID, acme.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("merge into itself err = %v, want ErrValidation", err)
	}
	if err := h.Service.MergeDimensionValue(ctx, boss.ID, acme.ID, 9999); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("merge into a missing value err = %v, want ErrValidation", err)
	}
	if err := h.Service.MergeDimensionValue(ctx, sam.ID, acme.ID, globex.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin merge err = %v, want ErrNotAuthorized", err)
	}

	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list after refused merges = %v, want [Acme Globex]", got)
	}
	if got := valueNames(dimensionNamed(t, h, "Pillar").Values); !equalStrings(got, []string{"Growth"}) {
		t.Errorf("Pillar list after refused merges = %v, want [Growth]", got)
	}
	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Acme"}) {
		t.Errorf("Alpha's values after refused merges = %v, want [Acme]", got)
	}
}

// A merge that fails part-way changes nothing: the Goals keep the merged
// value, the Report Definition still filters on it, and it stays listed.
func TestFailedMergeChangesNothing(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	customer := h.CreateDimension(boss, "Customer", "Acme", "ACME Corp")
	acme, acmeCorp := customer.Values[0], customer.Values[1]
	goal := h.CreateGoal(sam, "Alpha", "A matters.")
	h.AssignGoalValue(goal, acmeCorp)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Acme Corp", DimensionValueIDs: []int64{acmeCorp.ID}})
	// Fail the last step, after the Goals and filters have moved.
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_merge BEFORE DELETE ON dimension_values
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}

	if err := h.Service.MergeDimensionValue(ctx, boss.ID, acmeCorp.ID, acme.ID); err == nil {
		t.Fatal("MergeDimensionValue succeeded despite the injected failure")
	}

	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"ACME Corp"}) {
		t.Errorf("Alpha's values after a failed merge = %v, want [ACME Corp]", got)
	}
	if got, err := h.Service.GetReportDefinition(ctx, def.ID); err != nil || !sameSet(got.DimensionValueIDs, []int64{acmeCorp.ID}) {
		t.Errorf("filters after a failed merge = %v (%v), want [%d]", got.DimensionValueIDs, err, acmeCorp.ID)
	}
	if got := valueNames(dimensionNamed(t, h, "Customer").Values); !equalStrings(got, []string{"Acme", "ACME Corp"}) {
		t.Errorf("Customer list after a failed merge = %v, want [Acme ACME Corp]", got)
	}
}

// An Admin retires a Dimension: it stays listed, flagged Retired, a Goal that
// carries a value in it still reads that value, marked as being in a Retired
// Dimension, and none of its values can be newly given to a Goal. A non-Admin
// can't retire one (CONTEXT.md: Retired).
func TestAdminRetiresDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	growth, trust := pillar.Values[0], pillar.Values[1]
	core, infra := teams.Values[0], teams.Values[1]
	carrier := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	fresh := h.CreateGoal(sam, "Ship faster", "Slow ships lose deals.")
	h.AssignGoalValue(carrier, growth)
	h.AssignGoalValue(carrier, core)

	if err := h.Service.RetireDimension(ctx, sam.ID, pillar.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RetireDimension err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension Pillar: %v", err)
	}
	if err := h.Service.RetireDimension(ctx, boss.ID, teams.ID); err != nil {
		t.Fatalf("RetireDimension Team: %v", err)
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if len(dims) != 2 || !dims[0].Retired || !dims[1].Retired {
		t.Errorf("dimensions = %+v, want Pillar and Team both listed, Retired", dims)
	}

	values, err := h.Service.GoalValues(ctx, carrier.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 2 || !values[0].DimensionRetired || !values[1].DimensionRetired {
		t.Errorf("values = %+v, want Growth and Core still read, each in a Retired Dimension", values)
	}

	if err := h.Service.AssignGoalValue(ctx, sam.ID, fresh.ID, trust.ID); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("assigning a value of a Retired Dimension err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, boss.ID, fresh.ID, pillar.ID, "Speed"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("adding a value to a Retired Dimension through a Goal err = %v, want ErrValidation", err)
	}
	if err := h.Service.SetGoalValues(ctx, sam.ID, fresh.ID, teams.ID, []int64{infra.ID}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("setting values of a Retired Dimension err = %v, want ErrValidation", err)
	}
	// A Goal saving the values it already carries keeps them.
	if err := h.Service.SetGoalValues(ctx, sam.ID, carrier.ID, teams.ID, []int64{core.ID}); err != nil {
		t.Errorf("keeping a carried value of a Retired Dimension: %v", err)
	}
}

// An Admin restores a Retired Dimension and a Retired value, and each can be
// given to a Goal again; a non-Admin can restore neither (CONTEXT.md: Retired —
// an Admin can reverse it).
func TestAdminRestoresRetiredDimensionAndValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	quarter := h.CreateDimension(boss, "Quarter", "Q1")
	growth, q1 := pillar.Values[0], quarter.Values[0]
	goal := h.CreateGoal(sam, "Ship faster", "Slow ships lose deals.")
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, q1.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	if err := h.Service.RestoreDimension(ctx, sam.ID, pillar.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RestoreDimension err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RestoreDimensionValue(ctx, sam.ID, q1.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RestoreDimensionValue err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	if err := h.Service.RestoreDimensionValue(ctx, boss.ID, q1.ID); err != nil {
		t.Fatalf("RestoreDimensionValue: %v", err)
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.Retired || d.Values[0].Retired {
			t.Errorf("dimension = %+v, want it and its value restored", d)
		}
	}
	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, growth.ID); err != nil {
		t.Errorf("assigning a value of a restored Dimension: %v", err)
	}
	if err := h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, q1.ID); err != nil {
		t.Errorf("assigning a restored value: %v", err)
	}
	if err := h.Service.RestoreDimension(ctx, boss.ID, 9999); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("restoring a missing Dimension err = %v, want ErrValidation", err)
	}
}

// A Report Definition saved with a filter on a Dimension's value selects the
// same Goals once the Dimension is Retired (ADR 0005: saved Report Definitions
// keep working).
func TestReportFilterOnRetiredDimensionSelectsSameGoals(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	growth, trust := pillar.Values[0], pillar.Values[1]
	grower := h.CreateGoal(sam, "Alpha", "A matters.")
	truster := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(grower, growth)
	h.AssignGoalValue(truster, trust)
	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:              "Growth report",
		DimensionValueIDs: []int64{growth.ID},
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition: %v", err)
	}

	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals: %v", err)
	}
	if got, want := selectedIDs(selected), []int64{grower.ID}; !sameSet(got, want) {
		t.Errorf("Growth filter on a Retired Pillar selected %v, want %v", got, want)
	}
}

// A value can't contain a semicolon, since the import format separates values
// with one: defining a Dimension, adding a value, renaming one and naming a new
// one on a Goal are each refused, saying why, and nothing is added (#101).
func TestValueContainingSemicolonIsRefused(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")

	refusals := map[string]error{}
	_, refusals["DefineDimension"] = h.Service.CreateDimension(ctx, boss.ID, "Pillar", []string{"Growth", "R&D; Ops"})
	_, refusals["AddDimensionValue"] = h.Service.AddDimensionValue(ctx, boss.ID, customer.ID, "Acme; Globex")
	_, refusals["RenameDimensionValue"] = h.Service.RenameDimensionValue(ctx, boss.ID, customer.Values[0].ID, "Acme;")
	_, refusals["AssignGoalValueByName"] = h.Service.AssignGoalValueByName(ctx, sam.ID, goal.ID, customer.ID, "Initech; Umbrella")
	refusals["EditGoalValues"] = h.Service.EditGoalValues(ctx, sam.ID, []domain.ValueEdit{{GoalID: goal.ID, DimensionID: customer.ID, NewValue: "Hooli;"}})
	for call, err := range refusals {
		if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "can't contain a semicolon") {
			t.Errorf("%s err = %v, want a refusal saying a value can't contain a semicolon", call, err)
		}
	}

	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if len(dims) != 1 || !equalStrings(valueNames(dims[0].Values), []string{"Acme"}) {
		t.Errorf("Dimensions = %+v, want only Customer, still [Acme]", dims)
	}
	if got := goalValueNames(t, h, goal.ID); len(got) != 0 {
		t.Errorf("Goal values = %v, want none", got)
	}
}

// A value named with a semicolon before that was refused keeps working: it is
// read off its Goal, filters and groups as any value does, and an Admin may
// rename it to a name without one, though not to another with one (#101).
func TestValueAlreadyContainingSemicolonKeepsWorking(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	rnd := h.NameValueWithSemicolon(pillar.Values[1], "R&D; Ops")
	pillar.Values[1] = rnd
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	bravo := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	h.AssignGoalValue(bravo, rnd)

	if got := goalValueNames(t, h, bravo.ID); !equalStrings(got, []string{"R&D; Ops"}) {
		t.Errorf("Bravo's values = %v, want [R&D; Ops]", got)
	}
	all, err := h.Service.ListGoalsWithValues(ctx)
	if err != nil {
		t.Fatalf("ListGoalsWithValues: %v", err)
	}
	if got := goalTitles(domain.FilterGoals(all, map[int64][]int64{pillar.ID: {rnd.ID}})); !equalStrings(got, []string{"Bravo"}) {
		t.Errorf("filter on R&D; Ops = %v, want [Bravo]", got)
	}
	groups := domain.GroupGoalsByDimension(all, pillar)
	if len(groups) < 2 || groups[1].Value == nil || groups[1].Value.Value != "R&D; Ops" || !equalStrings(goalTitles(groups[1].Goals), []string{"Bravo"}) {
		t.Errorf("groups = %+v, want R&D; Ops -> [Bravo] second", groups)
	}

	if _, err := h.Service.RenameDimensionValue(ctx, boss.ID, rnd.ID, "R&D; Platform"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("rename to another name with a semicolon err = %v, want ErrValidation", err)
	}
	renamed, err := h.Service.RenameDimensionValue(ctx, boss.ID, rnd.ID, "R&D and Ops")
	if err != nil {
		t.Fatalf("RenameDimensionValue: %v", err)
	}
	if renamed.ID != rnd.ID || renamed.Value != "R&D and Ops" {
		t.Errorf("renamed = %+v, want value %d named R&D and Ops", renamed, rnd.ID)
	}
	if got := goalValueNames(t, h, bravo.ID); !equalStrings(got, []string{"R&D and Ops"}) {
		t.Errorf("Bravo's values after rename = %v, want [R&D and Ops]", got)
	}
}

// A Dimension can't take a Field's name whatever its letter case or
// surrounding spaces, nor another Dimension's, and the refusal says which one
// has it: Dimensions and Fields share one namespace (ADR 0005).
func TestDimensionNameCannotRepeatAFieldOrDimension(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateDimension(boss, "Pillar", "Growth")

	for name, want := range map[string]string{
		"budget":     "Budget is already a Field's name",
		" BUDGET ":   "Budget is already a Field's name",
		"pillar":     "Pillar is already a Dimension's name",
		"  Pillar  ": "Pillar is already a Dimension's name",
	} {
		_, err := h.Service.CreateDimension(ctx, boss.ID, name, []string{"Q1"})
		if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), want) {
			t.Errorf("CreateDimension(%q) err = %v, want ErrValidation saying %q", name, err, want)
		}
	}
	if got := len(dimensionList(t, h)); got != 1 {
		t.Errorf("Dimensions after refused definitions = %d, want 1", got)
	}
}

// dimensionList lists every Dimension, failing the test on error.
func dimensionList(t *testing.T, h *testsupport.Harness) []domain.Dimension {
	t.Helper()
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	return dims
}

// Renaming a value to match another in its Dimension, whatever its case or
// spacing, is refused saying to merge the two instead, and renames nothing;
// changing only the case or spacing of a value's own name is a rename.
func TestRenamingAValueIntoAnotherIsRefused(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Payments")
	payments := pillar.Values[1]

	for _, name := range []string{"growth", " GROWTH ", "Growth"} {
		_, err := h.Service.RenameDimensionValue(ctx, boss.ID, payments.ID, name)
		if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "merge") {
			t.Errorf("rename Payments to %q err = %v, want ErrValidation saying to merge", name, err)
		}
	}
	if got := valueNames(dimensionNamed(t, h, "Pillar").Values); !equalStrings(got, []string{"Growth", "Payments"}) {
		t.Fatalf("Pillar after refused renames = %v, want [Growth Payments]", got)
	}

	renamed, err := h.Service.RenameDimensionValue(ctx, boss.ID, payments.ID, " PAYMENTS ")
	if err != nil || renamed.Value != "PAYMENTS" {
		t.Errorf("rename Payments to its own name in capitals = %+v, %v; want PAYMENTS", renamed, err)
	}
}

// Marking a Retired Dimension required, or not required, is refused, even when
// it would change nothing, and leaves its setting as it was: required means
// nothing on a Dimension no longer offered (CONTEXT.md: Retired).
func TestRetiredDimensionsRequiredSettingCannotChange(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}

	for _, required := range []bool{false, true} {
		if err := h.Service.SetDimensionRequired(ctx, boss.ID, pillar.ID, required); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("mark Retired Pillar required=%v: err = %v, want ErrValidation", required, err)
		}
	}
	if !dimensionNamed(t, h, "Pillar").Required {
		t.Errorf("Retired Pillar lost its required setting")
	}
}
