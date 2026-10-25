package testsupport

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Nudge has sender ask goalID's Owner and Delegates to check in on it,
// failing the test on error. It is a scenario builder for arranging a Goal's
// Nudges (CONTEXT.md: Nudge): the Goal must be one that can be nudged now, and
// sender someone who can't check in on it.
func (h *Harness) Nudge(sender domain.Account, goalID int64) domain.Nudge {
	h.T.Helper()
	n, err := h.Service.Nudge(h.T.Context(), sender.ID, goalID)
	if err != nil {
		h.T.Fatalf("Nudge: %v", err)
	}
	return n
}
