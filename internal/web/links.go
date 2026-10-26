package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// errLinkWouldCycle is a link refused for making a cycle, as a form beside it
// says it: ErrCycle's own text names the two Goals by ID. It is an ErrCycle,
// so where no form is offered beside it, the refusal page answers it as one.
var errLinkWouldCycle error = linkWouldCycleError{}

// linkWouldCycleError is errLinkWouldCycle's type: its text is the form's
// sentence, and errors.Is matches it to domain.ErrCycle.
type linkWouldCycleError struct{}

func (linkWouldCycleError) Error() string {
	return "that Goal already contributes to this one, so linking them would make a cycle"
}

func (linkWouldCycleError) Unwrap() error { return domain.ErrCycle }

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
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusUnprocessableEntity, domain.Refuse(domain.ErrValidation, "a parent Goal is required"))
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
	render(w, r, http.StatusOK, pendingLinksPage(&current, pending, s.undoToast(w, r, current)))
}

// handleAcceptLink accepts a pending request; only the parent's Owner may. It
// returns to the page the Accept came from (its return: Home, or else the
// pending page), and a refusal links back there.
func (s *Server) handleAcceptLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := returnTo(r, "/links")
	if _, err := s.svc.AcceptLink(r.Context(), linkID, current.ID); err != nil {
		s.writeLinkError(w, r, current, "Can't accept link", back, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleRejectLink rejects a pending request; only the parent's Owner may. It
// returns to the page the reject came from (its return: Home, or else the
// pending page), which offers Undo once in a toast. A refusal links back
// there.
func (s *Server) handleRejectLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := returnTo(r, "/links")
	rejection, err := s.svc.RejectLink(r.Context(), linkID, current.ID)
	if err != nil {
		s.writeLinkError(w, r, current, "Can't reject link", back, err)
		return
	}
	s.offerUndo(w, r, undoLinkRejection, rejection.ID, rejection.UndoToken, back)
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
	s.offerUndo(w, r, undoLinkRemoval, removal.ID, removal.UndoToken, back)
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

func (s *Server) linkIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// writeLinkError answers a refused link action with refuse's page, headed by
// what was refused (e.g. "Can't accept link") and linking back. A link or
// suggestion that isn't there is the Not found page. refuse resolves the
// signed-in Account itself, so the one passed in goes unused.
func (s *Server) writeLinkError(w http.ResponseWriter, r *http.Request, _ domain.Account, refused, back string, err error) {
	s.refuse(w, r, refused, back, err)
}
