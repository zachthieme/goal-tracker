package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/seed"
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
	if err := run(t.Context(), logger, []string{"-db", dbPath, "-end", "2026-10-01"}); err != nil {
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

// -manifest writes the e2e manifest, judged at -manifest-at, as JSON a
// seed.Manifest decodes; without it, the seed writes none.
func TestSeedWritesTheManifestOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manifestPath := filepath.Join(dir, "default.manifest.json")
	args := []string{"-db", filepath.Join(dir, "with.db"), "-end", "2026-10-05",
		"-manifest", manifestPath, "-manifest-at", "2026-10-05T18:05:00Z"}
	if err := run(t.Context(), logger, args); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m seed.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if m.QuietGoal == nil || m.QuietGoal.ID == 0 || m.QuietGoal.Email == "" {
		t.Errorf("manifest quietGoal = %+v, want a Goal and its Owner", m.QuietGoal)
	}

	bare := filepath.Join(dir, "bare")
	if err := os.Mkdir(bare, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), logger, []string{"-db", filepath.Join(bare, "without.db"), "-end", "2026-10-05"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	entries, err := os.ReadDir(bare)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "manifest") || strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("seeding without -manifest wrote %s", e.Name())
		}
	}
}
