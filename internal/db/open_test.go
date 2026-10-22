package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

func TestOpenSetsBusyTimeout(t *testing.T) {
	t.Parallel()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var ms int
	if err := sqlDB.QueryRow(`PRAGMA busy_timeout`).Scan(&ms); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if ms != 5000 {
		t.Errorf("busy_timeout = %d ms, want 5000", ms)
	}
}

// The database runs in WAL mode, so a read doesn't wait behind a write, and
// with synchronous NORMAL, which WAL keeps safe from corruption on power loss.
func TestOpenRunsInWALMode(t *testing.T) {
	t.Parallel()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var sync int
	if err := sqlDB.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatalf("read synchronous: %v", err)
	}
	if sync != 1 {
		t.Errorf("synchronous = %d, want 1 (NORMAL)", sync)
	}
}

// A bulk load skips the fsync on each commit and nothing else: foreign keys
// are still enforced and the database still runs in WAL mode.
func TestOpenForBulkLoadTurnsOnlySynchronousOff(t *testing.T) {
	t.Parallel()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"), db.ForBulkLoad())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var sync int
	if err := sqlDB.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatalf("read synchronous: %v", err)
	}
	if sync != 0 {
		t.Errorf("synchronous = %d, want 0 (OFF)", sync)
	}
	var foreignKeys int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}
	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var ms int
	if err := sqlDB.QueryRow(`PRAGMA busy_timeout`).Scan(&ms); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if ms != 5000 {
		t.Errorf("busy_timeout = %d ms, want 5000", ms)
	}
}

// The pool is capped: past the one writer and the readers WAL lets alongside
// it, more connections would only queue on the write lock.
func TestOpenCapsThePoolAtEightConnections(t *testing.T) {
	t.Parallel()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if got := sqlDB.Stats().MaxOpenConnections; got != 8 {
		t.Errorf("MaxOpenConnections = %d, want 8", got)
	}
}

// A Contributor can't be added to a Goal that doesn't exist: the schema's
// foreign keys are enforced on the connections Open hands out.
func TestOpenEnforcesForeignKeys(t *testing.T) {
	t.Parallel()

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO accounts (id, email, created_at) VALUES (1, 'owner@example.com', '2026-01-02T15:04:05Z')`); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	_, err = sqlDB.Exec(`INSERT INTO contributors (goal_id, account_id, created_at) VALUES (999, 1, '2026-01-02T15:04:05Z')`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("insert Contributor for missing Goal: err = %v, want a foreign-key error", err)
	}
}

// openTally opens a fresh database with Open and gives it a small table for
// concurrent work to read and write.
func openTally(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE tally (n INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return sqlDB
}

// concurrently runs work once from each of workers goroutines at the same time
// and fails the test with a count of every error they returned.
func concurrently(t *testing.T, workers int, work func(worker int) error) {
	t.Helper()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for w := range workers {
		wg.Go(func() {
			if err := work(w); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("%d of %d operations failed; first: %v", len(errs), workers, errs[0])
	}
}

func TestOpenLetsReadsAndWritesOverlap(t *testing.T) {
	t.Parallel()

	sqlDB := openTally(t)

	// 20 writers and 20 readers. Without the busy timeout nearly all fail.
	concurrently(t, 40, func(worker int) error {
		if worker%2 == 0 {
			_, err := sqlDB.Exec(`INSERT INTO tally (n) VALUES (?)`, worker)
			return err
		}
		var count int
		return sqlDB.QueryRow(`SELECT count(*) FROM tally`).Scan(&count)
	})
}

func TestOpenQueuesTransactionsThatReadThenWrite(t *testing.T) {
	t.Parallel()

	sqlDB := openTally(t)

	// Without immediate transactions most fail, even with the busy timeout.
	concurrently(t, 20, func(int) error {
		tx, err := sqlDB.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var count int
		if err := tx.QueryRow(`SELECT count(*) FROM tally`).Scan(&count); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO tally (n) VALUES (?)`, count); err != nil {
			return err
		}
		return tx.Commit()
	})
}

// A read doesn't wait behind a long-running write. The write is bigger than
// SQLite's page cache, so it spills to the database file mid-transaction:
// without WAL that takes the exclusive lock, and every reader waits out the
// busy timeout behind it.
func TestOpenLetsAReadRunWhileAWriteIsHeld(t *testing.T) {
	t.Parallel()

	sqlDB := openTally(t)
	ctx := context.Background()
	if _, err := sqlDB.Exec(`INSERT INTO tally (n) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	writer, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("writer connection: %v", err)
	}
	defer func() { _ = writer.Close() }()
	reader, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("reader connection: %v", err)
	}
	defer func() { _ = reader.Close() }()

	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin write: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE bulk (b BLOB NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := tx.Exec(`WITH RECURSIVE mb(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM mb WHERE i < 4) INSERT INTO bulk (b) SELECT zeroblob(1 << 20) FROM mb`); err != nil {
		t.Fatalf("write past the page cache: %v", err)
	}

	start := time.Now()
	var count int
	if err := reader.QueryRowContext(ctx, `SELECT count(*) FROM tally`).Scan(&count); err != nil {
		t.Fatalf("read while the write is held: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("read took %v while a write was held, want under 100ms", elapsed)
	}
	if count != 1 {
		t.Errorf("read %d rows, want the 1 committed before the write", count)
	}
}

// DSN is the connection string Open opens with, so a caller that opens the
// database itself, as the test harness does, gets the same pragmas on the same
// file. The path holds a '#', which would end an unescaped URI's path.
func TestDSNOpensWhatOpenOpens(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts []db.OpenOption
	}{
		{"default", nil},
		{"ForBulkLoad", []db.OpenOption{db.ForBulkLoad()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "#app.db")
			viaOpen, err := db.Open(path, tc.opts...)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = viaOpen.Close() })
			viaDSN, err := sql.Open("sqlite", db.DSN(path, tc.opts...))
			if err != nil {
				t.Fatalf("open DSN: %v", err)
			}
			t.Cleanup(func() { _ = viaDSN.Close() })

			if _, err := viaDSN.Exec(`CREATE TABLE tally (n INTEGER NOT NULL)`); err != nil {
				t.Fatalf("create table through DSN: %v", err)
			}
			var tables int
			if err := viaOpen.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'tally'`).Scan(&tables); err != nil {
				t.Fatalf("read schema through Open: %v", err)
			}
			if tables != 1 {
				t.Errorf("Open sees %d tally tables, want the 1 made through DSN: they opened different files", tables)
			}

			for _, pragma := range []string{"busy_timeout", "foreign_keys", "journal_mode", "synchronous"} {
				var fromOpen, fromDSN string
				if err := viaOpen.QueryRow(`PRAGMA ` + pragma).Scan(&fromOpen); err != nil {
					t.Fatalf("read %s through Open: %v", pragma, err)
				}
				if err := viaDSN.QueryRow(`PRAGMA ` + pragma).Scan(&fromDSN); err != nil {
					t.Fatalf("read %s through DSN: %v", pragma, err)
				}
				if fromDSN != fromOpen {
					t.Errorf("%s = %q through DSN, want %q as through Open", pragma, fromDSN, fromOpen)
				}
			}
		})
	}
}
