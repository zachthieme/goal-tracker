package importer_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// downloadCSV downloads goals as the CSV the table offers, failing the test on
// error.
func downloadCSV(t *testing.T, h *testsupport.Harness, goals ...domain.Goal) string {
	t.Helper()
	var buf bytes.Buffer
	if err := importer.New(h.Service).Download(context.Background(), &buf, goals); err != nil {
		t.Fatalf("Download: %v", err)
	}
	return buf.String()
}

// The download writes the import format's columns behind a leading ID, one row
// per Goal given, with one column per live Dimension and Field: several values
// separated by semicolons and the Owner as an email. A Retired Dimension or
// Field has no column (#81).
func TestDownloadWritesTheImportFormatWithAnIDColumn(t *testing.T) {
	t.Parallel()

	const csv = `Title,Owner,So What,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar,Theme,Budget,Notes
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,,,ARR | USD | up | 1000000 | 2000000 | 2026-12-31,,Growth,Trust; Speed,1250000,
Ship checkout v2,eng@example.com,Checkout is slow.,Dated,2026-06-30,Beta @ 2026-05-01; GA @ 2026-06-15,,Grow revenue,Reliability,,,"Two
lines"
`
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")
	h.CreateSeveralValuesDimension(admin, "Theme", "Trust", "Speed")
	old := h.CreateDimension(admin, "Old", "Gone")
	h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.CreateField(admin, "Notes", domain.FieldLongText, "")
	stale := h.CreateField(admin, "Stale", domain.FieldShortText, "")

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(csv))
	if err != nil || !rep.Committed {
		t.Fatalf("Commit = %+v, %v", rep, err)
	}
	if err := h.Service.RetireDimension(context.Background(), admin.ID, old.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireField(context.Background(), admin.ID, stale.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	parent, err := h.Service.ViewGoal(context.Background(), rep.Rows[0].GoalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	child, err := h.Service.ViewGoal(context.Background(), rep.Rows[1].GoalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}

	got := downloadCSV(t, h, child, parent)
	want := "ID,Title,Owner,So What,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar,Theme,Budget,Notes\n" +
		strconv.FormatInt(child.ID, 10) + ",Ship checkout v2,eng@example.com,Checkout is slow.,Dated,2026-06-30,Beta @ 2026-05-01; GA @ 2026-06-15,,Grow revenue,Reliability,,,\"Two\nlines\"\n" +
		strconv.FormatInt(parent.ID, 10) + ",Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,,,ARR | USD | up | 1000000 | 2000000 | 2026-12-31,,Growth,Trust; Speed,1250000,\n"
	if got != want {
		t.Errorf("download =\n%s\nwant\n%s", got, want)
	}
}

// roundTripCSV is a spreadsheet whose Goals carry a value in a one-value, a
// several-values and an Extendable several-values Dimension, a number and a long-text Field,
// Milestones, a Metric and a parent.
const roundTripCSV = `Title,Owner,So What,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar,Theme,Customer,Budget,Notes
Grow revenue,ceo@example.com,Revenue is flat.,Ongoing,,,ARR | USD | up | 1000000 | 2000000 | 2026-12-31,,Growth,Trust; Speed,Acme,1250000,
Ship checkout v2,eng@example.com,Checkout is slow.,Dated,2026-06-30,Beta @ 2026-05-01; GA @ 2026-06-15,,Grow revenue,Reliability,,,,"Two
lines"
`

// arrangeRoundTrip defines roundTripCSV's Dimensions and Fields, commits it,
// and returns the two Goals it created: the parent, then the child.
func arrangeRoundTrip(t *testing.T, h *testsupport.Harness, admin domain.Account) (domain.Goal, domain.Goal) {
	t.Helper()
	h.CreateDimension(admin, "Pillar", "Growth", "Reliability")
	h.CreateSeveralValuesDimension(admin, "Theme", "Trust", "Speed")
	customer := h.CreateExtendableDimension(admin, "Customer", "Acme")
	h.SetDimensionSelection(admin, customer, domain.SelectionSeveral)
	h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.CreateField(admin, "Notes", domain.FieldLongText, "")
	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "goals.csv", []byte(roundTripCSV))
	if err != nil || !rep.Committed {
		t.Fatalf("Commit = %+v, %v", rep, err)
	}
	var goals []domain.Goal
	for _, row := range rep.Rows {
		g, err := h.Service.ViewGoal(context.Background(), row.GoalID)
		if err != nil {
			t.Fatalf("ViewGoal: %v", err)
		}
		goals = append(goals, g)
	}
	return goals[0], goals[1]
}

// historyLen is how many changes a Goal's Value history holds.
func historyLen(t *testing.T, h *testsupport.Harness, g domain.Goal) int {
	t.Helper()
	changes, err := h.Service.ValueHistory(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ValueHistory: %v", err)
	}
	return len(changes)
}

// Importing a download unchanged updates each Goal it names to what it already
// is: no Goal is created, nothing changes, and no history is written (#81).
func TestReimportingAnUnchangedDownloadChangesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	parent, child := arrangeRoundTrip(t, h, admin)
	before := downloadCSV(t, h, parent, child)
	parentHistory, childHistory := historyLen(t, h, parent), historyLen(t, h, child)

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "download.csv", []byte(before))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !rep.Committed || rep.HasErrors() {
		t.Fatalf("re-import not committed cleanly: %+v", rep)
	}
	for i, want := range []domain.Goal{parent, child} {
		if row := rep.Rows[i]; row.GoalID != want.ID || !row.Updated {
			t.Errorf("row %d = %+v, want an update of Goal %d", i, row, want.ID)
		}
	}
	if len(rep.Ignored) != 0 {
		t.Errorf("unchanged re-import reported ignored columns %v", rep.Ignored)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 2 {
		t.Errorf("re-import left %d Goals, want 2", len(goals))
	}
	if got := historyLen(t, h, parent); got != parentHistory {
		t.Errorf("parent history grew from %d to %d", parentHistory, got)
	}
	if got := historyLen(t, h, child); got != childHistory {
		t.Errorf("child history grew from %d to %d", childHistory, got)
	}
	if after := downloadCSV(t, h, parent, child); after != before {
		t.Errorf("download after re-import =\n%s\nwant\n%s", after, before)
	}
}

// Changing a Field cell and a Dimension cell in a download and importing it
// updates that Goal, clearing a value whose cell was emptied, and records each
// change in its history; the other Goal is untouched (#81).
func TestReimportingAChangedDownloadUpdatesTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	parent, child := arrangeRoundTrip(t, h, admin)
	csv := downloadCSV(t, h, parent, child)
	childHistory := historyLen(t, h, child)

	// The parent moves from Growth to Reliability, drops Speed, gains a new
	// Customer, and has its Budget changed.
	edited := strings.Replace(csv, ",Acme,Growth,Trust; Speed,1250000,", ",Acme; Newco,reliability,Trust,99,", 1)
	if edited == csv {
		t.Fatalf("the edit matched nothing in:\n%s", csv)
	}
	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "download.csv", []byte(edited))
	if err != nil || !rep.Committed {
		t.Fatalf("Commit = %+v, %v", rep, err)
	}

	if got := goalValueNames(t, h, parent); !slices.Equal(got, []string{"Acme", "Newco", "Reliability", "Trust"}) {
		t.Errorf("parent values = %v, want Acme, Newco, Reliability, Trust", got)
	}
	fields, err := h.Service.GoalFields(context.Background(), parent.ID)
	if err != nil || len(fields) != 1 || fields[0].Value != "99" {
		t.Errorf("parent Fields = %+v, %v; want Budget 99", fields, err)
	}
	changes, err := h.Service.ValueHistory(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("ValueHistory: %v", err)
	}
	var recorded []string
	for _, c := range changes {
		if c.Actor.ID == admin.ID && c.CreatedAt.Equal(changes[len(changes)-1].CreatedAt) {
			recorded = append(recorded, c.Attribute+": "+c.Before+" → "+c.After)
		}
	}
	for _, want := range []string{"Budget: 1250000 → 99", "Customer:  → Newco", "Pillar: Growth → Reliability", "Theme: Speed → "} {
		if !slices.Contains(recorded, want) {
			t.Errorf("history %v lacks %q", recorded, want)
		}
	}
	if got := historyLen(t, h, child); got != childHistory {
		t.Errorf("child history grew from %d to %d", childHistory, got)
	}

	// Emptying the Budget cell clears it.
	cleared := strings.Replace(edited, ",reliability,Trust,99,", ",reliability,Trust,,", 1)
	if rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "download.csv", []byte(cleared)); err != nil || !rep.Committed {
		t.Fatalf("Commit = %+v, %v", rep, err)
	}
	if fields, _ := h.Service.GoalFields(context.Background(), parent.ID); len(fields) != 0 {
		t.Errorf("parent Fields = %+v after emptying the cell, want none", fields)
	}
}

// A row whose ID matches no Goal is a row error, and since the whole file is
// validated first nothing is written: not another row's update, nor a new
// Goal, nor a new value in an Extendable list (#81).
func TestAnUnknownIDFailsTheFileAndWritesNothing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	parent, child := arrangeRoundTrip(t, h, admin)
	csv := downloadCSV(t, h, parent, child)
	edited := strings.Replace(csv, ",Acme,Growth,", ",Acme; Newco,Reliability,", 1) +
		"9999,Ghost,ghost@example.com,Gone.,Ongoing,,,,,,,,,\n" +
		",Brand new,new@example.com,It matters.,Ongoing,,,,,,,,,\n"
	parentHistory := historyLen(t, h, parent)

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "download.csv", []byte(edited))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if rep.Committed || rep.ErrorCount() != 1 {
		t.Fatalf("report = %+v, want one row error and nothing committed", rep)
	}
	if errs := rep.Rows[2].Errors; len(errs) != 1 || !strings.Contains(errs[0], "9999") {
		t.Errorf("unknown ID row errors = %v, want one naming ID 9999", errs)
	}
	if got := goalValueNames(t, h, parent); !slices.Equal(got, []string{"Acme", "Growth", "Speed", "Trust"}) {
		t.Errorf("parent values = %v, want them unchanged", got)
	}
	if got := historyLen(t, h, parent); got != parentHistory {
		t.Errorf("parent history grew from %d to %d", parentHistory, got)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 2 {
		t.Errorf("failed import left %d Goals, want 2", len(goals))
	}
	if got := dimensionValueNames(t, h, "Customer"); !slices.Equal(got, []string{"Acme"}) {
		t.Errorf("Customer list = %v, want just Acme", got)
	}
}

// A row with an ID updates only its Goal's Dimension values and Fields: a
// changed Title or So What on it is ignored, and the report names each such
// column once for the whole file (#81).
func TestChangedGoalColumnsOnARowWithAnIDAreIgnoredAndReported(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	parent, child := arrangeRoundTrip(t, h, admin)
	csv := downloadCSV(t, h, parent, child)
	edited := strings.Replace(csv, "Grow revenue,ceo@example.com,Revenue is flat.,", "Grow profit,ceo@example.com,Margins are thin.,", 1)
	edited = strings.Replace(edited, "Ship checkout v2,", "Ship checkout v3,", 1)

	rep, err := importer.New(h.Service).Commit(context.Background(), admin.ID, "download.csv", []byte(edited))
	if err != nil || !rep.Committed {
		t.Fatalf("Commit = %+v, %v", rep, err)
	}
	if !slices.Equal(rep.Ignored, []string{"Title", "So What"}) {
		t.Errorf("Ignored = %v, want [Title So What]", rep.Ignored)
	}
	for _, g := range []domain.Goal{parent, child} {
		got, err := h.Service.ViewGoal(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("ViewGoal: %v", err)
		}
		if got.Title != g.Title || got.SoWhat != g.SoWhat {
			t.Errorf("Goal %d = %q / %q, want it unchanged as %q / %q", g.ID, got.Title, got.SoWhat, g.Title, g.SoWhat)
		}
	}
}

// A download that would write a value containing a semicolon, named so before
// that was refused, is refused naming its Dimension and value, and writes
// nothing: the file could not be imported again, since the semicolon would
// split the value (#101).
func TestDownloadRefusesAValueContainingASemicolon(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth", "Reliability")
	rnd := h.NameValueWithSemicolon(pillar.Values[1], "R&D; Ops")
	alpha := h.CreateGoal(admin, "Alpha", "A matters.")
	bravo := h.CreateGoal(admin, "Bravo", "B matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	h.AssignGoalValue(bravo, rnd)

	var buf bytes.Buffer
	err := importer.New(h.Service).Download(context.Background(), &buf, []domain.Goal{alpha, bravo})
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), `Pillar value "R&D; Ops"`) || !strings.Contains(err.Error(), "semicolon") {
		t.Errorf("Download err = %v, want a refusal naming Pillar's value R&D; Ops and the semicolon", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Download wrote %q, want nothing", buf.String())
	}

	// Goals not carrying it still download.
	if got := downloadCSV(t, h, alpha); !strings.Contains(got, "Alpha") {
		t.Errorf("download of Alpha = %q, want its row", got)
	}
}
