package domain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Field is an admin-defined attribute whose value is entered directly on a
// Goal rather than chosen from a list (CONTEXT.md: Field; ADR 0005). It
// describes a Goal and never filters, groups or sums. Type is fixed at
// creation, and Unit labels a number Field's values (e.g. "$", "FTE").
// Required says every Active Goal should have a value in it; one that doesn't
// is Incomplete (CONTEXT.md: Incomplete).
type Field struct {
	ID       int64
	Name     string
	Type     string
	Unit     string
	Retired  bool
	Required bool
}

// A Field's Type: what its value holds (CONTEXT.md: Field).
const (
	FieldNumber    = "number"
	FieldShortText = "short_text"
	FieldLongText  = "long_text"
	FieldDate      = "date"
)

// CreateField defines a new Field of one of the four types. Only an Admin may
// (CONTEXT.md: Admin). The name is required. Only a number Field keeps a unit.
// It is written to the Definition log.
func (s *Service) CreateField(ctx context.Context, actorID int64, name, fieldType, unit string) (Field, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Field{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Field{}, fmt.Errorf("%w: a Field needs a name", ErrValidation)
	}
	switch fieldType {
	case FieldNumber:
		unit = strings.TrimSpace(unit)
	case FieldShortText, FieldLongText, FieldDate:
		unit = ""
	default:
		return Field{}, fmt.Errorf("%w: a Field holds a number, a short text, a long text or a date", ErrValidation)
	}
	if err := s.requireFreeAttributeName(ctx, name); err != nil {
		return Field{}, err
	}
	var field Field
	err := s.WithinTx(ctx, func(tx *Service) error {
		row, err := tx.queries.CreateField(ctx, db.CreateFieldParams{
			Name:      name,
			Type:      fieldType,
			Unit:      unit,
			CreatedAt: tx.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create field: %w", err)
		}
		field = fieldFromRow(row)
		return tx.recordFieldDefinitionChange(ctx, actorID, field.ID, "Created the Field %s, holding %s.", field.Name, holdsPhrase(field))
	})
	if err != nil {
		return Field{}, err
	}
	return field, nil
}

// FieldValue is a Goal's value in one Field, as typed: a number Field's parses
// as a number and a date Field's is a date as YYYY-MM-DD.
type FieldValue struct {
	Field Field
	Value string
}

// RetireField withdraws a Field: it is no longer offered for entry, yet the
// Goals with a value in it still show it. Nothing is deleted (CONTEXT.md:
// Retired; ADR 0005). It is written to the Definition log. Only an Admin may.
func (s *Service) RetireField(ctx context.Context, actorID, fieldID int64) error {
	return s.setFieldRetired(ctx, actorID, fieldID, true)
}

// RestoreField reverses a Field's retirement, so it is offered for entry again
// (CONTEXT.md: Retired — an Admin can reverse it). It is written to the
// Definition log. Only an Admin may.
func (s *Service) RestoreField(ctx context.Context, actorID, fieldID int64) error {
	return s.setFieldRetired(ctx, actorID, fieldID, false)
}

func (s *Service) setFieldRetired(ctx context.Context, actorID, fieldID int64, retired bool) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		before, err := tx.queries.GetField(ctx, fieldID)
		if err != nil {
			return fmt.Errorf("%w: field does not exist", ErrValidation)
		}
		if fieldFromRow(before).Retired == retired {
			return nil
		}
		if _, err := tx.queries.SetFieldRetired(ctx, db.SetFieldRetiredParams{Retired: boolFlag(retired), ID: fieldID}); err != nil {
			return fmt.Errorf("set field retired: %w", err)
		}
		verb := "Restored"
		if retired {
			verb = "Retired"
		}
		return tx.recordFieldDefinitionChange(ctx, actorID, fieldID, "%s the Field %s.", verb, before.Name)
	})
}

// SetFieldRequired marks a Field required, so a Proposed Goal can't become
// Active without a value in it, or unmarks it (CONTEXT.md: Incomplete). Marking
// is always allowed: Active Goals lacking a value stay Active and become
// Incomplete. It is written to the Definition log. Only an Admin may.
func (s *Service) SetFieldRequired(ctx context.Context, actorID, fieldID int64, required bool) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		before, err := tx.queries.GetField(ctx, fieldID)
		if err != nil {
			return fmt.Errorf("%w: field does not exist", ErrValidation)
		}
		if fieldFromRow(before).Required == required {
			return nil
		}
		if _, err := tx.queries.SetFieldRequired(ctx, db.SetFieldRequiredParams{Required: boolFlag(required), ID: fieldID}); err != nil {
			return fmt.Errorf("set field required: %w", err)
		}
		return tx.recordFieldDefinitionChange(ctx, actorID, fieldID, "Marked the Field %s %s.", before.Name, requiredWord(required))
	})
}

// OfferedFields keeps the Fields still offered for entry on a Goal, dropping
// the Retired ones (CONTEXT.md: Retired).
func OfferedFields(fields []Field) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if !f.Retired {
			out = append(out, f)
		}
	}
	return out
}

// SetGoalField sets a Goal's value in a Field, replacing the one it had; a
// blank value clears it (CONTEXT.md: Field). A change is kept in the Goal's
// Value history, and saving the value it already has changes nothing. A number
// Field's value must parse as a number and a date Field's as a YYYY-MM-DD date,
// or it is refused naming the Field. A Retired Field can't be set. Only the
// Goal's Owner, a Delegate or an Admin may (CONTEXT.md: Delegate).
func (s *Service) SetGoalField(ctx context.Context, actorID, goalID, fieldID int64, value string) error {
	if err := s.requireGoalValueSetter(ctx, actorID, goalID); errors.Is(err, ErrNotAuthorized) {
		return fmt.Errorf("%w: only the Owner, a Delegate or an Admin may set a Goal's Fields", ErrNotAuthorized)
	} else if err != nil {
		return err
	}
	row, err := s.queries.GetField(ctx, fieldID)
	if err != nil {
		return fmt.Errorf("%w: field does not exist", ErrValidation)
	}
	field := fieldFromRow(row)
	if field.Retired {
		return fmt.Errorf("%w: %s is retired, so it can't be set", ErrValidation, field.Name)
	}
	value = strings.TrimSpace(value)
	before, err := s.goalFieldValue(ctx, goalID, fieldID)
	if err != nil {
		return err
	}
	if value == before {
		return nil
	}
	if value == "" {
		if err := s.queries.ClearGoalFieldValue(ctx, db.ClearGoalFieldValueParams{GoalID: goalID, FieldID: fieldID}); err != nil {
			return fmt.Errorf("clear field value: %w", err)
		}
		return s.recordFieldChange(ctx, actorID, goalID, field, before, "")
	}
	if err := field.Check(value); err != nil {
		return err
	}
	if err := s.queries.SetGoalFieldValue(ctx, db.SetGoalFieldValueParams{
		GoalID:    goalID,
		FieldID:   fieldID,
		Value:     value,
		UpdatedAt: s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("set field value: %w", err)
	}
	return s.recordFieldChange(ctx, actorID, goalID, field, before, value)
}

// goalFieldValue is a Goal's value in one Field, empty when it has none.
func (s *Service) goalFieldValue(ctx context.Context, goalID, fieldID int64) (string, error) {
	values, err := s.GoalFields(ctx, goalID)
	if err != nil {
		return "", err
	}
	for _, v := range values {
		if v.Field.ID == fieldID {
			return v.Value, nil
		}
	}
	return "", nil
}

// GoalFields returns the values a Goal has, by Field name, Retired Fields
// included so their values stay readable (CONTEXT.md: Retired).
func (s *Service) GoalFields(ctx context.Context, goalID int64) ([]FieldValue, error) {
	rows, err := s.queries.ListGoalFieldValues(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal field values: %w", err)
	}
	out := make([]FieldValue, 0, len(rows))
	for _, r := range rows {
		out = append(out, FieldValue{Field: fieldFromRow(r.Field), Value: r.Value})
	}
	return out, nil
}

// Check refuses a value that doesn't parse as the Field's type, naming the
// Field, so the spreadsheet import reports it the way the Goal page does.
func (f Field) Check(value string) error {
	switch f.Type {
	case FieldNumber:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("%w: %s takes a number, and %q isn't one", ErrValidation, f.Name, value)
		}
	case FieldDate:
		if _, err := time.Parse(dateFormat, value); err != nil {
			return fmt.Errorf("%w: %s takes a date as YYYY-MM-DD, and %q isn't one", ErrValidation, f.Name, value)
		}
	}
	return nil
}

// ListFields returns every Field by name, Retired ones included.
func (s *Service) ListFields(ctx context.Context) ([]Field, error) {
	rows, err := s.queries.ListFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	out := make([]Field, 0, len(rows))
	for _, r := range rows {
		out = append(out, fieldFromRow(r))
	}
	return out, nil
}

// requireFreeAttributeName refuses a name for a new Dimension or Field that a
// Dimension or a Field already has, whatever its letter case or surrounding
// spaces, saying which one has it: the two share one namespace, so a name on a
// Goal, or a column in the spreadsheet import, says which attribute it is (ADR
// 0005). Names that already collide are left as they are.
func (s *Service) requireFreeAttributeName(ctx context.Context, name string) error {
	dims, err := s.queries.ListDimensions(ctx)
	if err != nil {
		return fmt.Errorf("list dimensions: %w", err)
	}
	for _, d := range dims {
		if strings.EqualFold(strings.TrimSpace(d.Name), name) {
			return fmt.Errorf("%w: %s is already a Dimension's name", ErrValidation, d.Name)
		}
	}
	fields, err := s.queries.ListFields(ctx)
	if err != nil {
		return fmt.Errorf("list fields: %w", err)
	}
	for _, f := range fields {
		if strings.EqualFold(strings.TrimSpace(f.Name), name) {
			return fmt.Errorf("%w: %s is already a Field's name", ErrValidation, f.Name)
		}
	}
	return nil
}

func fieldFromRow(f db.Field) Field {
	return Field{ID: f.ID, Name: f.Name, Type: f.Type, Unit: f.Unit, Retired: f.Retired != 0, Required: f.Required != 0}
}
