package web_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A refused action is answered by one page inside the app, whatever the area:
// headed by what was refused, saying why in one sentence, and linking Back to
// where it came from. Each kind of refusal has its status and its sentence; a
// thing that isn't there is the Not found page, unless the area says what
// isn't there; and a failure is a plain 500, unless the area says what failed.
// Either 500 is logged.
func TestARefusalIsOnePageSayingWhyWithItsStatus(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	var buf bytes.Buffer
	srv := web.NewServer(h.Service, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h.SignIn("sam@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	broken := errors.New("database is closed")
	for i, tc := range []struct {
		name   string
		err    error
		opts   []web.RefusalOption
		status int
		// reason is the refusal page's sentence; empty when the answer isn't
		// the refusal page.
		reason   string
		notFound bool
		logged   bool
	}{
		{name: "a cycle", err: domain.Refuse(domain.ErrCycle, "linking these Goals would make a cycle: “Cut costs” already contributes to “Renegotiate contracts”"), status: http.StatusConflict,
			reason: "Linking these Goals would make a cycle: “Cut costs” already contributes to “Renegotiate contracts”."},
		{name: "a validation", err: domain.Refuse(domain.ErrValidation, "this link request has already been accepted"), status: http.StatusUnprocessableEntity,
			reason: "This link request has already been accepted."},
		{name: "someone else's action", err: domain.Refuse(domain.ErrNotAuthorized, "only the parent's Owner may accept a link"), status: http.StatusForbidden,
			reason: "Only the parent's Owner may accept a link."},
		{name: "a thing that isn't there", err: domain.ErrNotFound, status: http.StatusNotFound, notFound: true},
		{name: "a thing that isn't there, said", err: fmt.Errorf("undo 7: %w", domain.ErrNotFound), opts: []web.RefusalOption{web.RefuseNotFoundSaying("There's nothing here to undo.")},
			status: http.StatusNotFound, reason: "There's nothing here to undo."},
		{name: "a failure", err: broken, status: http.StatusInternalServerError, logged: true},
		{name: "a failure, said", err: broken, opts: []web.RefusalOption{web.RefuseFailureSaying("The Undo couldn't be done, so nothing changed. Try again.")},
			status: http.StatusInternalServerError, reason: "The Undo couldn't be done, so nothing changed. Try again.", logged: true},
	} {
		path := fmt.Sprintf("/refusal-test/%d", i)
		srv.HandleRefusalForTest("GET "+path, "Can't do it", "/goals/12", tc.err, tc.opts...)
		buf.Reset()
		w := serveSignedIn(t, srv, sam, base, path)

		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, w.Code, tc.status)
		}
		logged := strings.Contains(buf.String(), `"msg":"server error"`) && strings.Contains(buf.String(), broken.Error())
		if logged != tc.logged {
			t.Errorf("%s: logged the failure %t, want %t:\n%s", tc.name, logged, tc.logged, &buf)
		}
		switch {
		case tc.notFound:
			if !hasTestID(parsePage(t, w.Body.String()), "not-found") {
				t.Errorf("%s: not the Not found page:\n%s", tc.name, w.Body)
			}
		case tc.reason == "":
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("%s: Content-Type %q, want a plain-text 500", tc.name, ct)
			}
		default:
			assertRefusalPage(t, parsePage(t, w.Body.String()), "Can't do it", tc.reason, "/goals/12")
		}
	}
}

// A refusal links Back only to a page of the app's own: any other back, one
// that would leave the app included, falls back to Home.
func TestARefusalLinksBackOnlyWithinTheApp(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h.SignIn("sam@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	for i, c := range []struct{ back, want string }{
		{"/follow-ups?scope=org", "/follow-ups?scope=org"},
		{"//evil.example", "/home"},
		{"http://x", "/home"},
		{"", "/home"},
		{"/goals\\12", "/home"},
	} {
		path := fmt.Sprintf("/refusal-back-test/%d", i)
		srv.HandleRefusalForTest("GET "+path, "Can't do it", c.back, domain.Refuse(domain.ErrValidation, "no"))
		w := serveSignedIn(t, srv, sam, base, path)
		page := parsePage(t, w.Body.String())
		if back := linkTo(findByTestID(t, page, "refusal"), c.want); back == nil || textOf(back) != "Back" {
			t.Errorf("back %q: no Back link to %s:\n%s", c.back, c.want, renderNode(page))
		}
	}
}

// A refusal's reason that opens with a value, such as an email or a Goal's
// title, reads as written; one that opens with a plain lowercase word is
// capitalised. Either way it ends in a full stop.
func TestARefusalReasonOpeningWithAValueReadsAsWritten(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	srv := web.NewServer(h.Service)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h.SignIn("sam@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	base, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	for i, c := range []struct {
		name string
		err  error
		want string
	}{
		{"an email", domain.Refuse(domain.ErrValidation, "%s already owns this Action Item", "owner@example.com"),
			"owner@example.com already owns this Action Item."},
		{"a Goal's title", domain.Refuse(domain.ErrValidation, "%s is Done, so it can't be activated", "iOS launch"),
			"iOS launch is Done, so it can't be activated."},
		{"a plain word", domain.Refuse(domain.ErrValidation, "a Milestone needs a name"),
			"A Milestone needs a name."},
	} {
		path := fmt.Sprintf("/refusal-reason-test/%d", i)
		srv.HandleRefusalForTest("GET "+path, "Can't do it", "/goals/12", c.err)
		w := serveSignedIn(t, srv, sam, base, path)
		page := parsePage(t, w.Body.String())
		if got := textOf(findByTestID(t, page, "refused-reason")); got != c.want {
			t.Errorf("%s: refused-reason = %q, want %q", c.name, got, c.want)
		}
	}
}

// serveSignedIn has srv answer a GET of path as client's signed-in Account
// would send it, and returns the answer once srv has finished with it.
func serveSignedIn(t *testing.T, srv *web.Server, client *http.Client, base *url.URL, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	for _, c := range client.Jar.Cookies(base) {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}
