package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// scheduledSlotFormat is how a scheduled run's slot is kept: UTC to the
// second, so slots sort as text.
const scheduledSlotFormat = time.RFC3339

// LastScheduledRun returns the slot the scheduled job last ran for: the time
// it was due, not when it ran. ok is false for a job that has never run.
func (s *Service) LastScheduledRun(ctx context.Context, job string) (slot time.Time, ok bool, err error) {
	raw, err := s.queries.GetScheduledRun(ctx, job)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read %s's last run: %w", job, err)
	}
	slot, err = time.Parse(scheduledSlotFormat, raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read %s's last run %q: %w", job, raw, err)
	}
	return slot, true, nil
}

// RecordScheduledRun records slot as the scheduled job's last run, and
// reports whether it did: a slot no later than the one recorded already has
// been run, by an earlier run or another process, and is not recorded again.
// A job calls it in the transaction that does its work, doing the work only
// when it records the slot, so a slot's work is done once and is never lost
// to a crash partway.
func (s *Service) RecordScheduledRun(ctx context.Context, job string, slot time.Time) (bool, error) {
	n, err := s.queries.RecordScheduledRun(ctx, db.RecordScheduledRunParams{
		Job: job, Slot: slot.UTC().Format(scheduledSlotFormat),
	})
	if err != nil {
		return false, fmt.Errorf("record %s's run: %w", job, err)
	}
	return n > 0, nil
}
