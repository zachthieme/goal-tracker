package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Link is a "contributes to" relationship from a child Goal to a parent Goal
// (CONTEXT.md: Contributes to). It is Pending until the parent's Owner accepts
// it, at which point it is Accepted and part of the Goal graph.
type Link struct {
	ID        int64
	Child     Goal
	Parent    Goal
	Status    string
	Note      string
	CreatedAt time.Time
}

// Link status values. A rejected or removed link is deleted, so it has no
// status of its own.
const (
	LinkPending  = "pending"
	LinkAccepted = "accepted"
)

var (
	// ErrCycle is returned when a link would make the Goal graph cyclic
	// (ADR-0001: the graph has no cycles).
	ErrCycle = errors.New("link would create a cycle")
	// ErrNotAuthorized is returned when the acting Account may not perform the
	// requested link action (e.g. accepting a request to a Goal it does not own).
	ErrNotAuthorized = errors.New("not authorized")
)

// RequestLinkInput is the request-a-link command's input: the requester offers
// their child Goal up to contribute to parent, with an optional note.
type RequestLinkInput struct {
	ChildID     int64
	ParentID    int64
	Note        string
	RequesterID int64
}

// RequestLink requests that ChildID contribute to ParentID. The requester must
// own the child. The request is accepted immediately when the requester also
// owns the parent (CONTEXT.md: creating a child directly under a Goal you own);
// otherwise it waits Pending for the parent's Owner. A request that would close
// a cycle is rejected here and again at accept time (ADR-0001).
func (s *Service) RequestLink(ctx context.Context, in RequestLinkInput) (Link, error) {
	if in.ChildID == in.ParentID {
		return Link{}, fmt.Errorf("%w: a Goal cannot contribute to itself", ErrValidation)
	}

	child, err := s.queries.GetGoal(ctx, in.ChildID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: child goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, in.ParentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, fmt.Errorf("%w: parent goal does not exist", ErrValidation)
		}
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}

	if child.Goal.OwnerID != in.RequesterID {
		return Link{}, fmt.Errorf("%w: only the child's Owner may request a link", ErrNotAuthorized)
	}

	if _, err := s.queries.GetLinkByChildParent(ctx, db.GetLinkByChildParentParams{
		ChildID:  in.ChildID,
		ParentID: in.ParentID,
	}); err == nil {
		return Link{}, fmt.Errorf("%w: a link between these Goals already exists", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Link{}, fmt.Errorf("look up existing link: %w", err)
	}

	if err := s.ensureNoCycle(ctx, in.ChildID, in.ParentID); err != nil {
		return Link{}, err
	}

	status := LinkPending
	if parent.Goal.OwnerID == in.RequesterID {
		status = LinkAccepted
	}

	now := s.clock.Now()
	row, err := s.queries.CreateLink(ctx, db.CreateLinkParams{
		ChildID:     in.ChildID,
		ParentID:    in.ParentID,
		Status:      status,
		Note:        strings.TrimSpace(in.Note),
		RequestedBy: in.RequesterID,
		CreatedAt:   now.Format(timeFormat),
	})
	if err != nil {
		return Link{}, fmt.Errorf("create link: %w", err)
	}

	return Link{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: now,
	}, nil
}

// AcceptLink accepts a pending request. The actor must own the parent Goal. The
// cycle check is repeated because another accepted link may have appeared since
// the request was made (ADR-0001).
func (s *Service) AcceptLink(ctx context.Context, linkID, actorID int64) (Link, error) {
	row, err := s.getPendingLink(ctx, linkID)
	if err != nil {
		return Link{}, err
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}
	if parent.Goal.OwnerID != actorID {
		return Link{}, fmt.Errorf("%w: only the parent's Owner may accept a link", ErrNotAuthorized)
	}
	if err := s.ensureNoCycle(ctx, row.ChildID, row.ParentID); err != nil {
		return Link{}, err
	}
	if err := s.queries.SetLinkStatus(ctx, db.SetLinkStatusParams{
		Status: LinkAccepted,
		ID:     linkID,
	}); err != nil {
		return Link{}, fmt.Errorf("accept link: %w", err)
	}
	return s.loadLink(ctx, linkID)
}

// RejectLink rejects a pending request, deleting it. The actor must own the
// parent Goal.
func (s *Service) RejectLink(ctx context.Context, linkID, actorID int64) error {
	row, err := s.getPendingLink(ctx, linkID)
	if err != nil {
		return err
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return fmt.Errorf("look up parent goal: %w", err)
	}
	if parent.Goal.OwnerID != actorID {
		return fmt.Errorf("%w: only the parent's Owner may reject a link", ErrNotAuthorized)
	}
	if err := s.queries.DeleteLink(ctx, linkID); err != nil {
		return fmt.Errorf("reject link: %w", err)
	}
	return nil
}

// RemoveLink removes an accepted link, deleting it. Either the child's Owner or
// the parent's Owner may remove it.
func (s *Service) RemoveLink(ctx context.Context, linkID, actorID int64) error {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: link %d", ErrNotFound, linkID)
		}
		return fmt.Errorf("look up link: %w", err)
	}
	if row.Status != LinkAccepted {
		return fmt.Errorf("%w: link %d is not accepted", ErrNotFound, linkID)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return fmt.Errorf("look up parent goal: %w", err)
	}
	if child.Goal.OwnerID != actorID && parent.Goal.OwnerID != actorID {
		return fmt.Errorf("%w: only an Owner of the linked Goals may remove the link", ErrNotAuthorized)
	}
	if err := s.queries.DeleteLink(ctx, linkID); err != nil {
		return fmt.Errorf("remove link: %w", err)
	}
	return nil
}

// PendingLinkRequests returns the requests awaiting a decision from ownerID, the
// Owner of each request's parent Goal.
func (s *Service) PendingLinkRequests(ctx context.Context, ownerID int64) ([]Link, error) {
	rows, err := s.queries.ListPendingLinksForOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list pending links: %w", err)
	}
	links := make([]Link, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Link.CreatedAt)
		links = append(links, Link{
			ID:        r.Link.ID,
			Child:     goalFromRow(r.Goal, r.Account),     // child goal + its Owner
			Parent:    goalFromRow(r.Goal_2, r.Account_2), // parent goal + its Owner
			Status:    r.Link.Status,
			Note:      r.Link.Note,
			CreatedAt: createdAt,
		})
	}
	return links, nil
}

// ParentsOf returns the Goals goalID contributes to through accepted links.
func (s *Service) ParentsOf(ctx context.Context, goalID int64) ([]Goal, error) {
	rows, err := s.queries.ListParentGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list parent goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

// ChildrenOf returns the Goals that contribute to goalID through accepted links.
func (s *Service) ChildrenOf(ctx context.Context, goalID int64) ([]Goal, error) {
	rows, err := s.queries.ListChildGoals(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list child goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

// getPendingLink loads a link that must exist and still be pending.
func (s *Service) getPendingLink(ctx context.Context, linkID int64) (db.Link, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Link{}, fmt.Errorf("%w: link %d", ErrNotFound, linkID)
		}
		return db.Link{}, fmt.Errorf("look up link: %w", err)
	}
	if row.Status != LinkPending {
		return db.Link{}, fmt.Errorf("%w: link %d is not pending", ErrNotFound, linkID)
	}
	return row, nil
}

// loadLink loads a link with both endpoint Goals resolved.
func (s *Service) loadLink(ctx context.Context, linkID int64) (Link, error) {
	row, err := s.queries.GetLink(ctx, linkID)
	if err != nil {
		return Link{}, fmt.Errorf("look up link: %w", err)
	}
	child, err := s.queries.GetGoal(ctx, row.ChildID)
	if err != nil {
		return Link{}, fmt.Errorf("look up child goal: %w", err)
	}
	parent, err := s.queries.GetGoal(ctx, row.ParentID)
	if err != nil {
		return Link{}, fmt.Errorf("look up parent goal: %w", err)
	}
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	return Link{
		ID:        row.ID,
		Child:     goalFromRow(child.Goal, child.Account),
		Parent:    goalFromRow(parent.Goal, parent.Account),
		Status:    row.Status,
		Note:      row.Note,
		CreatedAt: createdAt,
	}, nil
}
