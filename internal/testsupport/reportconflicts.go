package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// ActiveGoalDueLater creates an Active Dated Goal owned by owner with the given
// delivery date and one Milestone a week before it, failing the test on
// error. Unlike ActiveGoalDue's, its Milestone stays ahead as long as its
// date does, so a test can let a Report's baseline pass and still check it in
// Green.
func (h *Harness) ActiveGoalDueLater(owner domain.Account, title string, due time.Time) domain.Goal {
	h.T.Helper()
	ctx := h.T.Context()
	g := h.CreateGoal(owner, title, title+" matters.")
	if _, err := h.Service.MarkGoalDated(ctx, owner.ID, g.ID, due); err != nil {
		h.T.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestoneAsAuthor(ctx, owner.ID, domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Kickoff",
		TargetDate: due.AddDate(0, 0, -7),
	}); err != nil {
		h.T.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	active, err := h.Service.ActivateGoal(ctx, owner.ID, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}

// AcceptedLink links child as contributing to parent through an accepted link,
// requested by child's Owner and accepted by parent's, failing the test on
// error. It is a scenario builder for arranging links between Goals whose
// Owners differ, as a Schedule conflict between orgs is.
func (h *Harness) AcceptedLink(child, parent domain.Goal) {
	h.T.Helper()
	link := h.RequestLink(child.Owner, child, parent, "")
	if link.Status == domain.LinkAccepted {
		return
	}
	if _, err := h.Service.AcceptLink(h.T.Context(), link.ID, parent.Owner.ID); err != nil {
		h.T.Fatalf("AcceptLink: %v", err)
	}
}
