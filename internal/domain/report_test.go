package domain_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

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

// A Report Definition shows the Fields its author chose beside each Goal that
// has a value in them, in both treatments: the exception block and the
// one-line Green Goal. A chosen Field a Goal has no value in is left out, not
// shown blank, and a Field nobody chose isn't shown at all (ticket #78).
func TestReportShowsChosenFieldsBesideEachGoal(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	sponsor := h.CreateField(boss, "Sponsor", domain.FieldShortText, "")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.SetGoalField(boss, red, budget, "120")
	h.SetGoalField(boss, red, sponsor, "Dana")
	h.SetGoalField(boss, red, notes, "Not for the Report.")
	h.SetGoalField(boss, green, budget, "40")
	settle(h)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:     "MBR",
		RootIDs:  []int64{red.ID, green.ID},
		FieldIDs: []int64{budget.ID, sponsor.ID},
	})
	if got, want := def.FieldIDs, []int64{budget.ID, sponsor.ID}; !sameSet(got, want) {
		t.Errorf("saved Fields %v, want %v", got, want)
	}
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(r.Exceptions) != 1 || len(r.Lines) != 1 {
		t.Fatalf("got %d blocks and %d lines, want the Red block and the Green line", len(r.Exceptions), len(r.Lines))
	}
	if got, want := fieldReadings(r.Exceptions[0].Fields), []string{"Budget=120 $", "Sponsor=Dana"}; !slices.Equal(got, want) {
		t.Errorf("exception block shows Fields %v, want %v", got, want)
	}
	if got, want := fieldReadings(r.Lines[0].Fields), []string{"Budget=40 $"}; !slices.Equal(got, want) {
		t.Errorf("one-line Goal shows Fields %v, want %v (Sponsor unset, so left out)", got, want)
	}

	// A Report shows no Fields unless its author chooses some.
	plain := draftReport(t, h, boss, time.Time{}, red, green)
	if len(plain.Exceptions[0].Fields) != 0 || len(plain.Lines[0].Fields) != 0 {
		t.Errorf("a definition with no Fields chosen shows %v and %v", plain.Exceptions[0].Fields, plain.Lines[0].Fields)
	}
}

// A Report Definition can only be set to show Fields that exist and aren't
// Retired, as a Retired Field is no longer offered (CONTEXT.md: Retired). A
// Field retired after it was chosen keeps showing its values (ADR 0005: saved
// Report Definitions that reference it keep working).
func TestReportDefinitionChoosesOnlyOfferedFields(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SetGoalField(boss, g, budget, "120")
	if err := h.Service.RetireField(ctx, boss.ID, legacy.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}

	for name, ids := range map[string][]int64{"retired": {legacy.ID}, "unknown": {9999}} {
		if _, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
			Name: "MBR", RootIDs: []int64{g.ID}, FieldIDs: ids,
		}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("choosing a %s Field: err = %v, want ErrValidation", name, err)
		}
	}

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}, FieldIDs: []int64{budget.ID}})
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if got, want := fieldReadings(r.Exceptions[0].Fields), []string{"Budget=120 $"}; !slices.Equal(got, want) {
		t.Errorf("after retiring Budget the Goal shows Fields %v, want %v", got, want)
	}
}

// fieldReadings renders Field values as Name=Value Unit, for comparing what a
// Report shows beside a Goal.
func fieldReadings(values []domain.FieldValue) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strings.TrimSpace(v.Field.Name+"="+v.Value+" "+v.Field.Unit))
	}
	return out
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
