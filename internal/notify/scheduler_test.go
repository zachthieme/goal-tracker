package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/notify"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The weekly emails go out once when the clock reaches the configured day and
// time, and again a week later — not before, and not twice.
func TestSchedulerSendsTheWeeklyEmailsAtTheConfiguredTime(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when, err := notify.ParseWeekly("Monday", "09:00", time.UTC)
	if err != nil {
		t.Fatalf("ParseWeekly: %v", err)
	}
	// The harness clock starts on Friday 2026-01-02.
	sched := notify.NewScheduler(h.Clock, when, newNotifier(h).SendWeekly)
	ctx := context.Background()

	tick := func(at time.Time) {
		t.Helper()
		h.Clock.Set(at)
		if err := sched.Tick(ctx); err != nil {
			t.Fatalf("Tick at %v: %v", at, err)
		}
	}
	tick(time.Date(2026, 1, 5, 8, 59, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 0 {
		t.Fatalf("sent %d emails before Monday 09:00, want none", got)
	}
	tick(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	tick(time.Date(2026, 1, 5, 9, 1, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Fatalf("sent %d emails on Monday 09:00, want the one reminder", got)
	}
	tick(time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Fatalf("sent %d emails by the next Monday 09:00, want 2", got)
	}
}
