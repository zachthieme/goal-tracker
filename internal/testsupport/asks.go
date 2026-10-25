package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// AskForHelp submits a Check-in on goal as author whose Path to Green asks for
// help, failing the test on error (CONTEXT.md: Ask for help). It is the
// scenario builder for arranging an open ask; health is Yellow or Red.
func (h *Harness) AskForHelp(author domain.Account, goalID int64, health, status, path string, pathTargetDate time.Time) domain.Checkin {
	h.T.Helper()
	c, err := h.Service.SubmitCheckin(h.T.Context(), domain.SubmitCheckinInput{
		GoalID:         goalID,
		AuthorID:       author.ID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: pathTargetDate,
		AsksForHelp:    true,
	})
	if err != nil {
		h.T.Fatalf("SubmitCheckin asking for help: %v", err)
	}
	return c
}
