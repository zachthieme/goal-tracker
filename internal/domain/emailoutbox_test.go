package domain_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// lockCheckingSender records each email it is asked to send, and fails one
// sent while a transaction holds the database's write lock.
type lockCheckingSender struct {
	h *testsupport.Harness

	mu       sync.Mutex
	sent     []email.Message
	inLocked []email.Message
}

func (s *lockCheckingSender) Send(_ context.Context, msg email.Message) error {
	locked := s.h.WriteLockHeld()
	s.mu.Lock()
	defer s.mu.Unlock()
	if locked {
		s.inLocked = append(s.inLocked, msg)
		return errors.New("sent while a transaction holds the write lock")
	}
	s.sent = append(s.sent, msg)
	return nil
}

// Comment alerts and Action Item emails go out only once the transaction that
// recorded them has committed, so a slow mail server never holds the
// database's write lock (ticket #412).
func TestEmailIsSentOnlyOnceItsTransactionHasCommitted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sender := &lockCheckingSender{h: h}
	svc := domain.NewService(h.DB, h.Clock, sender, nil, domain.WithEmailDeliveredOnCommit())
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	other := h.SignIn("other@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)

	question, err := svc.AddComment(ctx, author.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := svc.ReplyToComment(ctx, owner.ID, question.ID, "Legal review."); err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	if _, err := svc.AddGoalComment(ctx, author.ID, g.ID, "Any news?"); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	item, err := svc.RaiseActionItem(ctx, author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: g.ID, Text: "Book the review.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if _, err := svc.ReassignActionItem(ctx, author.ID, item.ID, other.ID); err != nil {
		t.Fatalf("ReassignActionItem: %v", err)
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.inLocked) != 0 {
		t.Errorf("sent %+v while a transaction held the write lock, want none", sender.inLocked)
	}
	var to []string
	accounts := map[string]int64{owner.Email: owner.ID, author.Email: author.ID, other.Email: other.ID}
	for _, m := range sender.sent {
		to = append(to, m.To)
		if m.AccountID != accounts[m.To] {
			t.Errorf("email to %s is for Account %d, want %d", m.To, m.AccountID, accounts[m.To])
		}
	}
	want := []string{"owner@example.com", "author@example.com", "owner@example.com", "owner@example.com", "other@example.com"}
	if len(to) != len(want) {
		t.Fatalf("sent to %v, want %v: the comment, the reply, the Goal comment, the raise and the reassignment", to, want)
	}
	for i := range want {
		if to[i] != want[i] {
			t.Errorf("email %d to %q, want %q", i, to[i], want[i])
		}
	}
}

// flakySender fails its first failures sends, then records each email it
// sends.
type flakySender struct {
	mu       sync.Mutex
	failures int
	attempts int
	sent     []email.Message
}

func (s *flakySender) Send(_ context.Context, msg email.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	if s.attempts <= s.failures {
		return errors.New("mail is down")
	}
	s.sent = append(s.sent, msg)
	return nil
}

func (s *flakySender) counts() (attempts, sent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, len(s.sent)
}

// A comment whose alert can't be sent is kept, and its alert is tried again,
// waiting longer after each failure, until it goes out, once.
func TestCommentIsKeptAndItsAlertRetriedWhileMailIsDown(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sender := &flakySender{failures: 2}
	svc := domain.NewService(h.DB, h.Clock, sender, nil, domain.WithEmailDeliveredOnCommit())
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")

	if _, err := svc.AddGoalComment(ctx, reader.ID, g.ID, "Why?"); err != nil {
		t.Fatalf("AddGoalComment: %v, want the comment kept though its alert could not be sent", err)
	}
	threads, err := h.Service.GoalThreads(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads %+v, want the comment", threads)
	}

	// The first send failed as the comment committed: the next waits a
	// minute, the one after two more.
	steps := []struct {
		advance        time.Duration
		attempts, sent int
	}{
		{0, 1, 0},
		{59 * time.Second, 1, 0},
		{time.Second, 2, 0},
		{119 * time.Second, 2, 0},
		{time.Second, 3, 1},
		{time.Hour, 3, 1},
	}
	for i, step := range steps {
		h.Clock.Advance(step.advance)
		if err := svc.DeliverEmail(ctx); err != nil {
			t.Fatalf("step %d: DeliverEmail: %v", i, err)
		}
		if attempts, sent := sender.counts(); attempts != step.attempts || sent != step.sent {
			t.Errorf("step %d, %v on: %d attempts and %d sent, want %d and %d", i, step.advance, attempts, sent, step.attempts, step.sent)
		}
	}
	if sender.sent[0].To != "owner@example.com" {
		t.Errorf("alert to %q, want the Owner", sender.sent[0].To)
	}
}

// After its fifth failed send an email is no longer tried, and the outbox logs
// an error naming it and its recipient's Account, never their address.
func TestOutboxGivesUpOnAnEmailAfterFiveFailedSends(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sender := &flakySender{failures: 1000}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	svc := domain.NewService(h.DB, h.Clock, sender, nil, domain.WithEmailDeliveredOnCommit(), domain.WithEmailOutboxLogger(logger))
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")

	if _, err := svc.AddGoalComment(ctx, reader.ID, g.ID, "Why?"); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	for range 10 {
		h.Clock.Advance(24 * time.Hour)
		if err := svc.DeliverEmail(ctx); err != nil {
			t.Fatalf("DeliverEmail: %v", err)
		}
	}

	if attempts, _ := sender.counts(); attempts != 5 {
		t.Errorf("%d attempts, want 5", attempts)
	}
	var errs, warns []string
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		switch {
		case strings.Contains(line, "level=ERROR"):
			errs = append(errs, line)
		case strings.Contains(line, "level=WARN"):
			warns = append(warns, line)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("logged errors %q, want one, on giving up:\n%s", errs, log.String())
	}
	if len(warns) != 4 {
		t.Fatalf("logged warnings %q, want four, one for each retry:\n%s", warns, log.String())
	}
	accountID := fmt.Sprintf("account_id=%d", owner.ID)
	for _, want := range []string{"email_outbox_id=1", accountID, "attempts=5", "mail is down"} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("error %q does not mention %q", errs[0], want)
		}
	}
	for _, line := range append(warns, errs...) {
		if !strings.Contains(line, accountID) {
			t.Errorf("line %q does not name the recipient by %q", line, accountID)
		}
		if strings.Contains(line, "@") {
			t.Errorf("line %q carries an email address", line)
		}
	}
}

// The outbox, run in the background, sends a command's email once the
// command commits, without waiting for its next round, and stops when its
// context is done.
func TestRunEmailOutboxSendsQueuedEmailOnceItsCommandCommits(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	sender := &flakySender{}
	svc := domain.NewService(h.DB, h.Clock, sender, nil)
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		svc.RunEmailOutbox(ctx, time.Hour)
	}()

	if _, err := svc.AddGoalComment(ctx, reader.ID, g.ID, "Why?"); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, sent := sender.counts(); sent == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the comment's alert wasn't sent within 5s of it committing")
		}
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunEmailOutbox still running 5s after its context was done")
	}
}

// An email whose next attempt time can't be read is given up on, logged once,
// and skipped, so the email queued after it still goes out (ticket #498).
func TestOutboxSkipsAnEmailWhoseNextAttemptCantBeRead(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sender := &flakySender{}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	svc := domain.NewService(h.DB, h.Clock, sender, nil, domain.WithEmailOutboxLogger(logger))
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	if _, err := h.DB.ExecContext(ctx, `
		INSERT INTO email_outbox (recipient, account_id, subject, body, created_at, next_attempt_at)
		VALUES (?, ?, 'Broken', 'Unreadable.', ?, 'not a time')`,
		reader.Email, reader.ID, h.Clock.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("queue a broken email: %v", err)
	}
	if _, err := svc.AddGoalComment(ctx, reader.ID, g.ID, "Why?"); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}

	for pass := range 2 {
		if err := svc.DeliverEmail(ctx); err != nil {
			t.Fatalf("pass %d: DeliverEmail: %v", pass, err)
		}
	}

	attempts, sent := sender.counts()
	if attempts != 1 || sent != 1 || sender.sent[0].To != "owner@example.com" {
		t.Errorf("%d attempts, sent %+v, want the comment alert sent to the Owner once and the broken email never tried", attempts, sender.sent)
	}
	var tries int64
	var lastError string
	if err := h.DB.QueryRowContext(ctx, `SELECT attempts, last_error FROM email_outbox WHERE subject = 'Broken'`).Scan(&tries, &lastError); err != nil {
		t.Fatalf("read the broken email: %v", err)
	}
	if tries != 5 || !strings.Contains(lastError, "not a time") {
		t.Errorf("broken email has %d attempts and last error %q, want it given up on (5) saying why", tries, lastError)
	}
	var errs []string
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if strings.Contains(line, "level=ERROR") {
			errs = append(errs, line)
		}
	}
	if len(errs) != 1 {
		t.Fatalf("logged errors %q, want one, on skipping the broken email:\n%s", errs, log.String())
	}
	for _, want := range []string{"email_outbox_id=1", fmt.Sprintf("account_id=%d", reader.ID)} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("error %q does not mention %q", errs[0], want)
		}
	}
	if strings.Contains(errs[0], "@") {
		t.Errorf("error %q carries an email address", errs[0])
	}
}
