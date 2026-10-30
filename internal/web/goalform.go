package web

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalFormView is the New goal page: what was typed and, after a refused
// submit, each problem. A problem names the input it refuses by its form name
// — the name the domain's InputError carries — or none when it is the whole
// submit's.
type goalFormView struct {
	// Goal is the Proposed Goal being finished on the form, nil on the New
	// goal page. Its Milestones and Metrics, and the parents it has asked to
	// contribute to, Accepted or Pending, show read-only; the rows and the
	// picked parents only add to them.
	Goal               *domain.Goal
	ExistingMilestones []domain.Milestone
	ExistingMetrics    []domain.Metric
	// Dates is the clock the existing Milestones and Metrics show their dates
	// against, and whose year script writes the rest's against. Every render
	// of the page sets it.
	Dates displayDates
	// Today is today's date, ISO, in the Owner's Timezone, which the form's
	// date warnings judge against. Every render that sets Dates sets it.
	Today        string
	Linked       []linkedParent
	Title        string
	WhyItMatters string
	// Kind is the Kind chosen, "" for not chosen, and DeliveryDate the date
	// typed, which counts only for a Dated Goal.
	Kind         string
	DeliveryDate string
	// Cadence is the cadence segment chosen, its days or cadenceCustom, and
	// CadenceDays the days typed for Custom.
	Cadence     string
	CadenceDays string
	Milestones  []milestoneRow
	Metrics     []metricRow
	// Unsaved is the measure, Milestone or Metric, whose editor script has
	// open, unsaved, as the form posts it (inputEditing); "" when none is.
	Unsaved string
	// Parents are the Goals picked to contribute to, and Candidates every
	// other Goal, which the no-script select offers.
	Parents    []parentChoice
	Candidates []domain.Goal
	// Dimensions and Fields are those still offered, for Where it fits.
	// Chosen are the values ticked or selected, NewValues the value typed to
	// add to each Extendable Dimension, and FieldValues what's typed in each
	// Field, each by ID. Suggested says Chosen came from the parents rather
	// than a submit.
	Dimensions  []domain.Dimension
	Fields      []domain.Field
	Chosen      map[int64]bool
	NewValues   map[int64]string
	FieldValues map[int64]string
	Suggested   bool
	Problems    []*domain.InputError
	// Live says the Ready to activate card answers the form as typed, so its
	// Create and activate follows the checklist. Without it, as the page
	// loads, Create and activate is enabled, since the server decides.
	Live bool
}

// activation is the form, as typed, judged against the activation standard
// for its Ready to activate checklist. Its Owner is the person creating or
// finishing it. Only a Dated Goal's delivery date counts, and only one that
// parses; a Milestone or Metric row counts once anything is typed in it, as
// the saved ones do (domain.JudgeDefinition).
func (v goalFormView) activation() []domain.ActivationRequirement {
	d := domain.DefinitionDraft{
		WhyItMatters: v.WhyItMatters,
		Kind:         v.Kind,
		Milestones:   len(v.ExistingMilestones) + len(v.Milestones),
		Metrics:      len(v.ExistingMetrics) + len(v.Metrics),
		Chosen:       v.Chosen,
		NewValues:    v.NewValues,
		FieldValues:  v.FieldValues,
	}
	if v.Kind == domain.GoalDated {
		if date, err := parseDate(strings.TrimSpace(v.DeliveryDate)); err == nil {
			d.DeliveryDate = date
		}
	}
	return domain.JudgeDefinition(d, v.Dimensions, v.Fields)
}

// checklist is the form's Ready to activate checklist, as typed.
func (v goalFormView) checklist() []activationItem {
	return activationChecklist(v.activation(), v.Unsaved)
}

// requiredValues are the form's requirements of a value in a required
// Dimension or Field, in checklist order.
func (v goalFormView) requiredValues() []domain.ActivationRequirement {
	var out []domain.ActivationRequirement
	for _, r := range v.activation() {
		if r.Rule == domain.ActivationValue {
			out = append(out, r)
		}
	}
	return out
}

// year is the company's current year, as text, for How you'll know's script
// to write an item's date as showDate does: without its year in this one.
func (v goalFormView) year() string {
	if v.Dates.Loc == nil {
		return strconv.Itoa(v.Dates.Now.Year())
	}
	return strconv.Itoa(v.Dates.Now.In(v.Dates.Loc).Year())
}

// readyCount is how many of the Ready to activate checklist's items are done,
// of how many.
func (v goalFormView) readyCount() (done, of int) {
	items := v.checklist()
	for _, item := range items {
		if item.Done {
			done++
		}
	}
	return done, len(items)
}

// activateLabel is what the activate button reads: Create and activate on
// the New goal page, and Activate Goal finishing a Goal that exists, as the
// Goal page's Define card calls it.
func (v goalFormView) activateLabel() string {
	if v.Goal == nil {
		return "Create and activate"
	}
	return "Activate Goal"
}

// activateDisabled says Create and activate is disabled: only on the live
// card, while the checklist is incomplete.
func (v goalFormView) activateDisabled() bool {
	return v.Live && !allDone(v.checklist())
}

// parentChoice is a Goal offered or picked to contribute to: its Health, ""
// when it has none, and the label of its Owner when picking it asks that
// Owner to accept, "" when the creator owns it and it links at once.
type parentChoice struct {
	Goal   domain.Goal
	Health string
	Asks   string
}

// pickedParents are the Contributes to picker's chips: each parent picked,
// with why it was refused.
func (v goalFormView) pickedParents() []goalPickerChip {
	var chips []goalPickerChip
	for _, p := range v.Parents {
		chips = append(chips, goalPickerChip{Choice: p, Error: v.bad(domain.ParentInput(p.Goal.ID))})
	}
	return chips
}

// asked names each Owner the picked parents ask to accept, once each, in the
// order picked.
func (v goalFormView) asked() []string {
	var names []string
	for _, p := range v.Parents {
		if p.Asks != "" && !slices.Contains(names, p.Asks) {
			names = append(names, p.Asks)
		}
	}
	return names
}

// contributesNames are the titles of the Goals the form contributes to, for
// Contributes to's row: those the Goal being finished already asked to, then
// those picked.
func (v goalFormView) contributesNames() []string {
	var names []string
	for _, p := range v.Linked {
		names = append(names, p.Goal.Title)
	}
	for _, p := range v.Parents {
		names = append(names, p.Goal.Title)
	}
	return names
}

// contributesOpen says Contributes to's row opens by itself: when it holds a
// refused input.
func (v goalFormView) contributesOpen() bool {
	return v.refusedUnder(inputParentID, "parent:")
}

// fitsNames are what Where it fits' row names: in page order, each
// Dimension's values chosen and the value typed to add to it, then each
// Field's value typed.
func (v goalFormView) fitsNames() []string {
	var names []string
	for _, d := range v.Dimensions {
		for _, val := range v.choices(d) {
			if v.Chosen[val.ID] {
				names = append(names, val.Value)
			}
		}
		if typed := strings.TrimSpace(v.NewValues[d.ID]); d.Extendable() && typed != "" {
			names = append(names, typed)
		}
	}
	for _, f := range v.Fields {
		if typed := strings.TrimSpace(v.FieldValues[f.ID]); typed != "" {
			names = append(names, typed)
		}
	}
	return names
}

// fitsOpen says Where it fits' row opens by itself: when a required Dimension
// or Field in it isn't set, as the checklist links to it, or it holds a
// refused input.
func (v goalFormView) fitsOpen() bool {
	for _, r := range v.requiredValues() {
		if !r.Met {
			return true
		}
	}
	return v.refusedUnder("value:", inputNewValue, inputField)
}

// refusedUnder says a problem refuses an input named by any of prefixes.
func (v goalFormView) refusedUnder(prefixes ...string) bool {
	for _, p := range v.Problems {
		for _, prefix := range prefixes {
			if p.Input != "" && strings.HasPrefix(p.Input, prefix) {
				return true
			}
		}
	}
	return false
}

// The form's date warnings, which never refuse anything (#675).
const (
	goalFormWarnPastDelivery   = "This delivery date has already passed."
	goalFormWarnMilestoneLate  = "This Milestone is after the delivery date."
	goalFormWarnPastMetricDate = "This target date has already passed."
)

// warning is what the form, as typed, warns of beside the named input, or ""
// when nothing: a Dated Goal's delivery date before Today, a Milestone's date
// after that delivery date, or a Metric's target date before Today. An input
// refused shows its error instead, and any other input has no warning.
func (v goalFormView) warning(input string) string {
	if v.Today == "" || v.bad(input) != "" {
		return ""
	}
	today, err := parseDate(v.Today)
	if err != nil {
		return ""
	}
	var delivery time.Time
	if v.Kind == domain.GoalDated {
		delivery, _ = parseDate(strings.TrimSpace(v.DeliveryDate))
	}
	if input == domain.InputDeliveryDate {
		if !delivery.IsZero() && delivery.Before(today) {
			return goalFormWarnPastDelivery
		}
		return ""
	}
	for i, m := range v.Milestones {
		if input != domain.MilestoneInput(i, "date") {
			continue
		}
		if date, err := parseDate(strings.TrimSpace(m.Date)); err == nil && !delivery.IsZero() && date.After(delivery) {
			return goalFormWarnMilestoneLate
		}
		return ""
	}
	for i, m := range v.Metrics {
		if input != domain.MetricInput(i, "target_date") {
			continue
		}
		if date, err := parseDate(strings.TrimSpace(m.TargetDate)); err == nil && !date.IsZero() && date.Before(today) {
			return goalFormWarnPastMetricDate
		}
		return ""
	}
	return ""
}

// goalFormToday is today's date, ISO, in the Timezone of g's Owner, which the
// form's date warnings judge against (CONTEXT.md: Timezone).
func (s *Server) goalFormToday(g domain.Goal) string {
	return s.svc.Now().In(s.svc.GoalTimezone(g)).Format(dateLayout)
}

// milestoneRow is one Milestone row of the New goal form, as typed.
type milestoneRow struct {
	Name string
	Date string
}

// metricRow is one Metric row of the New goal form, as typed. Direction is the
// Direction select's choice, which counts only when Baseline and Target are
// equal.
type metricRow struct {
	Name       string
	Unit       string
	Baseline   string
	Target     string
	Direction  string
	TargetDate string
}

// summary is each problem the top of a refused form lists, under the input it
// links to, once: values posted together in a Dimension that takes one are
// each refused alike, at the one select.
func (v goalFormView) summary() []*domain.InputError {
	var out []*domain.InputError
	for _, p := range v.Problems {
		linked := &domain.InputError{Input: v.selectInput(p.Input), Message: p.Message}
		if !slices.ContainsFunc(out, func(q *domain.InputError) bool { return *q == *linked }) {
			out = append(out, linked)
		}
	}
	return out
}

// bad is why the named input was refused, or "" when it wasn't.
func (v goalFormView) bad(input string) string {
	for _, p := range v.Problems {
		if p.Input == input {
			return p.Message
		}
	}
	return ""
}

// milestoneRows are the Milestone rows to show: those typed, or one blank row
// when there are none.
func (v goalFormView) milestoneRows() []milestoneRow {
	if len(v.Milestones) == 0 {
		return []milestoneRow{{}}
	}
	return v.Milestones
}

// metricRows are the Metric rows to show: those typed, or one blank row when
// there are none.
func (v goalFormView) metricRows() []metricRow {
	if len(v.Metrics) == 0 {
		return []metricRow{{}}
	}
	return v.Metrics
}

// rowNames names the i-th row's inputs by the domain's names for them
// (domain.MilestoneInput, domain.MetricInput).
func rowNames(input func(i int, part string) string, i int) func(part string) string {
	return func(part string) string { return input(i, part) }
}

// templateRowNames names a <template> row's inputs, section[__i__].part, for
// script to number when it adds the row.
func templateRowNames(section string) func(part string) string {
	return func(part string) string { return section + "[__i__]." + part }
}

// handleNewGoalForm renders the New goal page, empty but for the parents
// ?parent=<id> picks, one each.
func (s *Server) handleNewGoalForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	var ids []int64
	for _, value := range r.URL.Query()["parent"] {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	v := goalFormView{Dates: s.displayDates(), Today: s.goalFormToday(domain.Goal{Owner: current})}
	var err error
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, ids, 0); err != nil {
		s.serverError(w, r, "could not load goals", err)
		return
	}
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		s.serverError(w, r, "could not load dimensions and fields", err)
		return
	}
	parentIDs := make([]int64, 0, len(v.Parents))
	for _, p := range v.Parents {
		parentIDs = append(parentIDs, p.Goal.ID)
	}
	if v.Chosen, err = s.svc.SuggestedValues(r.Context(), parentIDs, v.Dimensions); err != nil {
		s.serverError(w, r, "could not load parent goals' values", err)
		return
	}
	v.Suggested = len(v.Chosen) > 0
	render(w, r, http.StatusOK, goalFormPage(&current, v))
}

// loadWhereItFits gives v the Dimensions and Fields still offered for setting
// (CONTEXT.md: Retired).
func (s *Server) loadWhereItFits(ctx context.Context, v *goalFormView) error {
	dims, err := s.svc.ListDimensions(ctx)
	if err != nil {
		return fmt.Errorf("load dimensions: %w", err)
	}
	fields, err := s.svc.ListFields(ctx)
	if err != nil {
		return fmt.Errorf("load fields: %w", err)
	}
	v.Dimensions, v.Fields = domain.OfferedDimensions(dims), domain.OfferedFields(fields)
	return nil
}

// pickParents splits the Goals the Goal goalID could contribute to into the
// parents picked by ids, in that order, each once, and the candidates left
// over, as splitParents does, with each picked parent's Health and whom
// picking it asks.
func (s *Server) pickParents(ctx context.Context, current domain.Account, ids []int64, goalID int64) ([]parentChoice, []domain.Goal, error) {
	parents, candidates, err := s.splitParents(ctx, ids, goalID)
	if err != nil {
		return nil, nil, err
	}
	picked, err := s.parentChoices(ctx, current, parents)
	if err != nil {
		return nil, nil, err
	}
	return picked, candidates, nil
}

// splitParents splits the Goals the Goal goalID could contribute to into the
// parents picked by ids, in that order, each once, and the candidates left
// over. An id that is no such Goal picks nothing. A brand-new Goal, goalID 0,
// has no children or parents, so it could contribute to every Goal; a saved
// one to its domain.ParentCandidates.
func (s *Server) splitParents(ctx context.Context, ids []int64, goalID int64) ([]domain.Goal, []domain.Goal, error) {
	var all []domain.Goal
	var err error
	if goalID == 0 {
		all, err = s.svc.ListGoals(ctx)
	} else {
		all, err = s.svc.ParentCandidates(ctx, goalID)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load goals: %w", err)
	}
	picked, candidates := domain.PickParents(all, ids)
	return picked, candidates, nil
}

// parentChoices are goals offered to current to contribute to, in order: each
// one's Health, read as the Goal page's sidebar reads a linked Goal's, from an
// Active Goal's latest Check-in, and whom picking it asks. The Active Goals'
// latest Check-ins are read in one query, however many goals there are.
func (s *Server) parentChoices(ctx context.Context, current domain.Account, goals []domain.Goal) ([]parentChoice, error) {
	var active []int64
	for _, g := range goals {
		if g.Lifecycle == domain.LifecycleActive {
			active = append(active, g.ID)
		}
	}
	latest, err := s.svc.LatestCheckinsByGoal(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("load parent goals' check-ins: %w", err)
	}
	health := make(map[int64]string, len(latest))
	for id, c := range latest {
		health[id] = c.Health
	}
	return parentChoicesOf(current, goals, health), nil
}

// parentChoicesOf are goals offered to current to contribute to, in order,
// each with its Health out of health, keyed by Goal ID, and whom picking it
// asks.
func parentChoicesOf(current domain.Account, goals []domain.Goal, health map[int64]string) []parentChoice {
	var out []parentChoice
	for _, g := range goals {
		p := parentChoice{Goal: g, Health: health[g.ID]}
		if g.Owner.ID != current.ID {
			p.Asks = g.Owner.Label()
		}
		out = append(out, p)
	}
	return out
}

// inputParentID is the Contributes to field's name, posted once per parent
// Goal picked.
const inputParentID = "parent_id"

// inputRemoveParent is a parent chip's no-script Remove checkbox, posting the
// parent's id: a ticked one leaves that parent out.
const inputRemoveParent = "remove_parent"

// cadenceChips are the Check-in cadence's segments, each posting its days but
// Custom, which posts cadenceCustom and the days typed beside it.
var cadenceChips = []struct{ Value, Label string }{
	{"7", "Weekly"},
	{"14", "Every 2 weeks"},
	{"30", "Monthly"},
	{cadenceCustom, "Custom"},
}

// cadenceInWords says how often a Goal checks in, as its cadence's segment names
// it but lower-cased ("weekly", "every 2 weeks", "monthly"), or by its days
// for a cadence no segment but Custom posts ("every 3 days").
func cadenceInWords(days int) string {
	for _, c := range cadenceChips {
		if c.Value != cadenceCustom && c.Value == strconv.Itoa(days) {
			return strings.ToLower(c.Label)
		}
	}
	return "every " + plural(days, "day")
}

// cadenceChosen is the cadence segment to show chosen: Weekly at first, and
// Custom for days posted that no other segment posts.
func (v goalFormView) cadenceChosen() string {
	if v.Cadence == "" {
		return cadenceChips[0].Value
	}
	for _, c := range cadenceChips {
		if v.Cadence == c.Value {
			return c.Value
		}
	}
	return cadenceCustom
}

// customDays are the days to show beside Custom: those typed there, or the
// days posted that no other segment posts.
func (v goalFormView) customDays() string {
	if v.cadenceChosen() == cadenceCustom && v.Cadence != cadenceCustom {
		return v.Cadence
	}
	return v.CadenceDays
}

// inputCadenceDays is the number of days the Custom cadence segment reveals.
// cadenceCustom is that segment's value.
const (
	inputCadenceDays = "cadence_days"
	cadenceCustom    = "custom"
)

// goalSearchLimit is the most matches the Goal picker's search answers.
const goalSearchLimit = 20

// handleSearchGoals answers the Goal picker's search as it is typed: each
// candidate whose title contains q, ignoring case, less the parents already
// picked, which come along as parent_id, the first goalSearchLimit of them.
// goal names the Goal being finished on the define page or picked a parent
// for, and the candidates are those it could contribute to, or with
// for=suggestion those it could be suggested under (goalSearchCandidates).
// Each result carries the chip picking it adds.
func (s *Server) handleSearchGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	var picked []int64
	for _, value := range r.URL.Query()[inputParentID] {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			picked = append(picked, id)
		}
	}
	goalID, _ := strconv.ParseInt(r.URL.Query().Get(searchGoal), 10, 64)
	candidates, err := s.goalSearchCandidates(r.Context(), picked, goalID, r.URL.Query().Get(searchFor))
	if err != nil {
		s.serverError(w, r, "could not search goals", err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var found []domain.Goal
	for _, g := range candidates {
		if len(found) == goalSearchLimit {
			break
		}
		if strings.Contains(strings.ToLower(g.Title), q) {
			found = append(found, g)
		}
	}
	matches, err := s.parentChoices(r.Context(), current, found)
	if err != nil {
		s.serverError(w, r, "could not search goals", err)
		return
	}
	render(w, r, http.StatusOK, goalSearchResults(matches))
}

// goalSearchCandidates are the Goals the Goal picker's search looks among,
// less those picked: with forWhat searchForSuggestion, those goalID could be
// suggested under (domain.Service.SuggestableParents); otherwise those it
// could contribute to (splitParents).
func (s *Server) goalSearchCandidates(ctx context.Context, picked []int64, goalID int64, forWhat string) ([]domain.Goal, error) {
	if goalID != 0 && forWhat == searchForSuggestion {
		suggestable, err := s.svc.SuggestableParents(ctx, goalID)
		if err != nil {
			return nil, fmt.Errorf("load suggestable parents: %w", err)
		}
		_, rest := domain.PickParents(suggestable, picked)
		return rest, nil
	}
	_, rest, err := s.splitParents(ctx, picked, goalID)
	return rest, err
}

// handleCreateDefinedGoal submits the New goal page: the Goal is created as
// defined, and activated too when Create and activate posts activate=1, and
// the post lands on its page, which is the confirmation, so there is no toast.
// The server decides activation, whatever the button showed. A refused submit,
// activation included, comes back as the form, 422, as typed, with every
// problem listed at the top and marked on its input, and nothing saved. A
// value the handler can't parse is refused the same way, under its input's
// name, in the same 422 as the domain's problems.
func (s *Server) handleCreateDefinedGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, in := readGoalForm(r, current)
	in.Activate = r.PostFormValue(domain.InputActivate) == "1"
	g, err := s.svc.CreateDefinedGoal(r.Context(), in)
	if err == nil {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) {
		s.serverError(w, r, "could not create goal", err)
		return
	}
	v.Problems = goalFormProblems(err)
	v.Dates, v.Today = s.displayDates(), s.goalFormToday(domain.Goal{Owner: current})
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, in.ParentIDs, 0); err != nil {
		s.serverError(w, r, "could not load goals", err)
		return
	}
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		s.serverError(w, r, "could not load dimensions and fields", err)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
}

// searchGoal is the Goal picker's search parameter naming the Goal being
// finished on the define page, or picked a parent for.
const searchGoal = "goal"

// searchFor is the Goal picker's search parameter naming what it picks for:
// searchForSuggestion, the parents the Goal may be suggested under
// (domain.Service.SuggestableParents), or else those it may contribute to.
const (
	searchFor           = "for"
	searchForSuggestion = "suggestion"
)

// heading names the page: New goal, or Finish defining.
func (v goalFormView) heading() string {
	if v.Goal == nil {
		return "New goal"
	}
	return "Finish defining"
}

// action is where the form posts: /goals/new, or the define page's own
// address.
func (v goalFormView) action() string {
	if v.Goal == nil {
		return "/goals/new"
	}
	return fmt.Sprintf("/goals/%d/define", v.Goal.ID)
}

// checklistURL is where the form posts as it is typed for its live Ready to
// activate card.
func (v goalFormView) checklistURL() string {
	return v.action() + "/checklist"
}

// searchURL is the Contributes to search's address.
func (v goalFormView) searchURL() string {
	if v.Goal == nil {
		return "/goals/search"
	}
	return fmt.Sprintf("/goals/search?%s=%d", searchGoal, v.Goal.ID)
}

// cancelURL is where Cancel leaves the form for: the Goals list, or the Goal
// being finished.
func (v goalFormView) cancelURL() templ.SafeURL {
	if v.Goal == nil {
		return "/goals"
	}
	return templ.SafeURL(fmt.Sprintf("/goals/%d", v.Goal.ID))
}

// renameURL is the Goal page with its Edit title and Why it matters form
// open, where the Goal being finished is renamed, as the define page can't.
func (v goalFormView) renameURL() templ.SafeURL {
	return templ.SafeURL(fmt.Sprintf("/goals/%d?open=%s", v.Goal.ID, formTitleWhyItMatters))
}

// linkedParent is a Goal the Goal being finished already contributes to, or
// has asked to and is waiting on: Pending.
type linkedParent struct {
	Goal    domain.Goal
	Pending bool
}

// definableGoal is the Goal in the path, for its define page, when the domain
// says current may define it (domain.Service.DefinableGoal). Otherwise it
// writes the answer — Not found, the refusal page, 403, for anyone the rule
// refuses for who they are, and a redirect to the Goal's page once it is no longer Proposed — and
// ok is false.
func (s *Server) definableGoal(w http.ResponseWriter, r *http.Request, current domain.Account) (g domain.Goal, ok bool) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return domain.Goal{}, false
	}
	g, err := s.svc.DefinableGoal(r.Context(), current.ID, id)
	switch {
	case err == nil:
		return g, true
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, domain.ErrNotAuthorized):
		s.refuse(w, r, "Can't define Goal", goalPath(id), err)
	case errors.Is(err, domain.ErrValidation):
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", id), http.StatusSeeOther)
	default:
		s.serverError(w, r, "could not load goal", err)
	}
	return domain.Goal{}, false
}

// handleDefineGoalForm renders the define page: the New goal form for a
// Proposed Goal, prefilled from it.
func (s *Server) handleDefineGoalForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	page, err := s.svc.GoalDefinitionPage(r.Context(), g, nil)
	if err != nil {
		s.serverError(w, r, "could not load goal", err)
		return
	}
	v := definitionForm(page)
	s.fillDefinition(current, page, &v)
	render(w, r, http.StatusOK, goalFormPage(&current, v))
}

// definitionForm is the form as the saved Goal on page fills it: its Title,
// Why it matters, Kind and delivery date, cadence, Dimension values and
// Fields.
func definitionForm(page domain.GoalDefinitionPage) goalFormView {
	g := page.Goal
	v := goalFormView{
		Title:        g.Title,
		WhyItMatters: g.WhyItMatters,
		Kind:         g.Kind,
		DeliveryDate: fmtDate(g.DeliveryDate),
		Cadence:      strconv.Itoa(g.CadenceDays),
		Chosen:       map[int64]bool{},
		NewValues:    map[int64]string{},
		FieldValues:  map[int64]string{},
	}
	for _, val := range page.Values {
		v.Chosen[val.ID] = true
	}
	for _, f := range page.FieldValues {
		v.FieldValues[f.Field.ID] = f.Value
	}
	return v
}

// fillDefinition gives the define page's v, for current, the Goal it
// finishes, what it shows of it read-only — its Milestones, Metrics and
// parents, Accepted and Pending — the parents picked and the candidates left
// over, and Where it fits, all out of page. The Title is always the Goal's,
// which can't change here.
func (s *Server) fillDefinition(current domain.Account, page domain.GoalDefinitionPage, v *goalFormView) {
	g := page.Goal
	v.Goal, v.Title = &g, g.Title
	v.Dates, v.Today = s.displayDates(), s.goalFormToday(g)
	v.ExistingMilestones, v.ExistingMetrics = page.Milestones, page.Metrics
	v.Linked = nil
	for _, l := range page.Accepted {
		v.Linked = append(v.Linked, linkedParent{Goal: l.Goal})
	}
	for _, l := range page.Pending {
		v.Linked = append(v.Linked, linkedParent{Goal: l.Goal, Pending: true})
	}
	v.Parents = parentChoicesOf(current, page.Parents, page.ParentHealth)
	v.Candidates = page.Candidates
	v.Dimensions, v.Fields = page.Dimensions, page.Fields
}

// handleDefineGoal submits the define page: the Proposed Goal is changed as
// the form now says, and activated too when Activate Goal posts activate=1,
// and the post lands on its page, as handleCreateDefinedGoal's does. The form
// posts no Title, which can't change here, so the Goal's own is kept. A
// refused submit comes back as the define page, 422, as typed, with every
// problem, and nothing saved.
func (s *Server) handleDefineGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	v, in := readGoalForm(r, current)
	in.Title = g.Title
	in.Activate = r.PostFormValue(domain.InputActivate) == "1"
	_, err := s.svc.DefineGoal(r.Context(), current.ID, g.ID, in)
	if err == nil {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) {
		s.serverError(w, r, "could not save goal", err)
		return
	}
	v.Problems = goalFormProblems(err)
	page, err := s.svc.GoalDefinitionPage(r.Context(), g, in.ParentIDs)
	if err != nil {
		s.serverError(w, r, "could not load goal", err)
		return
	}
	s.fillDefinition(current, page, &v)
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
}

// handleDefineGoalChecklist is handleGoalFormChecklist for the define page,
// whose checklist counts the Goal's Milestones and Metrics with the rows. It
// reads only what the card judges, not the parents the page shows.
func (s *Server) handleDefineGoalChecklist(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	v, _ := readGoalForm(r, current)
	v.Goal = &g
	var err error
	if v.ExistingMilestones, err = s.svc.ListMilestones(r.Context(), g.ID); err != nil {
		s.serverError(w, r, "could not load milestones", err)
		return
	}
	if v.ExistingMetrics, err = s.svc.ListMetrics(r.Context(), g.ID); err != nil {
		s.serverError(w, r, "could not load metrics", err)
		return
	}
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		s.serverError(w, r, "could not load dimensions and fields", err)
		return
	}
	v.Live = true
	render(w, r, http.StatusOK, goalFormReady(v))
}

// handleGoalFormChecklist answers the New goal form as it is typed with its
// Ready to activate card alone, buttons included: the checklist run on the
// form's values, and Create and activate disabled until every item is done.
// It saves nothing.
func (s *Server) handleGoalFormChecklist(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, _ := readGoalForm(r, current)
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		s.serverError(w, r, "could not load dimensions and fields", err)
		return
	}
	v.Live = true
	render(w, r, http.StatusOK, goalFormReady(v))
}

// readGoalForm reads a New goal post: the form as typed, to show again, and
// the definition it posts for current, with each value that doesn't parse
// refused under its input's name (DefinedGoalInput.Unparsed).
func readGoalForm(r *http.Request, current domain.Account) (goalFormView, domain.DefinedGoalInput) {
	v := goalFormView{
		Title:        r.FormValue(domain.InputTitle),
		WhyItMatters: r.FormValue(domain.InputWhyItMatters),
		Kind:         r.FormValue(domain.InputKind),
		DeliveryDate: r.FormValue(domain.InputDeliveryDate),
		Cadence:      r.FormValue(domain.InputCadence),
		CadenceDays:  r.FormValue(inputCadenceDays),
	}
	v.Unsaved = unsavedMeasure(r.PostForm[inputEditing])
	in := domain.DefinedGoalInput{
		Title:        v.Title,
		WhyItMatters: v.WhyItMatters,
		OwnerID:      current.ID,
		Kind:         v.Kind,
	}
	var unparsed formProblems

	// Only a Dated Goal has a delivery date; one posted with any other Kind is
	// ignored, not refused.
	if in.Kind == domain.GoalDated && strings.TrimSpace(v.DeliveryDate) != "" {
		in.DeliveryDate = unparsed.date(domain.InputDeliveryDate, v.DeliveryDate, "")
	}

	// A segment posts its days; Custom posts the days typed beside it. Either is a
	// whole number above 0, so the domain is never sent 0 for a cadence chosen.
	if cadence := strings.TrimSpace(v.Cadence); cadence != "" {
		if cadence == cadenceCustom {
			cadence = strings.TrimSpace(v.CadenceDays)
		}
		days, err := strconv.Atoi(cadence)
		switch {
		case cadence == "":
			unparsed.add(domain.InputCadence, "a Custom cadence needs a number of days")
		case err != nil || days <= 0:
			unparsed.add(domain.InputCadence, "the Check-in cadence must be a whole number of days above 0, not %q", cadence)
		default:
			in.CadenceDays = days
		}
	}

	for _, value := range r.PostForm[inputParentID] {
		if value = strings.TrimSpace(value); value == "" || slices.Contains(r.PostForm[inputRemoveParent], value) {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			unparsed.add(inputParentID, "%q isn't a Goal", value)
			continue
		}
		in.ParentIDs = append(in.ParentIDs, id)
	}

	// Where it fits assigns only what is posted: a value suggested from the
	// parents and unticked isn't.
	v.Chosen = map[int64]bool{}
	for _, value := range r.PostForm[inputValueID] {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			unparsed.add("", "%q isn't a Dimension value", value)
			continue
		}
		v.Chosen[id] = true
		in.ValueIDs = append(in.ValueIDs, id)
	}
	v.NewValues = postedByID(r.PostForm, inputNewValue)
	v.FieldValues = postedByID(r.PostForm, inputField)
	in.NewValues, in.FieldValues = v.NewValues, v.FieldValues

	// Rows are numbered afresh, all-blank ones dropped, so the domain's
	// problems and the page shown again name the same inputs.
	for i, row := range formRows(r.PostForm, "milestones") {
		m := milestoneRow{Name: row["name"], Date: row["date"]}
		v.Milestones = append(v.Milestones, m)
		in.Milestones = append(in.Milestones, domain.MilestoneDefinition{Name: m.Name, Date: unparsed.date(domain.MilestoneInput(i, "date"), m.Date, "")})
	}
	for i, row := range formRows(r.PostForm, "metrics") {
		m := metricRow{Name: row["name"], Unit: row["unit"], Baseline: row["baseline"], Target: row["target"], Direction: row["direction"], TargetDate: row["target_date"]}
		v.Metrics = append(v.Metrics, m)
		baseline, baselineOK := readMetricNumber(&unparsed, domain.MetricInput(i, "baseline"), "baseline", m.Baseline)
		target, targetOK := readMetricNumber(&unparsed, domain.MetricInput(i, "target"), "target", m.Target)
		in.Metrics = append(in.Metrics, domain.MetricDefinition{
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  metricDirection(baseline, target, baselineOK && targetOK, m.Direction),
			Baseline:   baseline,
			Target:     target,
			TargetDate: unparsed.date(domain.MetricInput(i, "target_date"), m.TargetDate, ""),
		})
	}
	in.Unparsed = unparsed
	return v, in
}

// inputEditing is the hidden input each open Milestone or Metric editor holds,
// naming its measure, so the Ready to activate card can ask for it to be saved.
// It never reaches the domain: an editor open at submit is sent like a row.
const inputEditing = "editing"

// goalFormEditorID is the id the first open Milestone or Metric editor carries,
// which the checklist's "Save the … you started" links to.
const goalFormEditorID = "gf-editor"

// unsavedMeasure is the measure the first editing value names, Milestone or
// Metric, or "" when none does.
func unsavedMeasure(editing []string) string {
	if len(editing) > 0 && (editing[0] == "Milestone" || editing[0] == "Metric") {
		return editing[0]
	}
	return ""
}

// Where it fits posts each value chosen as value_id, the value typed to add to
// an Extendable Dimension as new_value:<dimension id> (domain.NewValueInput),
// and each Field's as field:<field id> (domain.FieldInput).
const (
	inputValueID  = "value_id"
	inputNewValue = "new_value:"
	inputField    = "field:"
)

// postedByID gathers the inputs named prefix<id>, by id, leaving out any whose
// id isn't a number.
func postedByID(form url.Values, prefix string) map[int64]string {
	byID := map[int64]string{}
	for name, values := range form {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		if id, err := strconv.ParseInt(rest, 10, 64); err == nil {
			byID[id] = values[0]
		}
	}
	return byID
}

// choices are the values of d Where it fits offers: its live ones, and any
// Retired one chosen, so a refused submit shows what was posted.
func (v goalFormView) choices(d domain.Dimension) []domain.DimensionValue {
	var out []domain.DimensionValue
	for _, val := range d.Values {
		if !val.Retired || v.Chosen[val.ID] {
			out = append(out, val)
		}
	}
	return out
}

// dimensionInput names the select of a Dimension that takes one value. A
// problem with the value chosen there names the value (domain.ValueInput), so
// the select stands for each of its values' inputs.
func dimensionInput(d domain.Dimension) string { return domain.DimensionInput(d.ID) }

// selectInput is the input a refused value of a one-value Dimension is shown
// at: its Dimension's select, or input itself when it is no such value.
func (v goalFormView) selectInput(input string) string {
	for _, d := range v.Dimensions {
		if d.TakesSeveral() {
			continue
		}
		for _, val := range d.Values {
			if domain.ValueInput(val.ID) == input {
				return dimensionInput(d)
			}
		}
	}
	return input
}

// badSelect is why the value chosen in a one-value Dimension's select was
// refused, or "" when it wasn't.
func (v goalFormView) badSelect(d domain.Dimension) string {
	for _, p := range v.Problems {
		if p.Input != "" && v.selectInput(p.Input) == dimensionInput(d) {
			return p.Message
		}
	}
	return ""
}

// invalid marks an input refused, described by why, or nothing when it
// wasn't.
func invalid(bad, input string) templ.Attributes {
	if bad == "" {
		return templ.Attributes{}
	}
	return templ.Attributes{"aria-invalid": "true", "aria-describedby": input + "-error"}
}

// metricDirection is a Metric row's direction: down when the target is below
// the baseline, up when above, and the Direction select's choice when they're
// equal (a hold-steady Metric). When either doesn't parse it can't be told,
// and the row is refused for that alone.
func metricDirection(baseline, target float64, parsed bool, chosen string) string {
	switch {
	case !parsed:
		return domain.MetricUp
	case target < baseline:
		return domain.MetricDown
	case target > baseline:
		return domain.MetricUp
	}
	return chosen
}

// goalFormProblems are a refused submit's problems, those that didn't parse
// among them, in the order the page shows its inputs.
func goalFormProblems(err error) []*domain.InputError {
	problems := problemsOf(err)
	slices.SortStableFunc(problems, func(a, b *domain.InputError) int {
		return slices.Compare(goalFormPlace(a.Input), goalFormPlace(b.Input))
	})
	return problems
}

// goalFormPlace is where an input sits on the New goal page, to sort its
// problems by: Title, Why it matters, Kind, delivery date, cadence, each Milestone
// row's inputs and each Metric row's, in turn, then Contributes to, then Where
// it fits' Dimension values and its Fields. Any other input sorts after those.
func goalFormPlace(input string) []int {
	single := []string{domain.InputTitle, domain.InputWhyItMatters, domain.InputKind, domain.InputDeliveryDate, domain.InputCadence}
	if at := slices.Index(single, input); at >= 0 {
		return []int{at}
	}
	sections := []struct {
		name  string
		parts []string
	}{
		{"milestones", []string{"name", "date"}},
		{"metrics", []string{"name", "unit", "baseline", "target", "direction", "target_date"}},
	}
	for s, section := range sections {
		var i int
		var part string
		if _, err := fmt.Sscanf(input, section.name+"[%d].%s", &i, &part); err == nil {
			return []int{len(single) + s, i, slices.Index(section.parts, part)}
		}
	}
	after := len(single) + len(sections)
	for p, prefixes := range [][]string{
		{inputParentID, "parent:"},
		{"value:", inputNewValue},
		{inputField},
	} {
		for _, prefix := range prefixes {
			if strings.HasPrefix(input, prefix) {
				return []int{after + p}
			}
		}
	}
	return []int{after + 3}
}

// formRows gathers a repeatable section's rows, posted as section[i].part,
// in the order of their i, leaving out each row whose every part is blank. A
// row's i is only its place: rows added and removed by script leave gaps.
func formRows(form url.Values, section string) []map[string]string {
	byIndex := map[int]map[string]string{}
	for name, values := range form {
		rest, ok := strings.CutPrefix(name, section+"[")
		if !ok {
			continue
		}
		index, part, ok := strings.Cut(rest, "].")
		if !ok {
			continue
		}
		i, err := strconv.Atoi(index)
		if err != nil || i < 0 {
			continue
		}
		if byIndex[i] == nil {
			byIndex[i] = map[string]string{}
		}
		byIndex[i][part] = values[0]
	}
	var rows []map[string]string
	for _, i := range slices.Sorted(maps.Keys(byIndex)) {
		row := byIndex[i]
		blank := true
		for _, value := range row {
			if strings.TrimSpace(value) != "" {
				blank = false
			}
		}
		if !blank {
			rows = append(rows, row)
		}
	}
	return rows
}
