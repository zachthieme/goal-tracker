package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleRequestLink requests that the Goal in the path contribute to the Goal
// named by parent_id, with an optional note. The requester must own the child.
// A refused request, a cycle included, comes back as the Goal page with the
// form open and the reason beside it.
func (s *Server) handleRequestLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	childID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	parentID, err := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	if err != nil {
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusUnprocessableEntity, errors.New("a parent Goal is required"))
		return
	}
	_, err = s.svc.RequestLink(r.Context(), domain.RequestLinkInput{
		ChildID:     childID,
		ParentID:    parentID,
		Note:        r.FormValue("note"),
		RequesterID: current.ID,
	})
	switch {
	case errors.Is(err, domain.ErrCycle):
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusConflict, err)
	case errors.Is(err, domain.ErrValidation):
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusUnprocessableEntity, err)
	case err != nil:
		writeLinkError(w, err)
	default:
		http.Redirect(w, r, "/goals/"+strconv.FormatInt(childID, 10), http.StatusSeeOther)
	}
}

// handlePendingLinks shows the current Account the link requests awaiting their
// decision as the Owner of each request's parent Goal.
func (s *Server) handlePendingLinks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pending, err := s.svc.PendingLinkRequests(r.Context(), current.ID)
	if err != nil {
		http.Error(w, "could not list pending requests", http.StatusInternalServerError)
		return
	}
	toast := s.linkRejectionToast(r.Context(), takeUndo(w, r), current, "")
	render(w, r, http.StatusOK, pendingLinksPage(&current, pending, toast))
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

// handleRejectLink rejects a pending request; only the parent's Owner may. It
// returns to the page the reject came from (from: Home, or the pending page),
// which offers Undo once in a toast.
func (s *Server) handleRejectLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	rejection, err := s.svc.RejectLink(r.Context(), linkID, current.ID)
	if err != nil {
		writeLinkError(w, err)
		return
	}
	back := requestPage(r.FormValue("from"), "/links")
	offerUndo(w, back, undoLinkRejection, rejection.ID, rejection.UndoToken)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleUndoLinkRejection puts the rejected request in the path back as
// pending; only the person who rejected it may, with the toast's token, and
// only once. It returns to
// the page the Undo came from (from), or refuses with a message and changes
// nothing.
func (s *Server) handleUndoLinkRejection(w http.ResponseWriter, r *http.Request, current domain.Account) {
	rejectionID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.RestoreLinkRequest(r.Context(), rejectionID, current.ID, r.FormValue(undoField)); err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, requestPage(r.FormValue("from"), "/links"), http.StatusSeeOther)
}

// linkRejectionToast is the toast a page carries straight after current
// rejected a link request (offer), with the Undo that puts it back — or nil
// when the offer isn't for a rejection current can still undo. from names the
// page for the Undo to return to, as the reject's own from did.
func (s *Server) linkRejectionToast(ctx context.Context, offer undoOffer, current domain.Account, from string) *toast {
	id, ok := offer.of(undoLinkRejection)
	if !ok {
		return nil
	}
	rejection, err := s.svc.LinkRejection(ctx, id)
	if err != nil || rejection.Restored || rejection.RejectedBy != current.ID {
		return nil
	}
	return &toast{
		Message: fmt.Sprintf("Request rejected: %s won't contribute to %s.", rejection.Child.Title, rejection.Parent.Title),
		Undo:    fmt.Sprintf("/link-rejections/%d/undo", rejection.ID),
		Fields:  offer.fields(fromField(from)...),
	}
}

// handleRemoveLink removes an accepted link; either linked Goal's Owner may.
// It returns to the Goal page the remove came from (goal_id, either end of the
// link), which offers Undo once in a toast.
func (s *Server) handleRemoveLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	removal, err := s.svc.RemoveLink(r.Context(), linkID, current.ID)
	if err != nil {
		writeLinkError(w, err)
		return
	}
	back := linkPage(removal.Child.ID, removal.Parent.ID, r.FormValue("goal_id"))
	offerUndo(w, back, undoLinkRemoval, removal.ID, removal.UndoToken)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleUndoLinkRemoval restores the removed link in the path as accepted; only
// the person who removed it may, with the toast's token, and only once. It
// returns to the Goal page
// the Undo came from (goal_id), or refuses with a message and changes nothing.
func (s *Server) handleUndoLinkRemoval(w http.ResponseWriter, r *http.Request, current domain.Account) {
	removalID, ok := linkIDFromPath(w, r)
	if !ok {
		return
	}
	link, err := s.svc.RestoreLink(r.Context(), removalID, current.ID, r.FormValue(undoField))
	if err != nil {
		writeLinkError(w, err)
		return
	}
	http.Redirect(w, r, linkPage(link.Child.ID, link.Parent.ID, r.FormValue("goal_id")), http.StatusSeeOther)
}

// linkPage is the Goal page to return to after acting on the link from childID
// to parentID: the page named by from when it is either end, the child's
// otherwise.
func linkPage(childID, parentID int64, from string) string {
	id, err := strconv.ParseInt(from, 10, 64)
	if err != nil || (id != childID && id != parentID) {
		id = childID
	}
	return "/goals/" + strconv.FormatInt(id, 10)
}

// linkRemovalToast is the toast the Goal page goalID carries straight after
// current removed one of its links (offer), with the Undo that restores it —
// or nil when the offer isn't for a removal of this Goal's link that current
// can still undo.
func (s *Server) linkRemovalToast(ctx context.Context, offer undoOffer, current domain.Account, goalID int64) *toast {
	id, ok := offer.of(undoLinkRemoval)
	if !ok {
		return nil
	}
	removal, err := s.svc.LinkRemoval(ctx, id)
	if err != nil || removal.Restored || removal.RemovedBy != current.ID ||
		(removal.Child.ID != goalID && removal.Parent.ID != goalID) {
		return nil
	}
	return &toast{
		Message: fmt.Sprintf("Link removed: %s no longer contributes to %s.", removal.Child.Title, removal.Parent.Title),
		Undo:    fmt.Sprintf("/link-removals/%d/undo", removal.ID),
		Fields:  offer.fields(toastField{Name: "goal_id", Value: strconv.FormatInt(goalID, 10)}),
	}
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
