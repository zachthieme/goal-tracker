package testsupport

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// StartHandoff has actor, the Goal's Owner or an Admin, offer goalID to to,
// failing the test on error. It is a scenario builder for arranging a pending
// Handoff (CONTEXT.md: Handoff).
func (h *Harness) StartHandoff(actor domain.Account, goalID int64, to domain.Account) domain.Handoff {
	h.T.Helper()
	ho, err := h.Service.StartHandoff(h.T.Context(), domain.StartHandoffInput{
		GoalID:    goalID,
		ToOwnerID: to.ID,
		ActorID:   actor.ID,
	})
	if err != nil {
		h.T.Fatalf("StartHandoff: %v", err)
	}
	return ho
}
