// Package testsupport is the shared test harness every layer's tests reuse: a
// SQLite database per test, copied from one the real migrations were applied to
// once per test binary, a controllable clock, a recording fake email sender, and scenario builders for
// arranging Accounts and Goals.
package testsupport

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Epoch is the instant the harness clock starts at, so timestamp assertions are
// deterministic.
var Epoch = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// Harness bundles a test's database, clock, email recorder, and the domain
// Service wired over them. The Service sends the email a command queues as the
// command's transaction commits, so a test asserts on h.Email as soon as the
// command returns.
type Harness struct {
	T       *testing.T
	DB      *sql.DB
	Clock   *clock.Fixed
	Email   *email.Recorder
	Service *domain.Service
	// path is the database's file, for WriteLockHeld's own connection.
	path string
}

// New returns a Harness backed by an on-disk SQLite database of its own, a copy
// of one the real migrations were applied to once per test binary. adminEmails
// receive the Admin flag on first sign-in.
func New(t *testing.T, adminEmails ...string) *Harness {
	t.Helper()

	tmpl, err := template()
	if err != nil {
		t.Fatalf("template database: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, tmpl, 0o600); err != nil {
		t.Fatalf("copy template database: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	clk := clock.NewFixed(Epoch)
	rec := email.NewRecorder()
	return &Harness{
		T:       t,
		DB:      sqlDB,
		Clock:   clk,
		Email:   rec,
		Service: domain.NewService(sqlDB, clk, rec, adminEmails, domain.WithEmailDeliveredOnCommit()),
		path:    path,
	}
}

// dsn opens the SQLite file at path. The database is thrown away with the
// test, so it skips fsync: waiting on the disk made up most of each test's
// time, and nearly ran a package's tests past go test's 10-minute timeout.
// Each test's database is copied from a template migrated once per test binary
// rather than migrated itself: running every migration for each of the
// hundreds of tests queued them all on modernc's process-wide allocator lock.
// The path is escaped because it's a URI: a test's temp directory can hold a
// '#' (an unnamed subtest is "#00"), which would otherwise end the path and
// open a new, empty database in place of the copy. Like the app's own
// db.Open, a statement that meets another connection's lock waits for it,
// transactions take the write lock at BEGIN, so a test running two requests at
// once sees them queue as they would in the app rather than fail with
// SQLITE_BUSY, and foreign keys are enforced, so a test that would orphan a row
// fails as the app would.
func dsn(path string) string {
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?_pragma=synchronous(off)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
}

// The migrated database every Harness copies, built on first use, or the error
// building it failed with.
var (
	templateOnce     sync.Once
	templateContents []byte
	templateErr      error
)

// template migrates a database once per test binary and returns its file's
// contents. Its temp directory is left for /tmp clearing, and never reused
// across runs, where its schema could be stale.
func template() ([]byte, error) {
	templateOnce.Do(func() { templateContents, templateErr = buildTemplate() })
	return templateContents, templateErr
}

func buildTemplate() ([]byte, error) {
	dir, err := os.MkdirTemp("", "goal-tracker-testdb-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "template.db")
	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Migrate(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Closed before reading, so the file is complete.
	if err := sqlDB.Close(); err != nil {
		return nil, fmt.Errorf("close: %w", err)
	}
	return os.ReadFile(path)
}

// DeliverEmail sends the email in the outbox that is due through the
// harness's recorder, failing the test on error. The harness's own Service
// sends what it queues as each command commits; call it after a command run
// through another Service, which leaves its email queued.
func (h *Harness) DeliverEmail() {
	h.T.Helper()
	if err := h.Service.DeliverEmail(context.Background()); err != nil {
		h.T.Fatalf("DeliverEmail: %v", err)
	}
}

// SignIn signs the email in through the domain service, failing the test on
// error. It is a scenario builder: use it to arrange the Owner a Goal needs.
func (h *Harness) SignIn(emailAddr string) domain.Account {
	h.T.Helper()
	acc, err := h.Service.SignIn(context.Background(), emailAddr)
	if err != nil {
		h.T.Fatalf("SignIn(%q): %v", emailAddr, err)
	}
	return acc
}

// SignInNamed signs the email in and gives the Account the Name the org's
// sign-in would supply (CONTEXT.md: Name), failing the test on error.
func (h *Harness) SignInNamed(emailAddr, name string) domain.Account {
	h.T.Helper()
	acc := h.SignIn(emailAddr)
	if err := h.Service.SetName(context.Background(), acc.ID, name); err != nil {
		h.T.Fatalf("SetName(%q): %v", emailAddr, err)
	}
	acc.Name = name
	return acc
}

// WriteLockHeld reports whether a transaction holds the database's write lock,
// by trying to take it from a connection of its own that won't wait for it. A
// test's email sender calls it to prove no email is sent while a transaction is
// open.
func (h *Harness) WriteLockHeld() bool {
	h.T.Helper()
	probe, err := sql.Open("sqlite", "file:"+(&url.URL{Path: h.path}).EscapedPath()+"?_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		h.T.Fatalf("open the write-lock probe: %v", err)
	}
	defer func() { _ = probe.Close() }()
	tx, err := probe.Begin()
	if err != nil {
		if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
			return true
		}
		h.T.Fatalf("take the write lock: %v", err)
	}
	_ = tx.Rollback()
	return false
}
