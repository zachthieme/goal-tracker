package domain

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// ValueChange is one change to a Goal's Dimension values or Fields, kept in its
// Value history (CONTEXT.md: Field — changes are kept in the Goal's history).
// Attribute names the Dimension or Field, and Before and After are the value
// before and after, empty for none, all as they were at the time, so a later
// rename or merge doesn't rewrite it. Several is true for a value added (Before
// empty) or removed (After empty) in a Dimension that took several values.
type ValueChange struct {
	ID        int64
	Actor     Account
	Attribute string
	Several   bool
	Before    string
	After     string
	CreatedAt time.Time
}

// ValueHistory returns every change to a Goal's Dimension values and Fields,
// oldest first. Changes made before the history was kept aren't in it.
func (s *Service) ValueHistory(ctx context.Context, goalID int64) ([]ValueChange, error) {
	rows, err := s.queries.ListValueChangesForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list value history: %w", err)
	}
	out := make([]ValueChange, 0, len(rows))
	for _, r := range rows {
		c := r.GoalValueChange
		createdAt, _ := time.Parse(timeFormat, c.CreatedAt)
		out = append(out, ValueChange{
			ID:        c.ID,
			Actor:     accountFromRow(r.Account),
			Attribute: c.Attribute,
			Several:   c.Several != 0,
			Before:    c.BeforeValue,
			After:     c.AfterValue,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// recordFieldChange keeps a change to a Goal's value in a Field in its Value
// history.
func (s *Service) recordFieldChange(ctx context.Context, actorID, goalID int64, field Field, before, after string) error {
	return s.recordValueChange(ctx, db.RecordValueChangeParams{
		GoalID:      goalID,
		ActorID:     actorID,
		FieldID:     &field.ID,
		Attribute:   field.Name,
		BeforeValue: before,
		AfterValue:  after,
	})
}

// recordDimensionChanges keeps in the Goal's Value history how its values in
// dim changed from before, the values it carried (in any Dimension) before the
// change. In a Dimension that takes several values each value removed or added
// is its own entry; in one that takes one, the value before and after are one
// entry. Nothing is kept when nothing changed.
func (s *Service) recordDimensionChanges(ctx context.Context, actorID, goalID int64, dim Dimension, before []DimensionValue) error {
	carried, err := s.GoalValues(ctx, goalID)
	if err != nil {
		return err
	}
	was, now := valuesIn(before, dim.ID), valuesIn(carried, dim.ID)
	change := db.RecordValueChangeParams{GoalID: goalID, ActorID: actorID, DimensionID: &dim.ID, Attribute: dim.Name}
	if !dim.TakesSeveral() {
		change.BeforeValue, change.AfterValue = valueNames(was), valueNames(now)
		if change.BeforeValue == change.AfterValue {
			return nil
		}
		return s.recordValueChange(ctx, change)
	}
	change.Several = 1
	for _, v := range was {
		if !slices.ContainsFunc(now, func(n DimensionValue) bool { return n.ID == v.ID }) {
			change.BeforeValue, change.AfterValue = v.Value, ""
			if err := s.recordValueChange(ctx, change); err != nil {
				return err
			}
		}
	}
	for _, v := range now {
		if !slices.ContainsFunc(was, func(w DimensionValue) bool { return w.ID == v.ID }) {
			change.BeforeValue, change.AfterValue = "", v.Value
			if err := s.recordValueChange(ctx, change); err != nil {
				return err
			}
		}
	}
	return nil
}

// valuesIn keeps the values in one Dimension.
func valuesIn(values []DimensionValue, dimensionID int64) []DimensionValue {
	var out []DimensionValue
	for _, v := range values {
		if v.DimensionID == dimensionID {
			out = append(out, v)
		}
	}
	return out
}

// valueNames is the values' text, comma-separated, empty for none.
func valueNames(values []DimensionValue) string {
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, v.Value)
	}
	return strings.Join(names, ", ")
}

func (s *Service) recordValueChange(ctx context.Context, change db.RecordValueChangeParams) error {
	change.CreatedAt = s.clock.Now().Format(timeFormat)
	if err := s.queries.RecordValueChange(ctx, change); err != nil {
		return fmt.Errorf("record value change: %w", err)
	}
	return nil
}
