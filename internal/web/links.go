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

// errNoParentLinked refuses a link request that picks no parent, which the
// Goal page's form says beside its picker.
var errNoParentLinked = domain.Refuse(domain.ErrValidation, "a parent Goal is required")

// handleRequestLink requests that the Goal in the path contribute to the Goal
// named by parent_id, with an optional note. The requester must own the child.
// A refused request, a cycle included, comes back as the Goal page with the
// form open and the reason beside it, one picking no parent, or none that
// parses, beside its picker.
func (s *Server) handleRequestLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	childID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	parentID, err := strconv.ParseInt(r.FormValue("parent_id"), 10, 64)
	if err != nil {
		s.renderRefusedForm(w, r, childID, current, formParentLink, http.StatusUnprocessableEntity, errNoParentLinked)
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
		s.refuse(w, r, "Can't request link", goalPath(childID), err)
	default:
		http.Redirect(w, r, goalPath(childID), http.StatusSeeOther)
	}
}

// handleAcceptLink accepts a pending request; only the parent's Owner may. It
// returns to the page the Accept came from (its return, or else Home), and a
// refusal links back there.
func (s *Server) handleAcceptLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := returnTo(r, "/home")
	if _, err := s.svc.AcceptLink(r.Context(), linkID, current.ID); err != nil {
		s.refuse(w, r, "Can't accept link", back, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleRejectLink rejects a pending request; only the parent's Owner may. It
// returns to the page the reject came from (its return, or else Home), which
// offers Undo once in a toast. A refusal links back there.
func (s *Server) handleRejectLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	linkID, ok := s.linkIDFromPath(w, r)
	if !ok {
		return
	}
	back := returnTo(r, "/home")
	rejection, err := s.svc.RejectLink(r.Context(), linkID, current.ID)
	if err != nil {
		s.refuse(w, r, "Can't reject link", back, err)
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
		s.refuse(w, r, "Can't remove link", back, err)
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
