package domain

import (
	"context"
	"fmt"
	"time"
)

// GoalListing is what a page listing many Goals shows of each beyond the Goal
// itself: the Goal list, in either layout, and the Delegates page.
type GoalListing struct {
	// Latest is its latest Check-in, nil when it has none, and Health its
	// current Health (currentHealth): Latest's while it is Active, and ""
	// otherwise.
	Latest *Checkin
	Health string
	// Freshness is its freshness signals (FreshnessByGoal), and Incomplete
	// the required Dimensions and Fields an Active Goal has no value in, none
	// when it isn't Incomplete (IncompleteByGoal; CONTEXT.md: Incomplete).
	Freshness  Freshness
	Incomplete []string
	// PriorDueDates are the delivery dates its Date Slips moved it from,
	// earliest first, shown struck through ahead of its current one.
	PriorDueDates []time.Time
}

// GoalListings reads each of goals' listing, keyed by Goal ID, in the same
// few queries however many Goals there are, so a page listing Goals can
// afford it.
func (s *Service) GoalListings(ctx context.Context, goals []Goal) (map[int64]GoalListing, error) {
	ids := goalIDsOf(goals)
	latest, err := s.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load latest check-ins: %w", err)
	}
	freshness, err := s.FreshnessByGoal(ctx, goals)
	if err != nil {
		return nil, fmt.Errorf("read freshness: %w", err)
	}
	incomplete, err := s.IncompleteByGoal(ctx, goals)
	if err != nil {
		return nil, fmt.Errorf("read incomplete: %w", err)
	}
	slips, err := s.DateSlipsByGoal(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load date slips: %w", err)
	}
	out := make(map[int64]GoalListing, len(goals))
	for _, g := range goals {
		l := GoalListing{
			Freshness:     freshness[g.ID],
			Incomplete:    incomplete[g.ID],
			PriorDueDates: priorDatesOf(slips[g.ID], 0),
		}
		if c, ok := latest[g.ID]; ok {
			l.Latest = &c
		}
		l.Health = currentHealth(g, l.Latest)
		out[g.ID] = l
	}
	return out, nil
}
