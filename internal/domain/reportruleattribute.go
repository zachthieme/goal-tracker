package domain

import (
	"context"
	"errors"
	"slices"
	"strconv"
)

// RuleAttribute is an attribute a Report rule can test, with the values it
// offers to test for. Attribute is RuleDimension, RuleOwner, RuleOrg,
// RuleLifecycle, RuleHealth or RuleTopLevel, and DimensionID the Dimension a
// RuleDimension one tests, 0 for any other. A Retired one is offered only to a
// rule that already tests it.
type RuleAttribute struct {
	Attribute   string
	DimensionID int64
	Label       string
	Values      []RuleValue
	Retired     bool
}

// RuleValue is a value a Report rule can test for: Value as ReportRule.Values
// holds it (a Dimension value's or an Account's id in decimal, or a Lifecycle
// or Health name), with its label. A Retired one is offered only to a rule
// that already holds it.
type RuleValue struct {
	Value   string
	Label   string
	Retired bool
}

// ReportRuleAttributes are what a Report rule can test, for a builder offering
// goals and holding rules: each Dimension and its values, then Owner, the
// Owner's org, Lifecycle, Health and Top-level, which tests none. A Retired
// Dimension marks itself and its values Retired, and a Retired value itself;
// the Owner's org is Retired when no one has a Manager, as an Org is then only
// its person. The Owner offers the goals' Owners, then anyone an Owner rule
// among rules names; the Owner's org those Owners, then each Manager, then
// anyone an Org rule names, so a saved rule keeps its people however the
// Goals' Owners or the directory have changed. A rule value that names no
// Account is skipped. Fields are not among them: they describe a Goal and
// never select it (ADR 0005).
func (s *Service) ReportRuleAttributes(ctx context.Context, goals []Goal, rules []ReportRule) ([]RuleAttribute, error) {
	dims, err := s.ListDimensions(ctx)
	if err != nil {
		return nil, err
	}
	managers, err := s.ReportRuleManagers(ctx)
	if err != nil {
		return nil, err
	}
	var owners []Account
	for _, g := range goals {
		if g.Owner.ID != 0 {
			owners = withRuleAccount(owners, g.Owner)
		}
	}
	ownerPeople, err := s.withRuleNamedAccounts(ctx, rules, RuleOwner, owners)
	if err != nil {
		return nil, err
	}
	orgPeople, err := s.withRuleNamedAccounts(ctx, rules, RuleOrg, append(slices.Clone(owners), managers...))
	if err != nil {
		return nil, err
	}

	var out []RuleAttribute
	for _, d := range dims {
		a := RuleAttribute{Attribute: RuleDimension, DimensionID: d.ID, Label: d.Name, Retired: d.Retired}
		for _, v := range d.Values {
			a.Values = append(a.Values, RuleValue{Value: strconv.FormatInt(v.ID, 10), Label: v.Value, Retired: d.Retired || v.Retired})
		}
		out = append(out, a)
	}
	people := func(accounts []Account) []RuleValue {
		var out []RuleValue
		for _, a := range accounts {
			out = append(out, RuleValue{Value: strconv.FormatInt(a.ID, 10), Label: a.Label()})
		}
		return out
	}
	named := func(names []string) []RuleValue {
		var out []RuleValue
		for _, n := range names {
			out = append(out, RuleValue{Value: n, Label: n})
		}
		return out
	}
	return append(out,
		RuleAttribute{Attribute: RuleOwner, Label: "Owner", Values: people(ownerPeople)},
		RuleAttribute{Attribute: RuleOrg, Label: "Owner's org", Values: people(orgPeople), Retired: len(managers) == 0},
		RuleAttribute{Attribute: RuleLifecycle, Label: "Lifecycle", Values: named(Lifecycles())},
		RuleAttribute{Attribute: RuleHealth, Label: "Health", Values: named([]string{HealthGreen, HealthYellow, HealthRed})},
		RuleAttribute{Attribute: RuleTopLevel, Label: "Top-level"},
	), nil
}

// withRuleNamedAccounts is people, one each in first-seen order, then anyone a
// rule among rules testing attribute (RuleOwner or RuleOrg) names who isn't
// among them, each read once per value naming them.
func (s *Service) withRuleNamedAccounts(ctx context.Context, rules []ReportRule, attribute string, people []Account) ([]Account, error) {
	var out []Account
	for _, p := range people {
		out = withRuleAccount(out, p)
	}
	for _, rule := range rules {
		if rule.Attribute != attribute {
			continue
		}
		for _, value := range rule.Values {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				continue
			}
			a, err := s.Account(ctx, id)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			out = withRuleAccount(out, a)
		}
	}
	return out, nil
}

// withRuleAccount is people with a added at the end, unless it is among them.
func withRuleAccount(people []Account, a Account) []Account {
	if slices.ContainsFunc(people, func(p Account) bool { return p.ID == a.ID }) {
		return people
	}
	return append(people, a)
}

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
