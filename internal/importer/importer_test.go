package importer_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

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

// A dry run reports each bad row's errors and saves nothing — not even the rows
// that are valid (ticket #22: a dry run shows errors row by row and saves
// nothing).
func TestDryRunReportsRowErrorsAndSavesNothing(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Delivery Date
Good goal,owner@example.com,It matters.,Ongoing,
Bad goal,owner@example.com,,Sideways,
,owner@example.com,No title.,Ongoing,
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if rep.Committed {
		t.Fatalf("dry run must not commit")
	}
	if len(rep.Rows) != 3 {
		t.Fatalf("want 3 row results, got %d (%+v)", len(rep.Rows), rep.Rows)
	}

	// Row 1 is clean; rows 2 and 3 each carry errors.
	if len(rep.Rows[0].Errors) != 0 {
		t.Errorf("row 1 should be clean, got errors: %v", rep.Rows[0].Errors)
	}
	if joined := strings.Join(rep.Rows[1].Errors, "; "); !strings.Contains(joined, "So What") || !strings.Contains(joined, "Kind") {
		t.Errorf("row 2 errors = %v, want mention of So What and Kind", rep.Rows[1].Errors)
	}
	if joined := strings.Join(rep.Rows[2].Errors, "; "); !strings.Contains(joined, "Title") {
		t.Errorf("row 3 errors = %v, want mention of Title", rep.Rows[2].Errors)
	}
	if rep.ErrorCount() != 2 {
		t.Errorf("ErrorCount = %d, want 2", rep.ErrorCount())
	}

	// Nothing was saved, including the valid row.
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("dry run saved %d Goals, want 0", len(goals))
	}
}

// A commit is all-or-nothing: one bad row rolls the whole import back, so even
// the valid rows are not saved (ticket #22: a commit is all-or-nothing in one
// transaction).
func TestCommitRollsBackOnAnyRowError(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Delivery Date
Valid one,owner@example.com,It matters.,Ongoing,
Valid two,owner@example.com,Also matters.,Dated,2026-06-30
Broken,owner@example.com,Missing kind.,Nonsense,
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if rep.Committed {
		t.Errorf("import with a bad row must not commit")
	}
	if !rep.HasErrors() {
		t.Errorf("expected the broken row to be reported")
	}

	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("rollback left %d Goals, want 0", len(goals))
	}
	// The Owner account is created inside the same transaction, so it rolls back too.
	if _, err := h.Service.SignIn(context.Background(), "check@example.com"); err != nil {
		t.Fatalf("sanity SignIn: %v", err)
	}
}

// Links between imported Goals are accepted automatically, but a cycle is
// rejected and rolls the import back (ticket #22: cycles are rejected).
func TestCommitRejectsCycleBetweenImportedGoals(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Parents
Chicken,owner@example.com,Needs the egg.,Ongoing,Egg
Egg,owner@example.com,Needs the chicken.,Ongoing,Chicken
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if rep.Committed {
		t.Errorf("import that closes a cycle must not commit")
	}
	var sawCycle bool
	for _, row := range rep.Rows {
		for _, e := range row.Errors {
			if strings.Contains(strings.ToLower(e), "cycle") {
				sawCycle = true
			}
		}
	}
	if !sawCycle {
		t.Errorf("expected a cycle error, got rows: %+v", rep.Rows)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 0 {
		t.Errorf("rejected cycle left %d Goals, want 0", len(goals))
	}
}

// Only an Admin may import Goals (CONTEXT.md: Admin can override links).
func TestImportRequiresAdmin(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	nonAdmin := h.SignIn("someone@example.com")

	_, err := importer.New(h.Service).DryRun(context.Background(), nonAdmin.ID, "goals.csv", []byte(cleanCSV))
	if !errorsIsNotAuthorized(err) {
		t.Fatalf("DryRun by non-Admin: err = %v, want ErrNotAuthorized", err)
	}
	_, err = importer.New(h.Service).Commit(context.Background(), nonAdmin.ID, "goals.csv", []byte(cleanCSV))
	if !errorsIsNotAuthorized(err) {
		t.Fatalf("Commit by non-Admin: err = %v, want ErrNotAuthorized", err)
	}
}

func errorsIsNotAuthorized(err error) bool {
	return err != nil && errors.Is(err, domain.ErrNotAuthorized)
}

// The same import works from an XLSX workbook, not just CSV (ticket #22: import
// CSV and XLSX).
func TestCommitCleanImportFromXLSX(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	rows := [][]string{
		{"Title", "Owner", "So What", "Kind", "Delivery Date", "Parents"},
		{"Parent goal", "boss@example.com", "It matters.", "Ongoing", "", ""},
		{"Child goal", "ic@example.com", "Supports the parent.", "Dated", "2026-06-30", "Parent goal"},
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	for r, row := range rows {
		for c, val := range row {
			cellRef, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := f.SetCellStr(sheet, cellRef, val); err != nil {
				t.Fatalf("set cell: %v", err)
			}
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !rep.Committed || rep.HasErrors() {
		t.Fatalf("xlsx import not clean: committed=%v rows=%+v", rep.Committed, rep.Rows)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("want 2 Goals from xlsx, got %d", len(goals))
	}
	byTitle := map[string]domain.Goal{}
	for _, g := range goals {
		byTitle[g.Title] = g
	}
	child, ok := byTitle["Child goal"]
	if !ok {
		t.Fatalf("child Goal not imported from xlsx")
	}
	parents, err := h.Service.ParentsOf(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("ParentsOf: %v", err)
	}
	if len(parents) != 1 || parents[0].Title != "Parent goal" {
		t.Errorf("child parents from xlsx = %+v, want [Parent goal]", parents)
	}
}
