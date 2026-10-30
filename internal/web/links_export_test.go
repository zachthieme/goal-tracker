package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// HandleRefusedLinkCycleForTest serves at pattern, among s's routes, a
// handler that answers as a link request refused for making a cycle does, for
// the signed-in Account and the Goal in the path's {id}, whether or not the
// Goal page offers them the link form: offered, it redirects to the Goal
// page, where that form is open with the reason beside it and what was sent
// in it. No request reaches the cycle check without that form offered, so the
// web_test package's tests reach the refusal page through this (#573).
func (s *Server) HandleRefusedLinkCycleForTest(pattern string) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, current domain.Account) {
		goalID, ok := s.goalIDFromPath(w, r)
		if !ok {
			return
		}
		s.refuseGoalForm(w, r, goalID, current, formParentLink, "Can't change Goal",
			domain.Refuse(domain.ErrCycle, "linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”"))
	}))
}
