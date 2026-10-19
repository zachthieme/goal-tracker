package web

import (
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Check-in page's Dates section sums up the Goal's dates in one line: what
// kind of Goal it is, with its delivery date when Dated, then how many Planned
// Milestones it has and which comes next (ticket #366).
func TestCheckinContextDates(t *testing.T) {
	t.Parallel()

	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	dated := domain.Goal{Kind: domain.GoalDated, DeliveryDate: day(time.July, 2)}
	milestone := func(name string, at time.Time, status string) domain.Milestone {
		return domain.Milestone{Name: name, TargetDate: at, Status: status}
	}
	dates := displayDates{Now: testsupport.Epoch, Loc: time.UTC}
	for _, tc := range []struct {
		name       string
		goal       domain.Goal
		milestones []domain.Milestone
		want       string
	}{
		{"Ongoing with no Milestones", domain.Goal{Kind: domain.GoalOngoing}, nil, "Ongoing · no Milestones"},
		{"Dated with two Planned Milestones", dated, []domain.Milestone{
			milestone("Alpha", day(time.January, 20), domain.MilestoneDone),
			milestone("Kickoff", day(time.February, 2), domain.MilestonePlanned),
			milestone("Beta", day(time.April, 2), domain.MilestonePlanned),
		}, "Delivers Jul 2 · 2 planned Milestones, next Kickoff Feb 2"},
		{"Dated with one Planned Milestone", dated, []domain.Milestone{
			milestone("Beta", day(time.April, 2), domain.MilestonePlanned),
		}, "Delivers Jul 2 · 1 planned Milestone, next Beta Apr 2"},
		{"Dated whose only Milestone is Done", dated, []domain.Milestone{
			milestone("Beta", day(time.April, 2), domain.MilestoneDone),
		}, "Delivers Jul 2 · no planned Milestones"},
		{"Dated with no delivery date", domain.Goal{Kind: domain.GoalDated}, nil, "Dated · no Milestones"},
		{"no Kind", domain.Goal{}, nil, "Not yet Dated or Ongoing · no Milestones"},
	} {
		if got := checkinContextDates(tc.goal, tc.milestones, dates); got != tc.want {
			t.Errorf("%s: checkinContextDates = %q, want %q", tc.name, got, tc.want)
		}
	}
}
