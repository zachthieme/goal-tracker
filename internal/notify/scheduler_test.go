package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

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
	return notify.NewScheduler(h.Service, notify.WeeklyJob, when, newNotifier(h).SendWeekly)
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
// poll rather than a week later, and once it succeeds the slot isn't run
// again.
func TestSchedulerRetriesAFailedRunAtTheNextPoll(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.ActiveGoal(sam, "Ship search", "People can't find things.")
	when := mondays9am(t)
	send := newNotifier(h).SendWeekly
	failures := 1
	sched := notify.NewScheduler(h.Service, notify.WeeklyJob, when, func(ctx context.Context, slot time.Time) error {
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
		t.Fatalf("sent %d emails at the poll after the failed run, want the one reminder", got)
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

// A run that keeps failing is still tried at every poll, but logged at its
// first failure and then after a backoff that doubles from a minute to at
// most an hour, each line naming the job and the slot; its recovery is logged
// once. A later failure is logged at once again (ticket #545).
func TestSchedulerLogsAFailingRunWithABackoff(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	when := mondays9am(t)
	failing, tries := true, 0
	sched := notify.NewScheduler(h.Service, notify.WeeklyJob, when, func(context.Context, time.Time) error {
		tries++
		if failing {
			return errors.New("database is locked")
		}
		return nil
	})
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	// poll sets the clock to at and polls sched, returning the lines it logged.
	poll := func(at time.Time) []map[string]any {
		t.Helper()
		h.Clock.Set(at)
		log.Reset()
		sched.Poll(context.Background(), logger)
		var lines []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("log line %q: %v", line, err)
			}
			lines = append(lines, rec)
		}
		return lines
	}
	// isFor reports whether rec is at level and names the weekly emails job and
	// slot.
	isFor := func(rec map[string]any, level string, slot time.Time) bool {
		return rec["level"] == level && rec["job"] == notify.WeeklyJob && rec["slot"] == slot.Format(time.RFC3339)
	}

	if got := poll(mondayAt9(0).Add(-time.Minute)); len(got) != 0 {
		t.Fatalf("logged %v before the slot, want nothing", got)
	}
	var loggedAt []int
	for minute := range 184 {
		for _, rec := range poll(mondayAt9(0).Add(time.Duration(minute) * time.Minute)) {
			if !isFor(rec, "ERROR", mondayAt9(0)) {
				t.Fatalf("minute %d logged %v, want an ERROR naming the job and Monday's slot", minute, rec)
			}
			loggedAt = append(loggedAt, minute)
		}
	}
	if tries != 184 {
		t.Errorf("tried the run %d times over 184 polls, want every poll", tries)
	}
	// Each wait doubles from a minute until it reaches the hour: 1, 2, 4, 8, 16,
	// 32, 60, 60 minutes.
	if want := []int{0, 1, 3, 7, 15, 31, 63, 123, 183}; !slices.Equal(loggedAt, want) {
		t.Errorf("logged the failure at minutes %v, want %v", loggedAt, want)
	}

	failing = false
	got := poll(mondayAt9(0).Add(184 * time.Minute))
	if len(got) != 1 || !isFor(got[0], "INFO", mondayAt9(0)) {
		t.Fatalf("logged %v on recovering, want one INFO naming the job and Monday's slot", got)
	}
	if got := poll(mondayAt9(0).Add(185 * time.Minute)); len(got) != 0 {
		t.Errorf("logged %v after recovering, want nothing more", got)
	}

	failing = true
	got = poll(mondayAt9(1))
	if len(got) != 1 || !isFor(got[0], "ERROR", mondayAt9(1)) {
		t.Errorf("logged %v on next week's first failure, want an ERROR at once naming its slot", got)
	}
}

// Restarted after Monday's slot was sent, the app that can't read the job's
// last run logs the failure without naming a slot: it doesn't know which slot
// is due, and Monday's is already done (ticket #547).
func TestSchedulerLogsAFailureToReadTheLastRunWithoutASlot(t *testing.T) {
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
	var log bytes.Buffer
	restarted.Poll(context.Background(), slog.New(slog.NewJSONHandler(&log, nil)))

	var rec map[string]any
	if err := json.Unmarshal(log.Bytes(), &rec); err != nil {
		t.Fatalf("log %q: want one JSON line: %v", log.String(), err)
	}
	if rec["level"] != "ERROR" || rec["job"] != notify.WeeklyJob || rec["err"] == nil {
		t.Errorf("logged %v, want an ERROR naming the job and the error", rec)
	}
	if slot, ok := rec["slot"]; ok {
		t.Errorf("logged slot %v for failing to read the last run, want no slot", slot)
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
