package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleLogDraftHighlight logs a Draft Highlight on the path's Goal, written by
// the current Account (CONTEXT.md: Draft Highlight). A plain post returns to
// the Goal page, and a refused note comes back on it, the reason in the Draft
// Highlights block and what was typed still in its form. The Highlight
// editor's htmx post gets the block back either way (renderDraftHighlights).
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
	if r.Header.Get("HX-Request") == "true" && (err == nil || errors.Is(err, domain.ErrValidation)) {
		s.renderDraftHighlights(w, r, goalID, current, draftHighlightRefusalOf(r, 0, err))
		return
	}
	s.writeFormResult(w, r, goalID, current, formDraftHighlight, err)
}

// handleEditDraftHighlight changes the path's pending Draft Highlight to the
// kind and note sent and returns to its Goal's page, or for the Highlight
// editor's htmx post, gives back the Draft Highlights block, the refused
// edit open in it again (renderDraftHighlights).
func (s *Server) handleEditDraftHighlight(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	draft, err := s.svc.EditDraftHighlight(r.Context(), current.ID, id, r.FormValue("kind"), r.FormValue("note"))
	if errors.Is(err, domain.ErrValidation) && r.Header.Get("HX-Request") == "true" {
		// The edit was refused for what was typed, so the Draft Highlight
		// exists and the viewer may see it.
		if refused, lookErr := s.svc.DraftHighlight(r.Context(), current.ID, id); lookErr == nil {
			s.renderDraftHighlights(w, r, refused.GoalID, current, draftHighlightRefusalOf(r, id, err))
			return
		}
	}
	s.writeDraftHighlightResult(w, r, draft.GoalID, current, "could not edit draft highlight", err)
}

// handleDeleteDraftHighlight deletes the path's pending Draft Highlight and
// returns to its Goal's page, or gives the Highlight editor's htmx post the
// Draft Highlights block without it.
func (s *Server) handleDeleteDraftHighlight(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	draft, err := s.svc.DeleteDraftHighlight(r.Context(), current.ID, id)
	s.writeDraftHighlightResult(w, r, draft.GoalID, current, "could not delete draft highlight", err)
}

// writeDraftHighlightResult answers an edit or delete of a Draft Highlight on
// goalID: done, a plain post returns to the Goal page and an htmx one gets the
// Draft Highlights block as it now stands. Refused, it is a 422 for what was
// typed, a 403 for someone who may not, and a 404 for one no longer pending.
func (s *Server) writeDraftHighlightResult(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, failed string, err error) {
	switch {
	case err == nil && r.Header.Get("HX-Request") == "true":
		s.renderDraftHighlights(w, r, goalID, current, draftHighlightRefusal{})
	case err == nil:
		http.Redirect(w, r, goalPath(goalID), http.StatusSeeOther)
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, sentence(domain.Reason(err)), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, domain.Reason(err), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
	default:
		s.serverError(w, r, failed, err)
	}
}

// draftHighlightRefusal is a Save from the Highlight editor that the Goal page's
// Draft Highlights block refused, to show again in the editor: the Draft
// Highlight edited (0 for one being logged), why, and the kind and note typed.
// Its zero value is no refusal.
type draftHighlightRefusal struct {
	ID     int64
	Reason string
	Kind   string
	Note   string
}

// draftHighlightRefusalOf is err, if it refused the post r for its values, as
// a refusal of the Save editing id (0 for logging one).
func draftHighlightRefusalOf(r *http.Request, id int64, err error) draftHighlightRefusal {
	if !errors.Is(err, domain.ErrValidation) {
		return draftHighlightRefusal{}
	}
	return draftHighlightRefusal{ID: id, Reason: sentence(domain.Reason(err)), Kind: r.PostForm.Get("kind"), Note: r.PostForm.Get("note")}
}

// renderDraftHighlights answers the Highlight editor's htmx post with the Goal
// page's Draft Highlights block for htmx to swap in place, with 200 even for
// a refusal, as htmx swaps no error status. A refused log comes back with the
// block's log form open, its reason and what was typed in it, for the editor
// to open on; a refused edit marks its Draft Highlight the same way. When the
// page no longer offers the log form, such as once the Goal is Done, a refused
// log or edit has no editor to open on: the reason comes back on its own, as
// renderRefusedForm sends it.
func (s *Server) renderDraftHighlights(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, refused draftHighlightRefusal) {
	view, err := s.goalPageView(r.Context(), goalID, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, "could not load goal", err)
		return
	}
	if refused.Reason != "" && !view.offers(formDraftHighlight) {
		http.Error(w, refused.Reason, http.StatusUnprocessableEntity)
		return
	}
	if refused.Reason != "" && refused.ID == 0 {
		view.Open, view.FormError, view.FormInput = formDraftHighlight, refused.Reason, r.PostForm
	}
	render(w, r, http.StatusOK, draftHighlightBlock(view, refused))
}
