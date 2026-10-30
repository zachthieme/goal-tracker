package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// ReportDefinition is a saved, reusable selection of Goals for a Report
// (CONTEXT.md: Report Definition). It selects Goals by Report rules on their
// attributes, with Goals to Also include and to Leave out, or by a hand-picked
// list, never by following contributes-to links (ADR 0007); and carries an
// introduction the Report's narrative opens with. The selection is computed on
// demand by SelectGoals (ADR-0004), so the definition stores the criteria,
// never the resulting Goal set.
type ReportDefinition struct {
	ID           int64
	Name         string
	Introduction string
	// Mode is ReportModeRules or ReportModePicked.
	Mode string
	// Rules are the Report rules a Goal must all meet, in a rules definition.
	Rules []ReportRule
	// Include are the Goals to Also include and Exclude the Goals to Leave out
	// of a rules definition, whatever its rules say.
	Include []int64
	Exclude []int64
	// Picked are the Goals a picked definition selects, in the order picked.
	Picked []int64
	// FieldIDs are the Fields the Report shows beside each Goal that has a
	// value in them. Empty, the default, shows none.
	FieldIDs []int64
	// CreatedBy is the id of the Account that saved it, who with its Report
	// Delegates and an Admin may change, curate and publish it.
	CreatedBy int64
	CreatedAt time.Time
}

// The modes a Report Definition selects its Goals in.
//
// Each name starts ReportMode: the CHECK test on report_definitions.mode finds
// its values by that prefix (enumchecks_test.go), so a value named otherwise
// goes unchecked.
const (
	ReportModeRules  = "rules"
	ReportModePicked = "picked"
)

// ReportRule is one Report rule: a condition on a Goal's attribute (CONTEXT.md:
// Report rule). The values within a rule are ORed: a Goal meets "is" or "is
// any of" by having any of them, and "is not" by having none of them.
type ReportRule struct {
	// Attribute is RuleDimension, RuleOwner, RuleOrg, RuleLifecycle,
	// RuleHealth or RuleTopLevel.
	Attribute string
	// DimensionID is the Dimension a RuleDimension rule tests, and 0 for every
	// other attribute.
	DimensionID int64
	// Op is RuleIs (exactly one value), RuleIsAnyOf or RuleIsNot.
	Op string
	// Values are what the rule tests for: Dimension value ids (of DimensionID)
	// or Account ids (of Owners, or of the people whose Org) in decimal, or Lifecycle or Health names. A RuleTopLevel
	// rule has none: it is "is Top-level" or "is not Top-level".
	Values []string
}

// The attributes a Report rule can test. Fields are not among them: a Report
// shows Fields but never selects by them (ADR 0005, ADR 0007).
//
// Each name starts Rule, not RuleIs, which names the operators: the CHECK test
// on report_rules.attribute finds its values by that prefix
// (enumchecks_test.go), so a value named otherwise goes unchecked.
const (
	RuleDimension = "dimension"
	RuleOwner     = "owner"
	// RuleOrg tests the Owner's Org: whether the Owner is in the org of
	// any of the people it names, by their Managers as they are when the
	// Report is drafted. Like the Owner rule, it never looks at Delegates.
	RuleOrg       = "org"
	RuleLifecycle = "lifecycle"
	RuleHealth    = "health"
	RuleTopLevel  = "top-level"
)

// The operators of a Report rule.
//
// Each name starts RuleIs: the CHECK test on report_rules.op finds its values
// by that prefix (enumchecks_test.go), so a value named otherwise goes
// unchecked.
const (
	RuleIs      = "is"
	RuleIsAnyOf = "is any of"
	RuleIsNot   = "is not"
)

// The lists a Report Definition names Goals in, as stored.
//
// Each name starts reportList: the CHECK test on report_definition_goals.list
// finds its values by that prefix (enumchecks_test.go), so a value named
// otherwise goes unchecked.
const (
	reportListPicked  = "picked"
	reportListInclude = "include"
	reportListExclude = "exclude"
)

// SaveReportDefinitionInput is the save-a-Report-Definition command's input.
type SaveReportDefinitionInput struct {
	Name         string
	Introduction string
	Mode         string
	Rules        []ReportRule
	Include      []int64
	Exclude      []int64
	Picked       []int64
	FieldIDs     []int64
}

// SelectedGoal is one Goal chosen by a Report Definition, carrying the fields the
// live draft shows one per line: the Goal (its title, Owner, and due date) and
// its current Health. Health is empty when the Goal has no Check-in yet — a
// Proposed Goal, or an Active one not yet checked in on (CONTEXT.md: Health).
// Fields are its values in the Fields the definition chose, filled in when the
// Report is drafted.
type SelectedGoal struct {
	Goal   Goal
	Health string
	Fields []FieldValue `json:",omitempty"`
	// Via is how the Goal came into the selection: SelectedByRules,
	// SelectedByAlsoInclude or SelectedByPicking. It isn't frozen with a
	// publication.
	Via string `json:"-"`
}

// The ways a Goal comes into a Report Definition's selection: a rules
// definition's rules match it, or only its Also include adds it; or a picked
// definition picks it.
const (
	SelectedByRules       = "rules"
	SelectedByAlsoInclude = "also include"
	SelectedByPicking     = "picked"
)

// ReportSelection is what a Report Definition selects (ADR 0007): its Goals,
// each saying how it came in, and LeftOut, the Goals its Leave out keeps out
// that its rules would otherwise have selected.
type ReportSelection struct {
	Goals   []SelectedGoal
	LeftOut []Goal
}

// added counts the Goals in only through Also include.
func (sel ReportSelection) added() int {
	n := 0
	for _, sg := range sel.Goals {
		if sg.Via == SelectedByAlsoInclude {
			n++
		}
	}
	return n
}

// SaveReportDefinition saves a reusable Report Definition. Anyone signed in may
// save one (CONTEXT.md: Report Definition), but a Departed person can't
// (saveReportDefinitionRefusal); actorID records who created it. It
// refuses each input it can't take with an *InputError naming it.
func (s *Service) SaveReportDefinition(ctx context.Context, actorID int64, in SaveReportDefinitionInput) (ReportDefinition, error) {
	in = normalizeReportDefinition(in)
	var id int64
	if err := s.WithinTx(ctx, func(tx *Service) error {
		if err := saveReportDefinitionRefusal(tx.accountActor(ctx, actorID)); err != nil {
			return err
		}
		if err := tx.validateReportDefinition(ctx, in, nil); err != nil {
			return err
		}
		row, err := tx.queries.CreateReportDefinition(ctx, db.CreateReportDefinitionParams{
			Name:         in.Name,
			Introduction: strings.TrimSpace(in.Introduction),
			Mode:         in.Mode,
			CreatedBy:    actorID,
			CreatedAt:    s.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create report definition: %w", err)
		}
		id = row.ID
		if err := tx.recordReportLog(ctx, id, actorID, ReportLogCreated, "Created"); err != nil {
			return err
		}
		return tx.saveReportSelection(ctx, id, in)
	}); err != nil {
		return ReportDefinition{}, err
	}
	return s.GetReportDefinition(ctx, id)
}

// UpdateReportDefinition edits the saved Report Definition id: its name,
// introduction, scope and Fields are replaced by in. Only its draft changes;
// its publications stay frozen as they were published. It refuses each input
// it can't take with an *InputError naming it, as SaveReportDefinition does.
// Only its creator, a Report Delegate or an Admin may edit it
// (ErrNotAuthorized); ErrNotFound when there is no such definition.
func (s *Service) UpdateReportDefinition(ctx context.Context, actorID, id int64, in SaveReportDefinitionInput) (ReportDefinition, error) {
	in = normalizeReportDefinition(in)
	if err := s.WithinTx(ctx, func(tx *Service) error {
		def, err := tx.GetReportDefinition(ctx, id)
		if err != nil {
			return err
		}
		if err := tx.reportRule(ctx, actorID, def, editReportRefusal); err != nil {
			return err
		}
		if err := tx.validateReportDefinition(ctx, in, def.FieldIDs); err != nil {
			return err
		}
		edits, err := tx.reportDefinitionEdits(ctx, def, in)
		if err != nil {
			return err
		}
		for _, edit := range edits {
			if err := tx.recordReportLog(ctx, id, actorID, ReportLogEdited, edit); err != nil {
				return err
			}
		}
		if err := tx.queries.UpdateReportDefinition(ctx, db.UpdateReportDefinitionParams{
			Name:         in.Name,
			Introduction: strings.TrimSpace(in.Introduction),
			Mode:         in.Mode,
			ID:           id,
		}); err != nil {
			return fmt.Errorf("update report definition: %w", err)
		}
		for _, clear := range []func(context.Context, int64) error{
			tx.queries.ClearReportRuleValues,
			tx.queries.ClearReportRules,
			tx.queries.ClearReportDefinitionGoals,
			tx.queries.ClearReportDefinitionFields,
		} {
			if err := clear(ctx, id); err != nil {
				return fmt.Errorf("clear report selection: %w", err)
			}
		}
		return tx.saveReportSelection(ctx, id, in)
	}); err != nil {
		return ReportDefinition{}, err
	}
	return s.GetReportDefinition(ctx, id)
}

// normalizeReportDefinition trims in's name and drops repeated ids from its
// lists, as saving them would.
func normalizeReportDefinition(in SaveReportDefinitionInput) SaveReportDefinitionInput {
	in.Name = strings.TrimSpace(in.Name)
	in.Include, in.Exclude, in.Picked = dedupeIDs(in.Include), dedupeIDs(in.Exclude), dedupeIDs(in.Picked)
	in.FieldIDs = dedupeIDs(in.FieldIDs)
	return in
}

// saveReportSelection stores what the definition defID selects by and shows:
// its rules, its listed Goals and its Fields.
func (s *Service) saveReportSelection(ctx context.Context, defID int64, in SaveReportDefinitionInput) error {
	for _, rule := range in.Rules {
		ruleID, err := s.queries.AddReportRule(ctx, db.AddReportRuleParams{
			ReportDefinitionID: defID,
			Attribute:          rule.Attribute,
			DimensionID:        rule.DimensionID,
			Op:                 rule.Op,
		})
		if err != nil {
			return fmt.Errorf("add report rule: %w", err)
		}
		for _, v := range dedupeIDs(rule.Values) {
			if err := s.queries.AddReportRuleValue(ctx, db.AddReportRuleValueParams{ReportRuleID: ruleID, Value: v}); err != nil {
				return fmt.Errorf("add report rule value: %w", err)
			}
		}
	}
	for list, ids := range map[string][]int64{reportListPicked: in.Picked, reportListInclude: in.Include, reportListExclude: in.Exclude} {
		for _, id := range ids {
			if err := s.queries.AddReportDefinitionGoal(ctx, db.AddReportDefinitionGoalParams{
				ReportDefinitionID: defID,
				List:               list,
				GoalID:             id,
			}); err != nil {
				return fmt.Errorf("list report goal: %w", err)
			}
		}
	}
	for _, id := range in.FieldIDs {
		if err := s.queries.AddReportDefinitionField(ctx, db.AddReportDefinitionFieldParams{
			ReportDefinitionID: defID,
			FieldID:            id,
		}); err != nil {
			return fmt.Errorf("add report field: %w", err)
		}
	}
	return nil
}

// The inputs of a Report Definition, by the names its errors give them. A
// rule's is made by RuleInput.
const (
	ReportInputName    = "name"
	ReportInputMode    = "mode"
	ReportInputRules   = "rules"
	ReportInputInclude = "include"
	ReportInputExclude = "exclude"
	ReportInputPicked  = "picked"
	ReportInputFields  = "fields"
)

// RuleInput names the i-th Report rule, counting from zero.
func RuleInput(i int) string { return fmt.Sprintf("rules[%d]", i) }

// validateReportDefinition returns every input of in that can't be saved, each
// as an *InputError, joined. A rules definition needs rules and carries no
// picked list; a picked definition needs Goals and carries no rules, Also
// include or Leave out. Every Goal listed must exist. shown are the Fields the
// saved definition already shows, which may since have been Retired.
func (s *Service) validateReportDefinition(ctx context.Context, in SaveReportDefinitionInput, shown []int64) error {
	var problems []error
	if in.Name == "" {
		problems = append(problems, inputError(ReportInputName, "give the report a name"))
	}
	switch in.Mode {
	case ReportModeRules:
		if len(in.Rules) == 0 {
			problems = append(problems, inputError(ReportInputRules, "add a rule: choose what it tests, such as a Team, and a value"))
		}
		for i, rule := range in.Rules {
			problem, err := s.validateReportRule(ctx, rule)
			if err != nil {
				return err
			}
			if problem != "" {
				problems = append(problems, inputError(RuleInput(i), "%s", problem))
			}
		}
		if len(in.Picked) > 0 {
			problems = append(problems, inputError(ReportInputPicked, "a report of Goals matching rules has no picked Goals"))
		}
	case ReportModePicked:
		if len(in.Picked) == 0 {
			problems = append(problems, inputError(ReportInputPicked, "pick at least one Goal"))
		}
		if len(in.Rules) > 0 {
			problems = append(problems, inputError(ReportInputRules, "a report of Goals I pick has no rules"))
		}
		if len(in.Include) > 0 {
			problems = append(problems, inputError(ReportInputInclude, "a report of Goals I pick has nothing to Also include"))
		}
		if len(in.Exclude) > 0 {
			problems = append(problems, inputError(ReportInputExclude, "a report of Goals I pick has nothing to Leave out"))
		}
	default:
		problems = append(problems, inputError(ReportInputMode, "choose Goals matching rules or Goals I pick, not %q", in.Mode))
	}
	for input, ids := range map[string][]int64{ReportInputPicked: in.Picked, ReportInputInclude: in.Include, ReportInputExclude: in.Exclude} {
		for _, id := range ids {
			if _, err := s.queries.GetGoal(ctx, id); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("look up goal: %w", err)
				}
				problems = append(problems, inputError(input, "a Goal in this list no longer exists; remove it and save again"))
				break
			}
		}
	}
	fieldProblem, err := s.validateReportFields(ctx, in.FieldIDs, shown)
	if err != nil {
		return err
	}
	if fieldProblem != nil {
		problems = append(problems, fieldProblem)
	}
	return errors.Join(problems...)
}

// validateReportRule says what is wrong with rule, or "" when nothing is. Its
// operator must be known, and "is" takes exactly one value. A Top-level rule
// takes no values and is "is" or "is not"; any other takes at least one, each
// a value of its Dimension, an Account (for an Owner or an Org rule), a
// Lifecycle or a Health.
func (s *Service) validateReportRule(ctx context.Context, rule ReportRule) (string, error) {
	switch rule.Op {
	case RuleIs, RuleIsAnyOf, RuleIsNot:
	default:
		return fmt.Sprintf("%q is not a rule operator", rule.Op), nil
	}
	values := dedupeIDs(rule.Values)
	if rule.Attribute == RuleTopLevel {
		if rule.Op == RuleIsAnyOf || len(values) > 0 {
			return `a Top-level rule is "is Top-level" or "is not Top-level"`, nil
		}
		return "", nil
	}
	if len(values) == 0 {
		return "choose a value for this rule", nil
	}
	if rule.Op == RuleIs && len(values) > 1 {
		return `"is" takes exactly one value`, nil
	}
	var known func(string) (bool, error)
	var kind string
	switch rule.Attribute {
	case RuleDimension:
		dim, err := s.queries.GetDimension(ctx, rule.DimensionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Sprintf("Dimension %d does not exist", rule.DimensionID), nil
			}
			return "", fmt.Errorf("look up dimension: %w", err)
		}
		kind = "a value of " + dim.Name
		known = func(v string) (bool, error) {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return false, nil
			}
			val, err := s.queries.GetDimensionValue(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return err == nil && val.DimensionID == dim.ID, err
		}
	case RuleOwner, RuleOrg:
		kind = "an Account"
		known = func(v string) (bool, error) {
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return false, nil
			}
			_, err = s.queries.GetAccount(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return err == nil, err
		}
	case RuleLifecycle:
		kind = "a Lifecycle"
		known = func(v string) (bool, error) { return slices.Contains(Lifecycles(), v), nil }
	case RuleHealth:
		kind = "a Health"
		known = func(v string) (bool, error) { return validHealth(v), nil }
	default:
		return fmt.Sprintf("%q is not a rule attribute", rule.Attribute), nil
	}
	for _, v := range values {
		ok, err := known(v)
		if err != nil {
			return "", fmt.Errorf("look up rule value: %w", err)
		}
		if !ok {
			return fmt.Sprintf("%q is not %s", v, kind), nil
		}
	}
	return "", nil
}

// validateReportFields refuses a chosen Field that doesn't exist or is
// Retired, as a Retired Field is no longer offered (CONTEXT.md: Retired) —
// unless it is among shown, the Fields the saved definition already shows,
// which keep working once Retired (ADR 0005).
func (s *Service) validateReportFields(ctx context.Context, ids, shown []int64) (*InputError, error) {
	for _, id := range ids {
		row, err := s.queries.GetField(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inputError(ReportInputFields, "field %d does not exist", id), nil
			}
			return nil, fmt.Errorf("look up field: %w", err)
		}
		if f := fieldFromRow(row); f.Retired && !slices.Contains(shown, id) {
			return inputError(ReportInputFields, "%s is retired, so a Report can't be set to show it", f.Name), nil
		}
	}
	return nil, nil
}

// GetReportDefinition returns the Report Definition with the given id, its rules
// and lists resolved. It returns ErrNotFound if none exists.
func (s *Service) GetReportDefinition(ctx context.Context, id int64) (ReportDefinition, error) {
	row, err := s.queries.GetReportDefinition(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportDefinition{}, fmt.Errorf("%w: report definition %d", ErrNotFound, id)
		}
		return ReportDefinition{}, fmt.Errorf("get report definition: %w", err)
	}
	return s.reportDefinitionFromRow(ctx, row)
}

// ListReportDefinitions returns every saved Report Definition, ordered by name,
// each with its rules and lists resolved.
func (s *Service) ListReportDefinitions(ctx context.Context) ([]ReportDefinition, error) {
	rows, err := s.queries.ListReportDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list report definitions: %w", err)
	}
	out := make([]ReportDefinition, 0, len(rows))
	for _, row := range rows {
		def, err := s.reportDefinitionFromRow(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, nil
}

func (s *Service) reportDefinitionFromRow(ctx context.Context, row db.ReportDefinition) (ReportDefinition, error) {
	createdAt, _ := time.Parse(timeFormat, row.CreatedAt)
	def := ReportDefinition{
		ID:           row.ID,
		Name:         row.Name,
		Introduction: row.Introduction,
		Mode:         row.Mode,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    createdAt,
	}
	rules, err := s.queries.ListReportRules(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report rules: %w", err)
	}
	values, err := s.queries.ListReportRuleValues(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report rule values: %w", err)
	}
	valuesOf := make(map[int64][]string, len(rules))
	for _, v := range values {
		valuesOf[v.ReportRuleID] = append(valuesOf[v.ReportRuleID], v.Value)
	}
	for _, r := range rules {
		def.Rules = append(def.Rules, ReportRule{
			Attribute:   r.Attribute,
			DimensionID: r.DimensionID,
			Op:          r.Op,
			Values:      valuesOf[r.ID],
		})
	}
	listed, err := s.queries.ListReportDefinitionGoals(ctx, row.ID)
	if err != nil {
		return ReportDefinition{}, fmt.Errorf("list report goals: %w", err)
	}
	for _, l := range listed {
		switch l.List {
		case reportListPicked:
			def.Picked = append(def.Picked, l.GoalID)
		case reportListInclude:
			def.Include = append(def.Include, l.GoalID)
		case reportListExclude:
			def.Exclude = append(def.Exclude, l.GoalID)
		}
	}
	if def.FieldIDs, err = s.queries.ListReportDefinitionFields(ctx, row.ID); err != nil {
		return ReportDefinition{}, fmt.Errorf("list report fields: %w", err)
	}
	return def, nil
}

// SelectGoals returns what a Report Definition selects, each Goal once, with
// the fields the live draft lists per line (CONTEXT.md: Report Definition):
// the picked list, or the Goals that meet every rule, with Also include added
// and Leave out taken away. A Goal that was Done or Cancelled before the
// baseline drops out however it came in, so finished work is reported once,
// and isn't among the Goals Leave out kept out either. since reports whether
// an instant falls after the baseline, as DraftReport reads changes.
func (s *Service) SelectGoals(ctx context.Context, def ReportDefinition, since func(time.Time) bool) (ReportSelection, error) {
	candidates, excluded, err := s.reportCandidates(ctx, def, since)
	if err != nil {
		return ReportSelection{}, err
	}
	var ended []int64
	for _, sg := range slices.Concat(candidates, excluded) {
		if LifecycleEnded(sg.Goal.Lifecycle) {
			ended = append(ended, sg.Goal.ID)
		}
	}
	checkins, err := s.checkinsByGoal(ctx, ended)
	if err != nil {
		return ReportSelection{}, err
	}
	finished := func(sg SelectedGoal) bool { return finishedBeforeGiven(sg.Goal, checkins[sg.Goal.ID], since) }
	sel := ReportSelection{Goals: make([]SelectedGoal, 0, len(candidates))}
	for _, sg := range candidates {
		if !finished(sg) {
			sel.Goals = append(sel.Goals, sg)
		}
	}
	for _, sg := range excluded {
		if !finished(sg) {
			sel.LeftOut = append(sel.LeftOut, sg.Goal)
		}
	}
	return sel, nil
}

// reportCandidates returns the Goals def selects before finished work drops
// out: its picked list, or the Goals that meet every rule or are in Also
// include, less those in Leave out; and those in Leave out that meet every
// rule. It runs the same few queries however many Goals there are.
func (s *Service) reportCandidates(ctx context.Context, def ReportDefinition, since func(time.Time) bool) (candidates, excluded []SelectedGoal, err error) {
	if def.Mode == ReportModePicked {
		goals, err := s.goalsByID(ctx, def.Picked)
		if err != nil {
			return nil, nil, err
		}
		picked, err := s.selectedGoals(ctx, goals)
		if err != nil {
			return nil, nil, err
		}
		for i := range picked {
			picked[i].Via = SelectedByPicking
		}
		return picked, nil, nil
	}

	goals, err := s.ListGoals(ctx)
	if err != nil {
		return nil, nil, err
	}
	selected, err := s.selectedGoals(ctx, goals)
	if err != nil {
		return nil, nil, err
	}
	valuesByGoal, err := s.goalValuesByGoal(ctx)
	if err != nil {
		return nil, nil, err
	}
	orgs, err := s.ruleOrgs(ctx, def.Rules)
	if err != nil {
		return nil, nil, err
	}
	subjects := make([]ruleSubject, 0, len(selected))
	var decidedByHistory []int64
	for _, sg := range selected {
		subject := ruleSubject{SelectedGoal: sg, values: valuesByGoal[sg.Goal.ID], orgs: orgs}
		subjects = append(subjects, subject)
		if lifecycleHistoryDecides(def.Rules, subject) {
			decidedByHistory = append(decidedByHistory, sg.Goal.ID)
		}
	}
	checkins, err := s.checkinsByGoal(ctx, decidedByHistory)
	if err != nil {
		return nil, nil, err
	}
	for _, subject := range subjects {
		h := goalHistory{goal: subject.Goal, checkins: checkins[subject.Goal.ID]}
		sg, matched := subject.SelectedGoal, meetsRulesGiven(def.Rules, subject, h, since)
		switch {
		case slices.Contains(def.Exclude, sg.Goal.ID):
			if matched {
				excluded = append(excluded, sg)
			}
		case matched:
			sg.Via = SelectedByRules
			candidates = append(candidates, sg)
		case slices.Contains(def.Include, sg.Goal.ID):
			sg.Via = SelectedByAlsoInclude
			candidates = append(candidates, sg)
		}
	}
	return candidates, excluded, nil
}

// lifecycleHistoryDecides reports whether the Goal meets rules turns on
// its Check-ins: the first rule it doesn't meet is a Lifecycle rule, which it
// still passes if it changed Lifecycle since the baseline.
func lifecycleHistoryDecides(rules []ReportRule, subject ruleSubject) bool {
	for _, r := range rules {
		if !r.matches(subject) {
			return r.Attribute == RuleLifecycle
		}
	}
	return false
}

// meetsRulesGiven reports whether the Goal meets every rule, given its history
// h, which holds its Check-ins when lifecycleHistoryDecides says they are
// needed. A Goal that changed Lifecycle since the baseline passes a Lifecycle
// rule it no longer meets, so its change is reported once.
func meetsRulesGiven(rules []ReportRule, subject ruleSubject, h goalHistory, since func(time.Time) bool) bool {
	for _, r := range rules {
		if r.matches(subject) {
			continue
		}
		if r.Attribute != RuleLifecycle || !h.lifecycleChanged(since) {
			return false
		}
	}
	return true
}

// finishedBeforeGiven reports whether g was Done or Cancelled before the
// baseline: it is now, and the Check-in that moved it there, found among its
// Check-ins, newest first, in checkins, isn't since.
func finishedBeforeGiven(g Goal, checkins []Checkin, since func(time.Time) bool) bool {
	if !LifecycleEnded(g.Lifecycle) {
		return false
	}
	// Newest first, so the first that moved it to its Lifecycle moved it last.
	for _, c := range checkins {
		if c.LifecycleChange.To == g.Lifecycle {
			return !since(c.CreatedAt)
		}
	}
	// No Check-in records the move, so it can't have been since the baseline.
	return true
}

// checkinsByGoal reads the Check-ins of each of goalIDs, newest first, keyed by
// Goal ID, in one query: what selecting a Report's Goals reads of their
// Lifecycle changes. Unlike ListCheckins it leaves out the Milestone changes
// and discarded Draft Highlights each Check-in recorded. No Goals reads
// nothing.
func (s *Service) checkinsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Checkin, error) {
	out := map[int64][]Checkin{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListCheckinsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list checkins: %w", err)
	}
	for _, r := range rows {
		out[r.Checkin.GoalID] = append(out[r.Checkin.GoalID], checkinFromRow(r.Checkin, r.Account, r.Account_2))
	}
	return out, nil
}

// goalsByID reads the Goals ids name, in that order, in one query, returning
// ErrNotFound when one doesn't exist.
func (s *Service) goalsByID(ctx context.Context, ids []int64) ([]Goal, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.queries.ListGoalsByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	byID := make(map[int64]Goal, len(rows))
	for _, r := range rows {
		byID[r.Goal.ID] = goalFromRow(r.Goal, r.Account)
	}
	goals := make([]Goal, 0, len(ids))
	for _, id := range ids {
		g, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: goal %d", ErrNotFound, id)
		}
		goals = append(goals, g)
	}
	return goals, nil
}

// selectedGoals is goals as a Report lists them, in order, each with its
// Health: its latest Check-in's, empty when it has none.
func (s *Service) selectedGoals(ctx context.Context, goals []Goal) ([]SelectedGoal, error) {
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	latest, err := s.LatestCheckinsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]SelectedGoal, 0, len(goals))
	for _, g := range goals {
		out = append(out, SelectedGoal{Goal: g, Health: latest[g.ID].Health})
	}
	return out, nil
}

// ruleSubject is what Report rules test a Goal on: the Goal, its Owner-set
// Health and its Dimension values, beside the Orgs its rules name.
type ruleSubject struct {
	SelectedGoal
	values []DimensionValue
	orgs   ruleOrgs
}

// ruleOrgs are the Orgs a definition's Org rules name: the ids of the
// people in each, keyed by the rule value naming its person.
type ruleOrgs map[string]map[int64]bool

// ruleOrgs walks the Org of each person an Org rule among rules names, by
// the Managers as they are now, so a Manager change moves Goals in or out at
// the next draft (CONTEXT.md: Org).
func (s *Service) ruleOrgs(ctx context.Context, rules []ReportRule) (ruleOrgs, error) {
	orgs := ruleOrgs{}
	for _, r := range rules {
		if r.Attribute != RuleOrg {
			continue
		}
		for _, v := range r.Values {
			if _, ok := orgs[v]; ok {
				continue
			}
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("org rule value %q: %w", v, err)
			}
			people, err := s.Org(ctx, id)
			if err != nil {
				return nil, err
			}
			orgs[v] = make(map[int64]bool, len(people))
			for _, p := range people {
				orgs[v][p.ID] = true
			}
		}
	}
	return orgs, nil
}

// matches reports whether the Goal meets the rule: has any of its values for
// "is" and "is any of", or none of them for "is not". A Top-level rule, which
// has no values, asks whether the Goal is Top-level.
func (r ReportRule) matches(subject ruleSubject) bool {
	if r.Attribute == RuleTopLevel {
		return subject.Goal.TopLevel == (r.Op != RuleIsNot)
	}
	var has func(value string) bool
	switch r.Attribute {
	case RuleDimension:
		has = func(value string) bool {
			return slices.ContainsFunc(subject.values, func(v DimensionValue) bool {
				return strconv.FormatInt(v.ID, 10) == value
			})
		}
	case RuleOwner:
		// The Owner of record, never a Delegate; a departed Owner stays it.
		has = func(value string) bool { return strconv.FormatInt(subject.Goal.Owner.ID, 10) == value }
	case RuleOrg:
		// The Owner of record's place in the Org, never a Delegate's.
		has = func(value string) bool { return subject.orgs[value][subject.Goal.Owner.ID] }
	case RuleLifecycle:
		has = func(value string) bool { return subject.Goal.Lifecycle == value }
	case RuleHealth:
		// No Health is no value, so it meets only "is not".
		has = func(value string) bool { return subject.Health == value }
	default:
		return false
	}
	listed := slices.ContainsFunc(r.Values, has)
	if r.Op == RuleIsNot {
		return !listed
	}
	return listed
}

// ReportRuleManagers returns everyone who is the Manager of someone other than
// themselves, by id, for the Report builder to offer an Org rule on. A
// self-reference, as a directory can say, puts no one under its person, so is
// left out: with none, AnyoneHasManager is false and an Org is only its person
// (CONTEXT.md: Org).
func (s *Service) ReportRuleManagers(ctx context.Context) ([]Account, error) {
	managed, err := s.queries.ListManagedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed accounts: %w", err)
	}
	var ids []int64
	for _, row := range managed {
		if *row.ManagerID != row.ID && !slices.Contains(ids, *row.ManagerID) {
			ids = append(ids, *row.ManagerID)
		}
	}
	slices.Sort(ids)
	out := make([]Account, 0, len(ids))
	for _, id := range ids {
		a, err := s.Account(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// GoalValuesByGoal returns every Goal's assigned Dimension values, keyed by
// Goal ID, as GoalValues returns one Goal's, in one query, so a page or file
// listing many Goals reads them all at once. A Goal with no values has no
// entry.
func (s *Service) GoalValuesByGoal(ctx context.Context) (map[int64][]DimensionValue, error) {
	return s.goalValuesByGoal(ctx)
}

// goalValuesByGoal returns every Goal's assigned Dimension values keyed by Goal
// id, as GoalValues reads one Goal's, for applying a Report Definition's
// Dimension filter across the candidates.
func (s *Service) goalValuesByGoal(ctx context.Context) (map[int64][]DimensionValue, error) {
	valRows, err := s.queries.ListAllGoalValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	byGoal := make(map[int64][]DimensionValue, len(valRows))
	for _, r := range valRows {
		byGoal[r.GoalID] = append(byGoal[r.GoalID], goalValueFromRow(r.DimensionValue, r.Dimension))
	}
	return byGoal, nil
}

// dedupeIDs returns ids with duplicates removed, keeping first-seen order.
func dedupeIDs[T comparable](ids []T) []T {
	seen := make(map[T]bool, len(ids))
	out := make([]T, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
