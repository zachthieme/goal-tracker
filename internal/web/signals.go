package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleGraphSignals shows the risks the Goal graph flags that nobody reported:
// the Unaligned Goals, the schedule conflicts, and the Goals whose parent is On
// Hold or Cancelled.
func (s *Server) handleGraphSignals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	signals, err := s.svc.GraphSignals(r.Context())
	if err != nil {
		http.Error(w, "could not read graph signals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, graphSignalsPage(&current, signals))
}

// handleSetTopLevel marks the Goal in the path a Top-level Goal when top_level
// is true, and unmarks it when false. Only an Admin may (CONTEXT.md: Top-level
// Goal).
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
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotAuthorized):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, domain.ErrNotFound):
			s.notFound(w, r)
		default:
			http.Error(w, "could not set Top-level", http.StatusInternalServerError)
		}
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}
