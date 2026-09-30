package testsupport

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// ActiveGoal creates a Dated Goal owned by owner, gives it a Milestone, and
// activates it, failing the test on error. Check-ins can only be written on an
// Active Goal, so tests that exercise them arrange one through this builder.
func (h *Harness) ActiveGoal(owner domain.Account, title, soWhat string) domain.Goal {
	h.T.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, title, soWhat)
	if _, err := h.Service.MarkGoalDated(ctx, g.ID, h.Clock.Now().AddDate(0, 6, 0)); err != nil {
		h.T.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta",
		TargetDate: h.Clock.Now().AddDate(0, 3, 0),
	}); err != nil {
		h.T.Fatalf("AddMilestone: %v", err)
	}
	active, err := h.Service.ActivateGoal(ctx, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}

// Checkin submits a Check-in on goal as author, failing the test on error. It is
// a scenario builder for arranging a Goal's Check-in history.
func (h *Harness) Checkin(author domain.Account, goalID int64, health, status string, path string, pathTargetDate time.Time) domain.Checkin {
	h.T.Helper()
	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goalID,
		AuthorID:       author.ID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: pathTargetDate,
	})
	if err != nil {
		h.T.Fatalf("SubmitCheckin: %v", err)
	}
	return c
}
