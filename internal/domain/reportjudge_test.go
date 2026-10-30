package domain

import (
	"slices"
	"testing"
	"time"
)

// Every exception reason makes its Goal an exception; some count it changed
// for the Reports list, some rank it as changed, and some add one to a count
// in the line a Report opens with (CONTEXT.md: Report).
func TestReportReasonsSayWhatEachCountsAs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		reason               ReportReason
		changed, rankChanged bool
		movement             ReportMovement
	}{
		{reason: ReportReasonOwnerless},
		{reason: ReportReasonRed},
		{reason: ReportReasonYellow},
		{reason: ReportReasonPathToGreenOverdue},
		{reason: ReportReasonCheckinOverdue},
		{reason: ReportReasonScheduleConflict},
		{reason: ReportReasonNew, changed: true, rankChanged: true, movement: ReportMovement{New: 1}},
		{reason: ReportReasonTurnedRed, changed: true, rankChanged: true, movement: ReportMovement{TurnedRed: 1}},
		{reason: ReportReasonTurnedYellow, changed: true, rankChanged: true, movement: ReportMovement{TurnedYellow: 1}},
		{reason: ReportReasonImprovedToYellow, changed: true, rankChanged: true, movement: ReportMovement{ImprovedToYellow: 1}},
		{reason: ReportReasonBackToGreen, changed: true, rankChanged: true, movement: ReportMovement{BackToGreen: 1}},
		{reason: ReportReasonPathToGreenDatePassed, rankChanged: true},
		{reason: ReportReasonDeliveryLater, changed: true, rankChanged: true, movement: ReportMovement{Slipped: 1}},
		{reason: ReportReasonDeliveryEarlier, changed: true, rankChanged: true},
		{reason: ReportReasonMilestoneSlipped, changed: true, rankChanged: true},
		{reason: ReportReasonMovedLifecycle, changed: true, rankChanged: true, movement: ReportMovement{ChangedLifecycle: 1}},
		{reason: ReportReasonDateSlipped, changed: true},
	}
	if len(cases) != len(reportReasonOrder) {
		t.Errorf("the table covers %d reasons, want all %d", len(cases), len(reportReasonOrder))
	}
	for i, tc := range cases {
		t.Run(string(tc.reason), func(t *testing.T) {
			if i < len(reportReasonOrder) && reportReasonOrder[i] != tc.reason {
				t.Errorf("reason %d is %q, want %q", i, reportReasonOrder[i], tc.reason)
			}
			reasons := []ReportReason{tc.reason}
			if !reportReasonsMakeException(reasons) {
				t.Error("makes no exception, want one")
			}
			if got := reportReasonsChanged(reasons); got != tc.changed {
				t.Errorf("changed = %v, want %v", got, tc.changed)
			}
			if got := reportReasonsRankChanged(reasons); got != tc.rankChanged {
				t.Errorf("ranks changed = %v, want %v", got, tc.rankChanged)
			}
			var m ReportMovement
			countReportReasons(&m, reasons)
			if m != tc.movement {
				t.Errorf("movement %+v, want %+v", m, tc.movement)
			}
		})
	}
}

// A Goal with no reasons is no exception, no change and no movement.
func TestNoReportReasonsMakeNoException(t *testing.T) {
	t.Parallel()

	if reportReasonsMakeException(nil) || reportReasonsChanged(nil) || reportReasonsRankChanged(nil) {
		t.Error("no reasons judged an exception or a change, want neither")
	}
	var m ReportMovement
	countReportReasons(&m, nil)
	if m != (ReportMovement{}) {
		t.Errorf("movement %+v, want none", m)
	}
}

// rankedBlock is an exception block to rank, holding only what a Report ranks
// by.
func rankedBlock(id int64, title string, health string, due time.Time, reasons ...ReportReason) ReportBlock {
	return ReportBlock{Goal: Goal{ID: id, Title: title, DeliveryDate: due}, Health: health, Reasons: reasons}
}

// rankedTitles lists blocks' titles in order.
func rankedTitles(blocks []ReportBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, b.Goal.Title)
	}
	return out
}

// A Report ranks its exceptions tier by tier, a block holding a tier ahead of
// one that doesn't: Ownerless, Red, Yellow, a Path to Green overdue, any
// Health but Green, Check-in overdue, a Schedule conflict, then changed since
// the baseline. Each pair here is two adjacent tiers, the lower block winning
// every tie-break, so only its tier puts the higher block first (CONTEXT.md:
// Report).
func TestReportRanksItsExceptionsTierByTier(t *testing.T) {
	t.Parallel()

	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	// higher sorts last by every tie-break: due later, its title after, its ID
	// greater.
	higher := func(health string, reasons ...ReportReason) ReportBlock {
		return rankedBlock(2, "Z higher", health, day(9, 1), reasons...)
	}
	lower := func(health string, reasons ...ReportReason) ReportBlock {
		return rankedBlock(1, "A lower", health, day(7, 1), reasons...)
	}
	cases := []struct {
		name          string
		higher, lower ReportBlock
	}{
		{"Ownerless before Red", higher(HealthGreen, ReportReasonOwnerless), lower(HealthRed, ReportReasonRed)},
		{"Red before Yellow", higher(HealthRed, ReportReasonRed), lower(HealthYellow, ReportReasonYellow)},
		{"Yellow before a Path to Green overdue", higher(HealthYellow, ReportReasonYellow), lower("", ReportReasonPathToGreenOverdue)},
		{"a Path to Green overdue before no Health", higher(HealthGreen, ReportReasonPathToGreenOverdue), lower("", ReportReasonCheckinOverdue)},
		{"no Health before Check-in overdue", higher("", ReportReasonScheduleConflict), lower(HealthGreen, ReportReasonCheckinOverdue)},
		{"no Health before Green, both Check-in overdue", higher("", ReportReasonCheckinOverdue), lower(HealthGreen, ReportReasonCheckinOverdue)},
		{"Check-in overdue before a Schedule conflict", higher(HealthGreen, ReportReasonCheckinOverdue), lower(HealthGreen, ReportReasonScheduleConflict)},
		{"a Schedule conflict before a change", higher(HealthGreen, ReportReasonScheduleConflict), lower(HealthGreen, ReportReasonNew)},
		{"a change before a date slipped alone", higher(HealthGreen, ReportReasonMilestoneSlipped, ReportReasonDateSlipped), lower(HealthGreen, ReportReasonDateSlipped)},
		{"a Path to Green date passed before a date slipped alone", higher(HealthYellow, ReportReasonPathToGreenDatePassed), lower(HealthYellow, ReportReasonDateSlipped)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, blocks := range [][]ReportBlock{{tc.higher, tc.lower}, {tc.lower, tc.higher}} {
				sortReportExceptions(blocks)
				if got, want := rankedTitles(blocks), []string{"Z higher", "A lower"}; !slices.Equal(got, want) {
					t.Errorf("ranked %q, want %q", got, want)
				}
			}
		})
	}
}

// Exceptions alike in every tier go by their delivery date, one dated ahead of
// one with none and the soonest first; then by title, ignoring case; then by
// Goal ID (CONTEXT.md: Report).
func TestReportBreaksTiesBetweenExceptionsAlikeInEveryTier(t *testing.T) {
	t.Parallel()

	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name          string
		first, second ReportBlock
	}{
		{"a delivery date before none", rankedBlock(2, "Z dated", HealthRed, day(9, 1), ReportReasonRed), rankedBlock(1, "A undated", HealthRed, time.Time{}, ReportReasonRed)},
		{"the soonest delivery date first", rankedBlock(2, "Z sooner", HealthRed, day(7, 1), ReportReasonRed), rankedBlock(1, "A later", HealthRed, day(9, 1), ReportReasonRed)},
		{"by title, ignoring case", rankedBlock(2, "apple", HealthRed, day(7, 1), ReportReasonRed), rankedBlock(1, "Banana", HealthRed, day(7, 1), ReportReasonRed)},
		{"by Goal ID", rankedBlock(1, "Same", HealthRed, day(7, 1), ReportReasonRed), rankedBlock(2, "Same", HealthRed, day(7, 1), ReportReasonRed)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, blocks := range [][]ReportBlock{{tc.first, tc.second}, {tc.second, tc.first}} {
				sortReportExceptions(blocks)
				if got, want := []int64{blocks[0].Goal.ID, blocks[1].Goal.ID}, []int64{tc.first.Goal.ID, tc.second.Goal.ID}; !slices.Equal(got, want) {
					t.Errorf("ranked Goals %v, want %v", got, want)
				}
			}
		})
	}
}
