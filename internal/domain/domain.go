// Package domain is the service boundary for Goal Tracker. Handlers and any
// other caller go through Service and speak in the domain vocabulary defined in
// CONTEXT.md (Goal, So What, Owner, Lifecycle); the SQL, the clock, and the
// email sender all sit behind it (ADR 0004: graph traversal stays behind this
// service so storage can change without touching callers).
package domain

import (
	"context"
	"database/sql"
	"fmt"
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
	// loc is the org's timezone: the calendar a Goal's cadence is counted in.
	loc *time.Location
	// baseURL is where people reach the web app, for the links in the emails
	// the Service sends itself (a comment alert). Empty leaves links relative.
	baseURL string
}

// Option configures a Service at construction.
type Option func(*Service)

// WithTimezone sets the org's timezone, the calendar a Goal's Check-in cadence
// is counted in (CONTEXT.md: Stale). Without it the org runs on UTC.
func WithTimezone(loc *time.Location) Option {
	return func(s *Service) { s.loc = loc }
}

// WithBaseURL sets where people reach the web app (e.g.
// http://localhost:8080), so links in the emails the Service sends point there.
func WithBaseURL(url string) Option {
	return func(s *Service) { s.baseURL = strings.TrimRight(url, "/") }
}

// NewService builds a Service over sqlDB. adminEmails are the addresses that
// receive the Admin flag when their account is first created on sign-in.
func NewService(sqlDB *sql.DB, clk clock.Clock, sender email.Sender, adminEmails []string, opts ...Option) *Service {
	admins := make(map[string]bool, len(adminEmails))
	for _, e := range adminEmails {
		admins[e] = true
	}
	s := &Service{
		db:      sqlDB,
		queries: db.New(sqlDB),
		clock:   clk,
		email:   sender,
		admins:  admins,
		loc:     time.UTC,
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
func (s *Service) WithinTx(ctx context.Context, fn func(tx *Service) error) error {
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
