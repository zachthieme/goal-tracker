package web_test

import (
	"context"
	"fmt"
	"html"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The New goal page is a plain form posting to itself: Title, focused, and Why it
// matters.
func TestNewGoalPageOffersTitleAndWhyItMatters(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	form := pageElement(t, page, "form", "goal-form")
	if tag := openTag(form); attr(tag, "method") != "post" || attr(tag, "action") != "/goals/new" {
		t.Errorf("the New goal form doesn't post to /goals/new: %s", tag)
	}
	if title := tagAround(t, form, `name="title"`); !strings.Contains(title, " autofocus") {
		t.Errorf("Title is not focused: %s", title)
	}
	tagAround(t, form, `name="why_it_matters"`)
}

// Submitting the New goal form with a Title and Why it matters creates a Proposed
// Goal the submitter owns and lands on its page, by a plain post with no
// script. No toast: the Goal's page is the confirmation.
func TestNewGoalFormCreatesAProposedGoalAndLandsOnIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, ts.URL+"/goals/new", url.Values{"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."}})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("the post created %d Goals, want 1", len(goals))
	}
	g := goals[0]
	if g.Title != "Cut checkout latency" || g.WhyItMatters != "Shoppers abandon slow carts." || g.Lifecycle != domain.LifecycleProposed || g.Owner.Email != "sam@example.com" {
		t.Errorf("created %+v, want sam's Proposed Goal as typed", g)
	}
	if want := fmt.Sprintf("/goals/%d", g.ID); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != want {
		t.Fatalf("the post landed on %s with status %d, want %s", resp.Request.URL, resp.StatusCode, want)
	}
	if strings.Contains(page, `data-testid="toast"`) {
		t.Errorf("landing on the new Goal shows a toast:\n%s", page)
	}
}

// A refused New goal submit comes back as the form, 422, with what was typed:
// a summary at the top lists each problem, and each bad input is marked
// invalid with its own message beside it. Nothing is created.
func TestNewGoalFormRefusalMarksEachBadInputAndKeepsValues(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	refused := func(form url.Values) string {
		t.Helper()
		resp := postForm(t, client, ts.URL+"/goals/new", form)
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity || resp.Request.URL.Path != "/goals/new" {
			t.Fatalf("posting %v answered %d at %s, want 422 at /goals/new:\n%s", form, resp.StatusCode, resp.Request.URL, page)
		}
		return page
	}

	page := refused(url.Values{"title": {""}, "why_it_matters": {""}})
	summary := pageElement(t, page, "div", "goal-form-errors")
	if !strings.Contains(openTag(summary), `role="alert"`) {
		t.Errorf("the error summary isn't an alert: %s", openTag(summary))
	}
	form := pageElement(t, page, "form", "goal-form")
	for name, message := range map[string]string{"title": "A Goal needs a title.", "why_it_matters": "A Goal needs to say why it matters."} {
		if !strings.Contains(summary, message) {
			t.Errorf("the summary doesn't list %q:\n%s", message, summary)
		}
		input := tagAround(t, form, `name="`+name+`"`)
		if attr(input, "aria-invalid") != "true" {
			t.Errorf("%s isn't marked invalid: %s", name, input)
		}
		described := attr(input, "aria-describedby")
		if described == "" || !strings.Contains(between(t, form, `id="`+described+`"`, "</"), message) {
			t.Errorf("%s isn't described by its message %q:\n%s", name, message, form)
		}
	}

	page = refused(url.Values{"title": {"Cut checkout latency"}, "why_it_matters": {" "}})
	form = pageElement(t, page, "form", "goal-form")
	title := tagAround(t, form, `name="title"`)
	if attr(title, "value") != "Cut checkout latency" || attr(title, "aria-invalid") != "" {
		t.Errorf("Title isn't kept as typed and valid: %s", title)
	}
	if attr(tagAround(t, form, `name="why_it_matters"`), "aria-invalid") != "true" {
		t.Errorf("a blank Why it matters isn't marked invalid:\n%s", form)
	}

	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("refused posts created %d Goals", len(goals))
	}
}

// newGoalForm is a New goal post with a Title and Why it matters, to add rows to.
func newGoalForm(rows map[string]string) url.Values {
	form := url.Values{"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."}}
	for name, value := range rows {
		form.Set(name, value)
	}
	return form
}

// The New goal form's Milestone and Metric rows are saved with the Goal, a
// Metric's direction read from its baseline and target, and a row left blank
// is ignored, wherever it sits.
func TestNewGoalFormSavesMilestoneAndMetricRows(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, ts.URL+"/goals/new", newGoalForm(map[string]string{
		"milestones[0].name": "Profile checkout", "milestones[0].date": "2026-11-02",
		"milestones[1].name": "", "milestones[1].date": "",
		"milestones[2].name": "Ship the fix", "milestones[2].date": "2026-12-14",
		"metrics[0].name": "", "metrics[0].unit": "", "metrics[0].baseline": "", "metrics[0].target": "", "metrics[0].target_date": "", "metrics[0].direction": "",
		"metrics[1].name": "p95 latency", "metrics[1].unit": "ms", "metrics[1].baseline": "1200", "metrics[1].target": "400", "metrics[1].target_date": "2026-12-31", "metrics[1].direction": "",
		"metrics[2].name": "Conversion", "metrics[2].unit": "%", "metrics[2].baseline": "2.5", "metrics[2].target": "3", "metrics[2].target_date": "2027-03-31", "metrics[2].direction": "down",
	}))
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil || len(goals) != 1 {
		t.Fatalf("ListGoals: %d Goals, %v", len(goals), err)
	}
	ctx := context.Background()
	milestones, err := h.Service.ListMilestones(ctx, goals[0].ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	var gotMilestones []string
	for _, m := range milestones {
		gotMilestones = append(gotMilestones, m.Name+" "+m.TargetDate.Format("2006-01-02"))
	}
	if want := []string{"Profile checkout 2026-11-02", "Ship the fix 2026-12-14"}; fmt.Sprint(gotMilestones) != fmt.Sprint(want) {
		t.Errorf("saved Milestones %q, want %q", gotMilestones, want)
	}
	metrics, err := h.Service.ListMetrics(ctx, goals[0].ID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	var gotMetrics []string
	for _, m := range metrics {
		gotMetrics = append(gotMetrics, fmt.Sprintf("%s %s %s %v→%v %s", m.Name, m.Unit, m.Direction, m.Baseline, m.Target, m.TargetDate.Format("2006-01-02")))
	}
	// The Direction select only counts when baseline and target are equal.
	if want := []string{"p95 latency ms down 1200→400 2026-12-31", "Conversion % up 2.5→3 2027-03-31"}; fmt.Sprint(gotMetrics) != fmt.Sprint(want) {
		t.Errorf("saved Metrics %q, want %q", gotMetrics, want)
	}
}

// refusedNewGoal posts the New goal form, wanting it refused: the form back,
// 422, at /goals/new.
func refusedNewGoal(t *testing.T, client *http.Client, baseURL string, form url.Values) string {
	t.Helper()
	resp := postForm(t, client, baseURL+"/goals/new", form)
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity || resp.Request.URL.Path != "/goals/new" {
		t.Fatalf("posting %v answered %d at %s, want 422 at /goals/new:\n%s", form, resp.StatusCode, resp.Request.URL, page)
	}
	return pageElement(t, page, "form", "goal-form")
}

// wantRefused checks the named input is marked invalid and described by its
// message, which says want.
func wantRefused(t *testing.T, form, name, want string) {
	t.Helper()
	input := tagAround(t, form, `name="`+name+`"`)
	if attr(input, "aria-invalid") != "true" {
		t.Errorf("%s isn't marked invalid: %s", name, input)
	}
	described := attr(input, "aria-describedby")
	if described == "" || !strings.Contains(html.UnescapeString(between(t, form, `id="`+described+`"`, "</")), want) {
		t.Errorf("%s isn't described by a message saying %q:\n%s", name, want, form)
	}
}

// A Metric whose target equals its baseline holds steady, so its direction is
// the one chosen in its Direction select; with none chosen, it is refused
// there.
func TestNewGoalFormHoldSteadyMetricTakesTheChosenDirection(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	holdSteady := func(direction string) url.Values {
		return newGoalForm(map[string]string{
			"metrics[0].name": "Error rate", "metrics[0].unit": "%", "metrics[0].baseline": "0.5", "metrics[0].target": "0.5",
			"metrics[0].target_date": "2026-12-31", "metrics[0].direction": direction,
		})
	}

	form := refusedNewGoal(t, client, ts.URL, holdSteady(""))
	wantRefused(t, form, "metrics[0].direction", `A Metric needs a direction of "up" or "down".`)
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Fatalf("a refused post created %d Goals", len(goals))
	}

	resp := postForm(t, client, ts.URL+"/goals/new", holdSteady("down"))
	defer func() { _ = resp.Body.Close() }()
	if page := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("the post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	goals, _ := h.Service.ListGoals(context.Background())
	if len(goals) != 1 {
		t.Fatalf("the post created %d Goals, want 1", len(goals))
	}
	metrics, err := h.Service.ListMetrics(context.Background(), goals[0].ID)
	if err != nil || len(metrics) != 1 || metrics[0].Direction != domain.MetricDown {
		t.Errorf("saved Metrics %+v (%v), want one holding steady, down", metrics, err)
	}
}

// A row's date or number that doesn't parse is refused under its input, in the
// same 422 as the domain's problems, and every row comes back as typed. A row
// with anything in it is a row, so its blank required inputs are refused too.
func TestNewGoalFormRefusesRowValuesThatDontParseKeepingTheRest(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := refusedNewGoal(t, client, ts.URL, url.Values{
		"title": {"Cut checkout latency"}, "why_it_matters": {""},
		"milestones[0].name": {"Profile checkout"}, "milestones[0].date": {"2026-11-02"},
		"milestones[1].name": {"Ship the fix"}, "milestones[1].date": {"next week"},
		"metrics[0].name": {"p95 latency"}, "metrics[0].unit": {"ms"}, "metrics[0].baseline": {"lots"}, "metrics[0].target": {"400"}, "metrics[0].target_date": {"2026-12-31"},
		"metrics[1].name": {"Conversion"}, "metrics[1].unit": {""}, "metrics[1].baseline": {""}, "metrics[1].target": {"3"}, "metrics[1].target_date": {""},
	})
	wantRefused(t, form, "why_it_matters", "A Goal needs to say why it matters.")
	wantRefused(t, form, "milestones[1].date", `"next week" isn't a date.`)
	wantRefused(t, form, "metrics[0].baseline", `"lots" isn't a number for the baseline.`)
	wantRefused(t, form, "metrics[1].unit", "A Metric needs a unit.")
	wantRefused(t, form, "metrics[1].baseline", "A Metric needs a baseline.")
	wantRefused(t, form, "metrics[1].target_date", "A Metric needs a target date.")
	for name, value := range map[string]string{
		"milestones[0].name": "Profile checkout", "milestones[0].date": "2026-11-02",
		"milestones[1].name": "Ship the fix", "milestones[1].date": "next week",
		"metrics[0].name": "p95 latency", "metrics[0].target": "400", "metrics[0].target_date": "2026-12-31",
		"metrics[1].name": "Conversion", "metrics[1].target": "3",
	} {
		input := tagAround(t, form, `name="`+name+`"`)
		if attr(input, "value") != value {
			t.Errorf("%s came back as %s, want %q", name, input, value)
		}
		if name != "milestones[1].date" && attr(input, "aria-invalid") != "" {
			t.Errorf("%s is marked invalid: %s", name, input)
		}
	}
	// The baseline that didn't parse can't say which way the Metric moves, so
	// its direction isn't refused as well.
	if attr(tagAround(t, form, `name="metrics[0].direction"`), "aria-invalid") != "" {
		t.Errorf("metrics[0].direction is refused though its baseline didn't parse:\n%s", form)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("a refused post created %d Goals", len(goals))
	}
}

// When only the handler refuses a value, the Goal the domain would have
// created is not kept.
func TestNewGoalFormKeepsNothingWhenOnlyAValueDoesntParse(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := refusedNewGoal(t, client, ts.URL, newGoalForm(map[string]string{
		"milestones[0].name": "Profile checkout", "milestones[0].date": "2026-13-45",
	}))
	wantRefused(t, form, "milestones[0].date", `"2026-13-45" isn't a date.`)
	if strings.Contains(form, "A Milestone needs a date") {
		t.Errorf("the date that didn't parse is also refused as missing:\n%s", form)
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("a refused post created %d Goals", len(goals))
	}
}

// How you'll know offers one blank Milestone row and one blank Metric row,
// whose Direction select is always there for when the target equals the
// baseline, plus a <template> of each row and a button to add one.
func TestNewGoalPageOffersMilestoneAndMetricRows(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := pageElement(t, getBody(t, client, ts.URL+"/goals/new"), "form", "goal-form")
	know := pageElement(t, form, "section", "goal-form-know")
	if h2s := headingTexts(know, "h2"); len(h2s) != 1 || h2s[0] != "How you'll know" {
		t.Errorf("the section is headed %q, want How you'll know:\n%s", h2s, know)
	}
	if h3s := headingTexts(know, "h3"); !slices.Equal(h3s, []string{"Milestones", "Metrics"}) {
		t.Errorf("the section's <h3>s read %q, want Milestones and Metrics", h3s)
	}
	for _, name := range []string{
		"milestones[0].name", "milestones[0].date",
		"metrics[0].name", "metrics[0].unit", "metrics[0].baseline", "metrics[0].target", "metrics[0].direction", "metrics[0].target_date",
	} {
		if input := tagAround(t, know, `name="`+name+`"`); attr(input, "value") != "" || strings.Contains(input, " required") {
			t.Errorf("%s isn't a blank, optional input: %s", name, input)
		}
	}
	if strings.Contains(know, `name="milestones[1].`) || strings.Contains(know, `name="metrics[1].`) {
		t.Errorf("the section offers more than one row of a kind:\n%s", know)
	}
	for _, section := range []string{"milestones", "metrics"} {
		template := between(t, know, `<template id="`+section+`-row"`, "</template>")
		if !strings.Contains(template, `name="`+section+`[__i__].name"`) {
			t.Errorf("the %s <template> has no row to clone:\n%s", section, template)
		}
	}
	for section, label := range map[string]string{"milestones": "+ Milestone", "metrics": "+ Metric"} {
		button := between(t, know, `data-add-row="`+section+`"`, "</button>")
		if !strings.Contains(button, label) {
			t.Errorf("no %q button adds a %s row:\n%s", label, section, button)
		}
	}
}

// The New goal form's Contributes to field posts each parent as parent_id,
// and the new Goal is linked to every one: at once to a parent the creator
// owns, and as a request the parent's Owner is asked to accept otherwise.
func TestNewGoalFormContributesToSeveralParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	own := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	theirs := h.CreateGoal(ana, "Faster site", "Speed sells.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := newGoalForm(nil)
	form["parent_id"] = []string{fmt.Sprint(own.ID), fmt.Sprint(theirs.ID)}
	resp := postForm(t, client, ts.URL+"/goals/new", form)
	defer func() { _ = resp.Body.Close() }()
	if page := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path == "/goals/new" {
		t.Fatalf("the post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	var created domain.Goal
	goals, _ := h.Service.ListGoals(context.Background())
	for _, g := range goals {
		if g.Title == "Cut checkout latency" {
			created = g
		}
	}
	if parents := h.ParentsOf(created); len(parents) != 1 || parents[0].ID != own.ID {
		t.Errorf("the new Goal's accepted parents are %+v, want only sam's own %q", parents, own.Title)
	}
	pending, err := h.Service.PendingLinkRequests(context.Background(), ana.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	if len(pending) != 1 || pending[0].Child.ID != created.ID || pending[0].Parent.ID != theirs.ID || pending[0].Status != domain.LinkPending {
		t.Errorf("ana is asked to accept %+v, want one Pending link from the new Goal to %q", pending, theirs.Title)
	}
}

// GET /goals/new?parent=<id> arrives with each parent picked: a chip holding
// its hidden parent_id input, its Health badge (or its Lifecycle when it has
// no Health), its title, and the × that removes it, all in the one element.
func TestNewGoalPageArrivesWithParentsPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	active := h.ActiveGoal(ana, "Faster site", "Speed sells.")
	h.Checkin(ana, active.ID, domain.HealthRed, "Blocked on the CDN.", "Swap CDN vendors.", h.Clock.Now().AddDate(0, 1, 0))
	proposed := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d&parent=%d", ts.URL, active.ID, proposed.ID))
	form := pageElement(t, page, "form", "goal-form")
	chips := strings.Split(between(t, form, `data-testid="parent-chips"`, "</ul>"), `data-testid="parent-chip"`)[1:]
	if len(chips) != 2 {
		t.Fatalf("the page shows %d parent chips, want 2:\n%s", len(chips), form)
	}
	for i, want := range []struct {
		goal  domain.Goal
		badge string
	}{{active, domain.HealthRed}, {proposed, domain.LifecycleProposed}} {
		chip := chips[i]
		input := tagAround(t, chip, `name="parent_id"`)
		if attr(input, "type") != "hidden" || attr(input, "value") != fmt.Sprint(want.goal.ID) {
			t.Errorf("chip %d's input is %s, want a hidden parent_id of %d", i, input, want.goal.ID)
		}
		if !strings.Contains(chip, want.goal.Title) || !strings.Contains(chip, `class="badge`) || !strings.Contains(chip, ">"+want.badge+"</span>") {
			t.Errorf("chip %d doesn't show %q with a %s badge:\n%s", i, want.goal.Title, want.badge, chip)
		}
		if remove := tagAround(t, chip, "data-remove-chip"); !strings.Contains(remove, `type="button"`) {
			t.Errorf("chip %d's × isn't a button: %s", i, remove)
		}
	}
}

// The Contributes to hint names each Owner a picked parent asks to accept:
// with one parent the creator's own and one Ana's, only Ana. Each chip
// carries its Owner's label for script to rebuild the hint from.
func TestNewGoalPageHintNamesOnlyOtherOwners(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignInNamed("sam@example.com", "Sam Reyes")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	own := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	theirs := h.CreateGoal(ana, "Faster site", "Speed sells.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d&parent=%d", ts.URL, own.ID, theirs.ID))
	hint := pageElement(t, page, "p", "parent-hint")
	if got := strings.TrimSpace(hint[strings.Index(hint, ">")+1:]); got != "Each Owner is asked to accept: Ana Torres." {
		t.Errorf("the hint says %q, want only Ana named", got)
	}
	if strings.Contains(openTag(hint), "hidden") {
		t.Errorf("the hint is hidden: %s", openTag(hint))
	}
	if chip := tagAround(t, page, fmt.Sprintf(`id="parent:%d"`, theirs.ID)); attr(chip, "data-asks") != "Ana Torres" {
		t.Errorf("Ana's Goal's chip doesn't carry her label: %s", chip)
	}
	if chip := tagAround(t, page, fmt.Sprintf(`id="parent:%d"`, own.ID)); strings.Contains(chip, "data-asks") {
		t.Errorf("sam's own Goal's chip asks someone: %s", chip)
	}

	page = getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, own.ID))
	if hint := pageElement(t, page, "p", "parent-hint"); !strings.Contains(openTag(hint), " hidden") {
		t.Errorf("with only sam's own parent, the hint isn't hidden: %s", hint)
	}
}

// GET /goals/search?q= answers the Contributes to search: each Goal whose
// title contains q, ignoring case, less the parents already picked, sent as
// parent_id. Each result holds the chip picking it adds.
func TestGoalSearchMatchesTitlesLessThosePicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	faster := h.CreateGoal(ana, "Faster SITE", "Speed sells.")
	picked := h.CreateGoal(sam, "Site reliability", "Outages cost us.")
	newSite := h.CreateGoal(sam, "A new website", "The old one is dated.")
	h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	q := url.Values{"q": {"site"}, "parent_id": {fmt.Sprint(picked.ID)}}
	page := getBody(t, client, ts.URL+"/goals/search?"+q.Encode())
	results := strings.Split(page, `data-testid="parent-result"`)[1:]
	var got []string
	for _, result := range results {
		button := between(t, result, "data-pick", "</button>")
		chip := between(t, result, "<template>", "</template>")
		input := tagAround(t, chip, `name="parent_id"`)
		for _, g := range []domain.Goal{faster, picked, newSite} {
			if strings.Contains(button, g.Title) {
				got = append(got, g.Title)
				if attr(input, "value") != fmt.Sprint(g.ID) {
					t.Errorf("picking %q adds the chip %s", g.Title, chip)
				}
			}
		}
	}
	if want := []string{faster.Title, newSite.Title}; fmt.Sprint(got) != fmt.Sprint(want) && fmt.Sprint(got) != fmt.Sprint([]string{newSite.Title, faster.Title}) {
		t.Errorf("searching %q found %q, want %q", "site", got, want)
	}
}

// The Contributes to search answers at most 20 matches: a query more Goals'
// titles contain finds the first 20 in list order, newest first, each with
// its Health badge when it has one (#499).
func TestGoalSearchAnswersAtMostTwentyMatches(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	for i := range 24 {
		h.CreateGoal(sam, fmt.Sprintf("Site goal %02d", i+1), "Speed sells.")
	}
	newest := h.ActiveGoal(sam, "Site goal 25", "Speed sells.")
	h.Checkin(sam, newest.ID, domain.HealthYellow, "Slower than planned.", "Cache the pages.", h.Clock.Now().AddDate(0, 1, 0))
	h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := parsePage(t, getBody(t, client, ts.URL+"/goals/search?"+url.Values{"q": {"site"}}.Encode()))
	results := findAllByTestID(page, "parent-result")
	var want []string
	for i := 25; i > 5; i-- {
		want = append(want, fmt.Sprintf("Site goal %02d", i))
	}
	if len(results) != len(want) {
		t.Fatalf("searching %q, which 25 Goals match, found %d results, want 20: %q", "site", len(results), textsOf(results))
	}
	for i, r := range results {
		if got := textOf(r); !strings.Contains(got, want[i]) {
			t.Errorf("result %d reads %q, want %s: the newest 20 matches in list order", i+1, got, want[i])
		}
	}
	// The <template> holds the chip a pick adds; the pick is what shows.
	pick := func(r *nethtml.Node) *nethtml.Node {
		return findAll(r, func(n *nethtml.Node) bool { return n.Data == "button" })[0]
	}
	assertHealthChip(t, healthChipIn(t, pick(results[0])), domain.HealthYellow)
	for _, r := range results[1:] {
		if chips := healthChipsIn(pick(r)); len(chips) != 0 {
			t.Errorf("the result %q, a Goal with no Check-in, shows a Health badge: %s", textOf(r), renderNode(r))
		}
	}
}

// Without script, Contributes to is a multiple select named parent_id offering
// every Goal not already picked, and posting its values links the new Goal to
// each, the way the chips' inputs do.
func TestNewGoalPageNoScriptSelectPostsParentIDs(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	picked := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	first := h.CreateGoal(sam, "Faster site", "Speed sells.")
	second := h.CreateGoal(sam, "Fewer outages", "Outages cost us.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, picked.ID))
	sel := pageElement(t, page, "select", "parent-select")
	if tag := openTag(sel); attr(tag, "name") != "parent_id" || !strings.Contains(tag, " multiple") {
		t.Fatalf("the no-script field isn't a multiple select of parent_id: %s", tag)
	}
	var offered []string
	for _, option := range strings.Split(sel, "<option")[1:] {
		offered = append(offered, attr("<option"+option, "value"))
	}
	slices.Sort(offered)
	if want := []string{fmt.Sprint(first.ID), fmt.Sprint(second.ID)}; fmt.Sprint(offered) != fmt.Sprint(want) {
		t.Fatalf("the select offers %q, want every Goal but the one picked, %q", offered, want)
	}

	form := newGoalForm(nil)
	form["parent_id"] = append([]string{fmt.Sprint(picked.ID)}, offered...)
	resp := postForm(t, client, ts.URL+"/goals/new", form)
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path == "/goals/new" {
		t.Fatalf("the post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, body)
	}
	var created domain.Goal
	goals, _ := h.Service.ListGoals(context.Background())
	for _, g := range goals {
		if g.Title == "Cut checkout latency" {
			created = g
		}
	}
	if parents := h.ParentsOf(created); len(parents) != 3 {
		t.Errorf("the new Goal contributes to %+v, want all three it was given", parents)
	}
}

// A refused New goal submit comes back with its parents still picked, as
// chips, and the hint naming whom they ask; a parent_id that isn't a Goal's
// is refused under the field.
func TestNewGoalFormRefusalKeepsParentsPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	theirs := h.CreateGoal(ana, "Faster site", "Speed sells.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := refusedNewGoal(t, client, ts.URL, url.Values{"title": {""}, "why_it_matters": {"Shoppers abandon slow carts."}, "parent_id": {fmt.Sprint(theirs.ID)}})
	chip := between(t, form, `data-testid="parent-chip"`, "</li>")
	if attr(tagAround(t, chip, `name="parent_id"`), "value") != fmt.Sprint(theirs.ID) {
		t.Errorf("the refused form lost its parent chip:\n%s", form)
	}
	if hint := pageElement(t, form, "p", "parent-hint"); !strings.Contains(hint, "Each Owner is asked to accept: Ana Torres.") {
		t.Errorf("the refused form's hint doesn't name Ana: %s", hint)
	}

	form = refusedNewGoal(t, client, ts.URL, newGoalForm(map[string]string{"parent_id": "first"}))
	if !strings.Contains(html.UnescapeString(between(t, form, `id="parent_id-error"`, "</")), `"first" isn't a Goal`) {
		t.Errorf("an unparsed parent_id isn't refused under the field:\n%s", form)
	}
}

// createdGoal posts the New goal form, wanting it to create one Goal, and
// returns that Goal: the newest titled as posted.
func createdGoal(t *testing.T, h *testsupport.Harness, client *http.Client, baseURL string, form url.Values) domain.Goal {
	t.Helper()
	resp := postForm(t, client, baseURL+"/goals/new", form)
	defer func() { _ = resp.Body.Close() }()
	if page := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path == "/goals/new" {
		t.Fatalf("posting %v answered %d at %s:\n%s", form, resp.StatusCode, resp.Request.URL, page)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	var created domain.Goal
	for _, g := range goals {
		if g.Title == form.Get("title") && g.ID > created.ID {
			created = g
		}
	}
	if created.ID == 0 {
		t.Fatalf("no Goal is titled %q among %+v", form.Get("title"), goals)
	}
	return created
}

// Delivery's Kind saves as chosen: Dated with its delivery date, Ongoing with
// none even when a date is posted, and neither leaves the Kind not chosen.
func TestNewGoalFormSavesTheKindChosen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, kind, date string
		wantKind         string
		wantDate         string
	}{
		{name: "dated", kind: "Dated", date: "2027-03-31", wantKind: domain.GoalDated, wantDate: "2027-03-31"},
		{name: "ongoing", kind: "Ongoing", wantKind: domain.GoalOngoing},
		{name: "ongoing ignores a date", kind: "Ongoing", date: "2027-03-31", wantKind: domain.GoalOngoing},
		{name: "not chosen", kind: "", wantKind: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			h.SignIn("sam@example.com")
			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "sam@example.com")

			form := newGoalForm(map[string]string{"delivery_date": tc.date})
			if tc.kind != "" {
				form.Set("kind", tc.kind)
			}
			g := createdGoal(t, h, client, ts.URL, form)
			var date string
			if !g.DeliveryDate.IsZero() {
				date = g.DeliveryDate.Format("2006-01-02")
			}
			if g.Kind != tc.wantKind || date != tc.wantDate {
				t.Errorf("saved Kind %q delivered %q, want %q delivered %q", g.Kind, date, tc.wantKind, tc.wantDate)
			}
		})
	}
}

// Delivery's cadence saves as chosen: each chip's days, or Custom's number
// of days.
func TestNewGoalFormSavesTheCadenceChosen(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		cadence, days string
		want          int
	}{
		{cadence: "7", want: 7},
		{cadence: "14", want: 14},
		{cadence: "30", want: 30},
		{cadence: "custom", days: "10", want: 10},
		{cadence: "custom", days: " 90 ", want: 90},
	} {
		t.Run(tc.cadence+tc.days, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			h.SignIn("sam@example.com")
			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "sam@example.com")

			g := createdGoal(t, h, client, ts.URL, newGoalForm(map[string]string{"cadence": tc.cadence, "cadence_days": tc.days}))
			if g.CadenceDays != tc.want {
				t.Errorf("saved a cadence of %d days, want %d", g.CadenceDays, tc.want)
			}
		})
	}
}

// A Custom cadence that isn't a whole number of days above 0 is refused under
// cadence, beside the days typed, in the same 422 as the domain's problems,
// and every other value comes back as chosen.
func TestNewGoalFormRefusesACustomCadenceThatIsntWholeDaysKeepingTheRest(t *testing.T) {
	t.Parallel()

	for days, want := range map[string]string{
		"0":      `The Check-in cadence must be a whole number of days above 0, not "0".`,
		"-3":     `The Check-in cadence must be a whole number of days above 0, not "-3".`,
		"weekly": `The Check-in cadence must be a whole number of days above 0, not "weekly".`,
		"1.5":    `The Check-in cadence must be a whole number of days above 0, not "1.5".`,
		"":       "A Custom cadence needs a number of days.",
	} {
		t.Run(days, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			h.SignIn("sam@example.com")
			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "sam@example.com")

			form := refusedNewGoal(t, client, ts.URL, url.Values{
				"title": {"Cut checkout latency"}, "why_it_matters": {""},
				"kind": {"Dated"}, "delivery_date": {"2027-03-31"},
				"cadence": {"custom"}, "cadence_days": {days},
			})
			wantRefused(t, form, "why_it_matters", "A Goal needs to say why it matters.")
			wantRefused(t, form, "cadence_days", want)
			if summary := pageElement(t, form, "div", "goal-form-errors"); !strings.Contains(summary, `href="#cadence"`) {
				t.Errorf("the summary doesn't link the cadence problem to #cadence:\n%s", summary)
			}
			if got := tagAround(t, form, `id="cadence"`); attr(got, "name") != "cadence_days" || attr(got, "value") != days {
				t.Errorf("#cadence isn't the Custom days as typed, %q: %s", days, got)
			}
			for _, id := range []string{"kind-dated", "cadence-custom"} {
				if radio := tagAround(t, form, `id="`+id+`"`); !strings.Contains(radio, " checked") {
					t.Errorf("%s isn't kept chosen: %s", id, radio)
				}
			}
			for _, id := range []string{"kind-ongoing", "cadence-7", "cadence-14", "cadence-30"} {
				if radio := tagAround(t, form, `id="`+id+`"`); strings.Contains(radio, " checked") {
					t.Errorf("%s is chosen: %s", id, radio)
				}
			}
			for name, value := range map[string]string{"title": "Cut checkout latency", "delivery_date": "2027-03-31"} {
				if input := tagAround(t, form, `name="`+name+`"`); attr(input, "value") != value || attr(input, "aria-invalid") != "" {
					t.Errorf("%s isn't kept as typed and valid: %s", name, input)
				}
			}
			if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
				t.Errorf("a refused post created %d Goals", len(goals))
			}
		})
	}
}

// A Dated Goal's delivery date, missing or not a date, is refused under
// delivery_date, and the summary lists each problem in the order the page
// shows its inputs: the Title and Why it matters, then Delivery, then How you'll know.
func TestNewGoalFormRefusesADatedGoalsDeliveryDateInPageOrder(t *testing.T) {
	t.Parallel()

	for date, want := range map[string]string{"": "A Dated Goal needs a delivery date.", "someday": `"someday" isn't a date.`} {
		t.Run(date, func(t *testing.T) {
			t.Parallel()

			h := testsupport.New(t)
			h.SignIn("sam@example.com")
			ts := newServer(t, h)
			client := signInClient(t, ts.URL, "sam@example.com")

			form := refusedNewGoal(t, client, ts.URL, url.Values{
				"title": {"Cut checkout latency"}, "why_it_matters": {""},
				"kind": {"Dated"}, "delivery_date": {date},
				"cadence": {"custom"}, "cadence_days": {"0"},
				"milestones[0].name": {"Profile checkout"}, "milestones[0].date": {""},
			})
			wantRefused(t, form, "delivery_date", want)
			if attr(tagAround(t, form, `name="delivery_date"`), "value") != date {
				t.Errorf("delivery_date isn't kept as typed, %q:\n%s", date, form)
			}
			summary := pageElement(t, form, "div", "goal-form-errors")
			var order []int
			for _, href := range []string{"#why_it_matters", "#delivery_date", "#cadence", "#milestones[0].date"} {
				order = append(order, strings.Index(summary, `href="`+href+`"`))
			}
			if slices.Contains(order, -1) || !slices.IsSorted(order) {
				t.Errorf("the summary doesn't list why_it_matters, delivery_date, cadence, then the Milestone's date (at %v):\n%s", order, summary)
			}
		})
	}
}

// The New goal form's Dated and Ongoing cards say what each means in the
// same words the Goal page's kind help uses.
func TestNewGoalKindCardsUseTheGoalPagesHelpText(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/goals/new")

	for _, tc := range []struct{ id, means string }{
		{"kind-dated", "Has a delivery date. Its Health is judged against that date and its Milestones."},
		{"kind-ongoing", "Has no delivery date. Its Health is judged against its Metrics' targets."},
	} {
		label := between(t, page, `id="`+tc.id+`"`, "</label>")
		if want := `<span class="hint">` + html.EscapeString(tc.means) + `</span>`; !strings.Contains(label, want) {
			t.Errorf("%s's card doesn't read %s: %s", tc.id, want, label)
		}
	}
	if strings.Contains(page, "Has a delivery date; its Health") {
		t.Errorf("the New goal form still reads the old semicolon wording")
	}
}

// Delivery: Kind and cadence are each a native radio group sharing one name,
// so arrow keys move within it. Neither Kind is chosen at first and Weekly
// is. The delivery date shows once Dated is chosen and Custom's days once
// Custom is, and chips that hide their radio still show focus.
func TestNewGoalPageOffersWhatAndWhyAndDelivery(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	form := pageElement(t, page, "form", "goal-form")
	what := pageElement(t, form, "section", "goal-form-what")
	if !strings.Contains(what, "the customer problem, and what changes when this succeeds") {
		t.Errorf("Why it matters isn't hinted:\n%s", what)
	}
	delivery := between(t, form, `data-testid="goal-form-delivery"`, `data-testid="goal-form-know"`)
	if h2s := headingTexts(delivery, "h2"); !slices.Equal(h2s, []string{"Delivery"}) {
		t.Errorf("Delivery is headed %q:\n%s", h2s, delivery)
	}
	for _, legend := range []string{"<legend>Kind</legend>", "<legend>Check-in cadence</legend>"} {
		if !strings.Contains(delivery, legend) {
			t.Errorf("Delivery has no %s:\n%s", legend, delivery)
		}
	}
	if strings.Index(form, `data-testid="goal-form-what"`) > strings.Index(form, `data-testid="goal-form-delivery"`) {
		t.Errorf("Delivery comes before the Title and Why it matters")
	}

	for _, radio := range []struct{ id, name, value, label string }{
		{"kind-dated", "kind", "Dated", "Dated"},
		{"kind-ongoing", "kind", "Ongoing", "Ongoing"},
		{"cadence-7", "cadence", "7", "Weekly"},
		{"cadence-14", "cadence", "14", "Every 2 weeks"},
		{"cadence-30", "cadence", "30", "Monthly"},
		{"cadence-custom", "cadence", "custom", "Custom"},
	} {
		tag := tagAround(t, delivery, `id="`+radio.id+`"`)
		if attr(tag, "type") != "radio" || attr(tag, "name") != radio.name || attr(tag, "value") != radio.value {
			t.Errorf("%s isn't a %s radio posting %q: %s", radio.id, radio.name, radio.value, tag)
		}
		if checked, want := strings.Contains(tag, " checked"), radio.id == "cadence-7"; checked != want {
			t.Errorf("%s chosen = %v at first, want %v: %s", radio.id, checked, want, tag)
		}
		if label := between(t, delivery, `id="`+radio.id+`"`, "</label>"); !strings.Contains(label, radio.label) {
			t.Errorf("%s isn't labelled %q: %s", radio.id, radio.label, label)
		}
	}
	for kind, means := range map[string]string{"kind-dated": "delivery date", "kind-ongoing": "Metrics"} {
		if label := between(t, delivery, `id="`+kind+`"`, "</label>"); !strings.Contains(label, means) {
			t.Errorf("%s isn't explained: %s", kind, label)
		}
	}
	if date := tagAround(t, delivery, `name="delivery_date"`); attr(date, "type") != "date" {
		t.Errorf("the delivery date isn't a date input: %s", date)
	}
	if days := tagAround(t, delivery, `name="cadence_days"`); attr(days, "type") != "number" || attr(days, "min") != "1" {
		t.Errorf("Custom's days aren't a number input from 1: %s", days)
	}

	style := between(t, page, ".gf-form{", "</style>")
	for _, rule := range []string{
		".gf-delivery:not(:has(#kind-dated:checked)) .gf-date{display:none}",
		".gf-cadence:not(:has(#cadence-custom:checked)) .gf-custom{display:none}",
		"label.gf-pick:has(input:focus-visible){outline:2px solid var(--color-focus)",
	} {
		if !strings.Contains(style, rule) {
			t.Errorf("the style block has no %s", rule)
		}
	}
	if strings.Contains(style, "box-shadow") {
		t.Errorf("the New goal page's styles cast a shadow")
	}
}

// whereItFits arranges an Admin's Dimensions and Field for the Where it fits
// section: Pillar, taking one value; Channel, taking several; Team,
// Extendable; and a number Field, Budget.
type whereItFits struct {
	pillar, channel, team domain.Dimension
	budget                domain.Field
}

func arrangeWhereItFits(h *testsupport.Harness) whereItFits {
	boss := h.SignIn("boss@example.com")
	return whereItFits{
		pillar:  h.CreateDimension(boss, "Pillar", "Growth", "Trust"),
		channel: h.CreateSeveralValuesDimension(boss, "Channel", "Web", "Mobile", "Retail"),
		team:    h.CreateExtendableDimension(boss, "Team", "Payments"),
		budget:  h.CreateField(boss, "Budget", domain.FieldNumber, "USD"),
	}
}

// goalValueNames are the values g carries, by name, sorted.
func goalValueNames(t *testing.T, h *testsupport.Harness, g domain.Goal) []string {
	t.Helper()
	values, err := h.Service.GoalValues(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("GoalValues: %v", err)
	}
	var names []string
	for _, v := range values {
		names = append(names, v.Value)
	}
	slices.Sort(names)
	return names
}

// fitsSection is the New goal page's Where it fits section, which holds
// fieldsets of its own.
func fitsSection(t *testing.T, page string) string {
	t.Helper()
	return between(t, page, `data-testid="goal-form-fits"`, `class="gf-actions"`)
}

// The Where it fits section offers each Dimension as a Goal takes it — a
// select of value_id for one value, value_id checkboxes for several, and an
// "add a value" input for an Extendable one — and each Field as an input of
// its type, and creating with them assigns them all.
func TestNewGoalFormAssignsDimensionValuesAndFields(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	section := fitsSection(t, page)
	growth, web, mobile := fits.pillar.Values[0], fits.channel.Values[0], fits.channel.Values[1]
	if tag := tagAround(t, section, fmt.Sprintf(`value="%d"`, growth.ID)); !strings.HasPrefix(tag, "<option") {
		t.Errorf("Pillar, taking one value, doesn't offer Growth as an option: %s", tag)
	}
	if sel := tagAround(t, section, fmt.Sprintf(`id="dimension:%d"`, fits.pillar.ID)); !strings.HasPrefix(sel, "<select") || attr(sel, "name") != "value_id" {
		t.Errorf("Pillar isn't a select of value_id: %s", sel)
	}
	for _, v := range []domain.DimensionValue{web, mobile} {
		box := tagAround(t, section, fmt.Sprintf(`id="value:%d"`, v.ID))
		if attr(box, "type") != "checkbox" || attr(box, "name") != "value_id" || attr(box, "value") != fmt.Sprint(v.ID) {
			t.Errorf("Channel, taking several, doesn't offer %s as a value_id checkbox: %s", v.Value, box)
		}
	}
	if add := tagAround(t, section, fmt.Sprintf(`name="new_value:%d"`, fits.team.ID)); attr(add, "type") != "text" {
		t.Errorf("Team, Extendable, has no text input to add a value: %s", add)
	}
	if strings.Contains(section, fmt.Sprintf(`name="new_value:%d"`, fits.pillar.ID)) {
		t.Errorf("Pillar, a Fixed list, offers to add a value:\n%s", section)
	}
	if budget := tagAround(t, section, fmt.Sprintf(`name="field:%d"`, fits.budget.ID)); attr(budget, "type") != "number" {
		t.Errorf("Budget, a number Field, isn't a number input: %s", budget)
	}

	form := newGoalForm(map[string]string{
		fmt.Sprintf("new_value:%d", fits.team.ID): "Search",
		fmt.Sprintf("field:%d", fits.budget.ID):   "25000",
	})
	form["value_id"] = []string{fmt.Sprint(growth.ID), fmt.Sprint(web.ID), fmt.Sprint(mobile.ID)}
	g := createdGoal(t, h, client, ts.URL, form)
	if got, want := goalValueNames(t, h, g), []string{"Growth", "Mobile", "Search", "Web"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the new Goal carries %q, want %q", got, want)
	}
	fields, err := h.Service.GoalFields(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("GoalFields: %v", err)
	}
	if len(fields) != 1 || fields[0].Field.ID != fits.budget.ID || fields[0].Value != "25000" {
		t.Errorf("the new Goal's Fields are %+v, want Budget 25000", fields)
	}
}

// fitsNote is the text of Where it fits' note, under its legend, which says
// what in the section is required.
func fitsNote(t *testing.T, page string) string {
	t.Helper()
	notes := elementTexts(page, "p", "goal-form-fits-note")
	if len(notes) != 1 {
		t.Fatalf("Where it fits has %d notes, want 1:\n%s", len(notes), fitsSection(t, page))
	}
	return notes[0]
}

// With a Dimension required, Where it fits no longer calls itself optional: it
// names the Dimension as required, as the checklist does.
func TestNewGoalPageWhereItFitsNamesARequiredDimension(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetDimensionRequired(boss, fits.team, true)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	note := fitsNote(t, getBody(t, client, ts.URL+"/goals/new"))
	if want := "Required: a value in Team. The rest are optional."; note != want {
		t.Errorf("Where it fits says %q, want %q", note, want)
	}
}

// With nothing required, Where it fits says the whole section is optional.
func TestNewGoalPageWhereItFitsIsOptionalWithNothingRequired(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	arrangeWhereItFits(h)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	note := fitsNote(t, getBody(t, client, ts.URL+"/goals/new"))
	if want := "Optional: the Goal's Dimension values and Fields."; note != want {
		t.Errorf("Where it fits says %q, want %q", note, want)
	}
}

// Arriving with a parent, Where it fits' note says the parent's values come
// ticked, whether or not anything is required: the sentence follows the
// Required note as it follows the Optional one.
func TestNewGoalPageWhereItFitsSaysSuggestedValuesComeTicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	revenue := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	h.AssignGoalValue(revenue, fits.pillar.Values[0])
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	withParent := fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, revenue.ID)

	note := fitsNote(t, getBody(t, client, withParent))
	if want := "Optional: the Goal's Dimension values and Fields. Values from the Goals it contributes to come ticked; untick any that don't fit."; note != want {
		t.Errorf("with nothing required, Where it fits says %q, want %q", note, want)
	}

	h.SetDimensionRequired(boss, fits.team, true)
	note = fitsNote(t, getBody(t, client, withParent))
	if want := "Required: a value in Team. The rest are optional. Values from the Goals it contributes to come ticked; untick any that don't fit."; note != want {
		t.Errorf("with Team required, Where it fits says %q, want %q", note, want)
	}
}

// A required Field is named as a required Dimension is, while a Dimension or
// Field required and then Retired isn't named, as the checklist doesn't list
// it.
func TestNewGoalPageWhereItFitsNamesARequiredFieldNotARetiredOne(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetFieldRequired(boss, fits.budget, true)
	h.SetDimensionRequired(boss, fits.pillar, true)
	notes := h.CreateField(boss, "Notes", domain.FieldShortText, "")
	h.SetFieldRequired(boss, notes, true)
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, fits.pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireField(ctx, boss.ID, notes.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	note := fitsNote(t, getBody(t, client, ts.URL+"/goals/new"))
	if want := "Required: a value in Budget. The rest are optional."; note != want {
		t.Errorf("Where it fits says %q, want %q", note, want)
	}
}

// With several required, Where it fits names each, in the checklist's order,
// on the define page as on the New goal page; with all of them required, none
// is left to call optional.
func TestDefineGoalPageWhereItFitsNamesEachRequired(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetDimensionRequired(boss, fits.team, true)
	h.SetFieldRequired(boss, fits.budget, true)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	define := fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID)

	note := fitsNote(t, getBody(t, client, define))
	if want := "Required: a value in Team and in Budget. The rest are optional."; note != want {
		t.Errorf("Where it fits says %q, want %q", note, want)
	}

	h.SetDimensionRequired(boss, fits.pillar, true)
	h.SetDimensionRequired(boss, fits.channel, true)
	note = fitsNote(t, getBody(t, client, define))
	if want := "Required: a value in Channel, in Pillar, in Team and in Budget."; note != want {
		t.Errorf("with all required, Where it fits says %q, want %q", note, want)
	}
}

// chosen reports whether Where it fits shows the value ticked or selected.
func chosen(t *testing.T, section string, v domain.DimensionValue) bool {
	t.Helper()
	tag := tagAround(t, section, fmt.Sprintf(`value="%d"`, v.ID))
	return strings.Contains(tag, " checked") || strings.Contains(tag, " selected")
}

// Arriving at /goals/new?parent=<id>, Where it fits comes with the parent's
// values ticked, less a Retired one; with two parents, their values together,
// but none in a Dimension taking one value where they disagree.
func TestNewGoalPageSuggestsTheParentsValues(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	growth, trust := fits.pillar.Values[0], fits.pillar.Values[1]
	web, mobile, retail := fits.channel.Values[0], fits.channel.Values[1], fits.channel.Values[2]
	payments := fits.team.Values[0]
	revenue := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	for _, v := range []domain.DimensionValue{growth, web, mobile, payments} {
		h.AssignGoalValue(revenue, v)
	}
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, mobile.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	trusted := h.CreateGoal(sam, "Earn trust", "Shoppers leave sites they doubt.")
	for _, v := range []domain.DimensionValue{trust, retail} {
		h.AssignGoalValue(trusted, v)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	section := fitsSection(t, getBody(t, client, ts.URL+"/goals/new"))
	for _, v := range []domain.DimensionValue{growth, trust, web, retail, payments} {
		if chosen(t, section, v) {
			t.Errorf("with no parent, %s comes chosen", v.Value)
		}
	}

	section = fitsSection(t, getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, revenue.ID)))
	for _, v := range []domain.DimensionValue{growth, web, payments} {
		if !chosen(t, section, v) {
			t.Errorf("with %q as parent, its %s isn't suggested:\n%s", revenue.Title, v.Value, section)
		}
	}
	if strings.Contains(section, fmt.Sprintf(`value="%d"`, mobile.ID)) {
		t.Errorf("the parent's Retired %s is offered:\n%s", mobile.Value, section)
	}

	section = fitsSection(t, getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d&parent=%d", ts.URL, revenue.ID, trusted.ID)))
	for _, v := range []domain.DimensionValue{web, retail, payments} {
		if !chosen(t, section, v) {
			t.Errorf("with both parents, %s isn't suggested:\n%s", v.Value, section)
		}
	}
	for _, v := range []domain.DimensionValue{growth, trust} {
		if chosen(t, section, v) {
			t.Errorf("the parents disagree on Pillar, yet %s is selected:\n%s", v.Value, section)
		}
	}
}

// Only what is still ticked on submit is assigned: a value suggested from the
// parent and unticked is left off the new Goal. With every Dimension and Field
// left empty, the Goal still saves, Proposed.
func TestNewGoalFormAssignsOnlyTheValuesStillTicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	growth, web, mobile := fits.pillar.Values[0], fits.channel.Values[0], fits.channel.Values[1]
	revenue := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	for _, v := range []domain.DimensionValue{growth, web, mobile} {
		h.AssignGoalValue(revenue, v)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	section := fitsSection(t, getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, revenue.ID)))
	if !chosen(t, section, mobile) {
		t.Fatalf("the parent's %s isn't suggested:\n%s", mobile.Value, section)
	}
	form := newGoalForm(nil)
	form["parent_id"] = []string{fmt.Sprint(revenue.ID)}
	form["value_id"] = []string{"", fmt.Sprint(web.ID)} // Pillar set to None, Mobile unticked
	g := createdGoal(t, h, client, ts.URL, form)
	if got, want := goalValueNames(t, h, g), []string{"Web"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("the new Goal carries %q, want only %q, still ticked", got, want)
	}

	empty := newGoalForm(nil)
	empty.Set(fmt.Sprintf("new_value:%d", fits.team.ID), "")
	empty.Set(fmt.Sprintf("field:%d", fits.budget.ID), "")
	empty["value_id"] = []string{""}
	g = createdGoal(t, h, client, ts.URL, empty)
	if g.Lifecycle != domain.LifecycleProposed || len(goalValueNames(t, h, g)) != 0 {
		t.Errorf("with Where it fits left empty, created %+v carrying %q, want a Proposed Goal with no values", g, goalValueNames(t, h, g))
	}
	if fields, err := h.Service.GoalFields(context.Background(), g.ID); err != nil || len(fields) != 0 {
		t.Errorf("with Where it fits left empty, the Goal has Fields %+v (%v)", fields, err)
	}
}

// A refused submit shows Where it fits exactly as posted, not the parents'
// suggestions; a Field value that isn't its type, or two values in a
// Dimension taking one, is refused at its input.
func TestNewGoalFormRefusalShowsWhereItFitsAsPosted(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	growth, web, retail := fits.pillar.Values[0], fits.channel.Values[0], fits.channel.Values[2]
	revenue := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	h.AssignGoalValue(revenue, growth)
	h.AssignGoalValue(revenue, web)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	posted := url.Values{
		"title": {""}, "why_it_matters": {"Shoppers abandon slow carts."},
		"parent_id": {fmt.Sprint(revenue.ID)},
		"value_id":  {"", fmt.Sprint(retail.ID)},
		fmt.Sprintf("new_value:%d", fits.team.ID): {"Search"},
		fmt.Sprintf("field:%d", fits.budget.ID):   {"lots"},
	}
	form := refusedNewGoal(t, client, ts.URL, posted)
	section := fitsSection(t, form)
	if chosen(t, section, growth) || chosen(t, section, web) {
		t.Errorf("the refused form shows the parent's suggestions, not what was posted:\n%s", section)
	}
	if !chosen(t, section, retail) {
		t.Errorf("the refused form lost Retail, ticked:\n%s", section)
	}
	if add := tagAround(t, section, fmt.Sprintf(`name="new_value:%d"`, fits.team.ID)); attr(add, "value") != "Search" {
		t.Errorf("the refused form lost the Team value typed: %s", add)
	}
	wantRefused(t, form, fmt.Sprintf("field:%d", fits.budget.ID), `Budget takes a number, and "lots" isn't one`)

	trust := fits.pillar.Values[1]
	posted = newGoalForm(nil)
	posted["value_id"] = []string{fmt.Sprint(growth.ID), fmt.Sprint(trust.ID)}
	form = refusedNewGoal(t, client, ts.URL, posted)
	sel := tagAround(t, form, fmt.Sprintf(`id="dimension:%d"`, fits.pillar.ID))
	if attr(sel, "aria-invalid") != "true" || !strings.Contains(between(t, form, `id="`+attr(sel, "aria-describedby")+`"`, "</"), "Pillar takes one value per Goal") {
		t.Errorf("two Pillar values aren't refused at Pillar's select: %s\n%s", sel, form)
	}
	if !strings.Contains(pageElement(t, form, "div", "goal-form-errors"), fmt.Sprintf(`href="#dimension:%d"`, fits.pillar.ID)) {
		t.Errorf("the problem summary doesn't link to Pillar's select:\n%s", form)
	}
}

// The summary lists Where it fits' problems after Contributes to's, as the
// page shows them: each Dimension's, then each Field's.
func TestNewGoalFormListsWhereItFitsProblemsInPageOrder(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := refusedNewGoal(t, client, ts.URL, url.Values{
		"title": {""}, "why_it_matters": {"Shoppers abandon slow carts."},
		"parent_id":                             {"first"},
		"value_id":                              {fmt.Sprint(fits.pillar.Values[0].ID), fmt.Sprint(fits.pillar.Values[1].ID)},
		fmt.Sprintf("field:%d", fits.budget.ID): {"lots"},
	})
	summary := pageElement(t, form, "div", "goal-form-errors")
	var order []int
	for _, href := range []string{"#title", "#parent_id", fmt.Sprintf("#dimension:%d", fits.pillar.ID), fmt.Sprintf("#field:%d", fits.budget.ID)} {
		order = append(order, strings.Index(summary, `href="`+href+`"`))
	}
	if slices.Contains(order, -1) || !slices.IsSorted(order) {
		t.Errorf("the summary doesn't list title, parent_id, Pillar, then Budget (at %v):\n%s", order, summary)
	}
	if n := strings.Count(summary, "Pillar takes one value per Goal"); n != 1 {
		t.Errorf("the summary lists Pillar's problem %d times, want once:\n%s", n, summary)
	}
}

// readyCard is the New goal page's Ready to activate card, and its Create and
// activate button.
func readyCard(t *testing.T, page string) (card, activate string) {
	t.Helper()
	card = pageElement(t, page, "section", "goal-form-ready")
	return card, tagAround(t, card, `name="activate"`)
}

// The Ready to activate card's checklist runs on the form as typed: posted to
// /goals/new/checklist, a partial form comes back as the card alone with each
// item done or to do, "n of m", and Create and activate disabled, and a
// complete one with it enabled. A required Dimension or Field counts as set by
// a value chosen, a value typed to add, or a Field's value typed.
func TestNewGoalChecklistRunsOnTheFormAsTyped(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	h.SetDimensionRequired(boss, pillar, true)
	team := h.CreateExtendableDimension(boss, "Team", "Payments")
	h.SetDimensionRequired(boss, team, true)
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "USD")
	h.SetFieldRequired(boss, budget, true)
	h.CreateField(boss, "Notes", domain.FieldShortText, "")
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	checklist := func(form url.Values) string {
		t.Helper()
		resp := postForm(t, client, ts.URL+"/goals/new/checklist", form)
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the checklist answered %d:\n%s", resp.StatusCode, body)
		}
		if strings.Contains(body, `data-testid="goal-form"`) || strings.Contains(body, "<html") {
			t.Errorf("the checklist answered more than the card:\n%s", body)
		}
		return body
	}

	partial := checklist(url.Values{"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."}, "kind": {"Dated"}, "milestones[0].name": {"Beta"}, "value_id": {fmt.Sprint(pillar.Values[0].ID)}})
	card, activate := readyCard(t, partial)
	want := map[string]bool{
		"Why it matters":                         true,
		"Owner":                                  true,
		"Dated with a delivery date, or Ongoing": false,
		"A Milestone or Metric":                  true,
		"A value in Pillar":                      true,
		"A value in Team":                        false,
		"A value in Budget":                      false,
	}
	if got := activationItems(t, card); !maps.Equal(got, want) {
		t.Errorf("partial form's checklist = %v, want %v", got, want)
	}
	if !strings.Contains(card, "4 of 7") {
		t.Errorf("the card doesn't say 4 of 7:\n%s", card)
	}
	if !strings.Contains(activate, " disabled") {
		t.Errorf("Create and activate is enabled with items missing: %s", activate)
	}

	complete := checklist(url.Values{
		"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."},
		"kind": {"Ongoing"}, "metrics[0].name": {"p95"},
		"value_id":                           {fmt.Sprint(pillar.Values[1].ID)},
		fmt.Sprintf("new_value:%d", team.ID): {"Search"},
		fmt.Sprintf("field:%d", budget.ID):   {"25000"},
	})
	card, activate = readyCard(t, complete)
	for item, done := range activationItems(t, card) {
		if !done {
			t.Errorf("%q still missing on a complete form", item)
		}
	}
	if !strings.Contains(card, "7 of 7") {
		t.Errorf("the card doesn't say 7 of 7:\n%s", card)
	}
	if strings.Contains(activate, " disabled") {
		t.Errorf("Create and activate is disabled on a complete form: %s", activate)
	}

	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("checking the form created %d Goals", len(goals))
	}
}

// The New goal page's Ready to activate card renders as of page load, with
// Create and activate enabled though nothing is typed yet, since without
// script the server decides. Script posts the form to /goals/new/checklist as
// it changes and swaps the card for the one that comes back, while the two
// buttons still submit the form plainly: Create and activate posting
// activate=1 and Save as Proposed posting nothing more.
func TestNewGoalPageRendersTheReadyCardEnabledWithoutScript(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	form := pageElement(t, page, "form", "goal-form")
	card, activate := readyCard(t, form)
	if got := activationItems(t, card); got["Why it matters"] || !got["Owner"] {
		t.Errorf("an empty form's checklist = %v, want Why it matters missing and Owner done", got)
	}
	if count := elementTexts(card, "p", "goal-form-ready-count"); len(count) != 1 || count[0] != "1 of 4 done" {
		t.Errorf("an empty form's card counts %q, want 1 of 4 done:\n%s", count, card)
	}
	var order []int
	for _, part := range []string{`id="goal-form-ready-head"`, `data-testid="goal-form-ready-count"`, "<progress", `data-testid="activation-checklist"`, ">Save as Proposed", `name="activate"`, "Saving needs", ">Cancel<"} {
		order = append(order, strings.Index(card, part))
	}
	if slices.Contains(order, -1) || !slices.IsSorted(order) {
		t.Errorf("the card isn't its heading, count, bar, checklist, buttons, note and Cancel in turn (at %v):\n%s", order, card)
	}
	if rule := cssRule(t, page, ".gf-activate"); rule != "order:-1" {
		t.Errorf("Create and activate doesn't show above Save as Proposed: .gf-activate{%s}", rule)
	}
	if strings.Contains(activate, " disabled") || attr(activate, "type") != "submit" || attr(activate, "value") != "1" || !strings.Contains(between(t, card, `name="activate"`, "</button>"), "Create and activate") {
		t.Errorf("Create and activate isn't an enabled submit posting activate=1: %s", activate)
	}
	if save := tagAround(t, card, ">Save as Proposed"); attr(save, "type") != "submit" || attr(save, "name") != "" {
		t.Errorf("Save as Proposed isn't a plain submit: %s", save)
	}
	if !strings.Contains(card, "Saving needs a Title and Why it matters, and a delivery date if Dated is chosen.") {
		t.Errorf("the card doesn't say what saving needs:\n%s", card)
	}
	if strings.Contains(card, "Only Title and Why it matters are needed to save.") {
		t.Errorf("the card still says only Title and Why it matters are needed to save:\n%s", card)
	}

	tag := openTag(form)
	for name, want := range map[string]string{
		"method":  "post",
		"action":  "/goals/new",
		"hx-post": "",
	} {
		if got := attr(tag, name); got != want {
			t.Errorf("the form's %s = %q, want %q", name, got, want)
		}
	}
	live := tagAround(t, form, `hx-post="/goals/new/checklist"`)
	for name, want := range map[string]string{
		"hx-trigger": "load, change from:closest form, input delay:400ms from:closest form",
		"hx-target":  "#" + attr(openTag(card), "id"),
		"hx-swap":    "outerHTML",
		"hx-sync":    "this:replace",
	} {
		if got := attr(live, name); got != want {
			t.Errorf("the live checklist's %s = %q, want %q", name, got, want)
		}
	}
	if attr(openTag(card), "id") == "" {
		t.Errorf("the card has no id to swap: %s", openTag(card))
	}
}

// An empty New goal form's checklist marks each item not yet met with a
// neutral ○ "to do", never a red ✗ "missing": nothing has gone wrong before
// anything is typed. The Owner, met by the person creating the Goal, is ✓
// "done". Only the mark shows: an empty ring, or a circle filled with Green's
// ink. The state is there for screen readers alone, so nothing beside the
// label can wrap (#357).
func TestNewGoalChecklistMarksUnmetItemsToDo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	list := pageElement(t, page, "ul", "activation-checklist")
	for _, li := range strings.Split(list, "<li ")[1:] {
		mark, label, state := activationItemParts(t, li)
		want := [2]string{"○", "to do"}
		if label == "Owner" {
			want = [2]string{"✓", "done"}
		}
		if got := [2]string{mark, state}; got != want {
			t.Errorf("%q is marked %q %q, want %q %q: %s", label, mark, state, want[0], want[1], li)
		}
		if !strings.Contains(li, `<span aria-hidden="true" class="gf-mark">`+mark+"</span>") {
			t.Errorf("%q's mark isn't hidden from screen readers: %s", label, li)
		}
		if !strings.Contains(li, `<span class="sr-only">`+state+"</span>") {
			t.Errorf("%q's state isn't for screen readers alone: %s", label, li)
		}
	}
	for _, gone := range []string{"✗", "missing"} {
		if strings.Contains(list, gone) {
			t.Errorf("the checklist says %q:\n%s", gone, list)
		}
	}
	if rule := cssRule(t, page, ".gf-mark"); !strings.Contains(rule, "border:2px solid var(--color-border-strong)") || !strings.Contains(rule, "border-radius:50%") {
		t.Errorf("an unmet item's mark isn't a ring in the control edge colour: %s", rule)
	}
	if rule := cssRule(t, page, ".gf-check.done .gf-mark"); !strings.Contains(rule, "background:var(--health-g-ink)") || !strings.Contains(rule, "color:var(--color-surface)") {
		t.Errorf("a met item's mark isn't filled with Green's ink: %s", rule)
	}
	if !strings.Contains(page, ".sr-only{position:absolute;width:1px;height:1px;overflow:hidden") {
		t.Errorf("the page's styles don't hide .sr-only")
	}
	if strings.Contains(page, ".gf-check .muted{margin-left:auto}") {
		t.Errorf("the checklist still pushes a visible state to the right")
	}
	if regexp.MustCompile(`\.gf-(check|mark)[^{}]*\{[^}]*--health-r-ink`).MatchString(page) {
		t.Errorf("the checklist still uses Red's ink")
	}
}

// htmx checks a form's required inputs before a request the form itself
// makes, so the live checklist posts from an element inside the New goal and
// define forms, not the form: an empty Title or Why it matters doesn't hold it back,
// and the checklist answers such a form, while the form keeps both inputs
// required for a real submit (#212).
func TestGoalFormChecklistAnswersAnEmptyTitleAndWhyItMatters(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/goals/new", fmt.Sprintf("/goals/%d/define", g.ID)} {
		form := pageElement(t, getBody(t, client, ts.URL+path), "form", "goal-form")
		if attr(openTag(form), "hx-post") != "" || attr(openTag(form), "hx-trigger") != "" {
			t.Errorf("%s: the form itself posts the checklist, so htmx validates it first: %s", path, openTag(form))
		}
		live := tagAround(t, form, `hx-post="`+path+`/checklist"`)
		if strings.HasPrefix(live, "<form") || !strings.Contains(attr(live, "hx-trigger"), "from:closest form") {
			t.Errorf("%s: the live checklist doesn't post from inside the form: %s", path, live)
		}
		for _, input := range []string{`name="title"`, `name="why_it_matters"`} {
			if tag := tagAround(t, form, input); !strings.Contains(tag, " required") {
				t.Errorf("%s: %s isn't required: %s", path, input, tag)
			}
		}

		resp := postForm(t, client, ts.URL+path+"/checklist", url.Values{"title": {""}, "why_it_matters": {""}, "kind": {"Ongoing"}})
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: the checklist answered an empty Title and Why it matters with %d:\n%s", path, resp.StatusCode, body)
		}
		card, activate := readyCard(t, body)
		if got := activationItems(t, card); got["Why it matters"] || !got["Dated with a delivery date, or Ongoing"] {
			t.Errorf("%s: the empty form's checklist = %v, want Why it matters missing and the Kind done", path, got)
		}
		if !strings.Contains(card, "2 of 4") || !strings.Contains(activate, " disabled") {
			t.Errorf("%s: the empty form's card isn't 2 of 4 with Create and activate disabled:\n%s", path, card)
		}
	}
}

// Enter in a text field submits the form by its first submit button, so on
// the New goal page and the define page that button is Save as Proposed,
// posting nothing more, never Create and activate (#165).
func TestGoalFormEnterSavesAsProposed(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/goals/new", fmt.Sprintf("/goals/%d/define", g.ID)} {
		form := pageElement(t, getBody(t, client, ts.URL+path), "form", "goal-form")
		first := tagAround(t, form, `type="submit"`)
		if attr(first, "name") != "" || attr(first, "value") != "" {
			t.Errorf("%s: the first submit posts %s=%s, want nothing more: %s", path, attr(first, "name"), attr(first, "value"), first)
		}
		if button := between(t, form, first, "</button>"); !strings.HasSuffix(button, ">Save as Proposed") {
			t.Errorf("%s: the first submit isn't Save as Proposed: %s", path, button)
		}
	}
}

// Create and activate creates the Goal Active when the form meets every rule,
// and lands on it. The server decides whatever the button showed: an
// incomplete form posted with activate=1 directly comes back as the form, 422,
// listing each rule it doesn't meet, and nothing is saved.
func TestNewGoalFormCreateAndActivate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "USD")
	h.SetFieldRequired(boss, budget, true)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := newGoalForm(map[string]string{"activate": "1", "kind": "Ongoing"})
	page := refusedNewGoal(t, client, ts.URL, form)
	summary := html.UnescapeString(pageElement(t, page, "div", "goal-form-errors"))
	for _, rule := range []string{"An Ongoing Goal needs at least one Metric.", "A Goal needs a value in Budget to become Active."} {
		if !strings.Contains(summary, rule) {
			t.Errorf("the refusal doesn't list %q:\n%s", rule, summary)
		}
	}
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("a refused activation saved %d Goals", len(goals))
	}

	form = newGoalForm(map[string]string{
		"activate": "1", "kind": "Ongoing",
		"metrics[0].name": "p95", "metrics[0].unit": "ms", "metrics[0].baseline": "900", "metrics[0].target": "300", "metrics[0].target_date": "2027-06-01",
		fmt.Sprintf("field:%d", budget.ID): "25000",
	})
	if g := createdGoal(t, h, client, ts.URL, form); g.Lifecycle != domain.LifecycleActive {
		t.Errorf("Create and activate on a complete form made a %s Goal, want Active", g.Lifecycle)
	}
}

// Save as Proposed, with only a Title and Why it matters, creates a Proposed Goal
// even where activation would need more.
func TestNewGoalFormSaveAsProposedNeedsOnlyTitleAndWhyItMatters(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.SetFieldRequired(boss, h.CreateField(boss, "Budget", domain.FieldNumber, "USD"), true)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	if g := createdGoal(t, h, client, ts.URL, newGoalForm(nil)); g.Lifecycle != domain.LifecycleProposed {
		t.Errorf("Save as Proposed made a %s Goal, want Proposed", g.Lifecycle)
	}
}

// GET /goals/<id>/define (#135) is the New goal form for a Proposed Goal,
// prefilled from it, posting back to the same address: its Title, which
// can't change here, Why it matters, Kind and delivery date, cadence, Dimension
// values and Fields. Its Milestones, Metrics and parents, Accepted and
// Pending, are listed read-only, and the Milestone and Metric rows only add.
func TestDefineGoalPageIsTheNewGoalFormPrefilled(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	ana := h.SignInNamed("ana@example.com", "Ana Torres")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ctx := context.Background()
	if _, err := h.Service.MarkGoalDated(ctx, g.Owner.ID, g.ID, testsupport.Epoch.AddDate(0, 6, 0)); err != nil {
		t.Fatalf("MarkGoalDated: %v", err)
	}
	if _, err := h.Service.SetCadence(ctx, g.Owner.ID, g.ID, 14); err != nil {
		t.Fatalf("SetCadence: %v", err)
	}
	if _, err := h.Service.AddMilestoneAsAuthor(ctx, g.Owner.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Beta cut", TargetDate: testsupport.Epoch.AddDate(0, 3, 0)}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	if _, err := h.Service.AddMetric(ctx, g.Owner.ID, domain.AddMetricInput{GoalID: g.ID, Name: "p95 latency", Unit: "ms", Direction: domain.MetricDown, Baseline: 900, Target: 300, TargetDate: testsupport.Epoch.AddDate(0, 6, 0)}); err != nil {
		t.Fatalf("AddMetric: %v", err)
	}
	web := fits.channel.Values[0]
	h.AssignGoalValue(g, web)
	h.SetGoalField(sam, g, fits.budget, "25000")
	accepted := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	pending := h.CreateGoal(ana, "Faster site", "Speed sells.")
	child := h.CreateGoal(sam, "Index faster", "Search lags.")
	free := h.CreateGoal(ana, "Earn trust", "Shoppers leave sites they doubt.")
	h.RequestLink(sam, g, accepted, "")
	h.RequestLink(sam, g, pending, "")
	h.RequestLink(sam, child, g, "")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID))
	form := pageElement(t, page, "form", "goal-form")
	if tag := openTag(form); attr(tag, "action") != fmt.Sprintf("/goals/%d/define", g.ID) {
		t.Errorf("the define form doesn't post to its own address: %s", tag)
	}
	if title := tagAround(t, form, `name="title"`); attr(title, "value") != g.Title || !strings.Contains(title, " readonly") {
		t.Errorf("Title isn't the Goal's, read-only: %s", title)
	}
	if !strings.Contains(form, ">"+g.WhyItMatters+"</textarea>") {
		t.Errorf("Why it matters isn't prefilled:\n%s", form)
	}
	if kind := tagAround(t, form, `id="kind-dated"`); !strings.Contains(kind, " checked") {
		t.Errorf("Dated isn't chosen: %s", kind)
	}
	if date := tagAround(t, form, `name="delivery_date"`); attr(date, "value") != "2026-07-02" {
		t.Errorf("the delivery date isn't the Goal's: %s", date)
	}
	if chip := tagAround(t, form, `id="cadence-14"`); !strings.Contains(chip, " checked") {
		t.Errorf("Every 2 weeks isn't chosen: %s", chip)
	}
	section := fitsSection(t, page)
	if !chosen(t, section, web) || chosen(t, section, fits.channel.Values[1]) {
		t.Errorf("Where it fits doesn't show just the Goal's Web ticked:\n%s", section)
	}
	if budget := tagAround(t, section, fmt.Sprintf(`name="field:%d"`, fits.budget.ID)); attr(budget, "value") != "25000" {
		t.Errorf("Budget isn't the Goal's: %s", budget)
	}

	know := pageElement(t, page, "section", "goal-form-know")
	existing := between(t, know, `data-testid="existing-measures"`, `data-testid="milestone-rows"`)
	for _, name := range []string{"Beta cut", "p95 latency"} {
		if !strings.Contains(existing, name) {
			t.Errorf("the existing %s isn't listed:\n%s", name, existing)
		}
	}
	if strings.Contains(existing, "<input") {
		t.Errorf("the existing Milestones and Metrics are editable here:\n%s", existing)
	}
	if rows := between(t, know, `data-testid="milestone-rows"`, "</div>"); strings.Contains(rows, "Beta cut") {
		t.Errorf("the existing Milestone is a row:\n%s", rows)
	}

	parents := pageElement(t, page, "fieldset", "goal-form-parents")
	linked := pageElement(t, parents, "ul", "linked-parents")
	for _, want := range []struct {
		goal domain.Goal
		says string
	}{{accepted, ""}, {pending, "Pending"}} {
		if !strings.Contains(linked, want.goal.Title) || !strings.Contains(linked, want.says) {
			t.Errorf("%s isn't listed as a parent (%s):\n%s", want.goal.Title, want.says, linked)
		}
	}
	if strings.Contains(linked, "<input") {
		t.Errorf("the existing parents post again:\n%s", linked)
	}
	options := pageElement(t, parents, "select", "parent-select")
	if !strings.Contains(options, fmt.Sprintf(`value="%d"`, free.ID)) {
		t.Errorf("%s isn't offered:\n%s", free.Title, options)
	}
	for _, left := range []domain.Goal{g, accepted, pending, child} {
		if strings.Contains(options, fmt.Sprintf(`value="%d"`, left.ID)) {
			t.Errorf("%s is offered as a parent:\n%s", left.Title, options)
		}
	}
}

// definePost is the define page's form for g as it loads, posted back
// unchanged but for what change sets.
func definePost(t *testing.T, client *http.Client, baseURL string, g domain.Goal, change func(url.Values)) *http.Response {
	t.Helper()
	form := url.Values{"title": {g.Title}, "why_it_matters": {g.WhyItMatters}, "kind": {g.Kind}, "cadence": {fmt.Sprint(g.CadenceDays)}}
	if !g.DeliveryDate.IsZero() {
		form.Set("delivery_date", g.DeliveryDate.Format("2006-01-02"))
	}
	change(form)
	return postForm(t, client, fmt.Sprintf("%s/goals/%d/define", baseURL, g.ID), form)
}

// The Owner changes the Kind, adds a Milestone, unticks one Dimension value
// and ticks another, and saves it all in one submit, landing on the Goal.
func TestDefineGoalFormSavesEveryChangeInOneSubmit(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.MarkGoalOngoing(context.Background(), g.Owner.ID, g.ID); err != nil {
		t.Fatalf("MarkGoalOngoing: %v", err)
	}
	g.Kind = domain.GoalOngoing
	web, mobile := fits.channel.Values[0], fits.channel.Values[1]
	h.AssignGoalValue(g, web)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := definePost(t, client, ts.URL, g, func(form url.Values) {
		form.Set("kind", domain.GoalDated)
		form.Set("delivery_date", "2026-09-01")
		form.Set("milestones[0].name", "Beta cut")
		form.Set("milestones[0].date", "2026-06-01")
		form["value_id"] = []string{fmt.Sprint(mobile.ID)}
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)

	if want := fmt.Sprintf("/goals/%d", g.ID); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != want {
		t.Fatalf("the post landed on %s with status %d, want %s:\n%s", resp.Request.URL, resp.StatusCode, want, page)
	}
	got, err := h.Service.ViewGoal(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ViewGoal: %v", err)
	}
	if got.Kind != domain.GoalDated || got.DeliveryDate.Format("2006-01-02") != "2026-09-01" || got.Lifecycle != domain.LifecycleProposed {
		t.Errorf("the Goal is %s %s due %v, want Proposed, Dated, due 2026-09-01", got.Lifecycle, got.Kind, got.DeliveryDate)
	}
	if milestones, _ := h.Service.ListMilestones(context.Background(), g.ID); len(milestones) != 1 || milestones[0].Name != "Beta cut" {
		t.Errorf("Milestones = %+v, want Beta cut", milestones)
	}
	if names := goalValueNames(t, h, g); !slices.Equal(names, []string{"Mobile"}) {
		t.Errorf("the Goal carries %v, want Mobile alone", names)
	}
}

// Only the Owner reaches either define route: anyone else gets 403. Once the
// Goal isn't Proposed, both send the Owner to its page.
func TestDefineGoalRoutesAnswerOnlyTheOwnerOfAProposedGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	proposed := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	active := h.ActiveGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	samClient := noRedirects(signInClient(t, ts.URL, "sam@example.com"))
	kimClient := noRedirects(signInClient(t, ts.URL, "kim@example.com"))
	answer := func(client *http.Client, method string, g domain.Goal) *http.Response {
		t.Helper()
		target := fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID)
		var resp *http.Response
		var err error
		if method == http.MethodGet {
			resp, err = ctxGet(t, client, target)
		} else {
			resp, err = ctxPostForm(t, client, target, url.Values{"title": {g.Title}, "why_it_matters": {"Changed."}, "kind": {domain.GoalOngoing}})
		}
		if err != nil {
			t.Fatalf("%s %s: %v", method, target, err)
		}
		_ = readBody(t, resp)
		return resp
	}

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		refused := answer(kimClient, method, proposed)
		defer func() { _ = refused.Body.Close() }()
		if refused.StatusCode != http.StatusForbidden {
			t.Errorf("%s by a non-Owner answered %d, want 403", method, refused.StatusCode)
		}
		resp := answer(samClient, method, active)
		defer func() { _ = resp.Body.Close() }()
		if want := fmt.Sprintf("/goals/%d", active.ID); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("%s on an Active Goal answered %d to %q, want 303 to %s", method, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
	if got, _ := h.Service.ViewGoal(context.Background(), proposed.ID); got.WhyItMatters != proposed.WhyItMatters || got.Kind != "" {
		t.Errorf("a refused post changed the Goal: %+v", got)
	}
}

// Someone other than the Owner who opens, submits or checks the define page
// is refused on the refusal page, 403, saying only the Owner may define it,
// with a way Back to the Goal.
func TestDefineGoalRoutesRefuseANonOwnerWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	proposed := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	kimClient := noRedirects(signInClient(t, ts.URL, "kim@example.com"))
	define := fmt.Sprintf("%s/goals/%d/define", ts.URL, proposed.ID)
	form := url.Values{"title": {proposed.Title}, "why_it_matters": {"Changed."}, "kind": {domain.GoalOngoing}}

	for _, c := range []struct {
		name string
		send func() (*http.Response, error)
	}{
		{"GET the define page", func() (*http.Response, error) { return ctxGet(t, kimClient, define) }},
		{"POST the define page", func() (*http.Response, error) { return ctxPostForm(t, kimClient, define, form) }},
		{"POST its checklist", func() (*http.Response, error) { return ctxPostForm(t, kimClient, define+"/checklist", form) }},
	} {
		resp, err := c.send()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		page := parsePage(t, readBody(t, resp))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s by a non-Owner: status %d, want 403", c.name, resp.StatusCode)
		}
		assertRefusalPage(t, page, "Can't define Goal", "Only the Goal's Owner may define it.", fmt.Sprintf("/goals/%d", proposed.ID))
	}
}

// A submit ticking a value Retired after the page loaded comes back as the
// define page, 422, naming the value, and keeps nothing it changed.
func TestDefineGoalFormRefusesAValueRetiredSinceTheLoad(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	web, mobile := fits.channel.Values[0], fits.channel.Values[1]
	h.AssignGoalValue(g, web)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	_ = getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID))
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, mobile.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}

	resp := definePost(t, client, ts.URL, g, func(form url.Values) {
		form.Set("kind", domain.GoalOngoing)
		form.Set("milestones[0].name", "Beta cut")
		form.Set("milestones[0].date", "2026-06-01")
		form["value_id"] = []string{fmt.Sprint(mobile.ID)}
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("the post answered %d, want 422:\n%s", resp.StatusCode, page)
	}
	summary := html.UnescapeString(pageElement(t, page, "div", "goal-form-errors"))
	if !strings.Contains(summary, "Mobile is retired") || !strings.Contains(summary, "wasn't saved") {
		t.Errorf("the refusal doesn't say the Goal wasn't saved for Mobile being retired:\n%s", summary)
	}
	if got, _ := h.Service.ViewGoal(context.Background(), g.ID); got.Kind != "" {
		t.Errorf("Kind = %q, want still none chosen", got.Kind)
	}
	if milestones, _ := h.Service.ListMilestones(context.Background(), g.ID); len(milestones) != 0 {
		t.Errorf("Milestones = %+v, want none", milestones)
	}
	if names := goalValueNames(t, h, g); !slices.Equal(names, []string{"Web"}) {
		t.Errorf("the Goal carries %v, want Web still", names)
	}
}

// Create and activate on the define page activates the Goal once the
// checklist, the Goal's own Milestones counted, is complete; the live
// checklist counts them too.
func TestDefineGoalFormCreateAndActivate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	if _, err := h.Service.AddMilestoneAsAuthor(context.Background(), g.Owner.ID, domain.AddMilestoneInput{GoalID: g.ID, Name: "Beta cut", TargetDate: testsupport.Epoch.AddDate(0, 3, 0)}); err != nil {
		t.Fatalf("AddMilestoneAsAuthor: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := url.Values{"title": {g.Title}, "why_it_matters": {g.WhyItMatters}, "kind": {domain.GoalDated}, "delivery_date": {"2026-09-01"}}
	card, status := postFormHX(t, client, fmt.Sprintf("%s/goals/%d/define/checklist", ts.URL, g.ID), form)
	if status != http.StatusOK || strings.Contains(tagAround(t, card, `name="activate"`), " disabled") {
		t.Errorf("the live checklist answered %d with Create and activate disabled:\n%s", status, card)
	}

	resp := definePost(t, client, ts.URL, g, func(form url.Values) {
		form.Set("kind", domain.GoalDated)
		form.Set("delivery_date", "2026-09-01")
		form.Set("activate", "1")
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", g.ID) {
		t.Fatalf("the post landed on %s, want the Goal:\n%s", resp.Request.URL, page)
	}
	if got, _ := h.Service.ViewGoal(context.Background(), g.ID); got.Lifecycle != domain.LifecycleActive {
		t.Errorf("Lifecycle = %q, want Active", got.Lifecycle)
	}
}

// On the define page the Contributes to search asks for the Goal being
// finished, and finds only the Goals it could contribute to: not itself, not
// a parent it has, and not one contributing to it, which would close a cycle.
func TestDefineGoalSearchOffersOnlyParentCandidates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Site speed", "Shoppers abandon slow pages.")
	child := h.CreateGoal(sam, "Site images", "Images are heavy.")
	parent := h.CreateGoal(sam, "Site revenue", "The business needs it.")
	free := h.CreateGoal(sam, "Site trust", "Shoppers doubt us.")
	h.RequestLink(sam, child, g, "")
	h.RequestLink(sam, g, parent, "")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID))
	search := tagAround(t, page, `id="parent-search"`)
	if want := fmt.Sprintf("/goals/search?goal=%d", g.ID); html.UnescapeString(attr(search, "hx-get")) != want {
		t.Errorf("the search asks %q, want %q", attr(search, "hx-get"), want)
	}
	results := getBody(t, client, fmt.Sprintf("%s/goals/search?goal=%d&q=site", ts.URL, g.ID))
	if n := strings.Count(results, `data-testid="parent-result"`); n != 1 || !strings.Contains(results, free.Title) {
		t.Errorf("the search found %d results, want only %s:\n%s", n, free.Title, results)
	}
}

// Without script, a Contributes to chip's Remove checkbox leaves its parent
// out: of the Goal saved, on the New goal and define pages alike, and of the
// chips a refused save comes back with (ticket #168).
func TestGoalFormParentChipRemoveLeavesItsParentOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	kept := h.ActiveGoal(sam, "Grow revenue", "The business needs it.")
	gone := h.ActiveGoal(sam, "Earn trust", "Customers stay.")
	proposed := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	pick := func(form url.Values) {
		form["parent_id"] = []string{fmt.Sprint(kept.ID), fmt.Sprint(gone.ID)}
		form["remove_parent"] = []string{fmt.Sprint(gone.ID)}
	}
	chips := func(page string) []string {
		var ids []string
		for _, m := range regexp.MustCompile(`name="parent_id" value="(\d+)"`).FindAllStringSubmatch(pageElement(t, page, "ul", "parent-chips"), -1) {
			ids = append(ids, m[1])
		}
		return ids
	}
	wantParents := func(page string, g domain.Goal) {
		t.Helper()
		if parents := h.ParentsOf(g); len(parents) != 1 || parents[0].ID != kept.ID {
			t.Errorf("%s: the Goal's parents are %+v, want only %q", page, parents, kept.Title)
		}
	}

	form := newGoalForm(nil)
	pick(form)
	wantParents("/goals/new", createdGoal(t, h, client, ts.URL, form))

	resp := definePost(t, client, ts.URL, proposed, pick)
	defer func() { _ = resp.Body.Close() }()
	if page := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", proposed.ID) {
		t.Fatalf("the define post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	wantParents("/goals/{id}/define", proposed)

	refused := newGoalForm(map[string]string{"title": ""})
	pick(refused)
	if got, want := chips(refusedNewGoal(t, client, ts.URL, refused)), []string{fmt.Sprint(kept.ID)}; !slices.Equal(got, want) {
		t.Errorf("a refused New goal comes back with parent chips %q, want %q", got, want)
	}
	again := h.CreateGoal(sam, "Ship the app", "Mobile matters.")
	resp = definePost(t, client, ts.URL, again, func(form url.Values) {
		form.Set("title", "")
		pick(form)
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a define post with no title answered %d, want 422:\n%s", resp.StatusCode, page)
	}
	if got, want := chips(page), []string{fmt.Sprint(kept.ID)}; !slices.Equal(got, want) {
		t.Errorf("a refused define comes back with parent chips %q, want %q", got, want)
	}
}

// A parent chip carries a Remove checkbox for its parent, which the form's
// script hides, as its × removes the chip instead (ticket #168).
func TestGoalFormParentChipHasARemoveCheckboxScriptHides(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.ActiveGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID))
	chip := between(t, page, `data-testid="parent-chip"`, "</li>")
	box := regexp.MustCompile(`<label class="([^"]*)"><input type="checkbox" name="remove_parent" value="` + fmt.Sprint(parent.ID) + `"> Remove</label>`).FindStringSubmatch(chip)
	if box == nil {
		t.Fatalf("the parent chip has no Remove checkbox for its parent:\n%s", chip)
	}
	if !slices.Contains(strings.Fields(box[1]), "gf-no-js") {
		t.Errorf("the parent chip's Remove is class %q, which script doesn't hide", box[1])
	}
	if !strings.Contains(page, ".gf-form.gf-js .gf-no-js{display:none}") {
		t.Errorf("the form's styles don't hide .gf-no-js when script runs")
	}
}

// The Contributes to search swaps its matches into its results list rather
// than inheriting the form's outerHTML swap, which would replace the list and
// lose the id the next search targets, and syncs only with itself, on the New
// goal and define pages alike (ticket #178). The search answers bare items,
// with no list of its own to nest and no id outside the chips' templates.
func TestGoalFormParentSearchKeepsItsResultsList(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Site speed", "Shoppers abandon slow pages.")
	h.CreateGoal(sam, "Site revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, path := range []string{"/goals/new", fmt.Sprintf("/goals/%d/define", g.ID)} {
		search := tagAround(t, getBody(t, client, ts.URL+path), `id="parent-search"`)
		for name, want := range map[string]string{
			"hx-target":  "#parent-results",
			"hx-swap":    "innerHTML",
			"hx-sync":    "this:replace",
			"hx-include": "#parent-chips [name='parent_id']",
		} {
			if got := html.UnescapeString(attr(search, name)); got != want {
				t.Errorf("%s: the search's %s = %q, want %q", path, name, got, want)
			}
		}
	}

	templates := regexp.MustCompile(`(?s)<template>.*?</template>`)
	for _, q := range []string{"site", "nothing like it"} {
		page := getBody(t, client, ts.URL+"/goals/search?"+url.Values{"q": {q}}.Encode())
		body := strings.TrimSpace(page)
		if !strings.HasPrefix(body, "<li") || strings.Contains(body, "<ul") {
			t.Errorf("searching %q answers more than bare items:\n%s", q, page)
		}
		if outside := templates.ReplaceAllString(body, ""); regexp.MustCompile(`\sid="`).MatchString(outside) {
			t.Errorf("searching %q answers an id outside the chips' templates:\n%s", q, page)
		}
	}
}

// readyItemLinks are the Ready to activate checklist's items, by label, each
// with the href it links to, "" when it links nowhere.
func readyItemLinks(t *testing.T, card string) map[string]string {
	t.Helper()
	list := pageElement(t, card, "ul", "activation-checklist")
	links := map[string]string{}
	for _, li := range strings.Split(list, "<li ")[1:] {
		_, label, _ := activationItemParts(t, li)
		href := ""
		if strings.Contains(li, "<a ") {
			href = attr(tagAround(t, li, "<a "), "href")
		}
		links[label] = href
	}
	return links
}

// assertLinksTo checks each linked checklist item's href names an element on
// page by id, so following it lands on the field or section that meets it.
func assertLinksTo(t *testing.T, what, page string, links map[string]string) {
	t.Helper()
	for item, href := range links {
		if id, ok := strings.CutPrefix(href, "#"); ok && !strings.Contains(page, ` id="`+id+`"`) {
			t.Errorf("%s: %q links to %s, which isn't on the page", what, item, href)
		}
	}
}

// Each item of the Ready to activate checklist links to the field, or the
// section, that would meet it, as the error summary links a refused input: Why it
// matters to its input, the Kind to its group, a Milestone or Metric to How you'll
// know, and each required Dimension and Field to its input. The Owner is the
// person creating the Goal, so nothing on the form meets it and it links
// nowhere.
func TestNewGoalChecklistLinksEachItemToItsField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetDimensionRequired(boss, fits.pillar, true)
	h.SetDimensionRequired(boss, fits.channel, true)
	h.SetFieldRequired(boss, fits.budget, true)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	card, _ := readyCard(t, page)
	links := readyItemLinks(t, card)
	know := links["A Milestone or Metric"]
	want := map[string]string{
		"Why it matters":                         "#why_it_matters",
		"Owner":                                  "",
		"Dated with a delivery date, or Ongoing": "#kind",
		"A Milestone or Metric":                  know,
		"A value in Pillar":                      fmt.Sprintf("#dimension:%d", fits.pillar.ID),
		"A value in Channel":                     fmt.Sprintf("#dimension:%d", fits.channel.ID),
		"A value in Budget":                      fmt.Sprintf("#field:%d", fits.budget.ID),
	}
	if !maps.Equal(links, want) {
		t.Errorf("a fresh form's checklist links = %v, want %v", links, want)
	}
	if section := tagAround(t, page, `data-testid="goal-form-know"`); know == "" || know != "#"+attr(section, "id") {
		t.Errorf("A Milestone or Metric links to %q, not How you'll know: %s", know, section)
	}
	assertLinksTo(t, "a fresh form", page, links)
}

// The checklist's links survive its live swaps on the New goal and define
// forms alike: as the page loads, and as the form posts to its checklist. Once
// Dated is chosen, the Kind item links to the delivery date it still needs,
// and once Ongoing is, the Metric item links to How you'll know.
func TestGoalFormChecklistLinksSurviveItsSwaps(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetDimensionRequired(boss, fits.team, true)
	h.SetFieldRequired(boss, fits.budget, true)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	team := fmt.Sprintf("#dimension:%d", fits.team.ID)
	budget := fmt.Sprintf("#field:%d", fits.budget.ID)

	for _, path := range []string{"/goals/new", fmt.Sprintf("/goals/%d/define", g.ID)} {
		page := getBody(t, client, ts.URL+path)
		card, _ := readyCard(t, page)
		want := map[string]string{
			"Why it matters":                         "#why_it_matters",
			"Owner":                                  "",
			"Dated with a delivery date, or Ongoing": "#kind",
			"A Milestone or Metric":                  "#know",
			"A value in Team":                        team,
			"A value in Budget":                      budget,
		}
		if got := readyItemLinks(t, card); !maps.Equal(got, want) {
			t.Errorf("%s: the loaded checklist links = %v, want %v", path, got, want)
		}
		assertLinksTo(t, path, page, want)

		for kind, want := range map[string]map[string]string{
			"Dated": {
				"Why it matters":                         "#why_it_matters",
				"Owner":                                  "",
				"Dated with a delivery date, or Ongoing": "#delivery_date",
				"A Milestone or Metric":                  "#know",
				"A value in Team":                        team,
				"A value in Budget":                      budget,
			},
			"Ongoing": {
				"Why it matters":                         "#why_it_matters",
				"Owner":                                  "",
				"Dated with a delivery date, or Ongoing": "#kind",
				"A Metric":                               "#know",
				"A value in Team":                        team,
				"A value in Budget":                      budget,
			},
		} {
			body, status := postFormHX(t, client, ts.URL+path+"/checklist", url.Values{"title": {"Cut checkout latency"}, "why_it_matters": {""}, "kind": {kind}})
			if status != http.StatusOK {
				t.Fatalf("%s: the checklist answered %s with %d:\n%s", path, kind, status, body)
			}
			card, _ := readyCard(t, body)
			if got := readyItemLinks(t, card); !maps.Equal(got, want) {
				t.Errorf("%s: %s posted, the checklist links = %v, want %v", path, kind, got, want)
			}
			assertLinksTo(t, path, page, want)
		}
	}
}

// The Goal page's activation checklist for a Proposed Goal links nowhere: the
// fields that meet it are on the define page, which Finish defining opens.
func TestProposedGoalChecklistLinksNowhere(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SetDimensionRequired(boss, fits.team, true)
	h.SetFieldRequired(boss, fits.budget, true)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, g.ID))
	list := pageElement(t, page, "ul", "activation-checklist")
	if strings.Contains(list, "<a ") {
		t.Errorf("the Proposed Goal's checklist links:\n%s", list)
	}
	if got := activationItems(t, page); len(got) != 6 {
		t.Errorf("the Proposed Goal's checklist = %v, want 6 items", got)
	}
}

// A Kind tile shows whether it's chosen: unchosen, an outline in the control
// edge colour with an empty radio dot; chosen, a strong primary edge and filled
// dot, both 3:1 in light (WCAG 1.4.11), and the subtle primary fill, which the
// Delivery date's white input sits on. A chosen Where it fits value, a pick,
// takes the same strong primary edge and fill.
// The edge is 2px either way, so choosing doesn't resize the tile. All of it
// waits on :has, so a browser without it keeps the native radio, which still
// chooses.
func TestNewGoalPageKindCardsShowWhichIsChosen(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	style := between(t, page, ".gf-form{", "</style>")
	const supports = "@supports selector(:has(*))"
	block := mediaBlock(t, style, supports)
	for sel, want := range map[string][]string{
		".gf-tile":                               {"border:2px solid var(--color-border-strong)"},
		".gf-segment::before":                    {"border:2px solid var(--color-border-strong)", "border-radius:50%"},
		".gf-tile:has(input:checked)":            {"border-color:var(--color-primary-strong)", "background:var(--color-primary-subtle)"},
		".gf-segment:has(input:checked)::before": {"border-color:var(--color-primary-strong)", "radial-gradient(circle,var(--color-primary-strong)"},
		".gf-pick:has(input:checked)":            {"border-color:var(--color-primary-strong)", "background:var(--color-primary-subtle)"},
	} {
		rule, ok := ruleFor(block, sel)
		if !ok {
			t.Errorf("%s has no %s rule", supports, sel)
			continue
		}
		for _, decl := range want {
			if !strings.Contains(rule, decl) {
				t.Errorf("%s declares %q, want %s", sel, rule, decl)
			}
		}
	}
	// An unticked Where it fits pick's edge needs no :has, so it's drawn
	// outside the block, 3:1 on the canvas (#431).
	if rule, ok := ruleFor(style, ".gf-pick"); !ok || !strings.Contains(rule, "border:1px solid var(--color-border-strong)") {
		t.Errorf(".gf-pick declares %q, want border:1px solid var(--color-border-strong)", rule)
	}
	if rule, _ := ruleFor(block, ".gf-tile:has(input:checked)"); strings.Contains(rule, "border-width") || strings.Contains(rule, "padding") || strings.Contains(rule, "border:") {
		t.Errorf("choosing a Kind tile resizes it: %s", rule)
	}
	if strings.Contains(style, "primary-light") {
		t.Errorf("a Kind tile still fills with --color-primary-light")
	}
	outside := strings.Replace(style, supports+"{"+block, "", 1)
	if strings.Contains(outside, "opacity:0") || strings.Contains(outside, ".gf-segment::before") {
		t.Errorf("the native radio is hidden, or a drawn dot shown, without :has:\n%s", outside)
	}
	if !strings.Contains(block, ".gf-pick input{position:absolute;opacity:0") {
		t.Errorf("with :has the native radio isn't hidden behind the drawn dot:\n%s", block)
	}
}

// headingTexts are the text of every tag element in page, a heading's, in
// order, markup dropped and whitespace collapsed.
func headingTexts(page, tag string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`(?s)<`+tag+`[ >].*?</`+tag+`>`).FindAllString(page, -1) {
		out = append(out, strings.Join(strings.Fields(html.UnescapeString(regexpTags.ReplaceAllString(m, " "))), " "))
	}
	return out
}

// assertFlatFormFrame fails unless page sits in the flat frame as a form
// (DESIGN.md § Page header, § Components: Sections): its page header's <h1>
// reads title under parent, when there is one, and no element on it carries the
// card class.
func assertFlatFormFrame(t *testing.T, page, parent, title string) {
	t.Helper()
	strip := between(t, page, `<div class="page-header">`, `<main class="page">`)
	if h1s := headingTexts(strip, "h1"); len(h1s) != 1 || h1s[0] != title {
		t.Errorf("the page header's <h1>s read %q, want %s:\n%s", h1s, title, strip)
	}
	crumb := ""
	if m := regexp.MustCompile(`<a class="ph-parent"[^>]*>([^<]*)</a>`).FindStringSubmatch(strip); m != nil {
		crumb = html.UnescapeString(m[1])
	}
	if crumb != parent {
		t.Errorf("the page header's breadcrumb reads %q, want %q:\n%s", crumb, parent, strip)
	}
	if h1s := headingTexts(between(t, page, `<main class="page">`, "</main>"), "h1"); len(h1s) != 0 {
		t.Errorf("the page repeats its title in its content: %q", h1s)
	}
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		if slices.Contains(strings.Fields(m[1]), "card") {
			t.Errorf("an element on the page is a card: %s", m[0])
		}
	}
}

// The New goal page is a form in the flat frame: "New goal" heads it in the
// page header, and it has no card. Its column holds the Title and Why it
// matters, then Delivery, How you'll know and More detail, and Ready to
// activate follows it, each a plain section headed by an <h2> with no step
// number. A muted line above the Title, which reads as a heading itself, says
// the Goal is Proposed until it's activated.
func TestNewGoalPageIsHeadedSectionsWithoutACard(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	assertFlatFormFrame(t, page, "", "New goal")
	form := pageElement(t, page, "form", "goal-form")
	var order []int
	for _, id := range []string{"goal-form-what", "goal-form-delivery", "goal-form-know", "goal-form-more", "goal-form-ready"} {
		at := strings.Index(form, `data-testid="`+id+`"`)
		if at < 0 {
			t.Fatalf("the form has no %s:\n%s", id, form)
		}
		order = append(order, at)
		if tag := tagAround(t, form, `data-testid="`+id+`"`); !strings.HasPrefix(tag, "<section") {
			t.Errorf("%s isn't a <section>: %s", id, tag)
		}
	}
	if !slices.IsSorted(order) {
		t.Errorf("the sections aren't What, Delivery, How you'll know, More detail, then Ready (at %v)", order)
	}

	h2s := headingTexts(form, "h2")
	if len(h2s) != 4 || h2s[0] != "Delivery" || h2s[1] != "How you'll know" || !strings.HasPrefix(h2s[2], "More detail ") || h2s[3] != "Ready to activate" {
		t.Errorf("the <h2>s read %q, want Delivery, How you'll know, More detail …, Ready to activate", h2s)
	}
	for _, h2 := range h2s {
		if regexp.MustCompile(`^(\d|Step)`).MatchString(h2) {
			t.Errorf("the <h2> %q is numbered", h2)
		}
	}
	if strings.Contains(page, `class="page-head"`) || strings.Contains(page, "Say what the Goal is and why it matters.") {
		t.Errorf("the page keeps its old page-head and lead")
	}

	what := pageElement(t, form, "section", "goal-form-what")
	if line := between(t, what, "<section", `name="title"`); !strings.Contains(html.UnescapeString(line), "Proposed until it's activated") {
		t.Errorf("no line above the Title says it's Proposed until it's activated:\n%s", line)
	}
	title := tagAround(t, what, `name="title"`)
	if attr(title, "placeholder") != "Name the outcome" {
		t.Errorf("the Title doesn't ask to Name the outcome: %s", title)
	}
	if label := between(t, what, "<label", `name="title"`); !strings.Contains(label, ">Title</span>") {
		t.Errorf("the Title isn't labelled Title:\n%s", label)
	}
	if label := html.UnescapeString(between(t, what, `name="title"`, `name="why_it_matters"`)); !strings.Contains(label, "Why it matters") || !strings.Contains(label, "· the customer problem, and what changes when this succeeds") {
		t.Errorf("Why it matters isn't labelled with its hint inline:\n%s", label)
	}
}

// Finish defining is the same page: its page header reads Finish defining
// under the Goal's title, linking back to it, with no card, and its Title
// stays the Goal's, read-only.
func TestDefineGoalPageIsHeadedFinishDefining(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID))
	assertFlatFormFrame(t, page, g.Title, "Finish defining")
	if crumb := tagAround(t, page, `class="ph-parent"`); attr(crumb, "href") != fmt.Sprintf("/goals/%d", g.ID) {
		t.Errorf("the breadcrumb doesn't link to the Goal: %s", crumb)
	}
	form := pageElement(t, page, "form", "goal-form")
	what := pageElement(t, form, "section", "goal-form-what")
	if title := tagAround(t, what, `name="title"`); attr(title, "value") != g.Title || !strings.Contains(title, " readonly") {
		t.Errorf("Title isn't the Goal's, read-only: %s", title)
	}
}

// Delivery's Kind is two tiles, the Dated one holding the Delivery date, and
// its cadence is one segmented control of the same four radios, Custom
// without an ellipsis.
func TestNewGoalPageDatedTileHoldsTheDeliveryDate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := pageElement(t, getBody(t, client, ts.URL+"/goals/new"), "form", "goal-form")
	delivery := between(t, form, `data-testid="goal-form-delivery"`, `data-testid="goal-form-know"`)
	tiles := strings.Split(between(t, delivery, `id="kind"`, "</fieldset>"), `class="gf-tile"`)[1:]
	if len(tiles) != 2 {
		t.Fatalf("the Kind has %d tiles, want 2:\n%s", len(tiles), delivery)
	}
	dated, ongoing := tiles[0], tiles[1]
	if !strings.Contains(dated, `id="kind-dated"`) || !strings.Contains(between(t, dated, `id="kind-dated"`, ""), `name="delivery_date"`) {
		t.Errorf("the Dated tile doesn't hold its radio, then the Delivery date:\n%s", dated)
	}
	if !strings.Contains(between(t, dated, "gf-date", ""), ">Delivery date</span>") {
		t.Errorf("the Delivery date isn't labelled:\n%s", dated)
	}
	if !strings.Contains(ongoing, `id="kind-ongoing"`) || strings.Contains(ongoing, "delivery_date") {
		t.Errorf("the Ongoing tile isn't its radio alone:\n%s", ongoing)
	}

	control := between(t, delivery, `class="gf-segctl"`, `class="field gf-custom"`)
	for _, chip := range []struct{ id, value, label string }{
		{"cadence-7", "7", "Weekly"},
		{"cadence-14", "14", "Every 2 weeks"},
		{"cadence-30", "30", "Monthly"},
		{"cadence-custom", "custom", "Custom"},
	} {
		radio := tagAround(t, control, `id="`+chip.id+`"`)
		if attr(radio, "type") != "radio" || attr(radio, "name") != "cadence" || attr(radio, "value") != chip.value {
			t.Errorf("%s isn't the cadence radio posting %s: %s", chip.id, chip.value, radio)
		}
		rest := between(t, control, `id="`+chip.id+`"`, "</label>")
		if label := strings.TrimSpace(regexpTags.ReplaceAllString(rest[strings.Index(rest, ">")+1:], "")); label != chip.label {
			t.Errorf("%s reads %q, want %q", chip.id, label, chip.label)
		}
	}
}

// The Check-in cadence is one segmented control in § Segmented controls' tinted
// look: a --color-chip-bg track holding the segments, the chosen one on the
// canvas with a strong primary edge, 3:1 on the track in light (WCAG 1.4.11),
// and in bold, so the choice reads by more than a fill. The native radio hides, and the focus ring shows on the focused
// segment, only with :has. At phone widths the Kind tiles stack and the control
// becomes a 2×2 grid the width of the form.
func TestNewGoalPageCadenceIsASegmentedControl(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	style := between(t, getBody(t, client, ts.URL+"/goals/new"), ".gf-form{", "</style>")
	for sel, want := range map[string][]string{
		".gf-segctl": {"background:var(--color-chip-bg)", "border-radius:var(--radius-md)"},
		".gf-seg":    {"border:1px solid transparent"},
	} {
		rule, ok := ruleFor(style, sel)
		if !ok {
			t.Errorf("the style block has no %s rule", sel)
		}
		for _, decl := range want {
			if !strings.Contains(rule, decl) {
				t.Errorf("%s declares %q, want %s", sel, rule, decl)
			}
		}
	}
	if rule, _ := ruleFor(style, ".gf-segctl"); strings.Contains(rule, "overflow") {
		t.Errorf("the control clips its segments' focus ring: %s", rule)
	}

	const supports = "@supports selector(:has(*))"
	block := mediaBlock(t, style, supports)
	for sel, want := range map[string][]string{
		".gf-seg input":                    {"position:absolute", "opacity:0"},
		".gf-seg:has(input:checked)":       {"background:var(--color-canvas)", "border-color:var(--color-primary-strong)", "color:var(--color-ink)", "font-weight:600"},
		".gf-seg:has(input:focus-visible)": {"outline:2px solid var(--color-focus)"},
	} {
		rule, ok := ruleFor(block, sel)
		if !ok {
			t.Errorf("%s has no %s rule", supports, sel)
		}
		for _, decl := range want {
			if !strings.Contains(rule, decl) {
				t.Errorf("%s declares %q, want %s", sel, rule, decl)
			}
		}
	}
	if outside := strings.Replace(style, supports+"{"+block, "", 1); strings.Contains(outside, ".gf-seg input") {
		t.Errorf("the cadence's native radio is hidden without :has")
	}

	phone := mediaBlock(t, style, "@media (max-width:600px)")
	for sel, want := range map[string]string{
		".gf-segments": "grid-template-columns:minmax(0,1fr)",
		".gf-segctl":   "grid-template-columns:1fr 1fr",
	} {
		if rule, _ := ruleFor(phone, sel); !strings.Contains(rule, want) {
			t.Errorf("at phone width %s declares %q, want %s", sel, rule, want)
		}
	}
}

// Beside How you'll know's heading, a line says what activation needs of it:
// at least one Milestone or Metric, or, once Ongoing is chosen, at least one
// Metric. Both are rendered and :has swaps them, so a browser without it
// shows the first; the old "optional" hint is gone.
func TestNewGoalPageHowYoullKnowSaysWhatActivationNeeds(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	know := pageElement(t, page, "section", "goal-form-know")
	head := between(t, know, "<h2", `data-testid="milestone-rows"`)
	any := tagAround(t, head, ">At least one Milestone or Metric to activate<")
	ongoing := tagAround(t, head, ">At least one Metric to activate<")
	if strings.Contains(know, "Milestones and Metrics are optional") {
		t.Errorf("How you'll know keeps its old optional hint:\n%s", know)
	}

	style := between(t, page, ".gf-form{", "</style>")
	anyClass, ongoingClass := "."+attr(any, "class"), "."+attr(ongoing, "class")
	if rule, _ := ruleFor(style, ongoingClass); !strings.Contains(rule, "display:none") {
		t.Errorf("the Ongoing line shows without :has: %s{%s}", ongoingClass, rule)
	}
	block := mediaBlock(t, style, "@supports selector(:has(*))")
	if rule, _ := ruleFor(block, ".gf-form:has(#kind-ongoing:checked) "+anyClass); !strings.Contains(rule, "display:none") {
		t.Errorf("choosing Ongoing doesn't hide %s: %q", anyClass, rule)
	}
	if rule, ok := ruleFor(block, ".gf-form:has(#kind-ongoing:checked) "+ongoingClass); !ok || strings.Contains(rule, "display:none") {
		t.Errorf("choosing Ongoing doesn't show %s: %q", ongoingClass, rule)
	}
}

// moreRow is a More detail row of page: its <details> opening tag and its
// summary's text, markup dropped and whitespace collapsed.
func moreRow(t *testing.T, page, testID string) (tag, summary string) {
	t.Helper()
	tag = tagAround(t, page, `data-testid="`+testID+`"`)
	if !strings.HasPrefix(tag, "<details") {
		t.Fatalf("%s isn't a <details>: %s", testID, tag)
	}
	text := between(t, between(t, page, `data-testid="`+testID+`"`, ""), "<summary", "</summary>")
	return tag, strings.Join(strings.Fields(html.UnescapeString(regexpTags.ReplaceAllString(text, " "))), " ")
}

// isOpen says a <details> opening tag renders it open.
func isOpen(tag string) bool {
	return regexp.MustCompile(`\sopen[\s>]`).MatchString(tag)
}

// More detail holds Contributes to as a one-line row, closed with nothing
// picked, saying so and offering to Set it; arriving with a parent picked, it
// names the parent and offers to Change it. Its fieldset keeps its accessible
// name in a visually hidden legend.
func TestNewGoalPageContributesToIsAMoreDetailRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(sam, "Grow revenue", "The business needs it.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, client, ts.URL+"/goals/new")
	more := pageElement(t, page, "section", "goal-form-more")
	tag, summary := moreRow(t, more, "goal-form-parents-row")
	if isOpen(tag) {
		t.Errorf("Contributes to opens with nothing picked: %s", tag)
	}
	if summary != "Contributes to · none Set" {
		t.Errorf("Contributes to's row reads %q, want Contributes to · none Set", summary)
	}
	if fieldset := between(t, more, `data-testid="goal-form-parents-row"`, `data-testid="goal-form-parents"`); !strings.Contains(fieldset, "</summary>") {
		t.Errorf("the Contributes to field isn't the row's body:\n%s", fieldset)
	}
	if !strings.Contains(more, `<legend class="sr-only">Contributes to</legend>`) {
		t.Errorf("the Contributes to field's legend isn't visually hidden:\n%s", more)
	}

	page = getBody(t, client, fmt.Sprintf("%s/goals/new?parent=%d", ts.URL, parent.ID))
	if _, summary = moreRow(t, page, "goal-form-parents-row"); summary != "Contributes to · Grow revenue Change" {
		t.Errorf("with a parent picked, the row reads %q, want Contributes to · Grow revenue Change", summary)
	}
}

// Where it fits is More detail's second row. With only optional Dimensions and
// Fields it's closed, not set, offering to Set it; with a required one unset,
// it opens by itself, since the checklist links into it. On the define page it
// names the values the Goal carries, in page order (Channel, then Pillar),
// offering to Change them.
func TestGoalFormWhereItFitsIsAMoreDetailRow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	fits := arrangeWhereItFits(h)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Cut checkout latency", "Shoppers abandon slow carts.")
	h.AssignGoalValue(g, fits.channel.Values[1])
	h.AssignGoalValue(g, fits.pillar.Values[0])
	h.SetGoalField(sam, g, fits.budget, "25000")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	tag, summary := moreRow(t, getBody(t, client, ts.URL+"/goals/new"), "goal-form-fits-row")
	if isOpen(tag) || summary != "Where it fits · not set Set" {
		t.Errorf("with nothing required, the row is %s reading %q, want closed, Where it fits · not set Set", tag, summary)
	}
	if more := pageElement(t, getBody(t, client, ts.URL+"/goals/new"), "section", "goal-form-more"); !strings.Contains(more, `<legend class="sr-only">Where it fits</legend>`) {
		t.Errorf("the Where it fits field's legend isn't visually hidden:\n%s", more)
	}

	tag, summary = moreRow(t, getBody(t, client, fmt.Sprintf("%s/goals/%d/define", ts.URL, g.ID)), "goal-form-fits-row")
	if isOpen(tag) || summary != "Where it fits · Mobile, Growth, 25000 Change" {
		t.Errorf("on the define page, the row is %s reading %q, want closed, Where it fits · Mobile, Growth, 25000 Change", tag, summary)
	}

	h.SetDimensionRequired(boss, fits.team, true)
	if tag, _ = moreRow(t, getBody(t, client, ts.URL+"/goals/new"), "goal-form-fits-row"); !isOpen(tag) {
		t.Errorf("with Team required and unset, the row is closed: %s", tag)
	}
}

// With no Dimensions or Fields, More detail holds Contributes to alone.
func TestNewGoalPageMoreDetailWithoutWhereItFits(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	more := pageElement(t, getBody(t, client, ts.URL+"/goals/new"), "section", "goal-form-more")
	if strings.Contains(more, "goal-form-fits") {
		t.Errorf("More detail offers Where it fits with nothing to set:\n%s", more)
	}
}

// A refused submit opens the More detail row holding a refused input, so its
// message isn't hidden: Contributes to for a parent that isn't a Goal, and
// Where it fits for a Field value that isn't its type. A row with nothing
// refused stays closed.
func TestNewGoalFormRefusalOpensTheRowItRefuses(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	fits := arrangeWhereItFits(h)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	form := refusedNewGoal(t, client, ts.URL, newGoalForm(map[string]string{"parent_id": "first"}))
	if tag, _ := moreRow(t, form, "goal-form-parents-row"); !isOpen(tag) {
		t.Errorf("a refused parent leaves Contributes to closed: %s", tag)
	}
	if tag, _ := moreRow(t, form, "goal-form-fits-row"); isOpen(tag) {
		t.Errorf("with nothing refused in it, Where it fits opens: %s", tag)
	}

	form = refusedNewGoal(t, client, ts.URL, newGoalForm(map[string]string{fmt.Sprintf("field:%d", fits.budget.ID): "lots"}))
	if tag, _ := moreRow(t, form, "goal-form-fits-row"); !isOpen(tag) {
		t.Errorf("a refused Budget leaves Where it fits closed: %s", tag)
	}
	if tag, _ := moreRow(t, form, "goal-form-parents-row"); isOpen(tag) {
		t.Errorf("with nothing refused in it, Contributes to opens: %s", tag)
	}
}

// With script, How you'll know's open Milestone or Metric editor posts
// editing=<Milestone or Metric>, and the Ready to activate card's How you'll
// know item becomes "Save the Milestone you started", to do, linked to the
// open editor, in place of "A Milestone or Metric": the count of items stays,
// and Create and activate waits. Without the marker the Metric typed meets the
// item as before (#369).
func TestNewGoalChecklistFlagsAMeasureStartedAndNotSaved(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	form := url.Values{
		"title": {"Cut checkout latency"}, "why_it_matters": {"Shoppers abandon slow carts."}, "kind": {"Dated"}, "delivery_date": {"2026-12-31"},
		"metrics[0].name": {"p95 latency"}, "metrics[0].unit": {"ms"}, "metrics[0].baseline": {"1200"}, "metrics[0].target": {"400"}, "metrics[0].target_date": {"2026-12-31"},
	}
	checklist := func(form url.Values) (card, activate string) {
		t.Helper()
		resp := postForm(t, client, ts.URL+"/goals/new/checklist", form)
		defer func() { _ = resp.Body.Close() }()
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("the checklist answered %d:\n%s", resp.StatusCode, body)
		}
		return readyCard(t, body)
	}

	card, activate := checklist(form)
	want := map[string]bool{"Why it matters": true, "Owner": true, "Dated with a delivery date, or Ongoing": true, "A Milestone or Metric": true}
	if got := activationItems(t, card); !maps.Equal(got, want) {
		t.Errorf("without an open editor the checklist = %v, want %v", got, want)
	}
	if strings.Contains(activate, " disabled") {
		t.Errorf("Create and activate is disabled with no editor open: %s", activate)
	}

	editing := url.Values{}
	maps.Copy(editing, form)
	editing.Set("editing", "Milestone")
	card, activate = checklist(editing)
	want = map[string]bool{"Why it matters": true, "Owner": true, "Dated with a delivery date, or Ongoing": true, "Save the Milestone you started": false}
	if got := activationItems(t, card); !maps.Equal(got, want) {
		t.Errorf("with a Milestone editor open the checklist = %v, want %v", got, want)
	}
	if !strings.Contains(card, "3 of 4 done") {
		t.Errorf("the card doesn't say 3 of 4 done:\n%s", card)
	}
	if link := tagAround(t, card, ">Save the Milestone you started<"); attr(link, "href") != "#gf-editor" {
		t.Errorf("the item doesn't link to the open editor: %s", link)
	}
	if !strings.Contains(activate, " disabled") {
		t.Errorf("Create and activate is enabled with an editor open: %s", activate)
	}
}

// A Metric editor still open at submit is sent like a saved item: its
// editing marker changes nothing, and the Metric it holds, numbered past the
// rows it was opened after, is created with the Goal (#369).
func TestNewGoalFormCreatesWhatAnOpenEditorHolds(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, ts.URL+"/goals/new", newGoalForm(map[string]string{
		"editing":             "Metric",
		"metrics[3].name":     "p95 latency",
		"metrics[3].unit":     "ms",
		"metrics[3].baseline": "1200", "metrics[3].target": "400", "metrics[3].target_date": "2026-12-31",
	}))
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Request.URL.Path, "/goals/") {
		t.Fatalf("the post answered %d at %s:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil || len(goals) != 1 {
		t.Fatalf("ListGoals: %d Goals, %v", len(goals), err)
	}
	metrics, err := h.Service.ListMetrics(context.Background(), goals[0].ID)
	if err != nil {
		t.Fatalf("ListMetrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "p95 latency" || metrics[0].Unit != "ms" || metrics[0].Baseline != 1200 || metrics[0].Target != 400 {
		t.Errorf("created Metrics %+v, want p95 latency 1200 → 400 ms", metrics)
	}
}

// How you'll know carries what its script builds Milestones and Metrics from:
// an editor <template> of each, headed New Milestone or New Metric, holding
// the editing marker, inputs named as the rows' are, and Save and Cancel that
// don't submit; and a saved item's <template> of each, with its name, its
// detail, and Edit and Remove. The section carries the company's current
// year, for script to write a date as the app does, on the New goal page and
// on a refused create alike (#369).
func TestNewGoalPageCarriesTheMeasureEditorsTemplates(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.Clock.Set(time.Date(2027, 6, 1, 12, 0, 0, 0, time.UTC))
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	know := pageElement(t, getBody(t, client, ts.URL+"/goals/new"), "section", "goal-form-know")
	if year := attr(openTag(know), "data-year"); year != "2027" {
		t.Errorf("How you'll know carries data-year=%q, want the company's 2027: %s", year, openTag(know))
	}
	editors := map[string]struct {
		measure, testID string
		parts           []string
	}{
		"milestones": {"Milestone", "milestone-editor", []string{"name", "date"}},
		"metrics":    {"Metric", "metric-editor", []string{"name", "unit", "baseline", "target", "target_date", "direction"}},
	}
	for section, want := range editors {
		template := between(t, know, `<template id="`+section+`-editor">`, "</template>")
		if !strings.Contains(template, `data-testid="`+want.testID+`"`) {
			t.Errorf("the %s editor <template> has no %s:\n%s", section, want.testID, template)
		}
		if !strings.Contains(template, ">New "+want.measure+"<") {
			t.Errorf("the %s editor isn't headed New %s:\n%s", section, want.measure, template)
		}
		if marker := tagAround(t, template, `name="editing"`); attr(marker, "type") != "hidden" || attr(marker, "value") != want.measure {
			t.Errorf("the %s editor's editing marker isn't a hidden %s: %s", section, want.measure, marker)
		}
		for _, part := range want.parts {
			tagAround(t, template, `name="`+section+`[__i__].`+part+`"`)
		}
		for _, label := range []string{"Save", "Cancel"} {
			if button := tagAround(t, template, ">"+label+"</button>"); attr(button, "type") != "button" {
				t.Errorf("the %s editor's %s would submit the form: %s", section, label, button)
			}
		}
	}
	items := map[string]string{"milestones": "milestone-item", "metrics": "metric-item"}
	for section, testID := range items {
		template := between(t, know, `<template id="`+section+`-item">`, "</template>")
		for _, part := range []string{`data-testid="` + testID + `"`, `data-testid="measure-item-name"`, `data-testid="measure-item-detail"`} {
			if !strings.Contains(template, part) {
				t.Errorf("the %s item <template> has no %s:\n%s", section, part, template)
			}
		}
		for _, label := range []string{"Edit", "Remove"} {
			if button := tagAround(t, template, ">"+label+"</button>"); attr(button, "type") != "button" || attr(button, "class") != "btn quiet sm" {
				t.Errorf("the %s item's %s isn't a quiet button that doesn't submit: %s", section, label, button)
			}
		}
	}

	resp := postForm(t, client, ts.URL+"/goals/new", newGoalForm(map[string]string{"milestones[0].name": "Beta"}))
	defer func() { _ = resp.Body.Close() }()
	refused := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a Milestone with no date answered %d, want 422", resp.StatusCode)
	}
	if year := attr(openTag(pageElement(t, refused, "section", "goal-form-know")), "data-year"); year != "2027" {
		t.Errorf("a refused create's How you'll know carries data-year=%q, want 2027", year)
	}
}

// A Custom cadence above 90 days is refused beside the days typed, in the
// same 422 as the form's other problems, and the days input offers no more
// than 90 (#675).
func TestNewGoalFormRefusesACustomCadenceOverNinetyDays(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	if days := formField(t, getPage(t, client, ts.URL+"/goals/new"), "cadence_days"); attrOf(days.Node, "max") != "90" {
		t.Errorf("Custom's days input has max=%q, want 90: %s", attrOf(days.Node, "max"), renderNode(days.Node))
	}

	form := parsePage(t, refusedNewGoal(t, client, ts.URL, url.Values{
		"title": {"Cut checkout latency"}, "why_it_matters": {""},
		"cadence": {"custom"}, "cadence_days": {"91"},
	}))
	days := formField(t, form, "cadence_days")
	if attrOf(days.Node, "id") != "cadence" || days.Value != "91" || attrOf(days.Node, "aria-invalid") != "true" {
		t.Errorf("#cadence isn't the 91 days typed, marked invalid: %s", renderNode(days.Node))
	}
	if days.Description != "The Check-in cadence can be at most 90 days." {
		t.Errorf("#cadence is described by %q, want the most it can be", days.Description)
	}
	var cadenceError *nethtml.Node
	for _, e := range findAllByTestID(form, "input-error") {
		if attrOf(e, "id") == "cadence-error" {
			cadenceError = e
		}
	}
	if cadenceError == nil || textOf(cadenceError) != "The Check-in cadence can be at most 90 days." {
		t.Errorf("no input-error cadence-error says the most the cadence can be:\n%s", renderNode(form))
	}
	if why := formField(t, form, "why_it_matters"); why.Description != "A Goal needs to say why it matters." {
		t.Errorf("Why it matters is described by %q, want its own problem in the same 422", why.Description)
	}
}
