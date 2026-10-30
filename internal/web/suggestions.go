package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// suggestParentView is what the Suggest a parent form shows: the Goal, the
// Goals that may be suggested as its parent, and, after a refused submit,
// why and what was sent. PickerError is why a submit picking no parent was
// refused, beside the picker; Error is any other refusal, above the form.
type suggestParentView struct {
	Goal        domain.Goal
	Candidates  []domain.Goal
	Error       string
	PickerError string
	Input       url.Values
}

// handleSuggestParentForm shows the form by which someone other than the
// Goal's Owner suggests a parent for it (CONTEXT.md: Parent suggestion).
func (s *Server) handleSuggestParentForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	s.renderSuggestParent(w, r, current, goalID, http.StatusOK, nil)
}

// handleSuggestParent suggests the Goal named by parent_id as the path's
// Goal's parent, with an optional note, and returns to the Goal page. A
// refused suggestion comes back as the form with the reason.
func (s *Server) handleSuggestParent(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	parentID, err := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	if err != nil {
		s.renderSuggestParent(w, r, current, goalID, http.StatusUnprocessableEntity, errors.New("pick a parent Goal to suggest"))
		return
	}
	_, err = s.svc.SuggestParent(r.Context(), current.ID, goalID, parentID, r.FormValue("note"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, domain.ErrCycle):
		s.renderSuggestParent(w, r, current, goalID, http.StatusConflict, errLinkWouldCycle)
	case errors.Is(err, domain.ErrValidation):
		s.renderSuggestParent(w, r, current, goalID, http.StatusUnprocessableEntity, err)
	case errors.Is(err, domain.ErrNotAuthorized):
		s.renderSuggestParent(w, r, current, goalID, http.StatusForbidden, err)
	case err != nil:
		s.serverError(w, r, "could not suggest a parent", err)
	default:
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", goalID), http.StatusSeeOther)
	}
}

// renderSuggestParent renders goalID's Suggest a parent form with status,
// and, when refused is set, its reason and what was sent.
func (s *Server) renderSuggestParent(w http.ResponseWriter, r *http.Request, current domain.Account, goalID int64, status int, refused error) {
	goal, err := s.svc.ViewGoal(r.Context(), goalID)
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "could not load goal", err)
		return
	}
	candidates, err := s.svc.SuggestableParents(r.Context(), goalID)
	if err != nil {
		s.serverError(w, r, "could not load goals", err)
		return
	}
	v := suggestParentView{Goal: goal, Candidates: candidates}
	if refused != nil {
		v.Error = sentence(domain.Reason(refused))
		v.Input = r.PostForm
	}
	render(w, r, status, suggestParentPage(&current, v))
}

// handleAcceptParentSuggestion accepts the open suggestion in the path, which
// requests its link as the Goal's Owner; only they may. It returns to the page
// the Accept came from (its return: Home, or else the Goal page), or refuses
// with a page saying why, linking back there.
func (s *Server) handleAcceptParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AcceptParentSuggestion(r.Context(), current.ID, p.ID); err != nil {
		s.refuse(w, r, "Can't accept suggestion", returnTo(r, goalPath(p.Goal.ID)), err)
		return
	}
	http.Redirect(w, r, returnTo(r, goalPath(p.Goal.ID)), http.StatusSeeOther)
}

// handleDeclineParentSuggestion declines the open suggestion in the path;
// only the Goal's Owner may. It returns to the page the Decline came from
// (its return: Home, or else the Goal page), which offers Undo once in a
// toast, or refuses with a page saying why, linking back there.
func (s *Server) handleDeclineParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	back := returnTo(r, goalPath(p.Goal.ID))
	token, err := s.svc.DeclineParentSuggestion(r.Context(), current.ID, p.ID)
	if err != nil {
		s.refuse(w, r, "Can't decline suggestion", back, err)
		return
	}
	s.offerUndo(w, r, undoSuggestionDecline, p.ID, token, back)
}

// handleWithdrawParentSuggestion withdraws the open suggestion in the path;
// only the person who made it may. It returns to the Goal page, or refuses
// with a page saying why, linking back there.
func (s *Server) handleWithdrawParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.WithdrawParentSuggestion(r.Context(), current.ID, p.ID); err != nil {
		s.refuse(w, r, "Can't withdraw suggestion", goalPath(p.Goal.ID), err)
		return
	}
	http.Redirect(w, r, goalPath(p.Goal.ID), http.StatusSeeOther)
}

// suggestionFromPath loads the Parent suggestion in the path, answering 404
// when there's none.
func (s *Server) suggestionFromPath(w http.ResponseWriter, r *http.Request) (domain.ParentSuggestion, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return domain.ParentSuggestion{}, false
	}
	p, err := s.svc.ParentSuggestion(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return domain.ParentSuggestion{}, false
	}
	if err != nil {
		s.serverError(w, r, "could not load the suggestion", err)
		return domain.ParentSuggestion{}, false
	}
	return p, true
}

// goalPath is goalID's Goal page.
func goalPath(goalID int64) string {
	return "/goals/" + strconv.FormatInt(goalID, 10)
}
