package domain_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The activation gate (CONTEXT.md: Lifecycle; AC on issue #3): Proposed → Active
// is rejected unless the Goal has a Why it matters, an Owner, and either a delivery
// date plus at least one Milestone or Metric (Dated) or at least one Metric
// (Ongoing). Each missing item comes back as its own validation error.

var futureDate = time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

func addMilestone(t *testing.T, h *testsupport.Harness, goalID int64) {
	t.Helper()
	if _, err := h.Service.AddMilestoneAsAuthor(context.Background(), h.GoalOwnerID(goalID), domain.AddMilestoneInput{
		GoalID:     goalID,
		Name:       "Beta cut",
		TargetDate: futureDate,
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
}

func addMetric(t *testing.T, h *testsupport.Harness, goalID int64) {
	t.Helper()
	if _, err := h.Service.AddMetric(context.Background(), h.GoalOwnerID(goalID), domain.AddMetricInput{
		GoalID:     goalID,
		Name:       "p95 latency",
		Unit:       "ms",
		Direction:  domain.MetricDown,
		Baseline:   1200,
		Target:     400,
		TargetDate: futureDate,
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
}

func TestActivateDatedGoalWithAMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	addMilestone(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateDatedGoalWithAMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	addMetric(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateOngoingGoalWithAMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), g.Owner.ID, g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	addMetric(t, h, g.ID)

	got, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

func TestActivateRejectsGoalNotMarkedDatedOrOngoing(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	_, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Dated") || !strings.Contains(err.Error(), "Ongoing") {
		t.Errorf("error should name the missing choice; got %q", err.Error())
	}
}

func TestActivateRejectsDatedGoalWithNoMilestoneOrMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := h.Service.MarkGoalDated(context.Background(), g.Owner.ID, g.ID, futureDate); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}

	_, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Milestone or Metric") {
		t.Errorf("error should name the missing Milestone or Metric; got %q", err.Error())
	}
}

func TestActivateRejectsOngoingGoalWithNoMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Keep the lights on", "Uptime keeps customers.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), g.Owner.ID, g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}

	_, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if !strings.Contains(err.Error(), "Metric") {
		t.Errorf("error should name the missing Metric; got %q", err.Error())
	}
}

// A Dated Goal that is missing both a delivery date and a Milestone/Metric comes
// back with each missing item as its own validation error, so the Owner sees the
// full list at once rather than one at a time. (Reached by activating a Goal
// whose kind is Dated but which has neither a date nor any child — see the gate
// contract on issue #3.)
func TestActivateReportsEveryMissingItem(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	_, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	reasons := domain.ActivationReasons(err)
	if len(reasons) == 0 {
		t.Fatalf("want the gate failures enumerated as separate reasons; got %v", err)
	}
	for _, r := range reasons {
		if !errors.Is(r, domain.ErrValidation) {
			t.Errorf("reason %q does not wrap ErrValidation", r)
		}
	}
}

func TestActivateLeavesGoalProposedWhenRejected(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	if _, err := h.Service.ActivateGoal(context.Background(), g.Owner.ID, g.ID); err == nil {
		t.Fatal("want the activation to be rejected")
	}
	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want it to stay %q", got.Lifecycle, domain.LifecycleProposed)
	}
}

// Only a Proposed Goal can be activated (CONTEXT.md: Lifecycle). One that is
// already Active, On Hold, or has ended is refused before the minimum standard
// is checked, and keeps its Lifecycle and the time it first became Active.
func TestActivateRefusesGoalThatIsNotProposed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lifecycle string
		arrange   func(h *testsupport.Harness, owner domain.Account) domain.Goal
	}{
		{domain.LifecycleActive, func(h *testsupport.Harness, owner domain.Account) domain.Goal {
			return h.ActiveGoal(owner, "Ship v2", "Customers wait too long for v2.")
		}},
		{domain.LifecycleOnHold, func(h *testsupport.Harness, owner domain.Account) domain.Goal {
			return h.OnHoldGoal(owner, "Ship v2", "Customers wait too long for v2.", "Waiting on legal.")
		}},
		{domain.LifecycleDone, func(h *testsupport.Harness, owner domain.Account) domain.Goal {
			g := h.ActiveGoal(owner, "Ship v2", "Customers wait too long for v2.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleDone)
			return g
		}},
		{domain.LifecycleCancelled, func(h *testsupport.Harness, owner domain.Account) domain.Goal {
			g := h.ActiveGoal(owner, "Ship v2", "Customers wait too long for v2.")
			h.EndGoalInCheckin(owner, g.ID, domain.LifecycleCancelled)
			return g
		}},
	}
	for _, tt := range tests {
		t.Run(tt.lifecycle, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			owner := h.SignIn("sam@example.com")
			g := tt.arrange(h, owner)
			activatedAt := g.ActivatedAt
			h.Clock.Advance(48 * time.Hour)

			_, err := h.Service.ActivateGoal(context.Background(), owner.ID, g.ID)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			want := "Ship v2 is " + tt.lifecycle + ", so it can't be activated"
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to say %q", err.Error(), want)
			}
			got, err := h.Service.ViewGoal(context.Background(), g.ID)
			if err != nil {
				t.Fatalf("ViewGoal: %v", err)
			}
			if got.Lifecycle != tt.lifecycle {
				t.Errorf("Lifecycle = %q, want it to stay %q", got.Lifecycle, tt.lifecycle)
			}
			if !got.ActivatedAt.Equal(activatedAt) {
				t.Errorf("ActivatedAt = %v, want it to stay %v", got.ActivatedAt, activatedAt)
			}
		})
	}
}

// JudgeDefinition judges the definition command's input, with what the
// Proposed Goal being defined has saved, against the activation standard:
// each requirement in checklist order, whether it is met, the input that
// meets it, and the reason ActivateGoal would give while it is unmet.
func TestJudgeDefinition(t *testing.T) {
	t.Parallel()

	const (
		whyReason       = "a Goal must say why it matters to become Active"
		kindReason      = "a Goal must be marked Dated or Ongoing to become Active"
		dateReason      = "a Dated Goal needs a delivery date"
		datedMeasure    = "a Dated Goal needs at least one Milestone or Metric"
		ongoingMeasure  = "an Ongoing Goal needs at least one Metric"
		pillarReason    = "a Goal needs a value in Pillar to become Active"
		budgetReason    = "a Goal needs a value in Budget to become Active"
		why             = "Customers wait too long for v2."
		pillarID        = int64(7)
		growthID        = int64(71)
		budgetID        = int64(9)
		retiredDimID    = int64(8)
		retiredFieldID  = int64(10)
		extendableDimID = int64(11)
	)
	owner := domain.ActivationRequirement{Rule: domain.ActivationOwner, Met: true}
	whyMet := domain.ActivationRequirement{Rule: domain.ActivationWhyItMatters, Met: true, Input: domain.InputWhyItMatters}
	ongoing := domain.ActivationRequirement{Rule: domain.ActivationKind, Met: true, Input: domain.InputKind}
	datedMet := domain.ActivationRequirement{Rule: domain.ActivationKind, Met: true, Input: domain.InputDeliveryDate}
	metricMet := domain.ActivationRequirement{Rule: domain.ActivationMetric, Met: true, Input: domain.InputHowYoullKnow}
	measureMet := domain.ActivationRequirement{Rule: domain.ActivationMilestoneOrMetric, Met: true, Input: domain.InputHowYoullKnow}
	pillar := domain.Dimension{ID: pillarID, Name: "Pillar", Required: true, Values: []domain.DimensionValue{{ID: growthID, DimensionID: pillarID, Value: "Growth"}}}
	team := domain.Dimension{ID: extendableDimID, Name: "Team", Required: true, List: domain.ListExtendable}
	retiredDim := domain.Dimension{ID: retiredDimID, Name: "Region", Required: true, Retired: true}
	budget := domain.Field{ID: budgetID, Name: "Budget", Type: domain.FieldNumber, Required: true}
	retiredField := domain.Field{ID: retiredFieldID, Name: "Cost centre", Type: domain.FieldShortText, Required: true, Retired: true}
	optionalField := domain.Field{ID: 12, Name: "Notes", Type: domain.FieldShortText}
	milestone := []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate}}
	metric := []domain.MetricDefinition{{Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown, Baseline: 1200, Target: 400}}

	tests := []struct {
		name   string
		in     domain.DefinedGoalInput
		saved  domain.SavedDefinition
		dims   []domain.Dimension
		fields []domain.Field
		want   []domain.ActivationRequirement
	}{
		{
			name: "nothing typed",
			in:   domain.DefinedGoalInput{WhyItMatters: "  "},
			want: []domain.ActivationRequirement{
				{Rule: domain.ActivationWhyItMatters, Input: domain.InputWhyItMatters, Reason: whyReason},
				owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				{Rule: domain.ActivationMilestoneOrMetric, Input: domain.InputHowYoullKnow},
			},
		},
		{
			name: "no Kind with a Milestone",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Milestones: milestone},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				measureMet,
			},
		},
		{
			name: "no Kind with a Metric",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Metrics: metric},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				measureMet,
			},
		},
		{
			name: "Dated without a date or a Milestone or Metric",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalDated},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputDeliveryDate, Reason: dateReason},
				{Rule: domain.ActivationMilestoneOrMetric, Input: domain.InputHowYoullKnow, Reason: datedMeasure},
			},
		},
		{
			name: "Dated with a date and a Milestone",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalDated, DeliveryDate: futureDate, Milestones: milestone},
			want: []domain.ActivationRequirement{whyMet, owner, datedMet, measureMet},
		},
		{
			name: "Dated with a date and a Metric",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalDated, DeliveryDate: futureDate, Metrics: metric},
			want: []domain.ActivationRequirement{whyMet, owner, datedMet, measureMet},
		},
		{
			name: "Ongoing with only a Milestone",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing, Milestones: milestone},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing,
				{Rule: domain.ActivationMetric, Input: domain.InputHowYoullKnow, Reason: ongoingMeasure},
			},
		},
		{
			name: "Ongoing with a Metric",
			in:   domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric},
			want: []domain.ActivationRequirement{whyMet, owner, ongoing, metricMet},
		},
		{
			name:   "required Dimension and Field not set, Retired ones left out",
			in:     domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric, FieldValues: map[int64]string{budgetID: "  "}},
			dims:   []domain.Dimension{pillar, retiredDim},
			fields: []domain.Field{optionalField, budget, retiredField},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Pillar", Input: domain.DimensionInput(pillarID), Reason: pillarReason},
				{Rule: domain.ActivationValue, Name: "Budget", Input: domain.FieldInput(budgetID), Reason: budgetReason},
			},
		},
		{
			name: "required Dimension set by a chosen value, Field by a typed one",
			in: domain.DefinedGoalInput{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric,
				ValueIDs:    []int64{growthID},
				FieldValues: map[int64]string{budgetID: "1200", retiredFieldID: "CC-1"},
			},
			dims:   []domain.Dimension{pillar, retiredDim},
			fields: []domain.Field{budget, retiredField},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Pillar", Met: true, Input: domain.DimensionInput(pillarID)},
				{Rule: domain.ActivationValue, Name: "Budget", Met: true, Input: domain.FieldInput(budgetID)},
			},
		},
		{
			name: "required Extendable Dimension set by a new value, Fixed one not",
			in: domain.DefinedGoalInput{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric,
				NewValues: map[int64]string{extendableDimID: "Platform", pillarID: "Safety"},
			},
			dims: []domain.Dimension{pillar, team},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Pillar", Input: domain.DimensionInput(pillarID), Reason: pillarReason},
				{Rule: domain.ActivationValue, Name: "Team", Met: true, Input: domain.DimensionInput(extendableDimID)},
			},
		},
		{
			name: "required Extendable Dimension with a blank new value",
			in: domain.DefinedGoalInput{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric,
				NewValues: map[int64]string{extendableDimID: " "},
			},
			dims: []domain.Dimension{team},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Team", Input: domain.DimensionInput(extendableDimID), Reason: "a Goal needs a value in Team to become Active"},
			},
		},
		{
			name:  "only the saved counts carry the measure",
			in:    domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing},
			saved: domain.SavedDefinition{Metrics: 1},
			want:  []domain.ActivationRequirement{whyMet, owner, ongoing, metricMet},
		},
		{
			name:  "no Kind posted, saved Dated with a date",
			in:    domain.DefinedGoalInput{WhyItMatters: why},
			saved: domain.SavedDefinition{Kind: domain.GoalDated, DeliveryDate: futureDate, Milestones: 1},
			want:  []domain.ActivationRequirement{whyMet, owner, datedMet, measureMet},
		},
		{
			name:  "no Kind posted, saved Ongoing",
			in:    domain.DefinedGoalInput{WhyItMatters: why, Metrics: metric},
			saved: domain.SavedDefinition{Kind: domain.GoalOngoing},
			want:  []domain.ActivationRequirement{whyMet, owner, ongoing, metricMet},
		},
		{
			name:   "required Field not posted, with a saved value",
			in:     domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric},
			saved:  domain.SavedDefinition{FieldValues: map[int64]string{budgetID: "1200"}},
			fields: []domain.Field{budget},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Budget", Met: true, Input: domain.FieldInput(budgetID)},
			},
		},
		{
			name:   "required Field posted blank, with a saved value",
			in:     domain.DefinedGoalInput{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: metric, FieldValues: map[int64]string{budgetID: " "}},
			saved:  domain.SavedDefinition{FieldValues: map[int64]string{budgetID: "1200"}},
			fields: []domain.Field{budget},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Budget", Input: domain.FieldInput(budgetID), Reason: budgetReason},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.JudgeDefinition(tt.in, tt.saved, tt.dims, tt.fields)
			if !slices.Equal(got, tt.want) {
				t.Errorf("JudgeDefinition =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

// The inputs the activation standard names are the New goal form's.
func TestActivationInputs(t *testing.T) {
	t.Parallel()

	if domain.InputHowYoullKnow != "know" {
		t.Errorf("InputHowYoullKnow = %q, want %q", domain.InputHowYoullKnow, "know")
	}
	if got := domain.DimensionInput(42); got != "dimension:42" {
		t.Errorf("DimensionInput(42) = %q, want %q", got, "dimension:42")
	}
}

// The Goal page judges a saved Goal against the activation standard as
// ActivateGoal does: each unmet requirement's Reason is one ActivateGoal
// refuses with, in order, and its value requirements are the Goal's required
// Dimensions and Fields, one for one.
func TestGoalPageJudgesActivationAsActivateGoalDoes(t *testing.T) {
	t.Parallel()

	type judged struct {
		rule   domain.ActivationRule
		met    bool
		reason string
	}
	met := func(rule domain.ActivationRule) judged { return judged{rule: rule, met: true} }
	tests := []struct {
		name    string
		arrange func(h *testsupport.Harness, boss, sam domain.Account) domain.Goal
		want    []judged
	}{
		{
			name: "a bare Goal",
			arrange: func(h *testsupport.Harness, _, sam domain.Account) domain.Goal {
				return h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
			},
			want: []judged{
				met(domain.ActivationWhyItMatters), met(domain.ActivationOwner),
				{domain.ActivationKind, false, "a Goal must be marked Dated or Ongoing to become Active"},
				{domain.ActivationMilestoneOrMetric, false, ""},
			},
		},
		{
			name: "a Dated Goal with a Milestone",
			arrange: func(h *testsupport.Harness, _, sam domain.Account) domain.Goal {
				return readyGoal(t, h, sam, "Ship v2")
			},
			want: []judged{
				met(domain.ActivationWhyItMatters), met(domain.ActivationOwner),
				met(domain.ActivationKind), met(domain.ActivationMilestoneOrMetric),
			},
		},
		{
			name: "an Ongoing Goal with no Metric",
			arrange: func(h *testsupport.Harness, _, sam domain.Account) domain.Goal {
				g := h.CreateGoal(sam, "Keep the lights on", "Uptime keeps customers.")
				addMilestone(t, h, g.ID)
				if _, err := h.Service.MarkGoalOngoing(context.Background(), sam.ID, g.ID); err != nil {
					t.Fatalf("MarkGoalOngoing: %v", err)
				}
				return g
			},
			want: []judged{
				met(domain.ActivationWhyItMatters), met(domain.ActivationOwner), met(domain.ActivationKind),
				{domain.ActivationMetric, false, "an Ongoing Goal needs at least one Metric"},
			},
		},
		{
			name: "a Goal missing a required Dimension",
			arrange: func(h *testsupport.Harness, boss, sam domain.Account) domain.Goal {
				pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
				h.SetDimensionRequired(boss, pillar, true)
				team := h.CreateDimension(boss, "Team", "Platform")
				h.SetDimensionRequired(boss, team, true)
				g := readyGoal(t, h, sam, "Ship v2")
				h.AssignGoalValue(g, team.Values[0])
				return g
			},
			want: []judged{
				met(domain.ActivationWhyItMatters), met(domain.ActivationOwner),
				met(domain.ActivationKind), met(domain.ActivationMilestoneOrMetric),
				{domain.ActivationValue, false, "a Goal needs a value in Pillar to become Active"},
				met(domain.ActivationValue),
			},
		},
		{
			name: "a Goal missing a required Field",
			arrange: func(h *testsupport.Harness, boss, sam domain.Account) domain.Goal {
				budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
				h.SetFieldRequired(boss, budget, true)
				g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
				if _, err := h.Service.MarkGoalDated(context.Background(), sam.ID, g.ID, futureDate); err != nil {
					t.Fatalf("MarkGoalDated: %v", err)
				}
				return g
			},
			want: []judged{
				met(domain.ActivationWhyItMatters), met(domain.ActivationOwner), met(domain.ActivationKind),
				{domain.ActivationMilestoneOrMetric, false, "a Dated Goal needs at least one Milestone or Metric"},
				{domain.ActivationValue, false, "a Goal needs a value in Budget to become Active"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t, "boss@example.com")
			boss := h.SignIn("boss@example.com")
			sam := h.SignIn("sam@example.com")
			g := tt.arrange(h, boss, sam)

			page, err := h.Service.GoalPage(context.Background(), sam.ID, g.ID)
			if err != nil {
				t.Fatalf("GoalPage: %v", err)
			}
			var got []judged
			for _, r := range page.Activation {
				got = append(got, judged{r.Rule, r.Met, r.Reason})
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Activation =\n%+v\nwant\n%+v", got, tt.want)
			}

			var values []domain.ActivationRequirement
			var wantReasons []string
			for _, r := range page.Activation {
				if r.Rule == domain.ActivationValue {
					values = append(values, r)
				}
				if !r.Met && r.Reason != "" {
					wantReasons = append(wantReasons, r.Reason)
				}
			}
			if len(values) != len(page.Required) {
				t.Fatalf("value requirements = %+v, want one per required value %+v", values, page.Required)
			}
			for i, r := range page.Required {
				if values[i].Name != r.Name || values[i].Met != r.Set {
					t.Errorf("value requirement %d = %+v, want Name %q, Met %v", i, values[i], r.Name, r.Set)
				}
			}

			_, err = h.Service.ActivateGoal(context.Background(), sam.ID, g.ID)
			var gotReasons []string
			for _, r := range domain.ActivationReasons(err) {
				gotReasons = append(gotReasons, domain.Reason(r))
			}
			if !slices.Equal(gotReasons, wantReasons) {
				t.Errorf("ActivateGoal reasons = %q, want %q", gotReasons, wantReasons)
			}
		})
	}
}

// The Ready to activate checklist, JudgeDefinition, judges the definition
// command's own input as the command does: an input lacking any one
// requirement is judged lacking only that one and refused by
// CreateDefinedGoal with Activate, saving nothing, and one meeting them all is
// judged met and created Active. A value typed for a Fixed Dimension counts
// for nothing and is refused, even from an Admin, by DefineGoal too (#688).
// Defining a Proposed Goal, its saved Milestones and Metrics, its saved Kind
// when none is given, and its saved value in a Field none is given for count,
// on the checklist and in DefineGoal alike (#688).
func TestJudgeDefinitionAgreesWithTheCommand(t *testing.T) {
	t.Parallel()

	type agreement struct {
		h        *testsupport.Harness
		admin    domain.Account
		owner    domain.Account
		pillar   domain.Dimension
		customer domain.Dimension
		budget   domain.Field
	}
	arrange := func(t *testing.T) agreement {
		h := testsupport.New(t, "admin@example.com")
		admin := h.SignIn("admin@example.com")
		a := agreement{
			h:        h,
			admin:    admin,
			owner:    h.SignIn("sam@example.com"),
			pillar:   h.CreateDimension(admin, "Pillar", "Growth"),
			customer: h.CreateExtendableDimension(admin, "Customer", "Acme"),
			budget:   h.CreateField(admin, "Budget", domain.FieldNumber, "$"),
		}
		h.SetDimensionRequired(admin, a.pillar, true)
		h.SetDimensionRequired(admin, a.customer, true)
		h.SetFieldRequired(admin, a.budget, true)
		return a
	}
	// complete meets every requirement, as a's Owner.
	complete := func(a agreement) domain.DefinedGoalInput {
		return domain.DefinedGoalInput{
			Title:        "Ship v2",
			WhyItMatters: "Customers wait too long for v2.",
			OwnerID:      a.owner.ID,
			Kind:         domain.GoalDated,
			DeliveryDate: futureDate,
			Milestones:   []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate.AddDate(0, 0, -30)}},
			ValueIDs:     []int64{a.pillar.Values[0].ID},
			NewValues:    map[int64]string{a.customer.ID: "Globex"},
			FieldValues:  map[int64]string{a.budget.ID: "25000"},
			Activate:     true,
		}
	}
	judge := func(t *testing.T, a agreement, in domain.DefinedGoalInput, saved domain.SavedDefinition) []domain.ActivationRequirement {
		t.Helper()
		ctx := context.Background()
		dims, err := a.h.Service.ListDimensions(ctx)
		if err != nil {
			t.Fatalf("ListDimensions: %v", err)
		}
		fields, err := a.h.Service.ListFields(ctx)
		if err != nil {
			t.Fatalf("ListFields: %v", err)
		}
		return domain.JudgeDefinition(in, saved, dims, fields)
	}
	// unmet names each unmet requirement by its rule, and a value's by its
	// Dimension's or Field's name too.
	unmet := func(reqs []domain.ActivationRequirement) []string {
		var out []string
		for _, r := range reqs {
			if !r.Met {
				out = append(out, strings.TrimSuffix(string(r.Rule)+" "+r.Name, " "))
			}
		}
		return out
	}
	problems := func(err error) []string {
		var out []string
		for _, p := range domain.InputErrors(err) {
			out = append(out, p.Input+": "+p.Message)
		}
		return out
	}

	const (
		kindReason     = "a Goal must be marked Dated or Ongoing to become Active"
		datedMeasure   = "a Dated Goal needs at least one Milestone or Metric"
		ongoingMeasure = "an Ongoing Goal needs at least one Metric"
	)
	tests := []struct {
		name string
		// lack takes one requirement out of the complete input.
		lack func(a agreement, in *domain.DefinedGoalInput)
		// unmet is the one requirement judged unmet.
		unmet string
		// refused is the one problem CreateDefinedGoal refuses with.
		refused func(a agreement) string
	}{
		{
			name:    "blank Why it matters",
			lack:    func(_ agreement, in *domain.DefinedGoalInput) { in.WhyItMatters = " " },
			unmet:   string(domain.ActivationWhyItMatters),
			refused: func(agreement) string { return domain.InputWhyItMatters + ": a Goal needs to say why it matters" },
		},
		{
			name:    "no Kind",
			lack:    func(_ agreement, in *domain.DefinedGoalInput) { in.Kind, in.DeliveryDate = "", time.Time{} },
			unmet:   string(domain.ActivationKind),
			refused: func(agreement) string { return domain.InputActivate + ": " + kindReason },
		},
		{
			name:    "Dated with no date",
			lack:    func(_ agreement, in *domain.DefinedGoalInput) { in.DeliveryDate = time.Time{} },
			unmet:   string(domain.ActivationKind),
			refused: func(agreement) string { return domain.InputDeliveryDate + ": a Dated Goal needs a delivery date" },
		},
		{
			name:    "Dated with no Milestone or Metric",
			lack:    func(_ agreement, in *domain.DefinedGoalInput) { in.Milestones = nil },
			unmet:   string(domain.ActivationMilestoneOrMetric),
			refused: func(agreement) string { return domain.InputActivate + ": " + datedMeasure },
		},
		{
			name: "Ongoing with only a Milestone",
			lack: func(_ agreement, in *domain.DefinedGoalInput) {
				in.Kind, in.DeliveryDate = domain.GoalOngoing, time.Time{}
			},
			unmet:   string(domain.ActivationMetric),
			refused: func(agreement) string { return domain.InputActivate + ": " + ongoingMeasure },
		},
		{
			name:  "the Fixed Dimension unset",
			lack:  func(_ agreement, in *domain.DefinedGoalInput) { in.ValueIDs = nil },
			unmet: string(domain.ActivationValue) + " Pillar",
			refused: func(agreement) string {
				return domain.InputActivate + ": a Goal needs a value in Pillar to become Active"
			},
		},
		{
			name:  "the Extendable Dimension's new value blank",
			lack:  func(a agreement, in *domain.DefinedGoalInput) { in.NewValues[a.customer.ID] = " " },
			unmet: string(domain.ActivationValue) + " Customer",
			refused: func(agreement) string {
				return domain.InputActivate + ": a Goal needs a value in Customer to become Active"
			},
		},
		{
			name:  "the Field blank",
			lack:  func(a agreement, in *domain.DefinedGoalInput) { in.FieldValues[a.budget.ID] = "" },
			unmet: string(domain.ActivationValue) + " Budget",
			refused: func(agreement) string {
				return domain.InputActivate + ": a Goal needs a value in Budget to become Active"
			},
		},
		{
			name: "an Admin Owner's new value for the Fixed Dimension",
			lack: func(a agreement, in *domain.DefinedGoalInput) {
				in.OwnerID, in.ValueIDs = a.admin.ID, nil
				in.NewValues[a.pillar.ID] = "Safety"
			},
			unmet: string(domain.ActivationValue) + " Pillar",
			refused: func(a agreement) string {
				return domain.NewValueInput(a.pillar.ID) + ": Pillar is a Fixed list, so a value can't be added to it here"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := arrange(t)
			ctx := context.Background()
			in := complete(a)
			tt.lack(a, &in)

			if got := unmet(judge(t, a, in, domain.SavedDefinition{})); !slices.Equal(got, []string{tt.unmet}) {
				t.Errorf("judged unmet = %q, want only %q", got, tt.unmet)
			}
			_, err := a.h.Service.CreateDefinedGoal(ctx, in)
			if got, want := problems(err), []string{tt.refused(a)}; !slices.Equal(got, want) {
				t.Errorf("CreateDefinedGoal's problems = %q, want %q", got, want)
			}
			if goals, _ := a.h.Service.ListGoals(ctx); len(goals) != 0 {
				t.Errorf("Goals = %+v, want none saved", goals)
			}
		})
	}

	t.Run("an Admin Owner's new value for the Fixed Dimension, defining a Proposed Goal", func(t *testing.T) {
		t.Parallel()

		a := arrange(t)
		ctx := context.Background()
		g := a.h.CreateGoal(a.admin, "Ship v2", "Customers wait too long for v2.")
		in := complete(a)
		in.ValueIDs = nil
		in.NewValues[a.pillar.ID] = "Safety"

		_, err := a.h.Service.DefineGoal(ctx, a.admin.ID, g.ID, in)

		want := []string{domain.NewValueInput(a.pillar.ID) + ": Pillar is a Fixed list, so a value can't be added to it here"}
		if got := problems(err); !slices.Equal(got, want) {
			t.Errorf("DefineGoal's problems = %q, want %q", got, want)
		}
		got, err := a.h.Service.ViewGoal(ctx, g.ID)
		if err != nil {
			t.Fatalf("ViewGoal: %v", err)
		}
		if got.Lifecycle != domain.LifecycleProposed || got.Kind != "" {
			t.Errorf("Goal = %s, Kind %q, want it left Proposed with no Kind", got.Lifecycle, got.Kind)
		}
		if milestones, _ := a.h.Service.ListMilestones(ctx, g.ID); len(milestones) != 0 {
			t.Errorf("Milestones = %+v, want none saved", milestones)
		}
	})

	defines := []struct {
		name string
		// arrange saves part of the definition on the Proposed Goal g.
		arrange func(t *testing.T, a agreement, g domain.Goal)
		// leave takes out of the complete input what g has saved.
		leave func(a agreement, in *domain.DefinedGoalInput)
		// kept checks DefineGoal kept what g had saved.
		kept func(t *testing.T, a agreement, g domain.Goal)
	}{
		{
			name:    "saved counts",
			arrange: func(t *testing.T, a agreement, g domain.Goal) { addMetric(t, a.h, g.ID) },
			leave: func(_ agreement, in *domain.DefinedGoalInput) {
				in.Kind, in.DeliveryDate, in.Milestones, in.Metrics = domain.GoalOngoing, time.Time{}, nil, nil
			},
			kept: func(*testing.T, agreement, domain.Goal) {},
		},
		{
			name: "saved Kind",
			arrange: func(t *testing.T, a agreement, g domain.Goal) {
				if _, err := a.h.Service.MarkGoalDated(context.Background(), a.owner.ID, g.ID, futureDate); err != nil {
					t.Fatalf("MarkGoalDated: %v", err)
				}
				addMilestone(t, a.h, g.ID)
			},
			leave: func(_ agreement, in *domain.DefinedGoalInput) {
				in.Kind, in.DeliveryDate, in.Milestones, in.Metrics = "", time.Time{}, nil, nil
			},
			kept: func(t *testing.T, a agreement, g domain.Goal) {
				if g.Kind != domain.GoalDated || !g.DeliveryDate.Equal(futureDate) {
					t.Errorf("Kind = %q due %v, want it kept Dated due %v", g.Kind, g.DeliveryDate, futureDate)
				}
			},
		},
		{
			name: "saved Field",
			arrange: func(t *testing.T, a agreement, g domain.Goal) {
				if err := a.h.Service.SetGoalField(context.Background(), a.owner.ID, g.ID, a.budget.ID, "25000"); err != nil {
					t.Fatalf("SetGoalField: %v", err)
				}
			},
			leave: func(a agreement, in *domain.DefinedGoalInput) { delete(in.FieldValues, a.budget.ID) },
			kept: func(t *testing.T, a agreement, g domain.Goal) {
				values, err := a.h.Service.GoalFields(context.Background(), g.ID)
				if err != nil {
					t.Fatalf("GoalFields: %v", err)
				}
				if len(values) != 1 || values[0].Field.ID != a.budget.ID || values[0].Value != "25000" {
					t.Errorf("Field values = %+v, want Budget kept at 25000", values)
				}
			},
		},
	}
	for _, tt := range defines {
		t.Run("defining with "+tt.name, func(t *testing.T) {
			t.Parallel()

			a := arrange(t)
			ctx := context.Background()
			g := a.h.CreateGoal(a.owner, "Ship v2", "Customers wait too long for v2.")
			tt.arrange(t, a, g)
			in := complete(a)
			in.Title = g.Title
			tt.leave(a, &in)

			if got := unmet(judge(t, a, in, savedDefinitionOf(t, a.h, g.ID))); len(got) != 0 {
				t.Errorf("judged unmet = %q, want all met", got)
			}
			defined, err := a.h.Service.DefineGoal(ctx, a.owner.ID, g.ID, in)
			if err != nil {
				t.Fatalf("DefineGoal: %v", err)
			}
			if defined.Lifecycle != domain.LifecycleActive {
				t.Errorf("Lifecycle = %q, want %q", defined.Lifecycle, domain.LifecycleActive)
			}
			tt.kept(t, a, defined)
		})
	}

	t.Run("meeting every requirement", func(t *testing.T) {
		t.Parallel()

		a := arrange(t)
		in := complete(a)

		if got := unmet(judge(t, a, in, domain.SavedDefinition{})); len(got) != 0 {
			t.Errorf("judged unmet = %q, want all met", got)
		}
		g, err := a.h.Service.CreateDefinedGoal(context.Background(), in)
		if err != nil {
			t.Fatalf("CreateDefinedGoal: %v", err)
		}
		if g.Lifecycle != domain.LifecycleActive {
			t.Errorf("Lifecycle = %q, want %q", g.Lifecycle, domain.LifecycleActive)
		}
	})
}

// savedDefinitionOf is what the saved Goal goalID has that defining it keeps
// or adds to.
func savedDefinitionOf(t *testing.T, h *testsupport.Harness, goalID int64) domain.SavedDefinition {
	t.Helper()
	ctx := context.Background()
	g, err := h.Service.ViewGoal(ctx, goalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	milestones, err := h.Service.ListMilestones(ctx, goalID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	metrics, err := h.Service.ListMetrics(ctx, goalID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	values, err := h.Service.GoalFields(ctx, goalID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	saved := domain.SavedDefinition{
		Kind: g.Kind, DeliveryDate: g.DeliveryDate,
		Milestones: len(milestones), Metrics: len(metrics),
		FieldValues: map[int64]string{},
	}
	for _, v := range values {
		saved.FieldValues[v.Field.ID] = v.Value
	}
	return saved
}
