package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// postSignInFetchedFrom posts the development sign-in form as a browser would
// from site, the value of its Sec-Fetch-Site header ("" sends none), without
// following the redirect, and returns the response's status.
func postSignInFetchedFrom(t *testing.T, base, site string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/signin", strings.NewReader(url.Values{"email": {"sam@example.com"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	resp, err := noRedirects(http.DefaultClient).Do(req)
	if err != nil {
		t.Fatalf("POST /signin from %q: %v", site, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	return resp.StatusCode
}

// A POST another site's page makes is refused, so a page elsewhere can't act
// as whoever is signed in; the same POST from the app's own pages, or from a
// client that says nothing of where it comes from, goes through (#415).
func TestACrossSitePostIsRefusedWith403(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	for site, want := range map[string]int{
		"cross-site":  http.StatusForbidden,
		"same-site":   http.StatusForbidden,
		"same-origin": http.StatusSeeOther,
		"none":        http.StatusSeeOther,
		"":            http.StatusSeeOther,
	} {
		if got := postSignInFetchedFrom(t, ts.URL, site); got != want {
			t.Errorf("POST /signin with Sec-Fetch-Site %q: status %d, want %d", site, got, want)
		}
	}
}

// eachSecurityHeadersResponse requests, from the app at base, a response of
// each kind the security headers must be on — a page, a static file, a 404, a
// redirect to sign-in, a refused cross-site POST — and calls check on each,
// with what it is and its body already read.
func eachSecurityHeadersResponse(t *testing.T, base string, check func(what string, resp *http.Response)) {
	t.Helper()
	for _, c := range []struct{ what, method, path, site string }{
		{"the sign-in page", http.MethodGet, "/signin", ""},
		{"a static file", http.MethodGet, "/static/app.css", ""},
		{"a missing static file", http.MethodGet, "/static/missing.css", ""},
		{"a page that needs sign-in", http.MethodGet, "/home", ""},
		{"a refused cross-site POST", http.MethodPost, "/signin", "cross-site"},
	} {
		req, err := http.NewRequestWithContext(t.Context(), c.method, base+c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.site != "" {
			req.Header.Set("Sec-Fetch-Site", c.site)
		}
		resp, err := noRedirects(http.DefaultClient).Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		check(fmt.Sprintf("%s (%s %s, status %d)", c.what, c.method, c.path, resp.StatusCode), resp)
	}
}

// Every response tells the browser not to show it in a frame on any site, not
// to load plugins or let a <base> tag move its relative links, not to guess its
// content type, and to send its address only to the app itself (#415, #491).
func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	want := map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'; object-src 'none'; base-uri 'none'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
	}
	eachSecurityHeadersResponse(t, ts.URL, func(what string, resp *http.Response) {
		for name, value := range want {
			if got := resp.Header.Values(name); len(got) != 1 || got[0] != value {
				t.Errorf("%s: %s is %q, want %q", what, name, got, value)
			}
		}
	})
}

// Served over https, every response tells the browser to reach the app only
// over https for the next year; served over plain http, none does, since a
// browser ignores it there and the app may never get https (#491).
func TestEveryResponseOverHTTPSCarriesStrictTransportSecurity(t *testing.T) {
	t.Parallel()

	for secure, want := range map[bool][]string{
		true:  {"max-age=31536000"},
		false: nil,
	} {
		h := testsupport.New(t)
		ts := httptest.NewServer(web.NewServer(h.Service, web.WithSecureCookies(secure)))
		t.Cleanup(ts.Close)
		eachSecurityHeadersResponse(t, ts.URL, func(what string, resp *http.Response) {
			if got := resp.Header.Values("Strict-Transport-Security"); !slices.Equal(got, want) {
				t.Errorf("WithSecureCookies(%v), %s: Strict-Transport-Security is %q, want %q", secure, what, got, want)
			}
		})
	}
}
