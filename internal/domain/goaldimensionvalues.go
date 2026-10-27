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

// DimensionValuesEdit is one edit of the values a Goal carries in one
// Dimension: the values chosen from its list, and a name typed in, which sets
// the value it matches or adds it to the list (CONTEXT.md: Extendable).
type DimensionValuesEdit struct {
	GoalID      int64
	DimensionID int64
	// ValueIDs are the values chosen.
	ValueIDs []int64
	// NewValue is a name typed in; blank means none was, unless NameRequired.
	NewValue string
	// NameRequired refuses a blank NewValue, for a form whose only input is
	// the name.
	NameRequired bool
	// Join gives the Goal the values given alongside those it carries, in a
	// Dimension that takes several; in one that takes one they replace it.
	// Without it the values given become exactly what the Goal carries in the
	// Dimension, so a value left out is removed and none clears it.
	Join bool
	// ValueInput names the input a chosen value came from and NewValueInput
	// the typed name's, for the problems refused under them; "value_id" and
	// "new_value" when not given.
	ValueInput    func(valueID int64) string
	NewValueInput string
}

// valueInput names the input valueID was chosen in.
func (e DimensionValuesEdit) valueInput(valueID int64) string {
	if e.ValueInput == nil {
		return "value_id"
	}
	return e.ValueInput(valueID)
}

// newValueInput names the input the name was typed in.
func (e DimensionValuesEdit) newValueInput() string {
	if e.NewValueInput == "" {
		return "new_value"
	}
	return e.NewValueInput
}

// SetGoalDimensionValues sets the values a Goal carries in one Dimension, as e
// says, and returns the value the typed name set, or the zero value when none
// was typed. It is the one command for giving a Goal Dimension values
// (CONTEXT.md: Dimension), and these are its rules:
//
//   - A Dimension that takes one value is given at most one, chosen or typed.
//   - A Retired value, or any value in a Retired Dimension, can't be newly
//     given; one the Goal already carries may be kept, except when e joins
//     the values given to those it carries (CONTEXT.md: Retired).
//   - A typed name sets the value it matches whatever its case or surrounding
//     spaces, and is refused when that is Retired. Otherwise it is added to
//     the list, written to the Definition log, and can't contain a semicolon;
//     only an Admin may add to a Fixed list (CONTEXT.md: Fixed).
//
// Only the Goal's Owner, a Delegate or an Admin may (CONTEXT.md: Delegate).
// The values, their Value history and any value added to the list are written
// together, and a refusal writes nothing.
func (s *Service) SetGoalDimensionValues(ctx context.Context, actorID int64, e DimensionValuesEdit) (DimensionValue, error) {
	var typed DimensionValue
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.requireGoalValueSetter(ctx, actorID, e.GoalID); err != nil {
			return err
		}
		var err error
		typed, err = tx.setJudgedGoalDimensionValues(ctx, actorID, e)
		return err
	})
	if err != nil {
		return DimensionValue{}, err
	}
	return typed, nil
}

// setJudgedGoalDimensionValues is SetGoalDimensionValues for a caller that
// holds the transaction and has judged that actorID may set the Goal's values.
// It refuses with the first problem it finds.
func (s *Service) setJudgedGoalDimensionValues(ctx context.Context, actorID int64, e DimensionValuesEdit) (DimensionValue, error) {
	name := strings.TrimSpace(e.NewValue)
	if name == "" && e.NameRequired {
		return DimensionValue{}, Refuse(ErrValidation, "%s", blankNameSentence)
	}
	dimRow, err := s.queries.GetDimension(ctx, e.DimensionID)
	if err != nil {
		return DimensionValue{}, Refuse(ErrValidation, "dimension does not exist")
	}
	dim := dimensionFromRow(dimRow)
	valueIDs := dedupeIDs(e.ValueIDs)
	given := len(valueIDs)
	if name != "" {
		given++
	}
	if given > 1 {
		if sentence := oneValueSentence(dim); sentence != "" {
			return DimensionValue{}, Refuse(ErrValidation, "%s", sentence)
		}
	}
	carried, err := s.GoalValues(ctx, e.GoalID)
	if err != nil {
		return DimensionValue{}, err
	}
	carries := make(map[int64]bool, len(carried))
	for _, v := range carried {
		carries[v.ID] = true
	}
	for _, id := range valueIDs {
		row, err := s.queries.GetDimensionValue(ctx, id)
		if err != nil || row.DimensionID != dim.ID {
			return DimensionValue{}, Refuse(ErrValidation, "that value is not in %s", dim.Name)
		}
		if err := joinInputErrors(e.chosenProblems(dimensionValueFromRow(row), dim, carries)); err != nil {
			return DimensionValue{}, err
		}
	}
	var typed DimensionValue
	matched := false
	if name != "" {
		typed, matched, err = s.valueNamed(ctx, dim.ID, name)
		if err != nil {
			return DimensionValue{}, err
		}
		if err := joinInputErrors(typedValueProblems(dim, name, typed, matched, e.newValueInput())); err != nil {
			return DimensionValue{}, err
		}
		if !matched {
			sentence, err := fixedListRefusal(dim, s.adminJudge(ctx, actorID))
			if err != nil {
				return DimensionValue{}, err
			}
			if sentence != "" {
				return DimensionValue{}, Refuse(ErrNotAuthorized, "%s", sentence)
			}
		}
	}

	// The values chosen are saved first, then the one typed is given as joining
	// them, so each step is kept in the Value history as it always was.
	if len(valueIDs) > 0 || !e.Join {
		if err := s.writeGoalDimensionValues(ctx, e.GoalID, dim, carried, valueIDs, e.Join); err != nil {
			return DimensionValue{}, err
		}
		if err := s.recordDimensionChanges(ctx, actorID, e.GoalID, dim, carried); err != nil {
			return DimensionValue{}, err
		}
	}
	if name == "" {
		return DimensionValue{}, nil
	}
	if !matched {
		if typed, err = s.addDimensionValue(ctx, actorID, dimRow, name); err != nil {
			return DimensionValue{}, err
		}
	}
	before, err := s.GoalValues(ctx, e.GoalID)
	if err != nil {
		return DimensionValue{}, err
	}
	if err := s.writeGoalDimensionValues(ctx, e.GoalID, dim, before, []int64{typed.ID}, true); err != nil {
		return DimensionValue{}, err
	}
	if err := s.recordDimensionChanges(ctx, actorID, e.GoalID, dim, before); err != nil {
		return DimensionValue{}, err
	}
	return typed, nil
}

// goalDimensionValueProblems are the problems with one DimensionValuesEdit, by
// kind, so a caller checking several Dimensions can list each kind together:
// those of its chosen values, in the order they came, of its typed name, and
// its refusals of several values in a Dimension that takes one.
type goalDimensionValueProblems struct {
	chosen, typed, oneValue []*InputError
}

// checkGoalDimensionValues checks e by SetGoalDimensionValues' rules, writing
// nothing and judging no Goal permission, and returns every problem under e's
// input names. carried are the values the Goal carries. isAdmin judges whether
// the person setting the values is an Admin, asked only when a new value would
// be added to a Fixed list. Only the values that pass their own checks count
// toward a Dimension's one value. err is a failure to check, or a Dimension
// that doesn't exist.
func (s *Service) checkGoalDimensionValues(ctx context.Context, e DimensionValuesEdit, carried map[int64]bool, isAdmin func() (bool, error)) (problems goalDimensionValueProblems, err error) {
	dimRow, err := s.queries.GetDimension(ctx, e.DimensionID)
	if errors.Is(err, sql.ErrNoRows) {
		return problems, Refuse(ErrValidation, "dimension does not exist")
	} else if err != nil {
		return problems, fmt.Errorf("load dimension: %w", err)
	}
	dim := dimensionFromRow(dimRow)
	// given are the inputs that would give the Goal a value.
	var given []string
	for _, id := range dedupeIDs(e.ValueIDs) {
		input := e.valueInput(id)
		row, err := s.queries.GetDimensionValue(ctx, id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && row.DimensionID != dim.ID) {
			problems.chosen = append(problems.chosen, inputError(input, "that value is not in %s", dim.Name))
			continue
		} else if err != nil {
			return problems, fmt.Errorf("look up dimension value: %w", err)
		}
		if more := e.chosenProblems(dimensionValueFromRow(row), dim, carried); len(more) > 0 {
			problems.chosen = append(problems.chosen, more...)
			continue
		}
		given = append(given, input)
	}
	if name := strings.TrimSpace(e.NewValue); name != "" {
		input := e.newValueInput()
		match, matched, err := s.valueNamed(ctx, dim.ID, name)
		if err != nil {
			return problems, err
		}
		sentence := ""
		if more := typedValueProblems(dim, name, match, matched, input); len(more) > 0 {
			problems.typed = append(problems.typed, more...)
		} else if !matched {
			if sentence, err = fixedListRefusal(dim, isAdmin); err != nil {
				return problems, err
			}
		}
		switch {
		case sentence != "":
			problems.typed = append(problems.typed, inputError(input, "%s", sentence))
		case len(problems.typed) == 0:
			given = append(given, input)
		}
	} else if e.NameRequired {
		problems.typed = append(problems.typed, inputError(e.newValueInput(), "%s", blankNameSentence))
	}
	if sentence := oneValueSentence(dim); sentence != "" && len(given) > 1 {
		for _, input := range given {
			problems.oneValue = append(problems.oneValue, inputError(input, "%s", sentence))
		}
	}
	return problems, nil
}

// writeGoalDimensionValues gives the Goal valueIDs in dim, which carried, the
// values it carries, are to join (join) or be replaced by. Joining them in a
// Dimension that takes one value replaces the one it carries.
func (s *Service) writeGoalDimensionValues(ctx context.Context, goalID int64, dim Dimension, carried []DimensionValue, valueIDs []int64, join bool) error {
	switch {
	case join && !dim.TakesSeveral():
		if err := s.queries.ClearGoalValuesInDimension(ctx, db.ClearGoalValuesInDimensionParams{
			GoalID:      goalID,
			DimensionID: dim.ID,
		}); err != nil {
			return fmt.Errorf("clear existing value: %w", err)
		}
	case !join:
		for _, v := range carried {
			if v.DimensionID != dim.ID || slices.Contains(valueIDs, v.ID) {
				continue
			}
			if err := s.queries.RemoveGoalValue(ctx, db.RemoveGoalValueParams{
				GoalID:           goalID,
				DimensionValueID: v.ID,
			}); err != nil {
				return fmt.Errorf("remove dimension value: %w", err)
			}
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

// chosenProblems checks val, chosen in dim for a Goal carrying carries, by
// chosenValueProblems: a value the Goal carries is kept, and so exempt, only
// when e's values become exactly what it carries.
func (e DimensionValuesEdit) chosenProblems(val DimensionValue, dim Dimension, carries map[int64]bool) []*InputError {
	return chosenValueProblems(val, dim, carries[val.ID] && !e.Join, e.valueInput(val.ID))
}

// blankNameSentence is why a blank name is refused where one is required.
const blankNameSentence = "a value cannot be blank"

// oneValueSentence is why more than one value given a Goal in dim is refused,
// when dim takes one value per Goal, and "" when it takes several.
func oneValueSentence(dim Dimension) string {
	if dim.TakesSeveral() {
		return ""
	}
	return fmt.Sprintf("%s takes one value per Goal", dim.Name)
}

// adminJudge asks whether actorID is an Admin, who alone may add to a Fixed
// list, the first time it is called, and answers every later call from that.
func (s *Service) adminJudge(ctx context.Context, actorID int64) func() (bool, error) {
	var isAdmin *bool
	return func() (bool, error) {
		if isAdmin == nil {
			err := s.requireAdmin(ctx, actorID)
			if err != nil && !errors.Is(err, ErrNotAuthorized) {
				return false, err
			}
			admin := err == nil
			isAdmin = &admin
		}
		return *isAdmin, nil
	}
}

// fixedListRefusal is why a new value can't be added to dim's list by the
// person isAdmin judges, or "" when it can: anyone setting a Goal's values may
// add to an Extendable list, and only an Admin to a Fixed one (CONTEXT.md:
// Extendable, Fixed). isAdmin is asked only for a Fixed list.
func fixedListRefusal(dim Dimension, isAdmin func() (bool, error)) (string, error) {
	if dim.Extendable() {
		return "", nil
	}
	ok, err := isAdmin()
	if err != nil || ok {
		return "", err
	}
	return fixedListSentence(dim), nil
}

// chosenValueProblems is the one check of a Dimension value chosen for a
// Goal, val in dim, refused under input: a Retired value, or one in a Retired
// Dimension, can't be newly given (CONTEXT.md: Retired). One the Goal already
// carries (carried) may be kept.
func chosenValueProblems(val DimensionValue, dim Dimension, carried bool, input string) []*InputError {
	switch {
	case carried:
		return nil
	case val.Retired:
		return []*InputError{inputError(input, "%s is retired and cannot be newly assigned", val.Value)}
	case dim.Retired:
		return []*InputError{retiredDimensionProblem(dim, input)}
	}
	return nil
}

// typedValueProblems is the one check of a value typed in for a Goal in dim,
// refused under input. match is the value on dim's list that name matches
// (valueNamed), when matched. A Retired Dimension's values can't be newly
// given, nor can a Retired value it matches (CONTEXT.md: Retired), and a new
// value can't contain a semicolon (checkValueName). Who may add to a Fixed
// list is fixedListRefusal's rule.
func typedValueProblems(dim Dimension, name string, match DimensionValue, matched bool, input string) []*InputError {
	switch {
	case dim.Retired:
		return []*InputError{retiredDimensionProblem(dim, input)}
	case matched && match.Retired:
		return []*InputError{inputError(input, "%s", retiredValueMatchSentence(match))}
	case matched:
		return nil
	}
	if err := checkValueName(strings.TrimSpace(name)); err != nil {
		return []*InputError{inputError(input, "%s", Reason(err))}
	}
	return nil
}

// retiredDimensionProblem refuses newly giving a Goal a value in a Retired
// Dimension, under input (CONTEXT.md: Retired).
func retiredDimensionProblem(dim Dimension, input string) *InputError {
	return inputError(input, "%s is retired, so its values can't be newly assigned", dim.Name)
}

// fixedListSentence is why someone who isn't an Admin is refused adding a
// value to dim, a Fixed list (CONTEXT.md: Fixed).
func fixedListSentence(dim Dimension) string {
	return fmt.Sprintf("%s is a Fixed list, so only an Admin may add to it", dim.Name)
}
