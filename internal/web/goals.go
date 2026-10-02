package web

import (
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

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// dateLayout is the format the HTML date input (<input type="date">) submits and
// that the domain's calendar dates round-trip through.
const dateLayout = "2006-01-02"

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	view, err := s.goalsListView(r, current)
	if err != nil {
		http.Error(w, "could not list goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalsPage(&current, view))
}

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.CreateGoal(r.Context(), domain.CreateGoalInput{
		Title:   r.FormValue("title"),
		SoWhat:  r.FormValue("so_what"),
		OwnerID: current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}

	// htmx swaps the Goal list in place; a plain form post reloads the page. The
	// swap re-renders the default unfiltered list — the propose form posts to
	// /goals with no query, so there is no filter or grouping to preserve.
	if r.Header.Get("HX-Request") == "true" {
		view, err := s.goalsListView(r, current)
		if err != nil {
			http.Error(w, "could not list goals", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusOK, goalList(view))
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

// goalsListData is everything the Goal list renders: the Goals (filtered and
// sorted problems first), the Dimensions offered for filtering and grouping,
// which values are currently selected, and — when grouping — the Goals bucketed
// by a Dimension's values.
type goalsListData struct {
	Rows       []goalRow
	Filter     goalFilter
	Dimensions []domain.Dimension
	Selected   map[int64]bool
	GroupID    int64
	Groups     []goalRowGroup
}

// moreFiltersSet reports whether a Dimension filter or grouping is chosen, so
// More filters, where they're folded away, opens to show it.
func (v goalsListData) moreFiltersSet() bool {
	return len(v.Selected) > 0 || v.GroupID != 0
}

// goalCount is the Goal list's total: "1 Goal", "3 Goals".
func goalCount(n int) string {
	if n == 1 {
		return "1 Goal"
	}
	return fmt.Sprintf("%d Goals", n)
}

// filtered reports whether any filter is narrowing the list, so an empty list
// says nothing matches rather than that there are no Goals.
func (v goalsListData) filtered() bool {
	return len(v.Selected) > 0 || v.Filter != goalFilter{}
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
	// Mine keeps the Goals the viewer Owns or is a Delegate on (?mine=1).
	Mine bool
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

// readGoalFilter reads the filter bar's fields from the query string.
func readGoalFilter(q url.Values) goalFilter {
	return goalFilter{
		Query:     strings.TrimSpace(q.Get("q")),
		Health:    q.Get("health"),
		Lifecycle: q.Get("lifecycle"),
		Mine:      q.Get("mine") == "1",
	}
}

// keeps reports whether row passes the filter. mine reports whether the viewer
// Owns the row's Goal or is a Delegate on it.
func (f goalFilter) keeps(row goalRow, mine func(domain.Goal) bool) bool {
	if f.Mine && !mine(row.Goal) {
		return false
	}
	if f.Query != "" {
		q := strings.ToLower(f.Query)
		owner := row.Goal.Owner
		if !strings.Contains(strings.ToLower(row.Goal.Title), q) && !strings.Contains(strings.ToLower(owner.Email), q) &&
			!strings.Contains(strings.ToLower(owner.Name), q) {
			return false
		}
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
// and its Dimension values: its latest Check-in, which carries its Health, and
// whether it is Stale or its Path to Green is overdue.
type goalRow struct {
	domain.GoalWithValues
	// Latest is the Goal's most recent Check-in, nil when it has none.
	Latest    *domain.Checkin
	Freshness domain.Freshness
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

// health is the Goal's current Health — its latest Check-in's — or "" when it
// has none, as a Proposed Goal or one a Check-in moved out of Active.
func (r goalRow) health() string {
	if r.Latest == nil {
		return ""
	}
	return r.Latest.Health
}

// lastCheckin says how long ago the Goal's latest Check-in was, counted on the
// org's calendar, or a dash when it has none. With a Check-in, the Goal's last
// update is that Check-in, so its freshness has already counted the days.
func (r goalRow) lastCheckin() string {
	if r.Latest == nil {
		return "—"
	}
	return daysAgo(r.Freshness.DaysSince)
}

// problemRank orders the Goal list so problems sort to the top: Red, then
// Ownerless, then Stale or Path to Green overdue, then Yellow, then Green, then
// no Health.
func (r goalRow) problemRank() int {
	switch {
	case r.health() == domain.HealthRed:
		return 0
	case r.Goal.Ownerless:
		return 1
	case r.Freshness.Stale || r.Freshness.PathToGreenOverdue:
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
	goals = domain.FilterGoals(goals, byDimension)
	filter := readGoalFilter(r.URL.Query())
	delegatedIDs := map[int64]bool{}
	if filter.Mine {
		delegated, err := s.svc.DelegatedGoals(ctx, current.ID)
		if err != nil {
			return goalsListData{}, err
		}
		for _, g := range delegated {
			delegatedIDs[g.ID] = true
		}
	}
	mine := func(g domain.Goal) bool { return g.Owner.ID == current.ID || delegatedIDs[g.ID] }

	rows := make([]goalRow, 0, len(goals))
	for _, gv := range goals {
		row, err := s.loadGoalRow(ctx, gv)
		if err != nil {
			return goalsListData{}, err
		}
		if filter.keeps(row, mine) {
			rows = append(rows, row)
		}
	}
	sortGoalRows(rows)

	view := goalsListData{
		Rows:       rows,
		Filter:     filter,
		Dimensions: dims,
		Selected:   selected,
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
	return view, nil
}

// loadGoalRow reads what gv's row shows beyond the Goal itself. It costs a few
// queries per Goal, which the list can afford for now.
func (s *Server) loadGoalRow(ctx context.Context, gv domain.GoalWithValues) (goalRow, error) {
	row := goalRow{GoalWithValues: gv}
	latest, ok, err := s.svc.LatestCheckin(ctx, gv.Goal.ID)
	if err != nil {
		return goalRow{}, fmt.Errorf("load latest check-in: %w", err)
	}
	if ok {
		row.Latest = &latest
	}
	if row.Freshness, err = s.svc.Freshness(ctx, gv.Goal.ID); err != nil {
		return goalRow{}, fmt.Errorf("read freshness: %w", err)
	}
	slips, err := s.svc.ListDateSlips(ctx, gv.Goal.ID)
	if err != nil {
		return goalRow{}, fmt.Errorf("load date slips: %w", err)
	}
	for _, slip := range slips {
		if slip.MilestoneID == 0 {
			row.PriorDates = append(row.PriorDates, slip.OldDate)
		}
	}
	return row, nil
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

func (s *Server) handleViewGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view, err := s.goalPageView(r.Context(), id, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalPage(&current, view))
}

// goalPageView loads everything the single-Goal page renders for the viewer. A
// missing Goal comes back as domain.ErrNotFound.
func (s *Server) goalPageView(ctx context.Context, id int64, current domain.Account) (goalView, error) {
	g, err := s.svc.ViewGoal(ctx, id)
	if err != nil {
		return goalView{}, err
	}

	parents, err := s.svc.ParentLinks(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load parents: %w", err)
	}
	children, err := s.svc.ChildLinks(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load children: %w", err)
	}
	// Candidate parents to contribute to: every other Goal. The domain rejects
	// self-links, duplicates, and cycles when the request is actually made.
	all, err := s.svc.ListGoals(ctx)
	if err != nil {
		return goalView{}, fmt.Errorf("load goals: %w", err)
	}
	candidates := make([]domain.Goal, 0, len(all))
	for _, c := range all {
		if c.ID != g.ID {
			candidates = append(candidates, c)
		}
	}

	milestones, err := s.svc.ListMilestones(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load milestones: %w", err)
	}
	metrics, err := s.svc.ListMetrics(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load metrics: %w", err)
	}
	// Each Metric's readings over time, for its trend against target (CONTEXT.md:
	// the Goal page shows each Metric's trend over time against its target).
	trends := make([]metricTrend, 0, len(metrics))
	for _, m := range metrics {
		readings, err := s.svc.ListMetricReadings(ctx, m.ID)
		if err != nil {
			return goalView{}, fmt.Errorf("load metric readings: %w", err)
		}
		trends = append(trends, metricTrend{Metric: m, Readings: readings})
	}
	highlights, err := s.svc.ListHighlightsByGoal(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load highlights: %w", err)
	}
	contributors, err := s.svc.ListContributors(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load contributors: %w", err)
	}
	delegates, err := s.svc.ListDelegates(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load delegates: %w", err)
	}
	revisions, err := s.svc.ListSoWhatRevisions(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load So What history: %w", err)
	}
	ownership, err := s.svc.OwnershipHistory(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load ownership history: %w", err)
	}
	dimensions, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return goalView{}, fmt.Errorf("load dimensions: %w", err)
	}
	values, err := s.svc.GoalValues(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load dimension values: %w", err)
	}
	checkins, err := s.svc.ListCheckins(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load check-ins: %w", err)
	}
	latest, ok, err := s.svc.LatestCheckin(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load latest check-in: %w", err)
	}
	var latestPtr *domain.Checkin
	if ok {
		latestPtr = &latest
	}
	rollup, err := s.svc.RolledUpHealth(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("compute rolled-up health: %w", err)
	}
	slips, err := s.svc.ListDateSlips(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("load date slips: %w", err)
	}
	churn, err := s.svc.MilestoneChurn(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("count milestone churn: %w", err)
	}
	signals, err := s.svc.GoalSignals(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("read graph signals: %w", err)
	}
	freshness, err := s.svc.Freshness(ctx, id)
	if err != nil {
		return goalView{}, fmt.Errorf("read freshness signals: %w", err)
	}

	// Each linked Goal's Health, shown beside it in the sidebar.
	linkHealth := map[int64]string{}
	for _, l := range slices.Concat(parents, children) {
		if l.Goal.Lifecycle != domain.LifecycleActive {
			continue
		}
		c, ok, err := s.svc.LatestCheckin(ctx, l.Goal.ID)
		if err != nil {
			return goalView{}, fmt.Errorf("load linked goal's check-in: %w", err)
		}
		if ok {
			linkHealth[l.Goal.ID] = c.Health
		}
	}

	// A Delegate may write Check-ins too, so the Goal page shows the Check-in
	// form to the Owner or any authorized Delegate (CONTEXT.md: Delegate).
	canCheckin := current.ID == g.Owner.ID
	for _, d := range delegates {
		if d.ID == current.ID {
			canCheckin = true
		}
	}
	// The Owner's Delegates and Admins set a Goal's Dimension values too
	// (CONTEXT.md: Delegate).
	canSetValues := canCheckin || current.IsAdmin

	return goalView{
		Goal:           g,
		Parents:        parents,
		Children:       children,
		LinkHealth:     linkHealth,
		Candidates:     candidates,
		Milestones:     milestones,
		Metrics:        metrics,
		Trends:         trends,
		Highlights:     highlights,
		Contributors:   contributors,
		Delegates:      delegates,
		CanCheckin:     canCheckin,
		CanSetValues:   canSetValues,
		Revisions:      revisions,
		Ownership:      ownership,
		Dimensions:     dimensions,
		Values:         values,
		Checkins:       checkins,
		LatestCheckin:  latestPtr,
		RolledUp:       rollup,
		DateSlips:      slips,
		MilestoneChurn: churn,
		Signals:        signals,
		Freshness:      freshness,
		SuggestedDate:  domain.SuggestDeliveryDate(s.svc.Now()).Format(dateLayout),
	}, nil
}

// goalView is everything the single-Goal page renders: the Goal itself, its
// graph links, and the Milestones, Metrics, Contributors, and So What history an
// Owner fills in before activating it.
type goalView struct {
	Goal     domain.Goal
	Parents  []domain.GoalLink
	Children []domain.GoalLink
	// LinkHealth is the Health of each Active parent and child with a
	// Check-in, keyed by Goal ID.
	LinkHealth map[int64]string
	Candidates []domain.Goal
	Milestones []domain.Milestone
	Metrics    []domain.Metric
	// Trends pairs each Metric with its readings over time, for the trend against
	// target shown on the Goal page. Highlights are the Goal's flagged notes,
	// newest first (CONTEXT.md: Metric, Highlight).
	Trends       []metricTrend
	Highlights   []domain.Highlight
	Contributors []domain.Account
	// Delegates are the Accounts the Owner has authorized to write Check-ins on
	// this Goal, and CanCheckin is true when the viewer may write one — the Owner
	// or one of those Delegates (CONTEXT.md: Delegate).
	Delegates  []domain.Account
	CanCheckin bool
	Revisions  []domain.SoWhatRevision
	// Ownership is the Goal's ownership history, oldest first: every Handoff
	// with its outcome and every Admin Reassign (CONTEXT.md: Handoff).
	Ownership []domain.Handoff
	// Dimensions are all defined Dimensions, Retired ones included so the values
	// the Goal carries in them stay readable; only the rest get value-assignment
	// controls. Values are the values this Goal currently carries, retired ones
	// included so they stay readable.
	// CanSetValues is true when the viewer may set them — the Owner, a Delegate
	// or an Admin.
	Dimensions   []domain.Dimension
	Values       []domain.DimensionValue
	CanSetValues bool
	// Checkins is the Goal's Check-in history (newest first) and LatestCheckin is
	// the most recent one, carrying the Goal's current Health, status, and Path to
	// Green. LatestCheckin is nil when the Goal has no Check-ins yet.
	Checkins      []domain.Checkin
	LatestCheckin *domain.Checkin
	// RolledUp is the Goal's Rolled-up Health — the worst Owner-set Health among
	// its Active children — shown next to the Owner-set Health (ADR-0003). Its
	// Present is false when there is nothing to roll up.
	RolledUp domain.RolledUpHealth
	// DateSlips is the Goal's Date Slip history, earliest first, covering its
	// delivery date and its Milestones' dates; its length is the slip count.
	// MilestoneChurn counts Milestones added or removed since the Goal became
	// Active (CONTEXT.md: Date Slip, Milestone Churn).
	DateSlips      []domain.DateSlip
	MilestoneChurn int
	// Signals are the risks the graph flags on this Goal that nobody reported:
	// Unaligned, schedule conflicts it is either side of, and parents that are On
	// Hold or Cancelled.
	Signals domain.GoalSignals
	// Freshness says whether the Goal is Stale or its Path to Green is overdue,
	// flagged as prominently as Red (CONTEXT.md: Stale, Path to Green).
	Freshness     domain.Freshness
	SuggestedDate string
	// ChildForm is the add-child-Goal form's input, filled in when a failed
	// create sends the page back.
	ChildForm childGoalForm
}

// priorDates returns the dates a Goal's delivery date (milestoneID 0) or one of
// its Milestones held before each of its Date Slips, earliest first, for
// showing struck through ahead of the current date (CONTEXT.md: Date Slip).
func (v goalView) priorDates(milestoneID int64) []time.Time {
	var out []time.Time
	for _, s := range v.DateSlips {
		if s.MilestoneID == milestoneID {
			out = append(out, s.OldDate)
		}
	}
	return out
}

// lifecycleNote explains the Goal's current Lifecycle from the Check-in that
// moved it there: the outcome of a Done Goal, or the reason it is On Hold or
// Cancelled (CONTEXT.md: Lifecycle). It is empty when there is nothing to say,
// such as for a Goal activated through the activation gate.
func (v goalView) lifecycleNote() string {
	for _, c := range v.Checkins {
		ch := c.LifecycleChange
		if !ch.Changed() {
			continue
		}
		if ch.To != v.Goal.Lifecycle {
			return ""
		}
		switch {
		case ch.Outcome != "":
			return "Outcome: " + ch.Outcome
		case ch.Reason != "":
			return "Reason: " + ch.Reason
		}
		return ""
	}
	return ""
}

// milestoneName names the Milestone a Date Slip moved, for the slip history.
func (v goalView) milestoneName(id int64) string {
	for _, m := range v.Milestones {
		if m.ID == id {
			return m.Name
		}
	}
	return fmt.Sprintf("Milestone %d", id)
}

// metricTrend pairs a Metric with its readings over time, so the Goal page can
// show each Metric's trend against its target (CONTEXT.md: Metric).
type metricTrend struct {
	Metric   domain.Metric
	Readings []domain.MetricReading
}

// assignedValues returns the values this Goal carries in the given Dimension —
// at most one unless the Dimension takes several — used to show the current
// assignment and preselect the assignment control.
func (v goalView) assignedValues(dimensionID int64) []domain.DimensionValue {
	var out []domain.DimensionValue
	for _, val := range v.Values {
		if val.DimensionID == dimensionID {
			out = append(out, val)
		}
	}
	return out
}

// childDefaults are the Goal's values offered as defaults to a child Goal: the
// ones that can still be newly assigned, so neither a Retired value nor one in a
// Retired Dimension (CONTEXT.md: Retired).
func (v goalView) childDefaults() []domain.DimensionValue {
	var out []domain.DimensionValue
	for _, val := range v.Values {
		if !val.Retired && !val.DimensionRetired {
			out = append(out, val)
		}
	}
	return out
}

// carries reports whether the Goal carries the value in the given Dimension.
func (v goalView) carries(value domain.DimensionValue) bool {
	for _, val := range v.Values {
		if val.ID == value.ID {
			return true
		}
	}
	return false
}

// goalIDFromPath parses the {id} path value, writing a 404 and returning ok
// false when it is not a number.
func goalIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// fmtDate renders a calendar date for a date input, or "" for the zero time.
func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

// fmtNum renders a Metric's baseline or target without trailing zeros.
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseDate reads a date form field in the HTML date-input format. An empty
// field is the zero time, which the domain treats as "no date".
func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, value)
}

// writeCommandResult redirects back to the Goal on success and surfaces a
// validation error (e.g. an activation gate failure) as 422 and a refusal to act
// on someone else's Goal as 403, each with its message.
func writeCommandResult(w http.ResponseWriter, r *http.Request, goalID int64, err error) {
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrValidation):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		case errors.Is(err, domain.ErrNotAuthorized):
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, "could not update goal", http.StatusInternalServerError)
		}
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

func (s *Server) handleMarkGoalDated(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("delivery_date"))
	if err != nil {
		http.Error(w, "invalid delivery date", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.MarkGoalDated(r.Context(), id, date)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleMarkGoalOngoing(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.MarkGoalOngoing(r.Context(), id)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleSetCadence(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	days, err := strconv.Atoi(r.FormValue("cadence_days"))
	if err != nil {
		http.Error(w, "invalid cadence", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.SetCadence(r.Context(), id, days)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditSoWhat(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.EditSoWhat(r.Context(), id, r.FormValue("so_what"), current.ID)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleAddContributor(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.AddContributorByEmail(r.Context(), id, r.FormValue("email"))
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleAddMilestone(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		http.Error(w, "invalid milestone date", http.StatusUnprocessableEntity)
		return
	}
	_, err = s.svc.AddMilestone(r.Context(), domain.AddMilestoneInput{
		GoalID:     id,
		Name:       r.FormValue("name"),
		TargetDate: date,
	})
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditMilestone(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		http.Error(w, "invalid milestone date", http.StatusUnprocessableEntity)
		return
	}
	m, err := s.svc.EditMilestone(r.Context(), domain.EditMilestoneInput{
		MilestoneID: id,
		Name:        r.FormValue("name"),
		TargetDate:  date,
	})
	if err != nil {
		writeCommandResult(w, r, id, err)
		return
	}
	writeCommandResult(w, r, m.GoalID, nil)
}

func (s *Server) handleAddMetric(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	in, err := metricInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	in.GoalID = id
	_, err = s.svc.AddMetric(r.Context(), in)
	writeCommandResult(w, r, id, err)
}

func (s *Server) handleEditMetric(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	in, err := metricInputFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	m, err := s.svc.EditMetric(r.Context(), domain.EditMetricInput{
		MetricID:   id,
		Name:       in.Name,
		Unit:       in.Unit,
		Direction:  in.Direction,
		Baseline:   in.Baseline,
		Target:     in.Target,
		TargetDate: in.TargetDate,
	})
	if err != nil {
		writeCommandResult(w, r, id, err)
		return
	}
	writeCommandResult(w, r, m.GoalID, nil)
}

// metricInputFromForm reads the numeric and date fields common to adding and
// editing a Metric. The GoalID is filled in by the caller.
func metricInputFromForm(r *http.Request) (domain.AddMetricInput, error) {
	baseline, err := strconv.ParseFloat(r.FormValue("baseline"), 64)
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid baseline")
	}
	target, err := strconv.ParseFloat(r.FormValue("target"), 64)
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid target")
	}
	date, err := parseDate(r.FormValue("target_date"))
	if err != nil {
		return domain.AddMetricInput{}, errors.New("invalid target date")
	}
	return domain.AddMetricInput{
		Name:       r.FormValue("name"),
		Unit:       r.FormValue("unit"),
		Direction:  r.FormValue("direction"),
		Baseline:   baseline,
		Target:     target,
		TargetDate: date,
	}, nil
}

func (s *Server) handleActivateGoal(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.ActivateGoal(r.Context(), id)
	writeCommandResult(w, r, id, err)
}

// handleAssignGoalValue sets the Goal's Dimension values (CONTEXT.md: Owners
// and their Delegates set a Goal's Dimension values). A form naming a
// dimension_id and a new_value, an Extendable Dimension's add-a-value input,
// sets the value so named, adding it to the list when it's new. One naming a
// dimension_id without a new_value, a several-values Dimension's checkboxes,
// saves its checked value_ids together, so an unchecked value is removed. A
// single value_id without one, a one-value Dimension's select, replaces any
// value the Goal already carries there, and an empty selection is a no-op.
// Anyone but the Owner, a Delegate or an Admin is refused.
func (s *Server) handleAssignGoalValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if rawDim := r.FormValue("dimension_id"); rawDim != "" {
		dimensionID, err := strconv.ParseInt(rawDim, 10, 64)
		if err != nil {
			http.Error(w, "invalid dimension", http.StatusUnprocessableEntity)
			return
		}
		if newValue, ok := r.Form["new_value"]; ok {
			_, err := s.svc.AssignGoalValueByName(r.Context(), current.ID, id, dimensionID, newValue[0])
			writeCommandResult(w, r, id, err)
			return
		}
		var valueIDs []int64
		for _, raw := range r.Form["value_id"] {
			valueID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid value", http.StatusUnprocessableEntity)
				return
			}
			valueIDs = append(valueIDs, valueID)
		}
		writeCommandResult(w, r, id, s.svc.SetGoalValues(r.Context(), current.ID, id, dimensionID, valueIDs))
		return
	}
	raw := r.FormValue("value_id")
	if raw == "" {
		writeCommandResult(w, r, id, nil)
		return
	}
	valueID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.Error(w, "invalid value", http.StatusUnprocessableEntity)
		return
	}
	writeCommandResult(w, r, id, s.svc.AssignGoalValue(r.Context(), current.ID, id, valueID))
}

// handleCreateChildGoal creates a Goal under the parent in the path: it is owned
// by the current Account, carries whichever of the parent's values were kept as
// defaults, and requests a "contributes to" link to the parent. The parent's
// values are offered as defaults but not inherited — only the kept ones are
// assigned (CONTEXT.md: Contributes to; the parent's values are offered as
// defaults).
func (s *Server) handleCreateChildGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	parentID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	form := childGoalForm{
		Title:  r.FormValue("title"),
		SoWhat: r.FormValue("so_what"),
		Kept:   map[int64]bool{},
	}
	var valueIDs []int64
	for _, raw := range r.Form["value_id"] {
		if valueID, err := strconv.ParseInt(raw, 10, 64); err == nil {
			valueIDs = append(valueIDs, valueID)
			form.Kept[valueID] = true
		}
	}

	// Creating the Goal, assigning its kept defaults, and requesting its link
	// are one transaction: a failure at any step (say, a default retired after
	// the form loaded) leaves no Goal behind.
	var child domain.Goal
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		var err error
		child, err = tx.CreateGoal(r.Context(), domain.CreateGoalInput{
			Title:   form.Title,
			SoWhat:  form.SoWhat,
			OwnerID: current.ID,
		})
		if err != nil {
			return err
		}
		for _, valueID := range valueIDs {
			if err := tx.AssignGoalValue(r.Context(), current.ID, child.ID, valueID); err != nil {
				return err
			}
		}
		_, err = tx.RequestLink(r.Context(), domain.RequestLinkInput{
			ChildID:     child.ID,
			ParentID:    parentID,
			RequesterID: current.ID,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			form.Error = err.Error()
			s.renderChildGoalFormError(w, r, parentID, current, form)
			return
		}
		http.Error(w, "could not create child goal", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(child.ID, 10), http.StatusSeeOther)
}

// childGoalForm is what a person typed into the add-child-Goal form, kept so a
// failed create re-renders the form as they left it. Kept holds the defaults
// they left checked; nil means the form is fresh and every default is checked.
type childGoalForm struct {
	Title  string
	SoWhat string
	Kept   map[int64]bool
	Error  string
}

// keeps reports whether the default value is checked in the form.
func (f childGoalForm) keeps(valueID int64) bool {
	return f.Kept == nil || f.Kept[valueID]
}

// renderChildGoalFormError re-renders the parent's page with 422, its
// add-child-Goal form carrying the error and the person's input.
func (s *Server) renderChildGoalFormError(w http.ResponseWriter, r *http.Request, parentID int64, current domain.Account, form childGoalForm) {
	view, err := s.goalPageView(r.Context(), parentID, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	view.ChildForm = form
	render(w, r, http.StatusUnprocessableEntity, goalPage(&current, view))
}

// health is the Goal's current Health — its latest Check-in's — while it is
// Active, or "" when it has none: a Proposed Goal, one not yet checked in on,
// or one On Hold, Done or Cancelled.
func (v goalView) health() string {
	if v.LatestCheckin == nil || v.Goal.Lifecycle != domain.LifecycleActive {
		return ""
	}
	return v.LatestCheckin.Health
}

// Sparkline geometry, in SVG user units: a Metric card's trend is drawn in a
// sparkW × sparkH box, inset by sparkPad so the end dot isn't clipped.
const (
	sparkW   = 240.0
	sparkH   = 56.0
	sparkPad = 5.0
)

// sparkline is a Metric's readings laid out for its card: a point per reading,
// oldest first, and the height of the target line, on a scale that fits both.
type sparkline struct {
	Points  []sparkPoint
	TargetY float64
}

// sparkPoint is one reading's place on the sparkline, with the value and date
// its hover tooltip names.
type sparkPoint struct {
	X, Y  float64
	Title string
}

// polyline is the readings as an SVG polyline's points attribute.
func (s sparkline) polyline() string {
	pts := make([]string, 0, len(s.Points))
	for _, p := range s.Points {
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", p.X, p.Y))
	}
	return strings.Join(pts, " ")
}

// last is the latest reading's point, drawn in the accent colour.
func (s sparkline) last() sparkPoint {
	return s.Points[len(s.Points)-1]
}

// sparkline lays the Metric's readings out against its target.
func (t metricTrend) sparkline() sparkline {
	lo, hi := t.Metric.Target, t.Metric.Target
	for _, r := range t.Readings {
		lo, hi = min(lo, r.Value), max(hi, r.Value)
	}
	if lo == hi {
		lo, hi = lo-1, hi+1
	}
	y := func(v float64) float64 {
		return sparkPad + (hi-v)/(hi-lo)*(sparkH-2*sparkPad)
	}
	s := sparkline{TargetY: y(t.Metric.Target)}
	for i, r := range t.Readings {
		x := sparkW / 2
		if n := len(t.Readings); n > 1 {
			x = sparkPad + float64(i)*(sparkW-2*sparkPad)/float64(n-1)
		}
		s.Points = append(s.Points, sparkPoint{
			X: x, Y: y(r.Value),
			Title: fmt.Sprintf("%s: %s %s", fmtDate(r.CreatedAt), fmtNum(r.Value), t.Metric.Unit),
		})
	}
	return s
}

// current is the Metric's latest reading, and whether it has one.
func (t metricTrend) current() (float64, bool) {
	if len(t.Readings) == 0 {
		return 0, false
	}
	return t.Readings[len(t.Readings)-1].Value, true
}

// trendLabel describes the sparkline for a screen reader: how many readings,
// which way they moved, and whether that is toward the target.
func (t metricTrend) trendLabel() string {
	m := t.Metric
	target := fmt.Sprintf("the target of %s %s", fmtNum(m.Target), m.Unit)
	n := len(t.Readings)
	switch n {
	case 0:
		return fmt.Sprintf("%s: no readings yet; %s", m.Name, target)
	case 1:
		return fmt.Sprintf("%s: 1 reading of %s %s, against %s", m.Name, fmtNum(t.Readings[0].Value), m.Unit, target)
	}
	first, last := t.Readings[0].Value, t.Readings[n-1].Value
	if first == last {
		return fmt.Sprintf("%s: %d readings, holding at %s %s, against %s", m.Name, n, fmtNum(last), m.Unit, target)
	}
	moved, better := "rising", last > first
	if last < first {
		moved = "falling"
	}
	if m.Direction == domain.MetricDown {
		better = !better
	}
	toward := "away from"
	if better {
		toward = "toward"
	}
	return fmt.Sprintf("%s: %d readings, %s from %s to %s %s, %s %s", m.Name, n, moved, fmtNum(first), fmtNum(last), m.Unit, toward, target)
}

// activationItem is one line of a Proposed Goal's activation checklist: a rule
// activation enforces and whether the Goal meets it yet.
type activationItem struct {
	Label string
	Done  bool
}

// activationChecklist restates the minimum standard domain.ActivateGoal
// enforces, so the Owner sees what's missing before they try: a So What, an
// Owner, Dated with a delivery date or Ongoing, and a Milestone or Metric for a
// Dated Goal or a Metric for an Ongoing one. The server still enforces the rules
// on activation.
func (v goalView) activationChecklist() []activationItem {
	g := v.Goal
	items := []activationItem{
		{"So What", strings.TrimSpace(g.SoWhat) != ""},
		{"Owner", g.Owner.ID != 0},
		{"Dated with a delivery date, or Ongoing", g.Kind == domain.GoalOngoing || (g.Kind == domain.GoalDated && !g.DeliveryDate.IsZero())},
	}
	if g.Kind == domain.GoalOngoing {
		return append(items, activationItem{"A Metric", len(v.Metrics) > 0})
	}
	return append(items, activationItem{"A Milestone or Metric", len(v.Milestones)+len(v.Metrics) > 0})
}

// readyToActivate reports whether every activation checklist item is done.
func (v goalView) readyToActivate() bool {
	for _, item := range v.activationChecklist() {
		if !item.Done {
			return false
		}
	}
	return true
}

// ownershipOutcome names an ownership change's outcome for the Goal page's
// Ownership history: a Handoff's status, or that an Admin reassigned the Goal.
func ownershipOutcome(status string) string {
	if status == domain.HandoffReassigned {
		return "reassigned by an Admin"
	}
	return status
}
