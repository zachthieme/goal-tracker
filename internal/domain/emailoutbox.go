package domain

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// emailOutbox is how a Service's queued email reaches its sender, shared by
// the Service and every transaction it opens. A command queues its emails in
// the email_outbox table inside its transaction (queueEmail), and they are
// sent only once it has committed, so a slow mail server never holds the
// database's write lock and a rolled-back command emails no one (ticket #412).
type emailOutbox struct {
	// delivering is held for a pass over the outbox, so two passes at once
	// can't both send a row.
	delivering sync.Mutex
	// onCommit has the transaction that queued email deliver it as soon as it
	// commits, in its caller, rather than waiting for RunEmailOutbox.
	onCommit bool
	// wake tells RunEmailOutbox that a transaction has committed email for it
	// to send. It holds one wake-up, so a burst of commits wakes it once.
	wake chan struct{}
	// logger is where a failed send is logged; nil logs to slog.Default().
	logger *slog.Logger
}

// WithEmailDeliveredOnCommit has the Service send the email a command queues
// as soon as the command's transaction commits, before the command returns,
// rather than leaving it for RunEmailOutbox. It is for tests, which assert on
// what was sent without running the outbox in the background.
func WithEmailDeliveredOnCommit() Option {
	return func(s *Service) { s.outbox.onCommit = true }
}

// WithEmailOutboxLogger sets where the email outbox logs a send that failed,
// and an email it gave up on. Without it, it logs to slog.Default().
func WithEmailOutboxLogger(logger *slog.Logger) Option {
	return func(s *Service) { s.outbox.logger = logger }
}

// QueueEmails queues msgs in the email outbox together, in the transaction s
// is bound to or else one of their own, to be sent once it commits and tried
// again while a send fails.
func (s *Service) QueueEmails(ctx context.Context, msgs []email.Message) error {
	return s.WithinTx(ctx, func(tx *Service) error {
		for _, msg := range msgs {
			if err := tx.queueEmail(ctx, msg); err != nil {
				return err
			}
		}
		return nil
	})
}

// queueEmail queues msg in the email outbox, to be sent once the transaction
// s is bound to commits, or at once when s is bound to none.
func (s *Service) queueEmail(ctx context.Context, msg email.Message) error {
	now := s.clock.Now().Format(timeFormat)
	if err := s.queries.QueueEmail(ctx, db.QueueEmailParams{
		Recipient: msg.To, AccountID: msg.AccountID, Subject: msg.Subject, Body: msg.Body,
		CreatedAt: now, NextAttemptAt: now,
	}); err != nil {
		return fmt.Errorf("queue email: %w", err)
	}
	if s.queuedEmail == nil {
		s.emailQueued(ctx)
	} else {
		*s.queuedEmail = true
	}
	return nil
}

// emailQueued is called once a transaction that queued email has committed.
// It wakes RunEmailOutbox to send it, or with WithEmailDeliveredOnCommit,
// sends it itself.
func (s *Service) emailQueued(ctx context.Context) {
	if s.outbox.onCommit {
		if err := s.DeliverEmail(ctx); err != nil {
			s.outbox.log().Error("email outbox: deliver", "err", err)
		}
		return
	}
	select {
	case s.outbox.wake <- struct{}{}:
	default:
	}
}

// RunEmailOutbox sends the email in the outbox as it falls due, until ctx is
// done: at once, whenever a command commits email, and every interval, for
// the emails waiting to be tried again.
func (s *Service) RunEmailOutbox(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := s.DeliverEmail(ctx); err != nil && ctx.Err() == nil {
			s.outbox.log().Error("email outbox: deliver", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.outbox.wake:
		case <-ticker.C:
		}
	}
}

// DeliverEmail sends the email in the outbox that is due, oldest first, and
// marks each sent. Call it outside any transaction: each send waits on the
// mail server.
func (s *Service) DeliverEmail(ctx context.Context) error {
	s.outbox.delivering.Lock()
	defer s.outbox.delivering.Unlock()
	rows, err := s.queries.ListUnsentEmail(ctx, emailMaxAttempts)
	if err != nil {
		return fmt.Errorf("list unsent email: %w", err)
	}
	for _, row := range rows {
		now := s.clock.Now()
		due, err := time.Parse(timeFormat, row.NextAttemptAt)
		if err != nil {
			// One unreadable row mustn't hold up the rest: give up on it.
			parseErr := fmt.Errorf("next attempt at %q: %w", row.NextAttemptAt, err)
			if err := s.queries.RecordEmailFailure(ctx, db.RecordEmailFailureParams{
				Attempts:      emailMaxAttempts,
				LastError:     parseErr.Error(),
				NextAttemptAt: now.Format(timeFormat),
				ID:            row.ID,
			}); err != nil {
				return fmt.Errorf("give up on email %d: %w", row.ID, err)
			}
			s.outbox.log().Error("email not sent: giving up, its next attempt time can't be read",
				"email_outbox_id", row.ID, "account_id", row.AccountID, "subject", row.Subject, "err", parseErr)
			continue
		}
		if due.After(now) {
			continue
		}
		sendErr := s.email.Send(ctx, email.Message{To: row.Recipient, AccountID: row.AccountID, Subject: row.Subject, Body: row.Body})
		if sendErr == nil {
			sentAt := now.Format(timeFormat)
			if err := s.queries.MarkEmailSent(ctx, db.MarkEmailSentParams{SentAt: &sentAt, ID: row.ID}); err != nil {
				return fmt.Errorf("mark email %d sent: %w", row.ID, err)
			}
			continue
		}
		attempts := row.Attempts + 1
		next := now.Add(emailRetryAfter(attempts))
		if err := s.queries.RecordEmailFailure(ctx, db.RecordEmailFailureParams{
			Attempts:      attempts,
			LastError:     sendErr.Error(),
			NextAttemptAt: next.Format(timeFormat),
			ID:            row.ID,
		}); err != nil {
			return fmt.Errorf("record email %d failed: %w", row.ID, err)
		}
		attrs := []any{"email_outbox_id", row.ID, "account_id", row.AccountID, "subject", row.Subject, "attempts", attempts, "err", sendErr}
		if attempts >= emailMaxAttempts {
			s.outbox.log().Error("email not sent: giving up after its last attempt failed", attrs...)
		} else {
			s.outbox.log().Warn("email not sent: will try again", append(attrs, "next_attempt_at", next)...)
		}
	}
	return nil
}

// log is where the outbox logs: its logger, or slog.Default().
func (o *emailOutbox) log() *slog.Logger {
	if o.logger == nil {
		return slog.Default()
	}
	return o.logger
}

// emailRetryAfter is how long the outbox waits to try an email again after
// its attempts-th failed send: a minute after the first, doubling each time.
func emailRetryAfter(attempts int64) time.Duration {
	return time.Minute << (attempts - 1)
}

// emailMaxAttempts is how many times an email is tried before the outbox
// gives up on it.
const emailMaxAttempts = 5
