package importer_test

import (
	"context"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// cleanCSV is a spreadsheet with two Goals: an Ongoing parent carrying a Metric
// and a Dimension value, and a Dated child carrying two Milestones that
// contributes to the parent by title.
const cleanCSV = `Title,Owner,So What,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,,,ARR | USD | up | 1000000 | 2000000 | 2026-12-31,,Growth
Ship checkout v2,eng@example.com,Checkout is slow.,Dated,2026-06-30,Beta @ 2026-05-01; GA @ 2026-06-15,,Grow revenue,Reliability
`

// An Admin commits a clean spreadsheet: both Goals are created with their Owners
// (new accounts), the Dated child's Milestones and the Ongoing parent's Metric
// and Dimension value land, and the child's contributes-to link to the parent is
// accepted automatically (ticket #22).
func TestCommitCleanImport(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(cleanCSV))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !rep.Committed {
		t.Fatalf("import not committed; report: %+v", rep)
	}
	if rep.HasErrors() {
		t.Fatalf("clean import reported errors: %+v", rep.Rows)
	}

	ctx := context.Background()
	goals, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("want 2 imported Goals, got %d", len(goals))
	}
	byTitle := map[string]domain.Goal{}
	for _, g := range goals {
		byTitle[g.Title] = g
	}

	parent, ok := byTitle["Grow revenue"]
	if !ok {
		t.Fatalf("parent Goal not imported; got %v", byTitle)
	}
	if parent.Kind != domain.GoalOngoing {
		t.Errorf("parent Kind = %q, want Ongoing", parent.Kind)
	}
	if parent.Owner.Email != "ceo@example.com" {
		t.Errorf("parent Owner = %q, want ceo@example.com", parent.Owner.Email)
	}
	metrics, err := h.Service.ListMetrics(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "ARR" || metrics[0].Unit != "USD" || metrics[0].Direction != domain.MetricUp {
		t.Errorf("parent Metric = %+v, want one ARR/USD/up metric", metrics)
	}
	if len(metrics) == 1 && (metrics[0].Baseline != 1000000 || metrics[0].Target != 2000000) {
		t.Errorf("parent Metric baseline/target = %v/%v, want 1000000/2000000", metrics[0].Baseline, metrics[0].Target)
	}
	values, err := h.Service.GoalValues(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Growth" {
		t.Errorf("parent Dimension values = %+v, want one Growth", values)
	}

	child, ok := byTitle["Ship checkout v2"]
	if !ok {
		t.Fatalf("child Goal not imported; got %v", byTitle)
	}
	if child.Kind != domain.GoalDated {
		t.Errorf("child Kind = %q, want Dated", child.Kind)
	}
	if got := child.DeliveryDate.Format("2006-01-02"); got != "2026-06-30" {
		t.Errorf("child DeliveryDate = %q, want 2026-06-30", got)
	}
	milestones, err := h.Service.ListMilestones(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(milestones) != 2 {
		t.Errorf("child Milestones = %d, want 2 (%+v)", len(milestones), milestones)
	}

	parents, err := h.Service.ParentsOf(ctx, child.ID)
	if err != nil {
		t.Fatalf("ParentsOf: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Errorf("child accepted parents = %+v, want [Grow revenue]", parents)
	}
}
