package domain

// RuleOperator is an operator a Report rule offers, Op (RuleIs, RuleIsAnyOf or
// RuleIsNot), with the words it reads as for the rule's attribute.
type RuleOperator struct {
	Op    string
	Label string
}

// RuleOperators are the operators a rule testing attribute offers, each worded
// so the rule reads as it tests: "is Top-level" and "is not Top-level" for a
// Top-level rule, which tests no values, "is in the org of" and the like for
// the Owner's org, and "is", "is any of" and "is not" for any other.
func RuleOperators(attribute string) []RuleOperator {
	switch attribute {
	case RuleTopLevel:
		return []RuleOperator{{RuleIs, "is Top-level"}, {RuleIsNot, "is not Top-level"}}
	case RuleOrg:
		return []RuleOperator{{RuleIs, "is in the org of"}, {RuleIsAnyOf, "is in the org of any of"}, {RuleIsNot, "is not in the org of"}}
	}
	return []RuleOperator{{RuleIs, "is"}, {RuleIsAnyOf, "is any of"}, {RuleIsNot, "is not"}}
}
