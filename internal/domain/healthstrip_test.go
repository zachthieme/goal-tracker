package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// healthStrip reads goalID's Health strip, failing the test on error.
func healthStrip(t *testing.T, h *testsupport.Harness, goalID int64) domain.HealthStrip {
	t.Helper()
	s, err := h.Service.HealthStrip(context.Background(), goalID)
	if err != nil {
		t.Fatalf("HealthStrip: %v", err)
	}
	return s
}

// date is a calendar date as the strip carries one.
func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// A weekly Goal's strip is its last 11 weeks, Monday to Sunday in the org's
// calendar, oldest first and this week last. A week with a Check-in takes its
// Health; a week the Goal was Active with no Check-in is missed.
func TestHealthStripShowsEachWeeksHealthAndTheWeeksWithNoCheckin(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	// The harness starts on Friday 2 Jan 2026, in the week of Monday 29 Dec.
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	for week := 0; week < 11; week++ {
		if week > 0 {
			h.Clock.Advance(7 * day)
		}
		switch week {
		case 3, 7:
			// No Check-in this week.
		case 5:
			h.Checkin(sam, g.ID, domain.HealthYellow, "Slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 14))
		default:
			h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
		}
	}

	strip := healthStrip(t, h, g.ID)
	if len(strip.Periods) != 11 {
		t.Fatalf("strip has %d periods, want 11", len(strip.Periods))
	}
	if first, last := strip.Periods[0], strip.Periods[10]; !first.First.Equal(date(2025, time.December, 29)) || !last.Last.Equal(date(2026, time.March, 15)) {
		t.Errorf("strip runs %v to %v, want Monday 29 Dec 2025 to Sunday 15 Mar 2026", first.First, last.Last)
	}
	for i, p := range strip.Periods {
		wantFirst := date(2025, time.December, 29).AddDate(0, 0, 7*i)
		if !p.First.Equal(wantFirst) || !p.Last.Equal(wantFirst.AddDate(0, 0, 6)) {
			t.Errorf("period %d runs %v to %v, want the week of %v", i, p.First, p.Last, wantFirst)
		}
		want := domain.HealthPeriod{First: p.First, Last: p.Last, Health: domain.HealthGreen, Lifecycle: domain.LifecycleActive}
		switch i {
		case 3, 7:
			want.Health, want.Missed = "", true
		case 5:
			want.Health = domain.HealthYellow
		}
		if p != want {
			t.Errorf("period %d = %+v, want %+v", i, p, want)
		}
	}
}

// A period with two Check-ins takes the later one's Health, even when both
// were made at the same moment.
func TestHealthStripPeriodTakesItsLastCheckinsHealth(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 0, 14))
	h.Clock.Advance(2 * time.Hour)
	h.Checkin(sam, g.ID, domain.HealthGreen, "Unblocked.", "", time.Time{})
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(sam, g.ID, domain.HealthYellow, "Actually, slipping.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 14))

	strip := healthStrip(t, h, g.ID)
	if got := strip.Periods[9].Health; got != domain.HealthGreen {
		t.Errorf("last week's Health = %q, want Green, its later Check-in's", got)
	}
	if got := strip.Periods[10].Health; got != domain.HealthYellow {
		t.Errorf("this week's Health = %q, want Yellow, the later of two Check-ins made at once", got)
	}
}

// The periods before a Goal became Active aren't missed: nobody owed a
// Check-in on it yet. A Goal activated 3 periods ago, in the week before last,
// has 8 blank weeks, then a missed week, then its Health.
func TestHealthStripLeavesThePeriodsBeforeActivationBlank(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.Clock.Advance(-14 * day)
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	strip := healthStrip(t, h, g.ID)
	for i, p := range strip.Periods[:8] {
		if p.Health != "" || p.Missed || p.Lifecycle != domain.LifecycleProposed {
			t.Errorf("period %d before activation = %+v, want blank, the Goal not yet Active", i, p)
		}
	}
	if p := strip.Periods[8]; !p.Missed || p.Lifecycle != domain.LifecycleActive {
		t.Errorf("activation week = %+v, want missed: Active with no Check-in", p)
	}
	for i := 9; i < 11; i++ {
		if p := strip.Periods[i]; p.Health != domain.HealthGreen || p.Missed {
			t.Errorf("period %d = %+v, want Green", i, p)
		}
	}
}

// The periods a Goal spends On Hold, and those after it is Done, are blank, not
// missed: nobody owes a Check-in on paused or finished work. The Check-in that
// puts it On Hold or marks it Done sets no Health, so its period is blank too.
func TestHealthStripLeavesOnHoldAndDonePeriodsBlank(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	submit := func(in domain.SubmitCheckinInput) {
		t.Helper()
		in.GoalID, in.AuthorID = g.ID, sam.ID
		if _, err := h.Service.SubmitCheckin(context.Background(), in); err != nil {
			t.Fatalf("SubmitCheckin: %v", err)
		}
	}
	for week := 0; week < 11; week++ {
		if week > 0 {
			h.Clock.Advance(7 * day)
		}
		switch week {
		case 1:
			submit(domain.SubmitCheckinInput{Status: "Pausing.", Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Waiting on legal."})
		case 2, 3, 5, 10:
			// No Check-in this week.
		case 4:
			submit(domain.SubmitCheckinInput{Health: domain.HealthGreen, Status: "Resuming.", Lifecycle: domain.LifecycleActive})
		case 9:
			submit(domain.SubmitCheckinInput{Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Search is live."})
		default:
			submit(domain.SubmitCheckinInput{Health: domain.HealthGreen, Status: "On track."})
		}
	}

	strip := healthStrip(t, h, g.ID)
	want := []struct {
		health    string
		missed    bool
		lifecycle string
	}{
		{domain.HealthGreen, false, domain.LifecycleActive},
		{"", false, domain.LifecycleOnHold},
		{"", false, domain.LifecycleOnHold},
		{"", false, domain.LifecycleOnHold},
		{domain.HealthGreen, false, domain.LifecycleActive},
		{"", true, domain.LifecycleActive},
		{domain.HealthGreen, false, domain.LifecycleActive},
		{domain.HealthGreen, false, domain.LifecycleActive},
		{domain.HealthGreen, false, domain.LifecycleActive},
		{"", false, domain.LifecycleDone},
		{"", false, domain.LifecycleDone},
	}
	for i, w := range want {
		p := strip.Periods[i]
		if p.Health != w.health || p.Missed != w.missed || p.Lifecycle != w.lifecycle {
			t.Errorf("week %d = %+v, want Health %q, missed %v, %s", i, p, w.health, w.missed, w.lifecycle)
		}
	}
}

// A Goal on a 14-day cadence has 14-day periods, the current one ending on the
// Sunday that closes this week.
func TestHealthStripPeriodsAreTheGoalsCadenceLong(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.Clock.Advance(-30 * 7 * day)
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	if _, err := h.Service.SetCadence(context.Background(), g.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Clock.Advance(29 * 7 * day)
	h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", h.Clock.Now().AddDate(0, 0, 14))
	h.Clock.Advance(7 * day)
	h.Checkin(sam, g.ID, domain.HealthYellow, "Unblocked, still behind.", "Add a reviewer.", h.Clock.Now().AddDate(0, 0, 14))

	strip := healthStrip(t, h, g.ID)
	if strip.CadenceDays != 14 || len(strip.Periods) != 11 {
		t.Fatalf("strip has %d periods of %d days, want 11 of 14", len(strip.Periods), strip.CadenceDays)
	}
	// Today is Friday 2 Jan 2026, so the current period runs from Monday 22 Dec
	// to Sunday 4 Jan, and the oldest from Monday 4 Aug.
	if p := strip.Periods[10]; !p.First.Equal(date(2025, time.December, 22)) || !p.Last.Equal(date(2026, time.January, 4)) {
		t.Errorf("current period runs %v to %v, want 22 Dec 2025 to 4 Jan 2026", p.First, p.Last)
	}
	if p := strip.Periods[0]; !p.First.Equal(date(2025, time.August, 4)) {
		t.Errorf("oldest period starts %v, want 4 Aug 2025", p.First)
	}
	// Both Check-ins, a week apart, fall in the current period, so it takes the
	// later one's Health and the period before it is missed.
	if p := strip.Periods[10]; p.Health != domain.HealthYellow {
		t.Errorf("current period's Health = %q, want Yellow", p.Health)
	}
	if p := strip.Periods[9]; !p.Missed {
		t.Errorf("period before = %+v, want missed", p)
	}
}

// Periods are counted in days of the org's timezone. A Check-in at 22:00 on
// Sunday 11 January in Los Angeles (Monday in UTC) falls in the week of 5
// January there, leaving the week of 12 January missed.
func TestHealthStripPeriodsAreInTheOrgsTimezone(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t)
	svc := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 1, 11, 22, 0, 0, 0, la))
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(time.Date(2026, 1, 13, 10, 0, 0, 0, la))
	strip, err := svc.HealthStrip(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("HealthStrip: %v", err)
	}
	if p := strip.Periods[9]; !p.First.Equal(date(2026, time.January, 5)) || p.Health != domain.HealthGreen {
		t.Errorf("week before = %+v, want the week of 5 January, Green", p)
	}
	if p := strip.Periods[10]; !p.Missed {
		t.Errorf("this week = %+v, want missed", p)
	}
}

// A Goal that has never been Active has no strip.
func TestProposedGoalHasNoHealthStrip(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Ship search", "People can't find things.")

	if strip := healthStrip(t, h, g.ID); len(strip.Periods) != 0 {
		t.Errorf("Proposed Goal's strip has %d periods, want none", len(strip.Periods))
	}
}
