package domain_test

import (
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
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
