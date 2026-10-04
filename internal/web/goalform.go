package web

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
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
	Linked             []linkedParent
	Title              string
	SoWhat             string
	// Kind is the Kind chosen, "" for not chosen, and DeliveryDate the date
	// typed, which counts only for a Dated Goal.
	Kind         string
	DeliveryDate string
	// Cadence is the cadence chip chosen, its days or cadenceCustom, and
	// CadenceDays the days typed for Custom.
	Cadence     string
	CadenceDays string
	Milestones  []milestoneRow
	Metrics     []metricRow
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

// activationFacts are the form's, as typed, for its Ready to activate
// checklist. Its Owner is the person creating it. Only a Dated Goal's
// delivery date counts, and only one that parses; a Milestone or Metric row
// counts once anything is typed in it. A required Dimension is set by a value
// chosen in it or, when it's Extendable, a value typed to add, and a required
// Field by a value typed (domain.RequiredValues reads a saved Goal's).
func (v goalFormView) activationFacts() activationFacts {
	f := activationFacts{
		SoWhat:     v.SoWhat,
		Owned:      true,
		Kind:       v.Kind,
		Milestones: len(v.ExistingMilestones) + len(v.Milestones),
		Metrics:    len(v.ExistingMetrics) + len(v.Metrics),
	}
	if v.Kind == domain.GoalDated {
		if d, err := parseDate(strings.TrimSpace(v.DeliveryDate)); err == nil {
			f.DeliveryDate = d
		}
	}
	for _, d := range v.Dimensions {
		if !d.Required || d.Retired {
			continue
		}
		set := d.Extendable() && strings.TrimSpace(v.NewValues[d.ID]) != ""
		for _, val := range d.Values {
			set = set || v.Chosen[val.ID]
		}
		f.Required = append(f.Required, domain.RequiredValue{Name: d.Name, Set: set})
	}
	for _, fl := range v.Fields {
		if fl.Required && !fl.Retired {
			f.Required = append(f.Required, domain.RequiredValue{Name: fl.Name, Set: strings.TrimSpace(v.FieldValues[fl.ID]) != ""})
		}
	}
	return f
}

// readyCount is how many of the Ready to activate checklist's items are done,
// of how many.
func (v goalFormView) readyCount() (done, of int) {
	items := v.activationFacts().checklist()
	for _, item := range items {
		if item.Done {
			done++
		}
	}
	return done, len(items)
}

// activateDisabled says Create and activate is disabled: only on the live
// card, while the checklist is incomplete.
func (v goalFormView) activateDisabled() bool {
	return v.Live && !v.activationFacts().ready()
}

// parentChoice is a Goal offered or picked to contribute to: its Health, ""
// when it has none, and the label of its Owner when picking it asks that
// Owner to accept, "" when the creator owns it and it links at once.
type parentChoice struct {
	Goal   domain.Goal
	Health string
	Asks   string
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
	var v goalFormView
	var err error
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, ids, 0); err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		http.Error(w, "could not load dimensions and fields", http.StatusInternalServerError)
		return
	}
	if v.Chosen, err = s.suggestedValues(r.Context(), v.Parents, v.Dimensions); err != nil {
		http.Error(w, "could not load parent goals' values", http.StatusInternalServerError)
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

// suggestedValues are the values a Goal contributing to parents is offered
// ticked: every value the parents carry that can still be newly assigned, so
// neither a Retired value nor one in a Retired Dimension (CONTEXT.md: the
// parent's values are offered as defaults, not inherited). In a Dimension that takes one
// value, parents carrying different ones suggest none there.
func (s *Server) suggestedValues(ctx context.Context, parents []parentChoice, dims []domain.Dimension) (map[int64]bool, error) {
	suggested := map[int64]bool{}
	inDimension := map[int64]map[int64]bool{}
	for _, p := range parents {
		values, err := s.svc.GoalValues(ctx, p.Goal.ID)
		if err != nil {
			return nil, fmt.Errorf("load parent goal's values: %w", err)
		}
		for _, val := range values {
			if val.Retired || val.DimensionRetired {
				continue
			}
			suggested[val.ID] = true
			if inDimension[val.DimensionID] == nil {
				inDimension[val.DimensionID] = map[int64]bool{}
			}
			inDimension[val.DimensionID][val.ID] = true
		}
	}
	for _, d := range dims {
		if ids := inDimension[d.ID]; !d.TakesSeveral() && len(ids) > 1 {
			for id := range ids {
				delete(suggested, id)
			}
		}
	}
	return suggested, nil
}

// pickParents splits the Goals the Goal goalID could contribute to into the
// parents picked by ids, in that order, each once, and the candidates left
// over. An id that is no such Goal picks nothing. A brand-new Goal, goalID 0,
// has no children or parents, so it could contribute to every Goal; a saved
// one to its domain.ParentCandidates.
func (s *Server) pickParents(ctx context.Context, current domain.Account, ids []int64, goalID int64) ([]parentChoice, []domain.Goal, error) {
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
	byID := map[int64]domain.Goal{}
	for _, g := range all {
		byID[g.ID] = g
	}
	var picked []parentChoice
	for _, id := range ids {
		g, ok := byID[id]
		if !ok {
			continue
		}
		delete(byID, id)
		p, err := s.parentChoice(ctx, current, g)
		if err != nil {
			return nil, nil, err
		}
		picked = append(picked, p)
	}
	var candidates []domain.Goal
	for _, g := range all {
		if _, left := byID[g.ID]; left {
			candidates = append(candidates, g)
		}
	}
	return picked, candidates, nil
}

// parentChoice is g offered to current to contribute to: its Health, read as
// the Goal page's sidebar reads a linked Goal's, from an Active Goal's latest
// Check-in, and whom picking it asks.
func (s *Server) parentChoice(ctx context.Context, current domain.Account, g domain.Goal) (parentChoice, error) {
	p := parentChoice{Goal: g}
	if g.Owner.ID != current.ID {
		p.Asks = g.Owner.Label()
	}
	if g.Lifecycle != domain.LifecycleActive {
		return p, nil
	}
	c, ok, err := s.svc.LatestCheckin(ctx, g.ID)
	if err != nil {
		return parentChoice{}, fmt.Errorf("load parent goal's check-in: %w", err)
	}
	if ok {
		p.Health = c.Health
	}
	return p, nil
}

// inputParentID is the Contributes to field's name, posted once per parent
// Goal picked.
const inputParentID = "parent_id"

// cadenceChips are the Check-in cadence's chips, each posting its days but
// Custom, which posts cadenceCustom and the days typed beside it.
var cadenceChips = []struct{ Value, Label string }{
	{"7", "Weekly"},
	{"14", "Every 2 weeks"},
	{"30", "Monthly"},
	{cadenceCustom, "Custom…"},
}

// cadenceChosen is the cadence chip to show chosen: Weekly at first, and
// Custom for days posted that no other chip posts.
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
// days posted that no other chip posts.
func (v goalFormView) customDays() string {
	if v.cadenceChosen() == cadenceCustom && v.Cadence != cadenceCustom {
		return v.Cadence
	}
	return v.CadenceDays
}

// inputCadenceDays is the number of days the Custom cadence chip reveals.
// cadenceCustom is that chip's value.
const (
	inputCadenceDays = "cadence_days"
	cadenceCustom    = "custom"
)

// handleSearchGoals answers the Contributes to search as it is typed: each
// candidate whose title contains q, ignoring case, less the parents already
// picked, which come along as parent_id. On the define page goal names the
// Goal being finished, and the candidates are those it could contribute to.
// Each result carries the chip picking it adds.
func (s *Server) handleSearchGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	var picked []int64
	for _, value := range r.URL.Query()[inputParentID] {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			picked = append(picked, id)
		}
	}
	goalID, _ := strconv.ParseInt(r.URL.Query().Get(searchGoal), 10, 64)
	_, candidates, err := s.pickParents(r.Context(), current, picked, goalID)
	if err != nil {
		http.Error(w, "could not search goals", http.StatusInternalServerError)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var matches []parentChoice
	for _, g := range candidates {
		if !strings.Contains(strings.ToLower(g.Title), q) {
			continue
		}
		p, err := s.parentChoice(r.Context(), current, g)
		if err != nil {
			http.Error(w, "could not search goals", http.StatusInternalServerError)
			return
		}
		matches = append(matches, p)
	}
	render(w, r, http.StatusOK, goalSearchResults(matches))
}

// errUnparsed rolls back a New goal submit the domain accepted when the
// handler refused some of its values.
var errUnparsed = errors.New("the New goal form has values that don't parse")

// handleCreateDefinedGoal submits the New goal page: the Goal is created as
// defined, and activated too when Create and activate posts activate=1, and
// the post lands on its page, which is the confirmation, so there is no toast.
// The server decides activation, whatever the button showed. A refused submit,
// activation included, comes back as the form, 422, as typed, with every
// problem listed at the top and marked on its input, and nothing saved. A
// value the handler can't parse is refused the same way, under its input's
// name, in the same 422 as the domain's problems.
func (s *Server) handleCreateDefinedGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v, in, unparsed := readGoalForm(r, current)
	in.Activate = r.PostFormValue(domain.InputActivate) == "1"

	// The domain checks what parsed even when something didn't, so every
	// problem comes back together; with any unparsed, nothing it wrote is
	// kept. Defining a Goal sends no email, so rolling it back recalls it all.
	var g domain.Goal
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		var err error
		g, err = tx.CreateDefinedGoal(r.Context(), in)
		if err == nil && len(unparsed) > 0 {
			return errUnparsed
		}
		return err
	})
	if err == nil {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) && !errors.Is(err, errUnparsed) {
		http.Error(w, "could not create goal", http.StatusInternalServerError)
		return
	}
	v.Problems = goalFormProblems(err, unparsed)
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, in.ParentIDs, 0); err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		http.Error(w, "could not load dimensions and fields", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
}

// searchGoal is the Contributes to search's parameter naming the Goal being
// finished on the define page.
const searchGoal = "goal"

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

// linkedParent is a Goal the Goal being finished already contributes to, or
// has asked to and is waiting on: Pending.
type linkedParent struct {
	Goal    domain.Goal
	Pending bool
}

// definableGoal is the Goal in the path, for its define page: one current
// owns that is still Proposed. Otherwise it writes the answer — Not found,
// 403 for anyone but the Owner, and a redirect to the Goal's page once it is
// no longer Proposed — and ok is false.
func (s *Server) definableGoal(w http.ResponseWriter, r *http.Request, current domain.Account) (g domain.Goal, ok bool) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return domain.Goal{}, false
	}
	g, err := s.svc.ViewGoal(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return domain.Goal{}, false
	} else if err != nil {
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return domain.Goal{}, false
	}
	if g.Owner.ID != current.ID {
		http.Error(w, "only the Goal's Owner may finish defining it", http.StatusForbidden)
		return domain.Goal{}, false
	}
	if g.Lifecycle != domain.LifecycleProposed {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return domain.Goal{}, false
	}
	return g, true
}

// handleDefineGoalForm renders the define page: the New goal form for a
// Proposed Goal, prefilled from it.
func (s *Server) handleDefineGoalForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	v, err := s.definitionForm(r.Context(), g)
	if err != nil {
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	if err := s.loadDefinition(r.Context(), current, g, &v, nil); err != nil {
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalFormPage(&current, v))
}

// definitionForm is the form as the saved Goal g fills it: its Title, So
// What, Kind and delivery date, cadence, Dimension values and Fields.
func (s *Server) definitionForm(ctx context.Context, g domain.Goal) (goalFormView, error) {
	v := goalFormView{
		Title:        g.Title,
		SoWhat:       g.SoWhat,
		Kind:         g.Kind,
		DeliveryDate: fmtDate(g.DeliveryDate),
		Cadence:      strconv.Itoa(g.CadenceDays),
		Chosen:       map[int64]bool{},
		NewValues:    map[int64]string{},
		FieldValues:  map[int64]string{},
	}
	values, err := s.svc.GoalValues(ctx, g.ID)
	if err != nil {
		return goalFormView{}, fmt.Errorf("load goal values: %w", err)
	}
	for _, val := range values {
		v.Chosen[val.ID] = true
	}
	fields, err := s.svc.GoalFields(ctx, g.ID)
	if err != nil {
		return goalFormView{}, fmt.Errorf("load goal fields: %w", err)
	}
	for _, f := range fields {
		v.FieldValues[f.Field.ID] = f.Value
	}
	return v, nil
}

// loadDefinition gives the define page's v the Goal g it finishes, what it
// shows of g read-only — its Milestones, Metrics and parents, Accepted and
// Pending — the parents picked by ids and the candidates left over, and Where
// it fits. The Title is always g's, which can't change here.
func (s *Server) loadDefinition(ctx context.Context, current domain.Account, g domain.Goal, v *goalFormView, ids []int64) error {
	v.Goal, v.Title = &g, g.Title
	var err error
	if v.ExistingMilestones, err = s.svc.ListMilestones(ctx, g.ID); err != nil {
		return fmt.Errorf("load milestones: %w", err)
	}
	if v.ExistingMetrics, err = s.svc.ListMetrics(ctx, g.ID); err != nil {
		return fmt.Errorf("load metrics: %w", err)
	}
	accepted, err := s.svc.ParentLinks(ctx, g.ID)
	if err != nil {
		return fmt.Errorf("load parents: %w", err)
	}
	pending, err := s.svc.PendingParentLinks(ctx, g.ID)
	if err != nil {
		return fmt.Errorf("load pending parents: %w", err)
	}
	v.Linked = nil
	for _, l := range accepted {
		v.Linked = append(v.Linked, linkedParent{Goal: l.Goal})
	}
	for _, l := range pending {
		v.Linked = append(v.Linked, linkedParent{Goal: l.Goal, Pending: true})
	}
	if v.Parents, v.Candidates, err = s.pickParents(ctx, current, ids, g.ID); err != nil {
		return err
	}
	return s.loadWhereItFits(ctx, v)
}

// handleDefineGoal submits the define page: the Proposed Goal is changed as
// the form now says, and activated too when Create and activate posts
// activate=1, and the post lands on its page, as handleCreateDefinedGoal's
// does. A refused submit comes back as the define page, 422, as typed, with
// every problem, and nothing saved.
func (s *Server) handleDefineGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	v, in, unparsed := readGoalForm(r, current)
	in.Activate = r.PostFormValue(domain.InputActivate) == "1"
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		_, err := tx.DefineGoal(r.Context(), current.ID, g.ID, in)
		if err == nil && len(unparsed) > 0 {
			return errUnparsed
		}
		return err
	})
	if err == nil {
		http.Redirect(w, r, fmt.Sprintf("/goals/%d", g.ID), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) && !errors.Is(err, errUnparsed) {
		http.Error(w, "could not save goal", http.StatusInternalServerError)
		return
	}
	v.Problems = goalFormProblems(err, unparsed)
	if err := s.loadDefinition(r.Context(), current, g, &v, in.ParentIDs); err != nil {
		http.Error(w, "could not load goal", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
}

// handleDefineGoalChecklist is handleGoalFormChecklist for the define page,
// whose checklist counts the Goal's Milestones and Metrics with the rows.
func (s *Server) handleDefineGoalChecklist(w http.ResponseWriter, r *http.Request, current domain.Account) {
	g, ok := s.definableGoal(w, r, current)
	if !ok {
		return
	}
	v, _, _ := readGoalForm(r, current)
	if err := s.loadDefinition(r.Context(), current, g, &v, nil); err != nil {
		http.Error(w, "could not load goal", http.StatusInternalServerError)
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
	v, _, _ := readGoalForm(r, current)
	if err := s.loadWhereItFits(r.Context(), &v); err != nil {
		http.Error(w, "could not load dimensions and fields", http.StatusInternalServerError)
		return
	}
	v.Live = true
	render(w, r, http.StatusOK, goalFormReady(v))
}

// readGoalForm reads a New goal post: the form as typed, to show again, the
// definition it posts for current, and each value that doesn't parse, refused
// under its input's name.
func readGoalForm(r *http.Request, current domain.Account) (goalFormView, domain.DefinedGoalInput, []*domain.InputError) {
	v := goalFormView{
		Title:        r.FormValue(domain.InputTitle),
		SoWhat:       r.FormValue(domain.InputSoWhat),
		Kind:         r.FormValue(domain.InputKind),
		DeliveryDate: r.FormValue(domain.InputDeliveryDate),
		Cadence:      r.FormValue(domain.InputCadence),
		CadenceDays:  r.FormValue(inputCadenceDays),
	}
	in := domain.DefinedGoalInput{
		Title:   v.Title,
		SoWhat:  v.SoWhat,
		OwnerID: current.ID,
		Kind:    v.Kind,
	}
	var unparsed []*domain.InputError
	refuse := func(input, format string, args ...any) {
		unparsed = append(unparsed, &domain.InputError{Input: input, Message: fmt.Sprintf(format, args...)})
	}
	date := func(input, value string) time.Time {
		d, err := parseDate(strings.TrimSpace(value))
		if err != nil {
			refuse(input, "%q isn't a date", value)
		}
		return d
	}
	number := func(input, what, value string) (float64, bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			refuse(input, "a Metric needs a %s", what)
			return 0, false
		}
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			refuse(input, "the %s must be a number, not %q", what, value)
			return 0, false
		}
		return n, true
	}

	// Only a Dated Goal has a delivery date; one posted with any other Kind is
	// ignored, not refused.
	if in.Kind == domain.GoalDated && strings.TrimSpace(v.DeliveryDate) != "" {
		in.DeliveryDate = date(domain.InputDeliveryDate, v.DeliveryDate)
	}

	// A chip posts its days; Custom posts the days typed beside it. Either is a
	// whole number above 0, so the domain is never sent 0 for a cadence chosen.
	if cadence := strings.TrimSpace(v.Cadence); cadence != "" {
		if cadence == cadenceCustom {
			cadence = strings.TrimSpace(v.CadenceDays)
		}
		days, err := strconv.Atoi(cadence)
		switch {
		case cadence == "":
			refuse(domain.InputCadence, "a Custom cadence needs a number of days")
		case err != nil || days <= 0:
			refuse(domain.InputCadence, "the Check-in cadence must be a whole number of days above 0, not %q", cadence)
		default:
			in.CadenceDays = days
		}
	}

	for _, value := range r.PostForm[inputParentID] {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			refuse(inputParentID, "%q isn't a Goal", value)
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
			refuse("", "%q isn't a Dimension value", value)
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
		in.Milestones = append(in.Milestones, domain.MilestoneDefinition{Name: m.Name, Date: date(domain.MilestoneInput(i, "date"), m.Date)})
	}
	for i, row := range formRows(r.PostForm, "metrics") {
		m := metricRow{Name: row["name"], Unit: row["unit"], Baseline: row["baseline"], Target: row["target"], Direction: row["direction"], TargetDate: row["target_date"]}
		v.Metrics = append(v.Metrics, m)
		baseline, baselineOK := number(domain.MetricInput(i, "baseline"), "baseline", m.Baseline)
		target, targetOK := number(domain.MetricInput(i, "target"), "target", m.Target)
		in.Metrics = append(in.Metrics, domain.MetricDefinition{
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  metricDirection(baseline, target, baselineOK && targetOK, m.Direction),
			Baseline:   baseline,
			Target:     target,
			TargetDate: date(domain.MetricInput(i, "target_date"), m.TargetDate),
		})
	}
	return v, in, unparsed
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
func dimensionInput(d domain.Dimension) string { return fmt.Sprintf("dimension:%d", d.ID) }

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

// goalFormProblems are a refused submit's problems in the order the page
// shows its inputs: the domain's, each displaced by the handler's under the
// same input, since a value that didn't parse reached the domain as blank.
func goalFormProblems(err error, unparsed []*domain.InputError) []*domain.InputError {
	refused := map[string]bool{}
	for _, p := range unparsed {
		refused[p.Input] = true
	}
	var problems []*domain.InputError
	for _, p := range domain.InputErrors(err) {
		if !refused[p.Input] {
			problems = append(problems, p)
		}
	}
	problems = append(problems, unparsed...)
	if len(problems) == 0 {
		problems = []*domain.InputError{{Message: strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")}}
	}
	slices.SortStableFunc(problems, func(a, b *domain.InputError) int {
		return slices.Compare(goalFormPlace(a.Input), goalFormPlace(b.Input))
	})
	return problems
}

// goalFormPlace is where an input sits on the New goal page, to sort its
// problems by: Title, So What, Kind, delivery date, cadence, each Milestone
// row's inputs and each Metric row's, in turn, then Contributes to, then Where
// it fits' Dimension values and its Fields. Any other input sorts after those.
func goalFormPlace(input string) []int {
	single := []string{domain.InputTitle, domain.InputSoWhat, domain.InputKind, domain.InputDeliveryDate, domain.InputCadence}
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
