package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Dimension is an admin-defined attribute with a fixed list of values, used to
// filter and group Goals (CONTEXT.md: Dimension). Selection says whether a Goal
// takes one of its values or several. Values holds the Dimension's values in
// display order, retired ones included.
type Dimension struct {
	ID        int64
	Name      string
	Selection string
	Values    []DimensionValue
}

// A Dimension's Selection: whether a Goal takes one of its values or several
// (CONTEXT.md: Dimension). A new Dimension takes one.
const (
	SelectionOne     = "one"
	SelectionSeveral = "several"
)

// TakesSeveral reports whether a Goal may carry several of the Dimension's
// values at once.
func (d Dimension) TakesSeveral() bool {
	return d.Selection == SelectionSeveral
}

// DimensionValue is one value in a Dimension's fixed list. A retired value is no
// longer offered for new assignments but stays readable on the Goals that
// already carry it (CONTEXT.md: Dimension).
type DimensionValue struct {
	ID          int64
	DimensionID int64
	Value       string
	Retired     bool
}

// CreateDimension defines a new Dimension with a fixed list of values. Only an
// Admin may define Dimensions (CONTEXT.md: Admin). The name and at least one
// value are required; blank values are dropped.
func (s *Service) CreateDimension(ctx context.Context, actorID int64, name string, values []string) (Dimension, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Dimension{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Dimension{}, fmt.Errorf("%w: a Dimension needs a name", ErrValidation)
	}
	cleaned := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			cleaned = append(cleaned, v)
		}
	}
	if len(cleaned) == 0 {
		return Dimension{}, fmt.Errorf("%w: a Dimension needs at least one value", ErrValidation)
	}

	now := s.clock.Now().Format(timeFormat)
	row, err := s.queries.CreateDimension(ctx, db.CreateDimensionParams{
		Name:      name,
		CreatedAt: now,
	})
	if err != nil {
		return Dimension{}, fmt.Errorf("create dimension: %w", err)
	}
	dim := dimensionFromRow(row)
	for _, v := range cleaned {
		val, err := s.queries.CreateDimensionValue(ctx, db.CreateDimensionValueParams{
			DimensionID: row.ID,
			Value:       v,
			CreatedAt:   now,
		})
		if err != nil {
			return Dimension{}, fmt.Errorf("create dimension value: %w", err)
		}
		dim.Values = append(dim.Values, dimensionValueFromRow(val))
	}
	return dim, nil
}

// AddDimensionValue adds a value to an existing Dimension's fixed list. Only an
// Admin may (CONTEXT.md: Admins add values). The value is required.
func (s *Service) AddDimensionValue(ctx context.Context, actorID, dimensionID int64, value string) (DimensionValue, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return DimensionValue{}, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return DimensionValue{}, fmt.Errorf("%w: a value cannot be blank", ErrValidation)
	}
	if _, err := s.queries.GetDimension(ctx, dimensionID); err != nil {
		return DimensionValue{}, fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	row, err := s.queries.CreateDimensionValue(ctx, db.CreateDimensionValueParams{
		DimensionID: dimensionID,
		Value:       value,
		CreatedAt:   s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return DimensionValue{}, fmt.Errorf("add dimension value: %w", err)
	}
	return dimensionValueFromRow(row), nil
}

// RenameDimensionValue renames a value, keeping its identity so every Goal
// assigned it follows the rename (CONTEXT.md: Admins rename values). Only an
// Admin may. The new name is required.
func (s *Service) RenameDimensionValue(ctx context.Context, actorID, valueID int64, newValue string) (DimensionValue, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return DimensionValue{}, err
	}
	newValue = strings.TrimSpace(newValue)
	if newValue == "" {
		return DimensionValue{}, fmt.Errorf("%w: a value cannot be blank", ErrValidation)
	}
	row, err := s.queries.SetDimensionValueName(ctx, db.SetDimensionValueNameParams{
		Value: newValue,
		ID:    valueID,
	})
	if err != nil {
		return DimensionValue{}, fmt.Errorf("rename dimension value: %w", err)
	}
	return dimensionValueFromRow(row), nil
}

// RetireDimensionValue retires a value so it is no longer offered for new
// assignments, yet stays readable on the Goals that already carry it (CONTEXT.md:
// Admins retire values; retired values stay readable). Only an Admin may.
func (s *Service) RetireDimensionValue(ctx context.Context, actorID, valueID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if _, err := s.queries.SetDimensionValueRetired(ctx, db.SetDimensionValueRetiredParams{
		Retired: 1,
		ID:      valueID,
	}); err != nil {
		return fmt.Errorf("retire dimension value: %w", err)
	}
	return nil
}

// SetDimensionSelection chooses whether a Goal takes one of the Dimension's
// values or several (CONTEXT.md: Dimension). Only an Admin may.
func (s *Service) SetDimensionSelection(ctx context.Context, actorID, dimensionID int64, selection string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if selection != SelectionOne && selection != SelectionSeveral {
		return fmt.Errorf("%w: a Dimension takes one value or several", ErrValidation)
	}
	if _, err := s.queries.GetDimension(ctx, dimensionID); err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	if _, err := s.queries.SetDimensionSelection(ctx, db.SetDimensionSelectionParams{
		Selection: selection,
		ID:        dimensionID,
	}); err != nil {
		return fmt.Errorf("set dimension selection: %w", err)
	}
	return nil
}

// ListDimensions returns every Dimension with its values in display order,
// retired values included.
func (s *Service) ListDimensions(ctx context.Context) ([]Dimension, error) {
	dimRows, err := s.queries.ListDimensions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dimensions: %w", err)
	}
	valRows, err := s.queries.ListAllDimensionValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list dimension values: %w", err)
	}
	byDim := make(map[int64][]DimensionValue, len(dimRows))
	for _, v := range valRows {
		byDim[v.DimensionID] = append(byDim[v.DimensionID], dimensionValueFromRow(v))
	}
	out := make([]Dimension, 0, len(dimRows))
	for _, d := range dimRows {
		dim := dimensionFromRow(d)
		dim.Values = byDim[d.ID]
		out = append(out, dim)
	}
	return out, nil
}

// AssignGoalValue gives a Goal a Dimension value (CONTEXT.md: Owners assign
// Dimension values to their Goals). In a Dimension that takes one value it
// replaces any value the Goal already has there; in one that takes several it is
// added alongside them. A retired value is not offered for a new assignment.
// Only the Goal's Owner or an Admin may assign its values.
func (s *Service) AssignGoalValue(ctx context.Context, actorID, goalID, valueID int64) error {
	if err := s.requireGoalValueSetter(ctx, actorID, goalID); err != nil {
		return err
	}
	val, err := s.queries.GetDimensionValue(ctx, valueID)
	if err != nil {
		return fmt.Errorf("%w: dimension value does not exist", ErrValidation)
	}
	if val.Retired != 0 {
		return fmt.Errorf("%w: that value is retired and cannot be newly assigned", ErrValidation)
	}
	dim, err := s.queries.GetDimension(ctx, val.DimensionID)
	if err != nil {
		return fmt.Errorf("load dimension: %w", err)
	}
	if !dimensionFromRow(dim).TakesSeveral() {
		if err := s.queries.ClearGoalValuesInDimension(ctx, db.ClearGoalValuesInDimensionParams{
			GoalID:      goalID,
			DimensionID: val.DimensionID,
		}); err != nil {
			return fmt.Errorf("clear existing value: %w", err)
		}
	}
	if err := s.queries.AssignGoalValueIfAbsent(ctx, db.AssignGoalValueIfAbsentParams{
		GoalID:           goalID,
		DimensionValueID: valueID,
		CreatedAt:        s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("assign dimension value: %w", err)
	}
	return nil
}

// SetGoalValues makes valueIDs exactly the values a Goal carries in one
// Dimension, so a set of checkboxes saves together: values left out are
// removed, and an empty set clears the Dimension (CONTEXT.md: Dimension). A
// Dimension that takes one value accepts at most one. A retired value may be
// kept by a Goal that already carries it but not newly given (CONTEXT.md:
// Retired). Only the Goal's Owner or an Admin may set its values.
func (s *Service) SetGoalValues(ctx context.Context, actorID, goalID, dimensionID int64, valueIDs []int64) error {
	if err := s.requireGoalValueSetter(ctx, actorID, goalID); err != nil {
		return err
	}
	dimRow, err := s.queries.GetDimension(ctx, dimensionID)
	if err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	dim := dimensionFromRow(dimRow)
	valueIDs = dedupeIDs(valueIDs)
	if len(valueIDs) > 1 && !dim.TakesSeveral() {
		return fmt.Errorf("%w: %s takes one value per Goal", ErrValidation, dim.Name)
	}
	carried, err := s.GoalValues(ctx, goalID)
	if err != nil {
		return err
	}
	carries := make(map[int64]bool, len(carried))
	for _, v := range carried {
		carries[v.ID] = true
	}
	keep := make(map[int64]bool, len(valueIDs))
	for _, id := range valueIDs {
		val, err := s.queries.GetDimensionValue(ctx, id)
		if err != nil || val.DimensionID != dimensionID {
			return fmt.Errorf("%w: that value is not in %s", ErrValidation, dim.Name)
		}
		if val.Retired != 0 && !carries[id] {
			return fmt.Errorf("%w: %s is retired and cannot be newly assigned", ErrValidation, val.Value)
		}
		keep[id] = true
	}

	for _, v := range carried {
		if v.DimensionID != dimensionID || keep[v.ID] {
			continue
		}
		if err := s.queries.RemoveGoalValue(ctx, db.RemoveGoalValueParams{
			GoalID:           goalID,
			DimensionValueID: v.ID,
		}); err != nil {
			return fmt.Errorf("remove dimension value: %w", err)
		}
	}
	now := s.clock.Now().Format(timeFormat)
	for _, id := range valueIDs {
		if err := s.queries.AssignGoalValueIfAbsent(ctx, db.AssignGoalValueIfAbsentParams{
			GoalID:           goalID,
			DimensionValueID: id,
			CreatedAt:        now,
		}); err != nil {
			return fmt.Errorf("assign dimension value: %w", err)
		}
	}
	return nil
}

// requireGoalValueSetter refuses anyone but the Goal's Owner or an Admin.
func (s *Service) requireGoalValueSetter(ctx context.Context, actorID, goalID int64) error {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		return fmt.Errorf("%w: goal does not exist", ErrValidation)
	}
	if goal.Goal.OwnerID == actorID {
		return nil
	}
	if err := s.requireAdmin(ctx, actorID); errors.Is(err, ErrNotAuthorized) {
		return fmt.Errorf("%w: only the Owner or an Admin may set a Goal's Dimension values", ErrNotAuthorized)
	} else if err != nil {
		return err
	}
	return nil
}

// GoalValues returns the Dimension values assigned to a Goal, retired ones
// included so they stay readable (CONTEXT.md: retired values stay readable).
func (s *Service) GoalValues(ctx context.Context, goalID int64) ([]DimensionValue, error) {
	rows, err := s.queries.ListGoalValues(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	out := make([]DimensionValue, 0, len(rows))
	for _, r := range rows {
		out = append(out, dimensionValueFromRow(r.DimensionValue))
	}
	return out, nil
}

// GoalWithValues is a Goal paired with the Dimension values assigned to it, the
// unit the Goal list filters and groups over.
type GoalWithValues struct {
	Goal   Goal
	Values []DimensionValue
}

// GoalGroup is one bucket of the Goal list grouped by a Dimension: the Goals
// sharing Value, or the Goals with no value in that Dimension when Value is nil.
type GoalGroup struct {
	Value *DimensionValue
	Goals []GoalWithValues
}

// ListGoalsWithValues returns every Goal, newest first, each with its assigned
// Dimension values resolved, for the Goal list's filtering and grouping.
func (s *Service) ListGoalsWithValues(ctx context.Context) ([]GoalWithValues, error) {
	goalRows, err := s.queries.ListGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	valRows, err := s.queries.ListAllGoalValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	byGoal := make(map[int64][]DimensionValue, len(goalRows))
	for _, r := range valRows {
		byGoal[r.GoalID] = append(byGoal[r.GoalID], dimensionValueFromRow(r.DimensionValue))
	}
	out := make([]GoalWithValues, 0, len(goalRows))
	for _, r := range goalRows {
		g := goalFromRow(r.Goal, r.Account)
		out = append(out, GoalWithValues{Goal: g, Values: byGoal[g.ID]})
	}
	return out, nil
}

// FilterGoals keeps the Goals matching selected, a faceted filter of
// dimensionID → chosen value IDs. Within a Dimension the chosen values are OR'd
// (a Goal passes if it has any of them); across Dimensions they are AND'd (a Goal
// must pass every Dimension that has a selection). An empty selection keeps
// everything.
func FilterGoals(goals []GoalWithValues, selected map[int64][]int64) []GoalWithValues {
	out := make([]GoalWithValues, 0, len(goals))
	for _, g := range goals {
		if goalMatchesFilter(g, selected) {
			out = append(out, g)
		}
	}
	return out
}

func goalMatchesFilter(g GoalWithValues, selected map[int64][]int64) bool {
	for dimID, wantIDs := range selected {
		if len(wantIDs) == 0 {
			continue
		}
		matched := false
		for _, v := range g.Values {
			if v.DimensionID != dimID {
				continue
			}
			for _, want := range wantIDs {
				if v.ID == want {
					matched = true
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// GroupGoalsByDimension buckets goals under dim's values, in the Dimension's
// value order, followed by an unassigned bucket (Value nil) for Goals with no
// value in dim. Buckets with no Goals are omitted, so retired-but-unused values
// don't clutter the list.
func GroupGoalsByDimension(goals []GoalWithValues, dim Dimension) []GoalGroup {
	byValue := make(map[int64][]GoalWithValues)
	var unassigned []GoalWithValues
	for _, g := range goals {
		if v, ok := valueInDimension(g, dim.ID); ok {
			byValue[v.ID] = append(byValue[v.ID], g)
		} else {
			unassigned = append(unassigned, g)
		}
	}
	groups := make([]GoalGroup, 0, len(dim.Values)+1)
	for i := range dim.Values {
		v := dim.Values[i]
		if bucket := byValue[v.ID]; len(bucket) > 0 {
			groups = append(groups, GoalGroup{Value: &v, Goals: bucket})
		}
	}
	if len(unassigned) > 0 {
		groups = append(groups, GoalGroup{Value: nil, Goals: unassigned})
	}
	return groups
}

func valueInDimension(g GoalWithValues, dimensionID int64) (DimensionValue, bool) {
	for _, v := range g.Values {
		if v.DimensionID == dimensionID {
			return v, true
		}
	}
	return DimensionValue{}, false
}

func dimensionFromRow(d db.Dimension) Dimension {
	return Dimension{ID: d.ID, Name: d.Name, Selection: d.Selection}
}

func dimensionValueFromRow(v db.DimensionValue) DimensionValue {
	return DimensionValue{
		ID:          v.ID,
		DimensionID: v.DimensionID,
		Value:       v.Value,
		Retired:     v.Retired != 0,
	}
}
