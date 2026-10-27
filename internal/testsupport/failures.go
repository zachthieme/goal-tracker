package testsupport

import "strings"

// This file holds the only SQLite failure syntax tests use: a test that needs
// a write or a read to fail names the table and the kind of write here,
// rather than writing its own trigger or rename.

// Write is a kind of write to a table that FailWrites makes fail.
type Write string

// The writes FailWrites makes fail, as SQLite names a trigger's event.
const (
	Insert Write = "INSERT"
	Update Write = "UPDATE"
	Delete Write = "DELETE"
)

// FailWrites makes every write of the given kind to table fail, from now on,
// with an error containing "injected failure", for a test of what a command
// does when its write fails. Each kind of write to each table has a trigger
// of its own, so calls for different tables or writes all hold.
func (h *Harness) FailWrites(write Write, table string) {
	h.T.Helper()
	trigger := "fail_" + strings.ToLower(string(write)) + "_" + table
	if _, err := h.DB.ExecContext(h.T.Context(), `CREATE TRIGGER `+trigger+` BEFORE `+string(write)+` ON `+table+`
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		h.T.Fatalf("fail %s on %s: %v", write, table, err)
	}
}

// FailReads makes every read of table, and every write to it, fail from now
// on with SQLite's "no such table: <table>" error, for a test of what a page
// does when a read fails. It renames the table to one of its own, so calls
// for different tables all hold.
func (h *Harness) FailReads(table string) {
	h.T.Helper()
	if _, err := h.DB.ExecContext(h.T.Context(), `ALTER TABLE `+table+` RENAME TO gone_`+table); err != nil {
		h.T.Fatalf("fail reads of %s: %v", table, err)
	}
}
