package testsupport

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// This file holds the code that builds the template database each test's
// database is copied from (see template).

// dsn opens the SQLite file at path with the app's own connection string, so
// a test's database sets the pragmas db.Open does (ADR 0009): a statement that
// meets another connection's lock waits for it, transactions take the write
// lock at BEGIN, foreign keys are enforced and the database runs in WAL mode,
// so tests see the app's queueing, foreign-key errors and reads alongside an
// open write. The database is thrown away with the test, so it is opened for a
// bulk load and skips fsync: waiting on the disk made up most of each test's
// time, and nearly ran a package's tests past go test's 10-minute timeout. The
// pool is left uncapped; db.Open's cap is for the app.
// Each test's database is copied from a template migrated once per test
// binary (see template) rather than migrated itself: running every migration
// for each of the hundreds of tests queued them all on modernc's process-wide
// allocator lock.
func dsn(path string) string {
	return db.DSN(path, db.ForBulkLoad())
}

// migrateTemplate migrates a new database at path.
func migrateTemplate(path string) error {
	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	// Built once, for every test, so no one test's context is the caller's.
	if err := db.Migrate(context.Background(), sqlDB); err != nil {
		_ = sqlDB.Close()
		return fmt.Errorf("migrate: %w", err)
	}
	// Closed before it is read, so the file is complete: closing the last
	// connection checkpoints the WAL into the database file and deletes the
	// -wal and -shm files beside it. The copy keeps WAL mode, which SQLite
	// records in the file itself.
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}
