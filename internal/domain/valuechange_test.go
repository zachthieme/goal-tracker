package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// valueHistory reads a Goal's Value history, failing the test on error.
func valueHistory(t *testing.T, h *testsupport.Harness, goalID int64) []domain.ValueChange {
	t.Helper()
	changes, err := h.Service.ValueHistory(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ValueHistory: %v", err)
	}
	return changes
}

// Changing a number Field records who changed it, when, and the value before
// and after (CONTEXT.md: Field — changes are kept in the Goal's history).
func TestChangingAFieldRecordsBeforeAndAfter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetGoalField(pat, goal, budget, "200")
	h.Clock.Advance(time.Hour)

	h.SetGoalField(pat, goal, budget, "350")

	changes := valueHistory(t, h, goal.ID)
	if len(changes) != 2 {
		t.Fatalf("history = %+v, want 2 entries", changes)
	}
	got := changes[1]
	if got.Actor.ID != pat.ID || got.Attribute != "Budget" || got.Before != "200" || got.After != "350" ||
		!got.CreatedAt.Equal(testsupport.Epoch.Add(time.Hour)) {
		t.Errorf("change = %+v, want pat changing Budget 200 → 350 an hour after the epoch", got)
	}
}

// Setting, changing and clearing a Goal's value in a one-value Dimension each
// record one entry, by the Dimension's name and values.
func TestSettingChangingAndClearingADimensionValueEachRecordOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	growth, trust := pillar.Values[0], pillar.Values[1]

	h.AssignGoalValue(goal, growth)
	h.AssignGoalValue(goal, trust)
	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, pillar.ID, nil); err != nil {
		t.Fatalf("clear Pillar: %v", err)
	}

	want := [][2]string{{"", "Growth"}, {"Growth", "Trust"}, {"Trust", ""}}
	changes := valueHistory(t, h, goal.ID)
	if len(changes) != len(want) {
		t.Fatalf("history = %+v, want %d entries", changes, len(want))
	}
	for i, c := range changes {
		if c.Attribute != "Pillar" || c.Before != want[i][0] || c.After != want[i][1] || c.Several || c.Actor.ID != pat.ID {
			t.Errorf("entry %d = %+v, want pat changing Pillar %q → %q", i, c, want[i][0], want[i][1])
		}
	}
}

// Saving a Goal's Dimension values or Fields without changing anything records
// nothing.
func TestSavingWithoutAChangeRecordsNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	region := h.CreateSeveralValuesDimension(boss, "Region", "EMEA", "APAC")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	h.AssignGoalValue(goal, pillar.Values[0])
	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, region.ID, []int64{region.Values[0].ID}); err != nil {
		t.Fatalf("SetGoalValues Region: %v", err)
	}
	h.SetGoalField(pat, goal, budget, "200")
	before := len(valueHistory(t, h, goal.ID))

	h.AssignGoalValue(goal, pillar.Values[0])
	for _, dim := range []struct {
		id  int64
		ids []int64
	}{{pillar.ID, []int64{pillar.Values[0].ID}}, {region.ID, []int64{region.Values[0].ID}}} {
		if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, dim.id, dim.ids); err != nil {
			t.Fatalf("SetGoalValues: %v", err)
		}
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, pat.ID, goal.ID, pillar.ID, " growth "); err != nil {
		t.Fatalf("AssignGoalValueByName: %v", err)
	}
	h.SetGoalField(pat, goal, budget, " 200 ")
	h.SetGoalField(pat, goal, notes, "")

	if after := valueHistory(t, h, goal.ID); len(after) != before {
		t.Errorf("history grew from %d to %d entries on saves that changed nothing: %+v", before, len(after), after[before:])
	}
}

// In a Dimension that takes several values, each value added or removed is its
// own entry, read as "added X" or "removed X".
func TestAddingAndRemovingSeveralValuesRecordsEachValue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	region := h.CreateSeveralValuesDimension(boss, "Region", "EMEA", "APAC", "AMER")
	emea, apac, amer := region.Values[0], region.Values[1], region.Values[2]
	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, region.ID, []int64{emea.ID, apac.ID}); err != nil {
		t.Fatalf("SetGoalValues: %v", err)
	}

	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, region.ID, []int64{apac.ID, amer.ID}); err != nil {
		t.Fatalf("SetGoalValues: %v", err)
	}

	want := [][2]string{{"", "EMEA"}, {"", "APAC"}, {"EMEA", ""}, {"", "AMER"}}
	changes := valueHistory(t, h, goal.ID)
	if len(changes) != len(want) {
		t.Fatalf("history = %+v, want %d entries", changes, len(want))
	}
	for i, c := range changes {
		if c.Attribute != "Region" || !c.Several || c.Before != want[i][0] || c.After != want[i][1] {
			t.Errorf("entry %d = %+v, want a several-values Region entry %q → %q", i, c, want[i][0], want[i][1])
		}
	}
}

// A Delegate's change is attributed to the Delegate, not the Owner
// (CONTEXT.md: Delegate).
func TestADelegatesChangeIsAttributedToTheDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	dee := h.SignInNamed("dee@example.com", "Dee Delegate")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(pat, dee, goal.ID)
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")

	if err := h.Service.AssignGoalValue(ctx, dee.ID, goal.ID, pillar.Values[0].ID); err != nil {
		t.Fatalf("Delegate assigns Pillar: %v", err)
	}
	h.SetGoalField(dee, goal, budget, "350")

	changes := valueHistory(t, h, goal.ID)
	if len(changes) != 2 {
		t.Fatalf("history = %+v, want 2 entries", changes)
	}
	for _, c := range changes {
		if c.Actor.ID != dee.ID || c.Actor.Name != "Dee Delegate" {
			t.Errorf("%s change by %+v, want the Delegate Dee", c.Attribute, c.Actor)
		}
	}
}

// Renaming or merging a value afterwards leaves the earlier entries reading as
// they did: the history keeps the values as they were at the time.
func TestRenamingOrMergingAValueLeavesEarlierEntriesAlone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	other := h.CreateGoal(pat, "Grow revenue", "Revenue funds the rest.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust", "Revenue")
	h.AssignGoalValue(goal, pillar.Values[0])
	h.AssignGoalValue(other, pillar.Values[2])

	if _, err := h.Service.RenameDimensionValue(ctx, boss.ID, pillar.Values[0].ID, "Expansion"); err != nil {
		t.Fatalf("RenameDimensionValue: %v", err)
	}
	if err := h.Service.MergeDimensionValue(ctx, boss.ID, pillar.Values[2].ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("MergeDimensionValue: %v", err)
	}

	for _, tc := range []struct {
		goal  domain.Goal
		after string
	}{{goal, "Growth"}, {other, "Revenue"}} {
		changes := valueHistory(t, h, tc.goal.ID)
		if len(changes) != 1 || changes[0].After != tc.after {
			t.Errorf("%s history = %+v, want one entry still reading %q", tc.goal.Title, changes, tc.after)
		}
	}
}

// When its history entry can't be written, setting or clearing a Goal's Field
// or Dimension value fails and leaves the value as it was: the change and its
// entry are written together.
func TestAChangeWhoseHistoryFailsLeavesTheValueUnchanged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateExtendableDimension(boss, "Pillar", "Growth", "Trust")
	region := h.CreateSeveralValuesDimension(boss, "Region", "EMEA", "APAC")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	h.AssignGoalValue(goal, pillar.Values[0])
	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, region.ID, []int64{region.Values[0].ID}); err != nil {
		t.Fatalf("SetGoalValues Region: %v", err)
	}
	h.SetGoalField(pat, goal, budget, "200")
	h.SetGoalField(pat, goal, notes, "Watch the pager.")
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON goal_value_changes
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}

	for _, change := range []struct {
		what string
		do   func() error
	}{
		{"set a Field", func() error { return h.Service.SetGoalField(ctx, pat.ID, goal.ID, budget.ID, "350") }},
		{"clear a Field", func() error { return h.Service.SetGoalField(ctx, pat.ID, goal.ID, notes.ID, "") }},
		{"assign a value", func() error { return h.Service.AssignGoalValue(ctx, pat.ID, goal.ID, pillar.Values[1].ID) }},
		{"assign a value by name", func() error {
			_, err := h.Service.AssignGoalValueByName(ctx, pat.ID, goal.ID, pillar.ID, "trust")
			return err
		}},
		{"set several values", func() error {
			return h.Service.SetGoalValues(ctx, pat.ID, goal.ID, region.ID, []int64{region.Values[1].ID})
		}},
		{"clear a Dimension", func() error { return h.Service.SetGoalValues(ctx, pat.ID, goal.ID, pillar.ID, nil) }},
	} {
		if err := change.do(); err == nil {
			t.Errorf("%s succeeded despite the injected failure", change.what)
		}
	}

	if got := goalValueNames(t, h, goal.ID); !equalStrings(got, []string{"Growth", "EMEA"}) {
		t.Errorf("values after failed changes = %v, want [Growth EMEA]", got)
	}
	if got := goalFieldValues(t, h, goal.ID); got["Budget"] != "200" || got["Notes"] != "Watch the pager." {
		t.Errorf("Fields after failed changes = %v, want Budget 200 and Notes kept", got)
	}
}

// Clearing a required one-value Dimension on an Active Goal is allowed: it
// records one "cleared" entry and leaves the Goal Incomplete. Clearing it again,
// with no value left, records nothing.
func TestClearingARequiredDimensionRecordsOneEntryAndLeavesTheGoalIncomplete(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	h.AssignGoalValue(goal, pillar.Values[0])

	for range 2 {
		if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, pillar.ID, nil); err != nil {
			t.Fatalf("clear Pillar: %v", err)
		}
	}

	changes := valueHistory(t, h, goal.ID)
	if len(changes) != 2 || changes[1].Before != "Growth" || changes[1].After != "" {
		t.Errorf("history = %+v, want Pillar set then cleared once", changes)
	}
	if missing, err := h.Service.Incomplete(ctx, goal); err != nil || !equalStrings(missing, []string{"Pillar"}) {
		t.Errorf("Incomplete = %v (%v), want [Pillar]", missing, err)
	}
}
