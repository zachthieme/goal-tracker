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
const idColumn = "ID"

// goalColumns are the import format's own columns, in the order the download
// writes them; each Dimension and Field column follows them.
var goalColumns = []string{"Title", "Owner", "So What", "Kind", "Delivery Date", "Milestones", "Metrics", "Parents"}

// entrySeparator joins the entries of a cell holding several, the way the
// import format writes them.
const entrySeparator = "; "

// Download writes goals as CSV in the import format, one row each in the order
// given (#81): a leading ID column holding each Goal's id, the import format's
// own columns, then one column per Dimension and per Field that isn't Retired.
// Several values or entries in a cell are separated by semicolons, and the
// Owner is written as an email, so the file can be edited and imported again.
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

	out := csv.NewWriter(w)
	header := append([]string{idColumn}, goalColumns...)
	for _, d := range dims {
		header = append(header, d.Name)
	}
	for _, f := range fields {
		header = append(header, f.Name)
	}
	if err := out.Write(header); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	for _, g := range goals {
		cells, err := goalCells(ctx, im.svc, g)
		if err != nil {
			return err
		}
		record := []string{strconv.FormatInt(g.ID, 10)}
		for _, col := range goalColumns {
			record = append(record, cells[col])
		}
		values, err := im.svc.GoalValues(ctx, g.ID)
		if err != nil {
			return err
		}
		for _, d := range dims {
			var names []string
			for _, v := range values {
				if v.DimensionID == d.ID {
					names = append(names, v.Value)
				}
			}
			record = append(record, strings.Join(names, entrySeparator))
		}
		fieldValues, err := im.svc.GoalFields(ctx, g.ID)
		if err != nil {
			return err
		}
		for _, f := range fields {
			var value string
			for _, fv := range fieldValues {
				if fv.Field.ID == f.ID {
					value = fv.Value
				}
			}
			record = append(record, value)
		}
		if err := out.Write(record); err != nil {
			return fmt.Errorf("write CSV: %w", err)
		}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}

// goalCells is g's cell in each of the import format's own columns, by column
// name, written as the import reads them: its Milestones not removed, its
// Metrics, and the titles of the Goals it contributes to.
func goalCells(ctx context.Context, svc *domain.Service, g domain.Goal) (map[string]string, error) {
	cells := map[string]string{
		"Title":   g.Title,
		"Owner":   g.Owner.Email,
		"So What": g.SoWhat,
		"Kind":    g.Kind,
	}
	if !g.DeliveryDate.IsZero() {
		cells["Delivery Date"] = g.DeliveryDate.Format(dateFormat)
	}
	milestones, err := svc.ListMilestones(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, m := range milestones {
		if m.Status != domain.MilestoneRemoved {
			entries = append(entries, m.Name+" @ "+m.TargetDate.Format(dateFormat))
		}
	}
	cells["Milestones"] = strings.Join(entries, entrySeparator)
	metrics, err := svc.ListMetrics(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	entries = nil
	for _, m := range metrics {
		entries = append(entries, strings.Join([]string{
			m.Name, m.Unit, m.Direction, formatNumber(m.Baseline), formatNumber(m.Target), m.TargetDate.Format(dateFormat),
		}, " | "))
	}
	cells["Metrics"] = strings.Join(entries, entrySeparator)
	parents, err := svc.ParentsOf(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	entries = nil
	for _, p := range parents {
		entries = append(entries, p.Title)
	}
	cells["Parents"] = strings.Join(entries, entrySeparator)
	return cells, nil
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
