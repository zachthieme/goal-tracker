package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Dimension is an admin-defined attribute whose values come from a list, used
// to filter and group Goals (CONTEXT.md: Dimension). Selection says whether a
// Goal takes one of its values or several, and List whether the list is Fixed
// or Extendable. Values holds the Dimension's values in display order, retired
// ones included. A Retired Dimension is no longer offered when setting a Goal's
// values, filtering or grouping the Goal list, or defining a Report, but the
// Goals carrying its values still show them (CONTEXT.md: Retired).
type Dimension struct {
	ID        int64
	Name      string
	Selection string
	List      string
	Retired   bool
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

// A Dimension's List: whether only an Admin adds values to it (Fixed) or anyone
// setting a Goal's value in it may (Extendable) (CONTEXT.md: Fixed,
// Extendable). A new Dimension is Fixed.
const (
	ListFixed      = "fixed"
	ListExtendable = "extendable"
)

// Extendable reports whether anyone setting a Goal's value in the Dimension may
// add a value to its list.
func (d Dimension) Extendable() bool {
	return d.List == ListExtendable
}

// DimensionValue is one value in a Dimension's fixed list. A retired value is no
// longer offered for new assignments but stays readable on the Goals that
// already carry it (CONTEXT.md: Dimension). DimensionRetired says the value's
// Dimension is Retired, and is filled in only where a value is read off a Goal.
type DimensionValue struct {
	ID               int64
	DimensionID      int64
	Value            string
	Retired          bool
	DimensionRetired bool
}

// CreateDimension defines a new Dimension with a fixed list of values. Only an
// Admin may define Dimensions (CONTEXT.md: Admin). The name and at least one
// value are required; blank values are dropped, and so is a value matching an
// earlier one whatever its case.
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
		v = strings.TrimSpace(v)
		if v != "" && !slices.ContainsFunc(cleaned, func(c string) bool { return strings.EqualFold(c, v) }) {
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

// AddDimensionValue adds a value to an existing Dimension's list. Only an Admin
// may (CONTEXT.md: Admins add values). The value is required. One matching an
// existing value whatever its case or surrounding spaces adds nothing and
// returns that value, and one matching a Retired value is refused.
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
	if match, ok, err := s.matchingValue(ctx, dimensionID, value); err != nil || ok {
		return match, err
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

// RestoreDimensionValue reverses a value's retirement, so it is offered for new
// assignments again (CONTEXT.md: Retired — an Admin can reverse it). Only an
// Admin may.
func (s *Service) RestoreDimensionValue(ctx context.Context, actorID, valueID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if _, err := s.queries.SetDimensionValueRetired(ctx, db.SetDimensionValueRetiredParams{
		Retired: 0,
		ID:      valueID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: dimension value does not exist", ErrValidation)
		}
		return fmt.Errorf("restore dimension value: %w", err)
	}
	return nil
}

// RetireDimension withdraws a whole Dimension: it is no longer offered when
// setting a Goal's values, nor in the Goal list's filter and grouping or the
// Report Definition form, yet the Goals carrying its values still show them and
// saved Report Definitions filtering on them keep working. Nothing is deleted
// (CONTEXT.md: Retired; ADR 0005). Only an Admin may.
func (s *Service) RetireDimension(ctx context.Context, actorID, dimensionID int64) error {
	return s.setDimensionRetired(ctx, actorID, dimensionID, true)
}

// RestoreDimension reverses a Dimension's retirement, returning it everywhere
// it was withdrawn from (CONTEXT.md: Retired — an Admin can reverse it). Only
// an Admin may.
func (s *Service) RestoreDimension(ctx context.Context, actorID, dimensionID int64) error {
	return s.setDimensionRetired(ctx, actorID, dimensionID, false)
}

func (s *Service) setDimensionRetired(ctx context.Context, actorID, dimensionID int64, retired bool) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	var flag int64
	if retired {
		flag = 1
	}
	if _, err := s.queries.SetDimensionRetired(ctx, db.SetDimensionRetiredParams{
		Retired: flag,
		ID:      dimensionID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: dimension does not exist", ErrValidation)
		}
		return fmt.Errorf("set dimension retired: %w", err)
	}
	return nil
}

// The directions MoveDimensionValue moves a value in its Dimension's list.
const (
	MoveUp   = "up"
	MoveDown = "down"
)

// MoveDimensionValue moves a value one place up or down its Dimension's list,
// the order its values are listed in everywhere (CONTEXT.md: Dimension). The
// first value moved up or the last moved down stays where it is. Only an Admin
// may.
func (s *Service) MoveDimensionValue(ctx context.Context, actorID, valueID int64, direction string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	step := map[string]int{MoveUp: -1, MoveDown: 1}[direction]
	if step == 0 {
		return fmt.Errorf("%w: a value moves up or down", ErrValidation)
	}
	val, err := s.queries.GetDimensionValue(ctx, valueID)
	if err != nil {
		return fmt.Errorf("%w: dimension value does not exist", ErrValidation)
	}
	rows, err := s.queries.ListDimensionValues(ctx, val.DimensionID)
	if err != nil {
		return fmt.Errorf("list dimension values: %w", err)
	}
	at := slices.IndexFunc(rows, func(r db.DimensionValue) bool { return r.ID == valueID })
	to := at + step
	if to < 0 || to >= len(rows) {
		return nil
	}
	rows[at], rows[to] = rows[to], rows[at]
	return s.setValueOrder(ctx, rows)
}

// SortDimensionValues puts a Dimension's values in alphabetical order, whatever
// their letter case. Only an Admin may.
func (s *Service) SortDimensionValues(ctx context.Context, actorID, dimensionID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if _, err := s.queries.GetDimension(ctx, dimensionID); err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	rows, err := s.queries.ListDimensionValues(ctx, dimensionID)
	if err != nil {
		return fmt.Errorf("list dimension values: %w", err)
	}
	slices.SortStableFunc(rows, func(a, b db.DimensionValue) int {
		if c := strings.Compare(strings.ToLower(a.Value), strings.ToLower(b.Value)); c != 0 {
			return c
		}
		return strings.Compare(a.Value, b.Value)
	})
	return s.setValueOrder(ctx, rows)
}

// setValueOrder stores rows' order as their Dimension's list order, all or
// nothing.
func (s *Service) setValueOrder(ctx context.Context, rows []db.DimensionValue) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		for i, r := range rows {
			if err := tx.queries.SetDimensionValuePosition(ctx, db.SetDimensionValuePositionParams{
				Position: int64(i),
				ID:       r.ID,
			}); err != nil {
				return fmt.Errorf("set dimension value position: %w", err)
			}
		}
		return nil
	})
}

// MergeDimensionValue merges one value into another in the same Dimension: every
// Goal carrying the merged value carries the target instead (once, if it had
// both), every Report Definition filter on it points at the target, and the
// merged value is gone from the list (CONTEXT.md: Extendable — merging values
// stays with Admins). It is all or nothing. Merging across Dimensions or into
// the value itself is refused. Only an Admin may.
func (s *Service) MergeDimensionValue(ctx context.Context, actorID, mergedID, targetID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	merged, err := s.queries.GetDimensionValue(ctx, mergedID)
	if err != nil {
		return fmt.Errorf("%w: dimension value does not exist", ErrValidation)
	}
	target, err := s.queries.GetDimensionValue(ctx, targetID)
	if err != nil {
		return fmt.Errorf("%w: the value to merge into does not exist", ErrValidation)
	}
	if merged.DimensionID != target.DimensionID {
		return fmt.Errorf("%w: %s and %s are in different Dimensions, so they can't be merged", ErrValidation, merged.Value, target.Value)
	}
	if merged.ID == target.ID {
		return fmt.Errorf("%w: a value can't be merged into itself", ErrValidation)
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		goals := db.RemoveMergedGoalValueWhereTargetCarriedParams{MergedID: mergedID, TargetID: targetID}
		if err := tx.queries.RemoveMergedGoalValueWhereTargetCarried(ctx, goals); err != nil {
			return fmt.Errorf("drop merged value from goals carrying the target: %w", err)
		}
		if err := tx.queries.MoveGoalValuesToTarget(ctx, db.MoveGoalValuesToTargetParams{TargetID: targetID, MergedID: mergedID}); err != nil {
			return fmt.Errorf("move goals to the target value: %w", err)
		}
		filters := db.RemoveMergedReportFilterWhereTargetFilteredParams{MergedID: mergedID, TargetID: targetID}
		if err := tx.queries.RemoveMergedReportFilterWhereTargetFiltered(ctx, filters); err != nil {
			return fmt.Errorf("drop merged report filters already on the target: %w", err)
		}
		if err := tx.queries.MoveReportFiltersToTarget(ctx, db.MoveReportFiltersToTargetParams{TargetID: targetID, MergedID: mergedID}); err != nil {
			return fmt.Errorf("move report filters to the target value: %w", err)
		}
		if err := tx.queries.DeleteDimensionValue(ctx, mergedID); err != nil {
			return fmt.Errorf("delete merged value: %w", err)
		}
		return nil
	})
}

// SeveralValuesError refuses switching a Dimension to one value while Goals
// still carry more than one of its values, naming those Goals so an Admin knows
// which to fix first. It is an ErrValidation.
type SeveralValuesError struct {
	Dimension string
	Goals     []Goal
}

func (e *SeveralValuesError) Error() string {
	titles := make([]string, 0, len(e.Goals))
	for _, g := range e.Goals {
		titles = append(titles, g.Title)
	}
	return fmt.Sprintf("%v: %s can't take one value while these Goals carry several: %s",
		ErrValidation, e.Dimension, strings.Join(titles, "; "))
}

func (e *SeveralValuesError) Unwrap() error { return ErrValidation }

// SetDimensionSelection chooses whether a Goal takes one of the Dimension's
// values or several (CONTEXT.md: Dimension). One to several is always allowed;
// several to one is refused with a *SeveralValuesError while any Goal carries
// more than one value in the Dimension. Only an Admin may.
func (s *Service) SetDimensionSelection(ctx context.Context, actorID, dimensionID int64, selection string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if selection != SelectionOne && selection != SelectionSeveral {
		return fmt.Errorf("%w: a Dimension takes one value or several", ErrValidation)
	}
	dim, err := s.queries.GetDimension(ctx, dimensionID)
	if err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	if selection == SelectionOne {
		rows, err := s.queries.ListGoalsWithSeveralValuesInDimension(ctx, dimensionID)
		if err != nil {
			return fmt.Errorf("list goals with several values: %w", err)
		}
		if len(rows) > 0 {
			refusal := &SeveralValuesError{Dimension: dim.Name}
			for _, r := range rows {
				refusal.Goals = append(refusal.Goals, goalFromRow(r.Goal, r.Account))
			}
			return refusal
		}
	}
	if _, err := s.queries.SetDimensionSelection(ctx, db.SetDimensionSelectionParams{
		Selection: selection,
		ID:        dimensionID,
	}); err != nil {
		return fmt.Errorf("set dimension selection: %w", err)
	}
	return nil
}

// SetDimensionList makes the Dimension's list Fixed or Extendable (CONTEXT.md:
// Fixed, Extendable). Either switch is always allowed: switching to Fixed only
// stops further additions, and the values already added stay. Only an Admin
// may.
func (s *Service) SetDimensionList(ctx context.Context, actorID, dimensionID int64, list string) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if list != ListFixed && list != ListExtendable {
		return fmt.Errorf("%w: a Dimension's list is Fixed or Extendable", ErrValidation)
	}
	if _, err := s.queries.GetDimension(ctx, dimensionID); err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	if _, err := s.queries.SetDimensionList(ctx, db.SetDimensionListParams{
		List: list,
		ID:   dimensionID,
	}); err != nil {
		return fmt.Errorf("set dimension list: %w", err)
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

// OfferedDimensions keeps the Dimensions still offered for setting, filtering
// and grouping Goals and for defining Reports, dropping the Retired ones
// (CONTEXT.md: Retired).
func OfferedDimensions(dims []Dimension) []Dimension {
	out := make([]Dimension, 0, len(dims))
	for _, d := range dims {
		if !d.Retired {
			out = append(out, d)
		}
	}
	return out
}

// AssignGoalValue gives a Goal a Dimension value (CONTEXT.md: Owners and their
// Delegates set a Goal's Dimension values). In a Dimension that takes one value it
// replaces any value the Goal already has there; in one that takes several it is
// added alongside them. A change is kept in the Goal's Value history. A retired
// value is not offered for a new assignment. Only the Goal's Owner, a Delegate
// or an Admin may assign its values.
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
	dimRow, err := s.queries.GetDimension(ctx, val.DimensionID)
	if err != nil {
		return fmt.Errorf("load dimension: %w", err)
	}
	dim := dimensionFromRow(dimRow)
	if dim.Retired {
		return retiredDimensionError(dim)
	}
	before, err := s.GoalValues(ctx, goalID)
	if err != nil {
		return err
	}
	if !dim.TakesSeveral() {
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
	return s.recordDimensionChanges(ctx, actorID, goalID, dim, before)
}

// AssignGoalValueByName gives a Goal the value named in a Dimension, adding it
// to the Dimension's list first when it isn't there, all in one step
// (CONTEXT.md: Extendable). A name matching an existing value whatever its case
// or surrounding spaces sets that value rather than adding one, and one
// matching a Retired value is refused. It is assigned as AssignGoalValue assigns, and the
// value the Goal now carries is returned. Only the Goal's Owner, a Delegate or
// an Admin may, and only an Admin may add to a Fixed list (CONTEXT.md: Fixed).
func (s *Service) AssignGoalValueByName(ctx context.Context, actorID, goalID, dimensionID int64, name string) (DimensionValue, error) {
	var val DimensionValue
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		val, err = tx.assignGoalValueByName(ctx, actorID, goalID, dimensionID, name)
		return err
	})
	if err != nil {
		return DimensionValue{}, err
	}
	return val, nil
}

// assignGoalValueByName is AssignGoalValueByName within a transaction the
// caller holds, so a value added to the list and its assignment stand or fall
// together.
func (s *Service) assignGoalValueByName(ctx context.Context, actorID, goalID, dimensionID int64, name string) (DimensionValue, error) {
	if err := s.requireGoalValueSetter(ctx, actorID, goalID); err != nil {
		return DimensionValue{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return DimensionValue{}, fmt.Errorf("%w: a value cannot be blank", ErrValidation)
	}
	dimRow, err := s.queries.GetDimension(ctx, dimensionID)
	if err != nil {
		return DimensionValue{}, fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	if dim := dimensionFromRow(dimRow); dim.Retired {
		return DimensionValue{}, retiredDimensionError(dim)
	}
	if match, ok, err := s.matchingValue(ctx, dimensionID, name); err != nil {
		return DimensionValue{}, err
	} else if ok {
		return match, s.AssignGoalValue(ctx, actorID, goalID, match.ID)
	}
	if dim := dimensionFromRow(dimRow); !dim.Extendable() {
		if err := s.requireAdmin(ctx, actorID); errors.Is(err, ErrNotAuthorized) {
			return DimensionValue{}, fmt.Errorf("%w: %s is a Fixed list, so only an Admin may add to it", ErrNotAuthorized, dim.Name)
		} else if err != nil {
			return DimensionValue{}, err
		}
	}
	row, err := s.queries.CreateDimensionValue(ctx, db.CreateDimensionValueParams{
		DimensionID: dimensionID,
		Value:       name,
		CreatedAt:   s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return DimensionValue{}, fmt.Errorf("add dimension value: %w", err)
	}
	val := dimensionValueFromRow(row)
	return val, s.AssignGoalValue(ctx, actorID, goalID, val.ID)
}

// SetGoalValues makes valueIDs exactly the values a Goal carries in one
// Dimension, so a set of checkboxes saves together: values left out are
// removed, and an empty set clears the Dimension (CONTEXT.md: Dimension). A
// change is kept in the Goal's Value history. A Dimension that takes one value
// accepts at most one. A retired value may be
// kept by a Goal that already carries it but not newly given (CONTEXT.md:
// Retired). Only the Goal's Owner, a Delegate or an Admin may set its values.
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
		if dim.Retired && !carries[id] {
			return retiredDimensionError(dim)
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
	return s.recordDimensionChanges(ctx, actorID, goalID, dim, carried)
}

// retiredDimensionError refuses newly giving a Goal a value in a Retired
// Dimension (CONTEXT.md: Retired).
func retiredDimensionError(dim Dimension) error {
	return fmt.Errorf("%w: %s is retired, so its values can't be newly assigned", ErrValidation, dim.Name)
}

// matchingValue finds the Dimension's value that name matches whatever its
// letter case or surrounding spaces ("acme " is "Acme"), so no list gains a
// near-duplicate. A match on a Retired value is refused, saying so (CONTEXT.md:
// Retired). ok is false when nothing matches.
func (s *Service) matchingValue(ctx context.Context, dimensionID int64, name string) (match DimensionValue, ok bool, err error) {
	rows, err := s.queries.ListDimensionValues(ctx, dimensionID)
	if err != nil {
		return DimensionValue{}, false, fmt.Errorf("list dimension values: %w", err)
	}
	name = strings.TrimSpace(name)
	for _, r := range rows {
		if !strings.EqualFold(strings.TrimSpace(r.Value), name) {
			continue
		}
		v := dimensionValueFromRow(r)
		if v.Retired {
			return DimensionValue{}, false, fmt.Errorf("%w: %s is retired, so it can't be added or newly assigned", ErrValidation, v.Value)
		}
		return v, true, nil
	}
	return DimensionValue{}, false, nil
}

// requireGoalValueSetter refuses anyone but the Goal's Owner, one of its
// Delegates, or an Admin (CONTEXT.md: Delegate).
func (s *Service) requireGoalValueSetter(ctx context.Context, actorID, goalID int64) error {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		return fmt.Errorf("%w: goal does not exist", ErrValidation)
	}
	if goal.Goal.OwnerID == actorID {
		return nil
	}
	if delegate, err := s.isDelegate(ctx, goalID, actorID); err != nil {
		return err
	} else if delegate {
		return nil
	}
	if err := s.requireAdmin(ctx, actorID); errors.Is(err, ErrNotAuthorized) {
		return fmt.Errorf("%w: only the Owner, a Delegate or an Admin may set a Goal's Dimension values", ErrNotAuthorized)
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
		out = append(out, goalValueFromRow(r.DimensionValue, r.Dimension))
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
		byGoal[r.GoalID] = append(byGoal[r.GoalID], goalValueFromRow(r.DimensionValue, r.Dimension))
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
// value in dim. A Goal with several values in dim appears in each of their
// buckets (ADR 0005). Buckets with no Goals are omitted, so retired-but-unused
// values don't clutter the list.
func GroupGoalsByDimension(goals []GoalWithValues, dim Dimension) []GoalGroup {
	byValue := make(map[int64][]GoalWithValues)
	var unassigned []GoalWithValues
	for _, g := range goals {
		assigned := false
		for _, v := range g.Values {
			if v.DimensionID == dim.ID {
				byValue[v.ID] = append(byValue[v.ID], g)
				assigned = true
			}
		}
		if !assigned {
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

func dimensionFromRow(d db.Dimension) Dimension {
	return Dimension{ID: d.ID, Name: d.Name, Selection: d.Selection, List: d.List, Retired: d.Retired != 0}
}

// goalValueFromRow is a value as read off a Goal, saying whether its Dimension
// is Retired.
func goalValueFromRow(v db.DimensionValue, d db.Dimension) DimensionValue {
	val := dimensionValueFromRow(v)
	val.DimensionRetired = d.Retired != 0
	return val
}

func dimensionValueFromRow(v db.DimensionValue) DimensionValue {
	return DimensionValue{
		ID:          v.ID,
		DimensionID: v.DimensionID,
		Value:       v.Value,
		Retired:     v.Retired != 0,
	}
}
