package web

import (
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleAddDelegate authorizes the account named by email as a Delegate on the
// Goal in the path. Only the Goal's Owner may do so, so a non-Owner's attempt
// comes back 403 on the refusal page (CONTEXT.md: a person an Owner
// authorizes).
func (s *Server) handleAddDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.AddDelegateByEmail(r.Context(), current.ID, id, r.FormValue("email"))
	s.answerDelegateCommand(w, r, id, current, "Can't add Delegate", err)
}

// handleRemoveDelegate revokes the Delegate named by email on the Goal in the
// path. Only the Goal's Owner may do so.
func (s *Server) handleRemoveDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.RemoveDelegateByEmail(r.Context(), current.ID, id, r.FormValue("email"))
	s.answerDelegateCommand(w, r, id, current, "Can't remove Delegate", err)
}

// handleDelegatePage lists every Goal the current Account is a Delegate for,
// each with its current Health, its latest Check-in and how long ago that
// was, and links each to the Check-in page (CONTEXT.md: Delegate).
func (s *Server) handleDelegatePage(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goals, err := s.svc.DelegatedGoals(r.Context(), current.ID)
	if err != nil {
		s.serverError(w, r, "could not load delegated goals", err)
		return
	}
	listings, err := s.svc.GoalListings(r.Context(), goals)
	if err != nil {
		s.serverError(w, r, "could not load delegated goals' check-ins", err)
		return
	}
	items := make([]delegatedGoal, 0, len(goals))
	for _, g := range goals {
		items = append(items, delegatedGoal{Goal: g, GoalListing: listings[g.ID]})
	}
	render(w, r, http.StatusOK, delegatePage(&current, items))
}

// delegatedGoal is one card on the Delegate's page: a Goal they may check in
// on and its listing — its current Health, its latest Check-in (nil when none
// yet) and its freshness, which counts the days since that Check-in.
type delegatedGoal struct {
	Goal domain.Goal
	domain.GoalListing
}

// answerDelegateCommand answers an add or remove of a Delegate on the Goal
// goalID: done, it returns to the Goal page. Refused, a bad input redirects
// to the Goal page, where the Delegates form is open with the reason above it
// and what was sent in it (goalPageForm); anything else is the refusal page,
// headed heading and linking Back to the Goal (answerRefused).
func (s *Server) answerDelegateCommand(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, heading string, err error) {
	if err != nil {
		s.answerRefused(w, r, refused{Err: err, Heading: heading, Back: goalPath(goalID), Form: s.goalPageForm(goalID, current, formDelegates)})
		return
	}
	http.Redirect(w, r, goalPath(goalID), http.StatusSeeOther)
}
