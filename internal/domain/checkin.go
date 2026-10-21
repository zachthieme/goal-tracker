package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// Checkin is an Owner's routine update to one Goal: its Health, a short status,
// and — when the Health is Yellow or Red — a Path to Green (CONTEXT.md:
// Check-in). Check-ins are immutable; a Goal's current Health, status, and Path
// to Green are its latest Check-in's. Each records both its author and the Owner
// it was written for (CONTEXT.md: Delegate).
type Checkin struct {
	ID          int64
	GoalID      int64
	Author      Account
	Owner       Account
	Health      string
	Status      string
	PathToGreen string
	// PathTargetDate is the target date for being back to Green; the zero time
	// when there is no Path to Green (CONTEXT.md: Path to Green).
	PathTargetDate time.Time
	// AsksForHelp marks the Path to Green as asking for help (CONTEXT.md: Path
	// to Green, Ask for help); never set without a Path to Green. Whether the
	// ask is still open is the Goal's, see OpenAskForHelp.
	AsksForHelp bool
	// Explanation is the Owner's reason their Health differs from the Rolled-up
	// Health (ADR-0003). Empty when the two match, or when there is nothing to
	// roll up.
	Explanation string
	// LifecycleChange is the move between Lifecycle states this Check-in made;
	// the zero value when it made none. A Check-in that takes the Goal out of
	// Active records no Health (CONTEXT.md: Health is how an Active Goal is
	// tracking; Lifecycle is independent of it).
	LifecycleChange LifecycleChange
	// MilestoneChanges are the Milestones this Check-in added, marked Done or
	// marked Removed, in the order made. Only ListCheckins loads them; a
	// Check-in from before they were recorded has none.
	MilestoneChanges []MilestoneChange
	// DiscardedDraftHighlights are the Draft Highlights this Check-in offered
	// and didn't keep, oldest first (CONTEXT.md: Draft Highlight). Only
	// ListCheckins loads them.
	DiscardedDraftHighlights []DraftHighlight
	CreatedAt                time.Time
}

// Health values a Check-in can set (CONTEXT.md: Health). Green needs no Path to
// Green; Yellow and Red require one.
const (
	HealthGreen  = "Green"
	HealthYellow = "Yellow"
	HealthRed    = "Red"
)

// The inputs of a Check-in, by the names its refusals (see InputError) and the
// Check-in form share. A Metric's reading, a Milestone's, a new Milestone's or a
// Highlight row's is made by its function below.
const (
	CheckinInputHealth               = "health"
	CheckinInputStatus               = "status"
	CheckinInputPathToGreen          = "path_to_green"
	CheckinInputPathTargetDateReason = "path_target_date_reason"
	CheckinInputExplanation          = "explanation"
	CheckinInputLifecycle            = "lifecycle"
	CheckinInputLifecycleReason      = "lifecycle_reason"
	CheckinInputOutcome              = "outcome"
	CheckinInputDeliveryDate         = "delivery_date"
	CheckinInputDeliveryDateReason   = "delivery_date_reason"
)

// CheckinReadingInput names the reading given for a Metric.
func CheckinReadingInput(metricID int64) string { return fmt.Sprintf("reading_%d", metricID) }

// CheckinMilestoneInput names one part ("date", "date_reason",
// "removed_reason") of a Planned Milestone's change.
func CheckinMilestoneInput(milestoneID int64, part string) string {
	return fmt.Sprintf("milestone_%s_%d", part, milestoneID)
}

// CheckinNewMilestoneInput names the i-th Milestone added, counting from 0.
func CheckinNewMilestoneInput(i int) string { return fmt.Sprintf("new_milestone_%d", i) }

// CheckinHighlightInput names the n-th Highlight row, counting from 1 as the
// Check-in form numbers them.
func CheckinHighlightInput(n int) string { return fmt.Sprintf("highlight_%d", n) }

func validHealth(h string) bool {
	return h == HealthGreen || h == HealthYellow || h == HealthRed
}

// needsPathToGreen reports whether a Health requires a Path to Green: Yellow and
// Red do, Green does not (CONTEXT.md: Path to Green).
func needsPathToGreen(health string) bool {
	return health == HealthYellow || health == HealthRed
}

// SubmitCheckinInput is the submit-a-Check-in command's input. PathToGreen and
// PathTargetDate are required only when Health is Yellow or Red.
type SubmitCheckinInput struct {
	GoalID         int64
	AuthorID       int64
	Health         string
	Status         string
	PathToGreen    string
	PathTargetDate time.Time
	// PathTargetDateReason is why PathTargetDate is later than the latest
	// Check-in's back-to-Green date: moving it later records a Date Slip that
	// needs one (CONTEXT.md: Date Slip). An earlier, unchanged or first date
	// needs none, and any stray reason is dropped.
	PathTargetDateReason string
	// AsksForHelp marks the Path to Green as asking for help (CONTEXT.md: Ask
	// for help). It is kept only with the Path to Green: a Check-in that is
	// Green, or takes the Goal out of Active, stores no ask.
	AsksForHelp bool
	// Explanation is required only when the Health differs from the Goal's
	// Rolled-up Health (ADR-0003).
	Explanation string
	// Readings records the current value of Metrics on the Goal (CONTEXT.md: a
	// Metric's current value is recorded at each Check-in). Each must name a
	// Metric on this Goal; a reading for a Metric on another Goal is rejected.
	Readings []MetricReadingInput
	// Highlights are the Check-in's Highlights, in the order entered
	// (CONTEXT.md: Highlight). Any number, of any mix of kinds; one with a blank
	// note is ignored. A row offered from a pending Draft Highlight names it:
	// the Check-in keeps it as a Highlight or discards it (see
	// planDraftHighlightPicks).
	Highlights []HighlightInput
	// DeliveryDate moves a Dated Goal's delivery date, recording a Date Slip
	// that needs DeliveryDateReason (CONTEXT.md: Date Slip). The zero time, or
	// the Goal's current date, leaves it unchanged.
	DeliveryDate       time.Time
	DeliveryDateReason string
	// Milestones are changes to the Goal's Milestones; each must name a
	// Milestone on this Goal.
	Milestones []MilestoneChangeInput
	// NewMilestones are Milestones added to the Goal in this Check-in; they
	// count toward its Milestone Churn.
	NewMilestones []NewMilestoneInput
	// Lifecycle moves the Goal to another Lifecycle in this Check-in; "" leaves
	// it alone (CONTEXT.md: Lifecycle). On Hold and Cancelled need a
	// LifecycleReason; Done needs a one-line Outcome and a reading for every
	// Metric on the Goal, its final value.
	Lifecycle       string
	LifecycleReason string
	Outcome         string
	// ConfirmOffTarget is the Owner's confirmation that the Goal is Green
	// although a Metric is off target; without it such a Check-in is refused
	// with a *GreenOffTargetError.
	ConfirmOffTarget bool
}

// SubmitCheckin records a Check-in on a Goal. The Goal must be Active — a
// Proposed Goal has no Health and can't be checked in on (CONTEXT.md: Health is
// how an Active Goal is tracking) — or On Hold, when the Check-in resumes or
// Cancels it. Health must be Green, Yellow, or Red and the status is required.
// Yellow or Red requires a Path to Green with text and a target date. A Check-in
// may also change the Goal's Lifecycle (see planLifecycleChange); one that takes
// the Goal out of Active records no Health. Only the Goal's Owner may submit;
// the Check-in records its author and the Owner it was written for. Check-ins
// are immutable, so this always inserts a new one.
func (s *Service) SubmitCheckin(ctx context.Context, in SubmitCheckinInput) (Checkin, error) {
	goal, err := s.queries.GetGoal(ctx, in.GoalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Checkin{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return Checkin{}, fmt.Errorf("look up goal: %w", err)
	}
	lifecycle, err := planLifecycleChange(goal.Goal.Lifecycle, in.Lifecycle, in.LifecycleReason, in.Outcome)
	if err != nil {
		return Checkin{}, err
	}
	// The Owner writes a Goal's Check-ins, as may any Delegate the Owner has
	// authorized; the Check-in records its author and still credits the Owner it
	// speaks for (CONTEXT.md: Delegate).
	if err := s.authorizeCheckinAuthor(ctx, goal.Goal.ID, goal.Goal.OwnerID, in.AuthorID); err != nil {
		return Checkin{}, err
	}

	status := strings.TrimSpace(in.Status)
	path := strings.TrimSpace(in.PathToGreen)
	explanation := strings.TrimSpace(in.Explanation)
	// Health is how an Active Goal is tracking, so only a Check-in that leaves
	// the Goal Active sets one; the rest record just their status.
	health := in.Health
	tracked := lifecycle.resultingLifecycle(goal.Goal.Lifecycle) == LifecycleActive
	if tracked {
		if err := validateCheckin(health, status, path, in.PathTargetDate); err != nil {
			return Checkin{}, err
		}
		// The Owner has to explain why their Health differs from the Rolled-up
		// Health — the worst among the Goal's Active children (ADR-0003). When
		// there is nothing to roll up, or the two match, no explanation is
		// required and any stray one is dropped.
		explanation, err = s.resolveRollupExplanation(ctx, goal.Goal.ID, health, explanation)
		if err != nil {
			return Checkin{}, err
		}
	} else {
		if status == "" {
			return Checkin{}, inputError(CheckinInputStatus, "a Check-in needs a status")
		}
		health, path, explanation = "", "", ""
	}

	// A reading may only name a Metric on this Goal (CONTEXT.md: a Check-in
	// records the current value of the Goal's Metrics).
	if err := s.validateReadings(ctx, goal.Goal.ID, in.Readings); err != nil {
		return Checkin{}, err
	}
	if lifecycle.To == LifecycleDone {
		if err := s.requireFinalValues(ctx, goal.Goal.ID, in.Readings); err != nil {
			return Checkin{}, err
		}
	}
	deliverySlip, err := planDeliverySlip(goal.Goal, in.DeliveryDate, in.DeliveryDateReason)
	if err != nil {
		return Checkin{}, err
	}
	// A slip is never hidden behind a Green: moving the delivery date later
	// means the Goal is not on track (CONTEXT.md: Date Slip).
	if health == HealthGreen && deliverySlip != nil && deliverySlip.later() {
		return Checkin{}, inputError(CheckinInputDeliveryDate, "a Check-in that moves the delivery date later can't be Green")
	}
	// A Milestone slip that doesn't move the delivery date doesn't affect Health
	// (CONTEXT.md: Milestone), so it is recorded but never gates the Health.
	milestones, err := s.planMilestoneChanges(ctx, goal.Goal.ID, in.Milestones, in.NewMilestones)
	if err != nil {
		return Checkin{}, err
	}
	if err := s.rejectGreenWhileOverdue(goalFromRow(goal.Goal, goal.Account), health, milestones.resulting); err != nil {
		return Checkin{}, err
	}
	// Health is the Owner's call (ADR-0003), so a Metric that missed its target
	// only asks them to confirm a Green; an overdue Milestone, refused above,
	// wins over it.
	if health == HealthGreen && !in.ConfirmOffTarget {
		if err := s.rejectGreenWhileOffTarget(ctx, goalFromRow(goal.Goal, goal.Account), in.Readings); err != nil {
			return Checkin{}, err
		}
	}

	// Moving the back-to-Green date later than the latest Check-in had it is a
	// Date Slip of that date (CONTEXT.md: Date Slip).
	var pathSlip *slipPlan
	if needsPathToGreen(health) {
		pathSlip, err = s.planPathSlip(ctx, goal.Goal.ID, in.PathTargetDate, in.PathTargetDateReason)
		if err != nil {
			return Checkin{}, err
		}
	}

	pathTargetDate := ""
	asksForHelp := int64(0)
	if needsPathToGreen(health) {
		pathTargetDate = in.PathTargetDate.Format(dateFormat)
		if in.AsksForHelp {
			asksForHelp = 1
		}
	} else {
		// A Green Check-in carries no Path to Green, so no ask for help.
		path = ""
	}

	// The Check-in and the Metric readings it records are one immutable update,
	// so they are written together or not at all.
	var out Checkin
	err = s.WithinTx(ctx, func(tx *Service) error {
		// The Highlights are planned within the transaction: which Draft
		// Highlights are still pending decides which offered rows count, and
		// they are cleared in the same transaction, so two Check-ins can't both
		// keep one.
		picks, err := tx.planDraftHighlightPicks(ctx, goal.Goal.ID, in.Highlights)
		if err != nil {
			return err
		}
		highlights, err := planHighlights(picks.rows)
		if err != nil {
			return err
		}
		c, err := tx.createCheckin(ctx, db.CreateCheckinParams{
			GoalID:          goal.Goal.ID,
			AuthorID:        in.AuthorID,
			OwnerID:         goal.Goal.OwnerID,
			Health:          health,
			Status:          status,
			PathToGreen:     path,
			PathTargetDate:  pathTargetDate,
			Explanation:     explanation,
			LifecycleFrom:   lifecycle.From,
			LifecycleTo:     lifecycle.To,
			LifecycleReason: lifecycle.Reason,
			Outcome:         lifecycle.Outcome,
			AsksForHelp:     asksForHelp,
		})
		if err != nil {
			return err
		}
		if lifecycle.Changed() {
			if _, err := tx.queries.SetGoalLifecycle(ctx, db.SetGoalLifecycleParams{Lifecycle: lifecycle.To, ID: goal.Goal.ID}); err != nil {
				return fmt.Errorf("set goal lifecycle: %w", err)
			}
			if lifecycle.To == LifecycleDone || lifecycle.To == LifecycleCancelled {
				if err := tx.closeSuggestionsOfEndedGoal(ctx, goal.Goal.ID); err != nil {
					return err
				}
			}
		}
		if err := tx.recordReadings(ctx, c.ID, in.Readings); err != nil {
			return err
		}
		for _, slip := range []*slipPlan{deliverySlip, pathSlip} {
			if slip == nil {
				continue
			}
			if err := tx.recordSlip(ctx, goal.Goal.ID, c.ID, *slip); err != nil {
				return err
			}
		}
		if err := milestones.apply(ctx, tx, goal.Goal.ID, c.ID, in.AuthorID); err != nil {
			return err
		}
		if err := tx.recordHighlights(ctx, c.ID, highlights); err != nil {
			return err
		}
		if err := picks.apply(ctx, tx, c.ID); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return Checkin{}, err
	}
	return out, nil
}

// planPathSlip plans the Date Slip, if any, of a Check-in on goalID whose
// back-to-Green date is newDate, against the Goal's latest Check-in, read
// before the new one is written; see planBackToGreenSlip.
func (s *Service) planPathSlip(ctx context.Context, goalID int64, newDate time.Time, reason string) (*slipPlan, error) {
	latest, ok, err := s.LatestCheckin(ctx, goalID)
	if err != nil || !ok {
		return nil, err
	}
	return planBackToGreenSlip(latest.PathTargetDate, newDate, reason)
}

// ErrGreenOffTarget is a Green Check-in refused because a Metric on the Goal is
// off target (see Metric.OffTarget). A full Check-in may be confirmed Green
// anyway; a No change can't repeat it.
var ErrGreenOffTarget = errors.New("green with a Metric off target")

// OffTargetMetric is a Metric that has missed its target, with the reading
// that missed it.
type OffTargetMetric struct {
	Metric  Metric
	Reading float64
}

// GreenOffTargetError refuses a Green Check-in, unconfirmed, while Metrics on
// the Goal are off target, naming each with its reading so the Owner can
// confirm the Green or change it. It is both an ErrGreenOffTarget and an
// ErrValidation.
type GreenOffTargetError struct {
	Metrics []OffTargetMetric
}

func (e *GreenOffTargetError) Error() string {
	missed := make([]string, 0, len(e.Metrics))
	for _, o := range e.Metrics {
		missed = append(missed, fmt.Sprintf("%s is %s %s; its target was %s by %s",
			o.Metric.Name, formatMetricValue(o.Reading), o.Metric.Unit, formatMetricValue(o.Metric.Target), o.Metric.TargetDate.Format(dateFormat)))
	}
	return fmt.Sprintf("%v: %s, so the Goal's Green needs confirming", ErrValidation, strings.Join(missed, "; "))
}

func (e *GreenOffTargetError) Unwrap() []error { return []error{ErrGreenOffTarget, ErrValidation} }

// formatMetricValue renders a reading or target without trailing zeros.
func formatMetricValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// rejectGreenWhileOffTarget refuses a Green on g with a *GreenOffTargetError
// while any of its Metrics is off target today, in the Goal's timezone; see
// Service.GoalTimezone and OffTargetMetrics.
func (s *Service) rejectGreenWhileOffTarget(ctx context.Context, g Goal, readings []MetricReadingInput) error {
	missed, err := s.offTargetMetricsOf(ctx, g, readings)
	if err != nil {
		return err
	}
	if len(missed) > 0 {
		return &GreenOffTargetError{Metrics: missed}
	}
	return nil
}

// OffTargetMetrics returns goalID's Metrics that are off target today, in the
// Goal's timezone (see GoalTimezone), each with the reading that missed; none
// when none is. Each Metric is judged by the reading in readings (as entered in
// a Check-in) or, when there is none, its latest recorded one; a Metric never
// read is not off target.
func (s *Service) OffTargetMetrics(ctx context.Context, goalID int64, readings []MetricReadingInput) ([]OffTargetMetric, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return nil, err
	}
	return s.offTargetMetricsOf(ctx, g, readings)
}

// offTargetMetricsOf is OffTargetMetrics for a Goal already loaded.
func (s *Service) offTargetMetricsOf(ctx context.Context, g Goal, readings []MetricReadingInput) ([]OffTargetMetric, error) {
	metrics, err := s.ListMetrics(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	entered := make(map[int64]float64, len(readings))
	for _, rd := range readings {
		entered[rd.MetricID] = rd.Value
	}
	today := s.goalToday(g)
	var missed []OffTargetMetric
	for _, m := range metrics {
		reading, ok := entered[m.ID]
		if !ok {
			recorded, err := s.queries.ListMetricReadings(ctx, m.ID)
			if err != nil {
				return nil, fmt.Errorf("list metric readings: %w", err)
			}
			if len(recorded) == 0 {
				continue
			}
			reading = recorded[len(recorded)-1].Value
		}
		if m.OffTarget(reading, today) {
			missed = append(missed, OffTargetMetric{Metric: m, Reading: reading})
		}
	}
	return missed, nil
}

// validateReadings checks that each reading names a Metric on goalID; a reading
// for a Metric that does not exist or belongs to another Goal is rejected.
func (s *Service) validateReadings(ctx context.Context, goalID int64, readings []MetricReadingInput) error {
	for _, rd := range readings {
		m, err := s.queries.GetMetric(ctx, rd.MetricID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: metric %d does not exist", ErrValidation, rd.MetricID)
			}
			return fmt.Errorf("look up metric: %w", err)
		}
		if m.GoalID != goalID {
			return fmt.Errorf("%w: metric %d is not on this Goal", ErrValidation, rd.MetricID)
		}
	}
	return nil
}

// recordReadings inserts a Check-in's Metric readings, each stamped with the
// Service's clock so they order over time for the trend display.
func (s *Service) recordReadings(ctx context.Context, checkinID int64, readings []MetricReadingInput) error {
	now := s.clock.Now().Format(timeFormat)
	for _, rd := range readings {
		if _, err := s.queries.CreateMetricReading(ctx, db.CreateMetricReadingParams{
			CheckinID: checkinID,
			MetricID:  rd.MetricID,
			Value:     rd.Value,
			CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("create metric reading: %w", err)
		}
	}
	return nil
}

// SubmitNoChangeCheckin records a Check-in that repeats the Goal's previous
// values in one click (CONTEXT.md: a Check-in should take minutes). It needs a
// previous Check-in to repeat, so it is rejected on a Goal with none. It is
// also refused, whatever the Health, while a Milestone is overdue: the missed
// date needs a full Check-in that moves it, marks it Done or removes it. So it
// is once the Path to Green's back-to-Green date has passed: the Owner must
// submit a Check-in with a new date and reason, or update the Health. The new
// Check-in is still immutable and records its own author and Owner.
func (s *Service) SubmitNoChangeCheckin(ctx context.Context, goalID, authorID int64) (Checkin, error) {
	goal, err := s.activeGoalForNoChange(ctx, goalID)
	if err != nil {
		return Checkin{}, err
	}
	if err := s.authorizeCheckinAuthor(ctx, goal.Goal.ID, goal.Goal.OwnerID, authorID); err != nil {
		return Checkin{}, err
	}

	prev, explanation, err := s.noChangeCheck(ctx, goalFromRow(goal.Goal, goal.Account))
	if err != nil {
		return Checkin{}, err
	}
	return s.createCheckin(ctx, db.CreateCheckinParams{
		GoalID:         goalID,
		AuthorID:       authorID,
		OwnerID:        goal.Goal.OwnerID,
		Health:         prev.Health,
		Status:         prev.Status,
		PathToGreen:    prev.PathToGreen,
		PathTargetDate: prev.PathTargetDate,
		AsksForHelp:    prev.AsksForHelp,
		Explanation:    explanation,
	})
}

// NoChangeRefusal reports why a No change on goalID would be refused for any
// author allowed to check in on it, or nil when one would be recorded. It
// gives the error SubmitNoChangeCheckin would, without recording anything, so
// a page can offer the No change only when it would be taken: none while a
// Milestone is overdue or the back-to-Green date has passed.
func (s *Service) NoChangeRefusal(ctx context.Context, goalID int64) error {
	goal, err := s.activeGoalForNoChange(ctx, goalID)
	if err != nil {
		return err
	}
	_, _, err = s.noChangeCheck(ctx, goalFromRow(goal.Goal, goal.Account))
	return err
}

// activeGoalForNoChange looks up goalID for a No change, refusing a Goal that
// doesn't exist or isn't Active.
func (s *Service) activeGoalForNoChange(ctx context.Context, goalID int64) (db.GetGoalRow, error) {
	goal, err := s.queries.GetGoal(ctx, goalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.GetGoalRow{}, fmt.Errorf("%w: goal does not exist", ErrValidation)
		}
		return db.GetGoalRow{}, fmt.Errorf("look up goal: %w", err)
	}
	if goal.Goal.Lifecycle != LifecycleActive {
		return db.GetGoalRow{}, fmt.Errorf("%w: only an Active Goal can be checked in on", ErrValidation)
	}
	return goal, nil
}

// noChangeCheck runs the checks a No change on g must pass once the Goal is
// known to be Active and the author allowed to check in, none of which depend
// on who is asking. It returns the previous Check-in to repeat and the
// explanation to record with it.
func (s *Service) noChangeCheck(ctx context.Context, g Goal) (db.Checkin, string, error) {
	prev, err := s.queries.GetLatestCheckin(ctx, g.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.Checkin{}, "", fmt.Errorf("%w: there is no previous Check-in to repeat", ErrValidation)
		}
		return db.Checkin{}, "", fmt.Errorf("look up previous checkin: %w", err)
	}
	// A no-change repeat still cannot violate the Rolled-up Health rule: if the
	// roll-up has since changed so the repeated Health would differ from it and
	// the previous Check-in carried no explanation, refuse it so the Owner uses
	// the full form to explain (ADR-0003).
	explanation, err := s.resolveRollupExplanation(ctx, g.ID, prev.Health, prev.Explanation)
	if err != nil {
		return db.Checkin{}, "", fmt.Errorf("%w — submit a Check-in to explain", err)
	}
	// Nor can it repeat any Health once a Milestone has gone overdue, as of
	// today in the Goal's timezone: the missed date needs a response.
	milestones, err := s.queries.ListMilestones(ctx, g.ID)
	if err != nil {
		return db.Checkin{}, "", fmt.Errorf("list milestones: %w", err)
	}
	if m, ok := overdueMilestone(milestones, s.goalToday(g).Format(dateFormat)); ok {
		return db.Checkin{}, "", fmt.Errorf("%w: submit a Check-in to move its date, mark it Done or remove it", &MilestoneOverdueError{Milestone: milestoneFromRow(m)})
	}
	// Nor once the back-to-Green date has passed, as of today in the Goal's
	// timezone, whatever the Health: the Owner must submit a Check-in with a
	// new date and reason, or update the Health (CONTEXT.md: Check-in).
	fresh, err := s.Freshness(ctx, g.ID)
	if err != nil {
		return db.Checkin{}, "", err
	}
	if fresh.PathToGreenOverdue {
		return db.Checkin{}, "", fmt.Errorf("%w: the back-to-Green date has passed: submit a Check-in with a new date and reason, or update the Health", ErrValidation)
	}
	// Nor a Green while a Metric is off target: only a full Check-in can
	// confirm that.
	if prev.Health == HealthGreen {
		if err := s.rejectGreenWhileOffTarget(ctx, g, nil); err != nil {
			return db.Checkin{}, "", fmt.Errorf("%w — submit a Check-in to confirm or update it", err)
		}
	}
	return prev, explanation, nil
}

// authorizeCheckinAuthor allows a Check-in to be written only by the Goal's
// Owner or a Delegate the Owner has authorized; anyone else is refused
// (CONTEXT.md: Delegate; acceptance: Non-Delegates can't submit Check-ins). A
// Departed person can't act, so is refused whichever they are (CONTEXT.md:
// Departed).
func (s *Service) authorizeCheckinAuthor(ctx context.Context, goalID, ownerID, authorID int64) error {
	author, err := s.queries.GetAccount(ctx, authorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: the author does not exist", ErrNotAuthorized)
		}
		return fmt.Errorf("look up author: %w", err)
	}
	if author.Departed != 0 {
		return fmt.Errorf("%w: a Departed person can't submit a Check-in", ErrNotAuthorized)
	}
	if authorID == ownerID {
		return nil
	}
	ok, err := s.isDelegate(ctx, goalID, authorID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: only the Owner or a Delegate may submit a Check-in", ErrNotAuthorized)
	}
	return nil
}

// resolveRollupExplanation enforces ADR-0003's explanation rule for a Check-in
// setting health on goalID: when the Goal's Rolled-up Health is present and
// differs from health, a non-empty explanation is required; otherwise none is,
// and any stray explanation is dropped. It returns the explanation to store.
func (s *Service) resolveRollupExplanation(ctx context.Context, goalID int64, health, explanation string) (string, error) {
	rollup, err := s.RolledUpHealth(ctx, goalID)
	if err != nil {
		return "", err
	}
	if rollup.Present && rollup.Health != health {
		if explanation == "" {
			return "", inputError(CheckinInputExplanation, "this Health differs from the Rolled-up Health (%s); explain why", rollup.Health)
		}
		return explanation, nil
	}
	return "", nil
}

// validateCheckin enforces the rules shared by every Check-in: a valid Health, a
// status, and — when Yellow or Red — a Path to Green with text and a target date.
func validateCheckin(health, status, path string, pathTargetDate time.Time) error {
	if !validHealth(health) {
		return inputError(CheckinInputHealth, "Health must be %q, %q, or %q", HealthGreen, HealthYellow, HealthRed)
	}
	if status == "" {
		return inputError(CheckinInputStatus, "a Check-in needs a status")
	}
	if needsPathToGreen(health) {
		if path == "" {
			return inputError(CheckinInputPathToGreen, "a %s Goal needs a Path to Green", health)
		}
		if pathTargetDate.IsZero() {
			return inputError(CheckinInputPathToGreen, "a Path to Green needs a target date")
		}
	}
	return nil
}

// createCheckin inserts an immutable Check-in row, stamped with the Service's
// clock, and returns it resolved.
func (s *Service) createCheckin(ctx context.Context, p db.CreateCheckinParams) (Checkin, error) {
	p.CreatedAt = s.clock.Now().Format(timeFormat)
	row, err := s.queries.CreateCheckin(ctx, p)
	if err != nil {
		return Checkin{}, fmt.Errorf("create checkin: %w", err)
	}
	author, err := s.queries.GetAccount(ctx, p.AuthorID)
	if err != nil {
		return Checkin{}, fmt.Errorf("look up author: %w", err)
	}
	owner, err := s.queries.GetAccount(ctx, p.OwnerID)
	if err != nil {
		return Checkin{}, fmt.Errorf("look up owner: %w", err)
	}
	return checkinFromRow(row, author, owner), nil
}

// LatestCheckin returns a Goal's most recent Check-in, which carries its current
// Health, status, and Path to Green. ok is false when the Goal has none yet.
func (s *Service) LatestCheckin(ctx context.Context, goalID int64) (Checkin, bool, error) {
	latest, err := s.LatestCheckinsByGoal(ctx, []int64{goalID})
	if err != nil {
		return Checkin{}, false, err
	}
	c, ok := latest[goalID]
	return c, ok, nil
}

// LatestCheckinsByGoal returns the most recent Check-in of each of goalIDs, as
// LatestCheckin reads one, keyed by Goal ID; a Goal with none has no entry. It
// runs one query however many Goals there are, so a page listing Goals can
// afford it, and none for no Goals.
func (s *Service) LatestCheckinsByGoal(ctx context.Context, goalIDs []int64) (map[int64]Checkin, error) {
	if len(goalIDs) == 0 {
		return map[int64]Checkin{}, nil
	}
	rows, err := s.queries.ListLatestCheckinsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list latest checkins: %w", err)
	}
	out := make(map[int64]Checkin, len(rows))
	for _, r := range rows {
		out[r.Checkin.GoalID] = checkinFromRow(r.Checkin, r.Account, r.Account_2)
	}
	return out, nil
}

// DateSlipsByGoal returns the Date Slips of each of goalIDs, earliest first, as
// ListDateSlips reads one Goal's, keyed by Goal ID; a Goal with none has no
// entry. It runs one query however many Goals there are, and none for no Goals.
func (s *Service) DateSlipsByGoal(ctx context.Context, goalIDs []int64) (map[int64][]DateSlip, error) {
	if len(goalIDs) == 0 {
		return map[int64][]DateSlip{}, nil
	}
	rows, err := s.queries.ListDateSlipsForGoals(ctx, goalIDs)
	if err != nil {
		return nil, fmt.Errorf("list date slips: %w", err)
	}
	out := make(map[int64][]DateSlip)
	for _, r := range rows {
		out[r.GoalID] = append(out[r.GoalID], dateSlipFromRow(r))
	}
	return out, nil
}

// ListCheckins returns a Goal's Check-in history, newest first, each with its
// author and the Owner it was written for resolved, the Milestone changes it
// recorded and the Draft Highlights it discarded.
func (s *Service) ListCheckins(ctx context.Context, goalID int64) ([]Checkin, error) {
	rows, err := s.queries.ListCheckins(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list checkins: %w", err)
	}
	changes, err := s.queries.ListMilestoneChangesByGoal(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list milestone changes: %w", err)
	}
	// A change made outside a Check-in belongs to none of them
	// (MilestoneChangesOutsideCheckins).
	changesBy := make(map[int64][]MilestoneChange)
	for _, r := range changes {
		if r.MilestoneChange.CheckinID == nil {
			continue
		}
		checkinID := *r.MilestoneChange.CheckinID
		changesBy[checkinID] = append(changesBy[checkinID], milestoneChangeFromRow(r))
	}
	discardedBy, err := s.discardedDraftHighlightsByCheckin(ctx, goalID)
	if err != nil {
		return nil, err
	}
	out := make([]Checkin, 0, len(rows))
	for _, r := range rows {
		c := checkinFromRow(r.Checkin, r.Account, r.Account_2)
		c.MilestoneChanges = changesBy[c.ID]
		c.DiscardedDraftHighlights = discardedBy[c.ID]
		out = append(out, c)
	}
	return out, nil
}

func checkinFromRow(c db.Checkin, author, owner db.Account) Checkin {
	createdAt, _ := time.Parse(timeFormat, c.CreatedAt)
	var pathTargetDate time.Time
	if c.PathTargetDate != "" {
		pathTargetDate, _ = time.Parse(dateFormat, c.PathTargetDate)
	}
	return Checkin{
		ID:             c.ID,
		GoalID:         c.GoalID,
		Author:         accountFromRow(author),
		Owner:          accountFromRow(owner),
		Health:         c.Health,
		Status:         c.Status,
		PathToGreen:    c.PathToGreen,
		PathTargetDate: pathTargetDate,
		AsksForHelp:    c.AsksForHelp != 0,
		Explanation:    c.Explanation,
		LifecycleChange: LifecycleChange{
			From:    c.LifecycleFrom,
			To:      c.LifecycleTo,
			Reason:  c.LifecycleReason,
			Outcome: c.Outcome,
		},
		CreatedAt: createdAt,
	}
}
