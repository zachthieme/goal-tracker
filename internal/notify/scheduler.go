package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
)

// Weekly is when in the week the emails go out: a day and a time of day on the
// org's calendar.
type Weekly struct {
	Day          time.Weekday
	Hour, Minute int
	Loc          *time.Location
}

// ParseWeekly reads a weekday name ("Monday") and a 24-hour time ("09:00") in
// loc, the org's timezone.
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

// Scheduler runs a job once a week, in process, at the Weekly day and time. It
// reads the time from its clock, so a test drives it by setting the clock and
// calling Tick.
type Scheduler struct {
	clock clock.Clock
	when  Weekly
	job   func(context.Context) error
	next  time.Time
}

// NewScheduler returns a Scheduler that runs job at the first occurrence of
// when after the clock's current time, then weekly.
func NewScheduler(clk clock.Clock, when Weekly, job func(context.Context) error) *Scheduler {
	return &Scheduler{clock: clk, when: when, job: job, next: when.Next(clk.Now())}
}

// Tick runs the job if its time has come, then schedules the next run a week
// on. A job missed while the process was down or asleep runs once, late, not
// once per missed week.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.clock.Now()
	if now.Before(s.next) {
		return nil
	}
	s.next = s.when.Next(now)
	return s.job(ctx)
}

// Run calls Tick every poll interval until ctx is done, logging a failed job.
func (s *Scheduler) Run(ctx context.Context, poll time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("weekly emails failed", "err", err)
			}
		}
	}
}
