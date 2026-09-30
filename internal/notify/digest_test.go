package notify_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/notify"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const (
	day     = 24 * time.Hour
	baseURL = "http://goals.test"
)

// newNotifier wires a Notifier over the harness's Service and its recording
// email sender, on the org's default UTC calendar.
func newNotifier(h *testsupport.Harness) *notify.Notifier {
	return notify.New(h.Service, h.Email, baseURL, time.UTC)
}

// sentTo returns the messages recorded for the address.
func sentTo(rec *email.Recorder, addr string) []email.Message {
	var out []email.Message
	for _, m := range rec.Sent() {
		if m.To == addr {
			out = append(out, m)
		}
	}
	return out
}

// An Owner whose Goal has gone Stale is reminded, and the reminder links
// straight to the Goal's pre-filled Check-in form.
func TestReminderListsStaleGoalWithLinkToItsCheckin(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(10 * day)

	if err := newNotifier(h).SendReminders(context.Background()); err != nil {
		t.Fatalf("SendReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1: %+v", len(msgs), h.Email.Sent())
	}
	body := msgs[0].Body
	if !strings.Contains(body, "Ship search") {
		t.Errorf("reminder does not name the Goal:\n%s", body)
	}
	if !strings.Contains(body, "Stale") {
		t.Errorf("reminder does not say the Goal is Stale:\n%s", body)
	}
	link := fmt.Sprintf("http://goals.test/goals/%d#checkin-form", g.ID)
	if !strings.Contains(body, link) {
		t.Errorf("reminder does not link to %s:\n%s", link, body)
	}
}
