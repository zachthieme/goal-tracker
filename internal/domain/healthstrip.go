package domain

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// healthStripPeriods is how many Check-in periods a Health strip covers.
const healthStripPeriods = 11

// HealthStrip is a Goal's Health over its last Check-in periods, oldest first
// and the current period last. It is read from the Goal's Check-ins and
// Lifecycle and changes nothing: not Stale, not Health. It has no periods for a
// Goal that has never been Active.
type HealthStrip struct {
	// CadenceDays is how long each period is: the Goal's Check-in cadence.
	CadenceDays int
	Periods     []HealthPeriod
}

// HealthPeriod is one Check-in period on a Health strip.
type HealthPeriod struct {
	// First and Last are the period's first and last days in the org's
	// calendar, as calendar dates (midnight UTC).
	First, Last time.Time
	// Health is the Health of the last Check-in made in the period, "" when
	// there was none or it left the Goal out of Active.
	Health string
	// NoCheckin is true when the Goal was Active in a finished period and nobody
	// checked in on it.
	NoCheckin bool
	// NotYetDue is true when the Goal is Active in a period that hasn't ended,
	// its last day today or later, and nobody has checked in on it yet: it can't
	// have been missed.
	NotYetDue bool
	// Lifecycle is the Lifecycle the Goal was in at the end of the period, or
	// now for the current one: Proposed for a period before it became Active.
	Lifecycle string
}

// HealthStrip reads goalID's Health strip.
func (s *Service) HealthStrip(ctx context.Context, goalID int64) (HealthStrip, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return HealthStrip{}, err
	}
	checkins, err := s.ListCheckins(ctx, goalID)
	if err != nil {
		return HealthStrip{}, err
	}
	return s.healthStrip(g, checkins), nil
}

// healthStrip lays g's Check-ins over its last periods. The periods are counted
// in days of the org's calendar, as Stale is, each the Goal's cadence long, and
// the current one ends on the Sunday that closes this week, so a weekly Goal's
// periods are the weeks its History is grouped by.
func (s *Service) healthStrip(g Goal, checkins []Checkin) HealthStrip {
	if g.ActivatedAt.IsZero() {
		return HealthStrip{}
	}
	cadence := max(g.CadenceDays, 1)
	byTime := slices.Clone(checkins)
	slices.SortFunc(byTime, func(a, b Checkin) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})

	today := orgDate(s.clock.Now(), s.loc)
	end := today.AddDate(0, 0, (7-int(today.Weekday()))%7)
	activated := orgDate(g.ActivatedAt, s.loc)
	out := HealthStrip{CadenceDays: cadence}
	for i := healthStripPeriods - 1; i >= 0; i-- {
		last := end.AddDate(0, 0, -cadence*i)
		p := HealthPeriod{First: last.AddDate(0, 0, 1-cadence), Last: last, Lifecycle: LifecycleProposed}
		// The Goal's Lifecycle as the period opened: Active once activated, then
		// moved by each earlier Check-in's Lifecycle change.
		if activated.Before(p.First) {
			p.Lifecycle = LifecycleActive
		}
		for _, c := range byTime {
			if orgDate(c.CreatedAt, s.loc).Before(p.First) {
				p.Lifecycle = c.LifecycleChange.resultingLifecycle(p.Lifecycle)
			}
		}
		// It was Active in the period if it was as the period opened or was
		// activated during it. Resuming from On Hold takes a Check-in, so a
		// period it resumed in always has one.
		active := p.Lifecycle == LifecycleActive
		if !activated.Before(p.First) && !activated.After(p.Last) {
			active, p.Lifecycle = true, LifecycleActive
		}
		checkedIn := false
		for _, c := range byTime {
			on := orgDate(c.CreatedAt, s.loc)
			if on.Before(p.First) || on.After(p.Last) {
				continue
			}
			// The last Check-in decides: one that leaves the Goal out of Active
			// sets no Health, so the period is blank.
			checkedIn = true
			p.Health = c.Health
			p.Lifecycle = c.LifecycleChange.resultingLifecycle(p.Lifecycle)
		}
		// Only a finished period can have gone without a Check-in.
		if active && !checkedIn {
			if p.Last.Before(today) {
				p.NoCheckin = true
			} else {
				p.NotYetDue = true
			}
		}
		out.Periods = append(out.Periods, p)
	}
	return out
}
