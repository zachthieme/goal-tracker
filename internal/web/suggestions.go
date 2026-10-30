package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"

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
	page, err := s.suggestParentForm(r, current, goalID, nil)
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "could not load goal", err)
		return
	}
	render(w, r, http.StatusOK, page)
}

// handleSuggestParent suggests the Goal named by parent_id as the path's
// Goal's parent, with an optional note, and returns to the Goal page. A
// refused suggestion comes back as the form with the reason, one picking no
// parent, or none that parses, beside the picker.
func (s *Server) handleSuggestParent(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	refuse := func(err error) {
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't suggest a parent", Back: goalPath(goalID),
			Form: asPage(func(err error) (templ.Component, error) {
				return s.suggestParentForm(r, current, goalID, err)
			})})
	}
	parentID, err := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	if err != nil {
		refuse(errNoParentSuggested)
		return
	}
	_, err = s.svc.SuggestParent(r.Context(), current.ID, goalID, parentID, r.FormValue("note"))
	if errors.Is(err, domain.ErrNotAuthorized) { // #701
		page, pageErr := s.suggestParentForm(r, current, goalID, err)
		if pageErr != nil {
			s.answerRefused(w, r, refused{Err: pageErr, Heading: "Can't suggest a parent", Back: goalPath(goalID)})
			return
		}
		render(w, r, http.StatusForbidden, page)
		return
	}
	if err != nil {
		refuse(err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/goals/%d", goalID), http.StatusSeeOther)
}

// errNoParentSuggested refuses a Suggest a parent submit that picks no
// parent, which the form says beside its picker.
var errNoParentSuggested = domain.Refuse(domain.ErrValidation, "pick a Goal for it to contribute to")

// suggestParentForm is goalID's Suggest a parent form and, when refused is
// set, its reason, beside the picker when no parent was picked
// (errNoParentSuggested) and above the form otherwise, and the note sent.
func (s *Server) suggestParentForm(r *http.Request, current domain.Account, goalID int64, refused error) (templ.Component, error) {
	goal, err := s.svc.ViewGoal(r.Context(), goalID)
	if err != nil {
		return nil, err
	}
	candidates, err := s.svc.SuggestableParents(r.Context(), goalID)
	if err != nil {
		return nil, fmt.Errorf("load goals: %w", err)
	}
	v := suggestParentView{Goal: goal, Candidates: candidates}
	if refused != nil {
		if errors.Is(refused, errNoParentSuggested) {
			v.PickerError = sentence(domain.Reason(refused))
		} else {
			v.Error = sentence(domain.Reason(refused))
		}
		v.Input = r.PostForm
	}
	return suggestParentPage(&current, v), nil
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
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't accept suggestion", Back: returnTo(r, goalPath(p.Goal.ID))})
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
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't decline suggestion", Back: back})
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
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't withdraw suggestion", Back: goalPath(p.Goal.ID)})
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
