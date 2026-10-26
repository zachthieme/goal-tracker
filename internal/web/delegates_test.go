package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"golang.org/x/net/html"
)

// TestSmokeDelegateChecksInOnOwnersGoal is the Delegate HTTP smoke test: an Owner
// authorizes a Delegate on the Goal page, the Delegate finds the Goal on their
// "Delegated to me" page, and checks in on it — the Check-in recording the
// Delegate as author and the Owner it was written for (CONTEXT.md: Delegate).
func TestSmokeDelegateChecksInOnOwnersGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com") // the Delegate's account must exist to be added by email
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	samClient := signInClient(t, ts.URL, "sam@example.com")
	tpmClient := signInClient(t, ts.URL, "tpm@example.com")

	// The Owner authorizes the Delegate on the Goal page.
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/delegates", ts.URL, goal.ID), url.Values{
		"email": {"tpm@example.com"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add delegate: status %d", resp.StatusCode)
	}
	goalBody := getBody(t, samClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if !strings.Contains(goalBody, "tpm@example.com") || !strings.Contains(goalBody, `data-testid="delegate"`) {
		t.Errorf("goal page does not list the Delegate; body:\n%s", goalBody)
	}

	// The Delegate's page lists the Goal they may check in on.
	delegateBody := getBody(t, tpmClient, ts.URL+"/delegates")
	if !strings.Contains(delegateBody, "Reduce outages") {
		t.Errorf("delegate page does not list the delegated Goal; body:\n%s", delegateBody)
	}
	if !strings.Contains(delegateBody, fmt.Sprintf(`href="/goals/%d/checkin"`, goal.ID)) {
		t.Errorf("delegate page does not link to the Check-in page; body:\n%s", delegateBody)
	}
	_ = resp.Body.Close()

	// The Delegate checks in on the Goal.
	resp = postForm(t, tpmClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"On track — checked in for Sam."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delegate check-in: status %d", resp.StatusCode)
	}

	history, err := h.Service.ListCheckins(context.Background(), goal.ID)
	if err != nil {
		t.Fatalf("ListCheckins: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	if history[0].Author.ID != tpm.ID {
		t.Errorf("Check-in Author = %d, want the Delegate %d", history[0].Author.ID, tpm.ID)
	}
	if history[0].Owner.ID != sam.ID {
		t.Errorf("Check-in Owner = %d, want the Owner %d", history[0].Owner.ID, sam.ID)
	}
}

// A person who is neither the Owner nor a Delegate cannot submit a Check-in on
// someone else's Goal through the web (acceptance: Non-Delegates can't submit).
func TestSmokeNonDelegateCheckinForbidden(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	melClient := signInClient(t, ts.URL, "mel@example.com")
	resp := postForm(t, melClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Meddling."},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Delegate check-in status = %d, want 403", resp.StatusCode)
	}
	if history, _ := h.Service.ListCheckins(context.Background(), goal.ID); len(history) != 0 {
		t.Errorf("a Check-in was recorded by a non-Delegate: %d", len(history))
	}
}

// Only the Owner may authorize a Delegate through the web; another user's attempt
// is refused (CONTEXT.md: a person an Owner authorizes).
func TestSmokeNonOwnerCannotAddDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	melClient := signInClient(t, ts.URL, "mel@example.com")
	resp := postForm(t, melClient, fmt.Sprintf("%s/goals/%d/delegates", ts.URL, goal.ID), url.Values{
		"email": {"tpm@example.com"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Owner add delegate status = %d, want 403", resp.StatusCode)
	}
	if delegates, _ := h.Service.ListDelegates(context.Background(), goal.ID); len(delegates) != 0 {
		t.Errorf("a Delegate was added by a non-Owner: %+v", delegates)
	}
}

// Someone not the Owner who posts an email no one has is refused as not
// permitted, before the email is looked up, so the refusal says nothing of
// who has an Account.
func TestNonOwnerAddingAnUnknownEmailAsDelegateIsForbidden(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	melClient := signInClient(t, ts.URL, "mel@example.com")
	resp := postForm(t, melClient, fmt.Sprintf("%s/goals/%d/delegates", ts.URL, goal.ID), url.Values{
		"email": {"nobody@example.com"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Owner add of an unknown email as Delegate: status = %d, want 403", resp.StatusCode)
	}
}

// Only the Owner may remove a Delegate through the web: another signed-in
// person posting the Owner's Remove form is refused and the Delegate stays,
// and the Owner's own Remove takes them off the Goal (CONTEXT.md: a person an
// Owner authorizes).
func TestOnlyOwnerRemovesDelegateThroughTheWeb(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)

	samClient := signInClient(t, ts.URL, "sam@example.com")
	melClient := signInClient(t, ts.URL, "mel@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	delegatesShown := func() []string {
		return textsOf(findAllByTestID(getPage(t, samClient, goalURL), "delegate"))
	}

	removeAction := fmt.Sprintf("/goals/%d/delegates/remove", goal.ID)
	forms := findAll(getPage(t, samClient, goalURL+"?open=delegates"), func(n *html.Node) bool {
		return n.Data == "form" && attrOf(n, "action") == removeAction
	})
	if len(forms) != 1 || buttonNamed(forms[0], "Remove") == nil {
		t.Fatalf("the Owner's Manage Delegates form offers %d Remove forms, want one for tpm", len(forms))
	}
	if got := formField(t, forms[0], "email").Value; got != "tpm@example.com" {
		t.Fatalf("the Remove form names %q, want tpm@example.com", got)
	}
	remove := formSubmission(forms[0])

	resp := postForm(t, melClient, ts.URL+removeAction, remove)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Owner remove delegate status = %d, want 403", resp.StatusCode)
	}
	if got := delegatesShown(); !slices.Equal(got, []string{"tpm"}) {
		t.Errorf("Delegates after a non-Owner's remove = %q, want tpm still listed", got)
	}
	_ = resp.Body.Close()

	resp = postForm(t, samClient, ts.URL+removeAction, remove)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != fmt.Sprintf("/goals/%d", goal.ID) {
		t.Fatalf("Owner remove delegate: status %d at %s, want the Goal page", resp.StatusCode, resp.Request.URL.Path)
	}
	if got := delegatesShown(); len(got) != 0 {
		t.Errorf("Delegates after the Owner's remove = %q, want none", got)
	}
}

// A Delegate add or remove by someone not the Goal's Owner answers the
// refusal page, 403, saying only the Owner may, with a way Back to the Goal,
// whether posted plainly or by htmx.
func TestDelegateChangeBySomeoneNotTheOwnerAnswersTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	h.SignIn("mel@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	mel := noRedirects(signInClient(t, ts.URL, "mel@example.com"))
	back := fmt.Sprintf("/goals/%d", goal.ID)

	for _, c := range []struct {
		name, path, heading, reason string
	}{
		{"an add", back + "/delegates", "Can't add Delegate", "Only the Goal's Owner may authorize a Delegate."},
		{"a remove", back + "/delegates/remove", "Can't remove Delegate", "Only the Goal's Owner may remove a Delegate."},
	} {
		form := url.Values{"email": {"tpm@example.com"}}
		resp := postForm(t, mel, ts.URL+c.path, form)
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s by someone not the Owner: status %d, want 403", c.name, resp.StatusCode)
		}
		assertRefusalPage(t, page, c.heading, c.reason, back)

		body, status := postFormHX(t, mel, ts.URL+c.path, form)
		if status != http.StatusForbidden {
			t.Errorf("%s by someone not the Owner, by htmx: status %d, want 403", c.name, status)
		}
		assertRefusalPage(t, parsePage(t, body), c.heading, c.reason, back)
	}
}

// The Delegate's page shows each delegated Goal as a row with its Health, its
// Owner, when it was last checked in on, and a Check in link to the Check-in
// page, rather than embedding the whole form. It sits in the flat frame: its
// name in the header strip, and the Goals hairline rows, none a card.
func TestDelegatePageShowsGoalRows(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	checked := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, checked.ID)
	h.Checkin(tpm, checked.ID, domain.HealthYellow, "Wobbling.", "Add a second on-call.", testsupport.Epoch.AddDate(0, 2, 0))
	proposed := h.CreateGoal(sam, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(sam, tpm, proposed.ID)
	h.Clock.Advance(3 * day)

	page := getBody(t, signInClient(t, ts.URL, "tpm@example.com"), ts.URL+"/delegates")
	if strings.Contains(page, `data-testid="checkin-form"`) {
		t.Errorf("the Delegate page still embeds the Check-in form")
	}
	assertInFlatFrame(t, page, "Delegated to me")
	row := delegatedRow(t, page, checked)
	for _, want := range []string{
		"Reduce outages",
		`data-testid="delegated-goal-owner">` + shownAs("sam@example.com", "sam"),
		"3 days ago",
		fmt.Sprintf(`href="/goals/%d/checkin"`, checked.ID),
	} {
		if !strings.Contains(row, want) {
			t.Errorf("row lacks %s:\n%s", want, row)
		}
	}

	assertHealthChip(t, findByTestID(t, parsePage(t, row), "delegated-goal-health"), "Yellow")

	rest := delegatedRow(t, page, proposed)
	if strings.Contains(rest, fmt.Sprintf(`href="/goals/%d/checkin"`, proposed.ID)) {
		t.Errorf("a Proposed Goal offers Check in: %s", rest)
	}
	if !strings.Contains(rest, `data-testid="delegated-goal-no-health"`) || !strings.Contains(rest, `data-testid="delegated-goal-not-active"`) {
		t.Errorf("a Proposed Goal's row doesn't say it has no Check-ins and can't be checked in on: %s", rest)
	}
}

// delegatedRow returns the Delegate page's row for g, from its opening <li>
// to its close.
func delegatedRow(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	at := strings.Index(page, navTo(g.ID))
	if at < 0 {
		t.Fatalf("the Delegate page doesn't list %s:\n%s", g.Title, page)
	}
	start := strings.LastIndex(page[:at], `<li data-testid="delegated-goal"`)
	if start < 0 {
		t.Fatalf("%s isn't in a delegated-goal row", g.Title)
	}
	return between(t, page[start:], "", "</li>")
}
