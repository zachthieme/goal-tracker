package testsupport_test

import (
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Failing inserts into one table fails each insert into it with the injected
// failure, and leaves inserts into another table working: signing in still
// inserts the Account.
func TestFailWritesFailsInsertsIntoOnlyThatTable(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)

	h.FailWrites(testsupport.Insert, "scheduled_runs")

	_, err := h.DB.Exec(`INSERT INTO scheduled_runs (job, slot) VALUES ('weekly', '2026-W01')`)
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("insert into scheduled_runs: err = %v, want the injected failure", err)
	}
	h.SignIn("owner@example.com")
}

// Failing updates leaves inserts into the table working, and a second call,
// failing its deletes, holds alongside the first.
func TestFailWritesFailsOnlyThatKindOfWrite(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)

	h.FailWrites(testsupport.Update, "scheduled_runs")
	h.FailWrites(testsupport.Delete, "scheduled_runs")

	if _, err := h.DB.Exec(`INSERT INTO scheduled_runs (job, slot) VALUES ('weekly', '2026-W01')`); err != nil {
		t.Fatalf("insert into scheduled_runs: %v", err)
	}
	for _, write := range []string{
		`UPDATE scheduled_runs SET slot = '2026-W02'`,
		`DELETE FROM scheduled_runs`,
	} {
		if _, err := h.DB.Exec(write); err == nil || !strings.Contains(err.Error(), "injected failure") {
			t.Errorf("%s: err = %v, want the injected failure", write, err)
		}
	}
}

// Failing reads of a table fails a SELECT from it with SQLite's missing-table
// error, and a second call, for another table, holds alongside the first.
func TestFailReadsFailsReadsOfThatTable(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)

	h.FailReads("milestones")
	h.FailReads("fields")

	for _, table := range []string{"milestones", "fields"} {
		_, err := h.DB.Exec(`SELECT * FROM ` + table)
		if err == nil || !strings.Contains(err.Error(), "no such table: "+table) {
			t.Errorf("select from %s: err = %v, want no such table: %s", table, err, table)
		}
	}
}
