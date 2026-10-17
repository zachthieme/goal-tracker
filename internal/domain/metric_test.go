package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func validMetric(goalID int64) domain.AddMetricInput {
	return domain.AddMetricInput{
		GoalID:     goalID,
		Name:       "p95 checkout latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	}
}

func TestAddMetricListsUnderTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "Faster checkout lifts conversion.")

	m, err := h.Service.AddMetric(context.Background(), validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if m.ID == 0 {
		t.Error("want a persisted Metric with a non-zero ID")
	}
	if m.Name != "p95 checkout latency" || m.Unit != "ms" || m.Direction != domain.MetricDown {
		t.Errorf("Metric = %+v", m)
	}
	if m.Baseline != 1200 || m.Target != 400 {
		t.Errorf("baseline/target = %v/%v", m.Baseline, m.Target)
	}

	list, err := h.Service.ListMetrics(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(list) != 1 || list[0].Name != "p95 checkout latency" {
		t.Errorf("ListMetrics = %+v", list)
	}
}

func TestAddMetricValidatesInput(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")

	noName := validMetric(g.ID)
	noName.Name = "  "
	noUnit := validMetric(g.ID)
	noUnit.Unit = ""
	badDir := validMetric(g.ID)
	badDir.Direction = "sideways"
	noDate := validMetric(g.ID)
	noDate.TargetDate = time.Time{}

	cases := map[string]domain.AddMetricInput{
		"no name":        noName,
		"no unit":        noUnit,
		"bad direction":  badDir,
		"no target date": noDate,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Service.AddMetric(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestEditMetricChangesFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")
	m, err := h.Service.AddMetric(context.Background(), validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	got, err := h.Service.EditMetric(context.Background(), domain.EditMetricInput{
		MetricID:   m.ID,
		Name:       "p99 checkout latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1500,
		Target:     500,
		TargetDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("EditMetric: %v", err)
	}
	if got.Name != "p99 checkout latency" || got.Baseline != 1500 || got.Target != 500 {
		t.Errorf("EditMetric = %+v", got)
	}
}

// RequireMetricOwner admits only the Owner of the Metric's Goal: another
// Account and a Delegate are refused, and a missing Metric is not found.
func TestRequireMetricOwnerAdmitsOnlyTheGoalsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	other := h.SignIn("pat@example.com")
	delegate := h.SignIn("tpm@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "Faster checkout lifts conversion.")
	h.AddDelegate(owner, delegate, g.ID)
	ctx := context.Background()
	m, err := h.Service.AddMetric(ctx, validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	if err := h.Service.RequireMetricOwner(ctx, owner.ID, m.ID); err != nil {
		t.Errorf("Owner: err = %v, want nil", err)
	}
	if err := h.Service.RequireMetricOwner(ctx, other.ID, m.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("another Account: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RequireMetricOwner(ctx, delegate.ID, m.ID); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Delegate: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.RequireMetricOwner(ctx, owner.ID, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing Metric: err = %v, want ErrNotFound", err)
	}
}

// A Metric is off target once its target date has passed and its reading
// hasn't reached the target in its direction; a reading equal to the target is
// on target, and before or on the target date nothing is off target.
func TestMetricOffTarget(t *testing.T) {
	t.Parallel()

	targetDate := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	dayAfter := targetDate.AddDate(0, 0, 1)
	up := domain.Metric{Name: "Activation", Unit: "percent", Direction: domain.MetricUp, Target: 60, TargetDate: targetDate}
	down := domain.Metric{Name: "Idle capacity", Unit: "percent", Direction: domain.MetricDown, Target: 20, TargetDate: targetDate}

	for _, tc := range []struct {
		name    string
		metric  domain.Metric
		reading float64
		today   time.Time
		want    bool
	}{
		{"up, below target, past its date", up, 59.5, dayAfter, true},
		{"up, at target, past its date", up, 60, dayAfter, false},
		{"up, above target, past its date", up, 72, dayAfter, false},
		{"down, above target, past its date", down, 40.88, dayAfter, true},
		{"down, at target, past its date", down, 20, dayAfter, false},
		{"down, below target, past its date", down, 12, dayAfter, false},
		{"up, below target, on its date", up, 10, targetDate, false},
		{"down, above target, before its date", down, 90, targetDate.AddDate(0, 0, -5), false},
	} {
		if got := tc.metric.OffTarget(tc.reading, tc.today); got != tc.want {
			t.Errorf("%s: OffTarget(%v, %s) = %v, want %v", tc.name, tc.reading, tc.today.Format("2006-01-02"), got, tc.want)
		}
	}
}
