package domain

import (
	"context"
	"fmt"
	"slices"
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
	// LastUpdate, judged against CadenceDays, the Goal's Check-in cadence.
	DaysSince   int
	CadenceDays int
	// PathToGreenOverdue is true when the Goal is Active, isn't Green, and the
	// target date of its Path to Green (PathTargetDate) has passed (CONTEXT.md:
	// Path to Green).
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
	latest, ok, err := s.LatestCheckin(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	var last lastCheckin
	if ok {
		last = lastCheckin{at: latest.CreatedAt, health: latest.Health, pathTargetDate: latest.PathTargetDate}
	}
	return s.judgeFreshness(g, last), nil
}

// GoalFreshness is a Goal with its freshness signals.
type GoalFreshness struct {
	Goal      Goal
	Freshness Freshness
}

// FreshnessSignals are the org-wide freshness signals: every Stale Goal,
// longest since its last update first, and every Goal whose Path to Green is
// overdue, longest overdue first. Both surface as prominently as Red.
type FreshnessSignals struct {
	Stale        []GoalFreshness
	OverduePaths []GoalFreshness
}

// FreshnessSignals reads the freshness signals of every Active Goal.
func (s *Service) FreshnessSignals(ctx context.Context) (FreshnessSignals, error) {
	rows, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return FreshnessSignals{}, fmt.Errorf("list active goals: %w", err)
	}
	var out FreshnessSignals
	for _, r := range rows {
		g := goalFromRow(r.Goal, r.Account)
		var last lastCheckin
		last.at, _ = time.Parse(timeFormat, r.CheckinCreatedAt)
		last.health = r.CheckinHealth
		last.pathTargetDate, _ = time.Parse(dateFormat, r.CheckinPathTargetDate)
		gf := GoalFreshness{Goal: g, Freshness: s.judgeFreshness(g, last)}
		if gf.Freshness.Stale {
			out.Stale = append(out.Stale, gf)
		}
		if gf.Freshness.PathToGreenOverdue {
			out.OverduePaths = append(out.OverduePaths, gf)
		}
	}
	slices.SortStableFunc(out.Stale, func(a, b GoalFreshness) int {
		return a.Freshness.LastUpdate.Compare(b.Freshness.LastUpdate)
	})
	slices.SortStableFunc(out.OverduePaths, func(a, b GoalFreshness) int {
		return a.Freshness.PathTargetDate.Compare(b.Freshness.PathTargetDate)
	})
	return out, nil
}

// lastCheckin is what judging freshness reads from a Goal's latest Check-in:
// when it was written, its Health, and its Path to Green's target date. It is
// the zero value when the Goal has no Check-in.
type lastCheckin struct {
	at             time.Time
	health         string
	pathTargetDate time.Time
}

// judgeFreshness applies the Stale and overdue-Path-to-Green rules to g, given
// its latest Check-in. Days are
// counted on the org's calendar, so a Check-in late on Monday and one early on
// Monday both count from Monday, and a Path to Green is overdue from the day
// after its target date.
func (s *Service) judgeFreshness(g Goal, latest lastCheckin) Freshness {
	last := latest.at
	if last.IsZero() {
		last = g.ActivatedAt
	}
	if last.IsZero() {
		return Freshness{}
	}
	now := s.clock.Now()
	active := g.Lifecycle == LifecycleActive
	days := s.daysBetween(last, now)
	target := latest.pathTargetDate
	return Freshness{
		Stale:              active && days > g.CadenceDays,
		LastUpdate:         last,
		DaysSince:          days,
		CadenceDays:        g.CadenceDays,
		PathToGreenOverdue: active && needsPathToGreen(latest.health) && !target.IsZero() && orgDate(now, s.loc).After(target),
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
