package web_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// The Admin page is the home of the Admin tools: an Admin sees cards linking to
// Dimensions and to Import goals; anyone else is refused.
func TestAdminPageIsForAdminsOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	h.SignIn("sam@example.com")

	resp, err := signInClient(t, ts.URL, "sam@example.com").Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin GET /admin: status %d, want 403; body:\n%s", resp.StatusCode, body)
	}

	page := getBody(t, signInClient(t, ts.URL, "ada@example.com"), ts.URL+"/admin")
	for _, tc := range []struct{ testID, href, label, blurb string }{
		{"admin-dimensions", `href="/dimensions"`, "Dimensions", "Define the Dimensions Goals are tagged with"},
		{"admin-imports", `href="/imports"`, "Import goals", "Load Goals from a CSV or XLSX spreadsheet"},
	} {
		card := pageElement(t, page, "a", tc.testID)
		for _, want := range []string{tc.href, `class="card`, tc.label, tc.blurb} {
			if !strings.Contains(card, want) {
				t.Errorf("Admin card %s lacks %s: %s", tc.testID, want, card)
			}
		}
	}
}

// The Admin page lists the open Goals whose Owner has left the company, each
// linking to the Goal where an Admin reassigns it. A Goal with a present Owner
// isn't listed, nor is a closed one (Cancelled) that waits on nobody.
func TestAdminPageListsOwnerlessGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignIn("kim@example.com")
	active := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	proposed := h.CreateGoal(sam, "Someday", "Maybe.")
	cancelled := h.ActiveGoal(sam, "Abandoned", "No longer matters.")
	if _, err := h.Service.SubmitCheckin(t.Context(), domain.SubmitCheckinInput{
		GoalID: cancelled.ID, AuthorID: sam.ID, Status: "Dropping this.",
		Lifecycle: domain.LifecycleCancelled, LifecycleReason: "Dropped.",
	}); err != nil {
		t.Fatalf("cancel %q: %v", cancelled.Title, err)
	}
	owned := h.ActiveGoal(kim, "Grow revenue", "It pays for everything.")
	client := signInClient(t, ts.URL, "ada@example.com")

	if list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-ownerless"); !strings.Contains(list, `data-testid="admin-ownerless-empty"`) || !strings.Contains(list, "Goals whose Owner has left the company") {
		t.Errorf("with no Ownerless Goals, the list doesn't say so:\n%s", list)
	}

	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-ownerless")
	for _, g := range []domain.Goal{active, proposed} {
		if !strings.Contains(list, navTo(g.ID)) || !strings.Contains(list, g.Title) {
			t.Errorf("Ownerless list lacks %q:\n%s", g.Title, list)
		}
	}
	for _, g := range []domain.Goal{cancelled, owned} {
		if strings.Contains(list, navTo(g.ID)) {
			t.Errorf("Ownerless list shows %q:\n%s", g.Title, list)
		}
	}
	if strings.Contains(list, "admin-ownerless-empty") {
		t.Errorf("Ownerless list shows its empty state beside Goals:\n%s", list)
	}
}

// The top bar ends with Admin for an Admin, marked current on the Admin page,
// and has no Admin item for anyone else. Dimensions has left the top bar for
// the Admin page, but /dimensions still opens for everyone.
func TestAdminNavItemIsForAdminsOnly(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	h.SignIn("sam@example.com")
	ada := signInClient(t, ts.URL, "ada@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, ada, ts.URL+"/admin")
	nav := page[strings.Index(page, `<nav class="nav">`):strings.Index(page, `<div class="me">`)]
	item := pageElement(t, nav, "a", "nav-admin")
	if !strings.Contains(item, `href="/admin"`) || !strings.Contains(item, `aria-current="page"`) {
		t.Errorf("on /admin, the Admin item doesn't link there marked current: %s", item)
	}
	if last := nav[strings.LastIndex(nav, "<a "):]; !strings.HasPrefix(last, `<a data-testid="nav-admin"`) {
		t.Errorf("Admin isn't the last nav item:\n%s", nav)
	}
	if home := pageElement(t, getBody(t, ada, ts.URL+"/home"), "a", "nav-admin"); strings.Contains(home, "aria-current") {
		t.Errorf("on /home, the Admin item is marked current: %s", home)
	}

	for who, client := range map[string]*http.Client{"Admin": ada, "non-Admin": sam} {
		page := getBody(t, client, ts.URL+"/dimensions")
		if strings.Contains(page, `data-testid="nav-dimensions"`) || strings.Contains(page, `class="navitem" href="/dimensions"`) {
			t.Errorf("the %s's top bar still has Dimensions", who)
		}
		if strings.Contains(page, `data-testid="nav-admin"`) != (who == "Admin") {
			t.Errorf("the %s's top bar has the Admin item: %v", who, who != "Admin")
		}
	}
}

// The Admin page lists every Departed person by Label, email a click away, each
// with a Mark returned… button that asks for confirmation — including a
// Delegate who owns no Goals and an Owner whose Goals were all reassigned, who
// have no Goal page to be returned from. A present person isn't listed, and
// with nobody Departed the list says so. Only an Admin sees it.
func TestAdminPageListsDepartedPeople(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	kim := h.SignInNamed("kim@example.com", "Kim Abara")
	pat := h.SignIn("pat@example.com")
	delegated := h.ActiveGoal(pat, "Grow revenue", "Revenue funds the rest.")
	h.AddDelegate(pat, kim, delegated.ID)
	moved := h.ActiveGoal(sam, "Migrate displays", "Displays fail often.")
	client := signInClient(t, ts.URL, "ada@example.com")

	if list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-departed"); !strings.Contains(list, `data-testid="admin-departed-empty"`) || !strings.Contains(list, "Nobody has left the company.") || !strings.Contains(list, "People who have left the company.") {
		t.Errorf("with nobody Departed, the list doesn't say nobody has left the company:\n%s", list)
	}

	for _, acc := range []domain.Account{sam, kim} {
		if err := h.Service.MarkDeparted(t.Context(), ada.ID, acc.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", acc.Email, err)
		}
	}
	if _, err := h.Service.ReassignGoal(t.Context(), ada.ID, moved.ID, pat.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}

	list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-departed")
	if strings.Contains(list, "admin-departed-empty") {
		t.Errorf("Departed list shows its empty state beside people:\n%s", list)
	}
	kimAt, samAt := strings.Index(list, shownAs("kim@example.com", "Kim Abara")), strings.Index(list, shownAs("sam@example.com", "sam"))
	if kimAt < 0 || samAt < 0 || kimAt > samAt {
		t.Errorf("Departed list doesn't show Kim Abara then sam by Label with email a click away:\n%s", list)
	}
	if strings.Contains(list, "pat@example.com") || strings.Contains(list, "ada@example.com") {
		t.Errorf("Departed list shows a present person:\n%s", list)
	}
	for _, acc := range []domain.Account{sam, kim} {
		row := openTag(between(t, list, fmt.Sprintf(`action="/accounts/%d/return"`, acc.ID), ""))
		if !strings.Contains(row, `onsubmit="return confirm(`) {
			t.Errorf("Mark returned for %s doesn't ask for confirmation: %s", acc.Email, row)
		}
	}
	if !strings.Contains(list, "Mark returned…") {
		t.Errorf("Departed list lacks Mark returned…:\n%s", list)
	}
	if strings.Contains(list, "this Owner") {
		t.Errorf("Departed list's confirmation speaks of an Owner, but not everyone listed owns a Goal:\n%s", list)
	}

	resp, err := signInClient(t, ts.URL, "pat@example.com").Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden || strings.Contains(body, "admin-departed") {
		t.Errorf("non-Admin GET /admin: status %d, want 403 without the list", resp.StatusCode)
	}
}

// Marking someone returned from the Admin page brings the Admin back to it,
// with that person no longer listed, and returns them: they can sign in again.
func TestAdminMarksDepartedPersonReturnedFromAdminPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	kim := h.SignIn("kim@example.com")
	sam := h.SignIn("sam@example.com")
	for _, acc := range []domain.Account{kim, sam} {
		if err := h.Service.MarkDeparted(t.Context(), ada.ID, acc.ID); err != nil {
			t.Fatalf("MarkDeparted(%s): %v", acc.Email, err)
		}
	}
	client := signInClient(t, ts.URL, "ada@example.com")

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/accounts/%d/return", ts.URL, kim.ID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Referer", ts.URL+"/admin")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST return: %v", err)
	}
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/admin" {
		t.Fatalf("after Mark returned: status %d at %s, want 200 at /admin", resp.StatusCode, resp.Request.URL.Path)
	}

	list := pageElement(t, page, "section", "admin-departed")
	if strings.Contains(list, "kim@example.com") {
		t.Errorf("Kim is still listed as Departed after returning:\n%s", list)
	}
	if !strings.Contains(list, shownAs("sam@example.com", "sam")) {
		t.Errorf("Sam, still Departed, left the list:\n%s", list)
	}
	if acc, err := h.Service.SignIn(t.Context(), "kim@example.com"); err != nil || acc.Departed {
		t.Errorf("SignIn after return = %+v, %v; want a present Account", acc, err)
	}
}

// adminDirectoryFake is a directory the Admin page tests sync from: people, or
// err when set.
type adminDirectoryFake struct {
	mu     sync.Mutex
	people []domain.DirectoryPerson
	err    error
}

func (f *adminDirectoryFake) People(context.Context) ([]domain.DirectoryPerson, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.people, f.err
}

func (f *adminDirectoryFake) set(people []domain.DirectoryPerson, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.people, f.err = people, err
}

// newDirectorySyncServer serves the app with the directory sync configured,
// reading dir.
func newDirectorySyncServer(t *testing.T, h *testsupport.Harness, dir domain.Directory) *httptest.Server {
	t.Helper()
	sync := directory.NewSync(h.Service, dir, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithDirectorySync(sync)))
	t.Cleanup(ts.Close)
	return ts
}

// pressSyncNow presses Sync now on the Admin page as client, and returns the
// page it lands on, failing the test unless that's the Admin page.
func pressSyncNow(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	section := pageElement(t, getBody(t, client, base+"/admin"), "section", "admin-directory-sync")
	form := tagAround(t, section, `action="/admin/directory-sync"`)
	if !strings.Contains(form, `method="post"`) || !strings.Contains(section, "Sync now") {
		t.Fatalf("the sync has no Sync now button posting to /admin/directory-sync:\n%s", section)
	}
	resp := postForm(t, client, base+"/admin/directory-sync", url.Values{})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/admin" {
		t.Fatalf("after Sync now: status %d at %s, want 200 at /admin:\n%s", resp.StatusCode, resp.Request.URL.Path, body)
	}
	return body
}

// With the directory sync configured, an Admin sees when it last ran and how
// it went, how many people it read or its error, and can run it at once with
// Sync now (ADR 0008).
func TestAdminSeesTheDirectorySyncAndCanSyncNow(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	dir := &adminDirectoryFake{people: []domain.DirectoryPerson{
		{Email: "ceo@example.com", Name: "Dana Whitfield"},
		{Email: "ada@example.com", Name: "Ada Okafor", ManagerEmail: "ceo@example.com"},
	}}
	ts := newDirectorySyncServer(t, h, dir)
	h.SignIn("ada@example.com")
	client := signInClient(t, ts.URL, "ada@example.com")

	status := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-directory-sync")
	if !strings.Contains(status, "Not synced yet") {
		t.Errorf("before any sync, the status doesn't say so:\n%s", status)
	}

	h.Clock.Advance(90 * time.Minute) // Jan 2, 2026, 16:34 UTC
	status = pageElement(t, pressSyncNow(t, client, ts.URL), "section", "admin-directory-sync")
	for _, want := range []string{"Jan 2, 16:34", "Read 2 people"} {
		if !strings.Contains(status, want) {
			t.Errorf("after Sync now, the status lacks %q:\n%s", want, status)
		}
	}
	if ceo, err := h.Service.AccountByEmail(t.Context(), "ceo@example.com"); err != nil || ceo.Name != "Dana Whitfield" {
		t.Errorf("Sync now didn't sync: ceo is %+v, %v", ceo, err)
	}

	dir.set(nil, errors.New("authentik users page 1: 403 Forbidden"))
	h.Clock.Advance(time.Hour)
	status = pageElement(t, pressSyncNow(t, client, ts.URL), "section", "admin-directory-sync")
	for _, want := range []string{"Jan 2, 17:34", "403 Forbidden"} {
		if !strings.Contains(status, want) {
			t.Errorf("after a failed Sync now, the status lacks %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "Read 2 people") {
		t.Errorf("after a failed Sync now, the status still shows the last success:\n%s", status)
	}
}

// Only an Admin can see the sync or run it: anyone else is refused both.
func TestNonAdminCantSeeOrRunTheDirectorySync(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	dir := &adminDirectoryFake{people: []domain.DirectoryPerson{{Email: "ceo@example.com", Name: "Dana Whitfield"}}}
	ts := newDirectorySyncServer(t, h, dir)
	h.SignIn("ada@example.com")
	h.SignIn("sam@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")

	for _, req := range []struct{ method, path string }{{http.MethodGet, "/admin"}, {http.MethodPost, "/admin/directory-sync"}} {
		var resp *http.Response
		if req.method == http.MethodGet {
			var err error
			if resp, err = sam.Get(ts.URL + req.path); err != nil {
				t.Fatal(err)
			}
		} else {
			resp = postForm(t, sam, ts.URL+req.path, url.Values{})
		}
		if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden || strings.Contains(body, "admin-directory-sync") {
			t.Errorf("non-Admin %s %s: status %d, want 403 without the sync", req.method, req.path, resp.StatusCode)
		}
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "ceo@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a non-Admin's Sync now ran the sync (err %v)", err)
	}
	home := getBody(t, sam, ts.URL+"/home")
	for _, absent := range []string{"Sync now", "admin-directory-sync"} {
		if strings.Contains(home, absent) {
			t.Errorf("a non-Admin's Home shows %q", absent)
		}
	}
}

// With the sync unconfigured, the Admin page shows neither its status nor Sync
// now, and there's nothing to post to.
func TestAdminPageHasNoDirectorySyncWhenUnconfigured(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	client := signInClient(t, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/admin")
	for _, absent := range []string{"admin-directory-sync", "Sync now", "/admin/directory-sync"} {
		if strings.Contains(page, absent) {
			t.Errorf("the unconfigured Admin page shows %q", absent)
		}
	}
	resp := postForm(t, noRedirects(client), ts.URL+"/admin/directory-sync", url.Values{})
	if body := readBody(t, resp); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /admin/directory-sync unconfigured: status %d, want 404:\n%s", resp.StatusCode, body)
	}
}

// The directory sync's time reads on the org's clock: 16:34 UTC on 2 Jan is
// 01:34 on 3 Jan in Tokyo (ticket #276).
func TestAdminSeesTheDirectorySyncTimeInTheOrgsTimezone(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	h.Service = domain.NewService(h.DB, h.Clock, h.Email, []string{"ada@example.com"}, domain.WithTimezone(tokyo))
	dir := &adminDirectoryFake{people: []domain.DirectoryPerson{{Email: "ceo@example.com", Name: "Dana Whitfield"}}}
	ts := newDirectorySyncServer(t, h, dir)
	h.SignIn("ada@example.com")
	client := signInClient(t, ts.URL, "ada@example.com")

	h.Clock.Advance(90 * time.Minute) // Jan 2, 2026, 16:34 UTC
	status := pageElement(t, pressSyncNow(t, client, ts.URL), "section", "admin-directory-sync")
	if !strings.Contains(status, "Jan 3, 01:34") {
		t.Errorf("the sync's time isn't Jan 3, 01:34 in Tokyo:\n%s", status)
	}
}

// issuePasswordLink presses Issue link on the Admin page for emailAddr as
// client, and returns the status and the page it answers with.
func issuePasswordLink(t *testing.T, client *http.Client, base, emailAddr string) (int, string) {
	t.Helper()
	section := pageElement(t, getBody(t, client, base+"/admin"), "section", "admin-password-links")
	form := tagAround(t, section, `action="/admin/password-links"`)
	if !strings.Contains(form, `method="post"`) || !strings.Contains(section, `type="email" name="email"`) || !strings.Contains(section, "Issue link") {
		t.Fatalf("the card has no email field and Issue link button posting to /admin/password-links:\n%s", section)
	}
	resp := postForm(t, noRedirects(client), base+"/admin/password-links", url.Values{"email": {emailAddr}})
	return resp.StatusCode, readBody(t, resp)
}

// With password sign-in, an Admin issues a set-password link from the Admin
// page and sees the whole link once, with when it expires; it creates the
// Account for an email that has none, and the link works.
func TestAdminIssuesASetPasswordLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	h.Service = domain.NewService(h.DB, h.Clock, h.Email, []string{"ada@example.com"}, domain.WithBaseURL("https://goals.example.com/"))
	ts := newPasswordServer(t, h)
	client := passwordSignInClient(t, h, ts.URL, "ada@example.com")

	status, page := issuePasswordLink(t, client, ts.URL, "new.hire@example.com")
	if status != http.StatusOK {
		t.Fatalf("Issue link: status %d, want 200:\n%s", status, page)
	}
	result := pageElement(t, page, "div", "admin-password-link")
	link := regexp.MustCompile(`https://goals\.example\.com/set-password\?token=[A-Za-z0-9_-]{43}`).FindString(result)
	if link == "" {
		t.Fatalf("the result shows no set-password link:\n%s", result)
	}
	for _, want := range []string{"new.hire@example.com", "Jan 5, 15:04", "Send this to the person. It works once and expires in 72 hours."} {
		if !strings.Contains(result, want) {
			t.Errorf("the result lacks %q:\n%s", want, result)
		}
	}

	token := strings.TrimPrefix(link, "https://goals.example.com/set-password?token=")
	acc, err := h.Service.PasswordLinkAccount(t.Context(), token)
	if err != nil || acc.Email != "new.hire@example.com" {
		t.Errorf("the shown link is for %+v (err %v), want new.hire@example.com", acc, err)
	}
	if strings.Contains(getBody(t, client, ts.URL+"/admin"), token) {
		t.Error("the Admin page shows the link again after it was issued")
	}
}

// The Issue link button is a full-size button, so it lines up with the 40px
// email field beside it.
func TestIssueLinkButtonLinesUpWithItsEmailField(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newPasswordServer(t, h)
	client := passwordSignInClient(t, h, ts.URL, "ada@example.com")

	section := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-password-links")
	if class := attr(tagAround(t, section, ">Issue link</button>"), "class"); class != "btn" {
		t.Errorf("the Issue link button's class is %q, want \"btn\"", class)
	}
}

// A Departed person can't be issued a set-password link: the Admin page says
// so, and shows no link.
func TestAdminCantIssueASetPasswordLinkToADepartedPerson(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newPasswordServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatal(err)
	}
	client := passwordSignInClient(t, h, ts.URL, "ada@example.com")

	status, page := issuePasswordLink(t, client, ts.URL, "sam@example.com")
	if status != http.StatusUnprocessableEntity {
		t.Errorf("Issue link for a Departed person: status %d, want 422", status)
	}
	section := pageElement(t, page, "section", "admin-password-links")
	if msg := pageElement(t, section, "p", "admin-password-link-error"); !strings.Contains(msg, "sam@example.com has left the company") {
		t.Errorf("the refusal says %s", msg)
	}
	if strings.Contains(page, "/set-password?token=") {
		t.Errorf("the page shows a link for a Departed person:\n%s", section)
	}
}

// A set-password link is only issued for an email address: anything else is
// refused under the field, which keeps what was typed, and no Account is made.
func TestAdminCantIssueASetPasswordLinkToSomethingNotAnEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newPasswordServer(t, h)
	client := passwordSignInClient(t, h, ts.URL, "ada@example.com")

	status, page := issuePasswordLink(t, client, ts.URL, "not-an-email")
	if status != http.StatusUnprocessableEntity {
		t.Errorf("Issue link for not-an-email: status %d, want 422", status)
	}
	section := pageElement(t, page, "section", "admin-password-links")
	if msg := elementTexts(section, "p", "admin-password-link-error"); len(msg) != 1 || msg[0] != "That isn't an email address." {
		t.Errorf("the refusal says %q, want \"That isn't an email address.\"", msg)
	}
	if value := attr(tagAround(t, section, `name="email"`), "value"); value != "not-an-email" {
		t.Errorf("the email field holds %q, want what was typed, \"not-an-email\"", value)
	}
	if strings.Contains(page, "/set-password?token=") {
		t.Errorf("the page shows a link for not-an-email:\n%s", section)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "not-an-email"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Issue link for not-an-email created an Account (err %v)", err)
	}
}

// Only an Admin may issue a set-password link: anyone else is refused, as with
// Sync now, and no link is made.
func TestNonAdminCantIssueASetPasswordLink(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newPasswordServer(t, h)
	sam := passwordSignInClient(t, h, ts.URL, "sam@example.com")

	resp := postForm(t, noRedirects(sam), ts.URL+"/admin/password-links", url.Values{"email": {"kim@example.com"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusForbidden || strings.Contains(body, "/set-password?token=") {
		t.Errorf("non-Admin POST /admin/password-links: status %d, want 403 with no link:\n%s", resp.StatusCode, body)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "kim@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a non-Admin's Issue link created an Account (err %v)", err)
	}
}

// Without password sign-in, the Admin page has no set-password links card and
// there's nothing to post to.
func TestAdminPageHasNoSetPasswordLinksWithoutPasswordSignIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	client := signInClient(t, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/admin")
	for _, absent := range []string{"admin-password-links", "Set-password links", "/admin/password-links"} {
		if strings.Contains(page, absent) {
			t.Errorf("the Admin page without password sign-in shows %q", absent)
		}
	}
	resp := postForm(t, noRedirects(client), ts.URL+"/admin/password-links", url.Values{"email": {"kim@example.com"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /admin/password-links without password sign-in: status %d, want 404:\n%s", resp.StatusCode, body)
	}
}

// adminRemoveAction is the action of the form that takes the Admin role from
// acc on the Admin page.
func adminRemoveAction(acc domain.Account) string {
	return fmt.Sprintf(`action="/admin/admins/%d/remove"`, acc.ID)
}

// The Admin page lists every Admin by Label, email a click away, before the
// Ownerless Goals. Each other Admin has a Remove
// Admin… button that asks for confirmation; the viewer's own row has none, as
// no Admin can take the role from themselves. A non-Admin isn't listed.
func TestAdminPageListsTheAdmins(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com", "kim@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	kim := h.SignInNamed("kim@example.com", "Kim Abara")
	h.SignIn("sam@example.com")
	client := signInClient(t, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/admin")
	if admins, ownerless := strings.Index(page, `data-testid="admin-admins"`), strings.Index(page, `data-testid="admin-ownerless"`); admins < 0 || admins > ownerless {
		t.Errorf("the Admins section doesn't come before the Ownerless Goals")
	}
	section := pageElement(t, page, "section", "admin-admins")
	for _, want := range []string{"<h2>Admins</h2>", "Admins define Dimensions and Fields, reassign Ownerless Goals and use this page."} {
		if !strings.Contains(section, want) {
			t.Errorf("the Admins section lacks %q:\n%s", want, section)
		}
	}
	rows := elementTexts(section, "tr", "admin-admin-person")
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "ada") || !strings.HasPrefix(rows[1], "Kim Abara") {
		t.Errorf("the Admins listed are %q, want ada then Kim Abara", rows)
	}
	for _, want := range []string{shownAs("ada@example.com", "ada"), shownAs("kim@example.com", "Kim Abara")} {
		if !strings.Contains(section, want) {
			t.Errorf("the Admins section doesn't show %s:\n%s", want, section)
		}
	}
	if strings.Contains(section, "sam@example.com") {
		t.Errorf("the Admins section lists a non-Admin:\n%s", section)
	}

	if strings.Contains(section, adminRemoveAction(ada)) {
		t.Errorf("the viewer's own row has a Remove Admin form:\n%s", section)
	}
	if !strings.Contains(rows[0], "you") {
		t.Errorf("the viewer's own row doesn't say it's them: %q", rows[0])
	}
	form := tagAround(t, section, adminRemoveAction(kim))
	if !strings.Contains(form, `method="post"`) || !strings.Contains(form, `onsubmit="return confirm(`) {
		t.Errorf("Remove Admin for Kim isn't a confirmed post: %s", form)
	}
	if class := attr(tagAround(t, section, ">Remove Admin…</button>"), "class"); class != "btn sm" {
		t.Errorf("the Remove Admin… button's class is %q, want \"btn sm\"", class)
	}

	add := tagAround(t, section, `action="/admin/admins"`)
	if !strings.Contains(add, `method="post"`) || !strings.Contains(add, "ad-link-form") || !strings.Contains(section, `type="email" name="email"`) {
		t.Errorf("the Admins section has no email form posting to /admin/admins:\n%s", section)
	}
	if class := attr(tagAround(t, section, ">Make Admin</button>"), "class"); class != "btn" {
		t.Errorf("the Make Admin button's class is %q, want \"btn\"", class)
	}
}

// With password sign-in, the Admins section comes after the set-password links.
func TestAdminsSectionFollowsTheSetPasswordLinks(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newPasswordServer(t, h)
	client := passwordSignInClient(t, h, ts.URL, "ada@example.com")

	page := getBody(t, client, ts.URL+"/admin")
	if links, admins := strings.Index(page, `data-testid="admin-password-links"`), strings.Index(page, `data-testid="admin-admins"`); links < 0 || admins < links {
		t.Errorf("the Admins section doesn't follow the set-password links")
	}
}

// makeAdmin presses Make Admin on the Admin page for emailAddr as client, and
// returns the response, unfollowed.
func makeAdmin(t *testing.T, client *http.Client, base, emailAddr string) *http.Response {
	t.Helper()
	return postForm(t, noRedirects(client), base+"/admin/admins", url.Values{"email": {emailAddr}})
}

// adminPageStatus is the status client gets opening the Admin page.
func adminPageStatus(t *testing.T, client *http.Client, base string) int {
	t.Helper()
	resp, err := client.Get(base + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	_ = readBody(t, resp)
	return resp.StatusCode
}

// An Admin makes someone an Admin by email from the Admin page, and is brought
// back to the Admins section; that person can open the Admin page on their
// next page load.
func TestAdminMakesSomeoneAnAdminFromTheAdminPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	h.SignIn("ada@example.com")
	h.SignIn("sam@example.com")
	ada := signInClient(t, ts.URL, "ada@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	if status := adminPageStatus(t, sam, ts.URL); status != http.StatusForbidden {
		t.Fatalf("Sam, not yet an Admin, GET /admin: status %d, want 403", status)
	}

	resp := makeAdmin(t, ada, ts.URL, " Sam@Example.com ")
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin#admin-admins" {
		t.Errorf("Make Admin: status %d to %q, want 303 to /admin#admin-admins", resp.StatusCode, resp.Header.Get("Location"))
	}
	if status := adminPageStatus(t, sam, ts.URL); status != http.StatusOK {
		t.Errorf("Sam, made an Admin, GET /admin: status %d, want 200", status)
	}
	if section := pageElement(t, getBody(t, ada, ts.URL+"/admin"), "section", "admin-admins"); !strings.Contains(section, shownAs("sam@example.com", "sam")) {
		t.Errorf("the Admins section doesn't list Sam:\n%s", section)
	}
}

// Make Admin never creates an Account: an email no one has is refused in the
// Admins section, which keeps what was typed. A Departed person, or a blank
// email, is refused there too, and nobody is made an Admin.
func TestAdminCantMakeAnAdminOfSomeoneWithoutAnAccount(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	client := signInClient(t, ts.URL, "ada@example.com")

	for _, tc := range []struct{ typed, kept, want string }{
		{" Nobody@Example.com ", "Nobody@Example.com", "No one with that email has an account yet."},
		{" Sam@Example.com", "Sam@Example.com", "Sam@Example.com has left the company. Mark them returned first."},
		{"  ", "", "Enter the person's email."},
	} {
		resp := makeAdmin(t, client, ts.URL, tc.typed)
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("Make Admin %q: status %d, want 422", tc.typed, resp.StatusCode)
			continue
		}
		section := pageElement(t, page, "section", "admin-admins")
		if msg := elementTexts(section, "p", "admin-admins-error"); len(msg) != 1 || msg[0] != tc.want {
			t.Errorf("Make Admin %q: the refusal says %q, want %q", tc.typed, msg, tc.want)
		}
		if value := attr(tagAround(t, section, `name="email"`), "value"); value != tc.kept {
			t.Errorf("Make Admin %q: the email field holds %q, want %q", tc.typed, value, tc.kept)
		}
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "nobody@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Make Admin for an email no one has created an Account (err %v)", err)
	}
	if acc, err := h.Service.Account(t.Context(), sam.ID); err != nil || acc.IsAdmin {
		t.Errorf("Sam after the refused Make Admin = %+v, %v; want a non-Admin", acc, err)
	}
}
