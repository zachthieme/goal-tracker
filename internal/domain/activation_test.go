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

// JudgeDefinition judges a definition as typed against the activation
// standard: each requirement in checklist order, whether it is met, the input
// that meets it, and the reason ActivateGoal would give while it is unmet.
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

	tests := []struct {
		name   string
		draft  domain.DefinitionDraft
		dims   []domain.Dimension
		fields []domain.Field
		want   []domain.ActivationRequirement
	}{
		{
			name:  "nothing typed",
			draft: domain.DefinitionDraft{WhyItMatters: "  "},
			want: []domain.ActivationRequirement{
				{Rule: domain.ActivationWhyItMatters, Input: domain.InputWhyItMatters, Reason: whyReason},
				owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				{Rule: domain.ActivationMilestoneOrMetric, Input: domain.InputHowYoullKnow},
			},
		},
		{
			name:  "no Kind with a Milestone",
			draft: domain.DefinitionDraft{WhyItMatters: why, Milestones: 1},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				measureMet,
			},
		},
		{
			name:  "no Kind with a Metric",
			draft: domain.DefinitionDraft{WhyItMatters: why, Metrics: 1},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputKind, Reason: kindReason},
				measureMet,
			},
		},
		{
			name:  "Dated without a date or a Milestone or Metric",
			draft: domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalDated},
			want: []domain.ActivationRequirement{
				whyMet, owner,
				{Rule: domain.ActivationKind, Input: domain.InputDeliveryDate, Reason: dateReason},
				{Rule: domain.ActivationMilestoneOrMetric, Input: domain.InputHowYoullKnow, Reason: datedMeasure},
			},
		},
		{
			name:  "Dated with a date and a Milestone",
			draft: domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalDated, DeliveryDate: futureDate, Milestones: 1},
			want:  []domain.ActivationRequirement{whyMet, owner, datedMet, measureMet},
		},
		{
			name:  "Dated with a date and a Metric",
			draft: domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalDated, DeliveryDate: futureDate, Metrics: 1},
			want:  []domain.ActivationRequirement{whyMet, owner, datedMet, measureMet},
		},
		{
			name:  "Ongoing with only a Milestone",
			draft: domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalOngoing, Milestones: 1},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing,
				{Rule: domain.ActivationMetric, Input: domain.InputHowYoullKnow, Reason: ongoingMeasure},
			},
		},
		{
			name:  "Ongoing with a Metric",
			draft: domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: 1},
			want:  []domain.ActivationRequirement{whyMet, owner, ongoing, metricMet},
		},
		{
			name:   "required Dimension and Field not set, Retired ones left out",
			draft:  domain.DefinitionDraft{WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: 1, FieldValues: map[int64]string{budgetID: "  "}},
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
			draft: domain.DefinitionDraft{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: 1,
				Chosen:      map[int64]bool{growthID: true},
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
			draft: domain.DefinitionDraft{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: 1,
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
			draft: domain.DefinitionDraft{
				WhyItMatters: why, Kind: domain.GoalOngoing, Metrics: 1,
				NewValues: map[int64]string{extendableDimID: " "},
			},
			dims: []domain.Dimension{team},
			want: []domain.ActivationRequirement{
				whyMet, owner, ongoing, metricMet,
				{Rule: domain.ActivationValue, Name: "Team", Input: domain.DimensionInput(extendableDimID), Reason: "a Goal needs a value in Team to become Active"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domain.JudgeDefinition(tt.draft, tt.dims, tt.fields)
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
