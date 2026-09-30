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
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.StartHandoffByEmail(r.Context(), goalID, r.FormValue("to_email"), current.ID); err != nil {
		writeHandoffError(w, err)
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
	render(w, r, http.StatusOK, pendingHandoffsPage(&current, pending))
}

// handleAcceptHandoff accepts a pending Handoff; only the new Owner may.
func (s *Server) handleAcceptHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := handoffIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AcceptHandoff(r.Context(), id, current.ID); err != nil {
		writeHandoffError(w, err)
		return
	}
	http.Redirect(w, r, "/handoffs", http.StatusSeeOther)
}

// handleRejectHandoff rejects a pending Handoff; only the new Owner may.
func (s *Server) handleRejectHandoff(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := handoffIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RejectHandoff(r.Context(), id, current.ID); err != nil {
		writeHandoffError(w, err)
		return
	}
	http.Redirect(w, r, "/handoffs", http.StatusSeeOther)
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
	// Return to wherever the action was triggered, usually a Goal page.
	if ref := r.Header.Get("Referer"); ref != "" {
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

// handleReassignGoal reassigns an Ownerless Goal to the Account named by email.
// Only an Admin may.
func (s *Server) handleReassignGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.ReassignGoalByEmail(r.Context(), current.ID, goalID, r.FormValue("email")); err != nil {
		writeHandoffError(w, err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
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
