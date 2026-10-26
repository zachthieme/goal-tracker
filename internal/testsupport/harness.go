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
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
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
		builder, err := templateBuilderSource()
		if err != nil {
			templateErr = err
			return
		}
		info, ok := debug.ReadBuildInfo()
		modules := make([]string, len(templateModules))
		for i, module := range templateModules {
			modules[i] = templateModuleVersion(info, ok, module)
		}
		name, err := templateCacheName(db.Migrations(), builder, modules)
		if err != nil {
			templateErr = err
			return
		}
		templateContents, templateErr = cachedTemplate(templateDir(), name, migrateTemplate)
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

// templateBuilderFiles are the Go source files, by path from the module root,
// whose code builds the template: they apply the migrations and open the
// database they are applied to.
var templateBuilderFiles = []string{
	"internal/db/migrate.go",
	"internal/db/open.go",
	"internal/testsupport/template.go",
}

// templateBuilderSource reads each of templateBuilderFiles from the checkout
// this harness was compiled from, found through the harness's own path, as
// the Go source isn't embedded in any binary.
func templateBuilderSource() (map[string][]byte, error) {
	_, harness, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("find the harness's source to read the template's builder")
	}
	return templateBuilderSourceFrom(harness)
}

// templateBuilderSourceFrom reads each of templateBuilderFiles from the
// module root two directories above harness, the path of this file. Built
// with -trimpath, that path is the module's import path rather than one on
// disk, so the harness can't find its source and says why.
func templateBuilderSourceFrom(harness string) (map[string][]byte, error) {
	if !filepath.IsAbs(harness) {
		return nil, fmt.Errorf("the test harness reads its source from disk to name the template database, and -trimpath hides where it is (its path is %s): run the tests without -trimpath", harness)
	}
	root := filepath.Join(filepath.Dir(harness), "..", "..")
	source := make(map[string][]byte, len(templateBuilderFiles))
	for _, path := range templateBuilderFiles {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read the template's builder %s: %w", path, err)
		}
		source[path] = body
	}
	return source, nil
}

// templateModules are the modules, by path, whose code the migrations run on:
// modernc.org/sqlite and the C library it is translated onto.
var templateModules = []string{"modernc.org/sqlite", "modernc.org/libc"}

// templateModuleVersion is the version and checksum of module in the build
// info (debug.ReadBuildInfo's results), or "unknown" when the build info, or
// the module in it, isn't there. A module replaced by a local directory has
// neither, so only the directory's path is named and the cache can't see
// edits made there: a person editing one clears $TMPDIR/goal-tracker-testdb
// (templateDir) by hand, so the next run builds the template afresh.
func templateModuleVersion(info *debug.BuildInfo, ok bool, module string) string {
	if !ok || info == nil {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path != module {
			continue
		}
		if dep.Replace != nil {
			dep = dep.Replace
		}
		return dep.Path + "@" + dep.Version + " " + dep.Sum
	}
	return "unknown"
}

// templateCacheName names the template built from migrations (migrations/*.sql
// in it) by builder (Go source by path) on modules (templateModuleVersion's
// results), after a hash of all three, so a changed, added or removed
// migration, an edit to the code that applies them, or another SQLite or libc
// names a new template rather than reusing one built without it.
func templateCacheName(migrations fs.FS, builder map[string][]byte, modules []string) (string, error) {
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return "", fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(paths)
	sum := sha256.New()
	for _, module := range modules {
		_, _ = fmt.Fprintf(sum, "module\x00%d\x00%s\x00", len(module), module)
	}
	for _, path := range slices.Sorted(maps.Keys(builder)) {
		body := builder[path]
		_, _ = fmt.Fprintf(sum, "\x00builder\x00%s\x00%d\x00", path, len(body))
		_, _ = sum.Write(body)
	}
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

// cachedTemplate returns the contents of the template called name (see
// templateCacheName), reading it from dir and marking it used if a binary has
// already built it there, and otherwise building it with build, which writes
// a migrated database to the path it is given. A template is built in a directory of its own and renamed into place,
// so a binary reading it while another builds it never reads half a file, and
// two that build it at once each rename a whole one. Building one deletes the
// other templates in dir that have gone unused for templateUnusedFor.
func cachedTemplate(dir, name string, build func(path string) error) ([]byte, error) {
	path := filepath.Join(dir, name)
	if contents, err := os.ReadFile(path); err == nil {
		// Marked as used, so building another template keeps it (see
		// removeOtherTemplates). A template gone since it was read is
		// simply built again, so a failure is ignored.
		now := time.Now()
		_ = os.Chtimes(path, now, now)
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
	removeOtherTemplates(dir, name, time.Now().Add(-templateUnusedFor))
	return contents, nil
}

// templateUnusedFor is how long a template goes unread and unwritten before
// building another may delete it: long enough that a checkout running its
// tests alongside one with other migrations keeps its own template, as each
// read refreshes it.
const templateUnusedFor = 24 * time.Hour

// removeOtherTemplates deletes every template in dir but the one called name
// that was last modified before cutoff. One another binary is still reading is
// gone only once it closes it, and one another binary looks for after it is
// gone is built again, so failures are ignored.
func removeOtherTemplates(dir, name string, cutoff time.Time) {
	others, _ := filepath.Glob(filepath.Join(dir, "template-*.db"))
	for _, other := range others {
		if filepath.Base(other) == name {
			continue
		}
		if info, err := os.Stat(other); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(other)
		}
	}
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
