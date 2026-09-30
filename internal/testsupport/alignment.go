package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// MarkTopLevel marks g as a Top-level Goal on behalf of admin, failing the test
// on error. It is a scenario builder for arranging the org's root outcomes.
func (h *Harness) MarkTopLevel(admin domain.Account, g domain.Goal) domain.Goal {
	h.T.Helper()
	marked, err := h.Service.MarkTopLevel(context.Background(), admin.ID, g.ID)
	if err != nil {
		h.T.Fatalf("MarkTopLevel: %v", err)
	}
	return marked
}

// Unaligned returns the Unaligned Goals, failing the test on error.
func (h *Harness) Unaligned() []domain.Goal {
	h.T.Helper()
	goals, err := h.Service.UnalignedGoals(context.Background())
	if err != nil {
		h.T.Fatalf("UnalignedGoals: %v", err)
	}
	return goals
}
