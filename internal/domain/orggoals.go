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

// add counts one Goal, with its latest Check-in's Health and its freshness.
func (c *SignalCounts) add(health string, f Freshness) {
	if health == HealthRed {
		c.Red++
	}
	if f.PathToGreenOverdue {
		c.PathOverdue++
	}
	if f.CheckinOverdue {
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

// OrgSignals is what is in trouble in someone's Org, person by person: Total
// counts the Goals Owned by everyone under them, and Under splits it among the
// people directly under them, so Total is the sum of Under's counts. Under
// puts the most Red first, then the most Path to Green overdue, the most
// Stale and the most Yellow, then goes by Name; those with nothing flagged
// come last.
type OrgSignals struct {
	Total SignalCounts
	Under []PersonSignals
}

// OrgSignals counts the Red, Path to Green overdue, Stale and Yellow Active
// Goals Owned in personID's Org, leaving out personID's own (CONTEXT.md: Org).
// A Goal is in the Org by its Owner alone, never its Delegates; an Ownerless
// Goal stays where its Departed Owner was. Each person below personID counts
// toward the person directly under personID through whom a level-by-level
// walk first reaches them, so a Manager cycle (ADR 0008) counts no one twice.
func (s *Service) OrgSignals(ctx context.Context, personID int64) (OrgSignals, error) {
	var out OrgSignals
	// row maps each person under personID to their entry in out.Under.
	row := map[int64]int{}
	reached := map[int64]bool{personID: true}
	for level := []*int64{&personID}; len(level) > 0; {
		under, err := s.queries.ListAccountsUnder(ctx, level)
		if err != nil {
			return OrgSignals{}, fmt.Errorf("list org: %w", err)
		}
		level = nil
		for _, r := range under {
			if reached[r.ID] {
				continue
			}
			reached[r.ID] = true
			if *r.ManagerID == personID {
				row[r.ID] = len(out.Under)
				out.Under = append(out.Under, PersonSignals{Person: accountFromRow(r)})
			} else {
				row[r.ID] = row[*r.ManagerID]
			}
			level = append(level, &r.ID)
		}
	}
	goals, err := s.queries.ListActiveGoalsWithLatestCheckin(ctx)
	if err != nil {
		return OrgSignals{}, fmt.Errorf("list active goals: %w", err)
	}
	for _, r := range goals {
		i, ok := row[r.Goal.OwnerID]
		if !ok {
			continue
		}
		g := goalFromRow(r.Goal, r.Account)
		f := s.judgeFreshness(g, lastCheckinFromRow(r))
		out.Under[i].Counts.add(r.CheckinHealth, f)
		out.Total.add(r.CheckinHealth, f)
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
