package web

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleRisks answers "what's going wrong?" for leadership: every Goal the
// freshness and graph signals flag, by problem type.
func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, err := s.loadRisks(r.Context())
	if err != nil {
		http.Error(w, "could not read risks", http.StatusInternalServerError)
		return
	}
	rows, err := s.riskRows(r.Context(), v)
	if err != nil {
		http.Error(w, "could not read risks", http.StatusInternalServerError)
		return
	}
	page := risksPageView{
		risksView: v,
		Rows:      rows,
		Group:     riskGroupKey(r.URL.Query().Get("group")),
		Today:     s.svc.Now().In(s.svc.Timezone()),
	}
	render(w, r, http.StatusOK, risksPage(&current, page))
}

// risksPageView is the Risks page: its lists, its rows, the group its address
// filters it to, "" for every group, and today, which its dates are read
// against.
type risksPageView struct {
	risksView
	Rows  []riskGoalRow
	Group string
	Today time.Time
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

// Blank is what the filtered group's card says when it holds no Goals, "" with
// no filter.
func (p risksPageView) Blank() string {
	for _, gk := range riskGroupKinds {
		if gk.Key == p.Group {
			return gk.Blank
		}
	}
	return ""
}

// attention is the header's words after its count of the Goals on the page.
func (p risksPageView) attention() string {
	if len(p.Rows) == 1 {
		return "Goal needs attention."
	}
	return "Goals need attention."
}

// riskChipClass is the badge style of a signal's chip: the Stale look for the
// freshness signals, the Ownerless look for Ownerless, and the Lifecycle look
// for the structural ones.
func riskChipClass(kind string) string {
	switch kind {
	case "stale", "path-overdue":
		return "st"
	case "ownerless":
		return "ol"
	}
	return "lc"
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
// signals that flags a Goal, the line it shows when it holds none, and
// whether the page is filtered to it.
type riskCard struct {
	riskGroup
	Signals []riskType
	Blank   string
	Current bool
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
		c := riskCard{riskGroup: groups[i], Blank: gk.Blank, Current: gk.Key == p.Group}
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
		if c := cmp.Compare(bStale.Days-b.Goal.CadenceDays, aStale.Days-a.Goal.CadenceDays); c != 0 {
			return c
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
