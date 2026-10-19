package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// An Owner submits a Green Check-in on an Active Goal: it records the Health, the
// status, its author and the Owner it was written for, and becomes the Goal's
// latest Check-in (CONTEXT.md: Check-in).
func TestSubmitCheckinRecordsHealthAuthorAndOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track for the beta.",
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.Health != domain.HealthGreen {
		t.Errorf("Health = %q, want %q", c.Health, domain.HealthGreen)
	}
	if c.Status != "On track for the beta." {
		t.Errorf("Status = %q, want the submitted status", c.Status)
	}
	if c.Author.ID != sam.ID {
		t.Errorf("Author = %d, want %d", c.Author.ID, sam.ID)
	}
	if c.Owner.ID != sam.ID {
		t.Errorf("Owner = %d, want %d", c.Owner.ID, sam.ID)
	}

	latest, ok, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("LatestCheckin: %v", err)
	}
	if !ok {
		t.Fatalf("LatestCheckin ok = false, want the Check-in just submitted")
	}
	if latest.ID != c.ID {
		t.Errorf("LatestCheckin ID = %d, want %d", latest.ID, c.ID)
	}
}

var pathDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

// A Proposed Goal has no Health and can't be checked in on (CONTEXT.md: Health
// is how an Active Goal is tracking).
func TestSubmitCheckinRejectedOnProposedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.") // stays Proposed

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Looks fine.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if _, ok, _ := h.Service.LatestCheckin(context.Background(), goal.ID); ok {
		t.Errorf("a Check-in was recorded on a Proposed Goal")
	}
}

// A Yellow or Red Check-in requires a Path to Green with both text and a target
// date (CONTEXT.md: Path to Green).
func TestSubmitCheckinYellowOrRedRequiresPathToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	for _, health := range []string{domain.HealthYellow, domain.HealthRed} {
		// Missing the Path text.
		_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:         goal.ID,
			AuthorID:       sam.ID,
			Health:         health,
			Status:         "Slipping.",
			PathTargetDate: pathDate,
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s without a Path text: err = %v, want ErrValidation", health, err)
		}
		// Missing the target date.
		_, err = h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
			GoalID:      goal.ID,
			AuthorID:    sam.ID,
			Health:      health,
			Status:      "Slipping.",
			PathToGreen: "Add two engineers.",
		})
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s without a target date: err = %v, want ErrValidation", health, err)
		}
	}

	// With both, it is accepted and keeps the Path.
	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthRed,
		Status:         "Slipping.",
		PathToGreen:    "Add two engineers.",
		PathTargetDate: pathDate,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin with a Path to Green: %v", err)
	}
	if c.PathToGreen != "Add two engineers." || !c.PathTargetDate.Equal(pathDate) {
		t.Errorf("Path to Green not recorded: %q / %v", c.PathToGreen, c.PathTargetDate)
	}
}

// A Green Check-in needs no Path to Green, and any Path text passed with it is
// dropped since Green carries none.
func TestSubmitCheckinGreenDropsPathToGreen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	c, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthGreen,
		Status:         "All good.",
		PathToGreen:    "leftover text",
		PathTargetDate: pathDate,
	})
	if err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	if c.PathToGreen != "" || !c.PathTargetDate.IsZero() {
		t.Errorf("Green Check-in kept a Path to Green: %q / %v", c.PathToGreen, c.PathTargetDate)
	}
}

// A Check-in needs a status.
func TestSubmitCheckinRequiresStatus(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "   ",
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// Only the Goal's Owner may submit a Check-in.
func TestSubmitCheckinOnlyByOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: pat.ID,
		Health:   domain.HealthGreen,
		Status:   "Meddling.",
	}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Fatalf("err = %v, want ErrNotAuthorized", err)
	}
}

// Check-ins are immutable and kept as history: the latest carries the Goal's
// current Health, status, and Path to Green, and ListCheckins returns the whole
// history newest first (CONTEXT.md: current values come from the latest
// Check-in).
func TestCheckinsAreImmutableHistoryWithLatestCurrent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	first := h.Checkin(sam, goal.ID, domain.HealthGreen, "Kicking off.", "", time.Time{})
	h.Clock.Advance(24 * time.Hour)
	second := h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked on vendor.", "Escalate to vendor.", pathDate)

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (Check-ins are immutable, never replaced)", len(history))
	}
	if history[0].ID != second.ID || history[1].ID != first.ID {
		t.Errorf("history order = [%d, %d], want newest first [%d, %d]", history[0].ID, history[1].ID, second.ID, first.ID)
	}

	latest, ok, err := h.Service.LatestCheckin(context.Background(), goal.ID)
	if err != nil || !ok {
		t.Fatalf("LatestCheckin: %v, ok=%v", err, ok)
	}
	if latest.Health != domain.HealthRed || latest.Status != "Blocked on vendor." || latest.PathToGreen != "Escalate to vendor." {
		t.Errorf("current values not from the latest Check-in: %+v", latest)
	}
}

// A Check-in records the current value of each Metric on the Goal, and a
// Metric's readings come back over time earliest-first for its trend against
// target (CONTEXT.md: Metric - its current value is recorded at each Check-in).
func TestSubmitCheckinRecordsMetricReadings(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")

	latency, err := h.Service.AddMetric(context.Background(), validMetric(goal.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Started measuring.",
		Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 900}},
	}); err != nil {
		t.Fatalf("SubmitCheckin with reading: %v", err)
	}
	h.Clock.Advance(7 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Improving.",
		Readings: []domain.MetricReadingInput{{MetricID: latency.ID, Value: 700}},
	}); err != nil {
		t.Fatalf("second SubmitCheckin with reading: %v", err)
	}

	readings, err := h.Service.ListMetricReadings(context.Background(), latency.ID)
	if err != nil {
		t.Fatalf("ListMetricReadings: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("readings = %d, want 2 recorded over two Check-ins", len(readings))
	}
	if readings[0].Value != 900 || readings[1].Value != 700 {
		t.Errorf("trend = [%v, %v], want earliest-first [900, 700]", readings[0].Value, readings[1].Value)
	}
}

// A reading for a Metric that is not on the Goal being checked in on is rejected,
// and the whole Check-in is refused (nothing is recorded).
func TestSubmitCheckinRejectsReadingForForeignMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut latency", "Faster checkout lifts conversion.")
	other := h.ActiveGoal(sam, "Grow signups", "More signups, more revenue.")
	foreign, err := h.Service.AddMetric(context.Background(), validMetric(other.ID))
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Mislabelled reading.",
		Readings: []domain.MetricReadingInput{{MetricID: foreign.ID, Value: 1}},
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded despite the foreign-metric reading: %d", len(history))
	}
}

// One-click "no change" records a Check-in that repeats the previous values,
// author and Owner it is written for aside.
func TestNoChangeCheckinRepeatsPreviousValues(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	prev := h.Checkin(sam, goal.ID, domain.HealthYellow, "Recovering.", "Ship the hotfix.", pathDate)
	h.Clock.Advance(24 * time.Hour)

	repeat, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
	if err != nil {
		t.Fatalf("SubmitNoChangeCheckin: %v", err)
	}
	if repeat.ID == prev.ID {
		t.Errorf("no-change reused the previous Check-in instead of recording a new one")
	}
	if repeat.Health != prev.Health || repeat.Status != prev.Status ||
		repeat.PathToGreen != prev.PathToGreen || !repeat.PathTargetDate.Equal(prev.PathTargetDate) {
		t.Errorf("no-change did not repeat previous values: got %+v, prev %+v", repeat, prev)
	}
}

// "No change" needs a previous Check-in to repeat; on a Goal with none it is
// rejected.
func TestNoChangeCheckinRejectedWithoutPrevious(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// A slip is never hidden behind a Green: a Check-in that moves the delivery date
// later can't be Green, while one that pulls it in can (CONTEXT.md: Date Slip).
func TestGreenRejectedWhenDeliveryDateMovesLater(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthGreen,
		Status:             "Still fine.",
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, 7),
		DeliveryDateReason: "Vendor slipped.",
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Green with a later delivery date: err = %v, want ErrValidation", err)
	}
	if slips, _ := h.Service.ListDateSlips(context.Background(), goal.ID); len(slips) != 0 {
		t.Errorf("recorded %d Date Slips despite the rejection", len(slips))
	}

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthGreen,
		Status:             "Ahead of plan.",
		DeliveryDate:       goal.DeliveryDate.AddDate(0, 0, -7),
		DeliveryDateReason: "Beta went faster than planned.",
	}); err != nil {
		t.Errorf("Green with an earlier delivery date: %v", err)
	}
}

// Green is rejected while any Milestone is overdue — Planned and past its date —
// judged by the Service's clock. On the Milestone's own date it is not yet
// overdue.
func TestGreenRejectedWhileMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	green := domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	}

	h.Clock.Set(beta.TargetDate.Add(23 * time.Hour))
	if _, err := h.Service.SubmitCheckin(context.Background(), green); err != nil {
		t.Fatalf("Green on the Milestone's own date: %v", err)
	}

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	_, err := h.Service.SubmitCheckin(context.Background(), green)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Green with an overdue Milestone: err = %v, want ErrValidation", err)
	}
	assertMilestoneOverdueError(t, err, beta)
	if want := `validation failed: Milestone "Beta" is overdue (due ` + beta.TargetDate.Format("2006-01-02") + `), so the Goal can't be Green`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}

	// Yellow is still accepted, with its Path to Green.
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Beta is late.",
		PathToGreen:    "Cut scope.",
		PathTargetDate: pathDate,
	}); err != nil {
		t.Fatalf("Yellow with an overdue Milestone: %v", err)
	}
}

// Slipping the overdue Milestone to a future date in the same Check-in means it
// is no longer overdue, and a Milestone slip doesn't affect Health, so Green is
// accepted.
func TestGreenAcceptedWhenOverdueMilestoneSlipsInSameCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 2))

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Beta re-planned; launch holds.",
		Milestones: []domain.MilestoneChangeInput{
			{MilestoneID: beta.ID, TargetDate: beta.TargetDate.AddDate(0, 0, 14), DateReason: "Design review moved."},
		},
	}); err != nil {
		t.Fatalf("Green after slipping the overdue Milestone: %v", err)
	}
}

// The one-click no-change Check-in can't repeat a Green while a Milestone is
// overdue, and says how to respond to the missed date.
func TestNoChangeGreenRejectedWhileMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	_, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("no-change Green with an overdue Milestone: err = %v, want ErrValidation", err)
	}
	assertOverdueNoChangeRefusal(t, err)
	assertMilestoneOverdueError(t, err, beta)
	if want := `validation failed: Milestone "Beta" is overdue (due ` + beta.TargetDate.Format("2006-01-02") + `): submit a Check-in to move its date, mark it Done or remove it`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

// assertMilestoneOverdueError checks err is a refusal for the overdue
// Milestone m that a caller can find with errors.As, naming m and its date.
func assertMilestoneOverdueError(t *testing.T, err error, m domain.Milestone) {
	t.Helper()
	var overdue *domain.MilestoneOverdueError
	if !errors.As(err, &overdue) {
		t.Fatalf("err = %v, want a *MilestoneOverdueError", err)
	}
	if overdue.Milestone.ID != m.ID || overdue.Milestone.Name != m.Name || !overdue.Milestone.TargetDate.Equal(m.TargetDate) {
		t.Errorf("overdue Milestone = %+v, want %+v", overdue.Milestone, m)
	}
}

// A No change can't repeat a Yellow or Red either while a Milestone is overdue:
// the missed date needs a response, so the refusal says what to do with it.
func TestNoChangeRefusedWhileMilestoneOverdueWhateverTheHealth(t *testing.T) {
	t.Parallel()

	for _, health := range []string{domain.HealthYellow, domain.HealthRed} {
		t.Run(health, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			sam := h.SignIn("sam@example.com")
			goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
			beta := onlyMilestone(t, h, goal.ID)
			h.Checkin(sam, goal.ID, health, "Behind.", "Add a second reviewer.", beta.TargetDate.AddDate(0, 1, 0))

			h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
			_, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("no-change %s with an overdue Milestone: err = %v, want ErrValidation", health, err)
			}
			assertOverdueNoChangeRefusal(t, err)
			if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 1 {
				t.Errorf("got %d Check-ins, want only the original one", len(history))
			}
		})
	}
}

// A full Yellow Check-in may still leave a Milestone overdue: only a Green or a
// No change is refused for it.
func TestYellowCheckinMayLeaveMilestoneOverdue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	beta := onlyMilestone(t, h, goal.ID)

	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Beta is late.",
		PathToGreen:    "Add a second reviewer.",
		PathTargetDate: beta.TargetDate.AddDate(0, 1, 0),
	}); err != nil {
		t.Fatalf("Yellow leaving an overdue Milestone: %v", err)
	}
}

// NoChangeRefusal answers, without recording anything, whether a No change
// would be refused and why: nil when it would be recorded, otherwise the
// refusal SubmitNoChangeCheckin gives.
func TestNoChangeRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("allowed", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})

		if err := h.Service.NoChangeRefusal(ctx, goal.ID); err != nil {
			t.Fatalf("NoChangeRefusal = %v, want nil", err)
		}
	})
	t.Run("no previous Check-in", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

		if err := h.Service.NoChangeRefusal(ctx, goal.ID); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
	})
	t.Run("not Active", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.OnHoldGoal(sam, "Ship v2", "Customers wait too long.", "Waiting on the vendor.")

		if err := h.Service.NoChangeRefusal(ctx, goal.ID); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
	})
	t.Run("overdue Milestone", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
		beta := onlyMilestone(t, h, goal.ID)
		h.Checkin(sam, goal.ID, domain.HealthRed, "Behind.", "Add a second reviewer.", beta.TargetDate.AddDate(0, 1, 0))

		h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))
		err := h.Service.NoChangeRefusal(ctx, goal.ID)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
		assertOverdueNoChangeRefusal(t, err)
	})
	t.Run("off-target Green", func(t *testing.T) {
		t.Parallel()
		h := testsupport.New(t)
		sam := h.SignIn("sam@example.com")
		goal, _ := offTargetMetricGoal(t, h, sam)

		err := h.Service.NoChangeRefusal(ctx, goal.ID)
		if !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("NoChangeRefusal = %v, want ErrValidation", err)
		}
		var offTarget *domain.GreenOffTargetError
		if !errors.As(err, &offTarget) {
			t.Fatalf("NoChangeRefusal = %v, want a *GreenOffTargetError", err)
		}
	})
}

// assertOverdueNoChangeRefusal checks a No change refused for the overdue Beta
// Milestone names it and says how to respond to the missed date.
func assertOverdueNoChangeRefusal(t *testing.T, err error) {
	t.Helper()
	for _, want := range []string{`Milestone "Beta" is overdue`, "move its date, mark it Done or remove it"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

// A Milestone added in the Check-in with a date already past is overdue at once,
// so the same Check-in can't be Green.
func TestGreenRejectedWhenAddedMilestoneIsAlreadyPast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:        goal.ID,
		AuthorID:      sam.ID,
		Health:        domain.HealthGreen,
		Status:        "Backfilling a gate.",
		NewMilestones: []domain.NewMilestoneInput{{Name: "Kickoff", TargetDate: h.Clock.Now().AddDate(0, 0, -3)}},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// offTargetMetricGoal arranges an Active Goal whose Idle capacity Metric (down,
// target 20 by Jan 30) last read 40.88 in a Green Check-in, then moves the
// clock to Feb 2: past the Metric's target date, so it is off target, but
// before the Goal's Milestone is due.
func offTargetMetricGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, domain.Metric) {
	t.Helper()
	ctx := context.Background()
	goal := h.ActiveGoal(owner, "Reclaim idle capacity", "Idle machines cost money.")
	m, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
		GoalID:     goal.ID,
		Name:       "Idle capacity",
		Unit:       "percent",
		Direction:  domain.MetricDown,
		Baseline:   55,
		Target:     20,
		TargetDate: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: owner.ID,
		Health:   domain.HealthGreen,
		Status:   "Draining the old pool.",
		Readings: []domain.MetricReadingInput{{MetricID: m.ID, Value: 40.88}},
	}); err != nil {
		t.Fatalf("Green before the Metric's target date: %v", err)
	}
	h.Clock.Set(time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC))
	return goal, m
}

// A Green Check-in while a Metric is off target is refused until the Owner
// confirms it: unconfirmed it records nothing and names the Metric and its
// reading; confirmed, it records Green.
func TestGreenWithOffTargetMetricNeedsConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)
	before, _, _ := h.Service.LatestCheckin(context.Background(), goal.ID)

	green := domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Still on track.",
	}
	_, err := h.Service.SubmitCheckin(context.Background(), green)
	if !errors.Is(err, domain.ErrGreenOffTarget) || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unconfirmed Green: err = %v, want ErrGreenOffTarget and ErrValidation", err)
	}
	var offTarget *domain.GreenOffTargetError
	if !errors.As(err, &offTarget) {
		t.Fatalf("unconfirmed Green: err = %v, want a *GreenOffTargetError", err)
	}
	if len(offTarget.Metrics) != 1 || offTarget.Metrics[0].Metric.ID != idle.ID || offTarget.Metrics[0].Reading != 40.88 {
		t.Errorf("off-target Metrics = %+v, want Idle capacity at 40.88", offTarget.Metrics)
	}
	if latest, _, _ := h.Service.LatestCheckin(context.Background(), goal.ID); latest.ID != before.ID {
		t.Errorf("an unconfirmed Green was recorded")
	}

	green.ConfirmOffTarget = true
	c, err := h.Service.SubmitCheckin(context.Background(), green)
	if err != nil {
		t.Fatalf("confirmed Green: %v", err)
	}
	if c.Health != domain.HealthGreen {
		t.Errorf("Health = %q, want %q", c.Health, domain.HealthGreen)
	}
}

// Only Green asks for confirming: a Yellow Check-in while a Metric is off
// target records as any other.
func TestYellowWithOffTargetMetricRecordsWithoutConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:         goal.ID,
		AuthorID:       sam.ID,
		Health:         domain.HealthYellow,
		Status:         "Idle capacity missed.",
		PathToGreen:    "Retire the old pool.",
		PathTargetDate: pathDate,
	}); err != nil {
		t.Fatalf("Yellow with an off-target Metric: %v", err)
	}
}

// The reading entered in the Check-in is the one judged: one that reaches the
// target means the Metric isn't off target, whatever it last read.
func TestGreenWithReadingReachingTargetNeedsNoConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "Hit it.",
		Readings: []domain.MetricReadingInput{{MetricID: idle.ID, Value: 20}},
	}); err != nil {
		t.Fatalf("Green with a reading at the target: %v", err)
	}
}

// A Metric that has never been read isn't off target, even past its target
// date.
func TestGreenWithUnreadMetricPastItsDateNeedsNoConfirming(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reclaim idle capacity", "Idle machines cost money.")
	if _, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
		GoalID: goal.ID, Name: "Idle capacity", Unit: "percent", Direction: domain.MetricDown,
		Baseline: 55, Target: 20, TargetDate: time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	h.Clock.Set(time.Date(2026, 2, 2, 15, 0, 0, 0, time.UTC))

	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	}); err != nil {
		t.Fatalf("Green with a never-read Metric: %v", err)
	}
}

// When a Milestone is overdue too, Green is refused for the Milestone, which
// no confirming gets past, and not with the off-target refusal.
func TestGreenRefusedForOverdueMilestoneBeforeOffTargetMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)
	beta := onlyMilestone(t, h, goal.ID)
	h.Clock.Set(beta.TargetDate.AddDate(0, 0, 1))

	_, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:   goal.ID,
		AuthorID: sam.ID,
		Health:   domain.HealthGreen,
		Status:   "On track.",
	})
	if !errors.Is(err, domain.ErrValidation) || errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("Green with an overdue Milestone and an off-target Metric: err = %v, want the overdue refusal", err)
	}

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrValidation) || errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("no-change Green with an overdue Milestone and an off-target Metric: err = %v, want the overdue refusal", err)
	}
}

// A Metric's target date is judged in the org's timezone: late on its target
// date in Los Angeles, already the next day in UTC, it isn't off target yet.
func TestOffTargetJudgedInOrgTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	h.Service = domain.NewService(h.DB, h.Clock, h.Email, nil, domain.WithTimezone(la))
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	green := domain.SubmitCheckinInput{GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "On track."}
	h.Clock.Set(time.Date(2026, 1, 31, 5, 0, 0, 0, time.UTC)) // Jan 30, 9pm in Los Angeles
	if _, err := h.Service.SubmitCheckin(context.Background(), green); err != nil {
		t.Fatalf("Green on the Metric's target date in the org's timezone: %v", err)
	}
	h.Clock.Set(time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)) // Jan 31, 1am in Los Angeles
	if _, err := h.Service.SubmitCheckin(context.Background(), green); !errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("Green the day after the Metric's target date: err = %v, want ErrGreenOffTarget", err)
	}
}

// A No change can't repeat a Green while a Metric is off target; the Owner
// writes a full Check-in instead.
func TestNoChangeGreenRefusedWhileMetricOffTarget(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, _ := offTargetMetricGoal(t, h, sam)

	if _, err := h.Service.SubmitNoChangeCheckin(context.Background(), goal.ID, sam.ID); !errors.Is(err, domain.ErrGreenOffTarget) {
		t.Fatalf("no-change Green with an off-target Metric: err = %v, want ErrGreenOffTarget", err)
	}
}

// OffTargetMetrics judges each of the Goal's Metrics by the reading entered,
// over the one last recorded: an entered reading still short of the target is
// off target, with that reading; one that meets the target leaves none.
func TestOffTargetMetricsJudgesTheReadingEntered(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal, idle := offTargetMetricGoal(t, h, sam)
	ctx := context.Background()

	missed, err := h.Service.OffTargetMetrics(ctx, goal.ID, []domain.MetricReadingInput{{MetricID: idle.ID, Value: 35.5}})
	if err != nil {
		t.Fatalf("OffTargetMetrics: %v", err)
	}
	if len(missed) != 1 || missed[0].Metric.ID != idle.ID || missed[0].Reading != 35.5 {
		t.Errorf("off-target Metrics = %+v, want Idle capacity at the 35.5 entered", missed)
	}

	missed, err = h.Service.OffTargetMetrics(ctx, goal.ID, []domain.MetricReadingInput{{MetricID: idle.ID, Value: 20}})
	if err != nil {
		t.Fatalf("OffTargetMetrics: %v", err)
	}
	if len(missed) != 0 {
		t.Errorf("off-target Metrics = %+v, want none once the reading entered meets the target", missed)
	}
}
