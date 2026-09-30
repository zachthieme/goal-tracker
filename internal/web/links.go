package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleRequestLink requests that the Goal in the path contribute to the Goal
// named by parent_id, with an optional note. The requester must own the child.
func (s *Server) handleRequestLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	childID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	parentID, err := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	if err != nil {
		http.Error(w, "a parent Goal is required", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.RequestLink(r.Context(), domain.RequestLinkInput{
		ChildID:     childID,
		ParentID:    parentID,
		Note:        r.FormValue("note"),
		RequesterID: current.ID,
	})
	if err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(childID, 10), http.StatusSeeOther)
}

// handlePendingLinks shows the current Account the link requests awaiting their
// decision as the Owner of each request's parent Goal.
func (s *Server) handlePendingLinks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pending, err := s.svc.PendingLinkRequests(r.Context(), current.ID)
	if err != nil {
		http.Error(w, "could not list pending requests", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, pendingLinksPage(&current, pending))
}

// handleAcceptLink accepts a pending request; only the parent's Owner may.
func (s *Server) handleAcceptLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AcceptLink(r.Context(), linkID, current.ID); err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, "/links", http.StatusSeeOther)
}

// handleRejectLink rejects a pending request; only the parent's Owner may.
func (s *Server) handleRejectLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RejectLink(r.Context(), linkID, current.ID); err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, "/links", http.StatusSeeOther)
}

// handleRemoveLink removes an accepted link; either linked Goal's Owner may.
func (s *Server) handleRemoveLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RemoveLink(r.Context(), linkID, current.ID); err != nil {
		writeLinkError(w, err)
		return
	}
	// Return to wherever the remove was triggered, usually a Goal page.
	if ref := r.Header.Get("Referer"); ref != "" {
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

func linkIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// writeLinkError maps a domain link error to an HTTP status.
func writeLinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrCycle):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "link action failed", http.StatusInternalServerError)
	}
}
