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

	m, err := h.Service.AddMetric(context.Background(), g.Owner.ID, validMetric(g.ID))
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
			if _, err := h.Service.AddMetric(context.Background(), owner.ID, in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}

	// Every problem comes back, each on its own line, not only the first.
	t.Run("no name and no unit", func(t *testing.T) {
		in := validMetric(g.ID)
		in.Name, in.Unit = "", " "
		_, err := h.Service.AddMetric(context.Background(), owner.ID, in)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if got, want := domain.Reason(err), "a Metric needs a name\na Metric needs a unit"; got != want {
			t.Errorf("Reason = %q, want %q", got, want)
		}
	})
}

func TestEditMetricChangesFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")
	m, err := h.Service.AddMetric(context.Background(), g.Owner.ID, validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	got, err := h.Service.EditMetric(context.Background(), owner.ID, domain.EditMetricInput{
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

// Metric reads a Metric by id, or is ErrNotFound for one there isn't.
func TestMetricReadsTheMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	h.CreateGoal(owner, "Earlier goal", "why")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")
	added, err := h.Service.AddMetric(context.Background(), owner.ID, validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	got, err := h.Service.Metric(context.Background(), added.ID)
	if err != nil {
		t.Fatalf("Metric: %v", err)
	}
	if got.ID != added.ID || got.GoalID != g.ID || got.Name != "p95 checkout latency" {
		t.Errorf("Metric = %+v, want p95 checkout latency on Goal %d", got, g.ID)
	}
	if _, err := h.Service.Metric(context.Background(), added.ID+1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Metric of an unknown id: err %v, want ErrNotFound", err)
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

// Only a Goal's Owner may add or edit its Metrics: anyone else, a Delegate and
// an Admin included, is refused and the Metrics stay as they were.
func TestMetricChangesAdmitOnlyTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")
	ctx := context.Background()
	m, err := h.Service.AddMetric(ctx, owner.ID, validMetric(g.ID))
	if err != nil {
		t.Fatalf("Owner adding a Metric: %v", err)
	}
	edit := domain.EditMetricInput{
		MetricID: m.ID, Name: "p99 checkout latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1500, Target: 500, TargetDate: m.TargetDate,
	}

	for name, actor := range notTheOwner(h, owner, g.ID) {
		if _, err := h.Service.AddMetric(ctx, actor.ID, validMetric(g.ID)); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s adding: err = %v, want ErrNotAuthorized", name, err)
		}
		if _, err := h.Service.EditMetric(ctx, actor.ID, edit); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s editing: err = %v, want ErrNotAuthorized", name, err)
		}
	}
	if ms, _ := h.Service.ListMetrics(ctx, g.ID); len(ms) != 1 || ms[0].Name != "p95 checkout latency" {
		t.Errorf("Metrics after refusals = %+v, want the Owner's one, unedited", ms)
	}
	if _, err := h.Service.EditMetric(ctx, owner.ID, edit); err != nil {
		t.Errorf("Owner editing: %v", err)
	}
}

// Adding or editing a Metric authorizes before it validates: anyone but the
// Goal's Owner is refused as not authorized whatever they send, and only the
// Owner hears what's wrong with it (#577).
func TestMetricChangesAuthorizeBeforeTheyValidate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	other := h.SignIn("pat@example.com")
	g := h.CreateGoal(owner, "Cut checkout latency", "why")
	ctx := context.Background()
	m, err := h.Service.AddMetric(ctx, owner.ID, validMetric(g.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	nameless := domain.AddMetricInput{GoalID: g.ID, Unit: "ms", Direction: domain.MetricDown, TargetDate: m.TargetDate}
	unparsed := domain.EditMetricInput{
		MetricID: m.ID, Name: "p99", Unit: "ms", Direction: domain.MetricDown, TargetDate: m.TargetDate,
		Unparsed: []*domain.InputError{{Input: "baseline", Message: `the baseline must be a number, not "lots"`}},
	}

	if _, err := h.Service.AddMetric(ctx, other.ID, nameless); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a stranger adding a nameless Metric: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.EditMetric(ctx, other.ID, unparsed); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("a stranger editing with a baseline that didn't parse: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.AddMetric(ctx, owner.ID, nameless); !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != "a Metric needs a name" {
		t.Errorf("the Owner adding a nameless Metric: err = %v, want it refused for needing a name", err)
	}
	if _, err := h.Service.EditMetric(ctx, owner.ID, unparsed); !errors.Is(err, domain.ErrValidation) || domain.Reason(err) != `the baseline must be a number, not "lots"` {
		t.Errorf("the Owner editing with a baseline that didn't parse: err = %v, want it refused under the baseline", err)
	}
	missing := unparsed
	missing.MetricID, missing.Unparsed = m.ID+100, nil
	if _, err := h.Service.EditMetric(ctx, owner.ID, missing); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editing a missing Metric: err = %v, want ErrNotFound", err)
	}
	if ms, _ := h.Service.ListMetrics(ctx, g.ID); len(ms) != 1 || ms[0].Name != "p95 checkout latency" {
		t.Errorf("Metrics after refusals = %+v, want the Owner's one, unedited", ms)
	}
}
