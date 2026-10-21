package web_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

func TestGoalsRequiresSignIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	// A client that does not follow redirects, so we can see the redirect itself.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(ts.URL + "/goals")
	if err != nil {
		t.Fatalf("GET /goals: %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status %d, want 303 redirect to sign-in", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/signin" {
		t.Errorf("redirect to %q, want /signin", loc)
	}
}

// Signing in lands on Home, and so does visiting / or the sign-in page while
// signed in.
func TestSignedInPersonLandsOnHome(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	if err != nil {
		t.Fatalf("POST /signin: %v", err)
	}
	_ = resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/home" {
		t.Errorf("sign-in: status %d to %q, want 303 to /home", resp.StatusCode, loc)
	}
	for _, path := range []string{"/", "/signin"} {
		resp, err := client.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/home" {
			t.Errorf("GET %s signed in: status %d to %q, want 303 to /home", path, resp.StatusCode, loc)
		}
	}
}

// The sign-in page is a single centred column on the canvas, the app's mark
// above it and no card (DESIGN.md § Sign-in and Set password): the "Sign in"
// heading, the development sign-in note, and the email field with its button.
// The signed-out bar already shows the app's name, so the column doesn't
// repeat it.
func TestSignInPageIsACentredColumn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)

	page := getBody(t, http.DefaultClient, ts.URL+"/signin")

	at := strings.Index(page, `<form data-testid="signin-card"`)
	if at < 0 {
		t.Fatalf("the sign-in page has no sign-in form:\n%s", page)
	}
	card := page[at : at+strings.Index(page[at:], "</form>")]
	if c := regexp.MustCompile(`class="(?:[^"]* )?card(?: [^"]*)?"`).FindString(page); c != "" {
		t.Errorf("the sign-in page has a card, %s:\n%s", c, page)
	}
	if above := between(t, page, "<main", `<form data-testid="signin-card"`); !strings.Contains(above, `class="brand-mark si-mark"`) {
		t.Errorf("the sign-in column has no app's mark above it:\n%s", above)
	}
	body := strings.TrimSpace(card[len(openTag(card))+1:])
	if !strings.HasPrefix(body, "<h1>Sign in</h1>") {
		t.Errorf("the sign-in form doesn't start with the Sign in heading:\n%s", card)
	}
	if strings.Contains(card, "Goal Tracker") {
		t.Errorf("the sign-in form repeats the brand:\n%s", card)
	}
	for _, want := range []string{"Development sign-in", `type="email" name="email"`, `<button type="submit" class="btn primary`} {
		if !strings.Contains(card, want) {
			t.Errorf("the sign-in form lacks %s:\n%s", want, card)
		}
	}
}

// Signing in as a Departed person fails with a visible reason, and no session
// cookie is issued (CONTEXT.md: Departed).
func TestDepartedPersonCannotSignIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	resp := postForm(t, http.DefaultClient, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("sign-in as Departed: status %d, want 403", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			t.Errorf("sign-in as Departed issued a session cookie: %v", c)
		}
	}
	if msg := pageElement(t, body, "p", "signin-error"); !strings.Contains(msg, "sam@example.com has been marked departed") {
		t.Errorf("sign-in page doesn't say why: %s", msg)
	}
}

// A Departed person can't sign in again by changing the case of their email:
// the form shows the same "marked departed" page and issues no session
// (CONTEXT.md: Account, Departed).
func TestDepartedPersonCannotSignInUnderADifferentCase(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	freya := h.SignIn("freya.nilsen@example.com")
	if err := h.Service.MarkDeparted(t.Context(), boss.ID, freya.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	resp := postForm(t, http.DefaultClient, ts.URL+"/signin", url.Values{"email": {"Freya.Nilsen@Example.com"}})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("sign-in as a different case of a Departed email: status %d, want 403", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			t.Errorf("sign-in as a different case of a Departed email issued a session cookie: %v", c)
		}
	}
	if msg := pageElement(t, body, "p", "signin-error"); !strings.Contains(msg, "has been marked departed") {
		t.Errorf("sign-in page doesn't say why: %s", msg)
	}
}

// A session that was valid before its person departed stops working: the next
// request is treated as signed out, so a Check-in POST is sent to sign-in rather
// than accepted (CONTEXT.md: Departed).
func TestDepartureEndsAnExistingSession(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	samClient := signInClient(t, ts.URL, "sam@example.com")

	if err := h.Service.MarkDeparted(t.Context(), boss.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	samClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := postForm(t, samClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"All good."},
	})
	_ = readBody(t, resp)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/signin" {
		t.Errorf("Check-in POST on a departed session: status %d to %q, want 303 to /signin", resp.StatusCode, loc)
	}
	if _, ok, err := h.Service.LatestCheckin(t.Context(), goal.ID); err != nil || ok {
		t.Errorf("the departed session's Check-in was recorded (ok=%v, err=%v)", ok, err)
	}
}

// A Delegate who departs can no longer check in on the Owner's Goal: their
// session is treated as signed out, and the Check-in isn't recorded.
func TestDepartedDelegateCannotCheckIn(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	tpm := h.SignIn("tpm@example.com")
	goal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(sam, tpm, goal.ID)
	tpmClient := signInClient(t, ts.URL, "tpm@example.com")

	if err := h.Service.MarkDeparted(t.Context(), boss.ID, tpm.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	tpmClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp := postForm(t, tpmClient, fmt.Sprintf("%s/goals/%d/checkins", ts.URL, goal.ID), url.Values{
		"health": {domain.HealthGreen},
		"status": {"Checked in for Sam."},
	})
	_ = readBody(t, resp)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/signin" {
		t.Errorf("Departed Delegate's Check-in POST: status %d to %q, want 303 to /signin", resp.StatusCode, loc)
	}
	if _, ok, err := h.Service.LatestCheckin(t.Context(), goal.ID); err != nil || ok {
		t.Errorf("the Departed Delegate's Check-in was recorded (ok=%v, err=%v)", ok, err)
	}
}

// sessionCookieOf signs emailAddr in through the form and returns the session
// cookie the server issued.
func sessionCookieOf(t *testing.T, baseURL, emailAddr string) *http.Cookie {
	t.Helper()
	resp := postForm(t, noRedirects(http.DefaultClient), baseURL+"/signin", url.Values{"email": {emailAddr}})
	_ = readBody(t, resp)
	for _, c := range resp.Cookies() {
		if c.Name == "gt_session" && c.Value != "" {
			return c
		}
	}
	t.Fatalf("sign in %q issued no session cookie", emailAddr)
	return nil
}

// signedOut reports whether a request carrying only cookie is sent to sign-in.
func signedOut(t *testing.T, baseURL string, cookie *http.Cookie) bool {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/home", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	resp, err := noRedirects(http.DefaultClient).Do(req)
	if err != nil {
		t.Fatalf("GET /home: %v", err)
	}
	_ = readBody(t, resp)
	return resp.StatusCode == http.StatusSeeOther && resp.Header.Get("Location") == "/signin"
}

// The session cookie is signed: one whose Account ID is swapped for another's,
// keeping its signature, doesn't sign anyone in.
func TestATamperedSessionCookieIsSignedOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	boss := h.SignIn("boss@example.com")
	sam := sessionCookieOf(t, ts.URL, "sam@example.com")
	if signedOut(t, ts.URL, sam) {
		t.Fatal("sam's own session cookie doesn't sign sam in")
	}

	parts := strings.Split(sam.Value, ".")
	if len(parts) != 5 {
		t.Fatalf("session cookie %q isn't <id>.<generation>.<signed-in>.<last-seen>.<signature>", sam.Value)
	}
	if parts[0] == strconv.FormatInt(boss.ID, 10) {
		t.Fatalf("sam and boss share Account ID %s", parts[0])
	}
	parts[0] = strconv.FormatInt(boss.ID, 10)
	forged := &http.Cookie{Name: "gt_session", Value: strings.Join(parts, ".")}
	if !signedOut(t, ts.URL, forged) {
		t.Errorf("a cookie with boss's ID and sam's signature signs someone in")
	}
}

// A session cookie from before cookies were signed, the bare Account ID, counts
// as signed out (the breaking change: everyone signs in once more).
func TestAnUnsignedSessionCookieIsSignedOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")

	legacy := &http.Cookie{Name: "gt_session", Value: strconv.FormatInt(sam.ID, 10)}
	if !signedOut(t, ts.URL, legacy) {
		t.Errorf("an unsigned gt_session=%d signs sam in", sam.ID)
	}
}

// The session cookie is HttpOnly and SameSite=Lax, and Secure when the app is
// served over https.
func TestTheSessionCookieIsSecureOverHTTPS(t *testing.T) {
	t.Parallel()

	for _, secure := range []bool{false, true} {
		h := testsupport.New(t)
		ts := httptest.NewServer(web.NewServer(h.Service, web.WithSecureCookies(secure)))
		t.Cleanup(ts.Close)

		c := sessionCookieOf(t, ts.URL, "sam@example.com")
		if c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("WithSecureCookies(%v): session cookie Secure=%v HttpOnly=%v SameSite=%v, want Secure=%v HttpOnly SameSite=Lax",
				secure, c.Secure, c.HttpOnly, c.SameSite, secure)
		}
	}
}

// EndSessions ends a session already open: the cookie issued before it sends
// /home to sign-in. Signing in again afterwards issues a cookie that works.
func TestEndingSessionsSignsAnOpenSessionOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	before := sessionCookieOf(t, ts.URL, "sam@example.com")
	if signedOut(t, ts.URL, before) {
		t.Fatal("sam's session cookie doesn't sign sam in")
	}
	sam, err := h.Service.AccountByEmail(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Service.EndSessions(t.Context(), sam.ID); err != nil {
		t.Fatalf("EndSessions: %v", err)
	}

	if !signedOut(t, ts.URL, before) {
		t.Error("a cookie issued before EndSessions still signs sam in")
	}
	if after := sessionCookieOf(t, ts.URL, "sam@example.com"); signedOut(t, ts.URL, after) {
		t.Error("signing in again after EndSessions doesn't sign sam in")
	}
}

// A "<id>.<signature>" cookie, the shape issued before cookies carried a
// session generation, counts as signed out even when signed with the right key
// (the breaking change: everyone signs in once more).
func TestATwoPartSessionCookieIsSignedOut(t *testing.T) {
	t.Parallel()

	key := []byte("0123456789abcdef0123456789abcdef")
	h := testsupport.New(t)
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithSessionKey(key)))
	t.Cleanup(ts.Close)
	sam := h.SignIn("sam@example.com")

	id := strconv.FormatInt(sam.ID, 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id))
	old := &http.Cookie{Name: "gt_session", Value: id + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}
	if !signedOut(t, ts.URL, old) {
		t.Errorf("a two-part gt_session=%s signs sam in", old.Value)
	}
}

// A session cookie whose generation is edited, keeping its signature, doesn't
// sign anyone in, so an ended session can't be revived by moving its
// generation on.
func TestASessionCookieWithAnEditedGenerationIsSignedOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	cookie := sessionCookieOf(t, ts.URL, "sam@example.com")
	sam, err := h.Service.AccountByEmail(t.Context(), "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Service.EndSessions(t.Context(), sam.ID); err != nil {
		t.Fatalf("EndSessions: %v", err)
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 5 || parts[1] != "0" {
		t.Fatalf("session cookie %q isn't <id>.0.<signed-in>.<last-seen>.<signature>", cookie.Value)
	}
	parts[1] = "1"
	edited := &http.Cookie{Name: "gt_session", Value: strings.Join(parts, ".")}
	if !signedOut(t, ts.URL, edited) {
		t.Errorf("a cookie with its generation edited to the current one signs sam in")
	}
}

// A session left idle for 7 days ends: 8 days after sign-in with no request in
// between, the cookie sends /home to sign-in.
func TestASessionIdleForAWeekIsSignedOut(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	cookie := sessionCookieOf(t, ts.URL, "sam@example.com")

	h.Clock.Advance(8 * 24 * time.Hour)

	if !signedOut(t, ts.URL, cookie) {
		t.Error("a session idle for 8 days still signs sam in")
	}
}

// A request slides the idle limit: with a request 6 days after sign-in, the
// session still signs sam in 12 days after it, past 7 days from sign-in.
func TestARequestKeepsASessionFromIdling(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	client := noRedirects(signInClient(t, ts.URL, "sam@example.com"))

	for _, day := range []int{6, 12} {
		h.Clock.Advance(6 * 24 * time.Hour)
		if clientSignedOut(t, client, ts.URL) {
			t.Fatalf("day %d, with a request every 6 days: sam is signed out", day)
		}
	}
}

// clientSignedOut reports whether client's GET /home is sent to sign-in.
func clientSignedOut(t *testing.T, client *http.Client, baseURL string) bool {
	t.Helper()
	resp, err := client.Get(baseURL + "/home")
	if err != nil {
		t.Fatalf("GET /home: %v", err)
	}
	_ = readBody(t, resp)
	return resp.StatusCode == http.StatusSeeOther && resp.Header.Get("Location") == "/signin"
}
