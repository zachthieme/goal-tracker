package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// AddDelegate authorizes delegate to write Check-ins on goal, acting as owner,
// failing the test on error. It is a scenario builder for arranging a Goal's
// Delegates (CONTEXT.md: Delegate).
func (h *Harness) AddDelegate(owner, delegate domain.Account, goalID int64) {
	h.T.Helper()
	if err := h.Service.AddDelegate(context.Background(), owner.ID, goalID, delegate.ID); err != nil {
		h.T.Fatalf("AddDelegate: %v", err)
	}
}
