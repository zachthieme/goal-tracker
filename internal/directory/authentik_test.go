package directory_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// fakeToken is the API token the fake Authentik accepts.
const fakeToken = "directory-token"

// authentikUser is a user as Authentik's users API lists it, as far as the
// sync reads one.
type authentikUser struct {
	Username   string         `json:"username"`
	Name       string         `json:"name"`
	Email      string         `json:"email"`
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}

// person is an internal user with a Name, and a manager attribute unless
// manager is empty.
func person(email, name, manager string) authentikUser {
	attrs := map[string]any{"email_verified": true}
	if manager != "" {
		attrs["manager"] = manager
	}
	return authentikUser{Username: email, Name: name, Email: email, Type: "internal", Attributes: attrs}
}

// fakeAuthentik serves Authentik's users API, /api/v3/core/users/, over users
// two to a page, to a request bearing fakeToken. failPage, when set, is a page
// it answers with a 500.
type fakeAuthentik struct {
	mu       sync.Mutex
	users    []authentikUser
	failPage int
}

func (f *fakeAuthentik) set(users []authentikUser, failPage int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users, f.failPage = users, failPage
}

const fakePageSize = 2

func (f *fakeAuthentik) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/api/v3/core/users/" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+fakeToken {
		http.Error(w, `{"detail":"Token invalid/expired"}`, http.StatusForbidden)
		return
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if page == f.failPage {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	pages := max(1, (len(f.users)+fakePageSize-1)/fakePageSize)
	start := min((page-1)*fakePageSize, len(f.users))
	end := min(start+fakePageSize, len(f.users))
	next := 0
	if page < pages {
		next = page + 1
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"pagination": map[string]any{
			"next": next, "previous": page - 1, "count": len(f.users),
			"current": page, "total_pages": pages, "start_index": start + 1, "end_index": end,
		},
		"results": f.users[start:end],
	})
}

// syncFixture is a Harness, a fake Authentik, and a Sync reading it, with the
// Sync's log in a buffer.
type syncFixture struct {
	h    *testsupport.Harness
	fake *fakeAuthentik
	sync *directory.Sync
	log  *bytes.Buffer
}

func newSyncFixture(t *testing.T, users ...authentikUser) *syncFixture {
	t.Helper()
	h := testsupport.New(t)
	fake := &fakeAuthentik{users: users}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	return &syncFixture{
		h:    h,
		fake: fake,
		sync: directory.NewSync(h.Service, directory.NewAuthentik(srv.URL+"/", fakeToken), logger),
		log:  &log,
	}
}

// account is the Account with emailAddr, failing the test if there's none.
func (f *syncFixture) account(t *testing.T, emailAddr string) domain.Account {
	t.Helper()
	acc, err := f.h.Service.AccountByEmail(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("AccountByEmail(%q): %v", emailAddr, err)
	}
	return acc
}

// managerOf is the email of emailAddr's Manager, or "" for none.
func (f *syncFixture) managerOf(t *testing.T, emailAddr string) string {
	t.Helper()
	acc := f.account(t, emailAddr)
	if acc.ManagerID == nil {
		return ""
	}
	mgr, err := f.h.Service.Account(t.Context(), *acc.ManagerID)
	if err != nil {
		t.Fatal(err)
	}
	return mgr.Email
}

// The sync reads every page of the directory and gives each person an Account
// with their Name and their Manager. An Account the tool already has, in
// another letter case, is the same person and gets their Manager; a Manager the
// tool has never seen gets an Account too (ADR 0008).
func TestSyncGivesEveryoneTheirNameAndManager(t *testing.T) {
	t.Parallel()

	f := newSyncFixture(t,
		person("ceo@example.com", "Dana Whitfield", ""),
		person("cto@example.com", "Priya Raman", "ceo@example.com"),
		person("Lead@Example.com", "Jonas Lindqvist", "cto@example.com"),
		person("dev@example.com", "Mei Lin", "lead@example.com"),
		person("new.hire@example.com", "Ada Okafor", "unknown.boss@example.com"),
	)
	existing := f.h.SignIn("lead@example.com")

	out := f.sync.Run(t.Context())
	if out.Err != nil {
		t.Fatalf("sync failed: %v", out.Err)
	}
	if out.People != 5 {
		t.Errorf("sync read %d people, want 5", out.People)
	}

	for email, want := range map[string]struct{ name, manager string }{
		"ceo@example.com":          {"Dana Whitfield", ""},
		"cto@example.com":          {"Priya Raman", "ceo@example.com"},
		"lead@example.com":         {"Jonas Lindqvist", "cto@example.com"},
		"dev@example.com":          {"Mei Lin", "lead@example.com"},
		"new.hire@example.com":     {"Ada Okafor", "unknown.boss@example.com"},
		"unknown.boss@example.com": {"", ""},
	} {
		acc := f.account(t, email)
		if acc.Name != want.name {
			t.Errorf("%s: Name %q, want %q", email, acc.Name, want.name)
		}
		if got := f.managerOf(t, email); got != want.manager {
			t.Errorf("%s: Manager %q, want %q", email, got, want.manager)
		}
	}
	if got := f.account(t, "lead@example.com").ID; got != existing.ID {
		t.Errorf("Lead@Example.com is Account %d, want the existing lead@example.com, %d", got, existing.ID)
	}
}

// A person the directory stops listing keeps their last-known Manager and isn't
// marked Departed (ADR 0008). A listed person whose manager attribute is gone,
// or empty, has no Manager any more.
func TestSyncKeepsTheManagerOfSomeoneNoLongerListed(t *testing.T) {
	t.Parallel()

	f := newSyncFixture(t,
		person("ceo@example.com", "Dana Whitfield", ""),
		person("cto@example.com", "Priya Raman", "ceo@example.com"),
		person("cpo@example.com", "Marcus Bell", "ceo@example.com"),
		person("lead@example.com", "Jonas Lindqvist", "cto@example.com"),
	)
	if out := f.sync.Run(t.Context()); out.Err != nil {
		t.Fatalf("first sync: %v", out.Err)
	}

	emptied := person("cpo@example.com", "Marcus Bell", "")
	emptied.Attributes["manager"] = ""
	f.fake.set([]authentikUser{
		person("ceo@example.com", "Dana Whitfield", ""),
		person("cto@example.com", "Priya Raman", ""),
		emptied,
	}, 0)
	if out := f.sync.Run(t.Context()); out.Err != nil {
		t.Fatalf("second sync: %v", out.Err)
	}

	if got := f.managerOf(t, "lead@example.com"); got != "cto@example.com" {
		t.Errorf("lead, no longer listed: Manager %q, want cto@example.com kept", got)
	}
	if f.account(t, "lead@example.com").Departed {
		t.Error("lead, no longer listed, is Departed")
	}
	for _, email := range []string{"cto@example.com", "cpo@example.com"} {
		if got := f.managerOf(t, email); got != "" {
			t.Errorf("%s, listed without a manager: Manager %q, want none", email, got)
		}
	}
}

// Service accounts and users without an email aren't people: they get no
// Account and aren't counted.
func TestSyncSkipsServiceAccountsAndUsersWithoutAnEmail(t *testing.T) {
	t.Parallel()

	service := person("outpost@example.com", "Outpost", "")
	service.Type = "service_account"
	internalService := person("ak-outpost@example.com", "Embedded outpost", "")
	internalService.Type = "internal_service_account"
	noEmail := person("", "AnonymousUser", "")
	noEmail.Username = "AnonymousUser"
	f := newSyncFixture(t,
		person("ceo@example.com", "Dana Whitfield", ""),
		service, internalService, noEmail,
		person("cto@example.com", "Priya Raman", "ceo@example.com"),
	)

	out := f.sync.Run(t.Context())
	if out.Err != nil {
		t.Fatalf("sync: %v", out.Err)
	}
	if out.People != 2 {
		t.Errorf("sync read %d people, want 2", out.People)
	}
	for _, email := range []string{"outpost@example.com", "ak-outpost@example.com"} {
		if _, err := f.h.Service.AccountByEmail(t.Context(), email); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("service account %s: AccountByEmail err %v, want ErrNotFound", email, err)
		}
	}
}

// A sync that fails part-way, here on its second page, changes nothing, logs
// the cause at ERROR, and reports the error as its outcome. The next run tries
// again.
func TestAFailedSyncLeavesManagersAsTheyWere(t *testing.T) {
	t.Parallel()

	f := newSyncFixture(t,
		person("ceo@example.com", "Dana Whitfield", ""),
		person("cto@example.com", "Priya Raman", "ceo@example.com"),
		person("lead@example.com", "Jonas Lindqvist", "cto@example.com"),
	)
	if out := f.sync.Run(t.Context()); out.Err != nil {
		t.Fatalf("first sync: %v", out.Err)
	}

	moved := []authentikUser{
		person("ceo@example.com", "Dana Whitfield", ""),
		person("cto@example.com", "Priya Raman", ""),
		person("lead@example.com", "Jonas Lindqvist", "ceo@example.com"),
		person("hire@example.com", "New Hire", "lead@example.com"),
	}
	f.fake.set(moved, 2)
	f.log.Reset()

	out := f.sync.Run(t.Context())
	if out.Err == nil {
		t.Fatal("a sync whose second page fails succeeded")
	}
	if last, ok := f.sync.Last(); !ok || last.Err == nil {
		t.Errorf("Last after a failed sync = %+v, %v; want the failure", last, ok)
	}
	if got := f.managerOf(t, "cto@example.com"); got != "ceo@example.com" {
		t.Errorf("cto: Manager %q after a failed sync, want ceo@example.com kept", got)
	}
	if got := f.managerOf(t, "lead@example.com"); got != "cto@example.com" {
		t.Errorf("lead: Manager %q after a failed sync, want cto@example.com kept", got)
	}
	if _, err := f.h.Service.AccountByEmail(t.Context(), "hire@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a failed sync created an Account for hire@example.com (err %v)", err)
	}
	if log := f.log.String(); !strings.Contains(log, "level=ERROR") || !strings.Contains(log, "500") {
		t.Errorf("a failed sync didn't log its cause at ERROR:\n%s", log)
	}

	f.fake.set(moved, 0)
	if out := f.sync.Run(t.Context()); out.Err != nil || out.People != 4 {
		t.Fatalf("the next sync: %+v, want 4 people read", out)
	}
	if got := f.managerOf(t, "lead@example.com"); got != "ceo@example.com" {
		t.Errorf("lead: Manager %q after the next sync, want ceo@example.com", got)
	}
}

// A directory refusing the token is a failed sync too.
func TestASyncWithABadTokenFails(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := httptest.NewServer(&fakeAuthentik{users: []authentikUser{person("ceo@example.com", "Dana Whitfield", "")}})
	t.Cleanup(srv.Close)
	s := directory.NewSync(h.Service, directory.NewAuthentik(srv.URL, "wrong"), slog.New(slog.DiscardHandler))

	out := s.Run(t.Context())
	if out.Err == nil || !strings.Contains(out.Err.Error(), "403") {
		t.Errorf("sync with a bad token: err %v, want a 403", out.Err)
	}
	if _, err := h.Service.AccountByEmail(t.Context(), "ceo@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a refused sync created an Account (err %v)", err)
	}
}

// Cycles and self-references are stored as the directory gives them, and each
// is logged at WARN naming its people (ADR 0008).
func TestSyncStoresCyclesAndWarnsOfThem(t *testing.T) {
	t.Parallel()

	f := newSyncFixture(t,
		person("a@example.com", "A", "b@example.com"),
		person("b@example.com", "B", "a@example.com"),
		person("self@example.com", "Self", "self@example.com"),
		person("c@example.com", "C", "a@example.com"),
	)

	if out := f.sync.Run(t.Context()); out.Err != nil {
		t.Fatalf("sync: %v", out.Err)
	}
	for email, want := range map[string]string{
		"a@example.com": "b@example.com", "b@example.com": "a@example.com",
		"self@example.com": "self@example.com", "c@example.com": "a@example.com",
	} {
		if got := f.managerOf(t, email); got != want {
			t.Errorf("%s: Manager %q, want %q", email, got, want)
		}
	}
	var warns []string
	for line := range strings.Lines(f.log.String()) {
		if strings.Contains(line, "level=WARN") {
			warns = append(warns, line)
		}
	}
	if len(warns) != 2 {
		t.Fatalf("want a WARN for each of 2 cycles, got:\n%s", f.log)
	}
	if !strings.Contains(warns[0]+warns[1], "a@example.com → b@example.com → a@example.com") &&
		!strings.Contains(warns[0]+warns[1], "b@example.com → a@example.com → b@example.com") {
		t.Errorf("no WARN names A and B's cycle:\n%s", f.log)
	}
	if !strings.Contains(warns[0]+warns[1], "self@example.com → self@example.com") {
		t.Errorf("no WARN names the self-reference:\n%s", f.log)
	}
	if strings.Contains(warns[0]+warns[1], "c@example.com") {
		t.Errorf("a WARN names c, who is below a cycle but not in it:\n%s", f.log)
	}
}
