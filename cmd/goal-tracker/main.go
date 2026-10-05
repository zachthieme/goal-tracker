// Command goal-tracker serves the Goal Tracker web app: stdlib net/http
// routing, templ pages plus htmx, and SQLite through the pure-Go modernc
// driver, all behind the domain service.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // the org's timezone resolves without the host's zoneinfo

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/directory"
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
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(cfg.timezone)
	if err != nil {
		return fmt.Errorf("GOAL_TRACKER_TIMEZONE: %w", err)
	}

	weekly, err := notify.ParseWeekly(cfg.reminderDay, cfg.reminderTime, loc)
	if err != nil {
		return fmt.Errorf("GOAL_TRACKER_REMINDER_DAY/GOAL_TRACKER_REMINDER_TIME: %w", err)
	}

	clk, err := appClock(cfg.startAt)
	if err != nil {
		return err
	}
	if cfg.startAt != "" {
		logger.Warn("clock is offset, for testing only: now starts at GOAL_TRACKER_START_AT and ticks from there", "start_at", cfg.startAt)
	}

	sqlDB, err := db.Open(cfg.dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	sender := email.LogSender{Logger: logger}
	svc := domain.NewService(sqlDB, clk, sender, cfg.adminEmails, domain.WithTimezone(loc), domain.WithBaseURL(cfg.baseURL))
	webOpts := []web.Option{
		web.WithLogger(logger),
		web.WithSecureCookies(strings.HasPrefix(cfg.baseURL, "https://")),
	}
	if cfg.sessionKey == nil {
		logger.Warn("GOAL_TRACKER_SESSION_KEY is unset: signing sessions with a random key, so they won't survive a restart")
		cfg.sessionKey = make([]byte, 32)
		_, _ = rand.Read(cfg.sessionKey)
	}
	webOpts = append(webOpts, web.WithSessionKey(cfg.sessionKey))
	if cfg.oidc != nil {
		o, err := web.NewOIDC(context.Background(), *cfg.oidc)
		if err != nil {
			return err
		}
		webOpts = append(webOpts, web.WithOIDC(o))
		logger.Info("signing in through the organization's OIDC provider", "issuer", cfg.oidc.Issuer, "redirect_url", cfg.oidc.RedirectURL)
	}
	if cfg.directory != nil {
		sync := directory.NewSync(svc, directory.NewAuthentik(cfg.directory.url, cfg.directory.token), logger)
		go sync.RunEvery(context.Background(), time.Hour)
		webOpts = append(webOpts, web.WithDirectorySync(sync))
		logger.Info("syncing people and their Managers from the org's directory, now and hourly", "url", cfg.directory.url)
	} else {
		logger.Info("GOAL_TRACKER_DIRECTORY_URL is unset: no directory sync, so no one has a Manager")
	}
	srv := web.NewServer(svc, webOpts...)

	notifier := notify.New(svc, sender, cfg.baseURL, loc)
	scheduler := notify.NewScheduler(clk, weekly, notifier.SendWeekly)
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
	// startAt, when set, is the RFC 3339 instant the app's clock starts at
	// instead of the wall clock. For testing only: the e2e suite runs against
	// a seed frozen at one date.
	startAt string
	// oidc, when set, is the org's OpenID Connect provider people sign in
	// through; nil leaves the development email form.
	oidc *web.OIDCConfig
	// sessionKey signs session cookies; nil when GOAL_TRACKER_SESSION_KEY is
	// unset.
	sessionKey []byte
	// directory, when set, is the org's directory Managers are synced from
	// (ADR 0008); nil leaves no one with a Manager.
	directory *directoryConfig
}

// directoryConfig is where the directory sync reads people from: Authentik's
// base URL and an API token that can read users.
type directoryConfig struct {
	url   string
	token string
}

func loadConfig() (config, error) {
	cfg := config{
		addr:         envOr("GOAL_TRACKER_ADDR", ":8080"),
		dbPath:       envOr("GOAL_TRACKER_DB", "goal-tracker.db"),
		timezone:     envOr("GOAL_TRACKER_TIMEZONE", "UTC"),
		reminderDay:  envOr("GOAL_TRACKER_REMINDER_DAY", "Monday"),
		reminderTime: envOr("GOAL_TRACKER_REMINDER_TIME", "09:00"),
		startAt:      os.Getenv("GOAL_TRACKER_START_AT"),
	}
	cfg.baseURL = envOr("GOAL_TRACKER_BASE_URL", defaultBaseURL(cfg.addr))
	for _, e := range strings.Split(os.Getenv("GOAL_TRACKER_ADMINS"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			cfg.adminEmails = append(cfg.adminEmails, e)
		}
	}

	oidc, err := oidcConfig(cfg.baseURL)
	if err != nil {
		return config{}, err
	}
	cfg.oidc = oidc

	cfg.directory, err = loadDirectoryConfig()
	if err != nil {
		return config{}, err
	}

	if v := os.Getenv("GOAL_TRACKER_SESSION_KEY"); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return config{}, fmt.Errorf("GOAL_TRACKER_SESSION_KEY isn't base64: %w", err)
		}
		if len(key) < 32 {
			return config{}, fmt.Errorf("GOAL_TRACKER_SESSION_KEY is %d bytes, want at least 32", len(key))
		}
		cfg.sessionKey = key
	}
	return cfg, nil
}

// oidcConfig is the org's OpenID Connect provider from the GOAL_TRACKER_OIDC_*
// variables: nil when none is set, an error naming the missing ones when only
// some are. The redirect URL is baseURL's /auth/callback.
func oidcConfig(baseURL string) (*web.OIDCConfig, error) {
	vars := []string{"GOAL_TRACKER_OIDC_ISSUER", "GOAL_TRACKER_OIDC_CLIENT_ID", "GOAL_TRACKER_OIDC_CLIENT_SECRET"}
	var values, missing []string
	for _, v := range vars {
		values = append(values, os.Getenv(v))
		if os.Getenv(v) == "" {
			missing = append(missing, v)
		}
	}
	switch len(missing) {
	case len(vars):
		return nil, nil
	case 0:
		return &web.OIDCConfig{
			Issuer:       values[0],
			ClientID:     values[1],
			ClientSecret: values[2],
			RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/callback",
		}, nil
	}
	return nil, errors.New("organization sign-in needs all of the GOAL_TRACKER_OIDC_* variables; missing " + strings.Join(missing, ", "))
}

// loadDirectoryConfig is the directory sync's settings from
// GOAL_TRACKER_DIRECTORY_URL and GOAL_TRACKER_DIRECTORY_TOKEN: nil when neither
// is set, an error naming the missing one when only one is.
func loadDirectoryConfig() (*directoryConfig, error) {
	u, token := os.Getenv("GOAL_TRACKER_DIRECTORY_URL"), os.Getenv("GOAL_TRACKER_DIRECTORY_TOKEN")
	switch {
	case u == "" && token == "":
		return nil, nil
	case token == "":
		return nil, errors.New("the directory sync needs both GOAL_TRACKER_DIRECTORY_* variables; missing GOAL_TRACKER_DIRECTORY_TOKEN")
	case u == "":
		return nil, errors.New("the directory sync needs both GOAL_TRACKER_DIRECTORY_* variables; missing GOAL_TRACKER_DIRECTORY_URL")
	}
	return &directoryConfig{url: u, token: token}, nil
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

// appClock is the clock the app reads "now" from: the wall clock, unless
// startAt (GOAL_TRACKER_START_AT) names an RFC 3339 instant for it to start
// at instead.
func appClock(startAt string) (clock.Clock, error) {
	if startAt == "" {
		return clock.Real{}, nil
	}
	start, err := time.Parse(time.RFC3339, startAt)
	if err != nil {
		return nil, fmt.Errorf("GOAL_TRACKER_START_AT: %w", err)
	}
	return clock.NewOffset(start), nil
}
