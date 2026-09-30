// Command seed fills a fresh database with a fake but realistic org — about 50
// Goals across six teams, with weeks of Check-in history — so the prototype can
// be demoed without real data (ticket #23). It works through the spreadsheet
// import and the domain commands, never by writing to the database directly,
// and it is deterministic: the same -seed and -end build the same org.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/seed"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	dbPath := fs.String("db", envOr("GOAL_TRACKER_DB", "goal-tracker.db"), "SQLite database to seed; it must have no Goals yet")
	seedValue := fs.Uint64("seed", seed.DefaultSeed, "random seed; the same seed builds the same org")
	adminEmail := fs.String("admin", "admin@example.com", "email of the Admin who defines the Team Dimension and runs the import")
	endFlag := fs.String("end", "", "last day of the Check-in history, YYYY-MM-DD (default: now)")
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

	// Skipping the fsync on every commit makes seeding weeks of Check-ins take
	// seconds instead of minutes; a seed that is cut short is simply rerun.
	sqlDB, err := sql.Open("sqlite", "file:"+*dbPath+"?_pragma=synchronous(off)")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	clk := clock.NewFixed(end)
	svc := domain.NewService(sqlDB, clk, email.LogSender{Logger: logger}, []string{*adminEmail})
	sum, err := seed.Run(context.Background(), svc, clk, seed.Options{Seed: *seedValue, Admin: *adminEmail})
	if err != nil {
		return err
	}
	logger.Info("seeded a fake org",
		"db", *dbPath, "goals", sum.Goals, "checkins", sum.Checkins,
		"admin", *adminEmail, "seed", *seedValue, "end", end.Format(time.DateOnly))
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
