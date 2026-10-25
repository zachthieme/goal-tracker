package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Freshness returns goalID's freshness signals, failing the test on error.
func (h *Harness) Freshness(goalID int64) domain.Freshness {
	h.T.Helper()
	f, err := h.Service.Freshness(h.T.Context(), goalID)
	if err != nil {
		h.T.Fatalf("Freshness: %v", err)
	}
	return f
}

// GreenGoalWithOverdueMilestone arranges an ActiveGoal owned by owner whose
// latest Check-in is Green while its Beta Milestone is one day overdue: it
// moves the clock to two days before Beta, checks in Green, then moves the
// clock on three days. Checked in three days ago on a weekly cadence, the Goal
// isn't Check-in overdue. It returns the Goal and Beta.
func (h *Harness) GreenGoalWithOverdueMilestone(owner domain.Account, title string) (domain.Goal, domain.Milestone) {
	h.T.Helper()
	g := h.ActiveGoal(owner, title, "It matters.")
	milestones, err := h.Service.ListMilestones(h.T.Context(), g.ID)
	if err != nil || len(milestones) != 1 {
		h.T.Fatalf("ListMilestones = %+v, %v; want just Beta", milestones, err)
	}
	beta := milestones[0]
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, -2).Add(12 * time.Hour))
	h.Checkin(owner, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(3 * 24 * time.Hour)
	return g, beta
}
