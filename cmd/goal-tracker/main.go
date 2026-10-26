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
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the org's timezone resolves without the host's zoneinfo

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/jobs"
	"github.com/zachthieme/goal-tracker/internal/notify"
	"github.com/zachthieme/goal-tracker/internal/pdf"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// main serves the app until SIGINT or SIGTERM asks it to stop. With the
// arguments password-link <email>, it prints a set-password link for that
// email and exits; with healthcheck, it checks the app serving here is healthy
// and exits non-zero if it isn't.
func main() {
	// Read before anything logs, healthcheck included.
	logger, err := newLogger(os.Stderr, os.Getenv("GOAL_TRACKER_LOG_LEVEL"), os.Getenv("GOAL_TRACKER_LOG_FORMAT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "goal-tracker failed:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, logger, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		logger.Error("goal-tracker failed", "err", err)
		os.Exit(1)
	}
}

// passwordLinkCommand is the argument that, followed by an email, has run
// print a set-password link for it instead of serving: how the operator gives
// the first Admin a password.
const passwordLinkCommand = "password-link"

// healthcheckCommand is the argument that has run check the app's /healthz
// instead of serving: the image's HEALTHCHECK, which has no curl to do it.
const healthcheckCommand = "healthcheck"

// run serves the app, configured from the environment, until ctx is done,
// and, with GOAL_TRACKER_METRICS_ADDR set, the app's request metrics on a
// second listener there (see newMetricsServer). It then shuts both down (see
// serve), stops the email outbox, the weekly emails and
// the directory sync, and closes the database. With args password-link <email>, it instead issues
// a set-password link for the email, creating the Account if there is none,
// writes it to stdout, and returns. With args healthcheck, it reads only
// GOAL_TRACKER_ADDR and returns checkHealth's verdict on the app serving
// there. Any other args are ignored.
func run(ctx context.Context, logger *slog.Logger, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == healthcheckCommand {
		return checkHealth(ctx, addrFromEnv(), healthcheckTimeout)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	var linkFor string
	if len(args) > 0 && args[0] == passwordLinkCommand {
		if cfg.signIn != signInPassword {
			return fmt.Errorf("%s needs GOAL_TRACKER_SIGNIN=%s; got %q", passwordLinkCommand, signInPassword, cfg.signIn)
		}
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: goal-tracker %s <email>", passwordLinkCommand)
		}
		linkFor = args[1]
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
	defer func() {
		_ = sqlDB.Close()
		logger.Debug("database closed")
	}()
	// The email outbox, the prune, the weekly emails and the directory sync
	// run until ctx is done, or run returns, and the database closes only once
	// they have stopped.
	background := jobs.New(clk, logger)
	defer background.Stop()

	if err := db.Migrate(ctx, sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	sender := email.LogSender{Logger: logger}
	svc := domain.NewService(sqlDB, clk, sender, cfg.adminEmails, domain.WithTimezone(loc), domain.WithBaseURL(cfg.baseURL), domain.WithEmailOutboxLogger(logger))
	if linkFor != "" {
		link, err := svc.IssueOperatorPasswordLink(ctx, linkFor)
		if err != nil {
			return fmt.Errorf("issue a set-password link for %s: %w", linkFor, err)
		}
		logger.Info("set-password link issued", "issuer", "command-line",
			"account_id", link.Account.ID, "email", link.Account.Email, "expires", link.Expires)
		_, err = fmt.Fprintln(stdout, svc.PasswordLinkURL(link.Token))
		return err
	}
	webOpts := []web.Option{
		web.WithLogger(logger),
		web.WithSecureCookies(strings.HasPrefix(cfg.baseURL, "https://")),
	}
	if cfg.signIn == signInDev {
		logger.Warn("GOAL_TRACKER_SIGNIN=dev: anyone who can reach the app can sign in as anyone. For development and tests only.")
	}
	if cfg.sessionKey == nil { // only with GOAL_TRACKER_SIGNIN=dev; loadConfig requires a key otherwise
		logger.Warn("GOAL_TRACKER_SESSION_KEY is unset: signing sessions with a random key, so they won't survive a restart")
		cfg.sessionKey = make([]byte, 32)
		_, _ = rand.Read(cfg.sessionKey)
	}
	webOpts = append(webOpts, web.WithSessionKey(cfg.sessionKey))
	if len(cfg.trustedProxies) > 0 {
		webOpts = append(webOpts, web.WithTrustedProxies(cfg.trustedProxies))
		logger.Info("the password sign-in throttle counts a request from a trusted proxy against the client its X-Forwarded-For names", "trusted_proxies", fmt.Sprint(cfg.trustedProxies))
	}
	if cfg.signIn == signInPassword {
		webOpts = append(webOpts, web.WithPasswordSignIn())
		logger.Info("signing in with passwords")
	}
	if cfg.oidc != nil {
		o, err := web.NewOIDC(ctx, *cfg.oidc)
		if err != nil {
			return err
		}
		webOpts = append(webOpts, web.WithOIDC(o))
		logger.Info("signing in through the organization's OIDC provider", "issuer", cfg.oidc.Issuer, "redirect_url", cfg.oidc.RedirectURL)
	}
	switch {
	case cfg.directory == nil:
		logger.Info("neither GOAL_TRACKER_DIRECTORY_FILE nor GOAL_TRACKER_DIRECTORY_URL is set: no directory sync, so no one has a Manager")
	case cfg.directory.file != "":
		webOpts = append(webOpts, startDirectorySync(background, svc, directory.NewFile(cfg.directory.file), logger))
		logger.Info("syncing people and their Managers from the directory file, now and hourly", "file", cfg.directory.file)
	default:
		webOpts = append(webOpts, startDirectorySync(background, svc, directory.NewAuthentik(cfg.directory.url, cfg.directory.token), logger))
		logger.Info("syncing people and their Managers from the org's directory, now and hourly", "url", cfg.directory.url)
	}
	var metrics *web.RequestMetrics
	if cfg.metricsAddr != "" {
		metrics = web.NewRequestMetrics()
		webOpts = append(webOpts, web.WithRequestMetrics(metrics))
	}
	pdfOpt, err := pdfDownloads(cfg.chrome, logger)
	if err != nil {
		return err
	}
	webOpts = append(webOpts, pdfOpt)
	srv := web.NewServer(svc, webOpts...)

	// The email commands queue, such as comment alerts, is sent once each
	// command commits, and retried while it fails.
	background.Add(jobs.Job{Name: "email outbox", Every: emailOutboxInterval, Wake: svc.EmailWake(), Run: svc.DeliverEmail})
	// Sent and given-up email, and expired Undo tokens, are deleted as they
	// go stale.
	background.Add(jobs.Job{Name: "prune", Every: pruneInterval, Run: func(ctx context.Context) error {
		return svc.PruneStaleAndLog(ctx, logger)
	}})
	// The weekly emails are queued in the outbox for each week's slot. A slot
	// missed while the app was down is sent once, late, as it starts.
	notifier := notify.New(svc)
	scheduler := notify.NewScheduler(svc, notify.WeeklyJob, weekly, notifier.SendWeekly)
	background.Add(jobs.Job{Name: notify.WeeklyJob, Every: time.Minute, Run: scheduler.Tick})
	background.Start(ctx)

	// Both listeners are open before either serves, so one that can't listen
	// stops startup.
	var lc net.ListenConfig
	var metricsLn net.Listener
	if metrics != nil {
		metricsLn, err = lc.Listen(ctx, "tcp", cfg.metricsAddr)
		if err != nil {
			return fmt.Errorf("GOAL_TRACKER_METRICS_ADDR: %w", err)
		}
		defer func() { _ = metricsLn.Close() }()
	}
	ln, err := lc.Listen(ctx, "tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	if metricsLn != nil {
		logger.Info("goal-tracker serving metrics", "addr", metricsLn.Addr().String(), "path", "/metrics")
	}
	logger.Info("goal-tracker listening", "addr", ln.Addr().String(), "db", cfg.dbPath, "timezone", loc.String(),
		"weekly_emails", fmt.Sprintf("%s %02d:%02d", weekly.Day, weekly.Hour, weekly.Minute))

	// The metrics listener serves until the app's does, and the app's until
	// the metrics listener stops on its own.
	serveCtx, stopServing := context.WithCancel(ctx)
	defer stopServing()
	metricsServed := make(chan error, 1)
	if metricsLn != nil {
		go func() {
			err := serve(serveCtx, newMetricsServer(cfg.metricsAddr, metrics), metricsLn, shutdownTimeout)
			if err != nil {
				err = fmt.Errorf("metrics: %w", err)
			}
			stopServing()
			metricsServed <- err
		}()
	} else {
		metricsServed <- nil
	}
	err = serve(serveCtx, newHTTPServer(cfg.addr, srv), ln, shutdownTimeout)
	stopServing()
	if err := errors.Join(err, <-metricsServed); err != nil {
		return err
	}
	logger.Info("goal-tracker stopped serving: every request in flight finished")
	return nil
}

// emailOutboxInterval is how often the email outbox looks for email due to be
// tried again; email a command queues is sent as soon as it commits.
const emailOutboxInterval = 15 * time.Second

// pruneInterval is how often stale rows are pruned (Service.PruneStale), as
// well as once at startup.
const pruneInterval = time.Hour

// shutdownTimeout is how long, asked to stop, the app gives the requests in
// flight to finish.
const shutdownTimeout = 30 * time.Second

// serve serves s on ln until ctx is done, then shuts s down: it stops taking
// connections and waits up to grace for the requests in flight to finish. It
// returns nil once they have, an error when they haven't within grace, after
// cutting them off, or why s stopped serving on its own.
func serve(ctx context.Context, s *http.Server, ln net.Listener, grace time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- s.Serve(ln) }()
	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		_ = s.Close()
		return fmt.Errorf("shut down: requests still in flight after %v: %w", grace, err)
	}
	return nil
}

// How long the HTTP server gives a request to arrive, headers and then
// whole, and to be answered, and how long it keeps an idle connection open.
const (
	readHeaderTimeout = 10 * time.Second
	// readTimeout is time enough for an import's upload, up to 5 MiB, on a
	// slow connection.
	readTimeout = time.Minute
	// writeTimeout runs from when the request's headers arrive. It is generous
	// so a PDF download, which may wait for a render slot and then render,
	// within web.PDFRenderTimeout together, still has time to be sent.
	writeTimeout = web.PDFRenderTimeout + time.Minute
	idleTimeout  = 2 * time.Minute
)

// newHTTPServer is the HTTP server for handler at addr, with timeouts that
// keep a slow or stalled client from holding a connection open.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// The log formats GOAL_TRACKER_LOG_FORMAT accepts: slog's key=value text
// lines, or one JSON object a line, for a log collector to parse.
const (
	logFormatText = "text"
	logFormatJSON = "json"
)

var logFormats = []string{logFormatText, logFormatJSON}

// logLevels are the levels GOAL_TRACKER_LOG_LEVEL accepts, each the least
// severe record logged.
var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// newLogger is the app's logger, writing to w at level
// (GOAL_TRACKER_LOG_LEVEL) in format (GOAL_TRACKER_LOG_FORMAT). Each is read
// whatever its case, with the space around it trimmed, and defaults, unset,
// to info and text. Any other value is an error naming its variable and the
// values it takes.
func newLogger(w io.Writer, level, format string) (*slog.Logger, error) {
	lvl, ok := slog.LevelInfo, true
	if level = strings.TrimSpace(level); level != "" {
		lvl, ok = logLevels[strings.ToLower(level)]
	}
	if !ok {
		return nil, fmt.Errorf("GOAL_TRACKER_LOG_LEVEL must be one of debug, info, warn, error; got %q", level)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	format = strings.TrimSpace(format)
	switch strings.ToLower(format) {
	case "", logFormatText:
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case logFormatJSON:
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	}
	return nil, fmt.Errorf("GOAL_TRACKER_LOG_FORMAT must be one of %s; got %q", strings.Join(logFormats, ", "), format)
}

// newMetricsServer is the HTTP server for GOAL_TRACKER_METRICS_ADDR, addr: it
// answers GET /metrics with what metrics has recorded, in the Prometheus text
// format, and anything else 404, with the app's own server's timeouts.
func newMetricsServer(addr string, metrics *web.RequestMetrics) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics)
	return newHTTPServer(addr, mux)
}

// healthcheckTimeout is how long the healthcheck command waits for /healthz
// to answer.
const healthcheckTimeout = 3 * time.Second

// checkHealth GETs /healthz from the app listening on addr, through the
// loopback whatever host addr names, and returns nil on a 200. Any other
// status, or no answer within timeout, is an error saying which.
func checkHealth(ctx context.Context, addr string, timeout time.Duration) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s: GOAL_TRACKER_ADDR: %w", healthcheckCommand, err)
	}
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", healthcheckCommand, err)
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", healthcheckCommand, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: GET %s: %s", healthcheckCommand, url, resp.Status)
	}
	return nil
}

// startDirectorySync has background sync from dir as it starts and hourly,
// and gives the web app the sync for the Admin page's Sync now.
func startDirectorySync(background *jobs.Runner, svc *domain.Service, dir domain.Directory, logger *slog.Logger) web.Option {
	dirSync := directory.NewSync(svc, dir, logger)
	background.Add(jobs.Job{Name: "directory sync", Every: time.Hour, Run: func(ctx context.Context) error {
		return dirSync.Run(ctx).Err
	}})
	return web.WithDirectorySync(dirSync)
}

// pdfDownloads is how a publication downloads as a PDF: rendered by the
// Chromium at chrome, or, with none, as its print page, printed from the
// browser. It logs which. With a Chromium, it checks TMPDIR, as
// pdf.CheckTMPDIR does: a warning it logs, and an error it returns, as
// Chromium couldn't render at all.
func pdfDownloads(chrome string, logger *slog.Logger) (web.Option, error) {
	if chrome == "" {
		logger.Info("no Chromium in GOAL_TRACKER_CHROME or on PATH (chromium, chromium-browser, google-chrome, google-chrome-stable): PDF downloads fall back to the print page, to print from the browser")
		return web.WithPDFRenderer(nil), nil
	}
	warning, err := pdf.CheckTMPDIR()
	if err != nil {
		return nil, fmt.Errorf("rendering PDF downloads with %s: %w", chrome, err)
	}
	if warning != "" {
		logger.Warn(warning)
	}
	logger.Info("rendering PDF downloads with Chromium", "chrome", chrome)
	return web.WithPDFRenderer(pdf.Chromium{Path: chrome}), nil
}

type config struct {
	addr        string
	dbPath      string
	adminEmails []string
	// timezone is the org's IANA timezone, the calendar Check-in cadences are
	// counted in. "Local" is refused: it is the host's zone, not a name a
	// Report block can freeze and read back.
	timezone string
	// reminderDay and reminderTime are when in the org's week the Check-in
	// reminders and parent digests go out.
	reminderDay  string
	reminderTime string
	// baseURL is where people reach the web app, for the links in emails:
	// GOAL_TRACKER_BASE_URL, required except with the development form, which
	// falls back to this machine's address.
	baseURL string
	// startAt, when set, is the RFC 3339 instant the app's clock starts at
	// instead of the wall clock. For testing only: the e2e suite runs against
	// a seed frozen at one date.
	startAt string
	// signIn is how people sign in, from GOAL_TRACKER_SIGNIN: one of
	// signInMethods.
	signIn string
	// oidc, with signIn "oidc", is the org's OpenID Connect provider people
	// sign in through; nil with "dev", which serves the development email form,
	// and with "password".
	oidc *web.OIDCConfig
	// sessionKey signs session cookies; nil when GOAL_TRACKER_SESSION_KEY is
	// unset, which only "dev" allows.
	sessionKey []byte
	// directory, when set, is the org's directory Managers are synced from
	// (ADR 0008); nil leaves no one with a Manager.
	directory *directoryConfig
	// chrome is the Chromium that renders PDF downloads: GOAL_TRACKER_CHROME,
	// or else one found on PATH; "" when there's none, and a PDF download is
	// the print page.
	chrome string
	// trustedProxies are the reverse proxies, from
	// GOAL_TRACKER_TRUSTED_PROXIES, whose X-Forwarded-For names the client the
	// password sign-in throttle counts a request against; none when it's
	// unset, and the throttle counts each against the address it came from.
	trustedProxies []netip.Prefix
	// metricsAddr, from GOAL_TRACKER_METRICS_ADDR, is where a second listener
	// serves GET /metrics; "" when it's unset, and no metrics are recorded or
	// served.
	metricsAddr string
}

// The sign-in methods GOAL_TRACKER_SIGNIN accepts: the org's OpenID Connect
// provider, passwords set through Admin-issued set-password links, or the
// development email form, which signs anyone in as anyone.
const (
	signInOIDC     = "oidc"
	signInPassword = "password"
	signInDev      = "dev"
)

var signInMethods = []string{signInOIDC, signInPassword, signInDev}

// directoryConfig is where the directory sync reads people from: a CSV file,
// or Authentik's base URL and an API token that can read users.
type directoryConfig struct {
	// file, when set, is the CSV file's path, and url and token are empty.
	file  string
	url   string
	token string
}

func loadConfig() (config, error) {
	cfg := config{
		addr:         addrFromEnv(),
		dbPath:       envOr("GOAL_TRACKER_DB", "goal-tracker.db"),
		timezone:     envOr("GOAL_TRACKER_TIMEZONE", "UTC"),
		reminderDay:  envOr("GOAL_TRACKER_REMINDER_DAY", "Monday"),
		reminderTime: envOr("GOAL_TRACKER_REMINDER_TIME", "09:00"),
		startAt:      os.Getenv("GOAL_TRACKER_START_AT"),
		metricsAddr:  trimmedEnv("GOAL_TRACKER_METRICS_ADDR"),
	}
	if cfg.timezone == "Local" {
		return config{}, errors.New("GOAL_TRACKER_TIMEZONE=Local is the host's zone, not an IANA name: set the company's timezone by name, such as America/Los_Angeles")
	}
	for _, e := range strings.Split(os.Getenv("GOAL_TRACKER_ADMINS"), ",") {
		if e = strings.TrimSpace(e); e != "" {
			cfg.adminEmails = append(cfg.adminEmails, e)
		}
	}

	cfg.signIn = os.Getenv("GOAL_TRACKER_SIGNIN")
	if !slices.Contains(signInMethods, cfg.signIn) {
		return config{}, fmt.Errorf("GOAL_TRACKER_SIGNIN must be one of %s; got %q", strings.Join(signInMethods, ", "), cfg.signIn)
	}

	// Only the development form falls back to this machine's address: behind
	// a proxy, a localhost base URL would put dead links in emails and leave
	// session cookies without Secure.
	cfg.baseURL = trimmedEnv("GOAL_TRACKER_BASE_URL")
	if cfg.baseURL == "" {
		if cfg.signIn != signInDev {
			return config{}, fmt.Errorf("GOAL_TRACKER_SIGNIN=%s needs GOAL_TRACKER_BASE_URL: where people reach the app, such as https://goals.example.com", cfg.signIn)
		}
		cfg.baseURL = defaultBaseURL(cfg.addr)
	}

	oidc, err := oidcConfig(cfg.signIn, cfg.baseURL)
	if err != nil {
		return config{}, err
	}
	cfg.oidc = oidc

	cfg.directory, err = loadDirectoryConfig()
	if err != nil {
		return config{}, err
	}

	cfg.chrome, err = pdf.Find(trimmedEnv("GOAL_TRACKER_CHROME"))
	if err != nil {
		return config{}, fmt.Errorf("GOAL_TRACKER_CHROME: %w", err)
	}
	// A TMPDIR too long for the Chromium stops startup here, before the
	// database opens or the directory syncs, as a missing Chromium does;
	// pdfDownloads logs a warning.
	if cfg.chrome != "" {
		if _, err := pdf.CheckTMPDIR(); err != nil {
			return config{}, fmt.Errorf("rendering PDF downloads with %s: %w", cfg.chrome, err)
		}
	}

	cfg.trustedProxies, err = parseTrustedProxies(os.Getenv("GOAL_TRACKER_TRUSTED_PROXIES"))
	if err != nil {
		return config{}, fmt.Errorf("GOAL_TRACKER_TRUSTED_PROXIES: %w", err)
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
	if cfg.sessionKey == nil && cfg.signIn != signInDev {
		return config{}, fmt.Errorf("GOAL_TRACKER_SIGNIN=%s needs GOAL_TRACKER_SESSION_KEY: 32 or more random bytes in base64 (openssl rand -base64 32)", cfg.signIn)
	}
	return cfg, nil
}

// oidcConfig is the org's OpenID Connect provider from the GOAL_TRACKER_OIDC_*
// variables for the sign-in method: with "oidc", an error naming any that are
// missing; with any other method, nil, or an error naming those set, so a
// half-meant organization sign-in can't fall back to the open development
// form. The redirect URL is baseURL's /auth/callback. A variable of only
// whitespace counts as unset, and the values kept are trimmed.
func oidcConfig(signIn, baseURL string) (*web.OIDCConfig, error) {
	vars := []string{"GOAL_TRACKER_OIDC_ISSUER", "GOAL_TRACKER_OIDC_CLIENT_ID", "GOAL_TRACKER_OIDC_CLIENT_SECRET"}
	var values, set, missing []string
	for _, v := range vars {
		value := trimmedEnv(v)
		values = append(values, value)
		if value == "" {
			missing = append(missing, v)
		} else {
			set = append(set, v)
		}
	}
	if signIn != signInOIDC {
		if len(set) > 0 {
			return nil, fmt.Errorf("GOAL_TRACKER_SIGNIN=%s doesn't use the GOAL_TRACKER_OIDC_* variables; unset %s, or set GOAL_TRACKER_SIGNIN=oidc", signIn, strings.Join(set, ", "))
		}
		return nil, nil
	}
	if len(missing) > 0 {
		return nil, errors.New("organization sign-in needs all of the GOAL_TRACKER_OIDC_* variables; missing " + strings.Join(missing, ", "))
	}
	return &web.OIDCConfig{
		Issuer:       values[0],
		ClientID:     values[1],
		ClientSecret: values[2],
		RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/callback",
	}, nil
}

// loadDirectoryConfig is the directory sync's settings: the CSV file named by
// GOAL_TRACKER_DIRECTORY_FILE, or Authentik from GOAL_TRACKER_DIRECTORY_URL
// and GOAL_TRACKER_DIRECTORY_TOKEN. It is nil when none is set, an error naming
// both sources when the file is set with either Authentik variable, and an
// error naming the missing one when only one Authentik variable is. A variable
// of only whitespace counts as unset, and the values kept are trimmed.
func loadDirectoryConfig() (*directoryConfig, error) {
	vars := []string{"GOAL_TRACKER_DIRECTORY_URL", "GOAL_TRACKER_DIRECTORY_TOKEN"}
	var set, missing []string
	for _, v := range vars {
		if trimmedEnv(v) == "" {
			missing = append(missing, v)
		} else {
			set = append(set, v)
		}
	}
	if file := trimmedEnv("GOAL_TRACKER_DIRECTORY_FILE"); file != "" {
		if len(set) > 0 {
			return nil, errors.New("the directory sync reads one source: GOAL_TRACKER_DIRECTORY_FILE or Authentik; unset GOAL_TRACKER_DIRECTORY_FILE, or unset " + strings.Join(set, ", "))
		}
		return &directoryConfig{file: file}, nil
	}
	switch len(missing) {
	case len(vars):
		return nil, nil
	case 0:
		return &directoryConfig{url: trimmedEnv(vars[0]), token: trimmedEnv(vars[1])}, nil
	}
	return nil, errors.New("the directory sync needs both GOAL_TRACKER_DIRECTORY_* variables; missing " + strings.Join(missing, ", "))
}

// parseTrustedProxies is the comma-separated list of CIDRs and bare IPs in v,
// a bare IP being a range of just that address, an IPv4-mapped IPv6 one its
// IPv4 address, and an IPv4-mapped CIDR (::ffff:a.b.c.d/n, n ≥ 96) the IPv4
// range a.b.c.d/(n-96). Blank entries are skipped, and the space around each
// is trimmed; an IPv4-mapped CIDR wider than /96, or any other entry that
// isn't a CIDR or an IP, is an error naming it.
func parseTrustedProxies(v string) ([]netip.Prefix, error) {
	var proxies []netip.Prefix
	for _, entry := range strings.Split(v, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if p, err := netip.ParsePrefix(entry); err == nil {
			// the address clientIP compares is unmapped, so a mapped range
			// is its IPv4 one
			if p.Addr().Is4In6() {
				if p.Bits() < 96 {
					return nil, fmt.Errorf("%q is an IPv4-mapped CIDR wider than ::ffff:0.0.0.0/96", entry)
				}
				p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
			}
			proxies = append(proxies, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("%q is neither a CIDR nor an IP", entry)
		}
		addr = addr.Unmap() // the address clientIP compares is unmapped
		proxies = append(proxies, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return proxies, nil
}

// trimmedEnv is the environment variable key without the whitespace around
// it, so one of only whitespace reads as unset.
func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// addrFromEnv is the address the app listens on: GOAL_TRACKER_ADDR, or :8080.
func addrFromEnv() string {
	return envOr("GOAL_TRACKER_ADDR", ":8080")
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
