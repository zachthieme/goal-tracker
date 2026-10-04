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
