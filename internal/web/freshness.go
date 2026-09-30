package web

import (
	"context"
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleFreshnessSignals shows the Stale list and the Goals whose Path to Green
// is overdue, surfaced as prominently as Red.
func (s *Server) handleFreshnessSignals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	signals, err := s.svc.FreshnessSignals(r.Context())
	if err != nil {
		http.Error(w, "could not read freshness signals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, freshnessSignalsPage(&current, signals))
}

// flaggedFreshness maps each Stale Goal, and each Goal whose Path to Green is
// overdue, to its freshness, so the Goal list can mark their rows.
func (s *Server) flaggedFreshness(ctx context.Context) (map[int64]domain.Freshness, error) {
	signals, err := s.svc.FreshnessSignals(ctx)
	if err != nil {
		return nil, err
	}
	flagged := map[int64]domain.Freshness{}
	for _, gf := range append(signals.Stale, signals.OverduePaths...) {
		flagged[gf.Goal.ID] = gf.Freshness
	}
	return flagged, nil
}
