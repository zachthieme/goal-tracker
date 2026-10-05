package domain

import (
	"context"
	"fmt"
)

// Chain returns the people in personID's Chain: the person first, then everyone
// below them by Manager, a level at a time (CONTEXT.md: Chain). The directory's
// cycles are stored as it gives them (ADR 0008), so the walk stops at anyone
// it has already reached. It returns ErrNotFound if no such Account exists.
func (s *Service) Chain(ctx context.Context, personID int64) ([]Account, error) {
	person, err := s.Account(ctx, personID)
	if err != nil {
		return nil, err
	}
	chain := []Account{person}
	reached := map[int64]bool{person.ID: true}
	for level := []*int64{&person.ID}; len(level) > 0; {
		under, err := s.queries.ListAccountsUnder(ctx, level)
		if err != nil {
			return nil, fmt.Errorf("list chain: %w", err)
		}
		level = nil
		for _, row := range under {
			if reached[row.ID] {
				continue
			}
			reached[row.ID] = true
			chain = append(chain, accountFromRow(row))
			level = append(level, &row.ID)
		}
	}
	return chain, nil
}

// HasAnyoneUnder reports whether anyone but personID is in their Chain:
// whether anyone has them as Manager. A person who manages only themselves,
// as a directory can say, has no one under them.
func (s *Service) HasAnyoneUnder(ctx context.Context, personID int64) (bool, error) {
	under, err := s.queries.ListAccountsUnder(ctx, []*int64{&personID})
	if err != nil {
		return false, fmt.Errorf("list chain: %w", err)
	}
	for _, row := range under {
		if row.ID != personID {
			return true, nil
		}
	}
	return false, nil
}
