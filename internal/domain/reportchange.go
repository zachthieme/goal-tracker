package domain

import (
	"fmt"
	"math"
	"slices"
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
// exceptions' reasons, as every Goal that changed is one, and from its
// membership changes when r reads against its previous publication, the
// publication they are read against.
func reportMovement(r Report, since func(time.Time) bool) *ReportMovement {
	var m ReportMovement
	for _, b := range r.Exceptions {
		countReportReasons(&m, b.Reasons)
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

// HealthUrgencyRank orders Health most urgent first: Red, Yellow, none, then
// Green. The Follow-ups page ranks by it, and a Report's rank tiers order
// Health alike (reportRankTiers).
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

// healthChange is the phrase for an exception reason saying how a block's
// Health changed since the baseline (reportHealthReason): "" for any other.
func healthChange(reason ReportReason) string {
	switch reason {
	case ReportReasonTurnedRed:
		return "Turned Red"
	case ReportReasonTurnedYellow:
		return "Turned Yellow"
	case ReportReasonImprovedToYellow:
		return "Improved to Yellow"
	case ReportReasonBackToGreen:
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

// reportChanges are the phrases for what changed on a Goal since the
// baseline, picked from j's reasons in their order: its Health; its Path to
// Green's date passing on or after the baseline date; its delivery date; its
// Milestones' dates; and its Lifecycle, now lifecycle. The New badge already
// says a Goal is new, so no phrase repeats it, and a date slip says nothing
// of its own.
func (j reportJudgment) reportChanges(lifecycle string) []string {
	var out []string
	for _, r := range j.reasons {
		switch r {
		case ReportReasonTurnedRed, ReportReasonTurnedYellow, ReportReasonImprovedToYellow, ReportReasonBackToGreen:
			out = append(out, healthChange(r))
		case ReportReasonPathToGreenDatePassed:
			out = append(out, "Path to Green date passed")
		case ReportReasonDeliveryLater:
			out = append(out, "Delivery date moved "+changeDuration(j.deliveryMoveDays)+" later")
		case ReportReasonDeliveryEarlier:
			out = append(out, "Delivery date moved "+changeDuration(-j.deliveryMoveDays)+" earlier")
		case ReportReasonMilestoneSlipped:
			if j.milestonesSlipped == 1 {
				out = append(out, "1 Milestone slipped")
			} else {
				out = append(out, fmt.Sprintf("%d Milestones slipped", j.milestonesSlipped))
			}
		case ReportReasonMovedLifecycle:
			out = append(out, "Moved to "+lifecycle)
		}
	}
	return out
}
