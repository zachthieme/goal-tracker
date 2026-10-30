package domain_test

import (
	"context"
	"errors"
	"maps"
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

// An Org rule selects by whether the Goal's Owner is in a named person's
// org: "is in the org of" the VP selects the Goals of the VP and everyone
// below; "of any of" two managers selects both parts; "is not" only Goals
// whose Owner is in none of them (CONTEXT.md: Report rule, Org).
func TestReportOrgRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	org := orgFixture(h)

	for _, c := range []struct {
		name string
		rule domain.ReportRule
		want []int64
	}{
		{"is in the org of the VP", orgRule(domain.RuleIs, org.vp), org.goals(org.vp, org.mia, org.max, org.ian, org.ida)},
		{"is in the org of any of Mia, Max", orgRule(domain.RuleIsAnyOf, org.mia, org.max), org.goals(org.mia, org.max, org.ian, org.ida)},
		{"is in the org of Mia", orgRule(domain.RuleIs, org.mia), org.goals(org.mia, org.ian)},
		{"is not in the org of Mia", orgRule(domain.RuleIsNot, org.mia), org.goals(org.vp, org.max, org.ida, org.out)},
		{"is not in the org of Mia or Max", orgRule(domain.RuleIsNot, org.mia, org.max), org.goals(org.vp, org.out)},
	} {
		def := h.SaveReportDefinition(org.vp, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{c.rule}})
		if got := selectByDefault(t, h, def); !sameSet(got, c.want) {
			t.Errorf("Owner %s selected %v, want %v", c.name, got, c.want)
		}
	}
}

// An Org rule selects by the Owner of record: a Delegate in the org
// doesn't bring a Goal in, and an Ownerless Goal stays in its Departed
// Owner's org (CONTEXT.md: Org, Ownerless).
func TestReportOrgRuleByOwnerOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	org := orgFixture(h)
	delegated := h.ActiveGoal(org.out, "Out's, delegated to Ian", "why")
	h.AddDelegate(org.out, org.ian, delegated.ID)
	if err := h.Service.MarkDeparted(context.Background(), org.vp.ID, org.ida.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	def := h.SaveReportDefinition(org.vp, domain.SaveReportDefinitionInput{Name: "VP", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{orgRule(domain.RuleIs, org.vp)}})
	if got, want := selectByDefault(t, h, def), org.goals(org.vp, org.mia, org.max, org.ian, org.ida); !sameSet(got, want) {
		t.Errorf("Owner is in the org of the VP selected %v, want %v", got, want)
	}
}

// An Org rule is a rule like any other: a Goal must meet it and every other
// rule, and Leave out still leaves a Goal out (CONTEXT.md: Report rule).
func TestReportOrgRuleCombines(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	org := orgFixture(h)
	proposed := h.CreateGoal(org.ian, "Ian's proposal", "why")

	def := h.SaveReportDefinition(org.vp, domain.SaveReportDefinitionInput{
		Name: "Mia's org, Active",
		Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{
			orgRule(domain.RuleIs, org.mia),
			namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive),
		},
		Exclude: org.goals(org.mia),
	})
	got := selectByDefault(t, h, def)
	if want := org.goals(org.ian); !sameSet(got, want) {
		t.Errorf("in the org of Mia, Active, leaving out Mia's, selected %v, want %v", got, want)
	}
	if slices.Contains(got, proposed.ID) {
		t.Errorf("selected the Proposed Goal %d despite the Lifecycle rule", proposed.ID)
	}
}

// An Org rule reads the Managers as they are at each draft: a Manager change
// moves a Goal in or out of the next, and the next publication lists it as
// entering or leaving, while the earlier publication keeps its snapshot
// (CONTEXT.md: Org; ADR 0008).
func TestReportOrgRuleFollowsAManagerChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	org := orgFixture(h)
	def := h.SaveReportDefinition(org.vp, domain.SaveReportDefinitionInput{Name: "Mia's org", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{orgRule(domain.RuleIs, org.mia)}})
	first := h.PublishReport(org.vp, def)
	if got, want := publishedGoalIDs(first), org.goals(org.mia, org.ian); !sameSet(got, want) {
		t.Fatalf("first publication has %v, want %v", got, want)
	}
	h.Clock.Advance(day)

	h.SetManager(org.ida, org.mia)
	h.SetManager(org.ian, org.max)

	if got, want := selectByDefault(t, h, def), org.goals(org.mia, org.ida); !sameSet(got, want) {
		t.Errorf("after the Manager changes, the draft selects %v, want %v", got, want)
	}
	second := h.PublishReport(org.vp, def)
	assertMembershipChanges(t, byGoal(t, h.PublicationFact(second, domain.ReportFactMembershipChanges).Value.(domain.ReportMembershipChangesFact).Changes), map[int64]string{
		org.goalOf[org.ida.ID].ID: "Added: " + domain.MembershipNowMatches,
		org.goalOf[org.ian.ID].ID: "Left: " + domain.MembershipNoLongerMatches,
	})
	kept, err := h.Service.GetPublication(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if got, want := publishedGoalIDs(kept), org.goals(org.mia, org.ian); !sameSet(got, want) {
		t.Errorf("the first publication now has %v, want its snapshot %v", got, want)
	}
}

// orgTestFixture is a three-level org: the VP manages Mia and Max, Mia manages
// Ian and Max manages Ida; Out has no Manager. Each owns one Active Goal.
type orgTestFixture struct {
	vp, mia, max, ian, ida, out domain.Account
	goalOf                      map[int64]domain.Goal
}

func orgFixture(h *testsupport.Harness) orgTestFixture {
	h.T.Helper()
	org := orgTestFixture{
		vp:     h.SignInNamed("vp@example.com", "Vera Patel"),
		mia:    h.SignInNamed("mia@example.com", "Mia Lund"),
		max:    h.SignInNamed("max@example.com", "Max Ruiz"),
		ian:    h.SignInNamed("ian@example.com", "Ian Cole"),
		ida:    h.SignInNamed("ida@example.com", "Ida Berg"),
		out:    h.SignInNamed("out@example.com", "Oona Hart"),
		goalOf: map[int64]domain.Goal{},
	}
	h.SetManager(org.mia, org.vp)
	h.SetManager(org.max, org.vp)
	h.SetManager(org.ian, org.mia)
	h.SetManager(org.ida, org.max)
	for _, a := range []domain.Account{org.vp, org.mia, org.max, org.ian, org.ida, org.out} {
		org.goalOf[a.ID] = h.ActiveGoal(a, a.Name+"'s goal", "why")
	}
	return org
}

// goals are the ids of the Goals the owners own in the org.
func (org orgTestFixture) goals(owners ...domain.Account) []int64 {
	var out []int64
	for _, o := range owners {
		out = append(out, org.goalOf[o.ID].ID)
	}
	return out
}

// orgRule is the rule "Owner op in the org of people".
func orgRule(op string, people ...domain.Account) domain.ReportRule {
	rule := domain.ReportRule{Attribute: domain.RuleOrg, Op: op}
	for _, p := range people {
		rule.Values = append(rule.Values, strconv.FormatInt(p.ID, 10))
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
// to Leave out, which may list one the rules don't select (CONTEXT.md: Report
// rule).
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
	samIdentity := h.CreateGoal(sam, "Sam, Identity", "why")
	h.AssignGoalValue(bossPlatform, platform)
	h.AssignGoalValue(bossIdentity, identity)
	h.AssignGoalValue(bossWeb, web)
	h.AssignGoalValue(samPlatform, platform)
	h.AssignGoalValue(samWeb, web)
	h.AssignGoalValue(samIdentity, identity)
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
		Include: []int64{samWeb.ID},
		Exclude: []int64{bossIdentity.ID, samIdentity.ID},
	})
	if got, want := selectByDefault(t, h, overridden), []int64{bossPlatform.ID, samWeb.ID}; !sameSet(got, want) {
		t.Errorf("with Also include and Leave out selected %v, want %v", got, want)
	}
}

// A rules definition's selection says how each Goal came in: matched by the
// rules, even if Also include lists it too, or added by Also include alone.
// It gives the Leave out Goals the rules would otherwise have selected, not
// one they don't match nor one finished before the baseline. A picked
// definition's Goals are all picked.
func TestReportSelectionSaysHowEachGoalCameIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	finished := h.ActiveGoal(boss, "Boss, finished long ago", "why")
	moveLifecycle(t, h, boss, finished, domain.LifecycleDone)
	settle(h)
	matched := h.ActiveGoal(boss, "Boss, matched", "why")
	listedAndMatched := h.ActiveGoal(boss, "Boss, also listed", "why")
	added := h.ActiveGoal(sam, "Sam, added", "why")
	leftOut := h.ActiveGoal(boss, "Boss, left out", "why")
	unmatched := h.ActiveGoal(sam, "Sam, never matched", "why")

	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "Overridden",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{ownerRule(domain.RuleIs, boss)},
		Include: []int64{listedAndMatched.ID, added.ID},
		Exclude: []int64{leftOut.ID, unmatched.ID, finished.ID},
	})
	sel, err := h.Service.SelectGoalsAgainst(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("SelectGoalsAgainst: %v", err)
	}
	via := map[int64]string{}
	for _, sg := range sel.Goals {
		via[sg.Goal.ID] = sg.Via
	}
	want := map[int64]string{
		matched.ID:          domain.SelectedByRules,
		listedAndMatched.ID: domain.SelectedByRules,
		added.ID:            domain.SelectedByAlsoInclude,
	}
	if !maps.Equal(via, want) {
		t.Errorf("selected %v, want %v", via, want)
	}
	var leftOutIDs []int64
	for _, g := range sel.LeftOut {
		leftOutIDs = append(leftOutIDs, g.ID)
	}
	if want := []int64{leftOut.ID}; !slices.Equal(leftOutIDs, want) {
		t.Errorf("left out %v, want %v", leftOutIDs, want)
	}

	picked := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "Picked", Mode: domain.ReportModePicked, Picked: []int64{matched.ID, added.ID},
	})
	if sel, err = h.Service.SelectGoalsAgainst(ctx, picked, time.Time{}); err != nil {
		t.Fatalf("SelectGoalsAgainst: %v", err)
	}
	for _, sg := range sel.Goals {
		if sg.Via != domain.SelectedByPicking {
			t.Errorf("picked %q came in by %q, want %q", sg.Goal.Title, sg.Via, domain.SelectedByPicking)
		}
	}
	if len(sel.Goals) != 2 || len(sel.LeftOut) != 0 {
		t.Errorf("picked selected %d Goals and left out %d, want 2 and 0", len(sel.Goals), len(sel.LeftOut))
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
// rules[i] counts from zero (CONTEXT.md: Report Definition, Report rule). Each
// message is in a person's words, naming what the builder shows (the report,
// a rule, a Goal) rather than "Report Definition", and the ones the builder
// can provoke say what to do. The table walks every refusal
// validateReportDefinition and validateReportRule give, the rule clashes,
// Also include and Leave out overlapping, and the Field refusals included
// (#671).
func TestSaveReportDefinitionRefusals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ann := h.SignInNamed("ann@example.com", "Ann Avery")
	bob := h.SignInNamed("bob@example.com", "Bob Byrne")
	team := h.CreateDimension(boss, "Team", "Platform", "Payments")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	platform, payments, growth := team.Values[0], team.Values[1], pillar.Values[0]
	g := h.CreateGoal(boss, "Grow revenue", "why")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	if err := h.Service.RetireField(context.Background(), boss.ID, legacy.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	topLevel := func(op string) domain.ReportRule { return domain.ReportRule{Attribute: domain.RuleTopLevel, Op: op} }
	isPlatform := dimensionRule(team, domain.RuleIs, platform)
	gone := "a Goal in this list no longer exists; remove it and save again"
	rules := func(rs ...domain.ReportRule) domain.SaveReportDefinitionInput {
		return domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: rs}
	}

	for _, c := range []struct {
		name    string
		in      domain.SaveReportDefinitionInput
		want    string
		message string
	}{
		{"blank name", domain.SaveReportDefinitionInput{Name: " ", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}}, "name", "give the report a name"},
		{"unknown mode", domain.SaveReportDefinitionInput{Name: "MBR", Mode: "graph", Picked: []int64{g.ID}}, "mode", `choose Goals matching rules or Goals I pick, not "graph"`},
		{"rules definition with no rules", rules(), "rules", "add a rule: choose what it tests, such as a Team, and a value"},
		{"rule with no values", rules(isPlatform, dimensionRule(team, domain.RuleIsAnyOf)), "rules[1]", "choose a value for this rule"},
		{"is with two values", rules(domain.ReportRule{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive, domain.LifecycleOnHold}}), "rules[0]", `"is" takes exactly one value`},
		{"unknown operator", rules(domain.ReportRule{Attribute: domain.RuleLifecycle, Op: "contains", Values: []string{domain.LifecycleActive}}), "rules[0]", `"contains" is not a rule operator`},
		{"unknown attribute", rules(domain.ReportRule{Attribute: "field", Op: domain.RuleIs, Values: []string{"1"}}), "rules[0]", `"field" is not a rule attribute`},
		{"unknown Dimension", rules(domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: 9999, Op: domain.RuleIs, Values: []string{strconv.FormatInt(platform.ID, 10)}}), "rules[0]", "Dimension 9999 does not exist"},
		{"unknown Dimension value", rules(domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: team.ID, Op: domain.RuleIs, Values: []string{"9999"}}), "rules[0]", `"9999" is not a value of Team`},
		{"value of another Dimension", rules(isPlatform, dimensionRule(team, domain.RuleIsNot, growth)), "rules[1]", `"` + strconv.FormatInt(growth.ID, 10) + `" is not a value of Team`},
		{"unknown Account", rules(domain.ReportRule{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{"9999"}}), "rules[0]", `"9999" is not an Account`},
		{"unknown Org person", rules(domain.ReportRule{Attribute: domain.RuleOrg, Op: domain.RuleIsAnyOf, Values: []string{"9999"}}), "rules[0]", `"9999" is not an Account`},
		{"unknown Lifecycle", rules(namedRule(domain.RuleLifecycle, domain.RuleIs, "Paused")), "rules[0]", `"Paused" is not a Lifecycle`},
		{"unknown Health", rules(namedRule(domain.RuleHealth, domain.RuleIsNot, "Blue")), "rules[0]", `"Blue" is not a Health`},
		{"Top-level with values", rules(domain.ReportRule{Attribute: domain.RuleTopLevel, Op: domain.RuleIs, Values: []string{"yes"}}), "rules[0]", `a Top-level rule is "is Top-level" or "is not Top-level"`},
		{"Top-level is any of", rules(domain.ReportRule{Attribute: domain.RuleTopLevel, Op: domain.RuleIsAnyOf}), "rules[0]", `a Top-level rule is "is Top-level" or "is not Top-level"`},
		{"unknown Also include", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Include: []int64{9999}}, "include", gone},
		{"unknown Leave out", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Exclude: []int64{9999}}, "exclude", gone},
		{"Goal in both Also include and Leave out", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Include: []int64{g.ID}, Exclude: []int64{g.ID}}, "exclude", "Grow revenue is in Also include too: keep it in one list"},
		{"rules definition with a picked list", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{isPlatform}, Picked: []int64{g.ID}}, "picked", "a report of Goals matching rules has no picked Goals"},
		{"picked definition with no Goals", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked}, "picked", "pick at least one Goal"},
		{"unknown picked Goal", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID, 9999}}, "picked", gone},
		{"picked definition with rules", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Rules: []domain.ReportRule{isPlatform}}, "rules", "a report of Goals I pick has no rules"},
		{"picked definition with Also include", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Include: []int64{g.ID}}, "include", "a report of Goals I pick has nothing to Also include"},
		{"picked definition with Leave out", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, Exclude: []int64{g.ID}}, "exclude", "a report of Goals I pick has nothing to Leave out"},
		{"Team rules that clash", rules(isPlatform, dimensionRule(team, domain.RuleIs, payments)), "rules[1]",
			`A Goal has one Team, so none can be both Platform and Payments. To include either, put them in one rule with "is any of".`},
		{"Owner rules that clash", rules(ownerRule(domain.RuleIs, ann), ownerRule(domain.RuleIs, bob)), "rules[1]",
			`A Goal has one Owner, so none can be both Ann Avery and Bob Byrne. To include either, put them in one rule with "is any of".`},
		{"Health rules that clash", rules(namedRule(domain.RuleHealth, domain.RuleIs, domain.HealthRed), namedRule(domain.RuleHealth, domain.RuleIs, domain.HealthGreen)), "rules[1]",
			`A Goal has one Health, so none can be both Red and Green. To include either, put them in one rule with "is any of".`},
		{"Top-level rules that clash", rules(topLevel(domain.RuleIs), topLevel(domain.RuleIsNot)), "rules[1]", "A Goal can't be both Top-level and not Top-level."},
		{"unknown Field", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{9999}}, "fields", "field 9999 does not exist"},
		{"Retired Field", domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{legacy.ID}}, "fields", "Legacy code is retired, so a Report can't be set to show it"},
	} {
		_, err := h.Service.SaveReportDefinition(context.Background(), boss.ID, c.in)
		problems := domain.InputErrors(err)
		if !errors.Is(err, domain.ErrValidation) || len(problems) != 1 || problems[0].Input != c.want {
			t.Errorf("%s: err = %v (inputs %v), want one InputError naming %s", c.name, err, inputNames(problems), c.want)
			continue
		}
		if got := problems[0].Message; got != c.message {
			t.Errorf("%s: message = %q, want %q", c.name, got, c.message)
		}
		for _, term := range []string{"Report Definition", "rules Report", "picked Report"} {
			if strings.Contains(problems[0].Message, term) {
				t.Errorf("%s: message %q says %q, a term a person doesn't see", c.name, problems[0].Message, term)
			}
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

// Two rules no Goal can meet both are refused beside the later one: "is" or
// "is any of" with no value in common on an attribute a Goal holds one value
// of (Owner, Health, a one-value Dimension), and "is" with "is not" Top-level.
// Every other pair saves: overlapping values, a several-value Dimension, two
// Dimensions, Lifecycle, the Owner's org, and an "is not" (CONTEXT.md: Report
// rule).
func TestSaveReportDefinitionRefusesRulesNoGoalCanMeetBoth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Bo Boss")
	ann := h.SignInNamed("ann@example.com", "Ann Avery")
	bob := h.SignInNamed("bob@example.com", "Bob Byrne")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth", "Payments")
	platform, growth, payments := team.Values[0], team.Values[1], team.Values[2]
	pillar := h.CreateDimension(boss, "Pillar", "Revenue")
	customer := h.CreateSeveralValuesDimension(boss, "Customer", "Acme", "Globex")
	acme, globex := customer.Values[0], customer.Values[1]
	topLevel := func(op string) domain.ReportRule { return domain.ReportRule{Attribute: domain.RuleTopLevel, Op: op} }
	orgRule := func(op string, accounts ...domain.Account) domain.ReportRule {
		rule := ownerRule(op, accounts...)
		rule.Attribute = domain.RuleOrg
		return rule
	}

	for _, c := range []struct {
		name    string
		rules   []domain.ReportRule
		message string
	}{
		{"Team is one and is any of others", []domain.ReportRule{dimensionRule(team, domain.RuleIs, platform), dimensionRule(team, domain.RuleIsAnyOf, growth, payments)},
			`A Goal has one Team, so none can be both Platform and Growth or Payments. To include either, put them in one rule with "is any of".`},
		{"Owner is one and is another", []domain.ReportRule{ownerRule(domain.RuleIs, ann), ownerRule(domain.RuleIs, bob)},
			`A Goal has one Owner, so none can be both Ann Avery and Bob Byrne. To include either, put them in one rule with "is any of".`},
		{"Health is Red and is Green", []domain.ReportRule{namedRule(domain.RuleHealth, domain.RuleIs, domain.HealthRed), namedRule(domain.RuleHealth, domain.RuleIs, domain.HealthGreen)},
			`A Goal has one Health, so none can be both Red and Green. To include either, put them in one rule with "is any of".`},
		{"is Top-level and is not", []domain.ReportRule{topLevel(domain.RuleIs), topLevel(domain.RuleIsNot)},
			"A Goal can't be both Top-level and not Top-level."},
	} {
		_, err := h.Service.SaveReportDefinition(context.Background(), boss.ID, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: c.rules})
		problems := domain.InputErrors(err)
		if !errors.Is(err, domain.ErrValidation) || len(problems) != 1 || problems[0].Input != "rules[1]" {
			t.Errorf("%s: err = %v (inputs %v), want one InputError naming rules[1]", c.name, err, inputNames(problems))
			continue
		}
		if got := problems[0].Message; got != c.message {
			t.Errorf("%s: message = %q, want %q", c.name, got, c.message)
		}
	}
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil {
		t.Fatalf("ListReportDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("refused definitions were saved: %+v", defs)
	}

	for _, c := range []struct {
		name  string
		rules []domain.ReportRule
	}{
		{"overlapping Team values", []domain.ReportRule{dimensionRule(team, domain.RuleIsAnyOf, platform, growth), dimensionRule(team, domain.RuleIsAnyOf, growth, payments)}},
		{"a repeated rule", []domain.ReportRule{dimensionRule(team, domain.RuleIs, platform), dimensionRule(team, domain.RuleIs, platform)}},
		{"a several-value Dimension", []domain.ReportRule{dimensionRule(customer, domain.RuleIs, acme), dimensionRule(customer, domain.RuleIs, globex)}},
		{"two one-value Dimensions", []domain.ReportRule{dimensionRule(team, domain.RuleIs, platform), dimensionRule(pillar, domain.RuleIs, pillar.Values[0])}},
		{"Lifecycle is Active and is Done", []domain.ReportRule{namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleActive), namedRule(domain.RuleLifecycle, domain.RuleIs, domain.LifecycleDone)}},
		{"two orgs of different people", []domain.ReportRule{orgRule(domain.RuleIsAnyOf, ann), orgRule(domain.RuleIsAnyOf, bob)}},
		{"is any of and is not", []domain.ReportRule{dimensionRule(team, domain.RuleIsAnyOf, platform, growth), dimensionRule(team, domain.RuleIsNot, growth)}},
		{"Owner is and is not", []domain.ReportRule{ownerRule(domain.RuleIs, ann), ownerRule(domain.RuleIsNot, bob)}},
		{"Top-level twice", []domain.ReportRule{topLevel(domain.RuleIs), topLevel(domain.RuleIs)}},
	} {
		if _, err := h.Service.SaveReportDefinition(context.Background(), boss.ID, domain.SaveReportDefinitionInput{Name: c.name, Mode: domain.ReportModeRules, Rules: c.rules}); err != nil {
			t.Errorf("%s: SaveReportDefinition = %v, want it saved", c.name, err)
		}
	}
}

// Each rule is refused for at most one clash, with the first earlier rule
// it can't hold beside, and a rule already refused clashes with none.
func TestSaveReportDefinitionRefusesEachClashOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Growth", "Payments")
	platform, growth, payments := team.Values[0], team.Values[1], team.Values[2]

	_, err := h.Service.SaveReportDefinition(context.Background(), boss.ID, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		dimensionRule(team, domain.RuleIs, platform),
		dimensionRule(team, domain.RuleIs, growth, payments),
		dimensionRule(team, domain.RuleIs, growth),
		dimensionRule(team, domain.RuleIs, payments),
	}})
	problems := domain.InputErrors(err)
	got := inputNames(problems)
	if want := []string{"rules[1]", "rules[2]", "rules[3]"}; !slices.Equal(got, want) {
		t.Fatalf("refused inputs = %v, want %v", got, want)
	}
	if problems[0].Message != `"is" takes exactly one value` {
		t.Errorf("rules[1] message = %q, want only its own refusal", problems[0].Message)
	}
	if want := "none can be both Platform and Growth."; !strings.Contains(problems[1].Message, want) {
		t.Errorf("rules[2] message = %q, want it against rules[0]: %q", problems[1].Message, want)
	}
	if want := "none can be both Platform and Payments."; !strings.Contains(problems[2].Message, want) {
		t.Errorf("rules[3] message = %q, want it against the first earlier rule only: %q", problems[2].Message, want)
	}
}

// The Org rule is stored as "org"; "chain", its name before the rename, is no
// rule attribute and saving one is refused like any unknown attribute
// (CONTEXT.md: Org).
func TestSaveReportDefinitionRefusesAChainRule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	org := orgFixture(h)
	vp := strconv.FormatInt(org.vp.ID, 10)

	_, err := h.Service.SaveReportDefinition(context.Background(), org.vp.ID, domain.SaveReportDefinitionInput{
		Name:  "VP",
		Mode:  domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: "chain", Op: domain.RuleIs, Values: []string{vp}}},
	})
	problems := domain.InputErrors(err)
	if len(problems) != 1 || problems[0].Input != "rules[0]" || problems[0].Message != `"chain" is not a rule attribute` {
		t.Errorf("err = %v, want one InputError on rules[0] saying \"chain\" is not a rule attribute", err)
	}
	if domain.RuleOrg != "org" {
		t.Errorf("RuleOrg = %q, want it stored as \"org\"", domain.RuleOrg)
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

// Editing a Report Definition replaces its name, introduction, scope and
// Fields; its draft then selects only by the new scope. A publication it
// already had stays frozen with the name and Goals it was published with
// (ticket #151).
func TestUpdateReportDefinitionChangesOnlyTheDraft(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Web")
	platform, web := team.Values[0], team.Values[1]
	onPlatform := h.ActiveGoal(boss, "Platform goal", "why")
	onWeb := h.ActiveGoal(boss, "Web goal", "why")
	picked := h.ActiveGoal(boss, "Picked goal", "why")
	h.AssignGoalValue(onPlatform, platform)
	h.AssignGoalValue(onWeb, web)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name:    "Platform MBR",
		Mode:    domain.ReportModeRules,
		Rules:   []domain.ReportRule{dimensionRule(team, domain.RuleIs, platform)},
		Include: []int64{picked.ID},
	})
	pub := h.PublishReport(boss, def)

	updated, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{
		Name:         " Web MBR ",
		Introduction: "Web only now.",
		Mode:         domain.ReportModeRules,
		Rules:        []domain.ReportRule{dimensionRule(team, domain.RuleIs, web)},
	})
	if err != nil {
		t.Fatalf("UpdateReportDefinition: %v", err)
	}
	if updated.ID != def.ID || updated.Name != "Web MBR" || updated.Introduction != "Web only now." {
		t.Errorf("updated = %d %q %q, want %d %q %q", updated.ID, updated.Name, updated.Introduction, def.ID, "Web MBR", "Web only now.")
	}
	if len(updated.Include) != 0 || len(updated.Rules) != 1 {
		t.Errorf("updated scope = rules %+v, include %v; want the one new rule and no Also include", updated.Rules, updated.Include)
	}
	reloaded, err := h.Service.GetReportDefinition(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	if got, want := selectByDefault(t, h, reloaded), []int64{onWeb.ID}; !sameSet(got, want) {
		t.Errorf("the edited draft selected %v, want %v", got, want)
	}

	frozen, err := h.Service.GetPublication(ctx, pub.ID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if frozen.Name != "Platform MBR" {
		t.Errorf("publication's name = %q, want the old %q", frozen.Name, "Platform MBR")
	}
	if got, want := publishedGoalIDs(frozen), []int64{onPlatform.ID, picked.ID}; !sameSet(got, want) {
		t.Errorf("publication's Goals = %v, want the old %v", got, want)
	}
}

// Only a Report Definition's creator, a Report Delegate or an Admin may edit
// it; anyone else is refused with ErrNotAuthorized and the definition stays as
// it was. Anyone signed in may still save one of their own (tickets #151,
// #434).
func TestUpdateReportDefinitionOnlyByItsCreatorADelegateOrAnAdmin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	alice := h.SignIn("alice@example.com")
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(alice, "Grow revenue", "why")
	in := func(name string) domain.SaveReportDefinitionInput {
		return domain.SaveReportDefinitionInput{Name: name, Mode: domain.ReportModePicked, Picked: []int64{g.ID}}
	}
	def := h.SaveReportDefinition(alice, in("Alice's MBR"))

	if def.CreatedBy != alice.ID {
		t.Errorf("CreatedBy = %d, want Alice, %d", def.CreatedBy, alice.ID)
	}
	defs, err := h.Service.ListReportDefinitions(ctx)
	if err != nil {
		t.Fatalf("ListReportDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].CreatedBy != alice.ID {
		t.Errorf("listed definitions = %+v, want Alice's, created by %d", defs, alice.ID)
	}
	dee := h.SignIn("dee@example.com")
	h.AddReportDelegate(alice, dee, def)
	for _, c := range []struct {
		who  string
		as   domain.Account
		want bool
	}{
		{"the creator", alice, true},
		{"an Admin", boss, true},
		{"a Report Delegate", dee, true},
		{"anyone else", sam, false},
	} {
		if p, err := h.Service.ReportPermissions(ctx, c.as.ID, def); err != nil || p.Manage != c.want {
			t.Errorf("ReportPermissions(%s).Manage = %v (err %v), want %v", c.who, p.Manage, err, c.want)
		}
		if err := h.Service.RequireReportEditor(ctx, c.as.ID, def); (err == nil) != c.want {
			t.Errorf("RequireReportEditor(%s) = %v, want refused %v", c.who, err, !c.want)
		}
	}

	if _, err := h.Service.UpdateReportDefinition(ctx, sam.ID, def.ID, in("Sam's now")); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("Sam editing Alice's definition: err = %v, want ErrNotAuthorized", err)
	}
	if got, _ := h.Service.GetReportDefinition(ctx, def.ID); got.Name != "Alice's MBR" {
		t.Errorf("after Sam's refused edit the name = %q, want %q", got.Name, "Alice's MBR")
	}
	if _, err := h.Service.UpdateReportDefinition(ctx, alice.ID, def.ID, in("Alice's edit")); err != nil {
		t.Errorf("Alice editing her own definition: %v", err)
	}
	updated, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, in("Admin's edit"))
	if err != nil {
		t.Fatalf("an Admin editing Alice's definition: %v", err)
	}
	if updated.Name != "Admin's edit" || updated.CreatedBy != alice.ID {
		t.Errorf("after the Admin's edit: %q created by %d, want %q still created by Alice", updated.Name, updated.CreatedBy, "Admin's edit")
	}
}

// Editing refuses what saving refuses, each with an InputError naming its
// input, and keeps the definition as it was; an unknown definition is
// ErrNotFound (ticket #151).
func TestUpdateReportDefinitionRefusals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	g := h.CreateGoal(boss, "Grow revenue", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})

	_, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{Name: " ", Mode: domain.ReportModeRules})
	if got := inputNames(domain.InputErrors(err)); !errors.Is(err, domain.ErrValidation) || !sameStrings(got, []string{"name", "rules"}) {
		t.Errorf("blank name and no rules: err = %v (inputs %v), want InputErrors naming name and rules", err, got)
	}
	got, err := h.Service.GetReportDefinition(ctx, def.ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	if got.Name != "MBR" || got.Mode != domain.ReportModePicked || !slices.Equal(got.Picked, []int64{g.ID}) {
		t.Errorf("after a refused edit: %q %s %v, want it as it was", got.Name, got.Mode, got.Picked)
	}

	if _, err := h.Service.UpdateReportDefinition(ctx, boss.ID, 9999, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editing an unknown definition: err = %v, want ErrNotFound", err)
	}
}

// Editing a Report Definition keeps a Field it already shows after that Field
// is Retired (ADR 0005: saved Report Definitions that reference it keep
// working), but still refuses adding a different Retired Field (ticket #164).
func TestUpdateReportDefinitionKeepsARetiredFieldItShows(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ctx := context.Background()
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	legacy := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	g := h.CreateGoal(boss, "Grow revenue", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{budget.ID}})
	for _, f := range []domain.Field{budget, legacy} {
		if err := h.Service.RetireField(ctx, boss.ID, f.ID); err != nil {
			t.Fatalf("RetireField %s: %v", f.Name, err)
		}
	}

	_, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{budget.ID, legacy.ID},
	})
	if got := inputNames(domain.InputErrors(err)); !errors.Is(err, domain.ErrValidation) || !sameStrings(got, []string{domain.ReportInputFields}) {
		t.Errorf("adding the Retired Legacy code: err = %v (inputs %v), want an InputError naming fields", err, got)
	}

	updated, err := h.Service.UpdateReportDefinition(ctx, boss.ID, def.ID, domain.SaveReportDefinitionInput{
		Name: "MBR renamed", Mode: domain.ReportModePicked, Picked: []int64{g.ID}, FieldIDs: []int64{budget.ID},
	})
	if err != nil {
		t.Fatalf("keeping the Retired Budget: %v", err)
	}
	if !slices.Equal(updated.FieldIDs, []int64{budget.ID}) {
		t.Errorf("after the edit FieldIDs = %v, want [%d]", updated.FieldIDs, budget.ID)
	}
}

// The Report builder offers an Org rule on the Managers of someone other than
// themselves: a self-reference, as a directory can give (ADR 0008), puts no one
// under its person, so isn't one.
func TestReportRuleManagersLeavesOutSelfReferences(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	vp := h.SignIn("vp@example.com")
	vera := h.SignIn("vera@example.com")
	mia := h.SignIn("mia@example.com")
	res := h.SyncDirectory(domain.DirectoryPerson{Email: vp.Email, ManagerEmail: vp.Email})
	if got, want := testsupport.CycleEmails(res.Cycles), [][]string{{"vp@example.com"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("the sync's Cycles = %q, want %q", got, want)
	}

	got, err := h.Service.ReportRuleManagers(ctx)
	if err != nil {
		t.Fatalf("ReportRuleManagers: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("with only a self-reference ReportRuleManagers = %v, want none", got)
	}

	h.SyncDirectory(domain.DirectoryPerson{Email: mia.Email, ManagerEmail: vera.Email})
	got, err = h.Service.ReportRuleManagers(ctx)
	if err != nil {
		t.Fatalf("ReportRuleManagers: %v", err)
	}
	var emails []string
	for _, a := range got {
		emails = append(emails, a.Email)
	}
	if want := []string{"vera@example.com"}; !slices.Equal(emails, want) {
		t.Errorf("ReportRuleManagers = %v, want %v", emails, want)
	}
}
