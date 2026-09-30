package domain

import (
	"context"
	"slices"
	"time"
)

// Report is the draft Report a Report Definition produces, read against a
// baseline (CONTEXT.md: Report). Exceptions get the full MBR block; every other
// selected Goal takes one line.
type Report struct {
	Definition ReportDefinition
	// Baseline is the date changes are read against: the reader's choice, or 30
	// days ago by default. It is a calendar date in the org's timezone.
	Baseline   time.Time
	Exceptions []ReportBlock
	Lines      []SelectedGoal
}

// ReportBlock is one exception Goal's full MBR block.
type ReportBlock struct {
	Goal   Goal
	Health string
}

// defaultBaselineDays is how far back a Report reads changes when the reader
// picks no baseline.
const defaultBaselineDays = 30

// DraftReport drafts the Report for def against baseline. A zero baseline
// takes the default, 30 days before today.
func (s *Service) DraftReport(ctx context.Context, def ReportDefinition, baseline time.Time) (Report, error) {
	selected, err := s.SelectGoals(ctx, def)
	if err != nil {
		return Report{}, err
	}
	if baseline.IsZero() {
		baseline = orgDate(s.clock.Now(), s.loc).AddDate(0, 0, -defaultBaselineDays)
	} else {
		y, m, d := baseline.Date()
		baseline = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	// since reports whether an instant falls on or after the baseline date in
	// the org's calendar.
	since := func(t time.Time) bool { return !orgDate(t, s.loc).Before(baseline) }
	r := Report{Definition: def, Baseline: baseline}
	for _, sg := range selected {
		h, err := s.goalHistory(ctx, sg.Goal)
		if err != nil {
			return Report{}, err
		}
		if !h.exception(since, s.judgeFreshness(sg.Goal, h.last())) {
			r.Lines = append(r.Lines, sg)
			continue
		}
		r.Exceptions = append(r.Exceptions, ReportBlock{Goal: sg.Goal, Health: sg.Health})
	}
	return r, nil
}

// goalHistory is what a Report reads about one Goal to judge it against the
// baseline: its Check-ins, newest first, and its Date Slips, earliest first.
type goalHistory struct {
	goal     Goal
	checkins []Checkin
	slips    []DateSlip
}

func (s *Service) goalHistory(ctx context.Context, g Goal) (goalHistory, error) {
	checkins, err := s.ListCheckins(ctx, g.ID)
	if err != nil {
		return goalHistory{}, err
	}
	slips, err := s.ListDateSlips(ctx, g.ID)
	if err != nil {
		return goalHistory{}, err
	}
	return goalHistory{goal: g, checkins: checkins, slips: slips}, nil
}

// latest is the Goal's latest Check-in, the zero Checkin when it has none.
func (h goalHistory) latest() Checkin {
	if len(h.checkins) == 0 {
		return Checkin{}
	}
	return h.checkins[0]
}

// last is the latest Check-in as freshness reads it.
func (h goalHistory) last() lastCheckin {
	c := h.latest()
	return lastCheckin{at: c.CreatedAt, health: c.Health, pathTargetDate: c.PathTargetDate}
}

// exception reports whether the Goal earns the full block: it is Red or
// Yellow, Stale, or Ownerless, or since the baseline it was created, slipped a
// date, or changed Lifecycle (CONTEXT.md: Report).
func (h goalHistory) exception(since func(time.Time) bool, fresh Freshness) bool {
	return needsPathToGreen(h.latest().Health) ||
		fresh.Stale ||
		h.goal.Ownerless ||
		since(h.goal.CreatedAt) ||
		slices.ContainsFunc(h.slips, func(d DateSlip) bool { return since(d.CreatedAt) }) ||
		h.lifecycleChanged(since)
}

// lifecycleChanged reports whether the Goal changed Lifecycle since the
// baseline: activated through the activation gate, or moved in a Check-in.
func (h goalHistory) lifecycleChanged(since func(time.Time) bool) bool {
	if !h.goal.ActivatedAt.IsZero() && since(h.goal.ActivatedAt) {
		return true
	}
	return slices.ContainsFunc(h.checkins, func(c Checkin) bool {
		return c.LifecycleChange.Changed() && since(c.CreatedAt)
	})
}
