package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Account is a person who can sign in and own Goals.
type Account struct {
	ID      int64
	Email   string
	IsAdmin bool
	// Departed is set once an Admin records that the person has left the org; the
	// Goals they still own are then Ownerless (CONTEXT.md: Ownerless).
	Departed bool
}

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// timeFormat is how timestamps are stored in SQLite TEXT columns.
const timeFormat = time.RFC3339Nano

func accountFromRow(a db.Account) Account {
	return Account{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin != 0, Departed: a.Departed != 0}
}

// Account returns the Account with the given id, resolving the current session.
// It returns ErrNotFound if no such Account exists.
func (s *Service) Account(ctx context.Context, id int64) (Account, error) {
	a, err := s.queries.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, fmt.Errorf("%w: account %d", ErrNotFound, id)
		}
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return accountFromRow(a), nil
}

// MarkDeparted records that the person behind accountID has left the org, so the
// Goals they still own become Ownerless (CONTEXT.md: Ownerless). Only an Admin
// may do this; actorID identifies the acting Account.
func (s *Service) MarkDeparted(ctx context.Context, actorID, accountID int64) error {
	if err := s.requireAdmin(ctx, actorID); err != nil {
		return err
	}
	if _, err := s.queries.GetAccount(ctx, accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: account does not exist", ErrValidation)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	if err := s.queries.SetAccountDeparted(ctx, db.SetAccountDepartedParams{
		Departed: 1,
		ID:       accountID,
	}); err != nil {
		return fmt.Errorf("mark departed: %w", err)
	}
	return nil
}

// requireAdmin returns ErrNotAuthorized unless actorID is an Admin.
func (s *Service) requireAdmin(ctx context.Context, actorID int64) error {
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: not an Admin", ErrNotAuthorized)
		}
		return fmt.Errorf("look up actor: %w", err)
	}
	if actor.IsAdmin == 0 {
		return fmt.Errorf("%w: only an Admin may do this", ErrNotAuthorized)
	}
	return nil
}

// SignIn resolves the development sign-in for emailAddr: it returns the existing
// Account, or creates one on first sign-in with the Admin flag set from config.
func (s *Service) SignIn(ctx context.Context, emailAddr string) (Account, error) {
	existing, err := s.queries.GetAccountByEmail(ctx, emailAddr)
	if err == nil {
		return accountFromRow(existing), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("look up account: %w", err)
	}

	isAdmin := int64(0)
	if s.admins[emailAddr] {
		isAdmin = 1
	}
	created, err := s.queries.CreateAccount(ctx, db.CreateAccountParams{
		Email:     emailAddr,
		IsAdmin:   isAdmin,
		CreatedAt: s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return Account{}, fmt.Errorf("create account: %w", err)
	}
	return accountFromRow(created), nil
}
