package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Weekly is when in the week the emails go out: a day and a time of day on the
// company's calendar.
type Weekly struct {
	Day          time.Weekday
	Hour, Minute int
	Loc          *time.Location
}

// ParseWeekly reads a weekday name ("Monday") and a 24-hour time ("09:00") in
// loc, the company's timezone.
func ParseWeekly(day, at string, loc *time.Location) (Weekly, error) {
	w := Weekly{Loc: loc}
	found := false
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(strings.TrimSpace(day), d.String()) {
			w.Day, found = d, true
		}
	}
	if !found {
		return Weekly{}, fmt.Errorf("day %q is not a weekday name such as Monday", day)
	}
	t, err := time.Parse("15:04", strings.TrimSpace(at))
	if err != nil {
		return Weekly{}, fmt.Errorf("time %q is not a 24-hour HH:MM time such as 09:00", at)
	}
	w.Hour, w.Minute = t.Hour(), t.Minute()
	return w, nil
}

// Next returns the first time after t that falls on w's day and time of day.
func (w Weekly) Next(t time.Time) time.Time {
	local := t.In(w.Loc)
	days := (int(w.Day) - int(local.Weekday()) + 7) % 7
	next := time.Date(local.Year(), local.Month(), local.Day()+days, w.Hour, w.Minute, 0, 0, w.Loc)
	if !next.After(t) {
		next = time.Date(local.Year(), local.Month(), local.Day()+days+7, w.Hour, w.Minute, 0, 0, w.Loc)
	}
	return next
}

// Prev returns the last time at or before t that falls on w's day and time of
// day: the slot most recently due.
func (w Weekly) Prev(t time.Time) time.Time {
	local := t.In(w.Loc)
	days := (int(local.Weekday()) - int(w.Day) + 7) % 7
	prev := time.Date(local.Year(), local.Month(), local.Day()-days, w.Hour, w.Minute, 0, 0, w.Loc)
	if prev.After(t) {
		prev = time.Date(local.Year(), local.Month(), local.Day()-days-7, w.Hour, w.Minute, 0, 0, w.Loc)
	}
	return prev
}

// Scheduler runs a job once a week, in process, at the Weekly day and time,
// each a slot. The slot the job last ran for is kept in the database
// (Service.LastScheduledRun), so a restart neither skips a week nor runs one
// twice. It reads the time from the Service's clock, so a test drives it by
// setting the clock and calling Tick.
type Scheduler struct {
	svc  *domain.Service
	job  string
	when Weekly
	run  func(ctx context.Context, slot time.Time) error
	// next is when the job runs next; zero until the first Tick reads the
	// job's last run.
	next time.Time
	// failures counts the polls failed in a row; Poll logs the next failure
	// once the clock reaches logAfter, then waits logWait more.
	failures int
	logAfter time.Time
	logWait  time.Duration
}

// schedulerMaxLogWait is the longest Poll goes without logging a run that
// keeps failing.
const schedulerMaxLogWait = time.Hour

// NewScheduler returns a Scheduler that runs job, by calling run with the slot
// it is due for, at each occurrence of when. run records the slot as the job's
// run (Service.RecordScheduledRun) in the transaction that does its work.
//
// On a restart, the first Tick reads the slot the job last ran for: if a slot
// has passed since, while the app was down, the job runs once, late, at that
// first Tick, however many weeks it missed. Otherwise it waits for the next
// slot. Started for the very first time, with no run recorded, it records the
// slot just passed as run without running it, so installing the app sends no
// surprise email, and waits for the next.
func NewScheduler(svc *domain.Service, job string, when Weekly, run func(ctx context.Context, slot time.Time) error) *Scheduler {
	return &Scheduler{svc: svc, job: job, when: when, run: run}
}

// Tick runs the job if its slot has come, for the slot most recently due,
// then schedules the next run for the slot after. A job missed while the
// process was asleep, or down (see NewScheduler), runs once, late, not once
// per missed week. A run that fails is tried again at the next Tick, for the
// slot then most recently due.
func (s *Scheduler) Tick(ctx context.Context) error {
	_, err := s.tick(ctx)
	return err
}

// tick is Tick, also returning the slot most recently due: the one a run
// would be, or was, for.
func (s *Scheduler) tick(ctx context.Context) (time.Time, error) {
	now := s.svc.Now()
	slot := s.when.Prev(now)
	if s.next.IsZero() {
		next, err := s.start(ctx, now)
		if err != nil {
			return slot, err
		}
		s.next = next
	}
	if now.Before(s.next) {
		return slot, nil
	}
	if err := s.run(ctx, slot); err != nil {
		return slot, err
	}
	s.next = s.when.Next(now)
	return slot, nil
}

// start is when the job first runs: now, when a slot has passed since its last
// recorded run, or else the next slot. A job with no run recorded has the slot
// just passed recorded as run.
func (s *Scheduler) start(ctx context.Context, now time.Time) (time.Time, error) {
	due := s.when.Prev(now)
	last, ok, err := s.svc.LastScheduledRun(ctx, s.job)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		if _, err := s.svc.RecordScheduledRun(ctx, s.job, due); err != nil {
			return time.Time{}, err
		}
		return s.when.Next(now), nil
	}
	if due.After(last) {
		return now, nil
	}
	return s.when.Next(now), nil
}

// Run calls Poll every poll interval until ctx is done.
func (s *Scheduler) Run(ctx context.Context, poll time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Poll(ctx, logger)
		}
	}
}

// Poll is one of Run's polls: it calls Tick and logs the outcome to logger,
// each line naming the job and the slot. A run that keeps failing is tried at
// every poll but logged at its first failure and then only after a wait that
// doubles from a minute up to an hour, with the count of polls failed so far;
// its recovery is logged once.
func (s *Scheduler) Poll(ctx context.Context, logger *slog.Logger) {
	slot, err := s.tick(ctx)
	if errors.Is(err, context.Canceled) {
		return
	}
	if err == nil {
		if s.failures > 0 {
			logger.Info("weekly emails recovered", "job", s.job, "slot", slot, "failures", s.failures)
			s.failures = 0
		}
		return
	}
	now := s.svc.Now()
	s.failures++
	if s.failures == 1 {
		s.logWait = time.Minute
	} else if now.Before(s.logAfter) {
		return
	}
	logger.Error("weekly emails failed", "job", s.job, "slot", slot, "failures", s.failures, "err", err)
	s.logAfter = now.Add(s.logWait)
	s.logWait = min(2*s.logWait, schedulerMaxLogWait)
}
