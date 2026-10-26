// Package domain is the service boundary for Goal Tracker. Handlers and any
// other caller go through Service and speak in the domain vocabulary defined in
// CONTEXT.md (Goal, Why it matters, Owner, Lifecycle); the SQL, the clock, and the
// email sender all sit behind it (ADR 0004: graph traversal stays behind this
// service so storage can change without touching callers).
package domain

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Service is the domain boundary. Construct it with NewService.
type Service struct {
	db      *sql.DB
	queries *db.Queries
	clock   clock.Clock
	email   email.Sender
	admins  map[string]bool
	// loc is the company's timezone, the calendar company-wide dates are read
	// in. A Goal's own dates are read in it too when its Owner has no Timezone
	// or it is Ownerless (see GoalTimezone).
	loc *time.Location
	// baseURL is where people reach the web app, for the links in the emails
	// the Service sends itself (a comment alert). Empty leaves links relative.
	baseURL string
	// hashes is the limit every argon2id hash runs under: at most one per CPU
	// at once, so a burst of sign-ins, each taking argonMemory, can't exhaust
	// the server's memory.
	hashes *hashLimiter
	// inTx says the Service is bound to a transaction WithinTx opened, so a
	// nested WithinTx joins it rather than opening another.
	inTx bool
	// goalImport is the Admin's spreadsheet import the Service runs commands
	// for, or nil (see ForImportBy).
	goalImport *goalImport
	// outbox sends the email commands queue once their transactions commit.
	outbox *emailOutbox
	// queuedEmail, in a Service bound to a transaction, says whether the
	// transaction has queued email, for the outbox to send once it commits;
	// it is shared by every copy of the Service bound to it. Nil outside one.
	queuedEmail *bool
	// dbFile is the database file Ping first found on disk, for later checks
	// to tell the path no longer names it; shared by every copy.
	dbFile *databaseFile
}

// Option configures a Service at construction.
type Option func(*Service)

// WithTimezone sets the company's timezone, used for company-wide dates, such
// as a Report's baseline, and for a Goal whose Owner has no Timezone or which is
// Ownerless (CONTEXT.md: Timezone; see GoalTimezone). Without it the company
// runs on UTC.
func WithTimezone(loc *time.Location) Option {
	return func(s *Service) { s.loc = loc }
}

// WithBaseURL sets where people reach the web app (e.g.
// http://localhost:8080), so links in the emails the Service sends point there.
func WithBaseURL(url string) Option {
	return func(s *Service) { s.baseURL = strings.TrimRight(url, "/") }
}

// NewService builds a Service over sqlDB. adminEmails are the addresses that
// receive the Admin flag when their account is first created on sign-in,
// matched whatever their case (CONTEXT.md: Account).
func NewService(sqlDB *sql.DB, clk clock.Clock, sender email.Sender, adminEmails []string, opts ...Option) *Service {
	admins := make(map[string]bool, len(adminEmails))
	for _, e := range adminEmails {
		admins[adminKey(e)] = true
	}
	s := &Service{
		db:      sqlDB,
		queries: db.New(writeOutsideTxGuard{db: sqlDB}),
		clock:   clk,
		email:   sender,
		admins:  admins,
		loc:     time.UTC,
		hashes:  newHashLimiter(runtime.GOMAXPROCS(0)),
		outbox:  &emailOutbox{wake: make(chan struct{}, 1)},
		dbFile:  &databaseFile{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithinTx runs fn against a Service bound to a single database transaction, so a
// multi-step command is all-or-nothing (ticket #22: a spreadsheet import commits
// in one transaction). The transaction commits when fn returns nil and rolls
// back when it returns an error, which WithinTx then returns unchanged — so a
// caller can force a rollback (e.g. a dry run, or an import that found row
// errors) by returning a sentinel error and recognising it on the way out. It
// rolls back too when fn panics, and lets the panic carry on up (#504).
// Called on a Service already bound to a transaction, it runs fn within that
// one, so a command and the commands it is built from commit together.
func (s *Service) WithinTx(ctx context.Context, fn func(tx *Service) error) error {
	if s.inTx {
		return fn(s)
	}
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rolled back however fn ends, a panic included, so the transaction never
	// outlives WithinTx holding the write lock: on a background context nothing
	// else would end it. After a commit it is a no-op (sql.ErrTxDone).
	defer func() { _ = sqlTx.Rollback() }()
	txSvc := &Service{
		db:          s.db,
		queries:     s.queries.WithTx(sqlTx),
		clock:       s.clock,
		email:       s.email,
		admins:      s.admins,
		loc:         s.loc,
		baseURL:     s.baseURL,
		hashes:      s.hashes,
		inTx:        true,
		goalImport:  s.goalImport,
		outbox:      s.outbox,
		queuedEmail: new(bool),
		dbFile:      s.dbFile,
	}
	if err := fn(txSvc); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	if *txSvc.queuedEmail {
		s.emailQueued(ctx)
	}
	return nil
}

// Now returns the current time from the Service's clock, so callers that need
// "today" (such as seeding a date picker) share the same clock the tests
// control.
func (s *Service) Now() time.Time {
	return s.clock.Now()
}

// Timezone returns the company's timezone, the calendar company-wide dates are
// read in and the pages show dates and times in. A Goal's own dates are judged
// in it only when its Owner has no Timezone or it is Ownerless; see
// GoalTimezone.
func (s *Service) Timezone() *time.Location {
	return s.loc
}

// Ping checks the database answers within ctx, for a health check. It reads
// schema_migrations rather than running a bare ping, which SQLite answers
// without touching the file: a missing, replaced or unreadable database file
// fails the read on a fresh connection, even where SQLite has opened an empty
// one in its place. A pooled connection keeps reading the file it opened after
// that file is deleted or replaced, so Ping also checks the path still names
// the file it found there on its first check, and fails once it is gone or is
// another file.
func (s *Service) Ping(ctx context.Context) error {
	var path string
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT file FROM pragma_database_list WHERE name = 'main'), count(*) FROM schema_migrations`).Scan(&path, &n); err != nil {
		return err
	}
	if path == "" { // an in-memory database has no file
		return nil
	}
	return s.dbFile.check(path)
}

// databaseFile is the database file Ping first found at the database's path.
type databaseFile struct {
	mu   sync.Mutex
	info os.FileInfo
}

// check fails when path names no file, or a file other than the one the first
// check found there, which it records.
func (f *databaseFile) check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("database file: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info == nil {
		f.info = info
		return nil
	}
	if !os.SameFile(f.info, info) {
		return fmt.Errorf("database file %s was replaced since the first health check", path)
	}
	return nil
}
