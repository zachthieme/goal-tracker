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

// Only the Goal's Owner or an Admin may assign its Dimension values; anyone else
// is refused and the Goal's value is left as it was (CONTEXT.md: Owners assign
// Dimension values to their Goals).
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
	// Only the Owner or an Admin may set them.
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
