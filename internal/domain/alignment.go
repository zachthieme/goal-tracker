package domain

import (
	"context"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// MarkTopLevel marks a Goal as a Top-level Goal: one of the org's root outcomes,
// not expected to contribute to anything and so never Unlinked (CONTEXT.md:
// Top-level Goal). Only an Admin may; actorID identifies the acting Account.
func (s *Service) MarkTopLevel(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.setTopLevel(ctx, actorID, goalID, true)
}

// UnmarkTopLevel clears a Goal's Top-level mark, so it is Unlinked again if it
// is Active and contributes to nothing. Only an Admin may.
func (s *Service) UnmarkTopLevel(ctx context.Context, actorID, goalID int64) (Goal, error) {
	return s.setTopLevel(ctx, actorID, goalID, false)
}

func (s *Service) setTopLevel(ctx context.Context, actorID, goalID int64, topLevel bool) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.goalRule(ctx, actorID, goalID, func(a goalActor, _ Goal) error {
			return goalAdminRefusal(a)
		}); err != nil {
			return err
		}
		var flag int64
		if topLevel {
			flag = 1
		}
		if err := tx.queries.SetGoalTopLevel(ctx, db.SetGoalTopLevelParams{
			TopLevel: flag,
			ID:       goalID,
		}); err != nil {
			return fmt.Errorf("set top-level: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// UnlinkedGoals lists the Unlinked Goals, newest first: Active Goals that
// contribute to no other Goal through an accepted link and aren't Top-level.
// Being Unlinked is allowed, but leadership should see it (CONTEXT.md:
// Unlinked). A pending link doesn't count until the parent's Owner accepts it.
func (s *Service) UnlinkedGoals(ctx context.Context) ([]Goal, error) {
	rows, err := s.queries.ListUnlinkedGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unlinked goals: %w", err)
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

// GraphSignals are the signals found in how Goals are linked and dated: the
// Unlinked Goals, the schedule conflicts between linked Goals, and the Goals
// whose parent is On Hold or Cancelled.
type GraphSignals struct {
	Unlinked          []Goal
	ScheduleConflicts []ScheduleConflict
	HaltedParents     []HaltedParent
}

// GoalSignals are the graph signals that touch one Goal: whether it is
// Unlinked, every schedule conflict it is the child or the parent in, and each
// of its parents that is On Hold or Cancelled.
type GoalSignals struct {
	Unlinked          bool
	ScheduleConflicts []ScheduleConflict
	HaltedParents     []HaltedParent
}

// edge is one accepted "contributes to" link with both endpoint Goals resolved.
type edge struct {
	child, parent Goal
}

// GraphSignals reads the org-wide graph signals off the current graph.
func (s *Service) GraphSignals(ctx context.Context) (GraphSignals, error) {
	unlinked, err := s.UnlinkedGoals(ctx)
	if err != nil {
		return GraphSignals{}, err
	}
	edges, err := s.acceptedEdges(ctx)
	if err != nil {
		return GraphSignals{}, err
	}
	return graphSignalsOf(unlinked, edges), nil
}

// acceptedEdges reads every accepted link as an edge from its child to its
// parent.
func (s *Service) acceptedEdges(ctx context.Context) ([]edge, error) {
	rows, err := s.queries.ListAcceptedLinkGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accepted links: %w", err)
	}
	edges := make([]edge, 0, len(rows))
	for _, r := range rows {
		edges = append(edges, edge{
			child:  goalFromRow(r.Goal, r.Account),
			parent: goalFromRow(r.Goal_2, r.Account_2),
		})
	}
	return edges, nil
}

// graphSignalsOf is GraphSignals given the Unlinked Goals and every accepted
// link's edge.
func graphSignalsOf(unlinked []Goal, edges []edge) GraphSignals {
	return GraphSignals{
		Unlinked:          unlinked,
		ScheduleConflicts: scheduleConflicts(edges),
		HaltedParents:     haltedParents(edges),
	}
}

// unlinkedAmong is UnlinkedGoals over active, every Active Goal in the order
// it was read, given every accepted link's edge, so a reader that has both
// needn't read the Unlinked Goals again.
func unlinkedAmong(active []Goal, edges []edge) []Goal {
	parents := map[int64]int{}
	for _, e := range edges {
		parents[e.child.ID]++
	}
	var out []Goal
	for _, g := range active {
		if isUnlinked(g, parents[g.ID]) {
			out = append(out, g)
		}
	}
	return out
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
	return goalSignalsOf(g, parents, children), nil
}

// goalSignalsOf is GoalSignals for g, which contributes to parents and which
// children contribute to.
func goalSignalsOf(g Goal, parents, children []Goal) GoalSignals {
	up := make([]edge, 0, len(parents))
	for _, p := range parents {
		up = append(up, edge{child: g, parent: p})
	}
	down := make([]edge, 0, len(children))
	for _, c := range children {
		down = append(down, edge{child: c, parent: g})
	}
	return GoalSignals{
		Unlinked:          isUnlinked(g, len(parents)),
		ScheduleConflicts: scheduleConflicts(append(up, down...)),
		// Only the child is flagged when its parent halts, so a Goal's own
		// halted-parent signals come from its links upward.
		HaltedParents: haltedParents(up),
	}
}

// isUnlinked applies the Unlinked rule to a Goal with the given number of
// accepted parents (CONTEXT.md: Unlinked).
func isUnlinked(g Goal, parents int) bool {
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
