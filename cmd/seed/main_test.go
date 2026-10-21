package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Seeding opens the database the way the app does (ADR 0009): at the path it
// was given, even one a URL would read differently, in WAL mode, and leaving
// no row that breaks a foreign key.
func TestSeedOpensTheDatabaseTheWayTheAppDoes(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "demo #1")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "goal-tracker.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(logger, []string{"-db", dbPath, "-end", "2026-10-01"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("seeded database isn't at %q: %v", dbPath, err)
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	var mode string
	if err := sqlDB.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	rows, err := sqlDB.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Error("foreign_key_check found a row that breaks a foreign key")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
}
