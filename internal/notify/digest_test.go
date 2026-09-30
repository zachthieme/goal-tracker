package notify_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
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

// A Goal is due when it would go Stale before next week's reminder if nobody
// checked in: on a weekly cadence, a Goal last checked in 3 days ago is due. A
// Goal on a 14-day cadence checked in 2 days ago is not, and an Owner with
// nothing due gets no email.
func TestReminderListsGoalsDueBeforeNextWeek(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	weekly := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	fortnightly := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	if _, err := h.Service.SetCadence(context.Background(), fortnightly.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Checkin(sam, weekly.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(1 * day)
	h.Checkin(kim, fortnightly.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(2 * day)

	if err := newNotifier(h).SendReminders(context.Background()); err != nil {
		t.Fatalf("SendReminders: %v", err)
	}

	msgs := sentTo(h.Email, "sam@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to sam = %d, want 1", len(msgs))
	}
	if body := msgs[0].Body; !strings.Contains(body, "Ship search") || strings.Contains(body, "Stale") {
		t.Errorf("want Ship search listed as due, not Stale:\n%s", body)
	}
	if got := sentTo(h.Email, "kim@example.com"); len(got) != 0 {
		t.Errorf("kim has nothing due but was sent %d reminders: %+v", len(got), got)
	}
}

// A Delegate writes Check-ins for the Owner, so they are reminded of the Goals
// they're a Delegate on, alongside the Owner, and told whose Goal it is.
func TestReminderGoesToDelegatesToo(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	h.Clock.Advance(10 * day)

	if err := newNotifier(h).SendReminders(context.Background()); err != nil {
		t.Fatalf("SendReminders: %v", err)
	}

	if got := sentTo(h.Email, "sam@example.com"); len(got) != 1 {
		t.Errorf("reminders to the Owner = %d, want 1", len(got))
	}
	msgs := sentTo(h.Email, "dee@example.com")
	if len(msgs) != 1 {
		t.Fatalf("reminders to the Delegate = %d, want 1", len(msgs))
	}
	body := msgs[0].Body
	link := fmt.Sprintf("http://goals.test/goals/%d#checkin-form", g.ID)
	if !strings.Contains(body, "Ship search") || !strings.Contains(body, link) {
		t.Errorf("Delegate's reminder does not list the Goal with its Check-in link:\n%s", body)
	}
	if !strings.Contains(body, "sam@example.com") {
		t.Errorf("Delegate's reminder does not say whose Goal it is:\n%s", body)
	}
}

// Someone who has left the org isn't emailed: their Goal is Ownerless, but its
// Delegate is still reminded.
func TestReminderSkipsADepartedOwner(t *testing.T) {
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	if err := h.Service.MarkDeparted(context.Background(), admin.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)

	if err := newNotifier(h).SendReminders(context.Background()); err != nil {
		t.Fatalf("SendReminders: %v", err)
	}

	if got := sentTo(h.Email, "sam@example.com"); len(got) != 0 {
		t.Errorf("departed Owner was sent %d reminders, want none", len(got))
	}
	if got := sentTo(h.Email, "dee@example.com"); len(got) != 1 {
		t.Errorf("reminders to the Delegate = %d, want 1", len(got))
	}
}

// digestTo returns the one digest sent to addr, failing the test when there
// isn't exactly one.
func digestTo(t *testing.T, rec *email.Recorder, addr string) string {
	t.Helper()
	msgs := sentTo(rec, addr)
	if len(msgs) != 1 {
		t.Fatalf("digests to %s = %d, want 1: %+v", addr, len(msgs), rec.Sent())
	}
	return msgs[0].Body
}

// A parent Owner's digest lists the link requests waiting on them, linking to
// where they decide.
func TestDigestListsPendingLinkRequests(t *testing.T) {
	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	kim := h.SignIn("kim@example.com")
	parent := h.ActiveGoal(pat, "Grow revenue", "It pays for everything.")
	child := h.ActiveGoal(kim, "Launch pricing page", "Buyers can't see prices.")
	h.RequestLink(kim, child, parent, "")

	if err := newNotifier(h).SendDigests(context.Background()); err != nil {
		t.Fatalf("SendDigests: %v", err)
	}

	body := digestTo(t, h.Email, "pat@example.com")
	for _, want := range []string{"Launch pricing page", "Grow revenue", "kim@example.com", "http://goals.test/links"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest does not mention %q:\n%s", want, body)
		}
	}
	if got := sentTo(h.Email, "kim@example.com"); len(got) != 0 {
		t.Errorf("kim owns no parent Goal but was sent %d digests", len(got))
	}
}
