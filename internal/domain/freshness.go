package domain

import (
	"context"
	"time"
)

// Freshness is how current a Goal's updates are (CONTEXT.md: Stale). It is
// computed when read, from the Goal's last Check-in and the org's calendar.
type Freshness struct {
	// Stale is true when the Goal is Active and its last Check-in — or its
	// activation, if it has none — is more than its cadence of days ago. On
	// Hold, Proposed, Done, and Cancelled Goals are never Stale.
	Stale bool
	// LastUpdate is the Goal's last Check-in, or its activation when it has
	// none. It is the zero time for a Goal that was never activated.
	LastUpdate time.Time
	// DaysSince is how many days of the org's calendar have passed since
	// LastUpdate.
	DaysSince int
	// PathToGreenOverdue is true when the Goal is Active, isn't Green, and the
	// target date of its Path to Green (PathTargetDate) has passed: a stalled
	// recovery (CONTEXT.md: Path to Green).
	PathToGreenOverdue bool
	// PathTargetDate is the date the latest Path to Green aims to be back at
	// Green by, the zero time when the Goal is Green or has no Check-in.
	PathTargetDate time.Time
}

// Freshness reads goalID's freshness signals.
func (s *Service) Freshness(ctx context.Context, goalID int64) (Freshness, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	// With no Check-in yet, latest is the zero Checkin.
	latest, _, err := s.LatestCheckin(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	return s.judgeFreshness(g, latest), nil
}

// judgeFreshness applies the Stale and overdue-Path-to-Green rules to g, whose
// latest Check-in is latest (the zero Checkin when it has none). Days are
// counted on the org's calendar, so a Check-in late on Monday and one early on
// Monday both count from Monday, and a Path to Green is overdue from the day
// after its target date.
func (s *Service) judgeFreshness(g Goal, latest Checkin) Freshness {
	last := latest.CreatedAt
	if last.IsZero() {
		last = g.ActivatedAt
	}
	if last.IsZero() {
		return Freshness{}
	}
	now := s.clock.Now()
	active := g.Lifecycle == LifecycleActive
	days := s.daysBetween(last, now)
	target := latest.PathTargetDate
	return Freshness{
		Stale:              active && days > g.CadenceDays,
		LastUpdate:         last,
		DaysSince:          days,
		PathToGreenOverdue: active && needsPathToGreen(latest.Health) && !target.IsZero() && orgDate(now, s.loc).After(target),
		PathTargetDate:     target,
	}
}

// daysBetween counts the calendar days from one instant to a later one, each
// read as a date in the org's timezone.
func (s *Service) daysBetween(from, to time.Time) int {
	return int(orgDate(to, s.loc).Sub(orgDate(from, s.loc)) / (24 * time.Hour))
}

// orgDate is the calendar date t falls on in loc, as midnight UTC so that dates
// subtract to whole days whatever the timezone's daylight-saving rules.
func orgDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
