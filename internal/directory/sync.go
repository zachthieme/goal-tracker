package directory

import (
	"context"
	"errors"
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

// Sync keeps the tool's Accounts and their Managers in step with the company's
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
// people in it, and so is each timezone the directory gives that isn't an IANA
// name, naming the person and the value.
func (s *Sync) Run(ctx context.Context) Outcome {
	return s.RunWith(ctx, s.log)
}

// RunWith is Run logging to logger instead of the one NewSync was given. It
// takes its turn with every other run.
func (s *Sync) RunWith(ctx context.Context, logger *slog.Logger) Outcome {
	return s.run(ctx, logger, func() (domain.DirectorySyncResult, error) {
		return s.svc.SyncDirectory(ctx, s.dir)
	})
}

// RunForAdmin is RunWith on the behalf of actorID, who must be an Admin, so a
// run on demand, like an Admin's Sync now, logs as part of the request it
// serves. For anyone else, it returns an Outcome whose Err is
// ErrNotAuthorized, without reading the directory or changing anything, and
// Last still reports the run before.
func (s *Sync) RunForAdmin(ctx context.Context, logger *slog.Logger, actorID int64) Outcome {
	return s.run(ctx, logger, func() (domain.DirectorySyncResult, error) {
		return s.svc.SyncDirectoryAsAdmin(ctx, actorID, s.dir)
	})
}

// run syncs the directory through syncDirectory, once no other run is, logs
// how it went to logger, and records it for Last. A refusal is neither logged
// nor recorded.
func (s *Sync) run(ctx context.Context, logger *slog.Logger, syncDirectory func() (domain.DirectorySyncResult, error)) Outcome {
	s.running.Lock()
	defer s.running.Unlock()

	res, err := syncDirectory()
	out := Outcome{At: s.svc.Now(), People: res.People, Err: err}
	if errors.Is(err, domain.ErrNotAuthorized) {
		return out
	}
	if err != nil {
		logger.Error("directory sync failed; Accounts and Managers are as they were, and the next sync tries again", "err", err)
	} else {
		logger.Info("directory sync read the company's people", "people", res.People)
		for _, cycle := range res.Cycles {
			logger.Warn("the directory's Managers form a cycle; each Org in it stops where it began", "people", cycleNames(cycle))
		}
		for _, tz := range res.InvalidTimezones {
			logger.Warn("the directory gives a timezone that isn't an IANA name; the person uses the company's", "email", tz.Email, "timezone", tz.Value)
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
