package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// LinkedGoal is another Goal as an exception block names it: one it
// contributes to, or one behind its Rolled-up Health (#246). InReport marks a
// Goal the same Report holds, as an exception block or one line, and Health is
// then the Health the Report shows for it; a child outside the Report has its
// latest Check-in's.
type LinkedGoal struct {
	GoalID   int64
	Title    string
	Health   string
	InReport bool
}

// linkReportBlocks names, on each of r's exception blocks, the Goals it
// contributes to that r holds, and behind it every child r holds and every
// Active child it doesn't whose latest Check-in is Red or Yellow. It reads
// what r holds once every selected Goal is placed, and only names: which Goals
// r holds never depends on their links (ADR 0007). reads holds the blocks'
// parents and children, and those children's latest Check-ins.
func linkReportBlocks(r *Report, reads blockReads) {
	held := map[int64]string{}
	for _, sg := range r.selected() {
		held[sg.Goal.ID] = sg.Health
	}
	for i := range r.Exceptions {
		b := &r.Exceptions[i]
		for _, p := range reads.parents[b.Goal.ID] {
			if health, ok := held[p.ID]; ok {
				b.ContributesTo = append(b.ContributesTo, LinkedGoal{GoalID: p.ID, Title: p.Title, Health: health, InReport: true})
			}
		}
		for _, c := range reads.children[b.Goal.ID] {
			if health, ok := held[c.ID]; ok {
				b.Behind = append(b.Behind, LinkedGoal{GoalID: c.ID, Title: c.Title, Health: health, InReport: true})
				continue
			}
			if c.Lifecycle != LifecycleActive {
				continue
			}
			if latest, ok := reads.childLatest[c.ID]; ok && needsPathToGreen(latest.Health) {
				b.Behind = append(b.Behind, LinkedGoal{GoalID: c.ID, Title: c.Title, Health: latest.Health})
			}
		}
		sortLinkedGoals(b.ContributesTo)
		sortLinkedGoals(b.Behind)
	}
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

// ReportScheduleConflict is one Schedule conflict an exception block's Goal is
// in, as the block names it, whether or not the Report holds the Goal at the
// other end (CONTEXT.md: Report): that Goal, its Owner and its delivery date,
// Due. AsParent marks the block's Goal as the parent, the other Goal a child
// due after it; otherwise the block's Goal is the child, due after the other
// Goal it contributes to.
type ReportScheduleConflict struct {
	AsParent bool
	GoalID   int64
	Title    string
	Owner    Account
	Due      time.Time
}

// reportScheduleConflicts are the Schedule conflicts g is in through its
// accepted links up to parents and down from children, as goalScheduleConflicts
// judges them: as the child first, then as the parent, each by the other
// Goal's delivery date, then title ignoring case, then id.
func reportScheduleConflicts(g Goal, parents, children []Goal) []ReportScheduleConflict {
	var asChild, asParent []ReportScheduleConflict
	for _, c := range goalScheduleConflicts(g, parents, children) {
		if c.Child.ID == g.ID {
			asChild = append(asChild, ReportScheduleConflict{
				GoalID: c.Parent.ID, Title: c.Parent.Title, Owner: c.Parent.Owner, Due: c.Parent.DeliveryDate,
			})
			continue
		}
		asParent = append(asParent, ReportScheduleConflict{
			AsParent: true,
			GoalID:   c.Child.ID, Title: c.Child.Title, Owner: c.Child.Owner, Due: c.Child.DeliveryDate,
		})
	}
	for _, cs := range [][]ReportScheduleConflict{asChild, asParent} {
		slices.SortStableFunc(cs, func(a, b ReportScheduleConflict) int {
			return cmp.Or(
				a.Due.Compare(b.Due),
				cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
				cmp.Compare(a.GoalID, b.GoalID),
			)
		})
	}
	return append(asChild, asParent...)
}
