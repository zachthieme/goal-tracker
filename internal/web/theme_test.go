package web_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// rootElement is the page's opening <html> tag, where the theme pin goes.
func rootElement(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, "<html")
	if start < 0 {
		t.Fatalf("page has no <html> element:\n%s", page)
	}
	return page[start : start+strings.Index(page[start:], ">")+1]
}

// getWithTheme fetches rawURL with the theme cookie set to value.
func getWithTheme(t *testing.T, client *http.Client, rawURL, value string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "gt_theme", Value: value})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

// The page is pinned to the chosen theme from the first byte, signed in or
// not. With no choice, or one the server doesn't recognise, it isn't pinned
// and follows the system.
func TestThemeCookiePinsEveryPage(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		cookie, want string
	}{
		{"light", `data-theme="light"`},
		{"dark", `data-theme="dark"`},
	} {
		if root := rootElement(t, getWithTheme(t, http.DefaultClient, ts.URL+"/signin", tc.cookie)); !strings.Contains(root, tc.want) {
			t.Errorf("sign-in page with %q chosen: root %s, want %s", tc.cookie, root, tc.want)
		}
		if root := rootElement(t, getWithTheme(t, client, ts.URL+"/home", tc.cookie)); !strings.Contains(root, tc.want) {
			t.Errorf("Home with %q chosen: root %s, want %s", tc.cookie, root, tc.want)
		}
	}

	for _, cookie := range []string{"system", "purple", ""} {
		if root := rootElement(t, getWithTheme(t, client, ts.URL+"/home", cookie)); strings.Contains(root, "data-theme") {
			t.Errorf("Home with %q in the cookie is pinned: %s", cookie, root)
		}
	}
	if root := rootElement(t, getBody(t, http.DefaultClient, ts.URL+"/signin")); strings.Contains(root, "data-theme") {
		t.Errorf("sign-in page with no cookie is pinned: %s", root)
	}
}

// noRedirects stops client following redirects, so a test sees where a
// response sends the person.
func noRedirects(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// chooseTheme posts a theme choice from the page at back and returns the
// response, unfollowed.
func chooseTheme(t *testing.T, client *http.Client, baseURL, choice, back string) *http.Response {
	t.Helper()
	resp := postForm(t, noRedirects(client), baseURL+"/theme", url.Values{"theme": {choice}, "return": {back}})
	_ = readBody(t, resp)
	return resp
}

// themeCookieIn is the theme cookie a response sets, nil when it sets none.
func themeCookieIn(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "gt_theme" {
			return c
		}
	}
	return nil
}

// Choosing Light or Dark remembers it in a cookie that lasts a year, renewed on
// each choice, and pins every page to it. Choosing System clears the cookie,
// and the pages follow the system again.
func TestChoosingAThemePinsItAndSystemClearsIt(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, choice := range []string{"dark", "light"} {
		resp := chooseTheme(t, client, ts.URL, choice, "/home")
		c := themeCookieIn(resp)
		if c == nil || c.Value != choice {
			t.Fatalf("choosing %s: cookie %v, want %s", choice, c, choice)
		}
		if year := int((365 * 24 * time.Hour).Seconds()); c.MaxAge != year || c.Path != "/" {
			t.Errorf("choosing %s: cookie lasts %ds on path %q, want a year (%ds) on /", choice, c.MaxAge, c.Path, year)
		}
		want := `data-theme="` + choice + `"`
		for _, path := range []string{"/home", "/goals"} {
			if root := rootElement(t, getBody(t, client, ts.URL+path)); !strings.Contains(root, want) {
				t.Errorf("%s after choosing %s: root %s, want %s", path, choice, root, want)
			}
		}
	}

	resp := chooseTheme(t, client, ts.URL, "system", "/home")
	if c := themeCookieIn(resp); c == nil || c.MaxAge >= 0 {
		t.Errorf("choosing system does not clear the cookie: %v", c)
	}
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); strings.Contains(root, "data-theme") {
		t.Errorf("Home after choosing system is still pinned: %s", root)
	}
}

// After choosing, the person lands back on the page they chose from. A return
// address that isn't a path on this site lands on Home instead.
func TestChoosingAThemeReturnsToThePage(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		back, want string
	}{
		{"/goals?owner=me", "/goals?owner=me"},
		{"/signin", "/signin"},
		{"", "/home"},
		{"goals", "/home"},
		{"https://evil.example/", "/home"},
		{"//evil.example/goals", "/home"},
		{`/\evil.example/goals`, "/home"},
		{"javascript:alert(1)", "/home"},
	} {
		resp := chooseTheme(t, client, ts.URL, "dark", tc.back)
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != tc.want {
			t.Errorf("choosing from %q: status %d to %q, want 303 to %q", tc.back, resp.StatusCode, loc, tc.want)
		}
	}
}

// The choice belongs to the browser, not the session: it can be made before
// signing in, and it survives signing out and in again.
func TestThemeChoiceOutlivesTheSession(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	if resp := chooseTheme(t, client, ts.URL, "dark", "/signin"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("choosing before sign-in: status %d, want 303", resp.StatusCode)
	}
	if root := rootElement(t, getBody(t, client, ts.URL+"/signin")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("sign-in page after choosing dark: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}}))
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("Home after signing in: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signout", nil))
	if root := rootElement(t, getBody(t, client, ts.URL+"/signin")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("sign-in page after signing out: root %s", root)
	}
	_ = readBody(t, postForm(t, client, ts.URL+"/signin", url.Values{"email": {"sam@example.com"}}))
	if root := rootElement(t, getBody(t, client, ts.URL+"/home")); !strings.Contains(root, `data-theme="dark"`) {
		t.Errorf("Home after signing in again: root %s", root)
	}
}

// themeMenu is the top bar's Theme disclosure on page.
func themeMenu(t *testing.T, page string) string {
	t.Helper()
	nav := page[strings.Index(page, `<nav class="nav">`):strings.Index(page, "</nav>")]
	return pageElement(t, nav, "details", "theme-menu")
}

// The top bar has a Theme menu, signed in or not, that opens and closes
// without script (a <details>) beside Sign out. It lists System, Light and
// Dark in words, marks the current choice, and each choice is a plain form
// post that comes back to the page it was made on.
func TestThemeMenuOffersTheThreeChoices(t *testing.T) {
	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	g := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	goalPath := "/goals/" + strconv.FormatInt(g.ID, 10)

	for _, tc := range []struct {
		name    string
		client  *http.Client
		path    string
		cookie  string
		current string
	}{
		{"sign-in page, nothing chosen", http.DefaultClient, "/signin", "", "system"},
		{"sign-in page, Dark chosen", http.DefaultClient, "/signin", "dark", "dark"},
		{"Goal page, nothing chosen", client, goalPath, "", "system"},
		{"Goal page, Light chosen", client, goalPath, "light", "light"},
		{"Goal page, unrecognised choice", client, goalPath, "purple", "system"},
	} {
		page := getWithTheme(t, tc.client, ts.URL+tc.path, tc.cookie)
		menu := themeMenu(t, page)
		if !strings.Contains(menu, ">Theme</summary>") {
			t.Errorf("%s: the menu is not labelled Theme: %s", tc.name, menu)
		}
		if !strings.Contains(menu, `method="post" action="/theme"`) {
			t.Errorf("%s: the menu does not post to /theme: %s", tc.name, menu)
		}
		if want := `name="return" value="` + tc.path + `"`; !strings.Contains(menu, want) {
			t.Errorf("%s: the menu does not return to %s: %s", tc.name, tc.path, menu)
		}
		if strings.Contains(menu, "onclick") || strings.Contains(menu, "<script") {
			t.Errorf("%s: the menu relies on script: %s", tc.name, menu)
		}
		for _, choice := range []struct{ value, label string }{{"system", "System"}, {"light", "Light"}, {"dark", "Dark"}} {
			button := pageElement(t, menu, "button", "theme-"+choice.value)
			if !strings.Contains(button, `type="submit"`) || !strings.Contains(button, `name="theme" value="`+choice.value+`"`) || !strings.Contains(button, choice.label) {
				t.Errorf("%s: the %s choice is not a submit of theme=%s labelled %s: %s", tc.name, choice.value, choice.value, choice.label, button)
			}
			if marked := strings.Contains(button, `aria-current="true"`); marked != (choice.value == tc.current) {
				t.Errorf("%s: %s marked current = %v, want %v: %s", tc.name, choice.value, marked, choice.value == tc.current, button)
			}
		}
	}

	page := getBody(t, client, ts.URL+goalPath)
	me := page[strings.Index(page, `<div class="me">`):strings.Index(page, "</nav>")]
	if !strings.Contains(me, `data-testid="theme-menu"`) || !strings.Contains(me, "Sign out") {
		t.Errorf("the Theme menu is not beside Sign out: %s", me)
	}
}

// A Report's Print view is black on white whatever theme is chosen: it renders
// the same with each choice as with none.
func TestPrintViewIgnoresTheTheme(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	printPath := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10) + "/publications/" + strconv.FormatInt(pub.ID, 10) + "/print"

	unchosen := getBody(t, client, printPath)
	if strings.Contains(unchosen, "data-theme") {
		t.Errorf("the Print view is pinned to a theme: %s", rootElement(t, unchosen))
	}
	for _, choice := range []string{"light", "dark"} {
		if page := getWithTheme(t, client, printPath, choice); page != unchosen {
			t.Errorf("the Print view with %s chosen differs from the one with none:\n%s", choice, page)
		}
	}
}

// The open Theme menu floats over the page, anchored under its button and no
// wider than the screen, so neither opening it nor a narrow screen widens the
// top bar. The current choice is marked to the eye, not only to a screen
// reader.
func TestThemeMenuFloatsWithoutWideningTheTopBar(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	if decls := cssRule(t, css, "\n.theme"); !strings.Contains(decls, "position:relative") {
		t.Errorf("the Theme menu does not anchor its panel: %s", decls)
	}
	decls := cssRule(t, css, "\n.theme-menu")
	for _, want := range []string{"position:absolute", "right:0", "max-width:calc(100vw"} {
		if !strings.Contains(decls, want) {
			t.Errorf("the Theme menu's panel lacks %s: %s", want, decls)
		}
	}
	if decls := cssRule(t, css, `.theme-choice[aria-current="true"]`); decls == "" {
		t.Errorf("the current theme is not marked to the eye")
	}
}
