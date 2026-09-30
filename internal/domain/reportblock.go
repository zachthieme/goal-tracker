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
	// Baseline is the date changes are read against: the reader's choice, or
	// by default the date of the Definition's previous publication, or 30 days
	// ago when it has none. It is a calendar date in the org's timezone.
	Baseline time.Time
	// Previous is the previous publication changes are read against, from the
	// instant it was published; its ID is 0 when they are read against a date.
	Previous PreviousPublication
	// ActionItems are the Definition's open Action Items, soonest due first,
	// shown at the top of the Report until each is closed.
	ActionItems []ActionItem
	Exceptions  []ReportBlock
	Lines       []SelectedGoal
}

// ReportBlock is one exception Goal's full MBR block.
type ReportBlock struct {
	Goal   Goal
	Health string
	// Badges flag what the reader must not miss, in the order BadgeNew,
	// BadgeNewDate, BadgeStale, BadgeOwnerless, BadgeOnHold.
	Badges []string
	// PriorDueDates are the delivery dates the Goal held before each of its
	// Date Slips, earliest first, shown struck through ahead of its current
	// date (~~10/15~~ 11/03).
	PriorDueDates []time.Time
	// Status, PathToGreen, PathTargetDate, and Explanation are the latest
	// Check-in's: its short status, the plan back to Green with its target
	// date, and why the Owner's Health differs from the Rolled-up Health.
	Status         string
	PathToGreen    string
	PathTargetDate time.Time
	Explanation    string
	Milestones     []ReportMilestone
	Metrics        []ReportMetric
	RolledUp       RolledUpHealth
}

// ReportMilestone is a Milestone as an exception block shows it: marked New
// when added since the baseline, Done or Removed by its Status, and with the
// dates it held before its Date Slips to strike through.
type ReportMilestone struct {
	Milestone  Milestone
	New        bool
	PriorDates []time.Time
}

// ReportMetric is a Metric against its target: Current is its latest reading,
// and Read is false when it has none yet.
type ReportMetric struct {
	Metric  Metric
	Current float64
	Read    bool
}

// Badges a ReportBlock can carry. New marks a Goal created since the baseline
// and New Date one whose delivery date slipped since it; the rest mirror the
// Goal's current state (CONTEXT.md: Stale, Ownerless, Lifecycle).
const (
	BadgeNew       = "New"
	BadgeNewDate   = "New Date"
	BadgeStale     = "Stale"
	BadgeOwnerless = "Ownerless"
	BadgeOnHold    = LifecycleOnHold
)

// defaultBaselineDays is how far back a Report reads changes when the reader
// picks no baseline.
const defaultBaselineDays = 30

// DraftReport drafts the Report for def against baseline. A zero baseline
// takes the default: the Definition's previous publication, so the next one
// marks what changed since it, or 30 days before today when there is none.
func (s *Service) DraftReport(ctx context.Context, def ReportDefinition, baseline time.Time) (Report, error) {
	selected, err := s.SelectGoals(ctx, def)
	if err != nil {
		return Report{}, err
	}
	r := Report{Definition: def}
	if r.ActionItems, err = s.OpenActionItems(ctx, def.ID); err != nil {
		return Report{}, err
	}
	if baseline.IsZero() {
		if r.Previous, err = s.previousPublication(ctx, def.ID); err != nil {
			return Report{}, err
		}
	}
	// since reports whether an instant counts as a change: after the previous
	// publication, which already showed anything recorded by then, or on or
	// after the baseline date in the org's calendar.
	var since func(time.Time) bool
	switch {
	case r.Previous.ID != 0:
		r.Baseline = orgDate(r.Previous.PublishedAt, s.loc)
		since = func(t time.Time) bool { return t.After(r.Previous.PublishedAt) }
	case baseline.IsZero():
		r.Baseline = orgDate(s.clock.Now(), s.loc).AddDate(0, 0, -defaultBaselineDays)
	default:
		y, m, d := baseline.Date()
		r.Baseline = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	if since == nil {
		since = func(t time.Time) bool { return !orgDate(t, s.loc).Before(r.Baseline) }
	}
	for _, sg := range selected {
		h, err := s.goalHistory(ctx, sg.Goal)
		if err != nil {
			return Report{}, err
		}
		fresh := s.judgeFreshness(sg.Goal, h.last())
		if !h.exception(since, fresh) {
			r.Lines = append(r.Lines, sg)
			continue
		}
		b, err := s.reportBlock(ctx, h, since, fresh)
		if err != nil {
			return Report{}, err
		}
		r.Exceptions = append(r.Exceptions, b)
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

// badges lists the block's badges in their fixed order.
func (h goalHistory) badges(since func(time.Time) bool, fresh Freshness) []string {
	var out []string
	if since(h.goal.CreatedAt) {
		out = append(out, BadgeNew)
	}
	if slices.ContainsFunc(h.slips, func(d DateSlip) bool { return d.MilestoneID == 0 && since(d.CreatedAt) }) {
		out = append(out, BadgeNewDate)
	}
	if fresh.Stale {
		out = append(out, BadgeStale)
	}
	if h.goal.Ownerless {
		out = append(out, BadgeOwnerless)
	}
	if h.goal.Lifecycle == LifecycleOnHold {
		out = append(out, BadgeOnHold)
	}
	return out
}

// reportBlock builds the full MBR block for an exception Goal.
func (s *Service) reportBlock(ctx context.Context, h goalHistory, since func(time.Time) bool, fresh Freshness) (ReportBlock, error) {
	latest := h.latest()
	b := ReportBlock{
		Goal:           h.goal,
		Health:         latest.Health,
		Badges:         h.badges(since, fresh),
		PriorDueDates:  h.priorDates(0),
		Status:         latest.Status,
		PathToGreen:    latest.PathToGreen,
		PathTargetDate: latest.PathTargetDate,
		Explanation:    latest.Explanation,
	}
	milestones, err := s.ListMilestones(ctx, h.goal.ID)
	if err != nil {
		return ReportBlock{}, err
	}
	for _, m := range milestones {
		b.Milestones = append(b.Milestones, ReportMilestone{
			Milestone:  m,
			New:        since(m.CreatedAt),
			PriorDates: h.priorDates(m.ID),
		})
	}
	metrics, err := s.ListMetrics(ctx, h.goal.ID)
	if err != nil {
		return ReportBlock{}, err
	}
	for _, m := range metrics {
		readings, err := s.ListMetricReadings(ctx, m.ID)
		if err != nil {
			return ReportBlock{}, err
		}
		rm := ReportMetric{Metric: m}
		if len(readings) > 0 {
			rm.Current, rm.Read = readings[len(readings)-1].Value, true
		}
		b.Metrics = append(b.Metrics, rm)
	}
	if b.RolledUp, err = s.RolledUpHealth(ctx, h.goal.ID); err != nil {
		return ReportBlock{}, err
	}
	return b, nil
}

// priorDates returns the dates the delivery date (milestoneID 0) or a
// Milestone held before each of its Date Slips, earliest first.
func (h goalHistory) priorDates(milestoneID int64) []time.Time {
	var out []time.Time
	for _, d := range h.slips {
		if d.MilestoneID == milestoneID {
			out = append(out, d.OldDate)
		}
	}
	return out
}
