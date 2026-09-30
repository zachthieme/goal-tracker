// Package domain is the service boundary for Goal Tracker. Handlers and any
// other caller go through Service and speak in the domain vocabulary defined in
// CONTEXT.md (Goal, So What, Owner, Lifecycle); the SQL, the clock, and the
// email sender all sit behind it (ADR 0004: graph traversal stays behind this
// service so storage can change without touching callers).
package domain

import (
	"database/sql"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Service is the domain boundary. Construct it with NewService.
type Service struct {
	queries *db.Queries
	clock   clock.Clock
	email   email.Sender
	admins  map[string]bool
}

// NewService builds a Service over sqlDB. adminEmails are the addresses that
// receive the Admin flag when their account is first created on sign-in.
func NewService(sqlDB *sql.DB, clk clock.Clock, sender email.Sender, adminEmails []string) *Service {
	admins := make(map[string]bool, len(adminEmails))
	for _, e := range adminEmails {
		admins[e] = true
	}
	return &Service{
		queries: db.New(sqlDB),
		clock:   clk,
		email:   sender,
		admins:  admins,
	}
}

// Now returns the current time from the Service's clock, so callers that need
// "today" (such as seeding a date picker) share the same clock the tests
// control.
func (s *Service) Now() time.Time {
	return s.clock.Now()
}
