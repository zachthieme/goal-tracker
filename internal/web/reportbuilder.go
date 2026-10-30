package web

import (
	"cmp"
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

// reportBuilderView is the builder page for a Report Definition, new or
// saved: what was typed and picked, the problems a refused save found, and
// the choices it offers (CONTEXT.md: Report Definition).
type reportBuilderView struct {
	// ID is the saved definition being edited, and 0 for a new one; Saved is
	// its name as saved, which the breadcrumb shows whatever Name is typed.
	ID           int64
	Saved        string
	Name         string
	Introduction string
	// Mode is domain.ReportModeRules or domain.ReportModePicked.
	Mode string
	// Rules are the rule rows, as typed.
	Rules []reportRuleRow
	// StartNote says which rule a new definition started with, under the
	// rules, on its first render only; "" when it started blank.
	StartNote string
	// PickedIDs, IncludeIDs and ExcludeIDs are the Goals picked by hand, to
	// Also include and to Leave out, each in the order listed; Picked, Include
	// and Exclude are those Goals.
	PickedIDs, IncludeIDs, ExcludeIDs []int64
	Picked, Include, Exclude          []domain.Goal
	// FieldIDs are the Fields chosen to show beside each Goal; SavedFieldIDs
	// are those the saved definition shows, which stay offered once Retired.
	FieldIDs, SavedFieldIDs []int64
	// Problems are a refused save's problems, each naming its input.
	Problems []*domain.InputError

	// Attributes are what a rule can test, each with the values it offers.
	Attributes []ruleAttribute
	// Offers are the Goals each list's picker offers to add, by the list's
	// input name (reportRuleSelection.offers); LeaveOutNeedsRule is true when
	// Leave out offers none because no rule is complete yet.
	Offers            map[string][]domain.Goal
	LeaveOutNeedsRule bool
	// Fields are the Fields still offered to show, and any Retired one the
	// saved definition already shows.
	Fields []domain.Field
	// Matches is the rail: the Goals the definition as typed would select.
	Matches reportMatches
}

// reportMatches is the builder's rail: how many Goals the definition as typed
// would select on its draft, the first railLength of them by title, and how
// many its rules match that Leave out takes away.
type reportMatches struct {
	// NeedsRule is a rules definition with no usable rule yet, which has no
	// matches to show.
	NeedsRule bool
	Count     int
	Shown     []reportMatch
	LeftOut   int
}

// reportMatch is a Goal on the rail. Added is one a rules definition selects
// only because of Also include.
type reportMatch struct {
	domain.SelectedGoal
	Added bool
}

// railLength is how many Goals the rail lists before "+ n more".
const railLength = 10

// more is the rail's closing line: "+ n more" for the Goals past those listed
// and "m left out" for those Leave out takes away, each only when not 0.
func (m reportMatches) more() string {
	var parts []string
	if n := m.Count - len(m.Shown); n > 0 {
		parts = append(parts, fmt.Sprintf("+ %d more", n))
	}
	if m.LeftOut > 0 {
		parts = append(parts, fmt.Sprintf("%d left out", m.LeftOut))
	}
	return strings.Join(parts, " · ")
}

// reportRuleRow is one rule row as the form posts it: its attribute's key, its
// operator, and its values, each as its option posts it.
type reportRuleRow struct {
	Attribute string
	Op        string
	Values    []string
}

// ruleAttribute is an attribute a rule can test: its key, as the attribute
// select posts it, its label, and the values it offers. A Retired one is
// offered only to a rule row that already tests it.
type ruleAttribute struct {
	Key     string
	Label   string
	Values  []ruleValue
	Retired bool
}

// ruleValue is a value a rule can test for: Value is what its option posts,
// the attribute's key and the domain's value joined by "=", so a value
// carries the attribute it belongs to. A Retired one is offered only to a
// rule row that already holds it.
type ruleValue struct {
	Value   string
	Label   string
	Retired bool
}

// offers reports whether the rule row offers attribute a: one not Retired, or
// the one it tests.
func (row reportRuleRow) offers(a ruleAttribute) bool {
	return !a.Retired || a.Key == row.Attribute
}

// offeredValues are the values of a the rule row offers: those not Retired,
// and those it holds.
func (row reportRuleRow) offeredValues(a ruleAttribute) []ruleValue {
	var out []ruleValue
	for _, v := range a.Values {
		if !v.Retired || slices.Contains(row.Values, v.Value) {
			out = append(out, v)
		}
	}
	return out
}

// valueGroups are the attributes, each with only its values the row offers,
// whose values the row shows: the one it tests, or every one it offers when
// it tests none yet. An attribute with no value to offer, such as Top-level,
// is left out.
func (row reportRuleRow) valueGroups(attributes []ruleAttribute) []ruleAttribute {
	var out []ruleAttribute
	for _, a := range attributes {
		if !row.offers(a) || (row.Attribute != "" && a.Key != row.Attribute) {
			continue
		}
		if values := row.offeredValues(a); len(values) > 0 {
			a.Values = values
			out = append(out, a)
		}
	}
	return out
}

// valueInputType is how the row's values are chosen: as radios when it tests
// an attribute "is" one value, the operator a row starts with, and as
// checkboxes for "is any of" and "is not", or before it tests anything.
func (row reportRuleRow) valueInputType() string {
	if row.Attribute != "" && (row.Op == domain.RuleIs || row.Op == "") {
		return "radio"
	}
	return "checkbox"
}

// The builder's Goal lists, each posted once per Goal in it.
const (
	inputPicked  = "picked"
	inputInclude = "include"
	inputExclude = "exclude"
)

// goalSearchInput is the name of the search box of the picker for list: one
// per list, as the search sends the whole form.
func goalSearchInput(list string) string {
	return list + "-q"
}

// inputShowField is a Field to show beside each Goal, posted once per Field.
const inputShowField = "field"

// dimensionAttribute is the key of a rule testing the Dimension dimID.
func dimensionAttribute(dimID int64) string {
	return "dimension:" + strconv.FormatInt(dimID, 10)
}

// handleNewReportForm shows the builder for a new Report Definition, selecting
// by rules until the author chooses to pick Goals by hand, and starting with
// the rule newReportStartingRule chooses for the author.
func (s *Server) handleNewReportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v := reportBuilderView{Mode: domain.ReportModeRules}
	start, note, err := s.newReportStartingRule(r, current)
	if err != nil {
		s.serverError(w, r, "could not load the builder", err)
		return
	}
	if note != "" {
		v.Rules, v.StartNote = []reportRuleRow{start}, note
	}
	if err := s.loadReportBuilder(r, &v); err != nil {
		s.serverError(w, r, "could not load the builder", err)
		return
	}
	render(w, r, http.StatusOK, reportBuilderPage(&current, v))
}

// newReportStartingRule is the rule a new Report Definition starts with, and
// the note saying so: the author's org when anyone is under them, or else
// their own Goals when they own any. With neither, note is "" and the builder
// starts with a blank row.
func (s *Server) newReportStartingRule(r *http.Request, author domain.Account) (row reportRuleRow, note string, err error) {
	authorID := strconv.FormatInt(author.ID, 10)
	under, err := s.svc.HasAnyoneUnder(r.Context(), author.ID)
	if err != nil {
		return reportRuleRow{}, "", err
	}
	if under {
		return reportRuleRow{Attribute: domain.RuleOrg, Op: domain.RuleIs, Values: []string{domain.RuleOrg + "=" + authorID}},
			"Started with your org. Change or remove it.", nil
	}
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return reportRuleRow{}, "", err
	}
	if slices.ContainsFunc(distinctOwners(goals), func(a domain.Account) bool { return a.ID == author.ID }) {
		return reportRuleRow{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{domain.RuleOwner + "=" + authorID}},
			"Started with your own Goals. Change or remove it.", nil
	}
	return reportRuleRow{}, "", nil
}

// handleNewReport saves the builder's new Report Definition and lands on its
// draft, as submitReportBuilder answers.
func (s *Server) handleNewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	s.submitReportBuilder(w, r, current, domain.ReportDefinition{}, func(in domain.SaveReportDefinitionInput) (domain.ReportDefinition, error) {
		return s.svc.SaveReportDefinition(r.Context(), current.ID, in)
	})
}

// submitReportBuilder answers the builder's form for the saved definition, or
// a new one when its ID is 0: save stores it as typed, and a stored definition lands
// on its draft. A refused save comes back as the builder, 422, as typed, with
// each problem beside its input; one refused to its sender is the refusal
// page, 403, linking Back to the definition's draft, or Reports for a new one.
// Add rule, a rule row's × and Show matches save nothing: the builder comes
// back as typed with a blank row added, without that row, or as it is, its
// rail listing the Goals as typed.
func (s *Server) submitReportBuilder(w http.ResponseWriter, r *http.Request, current domain.Account, saved domain.ReportDefinition,
	save func(in domain.SaveReportDefinitionInput) (domain.ReportDefinition, error),
) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	removing := r.PostForm.Has(inputRemoveRule)
	if removing {
		dropRuleRow(r.PostForm, r.PostForm.Get(inputRemoveRule))
	}
	v, in := readReportBuilder(r)
	v.ID, v.Saved, v.SavedFieldIDs = saved.ID, saved.Name, saved.FieldIDs
	do := r.PostFormValue("do")
	if do == "add-rule" || do == "show-matches" || removing {
		if do == "add-rule" {
			v.Rules = append(v.rows(), reportRuleRow{})
		}
		if err := s.loadReportBuilder(r, &v); err != nil {
			s.serverError(w, r, "could not load the builder", err)
			return
		}
		render(w, r, http.StatusOK, reportBuilderPage(&current, v))
		return
	}
	def, err := save(in)
	if err == nil {
		http.Redirect(w, r, "/reports/"+strconv.FormatInt(def.ID, 10), http.StatusSeeOther)
		return
	}
	// Only an edit can be refused for who is asking, and only when the
	// editor lost the right to since editableReport checked.
	rf := refused{Err: err, Heading: "Can't save Report", Back: "/reports",
		Form: asPage(func(_ context.Context, err error) (templ.Component, error) {
			v.Problems = problemsOf(err)
			if err := s.loadReportBuilder(r, &v); err != nil {
				return nil, fmt.Errorf("load the builder: %w", err)
			}
			return reportBuilderPage(&current, v), nil //nolint:contextcheck // templ hands its closures Render's ctx in a struct field, which contextcheck can't follow
		})}
	if saved.ID != 0 {
		rf.Heading, rf.Back = "Can't edit Report", "/reports/"+strconv.FormatInt(saved.ID, 10)
	}
	s.answerRefused(w, r, rf)
}

// handleReportMatches answers the builder's form, as it is typed, with only its
// rail. The optional id names the saved definition being edited, whose default
// baseline the matches are read against; with none, a new definition's.
func (s *Server) handleReportMatches(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	id, ok := s.builderDefinitionID(w, r)
	if !ok {
		return
	}
	v, _ := readReportBuilder(r)
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		s.serverError(w, r, "could not find the matches", err)
		return
	}
	m, err := s.matchReport(r, goals, v, id)
	if err != nil {
		s.serverError(w, r, "could not find the matches", err)
		return
	}
	render(w, r, http.StatusOK, reportMatchesRail(m))
}

// builderDefinitionID is the saved definition the builder's form, as posted
// or sent with a search, names by its optional id, whose default baseline its
// rules are read against; 0 for a new one. ok is false, the answer written,
// when the id names no definition.
func (s *Server) builderDefinitionID(w http.ResponseWriter, r *http.Request) (id int64, ok bool) {
	raw := r.Form.Get("id")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	if _, err := s.svc.GetReportDefinition(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return 0, false
		}
		s.serverError(w, r, "could not load report", err)
		return 0, false
	}
	return id, true
}

// handleSearchReportGoals answers a builder picker's search as it is typed,
// the search sending the builder's whole form: the Goals the list offers
// (reportRuleSelection.offers), at any level, whose titles contain what is
// typed in the list's box, ignoring case. Each result carries the chip
// picking it adds to that list. Leave out with no complete rule finds
// nothing, and says to add one.
func (s *Server) handleSearchReportGoals(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the search", http.StatusBadRequest)
		return
	}
	list := r.Form.Get("list")
	if !slices.Contains([]string{inputPicked, inputInclude, inputExclude}, list) {
		http.Error(w, "unknown Goal list", http.StatusBadRequest)
		return
	}
	id, ok := s.builderDefinitionID(w, r)
	if !ok {
		return
	}
	v, _ := readReportBuilderForm(r.Form)
	v.ID = id
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		s.serverError(w, r, "could not search goals", err)
		return
	}
	var sel reportRuleSelection
	if list != inputPicked {
		if sel, err = s.selectByReportRules(r, v); err != nil {
			s.serverError(w, r, "could not search goals", err)
			return
		}
	}
	q := strings.ToLower(strings.TrimSpace(r.Form.Get(goalSearchInput(list))))
	var matches []domain.Goal
	for _, g := range sel.offers(list, goals, v) {
		if strings.Contains(strings.ToLower(g.Title), q) {
			matches = append(matches, g)
		}
	}
	render(w, r, http.StatusOK, reportGoalSearchResults(list, matches, list == inputExclude && sel.NeedsRule))
}

// reportRuleSelection is what the builder's rules, as typed, select alone,
// before Also include and Leave out, as the rail reads them: the Goals they
// match, less any finished before the baseline. NeedsRule is true when no
// rule is complete yet, so they select nothing.
type reportRuleSelection struct {
	NeedsRule bool
	Selected  []int64
}

// selectByReportRules is what the rules of the builder v select alone, read
// against the default baseline of the saved definition v.ID, or of a new one.
func (s *Server) selectByReportRules(r *http.Request, v reportBuilderView) (reportRuleSelection, error) {
	rules := completeReportRules(v.Rules)
	if len(rules) == 0 {
		return reportRuleSelection{NeedsRule: true}, nil
	}
	sel, err := s.svc.SelectGoalsAgainst(r.Context(), domain.ReportDefinition{ID: v.ID, Mode: domain.ReportModeRules, Rules: rules}, time.Time{})
	if err != nil {
		return reportRuleSelection{}, err
	}
	out := reportRuleSelection{}
	for _, sg := range sel.Goals {
		out.Selected = append(out.Selected, sg.Goal.ID)
	}
	return out, nil
}

// offers are the Goals of goals the builder v's picker for list offers to
// add: for Goals I pick, every Goal not yet picked; for Also include, those
// the rules don't select, and for Leave out those they do, each less the
// Goals already in either list, so no pick is a no-op and no Goal is in both.
func (sel reportRuleSelection) offers(list string, goals []domain.Goal, v reportBuilderView) []domain.Goal {
	var out []domain.Goal
	for _, g := range goals {
		var offered bool
		switch list {
		case inputPicked:
			offered = !slices.Contains(v.PickedIDs, g.ID)
		case inputInclude, inputExclude:
			listed := slices.Contains(v.IncludeIDs, g.ID) || slices.Contains(v.ExcludeIDs, g.ID)
			offered = !listed && slices.Contains(sel.Selected, g.ID) == (list == inputExclude)
		}
		if offered {
			out = append(out, g)
		}
	}
	return out
}

// readReportBuilder reads the builder's posted form as typed, both modes'
// inputs kept, and the definition it saves, as readReportBuilderForm does.
func readReportBuilder(r *http.Request) (v reportBuilderView, in domain.SaveReportDefinitionInput) {
	return readReportBuilderForm(r.PostForm)
}

// readReportBuilderForm reads the builder's form as typed, both modes' inputs
// kept, and the definition it saves: only what the chosen mode selects by.
func readReportBuilderForm(form url.Values) (v reportBuilderView, in domain.SaveReportDefinitionInput) {
	v = reportBuilderView{
		Name:         form.Get("name"),
		Introduction: form.Get("introduction"),
		Mode:         form.Get("mode"),
		Rules:        reportRuleRows(form),
		PickedIDs:    listedGoals(form, inputPicked),
		IncludeIDs:   listedGoals(form, inputInclude),
		ExcludeIDs:   listedGoals(form, inputExclude),
		FieldIDs:     parseInt64s(form[inputShowField]),
	}
	in = domain.SaveReportDefinitionInput{Name: v.Name, Introduction: v.Introduction, Mode: v.Mode, FieldIDs: v.FieldIDs}
	switch v.Mode {
	case domain.ReportModePicked:
		in.Picked = v.PickedIDs
	case domain.ReportModeRules:
		in.Include, in.Exclude = v.IncludeIDs, v.ExcludeIDs
		for _, row := range v.Rules {
			in.Rules = append(in.Rules, row.rule())
		}
	}
	return v, in
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
// rules[i].op and rules[i].value, in the order of their i. Each row keeps only
// the values of its attribute: without script every attribute's values stay
// selected, so a row switched to another attribute still posts its old ones.
// A row then with neither an attribute nor a value is left out. A row's i is
// only its place.
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
		row := byIndex[i]
		row.Values = slices.DeleteFunc(slices.Clone(row.Values), func(posted string) bool {
			return row.Attribute == "" || !strings.HasPrefix(posted, row.Attribute+"=")
		})
		if row.Attribute != "" || len(row.Values) > 0 {
			rows = append(rows, *row)
		}
	}
	return rows
}

// rule is the Report rule the row posts. Its values are the domain's, without
// the attribute's key each option posts them under.
func (row reportRuleRow) rule() domain.ReportRule {
	rule := domain.ReportRule{Attribute: row.Attribute, Op: row.Op}
	if raw, ok := strings.CutPrefix(row.Attribute, "dimension:"); ok {
		rule.Attribute = domain.RuleDimension
		// An id that doesn't parse names no Dimension, which saving refuses.
		rule.DimensionID, _ = strconv.ParseInt(raw, 10, 64)
	}
	for _, posted := range row.Values {
		rule.Values = append(rule.Values, strings.TrimPrefix(posted, row.Attribute+"="))
	}
	return rule
}

// removeChip is the name of a chip's no-script Remove checkbox in list,
// posting its Goal's id.
func removeChip(list string) string {
	return "remove-" + list
}

// listedGoals are the ids form posts for the Goal list, without repeats, less
// those whose chip's Remove is ticked.
func listedGoals(form url.Values, list string) []int64 {
	removed := parseInt64s(form[removeChip(list)])
	return slices.DeleteFunc(dedupeInt64s(parseInt64s(form[list])), func(id int64) bool {
		return slices.Contains(removed, id)
	})
}

// parseInt64s are the ids among raw, each that parses.
func parseInt64s(raw []string) []int64 {
	out := make([]int64, 0, len(raw))
	for _, v := range raw {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
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

// loadReportBuilder fills in the choices the builder offers, the listed Goals
// as chips from their ids, and the rail. A Retired Field is offered only when
// the saved definition already shows it, as a Retired Dimension is to a rule.
func (s *Server) loadReportBuilder(r *http.Request, v *reportBuilderView) error {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return err
	}
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		return err
	}
	v.Fields = slices.DeleteFunc(fields, func(f domain.Field) bool {
		return f.Retired && !slices.Contains(v.SavedFieldIDs, f.ID)
	})
	v.Picked = goalsByID(goals, v.PickedIDs)
	v.Include = goalsByID(goals, v.IncludeIDs)
	v.Exclude = goalsByID(goals, v.ExcludeIDs)
	var rules []domain.ReportRule
	for _, row := range v.Rules {
		rules = append(rules, row.rule())
	}
	attributes, err := s.svc.ReportRuleAttributes(r.Context(), goals, rules)
	if err != nil {
		return err
	}
	v.Attributes = builderRuleAttributes(attributes)
	sel, err := s.selectByReportRules(r, *v)
	if err != nil {
		return err
	}
	v.Offers = map[string][]domain.Goal{}
	for _, list := range []string{inputPicked, inputInclude, inputExclude} {
		v.Offers[list] = sel.offers(list, goals, *v)
	}
	v.LeaveOutNeedsRule = sel.NeedsRule
	v.Matches, err = s.matchReport(r, goals, *v, v.ID)
	return err
}

// builderRuleAttributes are the domain's rule attributes as the builder's
// form posts them: each keyed by dimensionAttribute for a Dimension, or else
// by its attribute, and each value posting that key and the domain's value
// joined by "=".
func builderRuleAttributes(attributes []domain.RuleAttribute) []ruleAttribute {
	out := make([]ruleAttribute, 0, len(attributes))
	for _, a := range attributes {
		key := a.Attribute
		if a.Attribute == domain.RuleDimension {
			key = dimensionAttribute(a.DimensionID)
		}
		attr := ruleAttribute{Key: key, Label: a.Label, Retired: a.Retired}
		for _, v := range a.Values {
			attr.Values = append(attr.Values, ruleValue{Value: key + "=" + v.Value, Label: v.Label, Retired: v.Retired})
		}
		out = append(out, attr)
	}
	return out
}

// matchReport is the rail for the definition the builder v describes, read
// against the default baseline of the saved definition id, or of a new one
// when id is 0. It takes only the chosen mode's inputs, leaves out each rule
// with no values but a Top-level one, and each listed Goal that doesn't exist.
func (s *Server) matchReport(r *http.Request, goals []domain.Goal, v reportBuilderView, id int64) (reportMatches, error) {
	existing := func(ids []int64) []int64 {
		var out []int64
		for _, g := range goalsByID(goals, ids) {
			out = append(out, g.ID)
		}
		return out
	}
	def := domain.ReportDefinition{ID: id, Mode: domain.ReportModeRules}
	if v.Mode == domain.ReportModePicked {
		def.Mode, def.Picked = domain.ReportModePicked, existing(v.PickedIDs)
	} else {
		def.Rules = completeReportRules(v.Rules)
		if len(def.Rules) == 0 {
			return reportMatches{NeedsRule: true}, nil
		}
		def.Include, def.Exclude = existing(v.IncludeIDs), existing(v.ExcludeIDs)
	}
	sel, err := s.svc.SelectGoalsAgainst(r.Context(), def, time.Time{})
	if err != nil {
		return reportMatches{}, err
	}
	selected := sel.Goals
	m := reportMatches{Count: len(selected), LeftOut: len(sel.LeftOut)}
	slices.SortStableFunc(selected, func(a, b domain.SelectedGoal) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Goal.Title), strings.ToLower(b.Goal.Title)), cmp.Compare(a.Goal.ID, b.Goal.ID))
	})
	for _, sg := range selected[:min(len(selected), railLength)] {
		m.Shown = append(m.Shown, reportMatch{SelectedGoal: sg, Added: sg.Via == domain.SelectedByAlsoInclude})
	}
	return m, nil
}

// completeReportRules are the rules of rows that are complete enough to
// select by: each with a value, or a Top-level one, which takes none.
func completeReportRules(rows []reportRuleRow) []domain.ReportRule {
	var out []domain.ReportRule
	for _, row := range rows {
		if rule := row.rule(); rule.Attribute == domain.RuleTopLevel || len(rule.Values) > 0 {
			out = append(out, rule)
		}
	}
	return out
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

// title is the builder's heading: Edit report for a saved definition, New
// report otherwise.
func (v reportBuilderView) title() string {
	if v.ID != 0 {
		return "Edit report"
	}
	return "New report"
}

// draft is the saved definition's draft.
func (v reportBuilderView) draft() string {
	return "/reports/" + strconv.FormatInt(v.ID, 10)
}

// action is where the builder's form posts: the saved definition's edit, or
// a new definition.
func (v reportBuilderView) action() string {
	if v.ID != 0 {
		return v.draft() + "/edit"
	}
	return "/reports/new"
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

// ruleRowInput names part of the i-th rule row's inputs.
func ruleRowInput(i int, part string) string {
	return fmt.Sprintf("rules[%d].%s", i, part)
}
