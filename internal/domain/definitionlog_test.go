package domain_test

import (
	"context"
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
