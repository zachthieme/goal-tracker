package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// pdfRenderNever is a PDF renderer that fails the test if it runs.
type pdfRenderNever struct{ t *testing.T }

func (r pdfRenderNever) Render(context.Context, []byte, string) ([]byte, error) {
	r.t.Error("rendered a PDF with every render slot taken")
	return nil, nil
}

// With every render slot taken for longer than a download waits, the download
// is told the app is busy, a 503, and nothing renders.
func TestAPDFDownloadWithEveryRenderBusyIsUnavailable(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(boss, def)

	s := NewServer(h.Service, WithPDFRenderer(pdfRenderNever{t}))
	s.pdf.timeout = 50 * time.Millisecond
	for range cap(s.pdf.slots) {
		s.pdf.slots <- struct{}{}
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, publicationPath(pub)+"/pdf", nil)
	req.SetPathValue("id", strconv.FormatInt(def.ID, 10))
	req.SetPathValue("pub", strconv.FormatInt(pub.ID, 10))
	w := httptest.NewRecorder()
	s.handleDownloadPDF(w, req, boss)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503; body:\n%s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/pdf" {
		t.Errorf("a busy download is served as a PDF")
	}
}
