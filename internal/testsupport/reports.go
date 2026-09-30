package testsupport

import (
	"context"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// SaveReportDefinition saves a Report Definition on behalf of actor, failing the
// test on error. It is a scenario builder for arranging a saved report a test
// needs.
func (h *Harness) SaveReportDefinition(actor domain.Account, in domain.SaveReportDefinitionInput) domain.ReportDefinition {
	h.T.Helper()
	def, err := h.Service.SaveReportDefinition(context.Background(), actor.ID, in)
	if err != nil {
		h.T.Fatalf("SaveReportDefinition: %v", err)
	}
	return def
}
