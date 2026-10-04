package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// LogDraftHighlight logs a Draft Highlight of the given kind ("" for none) and
// note on goal as author, failing the test on error. It is a scenario builder
// for arranging a Goal's pending Draft Highlights (CONTEXT.md: Draft
// Highlight).
func (h *Harness) LogDraftHighlight(author domain.Account, goalID int64, kind, note string) domain.DraftHighlight {
	h.T.Helper()
	d, err := h.Service.LogDraftHighlight(context.Background(), domain.LogDraftHighlightInput{
		GoalID: goalID, AuthorID: author.ID, Kind: kind, Note: note,
	})
	if err != nil {
		h.T.Fatalf("LogDraftHighlight: %v", err)
	}
	return d
}
