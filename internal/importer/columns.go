package importer

import (
	"encoding/csv"
	"fmt"
	"io"
)

// Column is one of the import format's own columns: its header, whether a
// spreadsheet's header must have it, and what its cells hold (#676).
type Column struct {
	Name     string
	Required bool
	Meaning  string
}

// Columns are the import format's own columns, in the order the download and
// the template write them. Each Dimension and Field column follows them. The
// Import Goals page lists them, and docs/import-format.md's table must match.
var Columns = []Column{
	{Name: "ID", Meaning: "An existing Goal's id: the row updates that Goal's Dimension values and Fields instead of creating a Goal."},
	{Name: "Title", Required: true, Meaning: "The Goal's title, unique within the file."},
	{Name: "Owner", Required: true, Meaning: "The Owner's email address. An account is made for a new one."},
	{Name: "Why it matters", Required: true, Meaning: "The customer problem the Goal addresses."},
	{Name: "Kind", Required: true, Meaning: "Dated or Ongoing."},
	{Name: "Delivery Date", Meaning: "YYYY-MM-DD. Needed when Kind is Dated, and empty when it's Ongoing."},
	{Name: "Milestones", Meaning: "Name @ YYYY-MM-DD, several separated by semicolons."},
	{Name: "Metrics", Meaning: "Name | unit | up or down | baseline | target | YYYY-MM-DD, several separated by semicolons."},
	{Name: "Parents", Meaning: "Titles of Goals in the same file that this Goal contributes to, separated by semicolons."},
}

// WriteTemplate writes the import's CSV template: just the header row, the
// import format's own columns but the ID, in order. Uploaded unchanged it
// imports no rows (#676).
func WriteTemplate(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(goalColumns); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}
	return nil
}
