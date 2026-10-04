package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

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
	offer := takeUndo(w, r)
	v.Toast = cmp.Or(
		s.linkRejectionToast(r.Context(), offer, current, fromHome),
		s.handoffRejectionToast(r.Context(), offer, current, fromHome),
		s.suggestionDeclineToast(r.Context(), offer, current, 0, fromHome),
	)
	render(w, r, http.StatusOK, homePage(&current, v))
}

// homeView is what the Home page shows.
type homeView struct {
	// Due are the Active Goals the person Owns or is a Delegate on whose
	// Check-in is due before next week's reminder, the same rule the reminder
	// email uses, most overdue first.
	Due []domain.PersonalGoal
	// Requests are the link requests waiting on the person as the parent's
	// Owner, the Handoffs waiting on them as the proposed new Owner, and the
	// Parent suggestions on the Goals they Own, oldest first.
	Requests []homeRequest
	// Green, Yellow, and Red count the Active Goals the person Owns by Health,
	// and AtRisk lists the Red ones, then the Yellow.
	Green, Yellow, Red int
	AtRisk             []domain.PersonalGoal
	// Delegated counts the Goals the person is a Delegate on.
	Delegated int
	// Toast, when set, offers Undo for the request just rejected, or the
	// suggestion just declined, from Home.
	Toast *toast
}

// fromHome is the from a request's Reject and its Undo post when they come
// from Home, so each returns there rather than to the request's pending page.
const fromHome = "home"

// requestPage is the page to return to after deciding a request: Home when
// from says the decision came from there, the request's pending page
// otherwise.
func requestPage(from, pending string) string {
	if from == fromHome {
		return "/home"
	}
	return pending
}

// fromField is the hidden input that has an Undo return to the page from
// names, or none for the request's pending page.
func fromField(from string) []toastField {
	if from == "" {
		return nil
	}
	return []toastField{{Name: "from", Value: from}}
}

// NeedsYou counts what's waiting on the person: the Goals to check in on, and
// the link requests, Handoffs and Parent suggestions to decide.
func (v homeView) NeedsYou() int {
	return len(v.Due) + len(v.Requests)
}

// homeRequest is one request waiting on the person's decision: a link
// request, a Handoff or a Parent suggestion, whichever is set.
type homeRequest struct {
	Link       *domain.Link
	Handoff    *domain.Handoff
	Suggestion *domain.ParentSuggestion
}

// madeAt is when the request was made.
func (r homeRequest) madeAt() time.Time {
	switch {
	case r.Link != nil:
		return r.Link.CreatedAt
	case r.Suggestion != nil:
		return r.Suggestion.CreatedAt
	}
	return r.Handoff.CreatedAt
}

// loadHome reads accountID's Home page.
func (s *Server) loadHome(ctx context.Context, accountID int64) (homeView, error) {
	goals, err := s.svc.ActiveGoalsFor(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	var v homeView
	links, err := s.svc.PendingLinkRequests(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	handoffs, err := s.svc.PendingHandoffs(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	suggestions, err := s.svc.OpenParentSuggestionsFor(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	for i := range links {
		v.Requests = append(v.Requests, homeRequest{Link: &links[i]})
	}
	for i := range handoffs {
		v.Requests = append(v.Requests, homeRequest{Handoff: &handoffs[i]})
	}
	for i := range suggestions {
		v.Requests = append(v.Requests, homeRequest{Suggestion: &suggestions[i]})
	}
	slices.SortStableFunc(v.Requests, func(a, b homeRequest) int {
		return a.madeAt().Compare(b.madeAt())
	})
	delegated, err := s.svc.DelegatedGoals(ctx, accountID)
	if err != nil {
		return homeView{}, err
	}
	v.Delegated = len(delegated)
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
	slices.SortStableFunc(v.Due, func(a, b domain.PersonalGoal) int {
		return cmp.Compare(overdueDays(b.Freshness), overdueDays(a.Freshness))
	})
	return v, nil
}

// overdueDays is how many days past its cadence a Goal's Check-in is, negative
// while it still has days to spare.
func overdueDays(f domain.Freshness) int {
	return f.DaysSince - f.CadenceDays
}

// dueReason says why a Goal is listed to check in on.
func dueReason(g domain.PersonalGoal) string {
	f := g.Freshness
	if !g.CheckedIn {
		return fmt.Sprintf("No check-in since activation %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
	}
	return fmt.Sprintf("Last check-in %s on a %d-day cadence", daysAgo(f.DaysSince), f.CadenceDays)
}

// healthBarLabel reads the Health distribution bar's counts.
func healthBarLabel(v homeView) string {
	return fmt.Sprintf("%d Green, %d Yellow, %d Red", v.Green, v.Yellow, v.Red)
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
