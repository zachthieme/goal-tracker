package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ActivationRule is one rule of the minimum standard a Proposed Goal must meet
// to become Active (CONTEXT.md: Lifecycle, Incomplete).
type ActivationRule string

// The activation standard's rules: a Why it matters, an Owner, the Kind (Dated
// with a delivery date, or Ongoing), its measure (a Milestone or Metric, or,
// for an Ongoing Goal, a Metric), and a value in each required Dimension and
// Field.
const (
	ActivationWhyItMatters      ActivationRule = "why_it_matters"
	ActivationOwner             ActivationRule = "owner"
	ActivationKind              ActivationRule = "kind"
	ActivationMilestoneOrMetric ActivationRule = "milestone_or_metric"
	ActivationMetric            ActivationRule = "metric"
	ActivationValue             ActivationRule = "value"
)

// ActivationRequirement is one requirement of the activation standard, as a
// Goal or a definition as typed stands against it. Name is the required
// Dimension's or Field's, set only for ActivationValue. Input names the input
// that meets it ("" for the Owner, which no input does). Reason is the
// refusal ActivateGoal gives while it is unmet; it is empty once it is met,
// and for the measure of a Goal with no Kind, which the Kind's reason covers.
type ActivationRequirement struct {
	Rule   ActivationRule
	Name   string
	Met    bool
	Input  string
	Reason string
}

// DefinitionDraft is a Goal's definition as typed on the New goal or Define
// form: its Why it matters, its Kind and delivery date (zero when none
// parses), how many Milestones and Metrics it has, saved and typed, the
// Dimension values chosen, and the new value typed for each Dimension and the
// value typed in each Field, by ID. Its Owner is the person typing it.
type DefinitionDraft struct {
	WhyItMatters string
	Kind         string
	DeliveryDate time.Time
	Milestones   int
	Metrics      int
	Chosen       map[int64]bool
	NewValues    map[int64]string
	FieldValues  map[int64]string
}

// JudgeDefinition judges d against the activation standard, out of dims and
// fields, the Dimensions and Fields there are. A Retired Dimension or Field is
// never required; a required Dimension is met by a value chosen in it or, when
// it is Extendable, a new value typed, and a required Field by a value typed.
func JudgeDefinition(d DefinitionDraft, dims []Dimension, fields []Field) []ActivationRequirement {
	state := activationState{
		WhyItMatters: d.WhyItMatters,
		Owned:        true,
		Kind:         d.Kind,
		DeliveryDate: d.DeliveryDate,
		Milestones:   d.Milestones,
		Metrics:      d.Metrics,
	}
	for _, dim := range dims {
		if !dim.Required || dim.Retired {
			continue
		}
		set := dim.Extendable() && strings.TrimSpace(d.NewValues[dim.ID]) != ""
		for _, val := range dim.Values {
			set = set || d.Chosen[val.ID]
		}
		state.Required = append(state.Required, RequiredValue{Name: dim.Name, Set: set, DimensionID: dim.ID})
	}
	for _, f := range fields {
		if f.Required && !f.Retired {
			state.Required = append(state.Required, RequiredValue{Name: f.Name, Set: strings.TrimSpace(d.FieldValues[f.ID]) != "", FieldID: f.ID})
		}
	}
	return judgeActivation(state)
}

// activationState is what the activation standard reads of a Goal, saved or
// as typed: its Why it matters, whether it has an Owner, its Kind and
// delivery date, how many Milestones and Metrics it has, and each required
// Dimension and Field, set or not.
type activationState struct {
	WhyItMatters string
	Owned        bool
	Kind         string
	DeliveryDate time.Time
	Milestones   int
	Metrics      int
	Required     []RequiredValue
}

// judgeActivation judges f against the activation standard, a requirement
// per rule in checklist order: Why it matters, Owner, Kind, the measure, then
// each required Dimension and Field in f.Required's order. A saved Goal's Why
// it matters and Owner are guaranteed by CreateGoal, EditWhyItMatters and the
// schema, but the standard states them so it still holds if those invariants
// ever change.
func judgeActivation(f activationState) []ActivationRequirement {
	reqs := []ActivationRequirement{
		activationRequirement(ActivationWhyItMatters, strings.TrimSpace(f.WhyItMatters) != "", InputWhyItMatters,
			"a Goal must say why it matters to become Active"),
		activationRequirement(ActivationOwner, f.Owned, "", "a Goal needs an Owner to become Active"),
	}
	switch f.Kind {
	case GoalDated:
		reqs = append(reqs,
			activationRequirement(ActivationKind, !f.DeliveryDate.IsZero(), InputDeliveryDate, "a Dated Goal needs a delivery date"),
			activationRequirement(ActivationMilestoneOrMetric, f.Milestones+f.Metrics > 0, InputHowYoullKnow,
				"a Dated Goal needs at least one Milestone or Metric"))
	case GoalOngoing:
		reqs = append(reqs,
			activationRequirement(ActivationKind, true, InputKind, ""),
			activationRequirement(ActivationMetric, f.Metrics > 0, InputHowYoullKnow, "an Ongoing Goal needs at least one Metric"))
	default:
		reqs = append(reqs,
			activationRequirement(ActivationKind, false, InputKind, "a Goal must be marked Dated or Ongoing to become Active"),
			activationRequirement(ActivationMilestoneOrMetric, f.Milestones+f.Metrics > 0, InputHowYoullKnow, ""))
	}
	for _, r := range f.Required {
		input := DimensionInput(r.DimensionID)
		if r.FieldID != 0 {
			input = FieldInput(r.FieldID)
		}
		req := activationRequirement(ActivationValue, r.Set, input, fmt.Sprintf("a Goal needs a value in %s to become Active", r.Name))
		req.Name = r.Name
		reqs = append(reqs, req)
	}
	return reqs
}

// activationRequirement is a requirement of rule, met or not, met by input,
// carrying reason only while it is unmet.
func activationRequirement(rule ActivationRule, met bool, input, reason string) ActivationRequirement {
	if met {
		reason = ""
	}
	return ActivationRequirement{Rule: rule, Met: met, Input: input, Reason: reason}
}

// savedGoalActivationState is what the activation standard reads of g, saved
// with milestones Milestones, metrics Metrics and required, its
// RequiredValues.
func savedGoalActivationState(g Goal, milestones, metrics int, required []RequiredValue) activationState {
	return activationState{
		WhyItMatters: g.WhyItMatters,
		Owned:        g.Owner.ID != 0,
		Kind:         g.Kind,
		DeliveryDate: g.DeliveryDate,
		Milestones:   milestones,
		Metrics:      metrics,
		Required:     required,
	}
}

// judgeGoalActivation judges the saved Goal g against the activation
// standard, reading its Milestones, Metrics and required values.
func (s *Service) judgeGoalActivation(ctx context.Context, g Goal) ([]ActivationRequirement, error) {
	milestones, metrics, err := s.countMilestonesAndMetrics(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	required, err := s.RequiredValues(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	return judgeActivation(savedGoalActivationState(g, milestones, metrics, required)), nil
}

// activationRefusal is ActivateGoal's refusal of a Goal judged reqs: a
// validation error per unmet requirement that gives a reason, joined in
// order, or nil when there is none.
func activationRefusal(reqs []ActivationRequirement) error {
	var reasons []error
	for _, r := range reqs {
		if !r.Met && r.Reason != "" {
			reasons = append(reasons, Refuse(ErrValidation, "%s", r.Reason))
		}
	}
	return errors.Join(reasons...)
}
