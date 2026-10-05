package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Account is a person who can sign in and own Goals.
type Account struct {
	ID    int64
	Email string
	// Name is what the tool calls the person, from the org's sign-in; empty
	// until they have one (CONTEXT.md: Name). Show a person with Label, not
	// Name or Email.
	Name    string
	IsAdmin bool
	// Departed is set once an Admin records that the person has left the org; the
	// Goals they still own are then Ownerless (CONTEXT.md: Ownerless).
	Departed bool
	// ManagerID is the Account of this person's Manager, as the org's
	// directory records it; nil for someone it gives none (CONTEXT.md:
	// Manager). Only the directory sync sets it (ADR 0008).
	ManagerID *int64
}

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrDeparted is returned when a Departed person tries to sign in: they have
// left the org and can't sign in or act (CONTEXT.md: Departed).
var ErrDeparted = errors.New("account has departed")

// timeFormat is how timestamps are stored in SQLite TEXT columns.
const timeFormat = time.RFC3339Nano

func accountFromRow(a db.Account) Account {
	acc := Account{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin != 0, Departed: a.Departed != 0, ManagerID: a.ManagerID}
	if a.Name != nil {
		acc.Name = *a.Name
	}
	return acc
}

// Label is how the tool shows the person: their Name, or the part of their
// email before the @ until they have one (CONTEXT.md: Name). It is the one
// display rule every page, export, and email uses; a page puts the email on
// hover.
func (a Account) Label() string {
	if a.Name != "" {
		return a.Name
	}
	local, _, _ := strings.Cut(a.Email, "@")
	return local
}

// LongLabel is the person as introduced where there is no hover — an export or
// an email body: their Label with their email, "Ada Okafor
// (ada.okafor@example.com)". A person a snapshot from before Names shows by
// email (see Account.UnmarshalJSON) reads as the email alone.
func (a Account) LongLabel() string {
	if label := a.Label(); label != a.Email {
		return label + " (" + a.Email + ")"
	}
	return a.Email
}

// UnmarshalJSON reads an Account frozen in a Publication's snapshot, which
// records each person's Name as it was when published. A snapshot from before
// Accounts had Names has no Name at all; its people are shown by email, as they
// were then.
func (a *Account) UnmarshalJSON(data []byte) error {
	type frozen Account // without this method, so decoding doesn't recurse
	var f struct {
		frozen
		Name *string
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	*a = Account(f.frozen)
	a.Name = a.Email
	if f.Name != nil {
		a.Name = *f.Name
	}
	return nil
}

// Mentions shows people in a document without hover: the first mention of each
// person reads as their LongLabel, every later one as their Label, so duplicate
// Names are told apart once and then read short. The zero value is ready to
// use; use one per document.
type Mentions struct {
	seen map[string]bool
}

// Of returns how the document mentions a here: introduced the first time, by
// Label after that.
func (m *Mentions) Of(a Account) string {
	if m.seen[a.Email] {
		return a.Label()
	}
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	m.seen[a.Email] = true
	return a.LongLabel()
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

// AccountByEmail returns the Account with the given email, whatever its case
// or surrounding spaces (CONTEXT.md: Account), without creating one. It returns
// ErrNotFound if there is none.
func (s *Service) AccountByEmail(ctx context.Context, emailAddr string) (Account, error) {
	a, err := s.queries.GetAccountByEmail(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, fmt.Errorf("%w: account %s", ErrNotFound, emailAddr)
		}
		return Account{}, fmt.Errorf("get account: %w", err)
	}
	return accountFromRow(a), nil
}

// SetName records the Name the org's sign-in supplies for the person behind
// accountID (CONTEXT.md: Name); a blank name clears it. Nothing in the tool lets
// anyone type a Name: this is the seam a sign-in integration, or the seed,
// supplies it through.
func (s *Service) SetName(ctx context.Context, accountID int64, name string) error {
	var stored *string
	if name = strings.TrimSpace(name); name != "" {
		stored = &name
	}
	if err := s.queries.SetAccountName(ctx, db.SetAccountNameParams{Name: stored, ID: accountID}); err != nil {
		return fmt.Errorf("set name: %w", err)
	}
	return nil
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

// DepartedAccounts lists every Departed person, whether or not they still own
// a Goal, in the order they are shown: by Label, ignoring case, then by email.
// It is where an Admin finds someone to mark returned who has no Goal page to
// do it from.
func (s *Service) DepartedAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.queries.ListDepartedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list departed accounts: %w", err)
	}
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r))
	}
	slices.SortStableFunc(out, func(a, b Account) int {
		return strings.Compare(strings.ToLower(a.Label()), strings.ToLower(b.Label()))
	})
	return out, nil
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

// adminKey folds an email the way the accounts queries store and look it up —
// trimmed and lowercased — so the configured Admins match an email whatever its
// case (CONTEXT.md: Account).
func adminKey(emailAddr string) string {
	return strings.ToLower(strings.TrimSpace(emailAddr))
}

// EnsureAccount returns the Account with the given email, whatever its case,
// creating one if none exists yet (stored trimmed and lowercased, with the Admin
// flag set from config). It is the get-or-create the development sign-in
// performs, exposed on its own so the spreadsheet import can give an account to
// every person it names (ticket #22: people named in the file get accounts).
func (s *Service) EnsureAccount(ctx context.Context, emailAddr string) (Account, error) {
	existing, err := s.queries.GetAccountByEmail(ctx, emailAddr)
	if err == nil {
		return accountFromRow(existing), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("look up account: %w", err)
	}

	isAdmin := int64(0)
	if s.admins[adminKey(emailAddr)] {
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
