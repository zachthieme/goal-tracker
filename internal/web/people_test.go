package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

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
