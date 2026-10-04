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

// suggestPath is a Goal's Suggest a parent form.
func suggestPath(g domain.Goal) string {
	return fmt.Sprintf("/goals/%d/suggest-parent", g.ID)
}

// parentOption is the suggest form's <option> for g.
func parentOption(g domain.Goal) string {
	return fmt.Sprintf(`<option value="%d"`, g.ID)
}

// Someone other than the Owner follows Suggest a parent from the Goal's menu
// to a form picking from the Active and Proposed Goals the Goal could
// contribute to: not itself, its parents, its Pending parents, a Goal below
// it, or one Done or On Hold. Sending it records the suggestion, with its
// note, and returns to the Goal page with no toast.
func TestSuggestingAParentFromTheGoalMenu(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	active := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	proposed := h.CreateGoal(pat, "Cut costs", "Budgets are tight.")
	linked := h.ActiveGoal(sam, "Linked parent", "Already linked.")
	h.RequestLink(sam, goal, linked, "")
	pending := h.ActiveGoal(pat, "Pending parent", "Already requested.")
	h.RequestLink(sam, goal, pending, "")
	below := h.ActiveChildOf(sam, goal, "Below the Goal", "Contributes to it.")
	onHold := h.OnHoldGoal(pat, "On Hold parent", "Paused.", "Waiting on budget.")
	done := h.ActiveGoal(pat, "Done parent", "Finished.")
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: done.ID, AuthorID: pat.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "It shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin Done: %v", err)
	}

	client := signInClient(t, ts.URL, "pat@example.com")
	if got := menuItems(t, getBody(t, client, goalPageURL(ts.URL, goal)))["Suggest a parent"]; got != suggestPath(goal) {
		t.Fatalf("the menu's Suggest a parent links to %q, want %s", got, suggestPath(goal))
	}
	form := pageElement(t, getBody(t, client, ts.URL+suggestPath(goal)), "form", "suggest-parent")
	for _, g := range []domain.Goal{active, proposed} {
		if !strings.Contains(form, parentOption(g)) {
			t.Errorf("the picker leaves out %q:\n%s", g.Title, form)
		}
	}
	for _, g := range []domain.Goal{goal, linked, pending, below, onHold, done} {
		if strings.Contains(form, parentOption(g)) {
			t.Errorf("the picker offers %q:\n%s", g.Title, form)
		}
	}

	resp := postForm(t, client, ts.URL+suggestPath(goal), url.Values{"parent_id": {fmt.Sprint(active.ID)}, "note": {"outages start here"}})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", goal.ID) {
		t.Fatalf("suggesting landed on %s (status %d), want the Goal page", resp.Request.URL.Path, resp.StatusCode)
	}
	if strings.Contains(page, `data-testid="toast"`) {
		t.Errorf("suggesting shows a toast:\n%s", page)
	}
	open, err := h.Service.OpenParentSuggestions(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("OpenParentSuggestions: %v", err)
	}
	if len(open) != 1 || open[0].Parent.ID != active.ID || open[0].SuggestedBy.ID != pat.ID || open[0].Note != "outages start here" {
		t.Errorf("open suggestions = %+v, want pat's of the parent with the note", open)
	}

	owner := signInClient(t, ts.URL, "sam@example.com")
	if _, ok := menuItems(t, getBody(t, owner, goalPageURL(ts.URL, goal)))["Suggest a parent"]; ok {
		t.Errorf("the Owner is offered Suggest a parent")
	}
}

// A refused suggestion comes back as the form with the reason, as sent, and
// records nothing more.
func TestARefusedSuggestionReRendersTheForm(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	parent := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	if _, err := h.Service.SuggestParent(context.Background(), pat.ID, goal.ID, parent.ID, ""); err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}

	client := signInClient(t, ts.URL, "kim@example.com")
	resp := postForm(t, client, ts.URL+suggestPath(goal), url.Values{"parent_id": {fmt.Sprint(parent.ID)}, "note": {"me too"}})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	form := pageElement(t, page, "form", "suggest-parent")
	if reason := pageElement(t, page, "p", "form-error"); !strings.Contains(reason, "Pat Lee has already suggested that parent") {
		t.Errorf("the reason doesn't say who suggested it: %s", reason)
	}
	if !strings.Contains(form, "me too") || !strings.Contains(form, parentOption(parent)+` selected`) {
		t.Errorf("the form doesn't come back as sent:\n%s", form)
	}
	if all, _ := h.Service.ParentSuggestions(context.Background(), goal.ID); len(all) != 1 {
		t.Errorf("the Goal has %d suggestions, want 1", len(all))
	}
}
