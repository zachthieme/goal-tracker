package web_test

import (
	"context"
	"fmt"
	"html"
	"io"
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

func postForm(t *testing.T, client *http.Client, rawURL string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawURL, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// findGoalLink pulls the first /goals/<id> href out of the rendered list.
func findGoalLink(t *testing.T, body string) string {
	t.Helper()
	const marker = `href="/goals/`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no goal link in body:\n%s", body)
	}
	rest := body[i+len(`href="`):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("malformed goal link in body:\n%s", body)
	}
	return rest[:end]
}

// TestSmokeDefineAndActivateGoal is the define-and-activate HTTP smoke test: an
// Owner marks a Proposed Goal Dated, adds a Milestone and a Metric, sets the
// cadence, revises the So What, adds a Contributor, and activates it — then sees
// it rendered Active with everything it filled in.
func TestSmokeDefineAndActivateGoal(t *testing.T) {
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

	edit := between(t, getBody(t, samClient, goalURL), `<details id="edit-dimensions"`, "</details>")
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
	if !strings.Contains(section, fmt.Sprintf(`value="%d" checked`, core.ID)) {
		t.Errorf("Core's checkbox isn't checked:\n%s", section)
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

// Creating a Goal under a parent offers the parent's values as defaults: a kept
// default is assigned to the child and the child contributes to the parent, but
// the values are not inherited — a child created without them carries none
// (CONTEXT.md: the parent's values are offered as defaults, not inherited).
func TestCreateChildGoalOffersParentDefaults(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	growth := pillar.Values[0]
	parent := h.CreateGoal(sam, "Parent", "Parent matters.")
	h.AssignGoalValue(parent, growth)
	ts := newServer(t, h)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	parentURL := fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID)

	// The parent page offers Growth as a checked default.
	page := getBody(t, samClient, parentURL)
	if !strings.Contains(page, `data-testid="child-defaults"`) {
		t.Fatalf("parent page missing child defaults; body:\n%s", page)
	}
	if !strings.Contains(page, fmt.Sprintf(`value="%d" checked`, growth.ID)) {
		t.Errorf("parent value not offered as a checked default; body:\n%s", page)
	}

	// Keeping the default assigns Growth to the child and links it to the parent.
	resp := postForm(t, samClient, parentURL+"/children", url.Values{
		"title":    {"Kept child"},
		"so_what":  {"Child matters."},
		"value_id": {fmt.Sprintf("%d", growth.ID)},
	})
	childPage := readBody(t, resp)
	if !strings.Contains(childPage, `data-testid="goal-dimension-value">Growth`) {
		t.Errorf("kept default not assigned to the child; body:\n%s", childPage)
	}
	if !strings.Contains(childPage, "Parent") {
		t.Errorf("child does not contribute to the parent; body:\n%s", childPage)
	}

	// Creating a child without the default leaves it unassigned (not inherited).
	resp = postForm(t, samClient, parentURL+"/children", url.Values{
		"title":   {"Bare child"},
		"so_what": {"Child matters."},
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
	parentURL := fmt.Sprintf("%s/goals/%d", ts.URL, parent.ID)

	// The form is loaded while Growth is still live, then Growth is retired.
	if page := getBody(t, samClient, parentURL); !strings.Contains(page, fmt.Sprintf(`value="%d" checked`, growth.ID)) {
		t.Fatalf("parent page does not offer Growth as a default; body:\n%s", page)
	}
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, growth.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	resp := postForm(t, samClient, parentURL+"/children", url.Values{
		"title":    {"GrandKid"},
		"so_what":  {"Feeds the parent."},
		"value_id": {fmt.Sprintf("%d", core.ID), fmt.Sprintf("%d", growth.ID)},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422; body:\n%s", resp.StatusCode, body)
	}
	form := pageElement(t, body, "section", "add-child-goal")
	if !strings.Contains(form, "retired") {
		t.Errorf("child form does not say the value is retired; form:\n%s", form)
	}
	if !strings.Contains(form, `value="GrandKid"`) || !strings.Contains(form, "Feeds the parent.") {
		t.Errorf("child form lost the title or So What; form:\n%s", form)
	}
	if !strings.Contains(form, fmt.Sprintf(`value="%d" checked`, core.ID)) {
		t.Errorf("child form lost the kept Core default; form:\n%s", form)
	}

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
// from struck through, or a dash for an Ongoing Goal, and how long ago its last
// Check-in was.
func TestGoalListShowsDueDateAndLastCheckin(t *testing.T) {
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

	rows := goalRows(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals"))
	if got := rowTitles(rows, slipped, ongoing); !slices.Equal(got, []string{"Slipped", "Keep the lights on"}) {
		t.Fatalf("rows = %q", got)
	}

	if due := strings.Join(strings.Fields(pageElement(t, rows[0], "td", "goal-row-due")), " "); !strings.Contains(due, "<del>2026-07-02</del> 2026-07-16") {
		t.Errorf("slipped Goal's Due does not strike its prior date: %s", due)
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

// The Goal list's header names the page and offers New goal, which opens the
// propose form; the form still swaps the list in place under htmx. The Risks
// page has replaced the links to the signal pages.
func TestGoalListHeaderOpensProposeFormThatSwapsTheList(t *testing.T) {
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
	propose := pageElement(t, page, "details", "propose-goal")
	if summary := pageElement(t, propose, "summary", "new-goal"); !strings.Contains(openTag(summary), `class="btn primary"`) || !strings.Contains(summary, "New goal") {
		t.Errorf("New goal is not a primary button: %s", summary)
	}
	for _, want := range []string{`hx-post="/goals"`, `hx-target="#goal-list"`, `method="post"`, `action="/goals"`, `name="title"`, `name="so_what"`} {
		if !strings.Contains(propose, want) {
			t.Errorf("propose form lacks %s: %s", want, propose)
		}
	}
	for _, gone := range []string{`href="/signals"`, `href="/freshness"`} {
		if strings.Contains(page, gone) {
			t.Errorf("Goal list still links %s", gone)
		}
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/goals", strings.NewReader(url.Values{
		"title":   {"Cut checkout latency"},
		"so_what": {"Shoppers abandon slow carts."},
	}.Encode()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /goals: %v", err)
	}
	swap := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(swap, `<div id="goal-list"`) {
		t.Fatalf("htmx create should answer with the list alone; status %d, body:\n%s", resp.StatusCode, swap)
	}
	if rows := goalRows(t, swap); len(rows) != 1 || !strings.Contains(rows[0], "Cut checkout latency") {
		t.Errorf("swapped list lacks the new Goal's row:\n%s", swap)
	}
}

// The Goal page opens on a header a reader takes in at a glance: badges for
// Health, Lifecycle, Kind, Top-level and the Goal's Dimension values, the title,
// and a meta line naming the Owner, the delivery date with its slips struck,
// and the cadence. Check in and No change sit top right for whoever may check
// in.
func TestGoalPageHeaderSummarizesTheGoal(t *testing.T) {
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
	badges := pageElement(t, head, "div", "goal-badges")
	for _, want := range []string{
		`class="badge y"`, "Yellow",
		`data-testid="goal-lifecycle">Active<`,
		`data-testid="goal-kind">Dated<`,
		`data-testid="goal-top-level"`,
		`class="tag">Growth<`,
	} {
		if !strings.Contains(badges, want) {
			t.Errorf("header badges lack %s: %s", want, badges)
		}
	}
	if title := pageElement(t, head, "h1", "goal-title"); !strings.Contains(title, "Reduce outages") {
		t.Errorf("header title: %s", title)
	}
	meta := strings.Join(strings.Fields(pageElement(t, head, "p", "goal-meta")), " ")
	for _, want := range []string{
		`Owner <strong data-testid="goal-owner">` + shownAs("sam@example.com", "sam") + `</strong>`,
		`Delivers <span data-testid="goal-delivery-date"><del>2026-07-02</del> <strong>2026-07-16</strong></span>`,
		`Checks in <span data-testid="goal-cadence">every 7 days</span>`,
	} {
		if !strings.Contains(meta, want) {
			t.Errorf("meta line lacks %s: %s", want, meta)
		}
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

// At phone width the Goal page header wraps its actions onto their own line
// rather than squeezing the title and meta into a column a few letters wide:
// the summary claims a real width before the row's leftover space is shared,
// so it no longer fits beside Check in, No change and More (#42).
func TestGoalPageHeaderWrapsActionsAtPhoneWidthOverHTTP(t *testing.T) {
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

// cssRule returns the declarations of the page's style rule for selector.
func cssRule(t *testing.T, page, selector string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{([^}]*)\}`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("page has no %s rule", selector)
	}
	return m[1]
}

// The Goal page's Milestones are a table of status badge, name and date, the
// date's slips struck, headed by the Goal's slip count and Milestone Churn.
func TestGoalPageMilestonesTable(t *testing.T) {
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
	row := pageElement(t, section, "tr", "goal-milestone")
	for _, want := range []string{`class="badge lc" data-testid="milestone-status">Planned<`, "<td>Beta</td>", "2026-04-02"} {
		if !strings.Contains(row, want) {
			t.Errorf("Milestone row lacks %s: %s", want, row)
		}
	}
}

// moreMenu returns the Goal page's More menu: from its <details> to the end of
// the header, where it sits last.
func moreMenu(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `data-testid="goal-more"`, "</header>")
}

// assertMenuReaches checks the More menu offers label and that it opens a form
// posting to action — inline in the menu, or in the collapsed section the item
// links to lower on the page.
func assertMenuReaches(t *testing.T, page, label, action string) {
	t.Helper()
	menu := moreMenu(t, page)
	at := strings.Index(menu, ">"+label+"<")
	if at < 0 {
		t.Errorf("More menu lacks %q:\n%s", label, menu)
		return
	}
	item := between(t, menu[strings.LastIndex(menu[:at], "<li"):], "", "</li>")
	if i := strings.Index(item, `href="#`); i >= 0 {
		id := item[i+len(`href="#`):]
		id = id[:strings.IndexByte(id, '"')]
		section := between(t, page, `<details id="`+id+`"`, "</details>")
		if !strings.Contains(section, `action="`+action+`"`) {
			t.Errorf("%q links to #%s, which has no form posting to %s:\n%s", label, id, action, section)
		}
		return
	}
	if !strings.Contains(item, `action="`+action+`"`) {
		t.Errorf("%q does not open a form posting to %s:\n%s", label, action, item)
	}
}

// Every action an Owner takes on their Goal sits in the header's More menu,
// either inline or as a link to its collapsed form lower on the page.
func TestGoalPageMoreMenuHoldsOwnerActions(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	h.CreateDimension(ada, "Pillar", "Growth")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if summary := between(t, moreMenu(t, page), "<summary", "</summary>"); !strings.Contains(summary, "More ▾") {
		t.Errorf("the menu is not headed More ▾: %s", summary)
	}
	base := fmt.Sprintf("/goals/%d", goal.ID)
	assertMenuReaches(t, page, "Hand off", base+"/handoff")
	assertMenuReaches(t, page, "Add a delegate", base+"/delegates")
	assertMenuReaches(t, page, "Link to a parent goal", base+"/links")
	assertMenuReaches(t, page, "Add a child goal", base+"/children")
	assertMenuReaches(t, page, "Edit Dimension values", base+"/dimensions")
	for _, adminOnly := range []string{"Mark Top-level", "Mark owner departed…", "Mark returned…", "Reassign"} {
		if strings.Contains(moreMenu(t, page), adminOnly) {
			t.Errorf("an Owner who isn't an Admin is offered %q", adminOnly)
		}
	}
}

// An Admin's More menu adds Mark Top-level and Mark owner departed…, which asks
// for confirmation; on an Ownerless Goal it offers Reassign instead of Hand off,
// and Mark returned…, which also asks for confirmation.
func TestGoalPageMoreMenuHoldsAdminActions(t *testing.T) {
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

	page := getBody(t, adaClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	base := fmt.Sprintf("/goals/%d", goal.ID)
	assertMenuReaches(t, page, "Hand off", base+"/handoff")
	assertMenuReaches(t, page, "Mark Top-level", base+"/top-level")
	assertMenuReaches(t, page, "Mark owner departed…", fmt.Sprintf("/accounts/%d/depart", sam.ID))
	if depart := openTag(between(t, moreMenu(t, page), fmt.Sprintf(`action="/accounts/%d/depart"`, sam.ID), "")); !strings.Contains(depart, `onsubmit="return confirm(`) {
		t.Errorf("Mark departed doesn't ask for confirmation: %s", depart)
	}
	if nested := nestedControls(page); len(nested) > 0 {
		t.Errorf("the Admin's Goal page nests a control inside another: %q", nested)
	}

	if strings.Contains(moreMenu(t, page), "Mark returned…") {
		t.Errorf("a present Owner's Goal offers Mark returned…")
	}

	page = getBody(t, adaClient, fmt.Sprintf("%s/goals/%d", ts.URL, orphan.ID))
	assertMenuReaches(t, page, "Reassign", fmt.Sprintf("/goals/%d/reassign", orphan.ID))
	assertMenuReaches(t, page, "Mark returned…", fmt.Sprintf("/accounts/%d/return", kim.ID))
	if ret := openTag(between(t, moreMenu(t, page), fmt.Sprintf(`action="/accounts/%d/return"`, kim.ID), "")); !strings.Contains(ret, `onsubmit="return confirm(`) {
		t.Errorf("Mark returned doesn't ask for confirmation: %s", ret)
	}
	if nested := nestedControls(page); len(nested) > 0 {
		t.Errorf("an Ownerless Goal's page nests a control inside another: %q", nested)
	}
	if strings.Contains(moreMenu(t, page), "Hand off") {
		t.Errorf("an Ownerless Goal offers Hand off")
	}
}

// A Departed Delegate stays listed among the Goal's Delegates, marked departed;
// a present one isn't marked (CONTEXT.md: Departed).
func TestGoalPageMarksADepartedDelegate(t *testing.T) {
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

// Someone who neither Owns the Goal nor is an Admin can only add a child Goal
// from the More menu.
func TestGoalPageMoreMenuForOthersOffersOnlyAChildGoal(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "mel@example.com"), fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	assertMenuReaches(t, page, "Add a child goal", fmt.Sprintf("/goals/%d/children", goal.ID))
	for _, other := range []string{"Hand off", "Add a delegate", "Link to a parent goal", "Edit Dimension values", "Mark Top-level"} {
		if strings.Contains(moreMenu(t, page), other) {
			t.Errorf("a non-Owner is offered %q", other)
		}
	}
	if strings.Contains(page, `data-testid="checkin-link"`) {
		t.Errorf("a non-Owner is offered Check in")
	}
}

// The sidebar lists the Goals this one contributes to and those contributing to
// it, each with its Health, and removing a link asks for confirmation first. A
// breadcrumb leads back through Goals and the first parent.
func TestGoalPageSidebarLinksCarryHealthAndConfirmRemove(t *testing.T) {
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
		if remove := openTag(between(t, section, `/remove"`, "")); !strings.Contains(remove, `onsubmit="return confirm(`) {
			t.Errorf("%s: Remove doesn't ask for confirmation: %s", tc.section, remove)
		}
	}
}

// Each Metric is a card: its current value large, "baseline → target by date",
// and a sparkline of its readings with a dashed target line, labelled for a
// screen reader with the trend. The Owner edits it in a collapsed section.
func TestGoalPageMetricCardsShowASparkline(t *testing.T) {
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

// The Goal page's history — Check-ins, Date Slips, So What revisions and
// ownership changes — sits under History, each collapsed with its count in the
// summary.
func TestGoalPageHistoryIsCollapsedWithCounts(t *testing.T) {
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
	history := between(t, page, `data-testid="goal-history"`, "</aside>")
	for _, tc := range []struct{ testID, summary string }{
		{"goal-checkin-history", "Check-in history (2)"},
		{"goal-date-slips", "Date Slips (0)"},
		{"goal-so-what-history", "So What history (2)"}, // the original and the edit
		{"goal-ownership-history", "Ownership history (0)"},
	} {
		section := pageElement(t, history, "section", tc.testID)
		details := between(t, section, "<details", "</summary>")
		if strings.Contains(openTag(details), "open") {
			t.Errorf("%s is not collapsed: %s", tc.testID, openTag(details))
		}
		if !strings.Contains(details, tc.summary) {
			t.Errorf("%s summary lacks %q: %s", tc.testID, tc.summary, details)
		}
	}
	if !strings.Contains(pageElement(t, history, "section", "goal-checkin-history"), `data-testid="checkin-history"`) {
		t.Errorf("Check-in history section lacks the history list")
	}
}

// The Latest status card leads the main column with the latest Check-in's
// Health, status and Path to Green, and who wrote it and when.
func TestGoalPageLatestStatusCard(t *testing.T) {
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

// An Ongoing Goal needs a Metric to activate, and a Milestone alone doesn't
// satisfy its checklist.
func TestOngoingGoalChecklistNeedsAMetric(t *testing.T) {
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

// shownAs is how a page shows a person: by label, on a control that expands
// their email inline beside it, with the email on hover too (CONTEXT.md: Name).
func shownAs(emailAddr, label string) string {
	return `<span class="person"><button type="button" class="disclose" aria-expanded="false" title="` + emailAddr + `">` + label +
		`</button><span class="person-email" hidden>` + emailAddr + `</span></span>`
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
// wording is the one the hover title always carried.
func TestGoalPageExplainsSoWhatAndTopLevelOnActivation(t *testing.T) {
	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	g := h.MarkTopLevel(ada, h.ActiveGoal(ada, "Grow revenue", "It pays for everything."))
	page := getBody(t, signInClient(t, ts.URL, ada.Email), fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))

	for _, tc := range []struct{ label, control, text string }{
		{"So What", `<h3><button type="button" class="disclose help"`, "The customer problem this Goal addresses and what is expected to change when it succeeds."},
		{"Top-level", `<button type="button" class="badge lc disclose help" data-testid="goal-top-level"`, "One of the org's root outcomes"},
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
	disclosureScript(t, page)
}

// With a seeded org, every page that shows people shows them by Name with
// their email a click, tap or keypress away, never an email as text until
// asked for; the Print view, with no hover or controls, introduces them as
// Name (email) (CONTEXT.md: Name).
func TestSeededPagesShowPeopleByName(t *testing.T) {
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
	def := h.SaveReportDefinition(admin, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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

	edit := between(t, getBody(t, samClient, goalURL), `<details id="edit-dimensions"`, "</details>")
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

	if page := getBody(t, deeClient, goalURL); !strings.Contains(page, `<details id="edit-dimensions"`) || !strings.Contains(page, `href="#edit-dimensions"`) {
		t.Fatalf("Delegate's Goal page lacks the Dimension value controls:\n%s", page)
	}
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

	if page := getBody(t, coryClient, goalURL); strings.Contains(page, `<details id="edit-dimensions"`) {
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
		if first := strings.TrimSpace(block[len(openTag(block))+1:]); !strings.HasPrefix(first, tc.heading) {
			t.Errorf("%s does not open with %s: %.80s", tc.testID, tc.heading, first)
		}
	}
}

// A --color-border rule separates the Goal page's canvas blocks from the block
// above them.
func TestGoalPageCanvasBlocksSitUnderARule(t *testing.T) {
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
// a value and its values aren't child-Goal defaults. A Goal carrying a value in
// it still shows the value, marked retired, and one carrying none doesn't list
// it. Restoring the Dimension returns its control (CONTEXT.md: Retired).
func TestRetiredDimensionLeavesGoalPageControlsOverHTTP(t *testing.T) {
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

	page := getBody(t, client, goalURL)
	shown := between(t, page, `<section data-testid="goal-dimensions"`, `id="edit-dimensions"`)
	if !strings.Contains(shown, `data-testid="goal-dimension-value">Growth`) || !strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("Growth isn't shown marked retired:\n%s", shown)
	}
	if strings.Contains(shown, "Quarter") {
		t.Errorf("a Retired Dimension the Goal carries no value in is listed:\n%s", shown)
	}
	edit := between(t, page, `id="edit-dimensions"`, "</details>")
	for _, d := range []domain.Dimension{pillar, quarter} {
		if strings.Contains(edit, d.Name) {
			t.Errorf("the Retired %s is offered for setting:\n%s", d.Name, edit)
		}
	}
	if !strings.Contains(edit, "Team") {
		t.Errorf("the live Team isn't offered for setting:\n%s", edit)
	}
	if strings.Contains(page, `data-testid="child-defaults"`) {
		t.Errorf("a value of a Retired Dimension is offered as a child default:\n%s", pageElement(t, page, "fieldset", "child-defaults"))
	}

	if err := h.Service.RestoreDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RestoreDimension: %v", err)
	}
	page = getBody(t, client, goalURL)
	shown = between(t, page, `<section data-testid="goal-dimensions"`, `id="edit-dimensions"`)
	if strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("Growth is still marked retired after Pillar is restored:\n%s", shown)
	}
	if edit := between(t, page, `id="edit-dimensions"`, "</details>"); !strings.Contains(edit, `aria-label="Pillar"`) {
		t.Errorf("the restored Pillar isn't offered for setting:\n%s", edit)
	}
	if !strings.Contains(page, `data-testid="child-defaults"`) {
		t.Errorf("Growth isn't offered as a child default once Pillar is restored")
	}
}

// A Retired Dimension drops out of the Goal list's filter and grouping, and a
// stale link filtering or grouping by it is ignored rather than narrowing the
// list with no control to undo it. A Goal's row still shows its value, marked
// retired. Restoring the Dimension returns it to both (CONTEXT.md: Retired).
func TestRetiredDimensionLeavesGoalListFilterAndGroupingOverHTTP(t *testing.T) {
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

// Proposing a Goal from the table layout swaps the table back in, not the list
// layout, with the new Goal in it.
func TestProposeGoalFromTheTableKeepsTheTable(t *testing.T) {
	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals?layout=table&sort=title")
	form := between(t, page, `<details data-testid="propose-goal"`, "</details>")
	target := html.UnescapeString(attr(openTag(between(t, form, "<form", "")), "hx-post"))
	body, status := postFormHX(t, client, ts.URL+target, url.Values{"title": {"Cut latency"}, "so_what": {"Slow carts."}})
	if status != http.StatusOK {
		t.Fatalf("propose from the table: status %d", status)
	}
	if !strings.Contains(body, `<table data-testid="goal-table"`) || !strings.Contains(body, ">Cut latency<") {
		t.Errorf("proposing from the table didn't swap in the table with the new Goal:\n%s", body)
	}
}

// Every change to a Goal's Dimension values and Fields made through the Goal
// page is listed oldest first under its collapsed Value history, each with who
// made it by Name and when. A Delegate's change is attributed to the Delegate,
// a value added or removed in a several-values Dimension reads as "added X" or
// "removed X", a save that changes nothing adds no entry, and renaming a value
// afterwards leaves the entries reading as they did.
func TestGoalPageListsValueHistory(t *testing.T) {
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

	page := getBody(t, patClient, goalPageURL(ts.URL, goal))
	section := pageElement(t, pageElement(t, page, "div", "goal-history"), "section", "goal-value-history")
	details := between(t, section, "<details", "</summary>")
	if strings.Contains(openTag(details), "open") {
		t.Errorf("Value history is not collapsed: %s", openTag(details))
	}
	if !strings.Contains(details, "Value history (7)") {
		t.Errorf("Value history summary lacks its count of 7: %s", details)
	}
	patShown, deeShown := shownAs(pat.Email, "Pat Owner"), shownAs(dee.Email, "Dee Delegate")
	want := []struct{ change, by, at string }{
		{"Budget: set to 200", patShown, "2026-01-02 15:04"},
		{"Budget: 200 → 350", deeShown, "2026-01-02 16:04"},
		{"Pillar: set to Growth", patShown, "2026-01-02 18:04"},
		{"Region: added EMEA", patShown, "2026-01-02 20:04"},
		{"Region: removed EMEA", deeShown, "2026-01-02 21:04"},
		{"Region: added APAC", deeShown, "2026-01-02 21:04"},
		{"Budget: cleared (was 350)", patShown, "2026-01-02 22:04"},
	}
	entries := strings.Split(section, `data-testid="value-change"`)[1:]
	if len(entries) != len(want) {
		t.Fatalf("Value history has %d entries, want %d:\n%s", len(entries), len(want), section)
	}
	for i, w := range want {
		entry := html.UnescapeString(entries[i])
		for _, part := range []string{w.change, w.by, w.at} {
			if !strings.Contains(entry, html.UnescapeString(part)) {
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

// tableForm is what a browser would post from the Goal table's edit form as
// page renders it, and where to.
func tableForm(t *testing.T, page string) (string, url.Values) {
	t.Helper()
	form := between(t, page, `<form data-testid="goal-table-form"`, "</form>")
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
	return html.UnescapeString(attr(openTag(form), "action")), values
}

// controlName is the name of the form control on page labelled label.
func controlName(t *testing.T, page, label string) string {
	t.Helper()
	m := regexp.MustCompile(`<(input|select|textarea|fieldset)[^>]*\saria-label="` + regexp.QuoteMeta(html.EscapeString(label)) + `"[^>]*>`).FindString(page)
	if m == "" {
		t.Fatalf("page has no control labelled %q", label)
	}
	if name := attr(m, "name"); name != "" {
		return name
	}
	// A fieldset of checkboxes: the name its boxes share.
	rest := page[strings.Index(page, m):]
	return attr(regexp.MustCompile(`<input[^>]*type="checkbox"[^>]*>`).FindString(rest), "name")
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
