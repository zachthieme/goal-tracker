package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// MarkDeparted has admin record that person has left the company, failing the
// test on error. It is a scenario builder for arranging a Departed person
// (CONTEXT.md: Departed), such as a Manager an Ask for help walks past.
func (h *Harness) MarkDeparted(admin, person domain.Account) {
	h.T.Helper()
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, person.ID); err != nil {
		h.T.Fatalf("MarkDeparted(%s): %v", person.Email, err)
	}
}
