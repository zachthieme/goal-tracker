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

// handleRisks answers "what's going wrong?" for leadership: every Goal the
// freshness and graph signals flag, by problem type, scoped by its address to
// Everyone (?org=all), the viewer's own Goals (?org=mine) or an Org's
// (?org=), else the viewer's default scope, and to the Goals with an offered
// Dimension's value (?value=, one per Dimension). The scope narrows the rows
// only after they are built: loadRisks stays company-wide, for Needs an
// Admin.
func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	page, err := s.loadRisksPage(r.Context(), r.URL.Query(), current)
	if err != nil {
		s.serverError(w, r, "could not read risks", err)
		return
	}
	render(w, r, http.StatusOK, risksPage(&current, page))
}

// loadRisksPage builds the Risks page current sees at an address with query q.
func (s *Server) loadRisksPage(ctx context.Context, q url.Values, current domain.Account) (risksPageView, error) {
	v, err := s.loadRisks(ctx)
	if err != nil {
		return risksPageView{}, err
	}
	rows, err := s.riskRows(ctx, v)
	if err != nil {
		return risksPageView{}, err
	}
	delegated, err := s.svc.DelegatedGoals(ctx, current.ID)
	if err != nil {
		return risksPageView{}, fmt.Errorf("load delegated goals: %w", err)
	}
	facts := riskFixFacts{delegate: map[int64]bool{}, unreachable: map[int64]bool{}}
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
			return risksPageView{}, err
		}
		facts.unreachable[row.Goal.ID] = !slices.ContainsFunc(delegates, func(a domain.Account) bool { return !a.Departed })
	}
	if facts.nudged, err = s.svc.NudgedToday(ctx, ids); err != nil {
		return risksPageView{}, err
	}
	fixRiskRows(current, rows, facts)
	dims, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return risksPageView{}, err
	}
	goals, err := s.svc.ListGoals(ctx)
	if err != nil {
		return risksPageView{}, err
	}
	group := riskGroupKey(q.Get("group"))
	org, err := s.loadPageOrgScope(ctx, q, current, goals)
	if err != nil {
		return risksPageView{}, err
	}
	page := risksPageView{
		risksView:  v,
		Group:      group,
		Org:        org,
		Dimensions: domain.OfferedDimensions(dims),
		Today:      s.svc.Now().In(s.svc.Timezone()),
	}
	page.Values = riskValues(q["value"], page.Dimensions)
	var valued map[int64]bool
	if len(page.Values) > 0 {
		goals, err := s.svc.ListGoalsWithValues(ctx)
		if err != nil {
			return risksPageView{}, err
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

// riskValues are the offered Dimensions' values raw names by their IDs, in
// the Dimensions' order, at most one per Dimension: the first raw names there.
// A raw naming none of them is ignored.
func riskValues(raw []string, dims []domain.Dimension) []domain.DimensionValue {
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

// riskValueLabel is how the value select names a value: a Retired one says so.
func riskValueLabel(v domain.DimensionValue) string {
	if v.Retired {
		return v.Value + " (retired)"
	}
	return v.Value
}

// risksPageView is the Risks page: its lists, its rows in scope, its rows
// with its values in any scope, which Needs an Admin's card and table hold, the
// group its address filters it to, "" for every group, its Scope, the offered
// Dimensions and the values of theirs it is scoped to, at most one each, in
// the Dimensions' order, and today, which its dates are read against.
type risksPageView struct {
	risksView
	Rows        []riskGoalRow
	CompanyRows []riskGoalRow
	Group       string
	Org         pageOrgScope
	Dimensions  []domain.Dimension
	Values      []domain.DimensionValue
	Today       time.Time
}

// valueIn is the value of the Dimension dimID the page is scoped to, or the
// zero value when it is scoped to none of that Dimension's.
func (p risksPageView) valueIn(dimID int64) domain.DimensionValue {
	for _, v := range p.Values {
		if v.DimensionID == dimID {
			return v
		}
	}
	return domain.DimensionValue{}
}

// url is the Risks page's address filtered to group ("" for every group),
// keeping the page's scope and every value it is scoped to.
func (p risksPageView) url(group string) string {
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
		return "/risks"
	}
	return "/risks?" + q.Encode()
}

// Here is the page's own address, with its filters and scope.
func (p risksPageView) Here() string {
	return p.url(p.Group)
}

// ShowAllURL is the page's address unfiltered, keeping its scope.
func (p risksPageView) ShowAllURL() templ.SafeURL {
	return templ.SafeURL(p.url(""))
}

// ShowEverythingURL is the Risks page with no filter and scoped to Everyone.
func (p risksPageView) ShowEverythingURL() templ.SafeURL {
	return templ.SafeURL("/risks?org=" + scopeEveryoneValue)
}

// Shown are the rows the table shows: every row with no filter, else each row
// a signal of the filtered group flags, chipped with that group's signals
// only. Needs an Admin's are the company's, whatever the page's scope: an
// Admin's job there is company-wide.
func (p risksPageView) Shown() []riskGoalRow {
	var kinds []string
	for _, gk := range riskGroupKinds {
		if gk.Key == p.Group {
			kinds = gk.Kinds
		}
	}
	if kinds == nil {
		return p.Rows
	}
	rows := p.Rows
	if p.Group == riskGroupAdmin {
		rows = p.CompanyRows
	}
	var shown []riskGoalRow
	for _, r := range rows {
		var signals []riskSignal
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
func (p risksPageView) Filtered() bool {
	return p.Org.Name() != "" || len(p.Values) > 0 || p.Group != ""
}

// CardsWhenEmpty reports whether the page shows its cards after its empty
// line: for an Admin, with no group filter, while the company has a Goal
// Needs an Admin holds, so an Admin's own empty scope doesn't hide it.
func (p risksPageView) CardsWhenEmpty() bool {
	if !p.Org.Viewer.IsAdmin || p.Group != "" {
		return false
	}
	for i, g := range riskGroups(p.CompanyRows) {
		if riskGroupKinds[i].Key == riskGroupAdmin {
			return g.Count > 0
		}
	}
	return false
}

// OffersShowEverything reports whether the empty line links to everything:
// when the page is filtered, but not on Needs an Admin, which is company-wide
// already.
func (p risksPageView) OffersShowEverything() bool {
	return p.Filtered() && p.Group != riskGroupAdmin
}

// Empty is what the page says when no Goal is left to show: where nothing
// needs attention, naming Mine or the Org, each value with its Dimension's
// name, in the Dimensions' order, and the group's card's title, in that order.
// Needs an Admin, company-wide, names only the values: "Nothing needs an
// Admin in Team Platform."
func (p risksPageView) Empty() string {
	var parts []string
	if name := p.Org.Name(); name != "" && p.Group != riskGroupAdmin {
		parts = append(parts, name)
	}
	for _, d := range p.Dimensions {
		if v := p.valueIn(d.ID); v.ID != 0 {
			parts = append(parts, d.Name+" "+v.Value)
		}
	}
	if p.Group == riskGroupAdmin {
		if len(parts) == 0 {
			return "Nothing needs an Admin."
		}
		return "Nothing needs an Admin in " + strings.Join(parts, ", ") + "."
	}
	for _, gk := range riskGroupKinds {
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
func (p risksPageView) attentionCount() int {
	if p.Group == riskGroupAdmin {
		return len(p.Shown())
	}
	return len(p.Rows)
}

// attention is the header's words after its count, naming Mine or the Org
// the page is scoped to: "Goals to review in My org, most urgent first." On
// Needs an Admin they say the Goals wait on an Admin instead.
func (p risksPageView) attention() string {
	goals := "Goals"
	if p.attentionCount() == 1 {
		goals = "Goal"
	}
	if p.Group == riskGroupAdmin {
		return goals + " waiting on an Admin, most urgent first."
	}
	in := ""
	if name := p.Org.Name(); name != "" {
		in = " in " + name
	}
	return goals + " to review" + in + ", most urgent first."
}

// riskChipClass is the class of a signal's chip on a group card, matching the
// table's: the Red and Yellow Health badges for Not Green's, the Check-in
// overdue badge for the freshness signals (Milestone overdue among them), the
// Ownerless badge for Ownerless, and the outlined tag for the structural ones.
func riskChipClass(kind string) string {
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

// riskGroupKey is the group key names, or "" for an unknown one, which shows
// every group.
func riskGroupKey(key string) string {
	for _, gk := range riskGroupKinds {
		if gk.Key == key {
			return key
		}
	}
	return ""
}

// riskCard is a group's summary card: its count, a count of each of its
// signals that flags a Goal, the line it shows when it holds none, whether the
// page is filtered to it, and the page's address filtered to it, keeping the
// page's scope.
type riskCard struct {
	riskGroup
	Signals []riskType
	Blank   string
	Current bool
	Href    templ.SafeURL
}

// Attrs marks the card of the group the page is filtered to as the current one.
func (c riskCard) Attrs() templ.Attributes {
	if c.Current {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// Cards are the page's group cards, in the page's order, each counting the
// page's rows but Needs an Admin, which counts the company's: an Admin's job
// there is company-wide.
func (p risksPageView) Cards() []riskCard {
	names := map[string]string{}
	for _, rt := range p.Types() {
		names[rt.Anchor] = rt.Name
	}
	groups, company := riskGroups(p.Rows), riskGroups(p.CompanyRows)
	cards := make([]riskCard, 0, len(groups))
	for i, gk := range riskGroupKinds {
		rows, group := p.Rows, groups[i]
		if gk.Key == riskGroupAdmin {
			rows, group = p.CompanyRows, company[i]
		}
		c := riskCard{riskGroup: group, Blank: gk.Blank, Current: gk.Key == p.Group, Href: templ.SafeURL(p.url(gk.Key))}
		for _, kind := range gk.Kinds {
			n := 0
			for _, r := range rows {
				if _, ok := r.signal(kind); ok {
					n++
				}
			}
			if n > 0 {
				c.Signals = append(c.Signals, riskType{Anchor: kind, Name: names[kind], Count: n})
			}
		}
		cards = append(cards, c)
	}
	return cards
}

// risksView is what the Risks page shows, one list per problem type.
type risksView struct {
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
	OverdueActionItems []riskOverdueGoal
}

// riskOverdueGoal is a Goal with an overdue Action Item about it, and how many
// days the most overdue of them is past its due date.
type riskOverdueGoal struct {
	Goal domain.Goal
	Days int
}

// riskType is one of the Risks page's signals, or its chip on a group card:
// its kind, its name, and how many Goals it flags.
type riskType struct {
	Anchor, Name string
	Count        int
}

// Types are the Risks page's signals in the page's order.
func (v risksView) Types() []riskType {
	red, yellow := 0, 0
	for _, gf := range v.NotGreen {
		if riskHealthKind(gf.Health) == "red" {
			red++
		} else {
			yellow++
		}
	}
	return []riskType{
		{"red", "Red", red},
		{"yellow", "Yellow", yellow},
		{"asks-for-help", "Asks for help", len(v.Asks)},
		{"checkin-overdue", "Check-in overdue", len(v.CheckinOverdue)},
		{"path-overdue", "Path to Green overdue", len(v.OverduePaths)},
		{"milestone-overdue", "Milestone overdue", len(v.OverdueMilestones)},
		{"action-item-overdue", "Action Item overdue", len(v.OverdueActionItems)},
		{"ownerless", "Ownerless", len(v.Ownerless)},
		{"unlinked", "Unlinked", len(v.Unlinked)},
		{"schedule-conflicts", "Schedule conflicts", len(v.ScheduleConflicts)},
		{"halted-parents", "Parent On Hold or Cancelled", len(v.HaltedParents)},
	}
}

// riskDefinition is one of the Risks page's signals: the kind its chips
// carry, its name, and what it means.
type riskDefinition struct{ Kind, Name, Means string }

// riskDefinitions are the Risks page's signals in the page's order, each with
// what it means: what a chip's button opens, and what "What each signal
// means" lists.
var riskDefinitions = []riskDefinition{
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

// riskDefinitionsIn are the definitions of the signals flagging rows, in the
// page's order, each once: the popovers the chips' buttons open.
func riskDefinitionsIn(rows []riskGoalRow) []riskDefinition {
	chipped := map[string]bool{}
	for _, r := range rows {
		for _, sig := range r.Signals {
			chipped[sig.Kind] = true
		}
	}
	var defs []riskDefinition
	for _, d := range riskDefinitions {
		if chipped[d.Kind] {
			defs = append(defs, d)
		}
	}
	return defs
}

// riskMeansID is the id of the popover saying what the signal of kind means.
func riskMeansID(kind string) string {
	return "risk-means-" + kind
}

// Flagged counts the Goals the Risks page lists in scope, each once however
// many signals flag it.
func (v risksView) Flagged(scope pageOrgScope) int {
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

// countDefaultRisks counts the Goals flagged in acc's default scope, as many
// as the rows a bare /risks lists, for the top bar.
func (s *Server) countDefaultRisks(ctx context.Context, acc domain.Account) (int, error) {
	v, err := s.loadRisks(ctx)
	if err != nil {
		return 0, err
	}
	scope, err := s.resolvePageOrgScope(ctx, url.Values{}, acc)
	if err != nil {
		return 0, err
	}
	return v.Flagged(scope), nil
}

// loadRisks reads every Goal the company's signals flag.
func (s *Server) loadRisks(ctx context.Context) (risksView, error) {
	fresh, err := s.svc.FreshnessSignals(ctx)
	if err != nil {
		return risksView{}, err
	}
	goals, err := s.svc.ListGoals(ctx)
	if err != nil {
		return risksView{}, err
	}
	graph, err := s.svc.GraphSignals(ctx)
	if err != nil {
		return risksView{}, err
	}
	v := risksView{
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
		return risksView{}, err
	}
	v.OverdueActionItems = riskOverdueGoals(goals, items, s.svc.Now().In(s.svc.Timezone()))
	if v.Asks, err = s.svc.OpenAsksForHelp(ctx); err != nil {
		return risksView{}, err
	}
	return v, nil
}

// riskOverdueGoals are the Active Goals among goals that an overdue Action
// Item among items is about, in goals' order, each with the days its most
// overdue item is past its due date as of today.
func riskOverdueGoals(goals []domain.Goal, items []domain.ActionItemListing, today time.Time) []riskOverdueGoal {
	days := map[int64]int{}
	for _, l := range items {
		if d := calendarDays(l.Item.DueDate, today); d > days[l.Item.GoalID] {
			days[l.Item.GoalID] = d
		}
	}
	var out []riskOverdueGoal
	for _, g := range goals {
		if d, ok := days[g.ID]; ok && g.Lifecycle == domain.LifecycleActive {
			out = append(out, riskOverdueGoal{Goal: g, Days: d})
		}
	}
	return out
}

// riskGoalRow is one flagged Goal on the Risks page with every signal that
// flags it, so a Goal flagged twice is one row, not two.
type riskGoalRow struct {
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
	Signals        []riskSignal
	// Fix is the one action the row offers its viewer.
	Fix riskFix
}

// riskFix is a row's one Fix button: what it says, where it goes, and whether
// it is the page's primary kind of action. Post makes it a form posting to
// Href rather than a link; Disabled shows it without letting it be used.
type riskFix struct {
	Label    string
	Href     string
	Primary  bool
	Post     bool
	Disabled bool
}

// riskFixFacts are what the rows' Fixes turn on beyond the rows themselves:
// the Goals the viewer is a Delegate on, the Goals nudged today with their
// Nudge, and the Ownerless Goals with no Delegate left to check in on them.
type riskFixFacts struct {
	delegate    map[int64]bool
	nudged      map[int64]domain.Nudge
	unreachable map[int64]bool
}

// fixRiskRows sets each row's Fix for current.
func fixRiskRows(current domain.Account, rows []riskGoalRow, facts riskFixFacts) {
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
func (r riskGoalRow) fix(current domain.Account, delegate map[int64]bool, nudged *domain.Nudge, unreachable bool) riskFix {
	goal := fmt.Sprintf("/goals/%d", r.Goal.ID)
	if _, ownerless := r.signal("ownerless"); ownerless && current.IsAdmin {
		return riskFix{Label: "Reassign", Href: goal + "?open=reassign"}
	}
	_, checkinOverdue := r.signal("checkin-overdue")
	_, pathOverdue := r.signal("path-overdue")
	_, milestoneOverdue := r.signal("milestone-overdue")
	overdue := pathOverdue || milestoneOverdue
	if (checkinOverdue || overdue) && (r.Goal.Owner.ID == current.ID || delegate[r.Goal.ID]) {
		return riskFix{Label: "Check in", Href: goal + "/checkin", Primary: true}
	}
	if (checkinOverdue || overdue) && nudged != nil {
		return riskFix{Label: "Nudged today by " + nudged.Sender.Label(), Disabled: true}
	}
	if (checkinOverdue || overdue) && !unreachable {
		return riskFix{Label: "Nudge", Href: goal + "/nudge", Post: true}
	}
	if _, unlinked := r.signal("unlinked"); unlinked && r.Goal.Owner.ID == current.ID {
		return riskFix{Label: "Link to a parent", Href: goal + "?open=parent-link"}
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
		return riskFix{Label: "Open parent", Href: fmt.Sprintf("/goals/%d", parent.ID)}
	}
	if _, unlinked := r.signal("unlinked"); unlinked {
		return riskFix{Label: "Suggest a parent", Href: goal + "/suggest-parent"}
	}
	if _, overdue := r.signal("action-item-overdue"); overdue {
		return riskFix{Label: "See Action Items", Href: goal + "#action-items"}
	}
	return riskFix{Label: "Open Goal", Href: goal}
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
func (r riskGoalRow) scheduleConflictFix(current domain.Account, delegate map[int64]bool, nudged *domain.Nudge, unreachable bool) (riskFix, bool) {
	var parents []domain.Goal
	for _, sig := range r.Signals {
		if sig.Kind == "schedule-conflicts" {
			parents = append(parents, *sig.Related)
		}
	}
	if len(parents) == 0 {
		return riskFix{}, false
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
			return riskFix{Label: "Check in", Href: fmt.Sprintf("/goals/%d/checkin?open=dates", g.ID), Primary: true}, true
		}
		if mine == nil {
			mine = &g
		}
	}
	if mine != nil {
		return riskFix{Label: "Open Goal", Href: fmt.Sprintf("/goals/%d", mine.ID)}, true
	}
	if r.Goal.Lifecycle != domain.LifecycleActive {
		return riskFix{}, false
	}
	if nudged != nil {
		return riskFix{Label: "Nudged today by " + nudged.Sender.Label(), Disabled: true}, true
	}
	if !unreachable {
		return riskFix{Label: "Nudge", Href: fmt.Sprintf("/goals/%d/nudge", r.Goal.ID), Post: true}, true
	}
	return riskFix{}, false
}

// riskSignal is one reason a Goal is flagged. Kind names the signal, as the
// chip's data-kind on the Risks page. Days is how long the signal has held: for
// "checkin-overdue" the days since the last update, for "path-overdue" the days
// past the Path to Green's target date, for "milestone-overdue" the days past
// the earliest overdue Milestone's date, for "action-item-overdue" the days the
// most overdue Action Item about the Goal is past its due date, for
// "schedule-conflicts" the days the child's delivery date falls after the
// parent's, and 0 otherwise. Related is the parent for "schedule-conflicts" and
// "halted-parents". Person is whom an "asks-for-help" asks, nil when no one is
// above the Owner.
type riskSignal struct {
	Kind    string
	Days    int
	Related *domain.Goal
	Person  *domain.Account
}

// riskRows merges v's lists into one row per flagged Goal. Unlike loadRisks,
// which every signed-in page runs for the top bar's count, it reads each
// flagged Goal's latest Check-in, so only the Risks page calls it.
func (s *Server) riskRows(ctx context.Context, v risksView) ([]riskGoalRow, error) {
	var rows []riskGoalRow
	at := map[int64]int{}
	add := func(g domain.Goal, sig riskSignal) {
		i, ok := at[g.ID]
		if !ok {
			i = len(rows)
			at[g.ID] = i
			rows = append(rows, riskGoalRow{Goal: g})
		}
		rows[i].Signals = append(rows[i].Signals, sig)
	}
	for _, gf := range v.NotGreen {
		add(gf.Goal, riskSignal{Kind: riskHealthKind(gf.Health)})
	}
	for _, ask := range v.Asks {
		add(ask.Goal, riskSignal{Kind: "asks-for-help", Person: ask.Addressee})
	}
	for _, gf := range v.CheckinOverdue {
		add(gf.Goal, riskSignal{Kind: "checkin-overdue", Days: gf.Freshness.DaysSince})
	}
	// A Path to Green's overdue days are counted in the Goal's timezone, the
	// one its flag was judged in.
	for _, gf := range v.OverduePaths {
		add(gf.Goal, riskSignal{Kind: "path-overdue", Days: calendarDays(gf.Freshness.PathTargetDate, s.svc.Now().In(s.svc.GoalTimezone(gf.Goal)))})
	}
	// So are a Milestone's.
	for _, gf := range v.OverdueMilestones {
		add(gf.Goal, riskSignal{Kind: "milestone-overdue", Days: calendarDays(gf.Freshness.OverdueMilestoneDate, s.svc.Now().In(s.svc.GoalTimezone(gf.Goal)))})
	}
	for _, og := range v.OverdueActionItems {
		add(og.Goal, riskSignal{Kind: "action-item-overdue", Days: og.Days})
	}
	for _, g := range v.Ownerless {
		add(g, riskSignal{Kind: "ownerless"})
	}
	for _, g := range v.Unlinked {
		add(g, riskSignal{Kind: "unlinked"})
	}
	for _, c := range v.ScheduleConflicts {
		add(c.Child, riskSignal{Kind: "schedule-conflicts", Days: calendarDays(c.Parent.DeliveryDate, c.Child.DeliveryDate), Related: &c.Parent})
	}
	for _, hp := range v.HaltedParents {
		add(hp.Child, riskSignal{Kind: "halted-parents", Related: &hp.Parent})
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
	sortRiskRows(rows)
	return rows, nil
}

// riskHealthKind is the signal kind a Not Green Goal's Health flags it with:
// "red" for Red, "yellow" for Yellow.
func riskHealthKind(health string) string {
	if health == domain.HealthRed {
		return "red"
	}
	return "yellow"
}

// signal is the row's first signal of kind, and whether it has one.
func (r riskGoalRow) signal(kind string) (riskSignal, bool) {
	for _, sig := range r.Signals {
		if sig.Kind == kind {
			return sig, true
		}
	}
	return riskSignal{}, false
}

// signalKinds counts the distinct kinds of signal that flag the row, so a Goal
// under two halted parents counts its halted parents once.
func (r riskGoalRow) signalKinds() int {
	seen := map[string]bool{}
	for _, sig := range r.Signals {
		seen[sig.Kind] = true
	}
	return len(seen)
}

// riskHealthRank orders Health most urgent first: Red, Yellow, none, then
// Green.
func riskHealthRank(health string) int {
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

// sortRiskRows puts the most urgent Goals first, key by key: Active Goals
// before the Proposed and On Hold ones, which nothing has gone wrong with yet;
// then Ownerless first; then by Health (riskHealthRank); then a Path to Green
// overdue first, longest overdue first; then a Milestone overdue first, longest
// overdue first; then an overdue Action Item first, longest overdue first; then
// Check-in overdue first, furthest past its cadence first; then more distinct
// kinds of signal first; then by title, ignoring case.
func sortRiskRows(rows []riskGoalRow) {
	slices.SortStableFunc(rows, func(a, b riskGoalRow) int {
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
		if c := cmp.Compare(riskHealthRank(a.Health), riskHealthRank(b.Health)); c != 0 {
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

// riskDays says a count of days in words: "1 day", "22 days".
func riskDays(n int) string {
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

// riskGroup is a set of signal kinds grouped by who acts on them: Key names
// it, Name says it, and Count is how many Goals it flags, each once however
// many of its signals flag the Goal.
type riskGroup struct {
	Key, Name string
	Count     int
}

// riskGroupKinds are the Risks page's groups in the page's order, with the
// signal kinds each holds.
// Blank is what a group's card says when it holds no Goals.
var riskGroupKinds = []struct {
	Key, Name string
	Kinds     []string
	Blank     string
}{
	{"health", "Not Green", []string{"red", "yellow", "asks-for-help"}, "No Goal is Red or Yellow."},
	{"owner", "Waiting on a Check-in", []string{"checkin-overdue", "path-overdue", "milestone-overdue"}, "Nothing is waiting on a Check-in."},
	{"followup", "Action Items overdue", []string{"action-item-overdue"}, "No Action Item is overdue."},
	{"plan", "Placement and dates", []string{"unlinked", "schedule-conflicts"}, "Every Goal is placed and its dates fit."},
	{riskGroupAdmin, "Needs an Admin", []string{"ownerless", "halted-parents"}, "Nothing needs an Admin."},
}

// riskGroupAdmin is the key of the Needs an Admin group, whose card and table
// hold the company's rows.
const riskGroupAdmin = "admin"

// riskGroups counts the rows in each group, in the page's order.
func riskGroups(rows []riskGoalRow) []riskGroup {
	groups := make([]riskGroup, 0, len(riskGroupKinds))
	for _, gk := range riskGroupKinds {
		g := riskGroup{Key: gk.Key, Name: gk.Name}
		for _, r := range rows {
			if slices.ContainsFunc(r.Signals, func(sig riskSignal) bool { return slices.Contains(gk.Kinds, sig.Kind) }) {
				g.Count++
			}
		}
		groups = append(groups, g)
	}
	return groups
}
