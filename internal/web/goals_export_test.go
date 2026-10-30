package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// HandleRefusedCreateGoalForTest serves at pattern, among s's routes, a
// create of a Goal that the domain refuses with err, answered as
// handleCreateGoal answers it. A signed-in person always creates their own
// Goal, and a Departed one can't sign in, so the web_test package's tests
// reach a create refused for who is asking through this (#701).
func (s *Server) HandleRefusedCreateGoalForTest(pattern string, err error) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, _ domain.Account) {
		s.answerCreateGoal(w, r, err)
	}))
}
