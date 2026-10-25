package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Org returns the people in personID's Org: the person first, then everyone
// below them by Manager, a level at a time (CONTEXT.md: Org). The directory's
// cycles are stored as it gives them (ADR 0008), so the walk stops at anyone
// it has already reached. It returns ErrNotFound if no such Account exists.
func (s *Service) Org(ctx context.Context, personID int64) ([]Account, error) {
	person, err := s.Account(ctx, personID)
	if err != nil {
		return nil, err
	}
	org := []Account{person}
	reached := map[int64]bool{person.ID: true}
	for level := []*int64{&person.ID}; len(level) > 0; {
		under, err := s.queries.ListAccountsUnder(ctx, level)
		if err != nil {
			return nil, fmt.Errorf("list org: %w", err)
		}
		level = nil
		for _, row := range under {
			if reached[row.ID] {
				continue
			}
			reached[row.ID] = true
			org = append(org, accountFromRow(row))
			level = append(level, &row.ID)
		}
	}
	return org, nil
}

// HasAnyoneUnder reports whether anyone but personID is in their Org:
// whether anyone has them as Manager. A person who manages only themselves,
// as a directory can say, has no one under them.
func (s *Service) HasAnyoneUnder(ctx context.Context, personID int64) (bool, error) {
	under, err := s.queries.ListAccountsUnder(ctx, []*int64{&personID})
	if err != nil {
		return false, fmt.Errorf("list org: %w", err)
	}
	for _, row := range under {
		if row.ID != personID {
			return true, nil
		}
	}
	return false, nil
}

// AnyoneHasManager reports whether anyone has a Manager other than themselves,
// so an Org can hold more than its person (CONTEXT.md: Org). A self-reference,
// as a directory can say, isn't one; a Departed person keeps their Manager, so
// counts (ADR 0008).
func (s *Service) AnyoneHasManager(ctx context.Context) (bool, error) {
	managed, err := s.queries.ListManagedAccounts(ctx)
	if err != nil {
		return false, fmt.Errorf("list managed accounts: %w", err)
	}
	for _, row := range managed {
		if *row.ManagerID != row.ID {
			return true, nil
		}
	}
	return false, nil
}

// DirectlyUnder returns the people whose Manager is personID, by Name: the
// first level of their Org, without them. A person who manages only
// themselves, as a directory can say, has no one directly under them.
func (s *Service) DirectlyUnder(ctx context.Context, personID int64) ([]Account, error) {
	under, err := s.queries.ListAccountsUnder(ctx, []*int64{&personID})
	if err != nil {
		return nil, fmt.Errorf("list org: %w", err)
	}
	return directlyUnderAmong(personID, under), nil
}

// directlyUnderAmong is DirectlyUnder over accounts, which hold at least
// everyone whose Manager is personID.
func directlyUnderAmong(personID int64, accounts []db.Account) []Account {
	var out []Account
	for _, row := range accounts {
		if row.ManagerID != nil && *row.ManagerID == personID && row.ID != personID {
			out = append(out, accountFromRow(row))
		}
	}
	slices.SortFunc(out, func(a, b Account) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Label()), strings.ToLower(b.Label())), cmp.Compare(a.ID, b.ID))
	})
	return out
}
