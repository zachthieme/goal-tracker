package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// CreateDimension defines a Dimension with the given values on behalf of admin,
// failing the test on error. It is a scenario builder for arranging Dimensions a
// test needs.
func (h *Harness) CreateDimension(admin domain.Account, name string, values ...string) domain.Dimension {
	h.T.Helper()
	dim, err := h.Service.CreateDimension(context.Background(), admin.ID, name, values)
	if err != nil {
		h.T.Fatalf("CreateDimension: %v", err)
	}
	return dim
}

// AssignGoalValue assigns a Dimension value to a Goal, failing the test on error.
func (h *Harness) AssignGoalValue(goal domain.Goal, value domain.DimensionValue) {
	h.T.Helper()
	if err := h.Service.AssignGoalValue(context.Background(), goal.ID, value.ID); err != nil {
		h.T.Fatalf("AssignGoalValue: %v", err)
	}
}
