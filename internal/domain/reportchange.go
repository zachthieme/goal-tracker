package domain

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// reportException is an exception block as DraftReport orders and counts it,
// with what it reads of the Goal beside the block, out of the snapshot: whether
// its Path to Green is overdue.
type reportException struct {
	block       ReportBlock
	pathOverdue bool
}

// sortReportExceptions puts the worst-off exceptions first, key by key, as the
// Risks page ranks Goals: Ownerless first; then by Health, Red, Yellow, none,
// then Green; then a Path to Green overdue first; then Stale first. Then those
// that changed since the baseline come first, new trouble ahead of standing
// trouble; then the soonest delivery date, with none last; then by title,
// ignoring case, then by Goal id.
func sortReportExceptions(xs []reportException) {
	slices.SortStableFunc(xs, func(a, b reportException) int {
		ga, gb := a.block.Goal, b.block.Goal
		if c := cmp.Compare(reportFlagRank(ga.Ownerless), reportFlagRank(gb.Ownerless)); c != 0 {
			return c
		}
		if c := cmp.Compare(reportHealthRank(a.block.Health), reportHealthRank(b.block.Health)); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(a.pathOverdue), reportFlagRank(b.pathOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(a.block.hasBadge(BadgeStale)), reportFlagRank(b.block.hasBadge(BadgeStale))); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(a.block.changedSinceBaseline()), reportFlagRank(b.block.changedSinceBaseline())); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(!ga.DeliveryDate.IsZero()), reportFlagRank(!gb.DeliveryDate.IsZero())); c != 0 {
			return c
		}
		if c := ga.DeliveryDate.Compare(gb.DeliveryDate); c != 0 {
			return c
		}
		if c := cmp.Compare(strings.ToLower(ga.Title), strings.ToLower(gb.Title)); c != 0 {
			return c
		}
		return cmp.Compare(ga.ID, gb.ID)
	})
}

// reportFlagRank ranks a condition a Report puts first: 0 when it holds.
func reportFlagRank(holds bool) int {
	if holds {
		return 0
	}
	return 1
}

// reportHealthRank orders Health worst first: Red, Yellow, none, then Green.
func reportHealthRank(health string) int {
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

// hasBadge reports whether the block carries badge.
func (b ReportBlock) hasBadge(badge string) bool {
	return slices.Contains(b.Badges, badge)
}

// changedSinceBaseline reports whether the block's Goal changed since the
// baseline: it says what changed, or is new.
func (b ReportBlock) changedSinceBaseline() bool {
	return len(b.Changes) > 0 || b.hasBadge(BadgeNew)
}

// healthChange is the phrase for how a block's Health changed since the
// baseline, from prior, its Health then, to now: "" when it didn't.
func healthChange(prior, now string) string {
	switch {
	case prior == "":
		return ""
	case now == HealthRed:
		return "Turned Red"
	case now == HealthYellow && prior == HealthGreen:
		return "Turned Yellow"
	case now == HealthYellow && prior == HealthRed:
		return "Improved to Yellow"
	case now == HealthGreen:
		return "Back to Green"
	}
	return ""
}

// deliveryMoveDays is how many days the Goal's delivery date moved since the
// baseline, net: its current date less the date it held before its first Date
// Slip since, later positive. It is 0 when the date didn't move, or the Goal
// has no delivery date now.
func (h goalHistory) deliveryMoveDays(since func(time.Time) bool) int {
	if h.goal.DeliveryDate.IsZero() {
		return 0
	}
	for _, d := range h.slips {
		if d.MilestoneID == 0 && since(d.CreatedAt) {
			return int(math.Round(h.goal.DeliveryDate.Sub(d.OldDate).Hours() / 24))
		}
	}
	return 0
}

// changeDuration says how long a move of days is: in days under two weeks,
// and in whole weeks, rounded, from two weeks on.
func changeDuration(days int) string {
	switch {
	case days == 1:
		return "1 day"
	case days < 14:
		return fmt.Sprintf("%d days", days)
	}
	return fmt.Sprintf("%d weeks", int(math.Round(float64(days)/7)))
}

// milestonesSlipped is how many of the Goal's Milestones have a Date Slip
// since the baseline, each counted once.
func (h goalHistory) milestonesSlipped(since func(time.Time) bool) int {
	var ids []int64
	for _, d := range h.slips {
		if d.MilestoneID != 0 && since(d.CreatedAt) && !slices.Contains(ids, d.MilestoneID) {
			ids = append(ids, d.MilestoneID)
		}
	}
	return len(ids)
}

// movedLifecycle reports whether the Goal of b, h, moved Lifecycle since the
// baseline as a change in its own right: a Goal created since is only New,
// though activating it moved it to Active.
func (h goalHistory) movedLifecycle(b ReportBlock, since func(time.Time) bool) bool {
	return h.lifecycleChanged(since) && !b.hasBadge(BadgeNew)
}

// reportChanges are the phrases for what changed on b's Goal, h, since the
// baseline, in a fixed order: its Health; its Path to Green's date passing,
// as fresh judged it, on or after the baseline date; its delivery date; its
// Milestones' dates; and its Lifecycle. The New badge already says a Goal is
// new, so no phrase repeats it.
func (h goalHistory) reportChanges(b ReportBlock, since func(time.Time) bool, baseline time.Time, fresh Freshness) []string {
	var out []string
	if c := healthChange(b.PriorHealth, b.Health); c != "" {
		out = append(out, c)
	}
	if fresh.PathToGreenOverdue && !fresh.PathTargetDate.Before(baseline) {
		out = append(out, "Path to Green date passed")
	}
	switch days := h.deliveryMoveDays(since); {
	case days > 0:
		out = append(out, "Delivery date slipped "+changeDuration(days))
	case days < 0:
		out = append(out, "Delivery date moved "+changeDuration(-days)+" earlier")
	}
	switch n := h.milestonesSlipped(since); {
	case n == 1:
		out = append(out, "1 Milestone slipped")
	case n > 1:
		out = append(out, fmt.Sprintf("%d Milestones slipped", n))
	}
	if h.movedLifecycle(b, since) {
		out = append(out, "Moved to "+b.Goal.Lifecycle)
	}
	return out
}
