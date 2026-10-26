// Package email defines the outbound email seam, Sender, that the domain's
// email outbox sends each queued message through once its command has
// committed.
//
// LogSender, the Sender the app and the seed use, logs each message instead of
// delivering it: no real mail transport exists yet. Recorder records what
// would be sent, so a test can assert on it.
package email

import (
	"context"
	"log/slog"
	"sync"
)

// Message is a single outbound email. AccountID is the Account whose address
// To is: a log line names the recipient by it, never by their address.
type Message struct {
	To        string
	AccountID int64
	Subject   string
	Body      string
}

// Sender delivers outbound email. The domain sends a command's email through
// it only once the command's transaction has committed, from its email outbox,
// so a Send may be as slow as the mail server is, and a failed one is tried
// again.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// LogSender logs each message instead of delivering it. It is the development
// sender: no real mail transport exists yet, but sends are observable.
type LogSender struct{ Logger *slog.Logger }

// Send logs msg at info level: its recipient's Account ID and its subject,
// never their address.
func (l LogSender) Send(_ context.Context, msg Message) error {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("email send (development, not delivered)",
		"account_id", msg.AccountID, "subject", msg.Subject)
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
