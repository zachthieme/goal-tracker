package domain

import (
	"cmp"
	"context"
	"slices"
	"strings"
)

// LinkedGoal is another Goal as an exception block names it: one it
// contributes to, or one behind its Rolled-up Health (#246). InReport marks a
// Goal the same Report holds, as an exception block or one line, and Health is
// then the Health the Report shows for it; a child outside the Report has its
// latest Check-in's. It is frozen with the snapshot.
type LinkedGoal struct {
	GoalID   int64
	Title    string
	Health   string `json:",omitempty"`
	InReport bool   `json:",omitempty"`
}

// linkReportBlocks names, on each of r's exception blocks, the Goals it
// contributes to that r holds, and behind it every child r holds and every
// Active child it doesn't whose latest Check-in is Red or Yellow. It reads
// what r holds once every selected Goal is placed, and only names: which Goals
// r holds never depends on their links (ADR 0007).
func (s *Service) linkReportBlocks(ctx context.Context, r *Report) error {
	held := map[int64]string{}
	for _, sg := range r.selected() {
		held[sg.Goal.ID] = sg.Health
	}
	for i := range r.Exceptions {
		b := &r.Exceptions[i]
		parents, err := s.ParentsOf(ctx, b.Goal.ID)
		if err != nil {
			return err
		}
		for _, p := range parents {
			if health, ok := held[p.ID]; ok {
				b.ContributesTo = append(b.ContributesTo, LinkedGoal{GoalID: p.ID, Title: p.Title, Health: health, InReport: true})
			}
		}
		children, err := s.ChildrenOf(ctx, b.Goal.ID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if health, ok := held[c.ID]; ok {
				b.Behind = append(b.Behind, LinkedGoal{GoalID: c.ID, Title: c.Title, Health: health, InReport: true})
				continue
			}
			if c.Lifecycle != LifecycleActive {
				continue
			}
			latest, ok, err := s.LatestCheckin(ctx, c.ID)
			if err != nil {
				return err
			}
			if ok && needsPathToGreen(latest.Health) {
				b.Behind = append(b.Behind, LinkedGoal{GoalID: c.ID, Title: c.Title, Health: latest.Health})
			}
		}
		sortLinkedGoals(b.ContributesTo)
		sortLinkedGoals(b.Behind)
	}
	return nil
}

// sortLinkedGoals orders Goals Red first, then Yellow, then the rest, each by
// title ignoring case, then by id.
func sortLinkedGoals(goals []LinkedGoal) {
	slices.SortStableFunc(goals, func(a, b LinkedGoal) int {
		return cmp.Or(
			cmp.Compare(linkedHealthRank(a.Health), linkedHealthRank(b.Health)),
			cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
			cmp.Compare(a.GoalID, b.GoalID),
		)
	})
}

// linkedHealthRank puts Red before Yellow before every other Health.
func linkedHealthRank(health string) int {
	switch health {
	case HealthRed:
		return 0
	case HealthYellow:
		return 1
	default:
		return 2
	}
}
