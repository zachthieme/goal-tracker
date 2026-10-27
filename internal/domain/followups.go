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
	// Accounts are every Account, by ID, read whether or not an Ask for
	// help is open, so the page's Scope needn't read them again
	// (GoalScopeAmong).
	Accounts []Account
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
	// Permissions are what the viewer may do with the Goal, by the Goal rules
	// (goalPermissionsFor), but No change, which Follow-ups doesn't offer.
	// Nudge is whether the Nudge rule (nudgeRuleRefusal) lets them nudge it,
	// counting only the row's own Schedule conflicts, where the Goal is the
	// child, and whether it was nudged today aside. OwnedByViewer is whether
	// they are its Owner. Service.FollowUps judges them for its viewer.
	Permissions   GoalPermissions
	Nudge         bool
	OwnedByViewer bool
}

// FollowUpKind names a kind of signal that flags a Goal on the Follow-ups
// page, as its chip's data-kind there.
type FollowUpKind string

// The kinds of Follow-up signal.
const (
	FollowUpRed               FollowUpKind = "red"
	FollowUpYellow            FollowUpKind = "yellow"
	FollowUpAsksForHelp       FollowUpKind = "asks-for-help"
	FollowUpCheckinOverdue    FollowUpKind = "checkin-overdue"
	FollowUpPathOverdue       FollowUpKind = "path-overdue"
	FollowUpMilestoneOverdue  FollowUpKind = "milestone-overdue"
	FollowUpActionItemOverdue FollowUpKind = "action-item-overdue"
	FollowUpOwnerless         FollowUpKind = "ownerless"
	FollowUpUnlinked          FollowUpKind = "unlinked"
	FollowUpScheduleConflicts FollowUpKind = "schedule-conflicts"
	FollowUpHaltedParents     FollowUpKind = "halted-parents"
)

// followUpWaitsOnCheckin pairs each kind of signal that leaves a Goal waiting
// on a Check-in, the Nudge rule's overdue reasons (CONTEXT.md: Nudge), with
// the Freshness flag that raises it, in the order the Follow-ups page lists
// them. A Schedule conflict, which the Nudge rule allows apart, isn't one.
var followUpWaitsOnCheckin = []struct {
	kind   FollowUpKind
	raised func(Freshness) bool
}{
	{FollowUpCheckinOverdue, func(f Freshness) bool { return f.CheckinOverdue }},
	{FollowUpPathOverdue, func(f Freshness) bool { return f.PathToGreenOverdue }},
	{FollowUpMilestoneOverdue, func(f Freshness) bool { return f.MilestoneOverdue }},
}

// WaitsOnCheckinKinds are the kinds of signal that leave a Goal waiting on a
// Check-in (followUpWaitsOnCheckin), in page order: Check-in overdue, Path to
// Green overdue, Milestone overdue.
var WaitsOnCheckinKinds = func() []FollowUpKind {
	kinds := make([]FollowUpKind, 0, len(followUpWaitsOnCheckin))
	for _, w := range followUpWaitsOnCheckin {
		kinds = append(kinds, w.kind)
	}
	return kinds
}()

// FollowUpSignal is one reason a Goal is flagged. Kind names the signal, as
// the chip's data-kind on the Follow-ups page. Days is how long the signal has
// held: for FollowUpCheckinOverdue the days since the last update, for
// FollowUpPathOverdue the days past the Path to Green's target date, for
// FollowUpMilestoneOverdue the days past the earliest overdue Milestone's
// date, for FollowUpActionItemOverdue the days the most overdue Action Item
// about the Goal is past its due date, for FollowUpScheduleConflicts the days
// the child's delivery date falls after the parent's, and 0 otherwise.
// Related is the parent for FollowUpScheduleConflicts and
// FollowUpHaltedParents. Person is whom a FollowUpAsksForHelp asks, nil when
// no one is above the Owner. RelatedPermissions are, for
// FollowUpScheduleConflicts, what the viewer may do with the parent, as
// FollowUpRow.Permissions are for the row's Goal.
type FollowUpSignal struct {
	Kind               FollowUpKind
	Days               int
	Related            *Goal
	Person             *Account
	RelatedPermissions GoalPermissions
}

// Signal is the row's first signal of kind, and whether it has one.
func (r FollowUpRow) Signal(kind FollowUpKind) (FollowUpSignal, bool) {
	for _, sig := range r.Signals {
		if sig.Kind == kind {
			return sig, true
		}
	}
	return FollowUpSignal{}, false
}

// WaitsOnCheckin reports whether the row's Goal waits on a Check-in: one of
// its signals is of a kind in WaitsOnCheckinKinds.
func (r FollowUpRow) WaitsOnCheckin() bool {
	for _, w := range followUpWaitsOnCheckin {
		if _, ok := r.Signal(w.kind); ok {
			return true
		}
	}
	return false
}

// FollowUps reads the Follow-ups page for viewerID, reading each thing once
// (ADR 0010): every Goal the company's freshness and graph signals, an
// Ownerless Goal, an open Ask for help or an overdue Action Item flags, with
// the Goals the viewer is a Delegate on and every Account, which the open
// Asks for help are walked up; and judges from them what the viewer may do
// with each row's Goal (permitFollowUpRows).
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
	rows, err := s.queries.ListAskWalkAccounts(ctx)
	if err != nil {
		return FollowUps{}, fmt.Errorf("list accounts: %w", err)
	}
	accounts := orgWalkAccountsOf(rows)
	byID := make(map[int64]Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}
	if reads.asks, err = s.openAsksForHelpAmong(ctx, byID); err != nil {
		return FollowUps{}, err
	}
	flagged := s.followUpRowsAmong(reads)
	if err := s.readFollowUpRows(ctx, flagged); err != nil {
		return FollowUps{}, err
	}
	sortFollowUpRows(flagged)
	out := FollowUps{Rows: flagged, Delegated: map[int64]bool{}, Accounts: accounts}
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
	viewer, found := byID[viewerID]
	if err := permitFollowUpRows(out.Rows, goalActor{
		id:      viewerID,
		account: func() (Account, bool, error) { return viewer, found, nil },
	}, out.Delegated); err != nil {
		return FollowUps{}, err
	}
	return out, nil
}

// permitFollowUpRows judges what viewer may do with each of rows' Goals, and
// with each Schedule conflict's parent, by the Goal rules, viewer being a
// Delegate on the Goals delegated holds: it sets each row's Permissions,
// Nudge and OwnedByViewer, and each Schedule conflict's RelatedPermissions.
// viewer comes without delegate, which is read from delegated Goal by Goal.
// Rows have no No change to offer, so their Permissions have none.
func permitFollowUpRows(rows []FollowUpRow, viewer goalActor, delegated map[int64]bool) error {
	var failed error
	permits := func(refusal error) bool {
		ok, err := goalPagePermits(refusal)
		if err != nil && failed == nil {
			failed = err
		}
		return ok
	}
	on := func(g Goal) goalActor {
		a := viewer
		a.delegate = func() (bool, error) { return delegated[g.ID], nil }
		return a
	}
	for i := range rows {
		r := &rows[i]
		actor := on(r.Goal)
		r.Permissions = goalPermissionsFor(actor, r.Goal, nil, permits)
		r.OwnedByViewer = r.Goal.Owner.ID == viewer.id
		_, conflicted := r.Signal(FollowUpScheduleConflicts)
		r.Nudge = permits(nudgeRuleRefusal(actor, r.Goal, nudgeFacts{
			overdue:          r.WaitsOnCheckin(),
			scheduleConflict: conflicted,
			anyoneLeft:       !r.Unreachable,
		}))
		for j := range r.Signals {
			if sig := &r.Signals[j]; sig.Kind == FollowUpScheduleConflicts {
				sig.RelatedPermissions = goalPermissionsFor(on(*sig.Related), *sig.Related, nil, permits)
			}
		}
	}
	return failed
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
		_, red := r.Signal(FollowUpRed)
		_, yellow := r.Signal(FollowUpYellow)
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
		add(ask.Goal, FollowUpSignal{Kind: FollowUpAsksForHelp, Person: ask.Addressee})
	}
	for _, gf := range fresh.CheckinOverdue {
		add(gf.Goal, FollowUpSignal{Kind: FollowUpCheckinOverdue, Days: gf.Freshness.DaysSince})
	}
	// A Path to Green's overdue days are counted in the Goal's timezone, the
	// one its flag was judged in.
	for _, gf := range fresh.OverduePaths {
		add(gf.Goal, FollowUpSignal{Kind: FollowUpPathOverdue, Days: followUpCalendarDays(gf.Freshness.PathTargetDate, s.clock.Now().In(s.GoalTimezone(gf.Goal)))})
	}
	// So are a Milestone's.
	for _, gf := range fresh.OverdueMilestones {
		add(gf.Goal, FollowUpSignal{Kind: FollowUpMilestoneOverdue, Days: followUpCalendarDays(gf.Freshness.OverdueMilestoneDate, s.clock.Now().In(s.GoalTimezone(gf.Goal)))})
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
			add(g, FollowUpSignal{Kind: FollowUpActionItemOverdue, Days: d})
		}
	}
	for _, g := range goals {
		if g.Ownerless {
			add(g, FollowUpSignal{Kind: FollowUpOwnerless})
		}
	}
	graph := graphSignalsOf(unlinkedAmong(goals, reads.edges), reads.edges)
	for _, g := range graph.Unlinked {
		add(g, FollowUpSignal{Kind: FollowUpUnlinked})
	}
	for _, c := range graph.ScheduleConflicts {
		add(c.Child, FollowUpSignal{Kind: FollowUpScheduleConflicts, Days: followUpCalendarDays(c.Parent.DeliveryDate, c.Child.DeliveryDate), Related: &c.Parent})
	}
	for _, hp := range graph.HaltedParents {
		add(hp.Child, FollowUpSignal{Kind: FollowUpHaltedParents, Related: &hp.Parent})
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
// with: FollowUpRed for Red, FollowUpYellow for Yellow.
func followUpHealthKind(health string) FollowUpKind {
	if health == HealthRed {
		return FollowUpRed
	}
	return FollowUpYellow
}

// followUpSignalKinds counts the distinct kinds of signal that flag r, so a
// Goal under two halted parents counts its halted parents once.
func followUpSignalKinds(r FollowUpRow) int {
	seen := map[FollowUpKind]bool{}
	for _, sig := range r.Signals {
		seen[sig.Kind] = true
	}
	return len(seen)
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
// then Ownerless first; then by Health (HealthUrgencyRank); then a Path to
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
		_, aOwnerless := a.Signal(FollowUpOwnerless)
		_, bOwnerless := b.Signal(FollowUpOwnerless)
		if c := cmp.Compare(followUpRank(aOwnerless), followUpRank(bOwnerless)); c != 0 {
			return c
		}
		if c := cmp.Compare(HealthUrgencyRank(a.Health), HealthUrgencyRank(b.Health)); c != 0 {
			return c
		}
		aPath, aOverdue := a.Signal(FollowUpPathOverdue)
		bPath, bOverdue := b.Signal(FollowUpPathOverdue)
		if c := cmp.Compare(followUpRank(aOverdue), followUpRank(bOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bPath.Days, aPath.Days); c != 0 {
			return c
		}
		aMilestone, aMilestoneOverdue := a.Signal(FollowUpMilestoneOverdue)
		bMilestone, bMilestoneOverdue := b.Signal(FollowUpMilestoneOverdue)
		if c := cmp.Compare(followUpRank(aMilestoneOverdue), followUpRank(bMilestoneOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bMilestone.Days, aMilestone.Days); c != 0 {
			return c
		}
		aItem, aItemOverdue := a.Signal(FollowUpActionItemOverdue)
		bItem, bItemOverdue := b.Signal(FollowUpActionItemOverdue)
		if c := cmp.Compare(followUpRank(aItemOverdue), followUpRank(bItemOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bItem.Days, aItem.Days); c != 0 {
			return c
		}
		aCheckinOverdue, aIsCheckinOverdue := a.Signal(FollowUpCheckinOverdue)
		bCheckinOverdue, bIsCheckinOverdue := b.Signal(FollowUpCheckinOverdue)
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
