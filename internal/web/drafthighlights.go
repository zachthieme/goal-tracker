package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleLogDraftHighlight logs a Draft Highlight on the path's Goal, written by
// the current Account, and returns to the Goal page (CONTEXT.md: Draft
// Highlight). A refused note comes back on the Goal page, the reason in the
// Draft Highlights block and what was typed still in its form.
func (s *Server) handleLogDraftHighlight(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.LogDraftHighlight(r.Context(), domain.LogDraftHighlightInput{
		GoalID:   goalID,
		AuthorID: current.ID,
		Kind:     r.FormValue("kind"),
		Note:     r.FormValue("note"),
	})
	s.writeFormResult(w, r, goalID, current, formDraftHighlight, err)
}

// handleDeleteDraftHighlight deletes the path's pending Draft Highlight and
// returns to its Goal's page.
func (s *Server) handleDeleteDraftHighlight(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	draft, err := s.svc.DeleteDraftHighlight(r.Context(), current.ID, id)
	switch {
	case err == nil:
		http.Redirect(w, r, goalPath(draft.GoalID), http.StatusSeeOther)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, plainReason(err), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
	default:
		s.serverError(w, r, "could not delete draft highlight", err)
	}
}
