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

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalFormView is the New goal page: what was typed and, after a refused
// submit, each problem. A problem names the input it refuses by its form name
// — the name the domain's InputError carries — or none when it is the whole
// submit's.
type goalFormView struct {
	Title  string
	SoWhat string
	// Kind is the Kind chosen, "" for not chosen, and DeliveryDate the date
	// typed, which counts only for a Dated Goal.
	Kind         string
	DeliveryDate string
	// Cadence is the cadence chip chosen, its days or cadenceCustom, and
	// CadenceDays the days typed for Custom.
	Cadence     string
	CadenceDays string
	Milestones []milestoneRow
	Metrics    []metricRow
	// Parents are the Goals picked to contribute to, and Candidates every
	// other Goal, which the no-script select offers.
	Parents    []parentChoice
	Candidates []domain.Goal
	Problems   []*domain.InputError
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
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, ids); err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, goalFormPage(&current, v))
}

// pickParents splits every Goal into the parents picked by ids, in that
// order, each once, and the candidates left over. An id that is no Goal picks
// nothing. A brand-new Goal has no children, so no Goal is left out for
// making a cycle.
func (s *Server) pickParents(ctx context.Context, current domain.Account, ids []int64) ([]parentChoice, []domain.Goal, error) {
	all, err := s.svc.ListGoals(ctx)
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
// picked, which come along as parent_id. Each result carries the chip picking
// it adds.
func (s *Server) handleSearchGoals(w http.ResponseWriter, r *http.Request, current domain.Account) {
	var picked []int64
	for _, value := range r.URL.Query()[inputParentID] {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			picked = append(picked, id)
		}
	}
	_, candidates, err := s.pickParents(r.Context(), current, picked)
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
// defined and the post lands on its page, which is the confirmation, so there
// is no toast. A refused submit comes back as the form, 422, as typed, with
// every problem listed at the top and marked on its input. A value the
// handler can't parse is refused the same way, under its input's name, in the
// same 422 as the domain's problems.
func (s *Server) handleCreateDefinedGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
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
	if v.Parents, v.Candidates, err = s.pickParents(r.Context(), current, in.ParentIDs); err != nil {
		http.Error(w, "could not load goals", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, goalFormPage(&current, v))
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
// problems by: Title, So What, Kind, delivery date, cadence, then each
// Milestone row's inputs and each Metric row's, in turn. Any other input sorts
// after those.
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
	return []int{len(single) + len(sections)}
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
