package web

import (
	"net/http"
	"net/url"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleNudge nudges the path's Goal's Owner and Delegates to check in on it
// (CONTEXT.md: Nudge) and returns to the Follow-ups page it was sent from, keeping
// that page's filters (return). No toast confirms it: a Nudge can't be undone,
// and the row's Fix now says the Goal was nudged today. A refusal is said on a
// page of its own, linking back there too. The Nudge's email is queued, so a
// mail server that is down doesn't fail it.
func (s *Server) handleNudge(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	back := followUpsReturn(returnTo(r, "/follow-ups"))
	if _, err := s.svc.Nudge(r.Context(), current.ID, goalID); err != nil {
		s.answerRefused(w, r, refused{Err: err, Heading: "Can't nudge", Back: back,
			Options: []refusalOption{refuseFailureSaying("The Nudge couldn't be sent, so nothing changed. Try again.")}})
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// followUpsReturn is where a Nudge returns: the Follow-ups page at back, a
// page returnTo accepted, when back is that page, its filters and all, and the
// plain Follow-ups page otherwise.
func followUpsReturn(back string) string {
	u, err := url.Parse(back)
	if err != nil || u.Path != "/follow-ups" || u.RawQuery == "" {
		return "/follow-ups"
	}
	return "/follow-ups?" + u.RawQuery
}
