package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	nethtml "golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The person search offers everyone whose Name or email holds what was
// typed, ignoring case, by Name, and never a Departed person, who can't act
// (CONTEXT.md: Departed).
func TestPersonSearchFindsPeopleByNameOrEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	h.SignInNamed("pshah@example.com", "Priya Shah")
	h.SignInNamed("priya@example.com", "Priya Patel")
	old := h.SignInNamed("old@example.com", "Priya Old")
	h.SignInNamed("sam@example.com", "Sam")
	h.MarkDeparted(boss, old)
	client := signInClient(t, ts.URL, "boss@example.com")
	search := func(q string) string { return ts.URL + "/people/search?q=" + url.QueryEscape(q) }

	results := findAllByTestID(getPage(t, client, search("priya")), "person-result")
	if got, want := textsOf(results), []string{"Priya Patel priya@example.com", "Priya Shah pshah@example.com"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("searching priya offers %q, want %q", got, want)
	}

	results = findAllByTestID(getPage(t, client, search("  PSHAH@ ")), "person-result")
	if got, want := textsOf(results), []string{"Priya Shah pshah@example.com"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("searching PSHAH@ offers %q, want %q", got, want)
	}

	none := getPage(t, client, search("zzz"))
	if hasTestID(none, "person-result") || textOf(findByTestID(t, none, "person-no-results")) != "No one matches." {
		t.Errorf("searching zzz doesn't say no one matches:\n%s", renderNode(none))
	}

	blank := getPage(t, client, search(" "))
	if hasTestID(blank, "person-result") || hasTestID(blank, "person-no-results") {
		t.Errorf("a blank search answers something:\n%s", renderNode(blank))
	}
}

// The search answers at most personSearchLimit people, however many match.
func TestPersonSearchAnswersAtMostItsLimit(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	for i := range 12 {
		h.SignInNamed(fmt.Sprintf("person%02d@example.com", i), fmt.Sprintf("Person %02d", i))
	}
	client := signInClient(t, ts.URL, "person00@example.com")

	results := findAllByTestID(getPage(t, client, ts.URL+"/people/search?q=person"), "person-result")
	if len(results) != 10 {
		t.Errorf("searching person, which 12 people match, answers %d, want 10: %q", len(results), textsOf(results))
	}
}

// Signed out, the search answers no one: it sends the person to sign in, as
// every page needing a session does.
func TestPersonSearchSignedOutSendsToSignIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignInNamed("sam@example.com", "Sam")

	resp, err := ctxGet(t, noRedirects(http.DefaultClient), ts.URL+"/people/search?q=sam")
	if err != nil {
		t.Fatalf("GET /people/search: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := readBody(t, resp)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/signin" {
		t.Errorf("signed out: status %d to %q, want 303 to /signin", resp.StatusCode, loc)
	}
	if hasTestID(parsePage(t, body), "person-result") {
		t.Errorf("signed out, the search answers people:\n%s", body)
	}
}

// Each form that chooses a person holds the person picker: without script, a
// required email input, captioned as before, with its results list beside it.
func TestEveryFormChoosingAPersonHoldsThePersonPicker(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	orphan := h.ActiveGoal(kim, "Orphaned", "It matters.")
	h.MarkDeparted(ada, kim)
	def := h.SaveReportDefinition(sam, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}})
	pub := h.PublishReport(sam, def)
	_, err := h.Service.RaiseActionItem(t.Context(), sam.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Book the EU legal review.", OwnerID: sam.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if _, err := h.Service.AddComment(t.Context(), sam.ID, pub.ID, goal.ID, "Why did the vendor slip?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	ts := newServer(t, h)
	samClient := signInClient(t, ts.URL, "sam@example.com")
	adaClient := signInClient(t, ts.URL, "ada@example.com")
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)

	for _, tc := range []struct {
		client        *http.Client
		page, form    string
		name, caption string
	}{
		{samClient, goalPageURL(ts.URL, goal) + "?open=delegates", "add-delegate", "email", "Delegate email"},
		{samClient, goalPageURL(ts.URL, goal) + "?open=handoff", "handoff-goal", "to_email", "New Owner's email"},
		{adaClient, goalPageURL(ts.URL, orphan) + "?open=reassign", "reassign-goal", "email", "New Owner's email"},
		{samClient, draftURL, "add-report-delegate", "email", "Report Delegate email"},
		{samClient, pubURL, "action-item-form", "owner", "Owner"},
		{samClient, pubURL, "comment-action-item-form", "owner", "Owner"},
		{samClient, pubURL, "reassign-action-item", "owner", "New owner"},
	} {
		form := findByTestID(t, getPage(t, tc.client, tc.page), tc.form)
		findByTestID(t, form, "person-results")
		f := formField(t, form, tc.name)
		if f.Type != "email" || !hasAttr(f.Node, "required") || !hasAttr(f.Node, "data-person-picker") {
			t.Errorf("%s's %s isn't a required email input the person picker upgrades: %s", tc.form, tc.name, renderNode(f.Node))
		}
		if f.Label != tc.caption {
			t.Errorf("%s's %s is labelled %q, want %q", tc.form, tc.name, f.Label, tc.caption)
		}
	}
}

// A framed page loads the person picker's script once, in its head, so every
// picker on the page, and any htmx swaps in, is upgraded to search.
func TestFramedPageLoadsThePersonPickerScriptOnce(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "sam@example.com")

	page := getPage(t, client, ts.URL+"/home")
	heads := findAll(page, func(n *nethtml.Node) bool { return n.Data == "head" })
	if len(heads) != 1 {
		t.Fatalf("page has %d heads, want 1", len(heads))
	}
	pickers := findAll(page, func(n *nethtml.Node) bool {
		return n.Data == "script" && n.FirstChild != nil && strings.Contains(n.FirstChild.Data, "input[data-person-picker]")
	})
	if len(pickers) != 1 || pickers[0].Parent != heads[0] {
		t.Errorf("page loads the person picker's script %d times, want once, in its head", len(pickers))
	}
}
