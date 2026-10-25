package testsupport

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// SuggestParent has actor, anyone but the Goal's Owner, suggest that goal
// contribute to parent, failing the test on error. It is a scenario builder
// for arranging an open Parent suggestion (CONTEXT.md: Parent suggestion).
func (h *Harness) SuggestParent(actor domain.Account, goal, parent domain.Goal, note string) domain.ParentSuggestion {
	h.T.Helper()
	p, err := h.Service.SuggestParent(h.T.Context(), actor.ID, goal.ID, parent.ID, note)
	if err != nil {
		h.T.Fatalf("SuggestParent: %v", err)
	}
	return p
}
