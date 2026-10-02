package domain

import (
	"context"
	"fmt"
)

// MissingRequired names the Dimensions and Fields an Admin has marked required
// that the Goal has no value in, Dimensions first, each by name. A Retired
// Dimension or Field is never required, and a Retired value the Goal still
// carries counts as a value (CONTEXT.md: Incomplete, Retired). It holds
// whatever the Goal's Lifecycle: the activation gate reads it on a Proposed
// Goal, and Incomplete on an Active one.
func (s *Service) MissingRequired(ctx context.Context, goalID int64) ([]string, error) {
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
	var missing []string
	for _, row := range dims {
		if d := dimensionFromRow(row); d.Required && !d.Retired && !carried[d.ID] {
			missing = append(missing, d.Name)
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
		if f.Required && !f.Retired && !set[f.ID] {
			missing = append(missing, f.Name)
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
