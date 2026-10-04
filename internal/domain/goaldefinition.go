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

	"github.com/zachthieme/goal-tracker/internal/db"
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
		problems, err := tx.checkDefinedGoal(ctx, in, nil)
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

// DefineGoal finishes defining a Proposed Goal, as the New goal form shows it
// again, with CreateDefinedGoal's input, input names and rules for refusing
// it: every input is checked before anything is written, every problem comes
// back together, and any error leaves the Goal as it was. Only the Goal's
// Owner may (ErrNotAuthorized), and only while it is Proposed (ErrValidation).
// The actor is the Owner, whatever in.OwnerID says.
//
// The Goal is changed to match the input. A changed So What is kept as a
// revision (EditSoWhat), a Kind chosen, its delivery date and a cadence given
// replace the Goal's, and Milestones and Metrics are added to those it has. In
// each Dimension still offered the Goal carries just the values given, plus a
// new one typed for it, so a value left out is removed (SetGoalValues); a
// Retired value it already carries may be kept. Each Field still offered takes
// the value given, a blank one clearing it. A link is requested to each parent
// the Goal has no link to yet, Accepted or Pending; one that would close a
// cycle is refused. The Title can't be changed here.
func (s *Service) DefineGoal(ctx context.Context, actorID, goalID int64, in DefinedGoalInput) (Goal, error) {
	in.OwnerID = actorID
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		current, err := tx.loadGoal(ctx, goalID)
		if err != nil {
			return err
		}
		if current.Owner.ID != actorID {
			return fmt.Errorf("%w: only the Goal's Owner may define it", ErrNotAuthorized)
		}
		if current.Lifecycle != LifecycleProposed {
			return fmt.Errorf("%w: %s is %s, so it is no longer defined here", ErrValidation, current.Title, current.Lifecycle)
		}
		carried, err := tx.GoalValues(ctx, goalID)
		if err != nil {
			return err
		}
		carries := map[int64]bool{}
		for _, v := range carried {
			carries[v.ID] = true
		}
		if in.ParentIDs, err = tx.unlinkedParents(ctx, goalID, in.ParentIDs); err != nil {
			return err
		}
		if in.FieldValues, err = tx.changedFields(ctx, goalID, in.FieldValues); err != nil {
			return err
		}
		problems, err := tx.checkDefinedGoal(ctx, in, carries)
		if err != nil {
			return err
		}
		more, err := tx.checkRedefinedGoal(ctx, current, in)
		if err != nil {
			return err
		}
		if problems = append(problems, more...); len(problems) > 0 {
			return errors.Join(problems...)
		}
		g, err = tx.writeRedefinedGoal(ctx, current, in)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// ParentCandidates are the Goals goalID could be asked to contribute to, in
// ListGoals' order: every Goal but itself, those it is already linked to,
// Accepted or Pending, and those that contribute to it however indirectly,
// which would close a cycle (ADR-0001).
func (s *Service) ParentCandidates(ctx context.Context, goalID int64) ([]Goal, error) {
	all, err := s.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	accepted, err := s.ParentLinks(ctx, goalID)
	if err != nil {
		return nil, err
	}
	pending, err := s.PendingParentLinks(ctx, goalID)
	if err != nil {
		return nil, err
	}
	linked := map[int64]bool{goalID: true}
	for _, l := range append(accepted, pending...) {
		linked[l.Goal.ID] = true
	}
	var out []Goal
	for _, g := range all {
		if linked[g.ID] {
			continue
		}
		if err := s.ensureNoCycle(ctx, goalID, g.ID); errors.Is(err, ErrCycle) {
			continue
		} else if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// unlinkedParents are the parents of ids the Goal has no link to yet, in
// order: a link already Accepted or Pending is kept as it is, not requested
// again.
func (s *Service) unlinkedParents(ctx context.Context, goalID int64, ids []int64) ([]int64, error) {
	var out []int64
	for _, parentID := range ids {
		_, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{ChildID: goalID, ParentID: parentID})
		if errors.Is(err, sql.ErrNoRows) {
			out = append(out, parentID)
		} else if err != nil {
			return nil, fmt.Errorf("look up existing link: %w", err)
		}
	}
	return out, nil
}

// changedFields are the Field values of values that differ from the Goal's,
// so one it already has, even in a Field since Retired, is no change to
// check.
func (s *Service) changedFields(ctx context.Context, goalID int64, values map[int64]string) (map[int64]string, error) {
	have, err := s.GoalFields(ctx, goalID)
	if err != nil {
		return nil, err
	}
	was := map[int64]string{}
	for _, v := range have {
		was[v.Field.ID] = v.Value
	}
	out := map[int64]string{}
	for fieldID, value := range values {
		if strings.TrimSpace(value) != was[fieldID] {
			out[fieldID] = value
		}
	}
	return out, nil
}

// checkRedefinedGoal checks what redefining a saved Goal adds to
// checkDefinedGoal's rules: its Title stays as it is, and no parent is the
// Goal itself or would close a cycle (ADR-0001).
func (s *Service) checkRedefinedGoal(ctx context.Context, current Goal, in DefinedGoalInput) (problems []error, err error) {
	if title := strings.TrimSpace(in.Title); title != "" && title != current.Title {
		problems = append(problems, inputError(InputTitle, "a Goal's Title can't be changed here"))
	}
	for _, parentID := range dedupeIDs(in.ParentIDs) {
		if parentID == current.ID {
			problems = append(problems, inputError(ParentInput(parentID), "a Goal cannot contribute to itself"))
			continue
		}
		if err := s.ensureNoCycle(ctx, current.ID, parentID); errors.Is(err, ErrCycle) {
			problems = append(problems, inputError(ParentInput(parentID), "that Goal already contributes to this one, so this one can't contribute to it"))
		} else if err != nil {
			return nil, err
		}
	}
	return problems, nil
}

// writeRedefinedGoal writes each part of a checked definition over the saved
// Goal current, within the transaction the caller holds.
func (s *Service) writeRedefinedGoal(ctx context.Context, current Goal, in DefinedGoalInput) (Goal, error) {
	goalID := current.ID
	if soWhat := strings.TrimSpace(in.SoWhat); soWhat != current.SoWhat {
		if _, err := s.EditSoWhat(ctx, goalID, soWhat, in.OwnerID); err != nil {
			return Goal{}, err
		}
	}
	var err error
	switch {
	case in.Kind == GoalDated && (current.Kind != GoalDated || !current.DeliveryDate.Equal(in.DeliveryDate)):
		_, err = s.MarkGoalDated(ctx, goalID, in.DeliveryDate)
	case in.Kind == GoalOngoing && current.Kind != GoalOngoing:
		_, err = s.MarkGoalOngoing(ctx, goalID)
	}
	if err != nil {
		return Goal{}, err
	}
	if in.CadenceDays != 0 && in.CadenceDays != current.CadenceDays {
		if _, err := s.SetCadence(ctx, goalID, in.CadenceDays); err != nil {
			return Goal{}, err
		}
	}
	if err := s.addDefinedMeasures(ctx, goalID, in); err != nil {
		return Goal{}, err
	}
	dims, err := s.ListDimensions(ctx)
	if err != nil {
		return Goal{}, err
	}
	given := map[int64]bool{}
	for _, id := range in.ValueIDs {
		given[id] = true
	}
	for _, d := range OfferedDimensions(dims) {
		var ids []int64
		for _, val := range d.Values {
			if given[val.ID] {
				ids = append(ids, val.ID)
			}
		}
		if err := s.SetGoalValues(ctx, in.OwnerID, goalID, d.ID, ids); err != nil {
			return Goal{}, err
		}
		if name := strings.TrimSpace(in.NewValues[d.ID]); name != "" {
			if _, err := s.assignGoalValueByName(ctx, in.OwnerID, goalID, d.ID, name); err != nil {
				return Goal{}, err
			}
		}
	}
	fields, err := s.ListFields(ctx)
	if err != nil {
		return Goal{}, err
	}
	for _, f := range OfferedFields(fields) {
		value, changed := in.FieldValues[f.ID]
		if !changed {
			continue
		}
		if err := s.SetGoalField(ctx, in.OwnerID, goalID, f.ID, value); err != nil {
			return Goal{}, err
		}
	}
	return s.linkAndActivate(ctx, goalID, in)
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
	if err := s.addDefinedMeasures(ctx, g.ID, in); err != nil {
		return Goal{}, err
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
	return s.linkAndActivate(ctx, g.ID, in)
}

// addDefinedMeasures adds each Milestone and Metric of a definition to the
// Goal, within the transaction the caller holds.
func (s *Service) addDefinedMeasures(ctx context.Context, goalID int64, in DefinedGoalInput) error {
	for _, m := range in.Milestones {
		if _, err := s.AddMilestone(ctx, AddMilestoneInput{GoalID: goalID, Name: m.Name, TargetDate: m.Date}); err != nil {
			return err
		}
	}
	for _, m := range in.Metrics {
		if _, err := s.AddMetric(ctx, AddMetricInput{
			GoalID:     goalID,
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  m.Direction,
			Baseline:   m.Baseline,
			Target:     m.Target,
			TargetDate: m.TargetDate,
		}); err != nil {
			return err
		}
	}
	return nil
}

// linkAndActivate requests a link to each of a definition's parents and, with
// Activate, runs the activation gate, the last steps of defining a Goal,
// within the transaction the caller holds.
func (s *Service) linkAndActivate(ctx context.Context, goalID int64, in DefinedGoalInput) (Goal, error) {
	for _, parentID := range in.ParentIDs {
		if _, err := s.RequestLink(ctx, RequestLinkInput{ChildID: goalID, ParentID: parentID, RequesterID: in.OwnerID}); err != nil {
			return Goal{}, err
		}
	}
	if in.Activate {
		return s.activateDefinedGoal(ctx, goalID)
	}
	return s.loadGoal(ctx, goalID)
}

// checkDefinedGoal checks every input of a Goal's definition, writing nothing,
// and returns each problem as an *InputError, in the order the inputs come.
// carried are the values the Goal already carries, which it may keep though
// they are Retired (CONTEXT.md: Retired); a new Goal carries none. err is a
// failure to check, not a problem with the input.
func (s *Service) checkDefinedGoal(ctx context.Context, in DefinedGoalInput, carried map[int64]bool) (problems []error, err error) {
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
	valueProblems, err := s.checkDefinedValues(ctx, in, carried)
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
// one, naming each of them. A value in carried may be kept though it is
// Retired, or in a Retired Dimension, as SetGoalValues keeps it.
func (s *Service) checkDefinedValues(ctx context.Context, in DefinedGoalInput, carried map[int64]bool) (problems []error, err error) {
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
		case carried[valueID]:
			given(dim, input)
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
