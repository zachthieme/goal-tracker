package domain_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// raceAttributeCommands runs first and second at the same moment and waits for
// both, returning their errors.
func raceAttributeCommands(first, second func() error) (errFirst, errSecond error) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errFirst = first()
	}()
	go func() {
		defer wg.Done()
		<-start
		errSecond = second()
	}()
	close(start)
	wg.Wait()
	return errFirst, errSecond
}

// A Dimension switched to one value while a Goal is given two of its values
// at the same moment ends one way or the other, never taking one value while
// the Goal carries two (CONTEXT.md: Dimension).
func TestSwitchToOneValueAndSettingTwoValuesTogetherHaveOneOutcome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	core, infra := teams.Values[0], teams.Values[1]
	for i := range 30 {
		goal := h.CreateGoal(sam, fmt.Sprintf("Goal %d", i), "It matters.")
		errSwitch, errSet := raceAttributeCommands(
			func() error { return h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionOne) },
			func() error {
				return h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, []int64{core.ID, infra.ID})
			},
		)
		if errSwitch == nil && errSet == nil {
			t.Fatalf("round %d: switched to one value and set two values both succeeded", i)
		}
		if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, nil); err != nil {
			t.Fatalf("round %d: clear values: %v", i, err)
		}
		if err := h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionSeveral); err != nil {
			t.Fatalf("round %d: switch back to several: %v", i, err)
		}
	}
}

// A Dimension switched to one value while a Goal carrying one of its values is
// given another at the same moment ends one way or the other, never taking one
// value while the Goal carries two (CONTEXT.md: Dimension).
func TestSwitchToOneValueAndAssigningASecondValueTogetherHaveOneOutcome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	core, infra := teams.Values[0], teams.Values[1]
	for i := range 30 {
		goal := h.CreateGoal(sam, fmt.Sprintf("Goal %d", i), "It matters.")
		h.AssignGoalValue(goal, core)
		errSwitch, errAssign := raceAttributeCommands(
			func() error { return h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionOne) },
			func() error { return h.Service.AssignGoalValue(ctx, sam.ID, goal.ID, infra.ID) },
		)
		if errSwitch != nil {
			if err := h.Service.SetGoalValues(ctx, sam.ID, goal.ID, teams.ID, nil); err != nil {
				t.Fatalf("round %d: clear values: %v", i, err)
			}
			continue
		}
		values, err := h.Service.GoalValues(ctx, goal.ID)
		if err != nil {
			t.Fatalf("round %d: GoalValues: %v", i, err)
		}
		if len(values) > 1 {
			t.Fatalf("round %d: switched to one value (assign err = %v) yet the Goal carries %+v", i, errAssign, values)
		}
		if err := h.Service.SetDimensionSelection(ctx, boss.ID, teams.ID, domain.SelectionSeveral); err != nil {
			t.Fatalf("round %d: switch back to several: %v", i, err)
		}
	}
}

// Two values each merged into the other at the same moment: one merge is made
// and the other refused, so the Dimension keeps one of them, carried by every
// Goal that carried either (CONTEXT.md: Extendable — merging values).
func TestTwoValuesMergedIntoEachOtherTogetherOnlyOneMerges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	for i := range 30 {
		dim := h.CreateDimension(boss, fmt.Sprintf("Customer %d", i), "Acme", "ACME Corp")
		acme, acmeCorp := dim.Values[0], dim.Values[1]
		goal := h.CreateGoal(sam, fmt.Sprintf("Goal %d", i), "It matters.")
		h.AssignGoalValue(goal, acme)
		errOne, errOther := raceAttributeCommands(
			func() error { return h.Service.MergeDimensionValue(ctx, boss.ID, acme.ID, acmeCorp.ID) },
			func() error { return h.Service.MergeDimensionValue(ctx, boss.ID, acmeCorp.ID, acme.ID) },
		)
		if (errOne == nil) == (errOther == nil) {
			t.Fatalf("round %d: merge one way err = %v, the other way err = %v; want exactly one merged", i, errOne, errOther)
		}
		for _, err := range []error{errOne, errOther} {
			if err != nil && !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("round %d: err = %v, want ErrValidation for the one refused", i, err)
			}
		}
		if got := goalValueNames(t, h, goal.ID); len(got) != 1 {
			t.Fatalf("round %d: Goal carries %v, want the one value left", i, got)
		}
	}
}

// One value added to a Dimension's list twice at the same moment, differing
// only in case, is added once: the list gains no near-duplicate (CONTEXT.md:
// Admins add values).
func TestValueAddedTwiceTogetherIsAddedOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ctx := context.Background()

	customer := h.CreateDimension(boss, "Customer", "Globex")
	for i := range 30 {
		errOne, errOther := raceAttributeCommands(
			func() error {
				_, err := h.Service.AddDimensionValue(ctx, boss.ID, customer.ID, fmt.Sprintf("Acme %d", i))
				return err
			},
			func() error {
				_, err := h.Service.AddDimensionValue(ctx, boss.ID, customer.ID, fmt.Sprintf("acme %d", i))
				return err
			},
		)
		if errOne != nil || errOther != nil {
			t.Fatalf("round %d: add err = %v, the other add err = %v", i, errOne, errOther)
		}
		dims, err := h.Service.ListDimensions(ctx)
		if err != nil {
			t.Fatalf("round %d: ListDimensions: %v", i, err)
		}
		if got := len(dims[0].Values); got != i+2 {
			t.Fatalf("round %d: Customer has %d values %+v, want %d", i, got, dims[0].Values, i+2)
		}
	}
}
