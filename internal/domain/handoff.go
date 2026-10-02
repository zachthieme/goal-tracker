package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Handoff is the transfer of a Goal to a new Owner (CONTEXT.md: Handoff). It is
// started by the current Owner or an Admin, but takes effect only when the new
// Owner accepts it: until then it is Pending and ownership does not move.
type Handoff struct {
	ID          int64
	Goal        Goal
	From        Account
	To          Account
	InitiatedBy Account
	Status      string
	CreatedAt   time.Time
	// KeepableDelegates are the Goal's Delegates the new Owner chooses whether to
	// keep on accepting (CONTEXT.md: Delegate): those who aren't Departed, other
	// than the new Owner. Only PendingHandoffs resolves them.
	KeepableDelegates []Account
}

// Handoff status values: a Handoff's outcome. Every Handoff is kept with its
// outcome (CONTEXT.md: Handoff), so a Goal can have many past Handoffs but only
// one pending.
const (
	HandoffPending   = "pending"
	HandoffAccepted  = "accepted"
	HandoffRejected  = "rejected"
	HandoffCancelled = "cancelled"
	// HandoffReassigned records an Admin Reassign of an Ownerless Goal: an
	// ownership change kept in the same history, which no one accepts.
	HandoffReassigned = "reassigned"
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
		ID:          row.ID,
		Goal:        goalFromRow(goal.Goal, goal.Account),
		From:        accountFromRow(goal.Account),
		To:          accountFromRow(newOwner),
		InitiatedBy: accountFromRow(actor),
		Status:      row.Status,
		CreatedAt:   now,
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
// accepted by the new Owner). The new Owner chooses which of the Goal's
// Delegates to keep (CONTEXT.md: Delegate): every Delegate not in keep is
// removed, except a Departed one, who was never offered and stays. The new Owner
// is never kept as their own Delegate, since the Owner already writes the Goal's
// Check-ins. A Handoff whose from-Owner no longer owns the Goal is refused. The
// ownership change, the removals and the outcome are one transaction.
func (s *Service) AcceptHandoff(ctx context.Context, handoffID, actorID int64, keep []int64) (Handoff, error) {
	row, err := s.getPendingHandoff(ctx, handoffID)
	if err != nil {
		return Handoff{}, err
	}
	if row.ToOwner != actorID {
		return Handoff{}, fmt.Errorf("%w: only the new Owner may accept a Handoff", ErrNotAuthorized)
	}
	err = s.WithinTx(ctx, func(tx *Service) error {
		goal, err := tx.queries.GetGoal(ctx, row.GoalID)
		if err != nil {
			return fmt.Errorf("look up goal: %w", err)
		}
		if goal.Goal.OwnerID != row.FromOwner {
			return fmt.Errorf("%w: the Goal has changed hands since this Handoff was started", ErrValidation)
		}
		if err := tx.queries.SetGoalOwner(ctx, db.SetGoalOwnerParams{
			OwnerID: row.ToOwner,
			ID:      row.GoalID,
		}); err != nil {
			return fmt.Errorf("transfer ownership: %w", err)
		}
		delegates, err := tx.ListDelegates(ctx, row.GoalID)
		if err != nil {
			return err
		}
		for _, d := range delegates {
			kept := slices.Contains(keep, d.ID) && d.ID != row.ToOwner
			if d.Departed || kept {
				continue
			}
			if err := tx.queries.RemoveDelegate(ctx, db.RemoveDelegateParams{
				GoalID:    row.GoalID,
				AccountID: d.ID,
			}); err != nil {
				return fmt.Errorf("remove delegate: %w", err)
			}
		}
		if err := tx.queries.SetHandoffStatus(ctx, db.SetHandoffStatusParams{
			Status: HandoffAccepted,
			ID:     handoffID,
		}); err != nil {
			return fmt.Errorf("accept handoff: %w", err)
		}
		return nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return s.loadHandoff(ctx, handoffID)
}

// RejectHandoff declines a pending Handoff, keeping it with the outcome
// rejected; ownership stays put. Only the proposed new Owner may reject it, and
// they may Undo it (RestoreHandoff).
func (s *Service) RejectHandoff(ctx context.Context, handoffID, actorID int64) error {
	row, err := s.getPendingHandoff(ctx, handoffID)
	if err != nil {
		return err
	}
	if row.ToOwner != actorID {
		return fmt.Errorf("%w: only the new Owner may reject a Handoff", ErrNotAuthorized)
	}
	if err := s.queries.SetHandoffStatus(ctx, db.SetHandoffStatusParams{
		Status: HandoffRejected,
		ID:     handoffID,
	}); err != nil {
		return fmt.Errorf("reject handoff: %w", err)
	}
	return nil
}

// RestoreHandoff undoes rejecting a Handoff, putting it back as pending with
// the same proposed Owner, so the Goal's ownership history shows it pending
// rather than a rejection followed by something else. Only the person who
// rejected it, its proposed new Owner, may, and only while it is rejected: once
// undone it is pending, so a second Undo is refused. It is refused, changing
// nothing, when the Goal has another pending Handoff, its Owner has changed, or
// the proposed Owner has since Departed. Rejecting tells no one, so neither
// does the Undo.
func (s *Service) RestoreHandoff(ctx context.Context, handoffID, actorID int64) (Handoff, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	if row.ToOwner != actorID {
		return Handoff{}, fmt.Errorf("%w: only the person who rejected a Handoff may undo it", ErrNotAuthorized)
	}
	notRejected := fmt.Errorf("%w: this Handoff isn't rejected, so there is nothing to undo", ErrValidation)
	if row.Status != HandoffRejected {
		return Handoff{}, notRejected
	}
	err = s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.queries.GetPendingHandoffForGoal(ctx, row.GoalID); err == nil {
			return fmt.Errorf("%w: another Handoff of this Goal is pending", ErrValidation)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up pending handoff: %w", err)
		}
		goal, err := tx.queries.GetGoal(ctx, row.GoalID)
		if err != nil {
			return fmt.Errorf("look up goal: %w", err)
		}
		if goal.Goal.OwnerID != row.FromOwner {
			return fmt.Errorf("%w: the Goal has changed hands since this Handoff was rejected", ErrValidation)
		}
		to, err := tx.queries.GetAccount(ctx, row.ToOwner)
		if err != nil {
			return fmt.Errorf("look up new owner: %w", err)
		}
		if to.Departed != 0 {
			return fmt.Errorf("%w: the new Owner has left the org", ErrValidation)
		}
		n, err := tx.queries.ReopenRejectedHandoff(ctx, handoffID)
		if err != nil {
			return fmt.Errorf("restore handoff: %w", err)
		}
		if n == 0 {
			return notRejected
		}
		return nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return s.loadHandoff(ctx, handoffID)
}

// Handoff returns a Handoff with its Goal, both Owners, and its initiator
// resolved, so a page can say what was decided and offer its Undo.
func (s *Service) Handoff(ctx context.Context, handoffID int64) (Handoff, error) {
	if _, err := s.queries.GetHandoff(ctx, handoffID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	return s.loadHandoff(ctx, handoffID)
}

// PendingHandoffs returns the Handoffs awaiting a decision from toOwnerID, the
// proposed new Owner, each with its Goal, both Owners, and the Delegates offered
// to keep resolved.
func (s *Service) PendingHandoffs(ctx context.Context, toOwnerID int64) ([]Handoff, error) {
	rows, err := s.queries.ListPendingHandoffsForNewOwner(ctx, toOwnerID)
	if err != nil {
		return nil, fmt.Errorf("list pending handoffs: %w", err)
	}
	out := make([]Handoff, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Handoff.CreatedAt)
		delegates, err := s.ListDelegates(ctx, r.Goal.ID)
		if err != nil {
			return nil, err
		}
		keepable := make([]Account, 0, len(delegates))
		for _, d := range delegates {
			if !d.Departed && d.ID != toOwnerID {
				keepable = append(keepable, d)
			}
		}
		out = append(out, Handoff{
			ID:                r.Handoff.ID,
			Goal:              goalFromRow(r.Goal, r.Account), // Goal with its current (from) Owner
			From:              accountFromRow(r.Account),      // from_acct
			To:                accountFromRow(r.Account_2),    // to_acct
			Status:            r.Handoff.Status,
			CreatedAt:         createdAt,
			KeepableDelegates: keepable,
		})
	}
	return out, nil
}

// OwnershipHistory returns every ownership change of a Goal, oldest first: each
// Handoff with its outcome, and each Admin Reassign. The Goal itself is left
// unresolved on each entry.
func (s *Service) OwnershipHistory(ctx context.Context, goalID int64) ([]Handoff, error) {
	rows, err := s.queries.ListHandoffsForGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list ownership history: %w", err)
	}
	out := make([]Handoff, 0, len(rows))
	for _, r := range rows {
		createdAt, _ := time.Parse(timeFormat, r.Handoff.CreatedAt)
		out = append(out, Handoff{
			ID:          r.Handoff.ID,
			From:        accountFromRow(r.Account),   // from_acct
			To:          accountFromRow(r.Account_2), // to_acct
			InitiatedBy: accountFromRow(r.Account_3), // initiator
			Status:      r.Handoff.Status,
			CreatedAt:   createdAt,
		})
	}
	return out, nil
}

// ReassignGoal moves an Ownerless Goal to a present Owner (CONTEXT.md: an Admin
// reassigns an Ownerless Goal). Only an Admin may do this, and only for a Goal
// that is Ownerless — a Goal with a present Owner changes hands through a Handoff
// the new Owner accepts, not by fiat. The Reassign is kept in the Goal's
// ownership history with the outcome reassigned. A Handoff still pending on the
// Goal, one its Owner started before leaving, is cancelled in the same
// transaction: whichever of the two comes first replaces the Ownerless Owner.
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
	err = s.WithinTx(ctx, func(tx *Service) error {
		pending, err := tx.queries.GetPendingHandoffForGoal(ctx, goalID)
		switch {
		case err == nil:
			if err := tx.queries.SetHandoffStatus(ctx, db.SetHandoffStatusParams{
				Status: HandoffCancelled,
				ID:     pending.ID,
			}); err != nil {
				return fmt.Errorf("cancel pending handoff: %w", err)
			}
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("look up pending handoff: %w", err)
		}
		if err := tx.queries.SetGoalOwner(ctx, db.SetGoalOwnerParams{
			OwnerID: newOwnerID,
			ID:      goalID,
		}); err != nil {
			return fmt.Errorf("reassign goal: %w", err)
		}
		if _, err := tx.queries.CreateHandoff(ctx, db.CreateHandoffParams{
			GoalID:      goalID,
			FromOwner:   goal.Goal.OwnerID,
			ToOwner:     newOwnerID,
			Status:      HandoffReassigned,
			InitiatedBy: actorID,
			CreatedAt:   tx.clock.Now().Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("record reassign: %w", err)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
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

// loadHandoff loads a Handoff with its Goal, both Owners, and its initiator
// resolved.
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
	initiator, err := s.queries.GetAccount(ctx, row.InitiatedBy)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up initiator: %w", err)
	}
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	return Handoff{
		ID:          row.ID,
		Goal:        goalFromRow(goal.Goal, goal.Account),
		From:        accountFromRow(from),
		To:          accountFromRow(to),
		InitiatedBy: accountFromRow(initiator),
		Status:      row.Status,
		CreatedAt:   createdAt,
	}, nil
}
