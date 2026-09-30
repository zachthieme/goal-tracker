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
