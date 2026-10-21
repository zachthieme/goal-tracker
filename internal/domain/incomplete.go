package domain

import (
	"context"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// RequiredValue is a Dimension or Field an Admin has marked required, by name,
// and whether a Goal has a value in it (CONTEXT.md: Incomplete). Exactly one of
// DimensionID and FieldID is set, saying which it is.
type RequiredValue struct {
	Name        string
	Set         bool
	DimensionID int64
	FieldID     int64
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
	return requiredValues(dims, fields, carried, set), nil
}

// requiredValues is RequiredValues for a Goal that has a value in the
// Dimensions carried holds and the Fields set holds, by ID, out of dims and
// fields, every Dimension and Field there is.
func requiredValues(dims []db.Dimension, fields []Field, carried, set map[int64]bool) []RequiredValue {
	var out []RequiredValue
	for _, row := range dims {
		if d := dimensionFromRow(row); d.Required && !d.Retired {
			out = append(out, RequiredValue{Name: d.Name, Set: carried[d.ID], DimensionID: d.ID})
		}
	}
	for _, f := range fields {
		if f.Required && !f.Retired {
			out = append(out, RequiredValue{Name: f.Name, Set: set[f.ID], FieldID: f.ID})
		}
	}
	return out
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
	byGoal, err := s.IncompleteByGoal(ctx, []Goal{g})
	if err != nil {
		return nil, err
	}
	return byGoal[g.ID], nil
}

// IncompleteByGoal names what each of goals lacks, as Incomplete does for one,
// keyed by Goal ID; a Goal that isn't Incomplete has no entry. It runs the same
// few queries however many Goals there are, so a page listing Goals can afford
// it.
func (s *Service) IncompleteByGoal(ctx context.Context, goals []Goal) (map[int64][]string, error) {
	out := map[int64][]string{}
	var active []int64
	for _, g := range goals {
		if g.Lifecycle == LifecycleActive {
			active = append(active, g.ID)
		}
	}
	if len(active) == 0 {
		return out, nil
	}
	dims, err := s.queries.ListDimensions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dimensions: %w", err)
	}
	fields, err := s.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	carriedRows, err := s.queries.ListDimensionsCarriedByGoals(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	carried := map[int64]map[int64]bool{}
	for _, r := range carriedRows {
		if carried[r.GoalID] == nil {
			carried[r.GoalID] = map[int64]bool{}
		}
		carried[r.GoalID][r.DimensionID] = true
	}
	setRows, err := s.queries.ListFieldsSetOnGoals(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("list goal field values: %w", err)
	}
	set := map[int64]map[int64]bool{}
	for _, r := range setRows {
		if set[r.GoalID] == nil {
			set[r.GoalID] = map[int64]bool{}
		}
		set[r.GoalID][r.FieldID] = true
	}
	for _, id := range active {
		for _, r := range requiredValues(dims, fields, carried[id], set[id]) {
			if !r.Set {
				out[id] = append(out[id], r.Name)
			}
		}
	}
	return out, nil
}

// boolFlag is a bool as the 0 or 1 the schema stores it as.
func boolFlag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
