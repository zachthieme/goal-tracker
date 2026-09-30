// Package email defines the outbound email seam and a recording fake for tests.
//
// No mail is sent yet in the walking skeleton, but the seam exists so later
// tickets (Handoffs, Report publication) have a controllable sender from the
// start and tests can assert on what would be sent.
package email

import (
	"context"
	"log/slog"
	"sync"
)

// Message is a single outbound email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers outbound email.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender logs each message instead of delivering it. It is the development
// sender: no real mail transport exists yet, but sends are observable.
type LogSender struct{ Logger *slog.Logger }

// Send logs msg at info level.
func (l LogSender) Send(_ context.Context, msg Message) error {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("email send (development, not delivered)",
		"to", msg.To, "subject", msg.Subject)
	return nil
}

// Recorder is a Sender that records messages instead of delivering them, so a
// test can assert on what was sent.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// Send records msg.
func (r *Recorder) Send(_ context.Context, msg Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, msg)
	return nil
}

// Sent returns a copy of the messages recorded so far.
func (r *Recorder) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.sent))
	copy(out, r.sent)
	return out
}
