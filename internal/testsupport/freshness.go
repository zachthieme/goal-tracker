package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Freshness returns goalID's freshness signals, failing the test on error.
func (h *Harness) Freshness(goalID int64) domain.Freshness {
	h.T.Helper()
	f, err := h.Service.Freshness(context.Background(), goalID)
	if err != nil {
		h.T.Fatalf("Freshness: %v", err)
	}
	return f
}
