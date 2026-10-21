package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// A Report Delegate is a person a Report Definition's creator or an Admin
// authorizes to change it, curate its narrative and publish it: everything the
// creator may do except add or remove Report Delegates (CONTEXT.md: Report
// Delegate). Like a Goal's Delegates, they are surfaced as plain Accounts.

// CanManageReport reports whether actor may change def, curate its narrative
// and publish it: its creator, one of its Report Delegates (delegates, as
// ListReportDelegates returns them) or an Admin may (CONTEXT.md: Report
// Definition).
func CanManageReport(actor Account, def ReportDefinition, delegates []Account) bool {
	return CanManageReportDelegates(actor, def) ||
		slices.ContainsFunc(delegates, func(d Account) bool { return d.ID == actor.ID })
}

// CanManageReportDelegates reports whether actor may add and remove def's
// Report Delegates: only its creator or an Admin may.
func CanManageReportDelegates(actor Account, def ReportDefinition) bool {
	return actor.IsAdmin || actor.ID == def.CreatedBy
}

// requireReportManager returns ErrNotAuthorized, naming what actorID tried, unless
// they may change def, curate its narrative and publish it. An unknown actor
// may do none of it.
func (s *Service) requireReportManager(ctx context.Context, actorID int64, def ReportDefinition, doing string) error {
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up actor: %w", err)
	}
	delegates, lerr := s.ListReportDelegates(ctx, def.ID)
	if lerr != nil {
		return lerr
	}
	if err != nil || !CanManageReport(accountFromRow(actor), def, delegates) {
		return fmt.Errorf("%w: only its creator, a Report Delegate or an Admin may %s %s", ErrNotAuthorized, doing, def.Name)
	}
	return nil
}

// reportDelegatesDefinition returns the Report Definition defID if actorID may
// add and remove its Report Delegates: its creator or an Admin.
func (s *Service) reportDelegatesDefinition(ctx context.Context, actorID, defID int64) (ReportDefinition, error) {
	def, err := s.GetReportDefinition(ctx, defID)
	if err != nil {
		return ReportDefinition{}, err
	}
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReportDefinition{}, fmt.Errorf("look up actor: %w", err)
	}
	if err != nil || !CanManageReportDelegates(accountFromRow(actor), def) {
		return ReportDefinition{}, fmt.Errorf("%w: only its creator or an Admin may change who are Report Delegates on %s", ErrNotAuthorized, def.Name)
	}
	return def, nil
}

// AddReportDelegate authorizes accountID to change, curate and publish the
// Report Definition defID. Only its creator or an Admin may (ErrNotAuthorized);
// actorID is the acting Account. The creator is already authorized so cannot
// be added, and neither can someone already a Report Delegate, a Departed
// person or an Account that doesn't exist (ErrValidation).
func (s *Service) AddReportDelegate(ctx context.Context, actorID, defID, accountID int64) error {
	def, err := s.reportDelegatesDefinition(ctx, actorID, defID)
	if err != nil {
		return err
	}
	if accountID == def.CreatedBy {
		return fmt.Errorf("%w: the creator already manages %s", ErrValidation, def.Name)
	}
	acc, err := s.queries.GetAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: account does not exist", ErrValidation)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	if acc.Departed != 0 {
		return fmt.Errorf("%w: %s has left the company", ErrValidation, acc.Email)
	}
	delegates, err := s.ListReportDelegates(ctx, defID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(delegates, func(d Account) bool { return d.ID == accountID }) {
		return fmt.Errorf("%w: %s is already a Report Delegate", ErrValidation, acc.Email)
	}
	if err := s.queries.AddReportDelegate(ctx, db.AddReportDelegateParams{
		ReportDefinitionID: defID,
		AccountID:          accountID,
		CreatedAt:          s.clock.Now().Format(timeFormat),
	}); err != nil {
		return fmt.Errorf("add report delegate: %w", err)
	}
	return nil
}

// AddReportDelegateByEmail authorizes the Account with the given email as a
// Report Delegate on the Report Definition defID. It is the web-facing
// convenience over AddReportDelegate; an email with no account is rejected.
func (s *Service) AddReportDelegateByEmail(ctx context.Context, actorID, defID int64, email string) error {
	acc, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: no account with email %q", ErrValidation, email)
		}
		return fmt.Errorf("look up account: %w", err)
	}
	return s.AddReportDelegate(ctx, actorID, defID, acc.ID)
}

// RemoveReportDelegate revokes accountID's authorization on the Report
// Definition defID. Only its creator or an Admin may (ErrNotAuthorized).
// Removing someone who is not a Report Delegate is a no-op.
func (s *Service) RemoveReportDelegate(ctx context.Context, actorID, defID, accountID int64) error {
	if _, err := s.reportDelegatesDefinition(ctx, actorID, defID); err != nil {
		return err
	}
	if err := s.queries.RemoveReportDelegate(ctx, db.RemoveReportDelegateParams{
		ReportDefinitionID: defID,
		AccountID:          accountID,
	}); err != nil {
		return fmt.Errorf("remove report delegate: %w", err)
	}
	return nil
}

// ListReportDelegates returns the Report Definition defID's Report Delegates,
// ordered by email.
func (s *Service) ListReportDelegates(ctx context.Context, defID int64) ([]Account, error) {
	rows, err := s.queries.ListReportDelegates(ctx, defID)
	if err != nil {
		return nil, fmt.Errorf("list report delegates: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r.Account))
	}
	return out, nil
}
