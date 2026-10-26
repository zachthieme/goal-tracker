package domain

import (
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
)

// checkinFacts is what a Check-in on a Goal is judged from: the Goal, who is
// checking in, and today, a date in the Goal's timezone, with each other fact
// a rule may need as a function, as goalActor holds its facts, called only
// when a rule asks.
type checkinFacts struct {
	goal  Goal
	actor goalActor
	today time.Time
	// rollup is the Goal's Rolled-up Health (ADR-0003).
	rollup func() (RolledUpHealth, error)
	// metrics are the Goal's Metrics, and readings their recorded readings,
	// earliest first, keyed by Metric ID; a Metric never read has no entry.
	metrics  func() ([]Metric, error)
	readings func() (map[int64][]MetricReading, error)
	// metricExists says whether a Metric with the ID exists, on any Goal.
	metricExists func(id int64) (bool, error)
	// milestones are the Goal's Milestones, soonest first.
	milestones func() ([]Milestone, error)
	// latestCheckin is the Goal's latest Check-in, nil when it has none.
	latestCheckin func() (*Checkin, error)
	// pendingDraftHighlights are the Goal's pending Draft Highlights.
	pendingDraftHighlights func() ([]DraftHighlight, error)
}

// checkinPlan is what a Check-in judgeCheckin accepts writes: its row, the
// Lifecycle change it makes, its delivery date's and back-to-Green date's
// Date Slips (nil for none), its Milestone changes, its Highlights, and what
// it does with the Draft Highlights it offered.
type checkinPlan struct {
	row          db.CreateCheckinParams
	lifecycle    LifecycleChange
	deliverySlip *slipPlan
	pathSlip     *slipPlan
	milestones   milestonePlan
	highlights   []HighlightInput
	picks        draftHighlightPicks
}

// judgeCheckin judges the Check-in in on a Goal from facts, refusing it for
// the first rule it breaks or planning what it writes. Its rules, in order:
// who may check in and move the Goal's Lifecycle, and how; the Health, status
// and Path to Green, and the explanation a Health differing from the
// Rolled-up Health needs; the readings, and a Done's final values; the
// delivery date; the Milestone changes; and the Highlights. A Green is
// refused while the delivery date moves later, while a Milestone is overdue
// as the Check-in leaves them, and, unconfirmed, while a Metric is off target.
func judgeCheckin(in SubmitCheckinInput, facts checkinFacts) (checkinPlan, error) {
	// The Owner writes a Goal's Check-ins, as may any Delegate the Owner has
	// authorized; the Check-in records its author and still credits the Owner it
	// speaks for (CONTEXT.md: Delegate). One that leaves its Lifecycle as it
	// is must be allowed to (lifecycleKeptRefusal), and one that moves it moves
	// it only as LifecycleChangesFrom allows (lifecycleChangeRefusal); the plan
	// then asks only for the move's reason or outcome.
	g := facts.goal
	refusal := lifecycleKeptRefusal(facts.actor, g)
	if in.Lifecycle != "" && in.Lifecycle != g.Lifecycle {
		refusal = lifecycleChangeRefusal(facts.actor, g, in.Lifecycle)
	}
	if refusal != nil {
		return checkinPlan{}, refusal
	}
	lifecycle, err := planLifecycleChange(g.Lifecycle, in.Lifecycle, in.LifecycleReason, in.Outcome)
	if err != nil {
		return checkinPlan{}, err
	}

	status := strings.TrimSpace(in.Status)
	path := strings.TrimSpace(in.PathToGreen)
	explanation := strings.TrimSpace(in.Explanation)
	// Health is how an Active Goal is tracking, so only a Check-in that leaves
	// the Goal Active sets one; the rest record just their status.
	health := in.Health
	if lifecycle.resultingLifecycle(g.Lifecycle) == LifecycleActive {
		if err := validateCheckin(health, status, path, in.PathTargetDate); err != nil {
			return checkinPlan{}, err
		}
		// The Owner has to explain why their Health differs from the Rolled-up
		// Health — the worst among the Goal's Active children (ADR-0003). When
		// there is nothing to roll up, or the two match, no explanation is
		// required and any stray one is dropped.
		rollup, err := facts.rollup()
		if err != nil {
			return checkinPlan{}, err
		}
		if explanation, err = rollupExplanation(rollup, health, explanation); err != nil {
			return checkinPlan{}, err
		}
	} else {
		if status == "" {
			return checkinPlan{}, inputError(CheckinInputStatus, "a Check-in needs a status")
		}
		health, path, explanation = "", "", ""
	}

	// A reading may only name a Metric on this Goal (CONTEXT.md: a Check-in
	// records the current value of the Goal's Metrics).
	if len(in.Readings) > 0 {
		metrics, err := facts.metrics()
		if err != nil {
			return checkinPlan{}, err
		}
		if err := validateReadings(in.Readings, metrics, facts.metricExists); err != nil {
			return checkinPlan{}, err
		}
	}
	if lifecycle.To == LifecycleDone {
		metrics, err := facts.metrics()
		if err != nil {
			return checkinPlan{}, err
		}
		if err := requireFinalValues(metrics, in.Readings); err != nil {
			return checkinPlan{}, err
		}
	}
	deliverySlip, err := planDeliverySlip(g, in.DeliveryDate, in.DeliveryDateReason)
	if err != nil {
		return checkinPlan{}, err
	}
	// A slip is never hidden behind a Green: moving the delivery date later
	// means the Goal is not on track (CONTEXT.md: Date Slip).
	if health == HealthGreen && deliverySlip != nil && deliverySlip.later() {
		return checkinPlan{}, inputError(CheckinInputDeliveryDate, "a Check-in that moves the delivery date later can't be Green")
	}
	// A Milestone slip that doesn't move the delivery date doesn't affect Health
	// (CONTEXT.md: Milestone), so it is recorded but never gates the Health.
	current, err := facts.milestones()
	if err != nil {
		return checkinPlan{}, err
	}
	milestones, err := planMilestoneChanges(current, in.Milestones, in.NewMilestones)
	if err != nil {
		return checkinPlan{}, err
	}
	if err := rejectGreenWhileOverdue(health, milestones.resulting, facts.today); err != nil {
		return checkinPlan{}, err
	}
	// Health is the Owner's call (ADR-0003), so a Metric that missed its target
	// only asks them to confirm a Green; an overdue Milestone, refused above,
	// wins over it.
	if health == HealthGreen && !in.ConfirmOffTarget {
		metrics, err := facts.metrics()
		if err != nil {
			return checkinPlan{}, err
		}
		recorded, err := facts.readings()
		if err != nil {
			return checkinPlan{}, err
		}
		if missed := checkinOffTargetMetrics(metrics, recorded, in.Readings, facts.today); len(missed) > 0 {
			return checkinPlan{}, &GreenOffTargetError{Metrics: missed}
		}
	}

	// Moving the back-to-Green date later than the latest Check-in had it is a
	// Date Slip of that date (CONTEXT.md: Date Slip).
	var pathSlip *slipPlan
	pathTargetDate := ""
	asksForHelp := int64(0)
	if needsPathToGreen(health) {
		latest, err := facts.latestCheckin()
		if err != nil {
			return checkinPlan{}, err
		}
		if latest != nil {
			if pathSlip, err = planBackToGreenSlip(latest.PathTargetDate, in.PathTargetDate, in.PathTargetDateReason); err != nil {
				return checkinPlan{}, err
			}
		}
		pathTargetDate = in.PathTargetDate.Format(dateFormat)
		if in.AsksForHelp {
			asksForHelp = 1
		}
	} else {
		// A Green Check-in carries no Path to Green, so no ask for help.
		path = ""
	}

	// Which Draft Highlights are still pending decides which offered rows
	// count, and the Check-in clears them, so two can't both keep one.
	picks, err := planDraftHighlightPicks(in.Highlights, facts.pendingDraftHighlights)
	if err != nil {
		return checkinPlan{}, err
	}
	highlights, err := planHighlights(picks.rows)
	if err != nil {
		return checkinPlan{}, err
	}
	return checkinPlan{
		row: db.CreateCheckinParams{
			GoalID:          g.ID,
			AuthorID:        in.AuthorID,
			OwnerID:         g.Owner.ID,
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
		},
		lifecycle:    lifecycle,
		deliverySlip: deliverySlip,
		pathSlip:     pathSlip,
		milestones:   milestones,
		highlights:   highlights,
		picks:        picks,
	}, nil
}

// checkinOffTargetMetrics are those of metrics, one Goal's, off target as of
// today, a date in the Goal's timezone, each with the reading that missed:
// its reading among typed, as entered in a Check-in, or, when there is none,
// its latest in recorded, keyed by Metric ID (see offTargetMetricsAmong).
func checkinOffTargetMetrics(metrics []Metric, recorded map[int64][]MetricReading, typed []MetricReadingInput, today time.Time) []OffTargetMetric {
	entered := make(map[int64]float64, len(typed))
	for _, rd := range typed {
		entered[rd.MetricID] = rd.Value
	}
	return offTargetMetricsAmong(metrics, recorded, entered, today)
}
