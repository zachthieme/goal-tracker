package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// definitionLog reads the whole Definition log, newest first, failing the test
// on error.
func definitionLog(t *testing.T, h *testsupport.Harness) []domain.DefinitionChange {
	t.Helper()
	changes, err := h.Service.DefinitionLog(context.Background())
	if err != nil {
		t.Fatalf("DefinitionLog: %v", err)
	}
	return changes
}

// Defining a Dimension writes one entry saying who created it, when, and with
// which values.
func TestDefiningADimensionWritesOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.Clock.Advance(time.Hour)

	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")

	log := definitionLog(t, h)
	if len(log) != 1 {
		t.Fatalf("log = %+v, want 1 entry", log)
	}
	got := log[0]
	if got.Actor.ID != boss.ID || got.DimensionID != pillar.ID || got.FieldID != 0 ||
		!got.CreatedAt.Equal(testsupport.Epoch.Add(time.Hour)) {
		t.Errorf("entry = %+v, want boss defining Pillar an hour after the epoch", got)
	}
	if want := "Created the Dimension Pillar with Growth, Trust, taking one value from a Fixed list."; got.Summary != want {
		t.Errorf("summary = %q, want %q", got.Summary, want)
	}
}

// Each change to a Dimension's shape writes exactly one entry, by the Admin
// who made it, about that Dimension.
func TestEachChangeToADimensionsShapeWritesOneEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change func(s *domain.Service, adminID, dimensionID int64) error
		want   string
	}{
		{"retire", func(s *domain.Service, a, d int64) error { return s.RetireDimension(ctx, a, d) },
			"Retired the Dimension Pillar."},
		{"take several", func(s *domain.Service, a, d int64) error {
			return s.SetDimensionSelection(ctx, a, d, domain.SelectionSeveral)
		}, "Pillar now takes several values."},
		{"make Extendable", func(s *domain.Service, a, d int64) error {
			return s.SetDimensionList(ctx, a, d, domain.ListExtendable)
		}, "Pillar's list is now Extendable."},
		{"mark required", func(s *domain.Service, a, d int64) error { return s.SetDimensionRequired(ctx, a, d, true) },
			"Marked the Dimension Pillar required."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
			h.Clock.Advance(time.Hour)

			if err := tc.change(h.Service, boss.ID, pillar.ID); err != nil {
				t.Fatalf("change: %v", err)
			}

			log := definitionLog(t, h)
			if len(log) != 2 {
				t.Fatalf("log = %+v, want the creation and 1 entry", log)
			}
			got := log[0]
			if got.Actor.ID != boss.ID || got.DimensionID != pillar.ID || got.Summary != tc.want ||
				!got.CreatedAt.Equal(testsupport.Epoch.Add(time.Hour)) {
				t.Errorf("newest entry = %+v, want boss's %q an hour after the epoch", got, tc.want)
			}
		})
	}
}

// Undoing each change to a Dimension's shape writes its own entry.
func TestUndoingAChangeToADimensionsShapeWritesItsOwnEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateSeveralValuesDimension(boss, "Pillar", "Growth", "Trust")
	for _, change := range []func() error{
		func() error { return h.Service.RetireDimension(ctx, boss.ID, pillar.ID) },
		func() error { return h.Service.RestoreDimension(ctx, boss.ID, pillar.ID) },
		func() error { return h.Service.SetDimensionSelection(ctx, boss.ID, pillar.ID, domain.SelectionOne) },
		func() error { return h.Service.SetDimensionList(ctx, boss.ID, pillar.ID, domain.ListExtendable) },
		func() error { return h.Service.SetDimensionList(ctx, boss.ID, pillar.ID, domain.ListFixed) },
		func() error { return h.Service.SetDimensionRequired(ctx, boss.ID, pillar.ID, true) },
		func() error { return h.Service.SetDimensionRequired(ctx, boss.ID, pillar.ID, false) },
	} {
		if err := change(); err != nil {
			t.Fatalf("change: %v", err)
		}
	}

	want := []string{
		"Marked the Dimension Pillar not required.",
		"Marked the Dimension Pillar required.",
		"Pillar's list is now Fixed.",
		"Pillar's list is now Extendable.",
		"Pillar now takes one value.",
		"Restored the Dimension Pillar.",
		"Retired the Dimension Pillar.",
		"Pillar now takes several values.",
		"Created the Dimension Pillar with Growth, Trust, taking one value from a Fixed list.",
	}
	assertSummaries(t, definitionLog(t, h), want)
}

// assertSummaries fails the test unless log's sentences are want, in order.
func assertSummaries(t *testing.T, log []domain.DefinitionChange, want []string) {
	t.Helper()
	got := make([]string, 0, len(log))
	for _, c := range log {
		got = append(got, c.Summary)
	}
	if !slices.Equal(got, want) {
		t.Errorf("log, newest first =\n%q\nwant\n%q", got, want)
	}
}

// Each change to a Dimension's values writes exactly one entry, about the
// Dimension, naming the values as they were: a rename shows the old and new
// name, a merge both values, and sorting the list alphabetically is one entry.
func TestEachChangeToADimensionsValuesWritesOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Trust", "Growth", "Scale")
	trust, growth, scale := pillar.Values[0], pillar.Values[1], pillar.Values[2]
	for _, change := range []func() error{
		func() error { _, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, "Reach"); return err },
		func() error {
			_, err := h.Service.RenameDimensionValue(ctx, boss.ID, growth.ID, "Expansion")
			return err
		},
		func() error { return h.Service.RetireDimensionValue(ctx, boss.ID, scale.ID) },
		func() error { return h.Service.RestoreDimensionValue(ctx, boss.ID, scale.ID) },
		func() error { return h.Service.MoveDimensionValue(ctx, boss.ID, scale.ID, domain.MoveUp) },
		func() error { return h.Service.MoveDimensionValue(ctx, boss.ID, trust.ID, domain.MoveDown) },
		func() error { return h.Service.SortDimensionValues(ctx, boss.ID, pillar.ID) },
		func() error { return h.Service.MergeDimensionValue(ctx, boss.ID, scale.ID, growth.ID) },
	} {
		if err := change(); err != nil {
			t.Fatalf("change: %v", err)
		}
	}

	log := definitionLog(t, h)
	assertSummaries(t, log, []string{
		"Merged Scale into Expansion in Pillar.",
		"Sorted Pillar's values alphabetically.",
		"Moved Trust down in Pillar.",
		"Moved Scale up in Pillar.",
		"Restored Scale in Pillar.",
		"Retired Scale in Pillar.",
		"Renamed Growth to Expansion in Pillar.",
		"Added Reach to Pillar.",
		"Created the Dimension Pillar with Trust, Growth, Scale, taking one value from a Fixed list.",
	})
	for _, c := range log {
		if c.Actor.ID != boss.ID || c.DimensionID != pillar.ID {
			t.Errorf("entry %q = %+v, want boss's, about Pillar", c.Summary, c)
		}
	}
}

// A value an Owner adds to an Extendable list while setting their Goal's value
// is logged with that Owner; setting a value already on the list logs nothing,
// and neither does setting values on Goals, which is the Goal's history.
func TestAValueAnOwnerAddsToAnExtendableListIsLoggedWithThatOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	logged := len(definitionLog(t, h))

	if _, err := h.Service.AssignGoalValueByName(ctx, pat.ID, goal.ID, customer.ID, "Globex"); err != nil {
		t.Fatalf("add Globex: %v", err)
	}
	if _, err := h.Service.AssignGoalValueByName(ctx, pat.ID, goal.ID, customer.ID, "acme "); err != nil {
		t.Fatalf("set Acme: %v", err)
	}

	log := definitionLog(t, h)
	if len(log) != logged+1 {
		t.Fatalf("log = %+v, want 1 more entry", log)
	}
	if got := log[0]; got.Actor.ID != pat.ID || got.DimensionID != customer.ID || got.Summary != "Added Globex to Customer." {
		t.Errorf("newest entry = %+v, want pat adding Globex to Customer", got)
	}
}

// Each change to a Field's definition writes exactly one entry, by the Admin
// who made it, about that Field.
func TestEachChangeToAFieldWritesOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	for _, change := range []func() error{
		func() error { return h.Service.RetireField(ctx, boss.ID, budget.ID) },
		func() error { return h.Service.RestoreField(ctx, boss.ID, budget.ID) },
		func() error { return h.Service.SetFieldRequired(ctx, boss.ID, budget.ID, true) },
		func() error { return h.Service.SetFieldRequired(ctx, boss.ID, budget.ID, false) },
	} {
		if err := change(); err != nil {
			t.Fatalf("change: %v", err)
		}
	}

	log := definitionLog(t, h)
	assertSummaries(t, log, []string{
		"Marked the Field Budget not required.",
		"Marked the Field Budget required.",
		"Restored the Field Budget.",
		"Retired the Field Budget.",
		"Created the Field Notes, holding a long text.",
		"Created the Field Budget, holding a number in $.",
	})
	for i, c := range log {
		want := budget.ID
		if i == len(log)-2 {
			want = notes.ID
		}
		if c.Actor.ID != boss.ID || c.FieldID != want || c.DimensionID != 0 {
			t.Errorf("entry %q = %+v, want boss's, about Field %d", c.Summary, c, want)
		}
	}
}

// A refused change to a Dimension or Field writes nothing to the log.
func TestARefusedChangeWritesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateSeveralValuesDimension(boss, "Pillar", "Growth", "Trust")
	quarter := h.CreateDimension(boss, "Quarter", "Q1")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	if err := h.Service.SetGoalValues(ctx, pat.ID, goal.ID, pillar.ID, []int64{pillar.Values[0].ID, pillar.Values[1].ID}); err != nil {
		t.Fatalf("give the Goal both Pillars: %v", err)
	}
	before := definitionLog(t, h)

	for name, refused := range map[string]func() error{
		"a non-Admin defining a Dimension": func() error { _, err := h.Service.CreateDimension(ctx, pat.ID, "Team", []string{"Core"}); return err },
		"a non-Admin adding a value":       func() error { _, err := h.Service.AddDimensionValue(ctx, pat.ID, pillar.ID, "Scale"); return err },
		"a non-Admin renaming a value": func() error {
			_, err := h.Service.RenameDimensionValue(ctx, pat.ID, pillar.Values[0].ID, "Expansion")
			return err
		},
		"a non-Admin retiring a value":     func() error { return h.Service.RetireDimensionValue(ctx, pat.ID, pillar.Values[0].ID) },
		"a non-Admin retiring a Dimension": func() error { return h.Service.RetireDimension(ctx, pat.ID, pillar.ID) },
		"a non-Admin marking a Field required": func() error {
			return h.Service.SetFieldRequired(ctx, pat.ID, budget.ID, true)
		},
		"a non-Admin defining a Field": func() error {
			_, err := h.Service.CreateField(ctx, pat.ID, "Notes", domain.FieldLongText, "")
			return err
		},
		"one value while a Goal carries several": func() error {
			return h.Service.SetDimensionSelection(ctx, boss.ID, pillar.ID, domain.SelectionOne)
		},
		"a merge across Dimensions": func() error {
			return h.Service.MergeDimensionValue(ctx, boss.ID, pillar.Values[0].ID, quarter.Values[0].ID)
		},
		"a Field named like a Dimension": func() error {
			_, err := h.Service.CreateField(ctx, boss.ID, "pillar", domain.FieldShortText, "")
			return err
		},
		"an Owner adding to a Fixed list": func() error {
			_, err := h.Service.AssignGoalValueByName(ctx, pat.ID, goal.ID, quarter.ID, "Q2")
			return err
		},
	} {
		if err := refused(); err == nil {
			t.Errorf("%s was allowed", name)
		}
	}

	if after := definitionLog(t, h); len(after) != len(before) {
		t.Errorf("log grew from %d to %d entries: %+v", len(before), len(after), after[:len(after)-len(before)])
	}
}

// A value added to an Extendable list in a table edit that is refused as a
// whole is gone from the log with the rest of the edit: the entry is written
// in the same transaction as the change.
func TestAValueAddedInARefusedTableEditIsNotLogged(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	before := definitionLog(t, h)

	err := h.Service.EditGoalValues(ctx, pat.ID, []domain.ValueEdit{
		{GoalID: goal.ID, DimensionID: customer.ID, NewValue: "Globex"},
		{GoalID: goal.ID, FieldID: budget.ID, Value: "lots"},
	})
	if err == nil {
		t.Fatal("a table edit with a bad number was allowed")
	}

	if after := definitionLog(t, h); len(after) != len(before) {
		t.Errorf("log grew from %d to %d entries: %+v", len(before), len(after), after[:len(after)-len(before)])
	}
	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		for _, v := range d.Values {
			if v.Value == "Globex" {
				t.Errorf("Globex was added to %s by a refused edit", d.Name)
			}
		}
	}
}

// A change that changes nothing writes nothing: adding a value already on the
// list, moving the first value up, sorting a sorted list, or marking a
// Dimension or Field as it already is.
func TestAChangeThatChangesNothingWritesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	before := definitionLog(t, h)

	for _, change := range []func() error{
		func() error { _, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, " growth"); return err },
		func() error {
			_, err := h.Service.RenameDimensionValue(ctx, boss.ID, pillar.Values[0].ID, "Growth")
			return err
		},
		func() error { return h.Service.MoveDimensionValue(ctx, boss.ID, pillar.Values[0].ID, domain.MoveUp) },
		func() error { return h.Service.SortDimensionValues(ctx, boss.ID, pillar.ID) },
		func() error { return h.Service.RestoreDimensionValue(ctx, boss.ID, pillar.Values[0].ID) },
		func() error { return h.Service.RestoreDimension(ctx, boss.ID, pillar.ID) },
		func() error { return h.Service.SetDimensionSelection(ctx, boss.ID, pillar.ID, domain.SelectionOne) },
		func() error { return h.Service.SetDimensionList(ctx, boss.ID, pillar.ID, domain.ListFixed) },
		func() error { return h.Service.SetDimensionRequired(ctx, boss.ID, pillar.ID, false) },
		func() error { return h.Service.RestoreField(ctx, boss.ID, budget.ID) },
		func() error { return h.Service.SetFieldRequired(ctx, boss.ID, budget.ID, false) },
	} {
		if err := change(); err != nil {
			t.Fatalf("change: %v", err)
		}
	}

	if after := definitionLog(t, h); len(after) != len(before) {
		t.Errorf("log grew from %d to %d entries: %+v", len(before), len(after), after[:len(after)-len(before)])
	}
}

// The log narrows to one Dimension, its values' changes included, or to one
// Field, newest first.
func TestTheLogNarrowsToOneDimensionOrField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.CreateDimension(boss, "Quarter", "Q1")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Notes", domain.FieldLongText, "")
	if _, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, "Trust"); err != nil {
		t.Fatalf("add Trust: %v", err)
	}
	h.SetFieldRequired(boss, budget, true)

	pillarLog, err := h.Service.DimensionLog(ctx, pillar.ID)
	if err != nil {
		t.Fatalf("DimensionLog: %v", err)
	}
	assertSummaries(t, pillarLog, []string{
		"Added Trust to Pillar.",
		"Created the Dimension Pillar with Growth, taking one value from a Fixed list.",
	})
	budgetLog, err := h.Service.FieldLog(ctx, budget.ID)
	if err != nil {
		t.Fatalf("FieldLog: %v", err)
	}
	assertSummaries(t, budgetLog, []string{
		"Marked the Field Budget required.",
		"Created the Field Budget, holding a number in $.",
	})
}

// The Service offers no way to edit the log, and the database refuses to
// change or remove an entry whoever asks.
func TestTheLogCannotBeEdited(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.CreateDimension(h.SignIn("boss@example.com"), "Pillar", "Growth")

	for _, stmt := range []string{
		`UPDATE definition_changes SET summary = 'Nothing happened.'`,
		`DELETE FROM definition_changes`,
	} {
		if _, err := h.DB.Exec(stmt); err == nil {
			t.Errorf("%s was allowed", stmt)
		}
	}
	assertSummaries(t, definitionLog(t, h), []string{
		"Created the Dimension Pillar with Growth, taking one value from a Fixed list.",
	})
}
