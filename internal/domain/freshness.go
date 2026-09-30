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
}

// Freshness reads goalID's freshness signals.
func (s *Service) Freshness(ctx context.Context, goalID int64) (Freshness, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	latest, ok, err := s.LatestCheckin(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	var last time.Time
	if ok {
		last = latest.CreatedAt
	}
	return s.judgeFreshness(g, last), nil
}

// judgeFreshness applies the Stale rule to g, whose last Check-in was at
// lastCheckin (the zero time when it has none). Cadence is counted in whole
// days of the org's calendar, so a Check-in late on Monday and one early on
// Monday both count from Monday.
func (s *Service) judgeFreshness(g Goal, lastCheckin time.Time) Freshness {
	last := lastCheckin
	if last.IsZero() {
		last = g.ActivatedAt
	}
	if last.IsZero() {
		return Freshness{}
	}
	days := s.daysBetween(last, s.clock.Now())
	return Freshness{
		Stale:      g.Lifecycle == LifecycleActive && days > g.CadenceDays,
		LastUpdate: last,
		DaysSince:  days,
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
