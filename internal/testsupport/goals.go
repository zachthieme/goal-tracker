package testsupport

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// CreateGoal creates a Proposed Goal owned by owner, failing the test on error.
// It is a scenario builder for arranging Goals a test needs.
func (h *Harness) CreateGoal(owner domain.Account, title, whyItMatters string) domain.Goal {
	h.T.Helper()
	g, err := h.Service.CreateGoal(h.T.Context(), domain.CreateGoalInput{
		Title:        title,
		WhyItMatters: whyItMatters,
		OwnerID:      owner.ID,
	})
	if err != nil {
		h.T.Fatalf("CreateGoal: %v", err)
	}
	return g
}

// GoalOwnerID returns the ID of goalID's Owner, failing the test on error, for
// a test that has only the Goal's ID to hand and needs to act as its Owner.
func (h *Harness) GoalOwnerID(goalID int64) int64 {
	h.T.Helper()
	g, err := h.Service.ViewGoal(h.T.Context(), goalID)
	if err != nil {
		h.T.Fatalf("ViewGoal: %v", err)
	}
	return g.Owner.ID
}
