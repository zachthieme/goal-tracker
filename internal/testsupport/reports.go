package testsupport

import (
	"context"
	"time"

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

// PublishReport publishes the Report Definition def on behalf of actor against
// its default baseline, failing the test on error.
func (h *Harness) PublishReport(actor domain.Account, def domain.ReportDefinition) domain.Publication {
	h.T.Helper()
	pub, err := h.Service.PublishReport(context.Background(), actor.ID, def.ID, time.Time{})
	if err != nil {
		h.T.Fatalf("PublishReport: %v", err)
	}
	return pub
}
