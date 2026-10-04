package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// DefinedGoalInput is the create-a-defined-Goal command's input: everything
// the single-page New goal form gathers. Only Title, So What and the Owner are
// required; the rest defines as much of the Goal as the Owner has to hand.
type DefinedGoalInput struct {
	Title  string
	SoWhat string
	// OwnerID is the acting person, who owns the Goal.
	OwnerID int64
	// Kind is GoalDated, with DeliveryDate, GoalOngoing, or "" for not chosen.
	Kind         string
	DeliveryDate time.Time
	// CadenceDays is how often a Check-in is expected; 0 leaves the default.
	CadenceDays int
	Milestones  []MilestoneDefinition
	Metrics     []MetricDefinition
	// ValueIDs are Dimension values to give the Goal. NewValues names a value
	// to add to an Extendable Dimension's list and give the Goal, by Dimension
	// ID (CONTEXT.md: Extendable). FieldValues is the Goal's value in each
	// Field, by Field ID.
	ValueIDs    []int64
	NewValues   map[int64]string
	FieldValues map[int64]string
	// ParentIDs are the Goals this one is to contribute to, each requested as
	// RequestLink requests it (CONTEXT.md: Contributes to).
	ParentIDs []int64
	// Activate moves the Goal to Active once it is defined, through the
	// activation gate (CONTEXT.md: Lifecycle).
	Activate bool
}

// MilestoneDefinition is a Milestone to add to a Goal being defined.
type MilestoneDefinition struct {
	Name string
	Date time.Time
}

// MetricDefinition is a Metric to add to a Goal being defined.
type MetricDefinition struct {
	Name       string
	Unit       string
	Direction  string
	Baseline   float64
	Target     float64
	TargetDate time.Time
}

// CreateDefinedGoal creates a Goal with its whole definition in one
// transaction, composing the commands that define each part: its Kind,
// cadence, Milestones, Metrics, Dimension values, Fields and parent links,
// and, with Activate, the activation gate. It is all or nothing: any error
// leaves nothing saved.
//
// Every input is checked before anything is written, and every problem comes
// back together, each an *InputError naming its input (see InputErrors). A
// parent given twice, and several values in a Dimension that takes one, are
// refused rather than half-applied. Activation runs only on clean inputs,
// after the writes, by ActivateGoal's rules unchanged; each rule it fails
// comes back under InputActivate.
func (s *Service) CreateDefinedGoal(ctx context.Context, in DefinedGoalInput) (Goal, error) {
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		problems, err := tx.checkDefinedGoal(ctx, in)
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			return errors.Join(problems...)
		}
		g, err = tx.writeDefinedGoal(ctx, in)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// writeDefinedGoal creates the Goal and writes each part of its definition,
// within the transaction the caller holds.
func (s *Service) writeDefinedGoal(ctx context.Context, in DefinedGoalInput) (Goal, error) {
	g, err := s.CreateGoal(ctx, CreateGoalInput{Title: in.Title, SoWhat: in.SoWhat, OwnerID: in.OwnerID})
	if err != nil {
		return Goal{}, err
	}
	switch in.Kind {
	case GoalDated:
		_, err = s.MarkGoalDated(ctx, g.ID, in.DeliveryDate)
	case GoalOngoing:
		_, err = s.MarkGoalOngoing(ctx, g.ID)
	}
	if err != nil {
		return Goal{}, err
	}
	if in.CadenceDays != 0 {
		if _, err := s.SetCadence(ctx, g.ID, in.CadenceDays); err != nil {
			return Goal{}, err
		}
	}
	for _, m := range in.Milestones {
		if _, err := s.AddMilestone(ctx, AddMilestoneInput{GoalID: g.ID, Name: m.Name, TargetDate: m.Date}); err != nil {
			return Goal{}, err
		}
	}
	for _, m := range in.Metrics {
		if _, err := s.AddMetric(ctx, AddMetricInput{
			GoalID:     g.ID,
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  m.Direction,
			Baseline:   m.Baseline,
			Target:     m.Target,
			TargetDate: m.TargetDate,
		}); err != nil {
			return Goal{}, err
		}
	}
	for _, id := range dedupeIDs(in.ValueIDs) {
		if err := s.AssignGoalValue(ctx, in.OwnerID, g.ID, id); err != nil {
			return Goal{}, err
		}
	}
	// AddDimensionValue is Admin-only; any value setter may add to an
	// Extendable list this way (CONTEXT.md: Extendable).
	for _, dimID := range slices.Sorted(maps.Keys(in.NewValues)) {
		if strings.TrimSpace(in.NewValues[dimID]) == "" {
			continue
		}
		if _, err := s.assignGoalValueByName(ctx, in.OwnerID, g.ID, dimID, in.NewValues[dimID]); err != nil {
			return Goal{}, err
		}
	}
	for _, fieldID := range slices.Sorted(maps.Keys(in.FieldValues)) {
		if strings.TrimSpace(in.FieldValues[fieldID]) == "" {
			continue
		}
		if err := s.SetGoalField(ctx, in.OwnerID, g.ID, fieldID, in.FieldValues[fieldID]); err != nil {
			return Goal{}, err
		}
	}
	for _, parentID := range in.ParentIDs {
		if _, err := s.RequestLink(ctx, RequestLinkInput{ChildID: g.ID, ParentID: parentID, RequesterID: in.OwnerID}); err != nil {
			return Goal{}, err
		}
	}
	if in.Activate {
		return s.activateDefinedGoal(ctx, g.ID)
	}
	return s.loadGoal(ctx, g.ID)
}

// checkDefinedGoal checks every input of a Goal's definition, writing nothing,
// and returns each problem as an *InputError, in the order the inputs come.
// err is a failure to check, not a problem with the input.
func (s *Service) checkDefinedGoal(ctx context.Context, in DefinedGoalInput) (problems []error, err error) {
	refuse := func(input, format string, args ...any) {
		problems = append(problems, inputError(input, format, args...))
	}
	if strings.TrimSpace(in.Title) == "" {
		refuse(InputTitle, "a Goal needs a title")
	}
	if strings.TrimSpace(in.SoWhat) == "" {
		refuse(InputSoWhat, "a Goal needs a So What")
	}
	switch in.Kind {
	case GoalDated:
		if in.DeliveryDate.IsZero() {
			refuse(InputDeliveryDate, "a Dated Goal needs a delivery date")
		}
	case GoalOngoing, "":
	default:
		refuse(InputKind, "a Goal is %s or %s, not %q", GoalDated, GoalOngoing, in.Kind)
	}
	if in.CadenceDays < 0 {
		refuse(InputCadence, "the Check-in cadence must be a positive number of days")
	}
	for i, m := range in.Milestones {
		if strings.TrimSpace(m.Name) == "" {
			refuse(MilestoneInput(i, "name"), "a Milestone needs a name")
		}
		if m.Date.IsZero() {
			refuse(MilestoneInput(i, "date"), "a Milestone needs a date")
		}
	}
	for i, m := range in.Metrics {
		if strings.TrimSpace(m.Name) == "" {
			refuse(MetricInput(i, "name"), "a Metric needs a name")
		}
		if strings.TrimSpace(m.Unit) == "" {
			refuse(MetricInput(i, "unit"), "a Metric needs a unit")
		}
		if !validDirection(m.Direction) {
			refuse(MetricInput(i, "direction"), "a Metric needs a direction of %q or %q", MetricUp, MetricDown)
		}
		if m.TargetDate.IsZero() {
			refuse(MetricInput(i, "target_date"), "a Metric needs a target date")
		}
	}
	valueProblems, err := s.checkDefinedValues(ctx, in)
	if err != nil {
		return nil, err
	}
	problems = append(problems, valueProblems...)
	for _, fieldID := range slices.Sorted(maps.Keys(in.FieldValues)) {
		value := strings.TrimSpace(in.FieldValues[fieldID])
		if value == "" {
			continue
		}
		input := FieldInput(fieldID)
		row, err := s.queries.GetField(ctx, fieldID)
		if errors.Is(err, sql.ErrNoRows) {
			refuse(input, "that Field does not exist")
			continue
		} else if err != nil {
			return nil, fmt.Errorf("look up field: %w", err)
		}
		field := fieldFromRow(row)
		if field.Retired {
			refuse(input, "%s is retired, so it can't be set", field.Name)
		} else if err := field.Check(value); err != nil {
			refuse(input, "%s", validationMessage(err))
		}
	}
	given := map[int64]bool{}
	for _, parentID := range in.ParentIDs {
		input := ParentInput(parentID)
		if given[parentID] {
			refuse(input, "that parent Goal is given twice")
			continue
		}
		given[parentID] = true
		if _, err := s.queries.GetGoal(ctx, parentID); errors.Is(err, sql.ErrNoRows) {
			refuse(input, "that parent Goal does not exist")
		} else if err != nil {
			return nil, fmt.Errorf("look up parent goal: %w", err)
		}
	}
	return problems, nil
}

// checkDefinedValues checks the Dimension values a Goal is to be given, both
// those chosen and those typed in as new, by the rules AssignGoalValue and
// AssignGoalValueByName follow, and refuses several in a Dimension that takes
// one, naming each of them.
func (s *Service) checkDefinedValues(ctx context.Context, in DefinedGoalInput) (problems []error, err error) {
	refuse := func(input, format string, args ...any) {
		problems = append(problems, inputError(input, format, args...))
	}
	// inputsIn gathers the inputs that would give the Goal a value in each
	// Dimension, so several in one that takes one are refused together.
	inputsIn := map[int64][]string{}
	var dims []Dimension
	given := func(dim Dimension, input string) {
		if _, seen := inputsIn[dim.ID]; !seen {
			dims = append(dims, dim)
		}
		inputsIn[dim.ID] = append(inputsIn[dim.ID], input)
	}
	for _, valueID := range dedupeIDs(in.ValueIDs) {
		input := ValueInput(valueID)
		val, err := s.queries.GetDimensionValue(ctx, valueID)
		if errors.Is(err, sql.ErrNoRows) {
			refuse(input, "that value does not exist")
			continue
		} else if err != nil {
			return nil, fmt.Errorf("look up dimension value: %w", err)
		}
		dimRow, err := s.queries.GetDimension(ctx, val.DimensionID)
		if err != nil {
			return nil, fmt.Errorf("load dimension: %w", err)
		}
		dim := dimensionFromRow(dimRow)
		switch {
		case val.Retired != 0:
			refuse(input, "%s is retired and cannot be newly assigned", val.Value)
		case dim.Retired:
			refuse(input, "%s", validationMessage(retiredDimensionError(dim)))
		default:
			given(dim, input)
		}
	}
	for _, dimID := range slices.Sorted(maps.Keys(in.NewValues)) {
		name := strings.TrimSpace(in.NewValues[dimID])
		if name == "" {
			continue
		}
		input := NewValueInput(dimID)
		dimRow, err := s.queries.GetDimension(ctx, dimID)
		if errors.Is(err, sql.ErrNoRows) {
			refuse(input, "that Dimension does not exist")
			continue
		} else if err != nil {
			return nil, fmt.Errorf("look up dimension: %w", err)
		}
		dim := dimensionFromRow(dimRow)
		if dim.Retired {
			refuse(input, "%s", validationMessage(retiredDimensionError(dim)))
			continue
		}
		if !dim.Extendable() {
			refuse(input, "%s is a Fixed list, so a new value can't be added to it here", dim.Name)
			continue
		}
		if err := checkValueName(name); err != nil {
			refuse(input, "%s", validationMessage(err))
			continue
		}
		if _, _, err := s.matchingValue(ctx, dimID, name); errors.Is(err, ErrValidation) {
			refuse(input, "%s", validationMessage(err))
			continue
		} else if err != nil {
			return nil, err
		}
		given(dim, input)
	}
	for _, dim := range dims {
		if inputs := inputsIn[dim.ID]; len(inputs) > 1 && !dim.TakesSeveral() {
			for _, input := range inputs {
				refuse(input, "%s takes one value per Goal", dim.Name)
			}
		}
	}
	return problems, nil
}

// activateDefinedGoal runs the activation gate on the Goal just defined,
// unchanged, and puts each rule it fails under the activate input.
func (s *Service) activateDefinedGoal(ctx context.Context, goalID int64) (Goal, error) {
	g, err := s.ActivateGoal(ctx, goalID)
	if err == nil {
		return g, nil
	}
	reasons := ActivationReasons(err)
	problems := make([]error, 0, len(reasons))
	for _, r := range reasons {
		if !errors.Is(r, ErrValidation) {
			return Goal{}, err
		}
		problems = append(problems, inputError(InputActivate, "%s", validationMessage(r)))
	}
	return Goal{}, errors.Join(problems...)
}

// The inputs of a Goal's definition, by the names the domain's errors and the
// New goal form share. A Milestone's, Metric's, value's, Field's or parent's
// is made by its function below.
const (
	InputTitle        = "title"
	InputSoWhat       = "so_what"
	InputKind         = "kind"
	InputDeliveryDate = "delivery_date"
	InputCadence      = "cadence"
	InputActivate     = "activate"
)

// InputError refuses one input of a Goal's definition, naming it as the New
// goal form does, and saying why as the Goal page would. It is an
// ErrValidation.
type InputError struct {
	Input   string
	Message string
}

func (e *InputError) Error() string {
	return fmt.Sprintf("%v: %s", ErrValidation, e.Message)
}

func (e *InputError) Unwrap() error { return ErrValidation }

func inputError(input, format string, args ...any) *InputError {
	return &InputError{Input: input, Message: fmt.Sprintf(format, args...)}
}

// validationMessage is a validation error's reason without the ErrValidation
// prefix, as an InputError carries it.
func validationMessage(err error) string {
	return strings.TrimPrefix(err.Error(), ErrValidation.Error()+": ")
}

// MilestoneInput names one part ("name", "date") of the i-th Milestone.
func MilestoneInput(i int, part string) string {
	return fmt.Sprintf("milestones[%d].%s", i, part)
}

// MetricInput names one part ("name", "unit", "direction", "baseline",
// "target", "target_date") of the i-th Metric.
func MetricInput(i int, part string) string {
	return fmt.Sprintf("metrics[%d].%s", i, part)
}

// ValueInput names a chosen Dimension value.
func ValueInput(valueID int64) string { return fmt.Sprintf("value:%d", valueID) }

// NewValueInput names the new value typed in for a Dimension.
func NewValueInput(dimensionID int64) string { return fmt.Sprintf("new_value:%d", dimensionID) }

// FieldInput names the value given in a Field.
func FieldInput(fieldID int64) string { return fmt.Sprintf("field:%d", fieldID) }

// ParentInput names a parent Goal.
func ParentInput(goalID int64) string { return fmt.Sprintf("parent:%d", goalID) }

// InputErrors lists every input err refuses, in order, so a form can show
// each beside its input. It is empty when err names no input.
func InputErrors(err error) []*InputError {
	var out []*InputError
	var walk func(error)
	walk = func(err error) {
		switch e := err.(type) {
		case *InputError:
			out = append(out, e)
		case interface{ Unwrap() []error }:
			for _, inner := range e.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(e.Unwrap())
		}
	}
	walk(err)
	return out
}
