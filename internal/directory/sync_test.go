package directory_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// runEveryWait is how long a RunEvery test waits for something before failing.
const runEveryWait = 10 * time.Second

// countingDirectory lists one person, and counts how often it's been read.
type countingDirectory struct {
	reads atomic.Int64
}

func (d *countingDirectory) People(context.Context) ([]domain.DirectoryPerson, error) {
	d.reads.Add(1)
	return []domain.DirectoryPerson{{Email: "ceo@example.com", Name: "Dana Whitfield"}}, nil
}

// startRunEvery runs s.RunEvery(interval) in a goroutine, and returns a cancel
// that stops it and a channel closed once it has returned. The test's cleanup
// stops it too.
func startRunEvery(t *testing.T, s *directory.Sync, interval time.Duration) (cancel func(), done <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		s.RunEvery(ctx, interval)
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	return cancel, finished
}

// waitFor polls cond until it holds, failing the test if it doesn't within
// runEveryWait.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(runEveryWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited %v for %s", runEveryWait, what)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitReturned waits for done to close, failing the test if it doesn't within
// runEveryWait.
func waitReturned(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(runEveryWait):
		t.Fatalf("RunEvery didn't return within %v of its context being cancelled", runEveryWait)
	}
}

// RunEvery syncs straight away, without waiting for its first interval, and
// returns once its context is done.
func TestRunEverySyncsAtOnceAndStopsWithItsContext(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	dir := &countingDirectory{}
	s := directory.NewSync(h.Service, dir, slog.New(slog.DiscardHandler))

	cancel, done := startRunEvery(t, s, time.Hour)
	waitFor(t, "the first sync", func() bool { _, ok := s.Last(); return ok })
	if last, _ := s.Last(); last.Err != nil || last.People != 1 {
		t.Errorf("the first sync: %+v, want 1 person read", last)
	}
	if got := dir.reads.Load(); got != 1 {
		t.Errorf("the directory was read %d times before the first interval, want 1", got)
	}

	cancel()
	waitReturned(t, done)
}

// RunEvery syncs again on each interval, and stops syncing once it has
// returned.
func TestRunEverySyncsAgainEachInterval(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	dir := &countingDirectory{}
	s := directory.NewSync(h.Service, dir, slog.New(slog.DiscardHandler))

	cancel, done := startRunEvery(t, s, 10*time.Millisecond)
	waitFor(t, "three syncs", func() bool { return dir.reads.Load() >= 3 })

	cancel()
	waitReturned(t, done)
	// One more sync may have run between the cancel and RunEvery returning,
	// but none runs after it: the count stays put while the test checks the
	// synced Account.
	stopped := dir.reads.Load()
	if _, err := h.Service.AccountByEmail(t.Context(), "ceo@example.com"); err != nil {
		t.Errorf("the synced person has no Account: %v", err)
	}
	if got := dir.reads.Load(); got != stopped {
		t.Errorf("the directory was read %d times after RunEvery returned", got-stopped)
	}
}
