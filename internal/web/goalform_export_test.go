package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// HandleRefusedGoalFormForTest serves at pattern, among s's routes, a submit
// of the New goal page, or of the define page of the Goal in the path's {id}
// when the pattern has one, that the domain refuses with err, answered as
// handleCreateDefinedGoal and handleDefineGoal answer it. Each is refused for
// who is asking only when that changes between the page's check and the
// command's, so the web_test package's tests reach that answer through this
// (#701).
func (s *Server) HandleRefusedGoalFormForTest(pattern string, err error) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, current domain.Account) {
		v, in := readGoalForm(r, current)
		from := goalFormFrom{Posted: &v, In: in, ParentIDs: in.ParentIDs}
		if r.PathValue("id") != "" {
			g, ok := s.definableGoal(w, r, current)
			if !ok {
				return
			}
			from.Goal = &g
		}
		s.answerGoalFormRefused(w, r, current, from, err)
	}))
}
