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
	"github.com/zachthieme/goal-tracker/internal/jobs"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// countingDirectory lists one person, and counts how often it's been read.
type countingDirectory struct {
	reads atomic.Int64
}

func (d *countingDirectory) People(context.Context) ([]domain.DirectoryPerson, error) {
	d.reads.Add(1)
	return []domain.DirectoryPerson{{Email: "ceo@example.com", Name: "Dana Whitfield"}}, nil
}

// A directory sync job, run in the background, reads the directory as the
// runner starts, without waiting for its first Every.
func TestTheDirectorySyncJobReadsTheDirectoryAtStart(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	dir := &countingDirectory{}
	s := directory.NewSync(h.Service, dir, slog.New(slog.DiscardHandler))
	runner := jobs.New(h.Clock, slog.New(slog.DiscardHandler))
	runner.Add(jobs.Job{Name: "directory sync", Every: time.Hour, Run: func(ctx context.Context) error { return s.Run(ctx).Err }})
	runner.Start(t.Context())
	t.Cleanup(runner.Stop)

	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, ok := s.Last(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the directory sync job didn't sync within 10s of starting")
		}
	}
	if last, _ := s.Last(); last.Err != nil || last.People != 1 {
		t.Errorf("the first sync: %+v, want 1 person read", last)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "ceo@example.com"); err != nil {
		t.Errorf("the synced person has no Account: %v", err)
	}
}

// failingDirectory can't be read.
type failingDirectory struct{}

func (failingDirectory) People(context.Context) ([]domain.DirectoryPerson, error) {
	return nil, errors.New("directory unreachable")
}

// A failed Run, in the background, leaves logging its failure to the job
// runner, so it is logged once; an Admin's failed Sync now logs its own, as
// part of the request it serves.
func TestOnlyAnAdminsFailedSyncLogsItsFailure(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	var own, admins bytes.Buffer
	s := directory.NewSync(h.Service, failingDirectory{}, slog.New(slog.NewTextHandler(&own, nil)))

	if out := s.Run(t.Context()); out.Err == nil {
		t.Fatal("a sync of a directory that can't be read succeeded")
	}
	if own.Len() != 0 {
		t.Errorf("a failed Run logged %q, want nothing: the job runner logs it", own.String())
	}

	if out := s.RunForAdmin(t.Context(), slog.New(slog.NewTextHandler(&admins, nil)), ada.ID); out.Err == nil {
		t.Fatal("an Admin's sync of a directory that can't be read succeeded")
	}
	if log := admins.String(); !strings.Contains(log, "level=ERROR") || !strings.Contains(log, "directory unreachable") {
		t.Errorf("an Admin's failed sync logged %q, want its cause at ERROR", log)
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
