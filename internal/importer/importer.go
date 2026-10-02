// Package importer loads a pilot's real Goals from a spreadsheet instead of
// having an Admin enter them by hand (ticket #22). It reads CSV and XLSX, one
// Goal per row, and turns each row into the same domain commands a person would
// run: it gives every named Owner an account, creates the Goal with its So What,
// marks it Dated or Ongoing, adds its Milestones and Metrics, assigns its
// Dimension values (adding new ones to an Extendable list), sets its Fields,
// and links it to its parent Goals — accepting those links automatically, since
// only an Admin runs an import (CONTEXT.md: Admin can override links).
//
// An import is validated row by row. A DryRun reports the errors and saves
// nothing; a Commit is all-or-nothing: if any row has an error the whole import
// rolls back and nothing is saved.
//
// A row with an ID updates that existing Goal's Dimension values and Fields
// instead (#81), so a file the Goal table downloads (see Download) can be
// edited and imported again. The column format is documented in
// docs/import-format.md, with an example in testdata/import-example.csv.
package importer

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
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
// committed). Ignored names, once for the whole file, the import format's own
// columns that a row with an ID changed: such a row updates only its Goal's
// Dimension values and Fields, so those changes were ignored (#81).
type Report struct {
	Rows      []RowResult
	Committed bool
	Ignored   []string
}

// RowResult is what happened to one data row: its row number as the
// spreadsheet numbers it (the header is row 1), the Goal title it named, the id
// of the Goal created or updated from it (0 when nothing was saved), whether
// it updated an existing Goal (a row with an ID) rather than creating one, and
// any errors found on it.
type RowResult struct {
	Line    int
	Title   string
	GoalID  int64
	Updated bool
	Errors  []string
}

// UpdatedCount returns how many rows updated an existing Goal.
func (r Report) UpdatedCount() int {
	n := 0
	for _, row := range r.Rows {
		if row.Updated {
			n++
		}
	}
	return n
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
	fields, err := im.svc.ListFields(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("list fields: %w", err)
	}
	// A Retired Dimension or Field is no longer offered, so its column is
	// rejected like any unknown one (CONTEXT.md: Retired).
	attrs := attributes{
		dimensions: map[string]domain.Dimension{},
		fields:     map[string]domain.Field{},
	}
	for _, d := range domain.OfferedDimensions(dims) {
		attrs.dimensions[normalize(d.Name)] = d
	}
	for _, f := range domain.OfferedFields(fields) {
		attrs.fields[normalize(f.Name)] = f
	}

	lay, err := parseHeader(grid[0].cells, attrs)
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
		report = applyRows(ctx, tx, adminID, specs)
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

// attributes are the Dimensions and Fields a column header may name, by their
// normalized names.
type attributes struct {
	dimensions map[string]domain.Dimension
	fields     map[string]domain.Field
}

// layout maps the spreadsheet's columns to their indexes. The required columns
// carry an index; the optional ones are -1 when absent. Every column that is not
// one of the known headers is a Dimension or a Field column.
type layout struct {
	id         int
	title      int
	owner      int
	soWhat     int
	kind       int
	delivery   int
	milestones int
	metrics    int
	parents    int
	dimensions []dimensionColumn
	fields     []fieldColumn
}

// dimensionColumn is a spreadsheet column whose header names a Dimension; each
// cell holds the value to assign in that Dimension.
type dimensionColumn struct {
	dimension domain.Dimension
	index     int
}

// fieldColumn is a spreadsheet column whose header names a Field; each cell
// holds the Field's value.
type fieldColumn struct {
	field domain.Field
	index int
}

// goalColumns maps each of the import format's own columns the spreadsheet
// has to its index.
func (lay layout) goalColumns() map[string]int {
	cols := map[string]int{}
	for i, idx := range []int{lay.title, lay.owner, lay.soWhat, lay.kind, lay.delivery, lay.milestones, lay.metrics, lay.parents} {
		if idx >= 0 {
			cols[goalColumns[i]] = idx
		}
	}
	return cols
}

func parseHeader(header []string, attrs attributes) (layout, error) {
	lay := layout{id: -1, title: -1, owner: -1, soWhat: -1, kind: -1, delivery: -1, milestones: -1, metrics: -1, parents: -1}
	for i, raw := range header {
		switch normalize(raw) {
		case "id":
			lay.id = i
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
			if dim, ok := attrs.dimensions[normalize(raw)]; ok {
				lay.dimensions = append(lay.dimensions, dimensionColumn{dimension: dim, index: i})
			} else if f, ok := attrs.fields[normalize(raw)]; ok {
				lay.fields = append(lay.fields, fieldColumn{field: f, index: i})
			} else {
				return layout{}, fmt.Errorf("%w: column %q is not a known column, a Dimension or a Field", domain.ErrValidation, strings.TrimSpace(raw))
			}
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
// parsing it. A row whose Title, Owner, So What, Kind or Delivery Date is missing
// or bad is incomplete and creates nothing; a bad Milestone, Metric or Dimension
// value is reported but does not stop the rest of the row being checked.
//
// A row with an ID instead updates Goal goalID (#81): it sets the Goal's values
// in each Dimension column to dimensionSets, a blank cell clearing them, and
// its value in each Field column to fields, a blank value clearing it. Its
// other columns are ignored; cells keeps them, by column name, so the report
// can say which it changed.
type rowSpec struct {
	line          int
	update        bool
	goalID        int64
	dimensionSets []dimensionSet
	cells         map[string]string
	title         string
	owner         string
	soWhat        string
	kind          string
	delivery      time.Time
	milestones    []milestoneSpec
	metrics       []metricSpec
	parents       []string
	dimensions    []dimensionValue
	fields        []fieldValue
	errs          []string
	incomplete    bool
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

// dimensionValue is a value to give the Goal: an existing value by its id, or
// a value newValue to add to Extendable Dimension dimensionID's list.
type dimensionValue struct {
	valueID     int64
	dimensionID int64
	newValue    string
}

// dimensionSet is every value a row with an ID gives its Goal in one
// Dimension, none to clear it.
type dimensionSet struct {
	dimensionID int64
	values      []dimensionValue
}

type fieldValue struct {
	fieldID int64
	value   string
}

func parseRows(rows []sheetRow, lay layout) []rowSpec {
	specs := make([]rowSpec, 0, len(rows))
	for _, row := range rows {
		specs = append(specs, parseRow(row.number, row.cells, lay))
	}
	return specs
}

func parseRow(line int, row []string, lay layout) rowSpec {
	if raw := cell(row, lay.id); raw != "" {
		return parseUpdateRow(line, raw, row, lay)
	}
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
	// Every error so far is in a field the Goal cannot be created without.
	s.incomplete = len(s.errs) > 0

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
		values, errs := parseDimensionCell(dc.dimension, cell(row, dc.index))
		s.dimensions = append(s.dimensions, values...)
		s.errs = append(s.errs, errs...)
	}
	for _, fc := range lay.fields {
		v := cell(row, fc.index)
		if v == "" {
			continue
		}
		if err := fc.field.Check(v); err != nil {
			s.errs = append(s.errs, message(err))
			continue
		}
		s.fields = append(s.fields, fieldValue{fieldID: fc.field.ID, value: v})
	}
	return s
}

// parseUpdateRow parses a row whose ID cell holds rawID: it reads only the
// row's Dimension and Field columns, each one a value to set on the Goal, a
// blank cell clearing it (#81).
func parseUpdateRow(line int, rawID string, row []string, lay layout) rowSpec {
	s := rowSpec{line: line, update: true, title: cell(row, lay.title), cells: map[string]string{}}
	for col, idx := range lay.goalColumns() {
		s.cells[col] = cell(row, idx)
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		s.errs = append(s.errs, fmt.Sprintf("ID %q is not a Goal's id", rawID))
	}
	s.goalID = id
	for _, dc := range lay.dimensions {
		values, errs := parseDimensionCell(dc.dimension, cell(row, dc.index))
		s.dimensionSets = append(s.dimensionSets, dimensionSet{dimensionID: dc.dimension.ID, values: values})
		s.errs = append(s.errs, errs...)
	}
	for _, fc := range lay.fields {
		v := cell(row, fc.index)
		if v != "" {
			if err := fc.field.Check(v); err != nil {
				s.errs = append(s.errs, message(err))
				continue
			}
		}
		s.fields = append(s.fields, fieldValue{fieldID: fc.field.ID, value: v})
	}
	return s
}

// parseDimensionCell reads the values a Dimension cell gives a Goal, with an
// error for each it can't: more than one in a Dimension that takes one, or a
// value not in a Fixed Dimension's list.
func parseDimensionCell(dim domain.Dimension, raw string) ([]dimensionValue, []string) {
	values := splitValues(raw)
	if len(values) > 1 && !dim.TakesSeveral() {
		return nil, []string{fmt.Sprintf("%s takes one value per Goal", dim.Name)}
	}
	var (
		out  []dimensionValue
		errs []string
	)
	for _, v := range values {
		if valueID, ok := valueIDOf(dim, v); ok {
			out = append(out, dimensionValue{valueID: valueID})
			continue
		}
		// An Extendable list gains the unknown value when the Goal is
		// saved; a Fixed one is added to only from the Dimensions page
		// (CONTEXT.md: Fixed, Extendable).
		if !dim.Extendable() {
			errs = append(errs, fmt.Sprintf("%q is not a value of Dimension %q", v, dim.Name))
			continue
		}
		out = append(out, dimensionValue{dimensionID: dim.ID, newValue: v})
	}
	return out, errs
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

// applyRows creates a Goal for every complete row, then links the Goals to their
// parents, collecting every error found on each row rather than stopping at the
// first. It runs inside the caller's transaction, so a caller that rolls back (a
// dry run, or a commit that found errors) saves nothing.
func applyRows(ctx context.Context, tx *domain.Service, adminID int64, specs []rowSpec) Report {
	results := make([]RowResult, len(specs))
	// Only the rows creating a Goal are linked by title; a row with an ID
	// updates its Goal's values and nothing else.
	titleCount := map[string]int{}
	idCount := map[int64]int{}
	for _, s := range specs {
		switch {
		case s.update:
			idCount[s.goalID]++
		case s.title != "":
			titleCount[s.title]++
		}
	}
	for i, s := range specs {
		results[i] = RowResult{Line: s.line, Title: s.title, Updated: s.update, Errors: append([]string(nil), s.errs...)}
		switch {
		case s.update && s.goalID > 0 && idCount[s.goalID] > 1:
			results[i].Errors = append(results[i].Errors, fmt.Sprintf("ID %d is on more than one row", s.goalID))
		case !s.update && s.title != "" && titleCount[s.title] > 1:
			results[i].Errors = append(results[i].Errors, "duplicate Title in file; titles must be unique to link Goals by title")
		}
	}

	// goalIDs holds the Goal created or updated from each row, or 0;
	// titleToGoal maps a created Goal's title to it, for linking.
	goalIDs := make([]int64, len(specs))
	titleToGoal := map[string]int64{}
	ignored := map[string]bool{}
	for i, s := range specs {
		if s.update {
			if len(results[i].Errors) > 0 {
				continue
			}
			title, changed, errs := updateGoal(ctx, tx, adminID, s)
			results[i].Errors = append(results[i].Errors, errs...)
			for _, col := range changed {
				ignored[col] = true
			}
			if title != "" {
				results[i].Title = title
				goalIDs[i] = s.goalID
			}
			continue
		}
		if s.incomplete || titleCount[s.title] > 1 {
			continue
		}
		goalID, errs := createGoal(ctx, tx, adminID, s)
		results[i].Errors = append(results[i].Errors, errs...)
		if goalID != 0 {
			goalIDs[i] = goalID
			titleToGoal[s.title] = goalID
		}
	}

	for i, s := range specs {
		if s.update {
			continue
		}
		for _, parentTitle := range s.parents {
			if titleCount[parentTitle] == 0 {
				results[i].Errors = append(results[i].Errors, fmt.Sprintf("parent Goal %q is not one of the imported Goals", parentTitle))
				continue
			}
			// A parent's own row reports why it was not created; the link just
			// cannot be tried.
			parentID, ok := titleToGoal[parentTitle]
			if !ok || goalIDs[i] == 0 {
				continue
			}
			if _, err := tx.ImportLink(ctx, goalIDs[i], parentID); err != nil {
				results[i].Errors = append(results[i].Errors, linkMessage(err, parentTitle))
			}
		}
	}

	for i := range results {
		if len(results[i].Errors) == 0 {
			results[i].GoalID = goalIDs[i]
		}
	}
	report := Report{Rows: results}
	for _, col := range goalColumns {
		if ignored[col] {
			report.Ignored = append(report.Ignored, col)
		}
	}
	return report
}

// createGoal creates the row's Goal with its Kind, Milestones, Metrics and
// Dimension values. It returns the Goal's id (0 if the Goal itself could not be
// created) and every error found adding the rest.
func createGoal(ctx context.Context, tx *domain.Service, adminID int64, s rowSpec) (int64, []string) {
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
		valueID, err := ensureValue(ctx, tx, adminID, dv)
		if err != nil {
			errs = append(errs, message(err))
			continue
		}
		if err := tx.AssignGoalValue(ctx, adminID, g.ID, valueID); err != nil {
			errs = append(errs, message(err))
		}
	}
	for _, fv := range s.fields {
		if err := tx.SetGoalField(ctx, adminID, g.ID, fv.fieldID, fv.value); err != nil {
			errs = append(errs, message(err))
		}
	}
	return g.ID, errs
}

// updateGoal sets the values of Goal s.goalID from a row with an ID, as the
// Goal page sets them: each Dimension column's values together, so a value left
// out is removed, and each Field column's value, a blank one clearing it. Each
// change is kept in the Goal's Value history, and a value it already has
// changes nothing. It returns the Goal's title ("" when no Goal has the ID),
// the import format's own columns whose cells differ from the Goal, which are
// ignored, and every error found.
func updateGoal(ctx context.Context, tx *domain.Service, adminID int64, s rowSpec) (title string, ignored, errs []string) {
	g, err := tx.ViewGoal(ctx, s.goalID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil, []string{fmt.Sprintf("no Goal has ID %d", s.goalID)}
	} else if err != nil {
		return "", nil, []string{message(err)}
	}
	current, err := goalCells(ctx, tx, g)
	if err != nil {
		return "", nil, []string{message(err)}
	}
	for _, col := range goalColumns {
		if cellText, ok := s.cells[col]; ok && canonical(col, cellText) != canonical(col, current[col]) {
			ignored = append(ignored, col)
		}
	}
	for _, set := range s.dimensionSets {
		valueIDs := make([]int64, 0, len(set.values))
		for _, dv := range set.values {
			id, err := ensureValue(ctx, tx, adminID, dv)
			if err != nil {
				errs = append(errs, message(err))
				continue
			}
			valueIDs = append(valueIDs, id)
		}
		if err := tx.SetGoalValues(ctx, adminID, g.ID, set.dimensionID, valueIDs); err != nil {
			errs = append(errs, message(err))
		}
	}
	for _, fv := range s.fields {
		if err := tx.SetGoalField(ctx, adminID, g.ID, fv.fieldID, fv.value); err != nil {
			errs = append(errs, message(err))
		}
	}
	return g.Title, ignored, errs
}

// canonical is a cell in one of the import format's own columns as it compares
// with the Goal's: an email and a Kind whatever their letter case, and
// Milestones, Metrics and Parents whatever the spaces around their separators.
func canonical(col, text string) string {
	switch col {
	case "Owner", "Kind":
		return normalize(text)
	case "Milestones", "Metrics", "Parents":
		entries := splitEntries(text)
		for i, e := range entries {
			parts := strings.FieldsFunc(e, func(r rune) bool { return r == '|' || r == '@' })
			for j := range parts {
				parts[j] = strings.TrimSpace(parts[j])
			}
			entries[i] = strings.Join(parts, "|")
		}
		return strings.Join(entries, ";")
	}
	return strings.TrimSpace(text)
}

// ensureValue is dv's value id, adding a new value to its Extendable
// Dimension's list first, by the importing Admin and last in the list. An
// earlier row may already have added it, and then it is matched instead.
func ensureValue(ctx context.Context, tx *domain.Service, adminID int64, dv dimensionValue) (int64, error) {
	if dv.valueID != 0 {
		return dv.valueID, nil
	}
	added, err := tx.AddDimensionValue(ctx, adminID, dv.dimensionID, dv.newValue)
	if err != nil {
		return 0, err
	}
	return added.ID, nil
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

// splitValues splits a Dimension cell into its values, as splitEntries does,
// dropping a value that repeats an earlier one whatever its letter case.
func splitValues(raw string) []string {
	var out []string
	for _, v := range splitEntries(raw) {
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, v) }) {
			out = append(out, v)
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

// sheetRow is one row of the spreadsheet: its number as the spreadsheet numbers
// it (the header is row 1) and its cells.
type sheetRow struct {
	number int
	cells  []string
}

// parseGrid reads the spreadsheet into rows of cells, choosing the CSV or XLSX
// reader by the filename extension and content. It errors if the file has no
// header row.
func parseGrid(filename string, data []byte) ([]sheetRow, error) {
	var (
		grid []sheetRow
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

// parseCSV reads CSV records, numbering them as a spreadsheet opening the file
// would: the CSV reader skips blank lines, but each is still a row, and a quoted
// cell spanning several lines is still one row. A leading UTF-8 byte-order
// mark, which Excel writes when it saves "CSV UTF-8", is ignored (#101).
func parseCSV(data []byte) ([]sheetRow, error) {
	data = bytes.TrimPrefix(data, []byte("\uFEFF"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // rows may omit trailing empty columns
	var (
		rows     []sheetRow
		number   int
		nextLine = 1 // the file line the next row starts on, if none is blank
	)
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: could not read CSV: %v", domain.ErrValidation, err)
		}
		line, _ := r.FieldPos(0)
		number += line - nextLine + 1
		nextLine = line + 1
		for _, field := range record {
			nextLine += strings.Count(field, "\n")
		}
		rows = append(rows, sheetRow{number: number, cells: record})
	}
}

func isXLSX(filename string, data []byte) bool {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".xlsx") {
		return true
	}
	// An XLSX file is a ZIP archive, which starts with the "PK\x03\x04" magic.
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 0x03 && data[3] == 0x04
}

// parseXLSX reads the first worksheet of an XLSX workbook into rows of cells.
func parseXLSX(data []byte) ([]sheetRow, error) {
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
	grid := make([]sheetRow, len(rows))
	for i, row := range rows {
		grid[i] = sheetRow{number: i + 1, cells: row}
	}
	return grid, nil
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
