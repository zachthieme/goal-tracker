package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Report Definition selects a root Goal and its descendants down to a depth,
// following accepted links only (CONTEXT.md: Report Definition). depth 1 reaches
// the root's direct children; depth 2 reaches their children too.
func TestReportSelectsRootsToDepth(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")

	// A (root) <- B, C (contribute to A); D contributes to B.
	a := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	b := h.ActiveChildOf(boss, a, "Launch in EU", "Expand the market.")
	c := h.ActiveChildOf(boss, a, "Cut churn", "Keep customers.")
	d := h.ActiveChildOf(boss, b, "Localize checkout", "EU shoppers need local payment.")

	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:    "EU MBR",
		RootIDs: []int64{a.ID},
		Depth:   1,
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition: %v", err)
	}

	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals: %v", err)
	}
	if got, want := selectedIDs(selected), []int64{a.ID, b.ID, c.ID}; !sameSet(got, want) {
		t.Errorf("depth 1 selected %v, want %v", got, want)
	}

	def.Depth = 2
	selected, err = h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals depth 2: %v", err)
	}
	if got, want := selectedIDs(selected), []int64{a.ID, b.ID, c.ID, d.ID}; !sameSet(got, want) {
		t.Errorf("depth 2 selected %v, want %v", got, want)
	}
}

// A filter-only Report Definition (no roots) draws from every Goal and keeps
// those matching its Owner and Dimension filters (CONTEXT.md: filters alone).
// Filters apply after any traversal, so here they select across the whole org.
func TestReportFilterOnlySelection(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	growth, reliability := pillar.Values[0], pillar.Values[1]

	// bossGrowth matches both an Owner filter (boss) and a Dimension filter
	// (Growth); the others each miss at least one.
	bossGrowth := h.CreateGoal(boss, "Boss growth goal", "why")
	bossReliability := h.CreateGoal(boss, "Boss reliability goal", "why")
	samGrowth := h.CreateGoal(sam, "Sam growth goal", "why")
	h.AssignGoalValue(bossGrowth, growth)
	h.AssignGoalValue(bossReliability, reliability)
	h.AssignGoalValue(samGrowth, growth)

	// Dimension filter alone: every Growth Goal, regardless of Owner.
	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:              "Growth report",
		DimensionValueIDs: []int64{growth.ID},
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition (dimension): %v", err)
	}
	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals (dimension): %v", err)
	}
	if got, want := selectedIDs(selected), []int64{bossGrowth.ID, samGrowth.ID}; !sameSet(got, want) {
		t.Errorf("Dimension filter selected %v, want %v", got, want)
	}

	// Owner filter combined with the Dimension filter: only boss's Growth Goal.
	def, err = h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:              "Boss growth report",
		OwnerFilterID:     boss.ID,
		DimensionValueIDs: []int64{growth.ID},
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition (owner+dimension): %v", err)
	}
	selected, err = h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals (owner+dimension): %v", err)
	}
	if got, want := selectedIDs(selected), []int64{bossGrowth.ID}; !sameSet(got, want) {
		t.Errorf("Owner+Dimension filter selected %v, want %v", got, want)
	}
}

// A Report Definition with no name, or with no roots and no filters, is rejected
// (CONTEXT.md: a Report Definition selects Goals — root Goals, filters, or both).
func TestReportDefinitionValidation(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")

	if _, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		OwnerFilterID: boss.ID,
	}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("no name err = %v, want ErrValidation", err)
	}
	if _, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name: "Selects nothing",
	}); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("no roots and no filters err = %v, want ErrValidation", err)
	}
}

// A Goal reachable through several accepted-link paths, or under several roots,
// is selected once (CONTEXT.md: a Goal reached through several paths appears
// once). Here a diamond A -> {B, C} -> D reaches D through both B and C, and D is
// also its own overlapping root.
func TestReportDeduplicatesAcrossPaths(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")

	a := h.ActiveGoal(boss, "Grow revenue", "why")
	b := h.ActiveChildOf(boss, a, "Launch EU", "why")
	c := h.ActiveChildOf(boss, a, "Cut churn", "why")
	d := h.ActiveChildOf(boss, b, "Localize checkout", "why")
	// D contributes to C as well, closing the diamond.
	h.RequestLink(boss, d, c, "")

	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name: "Diamond report",
		// D is listed as a root too, so it is reachable three ways in all.
		RootIDs: []int64{a.ID, d.ID},
		Depth:   2,
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition: %v", err)
	}
	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals: %v", err)
	}
	// sameSet also fails if any id appears more than once in the result.
	if got, want := selectedIDs(selected), []int64{a.ID, b.ID, c.ID, d.ID}; !sameSet(got, want) {
		t.Errorf("selected %v, want each of %v exactly once", got, want)
	}
}

// Filters apply after traversal: a roots+depth definition first gathers the
// descendants, then keeps only those matching the filter (CONTEXT.md: filters
// apply after traversal). The live draft carries each selected Goal's current
// Health from its latest Check-in.
func TestReportFilterAppliesAfterTraversal(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	growth := pillar.Values[0]

	a := h.ActiveGoal(boss, "Grow revenue", "why")
	b := h.ActiveChildOf(boss, a, "Launch EU", "why")
	c := h.ActiveChildOf(boss, a, "Cut churn", "why")
	h.AssignGoalValue(b, growth) // only B carries the Growth value
	h.Checkin(boss, b.ID, domain.HealthYellow, "slipping", "add staff", h.Clock.Now().AddDate(0, 1, 0))

	def, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
		Name:              "EU growth report",
		RootIDs:           []int64{a.ID},
		Depth:             1,
		DimensionValueIDs: []int64{growth.ID},
	})
	if err != nil {
		t.Fatalf("SaveReportDefinition: %v", err)
	}
	selected, err := h.Service.SelectGoals(ctx, def)
	if err != nil {
		t.Fatalf("SelectGoals: %v", err)
	}
	// Traversal reaches A, B, C; the Growth filter keeps only B.
	if got, want := selectedIDs(selected), []int64{b.ID}; !sameSet(got, want) {
		t.Fatalf("selected %v, want %v (only the Growth descendant)", got, want)
	}
	if selected[0].Health != domain.HealthYellow {
		t.Errorf("Health = %q, want %q", selected[0].Health, domain.HealthYellow)
	}
	_ = c
}

func selectedIDs(selected []domain.SelectedGoal) []int64 {
	ids := make([]int64, 0, len(selected))
	for _, s := range selected {
		ids = append(ids, s.Goal.ID)
	}
	return ids
}

// sameSet reports whether got and want hold the same ids, order-independent, and
// with no id appearing more than once in got (selection de-duplicates).
func sameSet(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[int64]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] != 1 {
			return false
		}
	}
	return true
}
