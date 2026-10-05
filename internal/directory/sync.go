package directory

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Outcome is how one sync of the directory went: when it finished, and how
// many people it read, or the error that stopped it.
type Outcome struct {
	At     time.Time
	People int
	Err    error
}

// Sync keeps the tool's Accounts and their Managers in step with the org's
// directory (ADR 0008), and remembers how the last run went, for the Admin
// page. Its methods are safe to call at once; runs take turns.
type Sync struct {
	svc *domain.Service
	dir domain.Directory
	log *slog.Logger

	// running is held for the length of a run, so two never overlap.
	running sync.Mutex
	// mu guards last and ran.
	mu   sync.Mutex
	last Outcome
	ran  bool
}

// NewSync syncs svc's Accounts from dir, logging to logger.
func NewSync(svc *domain.Service, dir domain.Directory, logger *slog.Logger) *Sync {
	return &Sync{svc: svc, dir: dir, log: logger}
}

// Run syncs the directory now and returns how it went, which Last then
// reports. A failed run changes nothing and logs its cause at ERROR; the next
// run tries again. Each cycle among the Managers is logged at WARN, naming the
// people in it.
func (s *Sync) Run(ctx context.Context) Outcome {
	s.running.Lock()
	defer s.running.Unlock()

	res, err := s.svc.SyncDirectory(ctx, s.dir)
	out := Outcome{At: s.svc.Now(), People: res.People, Err: err}
	if err != nil {
		s.log.Error("directory sync failed; Accounts and Managers are as they were, and the next sync tries again", "err", err)
	} else {
		s.log.Info("directory sync read the org's people", "people", res.People)
		for _, cycle := range res.Cycles {
			s.log.Warn("the directory's Managers form a cycle; each Chain in it stops where it began", "people", cycleNames(cycle))
		}
	}

	s.mu.Lock()
	s.last, s.ran = out, true
	s.mu.Unlock()
	return out
}

// cycleNames is a cycle as "a@example.com → b@example.com → a@example.com".
func cycleNames(cycle []domain.Account) string {
	names := make([]string, 0, len(cycle)+1)
	for _, a := range cycle {
		names = append(names, a.Email)
	}
	names = append(names, cycle[0].Email)
	return strings.Join(names, " → ")
}

// Last is how the last run went, and false if there hasn't been one.
func (s *Sync) Last() (Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.ran
}

// RunEvery runs a sync now and then every interval until ctx is done.
func (s *Sync) RunEvery(ctx context.Context, interval time.Duration) {
	s.Run(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Run(ctx)
		}
	}
}
