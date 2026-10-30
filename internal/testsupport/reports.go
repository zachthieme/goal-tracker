package testsupport

import (
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// SaveReportDefinition saves a Report Definition on behalf of actor, failing the
// test on error. It is a scenario builder for arranging a saved report a test
// needs.
func (h *Harness) SaveReportDefinition(actor domain.Account, in domain.SaveReportDefinitionInput) domain.ReportDefinition {
	h.T.Helper()
	def, err := h.Service.SaveReportDefinition(h.T.Context(), actor.ID, in)
	if err != nil {
		h.T.Fatalf("SaveReportDefinition: %v", err)
	}
	return def
}

// PublishReport publishes the Report Definition def on behalf of actor against
// its default baseline, failing the test on error.
func (h *Harness) PublishReport(actor domain.Account, def domain.ReportDefinition) domain.Publication {
	h.T.Helper()
	pub, err := h.Service.PublishReport(h.T.Context(), actor.ID, def.ID, time.Time{})
	if err != nil {
		h.T.Fatalf("PublishReport: %v", err)
	}
	return pub
}

// PublicationFact is pub's first fact of the given kind, failing the test
// when pub has none. It is how a test reads what a publication froze.
func (h *Harness) PublicationFact(pub domain.Publication, kind domain.ReportFactKind) domain.ReportFact {
	h.T.Helper()
	for _, f := range pub.Facts {
		if f.Kind == kind {
			return f
		}
	}
	h.T.Fatalf("PublicationFact: publication %d has no %q fact", pub.ID, kind)
	return domain.ReportFact{}
}

// AddReportDelegate authorizes delegate to change, curate and publish def,
// acting as actor (its creator or an Admin), failing the test on error
// (CONTEXT.md: Report Delegate).
func (h *Harness) AddReportDelegate(actor, delegate domain.Account, def domain.ReportDefinition) {
	h.T.Helper()
	if err := h.Service.AddReportDelegate(h.T.Context(), actor.ID, def.ID, delegate.ID); err != nil {
		h.T.Fatalf("AddReportDelegate: %v", err)
	}
}
