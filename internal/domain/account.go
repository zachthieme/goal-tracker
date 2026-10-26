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
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Account is a person who can sign in and own Goals.
type Account struct {
	ID    int64
	Email string
	// Name is what the tool calls the person, from the company's sign-in; empty
	// until they have one (CONTEXT.md: Name). Show a person with Label, not
	// Name or Email.
	Name    string
	IsAdmin bool
	// Departed is set once an Admin records that the person has left the company; the
	// Goals they still own are then Ownerless (CONTEXT.md: Ownerless).
	Departed bool
	// ManagerID is the Account of this person's Manager, as the company's
	// directory records it; nil for someone it gives none (CONTEXT.md:
	// Manager). Only the directory sync sets it (ADR 0008).
	ManagerID *int64
	// Timezone is the IANA name of where the person works, as the company's
	// directory records it; empty for none, when they use the company's
	// (CONTEXT.md: Timezone). Only the directory sync sets it, and only to a
	// name loadTimezone accepts.
	Timezone string
}

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrDeparted is returned when a Departed person tries to sign in: they have
// left the company and can't sign in or act (CONTEXT.md: Departed).
var ErrDeparted = errors.New("account has departed")

// OIDCSubjectConflictError is returned when an OIDC sign-in's email names an
// Account already linked to another subject: the provider has likely given the
// email to someone new, who must not sign in as the person it once named
// (ticket #426). AccountID is the Account the email names.
type OIDCSubjectConflictError struct {
	AccountID int64
}

func (e *OIDCSubjectConflictError) Error() string {
	return fmt.Sprintf("account %d is linked to another OIDC subject", e.AccountID)
}

// timeFormat is how timestamps are stored in SQLite TEXT columns.
const timeFormat = time.RFC3339Nano

func accountFromRow(a db.Account) Account {
	acc := Account{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin != 0, Departed: a.Departed != 0, ManagerID: a.ManagerID, Timezone: a.Timezone}
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

// AccountsByIDs returns the Accounts ids name, as Account reads one, keyed by
// Account ID; an ID naming no one has no entry. It runs one query however many
// IDs there are, and none for no IDs.
func (s *Service) AccountsByIDs(ctx context.Context, ids []int64) (map[int64]Account, error) {
	out := map[int64]Account{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListAccountsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = accountFromRow(r)
	}
	return out, nil
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

// accountIDWithEmail is the ID of the Account with emailAddr, as
// AccountByEmail finds it, for a by-email command once it has authorized its
// actor: an email with no Account is ErrValidation, naming it.
func (s *Service) accountIDWithEmail(ctx context.Context, emailAddr string) (int64, error) {
	a, err := s.queries.GetAccountByEmail(ctx, strings.TrimSpace(emailAddr))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, Refuse(ErrValidation, "no account with email %q", emailAddr)
		}
		return 0, fmt.Errorf("look up account: %w", err)
	}
	return a.ID, nil
}

// SetName records the Name the company's sign-in supplies for the person behind
// accountID (CONTEXT.md: Name); a blank name clears it. Nothing in the tool lets
// anyone type a Name: this is the seam a sign-in integration, or the seed,
// supplies it through.
func (s *Service) SetName(ctx context.Context, accountID int64, name string) error {
	var stored *string
	if name = strings.TrimSpace(name); name != "" {
		stored = &name
	}
	return s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.queries.SetAccountName(ctx, db.SetAccountNameParams{Name: stored, ID: accountID}); err != nil {
			return fmt.Errorf("set name: %w", err)
		}
		return nil
	})
}

// MarkDeparted records that the person behind accountID has left the company, so the
// Goals they still own become Ownerless (CONTEXT.md: Ownerless), and, in the
// same transaction, cancels every pending Handoff to them and ends every session
// they have (EndSessions), so marking them returned later doesn't sign a browser
// from before the departure back in. Pending Handoffs they started as Owner stay
// pending, so the new Owner can still accept them. Only an Admin may do this,
// and only of someone who hasn't Departed already (markDepartedRefusal);
// actorID identifies the acting Account. An accountID naming no Account is
// refused first, whoever asks.
func (s *Service) MarkDeparted(ctx context.Context, actorID, accountID int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		person, err := tx.queries.GetAccount(ctx, accountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "account does not exist")
			}
			return fmt.Errorf("look up account: %w", err)
		}
		if err := markDepartedRefusal(tx.accountActor(ctx, actorID), accountFromRow(person)); err != nil {
			return err
		}
		if err := tx.queries.SetAccountDeparted(ctx, db.SetAccountDepartedParams{
			Departed: 1,
			ID:       accountID,
		}); err != nil {
			return fmt.Errorf("mark departed: %w", err)
		}
		if err := tx.queries.CancelPendingHandoffsTo(ctx, accountID); err != nil {
			return fmt.Errorf("cancel handoffs: %w", err)
		}
		return tx.EndSessions(ctx, accountID)
	})
}

// MarkReturned reverses a departure: the person behind accountID can sign in and
// act again, the Goals they still own stop being Ownerless, and their Delegate
// rights come back as they were. Nothing else is undone — a Goal reassigned
// while they were away stays with its new Owner, and a cancelled Handoff stays
// cancelled (CONTEXT.md: Departed). Only an Admin may do this, and only of
// someone who has Departed (markReturnedRefusal); actorID identifies the
// acting Account. An accountID naming no Account is refused first, whoever
// asks.
func (s *Service) MarkReturned(ctx context.Context, actorID, accountID int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		person, err := tx.queries.GetAccount(ctx, accountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "account does not exist")
			}
			return fmt.Errorf("look up account: %w", err)
		}
		if err := markReturnedRefusal(tx.accountActor(ctx, actorID), accountFromRow(person)); err != nil {
			return err
		}
		if err := tx.queries.SetAccountDeparted(ctx, db.SetAccountDepartedParams{
			Departed: 0,
			ID:       accountID,
		}); err != nil {
			return fmt.Errorf("mark returned: %w", err)
		}
		return nil
	})
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

// Admins lists every Admin, Departed ones included, in the order they are
// shown: by Label, ignoring case, as DepartedAccounts sorts.
func (s *Service) Admins(ctx context.Context) ([]Account, error) {
	rows, err := s.queries.ListAdminAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list admin accounts: %w", err)
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

// MakeAdmin gives the Admin role to the person with emailAddr, whatever its
// case or surrounding spaces, and returns their Account (CONTEXT.md: Admin);
// an Admin already is returned as they are. It never creates an Account, so a
// typo can't make a stray one: an email no one has is ErrValidation, and a
// Departed person is ErrDeparted. Only an Admin may do this; actorID
// identifies the acting Account.
func (s *Service) MakeAdmin(ctx context.Context, actorID int64, emailAddr string) (Account, error) {
	var acc Account
	err := s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.requireAdmin(ctx, actorID); err != nil {
			return err
		}
		row, err := tx.queries.GetAccountByEmail(ctx, emailAddr)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "no one with that email has an account")
			}
			return fmt.Errorf("look up account: %w", err)
		}
		acc = accountFromRow(row)
		if acc.Departed {
			return fmt.Errorf("%w: %s has left the company", ErrDeparted, acc.Email)
		}
		if acc.IsAdmin {
			return nil
		}
		if err := tx.queries.SetAccountAdmin(ctx, db.SetAccountAdminParams{IsAdmin: 1, ID: row.ID}); err != nil {
			return fmt.Errorf("make admin: %w", err)
		}
		acc.IsAdmin = true
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return acc, nil
}

// RemoveAdmin takes the Admin role from the person behind accountID; someone
// who isn't an Admin is left as they are. No Admin can take it from
// themselves, so there is always at least one (CONTEXT.md: Admin). Only an
// Admin may do this; actorID identifies the acting Account.
func (s *Service) RemoveAdmin(ctx context.Context, actorID, accountID int64) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		if err := tx.requireAdmin(ctx, actorID); err != nil {
			return err
		}
		if accountID == actorID {
			return Refuse(ErrValidation, "you can't remove your own Admin role")
		}
		row, err := tx.queries.GetAccount(ctx, accountID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Refuse(ErrValidation, "account does not exist")
			}
			return fmt.Errorf("look up account: %w", err)
		}
		if row.IsAdmin == 0 {
			return nil
		}
		if err := tx.queries.SetAccountAdmin(ctx, db.SetAccountAdminParams{IsAdmin: 0, ID: accountID}); err != nil {
			return fmt.Errorf("remove admin: %w", err)
		}
		return nil
	})
}

// requireAdmin returns ErrNotAuthorized unless actorID is an Admin.
func (s *Service) requireAdmin(ctx context.Context, actorID int64) error {
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Refuse(ErrNotAuthorized, "not an Admin")
		}
		return fmt.Errorf("look up actor: %w", err)
	}
	return adminRefusal(accountFromRow(actor))
}

// adminRefusal is requireAdmin's rule for actor, an Account that exists: an
// Admin who hasn't Departed, since a Departed person can't act (CONTEXT.md:
// Departed).
func adminRefusal(actor Account) error {
	if !actor.IsAdmin {
		return Refuse(ErrNotAuthorized, "only an Admin may do this")
	}
	if actor.Departed {
		return Refuse(ErrNotAuthorized, "a Departed Admin can't do this")
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
		return Account{}, fmt.Errorf("%w: %s has left the company", ErrDeparted, emailAddr)
	}
	return acc, nil
}

// SignInWithOIDC resolves a sign-in through the org's OpenID Connect provider,
// finding the Account by the subject the provider gives the person rather than
// by their email, which the provider may later give to someone else:
//
//  1. The Account linked to subject, whatever its email now.
//  2. Else the Account emailAddr names, created as SignIn would if there is
//     none, which is then linked to subject.
//  3. Else, when that Account is linked to another subject, or a sign-in
//     racing this one links subject to another Account first, nothing: the
//     sign-in is refused with an *OIDCSubjectConflictError.
//
// A Departed Account is refused with ErrDeparted, and isn't linked. The
// lookups and the link are one transaction, so a sign-in racing this one
// waits for it.
func (s *Service) SignInWithOIDC(ctx context.Context, subject, emailAddr string) (Account, error) {
	if subject == "" {
		return Account{}, errors.New("OIDC sign-in has no subject")
	}
	var acc Account
	err := s.WithinTx(ctx, func(tx *Service) error {
		linked, err := tx.queries.GetAccountByOIDCSubject(ctx, &subject)
		if err == nil {
			acc = accountFromRow(linked)
			if acc.Departed {
				return fmt.Errorf("%w: %s has left the company", ErrDeparted, acc.Email)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up account by OIDC subject: %w", err)
		}

		if acc, err = tx.SignIn(ctx, emailAddr); err != nil {
			return err
		}
		n, err := tx.queries.LinkAccountOIDCSubject(ctx, db.LinkAccountOIDCSubjectParams{OidcSubject: &subject, ID: acc.ID})
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			// Another Account is linked to subject already.
			return &OIDCSubjectConflictError{AccountID: acc.ID}
		}
		if err != nil {
			return fmt.Errorf("link account to OIDC subject: %w", err)
		}
		if n == 0 {
			// Already linked: to another subject, since this transaction
			// found no Account linked to this one.
			return &OIDCSubjectConflictError{AccountID: acc.ID}
		}
		return nil
	})
	if err != nil {
		return Account{}, err
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
	var acc Account
	err := s.WithinTx(ctx, func(tx *Service) error {
		existing, err := tx.queries.GetAccountByEmail(ctx, emailAddr)
		if err == nil {
			acc = accountFromRow(existing)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up account: %w", err)
		}

		isAdmin := int64(0)
		if tx.admins[adminKey(emailAddr)] {
			isAdmin = 1
		}
		created, err := tx.queries.CreateAccount(ctx, db.CreateAccountParams{
			Email:     emailAddr,
			IsAdmin:   isAdmin,
			CreatedAt: tx.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create account: %w", err)
		}
		acc = accountFromRow(created)
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return acc, nil
}
