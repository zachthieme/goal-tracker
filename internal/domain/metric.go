package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Metric is a measured quantity on a Goal with a unit, direction, baseline,
// target, and target date (CONTEXT.md: Metric). It is never aggregated across
// Goals (ADR-0003).
type Metric struct {
	ID         int64
	GoalID     int64
	Name       string
	Unit       string
	Direction  string
	Baseline   float64
	Target     float64
	TargetDate time.Time
	CreatedAt  time.Time
}

// Metric directions: whether higher (MetricUp) or lower (MetricDown) values are
// better. This is the direction of desired movement from baseline to target.
const (
	MetricUp   = "up"
	MetricDown = "down"
)

func validDirection(d string) bool {
	return d == MetricUp || d == MetricDown
}

// AddMetricInput is the add-Metric command's input.
type AddMetricInput struct {
	GoalID     int64
	Name       string
	Unit       string
	Direction  string
	Baseline   float64
	Target     float64
	TargetDate time.Time
}

// AddMetric adds a Metric to a Goal. Name, unit, a valid direction, and a
// target date are required.
func (s *Service) AddMetric(ctx context.Context, in AddMetricInput) (Metric, error) {
	name := strings.TrimSpace(in.Name)
	unit := strings.TrimSpace(in.Unit)
	if err := validateMetric(name, unit, in.Direction, in.TargetDate); err != nil {
		return Metric{}, err
	}
	if _, err := s.queries.GetGoal(ctx, in.GoalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Metric{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Metric{}, fmt.Errorf("look up goal: %w", err)
	}

	row, err := s.queries.CreateMetric(ctx, db.CreateMetricParams{
		GoalID:     in.GoalID,
		Name:       name,
		Unit:       unit,
		Direction:  in.Direction,
		Baseline:   in.Baseline,
		Target:     in.Target,
		TargetDate: in.TargetDate.Format(dateFormat),
		CreatedAt:  s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return Metric{}, fmt.Errorf("create metric: %w", err)
	}
	return metricFromRow(row), nil
}

// EditMetricInput is the edit-Metric command's input.
type EditMetricInput struct {
	MetricID   int64
	Name       string
	Unit       string
	Direction  string
	Baseline   float64
	Target     float64
	TargetDate time.Time
}

// EditMetric changes a Metric's fields. The same rules as AddMetric apply.
func (s *Service) EditMetric(ctx context.Context, in EditMetricInput) (Metric, error) {
	name := strings.TrimSpace(in.Name)
	unit := strings.TrimSpace(in.Unit)
	if err := validateMetric(name, unit, in.Direction, in.TargetDate); err != nil {
		return Metric{}, err
	}
	if _, err := s.queries.GetMetric(ctx, in.MetricID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Metric{}, fmt.Errorf("%w: metric %d", ErrNotFound, in.MetricID)
		}
		return Metric{}, fmt.Errorf("look up metric: %w", err)
	}

	row, err := s.queries.UpdateMetric(ctx, db.UpdateMetricParams{
		Name:       name,
		Unit:       unit,
		Direction:  in.Direction,
		Baseline:   in.Baseline,
		Target:     in.Target,
		TargetDate: in.TargetDate.Format(dateFormat),
		ID:         in.MetricID,
	})
	if err != nil {
		return Metric{}, fmt.Errorf("update metric: %w", err)
	}
	return metricFromRow(row), nil
}

// RequireMetricOwner is RequireGoalOwner for the Metric's Goal. It returns
// ErrNotFound if no such Metric exists.
func (s *Service) RequireMetricOwner(ctx context.Context, actorID, metricID int64) error {
	m, err := s.queries.GetMetric(ctx, metricID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: metric %d", ErrNotFound, metricID)
		}
		return fmt.Errorf("look up metric: %w", err)
	}
	return s.RequireGoalOwner(ctx, actorID, m.GoalID)
}

// ListMetrics returns a Goal's Metrics, earliest target date first.
func (s *Service) ListMetrics(ctx context.Context, goalID int64) ([]Metric, error) {
	rows, err := s.queries.ListMetrics(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	out := make([]Metric, 0, len(rows))
	for _, r := range rows {
		out = append(out, metricFromRow(r))
	}
	return out, nil
}

func validateMetric(name, unit, direction string, targetDate time.Time) error {
	if name == "" {
		return fmt.Errorf("%w: a Metric needs a name", ErrValidation)
	}
	if unit == "" {
		return fmt.Errorf("%w: a Metric needs a unit", ErrValidation)
	}
	if !validDirection(direction) {
		return fmt.Errorf("%w: a Metric needs a direction of %q or %q", ErrValidation, MetricUp, MetricDown)
	}
	if targetDate.IsZero() {
		return fmt.Errorf("%w: a Metric needs a target date", ErrValidation)
	}
	return nil
}

// MetricReading is a Metric's value recorded at a Check-in (CONTEXT.md: Metric -
// its current value is recorded at each Check-in). A Metric's readings ordered
// over time give its trend against target.
type MetricReading struct {
	ID        int64
	CheckinID int64
	MetricID  int64
	Value     float64
	CreatedAt time.Time
}

// MetricReadingInput records one Metric's current value in a Check-in. The
// Metric must be on the Goal being checked in on.
type MetricReadingInput struct {
	MetricID int64
	Value    float64
}

// ListMetricReadings returns a Metric's readings over time, earliest first, for
// its trend against target (CONTEXT.md: the Goal page shows each Metric's trend).
func (s *Service) ListMetricReadings(ctx context.Context, metricID int64) ([]MetricReading, error) {
	rows, err := s.queries.ListMetricReadings(ctx, metricID)
	if err != nil {
		return nil, fmt.Errorf("list metric readings: %w", err)
	}
	out := make([]MetricReading, 0, len(rows))
	for _, r := range rows {
		out = append(out, metricReadingFromRow(r))
	}
	return out, nil
}

func metricReadingFromRow(r db.MetricReading) MetricReading {
	createdAt, _ := time.Parse(timeFormat, r.CreatedAt)
	return MetricReading{
		ID:        r.ID,
		CheckinID: r.CheckinID,
		MetricID:  r.MetricID,
		Value:     r.Value,
		CreatedAt: createdAt,
	}
}

func metricFromRow(m db.Metric) Metric {
	targetDate, _ := time.Parse(dateFormat, m.TargetDate)
	createdAt, _ := time.Parse(timeFormat, m.CreatedAt)
	return Metric{
		ID:         m.ID,
		GoalID:     m.GoalID,
		Name:       m.Name,
		Unit:       m.Unit,
		Direction:  m.Direction,
		Baseline:   m.Baseline,
		Target:     m.Target,
		TargetDate: targetDate,
		CreatedAt:  createdAt,
	}
}
