package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleAdmin is the home of the Admin tools: Dimensions, Import goals, and
// the Ownerless Goals waiting for an Admin to reassign them. Only an Admin may
// open it.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may open the Admin page", http.StatusForbidden)
		return
	}
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not read goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, adminPage(&current, awaitingReassignment(goals)))
}

// awaitingReassignment are the Ownerless Goals still open: a Done or Cancelled
// Goal waits on nobody.
func awaitingReassignment(goals []domain.Goal) []domain.Goal {
	var out []domain.Goal
	for _, g := range goals {
		if g.Ownerless && g.Lifecycle != domain.LifecycleDone && g.Lifecycle != domain.LifecycleCancelled {
			out = append(out, g)
		}
	}
	return out
}
