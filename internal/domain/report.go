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
	// CreatedBy is the id of the Account that saved it, who with an Admin may
	// edit it.
	CreatedBy int64
	CreatedAt time.Time
}

// CanEditReportDefinition reports whether actor may edit def: its creator or
// an Admin may. Anyone signed in may save and publish one.
func CanEditReportDefinition(actor Account, def ReportDefinition) bool {
	return actor.IsAdmin || actor.ID == def.CreatedBy
}

// The modes a Report Definition selects its Goals in.
const (
	ReportModeRules  = "rules"
	ReportModePicked = "picked"
)

// ReportRule is one Report rule: a condition on a Goal's attribute (CONTEXT.md:
// Report rule). The values within a rule are ORed: a Goal meets "is" or "is
// any of" by having any of them, and "is not" by having none of them.
type ReportRule struct {
	// Attribute is RuleDimension, RuleOwner, RuleChain, RuleLifecycle,
	// RuleHealth or RuleTopLevel.
	Attribute string
	// DimensionID is the Dimension a RuleDimension rule tests, and 0 for every
	// other attribute.
	DimensionID int64
	// Op is RuleIs (exactly one value), RuleIsAnyOf or RuleIsNot.
	Op string
	// Values are what the rule tests for: Dimension value ids (of DimensionID)
	// or Account ids (of Owners, or of the people whose Chain) in decimal, or Lifecycle or Health names. A RuleTopLevel
	// rule has none: it is "is Top-level" or "is not Top-level".
	Values []string
}

// The attributes a Report rule can test. Fields are not among them: a Report
// shows Fields but never selects by them (ADR 0005, ADR 0007).
const (
	RuleDimension = "dimension"
	RuleOwner     = "owner"
	// RuleChain tests the Owner's Chain: whether the Owner is in the Chain of
	// any of the people it names, by their Managers as they are when the
	// Report is drafted. Like the Owner rule, it never looks at Delegates.
	RuleChain     = "chain"
	RuleLifecycle = "lifecycle"
	RuleHealth    = "health"
	RuleTopLevel  = "top-level"
)

// The operators of a Report rule.
const (
	RuleIs      = "is"
	RuleIsAnyOf = "is any of"
	RuleIsNot   = "is not"
)

// The lists a Report Definition names Goals in, as stored.
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
}

// SaveReportDefinition saves a reusable Report Definition. Anyone signed in may
// save one (CONTEXT.md: Report Definition); actorID records who created it. It
// refuses each input it can't take with an *InputError naming it.
func (s *Service) SaveReportDefinition(ctx context.Context, actorID int64, in SaveReportDefinitionInput) (ReportDefinition, error) {
	in = normalizeReportDefinition(in)
	if err := s.validateReportDefinition(ctx, in, nil); err != nil {
		return ReportDefinition{}, err
	}

	var id int64
	if err := s.WithinTx(ctx, func(tx *Service) error {
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
// Only its creator or an Admin may edit it (ErrNotAuthorized); ErrNotFound
// when there is no such definition.
func (s *Service) UpdateReportDefinition(ctx context.Context, actorID, id int64, in SaveReportDefinitionInput) (ReportDefinition, error) {
	def, err := s.GetReportDefinition(ctx, id)
	if err != nil {
		return ReportDefinition{}, err
	}
	actor, err := s.queries.GetAccount(ctx, actorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReportDefinition{}, fmt.Errorf("look up actor: %w", err)
	}
	if err != nil || !CanEditReportDefinition(accountFromRow(actor), def) {
		return ReportDefinition{}, fmt.Errorf("%w: only its creator or an Admin may edit %s", ErrNotAuthorized, def.Name)
	}
	in = normalizeReportDefinition(in)
	if err := s.validateReportDefinition(ctx, in, def.FieldIDs); err != nil {
		return ReportDefinition{}, err
	}
	if err := s.WithinTx(ctx, func(tx *Service) error {
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
		problems = append(problems, inputError(ReportInputName, "a Report Definition needs a name"))
	}
	switch in.Mode {
	case ReportModeRules:
		if len(in.Rules) == 0 {
			problems = append(problems, inputError(ReportInputRules, "a rules Report Definition needs a rule"))
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
			problems = append(problems, inputError(ReportInputPicked, "a rules Report Definition picks no Goals by hand"))
		}
	case ReportModePicked:
		if len(in.Picked) == 0 {
			problems = append(problems, inputError(ReportInputPicked, "a picked Report Definition needs a Goal"))
		}
		if len(in.Rules) > 0 {
			problems = append(problems, inputError(ReportInputRules, "a picked Report Definition has no rules"))
		}
		if len(in.Include) > 0 {
			problems = append(problems, inputError(ReportInputInclude, "a picked Report Definition has nothing to Also include"))
		}
		if len(in.Exclude) > 0 {
			problems = append(problems, inputError(ReportInputExclude, "a picked Report Definition has nothing to Leave out"))
		}
	default:
		problems = append(problems, inputError(ReportInputMode, "a Report Definition selects by rules or picked Goals, not %q", in.Mode))
	}
	for input, ids := range map[string][]int64{ReportInputPicked: in.Picked, ReportInputInclude: in.Include, ReportInputExclude: in.Exclude} {
		for _, id := range ids {
			if _, err := s.queries.GetGoal(ctx, id); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("look up goal: %w", err)
				}
				problems = append(problems, inputError(input, "goal %d does not exist", id))
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
// a value of its Dimension, an Account (for an Owner or a Chain rule), a
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
		return "a rule needs a value", nil
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
	case RuleOwner, RuleChain:
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
		known = func(v string) (bool, error) { return slices.Contains(lifecycles, v), nil }
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

// lifecycles are every Lifecycle a Goal can be in.
var lifecycles = []string{LifecycleProposed, LifecycleActive, LifecycleOnHold, LifecycleDone, LifecycleCancelled}

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

// SelectGoals returns the Goals a Report Definition selects, each once, with the
// fields the live draft lists per line (CONTEXT.md: Report Definition): the
// picked list, or the Goals that meet every rule, with Also include added and
// Leave out taken away. A Goal that was Done or Cancelled before the baseline
// drops out however it came in, so finished work is reported once. since
// reports whether an instant falls after the baseline, as DraftReport reads
// changes.
func (s *Service) SelectGoals(ctx context.Context, def ReportDefinition, since func(time.Time) bool) ([]SelectedGoal, error) {
	candidates, err := s.reportCandidates(ctx, def, since)
	if err != nil {
		return nil, err
	}
	out := make([]SelectedGoal, 0, len(candidates))
	for _, sg := range candidates {
		finished, err := s.finishedBefore(ctx, sg.Goal, since)
		if err != nil {
			return nil, err
		}
		if !finished {
			out = append(out, sg)
		}
	}
	return out, nil
}

// reportCandidates returns the Goals def selects before finished work drops
// out: its picked list, or the Goals that meet every rule or are in Also
// include, less those in Leave out.
func (s *Service) reportCandidates(ctx context.Context, def ReportDefinition, since func(time.Time) bool) ([]SelectedGoal, error) {
	if def.Mode == ReportModePicked {
		out := make([]SelectedGoal, 0, len(def.Picked))
		for _, id := range def.Picked {
			g, err := s.loadGoal(ctx, id)
			if err != nil {
				return nil, err
			}
			sg, err := s.selectedGoal(ctx, g)
			if err != nil {
				return nil, err
			}
			out = append(out, sg)
		}
		return out, nil
	}

	goals, err := s.ListGoals(ctx)
	if err != nil {
		return nil, err
	}
	valuesByGoal, err := s.goalValuesByGoal(ctx)
	if err != nil {
		return nil, err
	}
	chains, err := s.ruleChains(ctx, def.Rules)
	if err != nil {
		return nil, err
	}
	var out []SelectedGoal
	for _, g := range goals {
		if slices.Contains(def.Exclude, g.ID) {
			continue
		}
		sg, err := s.selectedGoal(ctx, g)
		if err != nil {
			return nil, err
		}
		meets, err := s.meetsRules(ctx, def.Rules, ruleSubject{SelectedGoal: sg, values: valuesByGoal[g.ID], chains: chains}, since)
		if err != nil {
			return nil, err
		}
		if meets || slices.Contains(def.Include, g.ID) {
			out = append(out, sg)
		}
	}
	return out, nil
}

// meetsRules reports whether the Goal meets every rule. A Goal that changed
// Lifecycle since the baseline passes a Lifecycle rule it no longer meets, so
// its change is reported once.
func (s *Service) meetsRules(ctx context.Context, rules []ReportRule, subject ruleSubject, since func(time.Time) bool) (bool, error) {
	for _, r := range rules {
		if r.matches(subject) {
			continue
		}
		if r.Attribute != RuleLifecycle {
			return false, nil
		}
		h, err := s.goalHistory(ctx, subject.Goal)
		if err != nil {
			return false, err
		}
		if !h.lifecycleChanged(since) {
			return false, nil
		}
	}
	return true, nil
}

// finishedBefore reports whether g was Done or Cancelled before the baseline:
// it is now, and the Check-in that moved it there isn't since.
func (s *Service) finishedBefore(ctx context.Context, g Goal, since func(time.Time) bool) (bool, error) {
	if g.Lifecycle != LifecycleDone && g.Lifecycle != LifecycleCancelled {
		return false, nil
	}
	checkins, err := s.ListCheckins(ctx, g.ID)
	if err != nil {
		return false, err
	}
	// Newest first, so the first that moved it to its Lifecycle moved it last.
	for _, c := range checkins {
		if c.LifecycleChange.To == g.Lifecycle {
			return !since(c.CreatedAt), nil
		}
	}
	// No Check-in records the move, so it can't have been since the baseline.
	return true, nil
}

// selectedGoal is g as a Report lists it, with its Health: its latest
// Check-in's, empty when it has none.
func (s *Service) selectedGoal(ctx context.Context, g Goal) (SelectedGoal, error) {
	latest, err := s.queries.GetLatestCheckin(ctx, g.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SelectedGoal{}, fmt.Errorf("get latest checkin: %w", err)
	}
	return SelectedGoal{Goal: g, Health: latest.Health}, nil
}

// ruleSubject is what Report rules test a Goal on: the Goal, its Owner-set
// Health and its Dimension values, beside the Chains its rules name.
type ruleSubject struct {
	SelectedGoal
	values []DimensionValue
	chains ruleChains
}

// ruleChains are the Chains a definition's Chain rules name: the ids of the
// people in each, keyed by the rule value naming its person.
type ruleChains map[string]map[int64]bool

// ruleChains walks the Chain of each person a Chain rule among rules names, by
// the Managers as they are now, so a Manager change moves Goals in or out at
// the next draft (CONTEXT.md: Chain).
func (s *Service) ruleChains(ctx context.Context, rules []ReportRule) (ruleChains, error) {
	chains := ruleChains{}
	for _, r := range rules {
		if r.Attribute != RuleChain {
			continue
		}
		for _, v := range r.Values {
			if _, ok := chains[v]; ok {
				continue
			}
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("chain rule value %q: %w", v, err)
			}
			people, err := s.Chain(ctx, id)
			if err != nil {
				return nil, err
			}
			chains[v] = make(map[int64]bool, len(people))
			for _, p := range people {
				chains[v][p.ID] = true
			}
		}
	}
	return chains, nil
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
	case RuleChain:
		// The Owner of record's place in the Chain, never a Delegate's.
		has = func(value string) bool { return subject.chains[value][subject.Goal.Owner.ID] }
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

// ReportRuleManagers returns everyone who is someone's Manager, by id, for the
// Report builder to offer a Chain rule on: with none, no one has a Manager and
// a Chain is only its person (CONTEXT.md: Chain).
func (s *Service) ReportRuleManagers(ctx context.Context) ([]Account, error) {
	managed, err := s.queries.ListManagedAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managed accounts: %w", err)
	}
	var ids []int64
	for _, row := range managed {
		if !slices.Contains(ids, *row.ManagerID) {
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

// goalValuesByGoal returns every Goal's assigned Dimension values keyed by Goal
// id, for applying a Report Definition's Dimension filter across the candidates.
func (s *Service) goalValuesByGoal(ctx context.Context) (map[int64][]DimensionValue, error) {
	valRows, err := s.queries.ListAllGoalValues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list goal values: %w", err)
	}
	byGoal := make(map[int64][]DimensionValue, len(valRows))
	for _, r := range valRows {
		byGoal[r.GoalID] = append(byGoal[r.GoalID], dimensionValueFromRow(r.DimensionValue))
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
