package domain

import (
	"context"
	"fmt"
	"strings"
)

// RolledUpHealth is a Goal's Rolled-up Health: the worst Owner-set Health among
// its Active children, computed when read (ADR-0003, CONTEXT.md: Rolled-up
// Health). It sits next to the Owner-set Health, and when the two differ the
// Owner has to explain why. Present is false when the Goal has no Active child
// that has set a Health yet — there is nothing to roll up.
//
// CheckinOverdueChildren counts how many of the ActiveChildren are Check-in
// overdue ("3 of 12 overdue"). It is shown beside the color and never changes
// it: Health stays Owner-set, and being Check-in overdue is its own signal
// (CONTEXT.md: Check-in overdue).
// RedChildren and YellowChildren count the ActiveChildren whose latest
// Check-in is Red or Yellow, for the count a Report shows beside the color
// ("2 of 6 Red · 1 Yellow"); a snapshot frozen before Reports counted them has
// neither.
type RolledUpHealth struct {
	Health                 string
	Present                bool
	CheckinOverdueChildren int
	ActiveChildren         int
	RedChildren            int `json:",omitempty"`
	YellowChildren         int `json:",omitempty"`
}

// CountLine says what sets the Rolled-up Health: how many of its Active
// children are Red, Yellow and Check-in overdue, each only when there are some,
// the first out of all of them ("2 of 6 Red · 1 Yellow · 1 overdue"). Green is
// never counted. It is empty when none are Red, Yellow or Check-in overdue.
func (r RolledUpHealth) CountLine() string {
	var parts []string
	for _, c := range []struct {
		n     int
		label string
	}{
		{r.RedChildren, HealthRed},
		{r.YellowChildren, HealthYellow},
		{r.CheckinOverdueChildren, "overdue"},
	} {
		if c.n == 0 {
			continue
		}
		if len(parts) == 0 {
			parts = append(parts, fmt.Sprintf("%d of %d %s", c.n, r.ActiveChildren, c.label))
		} else {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	return strings.Join(parts, " · ")
}

// healthRank orders Health from best to worst so the worst is the maximum:
// Green < Yellow < Red. An unknown value ranks below Green so it never wins.
func healthRank(health string) int {
	switch health {
	case HealthGreen:
		return 1
	case HealthYellow:
		return 2
	case HealthRed:
		return 3
	default:
		return 0
	}
}

// RolledUpHealth computes goalID's Rolled-up Health from its Active children's
// Owner-set Health (ADR-0003). Health is set by the Owner and never computed, so
// this reads each Active child's latest Check-in and keeps the worst. Only
// Active children count: a Proposed child has no Health, and a child with no
// Check-in yet has none either, so both are skipped. When no Active child has a
// Health, Present is false. Each Active child is also judged Stale or not and
// counted, and counted again when it is Red or Yellow. The traversal is one level — each parent shows the
// roll-up of its own direct children, which itself already reflects the levels
// below it.
func (s *Service) RolledUpHealth(ctx context.Context, goalID int64) (RolledUpHealth, error) {
	children, err := s.ChildrenOf(ctx, goalID)
	if err != nil {
		return RolledUpHealth{}, err
	}
	var out RolledUpHealth
	for _, child := range children {
		if child.Lifecycle != LifecycleActive {
			continue
		}
		latest, ok, err := s.LatestCheckin(ctx, child.ID)
		if err != nil {
			return RolledUpHealth{}, err
		}
		out.ActiveChildren++
		var last lastCheckin
		if ok {
			last.at = latest.CreatedAt
		}
		if s.judgeFreshness(child, last).CheckinOverdue {
			out.CheckinOverdueChildren++
		}
		if !ok {
			continue
		}
		switch latest.Health {
		case HealthRed:
			out.RedChildren++
		case HealthYellow:
			out.YellowChildren++
		}
		if !out.Present || healthRank(latest.Health) > healthRank(out.Health) {
			out.Health, out.Present = latest.Health, true
		}
	}
	return out, nil
}
