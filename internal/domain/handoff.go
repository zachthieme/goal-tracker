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

// Handoff is the transfer of a Goal to a new Owner (CONTEXT.md: Handoff). It is
// started by the current Owner or an Admin, but takes effect only when the new
// Owner accepts it: until then it is Pending and ownership does not move.
type Handoff struct {
	ID        int64
	Goal      Goal
	From      Account
	To        Account
	Status    string
	CreatedAt time.Time
}

// Handoff status values. A rejected Handoff is deleted, so it has no status of
// its own.
const (
	HandoffPending  = "pending"
	HandoffAccepted = "accepted"
)

// StartHandoffInput is the start-a-Handoff command's input: transfer GoalID to
// ToOwnerID, on behalf of ActorID (the current Owner or an Admin).
type StartHandoffInput struct {
	GoalID    int64
	ToOwnerID int64
	ActorID   int64
}

// StartHandoff proposes transferring a Goal to a new Owner. The actor must be
// the Goal's current Owner or an Admin. The Handoff waits Pending and ownership
// does not move until the new Owner accepts it (CONTEXT.md: Handoff). Handing a
// Goal to its current Owner, or starting a second Handoff while one is pending,
// is rejected.
func (s *Service) StartHandoff(ctx context.Context, in StartHandoffInput) (Handoff, error) {
	goal, err := s.queries.GetGoal(ctx, in.GoalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Handoff{}, fmt.Errorf("look up goal: %w", err)
	}

	actor, err := s.queries.GetAccount(ctx, in.ActorID)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up actor: %w", err)
	}
	if goal.Goal.OwnerID != in.ActorID && actor.IsAdmin == 0 {
		return Handoff{}, fmt.Errorf("%w: only the Owner or an Admin may start a Handoff", ErrNotAuthorized)
	}

	if in.ToOwnerID == goal.Goal.OwnerID {
		return Handoff{}, fmt.Errorf("%w: the Goal is already owned by that person", ErrValidation)
	}
	newOwner, err := s.queries.GetAccount(ctx, in.ToOwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: the new Owner does not exist", ErrValidation)
		}
		return Handoff{}, fmt.Errorf("look up new owner: %w", err)
	}
	if newOwner.Departed != 0 {
		return Handoff{}, fmt.Errorf("%w: the new Owner has left the org", ErrValidation)
	}

	if _, err := s.queries.GetPendingHandoffForGoal(ctx, in.GoalID); err == nil {
		return Handoff{}, fmt.Errorf("%w: a Handoff for this Goal is already pending", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Handoff{}, fmt.Errorf("look up pending handoff: %w", err)
	}

	now := s.clock.Now()
	row, err := s.queries.CreateHandoff(ctx, db.CreateHandoffParams{
		GoalID:      in.GoalID,
		FromOwner:   goal.Goal.OwnerID,
		ToOwner:     in.ToOwnerID,
		Status:      HandoffPending,
		InitiatedBy: in.ActorID,
		CreatedAt:   now.Format(timeFormat),
	})
	if err != nil {
		return Handoff{}, fmt.Errorf("create handoff: %w", err)
	}
	return Handoff{
		ID:        row.ID,
		Goal:      goalFromRow(goal.Goal, goal.Account),
		From:      accountFromRow(goal.Account),
		To:        accountFromRow(newOwner),
		Status:    row.Status,
		CreatedAt: now,
	}, nil
}

// StartHandoffByEmail starts a Handoff to the Account with the given email. It is
// the web-facing convenience over StartHandoff, since the tool identifies people
// by email (CONTEXT.md: development sign-in by email). An email with no account
// is rejected.
func (s *Service) StartHandoffByEmail(ctx context.Context, goalID int64, toEmail string, actorID int64) (Handoff, error) {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(toEmail))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: no account with email %q", ErrValidation, toEmail)
		}
		return Handoff{}, fmt.Errorf("look up new owner: %w", err)
	}
	return s.StartHandoff(ctx, StartHandoffInput{GoalID: goalID, ToOwnerID: acc.ID, ActorID: actorID})
}

// ReassignGoalByEmail reassigns an Ownerless Goal to the Account with the given
// email. It is the web-facing convenience over ReassignGoal. An email with no
// account is rejected.
func (s *Service) ReassignGoalByEmail(ctx context.Context, actorID, goalID int64, newOwnerEmail string) (Goal, error) {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(newOwnerEmail))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: no account with email %q", ErrValidation, newOwnerEmail)
		}
		return Goal{}, fmt.Errorf("look up new owner: %w", err)
	}
	return s.ReassignGoal(ctx, actorID, goalID, acc.ID)
}

// AcceptHandoff accepts a pending Handoff, moving the Goal to the new Owner. Only
// the proposed new Owner may accept it (CONTEXT.md: ownership changes are always
// accepted by the new Owner).
func (s *Service) AcceptHandoff(ctx context.Context, handoffID, actorID int64) (Handoff, error) {
	row, err := s.getPendingHandoff(ctx, handoffID)
	if err != nil {
		return Handoff{}, err
	}
	if row.ToOwner != actorID {
		return Handoff{}, fmt.Errorf("%w: only the new Owner may accept a Handoff", ErrNotAuthorized)
	}
	if err := s.queries.SetGoalOwner(ctx, db.SetGoalOwnerParams{
		OwnerID: row.ToOwner,
		ID:      row.GoalID,
	}); err != nil {
		return Handoff{}, fmt.Errorf("transfer ownership: %w", err)
	}
	if err := s.queries.SetHandoffStatus(ctx, db.SetHandoffStatusParams{
		Status: HandoffAccepted,
		ID:     handoffID,
	}); err != nil {
		return Handoff{}, fmt.Errorf("accept handoff: %w", err)
	}
	return s.loadHandoff(ctx, handoffID)
}

// RejectHandoff declines a pending Handoff, deleting it; ownership stays put.
// Only the proposed new Owner may reject it.
func (s *Service) RejectHandoff(ctx context.Context, handoffID, actorID int64) error {
	row, err := s.getPendingHandoff(ctx, handoffID)
	if err != nil {
		return err
	}
	if row.ToOwner != actorID {
		return fmt.Errorf("%w: only the new Owner may reject a Handoff", ErrNotAuthorized)
	}
	if err := s.queries.DeleteHandoff(ctx, handoffID); err != nil {
		return fmt.Errorf("reject handoff: %w", err)
	}
	return nil
}

// PendingHandoffs returns the Handoffs awaiting a decision from toOwnerID, the
// proposed new Owner, each with its Goal and both Owners resolved.
func (s *Service) PendingHandoffs(ctx context.Context, toOwnerID int64) ([]Handoff, error) {
	rows, err := s.queries.ListPendingHandoffsForNewOwner(ctx, toOwnerID)
	if err != nil {
		return nil, fmt.Errorf("list pending handoffs: %w", err)
	}
	out := make([]Handoff, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Handoff.CreatedAt)
		out = append(out, Handoff{
			ID:        r.Handoff.ID,
			Goal:      goalFromRow(r.Goal, r.Account), // Goal with its current (from) Owner
			From:      accountFromRow(r.Account),      // from_acct
			To:        accountFromRow(r.Account_2),    // to_acct
			Status:    r.Handoff.Status,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

// ReassignGoal moves an Ownerless Goal to a present Owner (CONTEXT.md: an Admin
// reassigns an Ownerless Goal). Only an Admin may do this, and only for a Goal
// that is Ownerless — a Goal with a present Owner changes hands through a Handoff
// the new Owner accepts, not by fiat.
func (s *Service) ReassignGoal(ctx context.Context, actorID, goalID, newOwnerID int64) (Goal, error) {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return Goal{}, err
	}
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Goal{}, fmt.Errorf("look up goal: %w", err)
	}
	if goal.Account.Departed == 0 {
		return Goal{}, fmt.Errorf("%w: only an Ownerless Goal is reassigned; hand off a Goal with a present Owner", ErrValidation)
	}
	newOwner, err := s.queries.GetAccount(ctx, newOwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Goal{}, fmt.Errorf("%w: the new Owner does not exist", ErrValidation)
		}
		return Goal{}, fmt.Errorf("look up new owner: %w", err)
	}
	if newOwner.Departed != 0 {
		return Goal{}, fmt.Errorf("%w: the new Owner has left the org", ErrValidation)
	}
	if err := s.queries.SetGoalOwner(ctx, db.SetGoalOwnerParams{
		OwnerID: newOwnerID,
		ID:      goalID,
	}); err != nil {
		return Goal{}, fmt.Errorf("reassign goal: %w", err)
	}
	return s.loadGoal(ctx, goalID)
}

// getPendingHandoff loads a Handoff that must exist and still be pending.
func (s *Service) getPendingHandoff(ctx context.Context, handoffID int64) (db.Handoff, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Handoff{}, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return db.Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	if row.Status != HandoffPending {
		return db.Handoff{}, fmt.Errorf("%w: handoff %d is not pending", ErrNotFound, handoffID)
	}
	return row, nil
}

// loadHandoff loads a Handoff with its Goal and both Owners resolved.
func (s *Service) loadHandoff(ctx context.Context, handoffID int64) (Handoff, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	goal, err := s.queries.GetGoal(ctx, row.GoalID)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up goal: %w", err)
	}
	from, err := s.queries.GetAccount(ctx, row.FromOwner)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up from owner: %w", err)
	}
	to, err := s.queries.GetAccount(ctx, row.ToOwner)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up to owner: %w", err)
	}
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	return Handoff{
		ID:        row.ID,
		Goal:      goalFromRow(goal.Goal, goal.Account),
		From:      accountFromRow(from),
		To:        accountFromRow(to),
		Status:    row.Status,
		CreatedAt: createdAt,
	}, nil
}
