package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return fmt.Errorf("look up goal: %w", err)
	}
	if actorID != goal.Goal.OwnerID {
		return fmt.Errorf("%w: only the Owner may authorize a Delegate", ErrNotAuthorized)
	}
	if accountID == goal.Goal.OwnerID {
		return fmt.Errorf("%w: the Owner already writes this Goal's Check-ins", ErrValidation)
	}
	if _, err := s.queries.GetAccount(ctx, accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: account does not exist", ErrValidation)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	if _, err := s.queries.GetDelegate(ctx, db.GetDelegateParams{
		GoalID:    goalID,
		AccountID: accountID,
	}); err == nil {
		return fmt.Errorf("%w: already a Delegate", ErrValidation)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up delegate: %w", err)
	}

	if _, err := s.queries.AddDelegate(ctx, db.AddDelegateParams{
		GoalID:    goalID,
		AccountID: accountID,
		CreatedAt: s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("add delegate: %w", err)
	}
	return nil
}

// AddDelegateByEmail authorizes the Account with the given email as a Delegate on
// the Goal. It is the web-facing convenience over AddDelegate, since the tool
// identifies people by email (CONTEXT.md: development sign-in by email). An email
// with no account is rejected.
func (s *Service) AddDelegateByEmail(ctx context.Context, actorID, goalID int64, email string) error {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no account with email %q", ErrValidation, email)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	return s.AddDelegate(ctx, actorID, goalID, acc.ID)
}

// RemoveDelegate revokes accountID's authorization to write Check-ins on the
// Goal. Only the Goal's Owner may do so (CONTEXT.md: a person an Owner
// authorizes); actorID is the acting Account. Removing someone who is not a
// Delegate is a no-op.
func (s *Service) RemoveDelegate(ctx context.Context, actorID, goalID, accountID int64) error {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return fmt.Errorf("look up goal: %w", err)
	}
	if actorID != goal.Goal.OwnerID {
		return fmt.Errorf("%w: only the Owner may remove a Delegate", ErrNotAuthorized)
	}
	if err := s.queries.RemoveDelegate(ctx, db.RemoveDelegateParams{
		GoalID:    goalID,
		AccountID: accountID,
	}); err != nil {
		return fmt.Errorf("remove delegate: %w", err)
	}
	return nil
}

// RemoveDelegateByEmail revokes the Delegate with the given email on the Goal. It
// is the web-facing convenience over RemoveDelegate. An email with no account is
// rejected.
func (s *Service) RemoveDelegateByEmail(ctx context.Context, actorID, goalID int64, email string) error {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no account with email %q", ErrValidation, email)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	return s.RemoveDelegate(ctx, actorID, goalID, acc.ID)
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
