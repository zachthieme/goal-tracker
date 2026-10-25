package directory_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
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

// Run logs to the logger NewSync was given; RunWith logs to the one it is
// given, and nothing of that run reaches NewSync's.
func TestRunWithLogsToTheLoggerItIsGiven(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	var own, given bytes.Buffer
	s := directory.NewSync(h.Service, &countingDirectory{}, slog.New(slog.NewTextHandler(&own, nil)))

	s.Run(t.Context())
	if !strings.Contains(own.String(), "people=1") {
		t.Errorf("Run logged %q to NewSync's logger, want the people count", own.String())
	}

	own.Reset()
	s.RunWith(t.Context(), slog.New(slog.NewTextHandler(&given, nil)))
	if !strings.Contains(given.String(), "people=1") {
		t.Errorf("RunWith logged %q to the logger it was given, want the people count", given.String())
	}
	if own.Len() != 0 {
		t.Errorf("RunWith logged %q to NewSync's logger, want nothing", own.String())
	}
}

// listDirectory lists whoever it's been set to, and counts how often it's
// been read.
type listDirectory struct {
	mu     sync.Mutex
	people []domain.DirectoryPerson
	reads  int
}

func (d *listDirectory) People(context.Context) ([]domain.DirectoryPerson, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	return d.people, nil
}

func (d *listDirectory) set(people ...domain.DirectoryPerson) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.people = people
}

func (d *listDirectory) readCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads
}

// A sync on an Admin's behalf, like Sync now, is refused for anyone else with
// ErrNotAuthorized: the directory isn't read, no Account changes, and Last
// still reports the run before. An Admin's runs as any other does.
func TestRunForAdminRefusesANonAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dir := &listDirectory{}
	dir.set(domain.DirectoryPerson{Email: "ceo@example.com"})
	s := directory.NewSync(h.Service, dir, slog.New(slog.DiscardHandler))
	before := s.Run(t.Context())
	h.Clock.Advance(time.Hour)

	dir.set(domain.DirectoryPerson{Email: "new.hire@example.com", ManagerEmail: "ceo@example.com"})
	refused := s.RunForAdmin(t.Context(), slog.New(slog.DiscardHandler), sam.ID)
	if !errors.Is(refused.Err, domain.ErrNotAuthorized) {
		t.Errorf("a non-Admin's sync: %+v, want ErrNotAuthorized", refused)
	}
	if got := dir.readCount(); got != 1 {
		t.Errorf("the directory was read %d times, want once, before the refused sync", got)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "new.hire@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a non-Admin's sync made an Account for the new hire (err %v)", err)
	}
	if last, ok := s.Last(); !ok || last != before {
		t.Errorf("Last after a refused sync: %+v, want the run before, %+v", last, before)
	}

	ran := s.RunForAdmin(t.Context(), slog.New(slog.DiscardHandler), ada.ID)
	if ran.Err != nil || ran.People != 1 {
		t.Errorf("an Admin's sync: %+v, want 1 person read", ran)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "new.hire@example.com"); err != nil {
		t.Errorf("an Admin's sync made no Account for the new hire: %v", err)
	}
	if last, _ := s.Last(); last != ran {
		t.Errorf("Last after an Admin's sync: %+v, want %+v", last, ran)
	}
}
