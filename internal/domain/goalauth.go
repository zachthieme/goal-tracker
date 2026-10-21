package domain

import (
	"context"
)

// authorizeGoalOwnerWrite allows actorID to change goalID only if they are its
// Owner (a Delegate and an Admin are refused), or ErrNotFound if no such Goal
// exists. It guards the commands the Goal page offers its Owner alone.
func (s *Service) authorizeGoalOwnerWrite(ctx context.Context, actorID, goalID int64) error {
	return s.RequireGoalOwner(ctx, actorID, goalID)
}
