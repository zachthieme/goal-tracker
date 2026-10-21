package domain_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// GoalFieldsByGoal reads several Goals' Field values at once, each Goal's the
// same as GoalFields reads it alone: a Retired Field's value included, and
// nothing for a Goal with no values (#448).
func TestGoalFieldsByGoalAgreesWithGoalFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	launch := h.CreateField(boss, "Launch", domain.FieldDate, "")

	both := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.SetGoalField(pat, both, budget, "100")
	h.SetGoalField(pat, both, notes, "Two teams.")
	retired := h.CreateGoal(pat, "Grow signups", "Growth funds us.")
	h.SetGoalField(pat, retired, launch, "2026-11-01")
	h.SetGoalField(pat, retired, notes, "Marketing-led.")
	none := h.CreateGoal(pat, "Hire a lead", "We need one.")
	if err := h.Service.RetireField(ctx, boss.ID, launch.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}

	ids := []int64{both.ID, retired.ID, none.ID}
	got, err := h.Service.GoalFieldsByGoal(ctx, ids)
	if err != nil {
		t.Fatalf("GoalFieldsByGoal: %v", err)
	}
	for _, id := range ids {
		want, err := h.Service.GoalFields(ctx, id)
		if err != nil {
			t.Fatalf("GoalFields(%d): %v", id, err)
		}
		if len(got[id]) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got[id], want)) {
			t.Errorf("GoalFieldsByGoal[%d] = %+v, want GoalFields' %+v", id, got[id], want)
		}
	}
	if values := got[retired.ID]; len(values) != 2 || values[0].Field.ID != launch.ID || !values[0].Field.Retired || values[0].Value != "2026-11-01" {
		t.Errorf("GoalFieldsByGoal[%d] = %+v, want Launch 2026-11-01 flagged Retired, then Notes", retired.ID, values)
	}
	if values := got[none.ID]; len(values) != 0 {
		t.Errorf("GoalFieldsByGoal[%d] = %+v, want no values", none.ID, values)
	}
}
