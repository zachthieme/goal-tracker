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
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Trust", "Growth", "Scale")
	trust, growth, scale := pillar.Values[0], pillar.Values[1], pillar.Values[2]
	for _, change := range []func() error{
		func() error { _, err := h.Service.AddDimensionValue(ctx, boss.ID, pillar.ID, "Reach"); return err },
		func() error { _, err := h.Service.RenameDimensionValue(ctx, boss.ID, growth.ID, "Expansion"); return err },
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
