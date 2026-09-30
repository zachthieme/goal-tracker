package web_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
	if body := readBody(t, resp); !strings.Contains(body, "boss@example.com") {
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
