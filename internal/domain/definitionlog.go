package domain

import (
	"context"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// DefinitionChange is one entry in the Definition log: a change to what
// Dimensions and Fields exist and how they're shaped, written only by the tool
// in the same transaction as the change. DimensionID or FieldID says which it
// is about (a change to a Dimension's value is about its Dimension), and
// Summary says what changed in a sentence, with the names as they were at the
// time. Values set on Goals aren't in it; they are in the Goal's Value history.
type DefinitionChange struct {
	ID          int64
	Actor       Account
	DimensionID int64
	FieldID     int64
	Summary     string
	CreatedAt   time.Time
}

// DefinitionLog returns every change to the Dimensions' and Fields'
// definitions, newest first. Anyone signed in may read it, and nobody can edit
// it. Changes made before the log was kept aren't in it.
func (s *Service) DefinitionLog(ctx context.Context) ([]DefinitionChange, error) {
	rows, err := s.queries.ListDefinitionChanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("list definition log: %w", err)
	}
	out := make([]DefinitionChange, 0, len(rows))
	for _, r := range rows {
		out = append(out, definitionChangeFromRow(r.DefinitionChange, r.Account))
	}
	return out, nil
}

// recordDimensionDefinitionChange writes one entry about a Dimension, or one of its
// values, to the Definition log.
func (s *Service) recordDimensionDefinitionChange(ctx context.Context, actorID, dimensionID int64, format string, args ...any) error {
	return s.recordDefinitionChange(ctx, db.RecordDefinitionChangeParams{
		ActorID:     actorID,
		DimensionID: &dimensionID,
		Summary:     fmt.Sprintf(format, args...),
	})
}

// recordFieldDefinitionChange writes one entry about a Field to the Definition
// log.
func (s *Service) recordFieldDefinitionChange(ctx context.Context, actorID, fieldID int64, format string, args ...any) error {
	return s.recordDefinitionChange(ctx, db.RecordDefinitionChangeParams{
		ActorID: actorID,
		FieldID: &fieldID,
		Summary: fmt.Sprintf(format, args...),
	})
}

func (s *Service) recordDefinitionChange(ctx context.Context, change db.RecordDefinitionChangeParams) error {
	change.CreatedAt = s.clock.Now().Format(timeFormat)
	if err := s.queries.RecordDefinitionChange(ctx, change); err != nil {
		return fmt.Errorf("record definition change: %w", err)
	}
	return nil
}

func definitionChangeFromRow(c db.DefinitionChange, actor db.Account) DefinitionChange {
	createdAt, _ := time.Parse(timeFormat, c.CreatedAt)
	change := DefinitionChange{
		ID:        c.ID,
		Actor:     accountFromRow(actor),
		Summary:   c.Summary,
		CreatedAt: createdAt,
	}
	if c.DimensionID != nil {
		change.DimensionID = *c.DimensionID
	}
	if c.FieldID != nil {
		change.FieldID = *c.FieldID
	}
	return change
}

// selectionPhrase says whether a Goal takes one of d's values or several, as
// the log words it.
func selectionPhrase(d Dimension) string {
	if d.TakesSeveral() {
		return "several values"
	}
	return "one value"
}

// listName is Fixed or Extendable, as CONTEXT.md names d's list.
func listName(d Dimension) string {
	if d.Extendable() {
		return "Extendable"
	}
	return "Fixed"
}

// requiredWord is how the log says a Dimension or Field was marked.
func requiredWord(required bool) string {
	if required {
		return "required"
	}
	return "not required"
}

// holdsPhrase says what f's value holds, as the log words it: "a number in $".
func holdsPhrase(f Field) string {
	switch f.Type {
	case FieldNumber:
		if f.Unit != "" {
			return "a number in " + f.Unit
		}
		return "a number"
	case FieldShortText:
		return "a short text"
	case FieldLongText:
		return "a long text"
	default:
		return "a date"
	}
}
