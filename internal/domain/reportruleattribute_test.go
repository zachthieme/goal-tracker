package domain_test

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A rule row's operators read as the rule does: "is Top-level" and "is not
// Top-level" for a Top-level rule, which tests no values; "is in the org of"
// and the like for the Owner's org; and "is", "is any of" and "is not" for
// every other attribute.
func TestRuleOperatorsWordEachAttributesOperators(t *testing.T) {
	t.Parallel()

	plain := []domain.RuleOperator{{Op: domain.RuleIs, Label: "is"}, {Op: domain.RuleIsAnyOf, Label: "is any of"}, {Op: domain.RuleIsNot, Label: "is not"}}
	for _, tc := range []struct {
		attribute string
		want      []domain.RuleOperator
	}{
		{domain.RuleTopLevel, []domain.RuleOperator{{Op: domain.RuleIs, Label: "is Top-level"}, {Op: domain.RuleIsNot, Label: "is not Top-level"}}},
		{domain.RuleOrg, []domain.RuleOperator{
			{Op: domain.RuleIs, Label: "is in the org of"}, {Op: domain.RuleIsAnyOf, Label: "is in the org of any of"}, {Op: domain.RuleIsNot, Label: "is not in the org of"},
		}},
		{domain.RuleDimension, plain},
		{domain.RuleOwner, plain},
		{domain.RuleLifecycle, plain},
		{domain.RuleHealth, plain},
		{"", plain},
	} {
		if got := domain.RuleOperators(tc.attribute); !slices.Equal(got, tc.want) {
			t.Errorf("RuleOperators(%q) = %v, want %v", tc.attribute, got, tc.want)
		}
	}
}

// What a Report rule can test is each Dimension by name with its values,
// then the Owner, the Owner's org, the Lifecycle, the Health and Top-level,
// which tests none. A Retired Dimension marks itself and every value Retired,
// and a Retired value marks only itself. The Owner offers the Goals' Owners,
// then anyone an Owner rule names; the Owner's org those Owners, then each
// Manager, then anyone an Org rule names, so a saved rule keeps its people
// though they own no Goal. A value naming no one is skipped.
func TestReportRuleAttributesListWhatARuleCanTest(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	ctx := context.Background()
	admin := h.SignInNamed("admin@example.com", "Ada Admin")
	morgan := h.SignInNamed("morgan@example.com", "Morgan Wu")
	priya := h.SignInNamed("priya@example.com", "Priya Raman")
	nora := h.SignInNamed("nora@example.com", "Nora Quinn")
	otto := h.SignInNamed("otto@example.com", "Otto Berg")
	h.SetManager(priya, morgan)
	team := h.CreateDimension(admin, "Team", "Platform", "Growth", "Legacy")
	region := h.CreateDimension(admin, "Region", "EMEA", "Americas")
	if err := h.Service.RetireDimensionValue(ctx, admin.ID, team.Values[2].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	if err := h.Service.RetireDimension(ctx, admin.ID, region.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	goals := []domain.Goal{
		h.ActiveGoal(priya, "Ship the platform", "It matters."),
		h.ActiveGoal(admin, "Plan the offsite", "It matters."),
		h.ActiveGoal(priya, "Grow signups", "It matters."),
	}
	id := func(a domain.Account) string { return strconv.FormatInt(a.ID, 10) }
	rules := []domain.ReportRule{
		{Attribute: domain.RuleOwner, Op: domain.RuleIsAnyOf, Values: []string{id(nora), id(priya), "not an id", "999999"}},
		{Attribute: domain.RuleOrg, Op: domain.RuleIs, Values: []string{id(otto)}},
		{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive}},
	}

	got, err := h.Service.ReportRuleAttributes(ctx, goals, rules)
	if err != nil {
		t.Fatalf("ReportRuleAttributes: %v", err)
	}

	value := func(id int64, label string, retired bool) domain.RuleValue {
		return domain.RuleValue{Value: strconv.FormatInt(id, 10), Label: label, Retired: retired}
	}
	person := func(a domain.Account, label string) domain.RuleValue { return value(a.ID, label, false) }
	named := func(names ...string) []domain.RuleValue {
		var out []domain.RuleValue
		for _, n := range names {
			out = append(out, domain.RuleValue{Value: n, Label: n})
		}
		return out
	}
	want := []domain.RuleAttribute{
		{Attribute: domain.RuleDimension, DimensionID: region.ID, Label: "Region", Retired: true, Values: []domain.RuleValue{
			value(region.Values[0].ID, "EMEA", true), value(region.Values[1].ID, "Americas", true),
		}},
		{Attribute: domain.RuleDimension, DimensionID: team.ID, Label: "Team", Values: []domain.RuleValue{
			value(team.Values[0].ID, "Platform", false), value(team.Values[1].ID, "Growth", false), value(team.Values[2].ID, "Legacy", true),
		}},
		{Attribute: domain.RuleOwner, Label: "Owner", Values: []domain.RuleValue{
			person(priya, "Priya Raman"), person(admin, "Ada Admin"), person(nora, "Nora Quinn"),
		}},
		{Attribute: domain.RuleOrg, Label: "Owner's org", Values: []domain.RuleValue{
			person(priya, "Priya Raman"), person(admin, "Ada Admin"), person(morgan, "Morgan Wu"), person(otto, "Otto Berg"),
		}},
		{Attribute: domain.RuleLifecycle, Label: "Lifecycle", Values: named("Proposed", "Active", "On Hold", "Done", "Cancelled")},
		{Attribute: domain.RuleHealth, Label: "Health", Values: named("Green", "Yellow", "Red")},
		{Attribute: domain.RuleTopLevel, Label: "Top-level"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReportRuleAttributes =\n%+v\nwant\n%+v", got, want)
	}
}

// The Owner's org is marked Retired when no one has a Manager, as an Org is
// then only its person, so only a saved rule that already tests it offers it.
func TestReportRuleAttributesRetireTheOwnersOrgWhenNoOneHasAManager(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	goals := []domain.Goal{h.ActiveGoal(admin, "Plan the offsite", "It matters.")}

	got, err := h.Service.ReportRuleAttributes(context.Background(), goals, nil)
	if err != nil {
		t.Fatalf("ReportRuleAttributes: %v", err)
	}
	at := slices.IndexFunc(got, func(a domain.RuleAttribute) bool { return a.Attribute == domain.RuleOrg })
	if at < 0 {
		t.Fatalf("no Owner's org among %+v", got)
	}
	if !got[at].Retired {
		t.Errorf("the Owner's org isn't Retired with no Manager: %+v", got[at])
	}
	if owner := got[at-1]; owner.Attribute != domain.RuleOwner || owner.Retired {
		t.Errorf("the Owner before it is %+v, want an Owner attribute not Retired", owner)
	}
}
