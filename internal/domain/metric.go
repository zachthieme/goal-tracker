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
//
// Each name starts Metric: the CHECK test on metrics.direction finds its values
// by that prefix (enumchecks_test.go), so a value named otherwise goes
// unchecked.
const (
	MetricUp   = "up"
	MetricDown = "down"
)

func validDirection(d string) bool {
	return d == MetricUp || d == MetricDown
}

// OffTarget reports whether reading has missed the Metric's target as of
// today, a calendar date in the Goal's timezone (see Service.GoalTimezone and
// orgDate): the target date has passed and the reading falls short of the
// target in the Metric's direction. A reading equal to the target is on target,
// and up to and including its target date a Metric is never off target.
func (m Metric) OffTarget(reading float64, today time.Time) bool {
	if !m.TargetDate.Before(today) {
		return false
	}
	switch m.Direction {
	case MetricUp:
		return reading < m.Target
	case MetricDown:
		return reading > m.Target
	}
	return false
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

// AddMetric adds a Metric to a Goal. Only the Goal's Owner may add one
// (ErrNotAuthorized), and that is checked first, so no one else learns what's
// wrong with what they sent. Name, unit, a valid direction, and a target date
// are required (metricProblems), and every one missing is refused together.
func (s *Service) AddMetric(ctx context.Context, actorID int64, in AddMetricInput) (Metric, error) {
	var row db.Metric
	err := s.WithinTx(ctx, func(tx *Service) error {
		if _, err := tx.openGoalCommand(ctx, actorID, in.GoalID, metricEditRefusal, nil); err != nil {
			return err
		}
		if err := joinInputErrors(metricProblems(in.Name, in.Unit, in.Direction, in.TargetDate, metricFormInput)); err != nil {
			return err
		}
		name, unit := strings.TrimSpace(in.Name), strings.TrimSpace(in.Unit)

		var err error
		row, err = tx.queries.CreateMetric(ctx, db.CreateMetricParams{
			GoalID:     in.GoalID,
			Name:       name,
			Unit:       unit,
			Direction:  in.Direction,
			Baseline:   in.Baseline,
			Target:     in.Target,
			TargetDate: in.TargetDate.Format(dateFormat),
			CreatedAt:  tx.clock.Now().Format(timeFormat),
		})
		if err != nil {
			return fmt.Errorf("create metric: %w", err)
		}
		return nil
	})
	if err != nil {
		return Metric{}, err
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
	// Unparsed are the values the caller couldn't parse, each refused under
	// its input's name, as DefinedGoalInput.Unparsed are.
	Unparsed []*InputError
}

// Metric returns the Metric id, or ErrNotFound when there is none. It is
// read-only and checks no one: a Goal's Metrics show on its page, which
// everyone signed in may see.
func (s *Service) Metric(ctx context.Context, id int64) (Metric, error) {
	row, err := s.queries.GetMetric(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Metric{}, fmt.Errorf("%w: metric %d", ErrNotFound, id)
		}
		return Metric{}, fmt.Errorf("look up metric: %w", err)
	}
	return metricFromRow(row), nil
}

// EditMetric changes a Metric's fields. The same rules as AddMetric apply,
// its Goal's Owner alone, checked first, included. A value the caller couldn't
// parse (see in.Unparsed) is refused before the rest is checked.
func (s *Service) EditMetric(ctx context.Context, actorID int64, in EditMetricInput) (Metric, error) {
	var row db.Metric
	err := s.WithinTx(ctx, func(tx *Service) error {
		current, err := tx.Metric(ctx, in.MetricID)
		if err != nil {
			return err
		}
		if _, err := tx.openGoalCommand(ctx, actorID, current.GoalID, metricEditRefusal, in.Unparsed); err != nil {
			return err
		}
		if err := joinInputErrors(metricProblems(in.Name, in.Unit, in.Direction, in.TargetDate, metricFormInput)); err != nil {
			return err
		}
		name, unit := strings.TrimSpace(in.Name), strings.TrimSpace(in.Unit)

		row, err = tx.queries.UpdateMetric(ctx, db.UpdateMetricParams{
			Name:       name,
			Unit:       unit,
			Direction:  in.Direction,
			Baseline:   in.Baseline,
			Target:     in.Target,
			TargetDate: in.TargetDate.Format(dateFormat),
			ID:         in.MetricID,
		})
		if err != nil {
			return fmt.Errorf("update metric: %w", err)
		}
		return nil
	})
	if err != nil {
		return Metric{}, err
	}
	return metricFromRow(row), nil
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

// MetricsByGoal returns each of goalIDs' Metrics, keyed by Goal ID, as
// ListMetrics returns one Goal's, in one query, so a page or file listing many
// Goals reads them all at once. A Goal with no Metrics has no entry.
func (s *Service) MetricsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Metric, error) {
	return s.metricsByGoal(ctx, goalIDs)
}

// metricsByGoal returns each of goalIDs' Metrics, as ListMetrics reads one
// Goal's, keyed by Goal ID, in one query. No Goals reads nothing.
func (s *Service) metricsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]Metric, error) {
	out := map[int64][]Metric{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListMetricsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list metrics: %w", err)
	}
	for _, r := range rows {
		out[r.GoalID] = append(out[r.GoalID], metricFromRow(r))
	}
	return out, nil
}

// metricProblems is the one check of a Metric's own input, shared by the
// commands that add or edit one and by Goal definition: it needs a name, a
// unit, a direction of MetricUp or MetricDown and a target date. Every
// problem is refused, under input(part) for its part ("name", "unit",
// "direction", "target_date").
func metricProblems(name, unit, direction string, targetDate time.Time, input func(part string) string) []*InputError {
	var problems []*InputError
	if strings.TrimSpace(name) == "" {
		problems = append(problems, inputError(input("name"), "a Metric needs a name"))
	}
	if strings.TrimSpace(unit) == "" {
		problems = append(problems, inputError(input("unit"), "a Metric needs a unit"))
	}
	if !validDirection(direction) {
		problems = append(problems, inputError(input("direction"), "a Metric needs a direction of %q or %q", MetricUp, MetricDown))
	}
	if targetDate.IsZero() {
		problems = append(problems, inputError(input("target_date"), "a Metric needs a target date"))
	}
	return problems
}

// metricFormInput names a part of a Metric as the Goal page's Metric forms
// do: by the part alone.
func metricFormInput(part string) string { return part }

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

// metricReadingsByMetric returns the readings of every Metric on goalIDs, as
// ListMetricReadings reads one Metric's, keyed by Metric ID, in one query. No
// Goals reads nothing.
func (s *Service) metricReadingsByMetric(ctx context.Context, goalIDs []int64) (map[int64][]MetricReading, error) {
	out := map[int64][]MetricReading{}
	if len(goalIDs) == 0 {
		return out, nil
	}
	rows, err := s.queries.ListMetricReadingsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list metric readings: %w", err)
	}
	for _, r := range rows {
		out[r.MetricReading.MetricID] = append(out[r.MetricReading.MetricID], metricReadingFromRow(r.MetricReading))
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
