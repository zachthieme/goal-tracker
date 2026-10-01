package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/a-h/templ"

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
	// PendingLinks are the link requests waiting on the person as the parent's
	// Owner, and PendingHandoffs the Handoffs waiting on them as the proposed
	// new Owner.
	PendingLinks    []domain.Link
	PendingHandoffs []domain.Handoff
	// Green, Yellow, and Red count the Active Goals the person Owns by Health,
	// and AtRisk lists the Red ones, then the Yellow.
	Green, Yellow, Red int
	AtRisk             []domain.PersonalGoal
	// Delegated are the Goals the person is a Delegate on.
	Delegated []domain.Goal
}

// NeedsYou counts what's waiting on the person: the Goals to check in on, and
// the link requests and Handoffs to decide.
func (v homeView) NeedsYou() int {
	return len(v.Due) + len(v.PendingLinks) + len(v.PendingHandoffs)
}

// loadHome reads accountID's Home page.
func (s *Server) loadHome(ctx context.Context, accountID int64) (homeView, error) {
	goals, err := s.svc.ActiveGoalsFor(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	var v homeView
	if v.PendingLinks, err = s.svc.PendingLinkRequests(ctx, accountID); err != nil {
		return homeView{}, err
	}
	if v.PendingHandoffs, err = s.svc.PendingHandoffs(ctx, accountID); err != nil {
		return homeView{}, err
	}
	if v.Delegated, err = s.svc.DelegatedGoals(ctx, accountID); err != nil {
		return homeView{}, err
	}
	var yellow []domain.PersonalGoal
	for _, g := range goals {
		if g.Freshness.DueBeforeNextReminder() {
			v.Due = append(v.Due, g)
		}
		if g.AsDelegate {
			continue
		}
		switch g.Health {
		case domain.HealthGreen:
			v.Green++
		case domain.HealthYellow:
			v.Yellow++
			yellow = append(yellow, g)
		case domain.HealthRed:
			v.Red++
			v.AtRisk = append(v.AtRisk, g)
		}
	}
	v.AtRisk = append(v.AtRisk, yellow...)
	return v, nil
}

// dueReason says why a Goal of the person's own is listed to check in on; the
// page says who delegated one to them.
func dueReason(g domain.PersonalGoal) string {
	f := g.Freshness
	if !g.CheckedIn {
		return fmt.Sprintf("No check-in since activation %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
	}
	return fmt.Sprintf("Last check-in %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
}

// thingsNeedYou reads the count of what's waiting on the person.
func thingsNeedYou(n int) string {
	if n == 1 {
		return "1 thing needs you"
	}
	return fmt.Sprintf("%d things need you", n)
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

// confirmScript asks the reader to confirm msg before a form is sent; msg is a
// constant with no quotes of its own.
func confirmScript(msg string) templ.ComponentScript {
	return templ.ComponentScript{Call: "return confirm('" + msg + "')"}
}
