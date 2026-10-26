package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/jobs"
)

// runnerWait is how long a test waits for the runner before failing.
const runnerWait = 10 * time.Second

// runnerStart is when the tests' clock reads at first.
var runnerStart = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

// startRunner starts a Runner with job on a clock.Fixed and logging to
// logger, and stops it in the test's cleanup.
func startRunner(t *testing.T, clk *clock.Fixed, logger *slog.Logger, job jobs.Job) *jobs.Runner {
	t.Helper()
	r := jobs.New(clk, logger)
	r.Add(job)
	r.Start(t.Context())
	t.Cleanup(r.Stop)
	return r
}

// awaitRun waits for the job to signal a run on runs, failing the test if
// none comes within runnerWait.
func awaitRun(t *testing.T, runs <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-runs:
	case <-time.After(runnerWait):
		t.Fatalf("waited %v for %s", runnerWait, what)
	}
}

// A job runs as the runner starts, without waiting for its first Every.
func TestAJobRunsAtStart(t *testing.T) {
	t.Parallel()

	runs := make(chan struct{}, 1)
	startRunner(t, clock.NewFixed(runnerStart), slog.New(slog.DiscardHandler), jobs.Job{
		Name:  "test job",
		Every: time.Hour,
		Run: func(context.Context) error {
			runs <- struct{}{}
			return nil
		},
	})

	awaitRun(t, runs, "the run at start")
}

// A job runs again each Every.
func TestAJobRunsAgainEachEvery(t *testing.T) {
	t.Parallel()

	runs := make(chan struct{}, 1)
	startRunner(t, clock.NewFixed(runnerStart), slog.New(slog.DiscardHandler), jobs.Job{
		Name:  "test job",
		Every: 10 * time.Millisecond,
		Run: func(context.Context) error {
			runs <- struct{}{}
			return nil
		},
	})

	for i := range 3 {
		awaitRun(t, runs, fmt.Sprintf("run %d", i+1))
	}
}

// A job runs again on each receive from its Wake, without waiting for its
// next Every.
func TestAJobRunsAgainOnAWake(t *testing.T) {
	t.Parallel()

	runs := make(chan struct{}, 1)
	wake := make(chan struct{})
	startRunner(t, clock.NewFixed(runnerStart), slog.New(slog.DiscardHandler), jobs.Job{
		Name:  "test job",
		Every: time.Hour,
		Wake:  wake,
		Run: func(context.Context) error {
			runs <- struct{}{}
			return nil
		},
	})

	awaitRun(t, runs, "the run at start")
	for i := range 2 {
		select {
		case wake <- struct{}{}:
		case <-time.After(runnerWait):
			t.Fatalf("the runner took no wake %d within %v", i+1, runnerWait)
		}
		awaitRun(t, runs, fmt.Sprintf("the run on wake %d", i+1))
	}
}

// Stop cancels every job and returns only once each one's Run has returned;
// no job runs after it.
func TestStopReturnsOnceEveryRunHasReturned(t *testing.T) {
	t.Parallel()

	r := jobs.New(clock.NewFixed(runnerStart), slog.New(slog.DiscardHandler))
	var started [2]chan struct{}
	var release [2]chan struct{}
	var runs [2]atomic.Int64
	for i := range 2 {
		started[i], release[i] = make(chan struct{}, 1), make(chan struct{})
		r.Add(jobs.Job{
			Name:  fmt.Sprintf("job %d", i),
			Every: time.Millisecond,
			Run: func(context.Context) error {
				// Each run waits for the test to release it, its context
				// done or not.
				runs[i].Add(1)
				select {
				case started[i] <- struct{}{}:
				default:
				}
				<-release[i]
				return nil
			},
		})
	}
	r.Start(t.Context())
	for i := range 2 {
		awaitRun(t, started[i], fmt.Sprintf("job %d's run", i))
	}

	stopped := make(chan struct{})
	go func() {
		r.Stop()
		close(stopped)
	}()
	close(release[0])
	select {
	case <-stopped:
		t.Fatal("Stop returned while job 1's Run was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release[1])
	select {
	case <-stopped:
	case <-time.After(runnerWait):
		t.Fatalf("Stop didn't return within %v of every Run returning", runnerWait)
	}

	before := [2]int64{runs[0].Load(), runs[1].Load()}
	time.Sleep(50 * time.Millisecond)
	for i := range 2 {
		if got := runs[i].Load(); got != before[i] {
			t.Errorf("job %d ran %d times after Stop returned", i, got-before[i])
		}
	}
}

// runnerLog is a log handler that keeps each record it handles, at any level,
// safe to read while the runner logs.
type runnerLog struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *runnerLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *runnerLog) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *runnerLog) WithGroup(string) slog.Handler            { return l }

func (l *runnerLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r.Clone())
	return nil
}

// take returns the records at level or above logged since the last take, and
// forgets them.
func (l *runnerLog) take(level slog.Level) []slog.Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	var got []slog.Record
	for _, r := range l.records {
		if r.Level >= level {
			got = append(got, r)
		}
	}
	l.records = nil
	return got
}

// attrOf is the value of r's attribute key, and false if it has none.
func attrOf(r slog.Record, key string) (slog.Value, bool) {
	var v slog.Value
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v, found = a.Value, true
			return false
		}
		return true
	})
	return v, found
}

// steppedJob is a job each of whose runs waits for the test to hand it the
// result to return, so the test sets the clock only while the runner waits
// on the run and isn't reading it.
type steppedJob struct {
	started chan struct{}
	results chan error
}

func newSteppedJob() *steppedJob {
	return &steppedJob{started: make(chan struct{}), results: make(chan error)}
}

func (j *steppedJob) run(ctx context.Context) error {
	select {
	case j.started <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-j.results:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// step waits for the job's next run to start, which is once the runner has
// logged the run before, then returns what was logged at INFO or above since
// the last step, sets the clock to at, and has the run return err.
func (j *steppedJob) step(t *testing.T, log *runnerLog, clk *clock.Fixed, at time.Time, err error) []slog.Record {
	t.Helper()
	awaitRun(t, j.started, "the next run")
	got := log.take(slog.LevelInfo)
	clk.Set(at)
	j.results <- err
	return got
}

// A job that keeps failing is still run at every tick, but its failure is
// logged at once and then only after a wait, on the runner's clock, that
// doubles from a minute up to an hour, with the count of runs failed so far;
// its recovery is logged once. A later failure is logged at once again.
func TestAFailingJobIsLoggedWithABackoff(t *testing.T) {
	t.Parallel()

	clk := clock.NewFixed(runnerStart)
	log := &runnerLog{}
	job := newSteppedJob()
	startRunner(t, clk, slog.New(log), jobs.Job{Name: "test job", Every: time.Millisecond, Run: job.run})
	failure := errors.New("database is locked")
	minute := func(m int) time.Time { return runnerStart.Add(time.Duration(m) * time.Minute) }

	// The log of the run at minute m is read at the step for minute m+1.
	if got := job.step(t, log, clk, minute(0), failure); len(got) != 0 {
		t.Fatalf("logged %v before any run, want nothing", got)
	}
	var loggedAt []int
	for m := 1; m <= 184; m++ {
		err := failure
		if m == 184 {
			err = nil
		}
		for _, rec := range job.step(t, log, clk, minute(m), err) {
			n, _ := attrOf(rec, "failures")
			name, _ := attrOf(rec, "job")
			logged, hasErr := attrOf(rec, "err")
			if rec.Level != slog.LevelError || rec.Message != "background job failed" || name.String() != "test job" ||
				!hasErr || logged.String() != failure.Error() || n.Int64() != int64(m) {
				t.Fatalf("minute %d logged %q %v, want an ERROR naming the job, the error and %d failures", m-1, rec.Message, rec, m)
			}
			loggedAt = append(loggedAt, m-1)
		}
	}
	// Each wait doubles from a minute until it reaches the hour: 1, 2, 4, 8, 16,
	// 32, 60, 60 minutes.
	if want := []int{0, 1, 3, 7, 15, 31, 63, 123, 183}; !slices.Equal(loggedAt, want) {
		t.Errorf("logged the failure at minutes %v, want %v", loggedAt, want)
	}

	got := job.step(t, log, clk, minute(185), nil)
	if len(got) != 1 || got[0].Level != slog.LevelInfo || got[0].Message != "background job recovered" {
		t.Fatalf("logged %v on recovering, want one INFO that the job recovered", got)
	}
	if n, _ := attrOf(got[0], "failures"); n.Int64() != 184 {
		t.Errorf("the recovery says %v failures, want 184", n)
	}
	if name, _ := attrOf(got[0], "job"); name.String() != "test job" {
		t.Errorf("the recovery names job %q, want the job's name", name)
	}
	if got := job.step(t, log, clk, minute(186), failure); len(got) != 0 {
		t.Errorf("logged %v after recovering, want nothing more", got)
	}

	got = job.step(t, log, clk, minute(187), failure)
	if len(got) != 1 || got[0].Level != slog.LevelError {
		t.Fatalf("logged %v on the next failure, want an ERROR at once", got)
	}
	if n, _ := attrOf(got[0], "failures"); n.Int64() != 1 {
		t.Errorf("the next failure says %v failures, want 1", n)
	}
}

// A run that fails because its context is done, as the app stops, logs no
// failure.
func TestAnErrorAfterTheContextIsDoneIsNotLogged(t *testing.T) {
	t.Parallel()

	log := &runnerLog{}
	r := jobs.New(clock.NewFixed(runnerStart), slog.New(log))
	running := make(chan struct{}, 1)
	r.Add(jobs.Job{Name: "test job", Every: time.Hour, Run: func(ctx context.Context) error {
		running <- struct{}{}
		<-ctx.Done()
		return fmt.Errorf("deliver email: %w", ctx.Err())
	}})
	r.Start(t.Context())
	awaitRun(t, running, "the run at start")
	r.Stop()

	if got := log.take(slog.LevelWarn); len(got) != 0 {
		t.Errorf("logged %v for the run its context cancelled, want nothing", got)
	}
}

// Each job's start is logged at DEBUG as the runner starts, and its stop as
// it ends, each naming the job.
func TestEachJobLogsItsStartAndStop(t *testing.T) {
	t.Parallel()

	log := &runnerLog{}
	r := jobs.New(clock.NewFixed(runnerStart), slog.New(log))
	for _, name := range []string{"email outbox", "prune"} {
		r.Add(jobs.Job{Name: name, Every: time.Hour, Run: func(context.Context) error { return nil }})
	}
	r.Start(t.Context())
	// lines is what was logged at DEBUG, each as message and job name.
	lines := func() []string {
		var got []string
		for _, rec := range log.take(slog.LevelDebug) {
			if rec.Level == slog.LevelDebug {
				name, _ := attrOf(rec, "job")
				got = append(got, rec.Message+": "+name.String())
			}
		}
		slices.Sort(got)
		return got
	}
	if got, want := lines(), []string{"job started: email outbox", "job started: prune"}; !slices.Equal(got, want) {
		t.Errorf("Start logged %q at DEBUG, want %q", got, want)
	}

	r.Stop()
	if got, want := lines(), []string{"job stopped: email outbox", "job stopped: prune"}; !slices.Equal(got, want) {
		t.Errorf("Stop logged %q at DEBUG, want %q", got, want)
	}
}
