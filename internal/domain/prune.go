package domain

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// emailRetention is how long the outbox keeps an email once it is sent, or
// once it was queued if it was given up on, before PruneStale deletes it.
const emailRetention = 90 * 24 * time.Hour

// StaleRowsPruned counts what one PruneStale deleted.
type StaleRowsPruned struct {
	// Email is the emails deleted from the outbox: sent, or given up on, more
	// than emailRetention ago.
	Email int64
	// UndoTokens is the Undo tokens deleted, each past its UndoWindow.
	UndoTokens int64
}

// PruneStale deletes, by the Service's clock, the email sent more than 90
// days ago, the email given up on that was queued more than 90 days ago, and
// the Undo tokens whose UndoWindow has passed. Email still being tried is
// kept however old (ticket #498). It deletes them all in one transaction, or
// none.
func (s *Service) PruneStale(ctx context.Context) (StaleRowsPruned, error) {
	now := s.clock.Now()
	var pruned StaleRowsPruned
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		pruned.Email, err = tx.queries.DeleteStaleEmail(ctx, db.DeleteStaleEmailParams{
			Cutoff:      now.Add(-emailRetention).Format(timeFormat),
			MaxAttempts: emailMaxAttempts,
		})
		if err != nil {
			return fmt.Errorf("prune email outbox: %w", err)
		}
		pruned.UndoTokens, err = tx.queries.DeleteUndoTokensIssuedBefore(ctx, now.Add(-UndoWindow).Format(timeFormat))
		if err != nil {
			return fmt.Errorf("prune undo tokens: %w", err)
		}
		return nil
	})
	if err != nil {
		return StaleRowsPruned{}, err
	}
	return pruned, nil
}

// PruneStaleAndLog is the prune job's run: it prunes stale rows (PruneStale)
// and logs what it deleted to logger. Its error is the job runner's to log.
func (s *Service) PruneStaleAndLog(ctx context.Context, logger *slog.Logger) error {
	pruned, err := s.PruneStale(ctx)
	if err != nil {
		return err
	}
	logger.Info("pruned stale rows", "email", pruned.Email, "undo_tokens", pruned.UndoTokens)
	return nil
}
