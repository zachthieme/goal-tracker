package db_test

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

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
