package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A scheduled job's last run is kept by slot: a job never run has none, and
// recording a slot takes only one later than the slot already recorded, so a
// slot is recorded as run once.
func TestScheduledRunRecordsEachSlotOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	monday := time.Date(2026, 1, 5, 9, 0, 0, 0, la)
	nextMonday := monday.AddDate(0, 0, 7)

	if _, ok, err := h.Service.LastScheduledRun(ctx, "weekly emails"); err != nil || ok {
		t.Fatalf("LastScheduledRun of a job never run: ok %v, err %v, want no run", ok, err)
	}
	steps := []struct {
		slot   time.Time
		record bool
		last   time.Time
	}{
		{monday, true, monday},
		{monday, false, monday},
		{nextMonday, true, nextMonday},
		{monday, false, nextMonday},
	}
	for i, step := range steps {
		recorded, err := h.Service.RecordScheduledRun(ctx, "weekly emails", step.slot)
		if err != nil {
			t.Fatalf("step %d: RecordScheduledRun: %v", i, err)
		}
		if recorded != step.record {
			t.Errorf("step %d: recording %v recorded %v, want %v", i, step.slot, recorded, step.record)
		}
		last, ok, err := h.Service.LastScheduledRun(ctx, "weekly emails")
		if err != nil || !ok || !last.Equal(step.last) {
			t.Errorf("step %d: last run %v (ok %v, err %v), want %v", i, last, ok, err, step.last)
		}
	}
	if _, ok, _ := h.Service.LastScheduledRun(ctx, "another job"); ok {
		t.Errorf("another job has a last run, want each job's kept apart")
	}
}
