package importer_test

import (
	"bytes"
	"context"
	"strconv"
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
// several-values and an Extendable Dimension, a number and a long-text Field,
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
	h.CreateExtendableDimension(admin, "Customer", "Acme")
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
