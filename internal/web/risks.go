package web

import (
	"context"
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleRisks answers "what's going wrong?" for leadership: every Goal the
// freshness and graph signals flag, by problem type.
func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, err := s.loadRisks(r.Context())
	if err != nil {
		http.Error(w, "could not read risks", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, risksPage(&current, v))
}

// risksView is what the Risks page shows, one list per problem type.
type risksView struct {
	Stale        []domain.GoalFreshness
	OverduePaths []domain.GoalFreshness
}

// loadRisks reads every Goal the org's signals flag.
func (s *Server) loadRisks(ctx context.Context) (risksView, error) {
	fresh, err := s.svc.FreshnessSignals(ctx)
	if err != nil {
		return risksView{}, err
	}
	return risksView{Stale: fresh.Stale, OverduePaths: fresh.OverduePaths}, nil
}
