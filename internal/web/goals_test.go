package web_test

import (
	"context"
	"fmt"
	"html"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after sign-in: status %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Signed in as "+shownAs("boss@example.com", "boss")) {
		t.Errorf("goals page does not show the signed-in user; body:\n%s", body)
	}

	// Create a Proposed Goal.
	resp = postForm(t, client, ts.URL+"/goals", url.Values{
		"title":   {"Cut checkout latency"},
		"so_what": {"Shoppers abandon slow carts."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after create goal: status %d", resp.StatusCode)
	}
	listBody := readBody(t, resp)
	if !strings.Contains(listBody, "Cut checkout latency") {
		t.Errorf("Goal list does not show the new Goal; body:\n%s", listBody)
	}

	// Find the Goal's own page and confirm it renders the full Goal.
	goalPath := findGoalLink(t, listBody)
	resp, err = client.Get(ts.URL + goalPath)
	if err != nil {
		t.Fatalf("GET %s: %v", goalPath, err)
	}
	goalBody := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("goal page status %d", resp.StatusCode)
	}
	for _, want := range []string{"Cut checkout latency", "Shoppers abandon slow carts.", "Proposed", "boss@example.com"} {
		if !strings.Contains(goalBody, want) {
			t.Errorf("goal page missing %q; body:\n%s", want, goalBody)
		}
	}
}

// findGoalLink pulls the first /goals/<id> href out of the rendered list.
func findGoalLink(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`href="(/goals/\d+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no goal link in body:\n%s", body)
	}
	return m[1]
}

// TestSmokeDefineAndActivateGoal is the define-and-activate HTTP smoke test: an
// Owner marks a Proposed Goal Dated, adds a Milestone and a Metric, sets the
// cadence, revises the So What, adds a Contributor, and activates it — then sees
// it rendered Active with everything it filled in.
func TestSmokeDefineAndActivateGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	// The Contributor's account must already exist to be added by email.
	h.SignIn("dana@example.com")

	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	postForm(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})

	resp := postForm(t, client, ts.URL+"/goals", url.Values{
		"title":   {"Cut checkout latency"},
		"so_what": {"Shoppers abandon slow carts."},
	})
	goalPath := findGoalLink(t, readBody(t, resp))
	goalURL := ts.URL + goalPath

	postForm(t, client, goalURL+"/dated", url.Values{"delivery_date": {"2026-06-15"}})
	postForm(t, client, goalURL+"/milestones", url.Values{
		"name":        {"Beta cut"},
		"target_date": {"2026-03-16"},
	})
	postForm(t, client, goalURL+"/metrics", url.Values{
		"name":        {"p95 checkout latency"},
		"unit":        {"ms"},
		"direction":   {"down"},
		"baseline":    {"1200"},
		"target":      {"400"},
		"target_date": {"2026-06-15"},
	})
	postForm(t, client, goalURL+"/cadence", url.Values{"cadence_days": {"14"}})
	postForm(t, client, goalURL+"/so-what", url.Values{"so_what": {"Faster checkout lifts conversion."}})
	postForm(t, client, goalURL+"/contributors", url.Values{"email": {"dana@example.com"}})

	resp = postForm(t, client, goalURL+"/activate", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("after activate: status %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{
		"Active",
		"Beta cut",
		"p95 checkout latency",
		"every 14 days",
		"Faster checkout lifts conversion.",
		"Shoppers abandon slow carts.", // the original So What, kept as a revision
		"dana@example.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("activated goal page missing %q; body:\n%s", want, body)
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
	postForm(t, client, ts.URL+"/signin", url.Values{"email": {"boss@example.com"}})

	resp := postForm(t, client, ts.URL+"/goals", url.Values{
		"title":   {"Bare goal"},
		"so_what": {"Some reason."},
	})
	goalPath := findGoalLink(t, readBody(t, resp))

	resp = postForm(t, client, ts.URL+goalPath+"/activate", nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("activate status = %d, want 422", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Dated") {
		t.Errorf("rejection should explain the missing Dated/Ongoing choice; body:\n%s", body)
	}
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
	postForm(t, samClient, goalURL+"/dimensions", url.Values{"value_id": {fmt.Sprintf("%d", pillar.Values[0].ID)}})
	page := getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-dimension-value">Growth`) {
		t.Fatalf("Goal page missing assigned Growth; body:\n%s", page)
	}
	postForm(t, samClient, goalURL+"/dimensions", url.Values{"value_id": {fmt.Sprintf("%d", pillar.Values[1].ID)}})
	page = getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-dimension-value">Reliability`) {
		t.Errorf("Goal page should show Reliability after reassignment; body:\n%s", page)
	}
	if strings.Contains(page, `data-testid="goal-dimension-value">Growth`) {
		t.Errorf("Growth should be replaced, not kept; body:\n%s", page)
	}

	// Admin retires Reliability; it stays readable on the Goal that carries it.
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, pillar.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	page = getBody(t, samClient, goalURL)
	if !strings.Contains(page, `data-testid="goal-dimension-value">Reliability`) {
		t.Errorf("retired value should stay readable on the Goal; body:\n%s", page)
	}
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

	edit := openForm(t, getBody(t, samClient, goalURL+"?open=dimensions"), "dimensions")
	for _, v := range []domain.DimensionValue{core, infra} {
		if !strings.Contains(edit, fmt.Sprintf(`type="checkbox" name="value_id" value="%d"`, v.ID)) {
			t.Errorf("no checkbox for %s:\n%s", v.Value, edit)
		}
	}
	if strings.Contains(edit, fmt.Sprintf(`value="%d"`, legacy.ID)) {
		t.Errorf("retired Legacy is offered:\n%s", edit)
	}
	if !strings.Contains(edit, `<select name="value_id" aria-label="Pillar"`) {
		t.Errorf("one-value Pillar lost its select:\n%s", edit)
	}

	dimID := fmt.Sprintf("%d", teams.ID)
	postForm(t, samClient, goalURL+"/dimensions", url.Values{
		"dimension_id": {dimID},
		"value_id":     {fmt.Sprintf("%d", core.ID), fmt.Sprintf("%d", infra.ID)},
	})
	section := pageElement(t, getBody(t, samClient, goalURL), "section", "goal-dimensions")
	for _, want := range []string{`data-testid="goal-dimension-value">Core`, `data-testid="goal-dimension-value">Infra`} {
		if !strings.Contains(section, want) {
			t.Errorf("Goal page missing %s after saving two values:\n%s", want, section)
		}
	}
	if edit := openForm(t, getBody(t, samClient, goalURL+"?open=dimensions"), "dimensions"); !strings.Contains(edit, fmt.Sprintf(`value="%d" checked`, core.ID)) {
		t.Errorf("Core's checkbox isn't checked:\n%s", edit)
	}

	postForm(t, samClient, goalURL+"/dimensions", url.Values{
		"dimension_id": {dimID},
		"value_id":     {fmt.Sprintf("%d", infra.ID)},
	})
	section = pageElement(t, getBody(t, samClient, goalURL), "section", "goal-dimensions")
	if strings.Contains(section, `data-testid="goal-dimension-value">Core`) {
		t.Errorf("Core still shown after it was unchecked:\n%s", section)
	}
	if !strings.Contains(section, `data-testid="goal-dimension-value">Infra`) {
		t.Errorf("Infra lost when Core was removed:\n%s", section)
	}

	postForm(t, samClient, goalURL+"/dimensions", url.Values{"dimension_id": {dimID}})
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

	empty := pageElement(t, getBody(t, otherClient, goalURL), "section", "goal-dimensions")
	if !strings.Contains(empty, "No Dimensions defined yet.") {
		t.Fatalf("Dimensions section isn't empty:\n%s", empty)
	}
	if !strings.Contains(empty, `href="/dimensions"`) {
		t.Errorf("empty Dimensions section has no link to /dimensions:\n%s", empty)
	}

	h.CreateDimension(boss, "Quarter", "Q3", "Q4")
	defined := pageElement(t, getBody(t, otherClient, goalURL), "section", "goal-dimensions")
	if !strings.Contains(defined, "Quarter") {
		t.Fatalf("Dimensions section doesn't list Quarter:\n%s", defined)
	}
	if !strings.Contains(defined, `href="/dimensions"`) {
		t.Errorf("Dimensions section has no link to /dimensions:\n%s", defined)
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

	grouped := getBody(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, teams.ID))
	groups := strings.Split(grouped, `<tbody data-testid="goal-group"`)[1:]
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want Core, Infra and Unassigned; body:\n%s", len(groups), grouped)
	}
	for i, label := range []string{"Core", "Infra"} {
		group := groups[i]
		if !strings.Contains(group, label) || !strings.Contains(group, ">Alpha<") {
			t.Errorf("group %d should be %s holding Alpha:\n%s", i, label, group)
		}
	}
	if got := pageElement(t, grouped, "p", "goal-count"); !strings.Contains(got, ">2 Goals") {
		t.Errorf("total = %q, want 2 Goals", got)
	}

	flat := getBody(t, client, ts.URL+"/goals")
	if got := pageElement(t, flat, "p", "goal-count"); !strings.Contains(got, ">2 Goals") {
		t.Errorf("flat total = %q, want 2 Goals", got)
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
	filtered := getBody(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, growth.ID))
	if !strings.Contains(filtered, "Alpha") || strings.Contains(filtered, ">Bravo<") {
		t.Errorf("filter by Growth should show only Alpha; body:\n%s", filtered)
	}

	// Group by Pillar: value-labelled groups appear.
	grouped := getBody(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if !strings.Contains(grouped, `data-testid="goal-group"`) {
		t.Fatalf("grouped list missing groups; body:\n%s", grouped)
	}
	if !strings.Contains(grouped, "Growth") || !strings.Contains(grouped, "Reliability") {
		t.Errorf("grouped list missing value labels; body:\n%s", grouped)
	}
}

// A grouped Goal list's group headers sit on the neutral alternate surface, not
// a teal fill: a header row is nothing to act on, and teal is kept for what is
// (DESIGN.md § System Mood).
func TestGoalListGroupHeadersAreNeutralOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.AssignGoalValue(h.CreateGoal(sam, "Alpha", "A matters."), pillar.Values[0])
	ts := newServer(t, h)

	grouped := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if !strings.Contains(grouped, `class="gl-group"`) {
		t.Fatalf("grouped list has no group header row; body:\n%s", grouped)
	}
	if rule := cssRule(t, grouped, ".gl-group th"); !strings.Contains(rule, "background:var(--color-surface-alt)") {
		t.Errorf("group header isn't on --color-surface-alt; rule: %s", rule)
	}
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
	page := getBody(t, samClient, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID))
	if !chosen(t, fitsSection(t, page), growth) {
		t.Errorf("parent value not offered as a chosen default; body:\n%s", page)
	}

	// Keeping the default assigns Growth to the child and links it to the parent.
	resp := postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":     {"Kept child"},
		"so_what":   {"Child matters."},
		"parent_id": {fmt.Sprint(parent.ID)},
		"value_id":  {fmt.Sprintf("%d", growth.ID)},
	})
	childPage := readBody(t, resp)
	if !strings.Contains(childPage, `data-testid="goal-dimension-value">Growth`) {
		t.Errorf("kept default not assigned to the child; body:\n%s", childPage)
	}
	if !strings.Contains(childPage, "Parent") {
		t.Errorf("child does not contribute to the parent; body:\n%s", childPage)
	}

	// Creating a child without the default leaves it unassigned (not inherited).
	resp = postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":     {"Bare child"},
		"so_what":   {"Child matters."},
		"parent_id": {fmt.Sprint(parent.ID)},
	})
	barePage := readBody(t, resp)
	if !strings.Contains(barePage, `data-testid="goal-dimension-unassigned"`) {
		t.Errorf("child without the default should be unassigned; body:\n%s", barePage)
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
	if page := getBody(t, samClient, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID)); !chosen(t, fitsSection(t, page), growth) {
		t.Fatalf("the New goal form does not offer Growth as a default; body:\n%s", page)
	}
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, growth.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	resp := postForm(t, samClient, ts.URL+"/goals/new", url.Values{
		"title":     {"GrandKid"},
		"so_what":   {"Feeds the parent."},
		"parent_id": {fmt.Sprint(parent.ID)},
		"value_id":  {fmt.Sprintf("%d", core.ID), fmt.Sprintf("%d", growth.ID)},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body:\n%s", resp.StatusCode, body)
	}
	form := pageElement(t, body, "form", "goal-form")
	if !strings.Contains(form, "retired") {
		t.Errorf("the form does not say the value is retired; form:\n%s", form)
	}
	if !strings.Contains(form, `value="GrandKid"`) || !strings.Contains(form, "Feeds the parent.") {
		t.Errorf("the form lost the title or So What; form:\n%s", form)
	}
	if !chosen(t, fitsSection(t, body), core) {
		t.Errorf("the form lost the kept Core default; form:\n%s", form)
	}
	tagAround(t, form, fmt.Sprintf(`id="parent:%d"`, parent.ID)) // the parent is still picked

	if list := getBody(t, samClient, ts.URL+"/goals"); strings.Contains(list, "GrandKid") {
		t.Errorf("a failed child create left a Goal behind; list:\n%s", list)
	}
	if children := h.ChildrenOf(parent); len(children) != 0 {
		t.Errorf("a failed child create requested a link: %v", children)
	}
}

// goalRows returns the Goal list's rows in the order they render, each from its
// opening <tr> to its closing </tr>.
func goalRows(t *testing.T, page string) []string {
	t.Helper()
	const marker = `<tr data-testid="goal-row"`
	var rows []string
	for {
		start := strings.Index(page, marker)
		if start < 0 {
			return rows
		}
		end := strings.Index(page[start:], "</tr>")
		if end < 0 {
			t.Fatalf("unterminated goal row:\n%s", page[start:])
		}
		rows = append(rows, page[start:start+end+len("</tr>")])
		page = page[start+end:]
	}
}

// pageTag returns the opening tag of the tag element carrying data-testid, for
// void elements such as <input> that pageElement can't close.
func pageTag(t *testing.T, page, tag, testID string) string {
	t.Helper()
	start := strings.Index(page, "<"+tag+` data-testid="`+testID+`"`)
	if start < 0 {
		t.Fatalf("page has no <%s> %q", tag, testID)
	}
	return openTag(page[start:])
}

// rowTitles names the Goals in rows, in order, by matching each row against the
// Goals it could be.
func rowTitles(rows []string, goals ...domain.Goal) []string {
	titles := make([]string, 0, len(rows))
	for _, r := range rows {
		for _, g := range goals {
			if strings.Contains(r, navTo(g.ID)) {
				titles = append(titles, g.Title)
			}
		}
	}
	return titles
}

// The Goal list is a table led by each Goal's Health, and problems sort to the
// top: Red, then Ownerless, then Stale or Path to Green overdue, then Yellow,
// then Green, then Goals with no Health, alphabetical within each group.
func TestGoalListSortsProblemsFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	later := testsupport.Epoch.AddDate(0, 2, 0)

	stale := h.ActiveGoal(sam, "Stale work", "It matters.")
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

	list := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	rows := goalRows(t, list)

	got := rowTitles(rows, stale, proposed, betaGreen, alphaGreen, yellow, ownerless, red)
	want := []string{"Zulu fire", "Orphaned", "Stale work", "Wobbly", "Alpha green", "Beta green", "Proposed idea"}
	if !slices.Equal(got, want) {
		t.Fatalf("row order = %q, want %q", got, want)
	}
	if !strings.Contains(list, "<th>Health</th>") {
		t.Errorf("Goal list has no Health column; body:\n%s", list)
	}
	for i, class := range []string{"r", "g", "lc", "y", "g", "g", "lc"} {
		health := pageElement(t, rows[i], "span", "goal-row-health")
		if !strings.Contains(openTag(health), `class="badge `+class+`"`) {
			t.Errorf("%s: Health badge is not .%s: %s", want[i], class, health)
		}
	}
	if health := pageElement(t, rows[6], "span", "goal-row-health"); !strings.Contains(health, "Proposed") {
		t.Errorf("a Goal with no Health shows no Lifecycle: %s", health)
	}
}

// Each row shows when the Goal is due, with the dates its Date Slips moved it
// from struck through (wrapping between dates, never mid-date: #180), or a dash
// for an Ongoing Goal, and how long ago its last Check-in was.
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
	if _, err := h.Service.MarkGoalOngoing(t.Context(), ongoing.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	h.Clock.Advance(3 * day)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals")
	rows := goalRows(t, page)
	if got := rowTitles(rows, slipped, ongoing); !slices.Equal(got, []string{"Slipped", "Keep the lights on"}) {
		t.Fatalf("rows = %q", got)
	}

	if due := strings.Join(strings.Fields(pageElement(t, rows[0], "td", "goal-row-due")), " "); !strings.Contains(due, `<td data-testid="goal-row-due" class="num gl-dates"><del>2026-07-02</del> <span>2026-07-16</span>`) {
		t.Errorf("slipped Goal's Due does not strike its prior date, each date whole: %s", due)
	}
	if rule := cssRule(t, page, ".gl-dates>*"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("a Due date can break mid-date: .gl-dates>*{%s}", rule)
	}
	if rule, ok := ruleFor(page, ".gl-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole Due cell is kept on one line, so its slips can't wrap: .gl-dates{%s}", rule)
	}
	if last := pageElement(t, rows[0], "td", "goal-row-last-checkin"); !strings.Contains(last, "3 days ago") {
		t.Errorf("Last check-in is not relative: %s", last)
	}
	if due := pageElement(t, rows[1], "td", "goal-row-due"); !strings.Contains(due, "—") || strings.Contains(due, "20") {
		t.Errorf("Ongoing Goal's Due is not a dash: %s", due)
	}
	if last := pageElement(t, rows[1], "td", "goal-row-last-checkin"); !strings.Contains(last, "—") {
		t.Errorf("a Goal with no Check-in shows a Last check-in: %s", last)
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
		page := getBody(t, client, ts.URL+"/goals?q="+url.QueryEscape(tc.q))
		if got := rowTitles(goalRows(t, page), latency, hiring); !slices.Equal(got, tc.want) {
			t.Errorf("q=%q: rows = %q, want %q", tc.q, got, tc.want)
		}
		if search := pageTag(t, page, "input", "goal-search"); !strings.Contains(search, `value="`+tc.q+`"`) {
			t.Errorf("q=%q: search box lost the query: %s", tc.q, search)
		}
		if tc.want == nil && !strings.Contains(page, `data-testid="no-goals"`) {
			t.Errorf("q=%q: no empty state", tc.q)
		}
	}
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
		page := getBody(t, client, ts.URL+"/goals?health="+health)
		if got := rowTitles(goalRows(t, page), green, yellow, red, proposed); !slices.Equal(got, []string{want}) {
			t.Errorf("health=%s: rows = %q, want [%q]", health, got, want)
		}
		if sel := pageElement(t, page, "select", "goal-health-filter"); !strings.Contains(sel, `value="`+health+`" selected`) {
			t.Errorf("health=%s: select lost the choice: %s", health, sel)
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

	all := getBody(t, client, ts.URL+"/goals")
	if got := rowTitles(goalRows(t, all), active, paused, proposed); len(got) != 3 {
		t.Errorf("default Lifecycle filter should keep every Goal; rows = %q", got)
	}
	for lifecycle, want := range map[string]string{
		domain.LifecycleActive:   "Active one",
		domain.LifecycleOnHold:   "Paused one",
		domain.LifecycleProposed: "Proposed one",
	} {
		page := getBody(t, client, ts.URL+"/goals?lifecycle="+url.QueryEscape(lifecycle))
		if got := rowTitles(goalRows(t, page), active, paused, proposed); !slices.Equal(got, []string{want}) {
			t.Errorf("lifecycle=%s: rows = %q, want [%q]", lifecycle, got, want)
		}
		if sel := pageElement(t, page, "select", "goal-lifecycle-filter"); !strings.Contains(sel, `value="`+lifecycle+`" selected`) {
			t.Errorf("lifecycle=%s: select lost the choice: %s", lifecycle, sel)
		}
	}
}

// Mine only keeps the Goals the viewer Owns or is a Delegate on, and its
// checkbox stays checked.
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

	page := getBody(t, client, ts.URL+"/goals?mine=1")
	got := rowTitles(goalRows(t, page), owned, delegated, others)
	if want := []string{"Kim's, delegated to Sam", "Sam's own"}; !slices.Equal(got, want) {
		t.Errorf("mine=1: rows = %q, want %q", got, want)
	}
	if box := pageTag(t, page, "input", "goal-mine-filter"); !strings.Contains(box, "checked") {
		t.Errorf("Mine only checkbox lost its check: %s", box)
	}

	page = getBody(t, client, ts.URL+"/goals")
	if got := rowTitles(goalRows(t, page), owned, delegated, others); len(got) != 3 {
		t.Errorf("without Mine only every Goal shows; rows = %q", got)
	}
	if box := pageTag(t, page, "input", "goal-mine-filter"); strings.Contains(box, "checked") {
		t.Errorf("Mine only is checked without ?mine=1: %s", box)
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

	plain := getBody(t, client, ts.URL+"/goals")
	if more := pageTag(t, plain, "details", "more-filters"); strings.Contains(more, "open") {
		t.Errorf("More filters is open with none set: %s", more)
	}

	filtered := getBody(t, client, fmt.Sprintf("%s/goals?value=%d", ts.URL, growth.ID))
	if more := pageTag(t, filtered, "details", "more-filters"); !strings.Contains(more, "open") {
		t.Errorf("More filters is closed with a value chosen: %s", more)
	}

	grouped := getBody(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if more := pageTag(t, grouped, "details", "more-filters"); !strings.Contains(more, "open") {
		t.Errorf("More filters is closed while grouping: %s", more)
	}
	section := pageElement(t, grouped, "tbody", "goal-group")
	if label := pageElement(t, section, "th", "goal-group-label"); !strings.Contains(label, "Growth") {
		t.Errorf("first section is not headed Growth: %s", label)
	}
	if got := rowTitles(goalRows(t, section), calm, fire, loose); !slices.Equal(got, []string{"Burning growth", "Calm growth"}) {
		t.Errorf("Growth section rows = %q, want the Red Goal first", got)
	}
	if !strings.Contains(grouped, "Unassigned") {
		t.Errorf("Goals without a Pillar have no Unassigned section; body:\n%s", grouped)
	}

	empty := getBody(t, client, ts.URL+"/goals?q=nothing+like+it")
	if none := pageElement(t, empty, "td", "no-goals"); !strings.Contains(none, "No Goals match.") {
		t.Errorf("a filtered-out list does not say nothing matches: %s", none)
	}
}

// The Goal list's header names the page and offers New goal, a primary
// button linking to the New goal page. The Risks page has replaced the links to
// the signal pages.
func TestGoalListHeaderOpensProposeFormThatSwapsTheList(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals")
	head := pageElement(t, page, "header", "goals-head")
	for _, want := range []string{"<h1>Goals</h1>", "Every goal in the org. Problems sort to the top."} {
		if !strings.Contains(head, want) {
			t.Errorf("header lacks %q: %s", want, head)
		}
	}
	newGoal := pageElement(t, head, "a", "new-goal")
	if tag := openTag(newGoal); attr(tag, "href") != "/goals/new" || attr(tag, "class") != "btn primary" || !strings.Contains(newGoal, "New goal") {
		t.Errorf("New goal is not a primary button linking to /goals/new: %s", newGoal)
	}
	for _, gone := range []string{`href="/signals"`, `href="/freshness"`} {
		if strings.Contains(page, gone) {
			t.Errorf("Goal list still links %s", gone)
		}
	}
}

// The Goal page opens on its title, then its So What as a lead paragraph, then
// one metadata line a reader takes in at a glance: the Health badge, then plain
// Lifecycle, Kind, Owner, the delivery date with its slips struck (wrapping
// between dates, never mid-date: #180), the cadence and Top-level. Dimension values stay in the sidebar, out of the head. Check
// in and No change sit top right for whoever may check in.
func TestGoalPageHeaderSummarizesTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	growth := h.CreateDimension(ada, "Pillar", "Growth").Values[0]
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, growth)
	h.MarkTopLevel(ada, goal)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	head := pageElement(t, page, "header", "goal-head")
	title := strings.Index(head, `data-testid="goal-title"`)
	soWhat := strings.Index(head, `data-testid="goal-so-what"`)
	metaAt := strings.Index(head, `data-testid="goal-meta"`)
	if title < 0 || soWhat < title || metaAt < soWhat {
		t.Errorf("head should read title, So What, metadata line in that order; at %d, %d, %d", title, soWhat, metaAt)
	}
	if title := pageElement(t, head, "h1", "goal-title"); !strings.Contains(title, "Reduce outages") {
		t.Errorf("header title: %s", title)
	}
	if lead := pageElement(t, head, "p", "goal-so-what"); !strings.Contains(lead, "Outages cost trust.") {
		t.Errorf("header So What: %s", lead)
	}
	meta := strings.Join(strings.Fields(pageElement(t, head, "p", "goal-meta")), " ")
	if !strings.HasPrefix(meta, `<p data-testid="goal-meta" class="gp-meta"><span class="badge y"><span class="dot"></span>Yellow</span>`) {
		t.Errorf("metadata line should lead with the Health badge: %s", meta)
	}
	for _, want := range []string{
		`<span data-testid="goal-lifecycle">Active</span>`,
		`<span data-testid="goal-kind">Dated</span>`,
		`Owner <strong data-testid="goal-owner">` + shownAs("sam@example.com", "sam") + `</strong>`,
		`Delivers <span data-testid="goal-delivery-date" class="gp-dates"><del>2026-07-02</del> <strong>2026-07-16</strong></span>`,
		`Checks in <span data-testid="goal-cadence">every 7 days</span>`,
		`data-testid="goal-top-level"`,
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("metadata line lacks %s: %s", want, meta)
		}
	}
	if rule := cssRule(t, page, ".gp-dates>*"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("the delivery date can break mid-date: .gp-dates>*{%s}", rule)
	}
	if strings.Count(meta, `class="badge`) != 1 {
		t.Errorf("only Health is a badge on the metadata line; the rest is plain text: %s", meta)
	}
	if strings.Contains(head, "Growth") {
		t.Errorf("the head shows a Dimension value; those stay in the sidebar: %s", head)
	}
	if !strings.Contains(pageElement(t, page, "section", "goal-dimensions"), "Growth") {
		t.Errorf("the sidebar lost the Goal's Dimension value")
	}
	actions := between(t, head, `data-testid="goal-actions"`, `data-testid="goal-more"`)
	if !strings.Contains(actions, fmt.Sprintf(`href="/goals/%d/checkin"`, goal.ID)) || !strings.Contains(actions, `class="btn primary"`) {
		t.Errorf("header has no primary Check in link: %s", actions)
	}
	if !strings.Contains(actions, `data-testid="no-change-checkin"`) {
		t.Errorf("header has no No change button: %s", actions)
	}
	if strings.Count(page, `data-testid="checkin-link"`) != 1 {
		t.Errorf("the Check in link should appear once, in the header")
	}
}

// A Goal that has no Health, no delivery date and isn't Top-level says only
// what it has on its metadata line: no Health badge, no Delivers, and no
// Top-level.
func TestGoalPageMetaLineShowsOnlyWhatTheGoalHas(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Tidy the backlog", "Nobody can find anything.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	meta := strings.Join(strings.Fields(pageElement(t, pageElement(t, page, "header", "goal-head"), "p", "goal-meta")), " ")
	for _, want := range []string{
		`<span data-testid="goal-lifecycle">Proposed</span>`,
		`<span data-testid="goal-kind">Not yet Dated or Ongoing</span>`,
		`data-testid="goal-owner"`,
		`data-testid="goal-cadence"`,
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("metadata line lacks %s: %s", want, meta)
		}
	}
	for _, absent := range []string{`class="badge`, "Delivers", `data-testid="goal-delivery-date"`, "Top-level"} {
		if strings.Contains(meta, absent) {
			t.Errorf("metadata line shows %s for a Proposed, undated, non-Top-level Goal: %s", absent, meta)
		}
	}
}

// At phone width the Goal page header wraps its actions onto their own line
// rather than squeezing the title and meta into a column a few letters wide:
// the summary claims a real width before the row's leftover space is shared,
// so it no longer fits beside Check in, No change and More (#42).
func TestGoalPageHeaderWrapsActionsAtPhoneWidthOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "A to-ce the API tier", "Callers need one front door.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	pageElement(t, page, "header", "goal-head")
	if rule := cssRule(t, page, ".gp-summary"); !strings.Contains(rule, "flex:1 1 320px") {
		t.Errorf("header summary has no flex-basis, so it shrinks beside the actions instead of wrapping them; rule: %s", rule)
	}
	// Wrapped, the actions stay flush right: the More menu opens leftward from
	// its button's right edge, and a left-aligned button would push it off-screen.
	if rule := cssRule(t, page, ".gp-actions"); !strings.Contains(rule, "margin-left:auto") {
		t.Errorf("header actions fall to the left when wrapped, so the More menu opens off the left edge; rule: %s", rule)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	section := strings.Join(strings.Fields(pageElement(t, page, "section", "goal-milestones")), " ")
	for _, want := range []string{
		`<span data-testid="goal-slip-count">0</span> slips`,
		`<span data-testid="goal-milestone-churn">0</span> added or removed since Active`,
		"<table>",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("Milestones lack %s: %s", want, section)
		}
	}
	if got := elementTexts(section, "tr", "goal-milestone"); !slices.Equal(got, []string{"2026-04-02 Beta"}) {
		t.Errorf("Milestone rows read %q, want Beta's date and name with no mark", got)
	}
	if strings.Contains(section, `data-testid="milestone-mark"`) {
		t.Errorf("an on-track Milestone carries a mark: %s", section)
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

	for _, tc := range []struct {
		who  string
		goal domain.Goal
	}{
		{"sam@example.com", proposed},
		{"sam@example.com", active},
		{"dana@example.com", active},
		{"dana@example.com", onHold},
	} {
		client := signInClient(t, ts.URL, tc.who)
		card := pageElement(t, getBody(t, client, goalPageURL(ts.URL, tc.goal)), "section", "goal-milestones")
		link := tagAround(t, card, `data-testid="open-milestones"`)
		page := getBody(t, client, ts.URL+html.UnescapeString(attr(link, "href")))
		open := openForm(t, page, "milestones")
		assertOpenIn(t, page, "milestones", `data-testid="goal-milestones"`, `data-testid="goal-highlights"`)
		for _, want := range []string{fmt.Sprintf(`action="/goals/%d/milestones"`, tc.goal.ID), `name="name"`, `type="date" name="target_date"`} {
			if !strings.Contains(open, want) {
				t.Errorf("%s on %s: the Add Milestone form lacks %s: %s", tc.who, tc.goal.Lifecycle, want, open)
			}
		}

		name := "Added by " + tc.who
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, tc.goal.ID), url.Values{"name": {name}, "target_date": {"2026-05-01"}})
		page = readBody(t, resp)
		if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", tc.goal.ID) {
			t.Fatalf("%s on %s: add answered %d at %s, want the Goal page", tc.who, tc.goal.Lifecycle, resp.StatusCode, resp.Request.URL)
		}
		rows := elementTexts(pageElement(t, page, "section", "goal-milestones"), "tr", "goal-milestone")
		if !slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "2026-05-01 "+name) }) {
			t.Errorf("%s on %s: Milestones read %q, want %s listed", tc.who, tc.goal.Lifecycle, rows, name)
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
	if _, err := h.Service.AddMilestone(t.Context(), domain.AddMilestoneInput{
		GoalID: proposed.ID, Name: "Pricing page", TargetDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
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

	for _, who := range []string{"pat@example.com", "ada@example.com"} {
		client := signInClient(t, ts.URL, who)
		for _, g := range []domain.Goal{proposed, active} {
			before := rows(g)
			resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, g.ID), url.Values{"name": {"Sneaky"}, "target_date": {"2026-05-01"}})
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusForbidden || !strings.Contains(html.UnescapeString(body), "only the Owner or a Delegate may change this Goal's Milestones") {
				t.Errorf("%s adding to %s: %d %q, want 403 with the reason", who, g.Lifecycle, resp.StatusCode, body)
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
				if page := getBody(t, client, u); strings.Contains(page, `data-testid="open-milestones"`) || strings.Contains(page, `data-testid="add-milestone"`) {
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
			if page := getBody(t, sam2, u); strings.Contains(page, `data-testid="open-milestones"`) || strings.Contains(page, `data-testid="add-milestone"`) {
				t.Errorf("the Owner of a %s Goal is offered the Add Milestone form at %s", lifecycle, u)
			}
		}
		before := rows(g)
		resp := postForm(t, sam2, fmt.Sprintf("%s/goals/%d/milestones", ts.URL, g.ID), url.Values{"name": {"Late"}, "target_date": {"2026-05-01"}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("adding to a %s Goal answered %d, want 422", lifecycle, resp.StatusCode)
		}
		if after := rows(g); !slices.Equal(after, before) {
			t.Errorf("a %s Goal took a Milestone: %q", lifecycle, after)
		}
	}
}

// The Owner's Edit on a Proposed Goal's Milestone still changes its name and
// date, and a Delegate may rename an Active Goal's Milestone; its date still
// moves only in a Check-in (CONTEXT.md: Date Slip).
func TestOwnerOrDelegateEditsAMilestoneOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	dana := h.SignIn("dana@example.com")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	if _, err := h.Service.AddMilestone(t.Context(), domain.AddMilestoneInput{
		GoalID: proposed.ID, Name: "Pricing page", TargetDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	active := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, dana, active.ID)
	ts := newServer(t, h)

	pricing := onlyMilestone(t, h, proposed)
	sam2 := signInClient(t, ts.URL, "sam@example.com")
	edit := tagAround(t, pageElement(t, getBody(t, sam2, goalPageURL(ts.URL, proposed)), "section", "goal-milestones"), fmt.Sprintf(`action="/milestones/%d"`, pricing.ID))
	resp := postForm(t, sam2, ts.URL+attr(edit, "action"), url.Values{"name": {"Pricing page live"}, "target_date": {"2026-04-15"}})
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, proposed); resp.StatusCode != http.StatusOK || got.Name != "Pricing page live" || got.TargetDate.Format("2006-01-02") != "2026-04-15" {
		t.Errorf("the Owner's Edit answered %d and left %q %s", resp.StatusCode, got.Name, got.TargetDate.Format("2006-01-02"))
	}

	beta := onlyMilestone(t, h, active)
	dana2 := signInClient(t, ts.URL, "dana@example.com")
	resp = postForm(t, dana2, fmt.Sprintf("%s/milestones/%d", ts.URL, beta.ID), url.Values{"name": {"Public beta"}, "target_date": {beta.TargetDate.Format("2006-01-02")}})
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, active); resp.StatusCode != http.StatusOK || got.Name != "Public beta" {
		t.Errorf("a Delegate's rename answered %d and left %q", resp.StatusCode, got.Name)
	}
	resp = postForm(t, dana2, fmt.Sprintf("%s/milestones/%d", ts.URL, beta.ID), url.Values{"name": {"Public beta"}, "target_date": {"2026-12-01"}})
	_ = readBody(t, resp)
	if got := onlyMilestone(t, h, active); resp.StatusCode != http.StatusUnprocessableEntity || !got.TargetDate.Equal(beta.TargetDate) {
		t.Errorf("moving an Active Goal's Milestone date outside a Check-in answered %d and left %s", resp.StatusCode, got.TargetDate)
	}
}

// goalMarkedMilestones is how the Goal page lists MilestoneMarksGoal's
// Milestones, earliest date first: mark, then date, then name.
var goalMarkedMilestones = []string{
	"Red 2026-01-20 Security review",
	"Done 2026-02-01 Pilot",
	"New 2026-03-10 Docs",
	"Yellow 2026-04-12 2026-04-02 Beta",
	"Yellow 2026-04-20 2026-03-20 (3) Vendor sign-off",
	"2026-05-01 Runbook",
	"Removed 2026-05-20 Launch party Removed: Budget cut.",
	"2026-06-15 GA launch",
}

// The Goal page's Milestones table reads Status, Date, Milestone. Each
// Milestone's mark, judged as of today, is the first that applies: Done,
// Removed, Red while overdue, Yellow once slipped, New when the latest
// Check-in added it — not an earlier one — and none while on track. Its date
// is the current one with the most recent it slipped from struck through, and
// a count once it slipped more than once. The marks are not a Health: the
// Goal keeps the Yellow its Owner set and its Rolled-up Health (ADR 0003).
func TestGoalPageListsMilestonesAsMarkDateNameOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.MilestoneMarksGoal(sam)
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	section := pageElement(t, page, "section", "goal-milestones")
	var headings []string
	for _, th := range regexp.MustCompile(`<th>([^<]*)</th>`).FindAllStringSubmatch(section, -1) {
		headings = append(headings, th[1])
	}
	if want := []string{"Status", "Date", "Milestone"}; !slices.Equal(headings, want) {
		t.Errorf("Milestone columns %q, want %q", headings, want)
	}
	if got := elementTexts(section, "tr", "goal-milestone"); !slices.Equal(got, goalMarkedMilestones) {
		t.Errorf("Milestones read\n %q\nwant\n %q", got, goalMarkedMilestones)
	}
	var vendor string
	for _, tr := range strings.Split(section, `<tr data-testid="goal-milestone"`) {
		if strings.Contains(tr, "Vendor sign-off") {
			vendor = tr
		}
	}
	if !strings.Contains(vendor, "<del>2026-03-20</del>") {
		t.Errorf("Vendor sign-off does not strike through the date it last slipped from:\n%s", vendor)
	}
	for _, older := range []string{"2026-03-01", "2026-03-10"} {
		if strings.Contains(vendor, older) {
			t.Errorf("Vendor sign-off still lists the older date %s:\n%s", older, vendor)
		}
	}
	if !strings.Contains(section, "<del>Launch party</del>") {
		t.Errorf("the Removed Milestone's name is not struck through:\n%s", section)
	}
	for _, want := range []string{`<span data-testid="goal-health">Yellow</span>`, `<span data-testid="goal-rollup-health">Yellow</span>`} {
		if !strings.Contains(page, want) {
			t.Errorf("Goal page lacks %s: the Owner's Yellow and the Rolled-up Health stand whatever the Milestones' marks", want)
		}
	}
}

// Each Milestone date, the current one and each struck slip, wraps as a whole,
// never mid-date ("2026-04-" over "02") at phone width, while the breaks between
// the dates still wrap (#172).
func TestGoalPageMilestoneDatesDoNotBreakOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	beta := onlyMilestone(t, h, goal)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	slipped := beta.TargetDate.AddDate(0, 0, 10)
	postForm(t, client, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Beta moves."},
		fmt.Sprintf("milestone_date_%d", beta.ID):        {slipped.Format("2006-01-02")},
		fmt.Sprintf("milestone_date_reason_%d", beta.ID): {"Vendor slipped."},
	})

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	row := pageElement(t, pageElement(t, page, "section", "goal-milestones"), "tr", "goal-milestone")
	cell := between(t, row, `<td class="num gp-dates">`, "</td>")
	for _, want := range []string{"<del>" + beta.TargetDate.Format("2006-01-02") + "</del>", "<span>" + slipped.Format("2006-01-02") + "</span>"} {
		if !strings.Contains(cell, want) {
			t.Errorf("the Milestone's date cell lacks %s: %s", want, cell)
		}
	}
	if rule := cssRule(t, page, ".gp-dates>*"); !strings.Contains(rule, "white-space:nowrap") {
		t.Errorf("a Milestone date can break mid-date: .gp-dates>*{%s}", rule)
	}
	if rule, ok := ruleFor(page, ".gp-dates"); ok && strings.Contains(rule, "nowrap") {
		t.Errorf("the whole date cell is kept on one line, so its slips can't wrap: .gp-dates{%s}", rule)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	list := pageElement(t, page, "ul", "delegate-list")
	departed := between(t, list, "tpm@example.com", "</li>")
	if !strings.Contains(departed, `data-testid="delegate-departed"`) || !strings.Contains(departed, "departed") {
		t.Errorf("the Departed Delegate isn't marked departed: %s", departed)
	}
	if present := between(t, list, "lee@example.com", "</li>"); strings.Contains(present, "departed") {
		t.Errorf("a present Delegate is marked departed: %s", present)
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	crumbs := strings.Join(strings.Fields(pageElement(t, page, "nav", "goal-breadcrumb")), " ")
	for _, want := range []string{`<a href="/goals">Goals</a>`, navTo(parent.ID) + `>Grow revenue</a>`, `<span aria-current="page">Reduce outages</span>`} {
		if !strings.Contains(crumbs, want) {
			t.Errorf("breadcrumb lacks %s: %s", want, crumbs)
		}
	}

	for _, tc := range []struct {
		section string
		linked  domain.Goal
		class   string
	}{
		{"goal-parents", parent, "r"},
		{"goal-children", child, "g"},
	} {
		section := pageElement(t, page, "section", tc.section)
		entry := between(t, section, navTo(tc.linked.ID), "</li>")
		if !strings.Contains(entry, `class="badge `+tc.class+`"`) {
			t.Errorf("%s: %s has no .%s Health badge: %s", tc.section, tc.linked.Title, tc.class, entry)
		}
		assertSubmitsAtOnce(t, tc.section+": Remove", tagAround(t, section, `/remove"`))
	}

	// Removing the link to the parent from its form unlinks it.
	client := signInClient(t, ts.URL, "sam@example.com")
	remove := strings.TrimPrefix(between(t, pageElement(t, page, "section", "goal-parents"), `action="`, `/remove"`), `action="`)
	if resp := postForm(t, client, ts.URL+remove+"/remove", url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("remove link: status %d", resp.StatusCode)
	}
	page = getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if parents := pageElement(t, page, "section", "goal-parents"); !strings.Contains(parents, `data-testid="no-parents"`) {
		t.Errorf("the parent is still linked after Remove:\n%s", parents)
	}
}

// Each Metric is a card: its current value large, "baseline → target by date",
// and a sparkline of its readings with a dashed target line, labelled for a
// screen reader with the trend. The Owner edits it in a collapsed section.
func TestGoalPageMetricCardsShowASparkline(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	ctx := t.Context()
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalDated(ctx, goal.ID, testsupport.Epoch.AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	metric, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown,
		Baseline: 1200, Target: 400, TargetDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	if _, err := h.Service.ActivateGoal(ctx, goal.ID); err != nil {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalURL)
	card := between(t, page, `data-testid="goal-metric"`, `data-testid="goal-milestones"`)
	if !strings.Contains(openTag(card), "card") {
		t.Errorf("the Metric is not a card: %s", openTag(card))
	}
	if current := between(t, card, `data-testid="metric-current"`, "</"); !strings.Contains(current, ">800") {
		t.Errorf("current value is not the latest reading: %s", current)
	}
	if !strings.Contains(between(t, card, "<", `data-testid="metric-current"`), "num") {
		t.Errorf("current value is not set as a number")
	}
	if !strings.Contains(card, "1200 → 400 ms by 2026-06-15") {
		t.Errorf("card lacks baseline → target by date:\n%s", card)
	}
	svg := between(t, card, "<svg", "</svg>")
	for _, want := range []string{
		`role="img"`,
		`aria-label="p95 latency: 2 readings, falling from 1000 to 800 ms, toward the target of 400 ms"`,
		"<polyline",
		`stroke-dasharray`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("sparkline lacks %s:\n%s", want, svg)
		}
	}
	edit := between(t, card, `<details data-testid="metric-edit"`, "</details>")
	if strings.Contains(openTag(edit), "open") || !strings.Contains(edit, fmt.Sprintf(`action="/metrics/%d"`, metric.ID)) {
		t.Errorf("the Owner's edit form is not in a collapsed section: %s", edit)
	}

	if other := getBody(t, signInClient(t, ts.URL, "mel@example.com"), goalURL); strings.Contains(other, fmt.Sprintf(`action="/metrics/%d"`, metric.ID)) {
		t.Errorf("a non-Owner is offered the Metric edit form")
	}
}

// The Goal page's History — Check-ins, Date Slips, So What revisions,
// ownership changes and value changes — is one timeline under History, open on
// load, with a filter chip per kind carrying its count.
func TestGoalPageHistoryIsOpenWithCounts(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Still fine.", "", time.Time{})
	if _, err := h.Service.EditSoWhat(t.Context(), goal.ID, "Outages cost trust and money.", sam.ID); err != nil {
		t.Fatalf("EditSoWhat: %v", err)
	}
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	history := historyBlock(t, page)
	if strings.Contains(history, "<details") {
		t.Errorf("History sits behind a disclosure:\n%s", history)
	}
	chips := historyChips(t, history)
	for label, count := range map[string]string{
		"All":        "4",
		"Check-ins":  "2",
		"Date Slips": "0",
		"So What":    "2", // the original and the edit
		"Ownership":  "0",
		"Values":     "0",
	} {
		if chips[label].count != count {
			t.Errorf("%q chip counts %q, want %s", label, chips[label].count, count)
		}
	}
	if n := len(historyEntries(history)); n != 4 {
		t.Errorf("History lists %d entries, want 4:\n%s", n, history)
	}
}

// The Latest status card leads the main column with the latest Check-in's
// Health, status and Path to Green, and who wrote it and when.
func TestGoalPageLatestStatusCard(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthYellow, "Wobbling.", "Add a second on-call.", testsupport.Epoch.AddDate(0, 2, 0))
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	status := pageElement(t, page, "section", "goal-checkins")
	if !strings.Contains(openTag(status), "card") {
		t.Errorf("Latest status is not a card: %s", openTag(status))
	}
	for _, want := range []string{"Latest status", "Wobbling.", "Add a second on-call.", "sam@example.com", "2026-01-02"} {
		if !strings.Contains(status, want) {
			t.Errorf("Latest status lacks %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, `data-testid="checkin-history"`) {
		t.Errorf("Latest status still carries the history")
	}
}

// activationItems reads the activation checklist on a Proposed Goal's page as
// each item's label mapped to whether it is done. Each item reads as a mark,
// its label, and "done" or "missing", so it never relies on the mark alone.
func activationItems(t *testing.T, page string) map[string]bool {
	t.Helper()
	list := pageElement(t, page, "ul", "activation-checklist")
	items := map[string]bool{}
	for _, li := range strings.Split(list, "<li ")[1:] {
		words := strings.Fields(regexpTags.ReplaceAllString(li[strings.Index(li, ">")+1:], " "))
		if len(words) < 3 {
			t.Fatalf("checklist item has no mark, label and state: %s", li)
		}
		done := strings.Contains(openTag(li), `data-done="true"`)
		if state := words[len(words)-1]; state != map[bool]string{true: "done", false: "missing"}[done] {
			t.Errorf("checklist item says %q against data-done=%v: %s", state, done, li)
		}
		items[strings.Join(words[1:len(words)-1], " ")] = done
	}
	return items
}

var regexpTags = regexp.MustCompile(`<[^>]*>`)

// A Proposed Goal's Owner sees the activation checklist drawn from the rules
// activation enforces, each item done or missing, and Activate stays disabled
// until every item is done.
func TestProposedGoalShowsActivationChecklist(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	page := getBody(t, client, goalURL)
	want := map[string]bool{
		"So What":                                true,
		"Owner":                                  true,
		"Dated with a delivery date, or Ongoing": false,
		"A Milestone or Metric":                  false,
	}
	if got := activationItems(t, page); !maps.Equal(got, want) {
		t.Errorf("bare Proposed Goal checklist = %v, want %v", got, want)
	}
	if button := between(t, pageElement(t, page, "form", "activate-goal"), "<button", ">"); !strings.Contains(button, "disabled") {
		t.Errorf("Activate is enabled with items missing: %s", button)
	}

	postForm(t, client, goalURL+"/dated", url.Values{"delivery_date": {"2026-06-15"}})
	page = getBody(t, client, goalURL)
	if got := activationItems(t, page); !got["Dated with a delivery date, or Ongoing"] || got["A Milestone or Metric"] {
		t.Errorf("Dated Goal with no Milestone or Metric: checklist = %v", got)
	}
	postForm(t, client, goalURL+"/milestones", url.Values{"name": {"Beta cut"}, "target_date": {"2026-03-16"}})
	page = getBody(t, client, goalURL)
	for item, done := range activationItems(t, page) {
		if !done {
			t.Errorf("%q still missing once the Goal is ready", item)
		}
	}
	if button := between(t, pageElement(t, page, "form", "activate-goal"), "<button", ">"); strings.Contains(button, "disabled") {
		t.Errorf("Activate is disabled with every item done: %s", button)
	}
}

// The Define card on a Proposed Goal's page holds no forms but Activate Goal:
// the checklist, the Activate Goal button, and a Finish defining link to the
// define page, where the Kind, cadence, So What, Milestones and Metrics are
// set (#135).
func TestDefineCardLinksToFinishDefining(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	card := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "section", "define-goal")
	for _, gone := range []string{"mark-kind", "set-cadence", "edit-so-what", "add-milestone", "add-metric"} {
		if strings.Contains(card, `data-testid="`+gone+`"`) {
			t.Errorf("the Define card still holds %s:\n%s", gone, card)
		}
	}
	if n := strings.Count(card, "<form"); n != 1 || !strings.Contains(card, `data-testid="activate-goal"`) {
		t.Errorf("the Define card holds %d forms, want only Activate Goal:\n%s", n, card)
	}
	activationItems(t, card)
	link := tagAround(t, card, `data-testid="finish-defining"`)
	if want := fmt.Sprintf("/goals/%d/define", goal.ID); attr(link, "href") != want || !strings.Contains(between(t, card, link, "</a>"), "Finish defining") {
		t.Errorf("Finish defining doesn't link to %s: %s", want, link)
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

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/children", ts.URL, goal.ID), url.Values{"title": {"Child"}, "so_what": {"It matters."}})
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

	postForm(t, client, goalURL+"/ongoing", nil)
	postForm(t, client, goalURL+"/milestones", url.Values{"name": {"Runbook"}, "target_date": {"2026-03-16"}})
	page := getBody(t, client, goalURL)
	if done, listed := activationItems(t, page)["A Metric"]; !listed || done {
		t.Errorf("Ongoing Goal with only a Milestone: A Metric listed=%v done=%v", listed, done)
	}
	if button := between(t, pageElement(t, page, "form", "activate-goal"), "<button", ">"); !strings.Contains(button, "disabled") {
		t.Errorf("Activate is enabled for an Ongoing Goal with no Metric: %s", button)
	}
}

// shownPlainAs is how a page shows a person inside another control, where a
// second control can't nest: by label, with their email on hover only.
func shownPlainAs(emailAddr, label string) string {
	return `<span class="person" title="` + emailAddr + `">` + label + `</span>`
}

// collapsedEmail is a person's email waiting, hidden, in its expansion.
var collapsedEmail = regexp.MustCompile(`<span class="person-email" hidden>[^<]*</span>`)

// visibleEmails returns every email a page shows as text rather than on hover,
// in a collapsed expansion, or in a form field.
func visibleEmails(page string) []string {
	page = collapsedEmail.ReplaceAllString(page, "")
	return regexp.MustCompile(`>[^<>]*@example\.com[^<>]*<`).FindAllString(page, -1)
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

	list := getBody(t, client, ts.URL+"/goals")
	for _, want := range []string{shownAs("ada.okafor@example.com", "Ada Okafor"), shownAs("sam@example.com", "sam")} {
		if !strings.Contains(list, want) {
			t.Errorf("Goal list does not show %s", want)
		}
	}
	page := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, named.ID))
	if owner := pageElement(t, page, "strong", "goal-owner"); !strings.Contains(owner, shownAs("ada.okafor@example.com", "Ada Okafor")) {
		t.Errorf("Goal page Owner reads %s, want Ada Okafor with their email a click away", owner)
	}
	for name, p := range map[string]string{"Goal list": list, "Goal page": page} {
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
	page := getBody(t, signInClient(t, ts.URL, ada.Email), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))

	owner := pageElement(t, page, "strong", "goal-owner")
	want := `<button type="button" class="disclose" aria-expanded="false" title="ada.okafor@example.com">Ada Okafor</button>` +
		`<span class="person-email" hidden>ada.okafor@example.com</span>`
	if !strings.Contains(owner, want) {
		t.Errorf("Goal page Owner reads %s, want %s", owner, want)
	}
	script := disclosureScript(t, page)
	for _, want := range []string{`.disclose`, `aria-expanded`, `hidden`} {
		if !strings.Contains(script, want) {
			t.Errorf("the page's disclosure script does not handle %s:\n%s", want, script)
		}
	}
}

// interactiveTag matches the open and close tags of the elements a control
// can't nest inside.
var interactiveTag = regexp.MustCompile(`<(/?)(a|button|label|summary)[\s>]`)

// nestedControls returns every interactive element a page opens inside
// another one, which HTML forbids.
func nestedControls(page string) []string {
	var nested []string
	depth := 0
	for _, at := range interactiveTag.FindAllStringSubmatchIndex(page, -1) {
		if at[3] > at[2] {
			depth = max(depth-1, 0)
			continue
		}
		if depth > 0 {
			tag, _, _ := strings.Cut(page[at[0]:], ">")
			nested = append(nested, tag+">")
		}
		depth++
	}
	return nested
}

// disclosureScript returns the page's script that opens and closes every
// .disclose control's expansion.
func disclosureScript(t *testing.T, page string) string {
	t.Helper()
	for _, s := range strings.Split(page, "<script>")[1:] {
		if body, _, ok := strings.Cut(s, "</script>"); ok && strings.Contains(body, "disclose") {
			return body
		}
	}
	t.Fatalf("page has no disclosure script")
	return ""
}

// The Goal page's So What definition and Top-level explanation each sit
// collapsed behind a button that opens them in place on a click, tap, Enter or
// Space, and reports whether they are open, so neither needs hover. The
// wording is the one the hover title always carried. The So What shows no
// heading, but assistive technology still hears it labelled "So What".
func TestGoalPageExplainsSoWhatAndTopLevelOnActivation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	g := h.MarkTopLevel(ada, h.ActiveGoal(ada, "Grow revenue", "It pays for everything."))
	page := getBody(t, signInClient(t, ts.URL, ada.Email), fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))

	for _, tc := range []struct{ label, control, text string }{
		{"What is a So What?", `<button type="button" class="disclose help explain gp-lead-help"`, "The customer problem this Goal addresses and what is expected to change when it succeeds."},
		{"Top-level", `<button type="button" class="disclose help" data-testid="goal-top-level"`, "One of the org's root outcomes"},
	} {
		at := strings.Index(page, tc.control)
		if at < 0 {
			t.Errorf("%s is not a disclosure button %s", tc.label, tc.control)
			continue
		}
		button, _, _ := strings.Cut(page[at+strings.Index(tc.control, "<button"):], "</button>")
		if !strings.HasSuffix(button, ">"+tc.label) || !strings.Contains(openTag(button), `aria-expanded="false"`) {
			t.Errorf("%s button does not start collapsed: %s", tc.label, button)
		}
		id := strings.TrimPrefix(between(t, openTag(button), `aria-controls="`, `" `), `aria-controls="`)
		if want := `id="` + id + `" class="explain" hidden>` + html.EscapeString(tc.text) + `<`; id == "" || !strings.Contains(page, want) {
			t.Errorf("%s button controls %q, want the collapsed explanation %s", tc.label, id, want)
		}
	}
	statement := openTag(pageElement(t, page, "article", "goal"))
	_, label, _ := strings.Cut(statement, `aria-labelledby="`)
	label, _, _ = strings.Cut(label, `"`)
	if want := `id="` + label + `" class="sr-only">So What<`; label == "" || !strings.Contains(page, want) {
		t.Errorf("the So What is not labelled for assistive technology by a visually hidden %s: %s", want, statement)
	}
	disclosureScript(t, page)
}

// With a seeded org, every page that shows people shows them by Name with
// their email a click, tap or keypress away, never an email as text until
// asked for; the Print view, with no hover or controls, introduces them as
// Name (email) (CONTEXT.md: Name).
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
	// another person to delegate to, hand off to, and leave the org.
	var g domain.Goal
	for _, c := range goals {
		if c.Lifecycle == domain.LifecycleActive && strings.Contains(c.Owner.Email, ".") && c.Owner.Name != "" {
			g = c
			break
		}
	}
	if g.ID == 0 {
		t.Fatal("seeded org has no Active Goal owned by a named person")
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
		{owner.Email, "/goals"},
		{owner.Email, fmt.Sprintf("/goals/%d", g.ID)},
		{owner.Email, "/home"},
		{owner.Email, "/links"},
		{owner.Email, "/risks"},
		{owner.Email, "/signals"},
		{owner.Email, "/freshness"},
		{other.Email, "/home"},
		{other.Email, "/handoffs"},
		{other.Email, "/delegates"},
		{adminEmail, "/admin"},
		{adminEmail, reportPath},
		{adminEmail, pubPath},
	}
	for _, p := range pages {
		page := getBody(t, client(p.as), ts.URL+p.path)
		if shown := visibleEmails(page); len(shown) > 0 {
			t.Errorf("%s as %s shows emails as text: %q", p.path, p.as, shown)
		}
		if nested := nestedControls(page); len(nested) > 0 {
			t.Errorf("%s as %s nests a control inside another: %q", p.path, p.as, nested)
		}
		if !regexp.MustCompile(`<button type="button" class="disclose" aria-expanded="false" title="([^"]+@example\.com)">[A-Z][a-z]+ [A-Z][a-z]+</button><span class="person-email" hidden>[^<]+@example\.com</span>`).MatchString(page) {
			t.Errorf("%s as %s shows nobody by Name with their email a click away", p.path, p.as)
		}
	}
	if page := getBody(t, client(owner.Email), fmt.Sprintf("%s/goals/%d", ts.URL, g.ID)); !strings.Contains(page, shownAs(owner.Email, owner.Name)) {
		t.Errorf("Goal page does not show its Owner as %s", shownAs(owner.Email, owner.Name))
	}
	printed := getBody(t, client(adminEmail), ts.URL+pubPath+"/print")
	if want := ">" + owner.Name + " (" + owner.Email + ")<"; !strings.Contains(printed, want) {
		t.Errorf("print page does not introduce the Owner as %s", want)
	}
	if strings.Contains(printed, `class="disclose"`) {
		t.Errorf("print page puts people behind a control")
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

	edit := openForm(t, getBody(t, samClient, goalURL+"?open=dimensions"), "dimensions")
	for _, name := range []string{"Customer", "Partner"} {
		if !strings.Contains(edit, fmt.Sprintf(`name="new_value" aria-label="Add a %s value"`, name)) {
			t.Errorf("Extendable %s has no add-a-value input:\n%s", name, edit)
		}
	}
	if strings.Contains(edit, `aria-label="Add a Pillar value"`) {
		t.Errorf("Fixed Pillar offers an add-a-value input:\n%s", edit)
	}

	for _, add := range []struct {
		dim   domain.Dimension
		value string
	}{{customer, "Globex"}, {partners, "Umbrella"}, {partners, "Hooli"}} {
		resp := postForm(t, samClient, goalURL+"/dimensions", url.Values{
			"dimension_id": {fmt.Sprintf("%d", add.dim.ID)},
			"new_value":    {add.value},
		})
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("adding %s: status %d: %s", add.value, resp.StatusCode, body)
		}
	}
	section := pageElement(t, getBody(t, samClient, goalURL), "section", "goal-dimensions")
	for _, want := range []string{"Globex", "Umbrella", "Hooli"} {
		if !strings.Contains(section, `data-testid="goal-dimension-value">`+want) {
			t.Errorf("Goal page doesn't show the added %s:\n%s", want, section)
		}
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Partner")); !slices.Equal(got, []string{"Initech", "Umbrella", "Hooli"}) {
		t.Errorf("Partner list = %v, want [Initech Umbrella Hooli]", got)
	}
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
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, "Hooli is retired") {
		t.Errorf("adding hooli: status %d body %q, want 422 saying Hooli is retired", status, body)
	}
}

// A Delegate sees the Goal page's Dimension value controls and sets values and
// adds to an Extendable list through them; a Contributor sees no controls and
// is refused with 403 (CONTEXT.md: Delegate, Contributor).
func TestDelegateSetsAndAddsDimensionValuesButContributorCannotOverHTTP(t *testing.T) {
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
	if err := h.Service.AddContributorByEmail(context.Background(), goal.ID, "cory@example.com"); err != nil {
		t.Fatalf("AddContributorByEmail: %v", err)
	}
	ts := newServer(t, h)
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	deeClient := signInClient(t, ts.URL, "dee@example.com")
	coryClient := signInClient(t, ts.URL, "cory@example.com")

	if page := getBody(t, deeClient, goalURL); menuItems(t, page)["Edit Dimension values"] == "" || !strings.Contains(page, `data-testid="open-dimensions"`) {
		t.Fatalf("Delegate's Goal page doesn't offer to edit Dimension values:\n%s", page)
	}
	openForm(t, getBody(t, deeClient, goalURL+"?open=dimensions"), "dimensions")
	for _, form := range []url.Values{
		{"value_id": {fmt.Sprintf("%d", pillar.Values[1].ID)}},
		{"dimension_id": {fmt.Sprintf("%d", customer.ID)}, "new_value": {"Globex"}},
	} {
		resp := postForm(t, deeClient, goalURL+"/dimensions", form)
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("Delegate posting %v: status %d: %s", form, resp.StatusCode, body)
		}
	}
	section := pageElement(t, getBody(t, deeClient, goalURL), "section", "goal-dimensions")
	for _, want := range []string{"Reliability", "Globex"} {
		if !strings.Contains(section, `data-testid="goal-dimension-value">`+want) {
			t.Errorf("Goal page doesn't show %s set by the Delegate:\n%s", want, section)
		}
	}

	if page := getBody(t, coryClient, goalURL+"?open=dimensions"); openForms(page) > 0 || strings.Contains(page, `data-testid="open-dimensions"`) {
		t.Errorf("Contributor's Goal page offers Dimension value controls:\n%s", page)
	}
	for _, form := range []url.Values{
		{"value_id": {fmt.Sprintf("%d", pillar.Values[0].ID)}},
		{"dimension_id": {fmt.Sprintf("%d", customer.ID)}, "new_value": {"Initech"}},
	} {
		resp := postForm(t, coryClient, goalURL+"/dimensions", form)
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Contributor posting %v: status %d, want 403", form, resp.StatusCode)
		}
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Customer")); !slices.Equal(got, []string{"Acme", "Globex"}) {
		t.Errorf("Customer list = %v, want [Acme Globex]", got)
	}
}

// On the Goal page only what a person acts on or reads as a unit of status is
// a raised card: the Check-in, each Metric and Milestones. So What, Highlights,
// History and the sidebar's blocks sit on the canvas (DESIGN.md § Components:
// Card).
func TestGoalPageCardsOnlyWhatAPersonActsOn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	if _, err := h.Service.AddMetric(t.Context(), domain.AddMetricInput{
		GoalID: goal.ID, Name: "Incidents", Unit: "per month", Direction: domain.MetricDown,
		Baseline: 9, Target: 2, TargetDate: testsupport.Epoch.AddDate(0, 6, 0),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	h.Checkin(sam, goal.ID, domain.HealthGreen, "Fine.", "", time.Time{})
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))

	for _, tc := range []struct {
		tag, testID string
		card        bool
	}{
		{"section", "goal-checkins", true},
		{"div", "goal-metric", true},
		{"section", "goal-milestones", true},
		{"article", "goal", false},
		{"section", "goal-highlights", false},
		{"div", "goal-history", false},
		{"section", "goal-parents", false},
		{"section", "goal-children", false},
		{"div", "goal-people", false},
		{"section", "goal-dimensions", false},
	} {
		if got := inCard(t, page, tc.tag, tc.testID); got != tc.card {
			t.Errorf("%s is a card = %v, want %v", tc.testID, got, tc.card)
		}
	}
}

// inCard reports whether the element with testID is a card or is wrapped
// directly in one.
func inCard(t *testing.T, page, tag, testID string) bool {
	t.Helper()
	el := openTag(pageElement(t, page, tag, testID))
	if hasClass(el, "card") {
		return true
	}
	before, _, _ := strings.Cut(page, el)
	before = strings.TrimRight(before, " \t\n")
	wrapper := before[strings.LastIndex(before, "<"):]
	return !strings.HasPrefix(wrapper, "</") && hasClass(wrapper, "card")
}

// hasClass reports whether an open tag's class attribute holds class.
func hasClass(openTag, class string) bool {
	_, rest, ok := strings.Cut(openTag, `class="`)
	if !ok {
		return false
	}
	list, _, _ := strings.Cut(rest, `"`)
	return slices.Contains(strings.Fields(list), class)
}

// Highlights and History are page sections on the canvas, so they take an h2;
// the sidebar's canvas blocks keep the h3 size (DESIGN.md § Components: Card).
func TestGoalPageCanvasBlockHeadings(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))

	for _, tc := range []struct{ tag, testID, heading string }{
		{"section", "goal-highlights", "<h2>Highlights</h2>"},
		{"div", "goal-history", "<h2>History</h2>"},
		{"section", "goal-parents", "<h3>Contributes to</h3>"},
		{"section", "goal-children", "<h3>Contributed to by</h3>"},
		{"div", "goal-people", "<h3>People</h3>"},
		{"section", "goal-dimensions", "<h3>Dimensions</h3>"},
	} {
		block := pageElement(t, page, tc.tag, tc.testID)
		// A group whose form opens in place heads it beside its heading.
		first := strings.TrimPrefix(strings.TrimSpace(block[len(openTag(block))+1:]), `<div class="gp-group-head">`)
		if !strings.HasPrefix(first, tc.heading) {
			t.Errorf("%s does not open with %s: %.80s", tc.testID, tc.heading, first)
		}
	}
}

// A --color-border rule separates the Goal page's canvas blocks from the block
// above them.
func TestGoalPageCanvasBlocksSitUnderARule(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))

	for _, tc := range []struct{ tag, testID string }{
		{"section", "goal-highlights"},
		{"div", "goal-history"},
		{"section", "goal-children"},
		{"div", "goal-people"},
		{"section", "goal-dimensions"},
	} {
		if block := openTag(pageElement(t, page, tc.tag, tc.testID)); !hasClass(block, "ruled") {
			t.Errorf("%s does not sit under a rule: %s", tc.testID, block)
		}
	}

	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	if rule := cssRule(t, css, "\n.ruled"); !strings.Contains(rule, "border-top:1px solid var(--color-border)") {
		t.Errorf(".ruled draws no --color-border rule: %s", rule)
	}
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

	page := getBody(t, client, goalURL+"?open=dimensions")
	shown := between(t, page, `<section data-testid="goal-dimensions"`, `data-open-form="dimensions"`)
	if !strings.Contains(shown, `data-testid="goal-dimension-value">Growth`) || !strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("Growth isn't shown marked retired:\n%s", shown)
	}
	if strings.Contains(shown, "Quarter") {
		t.Errorf("a Retired Dimension the Goal carries no value in is listed:\n%s", shown)
	}
	edit := openForm(t, page, "dimensions")
	for _, d := range []domain.Dimension{pillar, quarter} {
		if strings.Contains(edit, d.Name) {
			t.Errorf("the Retired %s is offered for setting:\n%s", d.Name, edit)
		}
	}
	if !strings.Contains(edit, "Team") {
		t.Errorf("the live Team isn't offered for setting:\n%s", edit)
	}
	childURL := fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, goal.ID)
	if fits := fitsSection(t, getBody(t, client, childURL)); strings.Contains(fits, fmt.Sprintf(`<option value="%d"`, growth.ID)) {
		t.Errorf("a value of a Retired Dimension is offered as a child default:\n%s", fits)
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	page = getBody(t, client, goalURL+"?open=dimensions")
	shown = between(t, page, `<section data-testid="goal-dimensions"`, `data-open-form="dimensions"`)
	if strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("Growth is still marked retired after Pillar is restored:\n%s", shown)
	}
	if edit := openForm(t, page, "dimensions"); !strings.Contains(edit, `aria-label="Pillar"`) {
		t.Errorf("the restored Pillar isn't offered for setting:\n%s", edit)
	}
	if !chosen(t, fitsSection(t, getBody(t, client, childURL)), growth) {
		t.Errorf("Growth isn't offered as a child default once Pillar is restored")
	}
}

// A Retired Dimension drops out of the Goal list's filter and grouping, and a
// stale link filtering or grouping by it is ignored rather than narrowing the
// list with no control to undo it. A Goal's row still shows its value, marked
// retired. Restoring the Dimension returns it to both (CONTEXT.md: Retired).
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

	page := getBody(t, client, ts.URL+"/goals")
	more := between(t, page, `<details data-testid="more-filters"`, "</details>")
	if strings.Contains(more, "Pillar") || strings.Contains(more, "Growth") {
		t.Errorf("the Retired Pillar is offered to filter or group by:\n%s", more)
	}
	if !strings.Contains(more, "Team") {
		t.Errorf("the live Team isn't offered to filter or group by:\n%s", more)
	}
	row := between(t, page, `<tr data-testid="goal-row"`, "</tr>")
	if !strings.Contains(row, ">Alpha<") || !strings.Contains(row, `data-testid="goal-value-tag" class="tag">Growth`) || !strings.Contains(row, `data-testid="retired"`) {
		t.Errorf("Alpha's row doesn't show Growth marked retired:\n%s", row)
	}

	stale := getBody(t, client, fmt.Sprintf("%s/goals?value=%d&group=%d", ts.URL, growth.ID, pillar.ID))
	if got := rowTitles(goalRows(t, stale), alpha, bravo); !slices.Equal(got, []string{"Alpha", "Bravo"}) {
		t.Errorf("a stale link on the Retired Pillar lists %q, want both Goals", got)
	}
	if strings.Contains(stale, `data-testid="goal-group"`) {
		t.Errorf("a stale link groups by the Retired Pillar")
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	more = between(t, getBody(t, client, ts.URL+"/goals"), `<details data-testid="more-filters"`, "</details>")
	if !strings.Contains(more, "<legend>Pillar</legend>") || !strings.Contains(more, fmt.Sprintf(`<option value="%d"`, pillar.ID)) {
		t.Errorf("the restored Pillar isn't offered to filter and group by:\n%s", more)
	}
	grouped := getBody(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, pillar.ID))
	if !strings.Contains(grouped, `data-testid="goal-group"`) {
		t.Errorf("the list doesn't group by the restored Pillar")
	}
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

	filters := between(t, getBody(t, client, ts.URL+"/goals"), `data-testid="goal-filters"`, "</form>")
	if !strings.Contains(filters, "Pillar") {
		t.Fatalf("the Goal list's controls don't offer the Pillar Dimension:\n%s", filters)
	}
	for _, name := range []string{"Budget", "Rationale"} {
		if strings.Contains(filters, name) {
			t.Errorf("the Goal list's filter or grouping offers the %s Field:\n%s", name, filters)
		}
	}

	form := getBody(t, client, fmt.Sprintf("%s/goals/%d/checkin", ts.URL, goal.ID))
	for _, text := range []string{"Budget", "Rationale", "Because outages"} {
		if strings.Contains(form, text) {
			t.Errorf("the Check-in form shows %q:\n%s", text, form)
		}
	}
}

// tableHeads names the Goal table's columns, in order, by their header text.
func tableHeads(t *testing.T, page string) []string {
	t.Helper()
	return cellTexts(between(t, page, `<thead data-testid="goal-table-head"`, "</thead>"), "th")
}

// tableRows returns the Goal table's rows in the order they render.
func tableRows(t *testing.T, page string) []string {
	t.Helper()
	tbody := between(t, page, `<table data-testid="goal-table"`, "</table>")
	var rows []string
	for _, part := range strings.Split(tbody, `<tr data-testid="goal-table-row"`)[1:] {
		row, _, ok := strings.Cut(part, "</tr>")
		if !ok {
			t.Fatalf("unterminated table row:\n%s", part)
		}
		rows = append(rows, row)
	}
	return rows
}

// tableCell is the text of row's cell under the column headed head.
func tableCell(t *testing.T, page, row, head string) string {
	t.Helper()
	i := slices.Index(tableHeads(t, page), head)
	if i < 0 {
		t.Fatalf("the Goal table has no %s column", head)
	}
	cells := cellTexts(row, "td")
	if i >= len(cells) {
		t.Fatalf("row has %d cells, no %s:\n%s", len(cells), head, row)
	}
	return cells[i]
}

// cellTexts is the visible text of each tag cell in html, whitespace collapsed.
func cellTexts(html, tag string) []string {
	var texts []string
	for _, m := range regexp.MustCompile(`(?s)<`+tag+`[\s>].*?</`+tag+`>`).FindAllString(html, -1) {
		text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m, " ")
		texts = append(texts, strings.Join(strings.Fields(text), " "))
	}
	return texts
}

// The Goal list's table layout has a column for the Goal's title, Owner, Health,
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals?layout=table")
	want := []string{"Title", "Owner", "Health", "Lifecycle", "Delivery date", "Last check-in", "Area", "Pillar", "Approver", "Budget"}
	if got := tableHeads(t, page); !slices.Equal(got, want) {
		t.Fatalf("columns = %q, want %q", got, want)
	}
	rows := tableRows(t, page)
	if len(rows) != 1 {
		t.Fatalf("got %d table rows, want 1", len(rows))
	}
	row := rows[0]
	if !strings.Contains(row, navTo(goal.ID)+">Reduce outages<") {
		t.Errorf("the title doesn't link to the Goal:\n%s", row)
	}
	for head, cell := range map[string]string{
		"Owner":     "Sam Ortiz sam@example.com", // the Name, its email disclosed on demand
		"Health":    "Green",
		"Lifecycle": "Active",
		"Pillar":    "Growth, Trust",
		"Budget":    "1200 $",
		"Area":      "",
		"Approver":  "",
	} {
		if got := tableCell(t, page, row, head); got != cell {
			t.Errorf("%s cell = %q, want %q", head, got, cell)
		}
	}
}

// linkQuery is the query string of the <a> carrying data-testid on page.
func linkQuery(t *testing.T, page, testID string) url.Values {
	t.Helper()
	tag := pageTag(t, page, "a", testID)
	u, err := url.Parse(html.UnescapeString(attr(tag, "href")))
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

	list := getBody(t, client, fmt.Sprintf("%s/goals?q=alpha&value=%d", ts.URL, growth.ID))
	toTable := linkQuery(t, list, "layout-table")
	if toTable.Get("layout") != "table" || toTable.Get("q") != "alpha" || toTable.Get("value") != fmt.Sprint(growth.ID) {
		t.Fatalf("the Table toggle drops a filter: %v", toTable)
	}

	// A fresh browser following the shared table link sees the same slice.
	table := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/goals?"+toTable.Encode())
	rows := tableRows(t, table)
	if got := rowTitles(rows, alpha, alphaTrust, bravo); !slices.Equal(got, []string{"Alpha launch"}) {
		t.Errorf("the shared table lists %q, want only Alpha launch", got)
	}
	filters := between(t, table, `data-testid="goal-filters"`, "</form>")
	if !strings.Contains(filters, `name="layout" value="table"`) {
		t.Errorf("applying the filter bar leaves the table:\n%s", filters)
	}
	if !strings.Contains(filters, `value="alpha"`) || !strings.Contains(filters, fmt.Sprintf(`value="%d" checked`, growth.ID)) {
		t.Errorf("the table's filter bar doesn't show the active filters:\n%s", filters)
	}

	toList := linkQuery(t, table, "layout-list")
	if toList.Has("layout") || toList.Get("q") != "alpha" || toList.Get("value") != fmt.Sprint(growth.ID) {
		t.Errorf("the List toggle drops a filter or stays in the table: %v", toList)
	}
}

// sortLink is the URL the Goal table's header for the column head links to.
func sortLink(t *testing.T, page, head string) string {
	t.Helper()
	thead := between(t, page, `<thead data-testid="goal-table-head"`, "</thead>")
	for _, th := range regexp.MustCompile(`(?s)<th[\s>].*?</th>`).FindAllString(thead, -1) {
		if cellTexts(th, "th")[0] == head {
			at := strings.Index(th, "<a ")
			if at < 0 {
				t.Fatalf("the %s header doesn't sort:\n%s", head, th)
			}
			href := attr(openTag(th[at:]), "href")
			if href == "" {
				t.Fatalf("the %s header doesn't sort:\n%s", head, th)
			}
			return html.UnescapeString(href)
		}
	}
	t.Fatalf("the Goal table has no %s header:\n%s", head, thead)
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
	order := func(page string) []string {
		return rowTitles(tableRows(t, page), nine, ten, hundred, unset)
	}

	page := getBody(t, client, ts.URL+"/goals?layout=table&q=n")
	if got, want := order(page), []string{"Unset", "Hundred", "Nine", "Ten"}; !slices.Equal(got, want) {
		t.Errorf("unsorted order = %q, want the problem-first %q", got, want)
	}

	up := sortLink(t, page, "Budget")
	page = getBody(t, client, ts.URL+up)
	if got, want := order(page), []string{"Nine", "Ten", "Hundred", "Unset"}; !slices.Equal(got, want) {
		t.Errorf("sorted by Budget = %q, want %q", got, want)
	}
	if strings.Contains(page, "Excluded") {
		t.Errorf("sorting dropped the search filter")
	}

	down := sortLink(t, page, "Budget")
	if down == up {
		t.Fatalf("sorting by Budget again doesn't reverse it: %s", down)
	}
	page = getBody(t, client, ts.URL+down)
	if got, want := order(page), []string{"Hundred", "Ten", "Nine", "Unset"}; !slices.Equal(got, want) {
		t.Errorf("reverse-sorted by Budget = %q, want %q", got, want)
	}
	if again := sortLink(t, page, "Budget"); again != up {
		t.Errorf("a third click on Budget links to %s, want ascending again %s", again, up)
	}
}

// submitColumns submits the Goal table's Columns form as a browser would, with
// the columns labelled hide unticked and every other column ticked, and returns
// the page it lands on.
func submitColumns(t *testing.T, client *http.Client, base, page string, hide ...string) string {
	t.Helper()
	form := between(t, page, `<form data-testid="goal-columns"`, "</form>")
	if action := attr(openTag(form), "action"); action != "/goals" || attr(openTag(form), "method") != "get" {
		t.Fatalf("the Columns form doesn't GET /goals:\n%s", form)
	}
	values := url.Values{}
	for _, m := range regexp.MustCompile(`<input([^>]*)>([^<]*)`).FindAllStringSubmatch(form, -1) {
		tag, label := "<input"+m[1], strings.TrimSpace(m[2])
		switch attr(tag, "type") {
		case "hidden":
			values.Add(attr(tag, "name"), html.UnescapeString(attr(tag, "value")))
		case "checkbox":
			if !slices.Contains(hide, label) {
				values.Add(attr(tag, "name"), attr(tag, "value"))
			}
		}
	}
	return getBody(t, client, base+"/goals?"+values.Encode())
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
	all := []string{"Title", "Owner", "Health", "Lifecycle", "Delivery date", "Last check-in", "Pillar", "Budget"}

	page := getBody(t, client, ts.URL+"/goals?layout=table&q=alpha")
	if got := tableHeads(t, page); !slices.Equal(got, all) {
		t.Fatalf("columns = %q, want %q", got, all)
	}
	if form := between(t, page, `<form data-testid="goal-columns"`, "</form>"); strings.Contains(form, `value="title"`) {
		t.Errorf("the Columns control offers to hide Title:\n%s", form)
	}

	page = submitColumns(t, client, ts.URL, page, "Owner", "Budget")
	want := []string{"Title", "Health", "Lifecycle", "Delivery date", "Last check-in", "Pillar"}
	if got := tableHeads(t, page); !slices.Equal(got, want) {
		t.Errorf("after hiding Owner and Budget, columns = %q, want %q", got, want)
	}
	if strings.Contains(between(t, page, `<table data-testid="goal-table"`, "</table>"), "Bravo") {
		t.Errorf("hiding columns dropped the search filter")
	}

	next := getBody(t, client, ts.URL+"/goals?layout=table")
	if got := tableHeads(t, next); !slices.Equal(got, want) {
		t.Errorf("on the next visit, columns = %q, want %q", got, want)
	}
	if got := tableHeads(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/goals?layout=table")); !slices.Equal(got, all) {
		t.Errorf("another browser's columns = %q, want all of %q", got, all)
	}

	everything := submitColumns(t, client, ts.URL, next, all...)
	if got := tableHeads(t, everything); !slices.Equal(got, []string{"Title"}) {
		t.Errorf("hiding every column leaves %q, want Title alone", got)
	}
	restored := submitColumns(t, client, ts.URL, everything)
	if got := tableHeads(t, restored); !slices.Equal(got, all) {
		t.Errorf("showing every column again leaves %q, want %q", got, all)
	}
}

// The Goal table has no totals row: no sum, count or average of a column,
// since numbers in a Field are never added up across Goals (ADR 0005). Grouping
// doesn't apply to it, and on a narrow screen it scrolls sideways inside its
// own container rather than widening the page.
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

	page := getBody(t, client, fmt.Sprintf("%s/goals?layout=table&group=%d", ts.URL, pillar.ID))
	table := between(t, page, `<table data-testid="goal-table"`, "</table>")
	if strings.Contains(table, "<tfoot") || strings.Contains(table, `data-testid="goal-group"`) {
		t.Errorf("the Goal table has a footer or a group:\n%s", table)
	}
	if rows, trs := len(tableRows(t, page)), strings.Count(table, "<tr"); rows != 2 || trs != 3 {
		t.Errorf("the Goal table has %d Goal rows among %d rows, want the header and 2 Goals:\n%s", rows, trs, table)
	}
	for _, aggregate := range []string{">30<", ">15<", "Total", "Sum", "Average"} {
		if strings.Contains(table, aggregate) {
			t.Errorf("the Goal table shows an aggregate %q:\n%s", aggregate, table)
		}
	}
	if more := between(t, page, `data-testid="goal-filters"`, "</form>"); strings.Contains(more, "Group by") {
		t.Errorf("the table offers Group by, which doesn't apply to it:\n%s", more)
	}

	if !regexp.MustCompile(`<div class="table-scroll">\s*<table data-testid="goal-table"`).MatchString(page) {
		t.Errorf("the Goal table isn't in its own scrolling container")
	}
	css := getBody(t, client, ts.URL+"/static/app.css")
	if rule := cssRule(t, css, ".table-scroll"); !strings.Contains(rule, "overflow-x:auto") {
		t.Errorf("the table's container doesn't scroll sideways: %s", rule)
	}
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

	chips := historyChips(t, historyBlock(t, getBody(t, patClient, goalPageURL(ts.URL, goal))))
	if chips["Values"].count != "7" {
		t.Errorf("the Values chip counts %q, want 7", chips["Values"].count)
	}
	patShown, deeShown := shownAs(pat.Email, "Pat Owner"), shownAs(dee.Email, "Dee Delegate")
	want := []struct{ change, by, at string }{
		{"Budget: cleared (was 350)", patShown, "Fri 2 Jan 22:04"},
		{"Region: added APAC", deeShown, "Fri 2 Jan 21:04"},
		{"Region: removed EMEA", deeShown, "Fri 2 Jan 21:04"},
		{"Region: added EMEA", patShown, "Fri 2 Jan 20:04"},
		{"Pillar: set to Growth", patShown, "Fri 2 Jan 18:04"},
		{"Budget: 200 → 350", deeShown, "Fri 2 Jan 16:04"},
		{"Budget: set to 200", patShown, "Fri 2 Jan 15:04"},
	}
	entries := valueEntries(t, patClient, goalPageURL(ts.URL, goal))
	if len(entries) != len(want) {
		t.Fatalf("History lists %d value changes, want %d:\n%s", len(entries), len(want), strings.Join(entries, "\n"))
	}
	for i, w := range want {
		for _, part := range []string{w.change, w.by, w.at} {
			if !strings.Contains(entries[i], html.UnescapeString(part)) {
				t.Errorf("entry %d lacks %q:\n%s", i, part, entries[i])
			}
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

	page := submitColumns(t, client, ts.URL, getBody(t, client, ts.URL+"/goals?layout=table&q=alpha"), "Budget")
	tag := pageTag(t, page, "a", "goal-table-download")
	u, err := url.Parse(html.UnescapeString(attr(tag, "href")))
	if err != nil {
		t.Fatalf("download href: %v", err)
	}
	if u.Path != "/goals/download" || u.Query().Get("q") != "alpha" {
		t.Fatalf("Download CSV links to %s, want /goals/download keeping q=alpha", u)
	}

	resp, err := client.Get(ts.URL + u.String())
	if err != nil {
		t.Fatalf("GET download: %v", err)
	}
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
	want := "ID,Title,Owner,So What,Kind,Delivery Date,Milestones,Metrics,Parents,Pillar,Budget\n" +
		fmt.Sprintf("%d,Alpha launch,sam@example.com,A matters.,,,,,,Growth; Trust,1200\n", alpha.ID)
	if body != want {
		t.Errorf("download =\n%s\nwant\n%s", body, want)
	}
}

// tableCellHTML is the markup of row's cell under the column headed head.
func tableCellHTML(t *testing.T, page, row, head string) string {
	t.Helper()
	i := slices.Index(tableHeads(t, page), head)
	if i < 0 {
		t.Fatalf("the Goal table has no %s column", head)
	}
	cells := regexp.MustCompile(`(?s)<td[\s>].*?</td>`).FindAllString(row, -1)
	if i >= len(cells) {
		t.Fatalf("row has %d cells, no %s:\n%s", len(cells), head, row)
	}
	return cells[i]
}

// hasInput reports whether markup holds a form control a person types or picks
// a value in.
func hasInput(markup string) bool {
	return regexp.MustCompile(`<(input|select|textarea)[\s>]`).MatchString(regexp.MustCompile(`<input[^>]*type="hidden"[^>]*>`).ReplaceAllString(markup, ""))
}

// editTable follows the Goal table's Edit values control from the table at
// rawURL, returning the table in edit mode.
func editTable(t *testing.T, client *http.Client, baseURL, rawURL string) string {
	t.Helper()
	tag := pageTag(t, getBody(t, client, baseURL+rawURL), "a", "goal-table-edit")
	return getBody(t, client, baseURL+html.UnescapeString(attr(tag, "href")))
}

// tableRowOf is the Goal table's row for goal.
func tableRowOf(t *testing.T, page string, goal domain.Goal) string {
	t.Helper()
	for _, row := range tableRows(t, page) {
		if strings.Contains(row, navTo(goal.ID)) {
			return row
		}
	}
	t.Fatalf("the Goal table has no row for %s", goal.Title)
	return ""
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

	page := editTable(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL, "/goals?layout=table")

	form := pageTag(t, page, "form", "goal-table-form")
	if attr(form, "method") != "post" || !strings.HasPrefix(attr(form, "action"), "/goals/values") {
		t.Errorf("edit mode isn't a plain form POST to /goals/values: %s", form)
	}
	if n := strings.Count(page, `data-testid="goal-table-save"`); n != 1 {
		t.Errorf("edit mode has %d Save buttons, want 1", n)
	}
	for _, tc := range []struct {
		goal     domain.Goal
		editable bool
	}{{alpha, true}, {beta, false}, {gamma, true}} {
		row := tableRowOf(t, page, tc.goal)
		for _, head := range []string{"Pillar", "Tags", "Budget", "Notes"} {
			if got := hasInput(tableCellHTML(t, page, row, head)); got != tc.editable {
				t.Errorf("%s's %s cell has an input = %v, want %v", tc.goal.Title, head, got, tc.editable)
			}
		}
		for _, head := range []string{"Title", "Owner", "Health", "Lifecycle", "Delivery date", "Last check-in"} {
			if cell := tableCellHTML(t, page, row, head); hasInput(cell) {
				t.Errorf("%s's %s cell has an input:\n%s", tc.goal.Title, head, cell)
			}
		}
	}
}

// emptyLinks are the links in the Goal table on page with no text to name them.
func emptyLinks(t *testing.T, page string) []string {
	t.Helper()
	table := between(t, page, `<table data-testid="goal-table"`, "</table>")
	var empty []string
	for _, m := range regexp.MustCompile(`(?s)<a[\s>].*?</a>`).FindAllString(table, -1) {
		if strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(m, "")) == "" {
			empty = append(empty, m)
		}
	}
	return empty
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

	for mode, page := range map[string]string{
		"table":     getBody(t, client, ts.URL+"/goals?layout=table"),
		"edit mode": editTable(t, client, ts.URL, "/goals?layout=table"),
	} {
		if empty := emptyLinks(t, page); len(empty) > 0 {
			t.Errorf("the Goal table in %s has links with no text:\n%s", mode, strings.Join(empty, "\n"))
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

	page := editTable(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL, "/goals?layout=table")

	if cell := tableCellHTML(t, page, tableRowOf(t, page, alpha), "Owner"); cell != "<td>"+shownAs("sam@example.com", "sam")+"</td>" {
		t.Errorf("Alpha's Owner cell isn't just the person control:\n%s", cell)
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

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Add a Tags value to Alpha"), "mobile; web")
	resp := postForm(t, client, ts.URL+action, form)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.StatusCode, body)
	}

	cell := tableCellHTML(t, body, tableRowOf(t, body, alpha), "Tags")
	errorTag := regexp.MustCompile(`<p[^>]*data-testid="cell-error"[^>]*>`).FindString(cell)
	if errorTag == "" || attr(errorTag, "id") == "" {
		t.Fatalf("Alpha's Tags has no error message with an id:\n%s", cell)
	}
	group := regexp.MustCompile(`<fieldset[^>]*>`).FindString(cell)
	if !slices.Contains(strings.Fields(attr(group, "aria-describedby")), attr(errorTag, "id")) {
		t.Errorf("Alpha's Tags group doesn't point at its error message %q: %s", attr(errorTag, "id"), group)
	}
	boxes := regexp.MustCompile(`<input[^>]*type="checkbox"[^>]*>`).FindAllString(cell, -1)
	if len(boxes) != 2 {
		t.Fatalf("Alpha's Tags has %d checkboxes, want 2:\n%s", len(boxes), cell)
	}
	for _, box := range boxes {
		if attr(box, "aria-invalid") != "true" {
			t.Errorf("checkbox isn't marked invalid: %s", box)
		}
	}
}

// tableForm is what a browser would post from the Goal table's edit form as
// page renders it, and where to.
func tableForm(t *testing.T, page string) (string, url.Values) {
	t.Helper()
	form := between(t, page, `<form data-testid="goal-table-form"`, "</form>")
	return html.UnescapeString(attr(openTag(form), "action")), formValues(form)
}

// formValues is what a browser submits from form as it renders: every input,
// the ticked checkboxes and radios, each textarea, and each select's chosen (or
// else first) option.
func formValues(form string) url.Values {
	values := url.Values{}
	for _, tag := range regexp.MustCompile(`<input[^>]*>`).FindAllString(form, -1) {
		name, value := attr(tag, "name"), html.UnescapeString(attr(tag, "value"))
		switch attr(tag, "type") {
		case "checkbox", "radio":
			if regexp.MustCompile(`\schecked[\s/>]`).MatchString(tag) {
				values.Add(name, value)
			}
		default:
			values.Add(name, value)
		}
	}
	for _, m := range regexp.MustCompile(`(?s)(<textarea[^>]*>)(.*?)</textarea>`).FindAllStringSubmatch(form, -1) {
		values.Add(attr(m[1], "name"), html.UnescapeString(m[2]))
	}
	for _, m := range regexp.MustCompile(`(?s)(<select[^>]*>)(.*?)</select>`).FindAllStringSubmatch(form, -1) {
		options := regexp.MustCompile(`<option[^>]*>`).FindAllString(m[2], -1)
		chosen := options[0]
		for _, o := range options {
			if regexp.MustCompile(`\sselected[\s/>]`).MatchString(o) {
				chosen = o
			}
		}
		values.Add(attr(m[1], "name"), attr(chosen, "value"))
	}
	return values
}

// controlName is the name of the form control on page labelled label.
func controlName(t *testing.T, page, label string) string {
	t.Helper()
	at := regexp.MustCompile(`<(input|select|textarea|fieldset)[^>]*\saria-label="` + regexp.QuoteMeta(html.EscapeString(label)) + `"[^>]*>`).FindStringIndex(page)
	if at == nil {
		t.Fatalf("page has no control labelled %q", label)
	}
	if name := attr(page[at[0]:at[1]], "name"); name != "" {
		return name
	}
	// A fieldset of checkboxes: the name its boxes share.
	return attr(regexp.MustCompile(`<input[^>]*type="checkbox"[^>]*>`).FindString(page[at[1]:]), "name")
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

	page := editTable(t, client, ts.URL, "/goals?layout=table&lifecycle=Proposed")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Pillar of Alpha"), valueID(pillar.Values[0]))
	form.Add(controlName(t, page, "Tags of Beta"), valueID(tags.Values[1]))
	form.Set(controlName(t, page, "Add a Tags value to Beta"), "mobile")
	form.Set(controlName(t, page, "Budget of Gamma"), "1200")
	form.Set(controlName(t, page, "Notes of Gamma"), "Line one\nLine two")
	resp := postForm(t, client, ts.URL+action, form)
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
	if row := tableRowOf(t, body, beta); tableCell(t, body, row, "Tags") != "ux, mobile" {
		t.Errorf("Beta's Tags = %q, want ux, mobile", tableCell(t, body, row, "Tags"))
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

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Pillar of Alpha"), valueID(pillar.Values[1]))
	form.Set(controlName(t, page, "Approver of Alpha"), "Dana & Co")
	form.Set(controlName(t, page, "Budget of Beta"), "lots")
	resp := postForm(t, client, ts.URL+action, form)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(pageElement(t, body, "div", "goal-table-errors"), "Nothing was saved") {
		t.Errorf("the refusal doesn't say nothing was saved")
	}
	if n := strings.Count(body, `data-testid="cell-error"`); n != 1 {
		t.Fatalf("%d cells are marked bad, want 1", n)
	}
	betaBudget := tableCellHTML(t, body, tableRowOf(t, body, beta), "Budget")
	if !strings.Contains(betaBudget, `data-testid="cell-error"`) || !strings.Contains(betaBudget, "Budget takes a number") ||
		!strings.Contains(betaBudget, `value="lots"`) || !strings.Contains(betaBudget, `aria-invalid="true"`) {
		t.Errorf("Beta's Budget isn't marked bad keeping lots:\n%s", betaBudget)
	}
	alphaRow := tableRowOf(t, body, alpha)
	if cell := tableCellHTML(t, body, alphaRow, "Approver"); !strings.Contains(cell, `value="Dana &amp; Co"`) {
		t.Errorf("Alpha's Approver lost what was typed:\n%s", cell)
	}
	if cell := tableCellHTML(t, body, alphaRow, "Pillar"); !regexp.MustCompile(`<option value="` + valueID(pillar.Values[1]) + `" selected`).MatchString(cell) {
		t.Errorf("Alpha's Pillar lost the value picked:\n%s", cell)
	}

	for _, g := range []domain.Goal{alpha, beta} {
		if changes, err := h.Service.ValueHistory(context.Background(), g.ID); err != nil || len(changes) != 0 {
			t.Errorf("%s's history = %+v (%v), want nothing saved", g.Title, changes, err)
		}
	}

	// Fixing the bad cell and saving the form as it came back saves it all.
	action, form = tableForm(t, body)
	form.Set(controlName(t, body, "Budget of Beta"), "900")
	if resp := postForm(t, client, ts.URL+action, form); resp.StatusCode != http.StatusOK || resp.Request.URL.Query().Has("edit") {
		t.Fatalf("saving the fixed form: status %d at %s:\n%s", resp.StatusCode, resp.Request.URL, readBody(t, resp))
	}
	for g, want := range map[int64]int{alpha.ID: 2, beta.ID: 1} {
		if changes, _ := h.Service.ValueHistory(context.Background(), g); len(changes) != want {
			t.Errorf("Goal %d's history = %+v, want %d entries", g, changes, want)
		}
	}
}

// A crafted save of the Goal table touching a Goal the person may not set
// values on — its row had no inputs — is refused with 403, and nothing in it is
// saved, not even the cells of the person's own Goals (#80; CONTEXT.md:
// Delegate).
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

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Budget of Mine"), "10")
	form.Set(fmt.Sprintf("d.%d.%d", theirs.ID, pillar.ID), valueID(pillar.Values[0]))
	form.Set(fmt.Sprintf("f.%d.%d", theirs.ID, budget.ID), "99")
	resp := postForm(t, client, ts.URL+action, form)
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "Theirs") {
		t.Errorf("status = %d, want 403 naming Theirs:\n%s", resp.StatusCode, body)
	}
	for _, g := range []domain.Goal{mine, theirs} {
		if changes, err := h.Service.ValueHistory(context.Background(), g.ID); err != nil || len(changes) != 0 {
			t.Errorf("%s's history = %+v (%v), want nothing saved", g.Title, changes, err)
		}
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

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	if cell := tableCell(t, page, tableRowOf(t, page, alpha), "Quarter"); cell != "— Q1 (retired) Q2" {
		t.Errorf("Alpha's Quarter offers %q, want its Retired Q1 marked", cell)
	}
	if cell := tableCell(t, page, tableRowOf(t, page, beta), "Tags"); cell != "infra ux" {
		t.Errorf("Beta's Tags offers %q, want no Retired value", cell)
	}
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Budget of Alpha"), "5")
	resp := postForm(t, client, ts.URL+action, form)
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

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	h.SetGoalField(boss, alpha, budget, "300")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Approver of Alpha"), "Dana")
	resp := postForm(t, client, ts.URL+action, form)
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
	if _, err := h.Service.MarkGoalOngoing(context.Background(), goal.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	if _, err := h.Service.AddMetric(context.Background(), domain.AddMetricInput{
		GoalID: goal.ID, Name: "p95", Unit: "ms", Direction: domain.MetricDown, Baseline: 900, Target: 300,
		TargetDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)

	items := activationItems(t, getBody(t, client, goalURL))
	for _, label := range []string{"A value in Pillar", "A value in Budget"} {
		if done, ok := items[label]; !ok || done {
			t.Errorf("checklist item %q: listed %v, done %v; want listed and missing (items %v)", label, ok, done, items)
		}
	}
	resp := postForm(t, client, goalURL+"/activate", nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Pillar") || !strings.Contains(body, "Budget") {
		t.Errorf("activate without the values: status %d, body %q; want 422 naming Pillar and Budget", resp.StatusCode, body)
	}

	h.AssignGoalValue(goal, pillar.Values[0])
	h.SetGoalField(sam, goal, budget, "100")
	items = activationItems(t, getBody(t, client, goalURL))
	if !items["A value in Pillar"] || !items["A value in Budget"] {
		t.Errorf("checklist after setting the values: %v, want both done", items)
	}
	if resp := postForm(t, client, goalURL+"/activate", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("activate with the values: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
}

// An Active Goal lacking a required value shows an Incomplete flag naming what
// it lacks, in a quieter style than Stale's; setting the value clears it, and
// a Goal that isn't Active never shows it (CONTEXT.md: Incomplete).
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

	flag := pageElement(t, getBody(t, client, goalURL), "p", "goal-incomplete")
	for _, want := range []string{"Incomplete", "Pillar", "Budget"} {
		if !strings.Contains(flag, want) {
			t.Errorf("Incomplete flag lacks %q: %s", want, flag)
		}
	}
	if !strings.Contains(openTag(flag), "lc") || strings.Contains(openTag(flag), " st") {
		t.Errorf("Incomplete flag should be quieter than Stale: %s", openTag(flag))
	}
	if page := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, onHold.ID)); strings.Contains(page, `data-testid="goal-incomplete"`) {
		t.Errorf("On Hold Goal is flagged Incomplete")
	}

	h.AssignGoalValue(goal, pillar.Values[0])
	h.SetGoalField(sam, goal, budget, "100")
	if page := getBody(t, client, goalURL); strings.Contains(page, `data-testid="goal-incomplete"`) {
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

	page := getBody(t, client, ts.URL+"/goals")
	rows := goalRows(t, page)
	if got, want := rowTitles(rows, goals...), []string{"Alpha, incomplete", "Bravo, On Hold", "Charlie, Proposed", "Mike, incomplete", "Zulu, complete"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q: Incomplete changes no sort order", got, want)
	}
	for _, row := range rows {
		marked := strings.Contains(row, `data-testid="incomplete"`)
		want := strings.Contains(row, navTo(lacking.ID)) || strings.Contains(row, navTo(lackingToo.ID))
		if marked != want {
			t.Errorf("row marked Incomplete %v, want %v: %s", marked, want, row)
		}
	}
	if box := pageTag(t, page, "input", "goal-incomplete-filter"); strings.Contains(box, "checked") {
		t.Errorf("Incomplete filter is checked without ?incomplete=1: %s", box)
	}

	page = getBody(t, client, ts.URL+"/goals?incomplete=1")
	if got, want := rowTitles(goalRows(t, page), goals...), []string{"Alpha, incomplete", "Mike, incomplete"}; !slices.Equal(got, want) {
		t.Errorf("incomplete=1: rows = %q, want %q", got, want)
	}
	if box := pageTag(t, page, "input", "goal-incomplete-filter"); !strings.Contains(box, "checked") {
		t.Errorf("Incomplete filter lost its check: %s", box)
	}
	table := getBody(t, client, ts.URL+"/goals?layout=table&incomplete=1")
	for _, g := range goals {
		listed := strings.Contains(table, navTo(g.ID))
		if want := g.ID == lacking.ID || g.ID == lackingToo.ID; listed != want {
			t.Errorf("table layout, incomplete=1: %s listed %v, want %v", g.Title, listed, want)
		}
	}
}

// A download that would include a value containing a semicolon, named so
// before that was refused, is refused naming the Dimension and value so an
// Admin can rename it, and returns no file (#101).
func TestGoalTableDownloadRefusesAValueContainingASemicolon(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	rnd := h.NameValueWithSemicolon(pillar.Values[1], "R&D; Ops")
	h.AssignGoalValue(h.CreateGoal(boss, "Alpha launch", "A matters."), rnd)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp, err := client.Get(ts.URL + "/goals/download?layout=table")
	if err != nil {
		t.Fatalf("GET download: %v", err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("download status = %d, want %d", resp.StatusCode, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(body, `Pillar value "R&D; Ops"`) || !strings.Contains(body, "can't contain a semicolon") {
		t.Errorf("download body = %q, want it to name Pillar's value R&D; Ops and the semicolon", body)
	}
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
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(openForm(t, body, "dimensions")), reason) {
		t.Errorf("Goal page: status %d, body %q; want 422 saying why beside the open form", resp.StatusCode, body)
	}

	page := editTable(t, client, ts.URL, "/goals?layout=table")
	action, form := tableForm(t, page)
	form.Set(controlName(t, page, "Add a Customer value to Alpha"), "Hooli;")
	resp = postForm(t, client, ts.URL+action, form)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("table edit: status = %d, want 422:\n%s", resp.StatusCode, body)
	}
	if cell := tableCellHTML(t, body, tableRowOf(t, body, goal), "Customer"); !strings.Contains(cell, `data-testid="cell-error"`) || !strings.Contains(html.UnescapeString(cell), reason) {
		t.Errorf("table edit: Alpha's Customer isn't marked bad saying why:\n%s", cell)
	}

	if got := dimensionValueNames(dimensionByName(t, h, "Customer")); !slices.Equal(got, []string{"Acme"}) {
		t.Errorf("Customer list = %v, want [Acme] with nothing added", got)
	}
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
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON goal_value_changes
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
	if body := readBody(t, resp); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("choosing Reliability: status %d, want 500; body:\n%s", resp.StatusCode, body)
	}
	section := pageElement(t, getBody(t, client, goalURL), "section", "goal-dimensions")
	if !strings.Contains(section, `data-testid="goal-dimension-value">Growth<`) || strings.Contains(section, `data-testid="goal-dimension-value">Reliability<`) {
		t.Errorf("the Goal doesn't keep Growth after a failed change:\n%s", section)
	}
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

	edit := openForm(t, getBody(t, client, goalURL+"?open=dimensions"), "dimensions")
	form := between(t, edit, "<form", `aria-label="Pillar"`)
	form = form[strings.LastIndex(form, "<form"):]
	if !strings.Contains(form, fmt.Sprintf(`name="dimension_id" value="%d"`, pillar.ID)) {
		t.Errorf("Pillar's select form doesn't name its Dimension:\n%s", form)
	}
	if sel := between(t, edit, `aria-label="Pillar"`, "</select>"); !strings.Contains(sel, `<option value="">None</option>`) {
		t.Errorf("Pillar's select doesn't offer None:\n%s", sel)
	}

	none := url.Values{"dimension_id": {fmt.Sprint(pillar.ID)}, "value_id": {""}}
	for range 2 {
		resp := postForm(t, client, goalURL+"/dimensions", none)
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("choosing None: status %d; body:\n%s", resp.StatusCode, body)
		}
	}

	page := getBody(t, client, goalURL)
	if !strings.Contains(pageElement(t, page, "section", "goal-dimensions"), `data-testid="goal-dimension-unassigned"`) {
		t.Errorf("Pillar still has a value after choosing None:\n%s", pageElement(t, page, "section", "goal-dimensions"))
	}
	entries := valueEntries(t, client, goalURL)
	if len(entries) != 2 || !strings.Contains(entries[0], "Pillar: cleared (was Growth)") {
		t.Errorf("History lists %d value changes, want one cleared entry after the set:\n%s", len(entries), strings.Join(entries, "\n"))
	}
	if flag := pageElement(t, page, "p", "goal-incomplete"); !strings.Contains(flag, "Pillar") {
		t.Errorf("Incomplete flag doesn't name Pillar: %s", flag)
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

	edit := openForm(t, getBody(t, client, goalPageURL(ts.URL, goal)+"?open=dimensions"), "dimensions")
	sel := between(t, edit, `aria-label="Pillar"`, "</select>")
	if !strings.Contains(sel, fmt.Sprintf(`<option value="%d" selected>Growth (retired)</option>`, pillar.Values[0].ID)) {
		t.Errorf("Pillar's select doesn't keep the retired Growth selected:\n%s", sel)
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
		page := getBody(t, client, ts.URL+layout)
		form := tagAround(t, page, `data-testid="goal-filters"`)
		for name, want := range map[string]string{
			"hx-get":      "/goals",
			"hx-trigger":  "submit, change[target.type!='search'], input[target.type=='search'] delay:400ms",
			"hx-target":   "#goal-list",
			"hx-select":   "#goal-list",
			"hx-swap":     "outerHTML",
			"hx-push-url": "true",
			"hx-sync":     "this:replace",
		} {
			if got := html.UnescapeString(attr(form, name)); got != want {
				t.Errorf("%s: filter form %s = %q, want %q", layout, name, got, want)
			}
		}
		if !strings.Contains(page, `hx-history="false"`) {
			t.Errorf("%s: the page keeps htmx's history snapshot, so Back would show stale filters", layout)
		}
	}
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
		if form := tagAround(t, page, `data-testid="goal-filters"`); attr(form, "method") != "get" || attr(form, "action") != "/goals" {
			t.Errorf("%s: without JavaScript the filter bar no longer GETs /goals: %s", layout, form)
		}
		filters := between(t, page, `data-testid="goal-filters"`, "</form>")
		noscript := between(t, filters, "<noscript>", "</noscript>")
		if !strings.Contains(noscript, `<button type="submit" class="btn">Apply</button>`) {
			t.Errorf("%s: the filter bar's <noscript> lacks Apply:\n%s", layout, filters)
		}
		if strings.Count(filters, "Apply") != 1 {
			t.Errorf("%s: the filter bar shows Apply outside <noscript>:\n%s", layout, filters)
		}
	}
	columns := between(t, getBody(t, client, ts.URL+"/goals?layout=table"), `<form data-testid="goal-columns"`, "</form>")
	if !strings.Contains(columns, `<button type="submit" class="btn sm">Apply</button>`) || strings.Contains(columns, "<noscript>") {
		t.Errorf("the Columns form lost its own Apply:\n%s", columns)
	}
}

// htmxFilter changes the filter bar on page as a person would — set changes
// one field — and GETs the view the way htmx does, answering the page htmx
// selects from and the address it pushes.
func htmxFilter(t *testing.T, client *http.Client, base, page string, set func(url.Values)) (string, string) {
	t.Helper()
	form := between(t, page, `data-testid="goal-filters"`, "</form>")
	values := formValues(form)
	set(values)
	address := attr(tagAround(t, page, `data-testid="goal-filters"`), "hx-get") + "?" + values.Encode()
	req, err := http.NewRequest(http.MethodGet, base+address, nil)
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
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d:\n%s", address, resp.StatusCode, body)
	}
	return body, address
}

// swappedList is the part of page htmx swaps in for the list: from #goal-list
// on, which the page ends with.
func swappedList(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `<div id="goal-list"`, "")
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
	table := getBody(t, client, ts.URL+"/goals?layout=table&sort=title&dir=desc&group="+group)
	body, address := htmxFilter(t, client, ts.URL, table, func(v url.Values) { v.Set("q", "launch") })
	swapped := swappedList(t, body)
	if got, want := rowTitles(tableRows(t, swapped), goals...), []string{"Charlie launch", "Bravo launch", "Alpha launch"}; !slices.Equal(got, want) {
		t.Errorf("table searched under htmx lists %q, want %q", got, want)
	}
	direct := swappedList(t, getBody(t, client, ts.URL+address))
	if got, want := rowTitles(tableRows(t, swapped), goals...), rowTitles(tableRows(t, direct), goals...); !slices.Equal(got, want) {
		t.Errorf("table under htmx lists %q, but %s loaded directly lists %q", got, address, want)
	}
	if !regexp.MustCompile(`aria-sort="descending"><a [^>]*>Title</a>`).MatchString(swapped) {
		t.Errorf("the table under htmx lost its sort:\n%s", swapped)
	}
	if toList := linkQuery(t, swapped, "layout-list"); toList.Get("group") != group || toList.Get("q") != "launch" {
		t.Errorf("the table under htmx lost its grouping or search: List links to %v", toList)
	}

	// The list, grouped by Pillar, narrowed to Mine only.
	list := getBody(t, client, ts.URL+"/goals?group="+group)
	body, address = htmxFilter(t, client, ts.URL, list, func(v url.Values) { v.Set("mine", "1") })
	swapped = swappedList(t, body)
	if got, want := cellTexts(swapped, "th"), []string{"Health", "Goal", "Owner", "Due", "Last check-in", "Dimension tags", "Growth", "Trust"}; !slices.Equal(got, want) {
		t.Errorf("the list under htmx heads %q, want %q: grouping lost", got, want)
	}
	if got, want := rowTitles(goalRows(t, swapped), goals...), []string{"Alpha launch", "Charlie launch", "Delta audit"}; !slices.Equal(got, want) {
		t.Errorf("Mine only under htmx lists %q, want %q", got, want)
	}
	direct = swappedList(t, getBody(t, client, ts.URL+address))
	if got, want := rowTitles(goalRows(t, swapped), goals...), rowTitles(goalRows(t, direct), goals...); !slices.Equal(got, want) {
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

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	block := pageElement(t, page, "section", "goal-highlights")
	entries := strings.Split(block, `data-testid="highlight"`)[1:]
	if len(entries) != 2 {
		t.Fatalf("Highlights block lists %d entries, want 2; block:\n%s", len(entries), block)
	}
	for i, want := range []string{"Retries masked the root cause.", "Missed the SLA."} {
		if !strings.Contains(entries[i], want) {
			t.Errorf("entry %d = %s, want %q", i+1, entries[i], want)
		}
		if !strings.Contains(entries[i], `title="sam@example.com"`) {
			t.Errorf("entry %d does not credit the Owner: %s", i+1, entries[i])
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
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("choose %q: status %d; body:\n%s", valueID, resp.StatusCode, body)
		}
	}
	history := func() []string {
		t.Helper()
		return valueEntries(t, client, goalURL)
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
	listRow := func() string {
		t.Helper()
		for _, row := range goalRows(t, getBody(t, client, ts.URL+"/goals")) {
			if strings.Contains(row, navTo(goal.ID)) {
				return row
			}
		}
		t.Fatalf("the Goal list has no row for %q", goal.Title)
		return ""
	}

	if page := getBody(t, client, goalURL); !strings.Contains(page, `data-testid="goal-incomplete"`) {
		t.Fatalf("the Goal lacking Pillar and Budget isn't flagged Incomplete")
	}
	if row := listRow(); !strings.Contains(row, `data-testid="incomplete"`) {
		t.Fatalf("the Goal list doesn't mark the Goal Incomplete: %s", row)
	}

	for _, path := range []string{fmt.Sprintf("/dimensions/%d/required", pillar.ID), fmt.Sprintf("/fields/%d/required", budget.ID)} {
		if resp := postForm(t, admin, ts.URL+path, url.Values{"required": {"0"}}); resp.StatusCode != http.StatusOK {
			t.Fatalf("unmark %s: status %d", path, resp.StatusCode)
		}
	}

	if page := getBody(t, client, goalURL); strings.Contains(page, `data-testid="goal-incomplete"`) {
		t.Errorf("the Goal page still flags the Goal Incomplete: %s", pageElement(t, page, "p", "goal-incomplete"))
	}
	if row := listRow(); strings.Contains(row, `data-testid="incomplete"`) {
		t.Errorf("the Goal list still marks the Goal Incomplete: %s", row)
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

	if page := getBody(t, client, goalPageURL(ts.URL, active)); !strings.Contains(page, `data-testid="goal-incomplete"`) {
		t.Fatalf("the Active Goal lacking Pillar isn't flagged Incomplete")
	}
	for _, g := range []domain.Goal{done, cancelled} {
		if page := getBody(t, client, goalPageURL(ts.URL, g)); strings.Contains(page, `data-testid="goal-incomplete"`) {
			t.Errorf("the %s Goal is flagged Incomplete: %s", g.Lifecycle, pageElement(t, page, "p", "goal-incomplete"))
		}
	}

	rows := goalRows(t, getBody(t, client, ts.URL+"/goals"))
	if got, want := rowTitles(rows, goals...), []string{"Alpha, Active", "Bravo, Done", "Charlie, Cancelled"}; !slices.Equal(got, want) {
		t.Fatalf("Goal list rows = %q, want %q", got, want)
	}
	for _, row := range rows {
		marked := strings.Contains(row, `data-testid="incomplete"`)
		if want := strings.Contains(row, navTo(active.ID)); marked != want {
			t.Errorf("row marked Incomplete %v, want %v: %s", marked, want, row)
		}
	}

	filtered := getBody(t, client, ts.URL+"/goals?incomplete=1")
	if got, want := rowTitles(goalRows(t, filtered), goals...), []string{"Alpha, Active"}; !slices.Equal(got, want) {
		t.Errorf("incomplete=1: rows = %q, want %q", got, want)
	}
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
	order := func(page string) []string {
		return rowTitles(tableRows(t, page), alpha, bravo, charlie, unset)
	}

	page := getBody(t, client, ts.URL+"/goals?layout=table")
	page = getBody(t, client, ts.URL+sortLink(t, page, "Kickoff"))
	if got, want := order(page), []string{"Charlie", "Alpha", "Bravo", "Delta"}; !slices.Equal(got, want) {
		t.Errorf("sorted by Kickoff = %q, want %q", got, want)
	}

	page = getBody(t, client, ts.URL+sortLink(t, page, "Kickoff"))
	if got, want := order(page), []string{"Bravo", "Alpha", "Charlie", "Delta"}; !slices.Equal(got, want) {
		t.Errorf("reverse-sorted by Kickoff = %q, want %q", got, want)
	}
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

	page := editTable(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL, "/goals?layout=table")

	for _, g := range []domain.Goal{alpha, beta, gamma} {
		row := tableRowOf(t, page, g)
		for _, head := range []string{"Pillar", "Tags", "Budget", "Notes"} {
			if cell := tableCellHTML(t, page, row, head); !hasInput(cell) {
				t.Errorf("the Admin gets no input in %s's %s cell:\n%s", g.Title, head, cell)
			}
		}
	}
	for _, g := range []domain.Goal{alpha, beta} {
		row := tableRowOf(t, page, g)
		for _, head := range []string{"Title", "Health", "Lifecycle", "Delivery date"} {
			if cell := tableCellHTML(t, page, row, head); !strings.Contains(cell, navTo(g.ID)) {
				t.Errorf("%s's %s cell doesn't link to the Goal:\n%s", g.Title, head, cell)
			}
		}
	}
}

// actionMenu returns the Goal page header's action menu: from its <details> to
// the end of the header, where it sits last.
func actionMenu(t *testing.T, page string) string {
	t.Helper()
	return between(t, pageElement(t, page, "header", "goal-head"), `data-testid="goal-more"`, "")
}

// An Owner's header offers Check in as the primary action, No change as a quiet
// one, and beside them a "⋯" menu named for assistive technology. A signed-in
// person with no role on the Goal gets neither button, only the menu.
func TestGoalPageHeaderActionsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.Checkin(sam, goal.ID, domain.HealthGreen, "On track.", "", time.Time{})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), goalPageURL(ts.URL, goal))
	actions := pageElement(t, page, "div", "goal-actions")
	if link := tagAround(t, actions, `data-testid="checkin-link"`); !hasClass(link, "primary") || attr(link, "href") != fmt.Sprintf("/goals/%d/checkin", goal.ID) {
		t.Errorf("Check in is not a primary link to the Check-in page: %s", link)
	}
	noChange := between(t, pageElement(t, actions, "form", "no-change-checkin"), "<button", ">")
	if !hasClass(noChange, "quiet") {
		t.Errorf("No change is not a quiet button: %s", noChange)
	}
	summary := between(t, actionMenu(t, page), "<summary", "</summary>")
	if !strings.Contains(summary, ">⋯") || attr(summary+">", "aria-label") == "" {
		t.Errorf("the menu is not a ⋯ with an accessible name: %s", summary)
	}

	page = getBody(t, signInClient(t, ts.URL, "mel@example.com"), goalPageURL(ts.URL, goal))
	actions = pageElement(t, page, "div", "goal-actions")
	for _, button := range []string{`data-testid="checkin-link"`, `data-testid="no-change-checkin"`} {
		if strings.Contains(actions, button) {
			t.Errorf("someone with no role on the Goal is offered %s: %s", button, actions)
		}
	}
	if !strings.Contains(actions, `data-testid="goal-more"`) {
		t.Errorf("someone with no role on the Goal has no action menu: %s", actions)
	}
}

// menuLink matches one of the action menu's items: a plain link and its label.
var menuLink = regexp.MustCompile(`<a [^>]*href="([^"]*)"[^>]*>([^<]*)</a>`)

// menuItems returns the action menu's items, label to link, failing when the
// menu holds anything but links: no form, control or nested disclosure.
func menuItems(t *testing.T, page string) map[string]string {
	t.Helper()
	list := between(t, actionMenu(t, page), "<ul", "</ul>")
	for _, control := range []string{"<form", "<input", "<button", "<select", "<textarea", "<details"} {
		if strings.Contains(list, control) {
			t.Errorf("the action menu holds a %s>, not only links:\n%s", control, list)
		}
	}
	items := map[string]string{}
	for _, m := range menuLink.FindAllStringSubmatch(list, -1) {
		items[html.UnescapeString(m[2])] = html.UnescapeString(m[1])
	}
	if strings.Count(list, "<li") != len(items) {
		t.Errorf("an action menu item is not a plain link:\n%s", list)
	}
	return items
}

// The action menu lists the actions a person may take, each a plain link to the
// Goal page with that action's form open, but Add a child Goal, which links to
// the New goal form with this Goal picked as its parent: an Owner gets Hand
// off, Add a delegate, Link to a parent Goal, Add a child Goal and, with
// Dimensions and Fields defined, Edit Dimension values and Edit Fields; an
// Admin adds Mark Top-level and Mark owner departed…, and on an Ownerless Goal
// Reassign and Mark returned… in place of Hand off; anyone else only Add a
// child Goal. Anyone but the Owner gets Suggest a parent, linking to its own
// form.
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
			"Hand off":              open(goal, "handoff"),
			"Add a delegate":        open(goal, "delegates"),
			"Link to a parent Goal": open(goal, "parent-link"),
			"Add a child Goal":      child(goal),
			"Edit Dimension values": open(goal, "dimensions"),
			"Edit Fields":           open(goal, "fields"),
		}},
		{"ada@example.com", goal, map[string]string{
			"Hand off":              open(goal, "handoff"),
			"Suggest a parent":      suggest(goal),
			"Add a child Goal":      child(goal),
			"Edit Dimension values": open(goal, "dimensions"),
			"Edit Fields":           open(goal, "fields"),
			"Mark Top-level":        open(goal, "top-level"),
			"Mark owner departed…":  open(goal, "depart"),
		}},
		{"ada@example.com", orphan, map[string]string{
			"Suggest a parent":      suggest(orphan),
			"Add a child Goal":      child(orphan),
			"Edit Dimension values": open(orphan, "dimensions"),
			"Edit Fields":           open(orphan, "fields"),
			"Mark Top-level":        open(orphan, "top-level"),
			"Mark returned…":        open(orphan, "return"),
			"Reassign":              open(orphan, "reassign"),
		}},
		{"mel@example.com", goal, map[string]string{
			"Suggest a parent": suggest(goal),
			"Add a child Goal": child(goal),
		}},
	} {
		page := getBody(t, signInClient(t, ts.URL, tc.who), goalPageURL(ts.URL, tc.goal))
		if got := menuItems(t, page); !maps.Equal(got, tc.want) {
			t.Errorf("%s on %q: menu is %v, want %v", tc.who, tc.goal.Title, got, tc.want)
		}
	}
}

// assertOpenIn checks the open form sits in the page's block from the element
// marked start up to the one marked next, the part of the page it concerns.
func assertOpenIn(t *testing.T, page, form, start, next string) {
	t.Helper()
	at := strings.Index(page, `data-open-form="`+form+`"`)
	from, to := strings.Index(page, start), strings.Index(page, next)
	if from < 0 || to < 0 || at < from || at > to {
		t.Errorf("%q does not open between %s and %s", form, start, next)
	}
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
		head       = `data-testid="goal-head"`
		actions    = `data-testid="goal-actions"`
		parents    = `data-testid="goal-parents"`
		children   = `data-testid="goal-children"`
		people     = `data-testid="goal-people"`
		dimensions = `data-testid="goal-dimensions"`
		fields     = `data-testid="goal-field-block"`
	)

	for _, tc := range []struct {
		who, label  string
		goal        domain.Goal
		form        string
		action      string
		start, next string
	}{
		{"sam", "Hand off", goal, "handoff", fmt.Sprintf("/goals/%d/handoff", goal.ID), people, dimensions},
		{"sam", "Add a delegate", goal, "delegates", fmt.Sprintf("/goals/%d/delegates", goal.ID), people, dimensions},
		{"sam", "Link to a parent Goal", goal, "parent-link", fmt.Sprintf("/goals/%d/links", goal.ID), parents, children},
		{"sam", "Edit Dimension values", goal, "dimensions", fmt.Sprintf("/goals/%d/dimensions", goal.ID), dimensions, fields},
		{"sam", "Edit Fields", goal, "fields", fmt.Sprintf("/goals/%d/fields", goal.ID), fields, "</aside>"},
		{"ada", "Hand off", goal, "handoff", fmt.Sprintf("/goals/%d/handoff", goal.ID), people, dimensions},
		{"ada", "Mark Top-level", goal, "top-level", fmt.Sprintf("/goals/%d/top-level", goal.ID), head, actions},
		{"ada", "Mark owner departed…", goal, "depart", fmt.Sprintf("/accounts/%d/depart", sam.ID), people, dimensions},
		{"ada", "Reassign", orphan, "reassign", fmt.Sprintf("/goals/%d/reassign", orphan.ID), people, dimensions},
		{"ada", "Mark returned…", orphan, "return", fmt.Sprintf("/accounts/%d/return", kim.ID), people, dimensions},
	} {
		client := clients[tc.who]
		plain := getBody(t, client, goalPageURL(ts.URL, tc.goal))
		if n := openForms(plain); n != 0 {
			t.Errorf("%s: the plain Goal page shows %d forms open", tc.label, n)
		}
		page := getBody(t, client, ts.URL+menuItems(t, plain)[tc.label])
		open := openForm(t, page, tc.form)
		if !strings.Contains(open, `action="`+tc.action+`"`) {
			t.Errorf("%s opens no form posting to %s: %s", tc.label, tc.action, open)
		}
		if n := strings.Count(open, " autofocus"); n != 1 {
			t.Errorf("%s: open form has %d focused controls, want 1: %s", tc.label, n, open)
		}
		assertOpenIn(t, page, tc.form, tc.start, tc.next)
		cancel := attr(tagAround(t, open, `data-testid="cancel-form"`), "href")
		if cancel != fmt.Sprintf("/goals/%d", tc.goal.ID) {
			t.Errorf("%s: Cancel leads to %q, not the plain Goal page", tc.label, cancel)
		}
		if n := openForms(getBody(t, client, ts.URL+cancel)); n != 0 {
			t.Errorf("%s: after Cancel the page shows %d forms open", tc.label, n)
		}
	}
}

// Each sidebar group's heading carries a small link, for whoever may use it,
// that opens the group's form right there: Manage for Delegates and for
// Contributors, Edit for Dimensions and for Fields, + Add for Contributes to.
// Nothing on the page jumps to a collapsed section any more.
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
	plain := getBody(t, sam2, goalPageURL(ts.URL, goal))

	for _, tc := range []struct {
		group, heading, link, form, action, next string
	}{
		{"goal-parents", "<h3>Contributes to</h3>", "+ Add", "parent-link", "/links", `data-testid="goal-children"`},
		{"goal-delegates", `<h3 class="label">Delegates</h3>`, "Manage", "delegates", "/delegates", `data-testid="goal-contributors"`},
		{"goal-contributors", `<h3 class="label">Contributors</h3>`, "Manage", "contributors", "/contributors", `data-testid="goal-dimensions"`},
		{"goal-dimensions", "<h3>Dimensions</h3>", "Edit", "dimensions", "/dimensions", `data-testid="goal-field-block"`},
		{"goal-field-block", "<h3>Fields</h3>", "Edit", "fields", "/fields", "</aside>"},
	} {
		head := between(t, plain, `data-testid="`+tc.group+`"`, "</div>")
		if !strings.Contains(head, tc.heading) {
			t.Errorf("%s's heading is not %s: %s", tc.group, tc.heading, head)
			continue
		}
		link := tagAround(t, head, `data-testid="open-`+tc.form+`"`)
		if text := between(t, head, link, "</a>")[len(link):]; text != html.EscapeString(tc.link) {
			t.Errorf("%s's link reads %q, want %q", tc.group, text, tc.link)
		}
		if name := attr(link, "aria-label"); !strings.Contains(name, strings.TrimPrefix(tc.link, "+ ")) {
			t.Errorf("%s's link is named %q, which lacks its text %q", tc.group, name, tc.link)
		}
		page := getBody(t, sam2, ts.URL+html.UnescapeString(attr(link, "href")))
		open := openForm(t, page, tc.form)
		if !strings.Contains(open, fmt.Sprintf(`action="/goals/%d%s"`, goal.ID, tc.action)) {
			t.Errorf("%s's link opens no form posting to %s: %s", tc.group, tc.action, open)
		}
		assertOpenIn(t, page, tc.form, `data-testid="`+tc.group+`"`, tc.next)
		if strings.Contains(between(t, page, `data-testid="`+tc.group+`"`, "</div>"), `data-testid="open-`+tc.form+`"`) {
			t.Errorf("%s still offers its link with its form open", tc.group)
		}
	}
	if strings.Contains(plain, `href="#`) {
		t.Errorf("the Goal page still links to a section further down: %s", between(t, plain, `href="#`, ">"))
	}
	if strings.Contains(between(t, plain, `data-testid="goal-sidebar"`, "</aside>"), "<details") {
		t.Errorf("the sidebar still folds a form into a collapsed section")
	}

	mel := getBody(t, signInClient(t, ts.URL, "mel@example.com"), goalPageURL(ts.URL, goal))
	if strings.Contains(between(t, mel, `data-testid="goal-sidebar"`, "</aside>"), `data-testid="open-`) {
		t.Errorf("someone with no role on the Goal is offered a sidebar form")
	}
}

// A refused submit from a form opened in place comes back as the Goal page
// with that same form open, the reason beside it, and what was typed still in
// it, rather than a bare error page.
func TestGoalPageRefusedFormComesBackOpenOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	h.CreateExtendableDimension(ada, "Team", "Core")
	budget := h.CreateField(ada, "Budget", domain.FieldNumber, "USD")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
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
		reason, typed     string
	}{
		{"sam", "handoff", path(goal, "/handoff"), url.Values{"to_email": {"nobody@example.com"}}, "no account with email", `value="nobody@example.com"`},
		{"ada", "reassign", path(orphan, "/reassign"), url.Values{"email": {"nobody@example.com"}}, "no account with email", `value="nobody@example.com"`},
		{"sam", "parent-link", path(goal, "/links"), url.Values{"parent_id": {fmt.Sprint(goal.ID)}, "note": {"Because."}}, "cannot contribute to itself", ">Because.</textarea>"},
		{"sam", "delegates", path(goal, "/delegates"), url.Values{"email": {"nobody@example.com"}}, "nobody@example.com", `value="nobody@example.com"`},
		{"sam", "contributors", path(proposed, "/contributors"), url.Values{"email": {"nobody@example.com"}}, "no account with email", `value="nobody@example.com"`},
		{"sam", "milestones", path(goal, "/milestones"), url.Values{"name": {"  "}, "target_date": {"2026-05-01"}}, "a Milestone needs a name", `value="2026-05-01"`},
		{"sam", "milestones", path(goal, "/milestones"), url.Values{"name": {"GA"}, "target_date": {""}}, "a Milestone needs a date", `value="GA"`},
		{"sam", "dimensions", path(goal, "/dimensions"), url.Values{"dimension_id": {fmt.Sprint(team.ID)}, "new_value": {"  "}}, "cannot be blank", `value="  "`},
		{"sam", "fields", path(goal, "/fields"), url.Values{"field_id": {fmt.Sprint(budget.ID)}, "value": {"lots"}}, "Budget", `value="lots"`},
	} {
		resp := postForm(t, clients[tc.who], ts.URL+tc.action, tc.sent)
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: refused submit is %d, want %d", tc.form, resp.StatusCode, http.StatusUnprocessableEntity)
			continue
		}
		open := openForm(t, page, tc.form)
		reason := pageElement(t, open, "p", "form-error")
		if !strings.Contains(html.UnescapeString(reason), tc.reason) {
			t.Errorf("%s: open form's error %q doesn't say %q", tc.form, reason, tc.reason)
		}
		if !strings.Contains(open, tc.typed) {
			t.Errorf("%s: open form lost what was typed (%s): %s", tc.form, tc.typed, open)
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
	opened := func(g domain.Goal, form string) string {
		return openForm(t, getBody(t, adaClient, fmt.Sprintf("%s/goals/%d?open=%s", ts.URL, g.ID, form)), form)
	}

	assertConfirms(t, "Mark departed", tagAround(t, opened(goal, "depart"), fmt.Sprintf(`action="/accounts/%d/depart"`, sam.ID)))
	assertConfirms(t, "Mark returned", tagAround(t, opened(orphan, "return"), fmt.Sprintf(`action="/accounts/%d/return"`, kim.ID)))
	assertSubmitsAtOnce(t, "Hand off", tagAround(t, opened(goal, "handoff"), "/handoff"))
	assertSubmitsAtOnce(t, "Reassign", tagAround(t, opened(orphan, "reassign"), "/reassign"))
	assertSubmitsAtOnce(t, "Top-level", tagAround(t, opened(goal, "top-level"), "/top-level"))
	for _, form := range []string{"depart", "handoff"} {
		if nested := nestedControls(getBody(t, adaClient, fmt.Sprintf("%s/goals/%d?open=%s", ts.URL, goal.ID, form))); len(nested) > 0 {
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
		{"dated", onGoal("dated"), url.Values{"delivery_date": {"2026-09-14"}}},
		{"ongoing", onGoal("ongoing"), nil},
		{"cadence", onGoal("cadence"), url.Values{"cadence_days": {"14"}}},
		{"add metric", onGoal("metrics"), url.Values{
			"name": {"Error rate"}, "unit": {"%"}, "direction": {"down"},
			"baseline": {"5"}, "target": {"1"}, "target_date": {"2026-06-15"},
		}},
		{"edit metric", func(_, metricID int64) string { return fmt.Sprintf("/metrics/%d", metricID) }, url.Values{
			"name": {"p99 checkout latency"}, "unit": {"ms"}, "direction": {"down"},
			"baseline": {"1500"}, "target": {"500"}, "target_date": {"2026-06-15"},
		}},
		{"activate", onGoal("activate"), nil},
		{"so what", onGoal("so-what"), url.Values{"so_what": {"Faster checkout lifts conversion."}}},
		{"contributors", onGoal("contributors"), url.Values{"email": {"dana@example.com"}}},
	}
}

// ownerOnlyGoal arranges a Proposed Dated Goal with a Metric, so it could be
// activated, and returns it with the Metric's id.
func ownerOnlyGoal(t *testing.T, h *testsupport.Harness, owner domain.Account) (domain.Goal, int64) {
	t.Helper()
	ctx := context.Background()
	g := h.CreateGoal(owner, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalDated(ctx, g.ID, time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	m, err := h.Service.AddMetric(ctx, domain.AddMetricInput{
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
	contributors, err := h.Service.ListContributors(ctx, goalID)
	if err != nil {
		t.Fatalf("ListContributors: %v", err)
	}
	metrics, err := h.Service.ListMetrics(ctx, goalID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	revisions, err := h.Service.ListSoWhatRevisions(ctx, goalID)
	if err != nil {
		t.Fatalf("ListSoWhatRevisions: %v", err)
	}
	return fmt.Sprintf("goal %+v\ncontributors %+v\nmetrics %+v\nrevisions %+v", g, contributors, metrics, revisions)
}

// Setting a Goal's Kind, delivery date, cadence, So What, Contributors and
// Metrics, and Activating it, are the Owner's alone: anyone else, a Delegate
// included, is refused with 403 and the Goal is left as it was (#191).
func TestOnlyTheOwnerSetsUpAGoalOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	delegate := h.SignIn("tpm@example.com")
	h.SignIn("other@example.com")
	h.SignIn("dana@example.com")
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
	h.SignIn("dana@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")

	for _, route := range ownerOnlyRoutes() {
		g, metricID := ownerOnlyGoal(t, h, owner)
		before := ownerOnlyGoalState(t, h, g.ID)
		resp := postForm(t, client, ts.URL+route.path(g.ID, metricID), route.form)
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
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s on a missing id with form %v: status = %d, want 404", route.name, form, resp.StatusCode)
			}
		}
	}
}
