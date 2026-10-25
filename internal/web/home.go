package web

import (
	"cmp"
	"context"
	"encoding/json"
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
	if err == nil {
		v.NoChangeRefused, err = s.refusedNoChanges(r.Context(), v.Due)
	}
	if err != nil {
		s.serverError(w, r, "could not load your week", err)
		return
	}
	v.Dates = s.displayDates()
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
	// NoChangeRefused marks, by Goal ID, the due Goals with a Check-in on which
	// a No change would be refused, so their row offers only Check in. Only
	// Home's own handler fills it.
	NoChangeRefused map[int64]bool
	// Asks are the open Asks for help addressed to the person, Red first,
	// then Yellow, then by Goal title (CONTEXT.md: Ask for help).
	Asks []domain.AskForHelp
	// Requests are the link requests waiting on the person as the parent's
	// Owner, the Handoffs waiting on them as the proposed new Owner, and the
	// Parent suggestions on the Goals they Own, oldest first.
	Requests []homeRequest
	// ActionItems are the open Action Items the person owns, from every
	// Report, soonest due first, so an overdue one leads.
	ActionItems []domain.ActionItemListing
	// Green, Yellow, and Red count the Active Goals the person Owns by Health,
	// and AtRisk lists the Red ones, then the Yellow.
	Green, Yellow, Red int
	AtRisk             []domain.PersonalGoal
	// Delegated counts the Goals the person is a Delegate on.
	Delegated int
	// Org is the My org section, nil for someone with no one under them.
	Org *homeOrg
	// Dates is the clock the page title's date is read from. Only Home's own
	// handler fills it.
	Dates displayDates
	// Toast, when set, offers Undo for the request just rejected, or the
	// suggestion just declined, from Home.
	Toast *toast
}

// homeOrg is Home's My org section: what is in trouble in the viewer's Org,
// leaving out the viewer's own Goals, person by person (CONTEXT.md: Org).
type homeOrg struct {
	// ViewerID is whose Org it is, for the links to it on Goals and Follow-ups.
	ViewerID int64
	// Total counts the flagged Goals across everyone under the viewer.
	Total domain.SignalCounts
	// Flagged are the people directly under the viewer whose Org has anything
	// flagged, most urgent first.
	Flagged []domain.PersonSignals
	// Clear counts the people directly under the viewer with nothing flagged.
	Clear int
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

// NeedsYou counts what's waiting on the person: the Goals to check in on, the
// Asks for help to answer, the Action Items to close, and the link requests,
// Handoffs and Parent suggestions to decide. The sidebar counts the same
// things through domain.Sidebar's NeedsYou.
func (v homeView) NeedsYou() int {
	return len(v.Due) + len(v.Asks) + len(v.ActionItems) + len(v.Requests)
}

// homeActionItemPlace reads where an Action Item on Home comes from and when
// it is due: its Report, the Goal it is about, if any, and its due date.
func homeActionItemPlace(l domain.ActionItemListing) string {
	place := l.ReportName
	if l.GoalTitle != "" {
		place += " · " + l.GoalTitle
	}
	return place + " · due " + showDateYear(l.Item.DueDate)
}

// homeActionItemPath is where an Action Item on Home leads: the Action Items
// on the page of the Goal it is about, or on the publication it was raised on
// when it is about the whole Report.
func homeActionItemPath(item domain.ActionItem) string {
	if item.GoalID != 0 {
		return fmt.Sprintf("/goals/%d#action-items", item.GoalID)
	}
	return fmt.Sprintf("/reports/%d/publications/%d#action-items", item.DefinitionID, item.PublicationID)
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
	if v.ActionItems, err = s.svc.OpenActionItemsOwnedBy(ctx, accountID); err != nil {
		return homeView{}, err
	}
	if v.Asks, err = s.svc.AsksForHelpTo(ctx, accountID); err != nil {
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
	if v.Org, err = s.loadHomeOrg(ctx, accountID); err != nil {
		return homeView{}, err
	}
	slices.SortStableFunc(v.Due, func(a, b domain.PersonalGoal) int {
		return cmp.Compare(overdueDays(b.Freshness), overdueDays(a.Freshness))
	})
	return v, nil
}

// refusedNoChanges asks, for the due Goals with a Check-in to repeat, whether
// a No change on each would be refused, and marks those that would by Goal ID.
func (s *Server) refusedNoChanges(ctx context.Context, due []domain.PersonalGoal) (map[int64]bool, error) {
	var goals []domain.Goal
	for _, g := range due {
		if g.CheckedIn {
			goals = append(goals, g.Goal)
		}
	}
	refusals, err := s.svc.NoChangeRefusalsByGoal(ctx, goals)
	if err != nil {
		return nil, fmt.Errorf("check no change on due goals: %w", err)
	}
	refused := make(map[int64]bool, len(refusals))
	for id := range refusals {
		refused[id] = true
	}
	return refused, nil
}

// homeOrgBadge is one count on the My org section: what it counts, for its
// data-kind, how it reads, and its badge's class.
type homeOrgBadge struct {
	Kind, Label, Class string
}

// homeOrgBadges are c's non-zero counts, Red, then Path to Green overdue,
// Check-in overdue and Yellow. Red and Yellow take their Health's colour, and
// the freshness signals the Check-in overdue mark's, as on Follow-ups.
func homeOrgBadges(c domain.SignalCounts) []homeOrgBadge {
	var out []homeOrgBadge
	for _, b := range []struct {
		n                  int
		kind, label, class string
	}{
		{c.Red, "red", domain.HealthRed, healthClass(domain.HealthRed)},
		{c.PathOverdue, "path-overdue", "Path to Green overdue", "st"},
		{c.CheckinOverdue, "checkin-overdue", "Check-in overdue", "st"},
		{c.Yellow, "yellow", domain.HealthYellow, healthClass(domain.HealthYellow)},
	} {
		if b.n > 0 {
			out = append(out, homeOrgBadge{Kind: b.kind, Label: fmt.Sprintf("%d %s", b.n, b.label), Class: b.class})
		}
	}
	return out
}

// loadHomeOrg reads accountID's My org section, or nil when they have no one
// under them.
func (s *Server) loadHomeOrg(ctx context.Context, accountID int64) (*homeOrg, error) {
	if under, err := s.svc.HasAnyoneUnder(ctx, accountID); err != nil || !under {
		return nil, err
	}
	sig, err := s.svc.OrgSignals(ctx, accountID)
	if err != nil {
		return nil, err
	}
	org := &homeOrg{ViewerID: accountID, Total: sig.Total}
	for _, p := range sig.Under {
		if p.Counts.Any() {
			org.Flagged = append(org.Flagged, p)
		} else {
			org.Clear++
		}
	}
	return org, nil
}

// overdueDays is how many days past its cadence a Goal's Check-in is, negative
// while it still has days to spare.
func overdueDays(f domain.Freshness) int {
	return f.DaysSince - f.CadenceDays
}

// homeDueAge says how long since a due Goal's last Check-in, or since its
// activation when it has none.
func homeDueAge(g domain.PersonalGoal) string {
	if !g.CheckedIn {
		return "no check-in since activation " + daysAgo(g.Freshness.DaysSince)
	}
	return "last check-in " + daysAgo(g.Freshness.DaysSince)
}

// homeDueCadence says how often a Check-in is due on a Goal ("every 7 days").
func homeDueCadence(f domain.Freshness) string {
	if f.CadenceDays == 1 {
		return "every day"
	}
	return fmt.Sprintf("every %d days", f.CadenceDays)
}

// healthAsOf dates a Check-in overdue Goal's Health as last reported ("as of 9
// days ago"), or is "" when the Goal isn't Check-in overdue or has no Check-in
// to date it by, and its Health badge reads as current.
func healthAsOf(g domain.PersonalGoal) string {
	if !g.Freshness.CheckinOverdue || !g.CheckedIn || g.Health == "" {
		return ""
	}
	return "as of " + daysAgo(g.Freshness.DaysSince)
}

// homeAsOfClass marks a Health pill whose Health is its last Check-in's, the
// Goal being Check-in overdue (healthAsOf), or is "" for a current one.
func homeAsOfClass(g domain.PersonalGoal) string {
	if healthAsOf(g) == "" {
		return ""
	}
	return "as-of"
}

// healthBarLabel reads the Health distribution bar's counts.
func healthBarLabel(v homeView) string {
	return fmt.Sprintf("%d Green, %d Yellow, %d Red", v.Green, v.Yellow, v.Red)
}

// homeToday reads today, in the org's timezone, as the page title's date:
// "Monday, Jan 12".
func homeToday(d displayDates) string {
	return d.Now.In(d.Loc).Weekday().String() + ", " + showDayOf(d.Now, d.Now, d.Loc)
}

// homeTodayISO is today, in the org's timezone, for the date's datetime=.
func homeTodayISO(d displayDates) string {
	return d.Now.In(d.Loc).Format(dateLayout)
}

// othersAllClear reads the count of people directly under the viewer whose
// Org has nothing flagged.
func othersAllClear(n int) string {
	if n == 1 {
		return "1 other all clear"
	}
	return fmt.Sprintf("%d others all clear", n)
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

// confirmScript asks the reader to confirm msg before a form is sent. Any text
// is safe: msg goes in as a JSON string literal, and the script is escaped for
// the attribute it's written into.
func confirmScript(msg string) templ.ComponentScript {
	lit, _ := json.Marshal(msg)
	return templ.JSUnsafeFuncCall("return confirm(" + string(lit) + ")")
}
