package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// HandleRefusedDelegateAddForTest serves at pattern, among s's routes, an add
// of a Delegate on the Goal in the path's {id} that the domain refuses with
// err, answered as handleAddDelegate answers it. The domain never refuses a
// Delegate change for a cycle, so the web_test package's tests reach that
// answer through this (#701).
func (s *Server) HandleRefusedDelegateAddForTest(pattern string, err error) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, current domain.Account) {
		goalID, ok := s.goalIDFromPath(w, r)
		if !ok {
			return
		}
		s.answerDelegateCommand(w, r, goalID, current, "Can't add Delegate", err)
	}))
}
