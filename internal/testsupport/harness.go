// Package testsupport is the shared test harness every layer's tests reuse: a
// SQLite database per test, copied from one the real migrations were applied to
// once per test binary, a controllable clock, a recording fake email sender, and scenario builders for
// arranging Accounts and Goals.
package testsupport

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/clock"
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
// of the template the real migrations were applied to (MigratedDatabaseFile).
// adminEmails receive the Admin flag on first sign-in.
func New(t *testing.T, adminEmails ...string) *Harness {
	t.Helper()

	path := MigratedDatabaseFile(t)
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

// MigratedDatabaseFile copies the migrated template database into a file of
// t's own and returns its path, for a test that opens the database file itself
// rather than through a Harness. The copy is in WAL mode, as the template is.
func MigratedDatabaseFile(t *testing.T) string {
	t.Helper()
	tmpl, err := template()
	if err != nil {
		t.Fatalf("template database: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, tmpl, 0o600); err != nil {
		t.Fatalf("copy template database: %v", err)
	}
	return path
}

// The migrated database every Harness copies, loaded on first use, or the
// error loading it failed with.
var (
	templateOnce     sync.Once
	templateContents []byte
	templateErr      error
)

// template returns the contents of the migrated template database, built
// once per test binary in a directory of its own that is removed once the
// file is read.
func template() ([]byte, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "goal-tracker-template-")
		if err != nil {
			templateErr = err
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()
		path := filepath.Join(dir, "template.db")
		if err := migrateTemplate(path); err != nil {
			templateErr = err
			return
		}
		templateContents, templateErr = os.ReadFile(path)
	})
	return templateContents, templateErr
}

// DeliverEmail sends the email in the outbox that is due through the
// harness's recorder, failing the test on error. The harness's own Service
// sends what it queues as each command commits; call it after a command run
// through another Service, which leaves its email queued.
func (h *Harness) DeliverEmail() {
	h.T.Helper()
	if err := h.Service.DeliverEmail(h.T.Context()); err != nil {
		h.T.Fatalf("DeliverEmail: %v", err)
	}
}

// SignIn signs the email in through the domain service, failing the test on
// error. It is a scenario builder: use it to arrange the Owner a Goal needs.
func (h *Harness) SignIn(emailAddr string) domain.Account {
	h.T.Helper()
	acc, err := h.Service.SignIn(h.T.Context(), emailAddr)
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
	if err := h.Service.SetName(h.T.Context(), acc.ID, name); err != nil {
		h.T.Fatalf("SetName(%q): %v", emailAddr, err)
	}
	acc.Name = name
	return acc
}

// WriteLockHeld reports whether a transaction holds the database's write lock,
// by trying to take it from a connection of its own that won't wait for it. A
// test's email sender calls it, under the context it sends with, to prove no
// email is sent while a transaction is open.
func (h *Harness) WriteLockHeld(ctx context.Context) bool {
	h.T.Helper()
	probe, err := sql.Open("sqlite", "file:"+(&url.URL{Path: h.path}).EscapedPath()+"?_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		h.T.Fatalf("open the write-lock probe: %v", err)
	}
	defer func() { _ = probe.Close() }()
	tx, err := probe.BeginTx(ctx, nil)
	if err != nil {
		if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
			return true
		}
		h.T.Fatalf("take the write lock: %v", err)
	}
	_ = tx.Rollback()
	return false
}
