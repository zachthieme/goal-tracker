package web

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleNudge nudges the path's Goal's Owner and Delegates to check in on it
// (CONTEXT.md: Nudge) and returns to the Follow-ups page it was sent from, keeping
// that page's filters (return). No toast confirms it: a Nudge can't be undone,
// and the row's Fix now says the Goal was nudged today. A refusal, or a Nudge
// recorded whose email couldn't go out, is said on a page of its own.
func (s *Server) handleNudge(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.Nudge(r.Context(), current.ID, goalID)
	switch {
	case err == nil:
		http.Redirect(w, r, followUpsReturn(r.FormValue("return")), http.StatusSeeOther)
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, domain.ErrValidation):
		render(w, r, http.StatusUnprocessableEntity, nudgeRefusedPage(&current, "Can't nudge", sentence(plainReason(err))))
	case errors.Is(err, domain.ErrNotAuthorized):
		render(w, r, http.StatusForbidden, nudgeRefusedPage(&current, "Can't nudge", sentence(plainReason(err))))
	case errors.Is(err, domain.ErrNudgeNotSent):
		// The Nudge stands; only the mail server failed it.
		render(w, r, http.StatusBadGateway, nudgeRefusedPage(&current, "Nudge not emailed", sentence(domain.Reason(err))))
	default:
		s.logServerError(r, err)
		render(w, r, http.StatusInternalServerError, nudgeRefusedPage(&current, "Can't nudge", "The Nudge couldn't be sent, so nothing changed. Try again."))
	}
}

// followUpsReturn is where a Nudge returns: the Follow-ups page at back when back is
// that page, its filters and all, and the plain Follow-ups page otherwise.
func followUpsReturn(back string) string {
	u, err := url.Parse(back)
	if err != nil || !localPath(back) || u.Path != "/follow-ups" || u.RawQuery == "" {
		return "/follow-ups"
	}
	return "/follow-ups?" + u.RawQuery
}
