package domain

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// ReportReason is one exception reason: one reason a Report judged a selected
// Goal an exception, a full block rather than one line (CONTEXT.md: Report).
// They aren't the membership reasons a Goal entered or left a Report for. The
// strings are frozen in publications (ReportBlockFacts.Reasons), so renaming
// one needs a publicationRecordVersion bump.
type ReportReason string

// The exception reasons, in the order a judgment lists them
// (reportReasonOrder). The first six are standing trouble; the rest are what
// changed since the baseline.
const (
	ReportReasonOwnerless             ReportReason = "ownerless"
	ReportReasonRed                   ReportReason = "red"
	ReportReasonYellow                ReportReason = "yellow"
	ReportReasonPathToGreenOverdue    ReportReason = "path to green overdue"
	ReportReasonCheckinOverdue        ReportReason = "check-in overdue"
	ReportReasonScheduleConflict      ReportReason = "schedule conflict"
	ReportReasonNew                   ReportReason = "new"
	ReportReasonTurnedRed             ReportReason = "turned red"
	ReportReasonTurnedYellow          ReportReason = "turned yellow"
	ReportReasonImprovedToYellow      ReportReason = "improved to yellow"
	ReportReasonBackToGreen           ReportReason = "back to green"
	ReportReasonPathToGreenDatePassed ReportReason = "path to green date passed"
	ReportReasonDeliveryLater         ReportReason = "delivery date later"
	ReportReasonDeliveryEarlier       ReportReason = "delivery date earlier"
	ReportReasonMilestoneSlipped      ReportReason = "milestone slipped"
	ReportReasonMovedLifecycle        ReportReason = "moved lifecycle"
	ReportReasonDateSlipped           ReportReason = "date slipped"
)

// reportReasonOrder is every exception reason in the order a judgment lists
// them.
var reportReasonOrder = []ReportReason{
	ReportReasonOwnerless, ReportReasonRed, ReportReasonYellow, ReportReasonPathToGreenOverdue,
	ReportReasonCheckinOverdue, ReportReasonScheduleConflict, ReportReasonNew,
	ReportReasonTurnedRed, ReportReasonTurnedYellow, ReportReasonImprovedToYellow, ReportReasonBackToGreen,
	ReportReasonPathToGreenDatePassed, ReportReasonDeliveryLater, ReportReasonDeliveryEarlier,
	ReportReasonMilestoneSlipped, ReportReasonMovedLifecycle, ReportReasonDateSlipped,
}

// reportReasonTraits are what an exception reason counts as. exception: it
// makes its Goal an exception. changed: it counts the Goal changed for the
// Reports list (changedGoals). rankChanged: it ranks the Goal as changed
// (reportRankTiers). movement: the ReportMovement count it adds one to, nil
// for none.
type reportReasonTraits struct {
	exception, changed, rankChanged bool
	movement                        func(*ReportMovement) *int
}

// reportReasonTraitsOf are each exception reason's traits.
var reportReasonTraitsOf = map[ReportReason]reportReasonTraits{
	ReportReasonOwnerless:             {exception: true},
	ReportReasonRed:                   {exception: true},
	ReportReasonYellow:                {exception: true},
	ReportReasonPathToGreenOverdue:    {exception: true},
	ReportReasonCheckinOverdue:        {exception: true},
	ReportReasonScheduleConflict:      {exception: true},
	ReportReasonNew:                   {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.New }},
	ReportReasonTurnedRed:             {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.TurnedRed }},
	ReportReasonTurnedYellow:          {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.TurnedYellow }},
	ReportReasonImprovedToYellow:      {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.ImprovedToYellow }},
	ReportReasonBackToGreen:           {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.BackToGreen }},
	ReportReasonPathToGreenDatePassed: {exception: true, rankChanged: true},
	ReportReasonDeliveryLater:         {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.Slipped }},
	ReportReasonDeliveryEarlier:       {exception: true, changed: true, rankChanged: true},
	ReportReasonMilestoneSlipped:      {exception: true, changed: true, rankChanged: true},
	ReportReasonMovedLifecycle:        {exception: true, changed: true, rankChanged: true, movement: func(m *ReportMovement) *int { return &m.ChangedLifecycle }},
	ReportReasonDateSlipped:           {exception: true, changed: true},
}

// reportReasonsAny reports whether any of reasons has the trait holds picks.
func reportReasonsAny(reasons []ReportReason, holds func(reportReasonTraits) bool) bool {
	return slices.ContainsFunc(reasons, func(r ReportReason) bool { return holds(reportReasonTraitsOf[r]) })
}

// reportReasonsMakeException reports whether reasons make their Goal an
// exception.
func reportReasonsMakeException(reasons []ReportReason) bool {
	return reportReasonsAny(reasons, func(t reportReasonTraits) bool { return t.exception })
}

// reportReasonsChanged reports whether reasons count their Goal changed since
// the baseline for the Reports list.
func reportReasonsChanged(reasons []ReportReason) bool {
	return reportReasonsAny(reasons, func(t reportReasonTraits) bool { return t.changed })
}

// reportReasonsRankChanged reports whether reasons rank their Goal as changed
// since the baseline, new trouble ahead of standing trouble.
func reportReasonsRankChanged(reasons []ReportReason) bool {
	return reportReasonsAny(reasons, func(t reportReasonTraits) bool { return t.rankChanged })
}

// countReportReasons adds one to m's count for each of reasons that counts as
// movement.
func countReportReasons(m *ReportMovement, reasons []ReportReason) {
	for _, r := range reasons {
		if count := reportReasonTraitsOf[r].movement; count != nil {
			*count(m)++
		}
	}
}

// reportJudgment is a Report's judgment of one selected Goal: its exception
// reasons, in reportReasonOrder, and the numbers its Changes phrases say — how
// many days its delivery date moved since the baseline, net and later
// positive, and how many of its Milestones slipped since.
type reportJudgment struct {
	reasons           []ReportReason
	deliveryMoveDays  int
	milestonesSlipped int
}

// judgeReportGoal judges a selected Goal, h, once: every exception reason it
// holds since the baseline, which since counts from and which falls on the
// date baseline, as fresh judged its Freshness, conflicted when it is in a
// Schedule conflict. It is the one place a Report detects an exception reason.
func judgeReportGoal(h goalHistory, since func(time.Time) bool, baseline time.Time, fresh Freshness, conflicted bool) reportJudgment {
	j := reportJudgment{deliveryMoveDays: h.deliveryMoveDays(since), milestonesSlipped: h.milestonesSlipped(since)}
	health := h.latest().Health
	created := since(h.goal.CreatedAt)
	for _, c := range []struct {
		reason ReportReason
		holds  bool
	}{
		{ReportReasonOwnerless, h.goal.Ownerless},
		{ReportReasonRed, health == HealthRed},
		{ReportReasonYellow, health == HealthYellow},
		{ReportReasonPathToGreenOverdue, fresh.PathToGreenOverdue},
		{ReportReasonCheckinOverdue, fresh.CheckinOverdue},
		{ReportReasonScheduleConflict, conflicted},
		{ReportReasonNew, created},
		{reportHealthReason(h.priorHealth(since), health), true},
		// pathToGreenPastTarget only holds for a Red or Yellow Goal.
		{ReportReasonPathToGreenDatePassed, fresh.PathToGreenOverdue && !fresh.PathTargetDate.Before(baseline)},
		{ReportReasonDeliveryLater, j.deliveryMoveDays > 0},
		{ReportReasonDeliveryEarlier, j.deliveryMoveDays < 0},
		{ReportReasonMilestoneSlipped, j.milestonesSlipped > 0},
		// A Goal created since is only New, though activating it moved it to
		// Active.
		{ReportReasonMovedLifecycle, h.lifecycleChanged(since) && !created},
		{ReportReasonDateSlipped, slices.ContainsFunc(h.slips, func(d DateSlip) bool { return since(d.CreatedAt) })},
	} {
		if c.holds && c.reason != "" {
			j.reasons = append(j.reasons, c.reason)
		}
	}
	return j
}

// reportHealthReason is the exception reason for how a Goal's Health changed
// since the baseline, from prior, its Health then (goalHistory.priorHealth),
// to now: "" when it didn't.
func reportHealthReason(prior, now string) ReportReason {
	switch {
	case prior == "":
		return ""
	case now == HealthRed:
		return ReportReasonTurnedRed
	case now == HealthYellow && prior == HealthGreen:
		return ReportReasonTurnedYellow
	case now == HealthYellow && prior == HealthRed:
		return ReportReasonImprovedToYellow
	case now == HealthGreen:
		return ReportReasonBackToGreen
	}
	return ""
}

// reportRankTiers are the tiers a Report ranks its exceptions by, most urgent
// first, a block holding a tier ahead of one that doesn't (CONTEXT.md:
// Report). Every tier but one is an exception reason, or for the last any
// that ranks as changed, new trouble ahead of standing trouble. The fifth,
// Green last, is the block's Health: after a Path to Green overdue, which
// only Red and Yellow hold, a block with no Health comes ahead of a Green one,
// as HealthUrgencyRank orders them.
var reportRankTiers = []func(ReportBlock) bool{
	reportBlockHolds(ReportReasonOwnerless),
	reportBlockHolds(ReportReasonRed),
	reportBlockHolds(ReportReasonYellow),
	reportBlockHolds(ReportReasonPathToGreenOverdue),
	func(b ReportBlock) bool { return b.Health != HealthGreen },
	reportBlockHolds(ReportReasonCheckinOverdue),
	reportBlockHolds(ReportReasonScheduleConflict),
	func(b ReportBlock) bool { return reportReasonsRankChanged(b.Reasons) },
}

// reportBlockHolds is the rank tier of blocks holding reason.
func reportBlockHolds(reason ReportReason) func(ReportBlock) bool {
	return func(b ReportBlock) bool { return slices.Contains(b.Reasons, reason) }
}

// sortReportExceptions puts the most urgent exceptions first: tier by tier
// (reportRankTiers); then the soonest delivery date, with none last; then by
// title, ignoring case, then by Goal id. The Follow-ups page ranks otherwise
// (sortFollowUpRows): it puts Active Goals ahead of the rest before any of
// these keys, and a Milestone overdue and then an overdue Action Item between
// a Path to Green overdue and Check-in overdue, none of which a Report ranks
// by; and a Report's ranking by Schedule conflict, change and delivery date is
// its own.
func sortReportExceptions(blocks []ReportBlock) {
	slices.SortStableFunc(blocks, func(a, b ReportBlock) int {
		for _, holds := range reportRankTiers {
			if c := cmp.Compare(reportFlagRank(holds(a)), reportFlagRank(holds(b))); c != 0 {
				return c
			}
		}
		ga, gb := a.Goal, b.Goal
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
