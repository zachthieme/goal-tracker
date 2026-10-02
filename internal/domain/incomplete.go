package domain

import (
	"context"
	"fmt"
)

// RequiredValue is a Dimension or Field an Admin has marked required, by name,
// and whether a Goal has a value in it (CONTEXT.md: Incomplete).
type RequiredValue struct {
	Name string
	Set  bool
}

// RequiredValues lists the Dimensions and Fields a Goal needs a value in,
// Dimensions first, each by name, saying whether it has one. A Retired
// Dimension or Field is never required, and a Retired value the Goal still
// carries counts as a value (CONTEXT.md: Incomplete, Retired). It holds
// whatever the Goal's Lifecycle: the activation checklist reads it on a
// Proposed Goal.
func (s *Service) RequiredValues(ctx context.Context, goalID int64) ([]RequiredValue, error) {
	dims, err := s.queries.ListDimensions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dimensions: %w", err)
	}
	values, err := s.GoalValues(ctx, goalID)
	if err != nil {
		return nil, err
	}
	carried := make(map[int64]bool, len(values))
	for _, v := range values {
		carried[v.DimensionID] = true
	}
	var out []RequiredValue
	for _, row := range dims {
		if d := dimensionFromRow(row); d.Required && !d.Retired {
			out = append(out, RequiredValue{Name: d.Name, Set: carried[d.ID]})
		}
	}

	fields, err := s.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	fieldValues, err := s.GoalFields(ctx, goalID)
	if err != nil {
		return nil, err
	}
	set := make(map[int64]bool, len(fieldValues))
	for _, v := range fieldValues {
		set[v.Field.ID] = true
	}
	for _, f := range fields {
		if f.Required && !f.Retired {
			out = append(out, RequiredValue{Name: f.Name, Set: set[f.ID]})
		}
	}
	return out, nil
}

// MissingRequired names the required Dimensions and Fields a Goal has no value
// in, in RequiredValues' order, whatever its Lifecycle. The activation gate
// refuses a Proposed Goal that lacks any.
func (s *Service) MissingRequired(ctx context.Context, goalID int64) ([]string, error) {
	required, err := s.RequiredValues(ctx, goalID)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, r := range required {
		if !r.Set {
			missing = append(missing, r.Name)
		}
	}
	return missing, nil
}

// Incomplete names what an Active Goal lacks in the Dimensions and Fields an
// Admin has marked required, as MissingRequired does, and is empty when it
// lacks nothing. Only an Active Goal is ever Incomplete: a Proposed one is held
// at the activation gate instead, and an On Hold, Done or Cancelled one is no
// longer tracked (CONTEXT.md: Incomplete).
func (s *Service) Incomplete(ctx context.Context, g Goal) ([]string, error) {
	if g.Lifecycle != LifecycleActive {
		return nil, nil
	}
	return s.MissingRequired(ctx, g.ID)
}

// boolFlag is a bool as the 0 or 1 the schema stores it as.
func boolFlag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
