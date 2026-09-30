package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

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
