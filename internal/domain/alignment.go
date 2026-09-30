package domain

import (
	"context"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// MarkTopLevel marks a Goal as a Top-level Goal: one of the org's root outcomes,
// not expected to contribute to anything and so never Unaligned (CONTEXT.md:
// Top-level Goal). Only an Admin may; actorID identifies the acting Account.
func (s *Service) MarkTopLevel(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.setTopLevel(ctx, actorID, goalID, true)
}

// UnmarkTopLevel clears a Goal's Top-level mark, so it is Unaligned again if it
// is Active and contributes to nothing. Only an Admin may.
func (s *Service) UnmarkTopLevel(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.setTopLevel(ctx, actorID, goalID, false)
}

func (s *Service) setTopLevel(ctx context.Context, actorID, goalID int64, topLevel bool) (Goal, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Goal{}, err
	}
	if _, err := s.loadGoal(ctx, goalID); err != nil {
		return Goal{}, err
	}
	var flag int64
	if topLevel {
		flag = 1
	}
	if err := s.queries.SetGoalTopLevel(ctx, db.SetGoalTopLevelParams{
		TopLevel: flag,
		ID:       goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("set top-level: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// UnalignedGoals lists the Unaligned Goals, newest first: Active Goals that
// contribute to no other Goal through an accepted link and aren't Top-level.
// Being Unaligned is allowed, but leadership should see it (CONTEXT.md:
// Unaligned). A pending link doesn't count until the parent's Owner accepts it.
func (s *Service) UnalignedGoals(ctx context.Context) ([]Goal, error) {
	rows, err := s.queries.ListUnalignedGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unaligned goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}
