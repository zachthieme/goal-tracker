package web_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/seed"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// TestSmokeSignInCreateAndViewGoal is the one end-to-end HTTP smoke test: a
// person signs in, creates a Proposed Goal, and sees it rendered on the Goal
// list and on its own page.
func TestSmokeSignInCreateAndViewGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	// Sign in — the account is created on first sign-in.
	resp := postForm(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after sign-in: status %d", resp.StatusCode)
	}
	who := findByTestID(t, parsePage(t, readBody(t, resp)), "current-user")
	if !strings.HasPrefix(textOf(who), "Signed in as boss") || !goalShows(who, "boss@example.com", "boss") {
		t.Errorf("goals page does not show the signed-in user: %s", renderNode(who))
	}

	// Create a Proposed Goal.
	resp = postForm(t, client, ts.URL+"/goals", url.Values{
		"title":          {"Cut checkout latency"},
		"why_it_matters": {"Shoppers abandon slow carts."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after create goal: status %d", resp.StatusCode)
	}
	link := findGoalLink(t, parsePage(t, readBody(t, resp)))
	if got := textOf(link); got != "Cut checkout latency" {
		t.Errorf("Goal list's first Goal link reads %q, want the new Goal", got)
	}

	// Find the Goal's own page and confirm it renders the full Goal.
	goalPath := attrOf(link, "href")
	resp, err = ctxGet(t, client, ts.URL+goalPath)
	if err != nil {
		t.Fatalf("GET %s: %v", goalPath, err)
	}
	defer func() { _ = resp.Body.Close() }()
	goalPage := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("goal page status %d", resp.StatusCode)
	}
	for testID, want := range map[string]string{
		"goal-title":          "Cut checkout latency",
		"goal-why-it-matters": "Shoppers abandon slow carts.",
		"goal-lifecycle":      "Proposed",
	} {
		if got := textOf(findByTestID(t, goalPage, testID)); got != want {
			t.Errorf("goal page's %s reads %q, want %q", testID, got, want)
		}
	}
	if owner := findByTestID(t, goalPage, "goal-owner"); !goalShows(owner, "boss@example.com", "boss") {
		t.Errorf("goal page's Owner isn't boss@example.com: %s", renderNode(owner))
	}
}

// findGoalLink returns the first link on the page to a Goal's own page,
// /goals/<id>.
func findGoalLink(t *testing.T, root *nethtml.Node) *nethtml.Node {
	t.Helper()
	goalPath := regexp.MustCompile(`^/goals/\d+$`)
	links := findAll(root, func(n *nethtml.Node) bool { return n.Data == "a" && goalPath.MatchString(attrOf(n, "href")) })
	if len(links) == 0 {
		t.Fatalf("no goal link in page:\n%s", renderNode(root))
	}
	return links[0]
}

// TestSmokeDefineAndActivateGoal is the define-and-activate HTTP smoke test: an
// Owner defines a Proposed Goal as Dated with a Metric and a cadence, adds a
// Milestone, revises the Why it matters, and activates it — then sees it
// rendered Active with everything it filled in.
func TestSmokeDefineAndActivateGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")

	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	postFormStatus(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})

	resp := postForm(t, client, ts.URL+"/goals", url.Values{
		"title":          {"Cut checkout latency"},
		"why_it_matters": {"Shoppers abandon slow carts."},
	})
	defer func() { _ = resp.Body.Close() }()
	goalPath := attrOf(findGoalLink(t, parsePage(t, readBody(t, resp))), "href")
	goalURL := ts.URL + goalPath

	postFormStatus(t, client, goalURL+"/define", url.Values{
		"title":                  {"Cut checkout latency"},
		"why_it_matters":         {"Shoppers abandon slow carts."},
		"kind":                   {domain.GoalDated},
		"delivery_date":          {"2026-06-15"},
		"cadence":                {"14"},
		"metrics[0].name":        {"p95 checkout latency"},
		"metrics[0].unit":        {"ms"},
		"metrics[0].direction":   {"down"},
		"metrics[0].baseline":    {"1200"},
		"metrics[0].target":      {"400"},
		"metrics[0].target_date": {"2026-06-15"},
	})
	postFormStatus(t, client, goalURL+"/milestones", url.Values{
		"name":        {"Beta cut"},
		"target_date": {"2026-03-16"},
	})
	postFormStatus(t, client, goalURL+"/why-it-matters", url.Values{"why_it_matters": {"Faster checkout lifts conversion."}})

	resp = postForm(t, client, goalURL+"/activate", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after activate: status %d", resp.StatusCode)
	}
	page := parsePage(t, readBody(t, resp))
	for _, tc := range []struct{ testID, want string }{
		{"goal-lifecycle", "Active"},
		{"goal-milestones", "Beta cut"},
		{"goal-metrics", "p95 checkout latency"},
		{"goal-cadence", "every 14 days"},
		{"goal-why-it-matters", "Faster checkout lifts conversion."},
		{"goal-history", "Shoppers abandon slow carts."}, // the original Why it matters, kept as a revision
	} {
		if got := textOf(findByTestID(t, page, tc.testID)); !strings.Contains(got, tc.want) {
			t.Errorf("activated goal page's %s reads %q, missing %q", tc.testID, got, tc.want)
		}
	}
}

// TestActivateProposedGoalReportsGateFailures is the one HTTP check that the
// activation gate's messages reach the user: activating a bare Proposed Goal is
// rejected with a 422 that names what is missing.
func TestActivateProposedGoalReportsGateFailures(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	postFormStatus(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})

	resp := postForm(t, client, ts.URL+"/goals", url.Values{
		"title":          {"Bare goal"},
		"why_it_matters": {"Some reason."},
	})
	defer func() { _ = resp.Body.Close() }()
	goalPath := attrOf(findGoalLink(t, parsePage(t, readBody(t, resp))), "href")

	resp = postForm(t, client, ts.URL+goalPath+"/activate", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("activate status = %d, want 422", resp.StatusCode)
	}
	// The refusal page says why.
	if reason := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "refused-reason")); !strings.Contains(reason, "Dated") {
		t.Errorf("rejection should explain the missing Dated/Ongoing choice; it says %q", reason)
	}
}

// The Owner posting activate on a Done Goal is refused with a 422 that says
// why, and the Goal page still shows it Done: an ended Goal can't be brought
// back by activating it again.
func TestActivateDoneGoalLeavesItDone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	owner := h.SignIn("sam@example.com")
	g := h.ActiveGoal(owner, "Ship v2", "Customers wait too long for v2.")
	h.EndGoalInCheckin(owner, g.ID, domain.LifecycleDone)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, g)

	resp := postForm(t, client, goalURL+"/activate", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("activate status = %d, want 422", resp.StatusCode)
	}
	if reason := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "refused-reason")); !strings.Contains(reason, "Ship v2 is Done, so it can't be activated") {
		t.Errorf("refusal should say the Goal is Done; it says %q", reason)
	}

	page := getPage(t, client, goalURL)
	if got := textOf(findByTestID(t, page, "goal-lifecycle")); got != domain.LifecycleDone {
		t.Errorf("Goal page shows the Goal %q, want it still Done", got)
	}
}

// goalDimensionValuesIn is what each Dimension value shown under root reads,
// in order.
func goalDimensionValuesIn(root *nethtml.Node) []string {
	return textsOf(findAllByTestID(root, "goal-dimension-value"))
}

// An Owner assigns a Dimension value to their Goal from the Goal page; after the
// value is retired by an Admin it stays readable on the Goal (CONTEXT.md: Owners
// assign Dimension values; retired values stay readable).
func TestOwnerAssignsDimensionValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	// Assign Growth, then replace it with Reliability.
	postFormStatus(t, samClient, goalURL+"/dimensions", url.Values{"value_id": {fmt.Sprintf("%d", pillar.Values[0].ID)}})
	if got := goalDimensionValuesIn(getPage(t, samClient, goalURL)); !slices.Contains(got, "Growth") {
		t.Fatalf("Goal page shows values %q, missing assigned Growth", got)
	}
	postFormStatus(t, samClient, goalURL+"/dimensions", url.Values{"value_id": {fmt.Sprintf("%d", pillar.Values[1].ID)}})
	got := goalDimensionValuesIn(getPage(t, samClient, goalURL))
	if !slices.Contains(got, "Reliability") {
		t.Errorf("Goal page shows values %q, want Reliability after reassignment", got)
	}
	if slices.Contains(got, "Growth") {
		t.Errorf("Goal page shows values %q: Growth should be replaced, not kept", got)
	}

	// Admin retires Reliability; it stays readable on the Goal that carries it.
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	if got := goalDimensionValuesIn(getPage(t, samClient, goalURL)); !slices.Contains(got, "Reliability") {
		t.Errorf("Goal page shows values %q: retired Reliability should stay readable on the Goal", got)
	}
}

// goalFormFieldWithValue returns the field named name under root whose value
// is value, a checkbox's or the select's holding an option of that value, or
// false when there is none.
func goalFormFieldWithValue(root *nethtml.Node, name, value string) (pageField, bool) {
	for _, f := range formFields(root, name) {
		if f.Type == "select" {
			if len(findAll(f.Node, func(n *nethtml.Node) bool { return n.Data == "option" && attrOf(n, "value") == value })) > 0 {
				return f, true
			}
		} else if f.Value == value {
			return f, true
		}
	}
	return pageField{}, false
}

// On the Goal page a several-values Dimension offers a checkbox per non-retired
// value, saved together: an Owner gives the Goal two values, then removes one,
// then clears them. A one-value Dimension keeps its select.
func TestOwnerSetsSeveralDimensionValuesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra", "Legacy")
	h.CreateDimension(boss, "Pillar", "Growth")
	core, infra, legacy := teams.Values[0], teams.Values[1], teams.Values[2]
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, legacy.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	edit := goalOpenFormIn(t, getPage(t, samClient, goalURL+"?open=dimensions"), "dimensions")
	for _, v := range []domain.DimensionValue{core, infra} {
		if f, ok := goalFormFieldWithValue(edit, "value_id", fmt.Sprint(v.ID)); !ok || f.Type != "checkbox" {
			t.Errorf("no checkbox for %s: %s", v.Value, renderNode(edit))
		}
	}
	if offered := findAll(edit, func(n *nethtml.Node) bool { return attrOf(n, "value") == fmt.Sprint(legacy.ID) }); len(offered) > 0 {
		t.Errorf("retired Legacy is offered: %s", renderNode(offered[0]))
	}
	if !slices.ContainsFunc(formFields(edit, "value_id"), func(f pageField) bool { return f.Type == "select" && f.Label == "Pillar" }) {
		t.Errorf("one-value Pillar lost its select: %s", renderNode(edit))
	}

	dimID := fmt.Sprintf("%d", teams.ID)
	postFormStatus(t, samClient, goalURL+"/dimensions", url.Values{
		"dimension_id": {dimID},
		"value_id":     {fmt.Sprintf("%d", core.ID), fmt.Sprintf("%d", infra.ID)},
	})
	section := findByTestID(t, getPage(t, samClient, goalURL), "goal-dimensions")
	for _, want := range []string{"Core", "Infra"} {
		if got := goalDimensionValuesIn(section); !slices.Contains(got, want) {
			t.Errorf("Goal page shows values %q after saving two values, missing %s", got, want)
		}
	}
	edit = goalOpenFormIn(t, getPage(t, samClient, goalURL+"?open=dimensions"), "dimensions")
	if f, ok := goalFormFieldWithValue(edit, "value_id", fmt.Sprint(core.ID)); !ok || !f.Checked {
		t.Errorf("Core's checkbox isn't checked: %s", renderNode(edit))
	}

	postFormStatus(t, samClient, goalURL+"/dimensions", url.Values{
		"dimension_id": {dimID},
		"value_id":     {fmt.Sprintf("%d", infra.ID)},
	})
	got := goalDimensionValuesIn(findByTestID(t, getPage(t, samClient, goalURL), "goal-dimensions"))
	if slices.Contains(got, "Core") {
		t.Errorf("Core still shown after it was unchecked: %q", got)
	}
	if !slices.Contains(got, "Infra") {
		t.Errorf("Infra lost when Core was removed: %q", got)
	}

	postFormStatus(t, samClient, goalURL+"/dimensions", url.Values{"dimension_id": {dimID}})
	values, err := h.Service.GoalValues(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("after unchecking everything, values = %+v, want none", values)
	}
}

// Only the Owner, a Delegate or an Admin sets a Goal's Dimension values: another
// signed-in person posting to the endpoint is refused with 403 and the value is
// unchanged.
func TestNonOwnerCannotAssignDimensionValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	owner := h.SignIn("owner@example.com")
	h.SignIn("other@example.com")
	quarter := h.CreateDimension(boss, "Quarter", "Q3", "Q4")
	goal := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, quarter.Values[1])
	ts := newServer(t, h)

	otherClient := signInClient(t, ts.URL, "other@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	resp := postForm(t, otherClient, goalURL+"/dimensions", url.Values{"value_id": {fmt.Sprintf("%d", quarter.Values[0].ID)}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Owner assign status = %d, want 403", resp.StatusCode)
	}

	values, err := h.Service.GoalValues(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].Value != "Q4" {
		t.Errorf("values = %+v, want Q4 unchanged", values)
	}
}

// With Dimensions out of the main nav, a non-Admin who doesn't own the Goal
// reaches the Dimensions page from the Goal page's Dimensions section — before
// any Dimension is defined as well as after.
func TestGoalPageDimensionsSectionLinksToDimensionsForNonOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	owner := h.SignIn("owner@example.com")
	h.SignIn("other@example.com")
	goal := h.CreateGoal(owner, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	otherClient := signInClient(t, ts.URL, "other@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	empty := findByTestID(t, getPage(t, otherClient, goalURL), "goal-dimensions")
	if got := textOf(findByTestID(t, empty, "no-dimensions-defined")); got != "No Dimensions defined yet." {
		t.Fatalf("Dimensions section isn't empty, it reads %q", got)
	}
	if linkTo(empty, "/dimensions") == nil {
		t.Errorf("empty Dimensions section has no link to /dimensions: %s", renderNode(empty))
	}

	h.CreateDimension(boss, "Quarter", "Q3", "Q4")
	defined := findByTestID(t, getPage(t, otherClient, goalURL), "goal-dimensions")
	if listed := textsOf(findAllByTestID(defined, "goal-dimension")); len(listed) != 1 || !strings.HasPrefix(listed[0], "Quarter") {
		t.Fatalf("Dimensions section lists %q, want Quarter", listed)
	}
	if linkTo(defined, "/dimensions") == nil {
		t.Errorf("Dimensions section has no link to /dimensions: %s", renderNode(defined))
	}
}

// Grouped by a several-values Dimension, a Goal with two values appears in both
// groups, and the list's total counts it once (ADR 0005).
func TestGoalListGroupsSeveralValuesGoalUnderEachValue(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	shared := h.CreateGoal(sam, "Alpha", "A matters.")
	h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(shared, teams.Values[0])
	h.AssignGoalValue(shared, teams.Values[1])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	grouped := getPage(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, teams.ID))
	groups := findAllByTestID(grouped, "goal-group")
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want Core, Infra and Unassigned: %s", len(groups), renderNode(grouped))
	}
	for i, label := range []string{"Core", "Infra"} {
		group := groups[i]
		if got := textOf(findByTestID(t, group, "goal-group-label")); got != label || !slices.Contains(goalRowTitlesIn(goalListRowsIn(group), shared), "Alpha") {
			t.Errorf("group %d should be %s holding Alpha, is %q: %s", i, label, got, renderNode(group))
		}
	}
	if got := textOf(findByTestID(t, grouped, "goal-count")); got != "2" {
		t.Errorf("total = %q, want 2", got)
	}

	flat := getPage(t, client, ts.URL+"/goals")
	if got := textOf(findByTestID(t, flat, "goal-count")); got != "2" {
		t.Errorf("flat total = %q, want 2", got)
	}
}

// The Goal list filters to the Goals carrying a chosen value and groups the list
// under each value of a chosen Dimension (CONTEXT.md: filter and group Goals).
func TestGoalListFiltersAndGroupsByDimension(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	growth, reliability := pillar.Values[0], pillar.Values[1]
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	bravo := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(alpha, growth)
	h.AssignGoalValue(bravo, reliability)
	ts := newServer(t, h)

	client := signInClient(t, ts.URL, "sam@example.com")

	// Filter to Growth: Alpha shows, Bravo does not.
	filtered := getPage(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, growth.ID))
	if got := goalRowTitlesIn(goalListRowsIn(filtered), alpha, bravo); !slices.Equal(got, []string{"Alpha"}) {
		t.Errorf("filter by Growth shows %q, want only Alpha", got)
	}

	// Group by Pillar: value-labelled groups appear.
	grouped := getPage(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if !hasTestID(grouped, "goal-group") {
		t.Fatalf("grouped list missing groups: %s", renderNode(grouped))
	}
	if labels := textsOf(findAllByTestID(grouped, "goal-group-label")); !slices.Contains(labels, "Growth") || !slices.Contains(labels, "Reliability") {
		t.Errorf("grouped list's group labels are %q, missing a value", labels)
	}
}

// goalFormFitsChosen reports whether the New goal form's Where it fits offers
// v chosen: its checkbox ticked or its option selected. It fails the test when
// Where it fits doesn't offer v at all.
func goalFormFitsChosen(t *testing.T, root *nethtml.Node, v domain.DimensionValue) bool {
	t.Helper()
	fits := findByTestID(t, root, "goal-form-fits")
	f, ok := goalFormFieldWithValue(fits, "value_id", fmt.Sprint(v.ID))
	if !ok {
		t.Fatalf("Where it fits doesn't offer %s: %s", v.Value, renderNode(fits))
	}
	if f.Type == "select" {
		return f.Value == fmt.Sprint(v.ID) && hasAttr(chosenOption(f.Node), "selected")
	}
	return f.Checked
}

// Creating a Goal under a parent, from /goals/new?parent=<id>, offers the
// parent's values as defaults: a kept default is assigned to the child and the
// child contributes to the parent, but the values are not inherited — a child
// created without them carries none (CONTEXT.md: the parent's values are
// offered as defaults, not inherited).
func TestCreateChildGoalOffersParentDefaults(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	growth := pillar.Values[0]
	parent := h.CreateGoal(sam, "Parent", "Parent matters.")
	h.AssignGoalValue(parent, growth)
	ts := newServer(t, h)

	samClient := signInClient(t, ts.URL, "sam@example.com")

	// The New goal form, opened for the parent, offers Growth chosen.
	if page := getPage(t, samClient, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID)); !goalFormFitsChosen(t, page, growth) {
		t.Errorf("parent value not offered as a chosen default: %s", renderNode(findByTestID(t, page, "goal-form-fits")))
	}

	// Keeping the default assigns Growth to the child and links it to the parent.
	resp := postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":          {"Kept child"},
		"why_it_matters": {"Child matters."},
		"parent_id":      {fmt.Sprint(parent.ID)},
		"value_id":       {fmt.Sprintf("%d", growth.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	childPage := parsePage(t, readBody(t, resp))
	if got := goalDimensionValuesIn(childPage); !slices.Contains(got, "Growth") {
		t.Errorf("kept default not assigned to the child, which shows values %q", got)
	}
	if parents := findByTestID(t, childPage, "goal-parents"); !strings.Contains(textOf(parents), "Parent") {
		t.Errorf("child does not contribute to the parent: %s", renderNode(parents))
	}

	// Creating a child without the default leaves it unassigned (not inherited).
	resp = postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":          {"Bare child"},
		"why_it_matters": {"Child matters."},
		"parent_id":      {fmt.Sprint(parent.ID)},
	})
	if barePage := parsePage(t, readBody(t, resp)); !hasTestID(barePage, "goal-dimension-unassigned") {
		t.Errorf("child without the default should be unassigned: %s", renderNode(findByTestID(t, barePage, "goal-dimensions")))
	}
}

// Creating a child Goal is all-or-nothing: when a default the form still offered
// was retired before the submit, the Goal is not created, no link is requested,
// and the form comes back with the error and what the person typed (ticket #26).
func TestCreateChildGoalWithRetiredValueLeavesNoGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	core := h.CreateDimension(boss, "Team", "Core").Values[0]
	growth := h.CreateDimension(boss, "Pillar", "Growth").Values[0]
	parent := h.CreateGoal(sam, "Parent", "Parent matters.")
	h.AssignGoalValue(parent, core)
	h.AssignGoalValue(parent, growth)
	ts := newServer(t, h)

	samClient := signInClient(t, ts.URL, "sam@example.com")

	// The form is loaded while Growth is still live, then Growth is retired.
	if page := getPage(t, samClient, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID)); !goalFormFitsChosen(t, page, growth) {
		t.Fatalf("the New goal form does not offer Growth as a default: %s", renderNode(findByTestID(t, page, "goal-form-fits")))
	}
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, growth.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	resp := postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":          {"GrandKid"},
		"why_it_matters": {"Feeds the parent."},
		"parent_id":      {fmt.Sprint(parent.ID)},
		"value_id":       {fmt.Sprintf("%d", core.ID), fmt.Sprintf("%d", growth.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body:\n%s", resp.StatusCode, body)
	}
	page := parsePage(t, body)
	form := findByTestID(t, page, "goal-form")
	if !strings.Contains(textOf(form), "retired") {
		t.Errorf("the form does not say the value is retired: %s", renderNode(form))
	}
	if title, why := formField(t, form, "title").Value, formField(t, form, "why_it_matters").Value; title != "GrandKid" || why != "Feeds the parent." {
		t.Errorf("the form lost the title or Why it matters: title %q, Why it matters %q", title, why)
	}
	if !goalFormFitsChosen(t, page, core) {
		t.Errorf("the form lost the kept Core default: %s", renderNode(findByTestID(t, page, "goal-form-fits")))
	}
	if picked := formFields(form, "parent_id"); !slices.ContainsFunc(picked, func(f pageField) bool { return f.Value == fmt.Sprint(parent.ID) }) {
		t.Errorf("the parent is no longer picked: %s", renderNode(form))
	}

	if list := getPage(t, samClient, ts.URL+"/goals"); strings.Contains(textOf(list), "GrandKid") {
		t.Errorf("a failed child create left a Goal behind: %q", textsOf(goalListRowsIn(list)))
	}
	if children := h.ChildrenOf(parent); len(children) != 0 {
		t.Errorf("a failed child create requested a link: %v", children)
	}
}

// The helpers below read the Goal list, the Goal page and the Goal table
// through the parser helpers (helpers_test.go): each returns the nodes a person
// meets there, or the text they read, never markup.

// goalHref is where a link to the Goal goalID goes.
func goalHref(goalID int64) string { return "/goals/" + strconv.FormatInt(goalID, 10) }

// goalListRowsIn returns the Goal list's rows under root, in the order they
// render.
func goalListRowsIn(root *nethtml.Node) []*nethtml.Node {
	return findAll(root, func(n *nethtml.Node) bool { return n.Data == "tr" && attrOf(n, "data-testid") == "goal-row" })
}

// goalRowTitlesIn names the Goals rows link to, in order, by matching each row
// against the Goals it could be.
func goalRowTitlesIn(rows []*nethtml.Node, goals ...domain.Goal) []string {
	titles := make([]string, 0, len(rows))
	for _, r := range rows {
		for _, g := range goals {
			if linkTo(r, goalHref(g.ID)) != nil {
				titles = append(titles, g.Title)
			}
		}
	}
	return titles
}

// goalListRowOf returns the Goal list's one row under root linking to g,
// failing the test unless there is exactly one.
func goalListRowOf(t *testing.T, root *nethtml.Node, g domain.Goal) *nethtml.Node {
	t.Helper()
	var found []*nethtml.Node
	for _, r := range goalListRowsIn(root) {
		if linkTo(r, goalHref(g.ID)) != nil {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Goal list has %d rows for %q, want 1:\n%s", len(found), g.Title, renderNode(root))
	}
	return found[0]
}

// goalTableHeadsIn is what each of the Goal table's column headers reads, in
// order.
func goalTableHeadsIn(t *testing.T, root *nethtml.Node) []string {
	t.Helper()
	return textsOf(findAll(findByTestID(t, root, "goal-table-head"), func(n *nethtml.Node) bool { return n.Data == "th" }))
}

// goalTableRowsIn returns the Goal table's rows of Goals, in order.
func goalTableRowsIn(t *testing.T, root *nethtml.Node) []*nethtml.Node {
	t.Helper()
	return findAllByTestID(findByTestID(t, root, "goal-table"), "goal-table-row")
}

// goalTableRowOf returns the Goal table's row linking to g, failing the test
// unless there is one.
func goalTableRowOf(t *testing.T, root *nethtml.Node, g domain.Goal) *nethtml.Node {
	t.Helper()
	for _, row := range goalTableRowsIn(t, root) {
		if linkTo(row, goalHref(g.ID)) != nil {
			return row
		}
	}
	t.Fatalf("the Goal table has no row for %s:\n%s", g.Title, renderNode(root))
	return nil
}

// goalTableCellIn returns row's cell under the column headed head in the
// Goal table under root.
func goalTableCellIn(t *testing.T, root, row *nethtml.Node, head string) *nethtml.Node {
	t.Helper()
	i := slices.Index(goalTableHeadsIn(t, root), head)
	if i < 0 {
		t.Fatalf("the Goal table has no %s column", head)
	}
	var cells []*nethtml.Node
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.ElementNode && c.Data == "td" {
			cells = append(cells, c)
		}
	}
	if i >= len(cells) {
		t.Fatalf("row has %d cells, no %s: %s", len(cells), head, renderNode(row))
	}
	return cells[i]
}

// goalActionMenuIn returns the Goal page header's "More ▾" action menu, the
// goal-more disclosure among the header's actions.
func goalActionMenuIn(t *testing.T, root *nethtml.Node) *nethtml.Node {
	t.Helper()
	menu := findByTestID(t, root, "goal-more")
	if !within(menu, findByTestID(t, root, "goal-actions")) {
		t.Fatalf("the action menu isn't among the header's actions: %s", renderNode(menu))
	}
	return menu
}

// goalMenuItemsIn returns the action menu's items, label to link, failing
// when the menu's list holds anything but links: no form, control or nested
// disclosure, and no item that isn't a link.
func goalMenuItemsIn(t *testing.T, root *nethtml.Node) map[string]string {
	t.Helper()
	menu := goalActionMenuIn(t, root)
	for _, control := range findAll(menu, func(n *nethtml.Node) bool {
		switch n.Data {
		case "form", "input", "button", "select", "textarea":
			return true
		case "details":
			return n != menu
		}
		return false
	}) {
		t.Errorf("the action menu holds a <%s>, not only links: %s", control.Data, renderNode(menu))
	}
	items := map[string]string{}
	for _, li := range findAll(menu, func(n *nethtml.Node) bool { return n.Data == "li" }) {
		links := findAll(li, func(n *nethtml.Node) bool { return n.Data == "a" })
		if len(links) != 1 || textOf(links[0]) != textOf(li) {
			t.Errorf("an action menu item is not a plain link: %s", renderNode(li))
			continue
		}
		items[textOf(links[0])] = attrOf(links[0], "href")
	}
	return items
}

// goalOpenFormIn returns the form the Goal page shows open in place, the
// element carrying data-open-form, failing unless exactly one form is open
// and it is the named one.
func goalOpenFormIn(t *testing.T, root *nethtml.Node, form string) *nethtml.Node {
	t.Helper()
	open := findAll(root, func(n *nethtml.Node) bool { return hasAttr(n, "data-open-form") })
	if len(open) != 1 {
		t.Fatalf("page shows %d forms open, want only %q", len(open), form)
	}
	if got := attrOf(open[0], "data-open-form"); got != form {
		t.Fatalf("page shows the %q form open, want %q", got, form)
	}
	return open[0]
}

// goalOpenFormsIn names the forms the Goal page shows open in place, in order.
func goalOpenFormsIn(root *nethtml.Node) []string {
	var names []string
	for _, n := range findAll(root, func(n *nethtml.Node) bool { return hasAttr(n, "data-open-form") }) {
		names = append(names, attrOf(n, "data-open-form"))
	}
	return names
}

// goalShownPerson is a person as a page shows them (CONTEXT.md: Name): the
// label they're shown by, the email on hover, and, for one shown as a control,
// whether the control starts collapsed over their hidden email.
type goalShownPerson struct {
	Label, Title string
	// Discloses reports a disclose button reading Label, collapsed, with the
	// email hidden beside it; a person shown plainly, inside another control,
	// has none.
	Discloses bool
	// Email is the hidden email the button discloses, or "".
	Email string
}

// goalPeopleShownIn returns each person shown under root, in order.
func goalPeopleShownIn(root *nethtml.Node) []goalShownPerson {
	var out []goalShownPerson
	for _, p := range findAll(root, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "person") }) {
		person := goalShownPerson{Label: shownTextOf(p), Title: attrOf(p, "title")}
		buttons := findAll(p, func(n *nethtml.Node) bool { return n.Data == "button" })
		emails := findAll(p, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "person-email") })
		if len(buttons) == 1 && len(emails) == 1 {
			b := buttons[0]
			person.Label, person.Title = textOf(b), attrOf(b, "title")
			person.Discloses = flatFrameHasClass(b, "disclose") && attrOf(b, "type") == "button" &&
				attrOf(b, "aria-expanded") == "false" && isHiddenElement(emails[0])
			person.Email = strings.Join(nodeWords(emails[0], emails[0], nil), " ")
		}
		out = append(out, person)
	}
	return out
}

// goalShows reports whether root shows the person emailAddr by label as a
// control that discloses their email, the email on hover too (shownAs).
func goalShows(root *nethtml.Node, emailAddr, label string) bool {
	return slices.Contains(goalPeopleShownIn(root), goalShownPerson{Label: label, Title: emailAddr, Discloses: true, Email: emailAddr})
}

// The Goal list is grouped by Health: Red, Yellow, Green, then the Goals with
// no Health, each under a header row with its shape, name and count — except
// that the No Health group leads when it holds a Goal that needs a decision or
// update, as the Check-in overdue "Overdue work" does here. Within a
// group, Goals that need a decision or update come first: Ownerless, then
// Check-in overdue or Path to Green overdue, then the rest, alphabetical
// within each. Each row leads with its Health's shape alone, named by an
// aria-label so colour is never the only signal, and a Goal with no Health
// shows its Lifecycle as a chip. Nothing sits in a card. The classes that
// draw the shapes and the chip are styles_test.go's
// (TestGoalListGroupsByHealthProblemsFirstInEachStyles).
func TestGoalListGroupsByHealthProblemsFirstInEach(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	later := testsupport.Epoch.AddDate(0, 2, 0)

	checkinOverdue := h.ActiveGoal(sam, "Overdue work", "It matters.")
	h.Clock.Advance(10 * day)
	proposed := h.CreateGoal(sam, "Proposed idea", "It matters.")
	betaGreen := h.ActiveGoal(sam, "Beta green", "It matters.")
	h.Checkin(sam, betaGreen.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	alphaGreen := h.ActiveGoal(sam, "Alpha green", "It matters.")
	h.Checkin(sam, alphaGreen.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	yellow := h.ActiveGoal(sam, "Wobbly", "It matters.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Wobbling.", "Fix it.", later)
	ownerless := h.ActiveGoal(kim, "Orphaned", "It matters.")
	h.Checkin(kim, ownerless.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	red := h.ActiveGoal(sam, "Zulu fire", "It matters.")
	h.Checkin(sam, red.ID, domain.HealthRed, "On fire.", "Put it out.", later)
	ts := newServer(t, h)
	goals := []domain.Goal{checkinOverdue, proposed, betaGreen, alphaGreen, yellow, ownerless, red}

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals?org=all")
	if cards := findAll(page, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "card") }); len(cards) > 0 {
		t.Errorf("the Goal list sits in a card: %s", renderNode(cards[0]))
	}
	list := findByTestID(t, page, "goal-list")
	groups := findAllByTestID(list, "goal-health-group")
	want := []struct {
		name, count string
		rows        []string
	}{
		{"No Health", "2", []string{"Overdue work", "Proposed idea"}},
		{"Red", "1", []string{"Zulu fire"}},
		{"Yellow", "1", []string{"Wobbly"}},
		{"Green", "3", []string{"Orphaned", "Alpha green", "Beta green"}},
	}
	if len(groups) != len(want) {
		t.Fatalf("got %d Health groups, want %d: %s", len(groups), len(want), renderNode(list))
	}
	for i, w := range want {
		head := findByTestID(t, groups[i], "goal-group-head")
		if mark := findByTestID(t, head, "goal-group-mark"); attrOf(mark, "aria-hidden") != "true" {
			t.Errorf("%s: header row's shape isn't hidden from assistive technology: %s", w.name, renderNode(mark))
		}
		if got := textOf(findByTestID(t, head, "goal-group-label")); got != w.name {
			t.Errorf("%s: header label = %q", w.name, got)
		}
		if got := textOf(findByTestID(t, head, "goal-group-count")); got != w.count {
			t.Errorf("%s: header count = %q, want %s", w.name, got, w.count)
		}
		rows := goalListRowsIn(groups[i])
		if got := goalRowTitlesIn(rows, goals...); !slices.Equal(got, w.rows) {
			t.Errorf("%s group rows = %q, want %q", w.name, got, w.rows)
		}
		for _, row := range rows {
			if mark := findByTestID(t, row, "goal-row-health"); attrOf(mark, "role") != "img" || attrOf(mark, "aria-label") != w.name {
				t.Errorf("%s row's Health mark = %s, want an img labelled %q", w.name, renderNode(mark), w.name)
			}
		}
	}
	if chip := findByTestID(t, goalListRowsIn(groups[0])[1], "goal-row-lifecycle"); textOf(chip) != "Proposed" {
		t.Errorf("a Goal with no Health doesn't show its Lifecycle as a chip: %s", renderNode(chip))
	}
}

// A Goal whose latest Check-in is Green while a Milestone is overdue is marked
// Milestone overdue beside its title, in Check-in overdue's chip
// (TestGoalListMarksAGreenGoalPastAMilestoneStyles), and sorts with the
// Check-in overdue Goals, at the head of the Green group, below the Yellow
// one. Its Health stays Green, and a plain Green Goal carries no such mark.
func TestGoalListMarksAGreenGoalPastAMilestone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	overdue, _ := h.GreenGoalWithOverdueMilestone(sam, "Zulu launch")
	yellow := h.ActiveGoal(sam, "Wobbly", "It matters.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Wobbling.", "Fix it.", h.Clock.Now().AddDate(0, 1, 0))
	green := h.ActiveGoal(sam, "Alpha green", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	ts := newServer(t, h)

	rows := goalListRowsIn(getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals?org=all"))
	if got, want := goalRowTitlesIn(rows, overdue, yellow, green), []string{"Wobbly", "Zulu launch", "Alpha green"}; !slices.Equal(got, want) {
		t.Fatalf("row order = %q, want %q", got, want)
	}
	if chip := findByTestID(t, rows[1], "milestone-overdue"); textOf(chip) != "Milestone overdue" {
		t.Errorf("Milestone overdue chip = %s, want one reading \"Milestone overdue\"", renderNode(chip))
	}
	if health := findByTestID(t, rows[1], "goal-row-health"); attrOf(health, "aria-label") != "Green" {
		t.Errorf("Health = %s, want the Owner's Green", renderNode(health))
	}
	for _, row := range []*nethtml.Node{rows[0], rows[2]} {
		if hasTestID(row, "milestone-overdue") {
			t.Errorf("an unmarked row carries a Milestone overdue chip: %s", renderNode(row))
		}
	}
}

// goalListDueDates is what each date a Goal list row's Due cell shows reads,
// each its own element, as tag and text ("del Jul 2"), in order.
func goalListDueDates(cell *nethtml.Node) []string {
	var dates []string
	for c := cell.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.ElementNode {
			dates = append(dates, c.Data+" "+textOf(c))
		}
	}
	return dates
}

// Each row shows when the Goal is due, with the dates its Date Slips moved it
// from struck through, each date whole in its own element so the dates wrap
// between them and never mid-date (#180; the rule keeping each whole is
// TestGoalListShowsDueDateAndLastCheckinStyles'), or a dash for an Ongoing
// Goal, and how long ago its last Check-in was.
func TestGoalListShowsDueDateAndLastCheckin(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	slipped := h.ActiveGoal(sam, "Slipped", "It matters.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:             slipped.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Later than planned.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		DeliveryDate:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		DeliveryDateReason: "Vendor slipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	ongoing := h.CreateGoal(sam, "Keep the lights on", "It matters.")
	if _, err := h.Service.MarkGoalOngoing(t.Context(), ongoing.Owner.ID, ongoing.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	h.Clock.Advance(3 * day)
	ts := newServer(t, h)

	rows := goalListRowsIn(getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals"))
	if got := goalRowTitlesIn(rows, slipped, ongoing); !slices.Equal(got, []string{"Slipped", "Keep the lights on"}) {
		t.Fatalf("rows = %q", got)
	}

	due := findByTestID(t, rows[0], "goal-row-due")
	if got, want := goalListDueDates(due), []string{"del Jul 2", "span Jul 16"}; !slices.Equal(got, want) || !strings.HasPrefix(runningText(due), "Jul 2 Jul 16") {
		t.Errorf("slipped Goal's Due does not strike its prior date, each date whole: reads %q as %q, want %q: %s", runningText(due), got, want, renderNode(due))
	}
	if last := textOf(findByTestID(t, rows[0], "goal-row-last-checkin")); !strings.Contains(last, "3 days ago") {
		t.Errorf("Last check-in is not relative: %q", last)
	}
	if due := textOf(findByTestID(t, rows[1], "goal-row-due")); !strings.Contains(due, "—") || strings.Contains(due, "20") {
		t.Errorf("Ongoing Goal's Due is not a dash: %q", due)
	}
	if last := textOf(findByTestID(t, rows[1], "goal-row-last-checkin")); !strings.Contains(last, "—") {
		t.Errorf("a Goal with no Check-in shows a Last check-in: %q", last)
	}
}

// Searching the Goal list keeps the Goals whose title or Owner's email or Name
// holds the text, ignoring case, and the search box keeps what was typed.
func TestGoalListSearchesTitleAndOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignInNamed("kim@example.com", "Kim Lee")
	latency := h.CreateGoal(sam, "Cut checkout latency", "It matters.")
	hiring := h.CreateGoal(kim, "Hire two engineers", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"LATENCY", []string{"Cut checkout latency"}},
		{"kim@", []string{"Hire two engineers"}},
		{"kim lee", []string{"Hire two engineers"}},
		{"example.com", []string{"Cut checkout latency", "Hire two engineers"}},
		{"nothing like it", nil},
	} {
		page := getPage(t, client, ts.URL+"/goals?org=all&q="+url.QueryEscape(tc.q))
		if got := goalRowTitlesIn(goalListRowsIn(page), latency, hiring); !slices.Equal(got, tc.want) {
			t.Errorf("q=%q: rows = %q, want %q", tc.q, got, tc.want)
		}
		if search := findByTestID(t, page, "goal-search"); attrOf(search, "value") != tc.q {
			t.Errorf("q=%q: search box lost the query: %s", tc.q, renderNode(search))
		}
		if tc.want == nil && !hasTestID(page, "no-goals") {
			t.Errorf("q=%q: no empty state", tc.q)
		}
	}
}

// goalFilterChoice is the value a Goal list filter select carries selected,
// or "" when it marks no option selected.
func goalFilterChoice(t *testing.T, root *nethtml.Node, testID string) string {
	t.Helper()
	if opt := chosenOption(findByTestID(t, root, testID)); opt != nil && hasAttr(opt, "selected") {
		return attrOf(opt, "value")
	}
	return ""
}

// The Health filter keeps the Goals at the chosen Health, or those with none,
// and its select keeps the choice.
func TestGoalListFiltersByHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	later := testsupport.Epoch.AddDate(0, 2, 0)
	green := h.ActiveGoal(sam, "Green one", "It matters.")
	h.Checkin(sam, green.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	yellow := h.ActiveGoal(sam, "Yellow one", "It matters.")
	h.Checkin(sam, yellow.ID, domain.HealthYellow, "Wobbling.", "Fix it.", later)
	red := h.ActiveGoal(sam, "Red one", "It matters.")
	h.Checkin(sam, red.ID, domain.HealthRed, "On fire.", "Put it out.", later)
	proposed := h.CreateGoal(sam, "Proposed one", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for health, want := range map[string]string{
		"green":  "Green one",
		"yellow": "Yellow one",
		"red":    "Red one",
		"none":   "Proposed one",
	} {
		page := getPage(t, client, ts.URL+"/goals?health="+health)
		if got := goalRowTitlesIn(goalListRowsIn(page), green, yellow, red, proposed); !slices.Equal(got, []string{want}) {
			t.Errorf("health=%s: rows = %q, want [%q]", health, got, want)
		}
		if got := goalFilterChoice(t, page, "goal-health-filter"); got != health {
			t.Errorf("health=%s: select lost the choice, has %q selected", health, got)
		}
	}
}

// The Lifecycle filter keeps the Goals in the chosen Lifecycle and, left at its
// default, keeps every Lifecycle; its select keeps the choice.
func TestGoalListFiltersByLifecycle(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	active := h.ActiveGoal(sam, "Active one", "It matters.")
	paused := h.OnHoldGoal(sam, "Paused one", "It matters.", "Budget freeze.")
	proposed := h.CreateGoal(sam, "Proposed one", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	all := getPage(t, client, ts.URL+"/goals")
	if got := goalRowTitlesIn(goalListRowsIn(all), active, paused, proposed); len(got) != 3 {
		t.Errorf("default Lifecycle filter should keep every Goal; rows = %q", got)
	}
	for lifecycle, want := range map[string]string{
		domain.LifecycleActive:   "Active one",
		domain.LifecycleOnHold:   "Paused one",
		domain.LifecycleProposed: "Proposed one",
	} {
		page := getPage(t, client, ts.URL+"/goals?lifecycle="+url.QueryEscape(lifecycle))
		if got := goalRowTitlesIn(goalListRowsIn(page), active, paused, proposed); !slices.Equal(got, []string{want}) {
			t.Errorf("lifecycle=%s: rows = %q, want [%q]", lifecycle, got, want)
		}
		if got := goalFilterChoice(t, page, "goal-lifecycle-filter"); got != lifecycle {
			t.Errorf("lifecycle=%s: select lost the choice, has %q selected", lifecycle, got)
		}
	}
}

// Scoped to Mine, by ?org=mine, the list keeps the Goals the viewer Owns or is
// a Delegate on, and the Scope select shows Mine chosen. Without Managers it offers Everyone and Mine
// alone. The other filters still combine with it.
func TestGoalListFiltersToMine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	owned := h.CreateGoal(sam, "Sam's own", "It matters.")
	delegated := h.CreateGoal(kim, "Kim's, delegated to Sam", "It matters.")
	h.AddDelegate(kim, sam, delegated.ID)
	others := h.CreateGoal(kim, "Kim's alone", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals?org=mine")
	got := goalRowTitlesIn(goalListRowsIn(page), owned, delegated, others)
	if want := []string{"Kim's, delegated to Sam", "Sam's own"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	choices, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter"))
	if value != "mine" || label != "Mine" {
		t.Errorf("the Scope select shows %q %q chosen, want Mine", value, label)
	}
	if want := []string{"Everyone", "Mine"}; !slices.Equal(choices, want) {
		t.Errorf("without Managers the Scope select offers %q, want %q", choices, want)
	}

	page = getPage(t, client, ts.URL+"/goals?org=all")
	if got := goalRowTitlesIn(goalListRowsIn(page), owned, delegated, others); len(got) != 3 {
		t.Errorf("for Everyone every Goal shows; rows = %q", got)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != "all" || label != "Everyone" {
		t.Errorf("for Everyone, the Scope select shows %q %q chosen, want Everyone", value, label)
	}

	later := testsupport.Epoch.AddDate(0, 2, 0)
	ownRed := h.ActiveGoal(sam, "Sam's red", "It matters.")
	h.Checkin(sam, ownRed.ID, domain.HealthRed, "On fire.", "Put it out.", later)
	kimsRed := h.ActiveGoal(kim, "Kim's red", "It matters.")
	h.Checkin(kim, kimsRed.ID, domain.HealthRed, "On fire.", "Put it out.", later)
	page = getPage(t, client, ts.URL+"/goals?org=mine&health=red")
	if got := goalRowTitlesIn(goalListRowsIn(page), owned, delegated, others, ownRed, kimsRed); !slices.Equal(got, []string{"Sam's red"}) {
		t.Errorf("Mine and Red: rows = %q, want only Sam's red", got)
	}
}

// The Scope is read from ?org= alone: the old ?mine=1 is ignored like any
// unknown parameter, so on its own it gives the viewer's default, My org for
// Mia, and beside an org that resolves to someone it gives that Org, listing
// Goals the viewer neither Owns nor is a Delegate on. An org naming no one, or
// malformed, gives the default, Mine for Oz.
func TestGoalListScopesToAnOrgOverMine(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)
	mia := signInClient(t, ts.URL, "mia@example.com")
	oz := signInClient(t, ts.URL, "oz@example.com")
	miaID := strconv.FormatInt(org.Mia.ID, 10)

	page := getPage(t, mia, ts.URL+"/goals?mine=1")
	if got, want := goalRowTitlesIn(goalListRowsIn(page), org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia at the old Mine address: rows = %q, want her default, My org's %q", got, want)
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != miaID {
		t.Errorf("Mia at the old Mine address: the Scope select shows %q chosen, want her default, %s", value, miaID)
	}

	page = getPage(t, oz, ts.URL+"/goals?mine=1&org="+miaID)
	if got, want := goalRowTitlesIn(goalListRowsIn(page), org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia's org beside the old Mine parameter: rows = %q, want %q", got, want)
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != miaID {
		t.Errorf("Mia's org beside the old Mine parameter: the Scope select shows %q chosen", value)
	}

	for _, raw := range []string{"abc", "999999"} {
		page := getPage(t, oz, ts.URL+"/goals?org="+raw)
		if got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%s: rows = %q, want Oz's default, Mine", raw, got)
		}
		if _, value, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != "mine" {
			t.Errorf("org=%s: the Scope select shows %q chosen, want Oz's default, Mine", raw, value)
		}
	}
}

// goalScopeOptionIn returns the Scope select's one option submitting value,
// failing the test unless there is exactly one.
func goalScopeOptionIn(t *testing.T, sel *nethtml.Node, value string) *nethtml.Node {
	t.Helper()
	opts := findAll(sel, func(n *nethtml.Node) bool { return n.Data == "option" && attrOf(n, "value") == value })
	if len(opts) != 1 {
		t.Fatalf("the Scope select has %d options submitting %q, want 1: %s", len(opts), value, renderNode(sel))
	}
	return opts[0]
}

// goalListCount is what the Goal list header's count reads.
func goalListCount(t *testing.T, page *nethtml.Node) string {
	t.Helper()
	return textOf(findByTestID(t, page, "goal-count"))
}

// goalListLinkQuery is the query of the link carrying testID on the Goal list.
func goalListLinkQuery(t *testing.T, page *nethtml.Node, testID string) url.Values {
	t.Helper()
	u, err := url.Parse(attrOf(findByTestID(t, page, testID), "href"))
	if err != nil {
		t.Fatalf("%s href: %v", testID, err)
	}
	return u.Query()
}

// My org keeps the Goals whose Owner is in the viewer's org, their own
// included: for a VP every level below them, for a manager only their part. The
// filter bar's one Scope select, its first control, offers Everyone, Mine, My
// org, then each other person's org under Other orgs, by Name with their email;
// a viewer with no one under them isn't offered My org.
func TestGoalListScopesToMyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)

	vera := signInClient(t, ts.URL, "vera@example.com")
	page := getPage(t, vera, scopeOrgURL(ts.URL, "/goals", org.Vera))
	got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...)
	if want := []string{"Max's goal", "Mia's goal", "Pax's goal", "Pia's goal", "Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Vera's My org: rows = %q, want %q", got, want)
	}
	sel := scopeSelectIn(t, page, "goal-scope-filter")
	choices, value, label := selectChoices(sel)
	if want := []string{
		"Everyone", "Mine", "My org",
		"Other orgs: Max Marsh (max@example.com)", "Other orgs: Mia Moss (mia@example.com)", "Other orgs: Oz Ortiz (oz@example.com)",
		"Other orgs: Pax Penn (pax@example.com)", "Other orgs: Pia Park (pia@example.com)",
	}; !slices.Equal(choices, want) {
		t.Errorf("the Scope select offers %q, want %q", choices, want)
	}
	if want := strconv.FormatInt(org.Vera.ID, 10); value != want || label != "My org" {
		t.Errorf("Vera's My org isn't the chosen scope: %q %q", value, label)
	}
	if attrOf(sel, "name") != "org" {
		t.Errorf("the Scope select doesn't submit ?org=: %s", renderNode(sel))
	}
	// The bar opens with the Scope chip: its key, read only by sight, then
	// the select.
	bar := findByTestID(t, page, "goal-filter-bar")
	if chip := firstElementChild(bar); chip == nil || chip.Data != "label" || sel.Parent != chip {
		t.Errorf("the Scope select isn't the filter bar's first control: %s", renderNode(bar))
	} else if key := firstElementChild(chip); key == nil || key.Data != "span" || textOf(key) != "Scope:" || attrOf(key, "aria-hidden") != "true" || key.NextSibling != sel {
		t.Errorf("the Scope chip doesn't read Scope: before its select: %s", renderNode(chip))
	}
	if len(formFields(page, "mine")) != 0 || strings.Contains(strings.Join(nodeWords(page, page, nil), " "), "Mine only") {
		t.Errorf("the filter bar still has a Mine only checkbox")
	}
	if count := goalListCount(t, page); count != "5 in My org" {
		t.Errorf("the count doesn't name My org: %q", count)
	}

	mia := signInClient(t, ts.URL, "mia@example.com")
	page = getPage(t, mia, scopeOrgURL(ts.URL, "/goals", org.Mia))
	got = goalRowTitlesIn(goalListRowsIn(page), org.goals()...)
	if want := []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia's My org: rows = %q, want %q", got, want)
	}

	pia := signInClient(t, ts.URL, "pia@example.com")
	page = getPage(t, pia, ts.URL+"/goals")
	if choices, _, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); slices.Contains(choices, "My org") {
		t.Errorf("Pia, with no one under her, is offered My org: %q", choices)
	}
}

// firstElementChild is n's first child element, or nil.
func firstElementChild(n *nethtml.Node) *nethtml.Node {
	if n == nil {
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.ElementNode {
			return c
		}
	}
	return nil
}

// Anyone may scope the list to another person's org, picked by Name with
// their email, and the list names whose org it is. A Goal is in an org by
// its Owner: a Delegate in the org doesn't bring in a Goal owned outside it,
// and an Ownerless Goal stays in the org its Departed Owner was in.
func TestGoalListScopesToAnotherPersonsOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	h.AddDelegate(org.Oz, org.Pax, org.OzGoal.ID)
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, org.Pax.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	pia := signInClient(t, ts.URL, "pia@example.com")
	maxID := strconv.FormatInt(org.Max.ID, 10)

	page := getPage(t, pia, ts.URL+"/goals")
	orgSel := scopeSelectIn(t, page, "goal-scope-filter")
	if opt := goalScopeOptionIn(t, orgSel, maxID); textOf(opt) != "Max Marsh (max@example.com)" || hasAttr(opt, "selected") {
		t.Errorf("the Scope select doesn't offer Max by Name and email: %s", renderNode(opt))
	}

	page = getPage(t, pia, scopeOrgURL(ts.URL, "/goals", org.Max))
	got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...)
	if want := []string{"Pax's goal", "Max's goal"}; !slices.Equal(got, want) {
		t.Errorf("Max's org: rows = %q, want %q (Pax's Ownerless Goal, not Oz's that Pax is a Delegate on)", got, want)
	}
	if count := goalListCount(t, page); count != "2 in Max Marsh's org" {
		t.Errorf("the count doesn't name Max's org: %q", count)
	}
	orgSel = scopeSelectIn(t, page, "goal-scope-filter")
	if opt := goalScopeOptionIn(t, orgSel, maxID); textOf(opt) != "Max Marsh (max@example.com)" || !hasAttr(opt, "selected") {
		t.Errorf("Max's org isn't the chosen scope: %s", renderNode(orgSel))
	}
}

// With no scope in its address the list opens on the viewer's default: My
// org for someone with anyone under them, Mine for everyone else, each shown
// chosen in the Scope select.
func TestGoalListOpensOnTheViewersDefaultScope(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)

	mia := signInClient(t, ts.URL, "mia@example.com")
	page := getPage(t, mia, ts.URL+"/goals")
	if got, want := goalRowTitlesIn(goalListRowsIn(page), org.goals()...), []string{"Mia's goal", "Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Mia's default: rows = %q, want %q", got, want)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != strconv.FormatInt(org.Mia.ID, 10) || label != "My org" {
		t.Errorf("Mia's default: the Scope select shows %q %q chosen, want My org", value, label)
	}
	if count := goalListCount(t, page); count != "2 in My org" {
		t.Errorf("Mia's default: the count doesn't name My org: %q", count)
	}

	pia := signInClient(t, ts.URL, "pia@example.com")
	page = getPage(t, pia, ts.URL+"/goals")
	if got, want := goalRowTitlesIn(goalListRowsIn(page), org.goals()...), []string{"Pia's goal"}; !slices.Equal(got, want) {
		t.Errorf("Pia's default: rows = %q, want %q", got, want)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != "mine" || label != "Mine" {
		t.Errorf("Pia's default: the Scope select shows %q %q chosen, want Mine", value, label)
	}
}

// An explicit Scope is never overridden by the default: ?org=all is Everyone,
// ?org=mine is Mine and ?org=<id> that person's Org, for a viewer with
// someone under them and one with no one alike. A malformed ?org=, or one
// naming no one, gives the viewer's default, not Everyone.
func TestGoalListKeepsAnExplicitScopeOverTheDefault(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)
	maxID := strconv.FormatInt(org.Max.ID, 10)

	for _, viewer := range []struct {
		email         string
		defaultValue  string
		defaultTitles []string
		mine          string
	}{
		{"mia@example.com", strconv.FormatInt(org.Mia.ID, 10), []string{"Mia's goal", "Pia's goal"}, "Mia's goal"},
		{"pia@example.com", "mine", []string{"Pia's goal"}, "Pia's goal"},
	} {
		client := signInClient(t, ts.URL, viewer.email)
		for _, c := range []struct {
			query, value string
			titles       []string
		}{
			{"org=all", "all", []string{"Max's goal", "Mia's goal", "Oz's goal", "Pax's goal", "Pia's goal", "Vera's goal"}},
			{"org=mine", "mine", []string{viewer.mine}},
			{"org=" + maxID, maxID, []string{"Max's goal", "Pax's goal"}},
			{"org=abc", viewer.defaultValue, viewer.defaultTitles},
			{"org=999999", viewer.defaultValue, viewer.defaultTitles},
			{"org=", viewer.defaultValue, viewer.defaultTitles},
		} {
			page := getPage(t, client, ts.URL+"/goals?"+c.query)
			if got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...); !slices.Equal(got, c.titles) {
				t.Errorf("%s, %s: rows = %q, want %q", viewer.email, c.query, got, c.titles)
			}
			if _, value, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != c.value {
				t.Errorf("%s, %s: the Scope select shows %q chosen, want %q", viewer.email, c.query, value, c.value)
			}
		}
	}
}

// The org lives in the list's address, so it survives grouping, the layout
// toggle and a reload; a ?org= naming no one, or malformed, lists the
// viewer's default.
func TestGoalListKeepsTheOrgInItsAddress(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	org := newScopeOrgFixture(h)
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Core")
	h.AssignGoalValue(org.PiaGoal, team.Values[0])
	h.AssignGoalValue(org.OzGoal, team.Values[0])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")
	orgID := strconv.FormatInt(org.Mia.ID, 10)

	page := getPage(t, client, ts.URL+"/goals?org="+orgID+"&group="+strconv.FormatInt(team.ID, 10))
	if got := textsOf(findAllByTestID(page, "goal-group-label")); !slices.Equal(got, []string{"Core", "Unassigned"}) {
		t.Errorf("grouped in Mia's org, groups = %q", got)
	}
	if got, want := goalRowTitlesIn(goalListRowsIn(page), org.goals()...), []string{"Pia's goal", "Mia's goal"}; !slices.Equal(got, want) {
		t.Errorf("grouped in Mia's org, rows = %q, want %q", got, want)
	}
	if sel := scopeSelectIn(t, page, "goal-scope-filter"); !hasAttr(goalScopeOptionIn(t, sel, orgID), "selected") {
		t.Errorf("grouped, the Scope select lost Mia: %s", renderNode(sel))
	}
	if q := goalListLinkQuery(t, page, "layout-table"); q.Get("org") != orgID {
		t.Errorf("the Table toggle drops the org: %s", q.Encode())
	}

	for _, raw := range []string{"abc", "999999", ""} {
		page := getPage(t, client, ts.URL+"/goals?org="+raw)
		if got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...); !slices.Equal(got, []string{"Oz's goal"}) {
			t.Errorf("org=%q: rows = %q, want Oz's default, Mine", raw, got)
		}
		if _, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != "mine" || label != "Mine" {
			t.Errorf("org=%q: Mine, Oz's default, isn't chosen: %q %q", raw, value, label)
		}
	}
}

// The scope parameter is ?org=; the old ?chain= is no scope at all, ignored
// like any unknown parameter, so the list is on the viewer's default, Vera's
// My org, and not Mia's.
func TestGoalListIgnoresTheOldChainParameter(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vera@example.com")

	unscoped := goalRowTitlesIn(goalListRowsIn(getPage(t, client, ts.URL+"/goals")), org.goals()...)
	page := getPage(t, client, ts.URL+"/goals?chain="+strconv.FormatInt(org.Mia.ID, 10))
	if got := goalRowTitlesIn(goalListRowsIn(page), org.goals()...); !slices.Equal(got, unscoped) || len(got) != 5 {
		t.Errorf("?chain=: rows = %q, want the unscoped %q", got, unscoped)
	}
	sel := scopeSelectIn(t, page, "goal-scope-filter")
	picked := findAll(sel, func(n *nethtml.Node) bool { return n.Data == "option" && hasAttr(n, "selected") })
	if mine := goalScopeOptionIn(t, sel, strconv.FormatInt(org.Vera.ID, 10)); textOf(mine) != "My org" || len(picked) != 1 || picked[0] != mine {
		t.Errorf("?chain=: Vera's default isn't the one chosen scope: %s", renderNode(sel))
	}
}

// An org holding no Goals says so, naming whose org it is.
func TestGoalListSaysAnEmptyOrgIsEmpty(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	org := newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.SetManager(lee, org.Vera)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vera@example.com")

	page := getPage(t, client, scopeOrgURL(ts.URL, "/goals", lee))
	if empty := textOf(findByTestID(t, page, "no-goals")); !strings.Contains(empty, "No Goals in Lee Lund's org.") {
		t.Errorf("an empty org doesn't say so: %q", empty)
	}
	page = getPage(t, client, scopeOrgURL(ts.URL, "/goals", lee)+"&health=red")
	if empty := textOf(findByTestID(t, page, "no-goals")); !strings.Contains(empty, "No Goals in Lee Lund's org match.") {
		t.Errorf("an empty org under another filter doesn't say so: %q", empty)
	}
}

// Everyone is an explicit ?org=all: the Scope select submits it, and a page
// showing Everyone keeps it in the links that keep its filters, since a bare
// /goals is the viewer's default.
func TestGoalListNamesEveryoneAsOrgAll(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "mia@example.com")

	page := getPage(t, client, ts.URL+"/goals?health=red")
	if opt := goalScopeOptionIn(t, scopeSelectIn(t, page, "goal-scope-filter"), "all"); textOf(opt) != "Everyone" || hasAttr(opt, "selected") {
		t.Errorf("the Scope select's Everyone doesn't submit org=all: %s", renderNode(opt))
	}

	page = getPage(t, client, ts.URL+"/goals?org=all")
	for _, id := range []string{"layout-table", "layout-list"} {
		if q := goalListLinkQuery(t, page, id); q.Get("org") != "all" {
			t.Errorf("on Everyone, the %s link drops org=all: %s", id, q.Encode())
		}
	}
}

// An empty default scope says so, naming it, with a link to everyone's Goals
// that keeps the list's other filters, in both layouts.
func TestGoalListSaysAnEmptyDefaultIsEmpty(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	newScopeOrgFixture(h)
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	ned := h.SignInNamed("ned@example.com", "Ned Nash")
	h.SetManager(ned, lee)
	ts := newServer(t, h)

	for _, c := range []struct{ email, empty string }{
		{"lee@example.com", "No Goals in My org."},
		{"ned@example.com", "No Goals in Mine."},
	} {
		client := signInClient(t, ts.URL, c.email)
		for _, path := range []string{"/goals", "/goals?layout=table"} {
			empty := findByTestID(t, getPage(t, client, ts.URL+path), "no-goals")
			if !strings.Contains(textOf(empty), c.empty) {
				t.Errorf("%s, %s: the empty default doesn't say %q: %q", c.email, path, c.empty, textOf(empty))
			}
			link := findByTestID(t, empty, "see-everyones")
			if href, _ := url.Parse(attrOf(link, "href")); href.Path != "/goals" || href.Query().Get("org") != "all" || !strings.Contains(textOf(link), "See everyone's →") {
				t.Errorf("%s, %s: the empty default's link isn't to everyone's Goals: %s", c.email, path, renderNode(link))
			}
		}
	}

	lees := signInClient(t, ts.URL, "lee@example.com")
	empty := findByTestID(t, getPage(t, lees, ts.URL+"/goals?health=red"), "no-goals")
	if !strings.Contains(textOf(empty), "No Goals in My org match.") {
		t.Errorf("an empty default under another filter doesn't say so: %q", textOf(empty))
	}
	href, _ := url.Parse(attrOf(findByTestID(t, empty, "see-everyones"), "href"))
	if q := href.Query(); q.Get("org") != "all" || q.Get("health") != "red" {
		t.Errorf("the link to everyone's Goals drops the other filters: %s", href)
	}

	if hasTestID(getPage(t, lees, scopeOrgURL(ts.URL, "/goals", lee)), "see-everyones") {
		t.Errorf("an explicitly chosen empty org links to everyone's Goals")
	}
}

// With My org offered and no one else's org holding Goals, the Scope select
// has no Other orgs group.
func TestGoalListScopeOffersNoOtherOrgsWhenNoneHoldGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	vera := h.SignInNamed("vera@example.com", "Vera Vance")
	lee := h.SignInNamed("lee@example.com", "Lee Lund")
	h.SetManager(lee, vera)
	h.ActiveGoal(vera, "Vera's goal", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vera@example.com")

	sel := scopeSelectIn(t, getPage(t, client, ts.URL+"/goals"), "goal-scope-filter")
	groups := findAll(sel, func(n *nethtml.Node) bool { return n.Data == "optgroup" })
	if choices, _, _ := selectChoices(sel); !slices.Equal(choices, []string{"Everyone", "Mine", "My org"}) || len(groups) != 0 {
		t.Errorf("with no other org holding Goals the Scope select offers %q: %s", choices, renderNode(sel))
	}
}

// With no Managers anywhere, as when no directory is synced, the Scope select
// offers no org, only Everyone and Mine, and a ?org= link names no one, so
// gives the viewer's default, Mine.
func TestGoalListOffersNoOrgWithoutManagers(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.CreateGoal(sam, "Sam's", "It matters.")
	h.CreateGoal(kim, "Kim's", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals?org="+strconv.FormatInt(kim.ID, 10))
	if choices, _, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); !slices.Equal(choices, []string{"Everyone", "Mine"}) {
		t.Errorf("without Managers the Scope select offers %q, want only Everyone and Mine", choices)
	}
	if got := len(goalListRowsIn(page)); got != 1 {
		t.Errorf("without Managers a ?org= scoped the list to %d Goals, want Mine's 1", got)
	}
	if _, value, _ := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != "mine" {
		t.Errorf("without Managers a ?org= chose %q, want the default Mine", value)
	}
}

// Whether the Goals pages offer Orgs is the company's answer, not the viewer's:
// with Vera managing Mia, Oz, who has no Manager and no one under them, can
// open a link to Vera's org though no Goal on the page has an Owner with a
// Manager, and sees it scoped and chosen.
func TestGoalListScopesAnOrgLinkForAViewerOutsideAnyOrg(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	vera := h.SignInNamed("vera@example.com", "Vera Vance")
	mia := h.SignInNamed("mia@example.com", "Mia Moss")
	oz := h.SignInNamed("oz@example.com", "Oz Ortiz")
	h.SetManager(mia, vera)
	veraGoal := h.ActiveGoal(vera, "Vera's goal", "It matters.")
	ozGoal := h.ActiveGoal(oz, "Oz's goal", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "oz@example.com")

	page := getPage(t, client, scopeOrgURL(ts.URL, "/goals", vera))
	if got, want := goalRowTitlesIn(goalListRowsIn(page), veraGoal, ozGoal), []string{"Vera's goal"}; !slices.Equal(got, want) {
		t.Errorf("Oz in Vera's org: rows = %q, want %q", got, want)
	}
	if _, value, label := selectChoices(scopeSelectIn(t, page, "goal-scope-filter")); value != strconv.FormatInt(vera.ID, 10) || label != "Vera Vance (vera@example.com)" {
		t.Errorf("Oz in Vera's org: the Scope select shows %q %q chosen, want Vera's org", value, label)
	}
}

// The Dimension filters and Group by fold under More filters, which opens when
// either is set; grouping gives each value its own table section headed by a
// row naming it, sorted problems first within.
func TestGoalListFoldsDimensionFiltersAndGroupsIntoSections(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	growth := pillar.Values[0]
	calm := h.ActiveGoal(sam, "Calm growth", "It matters.")
	h.Checkin(sam, calm.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	fire := h.ActiveGoal(sam, "Burning growth", "It matters.")
	h.Checkin(sam, fire.ID, domain.HealthRed, "On fire.", "Put it out.", testsupport.Epoch.AddDate(0, 2, 0))
	loose := h.CreateGoal(sam, "Loose end", "It matters.")
	h.AssignGoalValue(calm, growth)
	h.AssignGoalValue(fire, growth)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	plain := getPage(t, client, ts.URL+"/goals")
	if more := findByTestID(t, plain, "more-filters"); hasAttr(more, "open") {
		t.Errorf("More filters is open with none set: %s", renderNode(more))
	}

	filtered := getPage(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, growth.ID))
	if more := findByTestID(t, filtered, "more-filters"); !hasAttr(more, "open") {
		t.Errorf("More filters is closed with a value chosen: %s", renderNode(more))
	}

	// Group by shows among the filter bar's chips, so grouping alone leaves
	// More filters closed.
	grouped := getPage(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if more := findByTestID(t, grouped, "more-filters"); hasAttr(more, "open") {
		t.Errorf("More filters opens for a grouping alone: %s", renderNode(more))
	}
	sections := findAll(grouped, func(n *nethtml.Node) bool { return n.Data == "tbody" && attrOf(n, "data-testid") == "goal-group" })
	if len(sections) == 0 {
		t.Fatalf("the grouped list has no sections: %s", renderNode(grouped))
	}
	section := sections[0]
	if label := textOf(findByTestID(t, section, "goal-group-label")); !strings.Contains(label, "Growth") {
		t.Errorf("first section is not headed Growth: %q", label)
	}
	if got := goalRowTitlesIn(goalListRowsIn(section), calm, fire, loose); !slices.Equal(got, []string{"Burning growth", "Calm growth"}) {
		t.Errorf("Growth section rows = %q, want the Red Goal first", got)
	}
	if labels := textsOf(findAllByTestID(grouped, "goal-group-label")); !slices.Contains(labels, "Unassigned") {
		t.Errorf("Goals without a Pillar have no Unassigned section; sections %q", labels)
	}

	empty := getPage(t, client, ts.URL+"/goals?org=all&q=nothing+like+it")
	if none := textOf(findByTestID(t, empty, "no-goals")); !strings.Contains(none, "No Goals match.") {
		t.Errorf("a filtered-out list does not say nothing matches: %q", none)
	}
}

// Clear is offered only when the filter bar differs from a bare /goals: a
// Scope other than the viewer's default, search text, Health, Lifecycle,
// Incomplete, a Dimension value or a grouping. It links to the bare /goals
// (#376).
func TestGoalListOffersClearOnlyWhenAFilterIsSet(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/goals", "/goals?org=mine", "/goals?q=&health=&lifecycle=&group="} {
		if page := getPage(t, client, ts.URL+path); hasTestID(page, "goal-filters-clear") {
			t.Errorf("%s: Clear is offered with no filter set:\n%s", path, renderNode(findByTestID(t, page, "goal-filter-bar")))
		}
	}
	for _, path := range []string{
		"/goals?org=all",
		"/goals?q=grow",
		"/goals?health=red",
		"/goals?lifecycle=Active",
		"/goals?incomplete=1",
		fmt.Sprintf("/goals?value=%d", pillar.Values[0].ID),
		fmt.Sprintf("/goals?group=%d", pillar.ID),
	} {
		page := getPage(t, client, ts.URL+path)
		if !hasTestID(page, "goal-filters-clear") {
			t.Errorf("%s: Clear isn't offered with a filter set:\n%s", path, renderNode(findByTestID(t, page, "goal-filter-bar")))
			continue
		}
		if href := attrOf(findByTestID(t, page, "goal-filters-clear"), "href"); href != "/goals" {
			t.Errorf("%s: Clear links to %q, want the bare /goals", path, href)
		}
	}

	// With someone under them, the viewer's default is My org, so naming it
	// is no filter, while Mine is.
	h.SetManager(sam, boss)
	bossClient := signInClient(t, ts.URL, "boss@example.com")
	if page := getPage(t, bossClient, fmt.Sprintf("%s/goals?org=%d", ts.URL, boss.ID)); hasTestID(page, "goal-filters-clear") {
		t.Errorf("My org, the default, offers Clear")
	}
	if page := getPage(t, bossClient, ts.URL+"/goals?org=mine"); !hasTestID(page, "goal-filters-clear") {
		t.Errorf("Mine, for someone whose default is My org, doesn't offer Clear")
	}
}

// goalClearSlotIn returns the Clear slot on page and whether it is swapped
// out of band, failing unless it is the only element swapped out of band, so
// htmx replaces neither the form nor More filters, and it has an id to land
// on.
func goalClearSlotIn(t *testing.T, page *nethtml.Node) (*nethtml.Node, bool) {
	t.Helper()
	slot := findByTestID(t, page, "goal-filters-clear-slot")
	oob := findAll(page, func(n *nethtml.Node) bool { return hasAttr(n, "hx-swap-oob") })
	switch {
	case len(oob) == 0:
		return slot, false
	case len(oob) > 1 || oob[0] != slot:
		var swapped []string
		for _, n := range oob {
			swapped = append(swapped, renderNode(n))
		}
		t.Fatalf("out-of-band swaps other than the Clear slot: %q", swapped)
	}
	if attrOf(slot, "id") == "" {
		t.Fatalf("the Clear slot's out-of-band swap has no id to land on: %s", renderNode(slot))
	}
	return slot, true
}

// goalListFilterChangeUnderHTMX submits the Goal list's filter form on page
// as htmx does on a change, with set applied to what it would post, and
// returns the parsed response and the address asked for.
func goalListFilterChangeUnderHTMX(t *testing.T, client *http.Client, base string, page *nethtml.Node, set func(url.Values)) (*nethtml.Node, string) {
	t.Helper()
	form := findByTestID(t, page, "goal-filters")
	values := formSubmission(form)
	set(values)
	address := attrOf(form, "hx-get") + "?" + values.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+address, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "goal-list")
	req.Header.Set("HX-Current-URL", base+"/goals")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d:\n%s", address, resp.StatusCode, body)
	}
	return parsePage(t, body), address
}

// A filter changed under htmx swaps in only the list, so its response carries
// the Clear slot out of band: Clear when the address loaded in full would
// offer it, an empty slot when it wouldn't (#385). A full load swaps nothing
// out of band.
func TestGoalListFilterChangeUnderHTMXKeepsClearInStep(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	bare := getPage(t, client, ts.URL+"/goals")
	if slot, oob := goalClearSlotIn(t, bare); oob || hasTestID(slot, "goal-filters-clear") {
		t.Errorf("a full load of /goals swaps the Clear slot out of band or offers Clear: %s", renderNode(slot))
	}
	for name, set := range map[string]func(url.Values){
		"Scope":      func(v url.Values) { v.Set("org", "all") },
		"search":     func(v url.Values) { v.Set("q", "grow") },
		"Health":     func(v url.Values) { v.Set("health", "red") },
		"Lifecycle":  func(v url.Values) { v.Set("lifecycle", "Active") },
		"Incomplete": func(v url.Values) { v.Set("incomplete", "1") },
		"Dimension":  func(v url.Values) { v.Add("value", fmt.Sprint(pillar.Values[0].ID)) },
		"Group by":   func(v url.Values) { v.Set("group", fmt.Sprint(pillar.ID)) },
	} {
		body, address := goalListFilterChangeUnderHTMX(t, client, ts.URL, bare, set)
		slot, oob := goalClearSlotIn(t, body)
		if !oob {
			t.Errorf("%s set under htmx (%s): the Clear slot isn't swapped out of band", name, address)
			continue
		}
		if !hasTestID(slot, "goal-filters-clear") {
			t.Errorf("%s set under htmx (%s): the Clear slot lacks Clear: %s", name, address, renderNode(slot))
			continue
		}
		if href := attrOf(findByTestID(t, slot, "goal-filters-clear"), "href"); href != "/goals" {
			t.Errorf("%s set under htmx (%s): Clear links to %q, want the bare /goals", name, address, href)
		}
	}

	filtered := getPage(t, client, ts.URL+"/goals?health=red")
	if slot, oob := goalClearSlotIn(t, filtered); oob || !hasTestID(slot, "goal-filters-clear") {
		t.Errorf("a full load of /goals?health=red swaps the Clear slot out of band or lacks Clear: %s", renderNode(slot))
	}
	body, address := goalListFilterChangeUnderHTMX(t, client, ts.URL, filtered, func(v url.Values) { v.Set("health", "") })
	slot, oob := goalClearSlotIn(t, body)
	if !oob {
		t.Errorf("the last filter unset under htmx (%s): the Clear slot isn't swapped out of band", address)
	}
	for c := slot.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != nethtml.TextNode || strings.TrimSpace(c.Data) != "" {
			t.Errorf("the last filter unset under htmx (%s): the Clear slot isn't empty: %s", address, renderNode(slot))
			break
		}
	}
}

// goalFilterBarControlsIn returns the Goal list filter form's controls, in
// the order its markup runs: the Scope, Health and Lifecycle selects, the
// Incomplete checkbox, More filters and its summary, Group by, the Apply
// fallback, Clear, the search, and the Dimension filters' legend for
// dimension.
func goalFilterBarControlsIn(t *testing.T, form *nethtml.Node, dimension string) map[string]*nethtml.Node {
	t.Helper()
	one := func(what string, match func(*nethtml.Node) bool) *nethtml.Node {
		found := findAll(form, match)
		if len(found) != 1 {
			t.Fatalf("the filter form has %d %s, want 1: %s", len(found), what, renderNode(form))
		}
		return found[0]
	}
	controls := map[string]*nethtml.Node{}
	for _, id := range []string{"goal-scope-filter", "goal-health-filter", "goal-lifecycle-filter", "goal-incomplete-filter", "more-filters", "goal-search"} {
		controls[id] = findByTestID(t, form, id)
	}
	controls["summary"] = one("summaries", func(n *nethtml.Node) bool { return n.Data == "summary" })
	if hasTestID(form, "goal-filters-clear") {
		controls["goal-filters-clear"] = findByTestID(t, form, "goal-filters-clear")
	}
	if fields := formFields(form, "group"); len(fields) > 0 && fields[0].Type == "select" {
		controls["group"] = fields[0].Node
	}
	if noscripts := findAll(form, func(n *nethtml.Node) bool { return n.Data == "noscript" }); len(noscripts) > 0 {
		controls["noscript"] = noscripts[0]
	}
	if dimension != "" {
		controls["legend"] = one("legends for "+dimension, func(n *nethtml.Node) bool { return n.Data == "legend" && textOf(n) == dimension })
	}
	return controls
}

// The filter bar is one row: the Scope, Health, Lifecycle, Incomplete, More
// filters' summary, Clear and the search, in that order. More filters' panel,
// the Dimension filters, follows the row rather than sitting in it, so
// opening it adds a row below (#376).
func TestGoalFilterBarHoldsMoreFiltersSummaryInItsRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals?health=red")
	form := findByTestID(t, page, "goal-filters")
	row := findByTestID(t, form, "goal-filter-bar")
	controls := goalFilterBarControlsIn(t, form, "Pillar")
	var prev *nethtml.Node
	for _, part := range []string{
		"goal-scope-filter",
		"goal-health-filter",
		"goal-lifecycle-filter",
		"goal-incomplete-filter",
		"more-filters",
		"summary",
		"goal-filters-clear",
		"goal-search",
	} {
		n := controls[part]
		if n == nil || !within(n, row) || prev != nil && !precedes(prev, n) {
			t.Fatalf("the filter row doesn't hold %s after what comes before it:\n%s", part, renderNode(row))
		}
		prev = n
	}
	if fieldsets := findAll(row, func(n *nethtml.Node) bool { return n.Data == "fieldset" }); len(fieldsets) > 0 {
		t.Errorf("More filters' panel sits in the filter row:\n%s", renderNode(row))
	}

	panel := findByTestID(t, form, "more-filters-panel")
	if within(panel, row) || !precedes(row, panel) {
		t.Errorf("the panel doesn't follow the filter row:\n%s", renderNode(form))
	}
	if !within(controls["legend"], panel) {
		t.Errorf("the panel after the row doesn't hold the Dimension filters:\n%s", renderNode(panel))
	}
	summary := controls["summary"]
	if c := attrOf(summary, "aria-controls"); c == "" || attrOf(panel, "id") != c {
		t.Errorf("More filters' summary doesn't name the panel it shows: %s, %s", renderNode(summary), attrOf(panel, "id"))
	}
}

// Tab follows the filter bar as it is drawn (WCAG 2.4.3): the markup runs
// Scope, Health, Lifecycle, Incomplete, More filters' summary, Group by, Apply
// and Clear, then the search at the row's right, then the open panel's
// Dimension filters on the line below. No CSS order redraws it
// (TestGoalFilterBarTabOrderFollowsItsDrawnOrderStyles) (#404).
func TestGoalFilterBarTabOrderFollowsItsDrawnOrder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	h.CreateGoal(sam, "Grow revenue", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, pillar.Values[0].ID))
	form := findByTestID(t, page, "goal-filters")
	controls := goalFilterBarControlsIn(t, form, "Pillar")
	var prev *nethtml.Node
	for _, part := range []string{
		"goal-scope-filter",
		"goal-health-filter",
		"goal-lifecycle-filter",
		"goal-incomplete-filter",
		"summary",
		"group",
		"noscript",
		"goal-filters-clear",
		"goal-search",
		"legend",
	} {
		n := controls[part]
		if n == nil || prev != nil && !precedes(prev, n) {
			t.Fatalf("the filter bar's markup doesn't hold %s after what is drawn before it:\n%s", part, renderNode(form))
		}
		prev = n
	}
}

// The Goal list's header strip names the page with the count of the Goals it
// lists, then offers the List / Table switch and New goal, linking to the New
// goal page (DESIGN.md § Goal list); how they look is
// TestGoalListHeaderOpensProposeFormThatSwapsTheListStyles's. The Follow-ups
// page has replaced the links to the signal pages.
func TestGoalListHeaderOpensProposeFormThatSwapsTheList(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals")
	head := findByTestID(t, page, "goals-head")
	if h1 := headingNamed(head, "Goals"); h1 == nil || h1.Data != "h1" {
		t.Errorf("header lacks the h1 Goals: %s", renderNode(head))
	}
	if count := findByTestID(t, head, "goal-count"); count.Data != "span" || attrOf(count, "id") != "goal-count" || textOf(count) != "0" {
		t.Errorf("header's count isn't the goal-count reading 0: %s", renderNode(count))
	}
	layouts := findAll(head, func(n *nethtml.Node) bool { return n.Data == "nav" && attrOf(n, "aria-label") == "Layout" })
	if len(layouts) != 1 || attrOf(layouts[0], "id") != "goal-layout" {
		t.Errorf("header lacks the Layout switch, #goal-layout: %s", renderNode(head))
	}
	newGoal := findByTestID(t, head, "new-goal")
	if newGoal.Data != "a" || attrOf(newGoal, "href") != "/goals/new" || textOf(newGoal) != "New goal" {
		t.Errorf("New goal is not a link to /goals/new: %s", renderNode(newGoal))
	}
	for _, gone := range []string{"/signals", "/freshness"} {
		if linkTo(page, gone) != nil {
			t.Errorf("Goal list still links %s", gone)
		}
	}
}

// goalRailPropertiesIn reads the Goal page rail's label and value rows: each
// row's label (its <dt>) in order, and the value (its <dd>) under each label.
func goalRailPropertiesIn(t *testing.T, root *nethtml.Node) (labels []string, values map[string]*nethtml.Node) {
	t.Helper()
	values = map[string]*nethtml.Node{}
	for _, dt := range findAll(findByTestID(t, root, "goal-sidebar"), func(n *nethtml.Node) bool { return n.Data == "dt" }) {
		label := textOf(dt)
		labels = append(labels, label)
		for dd := dt.NextSibling; dd != nil; dd = dd.NextSibling {
			if dd.Type == nethtml.ElementNode {
				if dd.Data == "dd" {
					values[label] = dd
				}
				break
			}
		}
	}
	return labels, values
}

// goalMilestoneRowsIn reads each of the Goal page's Milestone rows under root,
// in order, every word of it as elementTexts reads a row.
func goalMilestoneRowsIn(root *nethtml.Node) []string {
	var rows []string
	for _, tr := range findAllByTestID(root, "goal-milestone") {
		if tr.Data == "tr" {
			rows = append(rows, strings.Join(nodeWords(tr, tr, nil), " "))
		}
	}
	return rows
}

// The Goal page's document column opens on its title, then its Why it matters
// as a lead paragraph. Its properties sit in the rail as label and value rows,
// in DESIGN.md § Properties rail's order: Health as its pill, Lifecycle, Kind,
// Owner, Delegates, the cadence, the delivery date with its slips struck,
// Top-level as the level, then the Dimension values. The header strip holds
// the breadcrumb, with Check in and No change at its right for whoever may
// check in. How the title, the lead, the rail and the dates look is
// TestGoalPageRailHoldsTheGoalsPropertiesStyles's.
func TestGoalPageRailHoldsTheGoalsProperties(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	growth := h.CreateDimension(ada, "Pillar", "Growth").Values[0]
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, growth)
	h.MarkTopLevel(ada, goal)
	h.AddDelegate(sam, kim, goal.ID)
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID:             goal.ID,
		AuthorID:           sam.ID,
		Health:             domain.HealthYellow,
		Status:             "Later than planned.",
		PathToGreen:        "Swap vendors.",
		PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
		DeliveryDate:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		DeliveryDateReason: "Vendor slipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin: %v", err)
	}
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	head := findByTestID(t, root, "goal-head")
	title, whyItMatters := findByTestID(t, head, "goal-title"), findByTestID(t, head, "goal-why-it-matters")
	if !precedes(title, whyItMatters) {
		t.Errorf("the column should open on the title, then Why it matters")
	}
	if title.Data != "h1" || textOf(title) != "Reduce outages" {
		t.Errorf("title is <%s> reading %q, want the h1 Reduce outages", title.Data, textOf(title))
	}
	if whyItMatters.Data != "p" || textOf(whyItMatters) != "Outages cost trust." {
		t.Errorf("Why it matters is <%s> reading %q, want a paragraph", whyItMatters.Data, textOf(whyItMatters))
	}

	rail := findByTestID(t, root, "goal-sidebar")
	if strings.Contains(textOf(head), "Growth") || !strings.Contains(textOf(rail), "Growth") {
		t.Errorf("the Dimension value isn't in the rail, out of the column's head")
	}
	labels, props := goalRailPropertiesIn(t, root)
	if want := []string{"Health", "Lifecycle", "Kind", "Owner", "Cadence", "Delivers", "Level", "Pillar"}; !slices.Equal(labels, want) {
		t.Errorf("the rail's property rows read %q, want %q", labels, want)
	}
	for _, label := range labels {
		if props[label] == nil {
			t.Fatalf("the rail's %s row has no value", label)
		}
	}
	// The Health property is a chip with no title beside it: its shape, the
	// word for a screen reader and as its tooltip.
	assertHealthChip(t, healthChipIn(t, props["Health"]), "Yellow")
	if got := findAllByTestID(props["Lifecycle"], "goal-lifecycle"); len(got) != 1 || textOf(props["Lifecycle"]) != "Active" || textOf(got[0]) != "Active" {
		t.Errorf("the Lifecycle row reads %q, want Active: %s", textOf(props["Lifecycle"]), renderNode(props["Lifecycle"]))
	}
	kind := findByTestID(t, props["Kind"], "goal-kind")
	if button := buttonNamed(kind, "Dated"); button == nil ||
		!flatFrameHasClass(button, "disclose") || !flatFrameHasClass(button, "help") ||
		attrOf(button, "type") != "button" || attrOf(button, "aria-expanded") != "false" || attrOf(button, "aria-controls") != "kind-help" ||
		attrOf(button, "title") != "Has a delivery date. Its Health is judged against that date and its Milestones." {
		t.Errorf("the Kind row isn't a collapsed Dated help button explaining Dated: %s", renderNode(kind))
	}
	if owner := findByTestID(t, props["Owner"], "goal-owner"); owner.Data != "strong" || !goalShows(owner, "sam@example.com", "sam") {
		t.Errorf("the Owner row doesn't show sam, their email a click away, in bold: %s", renderNode(props["Owner"]))
	}
	if cadence := findByTestID(t, props["Cadence"], "goal-cadence"); textOf(props["Cadence"]) != "every 7 days" || textOf(cadence) != "every 7 days" {
		t.Errorf("the Cadence row reads %q, want every 7 days", textOf(props["Cadence"]))
	}
	delivers := findByTestID(t, props["Delivers"], "goal-delivery-date")
	strong := textsOf(findAll(delivers, func(n *nethtml.Node) bool { return n.Data == "strong" }))
	if struck := struckThrough(delivers); textOf(delivers) != "Jul 2 Jul 16" || !slices.Equal(struck, []string{"Jul 2"}) || !slices.Equal(strong, []string{"Jul 16"}) {
		t.Errorf("the Delivers row reads %q with %q struck and %q emphasised, want Jul 2 struck before Jul 16 in bold", textOf(delivers), struck, strong)
	}
	if level := findByTestID(t, props["Level"], "goal-top-level"); level.Data != "button" || textOf(level) != "Top-level" ||
		!flatFrameHasClass(level, "disclose") || !flatFrameHasClass(level, "help") || attrOf(level, "type") != "button" {
		t.Errorf("the Level row isn't the Top-level help button: %s", renderNode(props["Level"]))
	}
	if value := findByTestID(t, props["Pillar"], "goal-dimension-value"); value.Data != "span" || textOf(value) != "Growth" {
		t.Errorf("the Pillar row doesn't hold Growth: %s", renderNode(props["Pillar"]))
	}
	owner, delegates, cadence := findByTestID(t, rail, "goal-owner"), findByTestID(t, rail, "goal-delegates"), findByTestID(t, rail, "goal-cadence")
	if !precedes(owner, delegates) || !precedes(delegates, cadence) {
		t.Errorf("the rail should read Owner, Delegates, cadence in that order")
	}
	if !goalShows(findByTestID(t, rail, "delegate-list"), "kim@example.com", "kim") {
		t.Errorf("the rail's Delegates row lacks kim")
	}

	strip := findByTestID(t, root, "goal-page-header")
	if !within(findByTestID(t, root, "goal-breadcrumb"), strip) {
		t.Errorf("the header strip has no breadcrumb: %s", renderNode(strip))
	}
	actions, more := findByTestID(t, root, "goal-actions"), findByTestID(t, root, "goal-more")
	if link := linkTo(actions, fmt.Sprintf("/goals/%d/checkin", goal.ID)); link == nil || !precedes(link, more) || attrOf(link, "data-testid") != "checkin-link" {
		t.Errorf("header strip has no Check in link before its menu: %s", renderNode(actions))
	}
	if noChange := findAllByTestID(actions, "no-change-checkin"); len(noChange) != 1 || !precedes(noChange[0], more) {
		t.Errorf("header strip has no No change button: %s", renderNode(actions))
	}
	if !precedes(actions, findByTestID(t, root, "goal-main")) {
		t.Errorf("Check in is not in the header strip above the column")
	}
	if n := len(findAllByTestID(root, "checkin-link")); n != 1 {
		t.Errorf("the Check in link appears %d times, want once, in the header strip", n)
	}
}

// A Goal that has no Health, no delivery date and isn't Top-level says only
// what it has in its rail: Health reads No Health, and there is no Delivers
// or Level row.
func TestGoalPageRailShowsOnlyWhatTheGoalHas(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Tidy the backlog", "Nobody can find anything.")
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	meta := findByTestID(t, root, "goal-meta")
	_, props := goalRailPropertiesIn(t, root)
	if health := props["Health"]; health == nil || textOf(health) != "No Health" || !within(health, meta) {
		t.Errorf("the rail's Health row doesn't read No Health: %s", renderNode(meta))
	}
	for testID, want := range map[string]string{"goal-lifecycle": "Proposed", "goal-kind": "Not yet Dated or Ongoing"} {
		if got := textOf(findByTestID(t, meta, testID)); got != want {
			t.Errorf("the rail's %s reads %q, want %q", testID, got, want)
		}
	}
	for _, testID := range []string{"goal-owner", "goal-cadence"} {
		if !hasTestID(meta, testID) {
			t.Errorf("the rail lacks %s: %s", testID, renderNode(meta))
		}
	}
	if badges := findAll(meta, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "badge") }); len(badges) > 0 {
		t.Errorf("the rail shows a badge for a Proposed Goal: %s", renderNode(badges[0]))
	}
	for _, testID := range []string{"goal-delivery-date", "goal-top-level"} {
		if hasTestID(meta, testID) {
			t.Errorf("the rail shows %s for an undated, non-Top-level Goal: %s", testID, renderNode(meta))
		}
	}
	all := strings.Join(nodeWords(meta, meta, nil), " ")
	for _, absent := range []string{"Delivers", "Top-level", "Level"} {
		if strings.Contains(all, absent) {
			t.Errorf("the rail says %s for a Proposed, undated, non-Top-level Goal: %s", absent, all)
		}
	}
}

// The Goal page's Milestones are a table of mark, date and name, headed by the
// Goal's slip count and Milestone Churn. A Milestone on track has no mark.
func TestGoalPageMilestonesTable(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)

	section := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-milestones")
	slips, churn := findByTestID(t, section, "goal-slip-count"), findByTestID(t, section, "goal-milestone-churn")
	if textOf(slips) != "0" || textOf(churn) != "0" || slips.Parent != churn.Parent {
		t.Errorf("Milestones' slip count reads %q and churn %q, want 0 and 0 side by side: %s", textOf(slips), textOf(churn), renderNode(section))
	} else if got, want := textOf(slips.Parent), "0 slips · 0 added or removed since Active"; got != want {
		t.Errorf("Milestones are headed %q, want %q", got, want)
	}
	if tables := findAll(section, func(n *nethtml.Node) bool { return n.Data == "table" }); len(tables) != 1 {
		t.Errorf("Milestones hold %d tables, want 1: %s", len(tables), renderNode(section))
	}
	if got := goalMilestoneRowsIn(section); !slices.Equal(got, []string{"Apr 2 Beta"}) {
		t.Errorf("Milestone rows read %q, want Beta's date and name with no mark", got)
	}
	if hasTestID(section, "milestone-mark") {
		t.Errorf("an on-track Milestone carries a mark: %s", renderNode(section))
	}
}

// The Goal page marks a Milestone overdue by today's date in the Goal's
// Owner's Timezone, as the No change it offers beside the mark is judged
// (CONTEXT.md: Timezone): with the company on UTC, late on Beta's date in the
// Owner's Los Angeles Beta has no Overdue mark and No change is offered; early
// the next day there, Beta is Overdue and No change is not.
func TestGoalPageMarksMilestoneOverdueInTheOwnersTimezoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SetTimezone(sam, "America/Los_Angeles")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	load := func() (root *nethtml.Node, beta string) {
		t.Helper()
		root = getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
		rows := goalMilestoneRowsIn(findByTestID(t, root, "goal-milestones"))
		if len(rows) != 1 {
			t.Fatalf("Milestone rows read %q, want only Beta's", rows)
		}
		return root, rows[0]
	}

	h.Clock.Set(time.Date(2026, 4, 3, 2, 0, 0, 0, time.UTC)) // Apr 2, 7pm in Los Angeles
	// The session from before the clock moved has ended; sign in again.
	client = signInClient(t, ts.URL, "sam@example.com")
	root, beta := load()
	if beta != "Apr 2 Beta" {
		t.Errorf("Beta's row reads %q on its date in the Owner's timezone, want no mark", beta)
	}
	if hasTestID(root, "no-change-unavailable") && strings.Contains(strings.Join(nodeWords(root, root, nil), " "), `Milestone "Beta" is overdue`) {
		t.Errorf("the Goal page refuses No change for Beta on its date in the Owner's timezone")
	}
	if !hasTestID(findByTestID(t, root, "goal-actions"), "no-change-checkin") {
		t.Errorf("the Goal page doesn't offer No change on Beta's date in the Owner's timezone")
	}

	h.Clock.Set(time.Date(2026, 4, 3, 8, 0, 0, 0, time.UTC)) // Apr 3, 1am in Los Angeles
	root, beta = load()
	if beta != "Overdue Apr 2 Beta" {
		t.Errorf("Beta's row reads %q the day after its date in the Owner's timezone, want it marked Overdue", beta)
	}
	if hasTestID(findByTestID(t, root, "goal-actions"), "no-change-checkin") {
		t.Errorf("the Goal page offers No change with Beta overdue in the Owner's timezone")
	}
}

// The Owner or a Delegate adds a Milestone from the Goal page's Milestones
// card while the Goal is Proposed, Active or On Hold: + Add opens an Add
// Milestone form in place, and posting it reloads the Goal page with the new
// Milestone listed (CONTEXT.md: Milestone).
func TestGoalPageAddsAMilestoneInPlaceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	onHold := h.OnHoldGoal(sam, "Migrate billing", "Billing is brittle.", "Waiting on finance.")
	for _, g := range []domain.Goal{proposed, active, onHold} {
		h.AddDelegate(sam, dana, g.ID)
	}
	ts := newServer(t, h)

	churn := func(card *nethtml.Node) string {
		return textOf(findByTestID(t, card, "goal-milestone-churn"))
	}
	// Only an addition while Active is Milestone Churn (CONTEXT.md: Milestone
	// Churn).
	for _, tc := range []struct {
		who                     string
		goal                    domain.Goal
		churnBefore, churnAfter string
	}{
		{"sam@example.com", proposed, "0", "0"},
		{"dana@example.com", proposed, "0", "0"},
		{"sam@example.com", active, "0", "1"},
		{"dana@example.com", active, "1", "2"},
		{"sam@example.com", onHold, "0", "0"},
		{"dana@example.com", onHold, "0", "0"},
	} {
		client := signInClient(t, ts.URL, tc.who)
		card := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, tc.goal)), "goal-milestones")
		if got := churn(card); got != tc.churnBefore {
			t.Errorf("%s on %s: churn before reads %q, want %s", tc.who, tc.goal.Lifecycle, got, tc.churnBefore)
		}
		link := findByTestID(t, card, "open-milestones")
		root := getPage(t, client, ts.URL+attrOf(link, "href"))
		open := goalOpenFormIn(t, root, "milestones")
		if !within(open, findByTestID(t, root, "goal-milestones")) {
			t.Errorf("%s on %s: the Add Milestone form doesn't open in the Milestones section", tc.who, tc.goal.Lifecycle)
		}
		forms := findAll(open, func(n *nethtml.Node) bool { return n.Data == "form" })
		if len(forms) != 1 || attrOf(forms[0], "action") != fmt.Sprintf("/goals/%d/milestones", tc.goal.ID) {
			t.Errorf("%s on %s: the Add Milestone form doesn't post to the Goal's Milestones: %s", tc.who, tc.goal.Lifecycle, renderNode(open))
		} else if names, dates := formFields(forms[0], "name"), formFields(forms[0], "target_date"); len(names) != 1 || len(dates) != 1 || dates[0].Type != "date" {
			t.Errorf("%s on %s: the Add Milestone form lacks a name and a target date: %s", tc.who, tc.goal.Lifecycle, renderNode(open))
		}

		name := "Added by " + tc.who
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, tc.goal.ID), url.Values{"name": {name}, "target_date": {"2026-05-01"}})
		defer func() { _ = resp.Body.Close() }()
		root = parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", tc.goal.ID) {
			t.Fatalf("%s on %s: add answered %d at %s, want the Goal page", tc.who, tc.goal.Lifecycle, resp.StatusCode, resp.Request.URL)
		}
		card = findByTestID(t, root, "goal-milestones")
		rows := goalMilestoneRowsIn(card)
		if !slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "May 1 "+name) }) {
			t.Errorf("%s on %s: Milestones read %q, want %s listed", tc.who, tc.goal.Lifecycle, rows, name)
		}
		if got := churn(card); got != tc.churnAfter {
			t.Errorf("%s on %s: churn after reads %q, want %s", tc.who, tc.goal.Lifecycle, got, tc.churnAfter)
		}
	}
}

// Only the Goal's Owner or a Delegate may add or edit its Milestones: anyone
// else is refused with 403 by both routes and nothing changes, and the Goal
// page offers them no Add Milestone form. A Done or Cancelled Goal takes no
// new Milestone, so even its Owner is offered no form and is refused.
func TestOnlyTheOwnerOrADelegateChangesMilestonesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("ada@example.com")
	h.SignIn("pat@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	if _, err := h.Service.AddMilestoneAsAuthor(t.Context(), proposed.Owner.ID, domain.AddMilestoneInput{
		GoalID: proposed.ID, Name: "Pricing page", TargetDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	rows := func(g domain.Goal) []string {
		t.Helper()
		ms, err := h.Service.ListMilestones(t.Context(), g.ID)
		if err != nil {
			t.Fatalf("ListMilestones: %v", err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.Name+" "+m.TargetDate.Format("2006-01-02"))
		}
		return out
	}
	offersAddMilestone := func(root *nethtml.Node) bool {
		return hasTestID(root, "open-milestones") || hasTestID(root, "add-milestone")
	}

	for _, who := range []string{"pat@example.com", "ada@example.com"} {
		client := signInClient(t, ts.URL, who)
		for _, g := range []domain.Goal{proposed, active} {
			before := rows(g)
			resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, g.ID), url.Values{"name": {"Sneaky"}, "target_date": {"2026-05-01"}})
			defer func() { _ = resp.Body.Close() }()
			reason := textOf(findByTestID(t, parsePage(t, readBody(t, resp)), "refused-reason"))
			if resp.StatusCode != http.StatusForbidden || reason != "Only the Owner or a Delegate may change this Goal's Milestones." {
				t.Errorf("%s adding to %s: %d %q, want 403 with the reason", who, g.Lifecycle, resp.StatusCode, reason)
			}
			m := onlyMilestone(t, h, g)
			resp = postForm(t, client, fmt.Sprintf("%s/milestones/%d", ts.URL, m.ID), url.Values{"name": {"Renamed"}, "target_date": {m.TargetDate.Format("2006-01-02")}})
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s editing on %s: %d, want 403", who, g.Lifecycle, resp.StatusCode)
			}
			if after := rows(g); !slices.Equal(after, before) {
				t.Errorf("%s changed %s's Milestones: %q, was %q", who, g.Lifecycle, after, before)
			}
			for _, u := range []string{goalPageURL(ts.URL, g), goalPageURL(ts.URL, g) + "?open=milestones"} {
				if offersAddMilestone(getPage(t, client, u)) {
					t.Errorf("%s is offered the Add Milestone form at %s", who, u)
				}
			}
		}
	}

	sam2 := signInClient(t, ts.URL, "sam@example.com")
	for _, lifecycle := range []string{domain.LifecycleDone, domain.LifecycleCancelled} {
		g := h.ActiveGoal(sam, "Ended "+lifecycle, "It mattered.")
		h.EndGoalInCheckin(sam, g.ID, lifecycle)
		for _, u := range []string{goalPageURL(ts.URL, g), goalPageURL(ts.URL, g) + "?open=milestones"} {
			if offersAddMilestone(getPage(t, sam2, u)) {
				t.Errorf("the Owner of a %s Goal is offered the Add Milestone form at %s", lifecycle, u)
			}
		}
		before := rows(g)
		resp := postForm(t, sam2, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, g.ID), url.Values{"name": {"Late"}, "target_date": {"2026-05-01"}})
		defer func() { _ = resp.Body.Close() }()
		body := textOf(parsePage(t, readBody(t, resp)))
		if want := "A " + lifecycle + " Goal's Milestones can't change."; resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, want) {
			t.Errorf("adding to a %s Goal answered %d %q, want 422 saying %q", lifecycle, resp.StatusCode, body, want)
		}
		for _, internal := range []string{"validation failed", "not authorized"} {
			if strings.Contains(body, internal) {
				t.Errorf("adding to a %s Goal answered %q, carrying the internal %q prefix", lifecycle, body, internal)
			}
		}
		if after := rows(g); !slices.Equal(after, before) {
			t.Errorf("a %s Goal took a Milestone: %q", lifecycle, after)
		}
	}
}

// The Owner's Edit on a Proposed Goal's Milestone changes its name and date,
// but a Delegate's on an Active Goal's Milestone is refused, rename and date
// alike: a Milestone is edited only while its Goal is Proposed, and after that
// its date moves only in a Check-in (CONTEXT.md: Milestone, Date Slip).
func TestOwnerOrDelegateEditsAMilestoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	if _, err := h.Service.AddMilestoneAsAuthor(t.Context(), proposed.Owner.ID, domain.AddMilestoneInput{
		GoalID: proposed.ID, Name: "Pricing page", TargetDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dana, active.ID)
	ts := newServer(t, h)

	pricing := onlyMilestone(t, h, proposed)
	sam2 := signInClient(t, ts.URL, "sam@example.com")
	section := findByTestID(t, getPage(t, sam2, goalPageURL(ts.URL, proposed)), "goal-milestones")
	editAction := fmt.Sprintf("/milestones/%d", pricing.ID)
	if edits := findAll(section, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == editAction }); len(edits) != 1 {
		t.Fatalf("the Milestones section has %d Edit forms posting to %s, want 1: %s", len(edits), editAction, renderNode(section))
	}
	resp := postForm(t, sam2, ts.URL+editAction, url.Values{"name": {"Pricing page live"}, "target_date": {"2026-04-15"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, proposed); resp.StatusCode != http.StatusOK || got.Name != "Pricing page live" || got.TargetDate.Format("2006-01-02") != "2026-04-15" {
		t.Errorf("the Owner's Edit answered %d and left %q %s", resp.StatusCode, got.Name, got.TargetDate.Format("2006-01-02"))
	}

	beta := onlyMilestone(t, h, active)
	dana2 := signInClient(t, ts.URL, "dana@example.com")
	resp = postForm(t, dana2, fmt.Sprintf("%s/milestones/%d", ts.URL, beta.ID), url.Values{"name": {"Public beta"}, "target_date": {beta.TargetDate.Format("2006-01-02")}})
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, active); resp.StatusCode != http.StatusUnprocessableEntity || got.Name != "Beta" {
		t.Errorf("a Delegate's rename on an Active Goal answered %d and left %q, want 422 and Beta", resp.StatusCode, got.Name)
	}
	resp = postForm(t, dana2, fmt.Sprintf("%s/milestones/%d", ts.URL, beta.ID), url.Values{"name": {"Public beta"}, "target_date": {"2026-12-01"}})
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, active); resp.StatusCode != http.StatusUnprocessableEntity || !got.TargetDate.Equal(beta.TargetDate) {
		t.Errorf("moving an Active Goal's Milestone date outside a Check-in answered %d and left %s", resp.StatusCode, got.TargetDate)
	}
}

// On a Proposed Goal a Delegate is offered the same Edit form on each
// Milestone as its Owner, and what they save is kept, name and date; a reader
// and an Admin who is neither Owner nor Delegate are offered none, for
// EditMilestone allows only the Owner and Delegates.
func TestDelegateEditsAProposedGoalsMilestoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	h.SignIn("ada@example.com")
	h.SignIn("pat@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	if _, err := h.Service.AddMilestoneAsAuthor(t.Context(), sam.ID, domain.AddMilestoneInput{
		GoalID: proposed.ID, Name: "Pricing page", TargetDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	h.AddDelegate(sam, dana, proposed.ID)
	ts := newServer(t, h)
	milestones := func(client *http.Client) *nethtml.Node {
		t.Helper()
		return findByTestID(t, getPage(t, client, goalPageURL(ts.URL, proposed)), "goal-milestones")
	}

	dana2 := signInClient(t, ts.URL, "dana@example.com")
	section := milestones(dana2)
	if name := formField(t, section, "name"); name.Value != "Pricing page" {
		t.Errorf("a Delegate's Edit form names the Milestone %q, want %q", name.Value, "Pricing page")
	}
	if date := formField(t, section, "target_date"); date.Type != "date" || date.Value != "2026-04-01" {
		t.Errorf("a Delegate's Edit form dates the Milestone %s %q, want date 2026-04-01", date.Type, date.Value)
	}
	if buttonNamed(section, "Save") == nil {
		t.Fatalf("a Delegate on a Proposed Goal isn't offered Save on its Milestone: %s", renderNode(section))
	}
	pricing := onlyMilestone(t, h, proposed)
	resp := postForm(t, dana2, fmt.Sprintf("%s/milestones/%d", ts.URL, pricing.ID), url.Values{"name": {"Pricing page live"}, "target_date": {"2026-04-15"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, proposed); resp.StatusCode != http.StatusOK || got.Name != "Pricing page live" || got.TargetDate.Format("2006-01-02") != "2026-04-15" {
		t.Errorf("a Delegate's Edit answered %d and left %q %s", resp.StatusCode, got.Name, got.TargetDate.Format("2006-01-02"))
	}

	for _, who := range []string{"pat@example.com", "ada@example.com"} {
		section := milestones(signInClient(t, ts.URL, who))
		if fields := formFields(section, "name"); len(fields) != 0 || buttonNamed(section, "Save") != nil {
			t.Errorf("%s is offered a Milestone's Edit form on a Proposed Goal: %s", who, renderNode(section))
		}
	}
}

// A Milestone added from the Goal page after the Goal's latest Check-in is
// New on the Goal page, as one that Check-in added would be; one added before
// it isn't, and nothing is New on a Goal with no Check-in yet (CONTEXT.md:
// Milestone).
func TestGoalPageMarksAMilestoneAddedSinceTheLatestCheckinNewOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	unchecked := h.ActiveGoal(sam, "Migrate billing", "Billing is brittle.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	add := func(g domain.Goal, name string) {
		t.Helper()
		h.Clock.Advance(time.Hour)
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, g.ID), url.Values{"name": {name}, "target_date": {"2026-05-01"}})
		defer func() { _ = resp.Body.Close() }()
		if _ = readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("adding %s answered %d", name, resp.StatusCode)
		}
	}
	add(goal, "Runbook")
	h.Clock.Advance(time.Hour)
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	add(goal, "GA")
	add(unchecked, "Docs")

	rows := func(g domain.Goal) []string {
		t.Helper()
		return goalMilestoneRowsIn(findByTestID(t, getPage(t, client, goalPageURL(ts.URL, g)), "goal-milestones"))
	}
	if got, want := rows(goal), []string{"Apr 2 Beta", "May 1 Runbook", "New May 1 GA"}; !slices.Equal(got, want) {
		t.Errorf("Milestones read %q, want %q", got, want)
	}
	if got, want := rows(unchecked), []string{"Apr 2 Beta", "May 1 Docs"}; !slices.Equal(got, want) {
		t.Errorf("with no Check-in, Milestones read %q, want %q", got, want)
	}
}

// goalMarkedMilestones is how the Goal page lists MilestoneMarksGoal's
// Milestones, earliest date first: mark, then date, then name.
var goalMarkedMilestones = []string{
	"Overdue Jan 20 Security review",
	"Done Feb 1 Pilot",
	"New Mar 10 Docs",
	"Slipped Apr 12 Apr 2 Beta",
	"Slipped Apr 20 Mar 20 (3) Vendor sign-off",
	"May 1 Runbook",
	"Removed May 20 Launch party Removed: Budget cut.",
	"Jun 15 GA launch",
}

// The Goal page's Milestones table reads Status, Date, Milestone. Each
// Milestone's mark, judged as of today, is the first that applies: Done,
// Removed, Overdue while past its date, Slipped once slipped, New when the
// latest Check-in added it — not an earlier one — and none while on track,
// each shown as its word: Overdue in Red's tint and Slipped in Yellow's, with
// no Health shape (#461). Its date
// is the current one with the most recent it slipped from struck through, and
// a count once it slipped more than once. The marks are not a Health: the
// Goal keeps the Yellow its Owner set and its Rolled-up Health (ADR 0003).
func TestGoalPageListsMilestonesAsMarkDateNameOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.MilestoneMarksGoal(sam)
	ts := newServer(t, h)

	doc := getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	section := findByTestID(t, doc, "goal-milestones")
	headings := textsOf(findAll(section, func(n *nethtml.Node) bool { return n.Data == "th" }))
	if want := []string{"Status", "Date", "Milestone"}; !slices.Equal(headings, want) {
		t.Errorf("Milestone columns %q, want %q", headings, want)
	}
	if got := goalMilestoneRowsIn(section); !slices.Equal(got, goalMarkedMilestones) {
		t.Errorf("Milestones read\n %q\nwant\n %q", got, goalMarkedMilestones)
	}
	var vendor *nethtml.Node
	for _, tr := range findAllByTestID(section, "goal-milestone") {
		if strings.Contains(textOf(tr), "Vendor sign-off") {
			vendor = tr
		}
	}
	if vendor == nil {
		t.Fatalf("no Milestone row reads Vendor sign-off: %s", renderNode(section))
	}
	if struck := struckThrough(vendor); !slices.Contains(struck, "Mar 20") {
		t.Errorf("Vendor sign-off strikes through %q, want the date it last slipped from, Mar 20: %s", struck, renderNode(vendor))
	}
	for _, n := range findAll(vendor, func(*nethtml.Node) bool { return true }) {
		if text := textOf(n); strings.HasSuffix(text, "Mar 1") || strings.Contains(text, "Mar 10") {
			t.Errorf("Vendor sign-off still lists an older date, Mar 1 or Mar 10: %s", renderNode(n))
		}
	}
	if struck := struckThrough(section); !slices.Contains(struck, "Launch party") {
		t.Errorf("the Removed Milestone's name is not struck through: %q struck", struck)
	}
	assertMilestoneMarksShowTheirWords(t, section, []string{"Overdue", "Done", "New", "Slipped", "Slipped", "Removed"})
	for _, testID := range []string{"goal-health", "goal-rollup-health"} {
		if got := textOf(findByTestID(t, doc, testID)); got != "Yellow" {
			t.Errorf("Goal page's %s reads %q, want Yellow: the Owner's Yellow and the Rolled-up Health stand whatever the Milestones' marks", testID, got)
		}
	}
}

// goalMilestoneDateCellIn returns row's Date cell, the cell under the
// Milestones table's Date column in section.
func goalMilestoneDateCellIn(t *testing.T, section, row *nethtml.Node) *nethtml.Node {
	t.Helper()
	i := slices.Index(textsOf(findAll(section, func(n *nethtml.Node) bool { return n.Data == "th" })), "Date")
	cells := findAll(row, func(n *nethtml.Node) bool { return n.Data == "td" })
	if i < 0 || i >= len(cells) {
		t.Fatalf("the Milestone row has no Date cell: %s", renderNode(row))
	}
	return cells[i]
}

// Each Milestone date cell shows the current date with the one it slipped
// from struck through, each date its own element, so each wraps as a whole,
// never mid-date ("2026-04-" over "02") at phone width, while the breaks
// between the dates still wrap (#172): how they wrap is
// TestGoalPageMilestoneDatesDoNotBreakOverHTTPStyles's.
func TestGoalPageMilestoneDatesDoNotBreakOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	beta := onlyMilestone(t, h, goal)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	slipped := beta.TargetDate.AddDate(0, 0, 10)
	postFormStatus(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Beta moves."},
		fmt.Sprintf("milestone_date_%d", beta.ID):        {slipped.Format("2006-01-02")},
		fmt.Sprintf("milestone_date_reason_%d", beta.ID): {"Vendor slipped."},
	})

	section := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-milestones")
	cell := goalMilestoneDateCellIn(t, section, findByTestID(t, section, "goal-milestone"))
	if struck := struckThrough(cell); !slices.Equal(struck, []string{beta.TargetDate.Format("Jan 2")}) {
		t.Errorf("the Milestone's date cell strikes through %q, want %s: %s", struck, beta.TargetDate.Format("Jan 2"), renderNode(cell))
	}
	if spans := textsOf(findAll(cell, func(n *nethtml.Node) bool { return n.Data == "span" })); !slices.Contains(spans, slipped.Format("Jan 2")) {
		t.Errorf("the Milestone's date cell doesn't show %s as a date of its own: %s", slipped.Format("Jan 2"), renderNode(cell))
	}
}

// A Departed Delegate stays listed among the Goal's Delegates, marked departed;
// a present one isn't marked (CONTEXT.md: Departed).
func TestGoalPageMarksADepartedDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	lee := h.SignIn("lee@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	h.AddDelegate(sam, lee, goal.ID)
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, tpm.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)

	list := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "delegate-list")
	delegate := func(emailAddr string) *nethtml.Node {
		t.Helper()
		for _, li := range findAllByTestID(list, "delegate") {
			if slices.ContainsFunc(goalPeopleShownIn(li), func(p goalShownPerson) bool { return p.Title == emailAddr }) {
				return li
			}
		}
		t.Fatalf("the Delegates list doesn't list %s: %s", emailAddr, renderNode(list))
		return nil
	}
	departed := delegate("tpm@example.com")
	if marks := findAllByTestID(departed, "delegate-departed"); len(marks) != 1 || textOf(marks[0]) != "departed" {
		t.Errorf("the Departed Delegate isn't marked departed: %s", renderNode(departed))
	}
	if present := delegate("lee@example.com"); hasTestID(present, "delegate-departed") || strings.Contains(textOf(present), "departed") {
		t.Errorf("a present Delegate is marked departed: %s", renderNode(present))
	}
}

// The sidebar lists the Goals this one contributes to and those contributing to
// it, each with its Health, and removing a link happens at once, with no
// confirmation (#56). A breadcrumb leads back through Goals and the first
// parent.
func TestGoalPageSidebarLinksCarryHealthAndRemoveAtOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "Revenue funds the rest.")
	h.Checkin(sam, parent.ID, domain.HealthRed, "On fire.", "Put it out.", testsupport.Epoch.AddDate(0, 2, 0))
	goal := h.ActiveChildOf(sam, parent, "Reduce outages", "Outages cost trust.")
	child := h.ActiveChildOf(sam, goal, "Migrate displays", "Displays fail often.")
	h.Checkin(sam, child.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	crumbs := findByTestID(t, root, "goal-breadcrumb")
	for href, want := range map[string]string{"/goals": "Goals", goalHref(parent.ID): "Grow revenue"} {
		if link := linkTo(crumbs, href); link == nil || textOf(link) != want {
			t.Errorf("breadcrumb lacks a link to %s reading %s: %s", href, want, renderNode(crumbs))
		}
	}
	if here := findAll(crumbs, func(n *nethtml.Node) bool { return attrOf(n, "aria-current") == "page" }); len(here) != 1 || here[0].Data != "span" || textOf(here[0]) != "Reduce outages" {
		t.Errorf("breadcrumb doesn't end on this Goal as the current page: %s", renderNode(crumbs))
	}

	for _, tc := range []struct {
		section string
		linked  domain.Goal
		health  string
		tint    string
	}{
		{"goal-parents", parent, domain.HealthRed, "r"},
		{"goal-children", child, domain.HealthGreen, "g"},
	} {
		section := findByTestID(t, root, tc.section)
		title := linkTo(section, goalHref(tc.linked.ID))
		if title == nil {
			t.Fatalf("%s doesn't link %s:\n%s", tc.section, tc.linked.Title, renderNode(section))
		}
		chip := healthChipIn(t, title.Parent)
		assertHealthChip(t, chip, tc.health)
		if !flatFrameHasClass(chip, tc.tint) {
			t.Errorf("%s: %s's Health badge isn't in %s's tint: %s", tc.section, tc.linked.Title, tc.health, renderNode(chip))
		}
		assertSubmitsAtOnce(t, tc.section+": Remove", renderNode(section), "/remove")
	}

	// Removing the link to the parent from its form unlinks it.
	client := signInClient(t, ts.URL, "sam@example.com")
	removes := findAll(findByTestID(t, root, "goal-parents"), func(n *nethtml.Node) bool {
		return n.Data == "form" && strings.HasSuffix(attrOf(n, "action"), "/remove")
	})
	if len(removes) != 1 {
		t.Fatalf("the parents section has %d Remove forms, want 1", len(removes))
	}
	if status := postFormStatus(t, client, ts.URL+attrOf(removes[0], "action"), url.Values{}); status != http.StatusOK {
		t.Fatalf("remove link: status %d", status)
	}
	if parents := findByTestID(t, getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-parents"); !hasTestID(parents, "no-parents") {
		t.Errorf("the parent is still linked after Remove:\n%s", renderNode(parents))
	}
}

// On a Goal's page, each parent and child link offers Remove to the Owner of
// either linked Goal and to no one else: not a Delegate, an Admin or a
// stranger (the people RemoveLink allows).
func TestGoalPageOffersLinkRemoveOnlyToEitherGoalsOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	chris := h.SignIn("chris@example.com")
	morgan := h.SignIn("morgan@example.com")
	h.SignIn("rita@example.com")
	h.SignIn("admin@example.com")
	goal := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	parent := h.ActiveGoal(pat, "Grow revenue", "We need to grow.")
	child := h.ActiveGoal(chris, "Index the catalog", "Search needs an index.")
	h.AddDelegate(sam, morgan, goal.ID)
	up := h.RequestLink(sam, goal, parent, "")
	if _, err := h.Service.AcceptLink(ctx, up.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink of the parent link: %v", err)
	}
	down := h.RequestLink(chris, child, goal, "")
	if _, err := h.Service.AcceptLink(ctx, down.ID, sam.ID); err != nil {
		t.Fatalf("AcceptLink of the child link: %v", err)
	}
	ts := newServer(t, h)

	for _, v := range []struct {
		who, name                 string
		removeParent, removeChild bool
	}{
		{"sam@example.com", "the Goal's Owner", true, true},
		{"pat@example.com", "the parent's Owner", true, false},
		{"chris@example.com", "the child's Owner", false, true},
		{"morgan@example.com", "a Delegate", false, false},
		{"admin@example.com", "an Admin", false, false},
		{"rita@example.com", "a stranger", false, false},
	} {
		page := getPage(t, signInClient(t, ts.URL, v.who), goalPageURL(ts.URL, goal))
		for _, s := range []struct {
			section string
			linked  domain.Goal
			want    bool
		}{
			{"goal-parents", parent, v.removeParent},
			{"goal-children", child, v.removeChild},
		} {
			section := findByTestID(t, page, s.section)
			if linkTo(section, goalHref(s.linked.ID)) == nil {
				t.Errorf("%s: %s doesn't link %s:\n%s", v.name, s.section, s.linked.Title, renderNode(section))
			}
			if got := buttonNamed(section, "Remove") != nil; got != s.want {
				t.Errorf("%s: Remove shown in %s = %t, want %t:\n%s", v.name, s.section, got, s.want, renderNode(section))
			}
		}
	}
}

// Activate Goal shows only to the Owner of a Proposed Goal: not to its
// Delegate, an Admin or a stranger, and not to the Owner once it is Active.
func TestGoalPageOffersActivateGoalOnlyToAProposedGoalsOwner(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com")
	sam := h.SignIn("sam@example.com")
	morgan := h.SignIn("morgan@example.com")
	h.SignIn("rita@example.com")
	h.SignIn("admin@example.com")
	proposed := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	active := h.ActiveGoal(sam, "Ship search", "People can't find things.")
	h.AddDelegate(sam, morgan, proposed.ID)
	ts := newServer(t, h)

	for _, v := range []struct {
		who, name string
		goal      domain.Goal
		want      bool
	}{
		{"sam@example.com", "the Owner of the Proposed Goal", proposed, true},
		{"morgan@example.com", "a Delegate on the Proposed Goal", proposed, false},
		{"admin@example.com", "an Admin on the Proposed Goal", proposed, false},
		{"rita@example.com", "a stranger to the Proposed Goal", proposed, false},
		{"sam@example.com", "the Owner of the Active Goal", active, false},
	} {
		page := getPage(t, signInClient(t, ts.URL, v.who), goalPageURL(ts.URL, v.goal))
		if got := buttonNamed(findByTestID(t, page, "goal-main"), "Activate Goal") != nil; got != v.want {
			t.Errorf("%s: Activate Goal shown = %t, want %t", v.name, got, v.want)
		}
	}
}

// Each Metric is a row: its name over "baseline → target by date", then a
// sparkline of its readings with a dashed target line, labelled for a screen
// reader with the trend, then its current value. The Owner edits it in a
// collapsed section. How the row and its figures look is
// TestGoalPageMetricRowsShowASparklineStyles's.
func TestGoalPageMetricRowsShowASparkline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	ctx := t.Context()
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalDated(ctx, goal.Owner.ID, goal.ID, testsupport.Epoch.AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	metric, err := h.Service.AddMetric(ctx, goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1200, Target: 400, TargetDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.ActivateGoal(ctx, goal.Owner.ID, goal.ID); err != nil {
		t.Fatalf("ActivateGoal: %v", err)
	}
	for _, v := range []float64{1000, 800} {
		if _, err := h.Service.SubmitCheckin(ctx, domain.SubmitCheckinInput{
			GoalID: goal.ID, AuthorID: sam.ID, Health: domain.HealthGreen, Status: "Faster.",
			Readings: []domain.MetricReadingInput{{MetricID: metric.ID, Value: v}},
		}); err != nil {
			t.Fatalf("SubmitCheckin: %v", err)
		}
	}
	ts := newServer(t, h)
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	editAction := fmt.Sprintf("/metrics/%d", metric.ID)
	editForms := func(root *nethtml.Node) []*nethtml.Node {
		return findAll(root, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == editAction })
	}

	card := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalURL), "goal-metric")
	name, current := headingNamed(card, "p95 latency"), findByTestID(t, card, "metric-current")
	svgs := findAll(card, func(n *nethtml.Node) bool { return n.Data == "svg" })
	if name == nil || name.Data != "h3" || len(svgs) == 0 || !precedes(name, svgs[0]) || !precedes(svgs[0], current) {
		t.Fatalf("the Metric row should read its h3 name, sparkline, current value in that order: %s", renderNode(card))
	}
	if got := textOf(current); got != "800" {
		t.Errorf("current value reads %q, want the latest reading, 800", got)
	}
	if !strings.Contains(textOf(card), "1200 → 400 ms by Jun 15") {
		t.Errorf("card lacks baseline → target by date: %s", textOf(card))
	}
	svg := svgs[0]
	if attrOf(svg, "role") != "img" || attrOf(svg, "aria-label") != "p95 latency: 2 readings, falling from 1000 to 800 ms, toward the target of 400 ms" {
		t.Errorf("sparkline isn't an image labelled with the trend: %s", renderNode(svg))
	}
	if len(findAll(svg, func(n *nethtml.Node) bool { return n.Data == "polyline" })) == 0 {
		t.Errorf("sparkline draws no line of readings: %s", renderNode(svg))
	}
	if len(findAll(svg, func(n *nethtml.Node) bool { return hasAttr(n, "stroke-dasharray") })) == 0 {
		t.Errorf("sparkline draws no dashed target line: %s", renderNode(svg))
	}
	edit := findByTestID(t, card, "metric-edit")
	if edit.Data != "details" || hasAttr(edit, "open") || len(editForms(edit)) != 1 {
		t.Errorf("the Owner's edit form is not in a collapsed section: %s", renderNode(edit))
	}

	if len(editForms(getPage(t, signInClient(t, ts.URL, "mel@example.com"), goalURL))) > 0 {
		t.Errorf("a non-Owner is offered the Metric edit form")
	}
}

// goalHistoryChipCountsIn reads the History block's filter chips as each
// chip's label mapped to the count beside it.
func goalHistoryChipCountsIn(t *testing.T, history *nethtml.Node) map[string]string {
	t.Helper()
	counts := map[string]string{}
	for _, chip := range findAllByTestID(findByTestID(t, history, "history-chips"), "history-chip") {
		var label []string
		var count *nethtml.Node
		for c := chip.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case nethtml.TextNode:
				label = append(label, strings.Fields(c.Data)...)
			case nethtml.ElementNode:
				count = c
			}
		}
		if count == nil {
			t.Fatalf("a History chip carries no count: %s", renderNode(chip))
		}
		counts[strings.Join(label, " ")] = textOf(count)
	}
	return counts
}

// The Goal page's History — Check-ins, Date Slips, Why it matters revisions,
// ownership changes and value changes — is one timeline under History, open on
// load, with a filter chip per kind carrying its count.
func TestGoalPageHistoryIsOpenWithCounts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Still fine.", "", time.Time{})
	if _, err := h.Service.EditWhyItMatters(t.Context(), goal.ID, "Outages cost trust and money.", sam.ID); err != nil {
		t.Fatalf("EditWhyItMatters: %v", err)
	}
	ts := newServer(t, h)

	history := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-history")
	if disclosures := findAll(history, func(n *nethtml.Node) bool { return n.Data == "details" }); len(disclosures) > 0 {
		t.Errorf("History sits behind a disclosure: %s", renderNode(disclosures[0]))
	}
	chips := goalHistoryChipCountsIn(t, history)
	for label, count := range map[string]string{
		"All":            "4",
		"Check-ins":      "2",
		"Date Slips":     "0",
		"Why it matters": "2", // the original and the edit
		"Ownership":      "0",
		"Values":         "0",
	} {
		if chips[label] != count {
			t.Errorf("%q chip counts %q, want %s", label, chips[label], count)
		}
	}
	if n := len(findAllByTestID(history, "history-entry")); n != 4 {
		t.Errorf("History lists %d entries, want 4:\n%s", n, renderNode(history))
	}
}

// The Latest status section leads the document column with the latest
// Check-in's Health, status and Path to Green, and who wrote it and when.
func TestGoalPageLatestStatusSection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Wobbling.", "Add a second on-call.", testsupport.Epoch.AddDate(0, 2, 0))
	ts := newServer(t, h)

	status := findByTestID(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "goal-checkins")
	if flatFrameHasClass(status, "card") {
		t.Errorf("Latest status is a card: %s", attrOf(status, "class"))
	}
	if heading := headingNamed(status, "Latest status"); heading == nil || heading.Data != "h2" {
		t.Errorf("Latest status doesn't open with its h2")
	}
	for _, want := range []string{"Wobbling.", "Add a second on-call.", "Jan 2"} {
		if !strings.Contains(textOf(status), want) {
			t.Errorf("Latest status lacks %q: %s", want, textOf(status))
		}
	}
	if !slices.ContainsFunc(goalPeopleShownIn(status), func(p goalShownPerson) bool { return p.Title == "sam@example.com" }) {
		t.Errorf("Latest status doesn't show sam@example.com wrote it: %s", renderNode(status))
	}
	if hasTestID(status, "checkin-history") {
		t.Errorf("Latest status still carries the history")
	}
}

// A Proposed Goal's Owner sees the activation checklist drawn from the rules
// activation enforces, each item done or to do, and Activate stays disabled
// until every item is done.
func TestProposedGoalShowsActivationChecklist(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	page := getPage(t, client, goalURL)
	want := map[string]bool{
		"Why it matters":                         true,
		"Owner":                                  true,
		"Dated with a delivery date, or Ongoing": false,
		"A Milestone or Metric":                  false,
	}
	if got := goalActivationChecklistIn(t, page); !maps.Equal(got, want) {
		t.Errorf("bare Proposed Goal checklist = %v, want %v", got, want)
	}
	if button := goalActivateButtonIn(t, page); !hasAttr(button, "disabled") {
		t.Errorf("Activate is enabled with items missing: %s", renderNode(button))
	}

	if _, err := h.Service.MarkGoalDated(t.Context(), sam.ID, goal.ID, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	page = getPage(t, client, goalURL)
	if got := goalActivationChecklistIn(t, page); !got["Dated with a delivery date, or Ongoing"] || got["A Milestone or Metric"] {
		t.Errorf("Dated Goal with no Milestone or Metric: checklist = %v", got)
	}
	postFormStatus(t, client, goalURL+"/milestones", url.Values{"name": {"Beta cut"}, "target_date": {"2026-03-16"}})
	page = getPage(t, client, goalURL)
	for item, done := range goalActivationChecklistIn(t, page) {
		if !done {
			t.Errorf("%q still missing once the Goal is ready", item)
		}
	}
	if button := goalActivateButtonIn(t, page); hasAttr(button, "disabled") {
		t.Errorf("Activate is disabled with every item done: %s", renderNode(button))
	}
}

// goalActivationChecklistIn reads the activation checklist under root as each
// item's label mapped to whether it is done. Each item reads as a mark, its
// label, and "done" or "to do", so it never relies on the mark alone: ✓ for
// done and a neutral ○ for to do.
func goalActivationChecklistIn(t *testing.T, root *nethtml.Node) map[string]bool {
	t.Helper()
	items := map[string]bool{}
	for _, li := range goalActivationChecklistItemsIn(t, root) {
		mark, label, state := goalActivationItemPartsOf(t, li)
		done := attrOf(li, "data-done") == "true"
		if want := map[bool]string{true: "done", false: "to do"}[done]; state != want {
			t.Errorf("checklist item says %q against data-done=%v, want %q: %s", state, done, want, renderNode(li))
		}
		if want := map[bool]string{true: "✓", false: "○"}[done]; mark != want {
			t.Errorf("checklist item is marked %q against data-done=%v, want %q: %s", mark, done, want, renderNode(li))
		}
		items[label] = done
	}
	return items
}

// goalActivationChecklistItemsIn returns the activation checklist's items
// under root, in order.
func goalActivationChecklistItemsIn(t *testing.T, root *nethtml.Node) []*nethtml.Node {
	t.Helper()
	list := findByTestID(t, root, "activation-checklist")
	return findAll(list, func(n *nethtml.Node) bool { return n.Data == "li" })
}

// goalActivationItemPartsOf splits what an activation checklist item reads
// into its mark, its label, and its trailing state, "done" or the two words
// "to do".
func goalActivationItemPartsOf(t *testing.T, li *nethtml.Node) (mark, label, state string) {
	t.Helper()
	words := strings.Fields(textOf(li))
	stateWords := 1
	if n := len(words); n >= 2 && words[n-2] == "to" && words[n-1] == "do" {
		stateWords = 2
	}
	if len(words) < 2+stateWords {
		t.Fatalf("checklist item has no mark, label and state: %s", renderNode(li))
	}
	n := len(words) - stateWords
	return words[0], strings.Join(words[1:n], " "), strings.Join(words[n:], " ")
}

// goalActivateButtonIn returns the Activate Goal button of the activate-goal
// form under root.
func goalActivateButtonIn(t *testing.T, root *nethtml.Node) *nethtml.Node {
	t.Helper()
	form := findByTestID(t, root, "activate-goal")
	button := buttonNamed(form, "Activate Goal")
	if button == nil {
		t.Fatalf("the activate-goal form has no Activate Goal button: %s", renderNode(form))
	}
	return button
}

// A Proposed Goal's checklist marks each item not yet met with a neutral ○
// "to do", as the New goal form does, never a red ✗ "missing", and each met
// item ✓ "done". How the marks are tinted is pinned in styles_test.go
// (TestProposedGoalChecklistMarksUnmetItemsToDoStyles).
func TestProposedGoalChecklistMarksUnmetItemsToDo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	for _, li := range goalActivationChecklistItemsIn(t, page) {
		mark, label, state := goalActivationItemPartsOf(t, li)
		want := [2]string{"○", "to do"}
		if label == "Why it matters" || label == "Owner" {
			want = [2]string{"✓", "done"}
		}
		if got := [2]string{mark, state}; got != want {
			t.Errorf("%q is marked %q %q, want %q %q: %s", label, mark, state, want[0], want[1], renderNode(li))
		}
	}
	list := findByTestID(t, page, "activation-checklist")
	for _, gone := range []string{"✗", "missing"} {
		if goalMentions(list, gone) {
			t.Errorf("the checklist says %q: %s", gone, renderNode(list))
		}
	}
}

// The Define card on a Proposed Goal's page holds no forms but Activate Goal:
// the checklist, the Activate Goal button, and a Finish defining link to the
// define page, where the Kind, cadence, Why it matters, Milestones and Metrics are
// set (#135).
func TestDefineCardLinksToFinishDefining(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	card := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, goal)), "define-goal")
	for _, gone := range []string{"mark-kind", "set-cadence", "edit-why-it-matters", "add-milestone", "add-metric"} {
		if hasTestID(card, gone) {
			t.Errorf("the Define card still holds %s: %s", gone, renderNode(card))
		}
	}
	if forms := findAll(card, func(n *nethtml.Node) bool { return n.Data == "form" }); len(forms) != 1 || attrOf(forms[0], "data-testid") != "activate-goal" {
		t.Errorf("the Define card holds %d forms, want only Activate Goal: %s", len(forms), renderNode(card))
	}
	goalActivationChecklistIn(t, card)
	link := findByTestID(t, card, "finish-defining")
	if want := fmt.Sprintf("/goals/%d/define", goal.ID); link.Data != "a" || attrOf(link, "href") != want || textOf(link) != "Finish defining" {
		t.Errorf("Finish defining doesn't link to %s: %s", want, renderNode(link))
	}
}

// Add a child Goal is the New goal form now, so its old route is gone.
func TestCreateChildGoalRouteIsGone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/children", ts.URL, goal.ID), url.Values{"title": {"Child"}, "why_it_matters": {"It matters."}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /goals/%d/children answered %d, want it gone", goal.ID, resp.StatusCode)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 1 {
		t.Errorf("the post created a Goal: %d Goals", len(goals))
	}
}

// An Ongoing Goal needs a Metric to activate, and a Milestone alone doesn't
// satisfy its checklist.
func TestOngoingGoalChecklistNeedsAMetric(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Keep the lights on", "Uptime is table stakes.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	if _, err := h.Service.MarkGoalOngoing(t.Context(), sam.ID, goal.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	postFormStatus(t, client, goalURL+"/milestones", url.Values{"name": {"Runbook"}, "target_date": {"2026-03-16"}})
	page := getPage(t, client, goalURL)
	if done, listed := goalActivationChecklistIn(t, page)["A Metric"]; !listed || done {
		t.Errorf("Ongoing Goal with only a Milestone: A Metric listed=%v done=%v", listed, done)
	}
	if button := goalActivateButtonIn(t, page); !hasAttr(button, "disabled") {
		t.Errorf("Activate is enabled for an Ongoing Goal with no Metric: %s", renderNode(button))
	}
}

// goalEmailExemptScopes are the Scope selects (scope.templ), on the Goal list
// and the Follow-ups page: the only selects whose options name people as Name
// (email). An option can't hold a control to disclose an email (#275).
var goalEmailExemptScopes = map[string]bool{"goal-scope-filter": true, "follow-ups-scope": true}

// visibleEmails returns each run of text under root that shows an email as
// text rather than on hover: every text holding one, but for a person's email
// waiting, hidden, in its expansion, and a Scope select's option.
func visibleEmails(root *nethtml.Node) []string {
	var shown []string
	for n := range root.Descendants() {
		if n.Type != nethtml.TextNode || !strings.Contains(n.Data, "@example.com") || goalEmailExempt(n) {
			continue
		}
		shown = append(shown, strings.TrimSpace(n.Data))
	}
	return shown
}

// goalEmailExempt reports whether the text n sits in a collapsed email
// expansion or in a Scope select's option.
func goalEmailExempt(n *nethtml.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		switch {
		case p.Type != nethtml.ElementNode:
		case flatFrameHasClass(p, "person-email") && isHiddenElement(p):
			return true
		case p.Data == "option":
			for sel := p.Parent; sel != nil; sel = sel.Parent {
				if sel.Type == nethtml.ElementNode && sel.Data == "select" {
					return goalEmailExemptScopes[attrOf(sel, "data-testid")]
				}
			}
		}
	}
	return false
}

// visibleEmails exempts only the Scope selects' options, which name people as
// Name (email) by design; an email in any other select's option still shows.
func TestVisibleEmailsExemptsOnlyScopeSelectOptions(t *testing.T) {
	t.Parallel()

	page := `<form>` +
		`<select data-testid="goal-scope-filter" name="org" aria-label="Scope"><option value="all">Everyone</option><optgroup label="Other orgs"><option value="3">Ada Okafor (ada.okafor@example.com)</option></optgroup></select>` +
		`<select data-testid="follow-ups-scope" name="org" aria-label="Scope"><option value="4" selected>Mei Lin (mei.lin@example.com)</option></select>` +
		`<select name="parent_id"><option value="5">Owned by tomas.berg@example.com</option></select>` +
		`<span class="person"><button type="button" class="disclose" aria-expanded="false" title="ada.okafor@example.com">Ada Okafor</button><span class="person-email" hidden>ada.okafor@example.com</span></span>` +
		`</form>`
	shown := visibleEmails(parsePage(t, page))
	if want := []string{"Owned by tomas.berg@example.com"}; !slices.Equal(shown, want) {
		t.Errorf("visibleEmails = %q, want %q", shown, want)
	}
}

// The Goal list and a Goal page show each Owner by Name, or by their email's
// local part until they have one, with the email a click away.
func TestGoalPagesShowPeopleByName(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	sam := h.SignIn("sam@example.com")
	named := h.CreateGoal(ada, "Cut checkout latency", "Shoppers abandon slow carts.")
	h.CreateGoal(sam, "Hire a PM", "Nobody owns the roadmap.")
	client := signInClient(t, ts.URL, "sam@example.com")

	list := getPage(t, client, ts.URL+"/goals?org=all")
	for _, want := range []struct{ email, label string }{{"ada.okafor@example.com", "Ada Okafor"}, {"sam@example.com", "sam"}} {
		if !goalShows(list, want.email, want.label) {
			t.Errorf("Goal list does not show %s as %q with their email a click away", want.email, want.label)
		}
	}
	page := getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, named.ID))
	if owner := findByTestID(t, page, "goal-owner"); owner.Data != "strong" || !goalShows(owner, "ada.okafor@example.com", "Ada Okafor") {
		t.Errorf("Goal page Owner reads %s, want Ada Okafor with their email a click away", renderNode(owner))
	}
	for name, p := range map[string]*nethtml.Node{"Goal list": list, "Goal page": page} {
		if shown := visibleEmails(p); len(shown) > 0 {
			t.Errorf("%s shows emails as text: %q", name, shown)
		}
	}
}

// A person's Name is a button that reports whether their email is expanded:
// the email sits hidden right after it, and the page's script shows or hides
// it on each click, tap, Enter or Space, so people who share a Name can be
// told apart without hover (CONTEXT.md: Name).
func TestPersonExpandsTheirEmailOnActivation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	ada := h.SignInNamed("ada.okafor@example.com", "Ada Okafor")
	goal := h.CreateGoal(ada, "Cut checkout latency", "Shoppers abandon slow carts.")
	page := getPage(t, signInClient(t, ts.URL, ada.Email), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))

	owner := findByTestID(t, page, "goal-owner")
	if owner.Data != "strong" || !goalShows(owner, "ada.okafor@example.com", "Ada Okafor") {
		t.Errorf("Goal page Owner reads %s, want Ada Okafor as a collapsed button over their hidden email", renderNode(owner))
	}
	if button := buttonNamed(owner, "Ada Okafor"); button == nil || !goalHiddenEmailFollows(button, "ada.okafor@example.com") {
		t.Errorf("Ada Okafor's hidden email doesn't sit right after their button: %s", renderNode(owner))
	}
	script := disclosureScript(t, page)
	for _, want := range []string{`.disclose`, `aria-expanded`, `hidden`} {
		if !strings.Contains(script, want) {
			t.Errorf("the page's disclosure script does not handle %s:\n%s", want, script)
		}
	}
}

// goalHiddenEmailFollows reports whether what comes right after button is the
// hidden person-email reading emailAddr.
func goalHiddenEmailFollows(button *nethtml.Node, emailAddr string) bool {
	next := button.NextSibling
	return next != nil && next.Type == nethtml.ElementNode && next.Data == "span" && flatFrameHasClass(next, "person-email") &&
		isHiddenElement(next) && strings.Join(nodeWords(next, next, nil), " ") == emailAddr
}

// disclosureScript returns the text of the page's script that opens and
// closes every .disclose control's expansion.
func disclosureScript(t *testing.T, root *nethtml.Node) string {
	t.Helper()
	for _, s := range findAll(root, func(n *nethtml.Node) bool { return n.Data == "script" && !hasAttr(n, "src") }) {
		var body strings.Builder
		for c := s.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == nethtml.TextNode {
				body.WriteString(c.Data)
			}
		}
		if strings.Contains(body.String(), "disclose") {
			return body.String()
		}
	}
	t.Fatalf("page has no disclosure script")
	return ""
}

// The Goal page's Why it matters definition and Top-level explanation each sit
// collapsed behind a button that opens them in place on a click, tap, Enter or
// Space, and reports whether they are open, so neither needs hover. The
// wording is the one the hover title always carried. The Why it matters shows no
// heading, but assistive technology still hears it labelled "Why it matters".
// How the What goes here? button is drawn is pinned in styles_test.go
// (TestGoalPageExplainsWhyItMattersAndTopLevelOnActivationStyles).
func TestGoalPageExplainsWhyItMattersAndTopLevelOnActivation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	g := h.MarkTopLevel(ada, h.ActiveGoal(ada, "Grow revenue", "It pays for everything."))
	page := getPage(t, signInClient(t, ts.URL, ada.Email), fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))

	topLevel := findByTestID(t, page, "goal-top-level")
	for _, tc := range []struct {
		label  string
		button *nethtml.Node
		text   string
	}{
		{"What goes here?", buttonNamed(page, "What goes here?"), "The customer problem this Goal addresses and what is expected to change when it succeeds."},
		{"Top-level", topLevel, "One of the company's root outcomes"},
	} {
		button := tc.button
		if button == nil || button.Data != "button" || attrOf(button, "type") != "button" || !flatFrameHasClass(button, "disclose") || !flatFrameHasClass(button, "help") {
			t.Errorf("%s is not a disclosure help button", tc.label)
			continue
		}
		if textOf(button) != tc.label || attrOf(button, "aria-expanded") != "false" {
			t.Errorf("%s button does not start collapsed: %s", tc.label, renderNode(button))
		}
		id := attrOf(button, "aria-controls")
		explains := findAll(page, func(n *nethtml.Node) bool { return id != "" && attrOf(n, "id") == id })
		if len(explains) != 1 || attrOf(explains[0], "class") != "explain" || !isHiddenElement(explains[0]) || textOf(explains[0]) != tc.text {
			t.Errorf("%s button controls %q, want the collapsed explanation, a hidden explain reading %q", tc.label, id, tc.text)
		}
	}
	statement := findByTestID(t, page, "goal")
	label := attrOf(statement, "aria-labelledby")
	labels := findAll(page, func(n *nethtml.Node) bool { return label != "" && attrOf(n, "id") == label })
	if statement.Data != "article" || len(labels) != 1 || attrOf(labels[0], "class") != "sr-only" || textOf(labels[0]) != "Why it matters" {
		t.Errorf("the Why it matters is not labelled for assistive technology by a visually hidden Why it matters: %s", renderNode(statement))
	}
	disclosureScript(t, page)
}

// assertGoalTermHelpIn checks that within holds a help disclosure for a
// glossary term, as assertTermHelp does: a disclose help button reading
// label, starting collapsed, carrying text as its title, and controlling a
// hidden explain paragraph under root that reads text. It returns the button.
func assertGoalTermHelpIn(t *testing.T, root, within *nethtml.Node, label, text string) *nethtml.Node {
	t.Helper()
	button := buttonNamed(within, label)
	if button == nil {
		t.Errorf("no button reads %q", label)
		return nil
	}
	if !flatFrameHasClass(button, "disclose") || !flatFrameHasClass(button, "help") {
		t.Errorf("%q is not a disclose help button: %s", label, renderNode(button))
	}
	if attrOf(button, "type") != "button" || attrOf(button, "aria-expanded") != "false" {
		t.Errorf("%q button does not start collapsed: %s", label, renderNode(button))
	}
	if got := attrOf(button, "title"); got != text {
		t.Errorf("%q button's title = %q, want %q", label, got, text)
	}
	id := attrOf(button, "aria-controls")
	explains := findAll(root, func(n *nethtml.Node) bool { return id != "" && attrOf(n, "id") == id })
	if len(explains) != 1 || !termHelpExplains(explains[0], text) {
		t.Errorf("%q button controls %q, want the collapsed explanation, a hidden explain paragraph reading %q", label, id, text)
	}
	return button
}

// The Goal page explains Dated or Ongoing from the word in its metadata line,
// and what a Delegate and Dimension values are under their sidebar headings,
// each collapsed behind a help button that opens the definition in place. A
// Goal not yet Dated or Ongoing says so in plain text, with nothing to open.
func TestGoalPageExplainsKindDelegatesAndDimensionValues(t *testing.T) {
	t.Parallel()

	const (
		dated     = "Has a delivery date. Its Health is judged against that date and its Milestones."
		ongoing   = "Has no delivery date. Its Health is judged against its Metrics' targets."
		delegate  = "Someone the Owner authorizes to write Check-ins and set this Goal's Dimension values and Fields. The Owner stays accountable."
		dimension = "Values from lists an Admin defines, such as Pillar or Quarter, used to filter and group Goals."
	)
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	client := signInClient(t, ts.URL, ada.Email)

	g := h.ActiveGoal(ada, "Grow revenue", "It pays for everything.")
	page := getPage(t, client, goalPageURL(ts.URL, g))
	if button := assertGoalTermHelpIn(t, page, goalKindIn(t, page), "Dated", dated); attrOf(button, "aria-controls") != "kind-help" {
		t.Errorf("the Dated button controls %q, want kind-help", attrOf(button, "aria-controls"))
	}
	for _, tc := range []struct{ label, id, text string }{
		{"What is a Delegate?", "delegate-help", delegate},
		{"What are Dimension values?", "dimension-values-help", dimension},
	} {
		if button := assertGoalTermHelpIn(t, page, page, tc.label, tc.text); attrOf(button, "aria-controls") != tc.id {
			t.Errorf("%q controls %q, want %s", tc.label, attrOf(button, "aria-controls"), tc.id)
		}
	}
	disclosureScript(t, page)

	o := h.CreateGoal(ada, "Keep the lights on", "Customers expect it.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), o.Owner.ID, o.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	page = getPage(t, client, goalPageURL(ts.URL, o))
	if button := assertGoalTermHelpIn(t, page, goalKindIn(t, page), "Ongoing", ongoing); attrOf(button, "aria-controls") != "kind-help" {
		t.Errorf("the Ongoing button controls %q, want kind-help", attrOf(button, "aria-controls"))
	}

	p := h.CreateGoal(ada, "Find a market", "We need one.")
	page = getPage(t, client, goalPageURL(ts.URL, p))
	kind := goalKindIn(t, page)
	if elements := findAll(kind, func(n *nethtml.Node) bool { return n != kind }); textOf(kind) != "Not yet Dated or Ongoing" || len(elements) > 0 {
		t.Errorf("a Goal with no kind shows %s, want plain Not yet Dated or Ongoing", renderNode(kind))
	}
	if explains := findAll(page, func(n *nethtml.Node) bool { return attrOf(n, "id") == "kind-help" }); len(explains) > 0 {
		t.Errorf("a Goal with no kind has a kind explanation")
	}
}

// goalKindIn returns the Goal page's word for its kind, the goal-kind span.
func goalKindIn(t *testing.T, root *nethtml.Node) *nethtml.Node {
	t.Helper()
	kind := findByTestID(t, root, "goal-kind")
	if kind.Data != "span" {
		t.Fatalf("the Goal's kind isn't a span: %s", renderNode(kind))
	}
	return kind
}

// An Ownerless badge says what Ownerless means on hover, on the Goals list and
// in a parent Goal's Contributed to by list, where a button in every row would
// be too heavy.
func TestOwnerlessBadgesSayWhatOwnerlessMeans(t *testing.T) {
	t.Parallel()

	const ownerless = "Its Owner has left the company. It gets a new Owner when an Admin reassigns it, or when someone accepts a Handoff its Owner started before leaving."
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(boss, "Reduce outages", "Outages cost trust.")
	child := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "")
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, boss.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}
	if err := h.Service.MarkDeparted(context.Background(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	client := signInClient(t, ts.URL, boss.Email)

	for _, tc := range []struct {
		where string
		page  *nethtml.Node
	}{
		{"the Goals list", getPage(t, client, ts.URL+"/goals?org=all")},
		{"the parent's Contributed to by list", getPage(t, client, goalPageURL(ts.URL, parent))},
	} {
		badges := findAllByTestID(tc.page, "ownerless")
		if len(badges) == 0 {
			t.Errorf("%s shows no Ownerless badge", tc.where)
		}
		for _, badge := range badges {
			if got := attrOf(badge, "title"); got != ownerless {
				t.Errorf("the Ownerless badge on %s has title %q, want %q", tc.where, got, ownerless)
			}
		}
	}
}

// With a seeded company, every page that shows people shows them by Name with
// their email a click, tap or keypress away, never an email as text until
// asked for; the Print view, with no hover or controls, introduces them as
// Name (email) (CONTEXT.md: Name). Only the Scope selects' options
// (goal-scope-filter, follow-ups-scope) are exempt: an option can't hold a control
// to disclose an email, so they name people as Name (email) to tell
// same-Named people apart (#275). Any other select's options are checked.
func TestSeededPagesShowPeopleByName(t *testing.T) {
	t.Parallel()

	const adminEmail = "admin@example.com"
	h := testsupport.New(t, adminEmail)
	ctx := context.Background()
	if _, err := seed.Run(ctx, h.Service, h.Clock, seed.Options{Seed: seed.DefaultSeed, Admin: adminEmail}); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
	admin := h.SignIn(adminEmail)
	goals, err := h.Service.ListGoals(ctx)
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}

	// An Active Goal owned by someone whose email spells out their Name, and
	// another person to delegate to, hand off to, and leave the company.
	var g domain.Goal
	for _, c := range goals {
		if c.Lifecycle == domain.LifecycleActive && strings.Contains(c.Owner.Email, ".") && c.Owner.Name != "" {
			g = c
			break
		}
	}
	if g.ID == 0 {
		t.Fatal("seeded company has no Active Goal owned by a named person")
	}
	owner := g.Owner
	var other, departed domain.Account
	for _, c := range goals {
		switch {
		case c.Owner.ID == owner.ID:
		case other.ID == 0:
			other = c.Owner
		case c.Owner.ID != other.ID && departed.ID == 0:
			departed = c.Owner
		}
	}
	h.AddDelegate(owner, other, g.ID)
	if _, err := h.Service.StartHandoffByEmail(ctx, g.ID, other.Email, owner.ID); err != nil {
		t.Fatalf("StartHandoffByEmail: %v", err)
	}
	h.RequestLink(other, h.CreateGoal(other, "Try a side project", "It might help."), g, "")
	if err := h.Service.MarkDeparted(ctx, admin.ID, departed.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	def := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(admin, def)
	comment, err := h.Service.AddComment(ctx, other.ID, pub.ID, g.ID, "Why the slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := h.Service.RaiseActionItem(ctx, admin.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, CommentID: comment.ID, Text: "Explain the slip.", OwnerID: owner.ID, DueDate: h.Clock.Now().AddDate(0, 0, 7),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}

	ts := newServer(t, h)
	clients := map[string]*http.Client{}
	client := func(addr string) *http.Client {
		if clients[addr] == nil {
			clients[addr] = signInClient(t, ts.URL, addr)
		}
		return clients[addr]
	}
	reportPath := fmt.Sprintf("/reports/%d", def.ID)
	pubPath := fmt.Sprintf("%s/publications/%d", reportPath, pub.ID)
	pages := []struct{ as, path string }{
		{owner.Email, "/goals?org=all"},
		{owner.Email, fmt.Sprintf("/goals/%d", g.ID)},
		{owner.Email, "/home"},
		{owner.Email, "/links"},
		{owner.Email, "/follow-ups?org=all"},
		{owner.Email, "/signals"},
		{owner.Email, "/freshness"},
		{other.Email, "/home"},
		{other.Email, "/handoffs"},
		{other.Email, "/delegates"},
		{adminEmail, "/admin"},
		{adminEmail, reportPath},
		{adminEmail, pubPath},
	}
	fullName := regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+$`)
	for _, p := range pages {
		body := getBody(t, client(p.as), ts.URL+p.path)
		page := parsePage(t, body)
		if shown := visibleEmails(page); len(shown) > 0 {
			t.Errorf("%s as %s shows emails as text: %q", p.path, p.as, shown)
		}
		if nested := goalNestedControls(body); len(nested) > 0 {
			t.Errorf("%s as %s nests a control inside another: %q", p.path, p.as, nested)
		}
		if !slices.ContainsFunc(goalPeopleShownIn(page), func(s goalShownPerson) bool {
			return s.Discloses && fullName.MatchString(s.Label) && strings.HasSuffix(s.Title, "@example.com") && strings.HasSuffix(s.Email, "@example.com")
		}) {
			t.Errorf("%s as %s shows nobody by Name with their email a click away", p.path, p.as)
		}
	}
	if page := getPage(t, client(owner.Email), fmt.Sprintf("%s/goals/%d", ts.URL, g.ID)); !goalShows(page, owner.Email, owner.Name) {
		t.Errorf("Goal page does not show its Owner as %s with their email a click away", owner.Name)
	}
	printed := getPage(t, client(adminEmail), ts.URL+pubPath+"/print")
	if want := owner.Name + " (" + owner.Email + ")"; !goalHasTextRun(printed, want) {
		t.Errorf("print page does not introduce the Owner as %s", want)
	}
	if controls := findAll(printed, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "disclose") }); len(controls) > 0 {
		t.Errorf("print page puts people behind a control: %s", renderNode(controls[0]))
	}
}

// goalHasTextRun reports whether a run of text under root, one text node
// between two tags, reads exactly s.
func goalHasTextRun(root *nethtml.Node, s string) bool {
	for n := range root.Descendants() {
		if n.Type == nethtml.TextNode && n.Data == s {
			return true
		}
	}
	return false
}

// goalNestedControls returns the opening tag of every interactive element, a
// link, button, label or summary, a page opens inside another one, which HTML
// forbids. It reads the page's tags as html's tokenizer reads them, not the
// parsed page, since the parser would close an outer link or button before
// the nested one opens and so hide the nesting.
func goalNestedControls(page string) []string {
	interactive := map[string]bool{"a": true, "button": true, "label": true, "summary": true}
	var nested []string
	depth := 0
	z := nethtml.NewTokenizer(strings.NewReader(page))
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			return nested
		}
		raw := string(z.Raw())
		name, _ := z.TagName()
		if !interactive[string(name)] {
			continue
		}
		switch tt {
		case nethtml.EndTagToken:
			depth = max(depth-1, 0)
		case nethtml.StartTagToken:
			if depth > 0 {
				nested = append(nested, raw)
			}
			depth++
		}
	}
}

// On the Goal page an Extendable Dimension, one-value or several-values, has an
// "add a value" input beside its choices; a Fixed one has none. Submitting it
// adds the value to the end of the list, in the order added, and sets it on
// the Goal in one step (CONTEXT.md: Extendable).
func TestOwnerAddsValueToExtendableDimensionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	partners := h.CreateExtendableDimension(boss, "Partner", "Initech")
	h.SetDimensionSelection(boss, partners, domain.SelectionSeveral)
	h.CreateDimension(boss, "Pillar", "Growth")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	edit := goalOpenFormIn(t, getPage(t, samClient, goalURL+"?open=dimensions"), "dimensions")
	var adds []string
	for _, f := range formFields(edit, "new_value") {
		adds = append(adds, f.Label)
	}
	for _, name := range []string{"Customer", "Partner"} {
		if !slices.Contains(adds, "Add a "+name+" value") {
			t.Errorf("Extendable %s has no add-a-value input: %s", name, renderNode(edit))
		}
	}
	if labelled := findAll(edit, func(n *nethtml.Node) bool { return attrOf(n, "aria-label") == "Add a Pillar value" }); len(labelled) > 0 {
		t.Errorf("Fixed Pillar offers an add-a-value input: %s", renderNode(edit))
	}

	for _, add := range []struct {
		dim   domain.Dimension
		value string
	}{{customer, "Globex"}, {partners, "Umbrella"}, {partners, "Hooli"}} {
		resp := postForm(t, samClient, goalURL+"/dimensions", url.Values{
			"dimension_id": {fmt.Sprintf("%d", add.dim.ID)},
			"new_value":    {add.value},
		})
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("adding %s: status %d: %s", add.value, resp.StatusCode, body)
		}
	}
	shown := goalRailDimensionValuesIn(t, getPage(t, samClient, goalURL))
	for _, want := range []string{"Globex", "Umbrella", "Hooli"} {
		if !slices.Contains(shown, want) {
			t.Errorf("Goal page doesn't show the added %s: %q", want, shown)
		}
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Partner")); !slices.Equal(got, []string{"Initech", "Umbrella", "Hooli"}) {
		t.Errorf("Partner list = %v, want [Initech Umbrella Hooli]", got)
	}
}

// goalRailDimensionValuesIn is what each Dimension value the Goal page's
// Dimensions section shows reads, in order.
func goalRailDimensionValuesIn(t *testing.T, root *nethtml.Node) []string {
	t.Helper()
	return textsOf(findAllByTestID(findByTestID(t, root, "goal-dimensions"), "goal-dimension-value"))
}

func dimensionValueNames(d domain.Dimension) []string {
	out := make([]string, 0, len(d.Values))
	for _, v := range d.Values {
		out = append(out, v.Value)
	}
	return out
}

// Adding from the Goal page: on a Fixed Dimension an Owner's new value is
// refused with 403 and nothing is added; "ACME " when Acme exists sets Acme and
// adds nothing; a match on a Retired value is refused with a message saying so
// (CONTEXT.md: Fixed, Extendable, Retired).
func TestAddingValueFromGoalPageRefusalsAndMatchesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme", "Hooli")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, customer.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	samClient := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	add := func(dim domain.Dimension, value string) (int, string) {
		resp := postForm(t, samClient, goalURL+"/dimensions", url.Values{
			"dimension_id": {fmt.Sprintf("%d", dim.ID)},
			"new_value":    {value},
		})
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		return resp.StatusCode, body
	}

	if status, _ := add(pillar, "Reliability"); status != http.StatusForbidden {
		t.Errorf("Owner adding to Fixed Pillar: status %d, want 403", status)
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Pillar")); !slices.Equal(got, []string{"Growth"}) {
		t.Errorf("Pillar list = %v, want [Growth] with nothing added", got)
	}

	if status, body := add(customer, "ACME "); status != http.StatusOK {
		t.Fatalf("adding ACME: status %d: %s", status, body)
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Customer")); !slices.Equal(got, []string{"Acme", "Hooli"}) {
		t.Errorf("Customer list = %v, want [Acme Hooli] with nothing created", got)
	}
	values, err := h.Service.GoalValues(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	if len(values) != 1 || values[0].ID != customer.Values[0].ID {
		t.Errorf("values = %+v, want the existing Acme", values)
	}

	status, body := add(customer, "hooli")
	if status != http.StatusUnprocessableEntity || !strings.Contains(textOf(parsePage(t, body)), "Hooli is retired") {
		t.Errorf("adding hooli: status %d body %q, want 422 saying Hooli is retired", status, body)
	}
}

// A Delegate sees the Goal page's Dimension value controls and sets values and
// adds to an Extendable list through them; an Account with no role on the Goal
// sees no controls and is refused with 403 (CONTEXT.md: Delegate).
func TestDelegateSetsAndAddsDimensionValuesButAStrangerCannotOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("cory@example.com")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dee, goal.ID)
	ts := newServer(t, h)
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	deeClient := signInClient(t, ts.URL, "dee@example.com")
	coryClient := signInClient(t, ts.URL, "cory@example.com")

	if page := getPage(t, deeClient, goalURL); goalMenuItemsIn(t, page)["Edit Dimension values"] == "" || !hasTestID(page, "open-dimensions") {
		t.Fatalf("Delegate's Goal page doesn't offer to edit Dimension values:\n%s", renderNode(page))
	}
	goalOpenFormIn(t, getPage(t, deeClient, goalURL+"?open=dimensions"), "dimensions")
	for _, form := range []url.Values{
		{"value_id": {fmt.Sprintf("%d", pillar.Values[1].ID)}},
		{"dimension_id": {fmt.Sprintf("%d", customer.ID)}, "new_value": {"Globex"}},
	} {
		resp := postForm(t, deeClient, goalURL+"/dimensions", form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("Delegate posting %v: status %d: %s", form, resp.StatusCode, body)
		}
	}
	shown := goalRailDimensionValuesIn(t, getPage(t, deeClient, goalURL))
	for _, want := range []string{"Reliability", "Globex"} {
		if !slices.Contains(shown, want) {
			t.Errorf("Goal page doesn't show %s set by the Delegate: %q", want, shown)
		}
	}

	if page := getPage(t, coryClient, goalURL+"?open=dimensions"); len(goalOpenFormsIn(page)) > 0 || hasTestID(page, "open-dimensions") {
		t.Errorf("stranger's Goal page offers Dimension value controls:\n%s", renderNode(page))
	}
	for _, form := range []url.Values{
		{"value_id": {fmt.Sprintf("%d", pillar.Values[0].ID)}},
		{"dimension_id": {fmt.Sprintf("%d", customer.ID)}, "new_value": {"Initech"}},
	} {
		resp := postForm(t, coryClient, goalURL+"/dimensions", form)
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("stranger posting %v: status %d, want 403", form, resp.StatusCode)
		}
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Customer")); !slices.Equal(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex]", got)
	}
}

// The Goal page is one flat surface in every Lifecycle: nothing on it is a
// card, the Proposed Goal's Define this Goal and the More menu included, with
// a Metric, a Milestone, a Highlight, a Draft Highlight and a parent on show
// (DESIGN.md § System: One surface).
func TestGoalPageHasNoCardInAnyLifecycleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "Revenue funds the rest.")
	withMetric := func(g domain.Goal) domain.Goal {
		t.Helper()
		if _, err := h.Service.AddMetric(t.Context(), g.Owner.ID, domain.AddMetricInput{
			GoalID: g.ID, Name: "Incidents", Unit: "per month", Direction: domain.MetricDown,
			Baseline: 9, Target: 2, TargetDate: testsupport.Epoch.AddDate(0, 6, 0),
		}); err != nil {
			t.Fatalf("AddMetric: %v", err)
		}
		return g
	}
	proposed := withMetric(h.CreateGoal(sam, "Tidy the backlog", "Nobody can find anything."))
	active := withMetric(h.ActiveChildOf(sam, parent, "Reduce outages", "Outages cost trust."))
	h.CheckinWithHighlights(sam, active.ID, domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."})
	h.LogDraftHighlight(sam, active.ID, domain.HighlightAccomplishment, "Paged nobody all week.")
	onHold := h.OnHoldGoal(sam, "Rewrite the pager", "The pager is old.", "Waiting on budget.")
	done := h.ActiveGoal(sam, "Ship the status page", "Customers ask.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	cancelled := h.ActiveGoal(sam, "Move to the new region", "Latency.")
	h.EndGoalInCheckin(sam, cancelled.ID, domain.LifecycleCancelled)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for lifecycle, g := range map[string]domain.Goal{
		domain.LifecycleProposed:  proposed,
		domain.LifecycleActive:    active,
		domain.LifecycleOnHold:    onHold,
		domain.LifecycleDone:      done,
		domain.LifecycleCancelled: cancelled,
	} {
		page := getPage(t, client, goalPageURL(ts.URL, g))
		if !goalShowsLifecycle(page, lifecycle) {
			t.Errorf("%s: the page doesn't show its Lifecycle", lifecycle)
		}
		if cards := goalPageCardsIn(page); len(cards) > 0 {
			t.Errorf("a %s Goal's page has a card, class=%q", lifecycle, attrOf(cards[0], "class"))
		}
	}
}

// goalShowsLifecycle reports whether the Goal page under root shows lifecycle
// as its Lifecycle.
func goalShowsLifecycle(root *nethtml.Node, lifecycle string) bool {
	return slices.Contains(textsOf(findAllByTestID(root, "goal-lifecycle")), lifecycle)
}

// goalPageCardsIn returns each element under root whose class names card on
// its own, not as part of another class's name.
func goalPageCardsIn(root *nethtml.Node) []*nethtml.Node {
	return findAll(root, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "card") })
}

// The Goal page stays one flat surface whoever reads it and whatever form is
// open in place: an Admin's and a stranger's view have no card, as the
// Owner's hasn't, and nor has the page with any of its ?open= forms open,
// each opened by someone it is offered to (DESIGN.md § System: One
// surface).
func TestGoalPageHasNoCardForAnyViewerOrOpenFormOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	lee := h.SignIn("lee@example.com")
	h.SignIn("kim@example.com")
	h.CreateDimension(ada, "Pillar", "Growth", "Reliability")
	h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	parent := h.ActiveGoal(sam, "Grow revenue", "Revenue funds the rest.")
	active := h.ActiveChildOf(sam, parent, "Reduce outages", "Outages cost trust.")
	if _, err := h.Service.AddMetric(t.Context(), active.Owner.ID, domain.AddMetricInput{
		GoalID: active.ID, Name: "Incidents", Unit: "per month", Direction: domain.MetricDown,
		Baseline: 9, Target: 2, TargetDate: testsupport.Epoch.AddDate(0, 6, 0),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	h.CheckinWithHighlights(sam, active.ID, domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."})
	h.LogDraftHighlight(sam, active.ID, domain.HighlightAccomplishment, "Paged nobody all week.")
	orphaned := h.ActiveGoal(lee, "Migrate displays", "Displays fail often.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, lee.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	clients := map[string]*http.Client{}
	for _, viewer := range []string{"ada@example.com", "sam@example.com", "kim@example.com"} {
		clients[viewer] = signInClient(t, ts.URL, viewer)
	}

	for viewer, client := range clients {
		page := getPage(t, client, goalPageURL(ts.URL, active))
		if !goalShowsLifecycle(page, domain.LifecycleActive) {
			t.Errorf("%s: the page doesn't show the Goal", viewer)
		}
		if cards := goalPageCardsIn(page); len(cards) > 0 {
			t.Errorf("%s's view of the Goal page has a card, class=%q", viewer, attrOf(cards[0], "class"))
		}
	}

	for _, tc := range []struct {
		form   string
		viewer string
		goal   domain.Goal
	}{
		{"handoff", "sam@example.com", active},
		{"reassign", "ada@example.com", orphaned},
		{"depart", "ada@example.com", active},
		{"return", "ada@example.com", orphaned},
		{"top-level", "ada@example.com", active},
		{"parent-link", "sam@example.com", active},
		{"delegates", "sam@example.com", active},
		{"milestones", "sam@example.com", active},
		{"dimensions", "sam@example.com", active},
		{"fields", "sam@example.com", active},
		{"draft-highlight", "sam@example.com", active},
		{"title-why-it-matters", "sam@example.com", active},
	} {
		page := getPage(t, clients[tc.viewer], goalPageURL(ts.URL, tc.goal)+"?open="+tc.form)
		if open := goalOpenFormIn(t, page, tc.form); !hasTestID(open, "cancel-form") {
			t.Errorf("the open %s form has no Cancel link: %s", tc.form, renderNode(open))
		}
		if cards := goalPageCardsIn(page); len(cards) > 0 {
			t.Errorf("the Goal page with %s open has a card, class=%q", tc.form, attrOf(cards[0], "class"))
		}
	}
}

// The Goal page's document column reads Latest status, Metrics, Milestones,
// Highlights, Draft Highlights, Action Items, Comments and History, in that
// order, each a section opening with its h2; the rail's groups open with an h3
// (DESIGN.md § Goal page sections, § Properties rail).
func TestGoalPageSectionHeadings(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	page := goalPageWithEverySection(t, h, ts)

	main := findByTestID(t, page, "goal-main")
	var before *nethtml.Node
	for _, tc := range []struct{ tag, testID, heading string }{
		{"section", "goal-checkins", "Latest status"},
		{"section", "goal-metrics", "Metrics"},
		{"section", "goal-milestones", "Milestones"},
		{"section", "goal-highlights", "Highlights"},
		{"section", "goal-draft-highlights", "Draft Highlights"},
		{"section", "goal-action-items", "Action Items"},
		{"section", "goal-comments", "Comments"},
		{"div", "goal-history", "History"},
	} {
		block := assertGoalBlockOpensWith(t, page, tc.tag, tc.testID, "h2", tc.heading)
		if !within(block, main) || before != nil && !precedes(before, block) {
			t.Errorf("%s is not in the document column after the section before it", tc.testID)
		}
		before = block
	}
	sidebar := findByTestID(t, page, "goal-sidebar")
	for _, tc := range []struct{ testID, heading string }{
		{"goal-dimensions", "Dimensions"},
		{"goal-field-block", "Fields"},
		{"goal-parents", "Contributes to"},
		{"goal-children", "Contributed to by"},
	} {
		if block := assertGoalBlockOpensWith(t, page, "section", tc.testID, "h3", tc.heading); !within(block, sidebar) {
			t.Errorf("%s is not in the rail", tc.testID)
		}
	}
}

// goalPageWithEverySection is the Owner's view of an Active Goal whose page
// shows every section its document column can: a Check-in with a Highlight,
// and an open Action Item raised on a Report covering it.
func goalPageWithEverySection(t *testing.T, h *testsupport.Harness, ts *httptest.Server) *nethtml.Node {
	t.Helper()
	f := newGoalActionItemFixture(h)
	h.CheckinWithHighlights(f.author, f.goal.ID, domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."})
	f.raise(f.mbrPub, f.sam, "Explain the slip.", 5, f.goal.ID)
	return getPage(t, signInClient(t, ts.URL, f.author.Email), goalPageURL(ts.URL, f.goal))
}

// assertGoalBlockOpensWith checks the tag element under root carrying testID
// opens with a level heading reading heading: it comes first, or first in the
// head a group whose form opens in place wraps it in, beside its link. It
// returns the element.
func assertGoalBlockOpensWith(t *testing.T, root *nethtml.Node, tag, testID, level, heading string) *nethtml.Node {
	t.Helper()
	block := findByTestID(t, root, testID)
	if block.Data != tag {
		t.Fatalf("%s is a <%s>, want a <%s>", testID, block.Data, tag)
	}
	first := goalFirstContentOf(block)
	if first != nil && first.Type == nethtml.ElementNode && first.Data == "div" {
		first = goalFirstContentOf(first)
	}
	if first == nil || first.Type != nethtml.ElementNode || first.Data != level || textOf(first) != heading {
		got := "nothing"
		if first != nil {
			got = renderNode(first)
		}
		t.Errorf("%s does not open with its %s %s: %.80s", testID, level, heading, got)
	}
	return block
}

// goalFirstContentOf is n's first child that shows anything: an element, or
// text that isn't only whitespace.
func goalFirstContentOf(n *nethtml.Node) *nethtml.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == nethtml.ElementNode:
			return c
		case c.Type == nethtml.TextNode && strings.TrimSpace(c.Data) != "":
			return c
		}
	}
	return nil
}

// A Retired Dimension isn't offered on the Goal page: it has no control to set
// a value and its values aren't child-Goal defaults on /goals/new?parent=. A Goal carrying a value in
// it still shows the value, marked retired, and one carrying none doesn't list
// it. Restoring the Dimension returns its control (CONTEXT.md: Retired).
func TestRetiredDimensionLeavesGoalPageControlsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	quarter := h.CreateSeveralValuesDimension(boss, "Quarter", "Q1", "Q2")
	h.CreateDimension(boss, "Team", "Core")
	growth := pillar.Values[0]
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, growth)
	ctx := context.Background()
	for _, d := range []domain.Dimension{pillar, quarter} {
		if err := h.Service.RetireDimension(ctx, boss.ID, d.ID); err != nil {
			t.Fatalf("RetireDimension %s: %v", d.Name, err)
		}
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	page := getPage(t, client, goalURL+"?open=dimensions")
	edit := goalOpenFormIn(t, page, "dimensions")
	section := findByTestID(t, page, "goal-dimensions")
	shownValues := goalOutside(findAllByTestID(section, "goal-dimension-value"), edit)
	if !slices.Contains(textsOf(shownValues), "Growth") || len(goalOutside(findAllByTestID(section, "retired"), edit)) == 0 {
		t.Errorf("Growth isn't shown marked retired: %s", renderNode(section))
	}
	for _, listed := range goalOutside(findAllByTestID(section, "goal-dimension"), edit) {
		if strings.Contains(textOf(listed), "Quarter") {
			t.Errorf("a Retired Dimension the Goal carries no value in is listed: %s", renderNode(listed))
		}
	}
	for _, d := range []domain.Dimension{pillar, quarter} {
		if goalMentions(edit, d.Name) {
			t.Errorf("the Retired %s is offered for setting: %s", d.Name, renderNode(edit))
		}
	}
	if !goalMentions(edit, "Team") {
		t.Errorf("the live Team isn't offered for setting: %s", renderNode(edit))
	}
	childURL := fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, goal.ID)
	fits := findByTestID(t, getPage(t, client, childURL), "goal-form-fits")
	if options := goalChoicesOf(fits, growth); len(options) > 0 {
		t.Errorf("a value of a Retired Dimension is offered as a child default: %s", renderNode(fits))
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	page = getPage(t, client, goalURL+"?open=dimensions")
	edit = goalOpenFormIn(t, page, "dimensions")
	if marks := goalOutside(findAllByTestID(findByTestID(t, page, "goal-dimensions"), "retired"), edit); len(marks) > 0 {
		t.Errorf("Growth is still marked retired after Pillar is restored: %s", renderNode(marks[0].Parent))
	}
	if labelled := findAll(edit, func(n *nethtml.Node) bool { return attrOf(n, "aria-label") == "Pillar" }); len(labelled) == 0 {
		t.Errorf("the restored Pillar isn't offered for setting: %s", renderNode(edit))
	}
	fits = findByTestID(t, getPage(t, client, childURL), "goal-form-fits")
	if choices := goalChoicesOf(fits, growth); len(choices) == 0 || !hasAttr(choices[0], "checked") && !hasAttr(choices[0], "selected") {
		t.Errorf("Growth isn't offered as a child default once Pillar is restored")
	}
}

// goalOutside is nodes less those inside skip.
func goalOutside(nodes []*nethtml.Node, skip *nethtml.Node) []*nethtml.Node {
	var out []*nethtml.Node
	for _, n := range nodes {
		if !within(n, skip) {
			out = append(out, n)
		}
	}
	return out
}

// goalChoicesOf returns the options and inputs under root that choose v.
func goalChoicesOf(root *nethtml.Node, v domain.DimensionValue) []*nethtml.Node {
	id := strconv.FormatInt(v.ID, 10)
	return findAll(root, func(n *nethtml.Node) bool {
		return (n.Data == "option" || n.Data == "input") && attrOf(n, "value") == id
	})
}

// goalMentions reports whether anything under root reads or is named s: its
// text, every option of a select included and hidden text too, or an
// attribute's value.
func goalMentions(root *nethtml.Node, s string) bool {
	nodes := append([]*nethtml.Node{root}, slices.Collect(root.Descendants())...)
	for _, n := range nodes {
		if n.Type == nethtml.TextNode && strings.Contains(n.Data, s) {
			return true
		}
		for _, a := range n.Attr {
			if strings.Contains(a.Val, s) {
				return true
			}
		}
	}
	return false
}

// A Retired Dimension drops out of the Goal list's filter and grouping, and a
// stale link filtering or grouping by it is ignored rather than narrowing the
// list with no control to undo it. A Goal's row still shows its value, marked
// retired. Restoring the Dimension returns it to both (CONTEXT.md: Retired).
// How the value is drawn is pinned in styles_test.go
// (TestRetiredDimensionLeavesGoalListFilterAndGroupingOverHTTPStyles).
func TestRetiredDimensionLeavesGoalListFilterAndGroupingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateDimension(boss, "Team", "Core")
	growth, trust := pillar.Values[0], pillar.Values[1]
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	bravo := h.CreateGoal(sam, "Bravo", "B matters.")
	h.AssignGoalValue(alpha, growth)
	h.AssignGoalValue(bravo, trust)
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals")
	filters := findByTestID(t, page, "goal-filters")
	if goalMentions(filters, "Pillar") || goalMentions(filters, "Growth") {
		t.Errorf("the Retired Pillar is offered to filter or group by: %s", renderNode(filters))
	}
	if !goalHasElementReading(filters, "legend", "Team") || !goalHasElementReading(filters, "option", "Team") {
		t.Errorf("the live Team isn't offered to filter and group by: %s", renderNode(filters))
	}
	row := goalListRowOf(t, page, alpha)
	if link := linkTo(row, goalHref(alpha.ID)); link == nil || textOf(link) != "Alpha" ||
		!slices.Contains(textsOf(findAllByTestID(row, "goal-value-tag")), "Growth") || !hasTestID(row, "retired") {
		t.Errorf("Alpha's row doesn't show Growth marked retired: %s", renderNode(row))
	}

	stale := getPage(t, client, fmt.Sprintf("%s/goals?value=%d&group=%d", ts.URL, growth.ID, pillar.ID))
	if got := goalRowTitlesIn(goalListRowsIn(stale), alpha, bravo); !slices.Equal(got, []string{"Alpha", "Bravo"}) {
		t.Errorf("a stale link on the Retired Pillar lists %q, want both Goals", got)
	}
	if hasTestID(stale, "goal-group") {
		t.Errorf("a stale link groups by the Retired Pillar")
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	filters = findByTestID(t, getPage(t, client, ts.URL+"/goals"), "goal-filters")
	pillarID := strconv.FormatInt(pillar.ID, 10)
	if !goalHasElementReading(filters, "legend", "Pillar") || len(findAll(filters, func(n *nethtml.Node) bool { return n.Data == "option" && attrOf(n, "value") == pillarID })) == 0 {
		t.Errorf("the restored Pillar isn't offered to filter and group by: %s", renderNode(filters))
	}
	if grouped := getPage(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID)); !hasTestID(grouped, "goal-group") {
		t.Errorf("the list doesn't group by the restored Pillar")
	}
}

// goalHasElementReading reports whether a tag element under root reads text.
func goalHasElementReading(root *nethtml.Node, tag, text string) bool {
	return len(findAll(root, func(n *nethtml.Node) bool { return n.Data == tag && textOf(n) == text })) > 0
}

// A Field describes a Goal and is never an update: it is offered in neither the
// Goal list's filter nor its grouping, nor on the Check-in form (ADR 0005).
func TestFieldsStayOffTheGoalListControlsAndCheckinForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CreateDimension(boss, "Pillar", "Growth")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	notes := h.CreateField(boss, "Rationale", domain.FieldLongText, "")
	h.SetGoalField(sam, goal, budget, "1200")
	h.SetGoalField(sam, goal, notes, "Because outages")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	filters := findByTestID(t, getPage(t, client, ts.URL+"/goals"), "goal-filters")
	if !goalMentions(filters, "Pillar") {
		t.Fatalf("the Goal list's controls don't offer the Pillar Dimension: %s", renderNode(filters))
	}
	for _, name := range []string{"Budget", "Rationale"} {
		if goalMentions(filters, name) {
			t.Errorf("the Goal list's filter or grouping offers the %s Field: %s", name, renderNode(filters))
		}
	}

	form := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	for _, text := range []string{"Budget", "Rationale", "Because outages"} {
		if goalMentions(form, text) {
			t.Errorf("the Check-in form shows %q", text)
		}
	}
}

// The Goal list's table layout has a column for the Goal's Health, which leads
// as it does everywhere a Goal's title shows, then its title, Owner,
// Lifecycle, delivery date and last Check-in, then one per live Dimension and
// one per live Field, each in name order; a Retired one has none, nor do
// Metrics and Milestones. A several-values cell lists its values with commas.
func TestGoalTableHasAColumnPerLiveDimensionAndField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Ortiz")
	pillar := h.CreateSeveralValuesDimension(boss, "Pillar", "Growth", "Trust")
	quarter := h.CreateDimension(boss, "Quarter", "Q1")
	h.CreateDimension(boss, "Area", "Payments")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Approver", domain.FieldShortText, "")
	old := h.CreateField(boss, "Legacy code", domain.FieldShortText, "")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, pillar.Values[0])
	h.AssignGoalValue(goal, pillar.Values[1])
	h.AssignGoalValue(goal, quarter.Values[0])
	h.SetGoalField(sam, goal, budget, "1200")
	h.SetGoalField(sam, goal, old, "X-1")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, quarter.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, old.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	ts := newServer(t, h)

	page := getPage(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals?layout=table")
	want := []string{"Health", "Title", "Owner", "Lifecycle", "Delivery date", "Last check-in", "Area", "Pillar", "Approver", "Budget"}
	if got := goalTableHeadsIn(t, page); !slices.Equal(got, want) {
		t.Fatalf("columns = %q, want %q", got, want)
	}
	rows := goalTableRowsIn(t, page)
	if len(rows) != 1 {
		t.Fatalf("got %d table rows, want 1", len(rows))
	}
	row := rows[0]
	title := linkTo(row, goalHref(goal.ID))
	if title == nil || textOf(title) != "Reduce outages" {
		t.Errorf("the title doesn't link to the Goal: %s", renderNode(row))
	}
	chip := healthChipIn(t, row)
	assertHealthChip(t, chip, "Green")
	if title == nil || !precedes(chip, title) {
		t.Errorf("the row's Health doesn't come before its title: %s", renderNode(row))
	}
	// The Owner cell shows the Name, its email disclosed on demand.
	if owner := goalTableCellIn(t, page, row, "Owner"); textOf(owner) != "Sam Ortiz" || !goalShows(owner, "sam@example.com", "Sam Ortiz") {
		t.Errorf("Owner cell = %q, want Sam Ortiz with their email a click away: %s", textOf(owner), renderNode(owner))
	}
	for head, cell := range map[string]string{
		"Health":    "Green",
		"Lifecycle": "Active",
		"Pillar":    "Growth, Trust",
		"Budget":    "1200 $",
		"Area":      "",
		"Approver":  "",
	} {
		if got := textOf(goalTableCellIn(t, page, row, head)); got != cell {
			t.Errorf("%s cell = %q, want %q", head, got, cell)
		}
	}
}

// goalLayoutLinkQueryIn is the query string of the link carrying data-testid
// under root, failing the test unless it is one link to the Goal list.
func goalLayoutLinkQueryIn(t *testing.T, root *nethtml.Node, testID string) url.Values {
	t.Helper()
	link := findByTestID(t, root, testID)
	if link.Data != "a" {
		t.Fatalf("%s isn't a link: %s", testID, renderNode(link))
	}
	u, err := url.Parse(attrOf(link, "href"))
	if err != nil {
		t.Fatalf("%s href: %v", testID, err)
	}
	if u.Path != "/goals" {
		t.Errorf("%s links to %s, want /goals", testID, u.Path)
	}
	return u.Query()
}

// Switching the Goal list between List and Table keeps every filter, and the
// table's URL alone reproduces the same view, so a table link can be shared.
// Applying the filter bar in the table stays in the table.
func TestGoalTableLayoutKeepsFiltersAndIsShareable(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	growth := pillar.Values[0]
	alpha := h.CreateGoal(sam, "Alpha launch", "A matters.")
	alphaTrust := h.CreateGoal(sam, "Alpha audit", "A matters.")
	bravo := h.CreateGoal(sam, "Bravo launch", "B matters.")
	h.AssignGoalValue(alpha, growth)
	h.AssignGoalValue(alphaTrust, pillar.Values[1])
	h.AssignGoalValue(bravo, growth)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	list := getPage(t, client, fmt.Sprintf("%s/goals?org=all&q=alpha&value=%d", ts.URL, growth.ID))
	toTable := goalLayoutLinkQueryIn(t, list, "layout-table")
	if toTable.Get("layout") != "table" || toTable.Get("q") != "alpha" || toTable.Get("value") != fmt.Sprint(growth.ID) {
		t.Fatalf("the Table toggle drops a filter: %v", toTable)
	}

	// A fresh browser following the shared table link sees the same slice.
	table := getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/goals?"+toTable.Encode())
	if got := goalRowTitlesIn(goalTableRowsIn(t, table), alpha, alphaTrust, bravo); !slices.Equal(got, []string{"Alpha launch"}) {
		t.Errorf("the shared table lists %q, want only Alpha launch", got)
	}
	filters := findByTestID(t, table, "goal-filters")
	if !slices.ContainsFunc(formFields(filters, "layout"), func(f pageField) bool { return f.Value == "table" }) {
		t.Errorf("applying the filter bar leaves the table: %s", renderNode(filters))
	}
	growthBox := slices.ContainsFunc(formFields(filters, "value"), func(f pageField) bool { return f.Value == fmt.Sprint(growth.ID) && f.Checked })
	if formField(t, filters, "q").Value != "alpha" || !growthBox {
		t.Errorf("the table's filter bar doesn't show the active filters: %s", renderNode(filters))
	}

	toList := goalLayoutLinkQueryIn(t, table, "layout-list")
	if toList.Has("layout") || toList.Get("q") != "alpha" || toList.Get("value") != fmt.Sprint(growth.ID) {
		t.Errorf("the List toggle drops a filter or stays in the table: %v", toList)
	}
}

// goalTableSortLinkIn is where the Goal table's header for the column head
// links to, failing the test unless the header is a link.
func goalTableSortLinkIn(t *testing.T, root *nethtml.Node, head string) string {
	t.Helper()
	thead := findByTestID(t, root, "goal-table-head")
	for _, th := range findAll(thead, func(n *nethtml.Node) bool { return n.Data == "th" }) {
		if textOf(th) != head {
			continue
		}
		links := findAll(th, func(n *nethtml.Node) bool { return n.Data == "a" && attrOf(n, "href") != "" })
		if len(links) == 0 {
			t.Fatalf("the %s header doesn't sort: %s", head, renderNode(th))
		}
		return attrOf(links[0], "href")
	}
	t.Fatalf("the Goal table has no %s header: %s", head, renderNode(thead))
	return ""
}

// A column header sorts the Goal table by that column, and again reverses it.
// A number Field sorts as numbers, so 9 comes before 10, and the Goals with no
// value sort last both ways. Unsorted, the table keeps the list's problem-first
// order. The sort keeps the filters.
func TestGoalTableSortsByAColumnWithUnsetValuesLast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	nine := h.CreateGoal(sam, "Nine", "It matters.")
	ten := h.CreateGoal(sam, "Ten", "It matters.")
	hundred := h.CreateGoal(sam, "Hundred", "It matters.")
	unset := h.ActiveGoal(sam, "Unset", "It matters.")
	h.Checkin(sam, unset.ID, domain.HealthRed, "On fire.", "Put it out.", testsupport.Epoch.AddDate(0, 2, 0))
	h.CreateGoal(sam, "Excluded", "It matters.")
	h.SetGoalField(sam, nine, budget, "9")
	h.SetGoalField(sam, ten, budget, "10")
	h.SetGoalField(sam, hundred, budget, "100")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	order := func(page *nethtml.Node) []string {
		return goalRowTitlesIn(goalTableRowsIn(t, page), nine, ten, hundred, unset)
	}

	page := getPage(t, client, ts.URL+"/goals?layout=table&q=n")
	if got, want := order(page), []string{"Unset", "Hundred", "Nine", "Ten"}; !slices.Equal(got, want) {
		t.Errorf("unsorted order = %q, want the problem-first %q", got, want)
	}

	up := goalTableSortLinkIn(t, page, "Budget")
	page = getPage(t, client, ts.URL+up)
	if got, want := order(page), []string{"Nine", "Ten", "Hundred", "Unset"}; !slices.Equal(got, want) {
		t.Errorf("sorted by Budget = %q, want %q", got, want)
	}
	if strings.Contains(textOf(page), "Excluded") {
		t.Errorf("sorting dropped the search filter")
	}

	down := goalTableSortLinkIn(t, page, "Budget")
	if down == up {
		t.Fatalf("sorting by Budget again doesn't reverse it: %s", down)
	}
	page = getPage(t, client, ts.URL+down)
	if got, want := order(page), []string{"Hundred", "Ten", "Nine", "Unset"}; !slices.Equal(got, want) {
		t.Errorf("reverse-sorted by Budget = %q, want %q", got, want)
	}
	if again := goalTableSortLinkIn(t, page, "Budget"); again != up {
		t.Errorf("a third click on Budget links to %s, want ascending again %s", again, up)
	}
}

// goalTableSubmitColumns submits the Goal table's Columns form under page as
// a browser would, with the columns labelled hide unticked and every other
// column ticked, and returns the page it lands on.
func goalTableSubmitColumns(t *testing.T, client *http.Client, base string, page *nethtml.Node, hide ...string) *nethtml.Node {
	t.Helper()
	form := findByTestID(t, page, "goal-columns")
	if form.Data != "form" || attrOf(form, "action") != "/goals" || attrOf(form, "method") != "get" {
		t.Fatalf("the Columns form doesn't GET /goals: %s", renderNode(form))
	}
	values := url.Values{}
	for _, input := range findAll(form, func(n *nethtml.Node) bool { return n.Data == "input" }) {
		switch attrOf(input, "type") {
		case "hidden":
			values.Add(attrOf(input, "name"), attrOf(input, "value"))
		case "checkbox":
			if !slices.Contains(hide, fieldLabel(input)) {
				values.Add(attrOf(input, "name"), attrOf(input, "value"))
			}
		}
	}
	return getPage(t, client, base+"/goals?"+values.Encode())
}

// A Columns control hides any Goal table column but Title. It is a plain form,
// so it works without JavaScript, and the choice is remembered in that browser:
// the column stays hidden on the next visit, while another browser still shows
// it. Hiding keeps the filters.
func TestGoalTableHiddenColumnStaysHiddenOnTheNextVisit(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	h.CreateField(boss, "Budget", domain.FieldNumber, "")
	h.CreateGoal(sam, "Alpha", "It matters.")
	h.CreateGoal(sam, "Bravo", "It matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	all := []string{"Health", "Title", "Owner", "Lifecycle", "Delivery date", "Last check-in", "Pillar", "Budget"}

	page := getPage(t, client, ts.URL+"/goals?layout=table&q=alpha")
	if got := goalTableHeadsIn(t, page); !slices.Equal(got, all) {
		t.Fatalf("columns = %q, want %q", got, all)
	}
	form := findByTestID(t, page, "goal-columns")
	if titles := findAll(form, func(n *nethtml.Node) bool { return attrOf(n, "value") == "title" }); len(titles) > 0 {
		t.Errorf("the Columns control offers to hide Title: %s", renderNode(form))
	}

	page = goalTableSubmitColumns(t, client, ts.URL, page, "Owner", "Budget")
	want := []string{"Health", "Title", "Lifecycle", "Delivery date", "Last check-in", "Pillar"}
	if got := goalTableHeadsIn(t, page); !slices.Equal(got, want) {
		t.Errorf("after hiding Owner and Budget, columns = %q, want %q", got, want)
	}
	if strings.Contains(textOf(findByTestID(t, page, "goal-table")), "Bravo") {
		t.Errorf("hiding columns dropped the search filter")
	}

	next := getPage(t, client, ts.URL+"/goals?layout=table")
	if got := goalTableHeadsIn(t, next); !slices.Equal(got, want) {
		t.Errorf("on the next visit, columns = %q, want %q", got, want)
	}
	if got := goalTableHeadsIn(t, getPage(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/goals?layout=table")); !slices.Equal(got, all) {
		t.Errorf("another browser's columns = %q, want all of %q", got, all)
	}

	everything := goalTableSubmitColumns(t, client, ts.URL, next, all...)
	if got := goalTableHeadsIn(t, everything); !slices.Equal(got, []string{"Title"}) {
		t.Errorf("hiding every column leaves %q, want Title alone", got)
	}
	restored := goalTableSubmitColumns(t, client, ts.URL, everything)
	if got := goalTableHeadsIn(t, restored); !slices.Equal(got, all) {
		t.Errorf("showing every column again leaves %q, want %q", got, all)
	}
}

// The Goal table has no totals row: no sum, count or average of a column,
// since numbers in a Field are never added up across Goals (ADR 0005). Grouping
// doesn't apply to it. That it scrolls sideways inside its own container on a
// narrow screen is TestGoalTableHasNoTotalsRowAndIsntGroupedStyles's.
func TestGoalTableHasNoTotalsRowAndIsntGrouped(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	alpha := h.CreateGoal(sam, "Alpha", "It matters.")
	bravo := h.CreateGoal(sam, "Bravo", "It matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	h.AssignGoalValue(bravo, pillar.Values[1])
	h.SetGoalField(sam, alpha, budget, "10")
	h.SetGoalField(sam, bravo, budget, "20")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, fmt.Sprintf("%s/goals?layout=table&group=%d", ts.URL, pillar.ID))
	table := findByTestID(t, page, "goal-table")
	if footers := findAll(table, func(n *nethtml.Node) bool { return n.Data == "tfoot" }); len(footers) > 0 || hasTestID(table, "goal-group") {
		t.Errorf("the Goal table has a footer or a group: %s", renderNode(table))
	}
	if rows, trs := len(goalTableRowsIn(t, page)), len(findAll(table, func(n *nethtml.Node) bool { return n.Data == "tr" })); rows != 2 || trs != 3 {
		t.Errorf("the Goal table has %d Goal rows among %d rows, want the header and 2 Goals: %s", rows, trs, renderNode(table))
	}
	for _, sum := range []string{"30", "15"} {
		if cells := findAll(table, func(n *nethtml.Node) bool { return textOf(n) == sum }); len(cells) > 0 {
			t.Errorf("the Goal table shows an aggregate %q: %s", sum, renderNode(cells[0]))
		}
	}
	for _, aggregate := range []string{"Total", "Sum", "Average"} {
		if strings.Contains(textOf(table), aggregate) {
			t.Errorf("the Goal table shows an aggregate %q: %s", aggregate, renderNode(table))
		}
	}
	filters := findByTestID(t, page, "goal-filters")
	groupBy := slices.ContainsFunc(formFields(filters, "group"), func(f pageField) bool { return f.Type == "select" })
	if groupBy || strings.Contains(textOf(filters), "Group by") {
		t.Errorf("the table offers Group by, which doesn't apply to it: %s", renderNode(filters))
	}
}

// goalValueHistoryIn is the Goal page's History at goalURL listing only what
// its Values chip lists, newest first, each entry checked to be a value
// change.
func goalValueHistoryIn(t *testing.T, client *http.Client, goalURL string) []*nethtml.Node {
	t.Helper()
	history := findByTestID(t, getPage(t, client, goalURL+"?history=values&shown=25"), "goal-history")
	entries := findAllByTestID(history, "history-entry")
	for _, e := range entries {
		if kind := attrOf(e, "data-kind"); kind != "value" {
			t.Errorf("the Values chip lists a %s entry: %s", kind, renderNode(e))
		}
	}
	return entries
}

// Every change to a Goal's Dimension values and Fields made through the Goal
// page is on its History timeline, newest first under the Values chip, each
// with who made it by Name and when. A Delegate's change is attributed to the
// Delegate, a value added or removed in a several-values Dimension reads as
// "added X" or "removed X", a save that changes nothing adds no entry, and
// renaming a value afterwards leaves the entries reading as they did.
func TestGoalPageListsValueHistory(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Owner")
	dee := h.SignInNamed("dee@example.com", "Dee Delegate")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(pat, dee, goal.ID)
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	region := h.CreateSeveralValuesDimension(boss, "Region", "EMEA", "APAC")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)
	patClient := signInClient(t, ts.URL, pat.Email)
	deeClient := signInClient(t, ts.URL, dee.Email)
	post := func(client *http.Client, path string, form url.Values) {
		t.Helper()
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/%s", ts.URL, goal.ID, path), form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s %v: status %d; body:\n%s", path, form, resp.StatusCode, body)
		}
		h.Clock.Advance(time.Hour)
	}
	id := func(n int64) string { return fmt.Sprint(n) }

	post(patClient, "fields", url.Values{"field_id": {id(budget.ID)}, "value": {"200"}})
	post(deeClient, "fields", url.Values{"field_id": {id(budget.ID)}, "value": {"350"}})
	post(deeClient, "fields", url.Values{"field_id": {id(budget.ID)}, "value": {"350"}})
	post(patClient, "dimensions", url.Values{"value_id": {id(pillar.Values[0].ID)}})
	post(patClient, "dimensions", url.Values{"value_id": {id(pillar.Values[0].ID)}})
	post(patClient, "dimensions", url.Values{"dimension_id": {id(region.ID)}, "value_id": {id(region.Values[0].ID)}})
	post(deeClient, "dimensions", url.Values{"dimension_id": {id(region.ID)}, "value_id": {id(region.Values[1].ID)}})
	post(patClient, "fields", url.Values{"field_id": {id(budget.ID)}, "value": {"350"}, "clear": {"1"}})
	if _, err := h.Service.RenameDimensionValue(context.Background(), boss.ID, pillar.Values[0].ID, "Expansion"); err != nil {
		t.Fatalf("RenameDimensionValue: %v", err)
	}

	chips := findByTestID(t, findByTestID(t, getPage(t, patClient, goalPageURL(ts.URL, goal)), "goal-history"), "history-chips")
	values := findAll(chips, func(n *nethtml.Node) bool {
		return attrOf(n, "data-testid") == "history-chip" && attrOf(n, "data-filter") == "values"
	})
	if len(values) != 1 || textOf(values[0]) != "Values 7" {
		t.Errorf("History's chips don't count 7 under Values: %s", renderNode(chips))
	}
	type person struct{ email, name string }
	patShown, deeShown := person{pat.Email, "Pat Owner"}, person{dee.Email, "Dee Delegate"}
	want := []struct {
		change string
		by     person
		at     string
	}{
		{"Budget: cleared (was 350)", patShown, "Jan 2, 22:04"},
		{"Region: added APAC", deeShown, "Jan 2, 21:04"},
		{"Region: removed EMEA", deeShown, "Jan 2, 21:04"},
		{"Region: added EMEA", patShown, "Jan 2, 20:04"},
		{"Pillar: set to Growth", patShown, "Jan 2, 18:04"},
		{"Budget: 200 → 350", deeShown, "Jan 2, 16:04"},
		{"Budget: set to 200", patShown, "Jan 2, 15:04"},
	}
	entries := goalValueHistoryIn(t, patClient, goalPageURL(ts.URL, goal))
	if len(entries) != len(want) {
		t.Fatalf("History lists %d value changes, want %d:\n%s", len(entries), len(want), strings.Join(textsOf(entries), "\n"))
	}
	for i, w := range want {
		text := textOf(entries[i])
		for _, part := range []string{w.change, w.at} {
			if !strings.Contains(text, part) {
				t.Errorf("entry %d lacks %q: %s", i, part, text)
			}
		}
		if !goalShows(entries[i], w.by.email, w.by.name) {
			t.Errorf("entry %d doesn't show %s by Name with their email a click away: %s", i, w.by.name, renderNode(entries[i]))
		}
	}
}

// The Goal table's Download CSV link returns exactly the Goals its filters
// keep, one row each behind an ID column, with a column for every live
// Dimension and Field, even one this browser hides, and the Owner as an email
// (#81).
func TestGoalTableDownloadsTheFilteredGoalsAsCSV(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Lee")
	pillar := h.CreateSeveralValuesDimension(boss, "Pillar", "Growth", "Trust")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	alpha := h.CreateGoal(sam, "Alpha launch", "A matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	h.AssignGoalValue(alpha, pillar.Values[1])
	h.SetGoalField(sam, alpha, budget, "1200")
	h.CreateGoal(sam, "Bravo launch", "B matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableSubmitColumns(t, client, ts.URL, getPage(t, client, ts.URL+"/goals?layout=table&q=alpha"), "Budget")
	u, err := url.Parse(attrOf(findByTestID(t, page, "goal-table-download"), "href"))
	if err != nil {
		t.Fatalf("download href: %v", err)
	}
	if u.Path != "/goals/download" || u.Query().Get("q") != "alpha" {
		t.Fatalf("Download CSV links to %s, want /goals/download keeping q=alpha", u)
	}

	resp, err := ctxGet(t, client, ts.URL+u.String())
	if err != nil {
		t.Fatalf("GET download: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
		t.Errorf("Content-Disposition = %q, want an attached .csv", cd)
	}
	want := "ID,Title,Owner,Why it matters,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar,Budget\n" +
		fmt.Sprintf("%d,Alpha launch,sam@example.com,A matters.,,,,,,Growth; Trust,1200\n", alpha.ID)
	if body != want {
		t.Errorf("download =\n%s\nwant\n%s", body, want)
	}
}

// goalTableInputsIn returns the form controls under root a person types or
// picks a value in: every input, select and textarea but a hidden input.
func goalTableInputsIn(root *nethtml.Node) []*nethtml.Node {
	return findAll(root, func(n *nethtml.Node) bool {
		return n.Data == "select" || n.Data == "textarea" || n.Data == "input" && attrOf(n, "type") != "hidden"
	})
}

// goalTableEditMode follows the Goal table's Edit values link from the table
// at rawURL, returning the table in edit mode.
func goalTableEditMode(t *testing.T, client *http.Client, baseURL, rawURL string) *nethtml.Node {
	t.Helper()
	edit := findByTestID(t, getPage(t, client, baseURL+rawURL), "goal-table-edit")
	if edit.Data != "a" || textOf(edit) != "Edit values" {
		t.Fatalf("the table's Edit values isn't a link: %s", renderNode(edit))
	}
	return getPage(t, client, baseURL+attrOf(edit, "href"))
}

// In edit mode the Goal table is one form with one Save: the rows of the Goals
// the person may set values on — as Owner, Delegate or Admin — get an input in
// each Dimension and Field cell, and every other row stays read-only. Title,
// Owner, Health, Lifecycle and delivery date never get one, since changing
// them takes a Check-in, a Date Slip reason, a Handoff or a Lifecycle reason
// (#80; CONTEXT.md: Delegate).
func TestGoalTableEditModeHasInputsOnlyOnRowsThePersonMayEdit(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateSeveralValuesDimension(boss, "Tags", "infra", "ux")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Notes", domain.FieldLongText, "")
	alpha := h.ActiveGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(pat, "Beta", "B matters.")
	gamma := h.CreateGoal(pat, "Gamma", "C matters.")
	h.AddDelegate(pat, sam, gamma.ID)
	ts := newServer(t, h)

	page := goalTableEditMode(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL, "/goals?layout=table&org=all")

	form := findByTestID(t, page, "goal-table-form")
	if form.Data != "form" || attrOf(form, "method") != "post" || !strings.HasPrefix(attrOf(form, "action"), "/goals/values") {
		t.Errorf("edit mode isn't a plain form POST to /goals/values: %s", renderNode(form))
	}
	if n := len(findAllByTestID(page, "goal-table-save")); n != 1 {
		t.Errorf("edit mode has %d Save buttons, want 1", n)
	}
	for _, tc := range []struct {
		goal     domain.Goal
		editable bool
	}{{alpha, true}, {beta, false}, {gamma, true}} {
		row := goalTableRowOf(t, page, tc.goal)
		for _, head := range []string{"Pillar", "Tags", "Budget", "Notes"} {
			if got := len(goalTableInputsIn(goalTableCellIn(t, page, row, head))) > 0; got != tc.editable {
				t.Errorf("%s's %s cell has an input = %v, want %v", tc.goal.Title, head, got, tc.editable)
			}
		}
		for _, head := range []string{"Title", "Owner", "Health", "Lifecycle", "Delivery date", "Last check-in"} {
			if cell := goalTableCellIn(t, page, row, head); len(goalTableInputsIn(cell)) > 0 {
				t.Errorf("%s's %s cell has an input: %s", tc.goal.Title, head, renderNode(cell))
			}
		}
	}
}

// A Goal with no Health and no delivery date shows nothing in those cells, in
// the Goal table and in its edit mode, rather than a link with no text that
// the keyboard stops on and a screen reader announces unnamed (#100).
func TestGoalTableHasNoEmptyLinks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignIn("sam@example.com")
	h.CreateGoal(sam, "Alpha", "A matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for mode, page := range map[string]*nethtml.Node{
		"table":     getPage(t, client, ts.URL+"/goals?layout=table"),
		"edit mode": goalTableEditMode(t, client, ts.URL, "/goals?layout=table"),
	} {
		for _, link := range findAll(findByTestID(t, page, "goal-table"), func(n *nethtml.Node) bool { return n.Data == "a" }) {
			if textOf(link) == "" {
				t.Errorf("the Goal table in %s has a link with no text: %s", mode, renderNode(link))
			}
		}
	}
}

// The Owner cell in the Goal table's edit mode is the person control that
// reveals the Owner's email, not a link: a link can't hold that control, and
// the Title already links to the Goal (#100, withdrawing that part of #80).
func TestGoalTableEditModeOwnerCellIsThePersonControl(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	sam := h.SignIn("sam@example.com")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	ts := newServer(t, h)

	page := goalTableEditMode(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL, "/goals?layout=table")

	cell := goalTableCellIn(t, page, goalTableRowOf(t, page, alpha), "Owner")
	var children []*nethtml.Node
	for c := cell.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.ElementNode {
			children = append(children, c)
		}
	}
	shown := goalPeopleShownIn(cell)
	if len(children) != 1 || !flatFrameHasClass(children[0], "person") || len(shown) != 1 || !goalShows(cell, "sam@example.com", "sam") || textOf(cell) != "sam" {
		t.Errorf("Alpha's Owner cell isn't just the person control: %s", renderNode(cell))
	}
	if links := findAll(cell, func(n *nethtml.Node) bool { return n.Data == "a" }); len(links) > 0 {
		t.Errorf("Alpha's Owner cell is a link: %s", renderNode(cell))
	}
}

// A refused several-values cell in the Goal table's edit form is announced the
// way a refused select or text input is: each of its checkboxes is marked
// invalid, and their group points at the cell's error message (#100).
func TestGoalTableRefusedSeveralValuesCellIsAnnounced(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	tags := h.CreateExtendableDimension(boss, "Tags", "infra", "ux")
	h.SetDimensionSelection(boss, tags, domain.SelectionSeveral)
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Add a Tags value to Alpha"), "mobile; web")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.StatusCode, body)
	}

	refused := parsePage(t, body)
	cell := goalTableCellIn(t, refused, goalTableRowOf(t, refused, alpha), "Tags")
	errs := findAllByTestID(cell, "cell-error")
	if len(errs) != 1 || errs[0].Data != "p" || attrOf(errs[0], "id") == "" {
		t.Fatalf("Alpha's Tags has no error message with an id: %s", renderNode(cell))
	}
	errorID := attrOf(errs[0], "id")
	groups := findAll(cell, func(n *nethtml.Node) bool { return n.Data == "fieldset" })
	if len(groups) == 0 || !slices.Contains(strings.Fields(attrOf(groups[0], "aria-describedby")), errorID) {
		t.Errorf("Alpha's Tags group doesn't point at its error message %q: %s", errorID, renderNode(cell))
	}
	boxes := findAll(cell, func(n *nethtml.Node) bool { return n.Data == "input" && attrOf(n, "type") == "checkbox" })
	if len(boxes) != 2 {
		t.Fatalf("Alpha's Tags has %d checkboxes, want 2: %s", len(boxes), renderNode(cell))
	}
	for _, box := range boxes {
		if attrOf(box, "aria-invalid") != "true" {
			t.Errorf("checkbox isn't marked invalid: %s", renderNode(box))
		}
	}
}

// goalTableFormIn is what a browser would post from the Goal table's edit
// form under page as it stands, and where to.
func goalTableFormIn(t *testing.T, page *nethtml.Node) (string, url.Values) {
	t.Helper()
	form := findByTestID(t, page, "goal-table-form")
	if form.Data != "form" {
		t.Fatalf("the Goal table's edit form isn't a form: %s", renderNode(form))
	}
	return attrOf(form, "action"), formSubmission(form)
}

// goalTableControlName is the name of the one form control under page
// labelled label: an input, select or textarea, or a fieldset of checkboxes,
// whose boxes share the name.
func goalTableControlName(t *testing.T, page *nethtml.Node, label string) string {
	t.Helper()
	controls := findAll(page, func(n *nethtml.Node) bool {
		switch n.Data {
		case "input", "select", "textarea", "fieldset":
			return attrOf(n, "aria-label") == label
		}
		return false
	})
	if len(controls) != 1 {
		t.Fatalf("page has %d controls labelled %q, want 1", len(controls), label)
	}
	if c := controls[0]; c.Data != "fieldset" {
		return attrOf(c, "name")
	}
	boxes := findAll(controls[0], func(n *nethtml.Node) bool { return n.Data == "input" && attrOf(n, "type") == "checkbox" })
	if len(boxes) == 0 {
		t.Fatalf("the %q group holds no checkboxes: %s", label, renderNode(controls[0]))
	}
	return attrOf(boxes[0], "name")
}

// valueID is the form value picking value.
func valueID(v domain.DimensionValue) string {
	return fmt.Sprintf("%d", v.ID)
}

// Saving the Goal table's edit form updates every Goal whose cells changed —
// here three, one of them as a Delegate and one with a value typed into an
// Extendable list — keeps each change in its Goal's history, and lands back on
// the table out of edit mode with its filters (#80).
func TestGoalTableSaveUpdatesEveryChangedGoalWithHistory(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	tags := h.CreateExtendableDimension(boss, "Tags", "infra", "ux")
	h.SetDimensionSelection(boss, tags, domain.SelectionSeveral)
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Notes", domain.FieldLongText, "")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(sam, "Beta", "B matters.")
	gamma := h.CreateGoal(pat, "Gamma", "C matters.")
	h.AddDelegate(pat, sam, gamma.ID)
	untouched := h.CreateGoal(sam, "Untouched", "U matters.")
	h.AssignGoalValue(untouched, pillar.Values[1])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table&lifecycle=Proposed")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Pillar of Alpha"), valueID(pillar.Values[0]))
	form.Add(goalTableControlName(t, page, "Tags of Beta"), valueID(tags.Values[1]))
	form.Set(goalTableControlName(t, page, "Add a Tags value to Beta"), "mobile")
	form.Set(goalTableControlName(t, page, "Budget of Gamma"), "1200")
	form.Set(goalTableControlName(t, page, "Notes of Gamma"), "Line one\nLine two")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/goals" {
		t.Fatalf("save landed on %s with status %d, want the Goal list:\n%s", resp.Request.URL, resp.StatusCode, body)
	}
	if q := resp.Request.URL.Query(); q.Get("layout") != "table" || q.Get("lifecycle") != "Proposed" || q.Has("edit") {
		t.Errorf("save landed on %s, want the table with its filter, out of edit mode", resp.Request.URL)
	}
	for goal, want := range map[string][]string{
		"Alpha":     {"Pillar: set to Growth"},
		"Beta":      {"Tags: added ux", "Tags: added mobile"},
		"Gamma":     {"Budget: set to 1200", "Notes: set to Line one\nLine two"},
		"Untouched": {"Pillar: set to Trust"},
	} {
		id := map[string]int64{"Alpha": alpha.ID, "Beta": beta.ID, "Gamma": gamma.ID, "Untouched": untouched.ID}[goal]
		changes, err := h.Service.ValueHistory(context.Background(), id)
		if err != nil {
			t.Fatalf("ValueHistory: %v", err)
		}
		var got []string
		for _, c := range changes {
			got = append(got, historyText(c))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s's history = %q, want %q", goal, got, want)
		}
	}
	saved := parsePage(t, body)
	if tags := textOf(goalTableCellIn(t, saved, goalTableRowOf(t, saved, beta), "Tags")); tags != "ux, mobile" {
		t.Errorf("Beta's Tags = %q, want ux, mobile", tags)
	}
}

// historyText is a Value history entry as the Goal page words it.
func historyText(c domain.ValueChange) string {
	switch {
	case c.Several && c.Before == "":
		return c.Attribute + ": added " + c.After
	case c.Several:
		return c.Attribute + ": removed " + c.Before
	case c.Before == "":
		return c.Attribute + ": set to " + c.After
	case c.After == "":
		return c.Attribute + ": cleared"
	}
	return c.Attribute + ": " + c.Before + " → " + c.After
}

// One invalid number in the Goal table's edit form saves nothing: the form
// comes back in edit mode with what was typed in every cell, the bad cell
// marked with why, and the Goals keep their values (#80).
func TestGoalTableSaveWithAnInvalidNumberSavesNothingAndKeepsWhatWasTyped(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Approver", domain.FieldShortText, "")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(sam, "Beta", "B matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Pillar of Alpha"), valueID(pillar.Values[1]))
	form.Set(goalTableControlName(t, page, "Approver of Alpha"), "Dana & Co")
	form.Set(goalTableControlName(t, page, "Budget of Beta"), "lots")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.StatusCode, body)
	}
	refused := parsePage(t, body)
	if !strings.Contains(textOf(findByTestID(t, refused, "goal-table-errors")), "Nothing was saved") {
		t.Errorf("the refusal doesn't say nothing was saved")
	}
	if n := len(findAllByTestID(refused, "cell-error")); n != 1 {
		t.Fatalf("%d cells are marked bad, want 1", n)
	}
	betaBudget := goalTableCellIn(t, refused, goalTableRowOf(t, refused, beta), "Budget")
	inputs := goalTableInputsIn(betaBudget)
	if !hasTestID(betaBudget, "cell-error") || !strings.Contains(textOf(betaBudget), "Budget takes a number") ||
		len(inputs) != 1 || attrOf(inputs[0], "value") != "lots" || attrOf(inputs[0], "aria-invalid") != "true" {
		t.Errorf("Beta's Budget isn't marked bad keeping lots: %s", renderNode(betaBudget))
	}
	alphaRow := goalTableRowOf(t, refused, alpha)
	if cell := goalTableCellIn(t, refused, alphaRow, "Approver"); !slices.ContainsFunc(goalTableInputsIn(cell), func(n *nethtml.Node) bool {
		return attrOf(n, "value") == "Dana & Co"
	}) {
		t.Errorf("Alpha's Approver lost what was typed: %s", renderNode(cell))
	}
	if cell := goalTableCellIn(t, refused, alphaRow, "Pillar"); !slices.ContainsFunc(findAll(cell, func(n *nethtml.Node) bool { return n.Data == "option" }), func(o *nethtml.Node) bool {
		return attrOf(o, "value") == valueID(pillar.Values[1]) && hasAttr(o, "selected")
	}) {
		t.Errorf("Alpha's Pillar lost the value picked: %s", renderNode(cell))
	}

	for _, g := range []domain.Goal{alpha, beta} {
		if changes, err := h.Service.ValueHistory(context.Background(), g.ID); err != nil || len(changes) != 0 {
			t.Errorf("%s's history = %+v (%v), want nothing saved", g.Title, changes, err)
		}
	}

	// Fixing the bad cell and saving the form as it came back saves it all.
	action, form = goalTableFormIn(t, refused)
	form.Set(goalTableControlName(t, refused, "Budget of Beta"), "900")
	resp = postForm(t, client, ts.URL+action, form)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Query().Has("edit") {
		t.Fatalf("saving the fixed form: status %d at %s:\n%s", resp.StatusCode, resp.Request.URL, readBody(t, resp))
	}
	for g, want := range map[int64]int{alpha.ID: 2, beta.ID: 1} {
		if changes, _ := h.Service.ValueHistory(context.Background(), g); len(changes) != want {
			t.Errorf("Goal %d's history = %+v, want %d entries", g, changes, want)
		}
	}
}

// A crafted save of the Goal table touching a Goal the person may not set
// values on — its row had no inputs — is refused with 403 on the refusal page,
// naming the Goal, with a way Back to the table out of edit mode, and nothing
// in it is saved, not even the cells of the person's own Goals (#80;
// CONTEXT.md: Delegate).
func TestGoalTableSaveForAGoalThePersonCantEditIsRefused(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	mine := h.CreateGoal(sam, "Mine", "M matters.")
	theirs := h.CreateGoal(pat, "Theirs", "T matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Budget of Mine"), "10")
	form.Set(fmt.Sprintf("d.%d.%d", theirs.ID, pillar.ID), valueID(pillar.Values[0]))
	form.Set(fmt.Sprintf("f.%d.%d", theirs.ID, budget.ID), "99")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't save Goals",
		"Only its Owner, a Delegate or an Admin may set the values of Theirs.", "/goals?layout=table")
	for _, g := range []domain.Goal{mine, theirs} {
		if changes, err := h.Service.ValueHistory(context.Background(), g.ID); err != nil || len(changes) != 0 {
			t.Errorf("%s's history = %+v (%v), want nothing saved", g.Title, changes, err)
		}
	}
}

// A crafted save of the Goal table touching a Goal that doesn't exist gets the
// Not found page, and nothing in it is saved.
func TestGoalTableSaveForAMissingGoalIsNotFound(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	mine := h.CreateGoal(sam, "Mine", "M matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Budget of Mine"), "10")
	form.Set(fmt.Sprintf("f.%d.%d", mine.ID+1000, budget.ID), "99")

	assertNotFoundPageWithForm(t, client, http.MethodPost, ts.URL+action, form, "/home")
	if changes, err := h.Service.ValueHistory(context.Background(), mine.ID); err != nil || len(changes) != 0 {
		t.Errorf("Mine's history = %+v (%v), want nothing saved", changes, err)
	}
}

// A Retired value a Goal carries stays offered, marked retired, in the Goal
// table's edit form, so saving another cell keeps it, and Retired values the
// Goal doesn't carry aren't offered, as on the Goal page (#80; CONTEXT.md:
// Retired).
func TestGoalTableEditKeepsRetiredValuesTheGoalCarries(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	quarter := h.CreateDimension(boss, "Quarter", "Q1", "Q2")
	tags := h.CreateSeveralValuesDimension(boss, "Tags", "infra", "legacy", "ux")
	h.CreateField(boss, "Budget", domain.FieldNumber, "")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(sam, "Beta", "B matters.")
	h.AssignGoalValue(alpha, quarter.Values[0])
	h.AssignGoalValue(alpha, tags.Values[1])
	for _, v := range []domain.DimensionValue{quarter.Values[0], tags.Values[1]} {
		if err := h.Service.RetireDimensionValue(ctx, boss.ID, v.ID); err != nil {
			t.Fatalf("RetireDimensionValue: %v", err)
		}
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	quarterCell := goalTableCellIn(t, page, goalTableRowOf(t, page, alpha), "Quarter")
	if got, want := textsOf(findAll(quarterCell, func(n *nethtml.Node) bool { return n.Data == "option" })), []string{"—", "Q1 (retired)", "Q2"}; !slices.Equal(got, want) {
		t.Errorf("Alpha's Quarter offers %q, want %q, its Retired Q1 marked", got, want)
	}
	if cell := textOf(goalTableCellIn(t, page, goalTableRowOf(t, page, beta), "Tags")); cell != "infra ux" {
		t.Errorf("Beta's Tags offers %q, want no Retired value", cell)
	}
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Budget of Alpha"), "5")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("save status %d:\n%s", resp.StatusCode, body)
	}

	values, err := h.Service.GoalValues(ctx, alpha.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	var names []string
	for _, v := range values {
		names = append(names, v.Value)
	}
	if got := strings.Join(names, ", "); got != "Q1, legacy" {
		t.Errorf("Alpha's values = %q, want its Retired Q1 and legacy kept", got)
	}
}

// Saving the Goal table's edit form applies only the cells changed in it, so a
// value someone else saved meanwhile in a cell left alone is kept, not put back
// to what the form showed (#80: Save submits every changed row).
func TestGoalTableSaveLeavesCellsItDidNotChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "")
	approver := h.CreateField(boss, "Approver", domain.FieldShortText, "")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	h.SetGoalField(sam, alpha, budget, "100")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	h.SetGoalField(boss, alpha, budget, "300")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Approver of Alpha"), "Dana")
	resp := postForm(t, client, ts.URL+action, form)
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("save status %d:\n%s", resp.StatusCode, body)
	}

	values, err := h.Service.GoalFields(context.Background(), alpha.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	got := map[int64]string{}
	for _, v := range values {
		got[v.Field.ID] = v.Value
	}
	if got[budget.ID] != "300" || got[approver.ID] != "Dana" {
		t.Errorf("Alpha's Budget, Approver = %q, %q, want 300 kept and Dana saved", got[budget.ID], got[approver.ID])
	}
}

// A required Dimension or Field joins the activation checklist, and activating
// without its value is refused naming it (CONTEXT.md: Incomplete).
func TestActivationNeedsRequiredValues(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(boss, budget, true)
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), goal.Owner.ID, goal.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	if _, err := h.Service.AddMetric(context.Background(), goal.Owner.ID, domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95", Unit: "ms", Direction: domain.MetricDown, Baseline: 900, Target: 300,
		TargetDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	items := goalActivationChecklistIn(t, getPage(t, client, goalURL))
	for _, label := range []string{"A value in Pillar", "A value in Budget"} {
		if done, ok := items[label]; !ok || done {
			t.Errorf("checklist item %q: listed %v, done %v; want listed and missing (items %v)", label, ok, done, items)
		}
	}
	resp := postForm(t, client, goalURL+"/activate", nil)
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if text := textOf(parsePage(t, body)); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(text, "Pillar") || !strings.Contains(text, "Budget") {
		t.Errorf("activate without the values: status %d, body %q; want 422 naming Pillar and Budget", resp.StatusCode, body)
	}

	h.AssignGoalValue(goal, pillar.Values[0])
	h.SetGoalField(sam, goal, budget, "100")
	items = goalActivationChecklistIn(t, getPage(t, client, goalURL))
	if !items["A value in Pillar"] || !items["A value in Budget"] {
		t.Errorf("checklist after setting the values: %v, want both done", items)
	}
	resp = postForm(t, client, goalURL+"/activate", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("activate with the values: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
}

// An Active Goal lacking a required value shows an Incomplete flag naming what
// it lacks; setting the value clears it, and a Goal that isn't Active never
// shows it (CONTEXT.md: Incomplete). That the flag is quieter than Check-in
// overdue's is TestGoalPageFlagsIncompleteStyles's.
func TestGoalPageFlagsIncomplete(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	onHold := h.OnHoldGoal(sam, "Rebuild search", "Nobody finds anything.", "Waiting on legal.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(boss, budget, true)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	flag := findByTestID(t, getPage(t, client, goalURL), "goal-incomplete")
	for _, want := range []string{"Incomplete", "Pillar", "Budget"} {
		if !strings.Contains(textOf(flag), want) {
			t.Errorf("Incomplete flag lacks %q: %s", want, renderNode(flag))
		}
	}
	if hasTestID(getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, onHold.ID)), "goal-incomplete") {
		t.Errorf("On Hold Goal is flagged Incomplete")
	}

	h.AssignGoalValue(goal, pillar.Values[0])
	h.SetGoalField(sam, goal, budget, "100")
	if hasTestID(getPage(t, client, goalURL), "goal-incomplete") {
		t.Errorf("Goal with every required value is still flagged Incomplete")
	}
}

// The Goal list marks each Incomplete Goal's row without moving it in the
// problems-first order, and its Incomplete filter keeps exactly the Incomplete
// Goals, in either layout (CONTEXT.md: Incomplete).
func TestGoalListMarksAndFiltersIncomplete(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	complete := h.ActiveGoal(sam, "Zulu, complete", "It matters.")
	lacking := h.ActiveGoal(sam, "Alpha, incomplete", "It matters.")
	lackingToo := h.ActiveGoal(sam, "Mike, incomplete", "It matters.")
	onHold := h.OnHoldGoal(sam, "Bravo, On Hold", "It matters.", "Waiting on legal.")
	proposed := h.CreateGoal(sam, "Charlie, Proposed", "It matters.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.AssignGoalValue(complete, pillar.Values[0])
	h.SetDimensionRequired(boss, pillar, true)
	goals := []domain.Goal{complete, lacking, lackingToo, onHold, proposed}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/goals")
	rows := goalListRowsIn(page)
	if got, want := goalRowTitlesIn(rows, goals...), []string{"Alpha, incomplete", "Bravo, On Hold", "Charlie, Proposed", "Mike, incomplete", "Zulu, complete"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q: Incomplete changes no sort order", got, want)
	}
	for _, row := range rows {
		marked := hasTestID(row, "incomplete")
		want := linkTo(row, goalHref(lacking.ID)) != nil || linkTo(row, goalHref(lackingToo.ID)) != nil
		if marked != want {
			t.Errorf("row marked Incomplete %v, want %v: %s", marked, want, renderNode(row))
		}
	}
	if box := findByTestID(t, page, "goal-incomplete-filter"); hasAttr(box, "checked") {
		t.Errorf("Incomplete filter is checked without ?incomplete=1: %s", renderNode(box))
	}

	page = getPage(t, client, ts.URL+"/goals?incomplete=1")
	if got, want := goalRowTitlesIn(goalListRowsIn(page), goals...), []string{"Alpha, incomplete", "Mike, incomplete"}; !slices.Equal(got, want) {
		t.Errorf("incomplete=1: rows = %q, want %q", got, want)
	}
	if box := findByTestID(t, page, "goal-incomplete-filter"); !hasAttr(box, "checked") {
		t.Errorf("Incomplete filter lost its check: %s", renderNode(box))
	}
	table := getPage(t, client, ts.URL+"/goals?layout=table&incomplete=1")
	for _, g := range goals {
		listed := linkTo(table, goalHref(g.ID)) != nil
		if want := g.ID == lacking.ID || g.ID == lackingToo.ID; listed != want {
			t.Errorf("table layout, incomplete=1: %s listed %v, want %v", g.Title, listed, want)
		}
	}
}

// A download that would include a value containing a semicolon, named so
// before that was refused, is refused on the refusal page naming the
// Dimension and value so an Admin can rename it, with a way Back to the list
// as it was, and returns no file (#101).
func TestGoalTableDownloadRefusesAValueContainingASemicolon(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	rnd := h.NameValueWithSemicolon(pillar.Values[1], "R&D; Ops")
	h.AssignGoalValue(h.CreateGoal(boss, "Alpha launch", "A matters."), rnd)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp, err := ctxGet(t, client, ts.URL+"/goals/download?layout=table")
	if err != nil {
		t.Fatalf("GET download: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("download status = %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't download Goals",
		`Alpha launch carries the Pillar value "R&D; Ops", and a value can't contain a semicolon, because the import format uses it to separate values; an Admin can rename it on the Dimensions page.`,
		"/goals?layout=table")
	if cd := resp.Header.Get("Content-Disposition"); cd != "" || strings.Contains(body, "ID,Title") {
		t.Errorf("refused download returned a file: Content-Disposition %q, body %q", cd, body)
	}
}

// Naming a new value containing a semicolon is refused, saying why, from the
// Goal page and from the table's edit mode, and nothing is added (#101).
func TestNewValueContainingASemicolonIsRefusedFromTheGoalPageAndTable(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	customer := h.CreateExtendableDimension(boss, "Customer", "Acme")
	goal := h.CreateGoal(sam, "Alpha", "A matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	const reason = "can't contain a semicolon, because the import format uses it to separate values"

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/dimensions", ts.URL, goal.ID), url.Values{
		"dimension_id": {fmt.Sprintf("%d", customer.ID)},
		"new_value":    {"Globex; Initech"},
	})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(textOf(goalOpenFormIn(t, parsePage(t, body), "dimensions")), reason) {
		t.Errorf("Goal page: status %d, body %q; want 422 saying why beside the open form", resp.StatusCode, body)
	}

	page := goalTableEditMode(t, client, ts.URL, "/goals?layout=table")
	action, form := goalTableFormIn(t, page)
	form.Set(goalTableControlName(t, page, "Add a Customer value to Alpha"), "Hooli;")
	resp = postForm(t, client, ts.URL+action, form)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("table edit: status = %d, want 422:\n%s", resp.StatusCode, body)
	}
	refused := parsePage(t, body)
	if cell := goalTableCellIn(t, refused, goalTableRowOf(t, refused, goal), "Customer"); !hasTestID(cell, "cell-error") || !strings.Contains(textOf(cell), reason) {
		t.Errorf("table edit: Alpha's Customer isn't marked bad saying why: %s", renderNode(cell))
	}

	if got := dimensionValueNames(dimensionByName(t, h, "Customer")); !slices.Equal(got, []string{"Acme"}) {
		t.Errorf("Customer list = %v, want [Acme] with nothing added", got)
	}
}

// goalDimensionValuesShownIn is what each value the Goal page's Dimensions
// section shows reads, in order.
func goalDimensionValuesShownIn(t *testing.T, page *nethtml.Node) []string {
	t.Helper()
	return textsOf(findAllByTestID(findByTestID(t, page, "goal-dimensions"), "goal-dimension-value"))
}

// When the Goal's history can't be written, choosing another value on the Goal
// page fails and the Goal keeps the value it had.
func TestFailedHistoryLeavesTheDimensionValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, pillar.Values[0])
	if _, err := h.DB.ExecContext(t.Context(), `CREATE TRIGGER fail_history BEFORE INSERT ON goal_value_changes
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	resp := postForm(t, client, goalURL+"/dimensions", url.Values{
		"dimension_id": {fmt.Sprint(pillar.ID)},
		"value_id":     {fmt.Sprint(pillar.Values[1].ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("choosing Reliability: status %d, want 500; body:\n%s", resp.StatusCode, body)
	}
	if shown := goalDimensionValuesShownIn(t, getPage(t, client, goalURL)); !slices.Contains(shown, "Growth") || slices.Contains(shown, "Reliability") {
		t.Errorf("the Goal doesn't keep Growth after a failed change: shows %q", shown)
	}
}

// goalDimensionSelectIn returns the one-value select labelled dimension in
// the Goal page's open Edit Dimension values form.
func goalDimensionSelectIn(t *testing.T, page *nethtml.Node, dimension string) *nethtml.Node {
	t.Helper()
	selects := findAll(goalOpenFormIn(t, page, "dimensions"), func(n *nethtml.Node) bool {
		return n.Data == "select" && attrOf(n, "aria-label") == dimension
	})
	if len(selects) != 1 {
		t.Fatalf("the open Dimensions form has %d selects labelled %s, want 1", len(selects), dimension)
	}
	return selects[0]
}

// A one-value Dimension's select on the Goal page offers "None", and choosing
// it removes the Goal's value there, recording one "cleared" entry; a required
// Dimension cleared leaves the Goal Incomplete. Choosing "None" again, with no
// value left, records nothing.
func TestChoosingNoneClearsAOneValueDimensionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.SetDimensionRequired(boss, pillar, true)
	h.AssignGoalValue(goal, pillar.Values[0])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)

	sel := goalDimensionSelectIn(t, getPage(t, client, goalURL+"?open=dimensions"), "Pillar")
	var form *nethtml.Node
	for p := sel.Parent; p != nil && form == nil; p = p.Parent {
		if p.Data == "form" {
			form = p
		}
	}
	if form == nil || formField(t, form, "dimension_id").Value != fmt.Sprint(pillar.ID) {
		t.Errorf("Pillar's select form doesn't name its Dimension: %s", renderNode(sel.Parent))
	}
	if !slices.ContainsFunc(findAll(sel, func(n *nethtml.Node) bool { return n.Data == "option" }), func(o *nethtml.Node) bool {
		return hasAttr(o, "value") && attrOf(o, "value") == "" && textOf(o) == "None" && !hasAttr(o, "selected")
	}) {
		t.Errorf("Pillar's select doesn't offer None: %s", renderNode(sel))
	}

	none := url.Values{"dimension_id": {fmt.Sprint(pillar.ID)}, "value_id": {""}}
	for range 2 {
		resp := postForm(t, client, goalURL+"/dimensions", none)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("choosing None: status %d; body:\n%s", resp.StatusCode, body)
		}
	}

	page := getPage(t, client, goalURL)
	if section := findByTestID(t, page, "goal-dimensions"); !hasTestID(section, "goal-dimension-unassigned") {
		t.Errorf("Pillar still has a value after choosing None: %s", renderNode(section))
	}
	entries := textsOf(goalValueHistoryIn(t, client, goalURL))
	if len(entries) != 2 || !strings.Contains(entries[0], "Pillar: cleared (was Growth)") {
		t.Errorf("History lists %d value changes, want one cleared entry after the set:\n%s", len(entries), strings.Join(entries, "\n"))
	}
	if flag := findByTestID(t, page, "goal-incomplete"); !strings.Contains(textOf(flag), "Pillar") {
		t.Errorf("Incomplete flag doesn't name Pillar: %s", renderNode(flag))
	}
}

// A retired value the Goal carries stays selected in its one-value select, so
// saving the select keeps it rather than clearing it to "None".
func TestOneValueSelectKeepsACarriedRetiredValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.AssignGoalValue(goal, pillar.Values[0])
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, pillar.Values[0].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	sel := goalDimensionSelectIn(t, getPage(t, client, goalPageURL(ts.URL, goal)+"?open=dimensions"), "Pillar")
	if !slices.ContainsFunc(findAll(sel, func(n *nethtml.Node) bool { return n.Data == "option" }), func(o *nethtml.Node) bool {
		return attrOf(o, "value") == valueID(pillar.Values[0]) && hasAttr(o, "selected") && textOf(o) == "Growth (retired)"
	}) {
		t.Errorf("Pillar's select doesn't keep the retired Growth selected: %s", renderNode(sel))
	}
}

// The Goal list's filter bar applies itself (#94): under htmx a filter change,
// a pause in typing the search or Enter GETs the filtered view, swaps in only
// the list (so the filter bar keeps its focus and an open More filters) and
// pushes the filtered address, so it can be copied, shared and reached with
// Back. The page opts out of htmx's history snapshot, so Back loads the earlier
// address afresh and the filter bar shows its filters too.
func TestGoalListFiltersApplyOnChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	for _, layout := range []string{"/goals", "/goals?layout=table"} {
		root := getPage(t, client, ts.URL+layout)
		form := findByTestID(t, root, "goal-filters")
		for name, want := range map[string]string{
			"hx-get":        "/goals",
			"hx-trigger":    "submit, change[target.type!='search'], input[target.type=='search'] delay:400ms",
			"hx-target":     "#goal-list",
			"hx-select":     "#goal-list",
			"hx-select-oob": "#goal-count,#goal-layout",
			"hx-swap":       "outerHTML",
			"hx-push-url":   "true",
			"hx-sync":       "this:replace",
		} {
			if got := attrOf(form, name); got != want {
				t.Errorf("%s: filter form %s = %q, want %q", layout, name, got, want)
			}
		}
		if optedOut := findAll(root, func(n *nethtml.Node) bool { return attrOf(n, "hx-history") == "false" }); len(optedOut) == 0 {
			t.Errorf("%s: the page keeps htmx's history snapshot, so Back would show stale filters", layout)
		}
	}
}

// goalPageWithoutScript parses page as a browser running no scripts reads it:
// what a <noscript> holds is part of the page, not text.
func goalPageWithoutScript(t *testing.T, page string) *nethtml.Node {
	t.Helper()
	doc, err := nethtml.ParseWithOptions(strings.NewReader(page), nethtml.ParseOptionEnableScripting(false))
	if err != nil {
		t.Fatalf("parse page without script: %v", err)
	}
	return doc
}

// goalApplyButtonsIn returns the submit buttons under root reading Apply.
func goalApplyButtonsIn(root *nethtml.Node) []*nethtml.Node {
	return findAll(root, func(n *nethtml.Node) bool {
		return n.Data == "button" && attrOf(n, "type") == "submit" && textOf(n) == "Apply"
	})
}

// insideNoscript reports whether n sits in a <noscript>.
func insideNoscript(n *nethtml.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == nethtml.ElementNode && p.Data == "noscript" {
			return true
		}
	}
	return false
}

// Without JavaScript the filter bar still works as a plain GET form, so its
// Apply button stays, but only there: it sits in <noscript>, so a browser
// running scripts, where the bar applies itself, doesn't show it (#94). The
// table's Columns form isn't a filter and keeps its own button.
func TestGoalListApplyButtonOnlyWithoutJavaScript(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	for _, layout := range []string{"/goals", "/goals?layout=table"} {
		page := getBody(t, client, ts.URL+layout)
		form := findByTestID(t, parsePage(t, page), "goal-filters")
		if form.Data != "form" || attrOf(form, "method") != "get" || attrOf(form, "action") != "/goals" {
			t.Errorf("%s: without JavaScript the filter bar no longer GETs /goals: %s", layout, renderNode(form))
		}
		if buttons := goalApplyButtonsIn(form); len(buttons) != 0 {
			t.Errorf("%s: a browser running scripts is shown the filter bar's Apply: %s", layout, renderNode(form))
		}
		noScript := findByTestID(t, goalPageWithoutScript(t, page), "goal-filters")
		if buttons := goalApplyButtonsIn(noScript); len(buttons) != 1 || !insideNoscript(buttons[0]) {
			t.Errorf("%s: the filter bar's <noscript> lacks Apply: %s", layout, renderNode(noScript))
		}
		if n := strings.Count(textOf(noScript), "Apply"); n != 1 {
			t.Errorf("%s: the filter bar shows Apply outside <noscript>: %s", layout, renderNode(noScript))
		}
	}
	columns := findByTestID(t, getPage(t, client, ts.URL+"/goals?layout=table"), "goal-columns")
	noscripts := findAll(columns, func(n *nethtml.Node) bool { return n.Data == "noscript" })
	if len(goalApplyButtonsIn(columns)) != 1 || len(noscripts) != 0 {
		t.Errorf("the Columns form lost its own Apply: %s", renderNode(columns))
	}
}

// htmxFilter changes the filter bar on page as a person would — set changes
// one field — and GETs the view the way htmx does, answering the page htmx
// selects from and the address it pushes. It reads what the filter form
// submits, and where to, from the parsed page.
func htmxFilter(t *testing.T, client *http.Client, base, page string, set func(url.Values)) (string, string) {
	t.Helper()
	form := findByTestID(t, parsePage(t, page), "goal-filters")
	values := formSubmission(form)
	set(values)
	address := attrOf(form, "hx-get") + "?" + values.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+address, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Target", "goal-list")
	req.Header.Set("HX-Current-URL", base+"/goals")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d:\n%s", address, resp.StatusCode, body)
	}
	return body, address
}

// goalSwappedListIn is the element htmx swaps in for the list, #goal-list, in
// page.
func goalSwappedListIn(t *testing.T, page string) *nethtml.Node {
	t.Helper()
	root := parsePage(t, page)
	lists := findAll(root, func(n *nethtml.Node) bool { return attrOf(n, "id") == "goal-list" })
	if len(lists) != 1 {
		t.Fatalf("page has %d #goal-list, want 1:\n%s", len(lists), page)
	}
	return lists[0]
}

// goalTableSortsBy reports whether the Goal table under root is sorted by the
// column headed head in dir, as its header tells assistive technology, and
// the header still links to its sort.
func goalTableSortsBy(root *nethtml.Node, head, dir string) bool {
	return len(findAll(root, func(n *nethtml.Node) bool {
		return n.Data == "th" && attrOf(n, "aria-sort") == dir && textOf(n) == head &&
			len(findAll(n, func(a *nethtml.Node) bool { return a.Data == "a" && hasAttr(a, "href") })) == 1
	})) == 1
}

// A filter changed under htmx answers the same Goals as its address loaded
// directly, and keeps the layout, the sort and the grouping (#94).
func TestGoalListFilterChangeUnderHTMXKeepsTheView(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	alpha := h.CreateGoal(sam, "Alpha launch", "A matters.")
	bravo := h.CreateGoal(boss, "Bravo launch", "B matters.")
	charlie := h.CreateGoal(sam, "Charlie launch", "C matters.")
	audit := h.CreateGoal(sam, "Delta audit", "D matters.")
	h.AssignGoalValue(alpha, pillar.Values[0])
	h.AssignGoalValue(bravo, pillar.Values[0])
	h.AssignGoalValue(charlie, pillar.Values[1])
	h.AssignGoalValue(audit, pillar.Values[1])
	goals := []domain.Goal{alpha, bravo, charlie, audit}
	group := fmt.Sprint(pillar.ID)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	// The table, sorted by Title descending and carrying a grouping, searched.
	table := getBody(t, client, ts.URL+"/goals?org=all&layout=table&sort=title&dir=desc&group="+group)
	body, address := htmxFilter(t, client, ts.URL, table, func(v url.Values) { v.Set("q", "launch") })
	swapped := goalSwappedListIn(t, body)
	if got, want := goalRowTitlesIn(goalTableRowsIn(t, swapped), goals...), []string{"Charlie launch", "Bravo launch", "Alpha launch"}; !slices.Equal(got, want) {
		t.Errorf("table searched under htmx lists %q, want %q", got, want)
	}
	direct := goalSwappedListIn(t, getBody(t, client, ts.URL+address))
	if got, want := goalRowTitlesIn(goalTableRowsIn(t, swapped), goals...), goalRowTitlesIn(goalTableRowsIn(t, direct), goals...); !slices.Equal(got, want) {
		t.Errorf("table under htmx lists %q, but %s loaded directly lists %q", got, address, want)
	}
	if !goalTableSortsBy(swapped, "Title", "descending") {
		t.Errorf("the table under htmx lost its sort: %s", renderNode(swapped))
	}
	// The List / Table switch sits in the header strip, which the filter form
	// selects out of band.
	if toList := goalLayoutLinkQueryIn(t, parsePage(t, body), "layout-list"); toList.Get("group") != group || toList.Get("q") != "launch" {
		t.Errorf("the table under htmx lost its grouping or search: List links to %v", toList)
	}

	// The list, grouped by Pillar, narrowed to Mine only.
	list := getBody(t, client, ts.URL+"/goals?org=all&group="+group)
	body, address = htmxFilter(t, client, ts.URL, list, func(v url.Values) { v.Set("org", "mine") })
	swapped = goalSwappedListIn(t, body)
	heads := textsOf(findAll(swapped, func(n *nethtml.Node) bool { return n.Data == "th" }))
	if want := []string{"Health", "Goal", "Owner", "Due", "Last check-in", "Growth 1", "Trust 2"}; !slices.Equal(heads, want) {
		t.Errorf("the list under htmx heads %q, want %q: grouping lost", heads, want)
	}
	if got, want := goalRowTitlesIn(goalListRowsIn(swapped), goals...), []string{"Alpha launch", "Charlie launch", "Delta audit"}; !slices.Equal(got, want) {
		t.Errorf("Mine only under htmx lists %q, want %q", got, want)
	}
	direct = goalSwappedListIn(t, getBody(t, client, ts.URL+address))
	if got, want := goalRowTitlesIn(goalListRowsIn(swapped), goals...), goalRowTitlesIn(goalListRowsIn(direct), goals...); !slices.Equal(got, want) {
		t.Errorf("the list under htmx lists %q, but %s loaded directly lists %q", got, address, want)
	}
}

// The Goal page lists each Highlight on its own, so two flagged in one
// Check-in are two entries, in the order entered, each crediting the Owner
// (CONTEXT.md: Highlight).
func TestGoalPageListsEachHighlightOfACheckinSeparately(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CheckinWithHighlights(sam, goal.ID,
		domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."},
		domain.HighlightInput{Kind: domain.HighlightMiss, Note: "Missed the SLA."},
	)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	block := findByTestID(t, root, "goal-highlights")
	entries := findAllByTestID(block, "highlight")
	if len(entries) != 2 {
		t.Fatalf("Highlights block lists %d entries, want 2; block:\n%s", len(entries), renderNode(block))
	}
	for i, want := range []string{"Retries masked the root cause.", "Missed the SLA."} {
		if !strings.Contains(textOf(entries[i]), want) {
			t.Errorf("entry %d reads %q, want %q", i+1, textOf(entries[i]), want)
		}
		credited := slices.ContainsFunc(goalPeopleShownIn(entries[i]), func(p goalShownPerson) bool { return p.Title == "sam@example.com" })
		if !credited {
			t.Errorf("entry %d does not credit the Owner: %s", i+1, renderNode(entries[i]))
		}
	}
}

// A Goal with no Highlights has no Highlights section on its page, for its
// Owner or anyone else; once it has one the section lists it.
func TestGoalPageLeavesOutAnEmptyHighlightsSection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	quiet := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	flagged := h.ActiveGoal(sam, "Migrate billing", "Billing is brittle.")
	h.CheckinWithHighlights(sam, flagged.ID, domain.HighlightInput{Kind: domain.HighlightInsight, Note: "Retries masked the root cause."})

	for _, who := range []string{"sam@example.com", "pat@example.com"} {
		client := signInClient(t, ts.URL, who)
		root := getPage(t, client, goalPageURL(ts.URL, quiet))
		for _, gone := range []string{"goal-highlights", "no-highlights"} {
			if hasTestID(root, gone) {
				t.Errorf("%s: a Goal with no Highlights shows %s", who, gone)
			}
		}
		if heading := headingNamed(root, "Highlights"); heading != nil {
			t.Errorf("%s: a Goal with no Highlights shows a Highlights heading: %s", who, renderNode(heading))
		}
		block := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, flagged)), "goal-highlights")
		if got := textsOf(findAllByTestID(block, "highlight")); len(got) != 1 || !strings.Contains(got[0], "Retries masked the root cause.") {
			t.Errorf("%s: the Highlights section lists %q, want the one flagged", who, got)
		}
	}
}

// Changing a one-value Dimension from one value to another on the Goal page
// records one Value history entry, not a removal and an addition, and clearing
// it with None records one more (ticket #74).
func TestChangingThenClearingAOneValueDimensionRecordsOneEntryEachOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	growth, trust := pillar.Values[0], pillar.Values[1]
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)
	choose := func(valueID string) {
		t.Helper()
		resp := postForm(t, client, goalURL+"/dimensions", url.Values{"dimension_id": {fmt.Sprint(pillar.ID)}, "value_id": {valueID}})
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("choose %q: status %d; body:\n%s", valueID, resp.StatusCode, body)
		}
	}
	history := func() []string {
		t.Helper()
		return textsOf(goalValueHistoryIn(t, client, goalURL))
	}

	choose(fmt.Sprint(growth.ID))
	choose(fmt.Sprint(trust.ID))
	entries := history()
	if len(entries) != 2 || !strings.Contains(entries[0], "Pillar: Growth → Trust") {
		t.Fatalf("after setting Growth then Trust, Value history = %d entries, want set then one change Growth → Trust:\n%s", len(entries), strings.Join(entries, "\n"))
	}

	choose("")
	entries = history()
	if len(entries) != 3 || !strings.Contains(entries[0], "Pillar: cleared (was Trust)") {
		t.Errorf("after clearing, Value history = %d entries, want one more, cleared (was Trust):\n%s", len(entries), strings.Join(entries, "\n"))
	}
}

// Once an Admin unmarks the required Dimension and Field a Goal lacks, the Goal
// page drops its Incomplete flag and the Goal list its row's mark (ticket #75).
func TestUnmarkingRequiredClearsTheIncompleteFlagOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(boss, budget, true)
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := goalPageURL(ts.URL, goal)
	listRow := func() *nethtml.Node {
		t.Helper()
		return goalListRowOf(t, getPage(t, client, ts.URL+"/goals"), goal)
	}

	if root := getPage(t, client, goalURL); !hasTestID(root, "goal-incomplete") {
		t.Fatalf("the Goal lacking Pillar and Budget isn't flagged Incomplete")
	}
	if row := listRow(); !hasTestID(row, "incomplete") {
		t.Fatalf("the Goal list doesn't mark the Goal Incomplete: %s", renderNode(row))
	}

	for _, path := range []string{fmt.Sprintf("/dimensions/%d/required", pillar.ID), fmt.Sprintf("/fields/%d/required", budget.ID)} {
		if status := postFormStatus(t, admin, ts.URL+path, url.Values{"required": {"0"}}); status != http.StatusOK {
			t.Fatalf("unmark %s: status %d", path, status)
		}
	}

	if root := getPage(t, client, goalURL); hasTestID(root, "goal-incomplete") {
		t.Errorf("the Goal page still flags the Goal Incomplete: %s", textsOf(findAllByTestID(root, "goal-incomplete")))
	}
	if row := listRow(); hasTestID(row, "incomplete") {
		t.Errorf("the Goal list still marks the Goal Incomplete: %s", renderNode(row))
	}
}

// closeGoal ends an Active Goal in a Check-in by its Owner, moving it to Done
// or Cancelled, failing the test on error.
func closeGoal(t *testing.T, h *testsupport.Harness, owner domain.Account, g domain.Goal, lifecycle string) domain.Goal {
	t.Helper()
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID:          g.ID,
		AuthorID:        owner.ID,
		Status:          "Closing this out.",
		Lifecycle:       lifecycle,
		LifecycleReason: "No longer needed.",
		Outcome:         "Shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin %s: %v", lifecycle, err)
	}
	g.Lifecycle = lifecycle
	return g
}

// A Done or Cancelled Goal that lacks a required value isn't flagged Incomplete
// on its page or its Goal list row, and the Incomplete filter leaves it out,
// keeping only the Active Goal that lacks one (ticket #75).
func TestDoneAndCancelledGoalsAreNeverIncompleteOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	active := h.ActiveGoal(sam, "Alpha, Active", "It matters.")
	done := closeGoal(t, h, sam, h.ActiveGoal(sam, "Bravo, Done", "It matters."), domain.LifecycleDone)
	cancelled := closeGoal(t, h, sam, h.ActiveGoal(sam, "Charlie, Cancelled", "It matters."), domain.LifecycleCancelled)
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.SetDimensionRequired(boss, pillar, true)
	goals := []domain.Goal{active, done, cancelled}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	if root := getPage(t, client, goalPageURL(ts.URL, active)); !hasTestID(root, "goal-incomplete") {
		t.Fatalf("the Active Goal lacking Pillar isn't flagged Incomplete")
	}
	for _, g := range []domain.Goal{done, cancelled} {
		if root := getPage(t, client, goalPageURL(ts.URL, g)); hasTestID(root, "goal-incomplete") {
			t.Errorf("the %s Goal is flagged Incomplete: %s", g.Lifecycle, textsOf(findAllByTestID(root, "goal-incomplete")))
		}
	}

	rows := goalListRowsIn(getPage(t, client, ts.URL+"/goals"))
	if got, want := goalRowTitlesIn(rows, goals...), []string{"Alpha, Active", "Bravo, Done", "Charlie, Cancelled"}; !slices.Equal(got, want) {
		t.Fatalf("Goal list rows = %q, want %q", got, want)
	}
	for _, row := range rows {
		marked := hasTestID(row, "incomplete")
		if want := linkTo(row, goalHref(active.ID)) != nil; marked != want {
			t.Errorf("row marked Incomplete %v, want %v: %s", marked, want, renderNode(row))
		}
	}

	filtered := getPage(t, client, ts.URL+"/goals?incomplete=1")
	if got, want := goalRowTitlesIn(goalListRowsIn(filtered), goals...), []string{"Alpha, Active"}; !slices.Equal(got, want) {
		t.Errorf("incomplete=1: rows = %q, want %q", got, want)
	}
}

// goalTableHeadSortHref is where the Goal table's column header reading head
// links to sort by it.
func goalTableHeadSortHref(t *testing.T, root *nethtml.Node, head string) string {
	t.Helper()
	for _, th := range findAll(findByTestID(t, root, "goal-table-head"), func(n *nethtml.Node) bool { return n.Data == "th" }) {
		if textOf(th) != head {
			continue
		}
		links := findAll(th, func(n *nethtml.Node) bool { return n.Data == "a" && attrOf(n, "href") != "" })
		if len(links) == 0 {
			t.Fatalf("the %s header doesn't sort: %s", head, renderNode(th))
		}
		return attrOf(links[0], "href")
	}
	t.Fatalf("the Goal table has no %s header", head)
	return ""
}

// A date Field column sorts the Goal table by date, earliest first and then
// latest first, with the Goals that have no date last both ways. The dates are
// chosen so that neither their month names, the titles nor the order the Goals
// were made in gives the same order (ticket #79).
func TestGoalTableSortsADateFieldAsDatesWithUnsetLast(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kickoff := h.CreateField(boss, "Kickoff", domain.FieldDate, "")
	alpha := h.CreateGoal(sam, "Alpha", "It matters.")
	bravo := h.CreateGoal(sam, "Bravo", "It matters.")
	charlie := h.CreateGoal(sam, "Charlie", "It matters.")
	unset := h.CreateGoal(sam, "Delta", "It matters.")
	h.SetGoalField(sam, alpha, kickoff, "2026-10-05")
	h.SetGoalField(sam, bravo, kickoff, "2027-01-20")
	h.SetGoalField(sam, charlie, kickoff, "2026-02-01")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	order := func(root *nethtml.Node) []string {
		return goalRowTitlesIn(goalTableRowsIn(t, root), alpha, bravo, charlie, unset)
	}

	root := getPage(t, client, ts.URL+"/goals?layout=table")
	root = getPage(t, client, ts.URL+goalTableHeadSortHref(t, root, "Kickoff"))
	if got, want := order(root), []string{"Charlie", "Alpha", "Bravo", "Delta"}; !slices.Equal(got, want) {
		t.Errorf("sorted by Kickoff = %q, want %q", got, want)
	}

	root = getPage(t, client, ts.URL+goalTableHeadSortHref(t, root, "Kickoff"))
	if got, want := order(root), []string{"Bravo", "Alpha", "Charlie", "Delta"}; !slices.Equal(got, want) {
		t.Errorf("reverse-sorted by Kickoff = %q, want %q", got, want)
	}
}

// goalTableEditModeIn follows the Goal table's Edit values link on the page at
// rawURL and parses the edit mode it opens.
func goalTableEditModeIn(t *testing.T, client *http.Client, baseURL, rawURL string) *nethtml.Node {
	t.Helper()
	edit := findByTestID(t, getPage(t, client, baseURL+rawURL), "goal-table-edit")
	return getPage(t, client, baseURL+attrOf(edit, "href"))
}

// goalCellTakesInput reports whether cell holds a control a person types or
// picks in: an input other than a hidden one, a select or a textarea.
func goalCellTakesInput(cell *nethtml.Node) bool {
	return len(findAll(cell, func(n *nethtml.Node) bool {
		return n.Data == "input" && attrOf(n, "type") != "hidden" || n.Data == "select" || n.Data == "textarea"
	})) > 0
}

// An Admin who neither Owns nor Delegates on any Goal gets an input in every
// Dimension and Field cell of every row in the Goal table's edit mode, and the
// Title, Health, Lifecycle and delivery date cells link to the Goal, since
// changing them happens there (ticket #80).
func TestGoalTableEditModeGivesAnAdminInputsOnEveryRowAndLinksToTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.CreateSeveralValuesDimension(boss, "Tags", "infra", "ux")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Notes", domain.FieldLongText, "")
	alpha := h.ActiveGoal(sam, "Alpha", "A matters.")
	h.Checkin(sam, alpha.ID, domain.HealthGreen, "On track.", "", time.Time{})
	beta := h.ActiveGoal(pat, "Beta", "B matters.")
	h.Checkin(pat, beta.ID, domain.HealthYellow, "Slipping.", "Add staff.", testsupport.Epoch.AddDate(0, 1, 0))
	gamma := h.CreateGoal(pat, "Gamma", "C matters.")
	ts := newServer(t, h)

	root := goalTableEditModeIn(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL, "/goals?layout=table&org=all")

	for _, g := range []domain.Goal{alpha, beta, gamma} {
		row := goalTableRowOf(t, root, g)
		for _, head := range []string{"Pillar", "Tags", "Budget", "Notes"} {
			if cell := goalTableCellIn(t, root, row, head); !goalCellTakesInput(cell) {
				t.Errorf("the Admin gets no input in %s's %s cell: %s", g.Title, head, renderNode(cell))
			}
		}
	}
	for _, g := range []domain.Goal{alpha, beta} {
		row := goalTableRowOf(t, root, g)
		for _, head := range []string{"Title", "Health", "Lifecycle", "Delivery date"} {
			if cell := goalTableCellIn(t, root, row, head); linkTo(cell, goalHref(g.ID)) == nil {
				t.Errorf("%s's %s cell doesn't link to the Goal: %s", g.Title, head, renderNode(cell))
			}
		}
	}
}

// An Owner's header offers Check in, a link to the Check-in page, and No
// change, and beside them a "More ▾" menu, named and titled "More actions". A
// signed-in person with no role on the Goal gets neither button, only the
// menu. How Check in and No change look is TestGoalPageHeaderActionsOverHTTPStyles'.
func TestGoalPageHeaderActionsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)

	root := getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	actions := findByTestID(t, root, "goal-actions")
	if link := findByTestID(t, actions, "checkin-link"); link.Data != "a" || attrOf(link, "href") != fmt.Sprintf("/goals/%d/checkin", goal.ID) {
		t.Errorf("Check in is not a link to the Check-in page: %s", renderNode(link))
	}
	if buttons := findAll(findByTestID(t, actions, "no-change-checkin"), func(n *nethtml.Node) bool { return n.Data == "button" }); len(buttons) == 0 {
		t.Errorf("No change is not a button: %s", renderNode(actions))
	}
	summaries := findAll(goalActionMenuIn(t, root), func(n *nethtml.Node) bool { return n.Data == "summary" })
	if len(summaries) != 1 {
		t.Fatalf("the action menu has %d buttons, want 1", len(summaries))
	}
	summary := summaries[0]
	if text := textOf(summary); text != "More ▾" {
		t.Errorf("the menu button reads %q, want %q: %s", text, "More ▾", renderNode(summary))
	}
	if attrOf(summary, "aria-label") != "More actions" || attrOf(summary, "title") != "More actions" {
		t.Errorf("the menu button is not named and titled %q: %s", "More actions", renderNode(summary))
	}

	root = getPage(t, signInClient(t, ts.URL, "mel@example.com"), goalPageURL(ts.URL, goal))
	actions = findByTestID(t, root, "goal-actions")
	for _, button := range []string{"checkin-link", "no-change-checkin"} {
		if hasTestID(actions, button) {
			t.Errorf("someone with no role on the Goal is offered %s: %s", button, renderNode(actions))
		}
	}
	if !hasTestID(actions, "goal-more") {
		t.Errorf("someone with no role on the Goal has no action menu: %s", renderNode(actions))
	}
}

// The header offers No change only where it would be recorded: on a Goal
// whose Milestone has gone overdue it offers only Check in, while on a Goal a
// No change would repeat it offers both (#235).
func TestGoalPageHeaderOffersNoChangeOnlyWhereItWouldBeRecorded(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	overdue := overdueGreenGoal(h, sam)
	repeatable := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, repeatable.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	actions := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, overdue)), "goal-actions")
	if !hasTestID(actions, "checkin-link") {
		t.Errorf("the overdue Goal's header has no Check in: %s", renderNode(actions))
	}
	if hasTestID(actions, "no-change-checkin") {
		t.Errorf("the overdue Goal's header offers a No change that would be refused: %s", renderNode(actions))
	}
	actions = findByTestID(t, getPage(t, client, goalPageURL(ts.URL, repeatable)), "goal-actions")
	for _, want := range []string{"checkin-link", "no-change-checkin"} {
		if !hasTestID(actions, want) {
			t.Errorf("the repeatable Goal's header lacks %s: %s", want, renderNode(actions))
		}
	}
}

// The action menu lists the actions a person may take, each a plain link to the
// Goal page with that action's form open, but Add a child Goal, which links to
// the New goal form with this Goal picked as its parent: an Owner gets Edit
// title and Why it matters, Hand off, Add a delegate, Link to a parent Goal, Add a child Goal and, with
// Dimensions and Fields defined, Edit Dimension values and Edit Fields; an
// Admin gets Edit title and Why it matters too, and adds Mark Top-level and Mark owner departed…, and on an Ownerless Goal
// Reassign and Mark returned… in place of Hand off; anyone else only Add a
// child Goal. Anyone but the Owner gets Suggest a parent, linking to its own
// form. The Owner also gets the Lifecycle changes, which lead to the Check-in
// page (TestGoalPageActionMenuOffersLifecycleChangesOverHTTP).
func TestGoalPageActionMenuListsLinksOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.SignIn("mel@example.com")
	h.CreateDimension(ada, "Pillar", "Growth")
	h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	open := func(g domain.Goal, form string) string { return fmt.Sprintf("/goals/%d?open=%s", g.ID, form) }
	child := func(g domain.Goal) string { return fmt.Sprintf("/goals/new?parent=%d", g.ID) }
	suggest := func(g domain.Goal) string { return fmt.Sprintf("/goals/%d/suggest-parent", g.ID) }

	for _, tc := range []struct {
		who  string
		goal domain.Goal
		want map[string]string
	}{
		{"sam@example.com", goal, map[string]string{
			"Edit title and Why it matters": open(goal, "title-why-it-matters"),
			"Hand off":                      open(goal, "handoff"),
			"Add a delegate":                open(goal, "delegates"),
			"Link to a parent Goal":         open(goal, "parent-link"),
			"Add a child Goal":              child(goal),
			"Edit Dimension values":         open(goal, "dimensions"),
			"Edit Fields":                   open(goal, "fields"),
			"Put on hold…":                  fmt.Sprintf("/goals/%d/checkin?lifecycle=On+Hold", goal.ID),
			"Mark done…":                    fmt.Sprintf("/goals/%d/checkin?lifecycle=Done", goal.ID),
			"Cancel…":                       fmt.Sprintf("/goals/%d/checkin?lifecycle=Cancelled", goal.ID),
		}},
		{"ada@example.com", goal, map[string]string{
			"Edit title and Why it matters": open(goal, "title-why-it-matters"),
			"Hand off":                      open(goal, "handoff"),
			"Suggest a parent":              suggest(goal),
			"Add a child Goal":              child(goal),
			"Edit Dimension values":         open(goal, "dimensions"),
			"Edit Fields":                   open(goal, "fields"),
			"Mark Top-level":                open(goal, "top-level"),
			"Mark owner departed…":          open(goal, "depart"),
		}},
		{"ada@example.com", orphan, map[string]string{
			"Edit title and Why it matters": open(orphan, "title-why-it-matters"),
			"Suggest a parent":              suggest(orphan),
			"Add a child Goal":              child(orphan),
			"Edit Dimension values":         open(orphan, "dimensions"),
			"Edit Fields":                   open(orphan, "fields"),
			"Mark Top-level":                open(orphan, "top-level"),
			"Mark returned…":                open(orphan, "return"),
			"Reassign":                      open(orphan, "reassign"),
		}},
		{"mel@example.com", goal, map[string]string{
			"Suggest a parent": suggest(goal),
			"Add a child Goal": child(goal),
		}},
	} {
		page := getPage(t, signInClient(t, ts.URL, tc.who), goalPageURL(ts.URL, tc.goal))
		if got := goalMenuItemsIn(t, page); !maps.Equal(got, tc.want) {
			t.Errorf("%s on %q: menu is %v, want %v", tc.who, tc.goal.Title, got, tc.want)
		}
	}
}

// goalLifecycleMenuItemsIn are the action menu's Lifecycle items, label to
// where each leads.
func goalLifecycleMenuItemsIn(t *testing.T, root *nethtml.Node) map[string]string {
	t.Helper()
	out := map[string]string{}
	for label, href := range goalMenuItemsIn(t, root) {
		switch label {
		case "Put on hold…", "Mark done…", "Cancel…":
			out[label] = href
		}
	}
	return out
}

// Whoever may check in — the Owner or a Delegate — finds the Lifecycle changes
// the Goal allows in the action menu, after the other items: Put on hold…,
// Mark done… and Cancel… on an Active Goal, Cancel… alone on an On Hold one,
// none on a Proposed, Done or Cancelled one. Each leads to the Check-in page
// with that change picked. Nobody else sees them, an Admin included.
func TestGoalPageActionMenuOffersLifecycleChangesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("mel@example.com")
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	onHold := h.OnHoldGoal(sam, "Grow revenue", "Revenue funds the rest.", "Waiting on budget.")
	proposed := h.CreateGoal(sam, "Hire a team", "We need people.")
	done := h.ActiveGoal(sam, "Ship v1", "Customers wait.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	cancelled := h.ActiveGoal(sam, "Rewrite in Rust", "Speed.")
	h.EndGoalInCheckin(sam, cancelled.ID, domain.LifecycleCancelled)
	for _, g := range []domain.Goal{active, onHold, proposed, done, cancelled} {
		h.AddDelegate(sam, tpm, g.ID)
	}
	ts := newServer(t, h)
	checkin := func(g domain.Goal, to string) string {
		return fmt.Sprintf("/goals/%d/checkin?lifecycle=%s", g.ID, url.QueryEscape(to))
	}

	for _, who := range []string{"sam@example.com", "tpm@example.com"} {
		client := signInClient(t, ts.URL, who)
		for _, tc := range []struct {
			goal domain.Goal
			want map[string]string
		}{
			{active, map[string]string{
				"Put on hold…": checkin(active, domain.LifecycleOnHold),
				"Mark done…":   checkin(active, domain.LifecycleDone),
				"Cancel…":      checkin(active, domain.LifecycleCancelled),
			}},
			{onHold, map[string]string{"Cancel…": checkin(onHold, domain.LifecycleCancelled)}},
			{proposed, map[string]string{}},
			{done, map[string]string{}},
			{cancelled, map[string]string{}},
		} {
			page := getPage(t, client, goalPageURL(ts.URL, tc.goal))
			if got := goalLifecycleMenuItemsIn(t, page); !maps.Equal(got, tc.want) {
				t.Errorf("%s on the %s Goal: Lifecycle items are %v, want %v", who, tc.goal.Lifecycle, got, tc.want)
			}
		}
	}
	for _, who := range []string{"mel@example.com", "ada@example.com"} {
		page := getPage(t, signInClient(t, ts.URL, who), goalPageURL(ts.URL, active))
		if got := goalLifecycleMenuItemsIn(t, page); len(got) > 0 {
			t.Errorf("%s, who may not check in, sees Lifecycle items %v", who, got)
		}
	}

	// They come last in the menu, in that order.
	menu := goalActionMenuIn(t, getPage(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, active)))
	labels := textsOf(findAll(menu, func(n *nethtml.Node) bool { return n.Data == "a" }))
	if n := len(labels); n < 4 || !slices.Equal(labels[n-3:], []string{"Put on hold…", "Mark done…", "Cancel…"}) {
		t.Errorf("the Lifecycle items don't come last: %v", labels)
	}
}

// assertGoalFormOpensBetween checks the open form sits in the page's block
// from the first element carrying data-testid start up to the first carrying
// next, the part of the page it concerns: it opens after start does, or inside
// it, and before next.
func assertGoalFormOpensBetween(t *testing.T, root, open *nethtml.Node, start, next string) {
	t.Helper()
	from, to := findAllByTestID(root, start), findAllByTestID(root, next)
	if len(from) == 0 || len(to) == 0 || !precedes(from[0], open) || !precedes(open, to[0]) {
		t.Errorf("%q does not open between %s and %s", attrOf(open, "data-open-form"), start, next)
	}
}

// goalFormsPostingTo returns the forms under root whose action is action.
func goalFormsPostingTo(root *nethtml.Node, action string) []*nethtml.Node {
	return findAll(root, func(n *nethtml.Node) bool { return n.Data == "form" && attrOf(n, "action") == action })
}

// Following an action menu item reloads the Goal page with exactly that form
// open, focused and in the part of the page it concerns, with a Cancel back to
// the plain page, which shows no form open. No script is involved: the menu
// item is a link and the form a plain post.
func TestGoalPageMenuItemOpensItsFormInPlaceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.CreateDimension(ada, "Pillar", "Growth")
	h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	clients := map[string]*http.Client{
		"sam": signInClient(t, ts.URL, "sam@example.com"),
		"ada": signInClient(t, ts.URL, "ada@example.com"),
	}
	const (
		head       = "goal-head"
		latest     = "goal-checkins"
		parents    = "goal-parents"
		children   = "goal-children"
		people     = "goal-people"
		dimensions = "goal-dimensions"
		fields     = "goal-field-block"
	)

	for _, tc := range []struct {
		who, label  string
		goal        domain.Goal
		form        string
		action      string
		start, next string
	}{
		{"sam", "Edit title and Why it matters", goal, "title-why-it-matters", fmt.Sprintf("/goals/%d/why-it-matters", goal.ID), head, latest},
		{"sam", "Hand off", goal, "handoff", fmt.Sprintf("/goals/%d/handoff", goal.ID), people, dimensions},
		{"sam", "Add a delegate", goal, "delegates", fmt.Sprintf("/goals/%d/delegates", goal.ID), people, dimensions},
		{"sam", "Link to a parent Goal", goal, "parent-link", fmt.Sprintf("/goals/%d/links", goal.ID), parents, children},
		{"sam", "Edit Dimension values", goal, "dimensions", fmt.Sprintf("/goals/%d/dimensions", goal.ID), dimensions, fields},
		{"sam", "Edit Fields", goal, "fields", fmt.Sprintf("/goals/%d/fields", goal.ID), fields, parents},
		{"ada", "Edit title and Why it matters", goal, "title-why-it-matters", fmt.Sprintf("/goals/%d/why-it-matters", goal.ID), head, latest},
		{"ada", "Hand off", goal, "handoff", fmt.Sprintf("/goals/%d/handoff", goal.ID), people, dimensions},
		{"ada", "Mark Top-level", goal, "top-level", fmt.Sprintf("/goals/%d/top-level", goal.ID), head, latest},
		{"ada", "Mark owner departed…", goal, "depart", fmt.Sprintf("/accounts/%d/depart", sam.ID), people, dimensions},
		{"ada", "Reassign", orphan, "reassign", fmt.Sprintf("/goals/%d/reassign", orphan.ID), people, dimensions},
		{"ada", "Mark returned…", orphan, "return", fmt.Sprintf("/accounts/%d/return", kim.ID), people, dimensions},
	} {
		client := clients[tc.who]
		plain := getPage(t, client, goalPageURL(ts.URL, tc.goal))
		if open := goalOpenFormsIn(plain); len(open) != 0 {
			t.Errorf("%s: the plain Goal page shows %d forms open: %q", tc.label, len(open), open)
		}
		page := getPage(t, client, ts.URL+goalMenuItemsIn(t, plain)[tc.label])
		open := goalOpenFormIn(t, page, tc.form)
		if len(goalFormsPostingTo(open, tc.action)) == 0 {
			t.Errorf("%s opens no form posting to %s: %s", tc.label, tc.action, renderNode(open))
		}
		if n := len(findAll(open, func(n *nethtml.Node) bool { return hasAttr(n, "autofocus") })); n != 1 {
			t.Errorf("%s: open form has %d focused controls, want 1: %s", tc.label, n, renderNode(open))
		}
		assertGoalFormOpensBetween(t, page, open, tc.start, tc.next)
		cancel := attrOf(findByTestID(t, open, "cancel-form"), "href")
		if cancel != fmt.Sprintf("/goals/%d", tc.goal.ID) {
			t.Errorf("%s: Cancel leads to %q, not the plain Goal page", tc.label, cancel)
		}
		if open := goalOpenFormsIn(getPage(t, client, ts.URL+cancel)); len(open) != 0 {
			t.Errorf("%s: after Cancel the page shows %d forms open: %q", tc.label, len(open), open)
		}
	}
}

// Each sidebar group's heading carries a small link, for whoever may use it,
// that opens the group's form right there: Manage for Delegates, Edit for
// Dimensions and for Fields, + Add for Contributes to.
// Nothing on the page jumps to a collapsed section any more. How the headings
// look is TestGoalPageSidebarLinksOpenTheirFormInPlaceOverHTTPStyles's.
func TestGoalPageSidebarLinksOpenTheirFormInPlaceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	h.CreateDimension(ada, "Pillar", "Growth")
	h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	ts := newServer(t, h)
	sam2 := signInClient(t, ts.URL, "sam@example.com")
	plain := getPage(t, sam2, goalPageURL(ts.URL, goal))

	for _, tc := range []struct {
		group, heading, link, form, action, next string
	}{
		{"goal-parents", "Contributes to", "+ Add", "parent-link", "/links", "goal-children"},
		{"goal-delegates", "Delegates", "Manage", "delegates", "/delegates", "goal-dimensions"},
		{"goal-dimensions", "Dimensions", "Edit", "dimensions", "/dimensions", "goal-field-block"},
		{"goal-field-block", "Fields", "Edit", "fields", "/fields", "goal-parents"},
	} {
		group := findByTestID(t, plain, tc.group)
		heading := headingNamed(group, tc.heading)
		if heading == nil || heading.Data != "h3" {
			t.Errorf("%s's heading is not an h3 reading %s: %s", tc.group, tc.heading, renderNode(group))
			continue
		}
		link := findByTestID(t, group, "open-"+tc.form)
		if link.Data != "a" || link.Parent != heading.Parent {
			t.Errorf("%s's link doesn't sit in its heading: %s", tc.group, renderNode(heading.Parent))
		}
		if text := textOf(link); text != tc.link {
			t.Errorf("%s's link reads %q, want %q", tc.group, text, tc.link)
		}
		if name := attrOf(link, "aria-label"); !strings.Contains(name, strings.TrimPrefix(tc.link, "+ ")) {
			t.Errorf("%s's link is named %q, which lacks its text %q", tc.group, name, tc.link)
		}
		page := getPage(t, sam2, ts.URL+attrOf(link, "href"))
		open := goalOpenFormIn(t, page, tc.form)
		if len(goalFormsPostingTo(open, fmt.Sprintf("/goals/%d%s", goal.ID, tc.action))) == 0 {
			t.Errorf("%s's link opens no form posting to %s: %s", tc.group, tc.action, renderNode(open))
		}
		assertGoalFormOpensBetween(t, page, open, tc.group, tc.next)
		if hasTestID(findByTestID(t, page, tc.group), "open-"+tc.form) {
			t.Errorf("%s still offers its link with its form open", tc.group)
		}
	}
	if jumps := findAll(plain, func(n *nethtml.Node) bool { return n.Data == "a" && strings.HasPrefix(attrOf(n, "href"), "#") }); len(jumps) > 0 {
		t.Errorf("the Goal page still links to a section further down: %s", renderNode(jumps[0]))
	}
	if folds := findAll(findByTestID(t, plain, "goal-sidebar"), func(n *nethtml.Node) bool { return n.Data == "details" }); len(folds) > 0 {
		t.Errorf("the sidebar still folds a form into a collapsed section: %s", renderNode(folds[0]))
	}

	mel := getPage(t, signInClient(t, ts.URL, "mel@example.com"), goalPageURL(ts.URL, goal))
	if offered := findAll(findByTestID(t, mel, "goal-sidebar"), func(n *nethtml.Node) bool {
		return strings.HasPrefix(attrOf(n, "data-testid"), "open-")
	}); len(offered) > 0 {
		t.Errorf("someone with no role on the Goal is offered a sidebar form: %s", renderNode(offered[0]))
	}
}

// A refused submit from a form opened in place comes back as the Goal page
// with that same form open, the reason beside it as a sentence without the
// internal "validation failed:" prefix (#192), and what was typed still in it,
// rather than a bare error page.
func TestGoalPageRefusedFormComesBackOpenOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.CreateExtendableDimension(ada, "Team", "Core")
	budget := h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	team := dimensionByName(t, h, "Team")
	ts := newServer(t, h)
	clients := map[string]*http.Client{
		"sam": signInClient(t, ts.URL, "sam@example.com"),
		"ada": signInClient(t, ts.URL, "ada@example.com"),
	}
	path := func(g domain.Goal, rest string) string { return fmt.Sprintf("/goals/%d%s", g.ID, rest) }

	for _, tc := range []struct {
		who, form, action string
		sent              url.Values
		reason            string
		// field is the control that keeps what was typed, typed.
		field, typed string
	}{
		{"sam", "handoff", path(goal, "/handoff"), url.Values{"to_email": {"nobody@example.com"}}, "No account with email", "to_email", "nobody@example.com"},
		{"ada", "reassign", path(orphan, "/reassign"), url.Values{"email": {"nobody@example.com"}}, "No account with email", "email", "nobody@example.com"},
		{"sam", "parent-link", path(goal, "/links"), url.Values{"parent_id": {fmt.Sprint(goal.ID)}, "note": {"Because."}}, "cannot contribute to itself", "note", "Because."},
		{"sam", "parent-link", path(goal, "/links"), url.Values{"parent_id": {""}, "note": {"Because."}}, "A parent Goal is required.", "note", "Because."},
		{"sam", "delegates", path(goal, "/delegates"), url.Values{"email": {"nobody@example.com"}}, "nobody@example.com", "email", "nobody@example.com"},
		{"sam", "milestones", path(goal, "/milestones"), url.Values{"name": {"  "}, "target_date": {"2026-05-01"}}, "A Milestone needs a name.", "target_date", "2026-05-01"},
		{"sam", "milestones", path(goal, "/milestones"), url.Values{"name": {"GA"}, "target_date": {""}}, "A Milestone needs a date.", "name", "GA"},
		{"sam", "dimensions", path(goal, "/dimensions"), url.Values{"dimension_id": {fmt.Sprint(team.ID)}, "new_value": {"  "}}, "cannot be blank", "new_value", "  "},
		{"sam", "fields", path(goal, "/fields"), url.Values{"field_id": {fmt.Sprint(budget.ID)}, "value": {"lots"}}, "Budget", "value", "lots"},
	} {
		resp := postForm(t, clients[tc.who], ts.URL+tc.action, tc.sent)
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: refused submit is %d, want %d", tc.form, resp.StatusCode, http.StatusUnprocessableEntity)
			continue
		}
		open := goalOpenFormIn(t, parsePage(t, page), tc.form)
		reason := textOf(findByTestID(t, open, "form-error"))
		if !strings.Contains(reason, tc.reason) {
			t.Errorf("%s: open form's error %q doesn't say %q", tc.form, reason, tc.reason)
		}
		for _, internal := range []string{"validation failed", "not authorized"} {
			if strings.Contains(reason, internal) {
				t.Errorf("%s: open form's error %q carries the internal %q prefix", tc.form, reason, internal)
			}
		}
		if typed := formFields(open, tc.field); !slices.ContainsFunc(typed, func(f pageField) bool { return f.Value == tc.typed }) {
			t.Errorf("%s: open form lost what was typed (%s %q): %s", tc.form, tc.field, tc.typed, renderNode(open))
		}
	}
}

// Add Milestone answers a date that doesn't parse by naming it, as Edit
// Milestone does, and only once it has checked who sent it: the Owner or a
// Delegate gets the form back with the reason beside it, a stranger only the
// refusal, and an empty date is still no date (#595).
func TestAddMilestoneNamesAnUnparseableDateOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dana, goal.ID)
	ts := newServer(t, h)
	action := fmt.Sprintf("%s/goals/%d/milestones", ts.URL, goal.ID)

	for _, tc := range []struct {
		who, email, date string
		status           int
		// reason is the open form's error, or the refusal page's for a 403.
		reason string
	}{
		{"the Owner", "sam@example.com", "soon", http.StatusUnprocessableEntity, `"soon" isn't a date.`},
		{"a Delegate", "dana@example.com", "soon", http.StatusUnprocessableEntity, `"soon" isn't a date.`},
		{"the Owner", "sam@example.com", "", http.StatusUnprocessableEntity, "A Milestone needs a date."},
		{"a stranger", "pat@example.com", "soon", http.StatusForbidden, "Only the Owner or a Delegate may change this Goal's Milestones."},
	} {
		resp := postForm(t, signInClient(t, ts.URL, tc.email), action, url.Values{"name": {"GA"}, "target_date": {tc.date}})
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != tc.status {
			t.Errorf("%s adding with date %q: status %d, want %d", tc.who, tc.date, resp.StatusCode, tc.status)
			continue
		}
		var reason string
		if tc.status == http.StatusForbidden {
			reason = textOf(findByTestID(t, page, "refused-reason"))
		} else {
			open := goalOpenFormIn(t, page, "milestones")
			reason = textOf(findByTestID(t, open, "form-error"))
			if typed := formFields(open, "name"); !slices.ContainsFunc(typed, func(f pageField) bool { return f.Value == "GA" }) {
				t.Errorf("%s adding with date %q: open form lost the typed name: %s", tc.who, tc.date, renderNode(open))
			}
		}
		if reason != tc.reason {
			t.Errorf("%s adding with date %q: reason %q, want %q", tc.who, tc.date, reason, tc.reason)
		}
	}
	if ms, _ := h.Service.ListMilestones(t.Context(), goal.ID); len(ms) != 1 {
		t.Errorf("ListMilestones = %+v, want only the one the Goal began with", ms)
	}
}

// goalPageNestedControls returns every interactive element, a link, button,
// label or summary, that page opens inside another one, which HTML forbids.
// It reads the tags as html's tokenizer reads them, since the parser would
// close the outer element first and hide the nesting.
func goalPageNestedControls(page string) []string {
	var nested []string
	depth := 0
	z := nethtml.NewTokenizer(strings.NewReader(page))
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			return nested
		}
		tok := z.Token()
		switch tok.Data {
		case "a", "button", "label", "summary":
		default:
			continue
		}
		switch tt {
		case nethtml.EndTagToken:
			depth = max(depth-1, 0)
		case nethtml.StartTagToken:
			if depth > 0 {
				nested = append(nested, tok.String())
			}
			depth++
		}
	}
}

// Opening an action in place changes nothing about which ones ask first: Mark
// owner departed and Mark returned still confirm in a browser dialog, and Hand
// off, Reassign and Top-level still submit at once.
func TestGoalPageOpenFormsKeepTheirConfirmationsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, kim.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	ts := newServer(t, h)
	adaClient := signInClient(t, ts.URL, "ada@example.com")
	opened := func(g domain.Goal, form string) *nethtml.Node {
		return goalOpenFormIn(t, getPage(t, adaClient, fmt.Sprintf("%s/goals/%d?open=%s", ts.URL, g.ID, form)), form)
	}
	assertConfirmsFirst := func(what string, open *nethtml.Node, action string) {
		t.Helper()
		forms := goalFormsPostingTo(open, action)
		if len(forms) != 1 || !strings.HasPrefix(attrOf(forms[0], "onsubmit"), "return confirm(") {
			t.Errorf("%s doesn't ask for confirmation: %s", what, renderNode(open))
		}
	}

	assertConfirmsFirst("Mark departed", opened(goal, "depart"), fmt.Sprintf("/accounts/%d/depart", sam.ID))
	assertConfirmsFirst("Mark returned", opened(orphan, "return"), fmt.Sprintf("/accounts/%d/return", kim.ID))
	assertSubmitsAtOnce(t, "Hand off", renderNode(opened(goal, "handoff")), "/handoff")
	assertSubmitsAtOnce(t, "Reassign", renderNode(opened(orphan, "reassign")), "/reassign")
	assertSubmitsAtOnce(t, "Top-level", renderNode(opened(goal, "top-level")), "/top-level")
	for _, form := range []string{"depart", "handoff"} {
		if nested := goalPageNestedControls(getBody(t, adaClient, fmt.Sprintf("%s/goals/%d?open=%s", ts.URL, goal.ID, form))); len(nested) > 0 {
			t.Errorf("the Goal page with %s open nests a control inside another: %q", form, nested)
		}
	}
}

// ownerOnlyRoute is one of the Goal-changing routes only the Goal's Owner may
// use (#191): its path for a Goal and Metric, and a form that changes the Goal.
type ownerOnlyRoute struct {
	name string
	path func(goalID, metricID int64) string
	form url.Values
}

func ownerOnlyRoutes() []ownerOnlyRoute {
	onGoal := func(suffix string) func(goalID, metricID int64) string {
		return func(goalID, _ int64) string { return fmt.Sprintf("/goals/%d/%s", goalID, suffix) }
	}
	return []ownerOnlyRoute{
		{"edit metric", func(_, metricID int64) string { return fmt.Sprintf("/metrics/%d", metricID) }, url.Values{
			"name": {"p99 checkout latency"}, "unit": {"ms"}, "direction": {"down"},
			"baseline": {"1500"}, "target": {"500"}, "target_date": {"2026-06-15"},
		}},
		{"activate", onGoal("activate"), nil},
		{"why it matters", onGoal("why-it-matters"), url.Values{"why_it_matters": {"Faster checkout lifts conversion."}}},
	}
}

// ownerOnlyGoal arranges a Proposed Dated Goal with a Metric, so it could be
// activated, and returns it with the Metric's id.
func ownerOnlyGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, int64) {
	t.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalDated(ctx, g.Owner.ID, g.ID, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	m, err := h.Service.AddMetric(ctx, g.Owner.ID, domain.AddMetricInput{
		GoalID: g.ID, Name: "p95 checkout latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1200, Target: 400, TargetDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	return g, m.ID
}

// ownerOnlyGoalState renders everything the owner-only routes can change about
// a Goal, so a test can tell whether a request changed it.
func ownerOnlyGoalState(t *testing.T, h *testsupport.Harness, goalID int64) string {
	t.Helper()
	ctx := context.Background()
	g, err := h.Service.ViewGoal(ctx, goalID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	metrics, err := h.Service.ListMetrics(ctx, goalID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	revisions, err := h.Service.ListWhyItMattersRevisions(ctx, goalID)
	if err != nil {
		t.Fatalf("ListWhyItMattersRevisions: %v", err)
	}
	return fmt.Sprintf("goal %+v\nmetrics %+v\nrevisions %+v", g, metrics, revisions)
}

// Editing a Goal's Metrics and Activating it are the Owner's alone, and its
// Why it matters the Owner's or an Admin's (#281): anyone else, a Delegate
// included, is refused with 403 and the Goal is left as it was (#191).
func TestOnlyTheOwnerSetsUpAGoalOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	delegate := h.SignIn("tpm@example.com")
	h.SignIn("other@example.com")
	ts := newServer(t, h)
	clients := map[string]*http.Client{
		"another Account": signInClient(t, ts.URL, "other@example.com"),
		"a Delegate":      signInClient(t, ts.URL, "tpm@example.com"),
	}

	for _, route := range ownerOnlyRoutes() {
		g, metricID := ownerOnlyGoal(t, h, owner)
		h.AddDelegate(owner, delegate, g.ID)
		before := ownerOnlyGoalState(t, h, g.ID)
		for who, client := range clients {
			resp := postForm(t, client, ts.URL+route.path(g.ID, metricID), route.form)
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s by %s: status = %d, want 403", route.name, who, resp.StatusCode)
			}
			if after := ownerOnlyGoalState(t, h, g.ID); after != before {
				t.Errorf("%s by %s changed the Goal:\nbefore %s\nafter  %s", route.name, who, before, after)
			}
		}
	}
}

// The Owner still uses each owner-only route as before (#191).
func TestOwnerStillSetsUpTheirGoalOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")

	for _, route := range ownerOnlyRoutes() {
		g, metricID := ownerOnlyGoal(t, h, owner)
		before := ownerOnlyGoalState(t, h, g.ID)
		resp := postForm(t, client, ts.URL+route.path(g.ID, metricID), route.form)
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", g.ID) {
			t.Errorf("%s by the Owner: status = %d at %s, want 200 back on the Goal", route.name, resp.StatusCode, resp.Request.URL.Path)
		}
		if after := ownerOnlyGoalState(t, h, g.ID); after == before {
			t.Errorf("%s by the Owner left the Goal unchanged: %s", route.name, after)
		}
	}
}

// A missing Goal, or Metric, is not found on the owner-only routes, whatever
// the form holds (#191).
func TestOwnerOnlyRoutesNotFoundForAMissingGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("owner@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")

	for _, route := range ownerOnlyRoutes() {
		for _, form := range []url.Values{route.form, nil} {
			resp := postForm(t, client, ts.URL+route.path(999, 999), form)
			defer func() { _ = resp.Body.Close() }()
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s on a missing id with form %v: status = %d, want 404", route.name, form, resp.StatusCode)
			}
		}
	}
}

// titleWhyItMattersOpen is the Goal page with the Edit title and Why it matters form open.
func titleWhyItMattersOpen(g domain.Goal) string {
	return fmt.Sprintf("/goals/%d?open=title-why-it-matters", g.ID)
}

// The Owner and an Admin get Edit title and Why it matters on a Goal in any
// Lifecycle, opening a form in the page head that holds the current title and
// Why it matters. A Delegate and another Account get neither the item nor the form
// (#281).
func TestEditTitleAndWhyItMattersIsOfferedToTheOwnerAndAnAdminOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("mel@example.com")
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	done := h.ActiveGoal(sam, "Ship v2", "Customers wait for v2.")
	h.EndGoalInCheckin(sam, done.ID, domain.LifecycleDone)
	ts := newServer(t, h)

	for _, g := range []domain.Goal{active, proposed, done} {
		h.AddDelegate(sam, tpm, g.ID)
		for _, who := range []string{"sam@example.com", "ada@example.com"} {
			client := signInClient(t, ts.URL, who)
			if got := goalMenuItemsIn(t, getPage(t, client, goalPageURL(ts.URL, g)))["Edit title and Why it matters"]; got != titleWhyItMattersOpen(g) {
				t.Errorf("%s on %q: Edit title and Why it matters leads to %q, want %q", who, g.Title, got, titleWhyItMattersOpen(g))
			}
			open := goalOpenFormIn(t, getPage(t, client, ts.URL+titleWhyItMattersOpen(g)), "title-why-it-matters")
			if headingNamed(open, "Edit title and Why it matters") == nil {
				t.Errorf("%s on %q: the open form is not labelled Edit title and Why it matters: %s", who, g.Title, renderNode(open))
			}
			if title := formField(t, open, "title"); title.Type != "text" || title.Value != g.Title || !hasAttr(title.Node, "autofocus") {
				t.Errorf("%s on %q: the Title input is not a focused text input holding %q: %s", who, g.Title, g.Title, renderNode(title.Node))
			}
			if why := formFields(open, "why_it_matters"); len(why) != 1 || why[0].Type != "textarea" || why[0].Value != g.WhyItMatters {
				t.Errorf("%s on %q: the Why it matters textarea doesn't hold %q: %s", who, g.Title, g.WhyItMatters, renderNode(open))
			}
			if buttonNamed(open, "Save") == nil {
				t.Errorf("%s on %q: the form has no Save button: %s", who, g.Title, renderNode(open))
			}
		}
		for _, who := range []string{"tpm@example.com", "mel@example.com"} {
			client := signInClient(t, ts.URL, who)
			if got, ok := goalMenuItemsIn(t, getPage(t, client, goalPageURL(ts.URL, g)))["Edit title and Why it matters"]; ok {
				t.Errorf("%s on %q is offered Edit title and Why it matters, leading to %q", who, g.Title, got)
			}
			if open := goalOpenFormsIn(getPage(t, client, ts.URL+titleWhyItMattersOpen(g))); len(open) != 0 {
				t.Errorf("%s on %q: ?open=title-why-it-matters shows %d forms open, want none", who, g.Title, len(open))
			}
		}
	}
}

// The Owner, and an Admin, change an Active Goal's title from the Edit title
// and Why it matters form: the save lands on the plain Goal page, which shows the new
// title, and its History carries a Title entry reading old → new, by whoever
// made it (#281).
func TestOwnerOrAdminChangesAnActiveGoalsTitleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	ts := newServer(t, h)

	for _, who := range []string{"sam@example.com", "ada@example.com"} {
		g := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
		client := signInClient(t, ts.URL, who)
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
			"title":          {"  Halve outages  "},
			"why_it_matters": {g.WhyItMatters},
		})
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", g.ID) || resp.Request.URL.RawQuery != "" {
			t.Errorf("%s: save is %d at %s, want 200 on the plain Goal page", who, resp.StatusCode, resp.Request.URL)
		}
		if title := findByTestID(t, page, "goal-title"); title.Data != "h1" || textOf(title) != "Halve outages" {
			t.Errorf("%s: the page's title is %s, want Halve outages", who, renderNode(title))
		}
		var titles []*nethtml.Node
		for _, e := range findAllByTestID(findByTestID(t, page, "goal-history"), "history-entry") {
			if attrOf(e, "data-kind") == "title" {
				titles = append(titles, e)
			}
		}
		if len(titles) != 1 {
			t.Fatalf("%s: History has %d title entries, want 1", who, len(titles))
		}
		text := textOf(titles[0])
		for _, want := range []string{"Title", "Reduce outages → Halve outages"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the Title entry %q doesn't say %q", who, text, want)
			}
		}
		label := strings.Split(who, "@")[0]
		if by := "by " + label; !strings.Contains(runningText(titles[0]), by) || !goalShows(titles[0], who, label) {
			t.Errorf("%s: the Title entry doesn't say it is %s, with their email a click away: %s", who, by, renderNode(titles[0]))
		}
	}
}

// titleWhyItMattersHistory counts a Goal's Why it matters revisions and title changes.
func titleWhyItMattersHistory(t *testing.T, h *testsupport.Harness, goalID int64) (whyItMattersRevisions, titles int) {
	t.Helper()
	revisions, err := h.Service.ListWhyItMattersRevisions(t.Context(), goalID)
	if err != nil {
		t.Fatalf("ListWhyItMattersRevisions: %v", err)
	}
	titleRevisions, err := h.Service.ListTitleRevisions(t.Context(), goalID)
	if err != nil {
		t.Fatalf("ListTitleRevisions: %v", err)
	}
	return len(revisions), len(titleRevisions)
}

// Saving the Edit title and Why it matters form changes only the field that
// differs: an unchanged title records no title change, and an unchanged Why it
// matters no Why it matters revision (#281).
func TestEditTitleAndWhyItMattersRecordsOnlyWhatChangedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	g := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	whyItMattersRevisions, titles := titleWhyItMattersHistory(t, h, g.ID)
	_ = postFormBody(t, client, fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
		"title": {"Reduce outages"}, "why_it_matters": {"Outages cost us customers."},
	})
	if s, ti := titleWhyItMattersHistory(t, h, g.ID); s != whyItMattersRevisions+1 || ti != titles {
		t.Errorf("only the Why it matters changed: %d Why it matters revisions and %d title changes, want %d and %d", s, ti, whyItMattersRevisions+1, titles)
	}

	g = h.ActiveGoal(sam, "Grow revenue", "Revenue funds the rest.")
	whyItMattersRevisions, titles = titleWhyItMattersHistory(t, h, g.ID)
	_ = postFormBody(t, client, fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
		"title": {"Grow revenue 20%"}, "why_it_matters": {" Revenue funds the rest. "},
	})
	if s, ti := titleWhyItMattersHistory(t, h, g.ID); s != whyItMattersRevisions || ti != titles+1 {
		t.Errorf("only the title changed: %d Why it matters revisions and %d title changes, want %d and %d", s, ti, whyItMattersRevisions, titles+1)
	}
}

// The Edit title and Why it matters form posts to /why-it-matters; the route it
// had while the term was So What is gone, with no redirect (#381).
func TestEditWhyItMattersHasOnlyItsNewRouteOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	g := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/so-what", ts.URL, g.ID), url.Values{
		"title": {"Reduce outages"}, "so_what": {"Outages cost us customers."},
	})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("the old route answers %d, want 404 or 405", resp.StatusCode)
	}

	resp = postForm(t, client, fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
		"title": {"Reduce outages"}, "why_it_matters": {"Outages cost us customers."},
	})
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save is %d, want 200", resp.StatusCode)
	}
	if why := textOf(findByTestID(t, page, "goal-why-it-matters")); why != "Outages cost us customers." {
		t.Errorf("the Goal page shows the Why it matters %q, want the saved one", why)
	}
	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.WhyItMatters != "Outages cost us customers." {
		t.Errorf("Why it matters = %q, want the edit saved", got.WhyItMatters)
	}
}

// A blank title is refused as at creation, and a blank Why it matters as ever: the
// page comes back with the form open, the reason beside it and what was typed,
// and neither field is saved (#281).
func TestEditTitleAndWhyItMattersRefusesABlankTitleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	whyItMattersRevisions, titles := titleWhyItMattersHistory(t, h, g.ID)

	resp := postForm(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
		"title": {"   "}, "why_it_matters": {"Outages cost us customers."},
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a blank title is %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	open := goalOpenFormIn(t, parsePage(t, page), "title-why-it-matters")
	if reason := textOf(findByTestID(t, open, "form-error")); !strings.Contains(reason, "A Goal needs a title.") {
		t.Errorf("the open form's error %q doesn't say A Goal needs a title.", reason)
	}
	if why := formField(t, open, "why_it_matters"); why.Type != "textarea" || why.Value != "Outages cost us customers." {
		t.Errorf("the open form lost the Why it matters as typed: %s", renderNode(open))
	}
	got, err := h.Service.ViewGoal(t.Context(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Title != "Reduce outages" || got.WhyItMatters != "Outages cost trust." {
		t.Errorf("a refused save changed the Goal to %q / %q", got.Title, got.WhyItMatters)
	}
	if s, ti := titleWhyItMattersHistory(t, h, g.ID); s != whyItMattersRevisions || ti != titles {
		t.Errorf("a refused save recorded %d Why it matters revisions and %d title changes, want %d and %d", s, ti, whyItMattersRevisions, titles)
	}

	resp = postForm(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID), url.Values{
		"title": {"Halve outages"}, "why_it_matters": {"  "},
	})
	page = readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a blank Why it matters is %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	if open := goalOpenFormIn(t, parsePage(t, page), "title-why-it-matters"); formField(t, open, "title").Value != "Halve outages" {
		t.Errorf("the open form lost the title as typed: %s", renderNode(open))
	}
	if got, _ := h.Service.ViewGoal(t.Context(), g.ID); got.Title != "Reduce outages" {
		t.Errorf("a save refused for its Why it matters still changed the title to %q", got.Title)
	}
	if s, ti := titleWhyItMattersHistory(t, h, g.ID); s != whyItMattersRevisions || ti != titles {
		t.Errorf("a save refused for its Why it matters recorded %d Why it matters revisions and %d title changes, want %d and %d", s, ti, whyItMattersRevisions, titles)
	}
}

// Nobody but the Owner or an Admin edits the title and Why it matters: a Delegate or
// another Account is refused with 403 and the Goal is left as it was. A post
// with only a Why it matters, as before the form had a title, still edits the Why it
// matters and leaves the title (#281).
func TestEditTitleAndWhyItMattersIsTheOwnersOrAnAdminsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("mel@example.com")
	g := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, g.ID)
	ts := newServer(t, h)
	goalURL := fmt.Sprintf("%s/goals/%d/why-it-matters", ts.URL, g.ID)

	for _, who := range []string{"tpm@example.com", "mel@example.com"} {
		resp := postForm(t, signInClient(t, ts.URL, who), goalURL, url.Values{
			"title": {"Halve outages"}, "why_it_matters": {"Outages cost us customers."},
		})
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", who, resp.StatusCode)
		}
		got, _ := h.Service.ViewGoal(t.Context(), g.ID)
		if got.Title != "Reduce outages" || got.WhyItMatters != "Outages cost trust." {
			t.Errorf("%s changed the Goal to %q / %q", who, got.Title, got.WhyItMatters)
		}
		if _, titles := titleWhyItMattersHistory(t, h, g.ID); titles != 0 {
			t.Errorf("%s recorded %d title changes", who, titles)
		}
	}

	_ = postFormBody(t, signInClient(t, ts.URL, "sam@example.com"), goalURL, url.Values{
		"why_it_matters": {"Outages cost us customers."},
	})
	got, _ := h.Service.ViewGoal(t.Context(), g.ID)
	if got.Title != "Reduce outages" || got.WhyItMatters != "Outages cost us customers." {
		t.Errorf("a post with only a Why it matters left the Goal at %q / %q, want the title kept and the Why it matters changed", got.Title, got.WhyItMatters)
	}
}

// goalEmphasisedDates is what each date under root shown emphasised reads, in
// order: a <strong>, as the date a Goal now delivers shows.
func goalEmphasisedDates(root *nethtml.Node) []string {
	return textsOf(findAll(root, func(n *nethtml.Node) bool { return n.Data == "strong" }))
}

// goalPlainDates is what each date under root shown plainly reads, in order:
// a <span> carrying nothing else, as the date a slipped date now stands at
// shows before what it slipped from, struck through.
func goalPlainDates(root *nethtml.Node) []string {
	return textsOf(findAll(root, func(n *nethtml.Node) bool { return n.Data == "span" && len(n.Attr) == 0 }))
}

// The Goal page shows its delivery date, the date it slipped from and its
// Milestone's date as "Jul 16", while the Check-in form's date inputs still
// hold ISO values (ticket #276).
func TestGoalPageShowsDatesInTheDateFormat(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.") // due Jul 2 2026, Beta on Apr 2
	client := signInClient(t, ts.URL, "sam@example.com")
	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health":               {domain.HealthYellow},
		"status":               {"Vendor is late."},
		"path_to_green":        {"Swap vendors."},
		"path_target_date":     {"2026-06-15"},
		"delivery_date":        {"2026-07-16"},
		"delivery_date_reason": {"Vendor API delayed two weeks."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit check-in: status %d", resp.StatusCode)
	}

	page := getPage(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	delivery := findByTestID(t, page, "goal-delivery-date")
	struck, delivers := findAll(delivery, func(n *nethtml.Node) bool { return n.Data == "del" }), findAll(delivery, func(n *nethtml.Node) bool { return n.Data == "strong" })
	if got, want := struckThrough(delivery), []string{"Jul 2"}; !slices.Equal(got, want) || !slices.Equal(goalEmphasisedDates(delivery), []string{"Jul 16"}) || !precedes(struck[0], delivers[0]) {
		t.Errorf("the delivery date is not shown as ~~Jul 2~~ Jul 16; element:\n%s", renderNode(delivery))
	}
	if milestone := findByTestID(t, page, "goal-milestone"); !slices.Contains(goalPlainDates(milestone), "Apr 2") {
		t.Errorf("the Milestone's date is not shown as Apr 2; row:\n%s", renderNode(milestone))
	}

	form := getPage(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	if got := formField(t, form, "delivery_date").Value; got != "2026-07-16" {
		t.Errorf("the Check-in form's delivery date input holds %q, want 2026-07-16", got)
	}
	beta := findAll(form, func(n *nethtml.Node) bool { return n.Data == "input" && attrOf(n, "aria-label") == "Date of Beta" })
	if len(beta) != 1 || attrOf(beta[0], "value") != "2026-04-02" {
		t.Errorf("the Check-in form's Date of Beta input doesn't hold 2026-04-02")
	}
}

// goalActionItemFixture is a Goal, Launch in EU, that two Reports, MBR and
// QBR, cover, each published so Action Items about it can be raised there.
type goalActionItemFixture struct {
	h      *testsupport.Harness
	author domain.Account
	sam    domain.Account
	goal   domain.Goal
	mbr    domain.ReportDefinition
	mbrPub domain.Publication
	qbr    domain.ReportDefinition
	qbrPub domain.Publication
}

func newGoalActionItemFixture(h *testsupport.Harness) goalActionItemFixture {
	f := goalActionItemFixture{h: h, author: h.SignIn("author@example.com"), sam: h.SignIn("sam.smith@example.com")}
	f.goal = h.ActiveGoal(f.author, "Launch in EU", "Expand the market.")
	f.mbr = h.SaveReportDefinition(f.author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{f.goal.ID}})
	f.mbrPub = h.PublishReport(f.author, f.mbr)
	f.qbr = h.SaveReportDefinition(f.author, domain.SaveReportDefinitionInput{Name: "QBR", Mode: domain.ReportModePicked, Picked: []int64{f.goal.ID}})
	f.qbrPub = h.PublishReport(f.author, f.qbr)
	return f
}

// raise raises an Action Item on pub for owner, due in dueIn days (negative
// for overdue), about goalID, or the whole Report for 0.
func (f goalActionItemFixture) raise(pub domain.Publication, owner domain.Account, text string, dueIn int, goalID int64) domain.ActionItem {
	f.h.T.Helper()
	item, err := f.h.Service.RaiseActionItem(context.Background(), f.author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: goalID, Text: text, OwnerID: owner.ID, DueDate: f.h.Clock.Now().AddDate(0, 0, dueIn),
	})
	if err != nil {
		f.h.T.Fatalf("RaiseActionItem: %v", err)
	}
	return item
}

// goalActionItemRowsIn returns each row of the Goal page's Action Items, in
// order, or none when the page has no such section. The section is the
// page's #action-items, where a closed Action Item comes back to.
func goalActionItemRowsIn(t *testing.T, root *nethtml.Node) []*nethtml.Node {
	t.Helper()
	sections := findAllByTestID(root, "goal-action-items")
	if len(sections) == 0 {
		return nil
	}
	if id := attrOf(sections[0], "id"); id != "action-items" {
		t.Errorf("the Action Items section's id is %q, want action-items", id)
	}
	return findAllByTestID(sections[0], "goal-action-item")
}

// A Goal's page lists its open Action Items from every Report, after its
// Milestones, soonest due first. Each row names its owner, when it's due and
// the Report it came from, linking to that publication's Action Items; an
// overdue one is flagged Overdue, and one due today isn't. A closed one isn't
// listed.
func TestGoalPageListsItsOpenActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newGoalActionItemFixture(h)
	f.raise(f.qbrPub, f.sam, "Book the venue.", 5, f.goal.ID)
	f.raise(f.mbrPub, f.sam, "Explain the slip.", -2, f.goal.ID)
	closed := f.raise(f.mbrPub, f.sam, "Already handled.", -5, f.goal.ID)
	if _, err := h.Service.CloseActionItem(context.Background(), f.sam.ID, closed.ID, "Done."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	client := signInClient(t, ts.URL, "author@example.com")

	page := getPage(t, client, goalPageURL(ts.URL, f.goal))
	rows := goalActionItemRowsIn(t, page)
	if len(rows) != 2 {
		t.Fatalf("Action Items rows = %d, want 2:\n%q", len(rows), textsOf(rows))
	}
	section := findByTestID(t, page, "goal-action-items")
	if heading := headingNamed(section, "Action Items"); heading == nil || heading.Data != "h2" {
		t.Errorf("the Action Items section has no Action Items heading")
	}
	if !precedes(findByTestID(t, page, "goal-milestones"), section) {
		t.Errorf("Action Items come before the Milestones")
	}
	overdue, later := rows[0], rows[1]
	for _, tc := range []struct {
		row         *nethtml.Node
		text, due   string
		report      domain.ReportDefinition
		publication domain.Publication
	}{
		{overdue, "Explain the slip.", "due Dec 31, 2025", f.mbr, f.mbrPub},
		{later, "Book the venue.", "due Jan 7, 2026", f.qbr, f.qbrPub},
	} {
		for _, want := range []string{tc.text, tc.due} {
			if !strings.Contains(textOf(tc.row), want) {
				t.Errorf("the %s row lacks %q:\n%s", tc.text, want, renderNode(tc.row))
			}
		}
		if !goalShows(tc.row, f.sam.Email, f.sam.Label()) {
			t.Errorf("the %s row doesn't show its owner %s:\n%s", tc.text, f.sam.Label(), renderNode(tc.row))
		}
		if from := linkTo(tc.row, fmt.Sprintf("/reports/%d/publications/%d#action-items", tc.report.ID, tc.publication.ID)); from == nil || textOf(from) != tc.report.Name {
			t.Errorf("the %s row doesn't link to %s's Action Items:\n%s", tc.text, tc.report.Name, renderNode(tc.row))
		}
	}
	if !hasTestID(overdue, "goal-action-item-overdue") {
		t.Errorf("an overdue Action Item isn't flagged Overdue:\n%s", renderNode(overdue))
	}
	if hasTestID(later, "goal-action-item-overdue") {
		t.Errorf("an Action Item due in five days is flagged Overdue:\n%s", renderNode(later))
	}
	if strings.Contains(strings.Join(textsOf(rows), " "), "Already handled.") {
		t.Errorf("a closed Action Item is listed")
	}

	f.raise(f.mbrPub, f.sam, "Call the vendor.", 0, f.goal.ID)
	rows = goalActionItemRowsIn(t, getPage(t, client, goalPageURL(ts.URL, f.goal)))
	if len(rows) != 3 || !strings.Contains(textOf(rows[1]), "Call the vendor.") {
		t.Fatalf("with one due today, rows = %q, want it second of 3", textsOf(rows))
	}
	if hasTestID(rows[1], "goal-action-item-overdue") {
		t.Errorf("an Action Item due today is flagged Overdue:\n%s", renderNode(rows[1]))
	}
}

// A Goal's page lists only the Action Items about it: not one about another
// Goal in the same Report, nor one about the whole Report. A Goal with no open
// Action Item has no Action Items section, even with a closed one.
func TestGoalPageListsOnlyItsOwnOpenActionItems(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newGoalActionItemFixture(h)
	other := h.ActiveGoal(f.author, "Hire in Berlin", "We need people there.")
	def := h.SaveReportDefinition(f.author, domain.SaveReportDefinitionInput{Name: "Ops review", Mode: domain.ReportModePicked, Picked: []int64{f.goal.ID, other.ID}})
	pub := h.PublishReport(f.author, def)
	f.raise(pub, f.sam, "Explain the slip.", 3, f.goal.ID)
	f.raise(pub, f.sam, "Post the job ad.", 3, other.ID)
	f.raise(pub, f.sam, "Send the minutes.", 3, 0)
	client := signInClient(t, ts.URL, "author@example.com")

	rows := goalActionItemRowsIn(t, getPage(t, client, goalPageURL(ts.URL, f.goal)))
	if len(rows) != 1 || !strings.Contains(textOf(rows[0]), "Explain the slip.") {
		t.Errorf("Launch in EU's Action Items = %q, want only Explain the slip.", textsOf(rows))
	}

	quiet := h.ActiveGoal(f.author, "Cut costs", "Margins are thin.")
	qdef := h.SaveReportDefinition(f.author, domain.SaveReportDefinitionInput{Name: "Cost review", Mode: domain.ReportModePicked, Picked: []int64{quiet.ID}})
	closed := f.raise(h.PublishReport(f.author, qdef), f.sam, "Audit the vendors.", 3, quiet.ID)
	if _, err := h.Service.CloseActionItem(context.Background(), f.sam.ID, closed.ID, "Done."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	if rows := goalActionItemRowsIn(t, getPage(t, client, goalPageURL(ts.URL, other))); len(rows) != 1 || !strings.Contains(textOf(rows[0]), "Post the job ad.") {
		t.Errorf("Hire in Berlin's Action Items = %q, want only Post the job ad.", textsOf(rows))
	}
	if page := getPage(t, client, goalPageURL(ts.URL, quiet)); hasTestID(page, "goal-action-items") {
		t.Errorf("a Goal with only a closed Action Item shows an Action Items section")
	}
}

// An Action Item's owner can close it from the Goal page, with a note, and
// comes back to the page's Action Items; anyone else reading the page sees it
// without a Close.
func TestGoalPageLetsAnActionItemsOwnerCloseIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	f := newGoalActionItemFixture(h)
	item := f.raise(f.mbrPub, f.sam, "Explain the slip.", 3, f.goal.ID)
	author := signInClient(t, ts.URL, "author@example.com")
	sam := noRedirects(signInClient(t, ts.URL, "sam.smith@example.com"))

	if rows := goalActionItemRowsIn(t, getPage(t, author, goalPageURL(ts.URL, f.goal))); len(rows) != 1 || hasTestID(rows[0], "close-action-item") {
		t.Errorf("someone who doesn't own the Action Item is offered its Close: %q", textsOf(rows))
	}
	rows := goalActionItemRowsIn(t, getPage(t, sam, goalPageURL(ts.URL, f.goal)))
	if len(rows) != 1 {
		t.Fatalf("Action Items rows = %d, want 1", len(rows))
	}
	back := fmt.Sprintf("/goals/%d#action-items", f.goal.ID)
	forms := goalFormsPostingTo(rows[0], fmt.Sprintf("/action-items/%d/close", item.ID))
	if len(forms) != 1 || !slices.ContainsFunc(formFields(forms[0], "return"), func(f pageField) bool { return f.Value == back }) {
		t.Fatalf("the owner's row has no Close that comes back to %s:\n%s", back, renderNode(rows[0]))
	}

	resp := postForm(t, sam, fmt.Sprintf("%s/action-items/%d/close", ts.URL, item.ID), url.Values{"note": {"Vendor was late."}, "return": {back}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != back {
		t.Errorf("close: status %d to %q, want 303 to %q", resp.StatusCode, resp.Header.Get("Location"), back)
	}
	if page := getPage(t, author, goalPageURL(ts.URL, f.goal)); hasTestID(page, "goal-action-items") {
		t.Errorf("the Goal page still lists the Action Item its owner closed")
	}
}

// A slip of the back-to-Green date isn't one of the delivery date: a Goal
// whose only slip moved its back-to-Green date strikes no delivery date, on its
// page or on the Goals list.
func TestABackToGreenSlipStrikesNoDeliveryDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Ship v2", "Customers wait too long.")
	h.Checkin(sam, goal.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate)
	h.CheckinMovingPath(sam, goal.ID, domain.HealthRed, "Blocked.", "Escalate.", pathDate.AddDate(0, 0, 7), "The vendor is out.")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, goalPageURL(ts.URL, goal))
	if delivery := findByTestID(t, page, "goal-delivery-date"); len(struckThrough(delivery)) > 0 {
		t.Errorf("the Goal page strikes a delivery date: %s", renderNode(delivery))
	}
	rows := goalListRowsIn(getPage(t, client, ts.URL+"/goals"))
	if len(rows) != 1 {
		t.Fatalf("Goals list has %d rows, want 1", len(rows))
	}
	if due := findByTestID(t, rows[0], "goal-row-due"); len(struckThrough(due)) > 0 {
		t.Errorf("the Goals list strikes a delivery date: %s", renderNode(due))
	}
}

// A back-to-Green date moved back earlier after slipping, which is no Date
// Slip, is never struck through beside itself: it strikes the most recent date
// it slipped from that differs, keeping the slip count, and with none that
// differs strikes nothing (CONTEXT.md: Date Slip).
func TestAPulledInBackToGreenDateStrikesTheDateItDiffersFrom(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")
	jun := func(d int) time.Time { return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC) }
	cell := func(goal domain.Goal) *nethtml.Node {
		t.Helper()
		cells := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, goal)), "goal-status-cells")
		return findByTestID(t, cells, "goal-back-to-green-cell")
	}

	twice := h.ActiveGoal(sam, "Slipped twice", "It matters.")
	h.Checkin(sam, twice.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(9))
	h.CheckinMovingPath(sam, twice.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(16), "The vendor is out.")
	h.CheckinMovingPath(sam, twice.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(23), "The vendor is still out.")
	h.Checkin(sam, twice.ID, domain.HealthYellow, "Better.", "Escalate.", jun(16))
	got := cell(twice)
	if !slices.Equal(goalPlainDates(got), []string{"Jun 16"}) || !slices.Equal(struckThrough(got), []string{"Jun 9"}) {
		t.Errorf("the cell doesn't read Jun 16 ~~Jun 9~~: %s", renderNode(got))
	}
	if slips := textsOf(findAllByTestID(got, "back-to-green-slips")); !slices.Equal(slips, []string{"(2)"}) {
		t.Errorf("the slip count = %q, want (2): %s", slips, renderNode(got))
	}

	once := h.ActiveGoal(sam, "Slipped once", "It matters.")
	h.Checkin(sam, once.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(9))
	h.CheckinMovingPath(sam, once.ID, domain.HealthRed, "Blocked.", "Escalate.", jun(16), "The vendor is out.")
	h.Checkin(sam, once.ID, domain.HealthYellow, "Better.", "Escalate.", jun(9))
	got = cell(once)
	if !slices.Equal(goalPlainDates(got), []string{"Jun 9"}) || len(struckThrough(got)) > 0 || hasTestID(got, "back-to-green-slips") {
		t.Errorf("a date back where it slipped from strikes a date or shows a count: %s", renderNode(got))
	}
}

// A delivery date moved back to a date it held before strikes only the dates
// that differ from it, on the Goal page and the Goals list: Nov 1 → Nov 9 →
// Nov 23 → Nov 9 reads ~~Nov 1~~ ~~Nov 23~~ Nov 9 (CONTEXT.md: Date Slip).
func TestAPulledBackDeliveryDateIsNotStruckThrough(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	nov := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	goal := h.ActiveGoalDue(sam, "Ship v2", nov(1))
	for _, due := range []time.Time{nov(9), nov(23), nov(9)} {
		h.Clock.Advance(time.Hour)
		if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
			GoalID:             goal.ID,
			AuthorID:           sam.ID,
			Health:             domain.HealthYellow,
			Status:             "Moving.",
			PathToGreen:        "Rescope.",
			PathTargetDate:     testsupport.Epoch.AddDate(0, 2, 0),
			DeliveryDate:       due,
			DeliveryDateReason: "Scope changed.",
		}); err != nil {
			t.Fatalf("SubmitCheckin: %v", err)
		}
	}
	client := signInClient(t, ts.URL, "sam@example.com")

	delivery := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, goal)), "goal-delivery-date")
	if got := struckThrough(delivery); !slices.Equal(got, []string{"Nov 1", "Nov 23"}) {
		t.Errorf("the Goal page strikes %q, want Nov 1 and Nov 23: %s", got, renderNode(delivery))
	}
	if !slices.Contains(goalEmphasisedDates(delivery), "Nov 9") {
		t.Errorf("the Goal page doesn't deliver Nov 9: %s", renderNode(delivery))
	}
	rows := goalListRowsIn(getPage(t, client, ts.URL+"/goals"))
	if len(rows) != 1 {
		t.Fatalf("Goals list has %d rows, want 1", len(rows))
	}
	due := findByTestID(t, rows[0], "goal-row-due")
	if got := struckThrough(due); !slices.Equal(got, []string{"Nov 1", "Nov 23"}) {
		t.Errorf("the Goals list strikes %q, want Nov 1 and Nov 23: %s", got, renderNode(due))
	}
	if !slices.Contains(goalPlainDates(due), "Nov 9") {
		t.Errorf("the Goals list isn't due Nov 9: %s", renderNode(due))
	}
}

// The Goal page lists the Goals a Goal contributes to and those contributing
// to it with each one's Health first, then its title, on one line: a chip with
// no word shown, read out and given as its tooltip; or, with no Health, its
// Lifecycle in words (#453). That the line lays them out side by side is
// TestGoalPageLinkedGoalsLeadWithTheirHealthStyles's.
func TestGoalPageLinkedGoalsLeadWithTheirHealth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	f := newCheckinChildrenFixture(h, sam)
	client := signInClient(t, ts.URL, "sam@example.com")

	children := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, f.parent)), "goal-children")
	for _, child := range []struct {
		goal   domain.Goal
		health string
	}{{f.yellow, "Yellow"}, {f.green, "Green"}} {
		title := linkTo(children, goalHref(child.goal.ID))
		if title == nil {
			t.Fatalf("Contributed to by has no link to %s:\n%s", child.goal.Title, renderNode(children))
		}
		item := title
		for item.Data != "li" {
			item = item.Parent
		}
		chip := healthChipIn(t, item)
		assertHealthChip(t, chip, child.health)
		if !precedes(chip, title) || chip.Parent != title.Parent {
			t.Errorf("%s's Health doesn't lead its title on one line:\n%s", child.goal.Title, renderNode(item))
		}
	}

	parents := findByTestID(t, getPage(t, client, goalPageURL(ts.URL, f.yellow)), "goal-parents")
	title := linkTo(parents, goalHref(f.parent.ID))
	lifecycle := findAll(parents, func(n *nethtml.Node) bool { return flatFrameHasClass(n, "lc") })
	if title == nil || len(lifecycle) != 1 || shownTextOf(lifecycle[0]) != "Active" || !precedes(lifecycle[0], title) || lifecycle[0].Parent != title.Parent {
		t.Errorf("a parent with no Health doesn't lead with its Lifecycle in words:\n%s", renderNode(parents))
	}
}

// Creating a Goal the domain refuses, with a blank title, answers 422 naming
// the reason, and creates no Goal.
func TestCreateGoalWithABlankTitleIsRefusedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, boss, ts.URL+"/goals", url.Values{
		"title":          {"   "},
		"why_it_matters": {"Shoppers abandon slow carts."},
	})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body:\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(textOf(parsePage(t, body)), "A Goal needs a title.") {
		t.Errorf("the refusal doesn't say a Goal needs a title; body:\n%s", body)
	}
	if goals, err := h.Service.ListGoals(context.Background()); err != nil || len(goals) != 0 {
		t.Errorf("ListGoals = %d Goals, %v after a refused create, want none", len(goals), err)
	}
}

// A Goal posted to /goals without a title is refused with the refusal page,
// its reason a sentence without the error's "validation failed:" prefix, and
// Back to the new Goal form, as no Goal exists yet (#573).
func TestCreatingAGoalWithoutATitleAnswersWithTheReason(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/goals", url.Values{"title": {""}, "why_it_matters": {"Some reason."}})
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	assertRefusalPage(t, parsePage(t, body), "Can't create Goal", "A Goal needs a title.", "/goals/new")
}

// A Goal command refused answers with the refusal page, headed Can't change
// Goal and linking Back to the Goal, at the status its refusal has: 422 for
// what was sent, 403 for who sent it (#573).
func TestARefusedGoalCommandAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	activateURL := goalPageURL(ts.URL, goal) + "/activate"

	for _, c := range []struct {
		who    string
		status int
		reason string
	}{
		{"sam@example.com", http.StatusUnprocessableEntity, "A Goal must be marked Dated or Ongoing to become Active."},
		{"pat@example.com", http.StatusForbidden, "Only the Goal's Owner may activate it."},
	} {
		resp := postForm(t, signInClient(t, ts.URL, c.who), activateURL, nil)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.who, resp.StatusCode, c.status)
		}
		assertRefusalPage(t, page, "Can't change Goal", c.reason, fmt.Sprintf("/goals/%d", goal.ID))
	}
}

// Marking a Goal Dated or Ongoing, setting its cadence and adding a Metric on
// their own have no form on any page, so their routes are gone (#577): the
// Goal page's define form does all four.
func TestGoalSetupRoutesNoPageOffersAreGone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")
	before := ownerOnlyGoalState(t, h, goal.ID)

	for suffix, form := range map[string]url.Values{
		"dated":   {"delivery_date": {"2026-06-15"}},
		"ongoing": nil,
		"cadence": {"cadence_days": {"14"}},
		"metrics": {
			"name": {"Error rate"}, "unit": {"%"}, "direction": {"down"},
			"baseline": {"5"}, "target": {"1"}, "target_date": {"2026-06-15"},
		},
	} {
		resp := postForm(t, client, goalPageURL(ts.URL, goal)+"/"+suffix, form)
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST /goals/%d/%s answered %d, want it gone", goal.ID, suffix, resp.StatusCode)
		}
	}
	if after := ownerOnlyGoalState(t, h, goal.ID); after != before {
		t.Errorf("a post to a gone route changed the Goal:\nbefore %s\nafter  %s", before, after)
	}
}

// Each Goal command authorizes before it validates, so what someone sent
// matters only once they may send it (#577): a stranger posting a malformed
// form is refused with 403, and the one permitted to post it is answered with
// 422 and the reason, beside the form or on the refusal page.
func TestGoalCommandsRefuseAStrangerBeforeWhatTheySent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	g, metricID := ownerOnlyGoal(t, h, sam)
	milestone, err := h.Service.AddMilestoneAsAuthor(t.Context(), sam.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta cut", TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	bare := h.CreateGoal(sam, "Bare goal", "Some reason.")
	ts := newServer(t, h)
	clients := map[string]*http.Client{
		"sam@example.com": signInClient(t, ts.URL, "sam@example.com"),
		"pat@example.com": signInClient(t, ts.URL, "pat@example.com"),
	}

	for _, c := range []struct {
		name, path string
		form       url.Values
		shownAt    string
		reason     string
	}{
		{"edit milestone", fmt.Sprintf("/milestones/%d", milestone.ID), url.Values{"name": {"Beta"}, "target_date": {"soon"}},
			"refused-reason", `"soon" isn't a date.`},
		{"edit metric", fmt.Sprintf("/metrics/%d", metricID), url.Values{
			"name": {"p99 checkout latency"}, "unit": {"ms"}, "direction": {"down"},
			"baseline": {"lots"}, "target": {"500"}, "target_date": {"2026-06-15"},
		}, "refused-reason", `"lots" isn't a number for the baseline.`},
		{"activate", fmt.Sprintf("/goals/%d/activate", bare.ID), nil,
			"refused-reason", "A Goal must be marked Dated or Ongoing to become Active."},
	} {
		before := ownerOnlyGoalState(t, h, g.ID) + ownerOnlyGoalState(t, h, bare.ID)

		resp := postForm(t, clients["pat@example.com"], ts.URL+c.path, c.form)
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s by a stranger: status %d, want 403", c.name, resp.StatusCode)
		}

		resp = postForm(t, clients["sam@example.com"], ts.URL+c.path, c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s by the Owner: status %d, want 422", c.name, resp.StatusCode)
		}
		if got := textOf(findByTestID(t, page, c.shownAt)); !strings.Contains(got, c.reason) {
			t.Errorf("%s by the Owner: %s reads %q, want %q", c.name, c.shownAt, got, c.reason)
		}
		if after := ownerOnlyGoalState(t, h, g.ID) + ownerOnlyGoalState(t, h, bare.ID); after != before {
			t.Errorf("%s changed a Goal:\nbefore %s\nafter  %s", c.name, before, after)
		}
	}
}

// The Contributor concept is gone (#593): the Owner's Goal page has no
// Contributors section, and its route answers as a gone one.
func TestGoalPageHasNoContributorsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("dana@example.com")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "sam@example.com")

	if page := getPage(t, client, goalPageURL(ts.URL, goal)); hasTestID(page, "goal-contributors") {
		t.Errorf("the Owner's Goal page has a Contributors section: %s", renderNode(page))
	}
	resp := postForm(t, client, goalPageURL(ts.URL, goal)+"/contributors", url.Values{"email": {"dana@example.com"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /goals/%d/contributors answered %d, want it gone", goal.ID, resp.StatusCode)
	}
}

// A refused Milestone or Metric edit links Back to the Goal it belongs to,
// not to the Goal whose id is the Milestone's or Metric's (#581): Goals made
// first take the low ids, so the edited records' ids name other Goals.
func TestRefusedMilestoneAndMetricEditsLinkBackToTheirGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	for _, title := range []string{"First goal", "Second goal", "Third goal"} {
		h.CreateGoal(sam, title, "Some reason.")
	}
	g, metricID := ownerOnlyGoal(t, h, sam)
	milestone, err := h.Service.AddMilestoneAsAuthor(t.Context(), sam.ID, domain.AddMilestoneInput{
		GoalID: g.ID, Name: "Beta cut", TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	if milestone.ID == g.ID || metricID == g.ID {
		t.Fatalf("Milestone %d and Metric %d must not share Goal %d's id", milestone.ID, metricID, g.ID)
	}
	ts := newServer(t, h)
	owner := signInClient(t, ts.URL, "sam@example.com")
	stranger := signInClient(t, ts.URL, "pat@example.com")

	for _, c := range []struct {
		name     string
		recordID int64
		// route is the edit's path, formatted with a record's id.
		route string
		form  url.Values
		// strangerReason is why a stranger is refused, ownerReason why the
		// Owner is refused what form sends.
		strangerReason, ownerReason string
	}{
		{"edit milestone", milestone.ID, "/milestones/%d",
			url.Values{"name": {"Beta"}, "target_date": {"soon"}},
			"Only the Owner or a Delegate may change this Goal's Milestones.", `"soon" isn't a date.`},
		{"edit metric", metricID, "/metrics/%d", url.Values{
			"name": {"p99 checkout latency"}, "unit": {"ms"}, "direction": {"down"},
			"baseline": {"lots"}, "target": {"500"}, "target_date": {"2026-06-15"},
		}, "Only the Goal's Owner may change its Metrics.", `"lots" isn't a number for the baseline.`},
	} {
		for _, by := range []struct {
			who    string
			client *http.Client
			status int
			reason string
		}{
			{"a stranger", stranger, http.StatusForbidden, c.strangerReason},
			{"the Owner", owner, http.StatusUnprocessableEntity, c.ownerReason},
		} {
			resp := postForm(t, by.client, ts.URL+fmt.Sprintf(c.route, c.recordID), c.form)
			defer func() { _ = resp.Body.Close() }()
			page := parsePage(t, readBody(t, resp))
			if resp.StatusCode != by.status {
				t.Errorf("%s by %s: status %d, want %d", c.name, by.who, resp.StatusCode, by.status)
			}
			assertRefusalPage(t, page, "Can't change Goal", by.reason, fmt.Sprintf("/goals/%d", g.ID))
			if link := linkTo(page, fmt.Sprintf("/goals/%d", c.recordID)); link != nil {
				t.Errorf("%s by %s links to /goals/%d, the Goal sharing its id:\n%s", c.name, by.who, c.recordID, renderNode(page))
			}
		}

		// One there isn't is still the Not found page, not a refusal.
		resp := postForm(t, owner, ts.URL+fmt.Sprintf(c.route, 999), c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusNotFound || !hasTestID(page, "not-found") {
			t.Errorf("%s on a missing id: status %d, want the Not found page:\n%s", c.name, resp.StatusCode, renderNode(page))
		}
	}
}
