// Package importer loads a pilot's real Goals from a spreadsheet instead of
// having an Admin enter them by hand (ticket #22). It reads CSV and XLSX, one
// Goal per row, and turns each row into the same domain commands a person would
// run: it gives every named Owner an account, creates the Goal with its So What,
// marks it Dated or Ongoing, adds its Milestones and Metrics, assigns its
// Dimension values, and links it to its parent Goals — accepting those links
// automatically, since only an Admin runs an import (CONTEXT.md: Admin can
// override links).
//
// An import is validated row by row. A DryRun reports the errors and saves
// nothing; a Commit is all-or-nothing: if any row has an error the whole import
// rolls back and nothing is saved. The column format is documented in
// docs/import-format.md, with an example in testdata/import-example.csv.
package importer

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// dateFormat is how calendar dates (delivery dates, target dates) are written in
// the spreadsheet, matching the domain's storage format.
const dateFormat = "2006-01-02"

// Importer runs spreadsheet imports through the domain service.
type Importer struct {
	svc *domain.Service
}

// New builds an Importer over the domain service.
func New(svc *domain.Service) *Importer {
	return &Importer{svc: svc}
}

// Report is the outcome of an import: one RowResult per data row, and whether
// the import was committed (a dry run, or a run that found errors, is never
// committed).
type Report struct {
	Rows      []RowResult
	Committed bool
}

// RowResult is what happened to one data row: its 1-based line number (the
// header is line 0), the Goal title it named, the id of the Goal created from it
// (0 when nothing was saved), and any errors found on it.
type RowResult struct {
	Line   int
	Title  string
	GoalID int64
	Errors []string
}

// HasErrors reports whether any row failed.
func (r Report) HasErrors() bool {
	return r.ErrorCount() > 0
}

// ErrorCount returns how many rows have at least one error.
func (r Report) ErrorCount() int {
	n := 0
	for _, row := range r.Rows {
		if len(row.Errors) > 0 {
			n++
		}
	}
	return n
}

// DryRun validates the spreadsheet and reports the errors row by row without
// saving anything (ticket #22: a dry run saves nothing).
func (im *Importer) DryRun(ctx context.Context, adminID int64, filename string, data []byte) (Report, error) {
	return im.run(ctx, adminID, filename, data, false)
}

// Commit imports every row in one transaction. It is all-or-nothing: if any row
// has an error the transaction rolls back and nothing is saved (ticket #22).
func (im *Importer) Commit(ctx context.Context, adminID int64, filename string, data []byte) (Report, error) {
	return im.run(ctx, adminID, filename, data, true)
}

func (im *Importer) run(ctx context.Context, adminID int64, filename string, data []byte, commit bool) (Report, error) {
	admin, err := im.svc.Account(ctx, adminID)
	if err != nil {
		return Report{}, fmt.Errorf("resolve importing Admin: %w", err)
	}
	if !admin.IsAdmin {
		return Report{}, fmt.Errorf("%w: only an Admin may import Goals", domain.ErrNotAuthorized)
	}

	grid, err := parseGrid(filename, data)
	if err != nil {
		return Report{}, err
	}

	dims, err := im.svc.ListDimensions(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list dimensions: %w", err)
	}
	dimByName := make(map[string]domain.Dimension, len(dims))
	for _, d := range dims {
		dimByName[normalize(d.Name)] = d
	}

	lay, err := parseHeader(grid[0], dimByName)
	if err != nil {
		return Report{}, err
	}
	specs := parseRows(grid[1:], lay)

	// stopImport forces WithinTx to roll back without treating it as an
	// infrastructure failure: on a dry run always, and on a commit that found
	// row errors so the import stays all-or-nothing.
	stopImport := errors.New("import rolled back")
	var report Report
	txErr := im.svc.WithinTx(ctx, func(tx *domain.Service) error {
		report = applyRows(ctx, tx, specs, dimByName)
		if !commit || report.HasErrors() {
			return stopImport
		}
		return nil
	})
	if txErr != nil && !errors.Is(txErr, stopImport) {
		return report, txErr
	}
	report.Committed = commit && !report.HasErrors()
	return report, nil
}

// layout maps the spreadsheet's columns to their indexes. The required columns
// carry an index; the optional ones are -1 when absent. Every column that is not
// one of the known headers is a Dimension column.
type layout struct {
	title      int
	owner      int
	soWhat     int
	kind       int
	delivery   int
	milestones int
	metrics    int
	parents    int
	dimensions []dimensionColumn
}

// dimensionColumn is a spreadsheet column whose header names a Dimension; each
// cell holds the value to assign in that Dimension.
type dimensionColumn struct {
	dimension string
	index     int
}

func parseHeader(header []string, dimByName map[string]domain.Dimension) (layout, error) {
	lay := layout{title: -1, owner: -1, soWhat: -1, kind: -1, delivery: -1, milestones: -1, metrics: -1, parents: -1}
	for i, raw := range header {
		switch normalize(raw) {
		case "title":
			lay.title = i
		case "owner":
			lay.owner = i
		case "so what":
			lay.soWhat = i
		case "kind":
			lay.kind = i
		case "delivery date":
			lay.delivery = i
		case "milestones":
			lay.milestones = i
		case "metrics":
			lay.metrics = i
		case "parents":
			lay.parents = i
		case "":
			// A blank header names no column; ignore it.
		default:
			if _, ok := dimByName[normalize(raw)]; !ok {
				return layout{}, fmt.Errorf("%w: column %q is neither a known field nor a defined Dimension", domain.ErrValidation, strings.TrimSpace(raw))
			}
			lay.dimensions = append(lay.dimensions, dimensionColumn{dimension: strings.TrimSpace(raw), index: i})
		}
	}
	var missing []string
	for _, req := range []struct {
		name string
		idx  int
	}{{"Title", lay.title}, {"Owner", lay.owner}, {"So What", lay.soWhat}, {"Kind", lay.kind}} {
		if req.idx < 0 {
			missing = append(missing, req.name)
		}
	}
	if len(missing) > 0 {
		return layout{}, fmt.Errorf("%w: the spreadsheet is missing required column(s): %s", domain.ErrValidation, strings.Join(missing, ", "))
	}
	return lay, nil
}

// rowSpec is one parsed data row: the Goal to create, plus any errors found while
// parsing it. A row with parse errors creates nothing.
type rowSpec struct {
	line       int
	title      string
	owner      string
	soWhat     string
	kind       string
	delivery   time.Time
	milestones []milestoneSpec
	metrics    []metricSpec
	parents    []string
	dimensions []dimensionValue
	errs       []string
}

type milestoneSpec struct {
	name string
	date time.Time
}

type metricSpec struct {
	name       string
	unit       string
	direction  string
	baseline   float64
	target     float64
	targetDate time.Time
}

type dimensionValue struct {
	dimension string
	value     string
}

func parseRows(rows [][]string, lay layout) []rowSpec {
	specs := make([]rowSpec, 0, len(rows))
	for i, row := range rows {
		specs = append(specs, parseRow(i+1, row, lay))
	}
	return specs
}

func parseRow(line int, row []string, lay layout) rowSpec {
	s := rowSpec{
		line:   line,
		title:  cell(row, lay.title),
		owner:  cell(row, lay.owner),
		soWhat: cell(row, lay.soWhat),
	}
	if s.title == "" {
		s.errs = append(s.errs, "Title is required")
	}
	if s.owner == "" {
		s.errs = append(s.errs, "Owner is required")
	} else if !strings.Contains(s.owner, "@") {
		s.errs = append(s.errs, fmt.Sprintf("Owner %q is not an email address", s.owner))
	}
	if s.soWhat == "" {
		s.errs = append(s.errs, "So What is required")
	}

	kindCell := cell(row, lay.kind)
	deliveryCell := cell(row, lay.delivery)
	switch normalize(kindCell) {
	case "dated":
		s.kind = domain.GoalDated
		if deliveryCell == "" {
			s.errs = append(s.errs, "a Dated Goal needs a Delivery Date")
		} else if d, err := parseDate(deliveryCell); err != nil {
			s.errs = append(s.errs, fmt.Sprintf("bad Delivery Date %q: use YYYY-MM-DD", deliveryCell))
		} else {
			s.delivery = d
		}
	case "ongoing":
		s.kind = domain.GoalOngoing
		if deliveryCell != "" {
			s.errs = append(s.errs, "an Ongoing Goal must not have a Delivery Date")
		}
	default:
		s.errs = append(s.errs, fmt.Sprintf("Kind %q must be \"Dated\" or \"Ongoing\"", kindCell))
	}

	for _, entry := range splitEntries(cell(row, lay.milestones)) {
		m, err := parseMilestone(entry)
		if err != nil {
			s.errs = append(s.errs, err.Error())
			continue
		}
		s.milestones = append(s.milestones, m)
	}
	for _, entry := range splitEntries(cell(row, lay.metrics)) {
		m, err := parseMetric(entry)
		if err != nil {
			s.errs = append(s.errs, err.Error())
			continue
		}
		s.metrics = append(s.metrics, m)
	}
	s.parents = splitEntries(cell(row, lay.parents))
	for _, dc := range lay.dimensions {
		if v := cell(row, dc.index); v != "" {
			s.dimensions = append(s.dimensions, dimensionValue{dimension: dc.dimension, value: v})
		}
	}
	return s
}

func parseMilestone(entry string) (milestoneSpec, error) {
	name, dateStr, ok := strings.Cut(entry, "@")
	name = strings.TrimSpace(name)
	dateStr = strings.TrimSpace(dateStr)
	if !ok || name == "" || dateStr == "" {
		return milestoneSpec{}, fmt.Errorf("bad Milestone %q: use \"Name @ YYYY-MM-DD\"", entry)
	}
	date, err := parseDate(dateStr)
	if err != nil {
		return milestoneSpec{}, fmt.Errorf("bad Milestone date in %q: use YYYY-MM-DD", entry)
	}
	return milestoneSpec{name: name, date: date}, nil
}

func parseMetric(entry string) (metricSpec, error) {
	parts := strings.Split(entry, "|")
	if len(parts) != 6 {
		return metricSpec{}, fmt.Errorf("bad Metric %q: use \"Name | unit | up|down | baseline | target | YYYY-MM-DD\"", entry)
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	m := metricSpec{name: parts[0], unit: parts[1], direction: normalize(parts[2])}
	if m.direction != domain.MetricUp && m.direction != domain.MetricDown {
		return metricSpec{}, fmt.Errorf("bad Metric direction %q in %q: use \"up\" or \"down\"", parts[2], entry)
	}
	baseline, err := strconv.ParseFloat(parts[3], 64)
	if err != nil {
		return metricSpec{}, fmt.Errorf("bad Metric baseline %q in %q", parts[3], entry)
	}
	target, err := strconv.ParseFloat(parts[4], 64)
	if err != nil {
		return metricSpec{}, fmt.Errorf("bad Metric target %q in %q", parts[4], entry)
	}
	date, err := parseDate(parts[5])
	if err != nil {
		return metricSpec{}, fmt.Errorf("bad Metric target date %q in %q: use YYYY-MM-DD", parts[5], entry)
	}
	m.baseline = baseline
	m.target = target
	m.targetDate = date
	return m, nil
}

// applyRows creates a Goal for every row that parsed cleanly, then links the
// Goals to their parents. It runs inside the caller's transaction, so a caller
// that rolls back (a dry run, or a commit that found errors) saves nothing.
func applyRows(ctx context.Context, tx *domain.Service, specs []rowSpec, dimByName map[string]domain.Dimension) Report {
	results := make([]RowResult, len(specs))
	titleCount := map[string]int{}
	for _, s := range specs {
		if s.title != "" {
			titleCount[s.title]++
		}
	}
	for i, s := range specs {
		results[i] = RowResult{Line: s.line, Title: s.title, Errors: append([]string(nil), s.errs...)}
		if s.title != "" && titleCount[s.title] > 1 {
			results[i].Errors = append(results[i].Errors, "duplicate Title in file; titles must be unique to link Goals by title")
		}
	}

	titleToGoal := map[string]int64{}
	for i, s := range specs {
		if len(results[i].Errors) > 0 {
			continue
		}
		goalID, errs := createGoal(ctx, tx, s, dimByName)
		if len(errs) > 0 {
			results[i].Errors = append(results[i].Errors, errs...)
			continue
		}
		results[i].GoalID = goalID
		titleToGoal[s.title] = goalID
	}

	for i, s := range specs {
		if results[i].GoalID == 0 {
			continue
		}
		for _, parentTitle := range s.parents {
			parentID, ok := titleToGoal[parentTitle]
			if !ok {
				results[i].Errors = append(results[i].Errors, fmt.Sprintf("parent Goal %q is not one of the imported Goals", parentTitle))
				continue
			}
			if _, err := tx.ImportLink(ctx, results[i].GoalID, parentID); err != nil {
				results[i].Errors = append(results[i].Errors, linkMessage(err, parentTitle))
			}
		}
	}
	return Report{Rows: results}
}

func createGoal(ctx context.Context, tx *domain.Service, s rowSpec, dimByName map[string]domain.Dimension) (int64, []string) {
	owner, err := tx.EnsureAccount(ctx, s.owner)
	if err != nil {
		return 0, []string{fmt.Sprintf("could not create Owner account: %v", err)}
	}
	g, err := tx.CreateGoal(ctx, domain.CreateGoalInput{Title: s.title, SoWhat: s.soWhat, OwnerID: owner.ID})
	if err != nil {
		return 0, []string{message(err)}
	}

	var errs []string
	if s.kind == domain.GoalDated {
		if _, err := tx.MarkGoalDated(ctx, g.ID, s.delivery); err != nil {
			errs = append(errs, message(err))
		}
	} else {
		if _, err := tx.MarkGoalOngoing(ctx, g.ID); err != nil {
			errs = append(errs, message(err))
		}
	}
	for _, m := range s.milestones {
		if _, err := tx.AddMilestone(ctx, domain.AddMilestoneInput{GoalID: g.ID, Name: m.name, TargetDate: m.date}); err != nil {
			errs = append(errs, message(err))
		}
	}
	for _, m := range s.metrics {
		if _, err := tx.AddMetric(ctx, domain.AddMetricInput{
			GoalID:     g.ID,
			Name:       m.name,
			Unit:       m.unit,
			Direction:  m.direction,
			Baseline:   m.baseline,
			Target:     m.target,
			TargetDate: m.targetDate,
		}); err != nil {
			errs = append(errs, message(err))
		}
	}
	for _, dv := range s.dimensions {
		dim, ok := dimByName[normalize(dv.dimension)]
		if !ok {
			errs = append(errs, fmt.Sprintf("unknown Dimension %q", dv.dimension))
			continue
		}
		valueID, ok := valueIDOf(dim, dv.value)
		if !ok {
			errs = append(errs, fmt.Sprintf("%q is not a value of Dimension %q", dv.value, dim.Name))
			continue
		}
		if err := tx.AssignGoalValue(ctx, g.ID, valueID); err != nil {
			errs = append(errs, message(err))
		}
	}
	if len(errs) > 0 {
		return 0, errs
	}
	return g.ID, nil
}

func valueIDOf(dim domain.Dimension, value string) (int64, bool) {
	for _, v := range dim.Values {
		if normalize(v.Value) == normalize(value) {
			return v.ID, true
		}
	}
	return 0, false
}

// message renders a domain error for a row, dropping the internal "validation
// failed:" prefix so the report reads plainly.
func message(err error) string {
	return strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")
}

func linkMessage(err error, parentTitle string) string {
	if errors.Is(err, domain.ErrCycle) {
		return fmt.Sprintf("linking to parent %q would create a cycle", parentTitle)
	}
	return fmt.Sprintf("could not link to parent %q: %s", parentTitle, message(err))
}

// cell returns the trimmed value of column idx in row, or "" when the column is
// absent (idx < 0) or the row is short (a trailing empty cell CSV omits).
func cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// splitEntries splits a multi-value cell on ";" into trimmed, non-empty entries.
func splitEntries(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseDate(s string) (time.Time, error) {
	return time.Parse(dateFormat, strings.TrimSpace(s))
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// parseGrid reads the spreadsheet into rows of cells, choosing the CSV or XLSX
// reader by the filename extension and content. It errors if the file has no
// header row.
func parseGrid(filename string, data []byte) ([][]string, error) {
	var (
		grid [][]string
		err  error
	)
	if isXLSX(filename, data) {
		grid, err = parseXLSX(data)
	} else {
		grid, err = parseCSV(data)
	}
	if err != nil {
		return nil, err
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("%w: the spreadsheet has no rows", domain.ErrValidation)
	}
	return grid, nil
}

func parseCSV(data []byte) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1 // rows may omit trailing empty columns
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: could not read CSV: %v", domain.ErrValidation, err)
	}
	return rows, nil
}

func isXLSX(filename string, data []byte) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".xlsx") {
		return true
	}
	// An XLSX file is a ZIP archive, which starts with the "PK\x03\x04" magic.
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04
}

// parseXLSX reads the first worksheet of an XLSX workbook into rows of cells.
func parseXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: could not read XLSX: %v", domain.ErrValidation, err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%w: the workbook has no worksheets", domain.ErrValidation)
	}
	sheet := sheets[0]
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read worksheet %q: %v", domain.ErrValidation, sheet, err)
	}
	raw, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("%w: could not read worksheet %q: %v", domain.ErrValidation, sheet, err)
	}
	props, err := f.GetWorkbookProps()
	if err != nil {
		return nil, fmt.Errorf("%w: could not read XLSX: %v", domain.ErrValidation, err)
	}
	date1904 := props.Date1904 != nil && *props.Date1904

	// A date cell holds a number that its format displays as a date — as
	// 06-30-26, or as Jul-26 for the first of a month — so its displayed text is
	// no use to the importer. Read every date cell as YYYY-MM-DD instead, in
	// whichever column it sits.
	for r, row := range rows {
		for c, shown := range row {
			if r >= len(raw) || c >= len(raw[r]) || raw[r][c] == shown {
				continue
			}
			if d, ok := dateCell(f, sheet, c+1, r+1, raw[r][c], date1904); ok {
				row[c] = d.Format(dateFormat)
			}
		}
	}
	return rows, nil
}

// dateCell reports the date held by the cell at (col, row), whose raw value is
// raw, and whether the cell is a date cell at all: a date-typed cell, or a number
// whose format shows a date.
func dateCell(f *excelize.File, sheet string, col, row int, raw string, date1904 bool) (time.Time, bool) {
	ref, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return time.Time{}, false
	}
	if typ, err := f.GetCellType(sheet, ref); err == nil && typ == excelize.CellTypeDate {
		// A date-typed cell stores an ISO 8601 date-time.
		if len(raw) >= len(dateFormat) {
			if d, err := parseDate(raw[:len(dateFormat)]); err == nil {
				return d, true
			}
		}
		return time.Time{}, false
	}
	serial, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, false
	}
	styleID, err := f.GetCellStyle(sheet, ref)
	if err != nil {
		return time.Time{}, false
	}
	style, err := f.GetStyle(styleID)
	if err != nil || !isDateFormat(style) {
		return time.Time{}, false
	}
	t, err := excelize.ExcelDateToTime(serial, date1904)
	if err != nil {
		return time.Time{}, false
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), true
}

// builtInDateFormats are the built-in number formats that show a date: 14–17
// (m/d/yy, d-mmm-yy, d-mmm, mmm-yy) and 22 (m/d/yy h:mm). 18–21 and 45–47 show
// only a time.
var builtInDateFormats = map[int]bool{14: true, 15: true, 16: true, 17: true, 22: true}

// isDateFormat reports whether a cell style's number format shows a date. A
// custom format shows one when, outside its quoted text, bracketed locale and
// colour codes, and escaped characters, it has a year or day token.
func isDateFormat(style *excelize.Style) bool {
	if style.CustomNumFmt == nil {
		return builtInDateFormats[style.NumFmt]
	}
	code := strings.ToLower(*style.CustomNumFmt)
	var inQuote, inBracket, escaped bool
	for _, ch := range code {
		switch {
		case escaped:
			escaped = false
		case inQuote:
			inQuote = ch != '"'
		case inBracket:
			inBracket = ch != ']'
		case ch == '"':
			inQuote = true
		case ch == '[':
			inBracket = true
		case ch == '\\', ch == '_', ch == '*':
			escaped = true
		case ch == 'y', ch == 'd':
			return true
		}
	}
	return false
}
