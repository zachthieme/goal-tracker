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
// the viewer's own Goals (?mine=1) and to the Goals with an offered
// Dimension's value (?value=). The scope narrows the rows only after they are
// built: loadRisks stays org-wide for the top bar's count.
func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	page, err := s.loadRisksPage(r.Context(), r.URL.Query(), current)
	if err != nil {
		http.Error(w, "could not read risks", http.StatusInternalServerError)
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
	page := risksPageView{
		risksView:  v,
		Group:      riskGroupKey(q.Get("group")),
		Mine:       q.Get("mine") == "1",
		Dimensions: domain.OfferedDimensions(dims),
		Today:      s.svc.Now().In(s.svc.Timezone()),
	}
	page.Value = riskValue(q.Get("value"), page.Dimensions)
	var valued map[int64]bool
	if page.Value.ID != 0 {
		goals, err := s.svc.ListGoalsWithValues(ctx)
		if err != nil {
			return risksPageView{}, err
		}
		valued = map[int64]bool{}
		for _, gv := range domain.FilterGoals(goals, map[int64][]int64{page.Value.DimensionID: {page.Value.ID}}) {
			valued[gv.Goal.ID] = true
		}
	}
	for _, row := range rows {
		if page.Mine && row.Goal.Owner.ID != current.ID && !facts.delegate[row.Goal.ID] {
			continue
		}
		if valued != nil && !valued[row.Goal.ID] {
			continue
		}
		page.Rows = append(page.Rows, row)
	}
	return page, nil
}

// riskValue is the offered Dimensions' value raw names by its ID, or the zero
// value when it names none of them.
func riskValue(raw string, dims []domain.Dimension) domain.DimensionValue {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return domain.DimensionValue{}
	}
	for _, d := range dims {
		for _, v := range d.Values {
			if v.ID == id {
				return v
			}
		}
	}
	return domain.DimensionValue{}
}

// riskValueLabel is how the value select names a value: a Retired one says so.
func riskValueLabel(v domain.DimensionValue) string {
	if v.Retired {
		return v.Value + " (retired)"
	}
	return v.Value
}

// risksPageView is the Risks page: its lists, its rows in scope, the group its
// address filters it to, "" for every group, whether it is scoped to the
// viewer's own Goals, the offered Dimensions and the value of theirs it is
// scoped to, if any, and today, which its dates are read against.
type risksPageView struct {
	risksView
	Rows       []riskGoalRow
	Group      string
	Mine       bool
	Dimensions []domain.Dimension
	Value      domain.DimensionValue
	Today      time.Time
}

// url is the Risks page's address filtered to group ("" for every group),
// scoped to mine, and keeping the page's value scope.
func (p risksPageView) url(group string, mine bool) string {
	q := url.Values{}
	if group != "" {
		q.Set("group", group)
	}
	if mine {
		q.Set("mine", "1")
	}
	if p.Value.ID != 0 {
		q.Set("value", strconv.FormatInt(p.Value.ID, 10))
	}
	if len(q) == 0 {
		return "/risks"
	}
	return "/risks?" + q.Encode()
}

// Here is the page's own address, with its filters and scope.
func (p risksPageView) Here() string {
	return p.url(p.Group, p.Mine)
}

// ShowAllURL is the page's address unfiltered, keeping its scope.
func (p risksPageView) ShowAllURL() templ.SafeURL {
	return templ.SafeURL(p.url("", p.Mine))
}

// ScopeURL is the page's address scoped to the viewer's own Goals or not,
// keeping its group.
func (p risksPageView) ScopeURL(mine bool) templ.SafeURL {
	return templ.SafeURL(p.url(p.Group, mine))
}

// ScopeAttrs marks the Everyone | Mine choice the page is scoped to as the
// current one.
func (p risksPageView) ScopeAttrs(mine bool) templ.Attributes {
	if p.Mine == mine {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// Shown are the rows the table shows: every row with no filter, else each row
// a signal of the filtered group flags, chipped with that group's signals
// only.
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
	var shown []riskGoalRow
	for _, r := range p.Rows {
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

// Filtered reports whether the page is scoped to Mine or a value, or filtered
// to a group.
func (p risksPageView) Filtered() bool {
	return p.Mine || p.Value.ID != 0 || p.Group != ""
}

// Empty is what the page says when no Goal is left to show: where nothing
// needs attention, naming Mine, the value with its Dimension's name, and the
// group's card's title, in that order.
func (p risksPageView) Empty() string {
	var parts []string
	if p.Mine {
		parts = append(parts, "Mine")
	}
	if p.Value.ID != 0 {
		for _, d := range p.Dimensions {
			if d.ID == p.Value.DimensionID {
				parts = append(parts, d.Name+" "+p.Value.Value)
			}
		}
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

// attention is the header's words after its count of the Goals on the page.
func (p risksPageView) attention() string {
	if len(p.Rows) == 1 {
		return "Goal needs attention. Worst first."
	}
	return "Goals need attention. Worst first."
}

// riskChipClass is the class of a signal's chip on a group card, matching the
// table's: the Stale badge for the freshness signals, the Ownerless badge for
// Ownerless, and the outlined tag for the structural ones.
func riskChipClass(kind string) string {
	switch kind {
	case "stale", "path-overdue":
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

// Cards are the page's group cards, in the page's order.
func (p risksPageView) Cards() []riskCard {
	names := map[string]string{}
	for _, rt := range p.Types() {
		names[rt.Anchor] = rt.Name
	}
	groups := riskGroups(p.Rows)
	cards := make([]riskCard, 0, len(groups))
	for i, gk := range riskGroupKinds {
		c := riskCard{riskGroup: groups[i], Blank: gk.Blank, Current: gk.Key == p.Group, Href: templ.SafeURL(p.url(gk.Key, p.Mine))}
		for _, kind := range gk.Kinds {
			n := 0
			for _, r := range p.Rows {
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
	Stale        []domain.GoalFreshness
	OverduePaths []domain.GoalFreshness
	// Ownerless are the Active Goals whose Owner has left the org.
	Ownerless []domain.Goal
	Unaligned []domain.Goal
	// ScheduleConflicts and HaltedParents are listed by their child, the Goal
	// that needs to change.
	ScheduleConflicts []domain.ScheduleConflict
	HaltedParents     []domain.HaltedParent
}

// riskType is one of the Risks page's signals, or its chip on a group card:
// its kind, its name, and how many Goals it flags.
type riskType struct {
	Anchor, Name string
	Count        int
}

// Types are the Risks page's signals in the page's order.
func (v risksView) Types() []riskType {
	return []riskType{
		{"stale", "Stale", len(v.Stale)},
		{"path-overdue", "Path to Green overdue", len(v.OverduePaths)},
		{"ownerless", "Ownerless", len(v.Ownerless)},
		{"unaligned", "Unaligned", len(v.Unaligned)},
		{"schedule-conflicts", "Schedule conflicts", len(v.ScheduleConflicts)},
		{"halted-parents", "Parent On Hold or Cancelled", len(v.HaltedParents)},
	}
}

// riskDefinitions are the Risks page's signals in the page's order, each with
// what it means, as the page's "What each risk means" says.
var riskDefinitions = []struct{ Name, Means string }{
	{"Stale", "Active Goals whose last Check-in, or activation if they have none, is older than their cadence."},
	{"Path to Green overdue", "Active Goals past their Path to Green's target date that still aren't Green."},
	{"Ownerless", "Active Goals whose Owner has left the org and hasn't been replaced."},
	{"Unaligned", "Active Goals that contribute to no other Goal and aren't Top-level."},
	{"Schedule conflicts", "Goals due later than a Goal they contribute to."},
	{"Parent On Hold or Cancelled", "Goals contributing to a Goal that is On Hold or Cancelled."},
}

// Flagged counts the Goals the Risks page lists, each once however many
// signals flag it.
func (v risksView) Flagged() int {
	flagged := map[int64]bool{}
	for _, list := range [][]domain.GoalFreshness{v.Stale, v.OverduePaths} {
		for _, gf := range list {
			flagged[gf.Goal.ID] = true
		}
	}
	for _, list := range [][]domain.Goal{v.Ownerless, v.Unaligned} {
		for _, g := range list {
			flagged[g.ID] = true
		}
	}
	for _, c := range v.ScheduleConflicts {
		flagged[c.Child.ID] = true
	}
	for _, hp := range v.HaltedParents {
		flagged[hp.Child.ID] = true
	}
	return len(flagged)
}

// loadRisks reads every Goal the org's signals flag.
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
		Stale:             fresh.Stale,
		OverduePaths:      fresh.OverduePaths,
		Unaligned:         graph.Unaligned,
		ScheduleConflicts: graph.ScheduleConflicts,
		HaltedParents:     graph.HaltedParents,
	}
	for _, g := range goals {
		if g.Ownerless && g.Lifecycle == domain.LifecycleActive {
			v.Ownerless = append(v.Ownerless, g)
		}
	}
	return v, nil
}

// riskGoalRow is one flagged Goal on the Risks page with every signal that
// flags it, so a Goal flagged twice is one row, not two.
type riskGoalRow struct {
	Goal domain.Goal
	// Health is the Goal's latest Check-in's Health, empty when it has none.
	Health  string
	Signals []riskSignal
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
		rows[i].Fix = rows[i].fix(current, facts.delegate[id], today, facts.unreachable[id])
	}
}

// fix is the row's Fix for current, given whether current is a Delegate on
// the Goal, its Nudge today, if any, and whether nobody is left to check in on
// it. It is the first that applies of:
//  1. Reassign, for an Ownerless Goal and an Admin;
//  2. Check in, for a Stale or Path to Green overdue Goal current Owns or is a
//     Delegate on (an Admin has no Check-in right);
//  3. Nudge, for someone else's Stale or Path to Green overdue Goal that
//     someone can check in on (CONTEXT.md: Nudge); once it has been nudged
//     today, a disabled "Nudged today by" whoever did. Freshness outranks
//     alignment, as 2 outranks 4;
//  4. Link to a parent, for an Unaligned Goal current Owns;
//  5. Compare dates, the Goal's own page, for a Schedule conflict;
//  6. Open parent, for a Goal under a halted parent: the first by title;
//  7. Suggest a parent, for an Unaligned Goal current doesn't Own, a Delegate
//     on it included (CONTEXT.md: Parent suggestion);
//  8. Open Goal, for anything else, a Stale Goal nobody can check in on among
//     them.
func (r riskGoalRow) fix(current domain.Account, delegate bool, nudged *domain.Nudge, unreachable bool) riskFix {
	goal := fmt.Sprintf("/goals/%d", r.Goal.ID)
	if _, ownerless := r.signal("ownerless"); ownerless && current.IsAdmin {
		return riskFix{Label: "Reassign", Href: goal + "?open=reassign"}
	}
	_, stale := r.signal("stale")
	_, overdue := r.signal("path-overdue")
	if (stale || overdue) && (r.Goal.Owner.ID == current.ID || delegate) {
		return riskFix{Label: "Check in", Href: goal + "/checkin", Primary: true}
	}
	if (stale || overdue) && nudged != nil {
		return riskFix{Label: "Nudged today by " + nudged.Sender.Label(), Disabled: true}
	}
	if (stale || overdue) && !unreachable {
		return riskFix{Label: "Nudge", Href: goal + "/nudge", Post: true}
	}
	if _, unaligned := r.signal("unaligned"); unaligned && r.Goal.Owner.ID == current.ID {
		return riskFix{Label: "Link to a parent", Href: goal + "?open=parent-link"}
	}
	if _, conflict := r.signal("schedule-conflicts"); conflict {
		return riskFix{Label: "Compare dates", Href: goal}
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
	if _, unaligned := r.signal("unaligned"); unaligned {
		return riskFix{Label: "Suggest a parent", Href: goal + "/suggest-parent"}
	}
	return riskFix{Label: "Open Goal", Href: goal}
}

// riskSignal is one reason a Goal is flagged. Kind names the signal, as the
// chip's data-kind on the Risks page. Days is how long the signal has held: for
// "stale" the days since the last update, for "path-overdue" the days past
// the Path to Green's target date, for "schedule-conflicts" the days the
// child's delivery date falls after the parent's, and 0 otherwise. Related is
// the parent for "schedule-conflicts" and "halted-parents".
type riskSignal struct {
	Kind    string
	Days    int
	Related *domain.Goal
}

// riskRows merges v's lists into one row per flagged Goal. Unlike loadRisks,
// which every signed-in page runs for the top bar's count, it reads each
// flagged Goal's latest Check-in, so only the Risks page calls it.
func (s *Server) riskRows(ctx context.Context, v risksView) ([]riskGoalRow, error) {
	today := s.svc.Now().In(s.svc.Timezone())
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
	for _, gf := range v.Stale {
		add(gf.Goal, riskSignal{Kind: "stale", Days: gf.Freshness.DaysSince})
	}
	for _, gf := range v.OverduePaths {
		add(gf.Goal, riskSignal{Kind: "path-overdue", Days: calendarDays(gf.Freshness.PathTargetDate, today)})
	}
	for _, g := range v.Ownerless {
		add(g, riskSignal{Kind: "ownerless"})
	}
	for _, g := range v.Unaligned {
		add(g, riskSignal{Kind: "unaligned"})
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
	}
	sortRiskRows(rows)
	return rows, nil
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

// riskHealthRank orders Health worst first: Red, Yellow, none, then Green.
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

// sortRiskRows puts the worst-off Goals first, key by key: Ownerless first;
// then by Health (riskHealthRank); then a Path to Green overdue first, longest
// overdue first; then Stale first, furthest past its cadence first; then by
// title, ignoring case. The structural signals don't move a row.
func sortRiskRows(rows []riskGoalRow) {
	slices.SortStableFunc(rows, func(a, b riskGoalRow) int {
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
		aStale, aIsStale := a.signal("stale")
		bStale, bIsStale := b.signal("stale")
		if c := cmp.Compare(boolRank(!aIsStale), boolRank(!bIsStale)); c != 0 {
			return c
		}
		if aIsStale && bIsStale {
			if c := cmp.Compare(bStale.Days-b.Goal.CadenceDays, aStale.Days-a.Goal.CadenceDays); c != 0 {
				return c
			}
		}
		return cmp.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title))
	})
}

// riskDate is a date as the Risks page says it, "Jan 2", with its year when
// that isn't today's.
func riskDate(t, today time.Time) string {
	if t.Year() != today.Year() {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2")
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
	{"owner", "Owner needs to update", []string{"stale", "path-overdue"}, "Every Owner is up to date."},
	{"plan", "Plan doesn't fit", []string{"unaligned", "schedule-conflicts"}, "Every plan fits."},
	{"admin", "Needs an Admin", []string{"ownerless", "halted-parents"}, "Nothing needs an Admin."},
}

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
