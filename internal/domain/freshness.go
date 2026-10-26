package domain

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Freshness is how current a Goal's updates are (CONTEXT.md: Check-in
// overdue). It is computed when read, from the Goal's last Check-in and the
// calendar of its Owner's Timezone (see Service.GoalTimezone).
type Freshness struct {
	// CheckinOverdue is true when the Goal is Active and its last Check-in — or
	// its activation, if it has none — is more than its cadence of days ago. On
	// Hold, Proposed, Done, and Cancelled Goals are never Check-in overdue.
	CheckinOverdue bool
	// LastUpdate is the Goal's last Check-in, or its activation when it has
	// none (CheckedIn is false). It is the zero time for a Goal that was never
	// activated.
	LastUpdate time.Time
	CheckedIn  bool
	// DaysSince is how many days of the Goal's Owner's Timezone (see
	// Service.GoalTimezone) have passed since LastUpdate, judged against
	// CadenceDays, the Goal's Check-in cadence.
	DaysSince   int
	CadenceDays int
	// PathToGreenOverdue is true when the Goal is Active, isn't Green, and the
	// target date of its Path to Green (PathTargetDate) has passed (CONTEXT.md:
	// Path to Green).
	PathToGreenOverdue bool
	// PathTargetDate is the date the latest Path to Green aims to be back at
	// Green by, the zero time when the Goal is Green or has no Check-in.
	PathTargetDate time.Time
	// MilestoneOverdue is true when the Goal is Active, its latest Check-in is
	// Green, and one of its Milestones is overdue: still Planned past its date
	// in the Goal's Owner's Timezone (see Service.GoalTimezone). The Health
	// stays the Owner's (CONTEXT.md: Milestone). OverdueMilestone and
	// OverdueMilestoneDate are that Milestone's name and date, the
	// earliest-dated when several are overdue. Every Freshness is judged
	// whole, from the Goal's Milestones too (judgeFreshness), so these three
	// are always answered.
	MilestoneOverdue     bool
	OverdueMilestone     string
	OverdueMilestoneDate time.Time
}

// reminderIntervalDays is how far apart the weekly Check-in reminders go out,
// in days. It is added to Freshness.DaysSince, so it counts days of the Goal's
// Owner's Timezone (see Service.GoalTimezone) as that does.
const reminderIntervalDays = 7

// DueBeforeNextReminder reports whether the Goal's Check-in is due: it is
// Check-in overdue already, or it would be before next week's reminder if
// nobody checked in (CONTEXT.md: Check-in overdue). The weekly reminder and the
// Home page both list the Goals it holds for.
func (f Freshness) DueBeforeNextReminder() bool {
	return f.CheckinOverdue || f.DaysSince+reminderIntervalDays > f.CadenceDays
}

// LastCheckinAge says, as the emails put it, how long ago the Goal's last
// Check-in was, or its activation when it has none, and how often one is due:
// "last Check-in 9 days ago (every 7 days)", "no Check-in since activation 10
// days ago (every 7 days)".
func (f Freshness) LastCheckinAge() string {
	ago := "today"
	switch {
	case f.DaysSince == 1:
		ago = "1 day ago"
	case f.DaysSince > 1:
		ago = fmt.Sprintf("%d days ago", f.DaysSince)
	}
	every := "every day"
	if f.CadenceDays != 1 {
		every = fmt.Sprintf("every %d days", f.CadenceDays)
	}
	if !f.CheckedIn {
		return fmt.Sprintf("no Check-in since activation %s (%s)", ago, every)
	}
	return fmt.Sprintf("last Check-in %s (%s)", ago, every)
}

// Freshness reads goalID's freshness signals.
func (s *Service) Freshness(ctx context.Context, goalID int64) (Freshness, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return Freshness{}, err
	}
	byGoal, err := s.FreshnessByGoal(ctx, []Goal{g})
	if err != nil {
		return Freshness{}, err
	}
	return byGoal[g.ID], nil
}

// FreshnessByGoal reads the freshness signals of each of goals, as Freshness
// reads one Goal's, keyed by Goal ID. It runs the same few queries however many
// Goals there are, so a page listing Goals can afford it.
func (s *Service) FreshnessByGoal(ctx context.Context, goals []Goal) (map[int64]Freshness, error) {
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	latest, err := s.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	last := make(map[int64]lastCheckin, len(goals))
	for _, g := range goals {
		if c, ok := latest[g.ID]; ok {
			last[g.ID] = lastCheckin{at: c.CreatedAt, health: c.Health, pathTargetDate: c.PathTargetDate}
		}
	}
	return s.freshnessGivenLast(ctx, goals, last)
}

// freshnessGivenLast is FreshnessByGoal given last, each of goals' latest
// Check-in by Goal ID (none for a Goal with no Check-in). It reads the
// Milestones of the Goals that could be Milestone overdue in one query.
func (s *Service) freshnessGivenLast(ctx context.Context, goals []Goal, last map[int64]lastCheckin) (map[int64]Freshness, error) {
	var green []int64
	for _, g := range goals {
		if greenWhileActive(g, last[g.ID].health) {
			green = append(green, g.ID)
		}
	}
	// Only a Green, Active Goal can be Milestone overdue, so only theirs are read.
	milestones, err := s.milestonesByGoal(ctx, green)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Freshness, len(goals))
	for _, g := range goals {
		out[g.ID] = s.judgeFreshness(g, last[g.ID], milestones[g.ID])
	}
	return out, nil
}

// greenWhileActive reports whether g is Active with a latest Check-in of the
// given Health that is Green: the only Goal that can be marked Milestone
// overdue.
func greenWhileActive(g Goal, health string) bool {
	return g.Lifecycle == LifecycleActive && health == HealthGreen
}

// markMilestoneOverdue sets f's Milestone overdue fields (see
// Freshness.MilestoneOverdue) for g, whose latest Check-in's Health is health,
// from g's milestones sorted by date, judging each by Milestone.OverdueOn as of
// today in g's timezone, as the Check-in refusals judge it. Only
// judgeFreshness calls it.
func (s *Service) markMilestoneOverdue(f *Freshness, g Goal, health string, milestones []Milestone) {
	if !greenWhileActive(g, health) {
		return
	}
	m, ok := overdueMilestone(milestones, s.goalToday(g))
	if !ok {
		return
	}
	f.MilestoneOverdue = true
	f.OverdueMilestone = m.Name
	f.OverdueMilestoneDate = m.TargetDate
}

// GoalFreshness is a Goal with its freshness signals and the Health of its
// latest Check-in ("" when it has none, or that Check-in set none).
type GoalFreshness struct {
	Goal      Goal
	Freshness Freshness
	Health    string
}

// FreshnessSignals are the org-wide freshness signals: every Check-in overdue
// Goal, longest since its last update first, and every Goal whose Path to Green
// is overdue, longest overdue first. Both surface as prominently as Red.
// NotGreen is every Active Goal whose latest Check-in is Red or Yellow, in the
// order the Active Goals are read. OverdueMilestones is every Goal marked
// Milestone overdue (see Freshness.MilestoneOverdue), earliest Milestone date
// first.
type FreshnessSignals struct {
	CheckinOverdue    []GoalFreshness
	OverduePaths      []GoalFreshness
	NotGreen          []GoalFreshness
	OverdueMilestones []GoalFreshness
}

// FreshnessSignals reads the freshness signals of every Active Goal: its
// Check-in overdue and overdue-Path-to-Green Goals, its Not Green ones, whose
// latest Check-in is Red or Yellow, and its Green ones with an overdue
// Milestone.
func (s *Service) FreshnessSignals(ctx context.Context) (FreshnessSignals, error) {
	rows, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return FreshnessSignals{}, fmt.Errorf("list active goals: %w", err)
	}
	overdue, err := s.listFreshnessOverdueMilestones(ctx)
	if err != nil {
		return FreshnessSignals{}, err
	}
	return s.freshnessSignalsAmong(rows, overdue), nil
}

// listFreshnessOverdueMilestones reads the Planned Milestones any Active Goal
// could be Milestone overdue by. Each Goal's today is its Owner's (see
// GoalTimezone), never later than the day after the UTC date, so reading the
// Milestones due before that misses none; judgeFreshness then judges each in
// its Goal's today.
func (s *Service) listFreshnessOverdueMilestones(ctx context.Context) ([]db.Milestone, error) {
	overdue, err := s.queries.ListOverdueMilestones(ctx, s.clock.Now().UTC().AddDate(0, 0, 1).Format(dateFormat))
	if err != nil {
		return nil, fmt.Errorf("list overdue milestones: %w", err)
	}
	return overdue, nil
}

// freshnessSignalsAmong is FreshnessSignals over rows, every Active Goal with
// its latest Check-in, given overdue, as listFreshnessOverdueMilestones reads
// them.
func (s *Service) freshnessSignalsAmong(rows []db.ListActiveGoalsWithLatestCheckinRow, overdue []db.Milestone) FreshnessSignals {
	milestones := freshnessMilestonesByGoal(overdue)
	var out FreshnessSignals
	for _, r := range rows {
		g := goalFromRow(r.Goal, r.Account)
		gf := GoalFreshness{Goal: g, Freshness: s.judgeFreshness(g, lastCheckinFromRow(r), milestones[g.ID]), Health: r.CheckinHealth}
		if gf.Freshness.MilestoneOverdue {
			out.OverdueMilestones = append(out.OverdueMilestones, gf)
		}
		if gf.Freshness.CheckinOverdue {
			out.CheckinOverdue = append(out.CheckinOverdue, gf)
		}
		if gf.Freshness.PathToGreenOverdue {
			out.OverduePaths = append(out.OverduePaths, gf)
		}
		if gf.Health == HealthRed || gf.Health == HealthYellow {
			out.NotGreen = append(out.NotGreen, gf)
		}
	}
	slices.SortStableFunc(out.CheckinOverdue, func(a, b GoalFreshness) int {
		return a.Freshness.LastUpdate.Compare(b.Freshness.LastUpdate)
	})
	slices.SortStableFunc(out.OverduePaths, func(a, b GoalFreshness) int {
		return a.Freshness.PathTargetDate.Compare(b.Freshness.PathTargetDate)
	})
	slices.SortStableFunc(out.OverdueMilestones, func(a, b GoalFreshness) int {
		return a.Freshness.OverdueMilestoneDate.Compare(b.Freshness.OverdueMilestoneDate)
	})
	return out
}

// freshnessMilestonesByGoal keys overdue, as listFreshnessOverdueMilestones
// reads them, by Goal ID, each Goal's earliest first.
func freshnessMilestonesByGoal(overdue []db.Milestone) map[int64][]Milestone {
	out := map[int64][]Milestone{}
	for _, m := range overdue {
		out[m.GoalID] = append(out[m.GoalID], milestoneFromRow(m))
	}
	return out
}

// PersonalGoal is an Active Goal someone Owns or is a Delegate on, with what
// their Home page reads of it: its freshness signals and the Health of its
// latest Check-in ("" when it has none, or that Check-in set none).
type PersonalGoal struct {
	Goal      Goal
	Freshness Freshness
	Health    string
	// CheckedIn is true when the Goal has a Check-in to repeat unchanged.
	CheckedIn bool
	// AsDelegate is true when the person is the Goal's Delegate, not its Owner.
	AsDelegate bool
}

// ActiveGoalsFor reads every Active Goal accountID Owns or is a Delegate on,
// longest since its last update first.
func (s *Service) ActiveGoalsFor(ctx context.Context, accountID int64) ([]PersonalGoal, error) {
	delegated, err := s.DelegatedGoals(ctx, accountID)
	if err != nil {
		return nil, err
	}
	isDelegate := make(map[int64]bool, len(delegated))
	for _, g := range delegated {
		isDelegate[g.ID] = true
	}
	rows, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active goals: %w", err)
	}
	overdue, err := s.listFreshnessOverdueMilestones(ctx)
	if err != nil {
		return nil, err
	}
	return s.personalGoalsAmong(rows, overdue, accountID, isDelegate), nil
}

// personalGoalsAmong is ActiveGoalsFor over rows, every Active Goal with its
// latest Check-in, given overdue, as listFreshnessOverdueMilestones reads
// them, and isDelegate, the IDs of the Goals accountID is a Delegate on.
func (s *Service) personalGoalsAmong(rows []db.ListActiveGoalsWithLatestCheckinRow, overdue []db.Milestone, accountID int64, isDelegate map[int64]bool) []PersonalGoal {
	milestones := freshnessMilestonesByGoal(overdue)
	var out []PersonalGoal
	for _, r := range rows {
		owns := r.Goal.OwnerID == accountID
		if !owns && !isDelegate[r.Goal.ID] {
			continue
		}
		g := goalFromRow(r.Goal, r.Account)
		last := lastCheckinFromRow(r)
		out = append(out, PersonalGoal{
			Goal:       g,
			Freshness:  s.judgeFreshness(g, last, milestones[g.ID]),
			Health:     r.CheckinHealth,
			CheckedIn:  !last.at.IsZero(),
			AsDelegate: !owns,
		})
	}
	slices.SortStableFunc(out, func(a, b PersonalGoal) int {
		return a.Freshness.LastUpdate.Compare(b.Freshness.LastUpdate)
	})
	return out
}

// lastCheckin is what judging freshness reads from a Goal's latest Check-in:
// when it was written, its Health, and its Path to Green's target date. It is
// the zero value when the Goal has no Check-in.
type lastCheckin struct {
	at             time.Time
	health         string
	pathTargetDate time.Time
}

// lastCheckinFromRow reads the latest Check-in an Active Goal row carries.
func lastCheckinFromRow(r db.ListActiveGoalsWithLatestCheckinRow) lastCheckin {
	var last lastCheckin
	last.at, _ = time.Parse(timeFormat, r.CheckinCreatedAt)
	last.health = r.CheckinHealth
	last.pathTargetDate, _ = time.Parse(dateFormat, r.CheckinPathTargetDate)
	return last
}

// judgeFreshness is the whole of g's Freshness, given its latest Check-in and
// milestones, its Milestones sorted by date: at least those still Planned past
// their date, or none when it can't be Milestone overdue (greenWhileActive).
// It applies the Check-in overdue, overdue-Path-to-Green and Milestone overdue
// rules. Days are counted on the calendar of g's Owner's Timezone (see
// GoalTimezone), so a Check-in late on Monday and one early on Monday both
// count from Monday, and a Path to Green is overdue from the day after its
// target date. A count that needs only one rule's answer, and no Milestones,
// asks checkinOverdueSince or pathToGreenPastTarget instead.
func (s *Service) judgeFreshness(g Goal, latest lastCheckin, milestones []Milestone) Freshness {
	last := lastUpdateOf(g, latest)
	if last.IsZero() {
		return Freshness{}
	}
	now := s.clock.Now()
	loc := s.GoalTimezone(g)
	days := daysBetween(last, now, loc)
	target := latest.pathTargetDate
	f := Freshness{
		CheckinOverdue:     checkinOverdueAfter(g, days),
		LastUpdate:         last,
		CheckedIn:          !latest.at.IsZero(),
		DaysSince:          days,
		CadenceDays:        g.CadenceDays,
		PathToGreenOverdue: pathToGreenPastTarget(g.Lifecycle, latest.health, target, now, loc),
		PathTargetDate:     target,
	}
	s.markMilestoneOverdue(&f, g, latest.health, milestones)
	return f
}

// lastUpdateOf is g's last update, given its latest Check-in: that Check-in,
// or g's activation when it has none, the zero time when it was never
// activated (Freshness.LastUpdate).
func lastUpdateOf(g Goal, latest lastCheckin) time.Time {
	if latest.at.IsZero() {
		return g.ActivatedAt
	}
	return latest.at
}

// checkinOverdueSince is the Check-in overdue rule alone (see
// Freshness.CheckinOverdue): whether g, whose latest Check-in was written at
// lastCheckinAt (the zero time when it has none), is Check-in overdue now. A
// count of Check-in overdue Goals asks it rather than judging a whole
// Freshness.
func (s *Service) checkinOverdueSince(g Goal, lastCheckinAt time.Time) bool {
	last := lastUpdateOf(g, lastCheckin{at: lastCheckinAt})
	if last.IsZero() {
		return false
	}
	return checkinOverdueAfter(g, daysBetween(last, s.clock.Now(), s.GoalTimezone(g)))
}

// checkinOverdueAfter is whether g is Check-in overdue when days of its
// Owner's Timezone have passed since its last update: it is Active and more
// than its cadence has.
func checkinOverdueAfter(g Goal, days int) bool {
	return g.Lifecycle == LifecycleActive && days > g.CadenceDays
}

// pathToGreenPastTarget is the overdue-Path-to-Green rule: a Goal of the
// given Lifecycle and Health has missed its Path to Green's target date as of
// asOf when it is Active, isn't Green, has a target, and asOf falls on a later
// date in loc, the Goal's Owner's Timezone — so overdue from the day after the
// target (CONTEXT.md: Path to Green). Freshness and a Report block's
// PathToGreenOverdueAt both judge by it.
func pathToGreenPastTarget(lifecycle, health string, target, asOf time.Time, loc *time.Location) bool {
	if lifecycle != LifecycleActive || !needsPathToGreen(health) || target.IsZero() {
		return false
	}
	return orgDate(asOf, loc).After(orgDate(target, target.Location()))
}

// daysBetween counts the calendar days from one instant to a later one, each
// read as a date in loc, the Goal's Owner's Timezone (see GoalTimezone).
func daysBetween(from, to time.Time, loc *time.Location) int {
	return int(orgDate(to, loc).Sub(orgDate(from, loc)) / (24 * time.Hour))
}

// orgDate is the calendar date t falls on in loc, as midnight UTC so that dates
// subtract to whole days whatever the timezone's daylight-saving rules.
func orgDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
