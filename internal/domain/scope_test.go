package domain_test

import (
	"slices"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// goalScope resolves the Scope viewer's page asks for over every Goal,
// failing the test on error.
func goalScope(t *testing.T, h *testsupport.Harness, viewer domain.Account, req domain.GoalScopeRequest) domain.GoalScope {
	t.Helper()
	dims, err := h.Service.ListDimensions(t.Context())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	goals, err := h.Service.ListGoals(t.Context())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	sc, err := h.Service.GoalScope(t.Context(), viewer, req, dims, goals)
	if err != nil {
		t.Fatalf("GoalScope(%s): %v", viewer.Email, err)
	}
	return sc
}

// keptTitles is the titles of the Goals sc keeps, sorted, failing the test
// on error.
func keptTitles(t *testing.T, h *testsupport.Harness, sc domain.GoalScope) []string {
	t.Helper()
	goals, err := h.Service.ListGoalsWithValues(t.Context())
	if err != nil {
		t.Fatalf("ListGoalsWithValues: %v", err)
	}
	var out []string
	for _, gv := range goals {
		if sc.Keeps(gv) {
			out = append(out, gv.Goal.Title)
		}
	}
	slices.Sort(out)
	return out
}

// An address naming no scope opens on the viewer's default: My org for
// someone with anyone under them, by each Goal's Owner, and Mine for
// everyone else (CONTEXT.md: Org).
func TestGoalScopeDefaultsToMyOrgForALeadAndMineForEveryoneElse(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	other := h.SignIn("other@example.com")
	h.SetManager(dev, lead)
	h.ActiveGoal(lead, "Lead's goal", "It matters.")
	h.ActiveGoal(dev, "Dev's goal", "It matters.")
	h.ActiveGoal(other, "Other's goal", "It matters.")

	sc := goalScope(t, h, lead, domain.GoalScopeRequest{})
	if !sc.Default || sc.Of == nil || sc.Of.ID != lead.ID || sc.Mine || !sc.OffersMine {
		t.Errorf("lead's default scope = %+v, want their own Org, offering My org", sc)
	}
	if got, want := keptTitles(t, h, sc), []string{"Dev's goal", "Lead's goal"}; !slices.Equal(got, want) {
		t.Errorf("lead's default scope keeps %q, want %q", got, want)
	}

	sc = goalScope(t, h, dev, domain.GoalScopeRequest{})
	if !sc.Default || sc.Of != nil || !sc.Mine || sc.OffersMine {
		t.Errorf("dev's default scope = %+v, want Mine, not offering My org", sc)
	}
	if got, want := keptTitles(t, h, sc), []string{"Dev's goal"}; !slices.Equal(got, want) {
		t.Errorf("dev's default scope keeps %q, want %q", got, want)
	}
}

// Mine is the Goals the viewer Owns or is a Delegate on, and not their
// reports' (CONTEXT.md: Delegate, Org).
func TestGoalScopeMineHoldsTheViewersDelegateGoals(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	other := h.SignIn("other@example.com")
	h.SetManager(dev, lead)
	h.ActiveGoal(lead, "Lead's goal", "It matters.")
	h.ActiveGoal(dev, "Dev's goal", "It matters.")
	delegated := h.ActiveGoal(other, "Delegated goal", "It matters.")
	h.ActiveGoal(other, "Other's goal", "It matters.")
	h.AddDelegate(other, lead, delegated.ID)

	sc := goalScope(t, h, lead, domain.GoalScopeRequest{Mine: true})
	if sc.Default || !sc.Mine || sc.Of != nil || sc.IsDefault() {
		t.Errorf("lead's Mine = %+v, want Mine, not their default", sc)
	}
	if got, want := keptTitles(t, h, sc), []string{"Delegated goal", "Lead's goal"}; !slices.Equal(got, want) {
		t.Errorf("lead's Mine keeps %q, want %q", got, want)
	}
	// My org is by Owner alone: the Delegate Goal isn't in it.
	if got, want := keptTitles(t, h, goalScope(t, h, lead, domain.GoalScopeRequest{})), []string{"Dev's goal", "Lead's goal"}; !slices.Equal(got, want) {
		t.Errorf("lead's My org keeps %q, want %q", got, want)
	}
}

// GoalScopeAmong, over the Accounts and Delegate Goals the Follow-ups page
// loader has already read, resolves the same Scope as GoalScope, which reads
// them itself (#592): for Mine, My org, a named Org and Everyone.
func TestGoalScopeAmongTheFollowUpsReadsIsGoalScope(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	ceo := h.SignIn("ceo@example.com")
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	other := h.SignIn("other@example.com")
	h.SetManager(lead, ceo)
	h.SetManager(dev, lead)
	h.SetManager(other, ceo)
	h.ActiveGoal(ceo, "CEO's goal", "It matters.")
	h.ActiveGoal(lead, "Lead's goal", "It matters.")
	h.ActiveGoal(dev, "Dev's goal", "It matters.")
	delegated := h.ActiveGoal(other, "Delegated goal", "It matters.")
	h.ActiveGoal(other, "Other's goal", "It matters.")
	h.AddDelegate(other, lead, delegated.ID)

	f, err := h.Service.FollowUps(t.Context(), lead.ID)
	if err != nil {
		t.Fatalf("FollowUps: %v", err)
	}
	if got, want := emailsOf(f.Accounts), []string{"ceo@example.com", "lead@example.com", "dev@example.com", "other@example.com"}; !slices.Equal(got, want) {
		t.Errorf("FollowUps' Accounts = %q, want %q", got, want)
	}
	dims, err := h.Service.ListDimensions(t.Context())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, c := range []struct {
		name string
		req  domain.GoalScopeRequest
		kept []string
	}{
		{"Mine", domain.GoalScopeRequest{Mine: true}, []string{"Delegated goal", "Lead's goal"}},
		{"My org", domain.GoalScopeRequest{}, []string{"Dev's goal", "Lead's goal"}},
		{"a named Org", domain.GoalScopeRequest{OrgOf: other.ID}, []string{"Delegated goal", "Other's goal"}},
		{"Everyone", domain.GoalScopeRequest{Everyone: true}, []string{"CEO's goal", "Delegated goal", "Dev's goal", "Lead's goal", "Other's goal"}},
	} {
		want := goalScope(t, h, lead, c.req)
		got := domain.GoalScopeAmong(lead, c.req, dims, f.Goals, f.Accounts, f.Delegated)
		if got.Default != want.Default || got.Mine != want.Mine || got.OffersMine != want.OffersMine ||
			(got.Of == nil) != (want.Of == nil) || (got.Of != nil && got.Of.ID != want.Of.ID) ||
			!slices.Equal(emailsOf(got.People), emailsOf(want.People)) || !slices.Equal(got.Values, want.Values) {
			t.Errorf("%s: GoalScopeAmong = %+v, want GoalScope's %+v", c.name, got, want)
		}
		if g, w := keptTitles(t, h, got), keptTitles(t, h, want); !slices.Equal(g, w) || !slices.Equal(g, c.kept) {
			t.Errorf("%s: GoalScopeAmong keeps %q and GoalScope %q, want %q", c.name, g, w, c.kept)
		}
	}
}

// emailsOf is the emails of people, in order.
func emailsOf(people []domain.Account) []string {
	out := make([]string, 0, len(people))
	for _, a := range people {
		out = append(out, a.Email)
	}
	return out
}

// The directory's cycles are stored as it gives them (ADR 0008), so both of
// the scope's walks stop at anyone they've reached: down an Org, and up from
// each Owner to the people whose Orgs the picker offers.
func TestGoalScopeWalksStopAtAManagerCycle(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	ana := h.SignInNamed("ana@example.com", "Ana")
	ben := h.SignInNamed("ben@example.com", "Ben")
	cy := h.SignInNamed("cy@example.com", "Cy")
	viewer := h.SignInNamed("viewer@example.com", "Viewer")
	// Ana manages Ben, who manages Cy, who manages Ana.
	h.SetManager(ben, ana)
	h.SetManager(cy, ben)
	h.SetManager(ana, cy)
	h.ActiveGoal(cy, "Cy's goal", "It matters.")
	h.ActiveGoal(viewer, "Viewer's goal", "It matters.")

	sc := goalScope(t, h, viewer, domain.GoalScopeRequest{OrgOf: ben.ID})
	if sc.Of == nil || sc.Of.ID != ben.ID || sc.Default {
		t.Fatalf("scope = %+v, want Ben's Org", sc)
	}
	if got, want := keptTitles(t, h, sc), []string{"Cy's goal"}; !slices.Equal(got, want) {
		t.Errorf("Ben's Org keeps %q, want %q", got, want)
	}
	if got, want := emailsOf(sc.People), []string{"ana@example.com", "ben@example.com", "cy@example.com"}; !slices.Equal(got, want) {
		t.Errorf("the picker offers %q, want %q", got, want)
	}
	if got, want := emailsOf(sc.Others()), []string{"ana@example.com", "ben@example.com", "cy@example.com"}; !slices.Equal(got, want) {
		t.Errorf("Others = %q, want %q", got, want)
	}
	// Viewer, outside the cycle and managing no one, defaults to Mine.
	if sc := goalScope(t, h, viewer, domain.GoalScopeRequest{}); !sc.Mine || sc.OffersMine {
		t.Errorf("viewer's default = %+v, want Mine", sc)
	}
}

// An Ownerless Goal's Departed Owner keeps their Manager, so the Goal stays
// in the Org its Owner was in (CONTEXT.md: Ownerless, Org).
func TestGoalScopeKeepsAnOwnerlessGoalInItsDepartedOwnersOrg(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	lead := h.SignIn("lead@example.com")
	gone := h.SignIn("gone@example.com")
	h.SetManager(gone, lead)
	h.ActiveGoal(gone, "Orphaned goal", "It matters.")
	h.MarkDeparted(admin, gone)

	if got, want := keptTitles(t, h, goalScope(t, h, lead, domain.GoalScopeRequest{})), []string{"Orphaned goal"}; !slices.Equal(got, want) {
		t.Errorf("lead's My org keeps %q, want %q", got, want)
	}
	if got := keptTitles(t, h, goalScope(t, h, admin, domain.GoalScopeRequest{OrgOf: admin.ID})); len(got) != 0 {
		t.Errorf("the admin's Org keeps %q, want nothing", got)
	}
}

// An address naming someone who doesn't exist opens on the viewer's default,
// as one naming no scope does.
func TestGoalScopeOfAnUnknownPersonIsTheViewersDefault(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	lead := h.SignIn("lead@example.com")
	dev := h.SignIn("dev@example.com")
	h.SetManager(dev, lead)

	if sc := goalScope(t, h, lead, domain.GoalScopeRequest{OrgOf: 9999}); !sc.Default || sc.Of == nil || sc.Of.ID != lead.ID {
		t.Errorf("lead's scope of no one = %+v, want their default, My org", sc)
	}
	if sc := goalScope(t, h, dev, domain.GoalScopeRequest{OrgOf: 9999}); !sc.Default || !sc.Mine {
		t.Errorf("dev's scope of no one = %+v, want their default, Mine", sc)
	}
}

// A person the directory gives as their own Manager isn't anyone's report:
// alone, no one has a Manager, so the page offers no Org and an address
// naming theirs opens on Mine; with a report, their Org is them and the
// report, and the walk up from their Goal stops at them (ADR 0008).
func TestGoalScopeOfASelfManagedPerson(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	boss := h.SignIn("boss@example.com")
	h.SetManager(boss, boss)
	h.ActiveGoal(boss, "Boss's goal", "It matters.")

	sc := goalScope(t, h, boss, domain.GoalScopeRequest{OrgOf: boss.ID})
	if !sc.Default || !sc.Mine || sc.OffersMine || sc.Offered() || len(sc.People) != 0 {
		t.Errorf("a self-managed person alone = %+v, want their default, Mine, offering no Org", sc)
	}

	dev := h.SignIn("dev@example.com")
	h.SetManager(dev, boss)
	h.ActiveGoal(dev, "Dev's goal", "It matters.")
	sc = goalScope(t, h, boss, domain.GoalScopeRequest{})
	if !sc.Default || sc.Of == nil || sc.Of.ID != boss.ID || !sc.OffersMine {
		t.Errorf("a self-managed lead's default = %+v, want My org", sc)
	}
	if got, want := keptTitles(t, h, sc), []string{"Boss's goal", "Dev's goal"}; !slices.Equal(got, want) {
		t.Errorf("a self-managed lead's My org keeps %q, want %q", got, want)
	}
	if got, want := emailsOf(goalScope(t, h, dev, domain.GoalScopeRequest{}).People), []string{"boss@example.com"}; !slices.Equal(got, want) {
		t.Errorf("dev's picker offers %q, want %q", got, want)
	}
}

// A page's values narrow its Goals: any of a Dimension's, and every
// Dimension's that has one. A value of a Retired Dimension is ignored, as no
// control could undo it, but a Retired value of an offered one still applies
// (CONTEXT.md: Retired).
func TestGoalScopeValuesOrWithinADimensionAndAndAcrossThem(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com")
	admin := h.SignIn("admin@example.com")
	team := h.CreateDimension(admin, "Team", "Platform", "Growth", "Legacy")
	region := h.CreateDimension(admin, "Region", "EMEA", "APAC")
	old := h.CreateDimension(admin, "Old", "Gone")
	platform, growth, legacy := team.Values[0], team.Values[1], team.Values[2]
	emea := region.Values[0]
	valued := func(title string, values ...domain.DimensionValue) {
		g := h.ActiveGoal(admin, title, "It matters.")
		for _, v := range values {
			h.AssignGoalValue(g, v)
		}
	}
	valued("Platform EMEA", platform, emea)
	valued("Growth", growth)
	valued("Legacy", legacy)
	valued("Platform, old", platform, old.Values[0])
	if err := h.Service.RetireDimensionValue(t.Context(), admin.ID, legacy.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	if err := h.Service.RetireDimension(t.Context(), admin.ID, old.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}

	for _, c := range []struct {
		name   string
		values []int64
		want   []string
	}{
		{"two of one Dimension", []int64{growth.ID, platform.ID}, []string{"Growth", "Platform EMEA", "Platform, old"}},
		{"one of each of two Dimensions", []int64{platform.ID, emea.ID}, []string{"Platform EMEA"}},
		{"a Retired value", []int64{legacy.ID}, []string{"Legacy"}},
		{"a Retired Dimension's value", []int64{old.Values[0].ID}, []string{"Growth", "Legacy", "Platform EMEA", "Platform, old"}},
		{"an unknown value", []int64{9999}, []string{"Growth", "Legacy", "Platform EMEA", "Platform, old"}},
	} {
		sc := goalScope(t, h, admin, domain.GoalScopeRequest{Everyone: true, Values: c.values})
		if got := keptTitles(t, h, sc); !slices.Equal(got, c.want) {
			t.Errorf("%s keeps %q, want %q", c.name, got, c.want)
		}
	}
	sc := goalScope(t, h, admin, domain.GoalScopeRequest{Everyone: true, Values: []int64{emea.ID, growth.ID, platform.ID}})
	var got []string
	for _, v := range sc.Values {
		got = append(got, v.Value)
	}
	if want := []string{"EMEA", "Growth", "Platform"}; !slices.Equal(got, want) {
		t.Errorf("Values = %q, want %q: the Dimensions' order, then the address's", got, want)
	}
}
