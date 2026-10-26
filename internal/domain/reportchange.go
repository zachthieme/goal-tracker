package domain

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// ReportMovement counts the Goals that moved since a Report's baseline, for
// the line the Report opens with (CONTEXT.md: Report). A Goal counts in every
// count it fits.
type ReportMovement struct {
	// TurnedRed, TurnedYellow, ImprovedToYellow and BackToGreen count the
	// Goals whose Health changed since the baseline, by what it changed to:
	// Red; Yellow from Green; Yellow from Red; Green.
	TurnedRed, TurnedYellow, ImprovedToYellow, BackToGreen int
	// Slipped counts the Goals whose delivery date is later than it was at
	// the baseline, ChangedLifecycle those that moved Lifecycle, and New
	// those created since. Entered and Left count the Goals that entered or
	// left the Report since its previous publication, a new Goal only as new;
	// a Report read against a date counts neither.
	Slipped, ChangedLifecycle, New, Entered, Left int
}

// Parts are the movement's counts that aren't zero, in its fields' order,
// each as it reads in the line ("2 turned Red").
func (m ReportMovement) Parts() []string {
	var out []string
	for _, c := range []struct {
		n    int
		what string
	}{
		{m.TurnedRed, "turned Red"},
		{m.TurnedYellow, "turned Yellow"},
		{m.ImprovedToYellow, "improved to Yellow"},
		{m.BackToGreen, "back to Green"},
		{m.Slipped, "slipped"},
		{m.ChangedLifecycle, "changed Lifecycle"},
		{m.New, "new"},
		{m.Entered, "entered"},
		{m.Left, "left"},
	} {
		if c.n > 0 {
			out = append(out, fmt.Sprintf("%d %s", c.n, c.what))
		}
	}
	return out
}

// reportMovement counts what moved on r since its baseline from its
// exceptions, as every Goal that changed is one, and from its membership
// changes when r reads against its previous publication, the publication they
// are read against.
func reportMovement(r Report, exceptions []reportException, since func(time.Time) bool) *ReportMovement {
	var m ReportMovement
	for _, x := range exceptions {
		b := x.block
		switch {
		case b.PriorHealth == "":
		case b.Health == HealthRed:
			m.TurnedRed++
		case b.Health == HealthYellow && b.PriorHealth == HealthGreen:
			m.TurnedYellow++
		case b.Health == HealthYellow && b.PriorHealth == HealthRed:
			m.ImprovedToYellow++
		case b.Health == HealthGreen:
			m.BackToGreen++
		}
		if x.slipped {
			m.Slipped++
		}
		if x.movedLifecycle {
			m.ChangedLifecycle++
		}
		if b.hasBadge(BadgeNew) {
			m.New++
		}
	}
	if r.Previous.ID != 0 {
		for _, c := range r.MembershipChanges {
			switch {
			case c.Direction == MembershipLeft:
				m.Left++
			case !since(c.Goal.CreatedAt):
				m.Entered++
			}
		}
	}
	return &m
}

// reportException is an exception block as DraftReport orders and counts it,
// with what it reads of the Goal beside the block, out of the snapshot: whether
// its Path to Green is overdue, its delivery date slipped, and it moved
// Lifecycle since the baseline.
type reportException struct {
	block                                ReportBlock
	pathOverdue, slipped, movedLifecycle bool
}

// sortReportExceptions puts the most urgent exceptions first, key by key, in
// the Report's order (CONTEXT.md: Report): Ownerless first; then by Health
// (HealthUrgencyRank); then a Path to Green overdue first; then Check-in
// overdue first. Then those that changed since the baseline come first, new
// trouble ahead of standing trouble; then the soonest delivery date, with none
// last; then by title, ignoring case, then by Goal id. The Follow-ups page
// ranks otherwise (sortFollowUpRows): it puts Active Goals ahead of the rest
// before any of these keys, and a Milestone overdue and then an overdue Action
// Item between a Path to Green overdue and Check-in overdue, none of which a
// Report ranks by; and a Report's ranking by change and delivery date is its
// own.
func sortReportExceptions(xs []reportException) {
	slices.SortStableFunc(xs, func(a, b reportException) int {
		ga, gb := a.block.Goal, b.block.Goal
		if c := cmp.Compare(reportFlagRank(ga.Ownerless), reportFlagRank(gb.Ownerless)); c != 0 {
			return c
		}
		if c := cmp.Compare(HealthUrgencyRank(a.block.Health), HealthUrgencyRank(b.block.Health)); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(a.pathOverdue), reportFlagRank(b.pathOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(reportFlagRank(a.block.hasBadge(BadgeCheckinOverdue)), reportFlagRank(b.block.hasBadge(BadgeCheckinOverdue))); c != 0 {
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

// HealthUrgencyRank orders Health most urgent first: Red, Yellow, none, then
// Green. The Follow-ups page and a Report both rank by it.
func HealthUrgencyRank(health string) int {
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
		if d.OnDeliveryDate() && since(d.CreatedAt) {
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
		out = append(out, "Delivery date moved "+changeDuration(days)+" later")
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
