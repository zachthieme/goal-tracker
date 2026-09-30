// Package clock provides the time source the domain reads, so tests can control
// "now" instead of depending on the wall clock.
package clock

import "time"

// Clock is a source of the current time.
type Clock interface {
	Now() time.Time
}

// Real reads the system wall clock.
type Real struct{}

// Now returns the current wall-clock time in UTC.
func (Real) Now() time.Time { return time.Now().UTC() }

// Fixed is a controllable clock for tests. Its time only advances when a test
// sets it, so timestamps in assertions are deterministic.
type Fixed struct{ t time.Time }

// NewFixed returns a Fixed clock reading t.
func NewFixed(t time.Time) *Fixed { return &Fixed{t: t.UTC()} }

// Now returns the clock's current time.
func (f *Fixed) Now() time.Time { return f.t }

// Set moves the clock to t.
func (f *Fixed) Set(t time.Time) { f.t = t.UTC() }

// Advance moves the clock forward by d.
func (f *Fixed) Advance(d time.Duration) { f.t = f.t.Add(d) }
