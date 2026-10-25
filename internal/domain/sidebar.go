package domain

import (
	"context"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Sidebar is what the sidebar on every signed-in page shows one person, read
// by Service.Sidebar (ADR 0010).
type Sidebar struct {
	// NeedsYou counts what is waiting on the person, as Home lists it: the
	// Goals they Own or are a Delegate on whose Check-in is due before the
	// next reminder, the Asks for help addressed to them, their open Action
	// Items, and the link requests, Handoffs and Parent suggestions waiting
	// on their decision.
	NeedsYou int
	// FollowUps counts the Goals the Follow-ups page flags in the person's
	// default scope, each once however many signals flag it: as many as a
	// bare Follow-ups page lists.
	FollowUps int
	// DirectlyUnder are the people whose Manager is the person, for My org,
	// as DirectlyUnder lists them.
	DirectlyUnder []Account
}

// Sidebar reads accountID's sidebar, reading each thing once and only what
// the sidebar shows (ADR 0010).
func (s *Service) Sidebar(ctx context.Context, accountID int64) (Sidebar, error) {
	accounts, err := s.queries.ListAskWalkAccounts(ctx)
	if err != nil {
		return Sidebar{}, fmt.Errorf("list accounts: %w", err)
	}
	byID := make(map[int64]Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = accountFromRow(a)
	}
	delegated, err := s.queries.ListDelegatedGoals(ctx, accountID)
	if err != nil {
		return Sidebar{}, fmt.Errorf("list delegated goals: %w", err)
	}
	isDelegate := make(map[int64]bool, len(delegated))
	for _, r := range delegated {
		isDelegate[r.Goal.ID] = true
	}
	active, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return Sidebar{}, fmt.Errorf("list active goals: %w", err)
	}
	sb := Sidebar{DirectlyUnder: directlyUnderAmong(accountID, accounts)}
	if sb.NeedsYou, err = s.sidebarNeedsYou(ctx, accountID, s.personalGoalsAmong(active, accountID, isDelegate), byID); err != nil {
		return Sidebar{}, err
	}
	if sb.FollowUps, err = s.sidebarFollowUps(ctx, accountID, active, isDelegate, byID); err != nil {
		return Sidebar{}, err
	}
	return sb, nil
}

// sidebarFollowUps counts the Goals the Follow-ups page flags in accountID's
// default scope (Sidebar.FollowUps), given active, every Active Goal with its
// latest Check-in, isDelegate, the IDs of the Goals accountID is a Delegate
// on, and byID, every Account by ID. Every Goal the freshness signals, an
// Ownerless Goal or an overdue Action Item flags is Active, so active holds
// them all, and the graph's signals flag the Goals on accepted links. An open
// Ask for help flags a Goal whose latest Check-in is Red or Yellow, which Not
// Green already flags, so the asks aren't read for it.
func (s *Service) sidebarFollowUps(ctx context.Context, accountID int64, active []db.ListActiveGoalsWithLatestCheckinRow, isDelegate map[int64]bool, byID map[int64]Account) (int, error) {
	overdue, err := s.listFreshnessOverdueMilestones(ctx)
	if err != nil {
		return 0, err
	}
	edges, err := s.acceptedEdges(ctx)
	if err != nil {
		return 0, err
	}
	items, err := s.queries.ListOverdueActionItems(ctx, orgDate(s.clock.Now(), s.loc).Format(dateFormat))
	if err != nil {
		return 0, fmt.Errorf("list overdue action items: %w", err)
	}
	goals := make([]Goal, 0, len(active))
	for _, r := range active {
		goals = append(goals, goalFromRow(r.Goal, r.Account))
	}
	keeps := sidebarDefaultScope(accountID, isDelegate, byID)
	flagged := map[int64]bool{}
	flag := func(g Goal) {
		if keeps(g) {
			flagged[g.ID] = true
		}
	}
	fresh := s.freshnessSignalsAmong(active, overdue)
	for _, list := range [][]GoalFreshness{fresh.NotGreen, fresh.CheckinOverdue, fresh.OverduePaths, fresh.OverdueMilestones} {
		for _, gf := range list {
			flag(gf.Goal)
		}
	}
	graph := graphSignalsOf(unlinkedAmong(goals, edges), edges)
	for _, g := range graph.Unlinked {
		flag(g)
	}
	for _, c := range graph.ScheduleConflicts {
		flag(c.Child)
	}
	for _, hp := range graph.HaltedParents {
		flag(hp.Child)
	}
	withOverdueItem := map[int64]bool{}
	for _, r := range items {
		withOverdueItem[r.ActionItem.GoalID] = true
	}
	for _, g := range goals {
		if g.Ownerless || withOverdueItem[g.ID] {
			flag(g)
		}
	}
	return len(flagged), nil
}

// sidebarDefaultScope is whether accountID's default scope on the Follow-ups
// page keeps a Goal: My org, by its Owner, when anyone in byID has accountID
// as their Manager, else Mine, the Goals they Own and, by isDelegate, the
// ones they are a Delegate on. It walks the Org in byID as Org walks it a
// level at a time, stopping at anyone reached.
func sidebarDefaultScope(accountID int64, isDelegate map[int64]bool, byID map[int64]Account) func(Goal) bool {
	under := map[int64][]int64{}
	for _, a := range byID {
		if a.ManagerID != nil && *a.ManagerID != a.ID {
			under[*a.ManagerID] = append(under[*a.ManagerID], a.ID)
		}
	}
	if len(under[accountID]) == 0 {
		return func(g Goal) bool { return g.Owner.ID == accountID || isDelegate[g.ID] }
	}
	in := map[int64]bool{accountID: true}
	for level := []int64{accountID}; len(level) > 0; {
		var next []int64
		for _, id := range level {
			for _, u := range under[id] {
				if !in[u] {
					in[u] = true
					next = append(next, u)
				}
			}
		}
		level = next
	}
	return func(g Goal) bool { return in[g.Owner.ID] }
}

// sidebarNeedsYou counts what is waiting on accountID (Sidebar.NeedsYou),
// given personal, the Active Goals they Own or are a Delegate on, and byID,
// every Account by ID, to find whom each Ask for help is addressed to.
func (s *Service) sidebarNeedsYou(ctx context.Context, accountID int64, personal []PersonalGoal, byID map[int64]Account) (int, error) {
	n := 0
	for _, g := range personal {
		if g.Freshness.DueBeforeNextReminder() {
			n++
		}
	}
	askRows, err := s.queries.ListOpenAsksForHelp(ctx)
	if err != nil {
		return 0, fmt.Errorf("list open asks for help: %w", err)
	}
	asks, err := openAsksAmong(askRows, byID)
	if err != nil {
		return 0, err
	}
	for _, a := range asks {
		if a.Addressee != nil && a.Addressee.ID == accountID {
			n++
		}
	}
	items, err := s.queries.ListOpenActionItemsForOwner(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("list owner's open action items: %w", err)
	}
	links, err := s.queries.ListPendingLinksForOwner(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("list pending links: %w", err)
	}
	handoffs, err := s.queries.ListPendingHandoffsForNewOwner(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("list pending handoffs: %w", err)
	}
	suggestions, err := s.queries.ListOpenParentSuggestionsForOwner(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("list open parent suggestions: %w", err)
	}
	return n + len(items) + len(links) + len(handoffs) + len(suggestions), nil
}
