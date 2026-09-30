package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleAddDelegate authorizes the account named by email as a Delegate on the
// Goal in the path. Only the Goal's Owner may do so, so a non-Owner's attempt
// comes back 403 (CONTEXT.md: a person an Owner authorizes).
func (s *Server) handleAddDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.AddDelegateByEmail(r.Context(), current.ID, id, r.FormValue("email"))
	writeDelegateResult(w, r, id, err)
}

// handleRemoveDelegate revokes the Delegate named by email on the Goal in the
// path. Only the Goal's Owner may do so.
func (s *Server) handleRemoveDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.RemoveDelegateByEmail(r.Context(), current.ID, id, r.FormValue("email"))
	writeDelegateResult(w, r, id, err)
}

// handleDelegatePage lists every Goal the current Account is a Delegate for and
// lets them check in on each without sharing the Owner's login (CONTEXT.md:
// Delegate). Each Goal carries a Check-in form prefilled from its latest
// Check-in and its Rolled-up Health.
func (s *Server) handleDelegatePage(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goals, err := s.svc.DelegatedGoals(r.Context(), current.ID)
	if err != nil {
		http.Error(w, "could not load delegated goals", http.StatusInternalServerError)
		return
	}
	items := make([]delegatedGoal, 0, len(goals))
	for _, g := range goals {
		latest, ok, err := s.svc.LatestCheckin(r.Context(), g.ID)
		if err != nil {
			http.Error(w, "could not load latest check-in", http.StatusInternalServerError)
			return
		}
		var latestPtr *domain.Checkin
		if ok {
			latestPtr = &latest
		}
		rollup, err := s.svc.RolledUpHealth(r.Context(), g.ID)
		if err != nil {
			http.Error(w, "could not compute rolled-up health", http.StatusInternalServerError)
			return
		}
		metrics, err := s.svc.ListMetrics(r.Context(), g.ID)
		if err != nil {
			http.Error(w, "could not load metrics", http.StatusInternalServerError)
			return
		}
		items = append(items, delegatedGoal{
			Goal:   g,
			Latest: latestPtr,
			Form:   checkinFormFromLatest(g.ID, latestPtr, rollup, metrics),
		})
	}
	render(w, r, http.StatusOK, delegatePage(&current, items))
}

// delegatedGoal is one row of the Delegate's page: a Goal they may check in on,
// its latest Check-in (nil when none yet), and the prefilled Check-in form.
type delegatedGoal struct {
	Goal   domain.Goal
	Latest *domain.Checkin
	Form   checkinFormData
}

// writeDelegateResult redirects back to the Goal on success and maps a domain
// Delegate error to a status: a non-Owner attempt is 403, a bad input 422.
func writeDelegateResult(w http.ResponseWriter, r *http.Request, goalID int64, err error) {
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotAuthorized):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, domain.ErrValidation):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		default:
			http.Error(w, "could not update delegates", http.StatusInternalServerError)
		}
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}
