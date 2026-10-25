package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SessionGeneration is accountID's session generation: the number its session
// cookies carry. Only a cookie carrying the current one signs the person in.
// An Account whose sessions were never ended is at 0.
func (s *Service) SessionGeneration(ctx context.Context, accountID int64) (int64, error) {
	gen, err := s.queries.GetSessionGeneration(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get session generation: %w", err)
	}
	return gen, nil
}

// EndSessions ends every session accountID has: it moves their session
// generation on, so each cookie issued before stops working and the next
// sign-in issues one for the new generation. Signing out, a password reset and
// MarkDeparted call it.
func (s *Service) EndSessions(ctx context.Context, accountID int64) error {
	if err := s.queries.EndAccountSessions(ctx, accountID); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return nil
}
