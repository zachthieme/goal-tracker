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
}

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// timeFormat is how timestamps are stored in SQLite TEXT columns.
const timeFormat = time.RFC3339Nano

func accountFromRow(a db.Account) Account {
	return Account{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin != 0}
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
