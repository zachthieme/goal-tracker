package domain_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// pruneQueueEmail puts an email to `to` in the outbox as it would stand after
// its sends: queued at createdAt, sent at sentAt if that isn't zero, and
// failed attempts times.
func pruneQueueEmail(t *testing.T, h *testsupport.Harness, to domain.Account, subject string, createdAt, sentAt time.Time, attempts int) {
	t.Helper()
	var sent *string
	if !sentAt.IsZero() {
		s := sentAt.Format(time.RFC3339Nano)
		sent = &s
	}
	if _, err := h.DB.ExecContext(context.Background(), `
		INSERT INTO email_outbox (recipient, account_id, subject, body, created_at, attempts, next_attempt_at, sent_at)
		VALUES (?, ?, ?, 'Body.', ?, ?, ?, ?)`,
		to.Email, to.ID, subject, createdAt.Format(time.RFC3339Nano), attempts,
		h.Clock.Now().Add(time.Hour).Format(time.RFC3339Nano), sent); err != nil {
		t.Fatalf("queue %q: %v", subject, err)
	}
}

// pruneOutboxSubjects lists the subjects of the emails left in the outbox.
func pruneOutboxSubjects(t *testing.T, h *testsupport.Harness) []string {
	t.Helper()
	rows, err := h.DB.QueryContext(context.Background(), `SELECT subject FROM email_outbox ORDER BY subject`)
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	return out
}

// The prune deletes email sent, or given up on, more than 90 days ago, and
// Undo tokens past their window; it keeps newer email, email still being
// tried however old, and an Undo still on offer (ticket #498).
func TestPruneStaleDeletesOldEmailAndExpiredUndoTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t, "boss@example.com")
	owner := h.SignIn("owner@example.com")
	h.Clock.Advance(400*24*time.Hour + 500*time.Millisecond)
	now := h.Clock.Now()
	day := 24 * time.Hour
	pruneQueueEmail(t, h, owner, "Sent 91 days ago", now.Add(-91*day), now.Add(-91*day), 0)
	pruneQueueEmail(t, h, owner, "Sent 89 days ago", now.Add(-95*day), now.Add(-89*day), 2)
	pruneQueueEmail(t, h, owner, "Given up on 91 days ago", now.Add(-91*day), time.Time{}, 5)
	pruneQueueEmail(t, h, owner, "Given up on 89 days ago", now.Add(-89*day), time.Time{}, 5)
	pruneQueueEmail(t, h, owner, "Still being tried", now.Add(-300*day), time.Time{}, 4)

	boss, _, trust, expired := retiredValue(t, h)
	h.Clock.Advance(domain.UndoWindow + time.Second)
	emea := h.CreateDimension(boss, "Region", "EMEA", "APAC").Values[0]
	h.Clock.Advance(time.Minute)
	live, err := h.Service.RetireDimensionValueWithUndo(ctx, boss.ID, emea.ID)
	if err != nil {
		t.Fatalf("RetireDimensionValueWithUndo: %v", err)
	}
	h.Clock.Advance(domain.UndoWindow - 2*time.Minute)

	pruned, err := h.Service.PruneStale(ctx)
	if err != nil {
		t.Fatalf("PruneStale: %v", err)
	}

	want := []string{"Given up on 89 days ago", "Sent 89 days ago", "Still being tried"}
	if got := pruneOutboxSubjects(t, h); !slices.Equal(got, want) {
		t.Errorf("outbox after the prune holds %q, want %q", got, want)
	}
	var tokens []string
	rows, err := h.DB.QueryContext(ctx, `SELECT token FROM undo_tokens`)
	if err != nil {
		t.Fatalf("list undo tokens: %v", err)
	}
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err != nil {
			t.Fatalf("scan undo token: %v", err)
		}
		tokens = append(tokens, tok)
	}
	_ = rows.Close()
	if !slices.Equal(tokens, []string{live}) || slices.Contains(tokens, expired) {
		t.Errorf("undo tokens after the prune = %q, want only the one still on offer", tokens)
	}
	if pruned != (domain.StaleRowsPruned{Email: 2, UndoTokens: 1}) {
		t.Errorf("PruneStale = %+v, want 2 emails and 1 Undo token", pruned)
	}
	if err := h.Service.UndoRetireDimensionValue(ctx, boss.ID, emea.ID, live); err != nil {
		t.Errorf("Undo still on offer after the prune: %v", err)
	}
	if !valueRetired(t, h, trust.ID) {
		t.Errorf("the expired Undo's value isn't Retired any more")
	}
}

// The prune, run in the background, prunes as it starts, logs what it
// deleted, and stops when its context is done.
func TestRunPrunePrunesAtOnceAndStopsWithItsContext(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	h.Clock.Advance(100 * 24 * time.Hour)
	pruneQueueEmail(t, h, owner, "Sent long ago", h.Clock.Now().Add(-95*24*time.Hour), h.Clock.Now().Add(-95*24*time.Hour), 0)
	var log pruneLogBuffer
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		h.Service.RunPrune(ctx, time.Hour, slog.New(slog.NewTextHandler(&log, nil)))
	}()

	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if strings.Contains(log.String(), "level=INFO") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("RunPrune logged no prune within 5s of starting")
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("RunPrune still running 5s after its context was done")
	}

	if line := log.String(); !strings.Contains(line, "email=1") || !strings.Contains(line, "undo_tokens=0") {
		t.Errorf("RunPrune logged %q, want it to say it deleted one email and no Undo tokens", line)
	}
	if got := pruneOutboxSubjects(t, h); len(got) != 0 {
		t.Errorf("outbox holds %q after RunPrune started, want it pruned", got)
	}
}

// pruneLogBuffer is a bytes.Buffer safe to write from one goroutine while another
// reads it.
type pruneLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *pruneLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *pruneLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
