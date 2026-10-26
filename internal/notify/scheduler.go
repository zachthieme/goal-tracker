package notify

import (
	"context"
	"fmt"
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
}

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
// slot then most recently due, and its error names the slot it was for. A
// failure to read the job's last run names no slot: until it is read, no slot
// is known to be due.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.svc.Now()
	if s.next.IsZero() {
		next, err := s.start(ctx, now)
		if err != nil {
			return err
		}
		s.next = next
	}
	if now.Before(s.next) {
		return nil
	}
	slot := s.when.Prev(now)
	if err := s.run(ctx, slot); err != nil {
		return fmt.Errorf("the run for the slot %s: %w", slot.Format(time.RFC3339), err)
	}
	s.next = s.when.Next(now)
	return nil
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
