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
// carries, by every rule for giving a Goal values; its check-only mode finds
// every problem under the inputs named, writing nothing (CONTEXT.md:
// Dimension, Extendable, Fixed, Retired, Delegate).
func TestSetGoalDimensionValues(t *testing.T) {
	t.Parallel()

	type fixture = goalDimensionValuesFixture
	pillar := func(f fixture) domain.Dimension { return f.pillar }
	tags := func(f fixture) domain.Dimension { return f.tags }
	sponsor := func(f fixture) domain.Dimension { return f.sponsor }
	region := func(f fixture) domain.Dimension { return f.region }
	ids := func(values ...domain.DimensionValue) []int64 {
		out := make([]int64, 0, len(values))
		for _, v := range values {
			out = append(out, v.ID)
		}
		return out
	}
	const (
		fixedPillar  = "Pillar is a Fixed list, so only an Admin may add to it"
		onePillar    = "Pillar takes one value per Goal"
		oneSponsor   = "Sponsor takes one value per Goal"
		notSetter    = "only the Owner, a Delegate or an Admin may set a Goal's Dimension values"
		semicolonAB  = "A;B: a value can't contain a semicolon, because the import format uses it to separate values"
		legacyNewly  = "legacy is retired and cannot be newly assigned"
		goneNewly    = "gone is retired and cannot be newly assigned"
		legacyTyped  = "legacy is retired, so it can't be added or newly assigned"
		regionNewly  = "Region is retired, so its values can't be newly assigned"
		blankRefusal = "a value cannot be blank"
	)
	for _, tc := range []struct {
		name string
		// edit is the edit and who makes it.
		edit func(f fixture) (actor domain.Account, e domain.DimensionValuesEdit)
		// checkOnly runs the edit in check-only mode, which finds problems,
		// each "input: message", in that order.
		checkOnly bool
		problems  func(f fixture) []string
		// dim is the Dimension whose values the Goal carries afterwards are
		// want, and whose list is listed, when that is given.
		dim    func(f fixture) domain.Dimension
		want   []string
		listed []string
		// typed is the value the typed name set, "" for none.
		typed string
		// refused is the refusal, "" for none, and kind the error it is.
		refused string
		kind    error
	}{
		{
			name: "exact in a several-values Dimension removes the values left out",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[1])}
			},
			dim:  tags,
			want: []string{"ux"},
		},
		{
			name: "join in a several-values Dimension adds alongside",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[1]), Join: true}
			},
			dim:  tags,
			want: []string{"infra", "ux", "legacy"},
		},
		{
			name: "exact in a one-value Dimension replaces",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, ValueIDs: ids(f.pillar.Values[1])}
			},
			dim:  pillar,
			want: []string{"Trust"},
		},
		{
			name: "exact with nothing chosen clears the Dimension",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID}
			},
			dim: pillar,
		},
		{
			name: "join in a one-value Dimension replaces",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, ValueIDs: ids(f.pillar.Values[1]), Join: true}
			},
			dim:  pillar,
			want: []string{"Trust"},
		},
		{
			name: "a typed name sets the value it matches whatever its case",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, NewValue: " growth ", Join: true}
			},
			dim:    pillar,
			want:   []string{"Growth"},
			listed: []string{"Growth", "Trust", "Old"},
			typed:  "Growth",
		},
		{
			name: "a typed name joins the values chosen in a several-values Dimension",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.boss, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[1]), NewValue: "Mobile"}
			},
			dim:    tags,
			want:   []string{"ux", "Mobile"},
			listed: []string{"infra", "ux", "legacy", "gone", "Mobile"},
			typed:  "Mobile",
		},
		{
			name: "a new name is added to an Extendable list",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.sponsor.ID, NewValue: "Dana", Join: true}
			},
			dim:    sponsor,
			want:   []string{"Dana"},
			listed: []string{"Ana", "Dana"},
			typed:  "Dana",
		},
		{
			name: "an Admin adds a new name to a Fixed list",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.boss, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, NewValue: "Moonshot", Join: true}
			},
			dim:    pillar,
			want:   []string{"Moonshot"},
			listed: []string{"Growth", "Trust", "Old", "Moonshot"},
			typed:  "Moonshot",
		},
		{
			name: "the Owner can't add a new name to a Fixed list",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, NewValue: "Moonshot", Join: true}
			},
			dim:     pillar,
			want:    []string{"Old"},
			listed:  []string{"Growth", "Trust", "Old"},
			refused: fixedPillar,
			kind:    domain.ErrNotAuthorized,
		},
		{
			name: "a typed name matching a Retired value is refused, though the Goal carries it",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, NewValue: "LEGACY", Join: true}
			},
			dim:     tags,
			want:    []string{"infra", "legacy"},
			refused: legacyTyped,
			kind:    domain.ErrValidation,
		},
		{
			name: "exact keeps a Retired value the Goal carries",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[2])}
			},
			dim:  tags,
			want: []string{"legacy"},
		},
		{
			name: "join refuses a Retired value the Goal carries",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[2]), Join: true}
			},
			dim:     tags,
			want:    []string{"infra", "legacy"},
			refused: legacyNewly,
			kind:    domain.ErrValidation,
		},
		{
			name: "exact refuses a Retired value the Goal doesn't carry",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[1], f.tags.Values[3])}
			},
			dim:     tags,
			want:    []string{"infra", "legacy"},
			refused: goneNewly,
			kind:    domain.ErrValidation,
		},
		{
			name: "join refuses a Retired value the Goal doesn't carry",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[3]), Join: true}
			},
			dim:     tags,
			want:    []string{"infra", "legacy"},
			refused: goneNewly,
			kind:    domain.ErrValidation,
		},
		{
			name: "a value in a Retired Dimension is refused",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.region.ID, ValueIDs: ids(f.region.Values[0])}
			},
			dim:     region,
			refused: regionNewly,
			kind:    domain.ErrValidation,
		},
		{
			name: "several chosen in a one-value Dimension are refused",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.pillar.ID, ValueIDs: ids(f.pillar.Values[0], f.pillar.Values[1])}
			},
			dim:     pillar,
			want:    []string{"Old"},
			refused: onePillar,
			kind:    domain.ErrValidation,
		},
		{
			name: "one chosen and one typed in a one-value Dimension are refused",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.sponsor.ID, ValueIDs: ids(f.sponsor.Values[0]), NewValue: "Dana"}
			},
			dim:     sponsor,
			listed:  []string{"Ana"},
			refused: oneSponsor,
			kind:    domain.ErrValidation,
		},
		{
			name: "a new name can't contain a semicolon",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.sponsor.ID, NewValue: "A;B", Join: true}
			},
			dim:     sponsor,
			listed:  []string{"Ana"},
			refused: semicolonAB,
			kind:    domain.ErrValidation,
		},
		{
			name: "someone not the Owner, a Delegate or an Admin is refused",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.pat, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, ValueIDs: ids(f.tags.Values[1])}
			},
			dim:     tags,
			want:    []string{"infra", "legacy"},
			refused: notSetter,
			kind:    domain.ErrNotAuthorized,
		},
		{
			name: "a blank name is refused when a name is required",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.sponsor.ID, NewValue: "  ", NameRequired: true, Join: true}
			},
			dim:     sponsor,
			refused: blankRefusal,
			kind:    domain.ErrValidation,
		},
		{
			name: "a blank name is no name typed when none is required",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{GoalID: f.goal.ID, DimensionID: f.tags.ID, NewValue: "  ", Join: true}
			},
			dim:  tags,
			want: []string{"infra", "legacy"},
		},
		{
			name: "check-only refuses several in a one-value Dimension and a Fixed list's new name, writing nothing",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{
					GoalID: f.goal.ID, DimensionID: f.pillar.ID,
					ValueIDs: ids(f.pillar.Values[1], f.pillar.Values[0]), NewValue: "Moonshot",
					ValueInput: domain.ValueInput, NewValueInput: domain.NewValueInput(f.pillar.ID),
				}
			},
			checkOnly: true,
			problems: func(f fixture) []string {
				return []string{
					domain.NewValueInput(f.pillar.ID) + ": " + fixedPillar,
					domain.ValueInput(f.pillar.Values[1].ID) + ": " + onePillar,
					domain.ValueInput(f.pillar.Values[0].ID) + ": " + onePillar,
				}
			},
			dim:    pillar,
			want:   []string{"Old"},
			listed: []string{"Growth", "Trust", "Old"},
		},
		{
			name: "check-only finds every chosen and typed problem",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{
					GoalID: f.goal.ID, DimensionID: f.tags.ID,
					ValueIDs: ids(f.tags.Values[3], f.tags.Values[1], f.tags.Values[2]), NewValue: "A;B", Join: true,
					ValueInput: domain.ValueInput, NewValueInput: domain.NewValueInput(f.tags.ID),
				}
			},
			checkOnly: true,
			problems: func(f fixture) []string {
				return []string{
					domain.ValueInput(f.tags.Values[3].ID) + ": " + goneNewly,
					domain.ValueInput(f.tags.Values[2].ID) + ": " + legacyNewly,
					domain.NewValueInput(f.tags.ID) + ": " + semicolonAB,
				}
			},
			dim:  tags,
			want: []string{"infra", "legacy"},
		},
		{
			name: "check-only counts a typed name toward one value, adding nothing",
			edit: func(f fixture) (domain.Account, domain.DimensionValuesEdit) {
				return f.sam, domain.DimensionValuesEdit{
					GoalID: f.goal.ID, DimensionID: f.sponsor.ID,
					ValueIDs: ids(f.sponsor.Values[0]), NewValue: "Dana",
					ValueInput: domain.ValueInput, NewValueInput: domain.NewValueInput(f.sponsor.ID),
				}
			},
			checkOnly: true,
			problems: func(f fixture) []string {
				return []string{
					domain.ValueInput(f.sponsor.Values[0].ID) + ": " + oneSponsor,
					domain.NewValueInput(f.sponsor.ID) + ": " + oneSponsor,
				}
			},
			dim:    sponsor,
			listed: []string{"Ana"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newGoalDimensionValuesFixture(t)
			ctx := context.Background()
			actor, e := tc.edit(f)

			if tc.checkOnly {
				found, err := domain.CheckGoalDimensionValues(ctx, f.h.Service, actor.ID, e)
				if err != nil {
					t.Fatalf("CheckGoalDimensionValues: %v", err)
				}
				var got []string
				for _, p := range found {
					got = append(got, p.Input+": "+p.Message)
				}
				if want := tc.problems(f); !slices.Equal(got, want) {
					t.Errorf("problems = %q, want %q", got, want)
				}
			} else {
				got, err := f.h.Service.SetGoalDimensionValues(ctx, actor.ID, e)
				switch {
				case tc.refused == "" && err != nil:
					t.Fatalf("SetGoalDimensionValues: %v", err)
				case tc.refused != "" && (!errors.Is(err, tc.kind) || domain.Reason(err) != tc.refused):
					t.Fatalf("SetGoalDimensionValues err = %v, want %v %q", err, tc.kind, tc.refused)
				}
				if got.Value != tc.typed {
					t.Errorf("typed value = %q, want %q", got.Value, tc.typed)
				}
			}

			dim := tc.dim(f)
			if values := f.valuesIn(t, dim); !slices.Equal(values, tc.want) {
				t.Errorf("Alpha's %s = %q, want %q", dim.Name, values, tc.want)
			}
			if tc.listed != nil {
				var listed []string
				for _, v := range dimensionNamed(t, f.h, dim.Name).Values {
					listed = append(listed, v.Value)
				}
				if !slices.Equal(listed, tc.listed) {
					t.Errorf("%s's list = %q, want %q", dim.Name, listed, tc.listed)
				}
			}
		})
	}
}
