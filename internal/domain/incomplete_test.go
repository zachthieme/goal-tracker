package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// readyGoal is a Proposed Goal that meets every activation rule but the
// required values: Dated with a Milestone.
func readyGoal(t *testing.T, h *testsupport.Harness, owner domain.Account, title string) domain.Goal {
	t.Helper()
	g := h.CreateGoal(owner, title, "It matters.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	addMilestone(t, h, g.ID)
	return g
}

// A Proposed Goal can't become Active while it lacks a value in a required
// Dimension, and the refusal names the Dimension (CONTEXT.md: Incomplete).
func TestActivateRefusesGoalWithoutRequiredDimensionValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.SetDimensionRequired(boss, pillar, true)
	g := readyGoal(t, h, sam, "Ship v2")

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Pillar") {
		t.Errorf("refusal should name Pillar; got %q", err.Error())
	}
	if got, _ := h.Service.ViewGoal(context.Background(), g.ID); got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want Proposed", got.Lifecycle)
	}

	h.AssignGoalValue(g, pillar.Values[0])
	if _, err := h.Service.ActivateGoal(context.Background(), g.ID); err != nil {
		t.Errorf("ActivateGoal with the value: %v", err)
	}
}

// A required Field holds activation the same way, and the refusal names it.
func TestActivateRefusesGoalWithoutRequiredFieldValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(boss, budget, true)
	g := readyGoal(t, h, sam, "Ship v2")

	_, err := h.Service.ActivateGoal(context.Background(), g.ID)
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), "Budget") {
		t.Fatalf("err = %v, want a validation error naming Budget", err)
	}

	h.SetGoalField(sam, g, budget, "1000")
	if _, err := h.Service.ActivateGoal(context.Background(), g.ID); err != nil {
		t.Errorf("ActivateGoal with the value: %v", err)
	}
}

// A Retired Dimension or Field is never required, so it holds nothing up.
func TestRetiredRequiredDimensionOrFieldHoldsNothing(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	h.SetFieldRequired(boss, budget, true)
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	g := readyGoal(t, h, sam, "Ship v2")

	if _, err := h.Service.ActivateGoal(ctx, g.ID); err != nil {
		t.Errorf("ActivateGoal: %v", err)
	}
}

// Only an Admin marks a Dimension or Field required.
func TestOnlyAdminMarksRequired(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	ctx := context.Background()

	if err := h.Service.SetDimensionRequired(ctx, sam.ID, pillar.ID, true); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("SetDimensionRequired by non-Admin err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.SetFieldRequired(ctx, sam.ID, budget.ID, true); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("SetFieldRequired by non-Admin err = %v, want ErrNotAuthorized", err)
	}
	dims, _ := h.Service.ListDimensions(ctx)
	fields, _ := h.Service.ListFields(ctx)
	if dims[0].Required || fields[0].Required {
		t.Errorf("Required = %v, %v after refusals, want false", dims[0].Required, fields[0].Required)
	}
	h.SetDimensionRequired(boss, pillar, true)
	h.SetFieldRequired(boss, budget, true)
	dims, _ = h.Service.ListDimensions(ctx)
	fields, _ = h.Service.ListFields(ctx)
	if !dims[0].Required || !fields[0].Required {
		t.Errorf("Required = %v, %v after an Admin marked them, want true", dims[0].Required, fields[0].Required)
	}
}

func incomplete(t *testing.T, h *testsupport.Harness, goalID int64) []string {
	t.Helper()
	g, err := h.Service.ViewGoal(context.Background(), goalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	missing, err := h.Service.Incomplete(context.Background(), g)
	if err != nil {
		t.Fatalf("Incomplete: %v", err)
	}
	return missing
}

// Marking a Dimension required leaves Active Goals Active, and flags the ones
// without a value Incomplete, naming the Dimension (CONTEXT.md: Incomplete).
// Setting the value clears the flag.
func TestMarkingRequiredFlagsActiveGoalsWithoutAValue(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	bare := h.ActiveGoal(sam, "Ship v2", "It matters.")
	tagged := h.ActiveGoal(sam, "Cut costs", "It matters.")
	h.AssignGoalValue(tagged, pillar.Values[0])
	h.SetGoalField(sam, tagged, budget, "10")

	h.SetDimensionRequired(boss, pillar, true)
	h.SetFieldRequired(boss, budget, true)

	if got, _ := h.Service.ViewGoal(context.Background(), bare.ID); got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want Active", got.Lifecycle)
	}
	if got := incomplete(t, h, bare.ID); strings.Join(got, ", ") != "Pillar, Budget" {
		t.Errorf("Incomplete = %q, want Pillar, Budget", got)
	}
	if got := incomplete(t, h, tagged.ID); len(got) != 0 {
		t.Errorf("Goal with both values: Incomplete = %q, want none", got)
	}

	h.AssignGoalValue(bare, pillar.Values[0])
	h.SetGoalField(sam, bare, budget, "5")
	if got := incomplete(t, h, bare.ID); len(got) != 0 {
		t.Errorf("after setting the values: Incomplete = %q, want none", got)
	}
}

// Unmarking required clears the flag for every Goal.
func TestUnmarkingRequiredClearsIncomplete(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	one := h.ActiveGoal(sam, "Ship v2", "It matters.")
	two := h.ActiveGoal(sam, "Cut costs", "It matters.")
	h.SetDimensionRequired(boss, pillar, true)
	h.SetFieldRequired(boss, budget, true)

	h.SetDimensionRequired(boss, pillar, false)
	h.SetFieldRequired(boss, budget, false)

	for _, g := range []domain.Goal{one, two} {
		if got := incomplete(t, h, g.ID); len(got) != 0 {
			t.Errorf("%s: Incomplete = %q, want none", g.Title, got)
		}
	}
}

// Only an Active Goal is ever Incomplete: On Hold, Done, Cancelled and Proposed
// Goals lacking a required value are not.
func TestOnlyActiveGoalsAreIncomplete(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	proposed := readyGoal(t, h, sam, "Proposed")
	onHold := h.OnHoldGoal(sam, "On Hold", "It matters.", "Waiting on legal.")
	cancelled := h.ActiveGoal(sam, "Cancelled", "It matters.")
	changeLifecycle(t, h, sam, cancelled, domain.LifecycleCancelled)
	done := h.ActiveGoal(sam, "Done", "It matters.")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: done.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin Done: %v", err)
	}
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)

	for _, g := range []domain.Goal{proposed, onHold, cancelled, done} {
		if got := incomplete(t, h, g.ID); len(got) != 0 {
			t.Errorf("%s Goal: Incomplete = %q, want none", g.Title, got)
		}
	}
}
