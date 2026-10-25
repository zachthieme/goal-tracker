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
	// earliest-dated when several are overdue. Only Service.Freshness,
	// Service.FreshnessByGoal and Service.FreshnessSignals set these three.
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
	var green []int64
	for _, g := range goals {
		if c, ok := latest[g.ID]; ok {
			last[g.ID] = lastCheckin{at: c.CreatedAt, health: c.Health, pathTargetDate: c.PathTargetDate}
		}
		if greenWhileActive(g, last[g.ID].health) {
			green = append(green, g.ID)
		}
	}
	// Only a Green, Active Goal can be Milestone overdue, so only theirs are read.
	milestones := map[int64][]db.Milestone{}
	if len(green) > 0 {
		rows, err := s.queries.ListMilestonesForGoals(ctx, green)
		if err != nil {
			return nil, fmt.Errorf("list milestones: %w", err)
		}
		for _, m := range rows {
			milestones[m.GoalID] = append(milestones[m.GoalID], m)
		}
	}
	out := make(map[int64]Freshness, len(goals))
	for _, g := range goals {
		f := s.judgeFreshness(g, last[g.ID])
		s.markMilestoneOverdue(&f, g, last[g.ID].health, milestones[g.ID])
		out[g.ID] = f
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
// from g's milestones sorted by date, judging overdue as overdueMilestone does
// as of today in g's timezone, as the Check-in refusals judge it.
func (s *Service) markMilestoneOverdue(f *Freshness, g Goal, health string, milestones []db.Milestone) {
	if !greenWhileActive(g, health) {
		return
	}
	m, ok := overdueMilestone(milestones, s.goalToday(g).Format(dateFormat))
	if !ok {
		return
	}
	f.MilestoneOverdue = true
	f.OverdueMilestone = m.Name
	f.OverdueMilestoneDate, _ = time.Parse(dateFormat, m.TargetDate)
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
// Milestones due before that misses none; markMilestoneOverdue then judges
// each in its Goal's today.
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
	milestones := map[int64][]db.Milestone{}
	for _, m := range overdue {
		milestones[m.GoalID] = append(milestones[m.GoalID], m)
	}
	var out FreshnessSignals
	for _, r := range rows {
		g := goalFromRow(r.Goal, r.Account)
		gf := GoalFreshness{Goal: g, Freshness: s.judgeFreshness(g, lastCheckinFromRow(r)), Health: r.CheckinHealth}
		s.markMilestoneOverdue(&gf.Freshness, g, gf.Health, milestones[g.ID])
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
	return s.personalGoalsAmong(rows, accountID, isDelegate), nil
}

// personalGoalsAmong is ActiveGoalsFor over rows, every Active Goal with its
// latest Check-in, given isDelegate, the IDs of the Goals accountID is a
// Delegate on.
func (s *Service) personalGoalsAmong(rows []db.ListActiveGoalsWithLatestCheckinRow, accountID int64, isDelegate map[int64]bool) []PersonalGoal {
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
			Freshness:  s.judgeFreshness(g, last),
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

// judgeFreshness applies the Check-in overdue and overdue-Path-to-Green rules
// to g, given its latest Check-in. Days are counted on the calendar of g's
// Owner's Timezone (see GoalTimezone), so a Check-in late on Monday and one
// early on Monday both count from Monday, and a Path to Green is overdue from
// the day after its target date.
func (s *Service) judgeFreshness(g Goal, latest lastCheckin) Freshness {
	last := latest.at
	if last.IsZero() {
		last = g.ActivatedAt
	}
	if last.IsZero() {
		return Freshness{}
	}
	now := s.clock.Now()
	loc := s.GoalTimezone(g)
	active := g.Lifecycle == LifecycleActive
	days := daysBetween(last, now, loc)
	target := latest.pathTargetDate
	return Freshness{
		CheckinOverdue:     active && days > g.CadenceDays,
		LastUpdate:         last,
		CheckedIn:          !latest.at.IsZero(),
		DaysSince:          days,
		CadenceDays:        g.CadenceDays,
		PathToGreenOverdue: active && needsPathToGreen(latest.health) && !target.IsZero() && orgDate(now, loc).After(target),
		PathTargetDate:     target,
	}
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
