package domain

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// AskForHelp is an open Ask for help: an Active Goal whose latest Check-in is
// Yellow or Red and marks its Path to Green as asking for help (CONTEXT.md:
// Path to Green, Ask for help). It is never stored as open: a later Check-in
// that unmarks it, or ends the Run off Green by being Green or taking the Goal
// out of Active, closes it, and an answer alone doesn't.
type AskForHelp struct {
	Goal        Goal
	Health      string
	PathToGreen string
	// Addressee is whom the ask goes to, worked out when read (see
	// AskAddressee); nil when no one is above the Owner.
	Addressee *Account
}

// AskAddressee is whom an Ask for help from owner goes to: the first Manager up
// owner's reporting line who hasn't left the company (CONTEXT.md: Ask for
// help). It is nil when there is no one, and is worked out from the directory
// as it is now, so a Manager marked Departed after the ask was made is skipped,
// and a directory change re-routes it. See askAddressee for the walk.
func (s *Service) AskAddressee(ctx context.Context, owner Account) (*Account, error) {
	return askAddressee(owner, func(id int64) (Account, bool, error) {
		a, err := s.Account(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return Account{}, false, nil
		}
		return a, err == nil, err
	})
}

// askAddressee walks up from owner's Manager, one Manager at a time, looking
// each up with account:
//   - no Manager, a self-reference (no Manager, as for ReportRuleManagers), or
//     someone the walk has already reached, the Owner included: it stops with
//     no addressee, as ADR 0008's Chain stops at anyone it has already reached;
//   - a Manager naming no Account: it stops with no addressee, since nothing
//     records who is above them. The directory sync gives every Manager it
//     names an Account, so this can't happen today; unlike loadPageOrgScope,
//     it isn't an error;
//   - a Departed Manager: it moves on to their Manager;
//   - anyone else is the addressee.
func askAddressee(owner Account, account func(id int64) (Account, bool, error)) (*Account, error) {
	reached := map[int64]bool{owner.ID: true}
	current := owner
	for {
		// Everyone the walk has passed is reached, so a self-reference is too.
		next := current.ManagerID
		if next == nil || reached[*next] {
			return nil, nil
		}
		manager, ok, err := account(*next)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if !manager.Departed {
			return &manager, nil
		}
		reached[manager.ID] = true
		current = manager
	}
}

// OpenAsksForHelp returns every open Ask for help, each with its addressee
// resolved: Red first, then Yellow, then by Goal title ignoring case.
func (s *Service) OpenAsksForHelp(ctx context.Context) ([]AskForHelp, error) {
	rows, err := s.queries.ListOpenAsksForHelp(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open asks for help: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// One read of everyone, so each ask's walk up the line is in memory.
	accounts, err := s.queries.ListAskWalkAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	byID := make(map[int64]Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = accountFromRow(a)
	}
	return openAsksAmong(rows, byID)
}

// openAsksForHelpAmong is OpenAsksForHelp given byID, every Account by ID,
// already read, so it reads only the open Asks for help.
func (s *Service) openAsksForHelpAmong(ctx context.Context, byID map[int64]Account) ([]AskForHelp, error) {
	rows, err := s.queries.ListOpenAsksForHelp(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open asks for help: %w", err)
	}
	return openAsksAmong(rows, byID)
}

// openAsksAmong is OpenAsksForHelp over rows, every open Ask for help, given
// byID, every Account by ID, to walk each ask's Owner up their line in.
func openAsksAmong(rows []db.ListOpenAsksForHelpRow, byID map[int64]Account) ([]AskForHelp, error) {
	lookup := func(id int64) (Account, bool, error) {
		a, ok := byID[id]
		return a, ok, nil
	}
	out := make([]AskForHelp, 0, len(rows))
	for _, r := range rows {
		ask := AskForHelp{Goal: goalFromRow(r.Goal, r.Account), Health: r.Health, PathToGreen: r.PathToGreen}
		var err error
		if ask.Addressee, err = askAddressee(ask.Goal.Owner, lookup); err != nil {
			return nil, err
		}
		out = append(out, ask)
	}
	slices.SortStableFunc(out, compareAsksForHelp)
	return out, nil
}

// AsksForHelpTo returns the open Asks for help addressed to accountID, in
// OpenAsksForHelp's order.
func (s *Service) AsksForHelpTo(ctx context.Context, accountID int64) ([]AskForHelp, error) {
	all, err := s.OpenAsksForHelp(ctx)
	if err != nil {
		return nil, err
	}
	var out []AskForHelp
	for _, a := range all {
		if a.Addressee != nil && a.Addressee.ID == accountID {
			out = append(out, a)
		}
	}
	return out, nil
}

// OpenAskForHelp returns goalID's open Ask for help, with its addressee; ok is
// false when it has none.
func (s *Service) OpenAskForHelp(ctx context.Context, goalID int64) (AskForHelp, bool, error) {
	r, err := s.queries.GetOpenAskForHelp(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AskForHelp{}, false, nil
		}
		return AskForHelp{}, false, fmt.Errorf("get open ask for help: %w", err)
	}
	ask := AskForHelp{Goal: goalFromRow(r.Goal, r.Account), Health: r.Health, PathToGreen: r.PathToGreen}
	if ask.Addressee, err = s.AskAddressee(ctx, ask.Goal.Owner); err != nil {
		return AskForHelp{}, false, err
	}
	return ask, true, nil
}

// compareAsksForHelp orders asks Red first, then Yellow, then by Goal title
// ignoring case.
func compareAsksForHelp(a, b AskForHelp) int {
	redFirst := func(h string) int {
		if h == HealthRed {
			return 0
		}
		return 1
	}
	return cmp.Or(
		cmp.Compare(redFirst(a.Health), redFirst(b.Health)),
		cmp.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title)),
	)
}
