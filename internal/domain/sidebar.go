package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Sidebar is what the sidebar on every signed-in page shows one person, read
// by Service.Sidebar (ADR 0010). Each part is marked by whether it was read,
// so a failed read leaves out only the parts it feeds.
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
	// NeedsYouRead, FollowUpsRead and DirectlyUnderRead are whether
	// NeedsYou, FollowUps and DirectlyUnder were read; one that wasn't holds
	// its zero value and is left out.
	NeedsYouRead, FollowUpsRead, DirectlyUnderRead bool
}

// Sidebar reads accountID's sidebar, reading each thing once and only what
// the sidebar shows (ADR 0010). When a read fails it returns, along with the
// error, the parts that read doesn't feed: every Account feeds all three, the
// Delegate Goals and Active Goals both counts, and the rest one count each.
func (s *Service) Sidebar(ctx context.Context, accountID int64) (Sidebar, error) {
	accounts, err := s.queries.ListAskWalkAccounts(ctx)
	if err != nil {
		return Sidebar{}, fmt.Errorf("list accounts: %w", err)
	}
	sb := Sidebar{DirectlyUnder: directlyUnderAmong(accountID, accounts), DirectlyUnderRead: true}
	all := orgWalkAccountsOf(accounts)
	byID := make(map[int64]Account, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	delegated, err := s.queries.ListDelegatedGoals(ctx, accountID)
	if err != nil {
		return sb, fmt.Errorf("list delegated goals: %w", err)
	}
	isDelegate := make(map[int64]bool, len(delegated))
	for _, r := range delegated {
		isDelegate[r.Goal.ID] = true
	}
	active, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return sb, fmt.Errorf("list active goals: %w", err)
	}
	needsYou, needsErr := s.sidebarNeedsYou(ctx, accountID, s.personalGoalsAmong(active, accountID, isDelegate), byID)
	if needsErr == nil {
		sb.NeedsYou, sb.NeedsYouRead = needsYou, true
	}
	followUps, followErr := s.sidebarFollowUps(ctx, accountID, active, isDelegate, all)
	if followErr == nil {
		sb.FollowUps, sb.FollowUpsRead = followUps, true
	}
	return sb, errors.Join(needsErr, followErr)
}

// sidebarFollowUps counts the Goals the Follow-ups page flags in accountID's
// default scope (Sidebar.FollowUps), given active, every Active Goal with its
// latest Check-in, isDelegate, the IDs of the Goals accountID is a Delegate
// on, and accounts, every Account by ID. It decides which Goals are flagged as
// Service.FollowUps does (followUpRowsAmong), reading only what that needs
// beyond active, and not the open Asks for help, which flag only Goals Not
// Green flags already.
func (s *Service) sidebarFollowUps(ctx context.Context, accountID int64, active []db.ListActiveGoalsWithLatestCheckinRow, isDelegate map[int64]bool, accounts []Account) (int, error) {
	reads, err := s.readFollowUpSignals(ctx, active)
	if err != nil {
		return 0, err
	}
	keeps := sidebarDefaultScope(accountID, isDelegate, accounts)
	n := 0
	for _, r := range s.followUpRowsAmong(reads) {
		if keeps(r.Goal) {
			n++
		}
	}
	return n, nil
}

// sidebarDefaultScope is whether accountID's default scope on the Follow-ups
// page keeps a Goal, as GoalScope resolves it over accounts, every Account by
// ID, with isDelegate, the IDs of the Goals they are a Delegate on, for
// Mine.
func sidebarDefaultScope(accountID int64, isDelegate map[int64]bool, accounts []Account) func(Goal) bool {
	viewer := Account{ID: accountID}
	if i := slices.IndexFunc(accounts, func(a Account) bool { return a.ID == accountID }); i >= 0 {
		viewer = accounts[i]
	}
	sc := goalScopeAmong(viewer, GoalScopeRequest{}, nil, nil, accounts)
	sc.delegated = isDelegate
	return sc.InScope
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
