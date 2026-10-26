package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// errLinkWouldCycle is a link refused for making a cycle, as a form beside it
// says it: ErrCycle's own text names the two Goals by ID.
var errLinkWouldCycle = errors.New("that Goal already contributes to this one, so linking them would make a cycle")

// handleRequestLink requests that the Goal in the path contribute to the Goal
// named by parent_id, with an optional note. The requester must own the child.
// A refused request, a cycle included, comes back as the Goal page with the
// form open and the reason beside it.
func (s *Server) handleRequestLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	childID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
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
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusConflict, errLinkWouldCycle)
	case errors.Is(err, domain.ErrValidation):
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusUnprocessableEntity, err)
	case err != nil:
		s.writeLinkError(w, r, current, "Can't request link", goalPath(childID), err)
	default:
		http.Redirect(w, r, goalPath(childID), http.StatusSeeOther)
	}
}

// handlePendingLinks shows the current Account the link requests awaiting their
// decision as the Owner of each request's parent Goal.
func (s *Server) handlePendingLinks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pending, err := s.svc.PendingLinkRequests(r.Context(), current.ID)
	if err != nil {
		s.serverError(w, r, "could not list pending requests", err)
		return
	}
	toast := s.linkRejectionToast(r.Context(), takeUndo(w, r), current, "")
	render(w, r, http.StatusOK, pendingLinksPage(&current, pending, toast))
}

// handleAcceptLink accepts a pending request; only the parent's Owner may. It
// returns to the page the Accept came from (from: Home, or the pending page),
// and a refusal links back there.
func (s *Server) handleAcceptLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := requestPage(r.FormValue("from"), "/links")
	if _, err := s.svc.AcceptLink(r.Context(), linkID, current.ID); err != nil {
		s.writeLinkError(w, r, current, "Can't accept link", back, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleRejectLink rejects a pending request; only the parent's Owner may. It
// returns to the page the reject came from (from: Home, or the pending page),
// which offers Undo once in a toast. A refusal links back there.
func (s *Server) handleRejectLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := requestPage(r.FormValue("from"), "/links")
	rejection, err := s.svc.RejectLink(r.Context(), linkID, current.ID)
	if err != nil {
		s.writeLinkError(w, r, current, "Can't reject link", back, err)
		return
	}
	offerUndo(w, back, undoLinkRejection, rejection.ID, rejection.UndoToken)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleUndoLinkRejection puts the rejected request in the path back as
// pending; only the person who rejected it may, with the toast's token, and
// only once. It returns to the page the Undo came from (from), or refuses
// with a page saying why, linking back there, and changes nothing.
func (s *Server) handleUndoLinkRejection(w http.ResponseWriter, r *http.Request, current domain.Account) {
	back := requestPage(r.FormValue("from"), "/links")
	rejectionID, ok := s.undoIDFromPath(w, r, current, back)
	if !ok {
		return
	}
	if _, err := s.svc.RestoreLinkRequest(r.Context(), rejectionID, current.ID, r.FormValue(undoField)); err != nil {
		s.refuseUndo(w, r, current, back, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
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
// link), which offers Undo once in a toast. Anyone else is refused with a page
// saying why, linking back to that Goal page (Home without one).
func (s *Server) handleRemoveLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	removal, err := s.svc.RemoveLink(r.Context(), linkID, current.ID)
	if err != nil {
		back := "/home"
		if goalID, err := strconv.ParseInt(r.FormValue("goal_id"), 10, 64); err == nil {
			back = goalPath(goalID)
		}
		s.writeLinkError(w, r, current, "Can't remove link", back, err)
		return
	}
	back := linkPage(removal.Child.ID, removal.Parent.ID, r.FormValue("goal_id"))
	offerUndo(w, back, undoLinkRemoval, removal.ID, removal.UndoToken)
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleUndoLinkRemoval restores the removed link in the path as accepted; only
// the person who removed it may, with the toast's token, and only once. It
// returns to the Goal page the Undo came from (goal_id), or refuses with a
// page saying why, linking back to that Goal page (Home without one), and
// changes nothing.
func (s *Server) handleUndoLinkRemoval(w http.ResponseWriter, r *http.Request, current domain.Account) {
	back := "/home"
	if goalID, err := strconv.ParseInt(r.FormValue("goal_id"), 10, 64); err == nil {
		back = "/goals/" + strconv.FormatInt(goalID, 10)
	}
	removalID, ok := s.undoIDFromPath(w, r, current, back)
	if !ok {
		return
	}
	link, err := s.svc.RestoreLink(r.Context(), removalID, current.ID, r.FormValue(undoField))
	if err != nil {
		s.refuseUndo(w, r, current, back, err)
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

func (s *Server) linkIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// writeLinkError answers a refused link action with a page inside the app,
// headed by what was refused (e.g. "Can't accept link"), saying why and
// linking back: a cycle 409, a validation 422, someone else's action 403. A
// link or suggestion that isn't there is the Not found page.
func (s *Server) writeLinkError(w http.ResponseWriter, r *http.Request, current domain.Account, refused, back string, err error) {
	var status int
	var reason string
	switch {
	case errors.Is(err, domain.ErrCycle):
		// ErrCycle's own text names the two Goals by ID, so a person reads
		// this sentence instead.
		status, reason = http.StatusConflict, "Linking these Goals would make a cycle: the parent already contributes to the child."
	case errors.Is(err, domain.ErrValidation):
		status, reason = http.StatusUnprocessableEntity, sentence(plainReason(err))
	case errors.Is(err, domain.ErrNotAuthorized):
		status, reason = http.StatusForbidden, sentence(plainReason(err))
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
		return
	default:
		s.serverError(w, r, "link action failed", err)
		return
	}
	render(w, r, status, linkRefusedPage(&current, refused, reason, back))
}
