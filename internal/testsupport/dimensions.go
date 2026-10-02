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

// AssignGoalValue assigns a Dimension value to a Goal on behalf of its Owner,
// failing the test on error.
func (h *Harness) AssignGoalValue(goal domain.Goal, value domain.DimensionValue) {
	h.T.Helper()
	if err := h.Service.AssignGoalValue(context.Background(), goal.Owner.ID, goal.ID, value.ID); err != nil {
		h.T.Fatalf("AssignGoalValue: %v", err)
	}
}

// CreateSeveralValuesDimension defines a Dimension in which a Goal takes
// several values, failing the test on error.
func (h *Harness) CreateSeveralValuesDimension(admin domain.Account, name string, values ...string) domain.Dimension {
	h.T.Helper()
	dim := h.CreateDimension(admin, name, values...)
	if err := h.Service.SetDimensionSelection(context.Background(), admin.ID, dim.ID, domain.SelectionSeveral); err != nil {
		h.T.Fatalf("SetDimensionSelection: %v", err)
	}
	dim.Selection = domain.SelectionSeveral
	return dim
}
