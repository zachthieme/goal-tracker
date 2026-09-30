// Package testsupport is the shared test harness every layer's tests reuse: a
// fresh SQLite database per test with the real migrations applied, a
// controllable clock, a recording fake email sender, and scenario builders for
// arranging Accounts and Goals.
package testsupport

import (
	"context"
	"database/sql"
	"path/filepath"
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
// Service wired over them.
type Harness struct {
	T       *testing.T
	DB      *sql.DB
	Clock   *clock.Fixed
	Email   *email.Recorder
	Service *domain.Service
}

// New returns a Harness backed by a fresh on-disk SQLite database with the real
// migrations applied. adminEmails receive the Admin flag on first sign-in.
func New(t *testing.T, adminEmails ...string) *Harness {
	t.Helper()

	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	clk := clock.NewFixed(Epoch)
	rec := email.NewRecorder()
	return &Harness{
		T:       t,
		DB:      sqlDB,
		Clock:   clk,
		Email:   rec,
		Service: domain.NewService(sqlDB, clk, rec, adminEmails),
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
