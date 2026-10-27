package domain

import (
	"context"
	"fmt"
)

// GoalDefinitionPage is everything the define page, the New goal form
// finishing a Proposed Goal, shows, read by Service.GoalDefinitionPage (ADR
// 0010).
type GoalDefinitionPage struct {
	Goal Goal
	// Values are the Dimension values the Goal carries and FieldValues what
	// its Fields hold, Retired ones included (GoalValues, GoalFields): the
	// form as saved.
	Values      []DimensionValue
	FieldValues []FieldValue
	// Milestones and Metrics are the Goal's own, which show read-only.
	Milestones []Milestone
	Metrics    []Metric
	// Accepted are the parents the Goal contributes to (ParentLinks), and
	// Pending those it has asked to and is waiting on (PendingParentLinks).
	Accepted []GoalLink
	Pending  []GoalLink
	// Parents are the Goals picked to contribute to, and Candidates the rest
	// of its ParentCandidates, split as PickParents splits them. ParentHealth
	// is the Health of each Active picked parent's latest Check-in, keyed by
	// Goal ID; one with no Check-in has no entry.
	Parents      []Goal
	ParentHealth map[int64]string
	Candidates   []Goal
	// Dimensions and Fields are those still offered for setting
	// (OfferedDimensions, OfferedFields).
	Dimensions []Dimension
	Fields     []Field
}

// GoalDefinitionPage reads everything the define page for g shows, running
// each query once, with the parents parentIDs picks (ADR 0010). g is the Goal
// DefinableGoal gave, so who may see the page is judged there, once; the
// parent links read for the page are the ones its candidates leave out.
func (s *Service) GoalDefinitionPage(ctx context.Context, g Goal, parentIDs []int64) (GoalDefinitionPage, error) {
	p := GoalDefinitionPage{Goal: g}
	var err error
	if p.Values, err = s.GoalValues(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, err
	}
	if p.FieldValues, err = s.GoalFields(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, err
	}
	if p.Milestones, err = s.ListMilestones(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, fmt.Errorf("load milestones: %w", err)
	}
	if p.Metrics, err = s.ListMetrics(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, fmt.Errorf("load metrics: %w", err)
	}
	if p.Accepted, err = s.ParentLinks(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, err
	}
	if p.Pending, err = s.PendingParentLinks(ctx, g.ID); err != nil {
		return GoalDefinitionPage{}, err
	}
	candidates, err := s.parentCandidatesOf(ctx, g.ID, p.Accepted, p.Pending)
	if err != nil {
		return GoalDefinitionPage{}, err
	}
	p.Parents, p.Candidates = PickParents(candidates, parentIDs)
	if p.ParentHealth, err = s.definitionParentHealth(ctx, p.Parents); err != nil {
		return GoalDefinitionPage{}, err
	}
	dims, err := s.ListDimensions(ctx)
	if err != nil {
		return GoalDefinitionPage{}, fmt.Errorf("load dimensions: %w", err)
	}
	fields, err := s.ListFields(ctx)
	if err != nil {
		return GoalDefinitionPage{}, fmt.Errorf("load fields: %w", err)
	}
	p.Dimensions, p.Fields = OfferedDimensions(dims), OfferedFields(fields)
	return p, nil
}

// PickParents splits goals into the parents picked by ids, in that order,
// each once, and the rest, in goals' order. An id that is none of goals picks
// nothing.
func PickParents(goals []Goal, ids []int64) (picked, rest []Goal) {
	byID := map[int64]Goal{}
	for _, g := range goals {
		byID[g.ID] = g
	}
	for _, id := range ids {
		g, ok := byID[id]
		if !ok {
			continue
		}
		delete(byID, id)
		picked = append(picked, g)
	}
	for _, g := range goals {
		if _, left := byID[g.ID]; left {
			rest = append(rest, g)
		}
	}
	return picked, rest
}

// definitionParentHealth is the Health of each Active one of goals' latest
// Check-in, keyed by Goal ID, read in one query, and none when no Goal is
// Active.
func (s *Service) definitionParentHealth(ctx context.Context, goals []Goal) (map[int64]string, error) {
	var active []int64
	for _, g := range goals {
		if g.Lifecycle == LifecycleActive {
			active = append(active, g.ID)
		}
	}
	latest, err := s.LatestCheckinsByGoal(ctx, active)
	if err != nil {
		return nil, fmt.Errorf("load parent goals' check-ins: %w", err)
	}
	health := make(map[int64]string, len(latest))
	for id, c := range latest {
		health[id] = c.Health
	}
	return health, nil
}
