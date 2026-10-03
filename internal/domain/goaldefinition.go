package domain

import (
	"context"
	"maps"
	"slices"
	"time"
)

// DefinedGoalInput is the create-a-defined-Goal command's input: everything
// the single-page New goal form gathers. Only Title, So What and the Owner are
// required; the rest defines as much of the Goal as the Owner has to hand.
type DefinedGoalInput struct {
	Title  string
	SoWhat string
	// OwnerID is the acting person, who owns the Goal.
	OwnerID int64
	// Kind is GoalDated, with DeliveryDate, GoalOngoing, or "" for not chosen.
	Kind         string
	DeliveryDate time.Time
	// CadenceDays is how often a Check-in is expected; 0 leaves the default.
	CadenceDays int
	Milestones  []MilestoneDefinition
	Metrics     []MetricDefinition
	// ValueIDs are Dimension values to give the Goal. NewValues names a value
	// to add to an Extendable Dimension's list and give the Goal, by Dimension
	// ID (CONTEXT.md: Extendable). FieldValues is the Goal's value in each
	// Field, by Field ID.
	ValueIDs    []int64
	NewValues   map[int64]string
	FieldValues map[int64]string
	// ParentIDs are the Goals this one is to contribute to, each requested as
	// RequestLink requests it (CONTEXT.md: Contributes to).
	ParentIDs []int64
	// Activate moves the Goal to Active once it is defined, through the
	// activation gate (CONTEXT.md: Lifecycle).
	Activate bool
}

// MilestoneDefinition is a Milestone to add to a Goal being defined.
type MilestoneDefinition struct {
	Name string
	Date time.Time
}

// MetricDefinition is a Metric to add to a Goal being defined.
type MetricDefinition struct {
	Name       string
	Unit       string
	Direction  string
	Baseline   float64
	Target     float64
	TargetDate time.Time
}

// CreateDefinedGoal creates a Goal with its whole definition in one
// transaction, composing the commands that define each part: its Kind,
// cadence, Milestones, Metrics, Dimension values, Fields and parent links,
// and, with Activate, the activation gate. It is all or nothing: any error
// leaves nothing saved.
func (s *Service) CreateDefinedGoal(ctx context.Context, in DefinedGoalInput) (Goal, error) {
	var g Goal
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		g, err = tx.writeDefinedGoal(ctx, in)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	return g, nil
}

// writeDefinedGoal creates the Goal and writes each part of its definition,
// within the transaction the caller holds.
func (s *Service) writeDefinedGoal(ctx context.Context, in DefinedGoalInput) (Goal, error) {
	g, err := s.CreateGoal(ctx, CreateGoalInput{Title: in.Title, SoWhat: in.SoWhat, OwnerID: in.OwnerID})
	if err != nil {
		return Goal{}, err
	}
	switch in.Kind {
	case GoalDated:
		_, err = s.MarkGoalDated(ctx, g.ID, in.DeliveryDate)
	case GoalOngoing:
		_, err = s.MarkGoalOngoing(ctx, g.ID)
	}
	if err != nil {
		return Goal{}, err
	}
	if in.CadenceDays != 0 {
		if _, err := s.SetCadence(ctx, g.ID, in.CadenceDays); err != nil {
			return Goal{}, err
		}
	}
	for _, m := range in.Milestones {
		if _, err := s.AddMilestone(ctx, AddMilestoneInput{GoalID: g.ID, Name: m.Name, TargetDate: m.Date}); err != nil {
			return Goal{}, err
		}
	}
	for _, m := range in.Metrics {
		if _, err := s.AddMetric(ctx, AddMetricInput{
			GoalID:     g.ID,
			Name:       m.Name,
			Unit:       m.Unit,
			Direction:  m.Direction,
			Baseline:   m.Baseline,
			Target:     m.Target,
			TargetDate: m.TargetDate,
		}); err != nil {
			return Goal{}, err
		}
	}
	for _, id := range in.ValueIDs {
		if err := s.AssignGoalValue(ctx, in.OwnerID, g.ID, id); err != nil {
			return Goal{}, err
		}
	}
	// AddDimensionValue is Admin-only; any value setter may add to an
	// Extendable list this way (CONTEXT.md: Extendable).
	for _, dimID := range slices.Sorted(maps.Keys(in.NewValues)) {
		if _, err := s.assignGoalValueByName(ctx, in.OwnerID, g.ID, dimID, in.NewValues[dimID]); err != nil {
			return Goal{}, err
		}
	}
	for _, fieldID := range slices.Sorted(maps.Keys(in.FieldValues)) {
		if err := s.SetGoalField(ctx, in.OwnerID, g.ID, fieldID, in.FieldValues[fieldID]); err != nil {
			return Goal{}, err
		}
	}
	for _, parentID := range in.ParentIDs {
		if _, err := s.RequestLink(ctx, RequestLinkInput{ChildID: g.ID, ParentID: parentID, RequesterID: in.OwnerID}); err != nil {
			return Goal{}, err
		}
	}
	return s.loadGoal(ctx, g.ID)
}
