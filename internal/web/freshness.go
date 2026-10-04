package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleFreshnessSignals shows the Stale list and the Goals whose Path to Green
// is overdue, surfaced as prominently as Red.
func (s *Server) handleFreshnessSignals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	signals, err := s.svc.FreshnessSignals(r.Context())
	if err != nil {
		s.serverError(w, r, "could not read freshness signals", err)
		return
	}
	render(w, r, http.StatusOK, freshnessSignalsPage(&current, signals))
}
