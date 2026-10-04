package web

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// reportBuilderView is the builder page for a new Report Definition: what was
// typed and picked, the problems a refused save found, and the choices it
// offers (CONTEXT.md: Report Definition).
type reportBuilderView struct {
	Name         string
	Introduction string
	// Mode is domain.ReportModeRules or domain.ReportModePicked.
	Mode string
	// Rules are the rule rows, as typed.
	Rules []reportRuleRow
	// PickedIDs, IncludeIDs and ExcludeIDs are the Goals picked by hand, to
	// Also include and to Leave out, each in the order listed; Picked, Include
	// and Exclude are those Goals.
	PickedIDs, IncludeIDs, ExcludeIDs []int64
	Picked, Include, Exclude          []domain.Goal
	// FieldIDs are the Fields chosen to show beside each Goal.
	FieldIDs []int64
	// Problems are a refused save's problems, each naming its input.
	Problems []*domain.InputError

	// Attributes are what a rule can test, each with the values it offers.
	Attributes []ruleAttribute
	// Goals are every Goal, at any level, the pickers offer.
	Goals []domain.Goal
	// Fields are the Fields still offered to show.
	Fields []domain.Field
	// Matches are the Goals the definition would select, shown after Show
	// matches, and nil before.
	Matches []reportGoal
}

// reportRuleRow is one rule row as the form posts it: its attribute's key, its
// operator, and its values, each as its option posts it.
type reportRuleRow struct {
	Attribute string
	Op        string
	Values    []string
}

// ruleAttribute is an attribute a rule can test: its key, as the attribute
// select posts it, its label, and the values it offers.
type ruleAttribute struct {
	Key    string
	Label  string
	Values []ruleValue
}

// ruleValue is a value a rule can test for: Value is what its option posts,
// the attribute's key and the domain's value joined by "=", so a value
// carries the attribute it belongs to.
type ruleValue struct {
	Value string
	Label string
}

// The builder's Goal lists, each posted once per Goal in it.
const (
	inputPicked  = "picked"
	inputInclude = "include"
	inputExclude = "exclude"
)

// inputShowField is a Field to show beside each Goal, posted once per Field.
const inputShowField = "field"

// dimensionAttribute is the key of a rule testing the Dimension dimID.
func dimensionAttribute(dimID int64) string {
	return "dimension:" + strconv.FormatInt(dimID, 10)
}

// handleNewReportForm shows the builder for a new Report Definition, selecting
// by rules until the author chooses to pick Goals by hand.
func (s *Server) handleNewReportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v := reportBuilderView{Mode: domain.ReportModeRules}
	if err := s.loadReportBuilder(r, &v); err != nil {
		http.Error(w, "could not load the builder", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, reportBuilderPage(&current, v))
}

// handleNewReport saves the builder's Report Definition and lands on its
// draft. A refused save comes back as the builder, 422, as typed, with each
// problem beside its input. Add rule, a rule row's × and Show matches save
// nothing: the builder comes back as typed with a blank row added, without
// that row, or listing the Goals its first draft would.
func (s *Server) handleNewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	removing := r.PostForm.Has(inputRemoveRule)
	if removing {
		dropRuleRow(r.PostForm, r.PostForm.Get(inputRemoveRule))
	}
	v, in, unparsed := readReportBuilder(r)
	do := r.PostFormValue("do")
	if do == "add-rule" || do == "show-matches" || removing {
		if do == "add-rule" {
			v.Rules = append(v.rows(), reportRuleRow{})
		}
		if err := s.loadReportBuilder(r, &v); err != nil {
			http.Error(w, "could not load the builder", http.StatusInternalServerError)
			return
		}
		if do == "show-matches" {
			if err := s.showMatches(r, &v, in); err != nil {
				http.Error(w, "could not find the matches", http.StatusInternalServerError)
				return
			}
		}
		render(w, r, http.StatusOK, reportBuilderPage(&current, v))
		return
	}
	// The domain checks what parsed even when something didn't, so every
	// problem comes back together; with any unparsed, nothing it saved is
	// kept.
	var def domain.ReportDefinition
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		var err error
		def, err = tx.SaveReportDefinition(r.Context(), current.ID, in)
		if err == nil && len(unparsed) > 0 {
			return errUnparsedReport
		}
		return err
	})
	if err == nil {
		http.Redirect(w, r, "/reports/"+strconv.FormatInt(def.ID, 10), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) && !errors.Is(err, errUnparsedReport) {
		http.Error(w, "could not save the report", http.StatusInternalServerError)
		return
	}
	v.Problems = reportBuilderProblems(err, unparsed)
	if err := s.loadReportBuilder(r, &v); err != nil {
		http.Error(w, "could not load the builder", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, reportBuilderPage(&current, v))
}

// handleSearchReportGoals answers a builder picker's search as it is typed:
// every Goal, at any level, whose title contains q, ignoring case, less those
// already in the picker's list, which come along under the list's name. Each
// result carries the chip picking it adds to that list.
func (s *Server) handleSearchReportGoals(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	list := r.URL.Query().Get("list")
	if !slices.Contains([]string{inputPicked, inputInclude, inputExclude}, list) {
		http.Error(w, "unknown Goal list", http.StatusBadRequest)
		return
	}
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		http.Error(w, "could not search goals", http.StatusInternalServerError)
		return
	}
	var listed []int64
	for _, raw := range r.URL.Query()[list] {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			listed = append(listed, id)
		}
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var matches []domain.Goal
	for _, g := range goals {
		if !slices.Contains(listed, g.ID) && strings.Contains(strings.ToLower(g.Title), q) {
			matches = append(matches, g)
		}
	}
	render(w, r, http.StatusOK, reportGoalSearchResults(list, matches))
}

// errUnparsedReport rolls back a builder save the domain accepted when the
// handler refused some of its values.
var errUnparsedReport = errors.New("the builder has values that don't parse")

// readReportBuilder reads the builder's form as typed, both modes' inputs
// kept, and the definition it saves: only what the chosen mode selects by.
// unparsed refuses each rule whose values don't belong to its attribute.
func readReportBuilder(r *http.Request) (v reportBuilderView, in domain.SaveReportDefinitionInput, unparsed []*domain.InputError) {
	v = reportBuilderView{
		Name:         r.PostFormValue("name"),
		Introduction: r.PostFormValue("introduction"),
		Mode:         r.PostFormValue("mode"),
		Rules:        reportRuleRows(r.PostForm),
		PickedIDs:    dedupeInt64s(formInt64s(r, inputPicked)),
		IncludeIDs:   dedupeInt64s(formInt64s(r, inputInclude)),
		ExcludeIDs:   dedupeInt64s(formInt64s(r, inputExclude)),
		FieldIDs:     formInt64s(r, inputShowField),
	}
	in = domain.SaveReportDefinitionInput{Name: v.Name, Introduction: v.Introduction, Mode: v.Mode, FieldIDs: v.FieldIDs}
	switch v.Mode {
	case domain.ReportModePicked:
		in.Picked = v.PickedIDs
	case domain.ReportModeRules:
		in.Include, in.Exclude = v.IncludeIDs, v.ExcludeIDs
		for i, row := range v.Rules {
			rule, ok := row.rule()
			if !ok {
				unparsed = append(unparsed, &domain.InputError{Input: domain.RuleInput(i), Message: "a rule's values must be values of what it tests"})
			}
			in.Rules = append(in.Rules, rule)
		}
	}
	return v, in, unparsed
}

// inputRemoveRule is a rule row's ×, posting the row's i.
const inputRemoveRule = "remove-rule"

// dropRuleRow removes the rule row posted as rules[i] from form.
func dropRuleRow(form url.Values, i string) {
	for name := range form {
		if strings.HasPrefix(name, "rules["+i+"].") {
			delete(form, name)
		}
	}
}

// reportRuleRows gathers the rule rows, posted as rules[i].attribute,
// rules[i].op and rules[i].value, in the order of their i, leaving out each
// row with neither an attribute nor a value. A row's i is only its place.
func reportRuleRows(form url.Values) []reportRuleRow {
	byIndex := map[int]*reportRuleRow{}
	for name, values := range form {
		rest, ok := strings.CutPrefix(name, "rules[")
		if !ok {
			continue
		}
		index, part, ok := strings.Cut(rest, "].")
		i, err := strconv.Atoi(index)
		if !ok || err != nil || i < 0 {
			continue
		}
		if byIndex[i] == nil {
			byIndex[i] = &reportRuleRow{}
		}
		switch part {
		case "attribute":
			byIndex[i].Attribute = values[0]
		case "op":
			byIndex[i].Op = values[0]
		case "value":
			byIndex[i].Values = values
		}
	}
	var rows []reportRuleRow
	for _, i := range slices.Sorted(maps.Keys(byIndex)) {
		if row := byIndex[i]; row.Attribute != "" || len(row.Values) > 0 {
			rows = append(rows, *row)
		}
	}
	return rows
}

// rule is the Report rule the row posts, and whether each of its values
// belongs to its attribute; one that doesn't is left out of the rule.
func (row reportRuleRow) rule() (domain.ReportRule, bool) {
	rule := domain.ReportRule{Attribute: row.Attribute, Op: row.Op}
	if raw, ok := strings.CutPrefix(row.Attribute, "dimension:"); ok {
		rule.Attribute = domain.RuleDimension
		// An id that doesn't parse names no Dimension, which saving refuses.
		rule.DimensionID, _ = strconv.ParseInt(raw, 10, 64)
	}
	belongs := true
	for _, posted := range row.Values {
		value, ok := strings.CutPrefix(posted, row.Attribute+"=")
		if !ok || row.Attribute == "" {
			belongs = false
			continue
		}
		rule.Values = append(rule.Values, value)
	}
	return rule, belongs
}

// dedupeInt64s is ids without repeats, in first-seen order: a Goal both
// chipped and chosen in the no-script select posts twice.
func dedupeInt64s(ids []int64) []int64 {
	var out []int64
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// loadReportBuilder fills in the choices the builder offers, and the listed
// Goals as chips from their ids.
func (s *Server) loadReportBuilder(r *http.Request, v *reportBuilderView) error {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return err
	}
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		return err
	}
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		return err
	}
	v.Fields = domain.OfferedFields(fields)
	v.Goals = goals
	v.Picked = goalsByID(goals, v.PickedIDs)
	v.Include = goalsByID(goals, v.IncludeIDs)
	v.Exclude = goalsByID(goals, v.ExcludeIDs)
	v.Attributes = ruleAttributes(domain.OfferedDimensions(dims), distinctOwners(goals))
	return nil
}

// ruleAttributes are what a new rule can test: each Dimension given and its
// values, but a Retired value, then Owner, Lifecycle, Health and Top-level.
// Fields are not among them: they describe a Goal and never select it (ADR
// 0005).
func ruleAttributes(dims []domain.Dimension, owners []domain.Account) []ruleAttribute {
	var out []ruleAttribute
	attribute := func(key, label string, values [][2]string) {
		a := ruleAttribute{Key: key, Label: label}
		for _, v := range values {
			a.Values = append(a.Values, ruleValue{Value: key + "=" + v[0], Label: v[1]})
		}
		out = append(out, a)
	}
	for _, d := range dims {
		var values [][2]string
		for _, v := range d.Values {
			if !v.Retired {
				values = append(values, [2]string{strconv.FormatInt(v.ID, 10), v.Value})
			}
		}
		attribute(dimensionAttribute(d.ID), d.Name, values)
	}
	var people [][2]string
	for _, o := range owners {
		people = append(people, [2]string{strconv.FormatInt(o.ID, 10), o.Label()})
	}
	attribute(domain.RuleOwner, "Owner", people)
	var lifecycles, healths [][2]string
	for _, l := range []string{domain.LifecycleProposed, domain.LifecycleActive, domain.LifecycleOnHold, domain.LifecycleDone, domain.LifecycleCancelled} {
		lifecycles = append(lifecycles, [2]string{l, l})
	}
	for _, h := range []string{domain.HealthGreen, domain.HealthYellow, domain.HealthRed} {
		healths = append(healths, [2]string{h, h})
	}
	attribute(domain.RuleLifecycle, "Lifecycle", lifecycles)
	attribute(domain.RuleHealth, "Health", healths)
	attribute(domain.RuleTopLevel, "Top-level", nil)
	return out
}

// showMatches lists the Goals the definition in would select on its first
// draft, read against the default baseline. A Goal listed that doesn't exist
// selects nothing.
func (s *Server) showMatches(r *http.Request, v *reportBuilderView, in domain.SaveReportDefinitionInput) error {
	ids := func(goals []domain.Goal) []int64 {
		out := make([]int64, 0, len(goals))
		for _, g := range goals {
			out = append(out, g.ID)
		}
		return out
	}
	def := domain.ReportDefinition{Mode: in.Mode, Rules: in.Rules}
	switch in.Mode {
	case domain.ReportModePicked:
		def.Picked = ids(v.Picked)
	case domain.ReportModeRules:
		def.Include, def.Exclude = ids(v.Include), ids(v.Exclude)
	}
	report, err := s.svc.DraftReport(r.Context(), def, time.Time{})
	if err != nil {
		return err
	}
	v.Matches = reportGoals(report)
	if v.Matches == nil {
		v.Matches = []reportGoal{}
	}
	return nil
}

// goalsByID are the Goals with the given ids, in the order given, leaving out
// any id no Goal has.
func goalsByID(goals []domain.Goal, ids []int64) []domain.Goal {
	var out []domain.Goal
	for _, id := range ids {
		if at := slices.IndexFunc(goals, func(g domain.Goal) bool { return g.ID == id }); at >= 0 {
			out = append(out, goals[at])
		}
	}
	return out
}

// reportBuilderProblems are a refused save's problems: the domain's, each
// displaced by the handler's under the same input, since a rule whose values
// didn't parse reached the domain without them.
func reportBuilderProblems(err error, unparsed []*domain.InputError) []*domain.InputError {
	var problems []*domain.InputError
	for _, p := range domain.InputErrors(err) {
		if !slices.ContainsFunc(unparsed, func(u *domain.InputError) bool { return u.Input == p.Input }) {
			problems = append(problems, p)
		}
	}
	problems = append(problems, unparsed...)
	if len(problems) == 0 {
		problems = []*domain.InputError{{Message: strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")}}
	}
	return problems
}

// bad is why input was refused, or "" when it wasn't.
func (v reportBuilderView) bad(input string) string {
	for _, p := range v.Problems {
		if p.Input == input {
			return p.Message
		}
	}
	return ""
}

// unplaced are the problems that name no input the builder shows, to list at
// its top; every other shows beside its input.
func (v reportBuilderView) unplaced() []*domain.InputError {
	placed := []string{domain.ReportInputName, domain.ReportInputMode, domain.ReportInputRules, domain.ReportInputInclude,
		domain.ReportInputExclude, domain.ReportInputPicked}
	if len(v.Fields) > 0 {
		placed = append(placed, domain.ReportInputFields)
	}
	for i := range v.rows() {
		placed = append(placed, domain.RuleInput(i))
	}
	var out []*domain.InputError
	for _, p := range v.Problems {
		if !slices.Contains(placed, p.Input) {
			out = append(out, p)
		}
	}
	return out
}

// rows are the rule rows to show: those typed, or one blank row when there
// are none.
func (v reportBuilderView) rows() []reportRuleRow {
	if len(v.Rules) == 0 {
		return []reportRuleRow{{}}
	}
	return v.Rules
}

// ruleOps are the operators a rule row offers, each with its label: "is
// Top-level" and "is not Top-level" for a Top-level rule, which tests no
// values.
func ruleOps(attribute string) [][2]string {
	if attribute == domain.RuleTopLevel {
		return [][2]string{{domain.RuleIs, "is Top-level"}, {domain.RuleIsNot, "is not Top-level"}}
	}
	return [][2]string{{domain.RuleIs, "is"}, {domain.RuleIsAnyOf, "is any of"}, {domain.RuleIsNot, "is not"}}
}

// ruleRowInput names part of the i-th rule row's inputs.
func ruleRowInput(i int, part string) string {
	return fmt.Sprintf("rules[%d].%s", i, part)
}
