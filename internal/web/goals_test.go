package web_test

import (
	"context"
	"fmt"
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

// Only the Owner (or an Admin) sets a Goal's Dimension values: another signed-in
// person posting to the endpoint is refused with 403 and the value is unchanged.
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

// Searching the Goal list keeps the Goals whose title or Owner's email holds the
// text, ignoring case, and the search box keeps what was typed.
func TestGoalListSearchesTitleAndOwner(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
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
		`Owner <strong data-testid="goal-owner">sam@example.com</strong>`,
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
	for _, adminOnly := range []string{"Mark Top-level", "Mark owner departed…", "Reassign"} {
		if strings.Contains(moreMenu(t, page), adminOnly) {
			t.Errorf("an Owner who isn't an Admin is offered %q", adminOnly)
		}
	}
}

// An Admin's More menu adds Mark Top-level and Mark owner departed…, which asks
// for confirmation; on an Ownerless Goal it offers Reassign instead of Hand off.
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

	page = getBody(t, adaClient, fmt.Sprintf("%s/goals/%d", ts.URL, orphan.ID))
	assertMenuReaches(t, page, "Reassign", fmt.Sprintf("/goals/%d/reassign", orphan.ID))
	if strings.Contains(moreMenu(t, page), "Hand off") {
		t.Errorf("an Ownerless Goal offers Hand off")
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

// The Goal page's history — Check-ins, Date Slips and So What revisions — sits
// under History, each collapsed with its count in the summary.
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
