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

// ScheduleConflict is a risk the graph flags on its own: Child contributes to
// Parent but is due to deliver later than Parent is. Both Goals are flagged.
type ScheduleConflict struct {
	Child  Goal
	Parent Goal
}

// HaltedParent is a risk the graph flags on its own: Child contributes to a
// Parent that has gone On Hold or been Cancelled, so the child's work may no
// longer be needed. The child is flagged.
type HaltedParent struct {
	Child  Goal
	Parent Goal
}

// GraphSignals are the risks the Goal graph flags that nobody reported: the
// Unaligned Goals, the schedule conflicts between linked Goals, and the Goals
// whose parent is On Hold or Cancelled.
type GraphSignals struct {
	Unaligned         []Goal
	ScheduleConflicts []ScheduleConflict
	HaltedParents     []HaltedParent
}

// GoalSignals are the graph signals that touch one Goal: whether it is
// Unaligned, every schedule conflict it is the child or the parent in, and each
// of its parents that is On Hold or Cancelled.
type GoalSignals struct {
	Unaligned         bool
	ScheduleConflicts []ScheduleConflict
	HaltedParents     []HaltedParent
}

// edge is one accepted "contributes to" link with both endpoint Goals resolved.
type edge struct {
	child, parent Goal
}

// GraphSignals reads the org-wide graph signals off the current graph.
func (s *Service) GraphSignals(ctx context.Context) (GraphSignals, error) {
	unaligned, err := s.UnalignedGoals(ctx)
	if err != nil {
		return GraphSignals{}, err
	}
	rows, err := s.queries.ListAcceptedLinkGoals(ctx)
	if err != nil {
		return GraphSignals{}, fmt.Errorf("list accepted links: %w", err)
	}
	edges := make([]edge, 0, len(rows))
	for _, r := range rows {
		edges = append(edges, edge{
			child:  goalFromRow(r.Goal, r.Account),
			parent: goalFromRow(r.Goal_2, r.Account_2),
		})
	}
	return GraphSignals{
		Unaligned:         unaligned,
		ScheduleConflicts: scheduleConflicts(edges),
		HaltedParents:     haltedParents(edges),
	}, nil
}

// GoalSignals reads the graph signals that touch goalID: its own links up to its
// parents and down to its children.
func (s *Service) GoalSignals(ctx context.Context, goalID int64) (GoalSignals, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return GoalSignals{}, err
	}
	parents, err := s.ParentsOf(ctx, goalID)
	if err != nil {
		return GoalSignals{}, err
	}
	children, err := s.ChildrenOf(ctx, goalID)
	if err != nil {
		return GoalSignals{}, err
	}
	up := make([]edge, 0, len(parents))
	for _, p := range parents {
		up = append(up, edge{child: g, parent: p})
	}
	down := make([]edge, 0, len(children))
	for _, c := range children {
		down = append(down, edge{child: c, parent: g})
	}
	return GoalSignals{
		Unaligned:         isUnaligned(g, len(parents)),
		ScheduleConflicts: scheduleConflicts(append(up, down...)),
		// Only the child is flagged when its parent halts, so a Goal's own
		// halted-parent signals come from its links upward.
		HaltedParents: haltedParents(up),
	}, nil
}

// isUnaligned applies the Unaligned rule to a Goal with the given number of
// accepted parents (CONTEXT.md: Unaligned).
func isUnaligned(g Goal, parents int) bool {
	return g.Lifecycle == LifecycleActive && !g.TopLevel && parents == 0
}

// scheduleConflicts keeps the edges whose child delivers later than its parent.
// Both Goals need a delivery date, and a Goal that is Done or Cancelled can no
// longer put its partner's date at risk, so it raises no conflict.
func scheduleConflicts(edges []edge) []ScheduleConflict {
	var out []ScheduleConflict
	for _, e := range edges {
		if ended(e.child) || ended(e.parent) {
			continue
		}
		if e.child.DeliveryDate.IsZero() || e.parent.DeliveryDate.IsZero() {
			continue
		}
		if e.child.DeliveryDate.After(e.parent.DeliveryDate) {
			out = append(out, ScheduleConflict{Child: e.child, Parent: e.parent})
		}
	}
	return out
}

// ended reports whether a Goal has reached an end state of its Lifecycle.
func ended(g Goal) bool {
	return g.Lifecycle == LifecycleDone || g.Lifecycle == LifecycleCancelled
}

// haltedParents keeps the edges whose parent is On Hold or Cancelled. A child
// that is itself Done or Cancelled has nothing left at risk, so it isn't flagged.
func haltedParents(edges []edge) []HaltedParent {
	var out []HaltedParent
	for _, e := range edges {
		if ended(e.child) {
			continue
		}
		if e.parent.Lifecycle == LifecycleOnHold || e.parent.Lifecycle == LifecycleCancelled {
			out = append(out, HaltedParent{Child: e.child, Parent: e.parent})
		}
	}
	return out
}
