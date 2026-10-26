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
// Together they add up to the Report's Goals. CheckinOverdue is how many of
// them, whatever their Health, are Check-in overdue.
type HealthCounts struct {
	Green, Yellow, Red, None int
	CheckinOverdue           int
}

// HealthCounts counts r's selected Goals by Health, and its Check-in overdue
// ones by their badge, as drafted or frozen — only an exception block can be
// Check-in overdue, since a Check-in overdue Goal is an exception. It reads
// only r, so a publication counts as it was published, and every surface
// counts alike.
func (r Report) HealthCounts() HealthCounts {
	var c HealthCounts
	for _, sg := range r.selected() {
		switch sg.Health {
		case HealthGreen:
			c.Green++
		case HealthYellow:
			c.Yellow++
		case HealthRed:
			c.Red++
		default:
			c.None++
		}
	}
	for _, b := range r.Exceptions {
		if slices.Contains(b.Badges, BadgeCheckinOverdue) {
			c.CheckinOverdue++
		}
	}
	return c
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
	return s.SummarizeReport(ctx, r)
}

// SummarizeReport summarises the Report r already drafted, as ReportSummary
// does its definition's draft, against r's baseline, so a page holding a draft
// needn't draft it again.
func (s *Service) SummarizeReport(ctx context.Context, r Report) (ReportSummary, error) {
	def := r.Definition
	sum := ReportSummary{Definition: def, Goals: len(r.selected()), Health: r.HealthCounts()}
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
	scope, err := s.reportScope(ctx, r)
	if err != nil {
		return ReportSummary{}, err
	}
	sum.Scope = scope
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

// changedGoals counts the Goals r reads as changed since its baseline — those
// it selects that were created, slipped a date, gained a Milestone, changed
// Lifecycle or changed Health since, and those that entered or left it — each
// once. A standing condition, such as staying Red, is no change.
func (s *Service) changedGoals(ctx context.Context, r Report) (int, error) {
	since := s.since(r)
	selected := r.selected()
	goals := make([]Goal, 0, len(selected))
	for _, sg := range selected {
		goals = append(goals, sg.Goal)
	}
	histories, err := s.goalHistories(ctx, goals)
	if err != nil {
		return 0, err
	}
	milestones, err := s.milestonesByGoal(ctx, goalIDsOf(goals))
	if err != nil {
		return 0, err
	}
	changed := map[int64]bool{}
	for _, g := range goals {
		if histories[g.ID].changed(since) || slices.ContainsFunc(milestones[g.ID], func(m Milestone) bool { return since(m.CreatedAt) }) {
			changed[g.ID] = true
		}
	}
	for _, c := range r.MembershipChanges {
		changed[c.Goal.ID] = true
	}
	return len(changed), nil
}

// reportScope says what the drafted Report r's Definition selects in plain
// words, its parts joined with " · ": a picked definition's count of Goals,
// or a rules definition's rules in order, then how many Goals are in only
// through Also include and how many Leave out kept out that the rules would
// otherwise have selected, each left off when none.
func (s *Service) reportScope(ctx context.Context, r Report) (string, error) {
	def := r.Definition
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
	if n := r.Added; n > 0 {
		overrides = append(overrides, fmt.Sprintf("%d added", n))
	}
	if n := r.LeftOut; n > 0 {
		overrides = append(overrides, fmt.Sprintf("%d left out", n))
	}
	if len(overrides) > 0 {
		parts = append(parts, strings.Join(overrides, ", "))
	}
	return strings.Join(parts, " · "), nil
}

// ruleScope says one Report rule in plain words: "<attribute>: <values>" for a
// Dimension, Owner or Health rule, a Lifecycle rule's values alone, "Owner is
// in the org of <people>" for an Org rule, and "Top-level" or "Not
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
	case RuleOwner, RuleOrg:
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
	if rule.Attribute == RuleOrg {
		if rule.Op == RuleIsNot {
			return "Owner is not in the org of " + text, nil
		}
		return "Owner is in the org of " + text, nil
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
