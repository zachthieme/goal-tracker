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

	"golang.org/x/net/html"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// A refused command is answered once for every area (answerRefused). A
// refusal of what was typed, or a cycle, comes back in place: an htmx post
// with a fragment gets it at 200; otherwise a form on another page comes back
// open there, through a redirect carrying it once, and a form that is the
// page is the page again, at 422, or 409 for a cycle. With neither, or when
// the form can't come back — its page no longer offers it, what it carries is
// too long, or a problem names no input of the form — it is the refusal page
// at that status. Someone else's action is the refusal page at 403, a thing
// that isn't there the Not found page, and anything else a logged 500,
// whatever the form.
func TestARefusedCommandIsAnsweredInPlaceOrOnTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	var buf bytes.Buffer
	srv := web.NewServer(h.Service, web.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	srv.HandleFormRefusalPageForTest()
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h.SignIn("sam@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")

	var (
		typed      = &domain.InputError{Input: "name", Message: "a thing needs a name"}
		stray      = &domain.InputError{Input: "colour", Message: "that isn't a colour"}
		whole      = domain.Refuse(domain.ErrValidation, "this thing is already done")
		cycle      = domain.Refuse(domain.ErrCycle, "linking these Goals would make a cycle")
		notTheirs  = domain.Refuse(domain.ErrNotAuthorized, "only the Owner may do this")
		notThere   = fmt.Errorf("thing 7: %w", domain.ErrNotFound)
		broken     = errors.New("database is closed")
		fields     = []string{"name", "note"}
		posted     = url.Values{"name": {"Typed name"}, "note": {"A note"}, "other": {"not the form's"}}
		tooLong    = url.Values{"name": {strings.Repeat("x", 4000)}}
		onPage     = web.FormRefusalCaseForTest{Form: "on-page", Fields: fields, Offered: true}
		notOffered = web.FormRefusalCaseForTest{Form: "on-page", Fields: fields}
		asPage     = web.FormRefusalCaseForTest{Form: "as-page"}
		noForm     = web.FormRefusalCaseForTest{}
	)
	with := func(c web.FormRefusalCaseForTest, err error, partial bool) web.FormRefusalCaseForTest {
		c.Err, c.Partial = err, partial
		return c
	}
	oneSentence := with(onPage, whole, false)
	oneSentence.OneSentence = true

	// Each answer the module gives: back on the form's page (redirect), the
	// form as the page (asPage), the fragment (partial), the refusal page,
	// the Not found page, or a 500.
	const (
		redirect = "redirect"
		asWhole  = "as-page"
		partial  = "partial"
		refusal  = "refusal"
		notFound = "not-found"
		failure  = "failure"
	)
	for i, tc := range []struct {
		name   string
		c      web.FormRefusalCaseForTest
		htmx   bool
		form   url.Values
		answer string
		status int
		// reason is what the answer says; problem, for a form back on its
		// page, the reason beside its name input.
		reason, problem string
	}{
		{name: "a typed value, on a page", c: with(onPage, typed, false), answer: redirect, status: http.StatusSeeOther, problem: "A thing needs a name."},
		{name: "a typed value, on a page, htmx", c: with(onPage, typed, false), htmx: true, answer: redirect, status: http.StatusSeeOther, problem: "A thing needs a name."},
		{name: "a typed value, on a page with a fragment", c: with(onPage, typed, true), answer: redirect, status: http.StatusSeeOther, problem: "A thing needs a name."},
		{name: "a typed value, on a page with a fragment, htmx", c: with(onPage, typed, true), htmx: true, answer: partial, status: http.StatusOK, reason: "A thing needs a name."},
		{name: "a typed value, as the page", c: with(asPage, typed, false), answer: asWhole, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, as the page, htmx", c: with(asPage, typed, false), htmx: true, answer: asWhole, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, as the page with a fragment", c: with(asPage, typed, true), answer: asWhole, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, as the page with a fragment, htmx", c: with(asPage, typed, true), htmx: true, answer: partial, status: http.StatusOK, reason: "A thing needs a name."},
		{name: "a typed value, no form", c: with(noForm, typed, false), answer: refusal, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, no form, htmx", c: with(noForm, typed, false), htmx: true, answer: refusal, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, a fragment only", c: with(noForm, typed, true), answer: refusal, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a typed value, a fragment only, htmx", c: with(noForm, typed, true), htmx: true, answer: partial, status: http.StatusOK, reason: "A thing needs a name."},

		{name: "a cycle, said in one sentence on a page", c: func() web.FormRefusalCaseForTest { c := with(onPage, cycle, true); c.OneSentence = true; return c }(), answer: redirect, status: http.StatusSeeOther, reason: "Linking these Goals would make a cycle."},
		{name: "a cycle, as the page", c: with(asPage, cycle, false), answer: asWhole, status: http.StatusConflict, reason: "Linking these Goals would make a cycle."},
		{name: "a cycle, as the page with a fragment, htmx", c: with(asPage, cycle, true), htmx: true, answer: partial, status: http.StatusOK, reason: "Linking these Goals would make a cycle."},
		{name: "a cycle, no form", c: with(noForm, cycle, false), answer: refusal, status: http.StatusConflict, reason: "Linking these Goals would make a cycle."},
		{name: "a cycle, no form, htmx", c: with(noForm, cycle, false), htmx: true, answer: refusal, status: http.StatusConflict, reason: "Linking these Goals would make a cycle."},

		{name: "someone else's, on a page with a fragment", c: with(onPage, notTheirs, true), answer: refusal, status: http.StatusForbidden, reason: "Only the Owner may do this."},
		{name: "someone else's, on a page with a fragment, htmx", c: with(onPage, notTheirs, true), htmx: true, answer: refusal, status: http.StatusForbidden, reason: "Only the Owner may do this."},
		{name: "someone else's, as the page", c: with(asPage, notTheirs, false), answer: refusal, status: http.StatusForbidden, reason: "Only the Owner may do this."},
		{name: "someone else's, no form", c: with(noForm, notTheirs, false), answer: refusal, status: http.StatusForbidden, reason: "Only the Owner may do this."},

		{name: "a thing that isn't there, on a page with a fragment", c: with(onPage, notThere, true), answer: notFound, status: http.StatusNotFound},
		{name: "a thing that isn't there, on a page with a fragment, htmx", c: with(onPage, notThere, true), htmx: true, answer: notFound, status: http.StatusNotFound},
		{name: "a thing that isn't there, as the page", c: with(asPage, notThere, false), answer: notFound, status: http.StatusNotFound},
		{name: "a thing that isn't there, no form", c: with(noForm, notThere, false), answer: notFound, status: http.StatusNotFound},

		{name: "a failure, on a page with a fragment", c: with(onPage, broken, true), answer: failure, status: http.StatusInternalServerError},
		{name: "a failure, on a page with a fragment, htmx", c: with(onPage, broken, true), htmx: true, answer: failure, status: http.StatusInternalServerError},
		{name: "a failure, as the page", c: with(asPage, broken, false), answer: failure, status: http.StatusInternalServerError},
		{name: "a failure, no form", c: with(noForm, broken, false), answer: failure, status: http.StatusInternalServerError},

		{name: "a whole-form refusal, said in one sentence", c: oneSentence, answer: redirect, status: http.StatusSeeOther, reason: "This thing is already done."},
		{name: "a form its page no longer offers", c: with(notOffered, typed, false), answer: refusal, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a cycle on a form its page no longer offers", c: func() web.FormRefusalCaseForTest { c := with(notOffered, cycle, false); c.OneSentence = true; return c }(), answer: refusal, status: http.StatusConflict, reason: "Linking these Goals would make a cycle."},
		{name: "a form too long to carry back", c: with(onPage, typed, false), form: tooLong, answer: refusal, status: http.StatusUnprocessableEntity, reason: "A thing needs a name."},
		{name: "a problem naming an input the form doesn't post", c: with(onPage, stray, false), answer: refusal, status: http.StatusUnprocessableEntity, reason: "That isn't a colour."},
		{name: "a whole-form refusal on a form saying why beside each field", c: with(onPage, whole, false), answer: refusal, status: http.StatusUnprocessableEntity, reason: "This thing is already done."},
	} {
		path := fmt.Sprintf("/form-refusal-test/%d", i)
		srv.HandleFormRefusalForTest("POST "+path, tc.c)
		form := cmpOr(tc.form, posted)
		buf.Reset()
		resp := postRefused(t, noRedirects(sam), ts.URL+path, form, tc.htmx)

		if resp.status != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, resp.status, tc.status)
		}
		cookie := resp.cookie
		if (cookie != nil) != (tc.answer == redirect) {
			t.Errorf("%s: set the refusal cookie %v, want %t", tc.name, cookie, tc.answer == redirect)
		}
		if logged := strings.Contains(buf.String(), `"msg":"server error"`); logged != (tc.answer == failure) {
			t.Errorf("%s: logged a server error %t, want %t:\n%s", tc.name, logged, tc.answer == failure, &buf)
		}
		body := resp.body
		page := parsePage(t, body)
		switch tc.answer {
		case redirect:
			want := web.FormRefusalPageForTest + "?shown=1#test-form"
			if got := resp.location; got != want {
				t.Errorf("%s: redirected to %q, want %q", tc.name, got, want)
			}
			if cookie != nil && cookie.Path != web.FormRefusalPageForTest {
				t.Errorf("%s: refusal cookie scoped to %q, want %q", tc.name, cookie.Path, web.FormRefusalPageForTest)
			}
			assertFormCameBack(t, tc.name, getPage(t, sam, ts.URL+want), form, tc.reason, tc.problem)
			if again := getPage(t, sam, ts.URL+want); !hasTestID(again, "plain-page") {
				t.Errorf("%s: the form came back twice:\n%s", tc.name, renderNode(again))
			}
		case asWhole, partial:
			if !hasTestID(page, tc.answer) {
				t.Errorf("%s: answer is not the %s:\n%s", tc.name, tc.answer, body)
			}
			if got := textOf(findByTestID(t, page, "form-reason")); got != tc.reason {
				t.Errorf("%s: says %q, want %q", tc.name, got, tc.reason)
			}
		case refusal:
			assertRefusalPage(t, page, "Can't do it", tc.reason, web.FormRefusalBackForTest)
		case notFound:
			if !hasTestID(page, "not-found") {
				t.Errorf("%s: not the Not found page:\n%s", tc.name, body)
			}
		}
	}
}

// refusedAnswer is how a refused post was answered, unfollowed: its status,
// where it redirects, the refused form's cookie it sets (nil for none), and
// its body.
type refusedAnswer struct {
	status   int
	location string
	cookie   *http.Cookie
	body     string
}

// postRefused posts form to rawURL as client, as htmx does when htmx, and
// returns the answer.
func postRefused(t *testing.T, client *http.Client, rawURL string, form url.Values, htmx bool) refusedAnswer {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer := refusedAnswer{status: resp.StatusCode, location: resp.Header.Get("Location"), body: readBody(t, resp)}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_refused" && c.MaxAge >= 0 {
			answer.cookie = c
		}
	}
	return answer
}

// assertFormCameBack fails the test, named name, unless page shows the test
// form back open, holding what form posted in its own inputs, name and note,
// and nothing else, saying reason above it, when set, and problem beside its
// name input, when set.
func assertFormCameBack(t *testing.T, name string, page *html.Node, form url.Values, reason, problem string) {
	t.Helper()
	refused := findByTestID(t, page, "refused-form")
	for _, input := range []string{"name", "note"} {
		if got := textOf(findByTestID(t, refused, "typed-"+input)); got != form.Get(input) {
			t.Errorf("%s: %s came back as %q, want %q", name, input, got, form.Get(input))
		}
	}
	if hasTestID(refused, "typed-other") {
		t.Errorf("%s: an input the form doesn't post came back:\n%s", name, renderNode(refused))
	}
	if got := shownOrEmpty(refused, "form-reason"); got != reason {
		t.Errorf("%s: says %q above the form, want %q", name, got, reason)
	}
	if got := shownOrEmpty(refused, "problem-name"); got != problem {
		t.Errorf("%s: says %q beside name, want %q", name, got, problem)
	}
}

// shownOrEmpty is the text of root's element with testID, "" when there is
// none.
func shownOrEmpty(root *html.Node, testID string) string {
	if !hasTestID(root, testID) {
		return ""
	}
	return textOf(findAllByTestID(root, testID)[0])
}

// cmpOr is v, or else fallback when v is empty.
func cmpOr(v, fallback url.Values) url.Values {
	if len(v) == 0 {
		return fallback
	}
	return v
}
