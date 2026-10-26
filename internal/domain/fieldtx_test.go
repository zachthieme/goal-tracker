package domain_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Goal's Field set to two values at the same moment keeps both changes in
// its Value history, the second starting from the value the first left
// (CONTEXT.md: Field).
func TestFieldSetTwiceTogetherKeepsAHistoryThatFollowsOn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	for i := range 30 {
		goal := h.CreateGoal(sam, fmt.Sprintf("Goal %d", i), "It matters.")
		errSam, errBoss := raceAttributeCommands(
			func() error { return h.Service.SetGoalField(ctx, sam.ID, goal.ID, budget.ID, "100") },
			func() error { return h.Service.SetGoalField(ctx, boss.ID, goal.ID, budget.ID, "200") },
		)
		if errSam != nil || errBoss != nil {
			t.Fatalf("round %d: Sam's set err = %v, the Admin's err = %v", i, errSam, errBoss)
		}
		history, err := h.Service.ValueHistory(ctx, goal.ID)
		if err != nil {
			t.Fatalf("round %d: ValueHistory: %v", i, err)
		}
		if len(history) != 2 || history[0].Before != "" || history[1].Before != history[0].After {
			t.Fatalf("round %d: Value history = %+v, want two changes, the second from the first's value", i, history)
		}
	}
}
