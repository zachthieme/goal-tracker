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
	"runtime"
	"strings"
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
		queries: db.New(sqlDB),
		clock:   clk,
		email:   sender,
		admins:  admins,
		loc:     time.UTC,
		hashes:  newHashLimiter(runtime.GOMAXPROCS(0)),
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
// errors) by returning a sentinel error and recognising it on the way out.
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
	txSvc := &Service{
		db:      s.db,
		queries: s.queries.WithTx(sqlTx),
		clock:   s.clock,
		email:   s.email,
		admins:  s.admins,
		loc:     s.loc,
		baseURL: s.baseURL,
		hashes:  s.hashes,
		inTx:    true,
	}
	if err := fn(txSvc); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
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
