// Command seed fills a fresh database with a fake but realistic company — about 50
// Goals across six teams, with weeks of Check-in history — so the prototype can
// be demoed without real data (ticket #23). It works through the spreadsheet
// import and the domain commands, never by writing to the database directly,
// and it is deterministic: the same -seed and -end build the same company.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/seed"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), logger, os.Args[1:]); err != nil {
		logger.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("GOAL_TRACKER_DB", "goal-tracker.db"), "SQLite database to seed; it must have no Goals yet")
	seedValue := fs.Uint64("seed", seed.DefaultSeed, "random seed; the same seed builds the same company")
	adminEmail := fs.String("admin", "admin@example.com", "email of the Admin who defines the Team Dimension and runs the import")
	endFlag := fs.String("end", "", "last day of the Check-in history, YYYY-MM-DD (default: now)")
	manifestPath := fs.String("manifest", "", "write the e2e manifest, the specs' Goals and people by role, as JSON to this file (default: none)")
	manifestAt := fs.String("manifest-at", "", "the instant the manifest's picks are judged at, RFC 3339 (default: the -end instant)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	end := clock.Real{}.Now()
	if *endFlag != "" {
		d, err := time.Parse(time.DateOnly, *endFlag)
		if err != nil {
			return fmt.Errorf("bad -end %q: use YYYY-MM-DD", *endFlag)
		}
		// The history's last Check-ins land that afternoon.
		end = d.Add(18 * time.Hour)
	}
	at := end
	if *manifestAt != "" {
		t, err := time.Parse(time.RFC3339, *manifestAt)
		if err != nil {
			return fmt.Errorf("bad -manifest-at %q: use RFC 3339, e.g. 2026-10-05T18:05:00Z", *manifestAt)
		}
		at = t
	}

	// Skipping the fsync on every commit makes seeding weeks of Check-ins take
	// seconds instead of minutes; a seed that is cut short is simply rerun.
	sqlDB, err := db.Open(*dbPath, db.ForBulkLoad())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := db.Migrate(ctx, sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	clk := clock.NewFixed(end)
	svc := domain.NewService(sqlDB, clk, email.LogSender{Logger: logger}, []string{*adminEmail})
	sum, err := seed.Run(ctx, svc, clk, seed.Options{Seed: *seedValue, Admin: *adminEmail})
	if err != nil {
		return err
	}
	logger.Info("seeded a fake company",
		"db", *dbPath, "goals", sum.Goals, "checkins", sum.Checkins,
		"admin", *adminEmail, "seed", *seedValue, "end", end.Format(time.DateOnly))

	if *manifestPath == "" {
		return nil
	}
	// The picks are judged where the e2e app's clock starts, not at -end.
	clk.Set(at)
	return writeManifest(ctx, svc, at, *manifestPath)
}

// writeManifest builds the e2e manifest over svc as of at, and writes it to
// path as indented JSON.
func writeManifest(ctx context.Context, svc *domain.Service, at time.Time, path string) error {
	m, err := seed.BuildManifest(ctx, svc, at)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
