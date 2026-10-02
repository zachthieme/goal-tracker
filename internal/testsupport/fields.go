package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// CreateField defines a Field of the given type on behalf of admin, failing the
// test on error. unit only sticks to a number Field.
func (h *Harness) CreateField(admin domain.Account, name, fieldType, unit string) domain.Field {
	h.T.Helper()
	f, err := h.Service.CreateField(context.Background(), admin.ID, name, fieldType, unit)
	if err != nil {
		h.T.Fatalf("CreateField: %v", err)
	}
	return f
}

// SetGoalField sets a Goal's value in a Field on behalf of actor, failing the
// test on error.
func (h *Harness) SetGoalField(actor domain.Account, goal domain.Goal, field domain.Field, value string) {
	h.T.Helper()
	if err := h.Service.SetGoalField(context.Background(), actor.ID, goal.ID, field.ID, value); err != nil {
		h.T.Fatalf("SetGoalField: %v", err)
	}
}
