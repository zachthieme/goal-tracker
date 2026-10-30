package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleLogDraftHighlight logs a Draft Highlight on the path's Goal, written by
// the current Account (CONTEXT.md: Draft Highlight). A plain post returns to
// the Goal page, and a refused note redirects to the Goal page, where the log
// form is open with the reason above it and what was sent in it
// (goalPageForm). The Highlight editor's htmx post gets the block back
// either way (draftHighlightsBlock).
// A log refused for anything but what was typed, plain or htmx, is the
// refusal page, headed Can't change highlight and linking Back to the Goal.
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
	if err != nil {
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't change highlight", Back: goalPath(goalID),
			Form: s.goalPageForm(goalID, current, formDraftHighlight),
			Partial: func(err error) (templ.Component, error) {
				return s.draftHighlightsBlock(r.Context(), goalID, current, draftHighlightRefusalOf(r, 0, err))
			}})
		return
	}
	s.answerDraftHighlightsChanged(w, r, goalID, current)
}

// handleEditDraftHighlight changes the path's pending Draft Highlight to the
// kind and note sent and returns to its Goal's page, or for the Highlight
// editor's htmx post, gives back the Draft Highlights block, a refused
// edit open in it again (draftHighlightsBlock).
func (s *Server) handleEditDraftHighlight(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	draft, err := s.svc.EditDraftHighlight(r.Context(), current.ID, id, r.FormValue("kind"), r.FormValue("note"))
	if err != nil {
		s.answerRefused(w, r, s.refusedDraftHighlight(r, id, current, err, true))
		return
	}
	s.answerDraftHighlightsChanged(w, r, draft.GoalID, current)
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
	if err != nil {
		s.answerRefused(w, r, s.refusedDraftHighlight(r, id, current, err, false))
		return
	}
	s.answerDraftHighlightsChanged(w, r, draft.GoalID, current)
}

// refusedDraftHighlight is an edit or delete of the Draft Highlight id
// refused with err, headed Can't change highlight. A refused change returns
// no Draft Highlight, so the way back is read again: its Goal page, or Home
// for a viewer who may not see it. A refused edit from the Highlight editor
// (editing) comes back open in the Draft Highlights block, when it can be
// read.
func (s *Server) refusedDraftHighlight(r *http.Request, id int64, current domain.Account, err error, editing bool) refused {
	rf := refused{Err: err, Heading: "Can't change highlight", Back: "/home"}
	draft, lookErr := s.svc.DraftHighlight(r.Context(), current.ID, id)
	if lookErr != nil {
		return rf
	}
	rf.Back = goalPath(draft.GoalID)
	if editing {
		rf.Partial = func(err error) (templ.Component, error) {
			return s.draftHighlightsBlock(r.Context(), draft.GoalID, current, draftHighlightRefusalOf(r, id, err))
		}
	}
	return rf
}

// answerDraftHighlightsChanged answers a change to the Goal goalID's Draft
// Highlights, done: a plain post returns to its Goal page and the Highlight
// editor's htmx post gets the Draft Highlights block as it now stands.
func (s *Server) answerDraftHighlightsChanged(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account) {
	if !isHTMX(r) {
		http.Redirect(w, r, goalPath(goalID), http.StatusSeeOther)
		return
	}
	block, err := s.draftHighlightsBlock(r.Context(), goalID, current, draftHighlightRefusal{})
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "could not load goal", err)
		return
	}
	answerSwap(w, r, block)
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

// draftHighlightsBlock is the Goal page's Draft Highlights block for the
// Highlight editor's htmx post to swap in place (answerSwap). A refused log
// comes back with the block's log form open, its reason and what was typed in
// it, for the editor to open on; a refused edit marks its Draft Highlight the
// same way. When the page no longer offers the log form, such as once the
// Goal is Done, a refused log or edit has no editor to open on, so there is
// no block (nil) and the reason comes back on the refusal page.
func (s *Server) draftHighlightsBlock(ctx context.Context, goalID int64, current domain.Account, refused draftHighlightRefusal) (templ.Component, error) {
	view, err := s.goalPageView(ctx, goalID, current)
	if err != nil {
		return nil, err
	}
	if refused.Reason != "" && !view.offers(formDraftHighlight) {
		return nil, nil
	}
	if refused.Reason != "" && refused.ID == 0 {
		view.Open, view.FormError = formDraftHighlight, refused.Reason
		view.FormInput = url.Values{"kind": {refused.Kind}, "note": {refused.Note}}
	}
	return draftHighlightBlock(view, refused), nil
}
