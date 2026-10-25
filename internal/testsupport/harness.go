// Package testsupport is the shared test harness every layer's tests reuse: a
// SQLite database per test, copied from one the real migrations were applied to
// once per test binary, a controllable clock, a recording fake email sender, and scenario builders for
// arranging Accounts and Goals.
package testsupport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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
// t's own and returns its path, for a test that opens the database itself, as
// internal/db's upgrade tests do to forget a migration and run it again. The
// copy is in WAL mode, as the template is.
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

// dsn opens the SQLite file at path with the app's own connection string, so
// a test's database sets the pragmas db.Open does (ADR 0009): a statement that
// meets another connection's lock waits for it, transactions take the write
// lock at BEGIN, foreign keys are enforced and the database runs in WAL mode,
// so tests see the app's queueing, foreign-key errors and reads alongside an
// open write. The database is thrown away with the test, so it is opened for a
// bulk load and skips fsync: waiting on the disk made up most of each test's
// time, and nearly ran a package's tests past go test's 10-minute timeout. The
// pool is left uncapped; db.Open's cap is for the app.
// Each test's database is copied from a template migrated once and cached
// (see templateDir) rather than migrated itself: running every migration for each
// of the hundreds of tests queued them all on modernc's process-wide allocator
// lock.
func dsn(path string) string {
	return db.DSN(path, db.ForBulkLoad())
}

// The migrated database every Harness copies, loaded on first use, or the
// error loading it failed with.
var (
	templateOnce     sync.Once
	templateContents []byte
	templateErr      error
)

// template returns the contents of the migrated template database, loaded once
// per test binary from the one cached in templateDir for the whole run, or
// built there by the first binary to need it.
func template() ([]byte, error) {
	templateOnce.Do(func() {
		templateContents, templateErr = cachedTemplate(templateDir(), db.Migrations(), migrateTemplate)
	})
	return templateContents, templateErr
}

// templateDir is where the template database is cached, shared by every test
// binary in a run and by later runs: under race detection, migrating it took
// most of a test binary's time, and each of the eight packages that use the
// harness built its own.
func templateDir() string {
	return filepath.Join(os.TempDir(), "goal-tracker-testdb")
}

// templateFormat is part of the template's cache name; change it when how
// migrateTemplate builds the template changes, so a template an older harness
// cached isn't reused.
const templateFormat = "1"

// templateCacheName names the template built from migrations (migrations/*.sql
// in it) after a hash of every migration's path and contents, so a changed,
// added or removed migration names a new template rather than reusing one
// built without it.
func templateCacheName(migrations fs.FS) (string, error) {
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return "", fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(paths)
	sum := sha256.New()
	_, _ = io.WriteString(sum, templateFormat)
	for _, path := range paths {
		body, err := fs.ReadFile(migrations, path)
		if err != nil {
			return "", fmt.Errorf("read migration %s: %w", path, err)
		}
		_, _ = fmt.Fprintf(sum, "\x00%s\x00%d\x00", path, len(body))
		_, _ = sum.Write(body)
	}
	return "template-" + hex.EncodeToString(sum.Sum(nil))[:16] + ".db", nil
}

// cachedTemplate returns the contents of the template built from migrations,
// reading it from dir if a binary has already built it there and otherwise
// building it with build, which writes a migrated database to the path it is
// given. A template is built in a directory of its own and renamed into place,
// so a binary reading it while another builds it never reads half a file, and
// two that build it at once each rename a whole one. Building one deletes the
// templates in dir that other migrations named.
func cachedTemplate(dir string, migrations fs.FS, build func(path string) error) ([]byte, error) {
	name, err := templateCacheName(migrations)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	if contents, err := os.ReadFile(path); err == nil {
		return contents, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(dir, "build-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()
	built := filepath.Join(work, "template.db")
	if err := build(built); err != nil {
		return nil, err
	}
	contents, err := os.ReadFile(built)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(built, path); err != nil {
		return nil, fmt.Errorf("cache the template: %w", err)
	}
	removeOtherTemplates(dir, name)
	return contents, nil
}

// removeOtherTemplates deletes every template in dir but the one called name.
// One another binary is still reading is gone only once it closes it, and one
// another binary looks for after it is gone is built again, so failures are
// ignored.
func removeOtherTemplates(dir, name string) {
	others, _ := filepath.Glob(filepath.Join(dir, "template-*.db"))
	for _, other := range others {
		if filepath.Base(other) != name {
			_ = os.Remove(other)
		}
	}
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
