package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Admin defines a Field of each type, a number one with its unit; a
// non-Admin cannot (CONTEXT.md: Admin defines Fields; Field).
func TestAdminDefinesFieldOfEachType(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	budget, err := h.Service.CreateField(ctx, boss.ID, "Budget", domain.FieldNumber, "$")
	if err != nil {
		t.Fatalf("CreateField number: %v", err)
	}
	if budget.Name != "Budget" || budget.Type != domain.FieldNumber || budget.Unit != "$" || budget.Retired {
		t.Errorf("Budget = %+v, want a number Field in $", budget)
	}
	for _, typ := range []string{domain.FieldShortText, domain.FieldLongText, domain.FieldDate} {
		if _, err := h.Service.CreateField(ctx, boss.ID, "A "+typ, typ, ""); err != nil {
			t.Errorf("CreateField %s: %v", typ, err)
		}
	}

	fields, err := h.Service.ListFields(ctx)
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	types := map[string]string{}
	for _, f := range fields {
		types[f.Name] = f.Type
	}
	want := map[string]string{
		"Budget":       domain.FieldNumber,
		"A short_text": domain.FieldShortText,
		"A long_text":  domain.FieldLongText,
		"A date":       domain.FieldDate,
	}
	for name, typ := range want {
		if types[name] != typ {
			t.Errorf("Field %q type = %q, want %q (fields %+v)", name, types[name], typ, fields)
		}
	}

	if _, err := h.Service.CreateField(ctx, sam.ID, "Notes", domain.FieldLongText, ""); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin CreateField err = %v, want ErrNotAuthorized", err)
	}
}

// A Field can't take a Dimension's name whatever its letter case, nor another
// Field's, so a name on a Goal says which attribute it is (ADR 0005).
func TestFieldNameCannotRepeatADimensionOrField(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()
	h.CreateDimension(boss, "Pillar", "Growth")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")

	for _, name := range []string{"Pillar", "pillar", " PILLAR ", "budget"} {
		_, err := h.Service.CreateField(ctx, boss.ID, name, domain.FieldShortText, "")
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("CreateField(%q) err = %v, want ErrValidation", name, err)
		}
	}
}

// The Owner and a Delegate set and clear a value of each type on a Goal; a
// Contributor can't (CONTEXT.md: Delegate; Contributor).
func TestOwnerAndDelegateSetAndClearFields(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	dee := h.SignIn("dee@example.com")
	cal := h.SignIn("cal@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(pat, dee, goal.ID)
	if err := h.Service.AddContributor(ctx, goal.ID, cal.ID); err != nil {
		t.Fatalf("AddContributor: %v", err)
	}
	set := map[domain.Field]string{
		h.CreateField(boss, "Budget", domain.FieldNumber, "$"): "1200.50",
		h.CreateField(boss, "Doc", domain.FieldShortText, ""):  "https://example.com/doc",
		h.CreateField(boss, "Notes", domain.FieldLongText, ""): "First line\nSecond line",
		h.CreateField(boss, "Kickoff", domain.FieldDate, ""):   "2026-03-01",
	}

	for _, actor := range []domain.Account{pat, dee} {
		for f, value := range set {
			if err := h.Service.SetGoalField(ctx, actor.ID, goal.ID, f.ID, value); err != nil {
				t.Fatalf("%s sets %s: %v", actor.Email, f.Name, err)
			}
		}
		got := goalFieldValues(t, h, goal.ID)
		for f, value := range set {
			if got[f.Name] != value {
				t.Errorf("after %s sets them, %s = %q, want %q", actor.Email, f.Name, got[f.Name], value)
			}
		}
		for f := range set {
			if err := h.Service.SetGoalField(ctx, actor.ID, goal.ID, f.ID, "  "); err != nil {
				t.Fatalf("%s clears %s: %v", actor.Email, f.Name, err)
			}
		}
		if got := goalFieldValues(t, h, goal.ID); len(got) != 0 {
			t.Errorf("after %s clears them, values = %v, want none", actor.Email, got)
		}
	}

	for f, value := range set {
		if err := h.Service.SetGoalField(ctx, cal.ID, goal.ID, f.ID, value); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("Contributor sets %s: err = %v, want ErrNotAuthorized", f.Name, err)
		}
	}
}

// goalFieldValues reads a Goal's Field values by Field name.
func goalFieldValues(t *testing.T, h *testsupport.Harness, goalID int64) map[string]string {
	t.Helper()
	values, err := h.Service.GoalFields(context.Background(), goalID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	out := map[string]string{}
	for _, v := range values {
		out[v.Field.Name] = v.Value
	}
	return out
}

// A number Field takes only a number and a date Field only a date; a bad value
// is refused naming the Field, and the Goal keeps the value it had.
func TestFieldRefusesAValueOfTheWrongType(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	kickoff := h.CreateField(boss, "Kickoff", domain.FieldDate, "")
	h.SetGoalField(pat, goal, budget, "-12.5")
	h.SetGoalField(pat, goal, kickoff, "2026-03-01")

	for _, tc := range []struct {
		field domain.Field
		value string
	}{
		{budget, "abc"}, {budget, "NaN"}, {budget, "Inf"}, {budget, "12 FTE"},
		{kickoff, "tomorrow"}, {kickoff, "2026-02-30"}, {kickoff, "03/01/2026"},
	} {
		err := h.Service.SetGoalField(ctx, pat.ID, goal.ID, tc.field.ID, tc.value)
		if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), tc.field.Name) {
			t.Errorf("%s = %q: err = %v, want ErrValidation naming %s", tc.field.Name, tc.value, err, tc.field.Name)
		}
	}
	got := goalFieldValues(t, h, goal.ID)
	if got["Budget"] != "-12.5" || got["Kickoff"] != "2026-03-01" {
		t.Errorf("after refusals, values = %v, want Budget -12.5 and Kickoff 2026-03-01", got)
	}
}

// An Admin retires a Field: it takes no value any more, yet a Goal with a value
// in it still has it; restoring it offers it again. A non-Admin may do neither
// (CONTEXT.md: Retired).
func TestAdminRetiresAndRestoresAField(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	ctx := context.Background()
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	h.SetGoalField(pat, goal, budget, "100")

	if err := h.Service.RetireField(ctx, pat.ID, budget.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RetireField err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	fields, err := h.Service.ListFields(ctx)
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if offered := domain.OfferedFields(fields); len(offered) != 1 || offered[0].ID != notes.ID {
		t.Errorf("OfferedFields = %+v, want only Notes", offered)
	}
	if err := h.Service.SetGoalField(ctx, pat.ID, goal.ID, budget.ID, "200"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("setting a Retired Field: err = %v, want ErrValidation", err)
	}
	values, err := h.Service.GoalFields(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	if len(values) != 1 || values[0].Value != "100" || !values[0].Field.Retired {
		t.Errorf("GoalFields = %+v, want Budget 100 flagged Retired", values)
	}

	if err := h.Service.RestoreField(ctx, pat.ID, budget.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("non-Admin RestoreField err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RestoreField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RestoreField: %v", err)
	}
	h.SetGoalField(pat, goal, budget, "200")
	if got := goalFieldValues(t, h, goal.ID); got["Budget"] != "200" {
		t.Errorf("after restore, Budget = %q, want 200", got["Budget"])
	}
	if err := h.Service.RestoreField(ctx, boss.ID, 9999); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("RestoreField of no Field: err = %v, want ErrValidation", err)
	}
}
