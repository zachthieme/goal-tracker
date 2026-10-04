package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The New goal page is a plain form posting to itself: Title, focused, and So
// What.
func TestNewGoalPageOffersTitleAndSoWhat(t *testing.T) {
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
	tagAround(t, form, `name="so_what"`)
}

// Submitting the New goal form with a Title and So What creates a Proposed
// Goal the submitter owns and lands on its page, by a plain post with no
// script. No toast: the Goal's page is the confirmation.
func TestNewGoalFormCreatesAProposedGoalAndLandsOnIt(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("sam@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	resp := postForm(t, client, ts.URL+"/goals/new", url.Values{"title": {"Cut checkout latency"}, "so_what": {"Shoppers abandon slow carts."}})
	page := readBody(t, resp)
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("the post created %d Goals, want 1", len(goals))
	}
	g := goals[0]
	if g.Title != "Cut checkout latency" || g.SoWhat != "Shoppers abandon slow carts." || g.Lifecycle != domain.LifecycleProposed || g.Owner.Email != "sam@example.com" {
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
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity || resp.Request.URL.Path != "/goals/new" {
			t.Fatalf("posting %v answered %d at %s, want 422 at /goals/new:\n%s", form, resp.StatusCode, resp.Request.URL, page)
		}
		return page
	}

	page := refused(url.Values{"title": {""}, "so_what": {""}})
	summary := pageElement(t, page, "div", "goal-form-errors")
	if !strings.Contains(openTag(summary), `role="alert"`) {
		t.Errorf("the error summary isn't an alert: %s", openTag(summary))
	}
	form := pageElement(t, page, "form", "goal-form")
	for name, message := range map[string]string{"title": "a Goal needs a title", "so_what": "a Goal needs a So What"} {
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

	page = refused(url.Values{"title": {"Cut checkout latency"}, "so_what": {" "}})
	form = pageElement(t, page, "form", "goal-form")
	title := tagAround(t, form, `name="title"`)
	if attr(title, "value") != "Cut checkout latency" || attr(title, "aria-invalid") != "" {
		t.Errorf("Title isn't kept as typed and valid: %s", title)
	}
	if attr(tagAround(t, form, `name="so_what"`), "aria-invalid") != "true" {
		t.Errorf("a blank So What isn't marked invalid:\n%s", form)
	}

	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Errorf("refused posts created %d Goals", len(goals))
	}
}

// newGoalForm is a New goal post with a Title and So What, to add rows to.
func newGoalForm(rows map[string]string) url.Values {
	form := url.Values{"title": {"Cut checkout latency"}, "so_what": {"Shoppers abandon slow carts."}}
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
	if described == "" || !strings.Contains(between(t, form, `id="`+described+`"`, "</"), want) {
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
	wantRefused(t, form, "metrics[0].direction", "a Metric needs a direction")
	if goals, _ := h.Service.ListGoals(context.Background()); len(goals) != 0 {
		t.Fatalf("a refused post created %d Goals", len(goals))
	}

	resp := postForm(t, client, ts.URL+"/goals/new", holdSteady("down"))
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
