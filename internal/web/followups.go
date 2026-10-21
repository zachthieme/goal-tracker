package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleFollowUps answers "what's going wrong?" for leadership: every Goal the
// freshness and graph signals flag, by problem type, scoped by its address to
// Everyone (?org=all), the viewer's own Goals (?org=mine) or an Org's
// (?org=), else the viewer's default scope, and to the Goals with an offered
// Dimension's value (?value=, one per Dimension). The scope narrows the rows
// only after they are built: loadFollowUps stays company-wide, for Needs an
// Admin.
func (s *Server) handleFollowUps(w http.ResponseWriter, r *http.Request, current domain.Account) {
	page, err := s.loadFollowUpsPage(r.Context(), r.URL.Query(), current)
	if err != nil {
		s.serverError(w, r, "could not read Follow-ups", err)
		return
	}
	render(w, r, http.StatusOK, followUpsPage(&current, page))
}

// loadFollowUpsPage builds the Follow-ups page current sees at an address with query q.
func (s *Server) loadFollowUpsPage(ctx context.Context, q url.Values, current domain.Account) (followUpsPageView, error) {
	v, err := s.loadFollowUps(ctx)
	if err != nil {
		return followUpsPageView{}, err
	}
	rows, err := s.followUpRows(ctx, v)
	if err != nil {
		return followUpsPageView{}, err
	}
	delegated, err := s.svc.DelegatedGoals(ctx, current.ID)
	if err != nil {
		return followUpsPageView{}, fmt.Errorf("load delegated goals: %w", err)
	}
	facts := followUpFixFacts{delegate: map[int64]bool{}, unreachable: map[int64]bool{}}
	for _, g := range delegated {
		facts.delegate[g.ID] = true
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Goal.ID)
		if !row.Goal.Ownerless {
			continue
		}
		delegates, err := s.svc.ListDelegates(ctx, row.Goal.ID)
		if err != nil {
			return followUpsPageView{}, err
		}
		facts.unreachable[row.Goal.ID] = !slices.ContainsFunc(delegates, func(a domain.Account) bool { return !a.Departed })
	}
	if facts.nudged, err = s.svc.NudgedToday(ctx, ids); err != nil {
		return followUpsPageView{}, err
	}
	fixFollowUpRows(current, rows, facts)
	dims, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return followUpsPageView{}, err
	}
	goals, err := s.svc.ListGoals(ctx)
	if err != nil {
		return followUpsPageView{}, err
	}
	group := followUpGroupKey(q.Get("group"))
	org, err := s.loadPageOrgScope(ctx, q, current, goals)
	if err != nil {
		return followUpsPageView{}, err
	}
	page := followUpsPageView{
		followUpsView: v,
		Group:         group,
		Org:           org,
		Dimensions:    domain.OfferedDimensions(dims),
		Today:         s.svc.Now().In(s.svc.Timezone()),
	}
	page.Values = followUpValues(q["value"], page.Dimensions)
	var valued map[int64]bool
	if len(page.Values) > 0 {
		goals, err := s.svc.ListGoalsWithValues(ctx)
		if err != nil {
			return followUpsPageView{}, err
		}
		byDimension := map[int64][]int64{}
		for _, v := range page.Values {
			byDimension[v.DimensionID] = []int64{v.ID}
		}
		valued = map[int64]bool{}
		for _, gv := range domain.FilterGoals(goals, byDimension) {
			valued[gv.Goal.ID] = true
		}
	}
	for _, row := range rows {
		if valued != nil && !valued[row.Goal.ID] {
			continue
		}
		page.CompanyRows = append(page.CompanyRows, row)
		if org.keeps(row.Goal) {
			page.Rows = append(page.Rows, row)
		}
	}
	return page, nil
}

// followUpValues are the offered Dimensions' values raw names by their IDs, in
// the Dimensions' order, at most one per Dimension: the first raw names there.
// A raw naming none of them is ignored.
func followUpValues(raw []string, dims []domain.Dimension) []domain.DimensionValue {
	var values []domain.DimensionValue
	for _, d := range dims {
	value:
		for _, r := range raw {
			id, err := strconv.ParseInt(r, 10, 64)
			if err != nil {
				continue
			}
			for _, v := range d.Values {
				if v.ID == id {
					values = append(values, v)
					break value
				}
			}
		}
	}
	return values
}

// followUpValueLabel is how the value select names a value: a Retired one says so.
func followUpValueLabel(v domain.DimensionValue) string {
	if v.Retired {
		return v.Value + " (retired)"
	}
	return v.Value
}

// followUpsPageView is the Follow-ups page: its lists, its rows in scope, its rows
// with its values in any scope, which Needs an Admin's card and table hold, the
// group its address filters it to, "" for every group, its Scope, the offered
// Dimensions and the values of theirs it is scoped to, at most one each, in
// the Dimensions' order, and today, which its dates are read against.
type followUpsPageView struct {
	followUpsView
	Rows        []followUpGoalRow
	CompanyRows []followUpGoalRow
	Group       string
	Org         pageOrgScope
	Dimensions  []domain.Dimension
	Values      []domain.DimensionValue
	Today       time.Time
}

// valueIn is the value of the Dimension dimID the page is scoped to, or the
// zero value when it is scoped to none of that Dimension's.
func (p followUpsPageView) valueIn(dimID int64) domain.DimensionValue {
	for _, v := range p.Values {
		if v.DimensionID == dimID {
			return v
		}
	}
	return domain.DimensionValue{}
}

// url is the Follow-ups page's address filtered to group ("" for every group),
// keeping the page's scope and every value it is scoped to.
func (p followUpsPageView) url(group string) string {
	q := url.Values{}
	if sc := p.Org.Value(); sc != "" {
		q.Set("org", sc)
	}
	if group != "" {
		q.Set("group", group)
	}
	for _, v := range p.Values {
		q.Add("value", strconv.FormatInt(v.ID, 10))
	}
	if len(q) == 0 {
		return "/follow-ups"
	}
	return "/follow-ups?" + q.Encode()
}

// Here is the page's own address, with its filters and scope.
func (p followUpsPageView) Here() string {
	return p.url(p.Group)
}

// ShowAllURL is the page's address unfiltered, keeping its scope.
func (p followUpsPageView) ShowAllURL() templ.SafeURL {
	return templ.SafeURL(p.url(""))
}

// ShowEverythingURL is the Follow-ups page with no filter and scoped to Everyone.
func (p followUpsPageView) ShowEverythingURL() templ.SafeURL {
	return templ.SafeURL("/follow-ups?org=" + scopeEveryoneValue)
}

// Shown are the rows the table shows: every row with no filter, else each row
// a signal of the filtered group flags, chipped with that group's signals
// only. Needs an Admin's are the company's, whatever the page's scope: an
// Admin's job there is company-wide.
func (p followUpsPageView) Shown() []followUpGoalRow {
	var kinds []string
	for _, gk := range followUpGroupKinds {
		if gk.Key == p.Group {
			kinds = gk.Kinds
		}
	}
	if kinds == nil {
		return p.Rows
	}
	rows := p.Rows
	if p.Group == followUpGroupAdmin {
		rows = p.CompanyRows
	}
	var shown []followUpGoalRow
	for _, r := range rows {
		var signals []followUpSignal
		for _, sig := range r.Signals {
			if slices.Contains(kinds, sig.Kind) {
				signals = append(signals, sig)
			}
		}
		if len(signals) > 0 {
			r.Signals = signals
			shown = append(shown, r)
		}
	}
	return shown
}

// Filtered reports whether the page is scoped to Mine, an Org or values, or
// filtered to a group.
func (p followUpsPageView) Filtered() bool {
	return p.Org.Name() != "" || len(p.Values) > 0 || p.Group != ""
}

// CardsWhenEmpty reports whether the page shows its cards after its empty
// line: for an Admin, with no group filter, while the company has a Goal
// Needs an Admin holds, so an Admin's own empty scope doesn't hide it.
func (p followUpsPageView) CardsWhenEmpty() bool {
	if !p.Org.Viewer.IsAdmin || p.Group != "" {
		return false
	}
	for i, g := range followUpGroups(p.CompanyRows) {
		if followUpGroupKinds[i].Key == followUpGroupAdmin {
			return g.Count > 0
		}
	}
	return false
}

// OffersShowEverything reports whether the empty line links to everything:
// when the page is filtered, but not on Needs an Admin, which is company-wide
// already.
func (p followUpsPageView) OffersShowEverything() bool {
	return p.Filtered() && p.Group != followUpGroupAdmin
}

// Empty is what the page says when no Goal is left to show: where nothing
// needs attention, naming Mine or the Org, each value with its Dimension's
// name, in the Dimensions' order, and the group's card's title, in that order.
// Needs an Admin, company-wide, names only the values: "Nothing needs an
// Admin in Team Platform."
func (p followUpsPageView) Empty() string {
	var parts []string
	if name := p.Org.Name(); name != "" && p.Group != followUpGroupAdmin {
		parts = append(parts, name)
	}
	for _, d := range p.Dimensions {
		if v := p.valueIn(d.ID); v.ID != 0 {
			parts = append(parts, d.Name+" "+v.Value)
		}
	}
	if p.Group == followUpGroupAdmin {
		if len(parts) == 0 {
			return "Nothing needs an Admin."
		}
		return "Nothing needs an Admin in " + strings.Join(parts, ", ") + "."
	}
	for _, gk := range followUpGroupKinds {
		if gk.Key == p.Group {
			parts = append(parts, gk.Name)
		}
	}
	if len(parts) == 0 {
		return "No Goals need attention."
	}
	return "No Goals need attention in " + strings.Join(parts, ", ") + "."
}

// attentionCount is the header's count: the Goals in the page's scope, or on
// Needs an Admin the company's Goals its table shows.
func (p followUpsPageView) attentionCount() int {
	if p.Group == followUpGroupAdmin {
		return len(p.Shown())
	}
	return len(p.Rows)
}

// attention is the header's words after its count, naming Mine or the Org
// the page is scoped to: "Goals to review in My org, most urgent first." On
// Needs an Admin they say the Goals wait on an Admin instead.
func (p followUpsPageView) attention() string {
	goals := "Goals"
	if p.attentionCount() == 1 {
		goals = "Goal"
	}
	if p.Group == followUpGroupAdmin {
		return goals + " waiting on an Admin, most urgent first."
	}
	in := ""
	if name := p.Org.Name(); name != "" {
		in = " in " + name
	}
	return goals + " to review" + in + ", most urgent first."
}

// followUpChipClass is the class of a signal's chip on a group card, matching the
// table's: the Red and Yellow Health badges for Not Green's, the Check-in
// overdue badge for the freshness signals (Milestone overdue among them), the
// Ownerless badge for Ownerless, and the outlined tag for the structural ones.
func followUpChipClass(kind string) string {
	switch kind {
	case "red":
		return "badge r"
	case "yellow":
		return "badge y"
	case "checkin-overdue", "path-overdue", "milestone-overdue", "action-item-overdue":
		return "badge st"
	case "ownerless":
		return "badge ol"
	}
	return "rk-tag"
}

// followUpGroupKey is the group key names, or "" for an unknown one, which shows
// every group.
func followUpGroupKey(key string) string {
	for _, gk := range followUpGroupKinds {
		if gk.Key == key {
			return key
		}
	}
	return ""
}

// followUpCard is a group's summary card: its count, a count of each of its
// signals that flags a Goal, the line it shows when it holds none, whether the
// page is filtered to it, and the page's address filtered to it, keeping the
// page's scope.
type followUpCard struct {
	followUpGroup
	Signals []followUpType
	Blank   string
	Current bool
	Href    templ.SafeURL
}

// Attrs marks the card of the group the page is filtered to as the current one.
func (c followUpCard) Attrs() templ.Attributes {
	if c.Current {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// Cards are the page's group cards, in the page's order, each counting the
// page's rows but Needs an Admin, which counts the company's: an Admin's job
// there is company-wide.
func (p followUpsPageView) Cards() []followUpCard {
	names := map[string]string{}
	for _, d := range followUpDefinitions {
		names[d.Kind] = d.Name
	}
	groups, company := followUpGroups(p.Rows), followUpGroups(p.CompanyRows)
	cards := make([]followUpCard, 0, len(groups))
	for i, gk := range followUpGroupKinds {
		rows, group := p.Rows, groups[i]
		if gk.Key == followUpGroupAdmin {
			rows, group = p.CompanyRows, company[i]
		}
		c := followUpCard{followUpGroup: group, Blank: gk.Blank, Current: gk.Key == p.Group, Href: templ.SafeURL(p.url(gk.Key))}
		for _, kind := range gk.Kinds {
			n := 0
			for _, r := range rows {
				if _, ok := r.signal(kind); ok {
					n++
				}
			}
			if n > 0 {
				c.Signals = append(c.Signals, followUpType{Anchor: kind, Name: names[kind], Count: n})
			}
		}
		cards = append(cards, c)
	}
	return cards
}

// followUpsView is what the Follow-ups page shows, one list per problem type.
type followUpsView struct {
	CheckinOverdue []domain.GoalFreshness
	OverduePaths   []domain.GoalFreshness
	// OverdueMilestones are the Active Goals whose latest Check-in is Green
	// while a Milestone is overdue (domain.Freshness.MilestoneOverdue).
	OverdueMilestones []domain.GoalFreshness
	// NotGreen are the Active Goals whose latest Check-in is Red or Yellow.
	NotGreen []domain.GoalFreshness
	// Asks are the open Asks for help, each a Goal NotGreen holds.
	Asks []domain.AskForHelp
	// Ownerless are the Active Goals whose Owner has left the company.
	Ownerless []domain.Goal
	Unlinked  []domain.Goal
	// ScheduleConflicts and HaltedParents are listed by their child, the Goal
	// that needs to change.
	ScheduleConflicts []domain.ScheduleConflict
	HaltedParents     []domain.HaltedParent
	// OverdueActionItems are the Active Goals an overdue Action Item is about,
	// one entry each.
	OverdueActionItems []followUpOverdueGoal
}

// followUpOverdueGoal is a Goal with an overdue Action Item about it, and how many
// days the most overdue of them is past its due date.
type followUpOverdueGoal struct {
	Goal domain.Goal
	Days int
}

// followUpType is a signal's chip on a group card: its kind, its name, and how
// many Goals it flags.
type followUpType struct {
	Anchor, Name string
	Count        int
}

// followUpDefinition is one of the Follow-ups page's signals: the kind its chips
// carry, its name, and what it means.
type followUpDefinition struct{ Kind, Name, Means string }

// followUpDefinitions are the Follow-ups page's signals in the page's order, each with
// what it means: what a chip's button opens, and what "What each signal
// means" lists. They are the one place a signal is named: the group cards'
// chips take their names from here too.
var followUpDefinitions = []followUpDefinition{
	{"red", "Red", "Active Goals whose latest Check-in is Red."},
	{"yellow", "Yellow", "Active Goals whose latest Check-in is Yellow."},
	{"asks-for-help", "Asks for help", "Red or Yellow Goals whose Path to Green asks for help from the Owner's Manager, until the Owner unmarks it or the Goal is back to Green."},
	{"checkin-overdue", "Check-in overdue", "Active Goals whose last Check-in, or activation if they have none, is older than their cadence."},
	{"path-overdue", "Path to Green overdue", "Active Goals past their Path to Green's target date and not yet Green."},
	{"milestone-overdue", "Milestone overdue", "Active Goals whose latest Check-in is Green while a Milestone is still Planned past its date. The Owner confirms or updates the Health at the next Check-in."},
	{"action-item-overdue", "Action Item overdue", "An Action Item about this Goal, raised in a Report, is past its due date."},
	{"ownerless", "Ownerless", "Active Goals whose Owner has left the company and hasn't been replaced."},
	{"unlinked", "Unlinked", "Active Goals that contribute to no other Goal and aren't Top-level."},
	{"schedule-conflicts", "Schedule conflicts", "Goals due later than a Goal they contribute to."},
	{"halted-parents", "Parent On Hold or Cancelled", "Goals contributing to a Goal that is On Hold or Cancelled."},
}

// followUpName is the name followUpDefinitions gives the signal kind, so a
// chip that shows the signal's plain name reads it from the one list.
func followUpName(kind string) string {
	for _, d := range followUpDefinitions {
		if d.Kind == kind {
			return d.Name
		}
	}
	return ""
}

// followUpDefinitionsIn are the definitions of the signals flagging rows, in the
// page's order, each once: the popovers the chips' buttons open.
func followUpDefinitionsIn(rows []followUpGoalRow) []followUpDefinition {
	chipped := map[string]bool{}
	for _, r := range rows {
		for _, sig := range r.Signals {
			chipped[sig.Kind] = true
		}
	}
	var defs []followUpDefinition
	for _, d := range followUpDefinitions {
		if chipped[d.Kind] {
			defs = append(defs, d)
		}
	}
	return defs
}

// followUpMeansID is the id of the popover saying what the signal of kind means.
func followUpMeansID(kind string) string {
	return "follow-up-means-" + kind
}

// Flagged counts the Goals the Follow-ups page lists in scope, each once however
// many signals flag it.
func (v followUpsView) Flagged(scope pageOrgScope) int {
	flagged := map[int64]bool{}
	flag := func(g domain.Goal) {
		if scope.keeps(g) {
			flagged[g.ID] = true
		}
	}
	for _, list := range [][]domain.GoalFreshness{v.NotGreen, v.CheckinOverdue, v.OverduePaths, v.OverdueMilestones} {
		for _, gf := range list {
			flag(gf.Goal)
		}
	}
	for _, list := range [][]domain.Goal{v.Ownerless, v.Unlinked} {
		for _, g := range list {
			flag(g)
		}
	}
	for _, c := range v.ScheduleConflicts {
		flag(c.Child)
	}
	for _, hp := range v.HaltedParents {
		flag(hp.Child)
	}
	for _, og := range v.OverdueActionItems {
		flag(og.Goal)
	}
	for _, ask := range v.Asks {
		flag(ask.Goal)
	}
	return len(flagged)
}

// countDefaultFollowUps counts the Goals flagged in acc's default scope, as many
// as the rows a bare /follow-ups lists, for the sidebar's Follow-ups count.
func (s *Server) countDefaultFollowUps(ctx context.Context, acc domain.Account) (int, error) {
	v, err := s.loadFollowUps(ctx)
	if err != nil {
		return 0, err
	}
	scope, err := s.resolvePageOrgScope(ctx, url.Values{}, acc)
	if err != nil {
		return 0, err
	}
	return v.Flagged(scope), nil
}

// loadFollowUps reads every Goal the company's signals flag.
func (s *Server) loadFollowUps(ctx context.Context) (followUpsView, error) {
	fresh, err := s.svc.FreshnessSignals(ctx)
	if err != nil {
		return followUpsView{}, err
	}
	goals, err := s.svc.ListGoals(ctx)
	if err != nil {
		return followUpsView{}, err
	}
	graph, err := s.svc.GraphSignals(ctx)
	if err != nil {
		return followUpsView{}, err
	}
	v := followUpsView{
		CheckinOverdue:    fresh.CheckinOverdue,
		OverduePaths:      fresh.OverduePaths,
		OverdueMilestones: fresh.OverdueMilestones,
		NotGreen:          fresh.NotGreen,
		Unlinked:          graph.Unlinked,
		ScheduleConflicts: graph.ScheduleConflicts,
		HaltedParents:     graph.HaltedParents,
	}
	for _, g := range goals {
		if g.Ownerless && g.Lifecycle == domain.LifecycleActive {
			v.Ownerless = append(v.Ownerless, g)
		}
	}
	items, err := s.svc.OverdueActionItems(ctx)
	if err != nil {
		return followUpsView{}, err
	}
	v.OverdueActionItems = followUpOverdueGoals(goals, items, s.svc.Now().In(s.svc.Timezone()))
	if v.Asks, err = s.svc.OpenAsksForHelp(ctx); err != nil {
		return followUpsView{}, err
	}
	return v, nil
}

// followUpOverdueGoals are the Active Goals among goals that an overdue Action
// Item among items is about, in goals' order, each with the days its most
// overdue item is past its due date as of today.
func followUpOverdueGoals(goals []domain.Goal, items []domain.ActionItemListing, today time.Time) []followUpOverdueGoal {
	days := map[int64]int{}
	for _, l := range items {
		if d := calendarDays(l.Item.DueDate, today); d > days[l.Item.GoalID] {
			days[l.Item.GoalID] = d
		}
	}
	var out []followUpOverdueGoal
	for _, g := range goals {
		if d, ok := days[g.ID]; ok && g.Lifecycle == domain.LifecycleActive {
			out = append(out, followUpOverdueGoal{Goal: g, Days: d})
		}
	}
	return out
}

// followUpGoalRow is one flagged Goal on the Follow-ups page with every signal that
// flags it, so a Goal flagged twice is one row, not two.
type followUpGoalRow struct {
	Goal domain.Goal
	// Health is the Goal's latest Check-in's Health, empty when it has none.
	Health string
	// RunOffGreen is how long a Not Green row's Goal has been Yellow or Red,
	// shown beside its Health; it isn't running on any other row (CONTEXT.md:
	// Run off Green).
	RunOffGreen domain.RunOffGreen
	// PathTargetDate is a Not Green row's back-to-Green date, its latest
	// Check-in's, and PathPriorDates the dates it slipped from within the Run
	// off Green, earliest first (see domain.BackToGreenPriorDates). Only a
	// Not Green row reads them.
	PathTargetDate time.Time
	PathPriorDates []time.Time
	Signals        []followUpSignal
	// Fix is the one action the row offers its viewer.
	Fix followUpFix
}

// followUpFix is a row's one Fix button: what it says, where it goes, and whether
// it is the page's primary kind of action. Post makes it a form posting to
// Href rather than a link; Disabled shows it without letting it be used.
type followUpFix struct {
	Label    string
	Href     string
	Primary  bool
	Post     bool
	Disabled bool
}

// followUpFixFacts are what the rows' Fixes turn on beyond the rows themselves:
// the Goals the viewer is a Delegate on, the Goals nudged today with their
// Nudge, and the Ownerless Goals with no Delegate left to check in on them.
type followUpFixFacts struct {
	delegate    map[int64]bool
	nudged      map[int64]domain.Nudge
	unreachable map[int64]bool
}

// fixFollowUpRows sets each row's Fix for current.
func fixFollowUpRows(current domain.Account, rows []followUpGoalRow, facts followUpFixFacts) {
	for i := range rows {
		id := rows[i].Goal.ID
		nudge, nudged := facts.nudged[id]
		var today *domain.Nudge
		if nudged {
			today = &nudge
		}
		rows[i].Fix = rows[i].fix(current, facts.delegate, today, facts.unreachable[id])
	}
}

// fix is the row's Fix for current, given the Goals current is a Delegate on,
// the row's Goal's Nudge today, if any, and whether nobody is left to check in
// on it. It is the first that applies of:
//  1. Reassign, for an Ownerless Goal and an Admin;
//  2. Check in, for a Check-in overdue, Path to Green overdue or Milestone
//     overdue Goal current Owns or is a Delegate on (an Admin has no Check-in
//     right);
//  3. Nudge, for someone else's Check-in overdue, Path to Green overdue or
//     Milestone overdue Goal that someone can check in on (CONTEXT.md:
//     Nudge); once it has been nudged today, a disabled "Nudged today by"
//     whoever did.
//     Freshness outranks alignment, as 2 outranks 4;
//  4. Link to a parent, for an Unlinked Goal current Owns;
//  5. For a Schedule conflict, see scheduleConflictFix: Check in, its dates
//     open, on current's own Goal in the conflict, or Nudge on the row's Goal
//     for anyone else (CONTEXT.md: Schedule conflict, Nudge);
//  6. Open parent, for a Goal under a halted parent: the first by title;
//  7. Suggest a parent, for an Unlinked Goal current doesn't Own, a Delegate
//     on it included (CONTEXT.md: Parent suggestion);
//  8. See Action Items, the Goal page's, for a Goal with an overdue Action
//     Item;
//  9. Open Goal, for anything else, a Check-in overdue Goal nobody can check in
//     on and a Goal flagged only for its Health, Red or Yellow, among them.
func (r followUpGoalRow) fix(current domain.Account, delegate map[int64]bool, nudged *domain.Nudge, unreachable bool) followUpFix {
	goal := fmt.Sprintf("/goals/%d", r.Goal.ID)
	if _, ownerless := r.signal("ownerless"); ownerless && current.IsAdmin {
		return followUpFix{Label: "Reassign", Href: goal + "?open=reassign"}
	}
	_, checkinOverdue := r.signal("checkin-overdue")
	_, pathOverdue := r.signal("path-overdue")
	_, milestoneOverdue := r.signal("milestone-overdue")
	overdue := pathOverdue || milestoneOverdue
	if (checkinOverdue || overdue) && (r.Goal.Owner.ID == current.ID || delegate[r.Goal.ID]) {
		return followUpFix{Label: "Check in", Href: goal + "/checkin", Primary: true}
	}
	if (checkinOverdue || overdue) && nudged != nil {
		return followUpFix{Label: "Nudged today by " + nudged.Sender.Label(), Disabled: true}
	}
	if (checkinOverdue || overdue) && !unreachable {
		return followUpFix{Label: "Nudge", Href: goal + "/nudge", Post: true}
	}
	if _, unlinked := r.signal("unlinked"); unlinked && r.Goal.Owner.ID == current.ID {
		return followUpFix{Label: "Link to a parent", Href: goal + "?open=parent-link"}
	}
	if f, ok := r.scheduleConflictFix(current, delegate, nudged, unreachable); ok {
		return f
	}
	var parent *domain.Goal
	for _, sig := range r.Signals {
		if sig.Kind == "halted-parents" && (parent == nil || strings.ToLower(sig.Related.Title) < strings.ToLower(parent.Title)) {
			parent = sig.Related
		}
	}
	if parent != nil {
		return followUpFix{Label: "Open parent", Href: fmt.Sprintf("/goals/%d", parent.ID)}
	}
	if _, unlinked := r.signal("unlinked"); unlinked {
		return followUpFix{Label: "Suggest a parent", Href: goal + "/suggest-parent"}
	}
	if _, overdue := r.signal("action-item-overdue"); overdue {
		return followUpFix{Label: "See Action Items", Href: goal + "#action-items"}
	}
	return followUpFix{Label: "Open Goal", Href: goal}
}

// scheduleConflictFix is the Fix for a row in a Schedule conflict, and
// whether it has one. Its candidates are the row's Goal, the child, then each
// conflict's parent by title; current can act on those they Own or are a
// Delegate on. It is the first that applies of:
//  1. Check in, its dates open, on the first candidate current can act on
//     that is Active;
//  2. Open Goal on the first candidate current can act on, when none is
//     Active, as only an Active Goal takes an ordinary Check-in;
//  3. for anyone else, while the row's Goal is Active, a disabled "Nudged
//     today by" whoever nudged it today, or else Nudge unless nobody is left
//     to check in on it.
//
// Otherwise the row has none here, and falls through to the later Fixes.
func (r followUpGoalRow) scheduleConflictFix(current domain.Account, delegate map[int64]bool, nudged *domain.Nudge, unreachable bool) (followUpFix, bool) {
	var parents []domain.Goal
	for _, sig := range r.Signals {
		if sig.Kind == "schedule-conflicts" {
			parents = append(parents, *sig.Related)
		}
	}
	if len(parents) == 0 {
		return followUpFix{}, false
	}
	slices.SortStableFunc(parents, func(a, b domain.Goal) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
	var mine *domain.Goal
	for _, g := range append([]domain.Goal{r.Goal}, parents...) {
		if g.Owner.ID != current.ID && !delegate[g.ID] {
			continue
		}
		if g.Lifecycle == domain.LifecycleActive {
			return followUpFix{Label: "Check in", Href: fmt.Sprintf("/goals/%d/checkin?open=dates", g.ID), Primary: true}, true
		}
		if mine == nil {
			mine = &g
		}
	}
	if mine != nil {
		return followUpFix{Label: "Open Goal", Href: fmt.Sprintf("/goals/%d", mine.ID)}, true
	}
	if r.Goal.Lifecycle != domain.LifecycleActive {
		return followUpFix{}, false
	}
	if nudged != nil {
		return followUpFix{Label: "Nudged today by " + nudged.Sender.Label(), Disabled: true}, true
	}
	if !unreachable {
		return followUpFix{Label: "Nudge", Href: fmt.Sprintf("/goals/%d/nudge", r.Goal.ID), Post: true}, true
	}
	return followUpFix{}, false
}

// followUpSignal is one reason a Goal is flagged. Kind names the signal, as the
// chip's data-kind on the Follow-ups page. Days is how long the signal has held: for
// "checkin-overdue" the days since the last update, for "path-overdue" the days
// past the Path to Green's target date, for "milestone-overdue" the days past
// the earliest overdue Milestone's date, for "action-item-overdue" the days the
// most overdue Action Item about the Goal is past its due date, for
// "schedule-conflicts" the days the child's delivery date falls after the
// parent's, and 0 otherwise. Related is the parent for "schedule-conflicts" and
// "halted-parents". Person is whom an "asks-for-help" asks, nil when no one is
// above the Owner.
type followUpSignal struct {
	Kind    string
	Days    int
	Related *domain.Goal
	Person  *domain.Account
}

// followUpRows merges v's lists into one row per flagged Goal. Unlike loadFollowUps,
// which every signed-in page runs for the sidebar's count, it reads each
// flagged Goal's latest Check-in, so only the Follow-ups page calls it.
func (s *Server) followUpRows(ctx context.Context, v followUpsView) ([]followUpGoalRow, error) {
	var rows []followUpGoalRow
	at := map[int64]int{}
	add := func(g domain.Goal, sig followUpSignal) {
		i, ok := at[g.ID]
		if !ok {
			i = len(rows)
			at[g.ID] = i
			rows = append(rows, followUpGoalRow{Goal: g})
		}
		rows[i].Signals = append(rows[i].Signals, sig)
	}
	for _, gf := range v.NotGreen {
		add(gf.Goal, followUpSignal{Kind: followUpHealthKind(gf.Health)})
	}
	for _, ask := range v.Asks {
		add(ask.Goal, followUpSignal{Kind: "asks-for-help", Person: ask.Addressee})
	}
	for _, gf := range v.CheckinOverdue {
		add(gf.Goal, followUpSignal{Kind: "checkin-overdue", Days: gf.Freshness.DaysSince})
	}
	// A Path to Green's overdue days are counted in the Goal's timezone, the
	// one its flag was judged in.
	for _, gf := range v.OverduePaths {
		add(gf.Goal, followUpSignal{Kind: "path-overdue", Days: calendarDays(gf.Freshness.PathTargetDate, s.svc.Now().In(s.svc.GoalTimezone(gf.Goal)))})
	}
	// So are a Milestone's.
	for _, gf := range v.OverdueMilestones {
		add(gf.Goal, followUpSignal{Kind: "milestone-overdue", Days: calendarDays(gf.Freshness.OverdueMilestoneDate, s.svc.Now().In(s.svc.GoalTimezone(gf.Goal)))})
	}
	for _, og := range v.OverdueActionItems {
		add(og.Goal, followUpSignal{Kind: "action-item-overdue", Days: og.Days})
	}
	for _, g := range v.Ownerless {
		add(g, followUpSignal{Kind: "ownerless"})
	}
	for _, g := range v.Unlinked {
		add(g, followUpSignal{Kind: "unlinked"})
	}
	for _, c := range v.ScheduleConflicts {
		add(c.Child, followUpSignal{Kind: "schedule-conflicts", Days: calendarDays(c.Parent.DeliveryDate, c.Child.DeliveryDate), Related: &c.Parent})
	}
	for _, hp := range v.HaltedParents {
		add(hp.Child, followUpSignal{Kind: "halted-parents", Related: &hp.Parent})
	}
	for i := range rows {
		latest, ok, err := s.svc.LatestCheckin(ctx, rows[i].Goal.ID)
		if err != nil {
			return nil, fmt.Errorf("load flagged goal's check-in: %w", err)
		}
		if ok {
			rows[i].Health = latest.Health
		}
		_, red := rows[i].signal("red")
		_, yellow := rows[i].signal("yellow")
		if red || yellow {
			if rows[i].RunOffGreen, err = s.svc.RunOffGreen(ctx, rows[i].Goal.ID); err != nil {
				return nil, fmt.Errorf("read not green goal's run off green: %w", err)
			}
			slips, err := s.svc.ListDateSlips(ctx, rows[i].Goal.ID)
			if err != nil {
				return nil, fmt.Errorf("load not green goal's date slips: %w", err)
			}
			rows[i].PathTargetDate = latest.PathTargetDate
			rows[i].PathPriorDates = domain.BackToGreenPriorDates(slips, rows[i].RunOffGreen)
		}
	}
	sortFollowUpRows(rows)
	return rows, nil
}

// followUpHealthKind is the signal kind a Not Green Goal's Health flags it with:
// "red" for Red, "yellow" for Yellow.
func followUpHealthKind(health string) string {
	if health == domain.HealthRed {
		return "red"
	}
	return "yellow"
}

// signal is the row's first signal of kind, and whether it has one.
func (r followUpGoalRow) signal(kind string) (followUpSignal, bool) {
	for _, sig := range r.Signals {
		if sig.Kind == kind {
			return sig, true
		}
	}
	return followUpSignal{}, false
}

// signalKinds counts the distinct kinds of signal that flag the row, so a Goal
// under two halted parents counts its halted parents once.
func (r followUpGoalRow) signalKinds() int {
	seen := map[string]bool{}
	for _, sig := range r.Signals {
		seen[sig.Kind] = true
	}
	return len(seen)
}

// followUpHealthRank orders Health most urgent first: Red, Yellow, none, then
// Green.
func followUpHealthRank(health string) int {
	switch health {
	case domain.HealthRed:
		return 0
	case domain.HealthYellow:
		return 1
	case domain.HealthGreen:
		return 3
	}
	return 2
}

// sortFollowUpRows puts the most urgent Goals first, key by key: Active Goals
// before the Proposed and On Hold ones, which nothing has gone wrong with yet;
// then Ownerless first; then by Health (followUpHealthRank); then a Path to Green
// overdue first, longest overdue first; then a Milestone overdue first, longest
// overdue first; then an overdue Action Item first, longest overdue first; then
// Check-in overdue first, furthest past its cadence first; then more distinct
// kinds of signal first; then by title, ignoring case.
func sortFollowUpRows(rows []followUpGoalRow) {
	slices.SortStableFunc(rows, func(a, b followUpGoalRow) int {
		aActive := a.Goal.Lifecycle == domain.LifecycleActive
		bActive := b.Goal.Lifecycle == domain.LifecycleActive
		if c := cmp.Compare(boolRank(!aActive), boolRank(!bActive)); c != 0 {
			return c
		}
		_, aOwnerless := a.signal("ownerless")
		_, bOwnerless := b.signal("ownerless")
		if c := cmp.Compare(boolRank(!aOwnerless), boolRank(!bOwnerless)); c != 0 {
			return c
		}
		if c := cmp.Compare(followUpHealthRank(a.Health), followUpHealthRank(b.Health)); c != 0 {
			return c
		}
		aPath, aOverdue := a.signal("path-overdue")
		bPath, bOverdue := b.signal("path-overdue")
		if c := cmp.Compare(boolRank(!aOverdue), boolRank(!bOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bPath.Days, aPath.Days); c != 0 {
			return c
		}
		aMilestone, aMilestoneOverdue := a.signal("milestone-overdue")
		bMilestone, bMilestoneOverdue := b.signal("milestone-overdue")
		if c := cmp.Compare(boolRank(!aMilestoneOverdue), boolRank(!bMilestoneOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bMilestone.Days, aMilestone.Days); c != 0 {
			return c
		}
		aItem, aItemOverdue := a.signal("action-item-overdue")
		bItem, bItemOverdue := b.signal("action-item-overdue")
		if c := cmp.Compare(boolRank(!aItemOverdue), boolRank(!bItemOverdue)); c != 0 {
			return c
		}
		if c := cmp.Compare(bItem.Days, aItem.Days); c != 0 {
			return c
		}
		aCheckinOverdue, aIsCheckinOverdue := a.signal("checkin-overdue")
		bCheckinOverdue, bIsCheckinOverdue := b.signal("checkin-overdue")
		if c := cmp.Compare(boolRank(!aIsCheckinOverdue), boolRank(!bIsCheckinOverdue)); c != 0 {
			return c
		}
		if aIsCheckinOverdue && bIsCheckinOverdue {
			if c := cmp.Compare(bCheckinOverdue.Days-b.Goal.CadenceDays, aCheckinOverdue.Days-a.Goal.CadenceDays); c != 0 {
				return c
			}
		}
		if c := cmp.Compare(b.signalKinds(), a.signalKinds()); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title))
	})
}

// followUpDays says a count of days in words: "1 day", "22 days".
func followUpDays(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// calendarDays counts the calendar days from one date to a later one, each
// read as the date it falls on in its own location.
func calendarDays(from, to time.Time) int {
	date := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	return int(date(to).Sub(date(from)) / (24 * time.Hour))
}

// followUpGroup is a set of signal kinds grouped by who acts on them: Key names
// it, Name says it, and Count is how many Goals it flags, each once however
// many of its signals flag the Goal.
type followUpGroup struct {
	Key, Name string
	Count     int
}

// followUpGroupKinds are the Follow-ups page's groups in the page's order, with the
// signal kinds each holds.
// Blank is what a group's card says when it holds no Goals.
var followUpGroupKinds = []struct {
	Key, Name string
	Kinds     []string
	Blank     string
}{
	{"health", "Not Green", []string{"red", "yellow", "asks-for-help"}, "No Goal is Red or Yellow."},
	{"owner", "Waiting on a Check-in", []string{"checkin-overdue", "path-overdue", "milestone-overdue"}, "Nothing is waiting on a Check-in."},
	{"followup", "Action Items overdue", []string{"action-item-overdue"}, "No Action Item is overdue."},
	{"plan", "Placement and dates", []string{"unlinked", "schedule-conflicts"}, "Every Goal is placed and its dates fit."},
	{followUpGroupAdmin, "Needs an Admin", []string{"ownerless", "halted-parents"}, "Nothing needs an Admin."},
}

// followUpGroupAdmin is the key of the Needs an Admin group, whose card and table
// hold the company's rows.
const followUpGroupAdmin = "admin"

// followUpGroups counts the rows in each group, in the page's order.
func followUpGroups(rows []followUpGoalRow) []followUpGroup {
	groups := make([]followUpGroup, 0, len(followUpGroupKinds))
	for _, gk := range followUpGroupKinds {
		g := followUpGroup{Key: gk.Key, Name: gk.Name}
		for _, r := range rows {
			if slices.ContainsFunc(r.Signals, func(sig followUpSignal) bool { return slices.Contains(gk.Kinds, sig.Kind) }) {
				g.Count++
			}
		}
		groups = append(groups, g)
	}
	return groups
}
