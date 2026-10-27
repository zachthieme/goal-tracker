package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/notify"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The weekly emails go out once when the clock reaches the configured day and
// time, and again a week later — not before, and not twice.
func TestSchedulerSendsTheWeeklyEmailsAtTheConfiguredTime(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when, err := notify.ParseWeekly("Monday", "09:00", time.UTC)
	if err != nil {
		t.Fatalf("ParseWeekly: %v", err)
	}
	// The harness clock starts on Friday 2026-01-02.
	sched := weeklyScheduler(h, when)
	tick := ticker(t, h)
	tick(sched, time.Date(2026, 1, 5, 8, 59, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 0 {
		t.Fatalf("sent %d emails before Monday 09:00, want none", got)
	}
	tick(sched, time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	tick(sched, time.Date(2026, 1, 5, 9, 1, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Fatalf("sent %d emails on Monday 09:00, want the one reminder", got)
	}
	tick(sched, time.Date(2026, 1, 12, 9, 0, 0, 0, time.UTC))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Fatalf("sent %d emails by the next Monday 09:00, want 2", got)
	}
}

// weeklyScheduler schedules the weekly emails over the harness's Service, as
// the app does: a Scheduler built on the same database again is the app
// restarted.
func weeklyScheduler(h *testsupport.Harness, when notify.Weekly) *notify.Scheduler {
	return notify.NewScheduler(h.Service, domain.WeeklyEmailsJob, when, h.Service.QueueWeeklyEmails)
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

// ticker returns a func that sets the harness clock to at and ticks sched,
// failing the test on error.
func ticker(t *testing.T, h *testsupport.Harness) func(sched *notify.Scheduler, at time.Time) {
	return func(sched *notify.Scheduler, at time.Time) {
		t.Helper()
		h.Clock.Set(at)
		if err := sched.Tick(context.Background()); err != nil {
			t.Fatalf("Tick at %v: %v", at, err)
		}
	}
}

// mondayAt9 is Monday 09:00 UTC weeks weeks after 2026-01-05.
func mondayAt9(weeks int) time.Time {
	return time.Date(2026, 1, 5+7*weeks, 9, 0, 0, 0, time.UTC)
}

// mondays9am is the weekly emails' slot in the tests: Monday 09:00 UTC.
func mondays9am(t *testing.T) notify.Weekly {
	t.Helper()
	when, err := notify.ParseWeekly("Monday", "09:00", time.UTC)
	if err != nil {
		t.Fatalf("ParseWeekly: %v", err)
	}
	return when
}

// Restarted 20 seconds after Monday's 09:00 slot, the app sends that week's
// emails once at its first tick, late, rather than waiting a week, and nothing
// more until the next slot.
func TestSchedulerRestartedJustAfterASlotSendsItsEmailsLate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when := mondays9am(t)
	tick := ticker(t, h)
	before := weeklyScheduler(h, when)
	tick(before, mondayAt9(0).Add(-time.Minute))
	tick(before, mondayAt9(0))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Fatalf("sent %d emails at the first Monday's slot, want the one reminder", got)
	}

	restarted := weeklyScheduler(h, when)
	tick(restarted, mondayAt9(1).Add(20*time.Second))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Fatalf("sent %d emails on the first tick after the restart, want the week's reminder: 2 in all", got)
	}
	for _, at := range []time.Time{mondayAt9(1).Add(time.Minute), mondayAt9(1).Add(time.Hour), mondayAt9(2).Add(-time.Minute)} {
		tick(restarted, at)
	}
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Errorf("sent %d emails before the next slot, want no more than 2", got)
	}
	tick(restarted, mondayAt9(2))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 3 {
		t.Errorf("sent %d emails at the next slot, want 3", got)
	}
}

// Down for three weeks, the app sends the weekly emails once when it starts,
// not once for each week it missed.
func TestSchedulerRestartedWeeksLaterSendsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when := mondays9am(t)
	tick := ticker(t, h)
	before := weeklyScheduler(h, when)
	tick(before, mondayAt9(0).Add(-time.Minute))
	tick(before, mondayAt9(0))

	restarted := weeklyScheduler(h, when)
	tick(restarted, mondayAt9(3).Add(2*time.Hour))
	tick(restarted, mondayAt9(3).Add(3*time.Hour))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Errorf("sent %d emails after three weeks down, want one more run: 2 in all", got)
	}
}

// The app started for the first time, just after a slot, sends nothing until
// the next slot: installing it emails no one by surprise.
func TestSchedulerFirstStartedJustAfterASlotWaitsForTheNext(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when := mondays9am(t)
	tick := ticker(t, h)
	sched := weeklyScheduler(h, when)
	tick(sched, mondayAt9(0).Add(20*time.Second))
	tick(sched, mondayAt9(1).Add(-time.Minute))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 0 {
		t.Fatalf("sent %d emails before the next slot, want none", got)
	}
	// Restarted, too, it still waits.
	tick(weeklyScheduler(h, when), mondayAt9(1).Add(-time.Minute))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 0 {
		t.Fatalf("sent %d emails restarted before the next slot, want none", got)
	}
	tick(sched, mondayAt9(1))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Errorf("sent %d emails at the next slot, want the one reminder", got)
	}
}

// A run that fails, such as on a database error, is tried again at the next
// Tick rather than a week later, and once it succeeds the slot isn't run
// again.
func TestSchedulerRetriesAFailedRunAtTheNextTick(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when := mondays9am(t)
	send := h.Service.QueueWeeklyEmails
	failures := 1
	sched := notify.NewScheduler(h.Service, domain.WeeklyEmailsJob, when, func(ctx context.Context, slot time.Time) error {
		if failures > 0 {
			failures--
			return errors.New("database is locked")
		}
		return send(ctx, slot)
	})
	tick := ticker(t, h)
	tick(sched, mondayAt9(0).Add(-time.Minute))
	h.Clock.Set(mondayAt9(0))
	if err := sched.Tick(context.Background()); err == nil {
		t.Fatalf("Tick at the slot succeeded, want the run's error")
	}
	if got := len(sentTo(h.Email, "sam@example.com")); got != 0 {
		t.Fatalf("sent %d emails from the failed run, want none", got)
	}

	tick(sched, mondayAt9(0).Add(time.Minute))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Fatalf("sent %d emails at the Tick after the failed run, want the one reminder", got)
	}
	for _, at := range []time.Time{mondayAt9(0).Add(2 * time.Minute), mondayAt9(1).Add(-time.Minute)} {
		tick(sched, at)
	}
	if got := len(sentTo(h.Email, "sam@example.com")); got != 1 {
		t.Errorf("sent %d emails before the next slot, want no more than 1", got)
	}
	tick(sched, mondayAt9(1))
	if got := len(sentTo(h.Email, "sam@example.com")); got != 2 {
		t.Errorf("sent %d emails at the next slot, want 2", got)
	}
}

// A run that fails returns an error naming the slot it was for, so the
// failure's log says which week's emails didn't go out.
func TestSchedulerAFailedRunsErrorNamesItsSlot(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	when := mondays9am(t)
	sched := notify.NewScheduler(h.Service, domain.WeeklyEmailsJob, when, func(context.Context, time.Time) error {
		return errors.New("database is locked")
	})
	ticker(t, h)(sched, mondayAt9(0).Add(-time.Minute))

	h.Clock.Set(mondayAt9(0).Add(time.Minute))
	err := sched.Tick(context.Background())
	if err == nil {
		t.Fatal("Tick at the slot succeeded, want the run's error")
	}
	if slot := mondayAt9(0).Format(time.RFC3339); !strings.Contains(err.Error(), slot) || !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("Tick's error %q, want it to name the slot %s and the run's error", err, slot)
	}
}

// Restarted after Monday's slot was sent, the app that can't read the job's
// last run fails without naming a slot: it doesn't know which slot is due, and
// Monday's is already done (ticket #547).
func TestSchedulerAFailureToReadTheLastRunNamesNoSlot(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	when := mondays9am(t)
	tick := ticker(t, h)
	sent := weeklyScheduler(h, when)
	tick(sent, mondayAt9(0).Add(-time.Minute))
	tick(sent, mondayAt9(0))

	restarted := weeklyScheduler(h, when)
	if err := h.DB.Close(); err != nil {
		t.Fatalf("closing the database: %v", err)
	}
	h.Clock.Set(mondayAt9(0).Add(time.Hour))
	err := restarted.Tick(context.Background())
	if err == nil {
		t.Fatal("Tick with the database closed succeeded, want an error")
	}
	if msg := err.Error(); strings.Contains(msg, "slot") || strings.Contains(msg, "2026-") {
		t.Errorf("Tick's error %q names a slot, want none", msg)
	}
}

// Two Schedulers on one database, such as two copies of the app, both run
// Monday's slot, and each of its emails is queued once.
func TestSchedulerRunningASlotTwiceQueuesItsEmailsOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, dee, g.ID)
	when := mondays9am(t)
	tick := ticker(t, h)
	one, two := weeklyScheduler(h, when), weeklyScheduler(h, when)
	tick(one, mondayAt9(0).Add(-time.Minute))
	tick(two, mondayAt9(0).Add(-time.Minute))
	tick(one, mondayAt9(0))
	tick(two, mondayAt9(0))

	if got := recipientsOf(h.Email.Sent()); len(got) != 2 || got[0] != "sam@example.com" || got[1] != "dee@example.com" {
		t.Errorf("sent to %v, want the Owner's and the Delegate's reminders once each", got)
	}
}

// recipientsOf lists who the messages went to, in order.
func recipientsOf(msgs []email.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.To)
	}
	return out
}

// The day and time are on the company's calendar: Monday 09:00 in Los Angeles
// is 17:00 UTC in winter.
func TestSchedulerReadsTheTimeInTheOrgsTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	when, err := notify.ParseWeekly("monday", "09:00", loc)
	if err != nil {
		t.Fatalf("ParseWeekly: %v", err)
	}
	runs := 0
	sched := notify.NewScheduler(h.Service, "counting", when, func(context.Context, time.Time) error { runs++; return nil })

	h.Clock.Set(time.Date(2026, 1, 5, 16, 59, 0, 0, time.UTC))
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if runs != 0 {
		t.Fatalf("ran at 08:59 in Los Angeles, want it to wait for 09:00")
	}
	h.Clock.Set(time.Date(2026, 1, 5, 17, 0, 0, 0, time.UTC))
	if err := sched.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if runs != 1 {
		t.Fatalf("runs at 09:00 in Los Angeles = %d, want 1", runs)
	}
}

// A day or time that can't be read is refused rather than guessed at.
func TestParseWeeklyRefusesAnUnreadableDayOrTime(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ day, at string }{
		{"Mon", "09:00"},
		{"Funday", "09:00"},
		{"Monday", "9am"},
		{"Monday", "25:00"},
	} {
		if _, err := notify.ParseWeekly(tc.day, tc.at, time.UTC); err == nil {
			t.Errorf("ParseWeekly(%q, %q) accepted it, want an error", tc.day, tc.at)
		}
	}
}
