package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ValueEdit is one cell of an edit to several Goals' values at once, from the
// Goal table (#80): the values a Goal is to carry in one Dimension, or its value
// in one Field.
type ValueEdit struct {
	GoalID int64
	// DimensionID names a Dimension cell, and ValueIDs are exactly the values
	// the Goal is to carry in it, as SetGoalValues takes them. NewValue is a
	// value typed in, set as AssignGoalValueByName sets it (CONTEXT.md:
	// Extendable): in a Dimension that takes one value it replaces ValueIDs,
	// and in one that takes several it joins them.
	DimensionID int64
	ValueIDs    []int64
	NewValue    string
	// FieldID names a Field cell, and Value is the Goal's value in it, blank
	// to clear it, as SetGoalField takes it.
	FieldID int64
	Value   string
}

// CellError is a cell of a table edit that was refused: the Goal and the
// Dimension or Field it is in, and why, as the Goal page would say it.
type CellError struct {
	GoalID      int64
	DimensionID int64
	FieldID     int64
	Message     string
}

// ValueEditError refuses a table edit with at least one bad cell, naming every
// one, in the order the edits came. Nothing in the edit was saved. It is an
// ErrValidation.
type ValueEditError struct {
	Cells []CellError
}

func (e *ValueEditError) Error() string {
	messages := make([]string, 0, len(e.Cells))
	for _, c := range e.Cells {
		messages = append(messages, c.Message)
	}
	return fmt.Sprintf("%v: %s", ErrValidation, strings.Join(messages, "; "))
}

func (e *ValueEditError) Unwrap() error { return ErrValidation }

// EditGoalValues applies every edit by the rules setting each value on its
// Goal's page follows, keeping each change in its Goal's Value history. It is
// all or nothing: when any cell is refused, nothing is saved and a
// *ValueEditError names every refused cell. An edit touching a Goal whose
// values the author may not set — they are not its Owner, a Delegate or an
// Admin — is refused whole, naming that Goal (CONTEXT.md: Delegate).
func (s *Service) EditGoalValues(ctx context.Context, actorID int64, edits []ValueEdit) error {
	checked := map[int64]bool{}
	for _, e := range edits {
		if checked[e.GoalID] {
			continue
		}
		checked[e.GoalID] = true
		if err := s.requireGoalValueSetter(ctx, actorID, e.GoalID); errors.Is(err, ErrNotAuthorized) {
			goal, err := s.queries.GetGoal(ctx, e.GoalID)
			if err != nil {
				return fmt.Errorf("load goal: %w", err)
			}
			return fmt.Errorf("%w: only its Owner, a Delegate or an Admin may set the values of %s", ErrNotAuthorized, goal.Goal.Title)
		} else if err != nil {
			return err
		}
	}
	refusal := &ValueEditError{}
	err := s.WithinTx(ctx, func(tx *Service) error {
		for _, e := range edits {
			// Every Goal's setter was checked above, so a refusal here is
			// the cell's own, e.g. adding to a Fixed list (CONTEXT.md: Fixed).
			err := tx.applyValueEdit(ctx, actorID, e)
			switch {
			case errors.Is(err, ErrValidation), errors.Is(err, ErrNotAuthorized):
				message := strings.TrimPrefix(err.Error(), ErrValidation.Error()+": ")
				refusal.Cells = append(refusal.Cells, CellError{
					GoalID:      e.GoalID,
					DimensionID: e.DimensionID,
					FieldID:     e.FieldID,
					Message:     strings.TrimPrefix(message, ErrNotAuthorized.Error()+": "),
				})
			case err != nil:
				return err
			}
		}
		if len(refusal.Cells) > 0 {
			return refusal
		}
		return nil
	})
	return err
}

// applyValueEdit sets one cell's value on its Goal.
func (s *Service) applyValueEdit(ctx context.Context, actorID int64, e ValueEdit) error {
	if e.FieldID != 0 {
		return s.SetGoalField(ctx, actorID, e.GoalID, e.FieldID, e.Value)
	}
	if strings.TrimSpace(e.NewValue) == "" {
		return s.SetGoalValues(ctx, actorID, e.GoalID, e.DimensionID, e.ValueIDs)
	}
	dim, err := s.queries.GetDimension(ctx, e.DimensionID)
	if err != nil {
		return fmt.Errorf("%w: dimension does not exist", ErrValidation)
	}
	if dimensionFromRow(dim).TakesSeveral() {
		if err := s.SetGoalValues(ctx, actorID, e.GoalID, e.DimensionID, e.ValueIDs); err != nil {
			return err
		}
	}
	_, err = s.assignGoalValueByName(ctx, actorID, e.GoalID, e.DimensionID, e.NewValue)
	return err
}
