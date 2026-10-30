package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// SignalCounts counts the Active Goals in an Org under each signal Home's My
// org card shows. A Goal counts under every signal it has, so the counts can
// add up to more than the Goals behind them.
type SignalCounts struct {
	Red, PathOverdue, CheckinOverdue, Yellow int
}

// Any reports whether anything is flagged.
func (c SignalCounts) Any() bool {
	return c.Red+c.PathOverdue+c.CheckinOverdue+c.Yellow > 0
}

// add counts one Goal, with its latest Check-in's Health and whether its Path
// to Green and its Check-in are overdue.
func (c *SignalCounts) add(health string, pathOverdue, checkinOverdue bool) {
	if health == HealthRed {
		c.Red++
	}
	if pathOverdue {
		c.PathOverdue++
	}
	if checkinOverdue {
		c.CheckinOverdue++
	}
	if health == HealthYellow {
		c.Yellow++
	}
}

// PersonSignals is one person directly under the viewer, with the counts of
// their Org's Goals.
type PersonSignals struct {
	Person Account
	Counts SignalCounts
}

// OrgSignals is what is in trouble in someone's Org, person by person. Total
// counts the Goals the Goal list's Org scope keeps for them, their own
// included; Own counts the Goals they Own themselves, and Under splits the
// rest among the people directly under them, so Total is the sum of Own and
// Under's counts. Under puts the most Red first, then the most Path to Green
// overdue, the most Check-in overdue and the most Yellow, then goes by Name;
// those with nothing flagged come last.
type OrgSignals struct {
	Total SignalCounts
	Own   SignalCounts
	Under []PersonSignals
}

// OrgSignals counts the Red, Path to Green overdue, Check-in overdue and Yellow
// Active Goals Owned in personID's Org, theirs included, as the Org scope
// GoalScope resolves for them keeps them (CONTEXT.md: Org). A Goal is in the
// Org by its Owner alone, never its Delegates; an Ownerless Goal stays where
// its Departed Owner was. personID's own Goals count in Own, and each person
// below personID counts toward the person directly under personID through
// whom orgWalk first reaches them, so a Manager cycle (ADR 0008) counts no
// one twice and personID's Goals in no one's row.
func (s *Service) OrgSignals(ctx context.Context, personID int64) (OrgSignals, error) {
	accounts, err := s.orgWalkAccounts(ctx)
	if err != nil {
		return OrgSignals{}, err
	}
	of := Account{ID: personID}
	if i := slices.IndexFunc(accounts, func(a Account) bool { return a.ID == personID }); i >= 0 {
		of = accounts[i]
	}
	var scope GoalScope
	scope.scopeToOrg(of, accounts)
	var out OrgSignals
	// row maps each person under personID to their entry in out.Under.
	row := map[int64]int{}
	for _, r := range orgWalk(personID, accounts) {
		if r.via == r.ID {
			row[r.ID] = len(out.Under)
			out.Under = append(out.Under, PersonSignals{Person: r.Account})
		} else {
			row[r.ID] = row[r.via]
		}
	}
	goals, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return OrgSignals{}, fmt.Errorf("list active goals: %w", err)
	}
	// Home's My org shows no Milestone overdue, so no Milestones are read and
	// no Freshness is judged: only the rules for the signals it counts.
	now := s.clock.Now()
	for _, r := range goals {
		g := goalFromRow(r.Goal, r.Account)
		if !scope.InScope(g) {
			continue
		}
		last := lastCheckinFromRow(r)
		pathOverdue := pathToGreenPastTarget(g.Lifecycle, last.health, last.pathTargetDate, now, s.GoalTimezone(g))
		checkinOverdue := s.checkinOverdueSince(g, last.at)
		if g.Owner.ID == personID {
			out.Own.add(r.CheckinHealth, pathOverdue, checkinOverdue)
		} else {
			out.Under[row[g.Owner.ID]].Counts.add(r.CheckinHealth, pathOverdue, checkinOverdue)
		}
		out.Total.add(r.CheckinHealth, pathOverdue, checkinOverdue)
	}
	slices.SortFunc(out.Under, func(a, b PersonSignals) int {
		x, y := a.Counts, b.Counts
		return cmp.Or(
			cmp.Compare(y.Red, x.Red),
			cmp.Compare(y.PathOverdue, x.PathOverdue),
			cmp.Compare(y.CheckinOverdue, x.CheckinOverdue),
			cmp.Compare(y.Yellow, x.Yellow),
			cmp.Compare(strings.ToLower(a.Person.Label()), strings.ToLower(b.Person.Label())),
			cmp.Compare(a.Person.Email, b.Person.Email),
		)
	})
	return out, nil
}
