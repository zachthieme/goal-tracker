package web_test

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// signInClient returns an HTTP client with its own cookie jar, signed in as
// emailAddr.
func signInClient(t *testing.T, baseURL, emailAddr string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := postForm(t, client, baseURL+"/signin", url.Values{"email": {emailAddr}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign in %q: status %d", emailAddr, resp.StatusCode)
	}
	_ = readBody(t, resp)
	return client
}

// navTo is the anchor a parents/children navigation list renders for a Goal —
// distinct from the request form's <option value="id">, so it tells a real link
// apart from a mere candidate.
func navTo(goalID int64) string {
	return fmt.Sprintf(`href="/goals/%d"`, goalID)
}

func getBody(t *testing.T, client *http.Client, rawURL string) string {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

func newServer(t *testing.T, h *testsupport.Harness) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(web.NewServer(h.Service))
	t.Cleanup(ts.Close)
	return ts
}

// assertSubmitsAtOnce checks a form's opening tag sends it without a browser
// dialog asking for confirmation first (#56).
func assertSubmitsAtOnce(t *testing.T, what, form string) {
	t.Helper()
	if strings.Contains(form, "onsubmit=") || strings.Contains(form, "confirm(") {
		t.Errorf("%s asks for confirmation: %s", what, form)
	}
}

// between returns s from the first start up to the first end after it, or to
// the end of s when end is "", failing the test if either is missing.
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	at := strings.Index(s, start)
	if at < 0 {
		t.Fatalf("no %s in:\n%s", start, s)
	}
	s = s[at:]
	if end == "" {
		return s
	}
	upTo := strings.Index(s, end)
	if upTo < 0 {
		t.Fatalf("no %s after %s in:\n%s", end, start, s)
	}
	return s[:upTo]
}

func postForm(t *testing.T, client *http.Client, rawURL string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(rawURL, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// cssRule returns the declarations of the page's style rule for selector.
func cssRule(t *testing.T, page, selector string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{([^}]*)\}`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("page has no %s rule", selector)
	}
	return m[1]
}

// shownAs is how a page shows a person: by label, on a control that expands
// their email inline beside it, with the email on hover too (CONTEXT.md: Name).
func shownAs(emailAddr, label string) string {
	return `<span class="person"><button type="button" class="disclose" aria-expanded="false" title="` + emailAddr + `">` + label +
		`</button><span class="person-email" hidden>` + emailAddr + `</span></span>`
}

// openForms counts the forms a Goal page shows open in place.
func openForms(page string) int {
	return strings.Count(page, `data-open-form="`)
}

// openForm returns the form the Goal page shows open in place, from its
// wrapper to its Cancel link, failing unless exactly one named form is open.
func openForm(t *testing.T, page, form string) string {
	t.Helper()
	if n := openForms(page); n != 1 {
		t.Fatalf("page shows %d forms open, want only %q", n, form)
	}
	open := between(t, page, `data-open-form="`, `data-testid="cancel-form"`)
	if !strings.HasPrefix(open, `data-open-form="`+form+`"`) {
		t.Fatalf("page shows the wrong form open, want %q: %s", form, open)
	}
	return open + tagAround(t, page, `data-testid="cancel-form"`)
}

// postFormHX posts a form with the HX-Request header set, as htmx does, and
// returns the body and status without following redirects or asserting 200.
func postFormHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b), resp.StatusCode
}

// pageElement returns the tag element carrying data-testid, up to its first
// closing tag, so an assertion can't be satisfied by the same text elsewhere on
// the page. It suits elements that don't nest their own tag.
func pageElement(t *testing.T, page, tag, testID string) string {
	t.Helper()
	start := strings.Index(page, "<"+tag+` data-testid="`+testID+`"`)
	if start < 0 {
		t.Fatalf("page has no <%s> %q", tag, testID)
	}
	end := strings.Index(page[start:], "</"+tag+">")
	if end < 0 {
		t.Fatalf("<%s> %q is not closed", tag, testID)
	}
	return page[start : start+end]
}

// elementTexts returns the text of every tag element carrying data-testid, in
// order, with its markup dropped and its whitespace collapsed, so an assertion
// can read the order a row's parts come in. It suits elements that don't nest
// their own tag.
func elementTexts(page, tag, testID string) []string {
	var out []string
	markup := regexp.MustCompile(`<[^>]*>`)
	for rest := page; ; {
		start := strings.Index(rest, "<"+tag+` data-testid="`+testID+`"`)
		if start < 0 {
			return out
		}
		rest = rest[start:]
		end := strings.Index(rest, "</"+tag+">")
		if end < 0 {
			return out
		}
		text := html.UnescapeString(markup.ReplaceAllString(rest[:end], " "))
		out = append(out, strings.Join(strings.Fields(text), " "))
		rest = rest[end:]
	}
}

// openTag returns element up to the end of its opening tag, so an assertion
// about the tag's attributes can't match its content.
func openTag(element string) string {
	if end := strings.Index(element, ">"); end >= 0 {
		return element[:end]
	}
	return element
}

// assertStyledBy checks the element's opening tag carries no style attribute
// but the class, and that rules declare exactly want for that class.
func assertStyledBy(t *testing.T, tag, rules, class, want string) {
	t.Helper()
	if strings.Contains(tag, " style=") {
		t.Errorf("element carries a style attribute: %s", tag)
	}
	if !slices.Contains(strings.Fields(attr(tag, "class")), class) {
		t.Errorf("element lacks class %s: %s", class, tag)
	}
	if got := cssRule(t, rules, "."+class); got != want {
		t.Errorf(".%s declares %q, want %q", class, got, want)
	}
}

// tagAround returns the whole opening tag holding marker, its attributes in
// whatever order they render.
func tagAround(t *testing.T, page, marker string) string {
	t.Helper()
	at := strings.Index(page, marker)
	if at < 0 {
		t.Fatalf("page has no %s", marker)
	}
	start := strings.LastIndex(page[:at], "<")
	end := strings.Index(page[at:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("%s is not inside a tag", marker)
	}
	return page[start : at+end+1]
}

// attr returns the value of the named attribute in an opening tag, or "".
func attr(tag, name string) string {
	m := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

// mediaBlocks returns the body of every media block the stylesheet opens with
// query, in order.
func mediaBlocks(css, query string) []string {
	var blocks []string
	for {
		at := strings.Index(css, query+"{")
		if at < 0 {
			return blocks
		}
		css = css[at+len(query)+1:]
		depth := 1
		for i := 0; i < len(css); i++ {
			if css[i] == '{' {
				depth++
			} else if css[i] == '}' {
				if depth--; depth == 0 {
					blocks = append(blocks, css[:i])
					css = css[i:]
					break
				}
			}
		}
		if depth != 0 {
			return blocks
		}
	}
}

// mediaBlock returns the body of the stylesheet's first media block opened by
// query.
func mediaBlock(t *testing.T, css, query string) string {
	t.Helper()
	blocks := mediaBlocks(css, query)
	if len(blocks) == 0 {
		t.Fatalf("stylesheet has no closed %s block", query)
	}
	return blocks[0]
}

// ruleFor returns the declarations of the rule in block whose selector list
// names sel.
func ruleFor(block, sel string) (string, bool) {
	for _, rule := range strings.Split(block, "}") {
		selectors, decls, ok := strings.Cut(rule, "{")
		if !ok {
			continue
		}
		for _, s := range strings.Split(selectors, ",") {
			if strings.TrimSpace(s) == sel {
				return decls, true
			}
		}
	}
	return "", false
}

// noRedirects stops client following redirects, so a test sees where a
// response sends the person.
func noRedirects(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// goalPageURL is the address of g's page on the server at base.
func goalPageURL(base string, g domain.Goal) string {
	return base + "/goals/" + strconv.FormatInt(g.ID, 10)
}

// pendingDraftNotes are the Goal's pending Draft Highlights' notes, oldest
// first, as its Owner sees them.
func pendingDraftNotes(t *testing.T, h *testsupport.Harness, owner domain.Account, goalID int64) []string {
	t.Helper()
	pending, err := h.Service.PendingDraftHighlights(context.Background(), owner.ID, goalID)
	if err != nil {
		t.Fatalf("PendingDraftHighlights: %v", err)
	}
	var out []string
	for _, d := range pending {
		out = append(out, d.Note)
	}
	return out
}
