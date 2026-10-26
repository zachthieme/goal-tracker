package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
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
//
// Each name starts Health: the CHECK test on checkins.health finds its values
// by that prefix (enumchecks_test.go), so a value named otherwise goes
// unchecked.
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
	// The Check-in is planned and written in one transaction: everything that
	// depends on the Goal's state — its Lifecycle, who may check in, the latest
	// Check-in a Date Slip compares with, its Milestones and Draft Highlights —
	// is read inside it, so two Check-ins sent together can't both close the
	// Goal or both plan from the same latest Check-in. The Check-in and all it
	// records are written together or not at all.
	var out Checkin
	err := s.WithinTx(ctx, func(tx *Service) error {
		c, err := tx.submitCheckinInTx(ctx, in)
		out = c
		return err
	})
	if err != nil {
		return Checkin{}, err
	}
	return out, nil
}

// submitCheckinInTx is SubmitCheckin's work, on a Service already inside its
// transaction: it reads the Goal, judges the Check-in from its facts
// (judgeCheckin) and writes what the judgement planned.
func (s *Service) submitCheckinInTx(ctx context.Context, in SubmitCheckinInput) (Checkin, error) {
	// Who may check in, and move the Goal's Lifecycle how, is judged with the
	// Check-in's other facts (judgeCheckin), so the Goal opens keeping the
	// actor for them.
	var actor goalActor
	goal, err := s.openGoalCommand(ctx, in.AuthorID, in.GoalID, func(a goalActor, _ Goal) error {
		actor = a
		return nil
	}, nil)
	if err != nil {
		return Checkin{}, err
	}
	plan, err := judgeCheckin(in, s.checkinFactsFor(ctx, goal, actor))
	if err != nil {
		return Checkin{}, err
	}
	c, err := s.createCheckin(ctx, plan.row)
	if err != nil {
		return Checkin{}, err
	}
	if lifecycle := plan.lifecycle; lifecycle.Changed() {
		if err := s.setGoalLifecycle(ctx, goal.ID, lifecycle); err != nil {
			return Checkin{}, err
		}
		if LifecycleEnded(lifecycle.To) {
			if err := s.closeSuggestionsOfEndedGoal(ctx, goal.ID); err != nil {
				return Checkin{}, err
			}
		}
	}
	if err := s.recordReadings(ctx, c.ID, in.Readings); err != nil {
		return Checkin{}, err
	}
	for _, slip := range []*slipPlan{plan.deliverySlip, plan.pathSlip} {
		if slip == nil {
			continue
		}
		if err := s.recordSlip(ctx, goal.ID, c.ID, *slip); err != nil {
			return Checkin{}, err
		}
	}
	if err := plan.milestones.apply(ctx, s, goal.ID, c.ID, in.AuthorID); err != nil {
		return Checkin{}, err
	}
	if err := s.recordHighlights(ctx, c.ID, plan.highlights); err != nil {
		return Checkin{}, err
	}
	if err := plan.picks.apply(ctx, s, c.ID); err != nil {
		return Checkin{}, err
	}
	return c, nil
}

// checkinFactsFor is the facts a Check-in on g by actor is judged from,
// read through s. Each fact is read only when a rule asks for it, and at most
// once: the Goal's Metrics, which the readings, a Done's final values and an
// off-target Green all ask for, run one query between them.
func (s *Service) checkinFactsFor(ctx context.Context, g Goal, actor goalActor) checkinFacts {
	return checkinFacts{
		goal:  g,
		actor: actor,
		today: s.goalToday(g),
		rollup: sync.OnceValues(func() (RolledUpHealth, error) {
			return s.RolledUpHealth(ctx, g.ID)
		}),
		metrics: sync.OnceValues(func() ([]Metric, error) {
			return s.ListMetrics(ctx, g.ID)
		}),
		readings: sync.OnceValues(func() (map[int64][]MetricReading, error) {
			return s.metricReadingsByMetric(ctx, []int64{g.ID})
		}),
		metricExists: s.metricExistsOnce(ctx),
		milestones: sync.OnceValues(func() ([]Milestone, error) {
			return s.ListMilestones(ctx, g.ID)
		}),
		latestCheckin: sync.OnceValues(func() (*Checkin, error) {
			latest, ok, err := s.LatestCheckin(ctx, g.ID)
			if err != nil || !ok {
				return nil, err
			}
			return &latest, nil
		}),
		pendingDraftHighlights: sync.OnceValues(func() ([]DraftHighlight, error) {
			return s.pendingDraftHighlights(ctx, g.ID)
		}),
	}
}

// metricExistsOnce says whether a Metric exists, looking each one up at most
// once.
func (s *Service) metricExistsOnce(ctx context.Context) func(id int64) (bool, error) {
	exists := map[int64]bool{}
	return func(id int64) (bool, error) {
		if ok, seen := exists[id]; seen {
			return ok, nil
		}
		_, err := s.queries.GetMetric(ctx, id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			exists[id] = false
		case err != nil:
			return false, fmt.Errorf("look up metric: %w", err)
		default:
			exists[id] = true
		}
		return exists[id], nil
	}
}

// setGoalLifecycle makes a Check-in's Lifecycle change on goalID, but only
// while the Goal is still in the Lifecycle the change was planned from. When
// it has left it, the Check-in is refused with ErrValidation, as one leaving
// the Goal's Lifecycle now as it is would be (lifecycleTakesNoCheckinsRefusal,
// lifecycleKeepRefusal), or else saying the Lifecycle changed meanwhile, so the caller's transaction keeps nothing it wrote.
func (s *Service) setGoalLifecycle(ctx context.Context, goalID int64, change LifecycleChange) error {
	n, err := s.queries.SetGoalLifecycle(ctx, db.SetGoalLifecycleParams{
		Lifecycle:     change.To,
		ID:            goalID,
		FromLifecycle: change.From,
	})
	if err != nil {
		return fmt.Errorf("set goal lifecycle: %w", err)
	}
	if n == 0 {
		goal, err := s.queries.GetGoal(ctx, goalID)
		if err != nil {
			return fmt.Errorf("look up goal: %w", err)
		}
		now := goal.Goal.Lifecycle
		if !LifecycleTakesCheckins(now) {
			return lifecycleTakesNoCheckinsRefusal(now)
		}
		if err := lifecycleKeepRefusal(now); err != nil {
			return err
		}
		return Refuse(ErrValidation, "the Goal's Lifecycle changed meanwhile: reload it and try again")
	}
	return nil
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
// the Goal are off target, naming each with its reading so a caller can ask
// the Owner to confirm the Green or change it, with the dates as a page shows
// them; its own text is a terse log form, not one for people. It is both an
// ErrGreenOffTarget and an ErrValidation.
type GreenOffTargetError struct {
	Metrics []OffTargetMetric
}

func (e *GreenOffTargetError) Error() string {
	missed := make([]string, 0, len(e.Metrics))
	for _, o := range e.Metrics {
		missed = append(missed, fmt.Sprintf("%s %s %s, target %s by %s",
			o.Metric.Name, formatMetricValue(o.Reading), o.Metric.Unit, formatMetricValue(o.Metric.Target), o.Metric.TargetDate.Format(dateFormat)))
	}
	return fmt.Sprintf("%v: green off target: %s", ErrValidation, strings.Join(missed, "; "))
}

func (e *GreenOffTargetError) Unwrap() []error { return []error{ErrGreenOffTarget, ErrValidation} }

// formatMetricValue renders a reading or target without trailing zeros.
func formatMetricValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// OffTargetMetrics returns goalID's Metrics that are off target today, in the
// Goal's timezone (see GoalTimezone), each with the reading that missed; none
// when none is. Each Metric is judged by the reading in readings (as entered in
// a Check-in) or, when there is none, its latest recorded one; a Metric never
// read is not off target. It reads the Goal's Metrics and all their readings,
// one query each.
func (s *Service) OffTargetMetrics(ctx context.Context, goalID int64, readings []MetricReadingInput) ([]OffTargetMetric, error) {
	g, err := s.loadGoal(ctx, goalID)
	if err != nil {
		return nil, err
	}
	metrics, err := s.ListMetrics(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	recorded, err := s.metricReadingsByMetric(ctx, []int64{g.ID})
	if err != nil {
		return nil, err
	}
	return checkinOffTargetMetrics(metrics, recorded, readings, s.goalToday(g)), nil
}

// offTargetMetricsAmong are those of metrics, one Goal's, off target as of
// today, a date in the Goal's timezone, each with the reading that missed:
// its reading in entered or, when there is none, its latest in recorded, each
// keyed by Metric ID. A Metric never read is not off target.
func offTargetMetricsAmong(metrics []Metric, recorded map[int64][]MetricReading, entered map[int64]float64, today time.Time) []OffTargetMetric {
	var missed []OffTargetMetric
	for _, m := range metrics {
		reading, ok := entered[m.ID]
		if !ok {
			readings := recorded[m.ID]
			if len(readings) == 0 {
				continue
			}
			reading = readings[len(readings)-1].Value
		}
		if m.OffTarget(reading, today) {
			missed = append(missed, OffTargetMetric{Metric: m, Reading: reading})
		}
	}
	return missed
}

// validateReadings checks that each reading names one of metrics, the Goal's;
// a reading for a Metric that does not exist or belongs to another Goal is
// rejected, and so is a second reading for one Metric: a Check-in records one
// value each. Only a reading naming none of metrics is looked up through
// exists, to say which it is.
func validateReadings(readings []MetricReadingInput, metrics []Metric, exists func(id int64) (bool, error)) error {
	onGoal := make(map[int64]Metric, len(metrics))
	for _, m := range metrics {
		onGoal[m.ID] = m
	}
	read := make(map[int64]bool, len(readings))
	for _, rd := range readings {
		m, ok := onGoal[rd.MetricID]
		if !ok {
			found, err := exists(rd.MetricID)
			if err != nil {
				return err
			}
			if !found {
				return Refuse(ErrValidation, "metric %d does not exist", rd.MetricID)
			}
			return Refuse(ErrValidation, "metric %d is not on this Goal", rd.MetricID)
		}
		if read[rd.MetricID] {
			return Refuse(ErrValidation, "a Check-in records one value per Metric; %q has more than one", m.Name)
		}
		read[rd.MetricID] = true
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
// Check-in is still immutable and records its own author and Owner. The Goal
// and what its verdict is judged from are read in the transaction that writes
// the new one, so a No change can't land on a Goal another Check-in has just
// closed.
func (s *Service) SubmitNoChangeCheckin(ctx context.Context, goalID, authorID int64) (Checkin, error) {
	var out Checkin
	err := s.WithinTx(ctx, func(tx *Service) error {
		g, err := tx.openGoalCommand(ctx, authorID, goalID, noChangeCheckinRefusal, nil)
		if err != nil {
			return err
		}

		facts, err := tx.noChangeFactsByGoal(ctx, []Goal{g})
		if err != nil {
			return err
		}
		verdict := judgeNoChange(facts[g.ID])
		if verdict.refusal != nil {
			return verdict.refusal
		}
		prev := *facts[g.ID].previous
		var pathTargetDate string
		if !prev.PathTargetDate.IsZero() {
			pathTargetDate = prev.PathTargetDate.Format(dateFormat)
		}
		var asksForHelp int64
		if prev.AsksForHelp {
			asksForHelp = 1
		}
		out, err = tx.createCheckin(ctx, db.CreateCheckinParams{
			GoalID:         goalID,
			AuthorID:       authorID,
			OwnerID:        g.Owner.ID,
			Health:         prev.Health,
			Status:         prev.Status,
			PathToGreen:    prev.PathToGreen,
			PathTargetDate: pathTargetDate,
			AsksForHelp:    asksForHelp,
			Explanation:    verdict.explanation,
		})
		return err
	})
	if err != nil {
		return Checkin{}, err
	}
	return out, nil
}

// NoChangeRefusal reports why a No change on goalID would be refused for any
// author allowed to check in on it, or nil when one would be recorded. It
// gives the error SubmitNoChangeCheckin would, without recording anything, so
// a page can offer the No change only when it would be taken: none while a
// Milestone is overdue or the back-to-Green date has passed. It is
// NoChangeRefusalsByGoal for the one Goal.
func (s *Service) NoChangeRefusal(ctx context.Context, goalID int64) error {
	// It judges for any allowed author, so opens the Goal for no one in
	// particular.
	g, err := s.openGoalCommand(ctx, 0, goalID, nil, nil)
	if err != nil {
		return err
	}
	if g.Lifecycle != LifecycleActive {
		return errNotActiveForNoChange
	}
	refusals, err := s.NoChangeRefusalsByGoal(ctx, []Goal{g})
	if err != nil {
		return err
	}
	return refusals[g.ID]
}

// NoChangeRefusalsByGoal reports, for each of goals, why a No change on it
// would be refused for any author allowed to check in on it, keyed by Goal ID;
// a Goal on which one would be recorded has no entry. It runs the same few
// queries however many Goals there are, so a page listing Goals can afford it.
func (s *Service) NoChangeRefusalsByGoal(ctx context.Context, goals []Goal) (map[int64]error, error) {
	facts, err := s.noChangeFactsByGoal(ctx, goals)
	if err != nil {
		return nil, err
	}
	out := map[int64]error{}
	for _, g := range goals {
		if refusal := judgeNoChange(facts[g.ID]).refusal; refusal != nil {
			out[g.ID] = refusal
		}
	}
	return out, nil
}

// noChangeFactsByGoal reads what a No change on each of goals is judged from,
// keyed by Goal ID: their latest Check-ins and their Active children's, which
// their Rolled-up Health is judged from, their Milestones, and their Metrics
// with all their readings. It runs the same few queries however many Goals
// there are, so one Goal is a batch of one; no Goals reads nothing.
func (s *Service) noChangeFactsByGoal(ctx context.Context, goals []Goal) (map[int64]noChangeFacts, error) {
	out := make(map[int64]noChangeFacts, len(goals))
	if len(goals) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(goals))
	for _, g := range goals {
		ids = append(ids, g.ID)
	}
	children, err := s.childrenByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	// One read holds the Goals' latest Check-ins and their Active children's.
	withChildren := slices.Clone(ids)
	for _, id := range ids {
		withChildren = append(withChildren, activeGoalIDs(children[id])...)
	}
	latest, err := s.LatestCheckinsByGoal(ctx, withChildren)
	if err != nil {
		return nil, err
	}
	milestones, err := s.milestonesByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	metrics, err := s.metricsByGoal(ctx, ids)
	if err != nil {
		return nil, err
	}
	readings, err := s.metricReadingsByMetric(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, g := range goals {
		var previous *Checkin
		if c, ok := latest[g.ID]; ok {
			previous = &c
		}
		out[g.ID] = s.noChangeFactsOf(g, previous, s.rollUp(children[g.ID], latest), milestones[g.ID], metrics[g.ID], readings)
	}
	return out, nil
}

// errNotActiveForNoChange and errNoCheckinToRepeat refuse a No change on a
// Goal that isn't Active, and on one with no previous Check-in to repeat.
var (
	errNotActiveForNoChange = Refuse(ErrValidation, "only an Active Goal takes a No change")
	errNoCheckinToRepeat    = Refuse(ErrValidation, "there is no previous Check-in to repeat")
)

// noChangeFacts is what judging a No change on a Goal reads of it, as of today
// in its timezone: whether it is Active, its previous Check-in (nil when it has
// none), its Rolled-up Health, its first overdue Milestone (nil when none is),
// whether its back-to-Green date has passed, and its Metrics off target by
// their latest readings.
type noChangeFacts struct {
	active             bool
	previous           *Checkin
	rollup             RolledUpHealth
	overdueMilestone   *Milestone
	pathToGreenOverdue bool
	offTarget          []OffTargetMetric
}

// noChangeFactsOf works out the facts a No change on g is judged from, given
// what was read of it: its latest Check-in, previous (nil when it has none),
// its Rolled-up Health, its Milestones soonest first, and its Metrics with
// their readings keyed by Metric ID. noChangeFactsByGoal and the Goal page
// both build them through it.
func (s *Service) noChangeFactsOf(g Goal, previous *Checkin, rollup RolledUpHealth, milestones []Milestone, metrics []Metric, readings map[int64][]MetricReading) noChangeFacts {
	today := s.goalToday(g)
	f := noChangeFacts{
		active:    g.Lifecycle == LifecycleActive,
		previous:  previous,
		rollup:    rollup,
		offTarget: offTargetMetricsAmong(metrics, readings, nil, today),
	}
	if m, ok := overdueMilestone(milestones, today); ok {
		f.overdueMilestone = &m
	}
	if previous != nil {
		f.pathToGreenOverdue = pathToGreenPastTarget(g.Lifecycle, previous.Health, previous.PathTargetDate, s.clock.Now(), s.GoalTimezone(g))
	}
	return f
}

// noChangeVerdict is the judgement of a No change on a Goal: why it would be
// refused (nil when it would be recorded) or the explanation to record with
// it, and the facts a page shows beside it, the Goal's first overdue
// Milestone and its Metrics off target, whatever refused it.
type noChangeVerdict struct {
	refusal          error
	explanation      string
	overdueMilestone *Milestone
	offTarget        []OffTargetMetric
}

// judgeNoChange judges a No change on a Goal from facts. Its refusals are
// checked in order: a Goal that isn't Active, one with no Check-in to repeat,
// a Health the Rolled-up Health now needs explaining, an overdue Milestone, a
// passed back-to-Green date, and a Green with a Metric off target.
func judgeNoChange(facts noChangeFacts) noChangeVerdict {
	explanation, refusal := noChangeRefusalOf(facts)
	return noChangeVerdict{
		refusal:          refusal,
		explanation:      explanation,
		overdueMilestone: facts.overdueMilestone,
		offTarget:        facts.offTarget,
	}
}

// noChangeRefusalOf is judgeNoChange's rule: the explanation a No change on a
// Goal of facts records, or why it is refused.
func noChangeRefusalOf(facts noChangeFacts) (string, error) {
	if !facts.active {
		return "", errNotActiveForNoChange
	}
	prev := facts.previous
	if prev == nil {
		return "", errNoCheckinToRepeat
	}
	// A no-change repeat still cannot violate the Rolled-up Health rule: if the
	// roll-up has since changed so the repeated Health would differ from it and
	// the previous Check-in carried no explanation, refuse it so the Owner uses
	// the full form to explain (ADR-0003).
	explanation, err := rollupExplanation(facts.rollup, prev.Health, prev.Explanation)
	if err != nil {
		return "", Refuse(ErrValidation, "%s — submit a Check-in to explain", Reason(err))
	}
	// Nor can it repeat any Health once a Milestone has gone overdue, as of
	// today in the Goal's timezone: the missed date needs a response.
	if m := facts.overdueMilestone; m != nil {
		return "", &MilestoneOverdueError{Milestone: *m, RulesOut: OverdueRulesOutNoChange}
	}
	// Nor once the back-to-Green date has passed, as of today in the Goal's
	// timezone, whatever the Health: the Owner must submit a Check-in with a
	// new date and reason, or update the Health (CONTEXT.md: Check-in).
	if facts.pathToGreenOverdue {
		return "", Refuse(ErrValidation, "the back-to-Green date has passed: submit a Check-in with a new date and reason, or update the Health")
	}
	// Nor a Green while a Metric is off target: only a full Check-in can
	// confirm that, as a page says beside the Metrics.
	if prev.Health == HealthGreen && len(facts.offTarget) > 0 {
		return "", &GreenOffTargetError{Metrics: facts.offTarget}
	}
	return explanation, nil
}

// rollupExplanation enforces ADR-0003's explanation rule, judgeCheckin's and
// judgeNoChange's, for a Check-in setting health on a Goal whose Rolled-up
// Health is rollup: when rollup is present and differs from health, a
// non-empty explanation is required; otherwise none is, and any stray
// explanation is dropped. It returns the explanation to store.
func rollupExplanation(rollup RolledUpHealth, health, explanation string) (string, error) {
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
	discardedBy, err := s.discardedDraftHighlightsByCheckin(ctx, goalID)
	if err != nil {
		return nil, err
	}
	return checkinHistory(rows, changes, discardedBy), nil
}

// checkinHistory is ListCheckins read from a Goal's Check-in rows, its
// Milestone changes and the Draft Highlights its Check-ins discarded, keyed by
// the Check-in.
func checkinHistory(rows []db.ListCheckinsRow, changes []db.ListMilestoneChangesByGoalRow, discardedBy map[int64][]DraftHighlight) []Checkin {
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
	out := make([]Checkin, 0, len(rows))
	for _, r := range rows {
		c := checkinFromRow(r.Checkin, r.Account, r.Account_2)
		c.MilestoneChanges = changesBy[c.ID]
		c.DiscardedDraftHighlights = discardedBy[c.ID]
		out = append(out, c)
	}
	return out
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
