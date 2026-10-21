package web

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
)

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if r.URL.Query().Has("columns") {
		s.handleGoalTableColumns(w, r)
		return
	}
	view, err := s.goalsListView(r, current)
	if err != nil {
		s.serverError(w, r, "could not list goals", err)
		return
	}
	view.SwapClearOOB = r.Header.Get("HX-Request") == "true"
	render(w, r, http.StatusOK, goalsPage(&current, view))
}

// handleDownloadGoals downloads the Goals the list's filters keep, in the
// table's order, as CSV in the import format, so the file can be edited and
// imported again to update their Dimension values and Fields (#81). Hidden
// columns are a view setting, so the file has every column regardless. A
// download that would write a value the import can't read back is refused,
// naming it, and returns no file (#101).
func (s *Server) handleDownloadGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	view, err := s.goalsListView(r, current)
	if err != nil {
		s.serverError(w, r, "could not list goals", err)
		return
	}
	goals := make([]domain.Goal, 0, len(view.Rows))
	if view.Table != nil {
		for _, row := range view.Table.Rows {
			goals = append(goals, row.Goal)
		}
	} else {
		for _, row := range view.Rows {
			goals = append(goals, row.Goal)
		}
	}
	var buf bytes.Buffer
	if err := importer.New(s.svc).Download(r.Context(), &buf, goals); errors.Is(err, domain.ErrValidation) {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	} else if err != nil {
		s.serverError(w, r, "could not download goals", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="goals.csv"`)
	_, _ = w.Write(buf.Bytes())
}

// goalsListData is everything the Goal list renders: the Goals (filtered and
// sorted problems first), the Dimensions offered for filtering and grouping,
// which values are currently selected, and the Goals bucketed by Health or,
// when grouping, by a Dimension's values.
type goalsListData struct {
	Rows   []goalRow
	Filter goalFilter
	// Org is the Scope the list is scoped to (?org=), and the Orgs it
	// offers.
	Org        pageOrgScope
	Dimensions []domain.Dimension
	Selected   map[int64]bool
	GroupID    int64
	Groups     []goalRowGroup
	// HealthGroups is the list layout's default grouping, by Health, when no
	// Dimension groups it.
	HealthGroups []goalHealthGroup
	// Query is the request's query string, so the layout toggle and the table's
	// column headers link to the same view with one thing changed.
	Query url.Values
	// Table is the table layout (?layout=table), nil in the list layout.
	Table *goalTable
	// Dates is the clock the list shows its dates against.
	Dates displayDates
	// SwapClearOOB marks Clear's slot to be swapped out of band, for an htmx
	// filter change that swaps in only the list (#385).
	SwapClearOOB bool
}

// moreFiltersSet reports whether a Dimension filter is chosen, so More
// filters, where they're folded away, opens to show it. Group by sits in the
// filter bar's row of chips, so a grouping needs no opening.
func (v goalsListData) moreFiltersSet() bool {
	return len(v.Selected) > 0
}

// anyGoalFilterSet reports whether the filter bar differs from a bare /goals —
// a Scope other than the viewer's default, or any other filter or grouping —
// so Clear, which goes back to it, is offered (#376).
func (v goalsListData) anyGoalFilterSet() bool {
	return v.moreFiltersSet() || (v.GroupID != 0 && v.Table == nil) || v.Filter != goalFilter{} || !v.Org.isDefault()
}

// filtered reports whether any filter is narrowing the list, so an empty list
// says nothing matches rather than that there are no Goals.
func (v goalsListData) filtered() bool {
	return len(v.Selected) > 0 || v.Filter != goalFilter{} || v.Org.Name() != ""
}

// noMatch is what the list says when its filters leave no Goal, naming the
// Scope it is scoped to: "No Goals match.", "No Goals in My org.", "No Goals
// in Mine.", or "No Goals in My org match." under another filter too.
func (v goalsListData) noMatch() string {
	switch {
	case v.Org.Name() == "":
		return "No Goals match."
	case len(v.Selected) == 0 && v.Filter == goalFilter{}:
		return "No Goals in " + v.Org.Name() + "."
	}
	return "No Goals in " + v.Org.Name() + " match."
}

// countLabel is the Goal list's total, the muted count beside the page's
// "Goals" title, naming the Org it is scoped to: "3", "3 in My org".
func (v goalsListData) countLabel() string {
	n := strconv.Itoa(len(v.Rows))
	if v.Org.Of == nil {
		return n
	}
	return n + " in " + v.Org.Name()
}

// goalFilter is the Goal list's filter bar, read from the URL so a filtered
// list can be shared and reloaded.
type goalFilter struct {
	// Query keeps the Goals whose title or Owner's email or Name holds it, ignoring
	// case (?q=).
	Query string
	// Health keeps the Goals at one Health: "green", "yellow", "red", or "none"
	// for those with no Health yet (?health=). "" keeps every Health.
	Health string
	// Lifecycle keeps the Goals in one Lifecycle (?lifecycle=). "" keeps every
	// Lifecycle.
	Lifecycle string
	// Incomplete keeps the Incomplete Goals (?incomplete=1; CONTEXT.md:
	// Incomplete).
	Incomplete bool
}

// lifecycleFilters are the Lifecycle filter's choices, in the order a Goal
// moves through them.
var lifecycleFilters = []string{
	domain.LifecycleProposed,
	domain.LifecycleActive,
	domain.LifecycleOnHold,
	domain.LifecycleDone,
	domain.LifecycleCancelled,
}

// healthFilters are the Health filter's choices, each keyed by its URL value to
// the Health it keeps; "none" keeps the Goals with no Health.
var healthFilters = []struct{ Value, Health string }{
	{"red", domain.HealthRed},
	{"yellow", domain.HealthYellow},
	{"green", domain.HealthGreen},
	{"none", ""},
}

// readGoalFilter reads the filter bar's fields, all but the Scope, from the
// query string.
func readGoalFilter(q url.Values) goalFilter {
	return goalFilter{
		Query:      strings.TrimSpace(q.Get("q")),
		Health:     q.Get("health"),
		Lifecycle:  q.Get("lifecycle"),
		Incomplete: q.Get("incomplete") == "1",
	}
}

// keeps reports whether row passes the filter.
func (f goalFilter) keeps(row goalRow) bool {
	if f.Query != "" {
		q := strings.ToLower(f.Query)
		owner := row.Goal.Owner
		if !strings.Contains(strings.ToLower(row.Goal.Title), q) && !strings.Contains(strings.ToLower(owner.Email), q) &&
			!strings.Contains(strings.ToLower(owner.Name), q) {
			return false
		}
	}
	if f.Incomplete && len(row.Incomplete) == 0 {
		return false
	}
	if f.Lifecycle != "" && row.Goal.Lifecycle != f.Lifecycle {
		return false
	}
	for _, hf := range healthFilters {
		if f.Health == hf.Value && row.health() != hf.Health {
			return false
		}
	}
	return true
}

// goalRow is one Goal in the Goal list with what its row shows beyond the Goal
// and its Dimension values: its latest Check-in, which carries its Health,
// whether it is Check-in overdue or its Path to Green is overdue, and whether
// it is Incomplete.
type goalRow struct {
	domain.GoalWithValues
	// Latest is the Goal's most recent Check-in, nil when it has none.
	Latest    *domain.Checkin
	Freshness domain.Freshness
	// Incomplete names the required Dimensions and Fields an Active Goal has
	// no value in, empty when it isn't Incomplete (CONTEXT.md: Incomplete).
	Incomplete []string
	// PriorDates are the delivery dates the Goal's Date Slips moved it from,
	// earliest first, shown struck through ahead of its current date.
	PriorDates []time.Time
}

// goalRowGroup is one bucket of the grouped Goal list: the rows sharing Value,
// or those with no value in the grouping Dimension when Value is nil.
type goalRowGroup struct {
	Value *domain.DimensionValue
	Rows  []goalRow
}

// goalHealthGroup is one Health's rows in the Goal list's default grouping:
// the Goals at Health, or those with no Health when it is "", in their sorted
// order.
type goalHealthGroup struct {
	Health string
	Rows   []goalRow
}

// name is the group's header: its Health, or "No Health".
func (g goalHealthGroup) name() string {
	if g.Health == "" {
		return "No Health"
	}
	return g.Health
}

// groupGoalRowsByHealth buckets rows by Health — Red, Yellow, Green, then no
// Health, the Health filter's order — keeping their sorted order in each
// bucket and leaving out a Health no row has. When a Goal with no Health needs
// a decision or an update (Ownerless, Check-in overdue or Milestone overdue:
// problemRank 1 or 2), the whole No Health group leads instead, so that Goal is
// surfaced as prominently as Red (CONTEXT.md).
func groupGoalRowsByHealth(rows []goalRow) []goalHealthGroup {
	var groups []goalHealthGroup
	for _, hf := range healthFilters {
		grp := goalHealthGroup{Health: hf.Health}
		for _, row := range rows {
			if row.health() == hf.Health {
				grp.Rows = append(grp.Rows, row)
			}
		}
		if len(grp.Rows) > 0 {
			groups = append(groups, grp)
		}
	}
	// The No Health group comes last; it moves to the front when it holds a
	// Goal with a Check-in or Milestone overdue, or no Owner (problemRank 1 or 2).
	if n := len(groups); n > 1 && groups[n-1].Health == "" && slices.ContainsFunc(groups[n-1].Rows, func(r goalRow) bool {
		rank := r.problemRank()
		return rank == 1 || rank == 2
	}) {
		groups = slices.Concat(groups[n-1:], groups[:n-1])
	}
	return groups
}

// healthMarkClass is the class of the shape a list shows for health: the
// Health's own, or "none" for no Health.
func healthMarkClass(health string) string {
	if health == "" {
		return "none"
	}
	return healthClass(health)
}

// health is the Goal's current Health — its latest Check-in's — or "" when it
// has none, as a Proposed Goal or one a Check-in moved out of Active.
func (r goalRow) health() string {
	if r.Latest == nil {
		return ""
	}
	return r.Latest.Health
}

// lastCheckin says how long ago the Goal's latest Check-in was, counted on the
// company's calendar, or a dash when it has none. With a Check-in, the Goal's last
// update is that Check-in, so its freshness has already counted the days.
func (r goalRow) lastCheckin() string {
	if r.Latest == nil {
		return "—"
	}
	return daysAgo(r.Freshness.DaysSince)
}

// problemRank orders the Goal list so Goals that need a decision or update
// come first: Red, then Ownerless, then Check-in overdue, Path to Green overdue
// or Milestone overdue, then Yellow, then Green, then no Health.
func (r goalRow) problemRank() int {
	switch {
	case r.health() == domain.HealthRed:
		return 0
	case r.Goal.Ownerless:
		return 1
	case r.Freshness.CheckinOverdue || r.Freshness.PathToGreenOverdue || r.Freshness.MilestoneOverdue:
		return 2
	case r.health() == domain.HealthYellow:
		return 3
	case r.health() == domain.HealthGreen:
		return 4
	}
	return 5
}

// sortGoalRows puts problems first (problemRank), alphabetical by title within
// each rank.
func sortGoalRows(rows []goalRow) {
	slices.SortStableFunc(rows, func(a, b goalRow) int {
		if c := cmp.Compare(a.problemRank(), b.problemRank()); c != 0 {
			return c
		}
		return cmp.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title))
	})
}

// goalsListView reads the Goal list's filters (the filter bar's fields and
// ?value=) and grouping (?group=) from the request, then loads, filters, sorts,
// and (optionally) groups the Goals. Only the Dimensions still offered filter
// and group: a ?value= or ?group= on a Retired one is ignored, since the list
// has no control to undo it (CONTEXT.md: Retired).
func (s *Server) goalsListView(r *http.Request, current domain.Account) (goalsListData, error) {
	ctx := r.Context()
	all, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return goalsListData{}, err
	}
	dims := domain.OfferedDimensions(all)
	goals, err := s.svc.ListGoalsWithValues(ctx)
	if err != nil {
		return goalsListData{}, err
	}

	// Resolve each ?value= to its Dimension, so filtering ORs within a Dimension
	// and ANDs across them.
	dimOfValue := map[int64]int64{}
	for _, d := range dims {
		for _, v := range d.Values {
			dimOfValue[v.ID] = d.ID
		}
	}
	selected := map[int64]bool{}
	byDimension := map[int64][]int64{}
	for _, raw := range r.URL.Query()["value"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			continue
		}
		if dimID, ok := dimOfValue[id]; ok {
			selected[id] = true
			byDimension[dimID] = append(byDimension[dimID], id)
		}
	}
	org, err := s.loadPageOrgScope(ctx, r.URL.Query(), current, goalsOfValues(goals))
	if err != nil {
		return goalsListData{}, err
	}
	goals = domain.FilterGoals(goals, byDimension)
	filter := readGoalFilter(r.URL.Query())

	goals = slices.DeleteFunc(goals, func(gv domain.GoalWithValues) bool { return !org.keeps(gv.Goal) })
	rows, err := s.loadGoalRows(ctx, goals)
	if err != nil {
		return goalsListData{}, err
	}
	rows = slices.DeleteFunc(rows, func(row goalRow) bool { return !filter.keeps(row) })
	sortGoalRows(rows)

	view := goalsListData{
		Rows:       rows,
		Filter:     filter,
		Org:        org,
		Dimensions: dims,
		Selected:   selected,
		Query:      r.URL.Query(),
		Dates:      s.displayDates(),
	}
	if view.Query.Get("layout") == layoutTable {
		table, err := s.goalTableView(ctx, rows, dims, view.Query, hiddenColumns(r), current)
		if err != nil {
			return goalsListData{}, err
		}
		view.Table = table
		return view, nil
	}
	if raw := r.URL.Query().Get("group"); raw != "" {
		if groupID, err := strconv.ParseInt(raw, 10, 64); err == nil {
			for _, d := range dims {
				if d.ID == groupID {
					view.GroupID = groupID
					view.Groups = groupGoalRows(rows, d)
				}
			}
		}
	}
	if view.GroupID == 0 {
		view.HealthGroups = groupGoalRowsByHealth(rows)
	}
	return view, nil
}

// goalsOfValues is the Goals of gvs, in order.
func goalsOfValues(gvs []domain.GoalWithValues) []domain.Goal {
	goals := make([]domain.Goal, 0, len(gvs))
	for _, gv := range gvs {
		goals = append(goals, gv.Goal)
	}
	return goals
}

// pageOrgScope is the Scope of a page of Goals (?org=): Everyone, Mine, or
// an Org, by each Goal's Owner and never its Delegates, and the Orgs the
// page offers to scope to (CONTEXT.md: Org). An Org adds no access, only a
// way to find Goals, so anyone may scope to anyone's (ADR 0002, ADR 0008).
// An address naming no scope opens on the viewer's default: My org for
// someone with anyone under them, Mine for everyone else. The Goals pages and
// Follow-ups share it.
type pageOrgScope struct {
	// Of is the person whose Org the page is scoped to, nil when it isn't.
	Of *domain.Account
	// in holds the Account IDs in Of's org.
	in map[int64]bool
	// Mine is whether the page is scoped to the Goals the viewer Owns or is
	// a Delegate on, delegated holding the latter's IDs. It is false when
	// the page is scoped to an Org.
	Mine      bool
	delegated map[int64]bool
	// Default is whether the address named no scope, or one naming no one,
	// so the page is on the viewer's default scope.
	Default bool
	// Viewer is who is looking, so their own Org reads as My org.
	Viewer domain.Account
	// OffersMine is whether the viewer has anyone under them, so My org is
	// offered; with no one, it would be Mine without the Delegate Goals.
	OffersMine bool
	// managed is whether anyone has a Manager other than themselves
	// (Service.AnyoneHasManager), so the page offers Orgs at all.
	managed bool
	// People are the others whose Org the page offers, by Label: every Owner
	// of a Goal and everyone above them, whose Orgs are the ones holding
	// Goals. It is empty when no one has a Manager other than themselves,
	// whoever is looking.
	People []domain.Account
}

// loadPageOrgScope reads the Scope of the address q into the scope current
// sees over goals, with the Orgs it offers. Whether the page offers Orgs is
// the company's answer, the same for every viewer: whether anyone has a
// Manager other than themselves.
func (s *Server) loadPageOrgScope(ctx context.Context, q url.Values, current domain.Account, goals []domain.Goal) (pageOrgScope, error) {
	scope, err := s.resolvePageOrgScope(ctx, q, current)
	if err != nil || !scope.managed {
		return scope, err
	}
	// Each Owner, then their Manager and on up, stopping at anyone reached: the
	// directory's cycles are stored as it gives them (ADR 0008).
	reached := map[int64]bool{}
	for _, g := range goals {
		for a := g.Owner; !reached[a.ID]; {
			reached[a.ID] = true
			scope.People = append(scope.People, a)
			if a.ManagerID == nil {
				break
			}
			if a, err = s.svc.Account(ctx, *a.ManagerID); err != nil {
				return pageOrgScope{}, err
			}
		}
	}
	scope.People = slices.DeleteFunc(scope.People, func(a domain.Account) bool { return a.ID == current.ID })
	slices.SortFunc(scope.People, func(a, b domain.Account) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Label()), strings.ToLower(b.Label())), cmp.Compare(a.Email, b.Email))
	})
	return scope, nil
}

// resolvePageOrgScope reads the Scope of the address q for current, without
// the Orgs it offers, from ?org= alone. An Account ID that names someone
// scopes to their Org. Otherwise ?org=all is Everyone, and ?org=mine is Mine.
// Anything else, no ?org= or one that is malformed or names no one, is the
// viewer's default (Default): My org when they have anyone under them, else
// Mine. When no one has a Manager, any ID names no one: the page then offers
// no Org, so has no control to undo it.
func (s *Server) resolvePageOrgScope(ctx context.Context, q url.Values, current domain.Account) (pageOrgScope, error) {
	scope := pageOrgScope{Viewer: current}
	var err error
	if scope.managed, err = s.svc.AnyoneHasManager(ctx); err != nil {
		return pageOrgScope{}, err
	}
	if scope.managed {
		if scope.OffersMine, err = s.svc.HasAnyoneUnder(ctx, current.ID); err != nil {
			return pageOrgScope{}, err
		}
	}
	raw := q.Get("org")
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && scope.managed {
		if ok, err := scope.scopeToOrg(ctx, s.svc, id); err != nil || ok {
			return scope, err
		}
	}
	switch {
	case raw == scopeEveryoneValue:
		return scope, nil
	case raw == scopeMineValue:
	case scope.OffersMine:
		scope.Default = true
		_, err := scope.scopeToOrg(ctx, s.svc, current.ID)
		return scope, err
	default:
		scope.Default = true
	}
	scope.Mine = true
	delegated, err := s.svc.DelegatedGoals(ctx, current.ID)
	if err != nil {
		return pageOrgScope{}, fmt.Errorf("load delegated goals: %w", err)
	}
	scope.delegated = map[int64]bool{}
	for _, g := range delegated {
		scope.delegated[g.ID] = true
	}
	return scope, nil
}

// scopeToOrg scopes c to the Org of the person id names, reporting whether
// it names anyone.
func (c *pageOrgScope) scopeToOrg(ctx context.Context, svc *domain.Service, id int64) (bool, error) {
	org, err := svc.Org(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	c.Of = &org[0]
	c.in = map[int64]bool{}
	for _, a := range org {
		c.in[a.ID] = true
	}
	return true, nil
}

// isDefault reports whether c is the viewer's default scope, whether the
// address named no scope or named the same one: My org when they have anyone
// under them, else Mine.
func (c pageOrgScope) isDefault() bool {
	if c.Default {
		return true
	}
	if c.OffersMine {
		return c.Of != nil && c.Of.ID == c.Viewer.ID
	}
	return c.Mine
}

// scopeMineValue is the ?org= scoping a page of Goals to Mine.
const scopeMineValue = "mine"

// scopeEveryoneValue is the ?org= scoping a page of Goals to Everyone.
const scopeEveryoneValue = "all"

// keeps reports whether g is in the scope: its Owner is in the scope's Org,
// the viewer Owns it or is a Delegate on it for Mine, or the scope is
// Everyone. An Ownerless Goal's Departed Owner keeps their Manager, so the
// Goal stays in the Org its Owner was in.
func (c pageOrgScope) keeps(g domain.Goal) bool {
	switch {
	case c.Of != nil:
		return c.in[g.Owner.ID]
	case c.Mine:
		return g.Owner.ID == c.Viewer.ID || c.delegated[g.ID]
	}
	return true
}

// Offered reports whether the page offers any Org to scope to: not when no
// one has a Manager.
func (c pageOrgScope) Offered() bool {
	return c.OffersMine || len(c.People) > 0
}

// Others are the people the picker offers beside My Org: People, and
// whoever the page is scoped to if a link named someone else.
func (c pageOrgScope) Others() []domain.Account {
	if c.Of == nil || c.Of.ID == c.Viewer.ID || slices.ContainsFunc(c.People, func(a domain.Account) bool { return a.ID == c.Of.ID }) {
		return c.People
	}
	return append([]domain.Account{*c.Of}, c.People...)
}

// Value is the ?org= the scope keeps in its page's address: "all" for
// Everyone, "mine" for Mine, the ID of the person whose Org it is, or "" for
// the viewer's default, which a bare address opens on.
func (c pageOrgScope) Value() string {
	switch {
	case c.Default:
		return ""
	case c.Of != nil:
		return strconv.FormatInt(c.Of.ID, 10)
	case c.Mine:
		return scopeMineValue
	}
	return scopeEveryoneValue
}

// Name is the scope by whose Goals it holds: "Mine", "My org" for the
// viewer's own Org, "Priya Raman's org" for anyone else's, or "" for
// Everyone.
func (c pageOrgScope) Name() string {
	if c.Mine {
		return "Mine"
	}
	if c.Of == nil {
		return ""
	}
	if c.Of.ID == c.Viewer.ID {
		return "My org"
	}
	return c.Of.Label() + "'s org"
}

// loadGoalRows reads what each of goals' rows shows beyond the Goal itself,
// in the same few queries however many Goals there are, keeping their order.
func (s *Server) loadGoalRows(ctx context.Context, goals []domain.GoalWithValues) ([]goalRow, error) {
	ids := make([]int64, 0, len(goals))
	plain := make([]domain.Goal, 0, len(goals))
	for _, gv := range goals {
		ids = append(ids, gv.Goal.ID)
		plain = append(plain, gv.Goal)
	}
	latest, err := s.svc.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load latest check-ins: %w", err)
	}
	freshness, err := s.svc.FreshnessByGoal(ctx, plain)
	if err != nil {
		return nil, fmt.Errorf("read freshness: %w", err)
	}
	incomplete, err := s.svc.IncompleteByGoal(ctx, plain)
	if err != nil {
		return nil, fmt.Errorf("read incomplete: %w", err)
	}
	slips, err := s.svc.DateSlipsByGoal(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load date slips: %w", err)
	}
	rows := make([]goalRow, 0, len(goals))
	for _, gv := range goals {
		row := goalRow{GoalWithValues: gv, Freshness: freshness[gv.Goal.ID], Incomplete: incomplete[gv.Goal.ID]}
		if c, ok := latest[gv.Goal.ID]; ok {
			row.Latest = &c
		}
		for _, slip := range slips[gv.Goal.ID] {
			if slip.OnDeliveryDate() {
				row.PriorDates = append(row.PriorDates, slip.OldDate)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// groupGoalRows buckets rows under dim's values the way
// domain.GroupGoalsByDimension does, keeping their sorted order in each bucket.
func groupGoalRows(rows []goalRow, dim domain.Dimension) []goalRowGroup {
	goals := make([]domain.GoalWithValues, 0, len(rows))
	byID := make(map[int64]goalRow, len(rows))
	for _, row := range rows {
		goals = append(goals, row.GoalWithValues)
		byID[row.Goal.ID] = row
	}
	var groups []goalRowGroup
	for _, g := range domain.GroupGoalsByDimension(goals, dim) {
		grp := goalRowGroup{Value: g.Value}
		for _, gv := range g.Goals {
			grp.Rows = append(grp.Rows, byID[gv.Goal.ID])
		}
		groups = append(groups, grp)
	}
	return groups
}

// layoutTable is the ?layout= value that shows the Goal list as a flat table,
// one row per Goal and a column per Dimension and Field.
const layoutTable = "table"

// queryURL links to the Goal list with this view's query string changed by
// edit, so a link changes one thing and keeps every filter.
func (v goalsListData) queryURL(edit func(url.Values)) templ.SafeURL {
	q := url.Values{}
	for k, vs := range v.Query {
		q[k] = slices.Clone(vs)
	}
	edit(q)
	if len(q) == 0 {
		return "/goals"
	}
	return templ.SafeURL("/goals?" + q.Encode())
}

// everyoneURL links to the Goal list scoped to Everyone, keeping every other
// filter.
func (v goalsListData) everyoneURL() templ.SafeURL {
	return v.queryURL(func(q url.Values) {
		q.Set("org", scopeEveryoneValue)
	})
}

// downloadURL downloads the Goals this view's filters keep as CSV.
func (v goalsListData) downloadURL() templ.SafeURL {
	if len(v.Query) == 0 {
		return "/goals/download"
	}
	return templ.SafeURL("/goals/download?" + v.Query.Encode())
}

// layoutURL links to this view in the list layout ("") or the table layout,
// keeping every filter.
func (v goalsListData) layoutURL(layout string) templ.SafeURL {
	return v.queryURL(func(q url.Values) {
		if layout == "" {
			q.Del("layout")
			return
		}
		q.Set("layout", layout)
	})
}
