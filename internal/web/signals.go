package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleSetTopLevel marks the Goal in the path a Top-level Goal when top_level
// is true, and unmarks it when false. Only an Admin may (CONTEXT.md: Top-level
// Goal): anyone else gets the refusal page, 403, linking Back to the Goal.
func (s *Server) handleSetTopLevel(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	topLevel, err := strconv.ParseBool(r.FormValue("top_level"))
	if err != nil {
		http.Error(w, "top_level must be true or false", http.StatusBadRequest)
		return
	}
	if topLevel {
		_, err = s.svc.MarkTopLevel(r.Context(), current.ID, goalID)
	} else {
		_, err = s.svc.UnmarkTopLevel(r.Context(), current.ID, goalID)
	}
	if errors.Is(err, domain.ErrValidation) { // #701
		s.serverError(w, r, "could not set Top-level", err)
		return
	}
	if err != nil {
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't change Top-level", Back: goalPath(goalID)})
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}
