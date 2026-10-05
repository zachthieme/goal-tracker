package web_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// The Admin page lists the open Goals whose Owner has left the org, each
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

	if list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-ownerless"); !strings.Contains(list, `data-testid="admin-ownerless-empty"`) {
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

	if list := pageElement(t, getBody(t, client, ts.URL+"/admin"), "section", "admin-departed"); !strings.Contains(list, `data-testid="admin-departed-empty"`) {
		t.Errorf("with nobody Departed, the list doesn't say so:\n%s", list)
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

	h.Clock.Advance(90 * time.Minute) // 2 Jan 2026 16:34 UTC
	status = pageElement(t, pressSyncNow(t, client, ts.URL), "section", "admin-directory-sync")
	for _, want := range []string{"2 Jan 2026 16:34", "Read 2 people"} {
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
	for _, want := range []string{"2 Jan 2026 17:34", "403 Forbidden"} {
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
