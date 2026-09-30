package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// CreateGoal creates a Proposed Goal owned by owner, failing the test on error.
// It is a scenario builder for arranging Goals a test needs.
func (h *Harness) CreateGoal(owner domain.Account, title, soWhat string) domain.Goal {
	h.T.Helper()
	g, err := h.Service.CreateGoal(context.Background(), domain.CreateGoalInput{
		Title:   title,
		SoWhat:  soWhat,
		OwnerID: owner.ID,
	})
	if err != nil {
		h.T.Fatalf("CreateGoal: %v", err)
	}
	return g
}
