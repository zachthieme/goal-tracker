package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// undoSuggestionDecline is the Undo offer of declining a Parent suggestion
// (see toast.go for the rest).
const undoSuggestionDecline = "parent-suggestion-decline"

// suggestParentView is what the Suggest a parent form shows: the Goal, the
// Goals that may be suggested as its parent, and, after a refused submit,
// why and what was sent.
type suggestParentView struct {
	Goal       domain.Goal
	Candidates []domain.Goal
	Error      string
	Input      url.Values
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
		s.renderSuggestParent(w, r, current, goalID, http.StatusConflict, errors.New("that Goal already contributes to this one, so linking them would make a cycle"))
	case errors.Is(err, domain.ErrValidation):
		s.renderSuggestParent(w, r, current, goalID, http.StatusUnprocessableEntity, err)
	case errors.Is(err, domain.ErrNotAuthorized):
		s.renderSuggestParent(w, r, current, goalID, http.StatusForbidden, err)
	case err != nil:
		http.Error(w, "could not suggest a parent", http.StatusInternalServerError)
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
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	candidates, err := s.svc.SuggestableParents(r.Context(), goalID)
	if err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	v := suggestParentView{Goal: goal, Candidates: candidates}
	if refused != nil {
		v.Error = sentence(plainReason(refused))
		v.Input = r.PostForm
	}
	render(w, r, status, suggestParentPage(&current, v))
}

// handleAcceptParentSuggestion accepts the open suggestion in the path, which
// requests its link as the Goal's Owner; only they may. It returns to the page
// the Accept came from (from: Home, or the Goal page).
func (s *Server) handleAcceptParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AcceptParentSuggestion(r.Context(), current.ID, p.ID); err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, requestPage(r.FormValue("from"), goalPath(p.Goal.ID)), http.StatusSeeOther)
}

// handleDeclineParentSuggestion declines the open suggestion in the path;
// only the Goal's Owner may. It returns to the page the Decline came from
// (from: Home, or the Goal page), which offers Undo once in a toast.
func (s *Server) handleDeclineParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	token, err := s.svc.DeclineParentSuggestion(r.Context(), current.ID, p.ID)
	if err != nil {
		writeLinkError(w, err)
		return
	}
	back := requestPage(r.FormValue("from"), goalPath(p.Goal.ID))
	offerUndo(w, back, undoSuggestionDecline, p.ID, token)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleUndoDeclineParentSuggestion puts the declined suggestion in the path
// back to open; only the person who declined it may, with the toast's token,
// and only once. It returns to the page the Undo came from (from: Home, or
// the Goal page), or refuses with a page saying why, linking back there, and
// changes nothing.
func (s *Server) handleUndoDeclineParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	back := "/home"
	id, ok := undoIDFromPath(w, r, current, back)
	if !ok {
		return
	}
	p, err := s.svc.ParentSuggestion(r.Context(), id)
	if err != nil {
		refuseUndo(w, r, current, back, err)
		return
	}
	back = requestPage(r.FormValue("from"), goalPath(p.Goal.ID))
	if err := s.svc.UndoDeclineParentSuggestion(r.Context(), current.ID, id, r.FormValue(undoField)); err != nil {
		refuseUndo(w, r, current, back, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleWithdrawParentSuggestion withdraws the open suggestion in the path;
// only the person who made it may. It returns to the Goal page.
func (s *Server) handleWithdrawParentSuggestion(w http.ResponseWriter, r *http.Request, current domain.Account) {
	p, ok := s.suggestionFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.WithdrawParentSuggestion(r.Context(), current.ID, p.ID); err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, goalPath(p.Goal.ID), http.StatusSeeOther)
}

// suggestionDeclineToast is the toast a page carries straight after current
// declined a Parent suggestion (offer), with the Undo that reopens it — or nil
// when the offer isn't for a declined suggestion on a Goal current Owns.
// goalID, when not 0, is the Goal page carrying it, which shows only its own
// Goal's. from names the page for the Undo to return to, as the Decline's own
// from did.
func (s *Server) suggestionDeclineToast(ctx context.Context, offer undoOffer, current domain.Account, goalID int64, from string) *toast {
	id, ok := offer.of(undoSuggestionDecline)
	if !ok {
		return nil
	}
	p, err := s.svc.ParentSuggestion(ctx, id)
	if err != nil || p.Status != domain.SuggestionDeclined || p.Goal.Owner.ID != current.ID ||
		(goalID != 0 && p.Goal.ID != goalID) {
		return nil
	}
	return &toast{
		Message: fmt.Sprintf("Suggestion declined: %s won't be linked to %s.", p.Goal.Title, p.Parent.Title),
		Undo:    fmt.Sprintf("/parent-suggestions/%d/undo-decline", p.ID),
		Fields:  offer.fields(fromField(from)...),
	}
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
		http.Error(w, "could not load the suggestion", http.StatusInternalServerError)
		return domain.ParentSuggestion{}, false
	}
	return p, true
}

// goalPath is goalID's Goal page.
func goalPath(goalID int64) string {
	return "/goals/" + strconv.FormatInt(goalID, 10)
}
