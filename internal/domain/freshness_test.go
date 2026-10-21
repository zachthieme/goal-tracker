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
// creation: a Goal proposed long ago and activated today isn't Check-in
// overdue, and it turns Check-in overdue only once more than its cadence (7
// days by default) has passed since activation (CONTEXT.md: Check-in overdue).
func TestGoalWithNoCheckinIsCheckinOverdueOnceItsCadencePassesSinceActivation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	g := h.CreateGoal(sam, "Ship search", "People can't find things.")
	if _, err := h.Service.MarkGoalDated(ctx, g.Owner.ID, g.ID, h.Clock.Now().AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.AddMilestone(ctx, g.Owner.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta", TargetDate: h.Clock.Now().AddDate(0, 3, 0),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	h.Clock.Advance(30 * day)
	if _, err := h.Service.ActivateGoal(ctx, g.Owner.ID, g.ID); err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}

	if f := h.Freshness(g.ID); f.CheckinOverdue {
		t.Errorf("just-activated Goal is Check-in overdue; want it judged from activation, not creation")
	}
	h.Clock.Advance(7 * day)
	if f := h.Freshness(g.ID); f.CheckinOverdue {
		t.Errorf("Goal is Check-in overdue exactly one cadence after activation; want Check-in overdue only once older than its cadence")
	}
	h.Clock.Advance(1 * day)
	if f := h.Freshness(g.ID); !f.CheckinOverdue {
		t.Errorf("Goal is not Check-in overdue 8 days after activation with a 7-day cadence")
	}
}

// Each Check-in restarts the count, and the count is against the Goal's own
// cadence: on a 14-day cadence, a Goal last checked in 10 days ago is fresh and
// one last checked in 15 days ago is Check-in overdue.
func TestGoalIsCheckinOverdueWhenItsLastCheckinIsOlderThanItsCadence(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Grow revenue", "It pays for everything.")
	if _, err := h.Service.SetCadence(context.Background(), g.Owner.ID, g.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	h.Clock.Advance(20 * day)
	checkin := h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Advance(10 * day)
	f := h.Freshness(g.ID)
	if f.CheckinOverdue {
		t.Errorf("Goal checked in 10 days ago on a 14-day cadence is Check-in overdue")
	}
	if !f.LastUpdate.Equal(checkin.CreatedAt) || f.DaysSince != 10 {
		t.Errorf("LastUpdate, DaysSince = %v, %d; want the Check-in at %v, 10 days ago", f.LastUpdate, f.DaysSince, checkin.CreatedAt)
	}
	h.Clock.Advance(5 * day)
	if f := h.Freshness(g.ID); !f.CheckinOverdue {
		t.Errorf("Goal checked in 15 days ago on a 14-day cadence is not Check-in overdue")
	}
}

// An On Hold Goal is never Check-in overdue, however long since its last
// Check-in: nobody owes an update on paused work (CONTEXT.md: Check-in overdue,
// Lifecycle).
func TestOnHoldGoalIsNeverCheckinOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.OnHoldGoal(sam, "Paused work", "It mattered.", "Budget freeze.")
	h.Clock.Advance(90 * day)
	if f := h.Freshness(g.ID); f.CheckinOverdue {
		t.Errorf("On Hold Goal is Check-in overdue after 90 days")
	}
}

// Cadence is counted in days of the org's configured timezone. A Check-in at
// 22:00 on Friday 2 January in Los Angeles (Saturday in UTC) is 8 days old at
// 10:00 on Saturday 10 January there — Check-in overdue on a 7-day cadence —
// although only 7 UTC days have passed.
func TestCadenceIsCountedInTheOrgTimezone(t *testing.T) {
	t.Parallel()

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
	if !f.CheckinOverdue || f.DaysSince != 8 {
		t.Errorf("Check-in overdue, DaysSince = %v, %d in Los Angeles; want Check-in overdue, 8 days", f.CheckinOverdue, f.DaysSince)
	}
	if f := h.Freshness(g.ID); f.CheckinOverdue {
		t.Errorf("Goal is Check-in overdue in UTC after 7 UTC days; want the default timezone to be UTC")
	}
}

// Check-in overdue is counted in days of the Owner's Timezone, not the
// company's (CONTEXT.md: Check-in overdue): a Check-in late on Jan 2 in UTC is
// Jan 3 in Tokyo, so a Tokyo Owner's Goal is 7 days on, not 8, and isn't yet
// Check-in overdue. An Owner with no Timezone is counted in the company's.
func TestCheckinOverdueIsCountedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	kenji := h.SignIn("kenji@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	g := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 1, 2, 16, 0, 0, 0, time.UTC)) // Jan 3, 1am in Tokyo
	h.Checkin(kenji, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC))
	if f := h.Freshness(g.ID); f.CheckinOverdue || f.DaysSince != 7 {
		t.Errorf("Check-in overdue, DaysSince = %v, %d for an Owner in Tokyo; want not Check-in overdue, 7 days", f.CheckinOverdue, f.DaysSince)
	}
	h.SetTimezone(kenji, "")
	if f := h.Freshness(g.ID); !f.CheckinOverdue || f.DaysSince != 8 {
		t.Errorf("Check-in overdue, DaysSince = %v, %d for an Owner with no Timezone; want Check-in overdue, 8 days in the company's", f.CheckinOverdue, f.DaysSince)
	}
}

// A Path to Green's target date passes in the Owner's Timezone (CONTEXT.md:
// Timezone): at 4pm UTC on its target date it is already the next day in
// Tokyo, so a Tokyo Owner's Path to Green is overdue, and listed so, while one
// whose Owner has no Timezone isn't yet.
func TestPathToGreenGoesOverdueInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	kenji := h.SignIn("kenji@example.com")
	sam := h.SignIn("sam@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	tokyo := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	plain := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	target := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	h.Checkin(kenji, tokyo.ID, domain.HealthRed, "Blocked.", "Escalate to the vendor.", target)
	h.Checkin(sam, plain.ID, domain.HealthRed, "Blocked.", "Escalate to the vendor.", target)

	h.Clock.Set(time.Date(2026, 1, 20, 16, 0, 0, 0, time.UTC)) // Jan 21, 1am in Tokyo
	if f := h.Freshness(tokyo.ID); !f.PathToGreenOverdue {
		t.Errorf("Path to Green of an Owner in Tokyo isn't overdue the day after its target there")
	}
	if f := h.Freshness(plain.ID); f.PathToGreenOverdue {
		t.Errorf("Path to Green of an Owner with no Timezone is overdue on its target date in the company's")
	}
	signals, err := h.Service.FreshnessSignals(context.Background())
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	if len(signals.OverduePaths) != 1 || signals.OverduePaths[0].Goal.ID != tokyo.ID {
		t.Errorf("OverduePaths = %+v, want only the Tokyo Owner's Goal", signals.OverduePaths)
	}
}

// A Goal is flagged once its Path to Green's target date has passed and it
// still isn't Green (CONTEXT.md: Path to Green). The target date itself is not
// yet overdue, and a Check-in back at Green clears the flag.
func TestPathToGreenIsOverdueOnceItsTargetDatePassesWithoutGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	target := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate to the vendor.", target)

	h.Clock.Set(time.Date(2026, 1, 20, 23, 0, 0, 0, time.UTC))
	if f := h.Freshness(g.ID); f.PathToGreenOverdue {
		t.Errorf("Path to Green is overdue on its own target date")
	}
	h.Clock.Set(time.Date(2026, 1, 21, 1, 0, 0, 0, time.UTC))
	h.Checkin(sam, g.ID, domain.HealthYellow, "Improving.", "Escalate to the vendor.", target)
	f := h.Freshness(g.ID)
	if !f.PathToGreenOverdue || !f.PathTargetDate.Equal(target) {
		t.Errorf("PathToGreenOverdue, PathTargetDate = %v, %v the day after the target while Yellow; want overdue, %v", f.PathToGreenOverdue, f.PathTargetDate, target)
	}
	h.Checkin(sam, g.ID, domain.HealthGreen, "Recovered.", "", time.Time{})
	if f := h.Freshness(g.ID); f.PathToGreenOverdue {
		t.Errorf("Goal back at Green is still flagged with an overdue Path to Green")
	}
}

// Rolled-up Health counts how many of a Goal's Active children are Check-in
// overdue ("2 of 3 overdue") without letting it change the color: two
// Check-in overdue Green children and a fresh Green one still roll up Green. An
// On Hold child is neither Check-in overdue nor counted.
func TestRolledUpHealthCountsCheckinOverdueChildrenWithoutChangingTheColor(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Org outcome", "It matters.")
	quiet := h.ActiveChildOf(sam, parent, "Quiet work", "Quiet why it matters.")
	h.Checkin(sam, quiet.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveChildOf(sam, parent, "Never checked in", "Silent why it matters.")
	paused := h.OnHoldGoal(sam, "Paused work", "Paused why it matters.", "Budget freeze.")
	h.RequestLink(sam, paused, parent, "")

	h.Clock.Advance(10 * day)
	fresh := h.ActiveChildOf(sam, parent, "Fresh work", "Fresh why it matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})

	got, err := h.Service.RolledUpHealth(context.Background(), parent.ID)
	if err != nil {
		t.Fatalf("RolledUpHealth: %v", err)
	}
	if !got.Present || got.Health != domain.HealthGreen {
		t.Errorf("Rolled-up Health = %q (present %v), want Green: Check-in overdue children don't change the color", got.Health, got.Present)
	}
	if got.CheckinOverdueChildren != 2 || got.ActiveChildren != 3 {
		t.Errorf("Check-in overdue children = %d of %d, want 2 of 3", got.CheckinOverdueChildren, got.ActiveChildren)
	}
}

// The org-wide freshness signals list every Check-in overdue Goal,
// longest-silent first, and every Goal whose Path to Green is overdue. Fresh
// Goals, On Hold Goals, and Proposed Goals aren't listed.
func TestFreshnessSignalsListCheckinOverdueGoalsAndOverduePaths(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	silent := h.ActiveGoal(sam, "Silent since activation", "It matters.")
	h.OnHoldGoal(sam, "Paused work", "It mattered.", "Budget freeze.")
	h.CreateGoal(sam, "Still an idea", "Maybe.")
	h.Clock.Advance(3 * day)
	lapsed := h.ActiveGoal(sam, "Lapsed", "It matters.")
	h.Checkin(sam, lapsed.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Advance(9 * day)
	fresh := h.ActiveGoal(sam, "Fresh", "It matters.")
	h.Checkin(sam, fresh.ID, domain.HealthGreen, "On track.", "", time.Time{})
	stalled := h.ActiveGoal(sam, "Stalled recovery", "It matters.")
	h.Checkin(sam, stalled.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))

	got, err := h.Service.FreshnessSignals(context.Background())
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	var checkinOverdue []string
	for _, gf := range got.CheckinOverdue {
		checkinOverdue = append(checkinOverdue, gf.Goal.Title)
	}
	if want := []string{"Silent since activation", "Lapsed"}; !equalStrings(checkinOverdue, want) {
		t.Errorf("Check-in overdue Goals = %q, want %q (longest-silent first)", checkinOverdue, want)
	}
	if len(got.OverduePaths) != 1 || got.OverduePaths[0].Goal.ID != stalled.ID {
		t.Errorf("overdue Paths to Green = %+v, want only %q", got.OverduePaths, stalled.Title)
	}
	if !got.CheckinOverdue[0].Freshness.LastUpdate.Equal(silent.ActivatedAt) || got.CheckinOverdue[0].Freshness.DaysSince != 12 {
		t.Errorf("Silent Goal's last update, days since = %v, %d; want its activation %v, 12 days", got.CheckinOverdue[0].Freshness.LastUpdate, got.CheckinOverdue[0].Freshness.DaysSince, silent.ActivatedAt)
	}
}

// The org-wide freshness signals list every Active Goal whose latest Check-in
// is Red or Yellow as Not Green, each with that Health. A Green Goal and one
// with no Check-in aren't listed.
func TestFreshnessSignalsListNotGreenGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	later := h.Clock.Now().AddDate(0, 1, 0)
	red := h.ActiveGoal(sam, "Red goal", "It matters.")
	h.Checkin(sam, red.ID, domain.HealthRed, "Blocked.", "Escalate.", later)
	yellow := h.ActiveGoal(sam, "Yellow goal", "It matters.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Slipping.", "Add a team.", later)
	green := h.ActiveGoal(sam, "Green goal", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.ActiveGoal(sam, "Never checked in", "It matters.")

	got, err := h.Service.FreshnessSignals(context.Background())
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	health := map[int64]string{}
	for _, gf := range got.NotGreen {
		health[gf.Goal.ID] = gf.Health
	}
	want := map[int64]string{red.ID: domain.HealthRed, yellow.ID: domain.HealthYellow}
	if len(health) != len(got.NotGreen) || len(health) != len(want) || health[red.ID] != want[red.ID] || health[yellow.ID] != want[yellow.ID] {
		t.Errorf("Not Green = %+v, want only %q Red and %q Yellow", got.NotGreen, red.Title, yellow.Title)
	}
}

// Resuming an On Hold Goal is a Check-in, so the count restarts from the resume,
// not from the Check-in before the Goal was paused.
func TestResumedGoalIsJudgedFromItsResume(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.OnHoldGoal(sam, "Paused work", "It mattered.", "Budget freeze.")
	h.Clock.Advance(60 * day)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Back on.", Lifecycle: domain.LifecycleActive,
	}); err != nil {
		t.Fatalf("SubmitCheckin resume: %v", err)
	}
	h.Clock.Advance(3 * day)
	if f := h.Freshness(g.ID); f.CheckinOverdue || f.DaysSince != 3 {
		t.Errorf("Check-in overdue, DaysSince = %v, %d three days after resuming; want fresh, 3", f.CheckinOverdue, f.DaysSince)
	}
}

// A Done Goal owes no more updates, so it is never Check-in overdue, and a Red
// Path to Green it left behind is never flagged.
func TestDoneGoalHasNoCheckinOverdueNorPathOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate.", time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: g.ID, AuthorID: sam.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Search shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin Done: %v", err)
	}
	h.Clock.Advance(90 * day)
	if f := h.Freshness(g.ID); f.CheckinOverdue || f.PathToGreenOverdue {
		t.Errorf("Done Goal: Check-in overdue %v, Path to Green overdue %v; want neither", f.CheckinOverdue, f.PathToGreenOverdue)
	}
}

// Days are calendar days in the org's timezone, so a daylight-saving change
// inside the cadence neither adds nor loses one: Los Angeles springs forward on
// 8 March 2026, and a Check-in at 09:00 on 2 March is exactly 7 days old at
// 08:00 on 9 March — not yet Check-in overdue — though fewer than 7×24 hours
// have passed.
func TestCadenceCountsCalendarDaysAcrossDaylightSaving(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t)
	svc := domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.Clock.Set(time.Date(2026, 3, 2, 9, 0, 0, 0, la))
	h.Checkin(sam, g.ID, domain.HealthGreen, "On track.", "", time.Time{})

	for _, tc := range []struct {
		now                time.Time
		days               int
		wantCheckinOverdue bool
	}{
		{time.Date(2026, 3, 9, 8, 0, 0, 0, la), 7, false},
		{time.Date(2026, 3, 10, 0, 30, 0, 0, la), 8, true},
	} {
		h.Clock.Set(tc.now)
		f, err := svc.Freshness(context.Background(), g.ID)
		if err != nil {
			t.Fatalf("Freshness: %v", err)
		}
		if f.CheckinOverdue != tc.wantCheckinOverdue || f.DaysSince != tc.days {
			t.Errorf("at %v: Check-in overdue, DaysSince = %v, %d; want %v, %d", tc.now, f.CheckinOverdue, f.DaysSince, tc.wantCheckinOverdue, tc.days)
		}
	}
}

// An Active Goal whose latest Check-in is Green while a Milestone is still
// Planned past its date is marked Milestone overdue, naming that Milestone, and
// listed so; the Health stays the Owner's, and the Goal isn't Check-in overdue.
func TestGreenGoalWithAnOverdueMilestoneIsMarkedMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g, beta := h.GreenGoalWithOverdueMilestone(sam, "Ship search")

	f := h.Freshness(g.ID)
	if !f.MilestoneOverdue || f.OverdueMilestone != "Beta" || !f.OverdueMilestoneDate.Equal(beta.TargetDate) || f.CheckinOverdue {
		t.Errorf("MilestoneOverdue, OverdueMilestone, OverdueMilestoneDate, Check-in overdue = %v, %q, %v, %v; want true, \"Beta\", %v, false",
			f.MilestoneOverdue, f.OverdueMilestone, f.OverdueMilestoneDate, f.CheckinOverdue, beta.TargetDate)
	}
	got, err := h.Service.FreshnessSignals(context.Background())
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	if len(got.OverdueMilestones) != 1 || got.OverdueMilestones[0].Goal.ID != g.ID || got.OverdueMilestones[0].Freshness.OverdueMilestone != "Beta" {
		t.Errorf("OverdueMilestones = %+v, want only %q, its Beta overdue", got.OverdueMilestones, g.Title)
	}
	if len(got.CheckinOverdue) != 0 {
		t.Errorf("Check-in overdue = %+v, want none", got.CheckinOverdue)
	}
}

// Only a Green Goal is marked Milestone overdue: a Yellow one with the same
// overdue Milestone isn't, nor is a Green one whose Milestone is due today,
// not before, nor an On Hold one. None is listed.
func TestOnlyAnActiveGreenGoalPastAMilestoneIsMarkedMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()

	paused, _ := h.GreenGoalWithOverdueMilestone(sam, "Paused work")
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID: paused.ID, AuthorID: sam.ID, Status: "Putting this On Hold.",
		Lifecycle: domain.LifecycleOnHold, LifecycleReason: "Budget freeze.",
	}); err != nil {
		t.Fatalf("SubmitCheckin On Hold: %v", err)
	}
	yellow, _ := h.GreenGoalWithOverdueMilestone(sam, "Slipping work")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Beta slipped.", "Move Beta.", h.Clock.Now().AddDate(0, 1, 0))

	dueToday := h.ActiveGoal(sam, "Due today", "It matters.")
	milestones, err := h.Service.ListMilestones(ctx, dueToday.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	h.Clock.Set(milestones[0].TargetDate.Add(9 * time.Hour))
	h.Checkin(sam, dueToday.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Clock.Set(milestones[0].TargetDate.Add(23 * time.Hour))

	for _, g := range []domain.Goal{paused, yellow, dueToday} {
		if f := h.Freshness(g.ID); f.MilestoneOverdue {
			t.Errorf("%q is marked Milestone overdue (%q)", g.Title, f.OverdueMilestone)
		}
	}
	got, err := h.Service.FreshnessSignals(ctx)
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	if len(got.OverdueMilestones) != 0 {
		t.Errorf("OverdueMilestones = %+v, want none", got.OverdueMilestones)
	}
}

// A Milestone's date passes in the Goal's Owner's Timezone (CONTEXT.md:
// Timezone), as the Check-in refusals judge it: at 4pm UTC on Beta's date it is
// already the next day in Tokyo, so a Tokyo Owner's Green Goal is marked and
// listed, while one whose Owner has no Timezone isn't yet. With several
// overdue, the earliest-dated one counts, and the list runs earliest first.
func TestMilestoneOverdueIsJudgedInTheOwnersTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	kenji := h.SignIn("kenji@example.com")
	sam := h.SignIn("sam@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	beta := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	tokyo := h.CreateGoal(kenji, "Ship search", "People can't find things.")
	plain := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	for _, g := range []domain.Goal{tokyo, plain} {
		if _, err := h.Service.MarkGoalDated(ctx, g.Owner.ID, g.ID, beta.AddDate(0, 6, 0)); err != nil {
			t.Fatalf("MarkGoalDated: %v", err)
		}
		if _, err := h.Service.AddMilestone(ctx, g.Owner.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Beta", TargetDate: beta}); err != nil {
			t.Fatalf("AddMilestone: %v", err)
		}
		if _, err := h.Service.ActivateGoal(ctx, g.Owner.ID, g.ID); err != nil {
			t.Fatalf("ActivateGoal: %v", err)
		}
	}
	if _, err := h.Service.AddMilestone(ctx, plain.Owner.ID, domain.AddMilestoneInput{GoalID: plain.ID, Name: "Alpha", TargetDate: beta.AddDate(0, 0, -3)}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	h.Clock.Set(beta.AddDate(0, 0, -5))
	h.Checkin(kenji, tokyo.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(sam, plain.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(beta.Add(16 * time.Hour)) // Jan 21, 1am in Tokyo
	if f := h.Freshness(tokyo.ID); !f.MilestoneOverdue || f.OverdueMilestone != "Beta" {
		t.Errorf("Tokyo Owner's Goal: MilestoneOverdue, OverdueMilestone = %v, %q; want Beta overdue the day after its date there", f.MilestoneOverdue, f.OverdueMilestone)
	}
	if f := h.Freshness(plain.ID); f.OverdueMilestone != "Alpha" || !f.OverdueMilestoneDate.Equal(beta.AddDate(0, 0, -3)) {
		t.Errorf("Goal of an Owner with no Timezone: OverdueMilestone, OverdueMilestoneDate = %q, %v; want Alpha, the earliest overdue, not Beta, due today in the company's",
			f.OverdueMilestone, f.OverdueMilestoneDate)
	}
	got, err := h.Service.FreshnessSignals(ctx)
	if err != nil {
		t.Fatalf("FreshnessSignals: %v", err)
	}
	var titles []string
	for _, gf := range got.OverdueMilestones {
		titles = append(titles, gf.Goal.Title+": "+gf.Freshness.OverdueMilestone)
	}
	if want := []string{"Reduce outages: Alpha", "Ship search: Beta"}; !equalStrings(titles, want) {
		t.Errorf("OverdueMilestones = %q, want %q (earliest Milestone first)", titles, want)
	}
}

// Moving a passed back-to-Green date later, with the reason that Date Slip
// needs, clears "Path to Green overdue": the flag reads the latest date.
func TestMovingThePassedBackToGreenDateLaterClearsPathToGreenOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	target := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	h.Checkin(sam, g.ID, domain.HealthRed, "Blocked.", "Escalate to the vendor.", target)

	h.Clock.Set(time.Date(2026, 1, 22, 9, 0, 0, 0, time.UTC))
	if f := h.Freshness(g.ID); !f.PathToGreenOverdue {
		t.Fatalf("Path to Green isn't overdue two days after its date")
	}
	later := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	h.CheckinMovingPath(sam, g.ID, domain.HealthRed, "Still blocked.", "Escalate to the vendor.", later, "The vendor is out until February.")
	if f := h.Freshness(g.ID); f.PathToGreenOverdue || !f.PathTargetDate.Equal(later) {
		t.Errorf("PathToGreenOverdue, PathTargetDate = %v, %v after moving the date; want not overdue, %v", f.PathToGreenOverdue, f.PathTargetDate, later)
	}
}
