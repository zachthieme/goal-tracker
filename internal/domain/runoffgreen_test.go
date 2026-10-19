package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// runOffGreen reads goalID's Run off Green through svc, failing the test on
// error.
func runOffGreen(t *testing.T, svc *domain.Service, goalID int64) domain.RunOffGreen {
	t.Helper()
	r, err := svc.RunOffGreen(context.Background(), goalID)
	if err != nil {
		t.Fatalf("RunOffGreen: %v", err)
	}
	return r
}

// redCheckin checks in Red on goalID as author, with the Path to Green Red
// needs.
func redCheckin(h *testsupport.Harness, author domain.Account, goalID int64) domain.Checkin {
	h.T.Helper()
	return h.Checkin(author, goalID, domain.HealthRed, "Blocked.", "Unblock it.", h.Clock.Now().AddDate(0, 0, 30))
}

// A Goal that turned Red today has been off Green for 1 week, counted from
// the Red Check-in (CONTEXT.md: Run off Green).
func TestRunOffGreenOfAGoalThatTurnedRedTodayIsOneWeek(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(time.Hour)
	red := redCheckin(h, sam, g.ID)

	r := runOffGreen(t, h.Service, g.ID)
	if !r.Running() || !r.Since.Equal(red.CreatedAt) {
		t.Errorf("Run off Green = %+v, want running since the Red Check-in at %v", r, red.CreatedAt)
	}
	if r.Weeks != 1 || r.Label() != "1 wk" {
		t.Errorf("Weeks, Label = %d, %q; want 1, \"1 wk\"", r.Weeks, r.Label())
	}
}

// A Goal with no Check-in, or whose latest Check-in is Green, is not off
// Green.
func TestRunOffGreenNeedsALatestCheckinThatIsNotGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	if r := runOffGreen(t, h.Service, g.ID); r.Running() || r.Weeks != 0 || r.Label() != "" {
		t.Errorf("Run off Green with no Check-in = %+v (%q), want none", r, r.Label())
	}

	redCheckin(h, sam, g.ID)
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthGreen, "Unblocked.", "", time.Time{})
	if r := runOffGreen(t, h.Service, g.ID); r.Running() || r.Weeks != 0 {
		t.Errorf("Run off Green after a Green Check-in = %+v, want none", r)
	}
}

// A Goal Red at weekly Check-ins on five consecutive Mondays, read on the
// fifth, is 5 weeks off Green: with an Owner in the company's timezone, the
// five Red squares its Health strip shows.
func TestRunOffGreenCountsCalendarWeeks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	// The harness starts on Friday 2 Jan 2026; the next Monday is 5 Jan.
	h.Clock.Set(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	first := redCheckin(h, sam, g.ID)
	for range 4 {
		h.Clock.Advance(7 * day)
		redCheckin(h, sam, g.ID)
	}

	r := runOffGreen(t, h.Service, g.ID)
	if !r.Since.Equal(first.CreatedAt) || r.Weeks != 5 || r.Label() != "5 wks" {
		t.Errorf("Run off Green = %+v (%q), want since %v, 5 weeks, \"5 wks\"", r, r.Label(), first.CreatedAt)
	}
}

// A run that began on a Sunday has spanned two calendar weeks by the next
// Monday.
func TestRunOffGreenBeganOnASundayIsTwoWeeksOnMonday(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 1, 4, 18, 0, 0, 0, time.UTC))
	redCheckin(h, sam, g.ID)
	h.Clock.Set(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))

	if r := runOffGreen(t, h.Service, g.ID); r.Weeks != 2 {
		t.Errorf("Weeks on the Monday after a Sunday Red = %d, want 2", r.Weeks)
	}
}

// Moving between Red and Yellow doesn't end a run; a Green Check-in does, and
// the next Yellow begins a new one (CONTEXT.md: Run off Green).
func TestRunOffGreenEndsOnlyAtAGreenCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")

	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	first := redCheckin(h, sam, g.ID)
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthYellow, "Better.", "Keep going.", h.Clock.Now().AddDate(0, 0, 14))
	h.Clock.Advance(7 * day)
	redCheckin(h, sam, g.ID)
	if r := runOffGreen(t, h.Service, g.ID); !r.Since.Equal(first.CreatedAt) {
		t.Errorf("Red, Yellow, Red runs since %v, want the first Red at %v", r.Since, first.CreatedAt)
	}

	other := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	redCheckin(h, sam, other.ID)
	h.Clock.Advance(7 * day)
	h.Checkin(sam, other.ID, domain.HealthGreen, "Fixed.", "", time.Time{})
	h.Clock.Advance(7 * day)
	yellow := h.Checkin(sam, other.ID, domain.HealthYellow, "Wobbling.", "Watch it.", h.Clock.Now().AddDate(0, 0, 14))
	if r := runOffGreen(t, h.Service, other.ID); !r.Since.Equal(yellow.CreatedAt) || r.Weeks != 1 {
		t.Errorf("Red, Green, Yellow = %+v, want since the Yellow at %v, 1 week", r, yellow.CreatedAt)
	}
}

// A Goal On Hold is not tracked, so it has no run while On Hold; the spell On
// Hold doesn't end the run either, so resumed still Red it runs from the Red
// before it.
func TestRunOffGreenShowsOnlyWhileActiveAndSurvivesOnHold(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	first := redCheckin(h, sam, g.ID)
	h.Clock.Advance(7 * day)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on legal.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	if r := runOffGreen(t, h.Service, g.ID); r.Running() || r.Weeks != 0 {
		t.Errorf("Run off Green of a Goal On Hold while Red = %+v, want none", r)
	}

	h.Clock.Advance(7 * day)
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthRed, Status: "Back, still blocked.",
		PathToGreen: "Unblock it.", PathTargetDate: h.Clock.Now().AddDate(0, 0, 30), Lifecycle: domain.LifecycleActive,
	}); err != nil {
		t.Fatalf("SubmitCheckin resume: %v", err)
	}
	if r := runOffGreen(t, h.Service, g.ID); !r.Since.Equal(first.CreatedAt) || r.Weeks != 3 {
		t.Errorf("Run off Green resumed Red = %+v, want since the first Red at %v, 3 weeks", r, first.CreatedAt)
	}

	// Nor does a Check-in with no Health begin one: a Goal put On Hold, then
	// resumed Yellow, runs from the Yellow.
	paused := h.OnHoldGoal(sam, "Reduce outages", "Outages cost trust.", "Waiting on legal.")
	h.Clock.Advance(7 * day)
	resumed, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: paused.ID, AuthorID: sam.ID, Health: domain.HealthYellow, Status: "Back, wobbling.",
		PathToGreen: "Watch it.", PathTargetDate: h.Clock.Now().AddDate(0, 0, 14), Lifecycle: domain.LifecycleActive,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin resume: %v", err)
	}
	if r := runOffGreen(t, h.Service, paused.ID); !r.Since.Equal(resumed.CreatedAt) || r.Weeks != 1 {
		t.Errorf("Run off Green resumed Yellow after On Hold = %+v, want since the Yellow at %v, 1 week", r, resumed.CreatedAt)
	}
}

// Weeks are calendar weeks of the Goal's timezone. A Check-in at 03:00 UTC on
// Monday 5 January is still Sunday the 4th in Los Angeles, so read at 18:00
// UTC on Monday the 12th the run spans three weeks there and two in UTC. An
// Owner with no Timezone of their own is counted in the company's; an Owner
// with one, in theirs (CONTEXT.md: Timezone).
func TestRunOffGreenCountsWeeksInTheGoalsTimezone(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t)
	laCompany := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	ana := h.SignIn("ana@example.com")
	h.SetTimezone(ana, "America/Los_Angeles")
	plain := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	owners := h.ActiveGoal(ana, "Reduce outages", "Outages cost trust.")
	h.Clock.Set(time.Date(2026, 1, 5, 3, 0, 0, 0, time.UTC))
	redCheckin(h, sam, plain.ID)
	redCheckin(h, ana, owners.ID)
	h.Clock.Set(time.Date(2026, 1, 12, 18, 0, 0, 0, time.UTC))

	if r := runOffGreen(t, h.Service, plain.ID); r.Weeks != 2 {
		t.Errorf("Weeks in a UTC company, Owner with no Timezone = %d, want 2", r.Weeks)
	}
	if r := runOffGreen(t, laCompany, plain.ID); r.Weeks != 3 {
		t.Errorf("Weeks in a Los Angeles company, Owner with no Timezone = %d, want 3", r.Weeks)
	}
	if r := runOffGreen(t, h.Service, owners.ID); r.Weeks != 3 {
		t.Errorf("Weeks for an Owner in Los Angeles, UTC company = %d, want 3, counted in the Owner's", r.Weeks)
	}
}

// A drafted Report's exception block carries the Goal's Run off Green in
// weeks, read when it is drafted; a Green exception, here an Ownerless Goal,
// carries none.
func TestReportBlockCarriesTheRunOffGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	ownerless := h.ActiveGoal(sam, "Cut churn", "Keep customers.")
	h.Checkin(sam, ownerless.ID, domain.HealthGreen, "On track.", "", time.Time{})
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	// Red on Mondays 5, 12 and 19 January, read on the 19th.
	h.Clock.Set(time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC))
	redCheckin(h, boss, red.ID)
	for range 2 {
		h.Clock.Advance(7 * day)
		redCheckin(h, boss, red.ID)
	}

	r := draftReport(t, h, boss, time.Time{}, red, ownerless)
	weeks := map[int64]int{}
	for _, b := range r.Exceptions {
		weeks[b.Goal.ID] = b.RunOffGreenWeeks
	}
	if len(weeks) != 2 || weeks[red.ID] != 3 || weeks[ownerless.ID] != 0 {
		t.Errorf("RunOffGreenWeeks by Goal = %v, want the Red Goal's 3 and the Ownerless Green Goal's 0", weeks)
	}
}
