package domain_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
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

// A refused activation comes after every write, so it must roll back all of
// them: the Goal, its Milestones, Metrics, values and Field values, a value it
// added to an Extendable list, and its links, Accepted or Pending.
func TestCreateDefinedGoalRefusedActivationLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	kim := s.h.SignIn("kim@example.com")
	own := s.h.CreateGoal(s.owner, "Win enterprise", "Enterprise deals stall.")
	theirs := s.h.CreateGoal(kim, "Grow revenue", "Revenue is flat.")
	s.h.SetFieldRequired(s.admin, s.h.CreateField(s.admin, "Budget", domain.FieldNumber, "$"), true)
	in := s.fullDefinition()
	in.ParentIDs = []int64{own.ID, theirs.ID}
	in.Activate = true

	if _, err := s.h.Service.CreateDefinedGoal(ctx, in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want the activation refused", err)
	}

	goals, err := s.h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 2 {
		t.Errorf("Goals = %+v, want only the two parents", goals)
	}
	dims, err := s.h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.ID == s.customer.ID && len(d.Values) != 1 {
			t.Errorf("Customer values = %+v, want only Acme", d.Values)
		}
	}
	pending, err := s.h.Service.PendingLinkRequests(ctx, kim.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Kim's pending requests = %+v, want none", pending)
	}
	children, err := s.h.Service.ChildrenOf(ctx, own.ID)
	if err != nil {
		t.Fatalf("ChildrenOf: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children of %s = %+v, want none", own.Title, children)
	}
	// With the Goal gone there is no Goal to list these by.
	for _, table := range []string{"milestones", "metrics", "goal_dimension_values", "goal_field_values", "links"} {
		var n int
		if err := s.h.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows = %d, want 0", table, n)
		}
	}
}

// One parent the Owner also owns links at once; the other waits Pending in its
// Owner's pending requests, as RequestLink leaves it.
func TestCreateDefinedGoalRequestsALinkToEachParent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	own := h.CreateGoal(sam, "Win enterprise", "Enterprise deals stall.")
	theirs := h.CreateGoal(kim, "Grow revenue", "Revenue is flat.")

	g, err := h.Service.CreateDefinedGoal(ctx, domain.DefinedGoalInput{
		Title:     "Ship v2",
		SoWhat:    "Customers wait too long for v2.",
		OwnerID:   sam.ID,
		ParentIDs: []int64{own.ID, theirs.ID},
	})
	if err != nil {
		t.Fatalf("CreateDefinedGoal: %v", err)
	}

	parents, err := h.Service.ParentsOf(ctx, g.ID)
	if err != nil {
		t.Fatalf("ParentsOf: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != own.ID {
		t.Errorf("Accepted parents = %+v, want only %s", parents, own.Title)
	}
	pending, err := h.Service.PendingLinkRequests(ctx, kim.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	if len(pending) != 1 || pending[0].Child.ID != g.ID || pending[0].Parent.ID != theirs.ID || pending[0].Status != domain.LinkPending {
		t.Errorf("Kim's pending requests = %+v, want Ship v2 → Grow revenue", pending)
	}
	if sent := h.Email.Sent(); len(sent) != 0 {
		t.Errorf("emails sent = %+v, want none", sent)
	}
}

// Every input is checked before anything is written, and every problem comes
// back together, each under the name of the input it is in, so the form can
// show them all at once.
func TestCreateDefinedGoalNamesEveryBadInputAtOnce(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	retiredValue := s.h.CreateDimension(s.admin, "Region", "EMEA", "APAC").Values[0]
	if err := s.h.Service.RetireDimensionValue(ctx, s.admin.ID, retiredValue.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	retiredDim := s.h.CreateDimension(s.admin, "Legacy", "Old")
	if err := s.h.Service.RetireDimension(ctx, s.admin.ID, retiredDim.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	parent := s.h.CreateGoal(s.owner, "Win enterprise", "Enterprise deals stall.")
	const missing = 9999

	_, err := s.h.Service.CreateDefinedGoal(ctx, domain.DefinedGoalInput{
		Title:        " ",
		SoWhat:       "",
		OwnerID:      s.owner.ID,
		Kind:         domain.GoalDated,
		DeliveryDate: time.Time{},
		CadenceDays:  -1,
		Milestones:   []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate}, {Name: " "}},
		Metrics: []domain.MetricDefinition{
			{Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown, TargetDate: futureDate},
			{Direction: "sideways"},
		},
		ValueIDs: []int64{
			s.team.Values[0].ID, s.team.Values[1].ID, // two in a one-value Dimension
			retiredValue.ID, retiredDim.Values[0].ID, missing,
		},
		NewValues:   map[int64]string{s.team.ID: "Ops", missing: "Initech"},
		FieldValues: map[int64]string{s.headcount.ID: "three", missing: "1"},
		ParentIDs:   []int64{parent.ID, missing, parent.ID},
		Activate:    true,
	})

	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	var got []string
	for _, p := range domain.InputErrors(err) {
		if p.Message == "" {
			t.Errorf("%s has no message", p.Input)
		}
		got = append(got, p.Input)
	}
	want := []string{
		"title", "so_what", "delivery_date", "cadence",
		"milestones[1].name", "milestones[1].date",
		"metrics[1].name", "metrics[1].unit", "metrics[1].direction", "metrics[1].target_date",
		id("value:", s.team.Values[0].ID), id("value:", s.team.Values[1].ID),
		id("value:", retiredValue.ID), id("value:", retiredDim.Values[0].ID), id("value:", missing),
		id("new_value:", s.team.ID), id("new_value:", missing),
		id("field:", s.headcount.ID), id("field:", missing),
		id("parent:", parent.ID), id("parent:", missing),
	}
	if !slices.Equal(sorted(got), sorted(want)) {
		t.Errorf("inputs refused = %v\nwant %v", sorted(got), sorted(want))
	}
	goals, err := s.h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 1 {
		t.Errorf("Goals = %+v, want only the parent", goals)
	}
}

// id is an input name for the thing with the given ID, e.g. "value:12".
func id(prefix string, n int64) string {
	return prefix + strconv.FormatInt(n, 10)
}

// Each check not met by the test above, one input at a time; a blank new value
// or Field value is no value rather than a problem.
func TestCreateDefinedGoalChecksEachInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		define func(t *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput)
		// input is the input refused, or "" when the definition is accepted.
		input func(s definedGoalSetup) string
	}{
		{
			name:   "a Kind that is neither Dated nor Ongoing",
			define: func(_ *testing.T, _ definedGoalSetup, in *domain.DefinedGoalInput) { in.Kind = "Weekly" },
			input:  func(definedGoalSetup) string { return "kind" },
		},
		{
			name: "a new value in a Retired Dimension",
			define: func(t *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				if err := s.h.Service.RetireDimension(context.Background(), s.admin.ID, s.customer.ID); err != nil {
					t.Fatalf("RetireDimension: %v", err)
				}
			},
			input: func(s definedGoalSetup) string { return id("new_value:", s.customer.ID) },
		},
		{
			name: "a new value matching a Retired value",
			define: func(t *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				if err := s.h.Service.RetireDimensionValue(context.Background(), s.admin.ID, s.customer.Values[0].ID); err != nil {
					t.Fatalf("RetireDimensionValue: %v", err)
				}
				in.NewValues[s.customer.ID] = "acme "
			},
			input: func(s definedGoalSetup) string { return id("new_value:", s.customer.ID) },
		},
		{
			name: "a new value with a semicolon",
			define: func(_ *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				in.NewValues[s.customer.ID] = "Globex; Initech"
			},
			input: func(s definedGoalSetup) string { return id("new_value:", s.customer.ID) },
		},
		{
			name: "a value chosen and a new one typed in a one-value Dimension",
			define: func(_ *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				in.ValueIDs = append(in.ValueIDs, s.customer.Values[0].ID)
			},
			input: func(s definedGoalSetup) string { return id("new_value:", s.customer.ID) },
		},
		{
			name: "a value in a Retired Field",
			define: func(t *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				if err := s.h.Service.RetireField(context.Background(), s.admin.ID, s.headcount.ID); err != nil {
					t.Fatalf("RetireField: %v", err)
				}
			},
			input: func(s definedGoalSetup) string { return id("field:", s.headcount.ID) },
		},
		{
			name: "a blank new value and a blank Field value",
			define: func(_ *testing.T, s definedGoalSetup, in *domain.DefinedGoalInput) {
				in.NewValues = map[int64]string{s.customer.ID: " ", 9999: ""}
				in.FieldValues = map[int64]string{s.headcount.ID: "", 9999: " "}
			},
			input: func(definedGoalSetup) string { return "" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newDefinedGoalSetup(t)
			in := s.fullDefinition()
			tt.define(t, s, &in)

			_, err := s.h.Service.CreateDefinedGoal(context.Background(), in)

			want := tt.input(s)
			if want == "" {
				if err != nil {
					t.Fatalf("CreateDefinedGoal: %v", err)
				}
				return
			}
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			var got []string
			for _, p := range domain.InputErrors(err) {
				got = append(got, p.Input)
			}
			if !slices.Contains(got, want) {
				t.Errorf("inputs refused = %v, want %s among them", got, want)
			}
		})
	}
}

// An Owner who is not an Admin adds a value to an Extendable Dimension's list
// by typing it in, and the Goal carries it (CONTEXT.md: Extendable).
func TestCreateDefinedGoalAddsANewValueToAnExtendableList(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()

	g, err := s.h.Service.CreateDefinedGoal(ctx, domain.DefinedGoalInput{
		Title:     "Ship v2",
		SoWhat:    "Customers wait too long for v2.",
		OwnerID:   s.owner.ID,
		NewValues: map[int64]string{s.customer.ID: "Globex"},
	})
	if err != nil {
		t.Fatalf("CreateDefinedGoal: %v", err)
	}

	dims, err := s.h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.ID == s.customer.ID && !slices.Equal(valueNames(d.Values), []string{"Acme", "Globex"}) {
			t.Errorf("Customer's list = %v, want Acme and Globex", valueNames(d.Values))
		}
	}
	values, err := s.h.Service.GoalValues(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if !slices.Equal(valueNames(values), []string{"Globex"}) {
		t.Errorf("values = %v, want Globex", valueNames(values))
	}
}

// DefineGoal (#135) finishes defining a Proposed Goal on the New goal form,
// with CreateDefinedGoal's input.

// definition is the input that leaves g as it is: its Title and So What, and
// nothing else changed.
func definition(g domain.Goal) domain.DefinedGoalInput {
	return domain.DefinedGoalInput{Title: g.Title, SoWhat: g.SoWhat, OwnerID: g.Owner.ID, Kind: g.Kind, DeliveryDate: g.DeliveryDate}
}

func TestDefineGoalChangesKindAddsAMilestoneAndSwapsAValueInOneCall(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	area := s.h.CreateSeveralValuesDimension(s.admin, "Area", "Billing", "Search", "Mobile")
	g := s.h.CreateGoal(s.owner, "Ship v2", "Customers wait too long for v2.")
	if _, err := s.h.Service.MarkGoalOngoing(ctx, g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	s.h.AssignGoalValue(g, area.Values[0])
	s.h.AssignGoalValue(g, area.Values[1])
	s.h.SetGoalField(s.owner, g, s.headcount, "3")
	in := definition(g)
	in.SoWhat = "Customers churn waiting for v2."
	in.Kind, in.DeliveryDate = domain.GoalDated, futureDate
	in.CadenceDays = 14
	in.Milestones = []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate.AddDate(0, 0, -30)}}
	in.ValueIDs = []int64{area.Values[1].ID, area.Values[2].ID} // Billing unticked, Mobile ticked
	in.FieldValues = map[int64]string{s.headcount.ID: ""}

	got, err := s.h.Service.DefineGoal(ctx, s.owner.ID, g.ID, in)
	if err != nil {
		t.Fatalf("DefineGoal: %v", err)
	}

	if got.ID != g.ID || got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Goal = %d %q, want %d still Proposed", got.ID, got.Lifecycle, g.ID)
	}
	if got.Kind != domain.GoalDated || !got.DeliveryDate.Equal(futureDate) {
		t.Errorf("Kind, delivery date = %q, %v, want Dated, %v", got.Kind, got.DeliveryDate, futureDate)
	}
	if got.CadenceDays != 14 {
		t.Errorf("CadenceDays = %d, want 14", got.CadenceDays)
	}
	if got.SoWhat != "Customers churn waiting for v2." {
		t.Errorf("So What = %q, want the new one", got.SoWhat)
	}
	revisions, err := s.h.Service.ListSoWhatRevisions(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListSoWhatRevisions: %v", err)
	}
	if len(revisions) == 0 || revisions[0].SoWhat != "Customers churn waiting for v2." || revisions[0].Author.ID != s.owner.ID {
		t.Errorf("latest revision = %+v, want the new So What by the Owner", revisions)
	}
	milestones, err := s.h.Service.ListMilestones(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(milestones) != 1 || milestones[0].Name != "Beta cut" {
		t.Errorf("Milestones = %+v, want Beta cut", milestones)
	}
	values, err := s.h.Service.GoalValues(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if names := sorted(valueNames(values)); !slices.Equal(names, []string{"Mobile", "Search"}) {
		t.Errorf("values = %v, want Search and Mobile", names)
	}
	fields, err := s.h.Service.GoalFields(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	if len(fields) != 0 {
		t.Errorf("Fields = %+v, want Headcount cleared", fields)
	}
}

// Only the Owner defines their Goal, and only while it is Proposed; either
// refusal changes nothing.
func TestDefineGoalRefusesAnyoneButTheOwnerAndAGoalNotProposed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ctx := context.Background()
	admin := h.SignIn("admin@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	proposed := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	h.AddDelegate(sam, kim, proposed.ID)
	active := h.ActiveGoal(sam, "Grow revenue", "Revenue is flat.")

	for _, actor := range []domain.Account{kim, admin} {
		in := definition(proposed)
		in.Kind = domain.GoalOngoing
		if _, err := h.Service.DefineGoal(ctx, actor.ID, proposed.ID, in); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("DefineGoal by %s: err = %v, want ErrNotAuthorized", actor.Email, err)
		}
	}
	in := definition(active)
	in.CadenceDays = 30
	if _, err := h.Service.DefineGoal(ctx, sam.ID, active.ID, in); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("DefineGoal on an Active Goal: err = %v, want ErrValidation", err)
	}

	if got, _ := h.Service.ViewGoal(ctx, proposed.ID); got.Kind != "" {
		t.Errorf("Kind = %q, want still none chosen", got.Kind)
	}
	if got, _ := h.Service.ViewGoal(ctx, active.ID); got.CadenceDays == 30 {
		t.Errorf("CadenceDays = %d, want unchanged", got.CadenceDays)
	}
}

// A value Retired after the form loaded is refused, under its input, and
// nothing else the submit changed is kept.
func TestDefineGoalWithAValueRetiredSinceLeavesTheGoalUnchanged(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	area := s.h.CreateSeveralValuesDimension(s.admin, "Area", "Billing", "Search")
	g := s.h.CreateGoal(s.owner, "Ship v2", "Customers wait too long for v2.")
	s.h.AssignGoalValue(g, area.Values[0])
	if err := s.h.Service.RetireDimensionValue(ctx, s.admin.ID, area.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	in := definition(g)
	in.Kind, in.DeliveryDate = domain.GoalDated, futureDate
	in.Milestones = []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate}}
	in.ValueIDs = []int64{area.Values[1].ID} // Billing unticked, Search ticked

	_, err := s.h.Service.DefineGoal(ctx, s.owner.ID, g.ID, in)

	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	var inputs []string
	for _, p := range domain.InputErrors(err) {
		inputs = append(inputs, p.Input)
	}
	if !slices.Equal(inputs, []string{id("value:", area.Values[1].ID)}) {
		t.Errorf("inputs refused = %v, want Search's", inputs)
	}
	got, err := s.h.Service.ViewGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Kind != "" {
		t.Errorf("Kind = %q, want still none chosen", got.Kind)
	}
	if milestones, _ := s.h.Service.ListMilestones(ctx, g.ID); len(milestones) != 0 {
		t.Errorf("Milestones = %+v, want none", milestones)
	}
	values, err := s.h.Service.GoalValues(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if names := valueNames(values); !slices.Equal(names, []string{"Billing"}) {
		t.Errorf("values = %v, want Billing still", names)
	}
}

// A Retired value the Goal already carries may be kept, as SetGoalValues
// keeps it, and a Field value it already has needs no checking.
func TestDefineGoalKeepsARetiredValueAndFieldTheGoalAlreadyHas(t *testing.T) {
	t.Parallel()

	s := newDefinedGoalSetup(t)
	ctx := context.Background()
	g := s.h.CreateGoal(s.owner, "Ship v2", "Customers wait too long for v2.")
	s.h.AssignGoalValue(g, s.team.Values[0])
	s.h.SetGoalField(s.owner, g, s.headcount, "3")
	if err := s.h.Service.RetireDimensionValue(ctx, s.admin.ID, s.team.Values[0].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	if err := s.h.Service.RetireField(ctx, s.admin.ID, s.headcount.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	in := definition(g)
	in.ValueIDs = []int64{s.team.Values[0].ID}
	in.FieldValues = map[int64]string{s.headcount.ID: "3"}

	if _, err := s.h.Service.DefineGoal(ctx, s.owner.ID, g.ID, in); err != nil {
		t.Fatalf("DefineGoal: %v", err)
	}

	values, err := s.h.Service.GoalValues(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if names := valueNames(values); !slices.Equal(names, []string{"Platform"}) {
		t.Errorf("values = %v, want Platform kept", names)
	}
}

// A parent already linked, Accepted or Pending, is kept and not requested
// again; a new one is requested; one that would close a cycle is refused.
func TestDefineGoalRequestsOnlyNewParentsAndRefusesACycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	accepted := h.CreateGoal(sam, "Win enterprise", "Enterprise deals stall.")
	pending := h.CreateGoal(kim, "Grow revenue", "Revenue is flat.")
	fresh := h.CreateGoal(kim, "Cut churn", "Customers leave.")
	child := h.CreateGoal(sam, "Fix search", "Search is slow.")
	h.RequestLink(sam, g, accepted, "")
	h.RequestLink(sam, g, pending, "")
	h.RequestLink(sam, child, g, "")

	in := definition(g)
	in.ParentIDs = []int64{child.ID}
	_, err := h.Service.DefineGoal(ctx, sam.ID, g.ID, in)
	var inputs []string
	for _, p := range domain.InputErrors(err) {
		inputs = append(inputs, p.Input)
	}
	if !slices.Equal(inputs, []string{id("parent:", child.ID)}) {
		t.Errorf("err = %v, want the cycle refused under %s", err, id("parent:", child.ID))
	}

	in.ParentIDs = []int64{accepted.ID, pending.ID, fresh.ID}
	if _, err := h.Service.DefineGoal(ctx, sam.ID, g.ID, in); err != nil {
		t.Fatalf("DefineGoal: %v", err)
	}
	requests, err := h.Service.PendingLinkRequests(ctx, kim.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	var parents []string
	for _, l := range requests {
		parents = append(parents, l.Parent.Title)
	}
	if !slices.Equal(sorted(parents), []string{"Cut churn", "Grow revenue"}) {
		t.Errorf("Kim's pending requests = %v, want Grow revenue once and Cut churn", parents)
	}
	if got := h.ParentsOf(g); len(got) != 1 || got[0].ID != accepted.ID {
		t.Errorf("Accepted parents = %+v, want %s", got, accepted.Title)
	}
}

// With Activate, a definition that completes the checklist activates the Goal;
// one that doesn't is refused under activate and keeps nothing.
func TestDefineGoalWithActivate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	in := definition(g)
	in.Kind, in.DeliveryDate = domain.GoalDated, futureDate
	in.Activate = true

	_, err := h.Service.DefineGoal(ctx, sam.ID, g.ID, in)
	if problems := domain.InputErrors(err); len(problems) != 1 || problems[0].Input != domain.InputActivate {
		t.Fatalf("err = %v, want the activation refused under activate", err)
	}
	if got, _ := h.Service.ViewGoal(ctx, g.ID); got.Kind != "" {
		t.Errorf("Kind = %q, want the refused submit to keep nothing", got.Kind)
	}

	in.Milestones = []domain.MilestoneDefinition{{Name: "Beta cut", Date: futureDate}}
	got, err := h.Service.DefineGoal(ctx, sam.ID, g.ID, in)
	if err != nil {
		t.Fatalf("DefineGoal: %v", err)
	}
	if got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", got.Lifecycle, domain.LifecycleActive)
	}
}

// The Title can't be changed while finishing a Goal's definition.
func TestDefineGoalRefusesAChangedTitle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	in := definition(g)
	in.Title = "Ship v3"

	_, err := h.Service.DefineGoal(context.Background(), sam.ID, g.ID, in)

	if problems := domain.InputErrors(err); len(problems) != 1 || problems[0].Input != domain.InputTitle {
		t.Errorf("err = %v, want the Title refused", err)
	}
}

// A Goal's parent candidates are the Goals it could be asked to contribute
// to: neither itself, nor a Goal that contributes to it however indirectly,
// which would close a cycle (ADR-0001), nor a Goal it is already linked to,
// Accepted or Pending.
func TestParentCandidatesLeaveOutCyclesAndExistingParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	g := h.CreateGoal(sam, "Ship v2", "Customers wait too long for v2.")
	child := h.CreateGoal(sam, "Fix search", "Search is slow.")
	grandchild := h.CreateGoal(sam, "Index faster", "Indexing lags.")
	accepted := h.CreateGoal(sam, "Win enterprise", "Enterprise deals stall.")
	pending := h.CreateGoal(kim, "Grow revenue", "Revenue is flat.")
	free := h.CreateGoal(kim, "Cut churn", "Customers leave.")
	h.RequestLink(sam, child, g, "")
	h.RequestLink(sam, grandchild, child, "")
	h.RequestLink(sam, g, accepted, "")
	h.RequestLink(sam, g, pending, "")

	candidates, err := h.Service.ParentCandidates(ctx, g.ID)
	if err != nil {
		t.Fatalf("ParentCandidates: %v", err)
	}

	var titles []string
	for _, c := range candidates {
		titles = append(titles, c.Title)
	}
	if !slices.Equal(sorted(titles), []string{free.Title}) {
		t.Errorf("candidates = %v, want only %s", titles, free.Title)
	}
}
