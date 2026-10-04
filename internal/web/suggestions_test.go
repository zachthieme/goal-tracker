package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
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

// The page names the Goal and its Owner as a sentence, with no stray space
// before the comma: "For Migrate displays, owned by sam" (#180).
func TestSuggestAParentPageNamesTheGoalAndOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+suggestPath(goal))
	lead := between(t, page, "For <a ", "</p>")
	if want := fmt.Sprintf(`For <a href="/goals/%d">Migrate displays</a>, owned by `, goal.ID); !strings.Contains(lead, want) {
		t.Errorf("the lead doesn't read %q:\n%s", want, lead)
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

// Someone else's Unaligned Goal on the Risks page has Suggest a parent as its
// Fix, and following it suggests a parent the same way.
func TestSuggestingAParentFromTheRisksFix(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	parent := h.CreateGoal(kim, "Grow revenue", "It pays for everything.")

	client := signInClient(t, ts.URL, "kim@example.com")
	label, href, _ := riskFix(t, riskRowOf(t, getBody(t, client, ts.URL+"/risks"), loner))
	if label != "Suggest a parent" {
		t.Fatalf("Fix = %q, want Suggest a parent", label)
	}
	form := pageElement(t, getBody(t, client, ts.URL+href), "form", "suggest-parent")
	action := html.UnescapeString(attr(openTag(form)+">", "action"))
	resp := postForm(t, client, ts.URL+action, url.Values{"parent_id": {fmt.Sprint(parent.ID)}})
	_ = readBody(t, resp)
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", loner.ID) {
		t.Errorf("suggesting landed on %s, want the Goal page", resp.Request.URL.Path)
	}
	if open, _ := h.Service.OpenParentSuggestions(context.Background(), loner.ID); len(open) != 1 || open[0].Parent.ID != parent.ID {
		t.Errorf("open suggestions = %+v, want kim's of the parent", open)
	}
}

// suggestionScene is Pat Lee's suggestion, with a note, that Sam's Goal
// contribute to Kim's parent.
func suggestionScene(t *testing.T, h *testsupport.Harness) (goal, parent domain.Goal, s domain.ParentSuggestion) {
	t.Helper()
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	kim := h.SignIn("kim@example.com")
	goal = h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	parent = h.CreateGoal(kim, "Reduce outages", "Outages cost trust.")
	s, err := h.Service.SuggestParent(context.Background(), pat.ID, goal.ID, parent.ID, "displays cause outages")
	if err != nil {
		t.Fatalf("SuggestParent: %v", err)
	}
	return goal, parent, s
}

// The Goal page lists its open suggestions for its Owner under Suggested
// parents, each saying who suggested which parent and why, with Decline and
// Accept returning to the Goal page; accepting requests the link. The
// suggester sees their own with Withdraw instead, and anyone else sees none.
func TestGoalPageListsSuggestedParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, parent, s := suggestionScene(t, h)
	h.SignIn("mel@example.com")
	base := fmt.Sprintf("/parent-suggestions/%d", s.ID)

	if page := getBody(t, signInClient(t, ts.URL, "mel@example.com"), goalPageURL(ts.URL, goal)); strings.Contains(page, `data-testid="goal-suggestions"`) {
		t.Errorf("someone else sees the suggestions")
	}

	pat := signInClient(t, ts.URL, "pat@example.com")
	list := pageElement(t, getBody(t, pat, goalPageURL(ts.URL, goal)), "ul", "goal-suggestions")
	if !strings.Contains(list, `action="`+base+`/withdraw"`) || strings.Contains(list, base+"/accept") || strings.Contains(list, base+"/decline") {
		t.Errorf("the suggester's list doesn't offer just Withdraw:\n%s", list)
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	list = pageElement(t, getBody(t, sam, goalPageURL(ts.URL, goal)), "ul", "goal-suggestions")
	for _, want := range []string{"Pat Lee", navTo(parent.ID), "displays cause outages", `action="` + base + `/decline"`, `action="` + base + `/accept"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the Owner's list lacks %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, base+"/withdraw") {
		t.Errorf("the Owner is offered Withdraw:\n%s", list)
	}

	resp := postForm(t, sam, ts.URL+base+"/accept", toastFormOf(t, list, base+"/accept"))
	page := readBody(t, resp)
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", goal.ID) {
		t.Errorf("accepting landed on %s, want the Goal page", resp.Request.URL.Path)
	}
	if strings.Contains(page, `data-testid="goal-suggestions"`) {
		t.Errorf("the Goal page still lists the accepted suggestion")
	}
	if pending := h.ParentsOf(goal); len(pending) != 0 {
		t.Errorf("the link is accepted before Kim decides: %+v", pending)
	}
	links, _ := h.Service.PendingParentLinks(context.Background(), goal.ID)
	if len(links) != 1 || links[0].Goal.ID != parent.ID {
		t.Errorf("Pending parents = %+v, want the suggested parent", links)
	}
}

// The suggester withdraws their open suggestion from the Goal page, which
// then no longer lists it.
func TestWithdrawingASuggestionFromTheGoalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, _, s := suggestionScene(t, h)

	pat := signInClient(t, ts.URL, "pat@example.com")
	resp := postForm(t, pat, fmt.Sprintf("%s/parent-suggestions/%d/withdraw", ts.URL, s.ID), url.Values{})
	page := readBody(t, resp)
	if resp.Request.URL.Path != fmt.Sprintf("/goals/%d", goal.ID) {
		t.Errorf("withdrawing landed on %s, want the Goal page", resp.Request.URL.Path)
	}
	if strings.Contains(page, `data-testid="goal-suggestions"`) {
		t.Errorf("the Goal page still lists the withdrawn suggestion")
	}
	if got, _ := h.Service.ParentSuggestion(context.Background(), s.ID); got.Status != domain.SuggestionWithdrawn {
		t.Errorf("Status = %q, want withdrawn", got.Status)
	}
}

// toastFormOf is the hidden inputs of the form in html posting to action.
func toastFormOf(t *testing.T, html, action string) url.Values {
	t.Helper()
	form := between(t, html, `action="`+action+`"`, "</form>")
	fields := url.Values{}
	for _, m := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(form, -1) {
		fields.Set(m[1], m[2])
	}
	return fields
}

// After declining a suggestion, from Home or from the Goal page, the page
// shown next carries a toast saying so with an Undo; the toast isn't shown on
// a later visit, and Undo returns to the same page with the suggestion open
// again.
func TestDecliningASuggestionOffersUndoOnce(t *testing.T) {
	t.Parallel()

	for _, from := range []string{"/home", "goal"} {
		t.Run(from, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			ts := newServer(t, h)
			goal, _, s := suggestionScene(t, h)
			page := from
			if from == "goal" {
				page = fmt.Sprintf("/goals/%d", goal.ID)
			}
			sam := signInClient(t, ts.URL, "sam@example.com")
			decline := fmt.Sprintf("/parent-suggestions/%d/decline", s.ID)
			resp := postForm(t, sam, ts.URL+decline, toastFormOf(t, getBody(t, sam, ts.URL+page), decline))
			landed := readBody(t, resp)
			if resp.Request.URL.Path != page {
				t.Fatalf("declining landed on %s, want %s", resp.Request.URL.Path, page)
			}
			toast := pageElement(t, landed, "aside", "toast")
			for _, want := range []string{"Migrate displays", "Reduce outages", "Undo"} {
				if !strings.Contains(toast, want) {
					t.Errorf("toast missing %q:\n%s", want, toast)
				}
			}
			if got, _ := h.Service.ParentSuggestion(context.Background(), s.ID); got.Status != domain.SuggestionDeclined {
				t.Errorf("Status = %q, want declined", got.Status)
			}
			if later := getBody(t, sam, ts.URL+page); strings.Contains(later, `data-testid="toast"`) {
				t.Errorf("toast shown again on a later visit")
			}

			resp = postForm(t, sam, ts.URL+undoAction(t, landed), toastFields(t, landed))
			_ = readBody(t, resp)
			if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != page {
				t.Errorf("undo landed on %s (status %d), want %s", resp.Request.URL.Path, resp.StatusCode, page)
			}
			if got, _ := h.Service.ParentSuggestion(context.Background(), s.ID); got.Status != domain.SuggestionOpen {
				t.Errorf("Status = %q, want open again", got.Status)
			}
		})
	}
}

// An Undo of a decline is refused, with a page saying why, once the link has
// been requested since.
func TestUndoingADeclineIsRefusedOnceTheLinkWasRequested(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal, parent, s := suggestionScene(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")
	resp := postForm(t, sam, fmt.Sprintf("%s/parent-suggestions/%d/decline", ts.URL, s.ID), url.Values{"from": {"home"}})
	landed := readBody(t, resp)
	h.RequestLink(h.SignIn("sam@example.com"), goal, parent, "")

	resp = postForm(t, sam, ts.URL+undoAction(t, landed), toastFields(t, landed))
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(page, "requested since") {
		t.Errorf("undo: status %d, want 422 saying the link was requested since:\n%s", resp.StatusCode, page)
	}
	if got, _ := h.Service.ParentSuggestion(context.Background(), s.ID); got.Status != domain.SuggestionDeclined {
		t.Errorf("Status = %q, want still declined", got.Status)
	}
}
