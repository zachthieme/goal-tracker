package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// ActiveGoal creates a Dated Goal owned by owner, gives it a Milestone, and
// activates it, failing the test on error. Check-ins can only be written on an
// Active Goal, so tests that exercise them arrange one through this builder.
func (h *Harness) ActiveGoal(owner domain.Account, title, whyItMatters string) domain.Goal {
	h.T.Helper()
	ctx := h.T.Context()
	g := h.CreateGoal(owner, title, whyItMatters)
	if _, err := h.Service.MarkGoalDated(ctx, owner.ID, g.ID, h.Clock.Now().AddDate(0, 6, 0)); err != nil {
		h.T.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestoneAsAuthor(ctx, owner.ID, domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta",
		TargetDate: h.Clock.Now().AddDate(0, 3, 0),
	}); err != nil {
		h.T.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	active, err := h.Service.ActivateGoal(ctx, owner.ID, g.ID)
	if err != nil {
		h.T.Fatalf("ActivateGoal: %v", err)
	}
	return active
}

// Checkin submits a Check-in on goal as author, failing the test on error. It is
// a scenario builder for arranging a Goal's Check-in history.
func (h *Harness) Checkin(author domain.Account, goalID int64, health, status string, path string, pathTargetDate time.Time) domain.Checkin {
	h.T.Helper()
	c, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
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

// CheckinMovingPath submits a Check-in on goal as author whose back-to-Green
// date may be later than the latest Check-in's, giving reason for moving it
// (CONTEXT.md: Date Slip), failing the test on error.
func (h *Harness) CheckinMovingPath(author domain.Account, goalID int64, health, status, path string, pathTargetDate time.Time, reason string) domain.Checkin {
	h.T.Helper()
	c, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
		GoalID:               goalID,
		AuthorID:             author.ID,
		Health:               health,
		Status:               status,
		PathToGreen:          path,
		PathTargetDate:       pathTargetDate,
		PathTargetDateReason: reason,
	})
	if err != nil {
		h.T.Fatalf("SubmitCheckin moving the back-to-Green date: %v", err)
	}
	return c
}

// CheckinWithHighlight submits a Green Check-in on goal as author carrying a
// Highlight of the given kind and note, failing the test on error. It is a
// scenario builder for arranging a Goal's Highlights.
func (h *Harness) CheckinWithHighlight(author domain.Account, goalID int64, kind, note string) domain.Checkin {
	h.T.Helper()
	return h.CheckinWithHighlights(author, goalID, domain.HighlightInput{Kind: kind, Note: note})
}

// CheckinWithHighlights submits a Green Check-in on goal as author carrying the
// given Highlights in order, failing the test on error.
func (h *Harness) CheckinWithHighlights(author domain.Account, goalID int64, highlights ...domain.HighlightInput) domain.Checkin {
	h.T.Helper()
	c, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
		GoalID:     goalID,
		AuthorID:   author.ID,
		Health:     domain.HealthGreen,
		Status:     "Check-in with Highlights.",
		Highlights: highlights,
	})
	if err != nil {
		h.T.Fatalf("SubmitCheckin with highlight: %v", err)
	}
	return c
}

// OnHoldGoal creates an Active Goal owned by owner and puts it On Hold in a
// Check-in with the given reason, failing the test on error.
func (h *Harness) OnHoldGoal(owner domain.Account, title, whyItMatters, reason string) domain.Goal {
	h.T.Helper()
	g := h.ActiveGoal(owner, title, whyItMatters)
	if _, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
		GoalID:          g.ID,
		AuthorID:        owner.ID,
		Status:          "Putting this On Hold.",
		Lifecycle:       domain.LifecycleOnHold,
		LifecycleReason: reason,
	}); err != nil {
		h.T.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	g.Lifecycle = domain.LifecycleOnHold
	return g
}

// EndGoalInCheckin takes goal, Active, to Done or Cancelled in a Check-in by owner,
// failing the test on error.
func (h *Harness) EndGoalInCheckin(owner domain.Account, goalID int64, lifecycle string) {
	h.T.Helper()
	if _, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
		GoalID:          goalID,
		AuthorID:        owner.ID,
		Status:          "Wrapping this up.",
		Lifecycle:       lifecycle,
		LifecycleReason: "No longer needed.",
		Outcome:         "It shipped.",
	}); err != nil {
		h.T.Fatalf("SubmitCheckin to %s: %v", lifecycle, err)
	}
}
