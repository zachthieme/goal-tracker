package domain

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReportSummary is what the Reports list says of a Report Definition at a
// glance, read from its draft against the default baseline: its previous
// publication, or 30 days ago when it has none.
type ReportSummary struct {
	Definition ReportDefinition
	// Goals is how many Goals the draft selects, and Health how many of them
	// have each Owner-set Health.
	Goals  int
	Health HealthCounts
	// LastPublication is the definition's latest publication, nil when it has
	// never been published.
	LastPublication *Publication
	// Changes is how many of the Goals changed since the last publication or
	// entered or left the Report since it, each counted once; nil when there
	// is no last publication.
	Changes *int
	// Scope is the definition's selection in plain words, e.g. "Team:
	// Platform or Identity · Active · 1 added, 1 left out" or "5 Goals picked
	// by hand".
	Scope string
}

// HealthCounts are how many of a Report's Goals have each Owner-set Health,
// and how many have none: a Proposed Goal, or one not yet checked in on.
// Together they add up to the Report's Goals.
type HealthCounts struct {
	Green, Yellow, Red, None int
}

// ReportSummaries summarises every saved Report Definition, ordered by name,
// each drafted for its counts.
func (s *Service) ReportSummaries(ctx context.Context) ([]ReportSummary, error) {
	defs, err := s.ListReportDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ReportSummary, 0, len(defs))
	for _, def := range defs {
		sum, err := s.ReportSummary(ctx, def)
		if err != nil {
			return nil, err
		}
		out = append(out, sum)
	}
	return out, nil
}

// ReportSummary summarises the Report Definition def from its draft against
// the default baseline.
func (s *Service) ReportSummary(ctx context.Context, def ReportDefinition) (ReportSummary, error) {
	r, err := s.DraftReport(ctx, def, time.Time{})
	if err != nil {
		return ReportSummary{}, err
	}
	sum := ReportSummary{Definition: def}
	for _, sg := range r.selected() {
		sum.Goals++
		switch sg.Health {
		case HealthGreen:
			sum.Health.Green++
		case HealthYellow:
			sum.Health.Yellow++
		case HealthRed:
			sum.Health.Red++
		default:
			sum.Health.None++
		}
	}
	if r.Previous.ID != 0 {
		pub, err := s.GetPublication(ctx, r.Previous.ID)
		if err != nil {
			return ReportSummary{}, err
		}
		sum.LastPublication = &pub
		changes, err := s.changedGoals(ctx, r)
		if err != nil {
			return ReportSummary{}, err
		}
		sum.Changes = &changes
	}
	if sum.Scope, err = s.reportScope(ctx, def); err != nil {
		return ReportSummary{}, err
	}
	return sum, nil
}

// selected are the Goals the Report selects with their Health, exceptions
// first.
func (r Report) selected() []SelectedGoal {
	out := make([]SelectedGoal, 0, len(r.Exceptions)+len(r.Lines))
	for _, b := range r.Exceptions {
		out = append(out, SelectedGoal{Goal: b.Goal, Health: b.Health})
	}
	return append(out, r.Lines...)
}

// AlsoIncludedOnly returns the ids of the Goals r selects only because its
// Definition lists them to Also include: they don't meet its rules at r's
// baseline. A picked Definition has none.
func (s *Service) AlsoIncludedOnly(ctx context.Context, r Report) (map[int64]bool, error) {
	def := r.Definition
	if def.Mode != ReportModeRules || len(def.Include) == 0 {
		return nil, nil
	}
	valuesByGoal, err := s.goalValuesByGoal(ctx)
	if err != nil {
		return nil, err
	}
	chains, err := s.ruleChains(ctx, def.Rules)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, sg := range r.selected() {
		if !slices.Contains(def.Include, sg.Goal.ID) {
			continue
		}
		meets, err := s.meetsRules(ctx, def.Rules, ruleSubject{SelectedGoal: sg, values: valuesByGoal[sg.Goal.ID], chains: chains}, s.since(r))
		if err != nil {
			return nil, err
		}
		if !meets {
			out[sg.Goal.ID] = true
		}
	}
	return out, nil
}

// changedGoals counts the Goals r reads as changed since its baseline — those
// it selects that were created, slipped a date, gained a Milestone, changed
// Lifecycle or changed Health since, and those that entered or left it — each
// once. A standing condition, such as staying Red, is no change.
func (s *Service) changedGoals(ctx context.Context, r Report) (int, error) {
	since := s.since(r)
	changed := map[int64]bool{}
	for _, sg := range r.selected() {
		h, err := s.goalHistory(ctx, sg.Goal)
		if err != nil {
			return 0, err
		}
		milestones, err := s.ListMilestones(ctx, sg.Goal.ID)
		if err != nil {
			return 0, err
		}
		if h.changed(since) || slices.ContainsFunc(milestones, func(m Milestone) bool { return since(m.CreatedAt) }) {
			changed[sg.Goal.ID] = true
		}
	}
	for _, c := range r.MembershipChanges {
		changed[c.Goal.ID] = true
	}
	return len(changed), nil
}

// reportScope says what def selects in plain words, its parts joined with
// " · ": a picked definition's count of Goals, or a rules definition's rules
// in order, then how many Goals it lists to Also include and to Leave out,
// each left off when none.
func (s *Service) reportScope(ctx context.Context, def ReportDefinition) (string, error) {
	if def.Mode == ReportModePicked {
		return pluralGoals(len(def.Picked)) + " picked by hand", nil
	}
	var parts []string
	for _, rule := range def.Rules {
		part, err := s.ruleScope(ctx, rule)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	var overrides []string
	if n := len(def.Include); n > 0 {
		overrides = append(overrides, fmt.Sprintf("%d added", n))
	}
	if n := len(def.Exclude); n > 0 {
		overrides = append(overrides, fmt.Sprintf("%d left out", n))
	}
	if len(overrides) > 0 {
		parts = append(parts, strings.Join(overrides, ", "))
	}
	return strings.Join(parts, " · "), nil
}

// ruleScope says one Report rule in plain words: "<attribute>: <values>" for a
// Dimension, Owner or Health rule, a Lifecycle rule's values alone, "Owner is
// in the Chain of <people>" for a Chain rule, and "Top-level" or "Not
// top-level". Values are joined with " or ", after "not" for "is not".
func (s *Service) ruleScope(ctx context.Context, rule ReportRule) (string, error) {
	if rule.Attribute == RuleTopLevel {
		if rule.Op == RuleIsNot {
			return "Not top-level", nil
		}
		return "Top-level", nil
	}
	label, name := "", func(v string) (string, error) { return v, nil }
	switch rule.Attribute {
	case RuleDimension:
		dim, err := s.queries.GetDimension(ctx, rule.DimensionID)
		if err != nil {
			return "", fmt.Errorf("look up dimension: %w", err)
		}
		label = dim.Name
		name = func(v string) (string, error) {
			id, _ := strconv.ParseInt(v, 10, 64)
			val, err := s.queries.GetDimensionValue(ctx, id)
			if err != nil {
				return "", fmt.Errorf("look up dimension value: %w", err)
			}
			return val.Value, nil
		}
	case RuleOwner, RuleChain:
		label = "Owner"
		name = func(v string) (string, error) {
			id, _ := strconv.ParseInt(v, 10, 64)
			a, err := s.Account(ctx, id)
			if err != nil {
				return "", err
			}
			return a.Label(), nil
		}
	case RuleHealth:
		label = "Health"
	}
	names := make([]string, 0, len(rule.Values))
	for _, v := range rule.Values {
		n, err := name(v)
		if err != nil {
			return "", err
		}
		names = append(names, n)
	}
	text := strings.Join(names, " or ")
	if rule.Attribute == RuleChain {
		if rule.Op == RuleIsNot {
			return "Owner is not in the Chain of " + text, nil
		}
		return "Owner is in the Chain of " + text, nil
	}
	if rule.Op == RuleIsNot {
		text = "not " + text
	}
	if label == "" {
		return text, nil
	}
	return label + ": " + text, nil
}

// pluralGoals is n Goals, as "1 Goal" or "n Goals".
func pluralGoals(n int) string {
	if n == 1 {
		return "1 Goal"
	}
	return fmt.Sprintf("%d Goals", n)
}
