package importer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

	// Row 2 (the first under the header) is clean; rows 3 and 4 each carry errors.
	if len(rep.Rows[0].Errors) != 0 {
		t.Errorf("row 2 should be clean, got errors: %v", rep.Rows[0].Errors)
	}
	if joined := strings.Join(rep.Rows[1].Errors, "; "); !strings.Contains(joined, "So What") || !strings.Contains(joined, "Kind") {
		t.Errorf("row 3 errors = %v, want mention of So What and Kind", rep.Rows[1].Errors)
	}
	if joined := strings.Join(rep.Rows[2].Errors, "; "); !strings.Contains(joined, "Title") {
		t.Errorf("row 4 errors = %v, want mention of Title", rep.Rows[2].Errors)
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

// The example file shipped with the format docs is valid: with the Dimension it
// uses defined, a dry run of it reports no errors (ticket #22: the column format
// is documented, with an example file in the repo).
func TestExampleFileDryRunsClean(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "import-example.csv"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "import-example.csv", data)
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if rep.HasErrors() {
		t.Fatalf("example file reported errors: %+v", rep.Rows)
	}
	if len(rep.Rows) != 3 {
		t.Errorf("example has %d rows, want 3", len(rep.Rows))
	}
}

// xlsxOf builds a one-sheet XLSX workbook whose rows are written with excelize's
// SetSheetRow, so a time.Time cell becomes a real date cell (a number with a date
// format) the way Excel and Sheets store a typed date.
func xlsxOf(t *testing.T, rows ...[]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	for r, row := range rows {
		cellRef, err := excelize.CoordinatesToCellName(1, r+1)
		if err != nil {
			t.Fatalf("cell name: %v", err)
		}
		if err := f.SetSheetRow(sheet, cellRef, &row); err != nil {
			t.Fatalf("set row: %v", err)
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	return buf.Bytes()
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// A date-typed Delivery Date cell — what Excel and Sheets make of a typed
// 2026-06-30 — imports as its date, not as the displayed text (#30). The first
// of a month is included because spreadsheets display it without the day.
func TestXLSXDateCellsImportAsTheirDate(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	data := xlsxOf(t,
		[]any{"Title", "Owner", "So What", "Kind", "Delivery Date"},
		[]any{"Mid-month goal", "owner@example.com", "It matters.", "Dated", date(2026, time.June, 30)},
		[]any{"First-of-month goal", "owner@example.com", "It matters.", "Dated", date(2026, time.July, 1)},
		[]any{"Text-date goal", "owner@example.com", "It matters.", "Dated", "2026-08-15"},
	)
	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.xlsx", data)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !rep.Committed || rep.HasErrors() {
		t.Fatalf("xlsx with date cells not clean: committed=%v rows=%+v", rep.Committed, rep.Rows)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	got := map[string]string{}
	for _, g := range goals {
		got[g.Title] = g.DeliveryDate.Format("2006-01-02")
	}
	want := map[string]string{
		"Mid-month goal":      "2026-06-30",
		"First-of-month goal": "2026-07-01",
		"Text-date goal":      "2026-08-15",
	}
	for title, w := range want {
		if got[title] != w {
			t.Errorf("%s DeliveryDate = %q, want %q", title, got[title], w)
		}
	}
}

// A date cell with a custom format — a UK sheet's d/m/yyyy — also imports as
// its date, and a date cell in any column is read as YYYY-MM-DD: one in the
// Milestones column shows up as such in the report (#30).
func TestXLSXCustomFormatDateCells(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	for r, row := range [][]any{
		{"Title", "Owner", "So What", "Kind", "Delivery Date", "Milestones"},
		{"UK goal", "owner@example.com", "It matters.", "Dated", date(2026, time.June, 30), ""},
		{"Nameless milestone", "owner@example.com", "It matters.", "Ongoing", "", date(2026, time.May, 1)},
	} {
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", r+1), &row); err != nil {
			t.Fatalf("set row: %v", err)
		}
	}
	ukDate := "d/m/yyyy"
	style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &ukDate})
	if err != nil {
		t.Fatalf("new style: %v", err)
	}
	for _, ref := range []string{"E2", "F3"} {
		if err := f.SetCellStyle(sheet, ref, ref, style); err != nil {
			t.Fatalf("set style: %v", err)
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 row results, got %+v", rep.Rows)
	}
	if len(rep.Rows[0].Errors) != 0 {
		t.Errorf("UK-format Delivery Date rejected: %v", rep.Rows[0].Errors)
	}
	if joined := strings.Join(rep.Rows[1].Errors, "; "); !strings.Contains(joined, `"2026-05-01"`) {
		t.Errorf("Milestones date cell errors = %v, want it read as \"2026-05-01\"", rep.Rows[1].Errors)
	}
}

// Rows are numbered as the spreadsheet numbers them: the header is row 1, so the
// first data row is row 2. A blank row still takes a number, and a cell spanning
// several lines is still one row (#30).
func TestRowsAreNumberedAsInTheSpreadsheet(t *testing.T) {
	const csv = "Title,Owner,So What,Kind\n" +
		"First,owner@example.com,,Ongoing\n" +
		"Second,owner@example.com,\"Spans\ntwo lines.\",Ongoing\n" +
		"\n" +
		"Fifth,owner@example.com,,Ongoing\n"
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	lines := map[string]int{}
	for _, row := range rep.Rows {
		lines[row.Title] = row.Line
	}
	for title, want := range map[string]int{"First": 2, "Second": 3, "Fifth": 5} {
		if lines[title] != want {
			t.Errorf("%s is row %d, want %d (rows: %+v)", title, lines[title], want, rep.Rows)
		}
	}

	xlsxRep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.xlsx", xlsxOf(t,
		[]any{"Title", "Owner", "So What", "Kind"},
		[]any{"First", "owner@example.com", "", "Ongoing"},
	))
	if err != nil {
		t.Fatalf("DryRun xlsx: %v", err)
	}
	if len(xlsxRep.Rows) != 1 || xlsxRep.Rows[0].Line != 2 {
		t.Errorf("xlsx rows = %+v, want First as row 2", xlsxRep.Rows)
	}
}

// A row with several problems reports all of them in one dry run, not just the
// first found (#30; docs/import-format.md: "each error found").
func TestDryRunReportsEveryErrorOnARow(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Milestones,Parents,Pillar
Otherwise fine,owner@example.com,It matters.,Ongoing,,No such parent,Sideways
Missing so what,owner@example.com,,Ongoing,Beta @ someday,Also missing,Upwards
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("want 2 row results, got %+v", rep.Rows)
	}
	for i, wants := range [][]string{
		{`"Sideways" is not a value of Dimension "Pillar"`, `parent Goal "No such parent"`},
		{"So What is required", `bad Milestone date in "Beta @ someday"`, `parent Goal "Also missing"`, `"Upwards" is not a value of Dimension "Pillar"`},
	} {
		row := rep.Rows[i]
		joined := strings.Join(row.Errors, "; ")
		for _, want := range wants {
			if !strings.Contains(joined, want) {
				t.Errorf("row %d (%s) errors = %v, want one mentioning %s", row.Line, row.Title, row.Errors, want)
			}
		}
		if len(row.Errors) != len(wants) {
			t.Errorf("row %d (%s) has %d errors, want %d: %v", row.Line, row.Title, len(row.Errors), len(wants), row.Errors)
		}
	}
}

// A column whose header names a Field sets that Field on the row's Goal, for
// each of the four types (#77; CONTEXT.md: Field).
func TestCommitSetsFieldColumns(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,budget,Sponsor note,Background,Review date
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,1250000.5,Board asked,"Long story,
over two lines",2026-11-30
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.CreateField(admin, "Sponsor note", domain.FieldShortText, "")
	h.CreateField(admin, "Background", domain.FieldLongText, "")
	h.CreateField(admin, "Review Date", domain.FieldDate, "")

	goal := commitOneGoal(t, h, admin, csv)
	got := map[string]string{}
	fields, err := h.Service.GoalFields(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	for _, f := range fields {
		got[f.Field.Name] = f.Value
	}
	want := map[string]string{
		"Budget":       "1250000.5",
		"Sponsor note": "Board asked",
		"Background":   "Long story,\nover two lines",
		"Review Date":  "2026-11-30",
	}
	for name, v := range want {
		if got[name] != v {
			t.Errorf("Field %s = %q, want %q", name, got[name], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("Goal Fields = %v, want %v", got, want)
	}
}

// commitOneGoal commits a one-row spreadsheet, failing the test unless it
// imports cleanly, and returns the Goal it created.
func commitOneGoal(t *testing.T, h *testsupport.Harness, admin domain.Account, csv string) domain.Goal {
	t.Helper()
	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !rep.Committed || len(rep.Rows) != 1 {
		t.Fatalf("import not committed; rows: %+v", rep.Rows)
	}
	goal, err := h.Service.ViewGoal(context.Background(), rep.Rows[0].GoalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	return goal
}

// A number or date Field cell that doesn't parse is reported against its row,
// alongside the row's other errors, and the import saves nothing (#77).
func TestDryRunReportsBadFieldValuesAgainstTheirRow(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Budget,Review date
Fine,owner@example.com,It matters.,Ongoing,12,2026-11-30
Bad budget,owner@example.com,It matters.,Ongoing,lots,
Bad date,owner@example.com,,Ongoing,,next week
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.CreateField(admin, "Review date", domain.FieldDate, "")

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if rep.Committed {
		t.Fatalf("import with bad Field values must not commit")
	}
	for i, wants := range [][]string{
		nil,
		{`Budget takes a number, and "lots" isn't one`},
		{"So What is required", `Review date takes a date as YYYY-MM-DD, and "next week" isn't one`},
	} {
		row := rep.Rows[i]
		joined := strings.Join(row.Errors, "; ")
		for _, want := range wants {
			if !strings.Contains(joined, want) {
				t.Errorf("row %d (%s) errors = %v, want one mentioning %s", row.Line, row.Title, row.Errors, want)
			}
		}
		if len(row.Errors) != len(wants) {
			t.Errorf("row %d (%s) has %d errors, want %d: %v", row.Line, row.Title, len(row.Errors), len(wants), row.Errors)
		}
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("rolled-back import left %d Goals, want 0", len(goals))
	}
}

// In a several-values Dimension's column a cell lists values separated by
// semicolons, spaces around each ignored, and the Goal takes them all (#77).
func TestCommitSetsSeveralValuesFromOneCell(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Themes
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,  Growth ;Trust ;
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateSeveralValuesDimension(admin, "Themes", "Growth", "Reliability", "Trust")

	goal := commitOneGoal(t, h, admin, csv)
	if got := goalValueNames(t, h, goal); strings.Join(got, ", ") != "Growth, Trust" {
		t.Errorf("Goal values = %v, want [Growth Trust]", got)
	}
}

// More than one value in a one-value Dimension's column is a row error (#77).
func TestDryRunRejectsSeveralValuesInAOneValueColumn(t *testing.T) {
	const csv = `Title,Owner,So What,Kind,Pillar
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,Growth; Reliability
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")

	rep, err := importer.New(h.Service).DryRun(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if want := "Pillar takes one value per Goal"; len(rep.Rows) != 1 || strings.Join(rep.Rows[0].Errors, "; ") != want {
		t.Errorf("rows = %+v, want one row with the error %q", rep.Rows, want)
	}
}

// goalValueNames lists the Dimension values a Goal carries, sorted.
func goalValueNames(t *testing.T, h *testsupport.Harness, goal domain.Goal) []string {
	t.Helper()
	values, err := h.Service.GoalValues(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	names := make([]string, 0, len(values))
	for _, v := range values {
		names = append(names, v.Value)
	}
	slices.Sort(names)
	return names
}
