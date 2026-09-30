package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

const day = 24 * time.Hour

// An Active Goal with no Check-in is judged from its activation, not its
// creation: a Goal proposed long ago and activated today isn't Stale, and it
// turns Stale only once more than its cadence (7 days by default) has passed
// since activation (CONTEXT.md: Stale).
func TestGoalWithNoCheckinIsStaleOnceItsCadencePassesSinceActivation(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	g := h.CreateGoal(sam, "Ship search", "People can't find things.")
	if _, err := h.Service.MarkGoalDated(ctx, g.ID, h.Clock.Now().AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 3, 0),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	h.Clock.Advance(30 * day)
	if _, err := h.Service.ActivateGoal(ctx, g.ID); err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}

	if f := h.Freshness(g.ID); f.Stale {
		t.Errorf("just-activated Goal is Stale; want it judged from activation, not creation")
	}
	h.Clock.Advance(7 * day)
	if f := h.Freshness(g.ID); f.Stale {
		t.Errorf("Goal is Stale exactly one cadence after activation; want Stale only once older than its cadence")
	}
	h.Clock.Advance(1 * day)
	if f := h.Freshness(g.ID); !f.Stale {
		t.Errorf("Goal is not Stale 8 days after activation with a 7-day cadence")
	}
}

// Each Check-in restarts the count, and the count is against the Goal's own
// cadence: on a 14-day cadence, a Goal last checked in 10 days ago is fresh and
// one last checked in 15 days ago is Stale.
func TestGoalIsStaleWhenItsLastCheckinIsOlderThanItsCadence(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	if _, err := h.Service.SetCadence(context.Background(), g.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Clock.Advance(20 * day)
	checkin := h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Advance(10 * day)
	f := h.Freshness(g.ID)
	if f.Stale {
		t.Errorf("Goal checked in 10 days ago on a 14-day cadence is Stale")
	}
	if !f.LastUpdate.Equal(checkin.CreatedAt) || f.DaysSince != 10 {
		t.Errorf("LastUpdate, DaysSince = %v, %d; want the Check-in at %v, 10 days ago", f.LastUpdate, f.DaysSince, checkin.CreatedAt)
	}
	h.Clock.Advance(5 * day)
	if f := h.Freshness(g.ID); !f.Stale {
		t.Errorf("Goal checked in 15 days ago on a 14-day cadence is not Stale")
	}
}

// An On Hold Goal is never Stale, however long since its last Check-in: nobody
// owes an update on paused work (CONTEXT.md: Stale, Lifecycle).
func TestOnHoldGoalIsNeverStale(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.OnHoldGoal(sam, "Paused work", "It mattered.", "Budget freeze.")
	h.Clock.Advance(90 * day)
	if f := h.Freshness(g.ID); f.Stale {
		t.Errorf("On Hold Goal is Stale after 90 days")
	}
}

// Cadence is counted in days of the org's configured timezone. A Check-in at
// 22:00 on Friday 2 January in Los Angeles (Saturday in UTC) is 8 days old at
// 10:00 on Saturday 10 January there — Stale on a 7-day cadence — although only
// 7 UTC days have passed.
func TestCadenceIsCountedInTheOrgTimezone(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t)
	svc := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 1, 2, 22, 0, 0, 0, la))
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(time.Date(2026, 1, 10, 10, 0, 0, 0, la))
	f, err := svc.Freshness(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("Freshness: %v", err)
	}
	if !f.Stale || f.DaysSince != 8 {
		t.Errorf("Stale, DaysSince = %v, %d in Los Angeles; want Stale, 8 days", f.Stale, f.DaysSince)
	}
	if f := h.Freshness(g.ID); f.Stale {
		t.Errorf("Goal is Stale in UTC after 7 UTC days; want the default timezone to be UTC")
	}
}
