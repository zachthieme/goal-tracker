package domain_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// raceDimensionCommands runs first and second at the same moment and waits for
// both, returning their errors.
func raceDimensionCommands(first, second func() error) (errFirst, errSecond error) {
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
		errSwitch, errSet := raceDimensionCommands(
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
		errSwitch, errAssign := raceDimensionCommands(
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
