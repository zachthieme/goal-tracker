package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleStartHandoff starts a Handoff of the Goal in the path to the Account
// named by to_email. Only the Goal's current Owner or an Admin may start it, and
// it takes effect only once the new Owner accepts.
func (s *Server) handleStartHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.StartHandoffByEmail(r.Context(), goalID, r.FormValue("to_email"), current.ID); err != nil {
		s.writeRefusedHandoff(w, r, goalID, current, formHandoff, "Can't hand off Goal", err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

// handlePendingHandoffs shows the current Account the Handoffs awaiting their
// acceptance as the proposed new Owner, and those they started that await
// someone else's.
func (s *Server) handlePendingHandoffs(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pending, err := s.svc.PendingHandoffs(r.Context(), current.ID)
	if err != nil {
		s.serverError(w, r, "could not list pending handoffs", err)
		return
	}
	started, err := s.svc.StartedHandoffs(r.Context(), current.ID)
	if err != nil {
		s.serverError(w, r, "could not list started handoffs", err)
		return
	}
	render(w, r, http.StatusOK, pendingHandoffsPage(&current, pending, started, s.undoToast(w, r, current)))
}

// handleAcceptHandoff accepts a pending Handoff; only the new Owner may. Each
// checked keep box names a Delegate to keep; the Goal's other Delegates who
// aren't Departed are removed (CONTEXT.md: Delegate).
func (s *Server) handleAcceptHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.handoffIDFromPath(w, r)
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
		s.refuse(w, r, "Can't accept Handoff", returnTo(r, "/handoffs"), err)
		return
	}
	http.Redirect(w, r, "/handoffs", http.StatusSeeOther)
}

// handleRejectHandoff rejects a pending Handoff; only the new Owner may. It
// returns to the page the reject came from (its return: Home, or else the
// pending page), which offers Undo once in a toast.
func (s *Server) handleRejectHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.handoffIDFromPath(w, r)
	if !ok {
		return
	}
	token, err := s.svc.RejectHandoff(r.Context(), id, current.ID)
	if err != nil {
		s.refuse(w, r, "Can't reject Handoff", returnTo(r, "/handoffs"), err)
		return
	}
	s.offerUndo(w, r, undoHandoffRejection, id, token, returnTo(r, "/handoffs"))
}

// handleCancelHandoff cancels a pending Handoff; only the person who started
// it or an Admin may. It comes back to the page the cancel came from (its
// return: the Goal, or else the pending page).
func (s *Server) handleCancelHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.handoffIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.CancelHandoff(r.Context(), id, current.ID); err != nil {
		s.refuse(w, r, "Can't cancel Handoff", returnTo(r, "/handoffs"), err)
		return
	}
	http.Redirect(w, r, returnTo(r, "/handoffs"), http.StatusSeeOther)
}

// handleDepartAccount marks the Account in the path as departed, making the Goals
// they still own Ownerless, and comes back to the form's return, or else
// /goals. Only an Admin may.
func (s *Server) handleDepartAccount(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.svc.MarkDeparted(r.Context(), current.ID, id); err != nil {
		s.refuse(w, r, "Can't mark departed", "/home", err)
		return
	}
	http.Redirect(w, r, returnTo(r, "/goals"), http.StatusSeeOther)
}

// handleReturnAccount reverses the departure of the Account in the path, so the
// Goals they still own stop being Ownerless and they can sign in again, and
// comes back to the form's return, or else /goals. Only an Admin may.
func (s *Server) handleReturnAccount(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.svc.MarkReturned(r.Context(), current.ID, id); err != nil {
		s.refuse(w, r, "Can't mark returned", "/home", err)
		return
	}
	http.Redirect(w, r, returnTo(r, "/goals"), http.StatusSeeOther)
}

// handleReassignGoal reassigns an Ownerless Goal to the Account named by email.
// Only an Admin may.
func (s *Server) handleReassignGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.ReassignGoalByEmail(r.Context(), current.ID, goalID, r.FormValue("email")); err != nil {
		s.writeRefusedHandoff(w, r, goalID, current, formReassign, "Can't reassign Goal", err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

// writeRefusedHandoff answers a refused Hand off or Reassign from the Goal
// page: a refused new Owner redirects to the Goal page, where that form is
// open with the reason beside it and what was sent in it, or, where the page
// doesn't offer the form, is the refusal page headed refused
// (refuseGoalForm); anything else is refuse's page, headed refused and
// linking Back to the Goal.
func (s *Server) writeRefusedHandoff(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, form goalForm, refused string, err error) {
	if errors.Is(err, domain.ErrValidation) {
		s.refuseGoalForm(w, r, goalID, current, form, refused, err)
		return
	}
	s.refuse(w, r, refused, goalPath(goalID), err)
}

func (s *Server) handoffIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}
