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
// below them by Manager, a level at a time (CONTEXT.md: Org), as orgWalk
// walks it, from one read of every Account. It returns ErrNotFound if no
// such Account exists.
func (s *Service) Org(ctx context.Context, personID int64) ([]Account, error) {
	accounts, err := s.orgWalkAccounts(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(accounts, func(a Account) bool { return a.ID == personID })
	if i < 0 {
		return nil, fmt.Errorf("%w: account %d", ErrNotFound, personID)
	}
	org := []Account{accounts[i]}
	for _, r := range orgWalk(personID, accounts) {
		org = append(org, r.Account)
	}
	return org, nil
}

// orgReach is one person below someone in their Org, with the person directly
// under them through whom an Org's walk first reached them.
type orgReach struct {
	Account
	via int64
}

// orgWalk walks personID's Org in accounts, every Account: everyone below
// personID by Manager, a level at a time and each level by Account ID. The
// directory's cycles are stored as it gives them (ADR 0008), so the walk
// stops at anyone it has already reached, personID among them, and a person
// who is their own Manager is no one's report. Each person comes with the one
// directly under personID through whom the walk first reached them. It is the
// one walk down an Org: Org, OrgSignals and a page's Scope all read it.
func orgWalk(personID int64, accounts []Account) []orgReach {
	under := map[int64][]Account{}
	for _, a := range accounts {
		if a.ManagerID != nil && *a.ManagerID != a.ID {
			under[*a.ManagerID] = append(under[*a.ManagerID], a)
		}
	}
	var out []orgReach
	reached := map[int64]bool{personID: true}
	via := map[int64]int64{}
	for level := []int64{personID}; len(level) > 0; {
		var next []Account
		for _, id := range level {
			next = append(next, under[id]...)
		}
		slices.SortFunc(next, func(a, b Account) int { return cmp.Compare(a.ID, b.ID) })
		level = nil
		for _, a := range next {
			if reached[a.ID] {
				continue
			}
			reached[a.ID] = true
			v := a.ID
			if *a.ManagerID != personID {
				v = via[*a.ManagerID]
			}
			via[a.ID] = v
			out = append(out, orgReach{Account: a, via: v})
			level = append(level, a.ID)
		}
	}
	return out
}

// orgWalkAccounts reads every Account, by ID, for orgWalk.
func (s *Service) orgWalkAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.queries.ListOrgWalkAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list org: %w", err)
	}
	return orgWalkAccountsOf(rows), nil
}

// orgWalkAccountsOf is rows, every Account by ID, as orgWalk walks them.
func orgWalkAccountsOf(rows []db.Account) []Account {
	out := make([]Account, 0, len(rows))
	for _, r := range rows {
		out = append(out, accountFromRow(r))
	}
	return out
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
