package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// A Delegate is a person an Owner authorizes to write and submit Check-ins for a
// Goal. Accountability stays with the Owner, and each Check-in still records who
// wrote it (CONTEXT.md: Delegate). Delegates are the Accounts so authorized on a
// Goal; they are surfaced as plain Accounts since a Delegate carries no state of
// its own beyond the authorization.

// AddDelegate authorizes accountID to write Check-ins on the Goal. Only the
// Goal's Owner may authorize a Delegate (CONTEXT.md: a person an Owner
// authorizes); actorID is the acting Account. The Goal and the account must
// exist, the Owner is already authorized so cannot be added, and authorizing the
// same Delegate twice is rejected.
func (s *Service) AddDelegate(ctx context.Context, actorID, goalID, accountID int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		goal, err := tx.delegatesGoal(ctx, actorID, goalID, actAddDelegate)
		if err != nil {
			return err
		}
		if accountID == goal.Owner.ID {
			return Refuse(ErrValidation, "the Owner already writes this Goal's Check-ins")
		}
		if _, err := tx.queries.GetAccount(ctx, accountID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "account does not exist")
			}
			return fmt.Errorf("look up account: %w", err)
		}
		if _, err := tx.queries.GetDelegate(ctx, db.GetDelegateParams{
			GoalID:    goalID,
			AccountID: accountID,
		}); err == nil {
			return Refuse(ErrValidation, "already a Delegate")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up delegate: %w", err)
		}

		if _, err := tx.queries.AddDelegate(ctx, db.AddDelegateParams{
			GoalID:    goalID,
			AccountID: accountID,
			CreatedAt: tx.clock.Now().Format(timeFormat),
		}); err != nil {
			return fmt.Errorf("add delegate: %w", err)
		}
		return nil
	})
}

// AddDelegateByEmail authorizes the Account with the given email as a Delegate on
// the Goal. It is the web-facing convenience over AddDelegate, since the tool
// identifies people by email (CONTEXT.md: development sign-in by email). An email
// with no account is rejected, but only once the actor may authorize a
// Delegate, so someone who may not learns nothing of who has an Account.
func (s *Service) AddDelegateByEmail(ctx context.Context, actorID, goalID int64, email string) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.delegatesGoal(ctx, actorID, goalID, actAddDelegate); err != nil {
			return err
		}
		accountID, err := tx.accountIDWithEmail(ctx, email)
		if err != nil {
			return err
		}
		return tx.AddDelegate(ctx, actorID, goalID, accountID)
	})
}

// The acts goalOwnerRefusal names when it refuses a change to a Goal's
// Delegates.
const (
	actAddDelegate    = "authorize a Delegate"
	actRemoveDelegate = "remove a Delegate"
)

// delegatesGoal loads goalID for a change to its Delegates by actorID, as
// AddDelegate and RemoveDelegate do inside their transaction: a missing Goal
// is ErrNotFound, and anyone goalOwnerRefusal refuses act is refused.
func (s *Service) delegatesGoal(ctx context.Context, actorID, goalID int64, act string) (Goal, error) {
	return s.openGoalCommand(ctx, actorID, goalID, func(a goalActor, g Goal) error {
		return goalOwnerRefusal(a, g, act)
	}, nil)
}

// RemoveDelegate revokes accountID's authorization to write Check-ins on the
// Goal. Only the Goal's Owner may do so (CONTEXT.md: a person an Owner
// authorizes); actorID is the acting Account. Removing someone who is not a
// Delegate is a no-op.
func (s *Service) RemoveDelegate(ctx context.Context, actorID, goalID, accountID int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.delegatesGoal(ctx, actorID, goalID, actRemoveDelegate); err != nil {
			return err
		}
		if err := tx.queries.RemoveDelegate(ctx, db.RemoveDelegateParams{
			GoalID:    goalID,
			AccountID: accountID,
		}); err != nil {
			return fmt.Errorf("remove delegate: %w", err)
		}
		return nil
	})
}

// RemoveDelegateByEmail revokes the Delegate with the given email on the Goal. It
// is the web-facing convenience over RemoveDelegate. An email with no account is
// rejected, but only once the actor may remove a Delegate.
func (s *Service) RemoveDelegateByEmail(ctx context.Context, actorID, goalID int64, email string) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.delegatesGoal(ctx, actorID, goalID, actRemoveDelegate); err != nil {
			return err
		}
		accountID, err := tx.accountIDWithEmail(ctx, email)
		if err != nil {
			return err
		}
		return tx.RemoveDelegate(ctx, actorID, goalID, accountID)
	})
}

// ListDelegates returns the Accounts an Owner has authorized to write Check-ins
// on the Goal, ordered by email.
func (s *Service) ListDelegates(ctx context.Context, goalID int64) ([]Account, error) {
	rows, err := s.queries.ListDelegates(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list delegates: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r.Account))
	}
	return out, nil
}

// ListDelegatesForGoals returns the Delegates of each of the Goals goalIDs,
// keyed by Goal ID and each Goal's ordered by email, as ListDelegates reads
// one Goal's. A Goal with no Delegates has no entry. It runs one query however
// many Goals there are.
func (s *Service) ListDelegatesForGoals(ctx context.Context, goalIDs []int64) (map[int64][]Account, error) {
	out := map[int64][]Account{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListDelegatesForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list delegates: %w", err)
	}
	for _, r := range rows {
		out[r.GoalID] = append(out[r.GoalID], accountFromRow(r.Account))
	}
	return out, nil
}

// DelegatedGoals returns every Goal the Account is a Delegate for, each with its
// Owner resolved — the Delegate's worklist of Goals they can check in on
// (CONTEXT.md: Delegate).
func (s *Service) DelegatedGoals(ctx context.Context, accountID int64) ([]Goal, error) {
	rows, err := s.queries.ListDelegatedGoals(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("list delegated goals: %w", err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	return goals, nil
}

// isDelegate reports whether accountID is a Delegate on the Goal — authorized by
// the Owner to write Check-ins.
func (s *Service) isDelegate(ctx context.Context, goalID, accountID int64) (bool, error) {
	if _, err := s.queries.GetDelegate(ctx, db.GetDelegateParams{
		GoalID:    goalID,
		AccountID: accountID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("look up delegate: %w", err)
	}
	return true, nil
}
