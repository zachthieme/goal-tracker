package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// HandleRefusedTopLevelForTest serves at pattern, among s's routes, a mark of
// the Goal in the path's {id} as Top-level that the domain refuses with err,
// answered as handleSetTopLevel answers it. The domain refuses Top-level only
// for who is asking, so the web_test package's tests reach any other refusal
// through this (#701).
func (s *Server) HandleRefusedTopLevelForTest(pattern string, err error) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, _ domain.Account) {
		goalID, ok := s.goalIDFromPath(w, r)
		if !ok {
			return
		}
		s.answerTopLevelCommand(w, r, goalID, err)
	}))
}
