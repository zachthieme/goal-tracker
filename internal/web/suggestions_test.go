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

	nethtml "golang.org/x/net/html"

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
	sc := newSuggestableScene(t, h)
	goal, active, pat := sc.goal, sc.active, sc.pat

	client := signInClient(t, ts.URL, "pat@example.com")
	if got := menuItems(t, getBody(t, client, goalPageURL(ts.URL, goal)))["Suggest a parent"]; got != suggestPath(goal) {
		t.Fatalf("the menu's Suggest a parent links to %q, want %s", got, suggestPath(goal))
	}
	form := pageElement(t, getBody(t, client, ts.URL+suggestPath(goal)), "form", "suggest-parent")
	for _, g := range []domain.Goal{sc.active, sc.proposed} {
		if !strings.Contains(form, parentOption(g)) {
			t.Errorf("the picker leaves out %q:\n%s", g.Title, form)
		}
	}
	for _, g := range sc.left {
		if strings.Contains(form, parentOption(g)) {
			t.Errorf("the picker offers %q:\n%s", g.Title, form)
		}
	}

	resp := postForm(t, client, ts.URL+suggestPath(goal), url.Values{"parent_id": {fmt.Sprint(active.ID)}, "note": {"outages start here"}})
	defer func() { _ = resp.Body.Close() }()
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

// suggestableScene is pat's view of sam's Active Goal: Goals that may be
// suggested as its parent, one Active with a Yellow Check-in and one
// Proposed, both Pat Lee's, and those that may not: the Goal itself, its
// parent, its Pending parent, a Goal below it, and an On Hold and a Done
// Goal.
type suggestableScene struct {
	pat                    domain.Account
	goal, active, proposed domain.Goal
	left                   []domain.Goal
}

// newSuggestableScene signs in sam and Pat Lee and sets up a suggestableScene.
func newSuggestableScene(t *testing.T, h *testsupport.Harness) suggestableScene {
	t.Helper()
	sam := h.SignIn("sam@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Lee")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	active := h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	h.Checkin(pat, active.ID, domain.HealthYellow, "Slipping.", "Add a second on-call.", h.Clock.Now().AddDate(0, 1, 0))
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
	return suggestableScene{pat: pat, goal: goal, active: active, proposed: proposed, left: []domain.Goal{goal, linked, pending, below, onHold, done}}
}

// Suggest a parent's picker searches only the Goals its select offers, those
// the Goal may be suggested under, less any already picked, each result
// showing its Health badge, title and Owner (#656).
func TestSuggestAParentSearchFindsOnlySuggestableParents(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sc := newSuggestableScene(t, h)
	client := signInClient(t, ts.URL, "pat@example.com")
	search := func(picked ...domain.Goal) []*nethtml.Node {
		q := url.Values{"goal": {fmt.Sprint(sc.goal.ID)}, "for": {"suggestion"}, "q": {""}}
		for _, g := range picked {
			q.Add("parent_id", fmt.Sprint(g.ID))
		}
		return findAllByTestID(getPage(t, client, ts.URL+"/goals/search?"+q.Encode()), "parent-result")
	}
	pick := func(r *nethtml.Node) *nethtml.Node {
		return findAll(r, func(n *nethtml.Node) bool { return n.Data == "button" })[0]
	}

	results := search()
	var titles []string
	for _, r := range results {
		titles = append(titles, textOf(pick(r)))
	}
	if len(results) != 2 {
		t.Fatalf("the search found %q, want only %q and %q", titles, sc.active.Title, sc.proposed.Title)
	}
	for _, r := range results {
		if !strings.Contains(textOf(pick(r)), "Pat Lee") {
			t.Errorf("the result %q doesn't show its Owner", textOf(pick(r)))
		}
		switch {
		case strings.Contains(textOf(pick(r)), sc.active.Title):
			assertHealthChip(t, healthChipIn(t, pick(r)), domain.HealthYellow)
		case strings.Contains(textOf(pick(r)), sc.proposed.Title):
			if lc := findAll(pick(r), func(n *nethtml.Node) bool { return textOf(n) == domain.LifecycleProposed }); len(lc) == 0 {
				t.Errorf("the Proposed result shows no Lifecycle badge: %s", renderNode(r))
			}
		default:
			t.Errorf("the search found %q, which can't be suggested", textOf(pick(r)))
		}
	}
	if left := search(sc.active); len(left) != 1 || !strings.Contains(textOf(pick(left[0])), sc.proposed.Title) {
		t.Errorf("with %q picked the search found %q, want only %q", sc.active.Title, textsOf(left), sc.proposed.Title)
	}
}

// The Suggest a parent page picks the parent with the Parent Goal picker,
// opening with nothing picked: its search, which takes the page's focus, and
// without script a select starting on "Choose a Goal…", so pressing Suggest
// untouched suggests no Goal that happened to sort first (#656).
func TestSuggestAParentPageOpensWithNoParentPicked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	h.CreateGoal(pat, "Cut costs", "Budgets are tight.")

	page := getPage(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+suggestPath(goal))
	picker := assertParentPickerOpensEmpty(t, findByTestID(t, page, "suggest-parent"))
	search := findAll(picker, func(n *nethtml.Node) bool { return attrOf(n, "id") == "parent-search" })[0]
	if want := fmt.Sprintf("/goals/search?goal=%d&for=suggestion", goal.ID); attrOf(search, "hx-get") != want {
		t.Errorf("the picker searches %q, want %q", attrOf(search, "hx-get"), want)
	}
	if focused := findAll(page, func(n *nethtml.Node) bool { return hasAttr(n, "autofocus") }); len(focused) != 1 || focused[0] != search {
		t.Errorf("the page focuses %d controls, want only the picker's search", len(focused))
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

// The Suggest a parent page sits in the flat frame, as sent and as refused: its
// name in the header strip, and its form a plain section, with no card.
func TestSuggestAParentPageIsInTheFlatFrame(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	client := signInClient(t, ts.URL, "pat@example.com")

	assertInFlatFrame(t, getBody(t, client, ts.URL+suggestPath(goal)), "Suggest a parent")
	assertInFlatFrame(t, postFormBody(t, client, ts.URL+suggestPath(goal), url.Values{"parent_id": {fmt.Sprint(goal.ID)}}), "Suggest a parent")
}

// A refused suggestion comes back as the form with the reason above it, the
// note as typed and nothing picked, and records nothing more (#656).
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
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	root := parsePage(t, page)
	if reason := textOf(findByTestID(t, root, "form-error")); !strings.Contains(reason, "Pat Lee has already suggested that parent") {
		t.Errorf("the reason doesn't say who suggested it: %s", reason)
	}
	form := findByTestID(t, root, "suggest-parent")
	if note := formField(t, form, "note").Value; note != "me too" {
		t.Errorf("the note comes back %q, want it as typed", note)
	}
	assertParentPickerOpensEmpty(t, form)
	if all, _ := h.Service.ParentSuggestions(context.Background(), goal.ID); len(all) != 1 {
		t.Errorf("the Goal has %d suggestions, want 1", len(all))
	}
}

// Suggesting as a parent a Goal that already contributes to the Goal is
// refused 409, with the form saying so in the sentence every cycle's refusal
// uses, naming both Goals by title, and records nothing (#645).
func TestSuggestingAParentThatWouldMakeACycleSaysSo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	below := h.ActiveChildOf(sam, goal, "Reduce outages", "Outages cost trust.")

	client := signInClient(t, ts.URL, "kim@example.com")
	resp := postForm(t, client, ts.URL+suggestPath(goal), url.Values{"parent_id": {fmt.Sprint(below.ID)}})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	want := "Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”."
	if got := textOf(findByTestID(t, page, "form-error")); got != want {
		t.Errorf("form-error = %q, want %q", got, want)
	}
	if all, _ := h.Service.ParentSuggestions(context.Background(), goal.ID); len(all) != 0 {
		t.Errorf("the Goal has %d suggestions, want none", len(all))
	}
}

// Suggest pressed with no parent picked suggests nothing: the form comes back,
// 422, with the reason beside the picker, which marks its controls refused,
// the note as typed and still nothing picked (#656).
func TestSuggestingWithNoParentPickedIsRefusedBesideThePicker(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.ActiveGoal(sam, "Migrate displays", "Old displays fail often.")
	h.ActiveGoal(pat, "Reduce outages", "Outages cost trust.")
	client := signInClient(t, ts.URL, "pat@example.com")

	for name, sent := range map[string]url.Values{
		"no parent_id":    {"note": {"outages start here"}},
		"empty parent_id": {"parent_id": {""}, "note": {"outages start here"}},
		"not an id":       {"parent_id": {"soon"}, "note": {"outages start here"}},
	} {
		resp := postForm(t, client, ts.URL+suggestPath(goal), sent)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, resp.StatusCode)
		}
		if hasTestID(page, "form-error") {
			t.Errorf("%s: the refusal is above the form, not beside the picker: %s", name, textOf(findByTestID(t, page, "form-error")))
		}
		form := findByTestID(t, page, "suggest-parent")
		picker := assertParentPickerOpensEmpty(t, form)
		assertPickerRefused(t, picker, "Pick a parent Goal to suggest.")
		if note := formField(t, form, "note").Value; note != "outages start here" {
			t.Errorf("%s: the note comes back %q, want it as typed", name, note)
		}
	}
	if all, _ := h.Service.ParentSuggestions(context.Background(), goal.ID); len(all) != 0 {
		t.Errorf("the Goal has %d suggestions, want none", len(all))
	}
}

// Someone else's Unlinked Goal on the Follow-ups page has Suggest a parent as its
// Fix, and following it suggests a parent the same way.
func TestSuggestingAParentFromTheFollowUpsFix(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	loner := h.ActiveGoal(sam, "Side project", "Nobody asked.")
	parent := h.CreateGoal(kim, "Grow revenue", "It pays for everything.")

	client := signInClient(t, ts.URL, "kim@example.com")
	label, href, _ := followUpFix(t, followUpRowOf(t, getBody(t, client, ts.URL+"/follow-ups?org=all"), loner))
	if label != "Suggest a parent" {
		t.Fatalf("Fix = %q, want Suggest a parent", label)
	}
	form := pageElement(t, getBody(t, client, ts.URL+href), "form", "suggest-parent")
	action := html.UnescapeString(attr(openTag(form)+">", "action"))
	resp := postForm(t, client, ts.URL+action, url.Values{"parent_id": {fmt.Sprint(parent.ID)}})
	defer func() { _ = resp.Body.Close() }()
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
// parents, each saying who suggested which parent and why, with Accept and
// Decline returning to the Goal page; accepting requests the link. The
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
	listed := parsePage(t, list)
	if accept, decline := buttonNamed(listed, "Accept"), buttonNamed(listed, "Decline"); accept == nil || decline == nil || !precedes(accept, decline) {
		t.Errorf("the Owner's list does not put Accept before Decline:\n%s", list)
	}

	resp := postForm(t, sam, ts.URL+base+"/accept", toastFormOf(t, list, base+"/accept"))
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
			defer func() { _ = resp.Body.Close() }()
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
	resp := postForm(t, sam, fmt.Sprintf("%s/parent-suggestions/%d/decline", ts.URL, s.ID), url.Values{"return": {"/home"}})
	defer func() { _ = resp.Body.Close() }()
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
