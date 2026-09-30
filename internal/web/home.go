package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleHome is the signed-in person's week: the Goals they should check in
// on, and what else is waiting on them.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, err := s.loadHome(r.Context(), current.ID)
	if err != nil {
		http.Error(w, "could not load your week", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, homePage(&current, v))
}

// homeView is what the Home page shows.
type homeView struct {
	// Due are the Active Goals the person Owns or is a Delegate on whose
	// Check-in is due before next week's reminder, the same rule the reminder
	// email uses.
	Due []domain.PersonalGoal
	// Delegated are the Goals the person is a Delegate on.
	Delegated []domain.Goal
}

// loadHome reads accountID's Home page.
func (s *Server) loadHome(ctx context.Context, accountID int64) (homeView, error) {
	goals, err := s.svc.ActiveGoalsFor(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	var v homeView
	if v.Delegated, err = s.svc.DelegatedGoals(ctx, accountID); err != nil {
		return homeView{}, err
	}
	for _, g := range goals {
		if g.Freshness.DueBeforeNextReminder() {
			v.Due = append(v.Due, g)
		}
	}
	return v, nil
}

// dueReason says why a Goal is listed to check in on.
func dueReason(g domain.PersonalGoal) string {
	if g.AsDelegate {
		return "Delegated to you by " + g.Goal.Owner.Email
	}
	f := g.Freshness
	if !g.CheckedIn {
		return fmt.Sprintf("No check-in since activation %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
	}
	return fmt.Sprintf("Last check-in %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
}

// daysAgo reads a count of days in the past.
func daysAgo(days int) string {
	switch days {
	case 0:
		return "today"
	case 1:
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}
