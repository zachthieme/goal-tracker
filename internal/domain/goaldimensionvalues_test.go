package domain_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalDimensionValuesFixture is a Goal Sam owns and the Dimensions a test of
// setting its values edits, with Boss an Admin and Pat no one on the Goal.
type goalDimensionValuesFixture struct {
	h              *testsupport.Harness
	boss, sam, pat domain.Account
	goal           domain.Goal
	// Pillar is a Fixed one-value Dimension: Growth, Trust, and Old, Retired,
	// which the Goal carries.
	pillar domain.Dimension
	// Tags is a Fixed several-values Dimension: infra and ux, and legacy and
	// gone, Retired, of which the Goal carries infra and legacy.
	tags domain.Dimension
	// Sponsor is an Extendable one-value Dimension: Ana.
	sponsor domain.Dimension
	// Region is a Retired several-values Dimension: EMEA.
	region domain.Dimension
}

func newGoalDimensionValuesFixture(t *testing.T) goalDimensionValuesFixture {
	t.Helper()
	ctx := context.Background()
	h := testsupport.New(t, "boss@example.com")
	f := goalDimensionValuesFixture{
		h:    h,
		boss: h.SignIn("boss@example.com"),
		sam:  h.SignIn("sam@example.com"),
		pat:  h.SignIn("pat@example.com"),
	}
	f.goal = h.CreateGoal(f.sam, "Alpha", "A matters.")
	f.pillar = h.CreateDimension(f.boss, "Pillar", "Growth", "Trust", "Old")
	f.tags = h.CreateSeveralValuesDimension(f.boss, "Tags", "infra", "ux", "legacy", "gone")
	f.sponsor = h.CreateExtendableDimension(f.boss, "Sponsor", "Ana")
	f.region = h.CreateSeveralValuesDimension(f.boss, "Region", "EMEA")
	h.AssignGoalValue(f.goal, f.pillar.Values[2])
	h.AssignGoalValue(f.goal, f.tags.Values[0])
	h.AssignGoalValue(f.goal, f.tags.Values[2])
	for _, v := range []domain.DimensionValue{f.pillar.Values[2], f.tags.Values[2], f.tags.Values[3]} {
		if err := h.Service.RetireDimensionValue(ctx, f.boss.ID, v.ID); err != nil {
			t.Fatalf("RetireDimensionValue: %v", err)
		}
	}
	if err := h.Service.RetireDimension(ctx, f.boss.ID, f.region.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	return f
}

// valuesIn names the values the fixture's Goal carries in dim, in list order.
func (f goalDimensionValuesFixture) valuesIn(t *testing.T, dim domain.Dimension) []string {
	t.Helper()
	values, err := f.h.Service.GoalValues(context.Background(), f.goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	var out []string
	for _, v := range values {
		if v.DimensionID == dim.ID {
			out = append(out, v.Value)
		}
	}
	return out
}

// SetGoalDimensionValues sets a Goal's values in one Dimension from the values
// chosen and a name typed, joining those it carries or becoming exactly what it
// carries, by every rule for giving a Goal values (CONTEXT.md: Dimension,
// Extendable, Fixed, Retired, Delegate).
func TestSetGoalDimensionValues(t *testing.T) {
	t.Parallel()

	type edit func(f goalDimensionValuesFixture) (actor domain.Account, e domain.DimensionValuesEdit)
	for _, tc := range []struct {
		name string
		edit edit
		// dim is the Dimension whose values the Goal carries afterwards are
		// want.
		dim  func(f goalDimensionValuesFixture) domain.Dimension
		want []string
		// typed is the value the typed name set, "" for none.
		typed string
		// refused is the refusal, "" for none, and kind the error it is.
		refused string
		kind    error
	}{
		{
			name: "exact in a several-values Dimension removes the values left out",
			edit: func(f goalDimensionValuesFixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: []int64{f.tags.Values[1].ID}}
			},
			dim:  func(f goalDimensionValuesFixture) domain.Dimension { return f.tags },
			want: []string{"ux"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newGoalDimensionValuesFixture(t)
			actor, e := tc.edit(f)

			got, err := f.h.Service.SetGoalDimensionValues(context.Background(), actor.ID, e)

			switch {
			case tc.refused == "" && err != nil:
				t.Fatalf("SetGoalDimensionValues: %v", err)
			case tc.refused != "" && (!errors.Is(err, tc.kind) || domain.Reason(err) != tc.refused):
				t.Fatalf("SetGoalDimensionValues err = %v, want %v %q", err, tc.kind, tc.refused)
			}
			if got.Value != tc.typed {
				t.Errorf("typed value = %q, want %q", got.Value, tc.typed)
			}
			if values := f.valuesIn(t, tc.dim(f)); !slices.Equal(values, tc.want) {
				t.Errorf("Alpha's %s = %q, want %q", tc.dim(f).Name, values, tc.want)
			}
		})
	}
}
