// Command goal-tracker serves the Goal Tracker web app: stdlib net/http
// routing, templ pages plus htmx, and SQLite through the pure-Go modernc
// driver, all behind the domain service.
package main

import (
	"context"
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
	"github.com/zachthieme/goal-tracker/internal/notify"
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

	weekly, err := notify.ParseWeekly(cfg.reminderDay, cfg.reminderTime, loc)
	if err != nil {
		return fmt.Errorf("GOAL_TRACKER_REMINDER_DAY/GOAL_TRACKER_REMINDER_TIME: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", "file:"+cfg.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	sender := email.LogSender{Logger: logger}
	svc := domain.NewService(sqlDB, clock.Real{}, sender, cfg.adminEmails, domain.WithTimezone(loc))
	srv := web.NewServer(svc)

	notifier := notify.New(svc, sender, cfg.baseURL, loc)
	scheduler := notify.NewScheduler(clock.Real{}, weekly, notifier.SendWeekly)
	go scheduler.Run(context.Background(), time.Minute, logger)

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("goal-tracker listening", "addr", cfg.addr, "db", cfg.dbPath, "timezone", loc.String(),
		"weekly_emails", fmt.Sprintf("%s %02d:%02d", weekly.Day, weekly.Hour, weekly.Minute))
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
	// reminderDay and reminderTime are when in the org's week the Check-in
	// reminders and parent digests go out.
	reminderDay  string
	reminderTime string
	// baseURL is where people reach the web app, for the links in emails.
	baseURL string
}

func loadConfig() config {
	cfg := config{
		addr:         envOr("GOAL_TRACKER_ADDR", ":8080"),
		dbPath:       envOr("GOAL_TRACKER_DB", "goal-tracker.db"),
		timezone:     envOr("GOAL_TRACKER_TIMEZONE", "UTC"),
		reminderDay:  envOr("GOAL_TRACKER_REMINDER_DAY", "Monday"),
		reminderTime: envOr("GOAL_TRACKER_REMINDER_TIME", "09:00"),
	}
	cfg.baseURL = envOr("GOAL_TRACKER_BASE_URL", defaultBaseURL(cfg.addr))
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

// defaultBaseURL is the web app's address on this machine when it listens on
// addr, e.g. http://localhost:8080 for ":8080".
func defaultBaseURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
