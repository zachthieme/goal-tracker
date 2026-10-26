// Package jobs runs the app's background jobs, such as the email outbox and
// the weekly emails: each at once, then on its own pacing, until the app
// stops, each failure logged the same way.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
)

// Job is a background job: Run, called again every Every and on each receive
// from Wake, if it has one.
type Job struct {
	// Name names the job in the log.
	Name string
	// Every is how often the job runs, by real time.
	Every time.Duration
	// Wake, optional, has the job run again before its next Every.
	Wake <-chan struct{}
	// Run is one run of the job; it should return soon once ctx is done.
	Run func(ctx context.Context) error
}

// Runner runs background jobs, from Start until Stop. A failed run is tried
// again at the job's next tick. Its failure is logged at ERROR, the first at
// once and then only after a wait, on the Runner's clock, that doubles from a
// minute up to maxFailureLogWait, with the count of runs failed so far; its
// recovery is logged once at INFO.
type Runner struct {
	clk    clock.Clock
	logger *slog.Logger
	jobs   []Job

	cancel  context.CancelFunc
	running sync.WaitGroup
}

// maxFailureLogWait is the longest a Runner goes without logging a job that
// keeps failing.
const maxFailureLogWait = time.Hour

// New returns a Runner that times its failure log's waits on clk and logs to
// logger.
func New(clk clock.Clock, logger *slog.Logger) *Runner {
	return &Runner{clk: clk, logger: logger}
}

// Add has the Runner run j. Call it before Start.
func (r *Runner) Add(j Job) {
	r.jobs = append(r.jobs, j)
}

// Start runs every job at once, then every Every and on each Wake, in the
// background, until Stop or ctx is done.
func (r *Runner) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	for _, j := range r.jobs {
		r.logger.Debug("job started", "job", j.Name)
		r.running.Go(func() { r.loop(ctx, j) })
	}
}

// Stop cancels every job and returns once each one's Run has returned. It
// does nothing if Start hasn't been called.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.running.Wait()
}

// loop runs j until ctx is done.
func (r *Runner) loop(ctx context.Context, j Job) {
	defer r.logger.Debug("job stopped", "job", j.Name)
	ticker := time.NewTicker(j.Every)
	defer ticker.Stop()
	var failed failureLog
	for ctx.Err() == nil {
		err := j.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		failed.record(r, j.Name, err)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-j.Wake:
		}
	}
}

// failureLog is how a job's failures in a row have been logged.
type failureLog struct {
	// failures counts the runs failed in a row; the next is logged once the
	// clock reaches logAfter, which then moves on by logWait.
	failures int
	logAfter time.Time
	logWait  time.Duration
}

// record logs the outcome of one of job's runs, as the Runner's policy says.
func (f *failureLog) record(r *Runner, job string, err error) {
	if err == nil {
		if f.failures > 0 {
			r.logger.Info("background job recovered", "job", job, "failures", f.failures)
			f.failures = 0
		}
		return
	}
	now := r.clk.Now()
	f.failures++
	if f.failures == 1 {
		f.logWait = time.Minute
	} else if now.Before(f.logAfter) {
		return
	}
	r.logger.Error("background job failed", "job", job, "failures", f.failures, "err", err)
	f.logAfter = now.Add(f.logWait)
	f.logWait = min(2*f.logWait, maxFailureLogWait)
}
