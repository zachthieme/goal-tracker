package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// judgeToday is the day the judge tests judge a Check-in on.
var judgeToday = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

// judgeDate is a calendar date in March 2026 for the judge tests, or in the
// month given.
func judgeDate(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

// judgeOwner is the Owner of judgeGoal, who checks in on it.
var judgeOwner = Account{ID: 1, Email: "sam@example.com"}

// judgeGoal is an Active Dated Goal due on 2026-06-30.
var judgeGoal = Goal{
	ID: 10, Title: "Reduce outages", Owner: judgeOwner, Lifecycle: LifecycleActive,
	Kind: GoalDated, DeliveryDate: judgeDate(time.June, 30),
}

// The judge tests' Metrics and Milestones on judgeGoal: an up Metric whose
// target date has passed, one still before its target date, a Milestone
// overdue since 2026-03-01 and one due in April.
var (
	judgeMetricPassed = Metric{ID: 31, GoalID: judgeGoal.ID, Name: "Uptime", Unit: "%", Direction: MetricUp, Target: 99, TargetDate: judgeDate(time.March, 1)}
	judgeMetricAhead  = Metric{ID: 32, GoalID: judgeGoal.ID, Name: "Incidents", Unit: "incidents", Direction: MetricDown, Target: 2, TargetDate: judgeDate(time.May, 1)}
	judgeOverdue      = Milestone{ID: 41, GoalID: judgeGoal.ID, Name: "Security review", TargetDate: judgeDate(time.March, 1), Status: MilestonePlanned}
	judgeUpcoming     = Milestone{ID: 42, GoalID: judgeGoal.ID, Name: "Beta", TargetDate: judgeDate(time.April, 1), Status: MilestonePlanned}
)

// judgeActor is someone judged as an author of judgeGoal's Check-ins: acc,
// a Delegate when delegate.
func judgeActor(acc Account, delegate bool) goalActor {
	return goalActor{
		id:       acc.ID,
		account:  func() (Account, bool, error) { return acc, true, nil },
		delegate: func() (bool, error) { return delegate, nil },
	}
}

// judgeFacts are judgeGoal's facts for its Owner, today: no children to roll
// up, no Metrics, Milestones, Check-ins or Draft Highlights.
func judgeFacts() checkinFacts {
	return checkinFacts{
		goal:                   judgeGoal,
		actor:                  judgeActor(judgeOwner, false),
		today:                  judgeToday,
		rollup:                 func() (RolledUpHealth, error) { return RolledUpHealth{}, nil },
		metrics:                func() ([]Metric, error) { return nil, nil },
		readings:               func() (map[int64][]MetricReading, error) { return nil, nil },
		metricExists:           func(int64) (bool, error) { return false, nil },
		milestones:             func() ([]Milestone, error) { return nil, nil },
		latestCheckin:          func() (*Checkin, error) { return nil, nil },
		pendingDraftHighlights: func() ([]DraftHighlight, error) { return nil, nil },
	}
}

// judgeGreen is a valid Green Check-in on judgeGoal.
func judgeGreen() SubmitCheckinInput {
	return SubmitCheckinInput{GoalID: judgeGoal.ID, AuthorID: judgeOwner.ID, Health: HealthGreen, Status: "On track."}
}

// judgeYellow is a valid Yellow Check-in on judgeGoal, back to Green by
// 2026-04-30.
func judgeYellow() SubmitCheckinInput {
	in := judgeGreen()
	in.Health, in.PathToGreen, in.PathTargetDate = HealthYellow, "A second vendor.", judgeDate(time.April, 30)
	return in
}

// withMetrics gives f judgeGoal's two Metrics, Uptime last read at 99.5.
func withMetrics(f *checkinFacts) {
	f.metrics = func() ([]Metric, error) { return []Metric{judgeMetricPassed, judgeMetricAhead}, nil }
	f.readings = func() (map[int64][]MetricReading, error) {
		return map[int64][]MetricReading{judgeMetricPassed.ID: {{MetricID: judgeMetricPassed.ID, Value: 99.5}}}, nil
	}
}

// withMilestones gives f judgeGoal's overdue and upcoming Milestones.
func withMilestones(f *checkinFacts) {
	f.milestones = func() ([]Milestone, error) { return []Milestone{judgeOverdue, judgeUpcoming}, nil }
}

// refusedAs checks the refusal is a *T.
func refusedAs[T error](t *testing.T, _ checkinPlan, err error) {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Errorf("judgeCheckin refused with %T %v; want a %T", err, err, target)
	}
}

// refusedSaying checks the refusal's text says want.
func refusedSaying(want string) func(t *testing.T, p checkinPlan, err error) {
	return func(t *testing.T, _ checkinPlan, err error) {
		t.Helper()
		if !strings.Contains(err.Error(), want) {
			t.Errorf("judgeCheckin refused with %q; want it to say %q", err, want)
		}
	}
}

// judgeCheckin refuses each rule's breach as SubmitCheckin does, with the
// refusal's kind, type and input name, and plans a Check-in that breaks none.
func TestJudgeCheckinAppliesEachRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		in    func(in *SubmitCheckinInput)
		facts func(f *checkinFacts)
		// want is the refusal's kind, or nil for a plan; input is the input
		// an *InputError names, "" for a refusal that names none.
		want  error
		input string
		// check inspects the plan or the refusal further.
		check func(t *testing.T, p checkinPlan, err error)
	}{
		{
			name:  "not the Owner or a Delegate",
			facts: func(f *checkinFacts) { f.actor = judgeActor(Account{ID: 2, Email: "kim@example.com"}, false) },
			want:  ErrNotAuthorized,
		},
		{
			name:  "a Delegate",
			facts: func(f *checkinFacts) { f.actor = judgeActor(Account{ID: 2, Email: "kim@example.com"}, true) },
			check: func(t *testing.T, p checkinPlan, _ error) {
				if p.row.AuthorID != judgeOwner.ID || p.row.OwnerID != judgeOwner.ID {
					t.Errorf("plan's row has author %d, Owner %d; want the input's author and the Goal's Owner, %d", p.row.AuthorID, p.row.OwnerID, judgeOwner.ID)
				}
			},
		},
		{
			name:  "a Lifecycle change the actor may not make",
			in:    func(in *SubmitCheckinInput) { in.Lifecycle = LifecycleProposed },
			want:  ErrValidation,
			input: CheckinInputLifecycle,
		},
		{
			name:  "a bad Health",
			in:    func(in *SubmitCheckinInput) { in.Health = "Blue" },
			want:  ErrValidation,
			input: CheckinInputHealth,
		},
		{
			name:  "no status",
			in:    func(in *SubmitCheckinInput) { in.Status = "  " },
			want:  ErrValidation,
			input: CheckinInputStatus,
		},
		{
			name: "no status while putting the Goal On Hold",
			in: func(in *SubmitCheckinInput) {
				in.Status, in.Lifecycle, in.LifecycleReason = "", LifecycleOnHold, "Funding paused."
			},
			want:  ErrValidation,
			input: CheckinInputStatus,
		},
		{
			name:  "Yellow without a Path to Green",
			in:    func(in *SubmitCheckinInput) { *in = judgeYellow(); in.PathToGreen = "" },
			want:  ErrValidation,
			input: CheckinInputPathToGreen,
		},
		{
			name: "a Health differing from the Rolled-up Health without an explanation",
			facts: func(f *checkinFacts) {
				f.rollup = func() (RolledUpHealth, error) { return RolledUpHealth{Health: HealthRed, Present: true}, nil }
			},
			want:  ErrValidation,
			input: CheckinInputExplanation,
		},
		{
			name: "a reading for a Metric on another Goal",
			in:   func(in *SubmitCheckinInput) { in.Readings = []MetricReadingInput{{MetricID: 99, Value: 1}} },
			facts: func(f *checkinFacts) {
				withMetrics(f)
				f.metricExists = func(id int64) (bool, error) { return id == 99, nil }
			},
			want:  ErrValidation,
			check: refusedSaying("metric 99 is not on this Goal"),
		},
		{
			name:  "a reading for a Metric that doesn't exist",
			in:    func(in *SubmitCheckinInput) { in.Readings = []MetricReadingInput{{MetricID: 99, Value: 1}} },
			facts: withMetrics,
			want:  ErrValidation,
			check: refusedSaying("metric 99 does not exist"),
		},
		{
			name: "two readings for one Metric",
			in: func(in *SubmitCheckinInput) {
				in.Readings = []MetricReadingInput{{MetricID: judgeMetricAhead.ID, Value: 1}, {MetricID: judgeMetricAhead.ID, Value: 2}}
			},
			facts: withMetrics,
			want:  ErrValidation,
			check: refusedSaying(`"Incidents" has more than one`),
		},
		{
			name: "Done without a final value for every Metric",
			in: func(in *SubmitCheckinInput) {
				in.Lifecycle, in.Outcome = LifecycleDone, "Shipped."
				in.Readings = []MetricReadingInput{{MetricID: judgeMetricPassed.ID, Value: 99.9}}
			},
			facts: withMetrics,
			want:  ErrValidation,
			input: CheckinReadingInput(judgeMetricAhead.ID),
		},
		{
			name: "a Green that moves the delivery date later",
			in: func(in *SubmitCheckinInput) {
				in.DeliveryDate, in.DeliveryDateReason = judgeDate(time.July, 31), "Vendor slipped."
			},
			want:  ErrValidation,
			input: CheckinInputDeliveryDate,
		},
		{
			name: "a Milestone change naming a Milestone not on the Goal",
			in: func(in *SubmitCheckinInput) {
				in.Milestones = []MilestoneChangeInput{{MilestoneID: 99, Status: MilestoneDone}}
			},
			facts: withMilestones,
			want:  ErrValidation,
			check: refusedSaying("milestone 99 is not on this Goal"),
		},
		{
			name:  "a Green with an overdue Milestone",
			facts: withMilestones,
			want:  ErrValidation,
			input: CheckinMilestoneInput(judgeOverdue.ID, "date"),
			check: refusedAs[*MilestoneOverdueError],
		},
		{
			name: "a Green with an overdue Milestone the Check-in moves",
			in: func(in *SubmitCheckinInput) {
				in.Milestones = []MilestoneChangeInput{{MilestoneID: judgeOverdue.ID, TargetDate: judgeDate(time.March, 20), DateReason: "Auditor's calendar."}}
			},
			facts: withMilestones,
			check: func(t *testing.T, p checkinPlan, _ error) {
				if len(p.milestones.slips) != 1 || p.milestones.slips[0].milestoneID != judgeOverdue.ID {
					t.Errorf("plan's Milestone slips = %+v; want the overdue Milestone's", p.milestones.slips)
				}
			},
		},
		{
			name: "an unconfirmed Green with a Metric off target",
			in: func(in *SubmitCheckinInput) {
				in.Readings = []MetricReadingInput{{MetricID: judgeMetricPassed.ID, Value: 97}}
			},
			facts: withMetrics,
			want:  ErrGreenOffTarget,
			check: refusedAs[*GreenOffTargetError],
		},
		{
			name: "a confirmed Green with a Metric off target",
			in: func(in *SubmitCheckinInput) {
				in.Readings = []MetricReadingInput{{MetricID: judgeMetricPassed.ID, Value: 97}}
				in.ConfirmOffTarget = true
			},
			facts: withMetrics,
			check: func(t *testing.T, p checkinPlan, _ error) {
				if p.row.Health != HealthGreen {
					t.Errorf("plan's Health = %q; want Green", p.row.Health)
				}
			},
		},
		{
			name: "a later back-to-Green date without a reason",
			in:   func(in *SubmitCheckinInput) { *in = judgeYellow() },
			facts: func(f *checkinFacts) {
				f.latestCheckin = func() (*Checkin, error) {
					return &Checkin{Health: HealthYellow, PathTargetDate: judgeDate(time.April, 15)}, nil
				}
			},
			want:  ErrValidation,
			input: CheckinInputPathTargetDateReason,
		},
		{
			name: "a valid Yellow moving the back-to-Green date later",
			in: func(in *SubmitCheckinInput) {
				*in = judgeYellow()
				in.PathTargetDateReason, in.AsksForHelp = "Vendor slipped.", true
			},
			facts: func(f *checkinFacts) {
				f.latestCheckin = func() (*Checkin, error) {
					return &Checkin{Health: HealthYellow, PathTargetDate: judgeDate(time.April, 15)}, nil
				}
			},
			check: func(t *testing.T, p checkinPlan, _ error) {
				want := slipPlan{backToGreen: true, oldDate: judgeDate(time.April, 15), newDate: judgeDate(time.April, 30), reason: "Vendor slipped."}
				if p.pathSlip == nil || *p.pathSlip != want {
					t.Errorf("plan's back-to-Green slip = %+v; want %+v", p.pathSlip, want)
				}
				if p.row.Health != HealthYellow || p.row.PathTargetDate != "2026-04-30" || p.row.AsksForHelp != 1 {
					t.Errorf("plan's row = %+v; want a Yellow back to Green by 2026-04-30, asking for help", p.row)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, facts := judgeGreen(), judgeFacts()
			if tc.in != nil {
				tc.in(&in)
			}
			if tc.facts != nil {
				tc.facts(&facts)
			}
			plan, err := judgeCheckin(in, facts)
			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("judgeCheckin refused: %v; want a plan", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("judgeCheckin = %v; want %v", err, tc.want)
			}
			var ie *InputError
			if errors.As(err, &ie) != (tc.input != "") || (ie != nil && ie.Input != tc.input) {
				t.Errorf("judgeCheckin refused %v naming input %q; want %q", err, inputOf(ie), tc.input)
			}
			if tc.check != nil {
				tc.check(t, plan, err)
			}
		})
	}
}

// A refusal reached before a rule needs a fact never asks for it: a Health
// the Rolled-up Health needs explained reads neither the Metrics, their
// readings, the Milestones, the latest Check-in nor the Draft Highlights,
// although the Check-in names a Metric and offers a Draft Highlight.
func TestJudgeCheckinAsksForNoFactPastItsRefusal(t *testing.T) {
	t.Parallel()
	facts := judgeFacts()
	facts.rollup = func() (RolledUpHealth, error) { return RolledUpHealth{Health: HealthRed, Present: true}, nil }
	asked := map[string]bool{}
	facts.metrics = func() ([]Metric, error) { asked["metrics"] = true; return nil, nil }
	facts.readings = func() (map[int64][]MetricReading, error) { asked["readings"] = true; return nil, nil }
	facts.metricExists = func(int64) (bool, error) { asked["metricExists"] = true; return false, nil }
	facts.milestones = func() ([]Milestone, error) { asked["milestones"] = true; return nil, nil }
	facts.latestCheckin = func() (*Checkin, error) { asked["latestCheckin"] = true; return nil, nil }
	facts.pendingDraftHighlights = func() ([]DraftHighlight, error) { asked["pendingDraftHighlights"] = true; return nil, nil }
	in := judgeYellow()
	in.Readings = []MetricReadingInput{{MetricID: judgeMetricPassed.ID, Value: 97}}
	in.Highlights = []HighlightInput{{Kind: HighlightInsight, Note: "Vendors matter.", DraftHighlightID: 7}}

	_, err := judgeCheckin(in, facts)
	var ie *InputError
	if !errors.As(err, &ie) || ie.Input != CheckinInputExplanation {
		t.Fatalf("judgeCheckin = %v; want the explanation refused", err)
	}
	for fact := range asked {
		t.Errorf("judgeCheckin asked for the Goal's %s before refusing for the explanation", fact)
	}
}

// inputOf is the input ie names, "" when ie is nil.
func inputOf(ie *InputError) string {
	if ie == nil {
		return ""
	}
	return ie.Input
}

// A Goal's current Health is its latest Check-in's only while it is Active:
// a Red Check-in reads Red on an Active Goal and "" on a Proposed, On Hold,
// Done or Cancelled one, and an Active Goal with no Check-in reads "" (#640).
func TestCurrentHealthReadsTheLatestCheckinOnlyWhileActive(t *testing.T) {
	t.Parallel()
	red := &Checkin{GoalID: judgeGoal.ID, Health: HealthRed}
	cases := []struct {
		name      string
		lifecycle string
		latest    *Checkin
		want      string
	}{
		{"Active", LifecycleActive, red, HealthRed},
		{"Proposed", LifecycleProposed, red, ""},
		{"On Hold", LifecycleOnHold, red, ""},
		{"Done", LifecycleDone, red, ""},
		{"Cancelled", LifecycleCancelled, red, ""},
		{"Active with no Check-in", LifecycleActive, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := judgeGoal
			g.Lifecycle = c.lifecycle
			if got := currentHealth(g, c.latest); got != c.want {
				t.Errorf("currentHealth(%s Goal) = %q, want %q", c.lifecycle, got, c.want)
			}
		})
	}
}
