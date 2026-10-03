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

// CreateDefinedGoal (#128) creates a fully defined Goal in one transaction,
// for the single-page New goal form.

func TestCreateDefinedGoalWithOnlyTitleAndSoWhatIsAProposedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")

	g, err := h.Service.CreateDefinedGoal(context.Background(), domain.DefinedGoalInput{
		Title:   "Ship v2",
		SoWhat:  "Customers wait too long for v2.",
		OwnerID: owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateDefinedGoal: %v", err)
	}

	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Title != "Ship v2" || got.SoWhat != "Customers wait too long for v2." {
		t.Errorf("Goal = %q / %q, want the Title and So What given", got.Title, got.SoWhat)
	}
	if got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleProposed)
	}
	if got.Owner.ID != owner.ID {
		t.Errorf("Owner = %d, want %d", got.Owner.ID, owner.ID)
	}
	if got.Kind != "" {
		t.Errorf("Kind = %q, want none chosen", got.Kind)
	}
	if got.CadenceDays != 7 {
		t.Errorf("CadenceDays = %d, want the default 7", got.CadenceDays)
	}
}

// definedGoalSetup is an org with an Admin, a non-Admin Owner, a one-value
// Dimension, an Extendable Dimension and a number Field, for defining Goals in.
type definedGoalSetup struct {
	h         *testsupport.Harness
	admin     domain.Account
	owner     domain.Account
	team      domain.Dimension
	customer  domain.Dimension
	headcount domain.Field
}

func newDefinedGoalSetup(t *testing.T) definedGoalSetup {
	t.Helper()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	return definedGoalSetup{
		h:         h,
		admin:     admin,
		owner:     h.SignIn("sam@example.com"),
		team:      h.CreateDimension(admin, "Team", "Platform", "Growth"),
		customer:  h.CreateExtendableDimension(admin, "Customer", "Acme"),
		headcount: h.CreateField(admin, "Headcount", domain.FieldNumber, "FTE"),
	}
}

// fullDefinition defines every part of a Dated Goal for s's Owner.
func (s definedGoalSetup) fullDefinition() domain.DefinedGoalInput {
	return domain.DefinedGoalInput{
		Title:        "Ship v2",
		SoWhat:       "Customers wait too long for v2.",
		OwnerID:      s.owner.ID,
		Kind:         domain.GoalDated,
		DeliveryDate: futureDate,
		CadenceDays:  14,
		Milestones:   []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate.AddDate(0, 0, -30)}},
		Metrics: []domain.MetricDefinition{{
			Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
			Baseline: 1200, Target: 400, TargetDate: futureDate,
		}},
		ValueIDs:    []int64{s.team.Values[0].ID},
		NewValues:   map[int64]string{s.customer.ID: "Globex"},
		FieldValues: map[int64]string{s.headcount.ID: "3"},
	}
}

func TestCreateDefinedGoalSavesTheWholeDefinitionWithoutActivating(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	parent := s.h.CreateGoal(s.owner, "Win enterprise", "Enterprise deals stall.")
	in := s.fullDefinition()
	in.ParentIDs = []int64{parent.ID}

	g, err := s.h.Service.CreateDefinedGoal(ctx, in)
	if err != nil {
		t.Fatalf("CreateDefinedGoal: %v", err)
	}

	got, err := s.h.Service.ViewGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleProposed)
	}
	if got.Kind != domain.GoalDated || !got.DeliveryDate.Equal(futureDate) {
		t.Errorf("Kind, delivery date = %q, %v, want Dated, %v", got.Kind, got.DeliveryDate, futureDate)
	}
	if got.CadenceDays != 14 {
		t.Errorf("CadenceDays = %d, want 14", got.CadenceDays)
	}
	milestones, err := s.h.Service.ListMilestones(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(milestones) != 1 || milestones[0].Name != "Beta cut" || !milestones[0].TargetDate.Equal(futureDate.AddDate(0, 0, -30)) {
		t.Errorf("Milestones = %+v, want Beta cut on %v", milestones, futureDate.AddDate(0, 0, -30))
	}
	metrics, err := s.h.Service.ListMetrics(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "p95 latency" || metrics[0].Unit != "ms" ||
		metrics[0].Direction != domain.MetricDown || metrics[0].Baseline != 1200 || metrics[0].Target != 400 ||
		!metrics[0].TargetDate.Equal(futureDate) {
		t.Errorf("Metrics = %+v, want p95 latency in ms, down from 1200 to 400 by %v", metrics, futureDate)
	}
	values, err := s.h.Service.GoalValues(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if names := valueNames(values); !slices.Equal(sorted(names), []string{"Globex", "Platform"}) {
		t.Errorf("values = %v, want Platform and Globex", names)
	}
	fields, err := s.h.Service.GoalFields(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	if len(fields) != 1 || fields[0].Field.ID != s.headcount.ID || fields[0].Value != "3" {
		t.Errorf("Fields = %+v, want Headcount 3", fields)
	}
	parents, err := s.h.Service.ParentsOf(ctx, g.ID)
	if err != nil {
		t.Fatalf("ParentsOf: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Errorf("parents = %+v, want %s", parents, parent.Title)
	}
}

// sorted returns names in order, so a test can compare them whatever order
// they came in.
func sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return out
}

func TestCreateDefinedGoalWithActivateMakesItActive(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	s.h.SetDimensionRequired(s.admin, s.team, true)
	s.h.SetFieldRequired(s.admin, s.headcount, true)
	in := s.fullDefinition()
	in.Activate = true

	g, err := s.h.Service.CreateDefinedGoal(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateDefinedGoal: %v", err)
	}

	got, err := s.h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
	if !got.ActivatedAt.Equal(testsupport.Epoch) {
		t.Errorf("ActivatedAt = %v, want %v", got.ActivatedAt, testsupport.Epoch)
	}
}

// Activation refused after the writes rolls every one of them back, and each
// unmet rule comes back as its own error under the activate input.
func TestCreateDefinedGoalRefusedActivationSavesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// define turns the full definition into one the gate refuses.
		define func(s definedGoalSetup, in *domain.DefinedGoalInput)
		want   []string
	}{
		{
			name: "a Dated Goal with no Milestone or Metric",
			define: func(_ definedGoalSetup, in *domain.DefinedGoalInput) {
				in.Milestones, in.Metrics = nil, nil
			},
			want: []string{"at least one Milestone or Metric"},
		},
		{
			name: "an Ongoing Goal with no Metric",
			define: func(_ definedGoalSetup, in *domain.DefinedGoalInput) {
				in.Kind, in.DeliveryDate, in.Metrics = domain.GoalOngoing, time.Time{}, nil
			},
			want: []string{"an Ongoing Goal needs at least one Metric"},
		},
		{
			name: "Kind not chosen",
			define: func(_ definedGoalSetup, in *domain.DefinedGoalInput) {
				in.Kind, in.DeliveryDate = "", time.Time{}
			},
			want: []string{"marked Dated or Ongoing"},
		},
		{
			name: "a required Dimension and a required Field with no value",
			define: func(s definedGoalSetup, in *domain.DefinedGoalInput) {
				s.h.SetDimensionRequired(s.admin, s.team, true)
				s.h.SetFieldRequired(s.admin, s.headcount, true)
				in.ValueIDs, in.FieldValues = nil, nil
			},
			want: []string{"a value in Team", "a value in Headcount"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newDefinedGoalSetup(t)
			ctx := context.Background()
			parent := s.h.CreateGoal(s.owner, "Win enterprise", "Enterprise deals stall.")
			in := s.fullDefinition()
			in.ParentIDs = []int64{parent.ID}
			in.Activate = true
			tt.define(s, &in)

			_, err := s.h.Service.CreateDefinedGoal(ctx, in)

			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			problems := domain.InputErrors(err)
			if len(problems) != len(tt.want) {
				t.Fatalf("problems = %v, want %d under activate", problems, len(tt.want))
			}
			for i, p := range problems {
				if p.Input != "activate" || !strings.Contains(p.Message, tt.want[i]) {
					t.Errorf("problem %d = %s: %q, want activate: …%s…", i, p.Input, p.Message, tt.want[i])
				}
			}
			goals, err := s.h.Service.ListGoals(ctx)
			if err != nil {
				t.Fatalf("ListGoals: %v", err)
			}
			if len(goals) != 1 || goals[0].ID != parent.ID {
				t.Errorf("Goals = %+v, want only the parent", goals)
			}
		})
	}
}
