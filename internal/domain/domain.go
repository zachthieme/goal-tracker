// Package domain is the service boundary for Goal Tracker. Handlers and any
// other caller go through Service and speak in the domain vocabulary defined in
// CONTEXT.md (Goal, So What, Owner, Lifecycle); the SQL, the clock, and the
// email sender all sit behind it (ADR 0004: graph traversal stays behind this
// service so storage can change without touching callers).
package domain

import (
	"database/sql"
	"errors"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Lifecycle values a Goal can be in (CONTEXT.md: Lifecycle). Only Proposed is
// reachable in the walking skeleton.
const (
	LifecycleProposed = "Proposed"
)

// ErrValidation is returned when a command's input is not acceptable, e.g. a
// Goal created without a title or a So What.
var ErrValidation = errors.New("validation failed")

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// Account is a person who can sign in and own Goals.
type Account struct {
	ID      int64
	Email   string
	IsAdmin bool
}

// Goal is the single unit of work being tracked (CONTEXT.md: Goal).
type Goal struct {
	ID        int64
	Title     string
	SoWhat    string
	Owner     Account
	Lifecycle string
	CreatedAt time.Time
}

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

// timeFormat is how timestamps are stored in SQLite TEXT columns.
const timeFormat = time.RFC3339Nano

func accountFromRow(a db.Account) Account {
	return Account{ID: a.ID, Email: a.Email, IsAdmin: a.IsAdmin != 0}
}
