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

// ErrDeparted is returned when a Departed person tries to sign in: they have
// left the org and can't sign in or act (CONTEXT.md: Departed).
var ErrDeparted = errors.New("account has departed")

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
// Goals they still own become Ownerless (CONTEXT.md: Ownerless), and cancels
// every pending Handoff to them in the same transaction. Pending Handoffs they
// started as Owner stay pending, so the new Owner can still accept them. Only an
// Admin may do this; actorID identifies the acting Account.
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
	return s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.SetAccountDeparted(ctx, db.SetAccountDepartedParams{
			Departed: 1,
			ID:       accountID,
		}); err != nil {
			return fmt.Errorf("mark departed: %w", err)
		}
		if err := tx.queries.CancelPendingHandoffsTo(ctx, accountID); err != nil {
			return fmt.Errorf("cancel handoffs: %w", err)
		}
		return nil
	})
}

// MarkReturned reverses a departure: the person behind accountID can sign in and
// act again, the Goals they still own stop being Ownerless, and their Delegate
// rights come back as they were. Nothing else is undone — a Goal reassigned
// while they were away stays with its new Owner, and a cancelled Handoff stays
// cancelled (CONTEXT.md: Departed). Only an Admin may do this; actorID
// identifies the acting Account.
func (s *Service) MarkReturned(ctx context.Context, actorID, accountID int64) error {
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
		Departed: 0,
		ID:       accountID,
	}); err != nil {
		return fmt.Errorf("mark returned: %w", err)
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
// A Departed Account is refused with ErrDeparted (CONTEXT.md: Departed).
func (s *Service) SignIn(ctx context.Context, emailAddr string) (Account, error) {
	acc, err := s.EnsureAccount(ctx, emailAddr)
	if err != nil {
		return Account{}, err
	}
	if acc.Departed {
		return Account{}, fmt.Errorf("%w: %s has left the org", ErrDeparted, emailAddr)
	}
	return acc, nil
}

// EnsureAccount returns the Account with the given email, creating one if none
// exists yet (with the Admin flag set from config). It is the get-or-create the
// development sign-in performs, exposed on its own so the spreadsheet import can
// give an account to every person it names (ticket #22: people named in the file
// get accounts).
func (s *Service) EnsureAccount(ctx context.Context, emailAddr string) (Account, error) {
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
