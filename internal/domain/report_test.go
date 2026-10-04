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

// A Dimension rule selects by the Goal's values in its Dimension: "is" and
// "is any of" when any of them is listed, "is not" only when none is. A Goal
// with no value there meets only "is not" (CONTEXT.md: Report rule).
func TestReportDimensionRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	customer := h.CreateSeveralValuesDimension(boss, "Customer", "Acme", "Globex", "Initech")
	acme, globex, initech := customer.Values[0], customer.Values[1], customer.Values[2]
	both := h.CreateGoal(boss, "Acme and Globex", "why")
	h.AssignGoalValue(both, acme)
	h.AssignGoalValue(both, globex)
	onlyInitech := h.CreateGoal(boss, "Initech", "why")
	h.AssignGoalValue(onlyInitech, initech)
	none := h.CreateGoal(boss, "No customer", "why")

	for _, c := range []struct {
		name string
		rule domain.ReportRule
		want []int64
	}{
		{"is one of its values", dimensionRule(customer, domain.RuleIs, globex), []int64{both.ID}},
		{"is any of", dimensionRule(customer, domain.RuleIsAnyOf, acme, initech), []int64{both.ID, onlyInitech.ID}},
		{"is not one of its values", dimensionRule(customer, domain.RuleIsNot, acme), []int64{onlyInitech.ID, none.ID}},
		{"is not any listed", dimensionRule(customer, domain.RuleIsNot, globex, initech), []int64{none.ID}},
	} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{c.rule}})
		if got := selectByDefault(t, h, def); !sameSet(got, c.want) {
			t.Errorf("Customer %s selected %v, want %v", c.name, got, c.want)
		}
	}
}

// An Owner rule selects by the Owner of record, never a Delegate. A departed
// Owner's Goals are Ownerless but still theirs on record, so "Owner is" keeps
// selecting them (CONTEXT.md: Report rule, Ownerless).
func TestReportOwnerRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	sam := h.SignIn("sam@example.com")
	alices := h.ActiveGoal(alice, "Alice's goal", "why")
	sams := h.ActiveGoal(sam, "Sam's goal", "why")
	h.AddDelegate(sam, alice, sams.ID)
	if err := h.Service.MarkDeparted(ctx, boss.ID, alice.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	for _, c := range []struct {
		name string
		rule domain.ReportRule
		want []int64
	}{
		{"is Alice", ownerRule(domain.RuleIs, alice), []int64{alices.ID}},
		{"is any of Alice, Sam", ownerRule(domain.RuleIsAnyOf, alice, sam), []int64{alices.ID, sams.ID}},
		{"is not Alice", ownerRule(domain.RuleIsNot, alice), []int64{sams.ID}},
	} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{c.rule}})
		if got := selectByDefault(t, h, def); !sameSet(got, c.want) {
			t.Errorf("Owner %s selected %v, want %v", c.name, got, c.want)
		}
	}
}

// ownerRule is the rule "Owner op accounts".
func ownerRule(op string, accounts ...domain.Account) domain.ReportRule {
	rule := domain.ReportRule{Attribute: domain.RuleOwner, Op: op}
	for _, a := range accounts {
		rule.Values = append(rule.Values, strconv.FormatInt(a.ID, 10))
	}
	return rule
}

// A Health rule selects by the Owner-set Health, the latest Check-in's. A Goal
// with no Health — Proposed, On Hold, Done or Cancelled, or Active with no
// Check-in yet — meets only "is not" (CONTEXT.md: Report rule).
func TestReportHealthRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	red := h.ActiveGoal(boss, "Red", "why")
	green := h.ActiveGoal(boss, "Green", "why")
	unchecked := h.ActiveGoal(boss, "Active, no Check-in", "why")
	proposed := h.CreateGoal(boss, "Proposed", "why")
	onHold := h.OnHoldGoal(boss, "On Hold", "why", "Waiting on budget.")
	done := h.ActiveGoal(boss, "Done", "why")
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked.", "Unblock.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})
	h.Checkin(boss, done.ID, domain.HealthRed, "Blocked.", "Unblock.", h.Clock.Now().AddDate(0, 1, 0))
	moveLifecycle(t, h, boss, done, domain.LifecycleDone)
	noHealth := []int64{unchecked.ID, proposed.ID, onHold.ID, done.ID}

	for _, c := range []struct {
		name string
		rule domain.ReportRule
		want []int64
	}{
		{"is Red", namedRule(domain.RuleHealth, domain.RuleIs, domain.HealthRed), []int64{red.ID}},
		{"is any of Red, Green", namedRule(domain.RuleHealth, domain.RuleIsAnyOf, domain.HealthRed, domain.HealthGreen), []int64{red.ID, green.ID}},
		{"is not Red", namedRule(domain.RuleHealth, domain.RuleIsNot, domain.HealthRed), append([]int64{green.ID}, noHealth...)},
	} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{c.rule}})
		if got := selectByDefault(t, h, def); !sameSet(got, c.want) {
			t.Errorf("Health %s selected %v, want %v", c.name, got, c.want)
		}
	}
}

// namedRule is the rule "attribute op names", for a Lifecycle or Health rule.
func namedRule(attribute, op string, names ...string) domain.ReportRule {
	return domain.ReportRule{Attribute: attribute, Op: op, Values: names}
}

// moveLifecycle moves the Active Goal g to lifecycle (Done, Cancelled or On
// Hold) in a Check-in by its Owner, failing the test on error.
func moveLifecycle(t *testing.T, h *testsupport.Harness, owner domain.Account, g domain.Goal, lifecycle string) {
	t.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          g.ID,
		AuthorID:        owner.ID,
		Status:          "Moving on.",
		Lifecycle:       lifecycle,
		LifecycleReason: "Priorities changed.",
		Outcome:         "Shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin to %s: %v", lifecycle, err)
	}
}

// A Top-level rule takes no values: it is "is Top-level" or "is not
// Top-level" (CONTEXT.md: Top-level Goal).
func TestReportTopLevelRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	top := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "why"))
	other := h.ActiveGoal(boss, "Launch in EU", "why")

	for op, want := range map[string][]int64{domain.RuleIs: {top.ID}, domain.RuleIsNot: {other.ID}} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: op, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
			{Attribute: domain.RuleTopLevel, Op: op},
		}})
		if got := selectByDefault(t, h, def); !sameSet(got, want) {
			t.Errorf("%s Top-level selected %v, want %v", op, got, want)
		}
	}
}

// A Lifecycle rule selects by the Goal's Lifecycle. With no Lifecycle rule,
// every Lifecycle is selected (CONTEXT.md: Report rule).
func TestReportLifecycleRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	proposed := h.CreateGoal(boss, "Proposed", "why")
	active := h.ActiveGoal(boss, "Active", "why")
	onHold := h.OnHoldGoal(boss, "On Hold", "why", "Waiting on budget.")
	settle(h)

	for _, c := range []struct {
		name  string
		rules []domain.ReportRule
		want  []int64
	}{
		{"is Active", []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive)}, []int64{active.ID}},
		{"is any of Active, On Hold", []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIsAnyOf, domain.LifecycleActive, domain.LifecycleOnHold)}, []int64{active.ID, onHold.ID}},
		{"is not Active", []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIsNot, domain.LifecycleActive)}, []int64{proposed.ID, onHold.ID}},
		{"with no Lifecycle rule", []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIsNot}}, []int64{proposed.ID, active.ID, onHold.ID}},
	} {
		def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: c.rules})
		if got := selectByDefault(t, h, def); !sameSet(got, c.want) {
			t.Errorf("Lifecycle %s selected %v, want %v", c.name, got, c.want)
		}
	}
}

// A Goal must meet every rule, and meets one by having any of its values. A
// rules definition then adds the Goals to Also include and takes away those
// to Leave out; a Goal in both is left out (CONTEXT.md: Report rule).
func TestReportRulesAndOverrides(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Identity", "Web")
	platform, identity, web := team.Values[0], team.Values[1], team.Values[2]
	bossPlatform := h.CreateGoal(boss, "Boss, Platform", "why")
	bossIdentity := h.CreateGoal(boss, "Boss, Identity", "why")
	bossWeb := h.CreateGoal(boss, "Boss, Web", "why")
	samPlatform := h.CreateGoal(sam, "Sam, Platform", "why")
	samWeb := h.CreateGoal(sam, "Sam, Web", "why")
	bothLists := h.CreateGoal(sam, "Sam, Identity", "why")
	h.AssignGoalValue(bossPlatform, platform)
	h.AssignGoalValue(bossIdentity, identity)
	h.AssignGoalValue(bossWeb, web)
	h.AssignGoalValue(samPlatform, platform)
	h.AssignGoalValue(samWeb, web)
	h.AssignGoalValue(bothLists, identity)
	rules := []domain.ReportRule{
		dimensionRule(team, domain.RuleIsAnyOf, platform, identity),
		ownerRule(domain.RuleIs, boss),
	}

	matched := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Rules", Mode: domain.ReportModeRules, Rules: rules})
	if got, want := selectByDefault(t, h, matched), []int64{bossPlatform.ID, bossIdentity.ID}; !sameSet(got, want) {
		t.Errorf("Team is any of Platform, Identity and Owner is boss selected %v, want %v", got, want)
	}

	overridden := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "Overridden",
		Mode:    domain.ReportModeRules,
		Rules:   rules,
		Include: []int64{samWeb.ID, bothLists.ID},
		Exclude: []int64{bossIdentity.ID, bothLists.ID},
	})
	if got, want := selectByDefault(t, h, overridden), []int64{bossPlatform.ID, samWeb.ID}; !sameSet(got, want) {
		t.Errorf("with Also include and Leave out selected %v, want %v", got, want)
	}
}

// A Goal Done or Cancelled before the baseline drops out of a Report however
// it was selected — by a rule, by Also include or by hand-picking — so
// finished work is reported once. One Done since the baseline is still
// selected, to report its finish (CONTEXT.md: Report Definition).
func TestReportDropsWorkFinishedBeforeTheBaseline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	doneBefore := h.ActiveGoal(boss, "Done before", "why")
	cancelledBefore := h.ActiveGoal(boss, "Cancelled before", "why")
	samDoneBefore := h.ActiveGoal(sam, "Sam's, done before", "why")
	doneSince := h.ActiveGoal(boss, "Done since", "why")
	moveLifecycle(t, h, boss, doneBefore, domain.LifecycleDone)
	moveLifecycle(t, h, boss, cancelledBefore, domain.LifecycleCancelled)
	moveLifecycle(t, h, sam, samDoneBefore, domain.LifecycleDone)
	settle(h)
	moveLifecycle(t, h, boss, doneSince, domain.LifecycleDone)

	byRule := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "By rule",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{ownerRule(domain.RuleIs, boss)},
		Include: []int64{samDoneBefore.ID},
	})
	if got, want := selectByDefault(t, h, byRule), []int64{doneSince.ID}; !sameSet(got, want) {
		t.Errorf("by rule and Also include selected %v, want %v", got, want)
	}
	picked := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:   "Picked",
		Mode:   domain.ReportModePicked,
		Picked: []int64{doneBefore.ID, cancelledBefore.ID, samDoneBefore.ID, doneSince.ID},
	})
	if got, want := selectByDefault(t, h, picked), []int64{doneSince.ID}; !sameSet(got, want) {
		t.Errorf("by hand-picking selected %v, want %v", got, want)
	}

	// Read against a baseline before they finished, each is reported.
	r, err := h.Service.DraftReport(context.Background(), picked, testsupport.Epoch)
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if got, want := append(blockIDs(r), lineIDs(r)...), picked.Picked; !sameSet(got, want) {
		t.Errorf("against a baseline before they finished, selected %v, want %v", got, want)
	}
}

// A Goal that changed Lifecycle since the baseline still passes a Lifecycle
// rule that now excludes it, so its change is reported once. The exception is
// the Lifecycle rule's alone: the Goal must still meet the other rules, and
// Leave out still leaves it out (CONTEXT.md: Report rule).
func TestReportLifecycleRuleKeepsAGoalThatChangedSinceTheBaseline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	active := h.ActiveGoal(boss, "Still Active", "why")
	pausedBefore := h.ActiveGoal(boss, "On Hold before", "why")
	pausedSince := h.ActiveGoal(boss, "On Hold since", "why")
	doneSince := h.ActiveGoal(boss, "Done since", "why")
	samsPausedSince := h.ActiveGoal(sam, "Sam's, On Hold since", "why")
	moveLifecycle(t, h, boss, pausedBefore, domain.LifecycleOnHold)
	settle(h)
	moveLifecycle(t, h, boss, pausedSince, domain.LifecycleOnHold)
	moveLifecycle(t, h, boss, doneSince, domain.LifecycleDone)
	moveLifecycle(t, h, sam, samsPausedSince, domain.LifecycleOnHold)
	isActive := namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive)

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Active", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		isActive,
		ownerRule(domain.RuleIs, boss),
	}})
	if got, want := selectByDefault(t, h, def), []int64{active.ID, pausedSince.ID, doneSince.ID}; !sameSet(got, want) {
		t.Errorf("Lifecycle is Active and Owner is boss selected %v, want %v", got, want)
	}

	left := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "Active, leaving out",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{isActive},
		Exclude: []int64{pausedSince.ID},
	})
	if got, want := selectByDefault(t, h, left), []int64{active.ID, doneSince.ID, samsPausedSince.ID}; !sameSet(got, want) {
		t.Errorf("Lifecycle is Active, leaving one out, selected %v, want %v", got, want)
	}
}

// Saving refuses each input it can't take with an InputError naming it:
// rules[i] counts from zero (CONTEXT.md: Report Definition, Report rule).
func TestSaveReportDefinitionRefusals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	platform, growth := team.Values[0], pillar.Values[0]
	g := h.CreateGoal(boss, "Grow revenue", "why")
	isPlatform := dimensionRule(team, domain.RuleIs, platform)
	rules := func(rs ...domain.ReportRule) domain.SaveReportDefinitionInput {
		return domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: rs}
	}

	for _, c := range []struct {
		name string
		in   domain.SaveReportDefinitionInput
		want string
	}{
		{"blank name", domain.SaveReportDefinitionInput{Name: " ", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}}, "name"},
		{"unknown mode", domain.SaveReportDefinitionInput{Name: "MBR", Mode: "graph", Picked: []int64{g.ID}}, "mode"},
		{"rules definition with no rules", rules(), "rules"},
		{"rule with no values", rules(isPlatform, dimensionRule(team, domain.RuleIsAnyOf)), "rules[1]"},
		{"is with two values", rules(domain.ReportRule{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive, domain.LifecycleOnHold}}), "rules[0]"},
		{"unknown operator", rules(domain.ReportRule{Attribute: domain.RuleLifecycle, Op: "contains", Values: []string{domain.LifecycleActive}}), "rules[0]"},
		{"unknown attribute", rules(domain.ReportRule{Attribute: "field", Op: domain.RuleIs, Values: []string{"1"}}), "rules[0]"},
		{"unknown Dimension", rules(domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: 9999, Op: domain.RuleIs, Values: []string{strconv.FormatInt(platform.ID, 10)}}), "rules[0]"},
		{"unknown Dimension value", rules(domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIs, Values: []string{"9999"}}), "rules[0]"},
		{"value of another Dimension", rules(isPlatform, dimensionRule(team, domain.RuleIsNot, growth)), "rules[1]"},
		{"unknown Account", rules(domain.ReportRule{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{"9999"}}), "rules[0]"},
		{"unknown Lifecycle", rules(namedRule(domain.RuleLifecycle, domain.RuleIs, "Paused")), "rules[0]"},
		{"unknown Health", rules(namedRule(domain.RuleHealth, domain.RuleIsNot, "Blue")), "rules[0]"},
		{"Top-level with values", rules(domain.ReportRule{Attribute: domain.RuleTopLevel, Op: domain.RuleIs, Values: []string{"yes"}}), "rules[0]"},
		{"Top-level is any of", rules(domain.ReportRule{Attribute: domain.RuleTopLevel, Op: domain.RuleIsAnyOf}), "rules[0]"},
		{"unknown Also include", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Include: []int64{9999}}, "include"},
		{"unknown Leave out", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Exclude: []int64{9999}}, "exclude"},
		{"rules definition with a picked list", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Picked: []int64{g.ID}}, "picked"},
		{"picked definition with no Goals", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked}, "picked"},
		{"unknown picked Goal", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID, 9999}}, "picked"},
		{"picked definition with rules", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Rules: []domain.ReportRule{isPlatform}}, "rules"},
		{"picked definition with Also include", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Include: []int64{g.ID}}, "include"},
		{"picked definition with Leave out", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Exclude: []int64{g.ID}}, "exclude"},
	} {
		_, err := h.Service.SaveReportDefinition(context.Background(), boss.ID, c.in)
		problems := domain.InputErrors(err)
		if !errors.Is(err, domain.ErrValidation) || len(problems) != 1 || problems[0].Input != c.want {
			t.Errorf("%s: err = %v (inputs %v), want one InputError naming %s", c.name, err, inputNames(problems), c.want)
		}
	}

	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil {
		t.Fatalf("ListReportDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("refused definitions were saved: %+v", defs)
	}
}

// inputNames lists the inputs problems name.
func inputNames(problems []*domain.InputError) []string {
	out := make([]string, 0, len(problems))
	for _, p := range problems {
		out = append(out, p.Input)
	}
	return out
}

// dimensionRule is the rule "dim op values".
func dimensionRule(dim domain.Dimension, op string, values ...domain.DimensionValue) domain.ReportRule {
	rule := domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: dim.ID, Op: op}
	for _, v := range values {
		rule.Values = append(rule.Values, strconv.FormatInt(v.ID, 10))
	}
	return rule
}

// selectByDefault returns the ids of the Goals def selects against the
// default baseline: its previous publication, or 30 days ago.
func selectByDefault(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) []int64 {
	t.Helper()
	r, err := h.Service.DraftReport(context.Background(), def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	return append(blockIDs(r), lineIDs(r)...)
}

// A Report Definition shows the Fields its author chose beside each Goal that
// has a value in them, in both treatments: the exception block and the
// one-line Green Goal. A chosen Field a Goal has no value in is left out, not
// shown blank, and a Field nobody chose isn't shown at all (ticket #78).
func TestReportShowsChosenFieldsBesideEachGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	sponsor := h.CreateField(boss, "Sponsor", domain.FieldShortText, "")
	notes := h.CreateField(boss, "Notes", domain.FieldLongText, "")
	red := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	green := h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	h.SetGoalField(boss, red, budget, "120")
	h.SetGoalField(boss, red, sponsor, "Dana")
	h.SetGoalField(boss, red, notes, "Not for the Report.")
	h.SetGoalField(boss, green, budget, "40")
	settle(h)
	h.Checkin(boss, red.ID, domain.HealthRed, "Blocked on legal.", "Hire counsel.", h.Clock.Now().AddDate(0, 1, 0))
	h.Checkin(boss, green.ID, domain.HealthGreen, "On track.", "", time.Time{})

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "MBR",
		Mode: domain.ReportModePicked, Picked: []int64{red.ID, green.ID},
		FieldIDs: []int64{budget.ID, sponsor.ID},
	})
	if got, want := def.FieldIDs, []int64{budget.ID, sponsor.ID}; !sameSet(got, want) {
		t.Errorf("saved Fields %v, want %v", got, want)
	}
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if len(r.Exceptions) != 1 || len(r.Lines) != 1 {
		t.Fatalf("got %d blocks and %d lines, want the Red block and the Green line", len(r.Exceptions), len(r.Lines))
	}
	if got, want := fieldReadings(r.Exceptions[0].Fields), []string{"Budget=120 $", "Sponsor=Dana"}; !slices.Equal(got, want) {
		t.Errorf("exception block shows Fields %v, want %v", got, want)
	}
	if got, want := fieldReadings(r.Lines[0].Fields), []string{"Budget=40 $"}; !slices.Equal(got, want) {
		t.Errorf("one-line Goal shows Fields %v, want %v (Sponsor unset, so left out)", got, want)
	}

	// A Report shows no Fields unless its author chooses some.
	plain := draftReport(t, h, boss, time.Time{}, red, green)
	if len(plain.Exceptions[0].Fields) != 0 || len(plain.Lines[0].Fields) != 0 {
		t.Errorf("a definition with no Fields chosen shows %v and %v", plain.Exceptions[0].Fields, plain.Lines[0].Fields)
	}
}

// A Report Definition can only be set to show Fields that exist and aren't
// Retired, as a Retired Field is no longer offered (CONTEXT.md: Retired). A
// Field retired after it was chosen keeps showing its values (ADR 0005: saved
// Report Definitions that reference it keep working).
func TestReportDefinitionChoosesOnlyOfferedFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SetGoalField(boss, g, budget, "120")
	if err := h.Service.RetireField(ctx, boss.ID, legacy.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}

	for name, ids := range map[string][]int64{"retired": {legacy.ID}, "unknown": {9999}} {
		if _, err := h.Service.SaveReportDefinition(ctx, boss.ID, domain.SaveReportDefinitionInput{
			Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: ids,
		}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("choosing a %s Field: err = %v, want ErrValidation", name, err)
		}
	}

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{budget.ID}})
	if err := h.Service.RetireField(ctx, boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	if got, want := fieldReadings(r.Exceptions[0].Fields), []string{"Budget=120 $"}; !slices.Equal(got, want) {
		t.Errorf("after retiring Budget the Goal shows Fields %v, want %v", got, want)
	}
}

// fieldReadings renders Field values as Name=Value Unit, for comparing what a
// Report shows beside a Goal.
func fieldReadings(values []domain.FieldValue) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strings.TrimSpace(v.Field.Name+"="+v.Value+" "+v.Field.Unit))
	}
	return out
}

func selectedIDs(selected []domain.SelectedGoal) []int64 {
	ids := make([]int64, 0, len(selected))
	for _, s := range selected {
		ids = append(ids, s.Goal.ID)
	}
	return ids
}

// sameSet reports whether got and want hold the same ids, order-independent, and
// with no id appearing more than once in got (selection de-duplicates).
func sameSet(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[int64]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] != 1 {
			return false
		}
	}
	return true
}
