// Command goal-tracker serves the Goal Tracker web app: stdlib net/http
// routing, templ pages plus htmx, and SQLite through the pure-Go modernc
// driver, all behind the domain service.
package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // the org's timezone resolves without the host's zoneinfo

	_ "modernc.org/sqlite" // pure-Go SQLite driver (ADR 0004)

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("goal-tracker failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg := loadConfig()
	loc, err := time.LoadLocation(cfg.timezone)
	if err != nil {
		return fmt.Errorf("GOAL_TRACKER_TIMEZONE: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", "file:"+cfg.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	svc := domain.NewService(sqlDB, clock.Real{}, email.LogSender{Logger: logger}, cfg.adminEmails, domain.WithTimezone(loc))
	srv := web.NewServer(svc)

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("goal-tracker listening", "addr", cfg.addr, "db", cfg.dbPath, "timezone", loc.String())
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

type config struct {
	addr        string
	dbPath      string
	adminEmails []string
	// timezone is the org's IANA timezone, the calendar Check-in cadences are
	// counted in.
	timezone string
}

func loadConfig() config {
	cfg := config{
		addr:     envOr("GOAL_TRACKER_ADDR", ":8080"),
		dbPath:   envOr("GOAL_TRACKER_DB", "goal-tracker.db"),
		timezone: envOr("GOAL_TRACKER_TIMEZONE", "UTC"),
	}
	for _, e := range strings.Split(os.Getenv("GOAL_TRACKER_ADMINS"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			cfg.adminEmails = append(cfg.adminEmails, e)
		}
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
