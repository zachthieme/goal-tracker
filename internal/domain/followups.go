package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// FollowUps is what the Follow-ups page shows one viewer, read by
// Service.FollowUps (ADR 0010): the company's flagged Goals, whatever the
// page's scope, since Needs an Admin holds the company's rows, with what the
// rows' Fixes turn on.
type FollowUps struct {
	// Rows are the flagged Goals, one each with every signal that flags it,
	// most urgent first (see sortFollowUpRows).
	Rows []FollowUpRow
	// Goals are every Goal, as ListGoals lists them, for the page's Scope.
	Goals []Goal
	// Delegated holds the IDs of the Goals the viewer is a Delegate on
	// (DelegatedGoals).
	Delegated map[int64]bool
}

// FollowUpRow is one flagged Goal with every signal that flags it, so a Goal
// flagged twice is one row, not two.
type FollowUpRow struct {
	Goal Goal
	// Health is the Goal's latest Check-in's Health, empty when it has none.
	Health string
	// RunOffGreen is how long a Not Green row's Goal has been Yellow or Red;
	// it isn't running on any other row (CONTEXT.md: Run off Green).
	RunOffGreen RunOffGreen
	// PathTargetDate is a Not Green row's back-to-Green date, its latest
	// Check-in's, and PathPriorDates the dates it slipped from within the Run
	// off Green, earliest first (see BackToGreenPriorDates). Only a Not Green
	// row has them.
	PathTargetDate time.Time
	PathPriorDates []time.Time
	Signals        []FollowUpSignal
	// NudgedToday is the Goal's Nudge today, nil when nobody has nudged it
	// today (NudgedToday).
	NudgedToday *Nudge
	// Unreachable is whether nobody is left to check in on the Goal: it is
	// Ownerless and none of its Delegates is still in the company.
	Unreachable bool
}

// FollowUpSignal is one reason a Goal is flagged. Kind names the signal, as
// the chip's data-kind on the Follow-ups page. Days is how long the signal has
// held: for "checkin-overdue" the days since the last update, for
// "path-overdue" the days past the Path to Green's target date, for
// "milestone-overdue" the days past the earliest overdue Milestone's date, for
// "action-item-overdue" the days the most overdue Action Item about the Goal
// is past its due date, for "schedule-conflicts" the days the child's delivery
// date falls after the parent's, and 0 otherwise. Related is the parent for
// "schedule-conflicts" and "halted-parents". Person is whom an
// "asks-for-help" asks, nil when no one is above the Owner.
type FollowUpSignal struct {
	Kind    string
	Days    int
	Related *Goal
	Person  *Account
}

// Signal is the row's first signal of kind, and whether it has one.
func (r FollowUpRow) Signal(kind string) (FollowUpSignal, bool) {
	for _, sig := range r.Signals {
		if sig.Kind == kind {
			return sig, true
		}
	}
	return FollowUpSignal{}, false
}

// FollowUps reads the Follow-ups page for viewerID, reading each thing once
// (ADR 0010): every Goal the company's freshness and graph signals, an
// Ownerless Goal, an open Ask for help or an overdue Action Item flags, with
// the Goals the viewer is a Delegate on.
func (s *Service) FollowUps(ctx context.Context, viewerID int64) (FollowUps, error) {
	active, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return FollowUps{}, fmt.Errorf("list active goals: %w", err)
	}
	overdue, err := s.listFreshnessOverdueMilestones(ctx)
	if err != nil {
		return FollowUps{}, err
	}
	reads, err := s.readFollowUpSignals(ctx, active, overdue)
	if err != nil {
		return FollowUps{}, err
	}
	if reads.asks, err = s.OpenAsksForHelp(ctx); err != nil {
		return FollowUps{}, err
	}
	rows := s.followUpRowsAmong(reads)
	if err := s.readFollowUpRows(ctx, rows); err != nil {
		return FollowUps{}, err
	}
	sortFollowUpRows(rows)
	out := FollowUps{Rows: rows, Delegated: map[int64]bool{}}
	if out.Goals, err = s.ListGoals(ctx); err != nil {
		return FollowUps{}, err
	}
	delegated, err := s.DelegatedGoals(ctx, viewerID)
	if err != nil {
		return FollowUps{}, fmt.Errorf("load delegated goals: %w", err)
	}
	for _, g := range delegated {
		out.Delegated[g.ID] = true
	}
	return out, nil
}

// readFollowUpRows reads what rows show beyond their signals: each one's
// Health when its Goal isn't Active, an Active one's having come with it,
// each Not Green one's Run off Green and back-to-Green slips, each one's
// Nudge today, and whether each Ownerless one has anyone left to check in
// on it.
func (s *Service) readFollowUpRows(ctx context.Context, rows []FollowUpRow) error {
	ids := make([]int64, 0, len(rows))
	var inactiveIDs, notGreenIDs, ownerlessIDs []int64
	var notGreen []Goal
	for _, r := range rows {
		ids = append(ids, r.Goal.ID)
		if r.Goal.Lifecycle != LifecycleActive {
			inactiveIDs = append(inactiveIDs, r.Goal.ID)
		}
		_, red := r.Signal("red")
		_, yellow := r.Signal("yellow")
		if red || yellow {
			notGreen = append(notGreen, r.Goal)
			notGreenIDs = append(notGreenIDs, r.Goal.ID)
		}
		if r.Goal.Ownerless {
			ownerlessIDs = append(ownerlessIDs, r.Goal.ID)
		}
	}
	latest, err := s.LatestCheckinsByGoal(ctx, inactiveIDs)
	if err != nil {
		return fmt.Errorf("load flagged goals' check-ins: %w", err)
	}
	runs, err := s.RunOffGreenByGoal(ctx, notGreen)
	if err != nil {
		return fmt.Errorf("read not green goals' run off green: %w", err)
	}
	slips, err := s.DateSlipsByGoal(ctx, notGreenIDs)
	if err != nil {
		return fmt.Errorf("load not green goals' date slips: %w", err)
	}
	delegates, err := s.ListDelegatesForGoals(ctx, ownerlessIDs)
	if err != nil {
		return err
	}
	nudged, err := s.NudgedToday(ctx, ids)
	if err != nil {
		return err
	}
	for i := range rows {
		r := &rows[i]
		id := r.Goal.ID
		if c, ok := latest[id]; ok {
			r.Health = c.Health
		}
		if n, ok := nudged[id]; ok {
			r.NudgedToday = &n
		}
		if r.Goal.Ownerless {
			r.Unreachable = !slices.ContainsFunc(delegates[id], func(a Account) bool { return !a.Departed })
		}
		run, ok := runs[id]
		if !ok {
			continue
		}
		r.RunOffGreen = run
		r.PathPriorDates = BackToGreenPriorDates(slips[id], run)
	}
	return nil
}

// followUpReads are the reads which Goals Follow-ups flags is decided from:
// every Active Goal with its latest Check-in, the Planned Milestones any of
// them could be overdue by (listFreshnessOverdueMilestones), every accepted
// link's edge, the open Action Items about a Goal due before today, and the
// open Asks for help.
type followUpReads struct {
	active            []db.ListActiveGoalsWithLatestCheckinRow
	overdueMilestones []db.Milestone
	edges             []edge
	overdueItems      []db.ListOverdueActionItemsRow
	asks              []AskForHelp
}

// readFollowUpSignals reads, beside active, every Active Goal with its latest
// Check-in, and overdue, the Planned Milestones any of them could be overdue
// by, the rest of followUpReads but the open Asks for help.
func (s *Service) readFollowUpSignals(ctx context.Context, active []db.ListActiveGoalsWithLatestCheckinRow, overdue []db.Milestone) (followUpReads, error) {
	reads := followUpReads{active: active, overdueMilestones: overdue}
	var err error
	if reads.edges, err = s.acceptedEdges(ctx); err != nil {
		return followUpReads{}, err
	}
	if reads.overdueItems, err = s.queries.ListOverdueActionItems(ctx, orgDate(s.clock.Now(), s.loc).Format(dateFormat)); err != nil {
		return followUpReads{}, fmt.Errorf("list overdue action items: %w", err)
	}
	return reads, nil
}

// followUpRowsAmong merges the signals reads hold into one row per flagged
// Goal, unsorted, reading nothing: the Not Green Goals, the Asks for help,
// the Check-in overdue, Path to Green overdue and Milestone overdue Goals,
// the Active Goals with an overdue Action Item, the Ownerless and Unlinked
// Active Goals, then the children in a schedule conflict or under a halted
// parent. An Active row's Health, and a Not Green one's back-to-Green date,
// are its latest Check-in's, as active holds it; an inactive row's are left
// for readFollowUpRows. An open Ask for help only ever flags a Goal Not Green
// flags already, so a reader counting the flagged Goals needn't read the
// asks.
func (s *Service) followUpRowsAmong(reads followUpReads) []FollowUpRow {
	var rows []FollowUpRow
	at := map[int64]int{}
	add := func(g Goal, sig FollowUpSignal) {
		i, ok := at[g.ID]
		if !ok {
			i = len(rows)
			at[g.ID] = i
			rows = append(rows, FollowUpRow{Goal: g})
		}
		rows[i].Signals = append(rows[i].Signals, sig)
	}
	goals := make([]Goal, 0, len(reads.active))
	latest := make(map[int64]lastCheckin, len(reads.active))
	for _, r := range reads.active {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
		latest[r.Goal.ID] = lastCheckinFromRow(r)
	}
	fresh := s.freshnessSignalsAmong(reads.active, reads.overdueMilestones)
	for _, gf := range fresh.NotGreen {
		add(gf.Goal, FollowUpSignal{Kind: followUpHealthKind(gf.Health)})
	}
	for _, ask := range reads.asks {
		add(ask.Goal, FollowUpSignal{Kind: "asks-for-help", Person: ask.Addressee})
	}
	for _, gf := range fresh.CheckinOverdue {
		add(gf.Goal, FollowUpSignal{Kind: "checkin-overdue", Days: gf.Freshness.DaysSince})
	}
	// A Path to Green's overdue days are counted in the Goal's timezone, the
	// one its flag was judged in.
	for _, gf := range fresh.OverduePaths {
		add(gf.Goal, FollowUpSignal{Kind: "path-overdue", Days: followUpCalendarDays(gf.Freshness.PathTargetDate, s.clock.Now().In(s.GoalTimezone(gf.Goal)))})
	}
	// So are a Milestone's.
	for _, gf := range fresh.OverdueMilestones {
		add(gf.Goal, FollowUpSignal{Kind: "milestone-overdue", Days: followUpCalendarDays(gf.Freshness.OverdueMilestoneDate, s.clock.Now().In(s.GoalTimezone(gf.Goal)))})
	}
	// An Action Item's are counted in the org's timezone, the one it is
	// judged overdue in.
	today := s.clock.Now().In(s.loc)
	itemDays := map[int64]int{}
	for _, r := range reads.overdueItems {
		due, _ := time.Parse(dateFormat, r.ActionItem.DueDate)
		if d := followUpCalendarDays(due, today); d > itemDays[r.ActionItem.GoalID] {
			itemDays[r.ActionItem.GoalID] = d
		}
	}
	for _, g := range goals {
		if d, ok := itemDays[g.ID]; ok {
			add(g, FollowUpSignal{Kind: "action-item-overdue", Days: d})
		}
	}
	for _, g := range goals {
		if g.Ownerless {
			add(g, FollowUpSignal{Kind: "ownerless"})
		}
	}
	graph := graphSignalsOf(unlinkedAmong(goals, reads.edges), reads.edges)
	for _, g := range graph.Unlinked {
		add(g, FollowUpSignal{Kind: "unlinked"})
	}
	for _, c := range graph.ScheduleConflicts {
		add(c.Child, FollowUpSignal{Kind: "schedule-conflicts", Days: followUpCalendarDays(c.Parent.DeliveryDate, c.Child.DeliveryDate), Related: &c.Parent})
	}
	for _, hp := range graph.HaltedParents {
		add(hp.Child, FollowUpSignal{Kind: "halted-parents", Related: &hp.Parent})
	}
	for i := range rows {
		if last, ok := latest[rows[i].Goal.ID]; ok {
			rows[i].Health = last.health
			if needsPathToGreen(last.health) {
				rows[i].PathTargetDate = last.pathTargetDate
			}
		}
	}
	return rows
}

// followUpCalendarDays counts the calendar days from one date to a later one,
// each read as the date it falls on in its own location. A date-only value,
// such as a due date, is midnight UTC on its date, so this reads it as that
// date wherever to is: unlike daysBetween, which would read it as the day
// before in a timezone behind UTC.
func followUpCalendarDays(from, to time.Time) int {
	date := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(date(to).Sub(date(from)) / (24 * time.Hour))
}

// followUpHealthKind is the signal kind a Not Green Goal's Health flags it
// with: "red" for Red, "yellow" for Yellow.
func followUpHealthKind(health string) string {
	if health == HealthRed {
		return "red"
	}
	return "yellow"
}

// followUpSignalKinds counts the distinct kinds of signal that flag r, so a
// Goal under two halted parents counts its halted parents once.
func followUpSignalKinds(r FollowUpRow) int {
	seen := map[string]bool{}
	for _, sig := range r.Signals {
		seen[sig.Kind] = true
	}
	return len(seen)
}

// followUpHealthRank orders Health most urgent first: Red, Yellow, none, then
// Green.
func followUpHealthRank(health string) int {
	switch health {
	case HealthRed:
		return 0
	case HealthYellow:
		return 1
	case HealthGreen:
		return 3
	}
	return 2
}

// followUpRank orders true before false.
func followUpRank(b bool) int {
	if b {
		return 0
	}
	return 1
}

// sortFollowUpRows puts the most urgent Goals first, key by key: Active Goals
// before the Proposed and On Hold ones, which nothing has gone wrong with yet;
// then Ownerless first; then by Health (followUpHealthRank); then a Path to
// Green overdue first, longest overdue first; then a Milestone overdue first,
// longest overdue first; then an overdue Action Item first, longest overdue
// first; then Check-in overdue first, furthest past its cadence first; then
// more distinct kinds of signal first; then by title, ignoring case.
func sortFollowUpRows(rows []FollowUpRow) {
	slices.SortStableFunc(rows, func(a, b FollowUpRow) int {
		aActive := a.Goal.Lifecycle == LifecycleActive
		bActive := b.Goal.Lifecycle == LifecycleActive
		if c := cmp.Compare(followUpRank(aActive), followUpRank(bActive)); c != 0 {
			return c
		}
		_, aOwnerless := a.Signal("ownerless")
		_, bOwnerless := b.Signal("ownerless")
		if c := cmp.Compare(followUpRank(aOwnerless), followUpRank(bOwnerless)); c != 0 {
			return c
		}
		if c := cmp.Compare(followUpHealthRank(a.Health), followUpHealthRank(b.Health)); c != 0 {
			return c
		}
		aPath, aOverdue := a.Signal("path-overdue")
		bPath, bOverdue := b.Signal("path-overdue")
		if c := cmp.Compare(followUpRank(aOverdue), followUpRank(bOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bPath.Days, aPath.Days); c != 0 {
			return c
		}
		aMilestone, aMilestoneOverdue := a.Signal("milestone-overdue")
		bMilestone, bMilestoneOverdue := b.Signal("milestone-overdue")
		if c := cmp.Compare(followUpRank(aMilestoneOverdue), followUpRank(bMilestoneOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bMilestone.Days, aMilestone.Days); c != 0 {
			return c
		}
		aItem, aItemOverdue := a.Signal("action-item-overdue")
		bItem, bItemOverdue := b.Signal("action-item-overdue")
		if c := cmp.Compare(followUpRank(aItemOverdue), followUpRank(bItemOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bItem.Days, aItem.Days); c != 0 {
			return c
		}
		aCheckinOverdue, aIsCheckinOverdue := a.Signal("checkin-overdue")
		bCheckinOverdue, bIsCheckinOverdue := b.Signal("checkin-overdue")
		if c := cmp.Compare(followUpRank(aIsCheckinOverdue), followUpRank(bIsCheckinOverdue)); c != 0 {
			return c
		}
		if aIsCheckinOverdue && bIsCheckinOverdue {
			if c := cmp.Compare(bCheckinOverdue.Days-b.Goal.CadenceDays, aCheckinOverdue.Days-a.Goal.CadenceDays); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(followUpSignalKinds(b), followUpSignalKinds(a)); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title))
	})
}
