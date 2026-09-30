package testsupport

import (
	"context"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// MarkTopLevel marks g as a Top-level Goal on behalf of admin, failing the test
// on error. It is a scenario builder for arranging the org's root outcomes.
func (h *Harness) MarkTopLevel(admin domain.Account, g domain.Goal) domain.Goal {
	h.T.Helper()
	marked, err := h.Service.MarkTopLevel(context.Background(), admin.ID, g.ID)
	if err != nil {
		h.T.Fatalf("MarkTopLevel: %v", err)
	}
	return marked
}

// Unaligned returns the Unaligned Goals, failing the test on error.
func (h *Harness) Unaligned() []domain.Goal {
	h.T.Helper()
	goals, err := h.Service.UnalignedGoals(context.Background())
	if err != nil {
		h.T.Fatalf("UnalignedGoals: %v", err)
	}
	return goals
}

// ActiveGoalDue creates an Active Dated Goal owned by owner with the given
// delivery date and one Milestone ahead of it, failing the test on error. It is
// a scenario builder for arranging Goals whose delivery dates differ.
func (h *Harness) ActiveGoalDue(owner domain.Account, title string, due time.Time) domain.Goal {
	h.T.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, title, title+" matters.")
	if _, err := h.Service.MarkGoalDated(ctx, g.ID, due); err != nil {
		h.T.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Kickoff",
		TargetDate: h.Clock.Now().AddDate(0, 1, 0),
	}); err != nil {
		h.T.Fatalf("AddMilestone: %v", err)
	}
	active, err := h.Service.ActivateGoal(ctx, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}

// GraphSignals returns the org-wide graph signals, failing the test on error.
func (h *Harness) GraphSignals() domain.GraphSignals {
	h.T.Helper()
	signals, err := h.Service.GraphSignals(context.Background())
	if err != nil {
		h.T.Fatalf("GraphSignals: %v", err)
	}
	return signals
}

// GoalSignals returns the graph signals flagged on g, failing the test on error.
func (h *Harness) GoalSignals(g domain.Goal) domain.GoalSignals {
	h.T.Helper()
	signals, err := h.Service.GoalSignals(context.Background(), g.ID)
	if err != nil {
		h.T.Fatalf("GoalSignals: %v", err)
	}
	return signals
}
