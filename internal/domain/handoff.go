package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
//
// Each name starts Handoff: the CHECK test on handoffs.status finds its values
// by that prefix (enumchecks_test.go), so a value named otherwise goes
// unchecked.
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
// Goal to its current Owner, handing off an Ownerless Goal (an Admin reassigns
// it instead), or starting a second Handoff while one is pending, is rejected.
func (s *Service) StartHandoff(ctx context.Context, in StartHandoffInput) (Handoff, error) {
	// Every check reads inside the transaction that writes the Handoff, so two
	// started together (a double-submit) can't both find none pending.
	var ho Handoff
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		ho, err = tx.startHandoff(ctx, in)
		return err
	})
	if err != nil {
		return Handoff{}, err
	}
	return ho, nil
}

// startHandoff is StartHandoff's checks and insert, run inside its transaction.
func (s *Service) startHandoff(ctx context.Context, in StartHandoffInput) (Handoff, error) {
	goal, err := s.ownershipGoal(ctx, in.ActorID, in.GoalID, handoffRefusal)
	if err != nil {
		return Handoff{}, err
	}
	actor, err := s.queries.GetAccount(ctx, in.ActorID)
	if err != nil {
		return Handoff{}, fmt.Errorf("look up actor: %w", err)
	}

	if in.ToOwnerID == goal.Owner.ID {
		return Handoff{}, Refuse(ErrValidation, "the Goal is already owned by that person")
	}
	newOwner, err := s.queries.GetAccount(ctx, in.ToOwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, Refuse(ErrValidation, "the new Owner does not exist")
		}
		return Handoff{}, fmt.Errorf("look up new owner: %w", err)
	}
	if newOwner.Departed != 0 {
		return Handoff{}, Refuse(ErrValidation, "the new Owner has left the company")
	}

	if _, err := s.queries.GetPendingHandoffForGoal(ctx, in.GoalID); err == nil {
		return Handoff{}, Refuse(ErrValidation, "a Handoff for this Goal is already pending")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Handoff{}, fmt.Errorf("look up pending handoff: %w", err)
	}

	now := s.clock.Now()
	row, err := s.queries.CreateHandoff(ctx, db.CreateHandoffParams{
		GoalID:      in.GoalID,
		FromOwner:   goal.Owner.ID,
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
		Goal:        goal,
		From:        goal.Owner,
		To:          accountFromRow(newOwner),
		InitiatedBy: accountFromRow(actor),
		Status:      row.Status,
		CreatedAt:   now,
	}, nil
}

// ownershipGoal loads goalID for a change of its Owner by actorID, as
// StartHandoff and ReassignGoal do inside their transaction: a missing Goal is
// ErrNotFound, and anyone rule refuses is refused.
func (s *Service) ownershipGoal(ctx context.Context, actorID, goalID int64, rule func(goalActor, Goal) error) (Goal, error) {
	return s.openGoalCommand(ctx, actorID, goalID, rule, nil)
}

// StartHandoffByEmail starts a Handoff to the Account with the given email. It is
// the web-facing convenience over StartHandoff, since the tool identifies people
// by email (CONTEXT.md: development sign-in by email). An email with no account
// is rejected, but only once the actor may start the Handoff, so someone who
// may not learns nothing of who has an Account.
func (s *Service) StartHandoffByEmail(ctx context.Context, goalID int64, toEmail string, actorID int64) (Handoff, error) {
	var ho Handoff
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.ownershipGoal(ctx, actorID, goalID, handoffRefusal); err != nil {
			return err
		}
		toOwnerID, err := tx.accountIDWithEmail(ctx, toEmail)
		if err != nil {
			return err
		}
		ho, err = tx.startHandoff(ctx, StartHandoffInput{GoalID: goalID, ToOwnerID: toOwnerID, ActorID: actorID})
		return err
	})
	if err != nil {
		return Handoff{}, err
	}
	return ho, nil
}

// ReassignGoalByEmail reassigns an Ownerless Goal to the Account with the given
// email. It is the web-facing convenience over ReassignGoal. An email with no
// account is rejected, but only once the actor may reassign the Goal.
func (s *Service) ReassignGoalByEmail(ctx context.Context, actorID, goalID int64, newOwnerEmail string) (Goal, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.ownershipGoal(ctx, actorID, goalID, reassignRefusal); err != nil {
			return err
		}
		newOwnerID, err := tx.accountIDWithEmail(ctx, newOwnerEmail)
		if err != nil {
			return err
		}
		return tx.reassignGoal(ctx, actorID, goalID, newOwnerID)
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// AcceptHandoff accepts a pending Handoff, moving the Goal to the new Owner. Only
// the proposed new Owner may accept it (CONTEXT.md: ownership changes are always
// accepted by the new Owner). The new Owner chooses which of the Goal's
// Delegates to keep (CONTEXT.md: Delegate): every Delegate not in keep is
// removed, except a Departed one, who was never offered and stays. The new Owner
// is never kept as their own Delegate, since the Owner already writes the Goal's
// Check-ins. A Handoff whose from-Owner no longer owns the Goal is refused. The
// checks, the ownership change, the removals and the outcome are one
// transaction, so a Handoff rejected meanwhile isn't also accepted.
func (s *Service) AcceptHandoff(ctx context.Context, handoffID, actorID int64, keep []int64) (Handoff, error) {
	err := s.WithinTx(ctx, func(tx *Service) error {
		row, err := tx.handoffToDecide(ctx, handoffID, actorID, "accept")
		if err != nil {
			return err
		}
		goal, err := tx.queries.GetGoal(ctx, row.GoalID)
		if err != nil {
			return fmt.Errorf("look up goal: %w", err)
		}
		if goal.Goal.OwnerID != row.FromOwner {
			return Refuse(ErrValidation, "the Goal has changed hands since this Handoff was started")
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
		return tx.decidePendingHandoff(ctx, handoffID, HandoffAccepted)
	})
	if err != nil {
		return Handoff{}, err
	}
	return s.loadHandoff(ctx, handoffID)
}

// RejectHandoff declines a pending Handoff, keeping it with the outcome
// rejected; ownership stays put. Only the proposed new Owner may reject it, and
// they may Undo it (RestoreHandoff) with the token it returns, for UndoWindow.
// The checks read inside the transaction that rejects it, so a Handoff
// accepted meanwhile isn't also rejected.
func (s *Service) RejectHandoff(ctx context.Context, handoffID, actorID int64) (string, error) {
	var token string
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.handoffToDecide(ctx, handoffID, actorID, "reject"); err != nil {
			return err
		}
		if err := tx.decidePendingHandoff(ctx, handoffID, HandoffRejected); err != nil {
			return err
		}
		var err error
		token, err = tx.issueUndo(ctx, undoHandoffRejection, handoffID, actorID, "")
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// RestoreHandoff undoes rejecting a Handoff, putting it back as pending with
// the same proposed Owner, so the Goal's ownership history shows it pending
// rather than a rejection followed by something else. Only the person who
// rejected it, its proposed new Owner, may, with the token RejectHandoff gave
// them, within UndoWindow, and only while it is rejected: once undone it is
// pending, so a second Undo is refused. Presenting the token spends it, even
// when the Undo is then refused. It is refused, changing nothing, when the
// Goal has another pending Handoff, its Owner has changed, any later Handoff
// of the Goal exists, or the proposed Owner has since Departed. Rejecting
// tells no one, so neither does the Undo.
func (s *Service) RestoreHandoff(ctx context.Context, handoffID, actorID int64, token string) (Handoff, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Handoff{}, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	if err := handoffRejectionUndoRefusal(row, actorID); err != nil {
		return Handoff{}, err
	}
	if _, err := s.spendUndo(ctx, token, undoHandoffRejection, handoffID, actorID); err != nil {
		return Handoff{}, err
	}
	err = s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.queries.GetPendingHandoffForGoal(ctx, row.GoalID); err == nil {
			return Refuse(ErrValidation, "another Handoff of this Goal is pending")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up pending handoff: %w", err)
		}
		goal, err := tx.queries.GetGoal(ctx, row.GoalID)
		if err != nil {
			return fmt.Errorf("look up goal: %w", err)
		}
		if goal.Goal.OwnerID != row.FromOwner {
			return Refuse(ErrValidation, "the Goal has changed hands since this Handoff was rejected")
		}
		later, err := tx.queries.CountLaterHandoffs(ctx, db.CountLaterHandoffsParams{GoalID: row.GoalID, ID: handoffID})
		if err != nil {
			return fmt.Errorf("look up later handoffs: %w", err)
		}
		if later > 0 {
			return Refuse(ErrValidation, "the Goal has been handed off again since this Handoff was rejected")
		}
		to, err := tx.queries.GetAccount(ctx, row.ToOwner)
		if err != nil {
			return fmt.Errorf("look up new owner: %w", err)
		}
		if to.Departed != 0 {
			return Refuse(ErrValidation, "the new Owner has left the company")
		}
		n, err := tx.queries.ReopenRejectedHandoff(ctx, handoffID)
		if err != nil {
			return fmt.Errorf("restore handoff: %w", err)
		}
		if n == 0 {
			return errHandoffNotRejected
		}
		return nil
	})
	if err != nil {
		return Handoff{}, err
	}
	return s.loadHandoff(ctx, handoffID)
}

// errHandoffNotRejected refuses to undo a Handoff that isn't rejected.
var errHandoffNotRejected = Refuse(ErrValidation, "this Handoff isn't rejected, so there is nothing to undo")

// handoffRejectionUndoRefusal is the rule for who may still undo rejecting
// handoff, which RestoreHandoff and CanUndoHandoffRejection both ask: actorID
// is its proposed new Owner, who rejected it, and it is still rejected.
func handoffRejectionUndoRefusal(handoff db.Handoff, actorID int64) error {
	if handoff.ToOwner != actorID {
		return Refuse(ErrNotAuthorized, "only the person who rejected a Handoff may undo it")
	}
	if handoff.Status != HandoffRejected {
		return errHandoffNotRejected
	}
	return nil
}

// CanUndoHandoffRejection says whether actorID may still undo rejecting
// Handoff handoffID with token, by RestoreHandoff's own rule: for the viewer,
// the token and the Handoff itself. It doesn't answer for what has happened
// since to the Goal, its other Handoffs or its new Owner, which RestoreHandoff
// checks only when the Undo is posted. A Handoff that doesn't exist is
// ErrNotFound.
func (s *Service) CanUndoHandoffRejection(ctx context.Context, actorID, handoffID int64, token string) (bool, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return false, fmt.Errorf("look up handoff: %w", err)
	}
	refusal := handoffRejectionUndoRefusal(row, actorID)
	if refusal == nil {
		_, refusal = s.peekUndo(ctx, token, undoHandoffRejection, handoffID, actorID)
	}
	return goalPagePermits(refusal)
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
	// Every check reads inside the transaction that writes the Reassign, so a
	// Handoff accepted meanwhile, or a new Owner marked Departed meanwhile,
	// isn't overwritten by it.
	err := s.WithinTx(ctx, func(tx *Service) error {
		return tx.reassignGoal(ctx, actorID, goalID, newOwnerID)
	})
	if err != nil {
		return Goal{}, err
	}
	return s.loadGoal(ctx, goalID)
}

// reassignGoal is ReassignGoal's checks and writes, run inside its transaction.
func (s *Service) reassignGoal(ctx context.Context, actorID, goalID, newOwnerID int64) error {
	goal, err := s.ownershipGoal(ctx, actorID, goalID, reassignRefusal)
	if err != nil {
		return err
	}
	newOwner, err := s.queries.GetAccount(ctx, newOwnerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Refuse(ErrValidation, "the new Owner does not exist")
		}
		return fmt.Errorf("look up new owner: %w", err)
	}
	if newOwner.Departed != 0 {
		return Refuse(ErrValidation, "the new Owner has left the company")
	}
	pending, err := s.queries.GetPendingHandoffForGoal(ctx, goalID)
	switch {
	case err == nil:
		if err := s.decidePendingHandoff(ctx, pending.ID, HandoffCancelled); err != nil {
			return err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("look up pending handoff: %w", err)
	}
	if err := s.queries.SetGoalOwner(ctx, db.SetGoalOwnerParams{
		OwnerID: newOwnerID,
		ID:      goalID,
	}); err != nil {
		return fmt.Errorf("reassign goal: %w", err)
	}
	if _, err := s.queries.CreateHandoff(ctx, db.CreateHandoffParams{
		GoalID:      goalID,
		FromOwner:   goal.Owner.ID,
		ToOwner:     newOwnerID,
		Status:      HandoffReassigned,
		InitiatedBy: actorID,
		CreatedAt:   s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("record reassign: %w", err)
	}
	return nil
}

// handoffToDecide loads a Handoff for its new Owner to accept or reject
// (decision): it must exist (else Not found), the actor must be its new Owner
// (else not authorized), and it must still be pending. One already decided is
// refused saying how it was decided, so a second click or an old tab learns
// why.
func (s *Service) handoffToDecide(ctx context.Context, handoffID, actorID int64, decision string) (db.Handoff, error) {
	row, err := s.queries.GetHandoff(ctx, handoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Handoff{}, fmt.Errorf("%w: handoff %d", ErrNotFound, handoffID)
		}
		return db.Handoff{}, fmt.Errorf("look up handoff: %w", err)
	}
	if row.ToOwner != actorID {
		return db.Handoff{}, Refuse(ErrNotAuthorized, "only the new Owner may %s a Handoff", decision)
	}
	switch row.Status {
	case HandoffPending:
		return row, nil
	case HandoffAccepted:
		return db.Handoff{}, Refuse(ErrValidation, "this Handoff was already accepted")
	case HandoffRejected:
		return db.Handoff{}, Refuse(ErrValidation, "this Handoff was already rejected")
	case HandoffCancelled:
		return db.Handoff{}, Refuse(ErrValidation, "this Handoff was cancelled")
	case HandoffReassigned:
		return db.Handoff{}, Refuse(ErrValidation, "this Goal was already reassigned by an Admin")
	default:
		return db.Handoff{}, fmt.Errorf("handoff %d has unknown status %q", handoffID, row.Status)
	}
}

// decidePendingHandoff records a Handoff's outcome, only while it is still
// pending. Each caller has read it pending in the same transaction, so the
// n == 0 branch is a guard that shouldn't fire; if it does, the error rolls
// the caller's transaction back, keeping nothing else it wrote.
func (s *Service) decidePendingHandoff(ctx context.Context, handoffID int64, status string) error {
	n, err := s.queries.SetHandoffStatus(ctx, db.SetHandoffStatusParams{
		Status: status,
		ID:     handoffID,
	})
	if err != nil {
		return fmt.Errorf("set handoff %s: %w", status, err)
	}
	if n == 0 {
		return fmt.Errorf("set handoff %d %s: it is no longer pending", handoffID, status)
	}
	return nil
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
