package testsupport

import (
	"context"
	"encoding/json"
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

// PublishBeforeTheReportLog publishes def's draft as actor now, as a
// publication made before the Report log was kept: it has no Report log entry,
// and its snapshot names no publisher. It fails the test on error.
func (h *Harness) PublishBeforeTheReportLog(actor domain.Account, def domain.ReportDefinition) domain.Publication {
	h.T.Helper()
	ctx := context.Background()
	report, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		h.T.Fatalf("PublishBeforeTheReportLog: %v", err)
	}
	frozen, err := json.Marshal(report)
	if err != nil {
		h.T.Fatalf("PublishBeforeTheReportLog: %v", err)
	}
	if _, err := h.DB.Exec(`INSERT INTO report_publications (report_definition_id, published_by, published_at, snapshot) VALUES (?, ?, ?, ?)`,
		def.ID, actor.ID, h.Clock.Now().Format(time.RFC3339Nano), string(frozen)); err != nil {
		h.T.Fatalf("PublishBeforeTheReportLog: %v", err)
	}
	pubs, err := h.Service.ListPublications(ctx, def.ID)
	if err != nil || len(pubs) == 0 {
		h.T.Fatalf("PublishBeforeTheReportLog: ListPublications = %d publications, %v", len(pubs), err)
	}
	return pubs[0]
}

// AddReportDelegate authorizes delegate to change, curate and publish def,
// acting as actor (its creator or an Admin), failing the test on error
// (CONTEXT.md: Report Delegate).
func (h *Harness) AddReportDelegate(actor, delegate domain.Account, def domain.ReportDefinition) {
	h.T.Helper()
	if err := h.Service.AddReportDelegate(context.Background(), actor.ID, def.ID, delegate.ID); err != nil {
		h.T.Fatalf("AddReportDelegate: %v", err)
	}
}
