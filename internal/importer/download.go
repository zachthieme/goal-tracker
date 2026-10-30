package importer

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// idColumn heads the column that names the Goal a row updates (#81).
var idColumn = Columns[0].Name

// goalColumns are the import format's own columns but the ID, in the order the
// download writes them; each Dimension and Field column follows them.
var goalColumns = columnNames(Columns[1:])

// columnNames are the names of cols, in order.
func columnNames(cols []Column) []string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return names
}

// entrySeparator joins the entries of a cell holding several, the way the
// import format writes them.
const entrySeparator = "; "

// Download writes goals as CSV in the import format, one row each in the order
// given (#81): a leading ID column holding each Goal's id, the import format's
// own columns, then one column per Dimension and per Field that isn't Retired.
// Several values or entries in a cell are separated by semicolons, and the
// Owner is written as an email, so the file can be edited and imported again.
// A Goal carrying a value that contains a semicolon, named so before that was
// refused, would split on import, so the download is refused naming the value,
// and nothing is written (#101). Every cell but an ID that a spreadsheet would
// run as a formula is written behind a ' (see quoteFormula), which the import
// drops again (#424). Every Goal's values, Fields, Milestones, Metrics and
// parents are read up front, in as many queries for many Goals as for one
// (#450).
func (im *Importer) Download(ctx context.Context, w io.Writer, goals []domain.Goal) error {
	dims, err := im.svc.ListDimensions(ctx)
	if err != nil {
		return fmt.Errorf("list dimensions: %w", err)
	}
	fields, err := im.svc.ListFields(ctx)
	if err != nil {
		return fmt.Errorf("list fields: %w", err)
	}
	dims, fields = domain.OfferedDimensions(dims), domain.OfferedFields(fields)

	header := append([]string{idColumn}, goalColumns...)
	for _, d := range dims {
		header = append(header, d.Name)
	}
	for _, f := range fields {
		header = append(header, f.Name)
	}
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	columns, err := readGoalColumns(ctx, im.svc, ids)
	if err != nil {
		return err
	}
	values, err := im.svc.GoalValuesByGoal(ctx)
	if err != nil {
		return err
	}
	fieldValues, err := im.svc.GoalFieldsByGoal(ctx, ids)
	if err != nil {
		return err
	}
	records := [][]string{header}
	for _, g := range goals {
		cells := goalCells(g, columns)
		record := []string{strconv.FormatInt(g.ID, 10)}
		for _, col := range goalColumns {
			record = append(record, cells[col])
		}
		for _, d := range dims {
			var names []string
			for _, v := range values[g.ID] {
				if v.DimensionID != d.ID {
					continue
				}
				if strings.Contains(v.Value, ";") {
					return domain.Refuse(domain.ErrValidation,
						"%s carries the %s value %q, and a value can't contain a semicolon, because the import format uses it to separate values; an Admin can rename it on the Dimensions page", g.Title, d.Name, v.Value)
				}
				names = append(names, v.Value)
			}
			record = append(record, strings.Join(names, entrySeparator))
		}
		for _, f := range fields {
			var value string
			for _, fv := range fieldValues[g.ID] {
				if fv.Field.ID == f.ID {
					value = fv.Value
				}
			}
			record = append(record, value)
		}
		records = append(records, record)
	}
	for _, record := range records {
		for i := 1; i < len(record); i++ {
			record[i] = quoteFormula(record[i])
		}
	}
	if err := csv.NewWriter(w).WriteAll(records); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}

// goalColumnReads is what the import format's own columns hold of each of some
// Goals beyond the Goal itself, keyed by Goal ID: its Milestones, its Metrics
// and the Goals it contributes to.
type goalColumnReads struct {
	milestones map[int64][]domain.Milestone
	metrics    map[int64][]domain.Metric
	parents    map[int64][]domain.Goal
}

// readGoalColumns reads what the import format's own columns hold of goalIDs,
// in as many queries for many Goals as for one (#450).
func readGoalColumns(ctx context.Context, svc *domain.Service, goalIDs []int64) (goalColumnReads, error) {
	var reads goalColumnReads
	var err error
	if reads.milestones, err = svc.MilestonesByGoal(ctx, goalIDs); err != nil {
		return goalColumnReads{}, err
	}
	if reads.metrics, err = svc.MetricsByGoal(ctx, goalIDs); err != nil {
		return goalColumnReads{}, err
	}
	if reads.parents, err = svc.ParentsByGoal(ctx, goalIDs); err != nil {
		return goalColumnReads{}, err
	}
	return reads, nil
}

// goalCells is g's cell in each of the import format's own columns, by column
// name, written as the import reads them: its Milestones not removed, its
// Metrics, and the titles of the Goals it contributes to, from reads.
func goalCells(g domain.Goal, reads goalColumnReads) map[string]string {
	cells := map[string]string{
		"Title":          g.Title,
		"Owner":          g.Owner.Email,
		"Why it matters": g.WhyItMatters,
		"Kind":           g.Kind,
	}
	if !g.DeliveryDate.IsZero() {
		cells["Delivery Date"] = g.DeliveryDate.Format(dateFormat)
	}
	var entries []string
	for _, m := range reads.milestones[g.ID] {
		if m.Status != domain.MilestoneRemoved {
			entries = append(entries, m.Name+" @ "+m.TargetDate.Format(dateFormat))
		}
	}
	cells["Milestones"] = strings.Join(entries, entrySeparator)
	entries = nil
	for _, m := range reads.metrics[g.ID] {
		entries = append(entries, strings.Join([]string{
			m.Name, m.Unit, m.Direction, formatNumber(m.Baseline), formatNumber(m.Target), m.TargetDate.Format(dateFormat),
		}, " | "))
	}
	cells["Metrics"] = strings.Join(entries, entrySeparator)
	entries = nil
	for _, p := range reads.parents[g.ID] {
		entries = append(entries, p.Title)
	}
	cells["Parents"] = strings.Join(entries, entrySeparator)
	return cells
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// formulaTriggers are the characters that make a spreadsheet run a cell
// starting with one as a formula, so a cell such as =HYPERLINK(…) in a Goal's
// title can't run when an Admin opens the download (#424).
const formulaTriggers = "=+-@\t\r"

// quoteFormula is a download cell written so a spreadsheet opens it as text: a
// cell starting with a formula trigger gets a leading ', and so does one that
// already starts with one or more ' and then a trigger, so that unquoteFormula
// gives it back with its own ' (#443).
func quoteFormula(text string) string {
	if startsWithFormula(text) {
		return "'" + text
	}
	return text
}

// unquoteFormula is an imported cell with the ' that quoteFormula put in front
// of it dropped again. Any other leading ' is part of the cell.
func unquoteFormula(text string) string {
	if rest, ok := strings.CutPrefix(text, "'"); ok && startsWithFormula(rest) {
		return rest
	}
	return text
}

// startsWithFormula reports whether text, past any leading ', starts with a
// formula trigger.
func startsWithFormula(text string) bool {
	text = strings.TrimLeft(text, "'")
	return text != "" && strings.ContainsRune(formulaTriggers, rune(text[0]))
}
