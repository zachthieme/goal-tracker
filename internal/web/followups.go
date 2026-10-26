package web

import (
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
// Dimension's values (?value=), any of a Dimension's and every Dimension's
// with one, as on the Goal list (domain.GoalScope). The scope narrows the rows
// only after Service.FollowUps has read them: its rows are the company's, for
// Needs an Admin.
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
	f, err := s.svc.FollowUps(ctx, current.ID)
	if err != nil {
		return followUpsPageView{}, err
	}
	rows := make([]followUpGoalRow, 0, len(f.Rows))
	for _, r := range f.Rows {
		row := followUpGoalRow{FollowUpRow: r}
		row.Fix = row.fix(current, f.Delegated)
		rows = append(rows, row)
	}
	dims, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return followUpsPageView{}, err
	}
	group := followUpGroupKey(q.Get("group"))
	// The scope is resolved over the loader's Accounts and Delegate Goals,
	// not read again.
	org := pageOrgScope{domain.GoalScopeAmong(current, readGoalScopeRequest(q), dims, f.Goals, f.Accounts, f.Delegated)}
	page := followUpsPageView{
		Group:      group,
		Org:        org,
		Dimensions: domain.OfferedDimensions(dims),
		Today:      s.svc.Now().In(s.svc.Timezone()),
	}
	// Rows hold no values, so with values to narrow to they're read again.
	var values map[int64]domain.GoalWithValues
	if len(org.Values) > 0 {
		goals, err := s.svc.ListGoalsWithValues(ctx)
		if err != nil {
			return followUpsPageView{}, err
		}
		values = make(map[int64]domain.GoalWithValues, len(goals))
		for _, gv := range goals {
			values[gv.Goal.ID] = gv
		}
	}
	for _, row := range rows {
		if values != nil && !org.HasValues(values[row.Goal.ID]) {
			continue
		}
		page.CompanyRows = append(page.CompanyRows, row)
		if org.InScope(row.Goal) {
			page.Rows = append(page.Rows, row)
		}
	}
	return page, nil
}

// followUpValueLabel is how the value select names a value: a Retired one says so.
func followUpValueLabel(v domain.DimensionValue) string {
	if v.Retired {
		return v.Value + " (retired)"
	}
	return v.Value
}

// followUpsPageView is the Follow-ups page: its rows in scope, its rows
// with its values in any scope, which Needs an Admin's card and table hold, the
// group its address filters it to, "" for every group, its scope, with the
// values it narrows to, the offered Dimensions, and today, which its dates are
// read against.
type followUpsPageView struct {
	Rows        []followUpGoalRow
	CompanyRows []followUpGoalRow
	Group       string
	Org         pageOrgScope
	Dimensions  []domain.Dimension
	Today       time.Time
}

// valueIn is the first value of the Dimension dimID the page is scoped to,
// the one its select shows, or the zero value when it is scoped to none of
// that Dimension's. A second reaches the page only by its address, as from
// the Goal list, and narrows it all the same.
func (p followUpsPageView) valueIn(dimID int64) domain.DimensionValue {
	for _, v := range p.Org.Values {
		if v.DimensionID == dimID {
			return v
		}
	}
	return domain.DimensionValue{}
}

// url is the Follow-ups page's address filtered to group ("" for every group),
// keeping the page's scope and every value it narrows to.
func (p followUpsPageView) url(group string) string {
	q := url.Values{}
	if sc := p.Org.Value(); sc != "" {
		q.Set("org", sc)
	}
	if group != "" {
		q.Set("group", group)
	}
	for _, v := range p.Org.Values {
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
		var signals []domain.FollowUpSignal
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
	return p.Org.Name() != "" || len(p.Org.Values) > 0 || p.Group != ""
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
// needs attention, naming Mine or the Org, each value it narrows to with its
// Dimension's name, in the Dimensions' order, and the group's card's title, in
// that order.
// Needs an Admin, company-wide, names only the values: "Nothing needs an
// Admin in Team Platform."
func (p followUpsPageView) Empty() string {
	var parts []string
	if name := p.Org.Name(); name != "" && p.Group != followUpGroupAdmin {
		parts = append(parts, name)
	}
	for _, v := range p.Org.Values {
		for _, d := range p.Dimensions {
			if d.ID == v.DimensionID {
				parts = append(parts, d.Name+" "+v.Value)
			}
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
				if _, ok := r.Signal(kind); ok {
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

// followUpGoalRow is one flagged Goal on the Follow-ups page, as
// Service.FollowUps reads it, with its Fix.
type followUpGoalRow struct {
	domain.FollowUpRow
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

// fix is the row's Fix for current, given the Goals current is a Delegate on,
// by the row's Goal's Nudge today, if any, and whether nobody is left to check
// in on it. It is the first that applies of:
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
func (r followUpGoalRow) fix(current domain.Account, delegate map[int64]bool) followUpFix {
	nudged, unreachable := r.NudgedToday, r.Unreachable
	goal := fmt.Sprintf("/goals/%d", r.Goal.ID)
	if _, ownerless := r.Signal("ownerless"); ownerless && current.IsAdmin {
		return followUpFix{Label: "Reassign", Href: goal + "?open=reassign"}
	}
	_, checkinOverdue := r.Signal("checkin-overdue")
	_, pathOverdue := r.Signal("path-overdue")
	_, milestoneOverdue := r.Signal("milestone-overdue")
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
	if _, unlinked := r.Signal("unlinked"); unlinked && r.Goal.Owner.ID == current.ID {
		return followUpFix{Label: "Link to a parent", Href: goal + "?open=parent-link"}
	}
	if f, ok := r.scheduleConflictFix(current, delegate); ok {
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
	if _, unlinked := r.Signal("unlinked"); unlinked {
		return followUpFix{Label: "Suggest a parent", Href: goal + "/suggest-parent"}
	}
	if _, overdue := r.Signal("action-item-overdue"); overdue {
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
func (r followUpGoalRow) scheduleConflictFix(current domain.Account, delegate map[int64]bool) (followUpFix, bool) {
	nudged, unreachable := r.NudgedToday, r.Unreachable
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

// followUpDays says a count of days in words: "1 day", "22 days".
func followUpDays(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
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
			if slices.ContainsFunc(r.Signals, func(sig domain.FollowUpSignal) bool { return slices.Contains(gk.Kinds, sig.Kind) }) {
				g.Count++
			}
		}
		groups = append(groups, g)
	}
	return groups
}
