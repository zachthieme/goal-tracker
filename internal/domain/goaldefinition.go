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
// the single-page New goal form gathers. Title, Why it matters and the Owner are
// required, and a Dated Goal also needs its delivery date; the rest defines as
// much of the Goal as the Owner has to hand.
type DefinedGoalInput struct {
	Title        string
	WhyItMatters string
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
	// Unparsed are the values the caller couldn't parse, each refused under
	// its input's name. With any, nothing is saved, but the rest is still
	// checked, activation included, so every problem comes back together.
	Unparsed []*InputError
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
// comes back under InputActivate. A value the caller couldn't parse (see
// in.Unparsed) is refused with the rest, under its input.
func (s *Service) CreateDefinedGoal(ctx context.Context, in DefinedGoalInput) (Goal, error) {
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		problems, err := tx.checkDefinedGoal(ctx, in, 0, nil)
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			return withUnparsed(errors.Join(problems...), in.Unparsed)
		}
		g, err = tx.writeDefinedGoal(ctx, in)
		return withUnparsed(err, in.Unparsed)
	})
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// withUnparsed adds the values the caller couldn't parse to err, the outcome
// of checking and writing the rest, so a definition with any is refused with
// every problem together: each one replaces whatever err says of its input,
// which saw no value, and the rest of err's problems come first. An err that
// isn't a refusal is returned unchanged.
func withUnparsed(err error, unparsed []*InputError) error {
	if len(unparsed) == 0 || (err != nil && !errors.Is(err, ErrValidation)) {
		return err
	}
	refused := map[string]bool{}
	for _, p := range unparsed {
		refused[p.Input] = true
	}
	var problems []error
	for _, p := range InputErrors(err) {
		if !refused[p.Input] {
			problems = append(problems, p)
		}
	}
	for _, p := range unparsed {
		problems = append(problems, p)
	}
	return errors.Join(problems...)
}

// unparsedRefusal refuses the values the caller couldn't parse, every one
// together, for a command whose other input is checked only once they parse;
// it is nil when there are none.
func unparsedRefusal(unparsed []*InputError) error {
	return withUnparsed(nil, unparsed)
}

// DefinableGoal is the Goal goalID for its define page, when actorID may
// define it by DefineGoal's rule: its Owner, while it is Proposed. Anyone else
// is refused with ErrNotAuthorized, and the Owner once it is no longer
// Proposed with ErrValidation; a missing Goal is ErrNotFound. DefineGoal asks
// the rule again, inside its transaction.
func (s *Service) DefinableGoal(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.openGoalCommand(ctx, actorID, goalID, defineGoalRefusal, nil)
}

// DefineGoal finishes defining a Proposed Goal, as the New goal form shows it
// again, with CreateDefinedGoal's input, input names and rules for refusing
// it: every input is checked before anything is written, every problem comes
// back together, and any error leaves the Goal as it was. Only the Goal's
// Owner may (ErrNotAuthorized), and only while it is Proposed (ErrValidation).
// The actor is the Owner, whatever in.OwnerID says. A value the caller
// couldn't parse is refused with the rest, as CreateDefinedGoal refuses it.
//
// The Goal is changed to match the input. A changed Why it matters is kept as a
// revision (EditWhyItMatters), a Kind chosen, its delivery date and a cadence given
// replace the Goal's, and Milestones and Metrics are added to those it has. In
// each Dimension still offered the Goal carries just the values given, plus a
// new one typed for it, so a value left out is removed (SetGoalDimensionValues);
// a Retired value it already carries may be kept. Each Field still offered
// takes the value given, a blank one clearing it. A link is requested to each parent
// the Goal has no link to yet, Accepted or Pending; one that would close a
// cycle is refused. The Title can't be changed here.
func (s *Service) DefineGoal(ctx context.Context, actorID, goalID int64, in DefinedGoalInput) (Goal, error) {
	in.OwnerID = actorID
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		// Its unparsed values are refused with the rest of its problems
		// (withUnparsed), not on their own.
		current, err := tx.openGoalCommand(ctx, actorID, goalID, defineGoalRefusal, nil)
		if err != nil {
			return err
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
		problems, err := tx.checkDefinedGoal(ctx, in, goalID, carries)
		if err != nil {
			return err
		}
		more, err := tx.checkRedefinedGoal(ctx, current, in)
		if err != nil {
			return err
		}
		if problems = append(problems, more...); len(problems) > 0 {
			return withUnparsed(errors.Join(problems...), in.Unparsed)
		}
		g, err = tx.writeRedefinedGoal(ctx, current, in)
		return withUnparsed(err, in.Unparsed)
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
	left, err := s.parentCandidateContributors(ctx, goalID)
	if err != nil {
		return nil, err
	}
	left[goalID] = true
	for _, l := range append(accepted, pending...) {
		left[l.Goal.ID] = true
	}
	var out []Goal
	for _, g := range all {
		if !left[g.ID] {
			out = append(out, g)
		}
	}
	return out, nil
}

// parentCandidateContributors are the IDs of the Goals that contribute to
// goalID through accepted links, however indirectly: its descendants, read
// one level at a time, one query a level. Accepted links never form a cycle
// (ADR-0001), so the walk ends.
func (s *Service) parentCandidateContributors(ctx context.Context, goalID int64) (map[int64]bool, error) {
	seen := map[int64]bool{}
	frontier := []int64{goalID}
	for len(frontier) > 0 {
		children, err := s.childrenByGoal(ctx, frontier)
		if err != nil {
			return nil, err
		}
		var next []int64
		for _, id := range frontier {
			for _, c := range children[id] {
				if !seen[c.ID] {
					seen[c.ID] = true
					next = append(next, c.ID)
				}
			}
		}
		frontier = next
	}
	return seen, nil
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
// checkDefinedGoal's rules: its Title stays as it is, and no parent would
// close a cycle (ADR-0001). checkDefinedGoal refuses the Goal itself as a
// parent.
func (s *Service) checkRedefinedGoal(ctx context.Context, current Goal, in DefinedGoalInput) (problems []error, err error) {
	if title := strings.TrimSpace(in.Title); title != "" && title != current.Title {
		problems = append(problems, inputError(InputTitle, "a Goal's Title can't be changed here"))
	}
	for _, parentID := range dedupeIDs(in.ParentIDs) {
		if parentID == current.ID {
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
	if whyItMatters := strings.TrimSpace(in.WhyItMatters); whyItMatters != current.WhyItMatters {
		if _, err := s.EditWhyItMatters(ctx, goalID, whyItMatters, in.OwnerID); err != nil {
			return Goal{}, err
		}
	}
	var err error
	switch {
	case in.Kind == GoalDated && (current.Kind != GoalDated || !current.DeliveryDate.Equal(in.DeliveryDate)):
		_, err = s.MarkGoalDated(ctx, in.OwnerID, goalID, in.DeliveryDate)
	case in.Kind == GoalOngoing && current.Kind != GoalOngoing:
		_, err = s.MarkGoalOngoing(ctx, in.OwnerID, goalID)
	}
	if err != nil {
		return Goal{}, err
	}
	if in.CadenceDays != 0 && in.CadenceDays != current.CadenceDays {
		if _, err := s.SetCadence(ctx, in.OwnerID, goalID, in.CadenceDays); err != nil {
			return Goal{}, err
		}
	}
	if err := s.addDefinedMeasures(ctx, goalID, in); err != nil {
		return Goal{}, err
	}
	if err := s.writeDefinedValues(ctx, goalID, in); err != nil {
		return Goal{}, err
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
	g, err := s.CreateGoal(ctx, CreateGoalInput{Title: in.Title, WhyItMatters: in.WhyItMatters, OwnerID: in.OwnerID})
	if err != nil {
		return Goal{}, err
	}
	switch in.Kind {
	case GoalDated:
		_, err = s.MarkGoalDated(ctx, in.OwnerID, g.ID, in.DeliveryDate)
	case GoalOngoing:
		_, err = s.MarkGoalOngoing(ctx, in.OwnerID, g.ID)
	}
	if err != nil {
		return Goal{}, err
	}
	if in.CadenceDays != 0 {
		if _, err := s.SetCadence(ctx, in.OwnerID, g.ID, in.CadenceDays); err != nil {
			return Goal{}, err
		}
	}
	if err := s.addDefinedMeasures(ctx, g.ID, in); err != nil {
		return Goal{}, err
	}
	if err := s.writeDefinedValues(ctx, g.ID, in); err != nil {
		return Goal{}, err
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

// writeDefinedValues gives the Goal its definition's Dimension values, within
// the transaction the caller holds, by SetGoalDimensionValues' rules: in each
// Dimension still offered it carries just the values chosen, plus the one
// typed for it.
func (s *Service) writeDefinedValues(ctx context.Context, goalID int64, in DefinedGoalInput) error {
	dims, err := s.ListDimensions(ctx)
	if err != nil {
		return err
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
		if _, err := s.setJudgedGoalDimensionValues(ctx, in.OwnerID, DimensionValuesEdit{
			GoalID:      goalID,
			DimensionID: d.ID,
			ValueIDs:    ids,
			NewValue:    in.NewValues[d.ID],
		}); err != nil {
			return err
		}
	}
	return nil
}

// addDefinedMeasures adds each Milestone and Metric of a definition to the
// Goal, within the transaction the caller holds.
func (s *Service) addDefinedMeasures(ctx context.Context, goalID int64, in DefinedGoalInput) error {
	for _, m := range in.Milestones {
		if _, err := s.AddMilestoneAsAuthor(ctx, in.OwnerID, AddMilestoneInput{GoalID: goalID, Name: m.Name, TargetDate: m.Date}); err != nil {
			return err
		}
	}
	for _, m := range in.Metrics {
		if _, err := s.AddMetric(ctx, in.OwnerID, AddMetricInput{
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
		return s.activateDefinedGoal(ctx, in.OwnerID, goalID)
	}
	return s.loadGoal(ctx, goalID)
}

// checkDefinedGoal checks every input of a Goal's definition, writing nothing,
// and returns each problem as an *InputError, in the order the inputs come.
// Each part is checked by the check its own command uses (milestoneProblems,
// metricProblems, the value checks, fieldValueProblems, parentLinkProblems);
// what only definition refuses is checked here. goalID is the Goal being
// defined, 0 for a new one. carried are the values the Goal already carries,
// which it may keep though they are Retired (CONTEXT.md: Retired); a new Goal
// carries none. err is a failure to check, not a problem with the input.
func (s *Service) checkDefinedGoal(ctx context.Context, in DefinedGoalInput, goalID int64, carried map[int64]bool) (problems []error, err error) {
	refuse := func(input, format string, args ...any) {
		problems = append(problems, inputError(input, format, args...))
	}
	if strings.TrimSpace(in.Title) == "" {
		refuse(InputTitle, "a Goal needs a title")
	}
	if strings.TrimSpace(in.WhyItMatters) == "" {
		refuse(InputWhyItMatters, "a Goal needs to say why it matters")
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
		problems = appendInputErrors(problems, milestoneProblems(m.Name, m.Date, MilestoneInput(i, "name"), MilestoneInput(i, "date")))
	}
	for i, m := range in.Metrics {
		problems = appendInputErrors(problems, metricProblems(m.Name, m.Unit, m.Direction, m.TargetDate, func(part string) string {
			return MetricInput(i, part)
		}))
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
		problems = appendInputErrors(problems, fieldValueProblems(fieldFromRow(row), value, input))
	}
	given := map[int64]bool{}
	for _, parentID := range in.ParentIDs {
		input := ParentInput(parentID)
		if given[parentID] {
			refuse(input, "that parent Goal is given twice")
			continue
		}
		given[parentID] = true
		_, err := s.queries.GetGoal(ctx, parentID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("look up parent goal: %w", err)
		}
		problems = appendInputErrors(problems, parentLinkProblems(goalID, parentID, err == nil, input))
	}
	return problems, nil
}

// checkDefinedValues checks the Dimension values a Goal is to be given, both
// those chosen and those typed in as new, by SetGoalDimensionValues' rules
// (checkGoalDimensionValues) in each Dimension given values, under their
// inputs, refusing a value or Dimension that doesn't exist. A value in carried
// may be kept though it is Retired, or in a Retired Dimension. Whether the
// Owner is an Admin, who alone may add to a Fixed list, is asked at most once.
// The problems come in the form's order: the chosen values', in in.ValueIDs'
// order, then the typed values', by Dimension ID, then the refusals of several
// values in a Dimension that takes one.
func (s *Service) checkDefinedValues(ctx context.Context, in DefinedGoalInput, carried map[int64]bool) (problems []error, err error) {
	// order places each input in the form's order, and edits gathers what is
	// given in each Dimension, in the order the Dimensions come.
	order := map[string]int{}
	edits := map[int64]*DimensionValuesEdit{}
	var dimIDs []int64
	editIn := func(dimID int64) *DimensionValuesEdit {
		if edits[dimID] == nil {
			edits[dimID] = &DimensionValuesEdit{DimensionID: dimID, ValueInput: ValueInput, NewValueInput: NewValueInput(dimID)}
			dimIDs = append(dimIDs, dimID)
		}
		return edits[dimID]
	}
	var chosen, typed, oneValue []*InputError
	for _, valueID := range dedupeIDs(in.ValueIDs) {
		input := ValueInput(valueID)
		order[input] = len(order)
		val, err := s.queries.GetDimensionValue(ctx, valueID)
		if errors.Is(err, sql.ErrNoRows) {
			chosen = append(chosen, inputError(input, "that value does not exist"))
			continue
		} else if err != nil {
			return nil, fmt.Errorf("look up dimension value: %w", err)
		}
		e := editIn(val.DimensionID)
		e.ValueIDs = append(e.ValueIDs, valueID)
	}
	for _, dimID := range slices.Sorted(maps.Keys(in.NewValues)) {
		name := strings.TrimSpace(in.NewValues[dimID])
		if name == "" {
			continue
		}
		input := NewValueInput(dimID)
		order[input] = len(order)
		if _, err := s.queries.GetDimension(ctx, dimID); errors.Is(err, sql.ErrNoRows) {
			typed = append(typed, inputError(input, "that Dimension does not exist"))
			continue
		} else if err != nil {
			return nil, fmt.Errorf("look up dimension: %w", err)
		}
		editIn(dimID).NewValue = name
	}
	isAdmin := s.adminJudge(ctx, in.OwnerID)
	var oneValueByDimension [][]*InputError
	for _, dimID := range dimIDs {
		found, err := s.checkGoalDimensionValues(ctx, *edits[dimID], carried, isAdmin)
		if err != nil {
			return nil, err
		}
		chosen = append(chosen, found.chosen...)
		typed = append(typed, found.typed...)
		if len(found.oneValue) > 0 {
			oneValueByDimension = append(oneValueByDimension, found.oneValue)
		}
	}
	byOrder := func(a, b *InputError) int { return order[a.Input] - order[b.Input] }
	slices.SortStableFunc(chosen, byOrder)
	slices.SortStableFunc(typed, byOrder)
	// A Dimension's refusals come where the first value it was given does.
	slices.SortStableFunc(oneValueByDimension, func(a, b []*InputError) int { return byOrder(a[0], b[0]) })
	for _, refusals := range oneValueByDimension {
		oneValue = append(oneValue, refusals...)
	}
	problems = appendInputErrors(problems, chosen)
	problems = appendInputErrors(problems, typed)
	return appendInputErrors(problems, oneValue), nil
}

// activateDefinedGoal runs the activation gate on the Goal just defined,
// unchanged, and puts each rule it fails under the activate input.
func (s *Service) activateDefinedGoal(ctx context.Context, actorID, goalID int64) (Goal, error) {
	g, err := s.ActivateGoal(ctx, actorID, goalID)
	if err == nil {
		return g, nil
	}
	reasons := ActivationReasons(err)
	problems := make([]error, 0, len(reasons))
	for _, r := range reasons {
		if !errors.Is(r, ErrValidation) {
			return Goal{}, err
		}
		problems = append(problems, inputError(InputActivate, "%s", Reason(r)))
	}
	return Goal{}, errors.Join(problems...)
}

// The inputs of a Goal's definition, by the names the domain's errors and the
// New goal form share. A Milestone's, Metric's, value's, Field's or parent's
// is made by its function below.
const (
	InputTitle        = "title"
	InputWhyItMatters = "why_it_matters"
	InputKind         = "kind"
	InputDeliveryDate = "delivery_date"
	InputCadence      = "cadence"
	InputActivate     = "activate"
)

// InputError refuses one input of a Goal's definition, naming it as the New
// goal form does, and saying why as the Goal page would; or one input of a
// Check-in, named as the Check-in form does. It is an ErrValidation, and the
// refusal Err when it carries one.
type InputError struct {
	Input   string
	Message string
	// Err is the typed refusal Message tells of, for a caller to find with
	// errors.As (as a *MilestoneOverdueError); nil when there is none.
	Err error
}

func (e *InputError) Error() string {
	return fmt.Sprintf("%v: %s", ErrValidation, e.Message)
}

func (e *InputError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrValidation}
	}
	return []error{ErrValidation, e.Err}
}

func (e *InputError) shownReason() string { return e.Message }

func inputError(input, format string, args ...any) *InputError {
	return &InputError{Input: input, Message: fmt.Sprintf(format, args...)}
}

// joinInputErrors is a command's refusal of problems, a Goal part's check
// (milestoneProblems and the like), every one joined in order, or nil when
// there are none.
func joinInputErrors(problems []*InputError) error {
	errs := make([]error, 0, len(problems))
	for _, p := range problems {
		errs = append(errs, p)
	}
	return errors.Join(errs...)
}

// appendInputErrors adds more, a Goal part's problems, to problems, in order.
func appendInputErrors(problems []error, more []*InputError) []error {
	for _, p := range more {
		problems = append(problems, p)
	}
	return problems
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
		switch e := err.(type) { //nolint:errorlint // walks the joined tree itself, to collect every *InputError; errors.As would stop at the first
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
