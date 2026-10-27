package domain

import "context"

// CheckGoalDimensionValues is SetGoalDimensionValues' check-only mode on e for
// actorID, the person who would set the values: every problem, the chosen
// values' first, then the typed name's, then the refusals of several values in
// a Dimension that takes one.
func CheckGoalDimensionValues(ctx context.Context, s *Service, actorID int64, e DimensionValuesEdit) ([]*InputError, error) {
	values, err := s.GoalValues(ctx, e.GoalID)
	if err != nil {
		return nil, err
	}
	carried := map[int64]bool{}
	for _, v := range values {
		carried[v.ID] = true
	}
	found, err := s.checkGoalDimensionValues(ctx, e, carried, s.adminJudge(ctx, actorID))
	if err != nil {
		return nil, err
	}
	return append(append(found.chosen, found.typed...), found.oneValue...), nil
}
