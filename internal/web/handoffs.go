package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleStartHandoff starts a Handoff of the Goal in the path to the Account
// named by to_email. Only the Goal's current Owner or an Admin may start it, and
// it takes effect only once the new Owner accepts.
func (s *Server) handleStartHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.StartHandoffByEmail(r.Context(), goalID, r.FormValue("to_email"), current.ID); err != nil {
		s.writeRefusedHandoff(w, r, goalID, current, formHandoff, err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

// handlePendingHandoffs shows the current Account the Handoffs awaiting their
// acceptance as the proposed new Owner.
func (s *Server) handlePendingHandoffs(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pending, err := s.svc.PendingHandoffs(r.Context(), current.ID)
	if err != nil {
		http.Error(w, "could not list pending handoffs", http.StatusInternalServerError)
		return
	}
	toast := s.handoffRejectionToast(r.Context(), takeUndo(w, r), current, "")
	render(w, r, http.StatusOK, pendingHandoffsPage(&current, pending, toast))
}

// handleAcceptHandoff accepts a pending Handoff; only the new Owner may. Each
// checked keep box names a Delegate to keep; the Goal's other Delegates who
// aren't Departed are removed (CONTEXT.md: Delegate).
func (s *Server) handleAcceptHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := handoffIDFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	keep := make([]int64, 0, len(r.PostForm["keep"]))
	for _, v := range r.PostForm["keep"] {
		delegateID, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "bad Delegate to keep", http.StatusBadRequest)
			return
		}
		keep = append(keep, delegateID)
	}
	if _, err := s.svc.AcceptHandoff(r.Context(), id, current.ID, keep); err != nil {
		writeHandoffError(w, err)
		return
	}
	http.Redirect(w, r, "/handoffs", http.StatusSeeOther)
}

// handleRejectHandoff rejects a pending Handoff; only the new Owner may. It
// returns to the page the reject came from (from: Home, or the pending page),
// which offers Undo once in a toast.
func (s *Server) handleRejectHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := handoffIDFromPath(w, r)
	if !ok {
		return
	}
	token, err := s.svc.RejectHandoff(r.Context(), id, current.ID)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	back := requestPage(r.FormValue("from"), "/handoffs")
	offerUndo(w, back, undoHandoffRejection, id, token)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleRestoreHandoff puts the rejected Handoff in the path back as pending;
// only the person who rejected it may, with the toast's token, and only once.
// It returns to the page
// the Undo came from (from), or refuses with a message and changes nothing.
func (s *Server) handleRestoreHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := handoffIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.RestoreHandoff(r.Context(), id, current.ID, r.FormValue(undoField)); err != nil {
		writeHandoffError(w, err)
		return
	}
	http.Redirect(w, r, requestPage(r.FormValue("from"), "/handoffs"), http.StatusSeeOther)
}

// handoffRejectionToast is the toast a page carries straight after current
// rejected a Handoff (offer), with the Undo that puts it back — or nil when
// the offer isn't for a Handoff current rejected and can still undo. from
// names the page for the Undo to return to, as the reject's own from did.
func (s *Server) handoffRejectionToast(ctx context.Context, offer undoOffer, current domain.Account, from string) *toast {
	id, ok := offer.of(undoHandoffRejection)
	if !ok {
		return nil
	}
	ho, err := s.svc.Handoff(ctx, id)
	if err != nil || ho.Status != domain.HandoffRejected || ho.To.ID != current.ID {
		return nil
	}
	return &toast{
		Message: fmt.Sprintf("Handoff rejected: %s stays with %s.", ho.Goal.Title, ho.From.Label()),
		Undo:    fmt.Sprintf("/handoffs/%d/restore", ho.ID),
		Fields:  offer.fields(fromField(from)...),
	}
}

// handleDepartAccount marks the Account in the path as departed, making the Goals
// they still own Ownerless. Only an Admin may.
func (s *Server) handleDepartAccount(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.svc.MarkDeparted(r.Context(), current.ID, id); err != nil {
		writeHandoffError(w, err)
		return
	}
	redirectBack(w, r)
}

// handleReturnAccount reverses the departure of the Account in the path, so the
// Goals they still own stop being Ownerless and they can sign in again. Only an
// Admin may.
func (s *Server) handleReturnAccount(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.svc.MarkReturned(r.Context(), current.ID, id); err != nil {
		writeHandoffError(w, err)
		return
	}
	redirectBack(w, r)
}

// handleReassignGoal reassigns an Ownerless Goal to the Account named by email.
// Only an Admin may.
func (s *Server) handleReassignGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.ReassignGoalByEmail(r.Context(), current.ID, goalID, r.FormValue("email")); err != nil {
		s.writeRefusedHandoff(w, r, goalID, current, formReassign, err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

// writeRefusedHandoff answers a refused Hand off or Reassign from the Goal
// page: a refused new Owner re-renders the page with the form open and the
// reason beside it; anything else is writeHandoffError's.
func (s *Server) writeRefusedHandoff(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, form goalForm, err error) {
	if errors.Is(err, domain.ErrValidation) {
		s.renderRefusedForm(w, r, goalID, current, form, http.StatusUnprocessableEntity, err)
		return
	}
	writeHandoffError(w, err)
}

// redirectBack returns to wherever an action was triggered, usually a Goal page.
func redirectBack(w http.ResponseWriter, r *http.Request) {
	if ref := r.Header.Get("Referer"); ref != "" {
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

func handoffIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// writeHandoffError maps a domain Handoff/Ownerless error to an HTTP status.
func writeHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "handoff action failed", http.StatusInternalServerError)
	}
}
