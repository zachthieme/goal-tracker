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

// CreateExtendableDimension defines a Dimension whose list anyone setting a
// Goal's value in it may add to, failing the test on error.
func (h *Harness) CreateExtendableDimension(admin domain.Account, name string, values ...string) domain.Dimension {
	h.T.Helper()
	dim := h.CreateDimension(admin, name, values...)
	if err := h.Service.SetDimensionList(context.Background(), admin.ID, dim.ID, domain.ListExtendable); err != nil {
		h.T.Fatalf("SetDimensionList: %v", err)
	}
	dim.List = domain.ListExtendable
	return dim
}

// SetDimensionSelection switches whether a Goal takes one of dim's values or
// several, on behalf of admin, failing the test on error.
func (h *Harness) SetDimensionSelection(admin domain.Account, dim domain.Dimension, selection string) {
	h.T.Helper()
	if err := h.Service.SetDimensionSelection(context.Background(), admin.ID, dim.ID, selection); err != nil {
		h.T.Fatalf("SetDimensionSelection: %v", err)
	}
}

// SetDimensionRequired marks dim required, or unmarks it, on behalf of admin,
// failing the test on error.
func (h *Harness) SetDimensionRequired(admin domain.Account, dim domain.Dimension, required bool) {
	h.T.Helper()
	if err := h.Service.SetDimensionRequired(context.Background(), admin.ID, dim.ID, required); err != nil {
		h.T.Fatalf("SetDimensionRequired: %v", err)
	}
}
