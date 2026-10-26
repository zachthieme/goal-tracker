package web_test

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"iter"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"

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
	defer func() { _ = resp.Body.Close() }()
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
	resp, err := ctxGet(t, client, rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
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

// assertSubmitsAtOnce checks the first form on page whose action ends in
// marker, a whole action path or its tail (/remove), sends it without a browser
// dialog asking for confirmation first (#56): no element from its <form> to its
// </form>, the form or its submit button, has a submit handler, htmx's
// hx-confirm, or an event handler, an on* attribute or htmx's hx-on*, that
// calls confirm(). htmx reads data-hx-* as hx-*, so a data-hx- attribute counts
// as its hx- one. It reads each tag's attributes, as the tokenizer parses them,
// both to pick the form and to check it, so text in another attribute's value
// that looks like an action or a handler isn't one. A form whose </form> isn't
// in page fails: the check can't reach its end.
func assertSubmitsAtOnce(t *testing.T, what, page, marker string) {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(page))
	found, depth := false, 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := string(z.Raw())
		tok := z.Token()
		if depth == 0 && (tt != html.StartTagToken || tok.Data != "form" || !strings.HasSuffix(tokenAttr(tok, "action"), marker)) {
			continue
		}
		found = true
		switch {
		case tt == html.StartTagToken && tok.Data == "form":
			depth++
		case tt == html.EndTagToken && tok.Data == "form":
			depth--
		}
		for _, a := range tok.Attr {
			key := a.Key
			if strings.HasPrefix(key, "data-hx-") {
				key = strings.TrimPrefix(key, "data-")
			}
			handler := strings.HasPrefix(key, "on") || strings.HasPrefix(key, "hx-on")
			if key == "onsubmit" || key == "hx-confirm" || handler && strings.Contains(a.Val, "confirm(") {
				t.Errorf("%s asks for confirmation: %s", what, raw)
			}
		}
		if depth == 0 {
			return
		}
	}
	if !found {
		t.Fatalf("no form's action ends in %s in:\n%s", marker, page)
	}
	t.Fatalf("no </form> closes the form whose action ends in %s in:\n%s", marker, page)
}

// between returns s from the first start up to the first end after it, or to
// the end of s when end is "", failing the test if either is missing. It
// matches strings, not tags: its callers cut markup at any text, often partway
// through a tag, which no parser reading can give them. It is retired as its
// callers convert to the parser helpers below (#437).
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

// ctxGet is client.Get(rawURL), made under the test's context.
func ctxGet(t *testing.T, client *http.Client, rawURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// ctxPost is client.Post(rawURL, contentType, body), made under the test's
// context.
func ctxPost(t *testing.T, client *http.Client, rawURL, contentType string, body io.Reader) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return client.Do(req)
}

// ctxPostForm is client.PostForm(rawURL, form), made under the test's
// context.
func ctxPostForm(t *testing.T, client *http.Client, rawURL string, form url.Values) (*http.Response, error) {
	t.Helper()
	return ctxPost(t, client, rawURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
}

func postForm(t *testing.T, client *http.Client, rawURL string, form url.Values) *http.Response {
	t.Helper()
	resp, err := ctxPostForm(t, client, rawURL, form)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

// postFormStatus posts form to rawURL, as postForm does, and returns the
// response's status, closing its body unread.
func postFormStatus(t *testing.T, client *http.Client, rawURL string, form url.Values) int {
	t.Helper()
	resp := postForm(t, client, rawURL, form)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// postFormBody posts form to rawURL, as postForm does, and returns the
// response's body, whatever its status.
func postFormBody(t *testing.T, client *http.Client, rawURL string, form url.Values) string {
	t.Helper()
	resp := postForm(t, client, rawURL, form)
	defer func() { _ = resp.Body.Close() }()
	return readBody(t, resp)
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

// openForms counts the forms a Goal page shows open in place: the elements
// carrying data-open-form.
func openForms(page string) int {
	n := 0
	for tag := range markupTags(page) {
		if tokenHasAttr(tag.tok, "data-open-form") {
			n++
		}
	}
	return n
}

// openForm returns the form the Goal page shows open in place, from its
// wrapper's opening tag to the end of its Cancel link's, failing unless
// exactly one named form is open.
func openForm(t *testing.T, page, form string) string {
	t.Helper()
	if n := openForms(page); n != 1 {
		t.Fatalf("page shows %d forms open, want only %q", n, form)
	}
	start := -1
	for tag := range markupTags(page) {
		switch {
		case start < 0 && tokenHasAttr(tag.tok, "data-open-form"):
			if got := tokenAttr(tag.tok, "data-open-form"); got != form {
				t.Fatalf("page shows the wrong form open, want %q: %s", form, tag.raw)
			}
			start = tag.at
		case start >= 0 && tokenAttr(tag.tok, "data-testid") == "cancel-form":
			return page[start : tag.at+len(tag.raw)]
		}
	}
	t.Fatalf("the open %q form has no Cancel link", form)
	return ""
}

// markupTag is a start or self-closing tag as a page writes it.
type markupTag struct {
	at  int    // where it starts in the page
	raw string // its markup
	tok html.Token
}

// markupTags yields the start and self-closing tags in page, in order, read by
// html's tokenizer.
func markupTags(page string) iter.Seq[markupTag] {
	return func(yield func(markupTag) bool) {
		z := html.NewTokenizer(strings.NewReader(page))
		for at := 0; ; {
			tt := z.Next()
			if tt == html.ErrorToken {
				return
			}
			raw := string(z.Raw())
			if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
				if !yield(markupTag{at: at, raw: raw, tok: z.Token()}) {
					return
				}
			}
			at += len(raw)
		}
	}
}

// postFormHX posts a form with the HX-Request header set, as htmx does, and
// returns the body and status without following redirects or asserting 200.
func postFormHX(t *testing.T, client *http.Client, rawURL string, form url.Values) (string, int) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rawURL, strings.NewReader(form.Encode()))
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

// pageElement returns the tag element carrying data-testid, from its opening
// tag up to its own closing tag, so an assertion can't be satisfied by the same
// text elsewhere on the page. Its attributes may come in any order.
func pageElement(t *testing.T, page, tag, testID string) string {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(page))
	at, start, depth := 0, -1, 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := len(z.Raw())
		tok := z.Token()
		switch {
		case tok.Data != tag:
		case start < 0 && tt == html.StartTagToken && tokenAttr(tok, "data-testid") == testID:
			start, depth = at, 1
		case start >= 0 && tt == html.StartTagToken:
			depth++
		case start >= 0 && tt == html.EndTagToken:
			if depth--; depth == 0 {
				return page[start:at]
			}
		}
		at += raw
	}
	if start < 0 {
		t.Fatalf("page has no <%s> %q", tag, testID)
	}
	t.Fatalf("<%s> %q is not closed", tag, testID)
	return ""
}

// tokenAttr is the value of a token's named attribute, or "".
func tokenAttr(tok html.Token, name string) string {
	for _, a := range tok.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// tokenHasAttr reports whether a token carries the named attribute, whatever
// its value.
func tokenHasAttr(tok html.Token, name string) bool {
	return slices.ContainsFunc(tok.Attr, func(a html.Attribute) bool { return a.Key == name })
}

// elementTexts returns the text of every tag element carrying data-testid, in
// order, read as textOf reads it but with hidden elements' text kept, so an
// assertion can read the order a row's parts come in.
func elementTexts(page, tag, testID string) []string {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range findAllByTestID(doc, testID) {
		if n.Data == tag {
			out = append(out, strings.Join(nodeWords(n, n, nil), " "))
		}
	}
	return out
}

// openTag returns element up to the end of its opening tag, without the
// closing >, so an assertion about the tag's attributes can't match its
// content. An element cut from partway through a tag, as between cuts one
// from an attribute, gives the rest of that tag.
func openTag(element string) string {
	lead := partwayTagLead(element)
	for tag := range markupTags(lead + element) {
		if tag.at == 0 {
			return strings.TrimSuffix(tag.raw[len(lead):], ">")
		}
		break
	}
	return element
}

// partwayTagLead is what markup cut from partway through a tag, as between
// cuts it from an attribute, needs before it for html's tokenizer to read it
// as the rest of a tag: a tag name, or nothing when markup starts a tag.
func partwayTagLead(markup string) string {
	if strings.HasPrefix(markup, "<") {
		return ""
	}
	return "<x "
}

// assertStyledBy checks the element's opening tag carries no style attribute
// but the class, and that rules declare exactly want for that class. It reads
// the tag's attributes as attr reads them.
func assertStyledBy(t *testing.T, tag, rules, class, want string) {
	t.Helper()
	for read := range markupTags(partwayTagLead(tag) + tag + ">") {
		if tokenHasAttr(read.tok, "style") {
			t.Errorf("element carries a style attribute: %s", tag)
		}
	}
	if !slices.Contains(strings.Fields(attr(tag, "class")), class) {
		t.Errorf("element lacks class %s: %s", class, tag)
	}
	if got := cssRule(t, rules, "."+class); got != want {
		t.Errorf(".%s declares %q, want %q", class, got, want)
	}
}

// tagAround returns the whole opening tag holding marker, its attributes in
// whatever order they render, read by html's tokenizer: the first tag marker
// starts inside, or, where marker starts in text first, the tag of the
// element around the text. A marker may run on past its tag, as
// ">Save</button>" finds the button reading Save.
func tagAround(t *testing.T, page, marker string) string {
	t.Helper()
	var around []openElement // innermost last
	z := html.NewTokenizer(strings.NewReader(page))
	for at := 0; ; {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := string(z.Raw())
		reach := min(at+len(raw)+len(marker)-1, len(page))
		holds := strings.Contains(page[at:reach], marker)
		switch tt {
		case html.SelfClosingTagToken:
			if holds {
				return raw
			}
		case html.StartTagToken:
			if holds {
				return raw
			}
			if name, _ := z.TagName(); !voidElements[string(name)] {
				around = append(around, openElement{name: string(name), tag: raw})
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if i := slices.IndexFunc(around, func(e openElement) bool { return e.name == string(name) }); i >= 0 {
				around = around[:i]
			}
		case html.TextToken:
			if holds && len(around) > 0 {
				return around[len(around)-1].tag
			}
		}
		at += len(raw)
	}
	t.Fatalf("no tag on the page holds %s", marker)
	return ""
}

// openElement is an element tagAround has read the opening tag of but not yet
// its closing tag: its name and raw opening tag.
type openElement struct{ name, tag string }

// voidElements are the elements that have no content and no closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// attr returns the value of the named attribute in an opening tag, or "",
// read by html's tokenizer. The value comes as the markup writes it, still
// HTML-escaped ("Plan &amp; Do"), because callers compare it with escaped
// text; the tokenizer would decode it. The tag may lack its closing >, as
// openTag gives it, or start partway through, as between cuts it. Given more
// than one tag, attr reads the first that carries the attribute.
func attr(tag, name string) string {
	// A character no page uses stands in for & while the tokenizer reads the
	// tag, so it finds no entity to decode.
	const amp = "\uE000"
	for read := range markupTags(partwayTagLead(tag) + strings.ReplaceAll(tag, "&", amp) + ">") {
		if tokenHasAttr(read.tok, name) {
			return strings.ReplaceAll(tokenAttr(read.tok, name), amp, "&")
		}
	}
	return ""
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

// ruleFor returns the declarations of every rule in block whose selector
// list names sel, in order, each set apart by ;, so a declaration in a later
// rule for sel is read too. A comment before the rule isn't part of its
// selector.
func ruleFor(block, sel string) (string, bool) {
	var found []string
	for _, rule := range strings.Split(block, "}") {
		selectors, decls, ok := strings.Cut(rule, "{")
		if !ok {
			continue
		}
		selectors = cssComment.ReplaceAllString(selectors, "")
		if slices.ContainsFunc(strings.Split(selectors, ","), func(s string) bool { return strings.TrimSpace(s) == sel }) {
			found = append(found, decls)
		}
	}
	return strings.Join(found, ";"), len(found) > 0
}

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// The Follow-ups row helpers below read a row's markup as a string, for the
// Nudge and Suggest a parent tests that reach a row from the Follow-ups page
// (nudges_test.go, suggestions_test.go). followups_test.go reads its rows
// through the parser instead (followUpRowIn).

// followUpRows returns every row of the Follow-ups page's table.
func followUpRows(page string) []string {
	var rows []string
	for rest := page; ; {
		start := strings.Index(rest, `<tr data-testid="follow-up-row"`)
		if start < 0 {
			return rows
		}
		end := strings.Index(rest[start:], "</tr>")
		if end < 0 {
			return append(rows, rest[start:])
		}
		rows = append(rows, rest[start:start+end])
		rest = rest[start+end:]
	}
}

// followUpRowOf returns the Follow-ups page's one row for g, failing the test unless
// the table holds exactly one row linking to it.
func followUpRowOf(t *testing.T, page string, g domain.Goal) string {
	t.Helper()
	var found []string
	for _, row := range followUpRows(page) {
		if isFollowUpRowOf(row, g) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the Follow-ups table has %d rows for %q, want 1:\n%s", len(found), g.Title, page)
	}
	return found[0]
}

// isFollowUpRowOf reports whether a Follow-ups table row is g's: its Goal cell links
// to g.
func isFollowUpRowOf(row string, g domain.Goal) bool {
	return strings.Contains(row, `<td data-testid="follow-up-goal" class="rk-goal">`+`<a `+navTo(g.ID)+`>`)
}

// followUpChip returns a row's chip for one signal kind.
func followUpChip(t *testing.T, row, kind string) string {
	t.Helper()
	start := strings.Index(row, `<span data-testid="follow-up-signal" data-kind="`+kind+`"`)
	if start < 0 {
		t.Fatalf("row has no %s chip:\n%s", kind, row)
	}
	end := strings.Index(row[start:], "</span>")
	if end < 0 {
		t.Fatalf("the %s chip is not closed:\n%s", kind, row)
	}
	return row[start : start+end]
}

// followUpFix returns a row's one Fix: its label, its address (a form's action,
// for a Fix that posts, and "" for a disabled one), and whether it is the
// primary button, failing the test unless the fix cell holds exactly one link,
// one form or one button.
func followUpFix(t *testing.T, row string) (label, href string, primary bool) {
	t.Helper()
	cell := pageElement(t, row, "td", "follow-up-fix")
	links, forms, buttons := strings.Count(cell, "<a "), strings.Count(cell, "<form "), strings.Count(cell, "<button ")
	if links+buttons != 1 || forms > buttons {
		t.Fatalf("fix cell has %d links, %d forms and %d buttons, want one Fix: %s", links, forms, buttons, cell)
	}
	if buttons == 1 {
		_, button, _ := strings.Cut(cell, "<button ")
		tag := openTag("<button " + button)
		label, _, _ = strings.Cut(button[len(tag)-len("<button ")+1:], "</button>")
		if _, form, ok := strings.Cut(cell, "<form "); ok {
			href = html.UnescapeString(attr(openTag("<form "+form)+">", "action"))
		}
		return label, href, strings.Contains(tag, "primary")
	}
	_, link, _ := strings.Cut(cell, "<a ")
	link = "<a " + link
	tag := openTag(link)
	_, rest, _ := strings.Cut(tag, `href="`)
	href, _, _ = strings.Cut(rest, `"`)
	label, _, _ = strings.Cut(link[len(tag)+1:], "</a>")
	return label, html.UnescapeString(href), strings.Contains(tag, "primary")
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

// onlyMilestone returns the one Milestone an ActiveGoal starts with.
func onlyMilestone(t *testing.T, h *testsupport.Harness, goal domain.Goal) domain.Milestone {
	t.Helper()
	ms, err := h.Service.ListMilestones(context.Background(), goal.ID)
	if err != nil || len(ms) != 1 {
		t.Fatalf("ListMilestones = %v, %v", ms, err)
	}
	return ms[0]
}

// scopeOrgFixture is three levels of people for scoping a page to an Org (CONTEXT.md:
// Org): Vera, a VP, manages Mia and Max; Mia manages Pia and Max manages
// Pax. Oz is outside it, with no Manager. Each owns one Active Goal.
type scopeOrgFixture struct {
	Vera, Mia, Max, Pia, Pax, Oz                         domain.Account
	VeraGoal, MiaGoal, MaxGoal, PiaGoal, PaxGoal, OzGoal domain.Goal
}

// newScopeOrgFixture arranges a scopeOrgFixture in h, its people signed in with Names.
func newScopeOrgFixture(h *testsupport.Harness) scopeOrgFixture {
	h.T.Helper()
	var o scopeOrgFixture
	o.Vera = h.SignInNamed("vera@example.com", "Vera Vance")
	o.Mia = h.SignInNamed("mia@example.com", "Mia Moss")
	o.Max = h.SignInNamed("max@example.com", "Max Marsh")
	o.Pia = h.SignInNamed("pia@example.com", "Pia Park")
	o.Pax = h.SignInNamed("pax@example.com", "Pax Penn")
	o.Oz = h.SignInNamed("oz@example.com", "Oz Ortiz")
	h.SetManager(o.Mia, o.Vera)
	h.SetManager(o.Max, o.Vera)
	h.SetManager(o.Pia, o.Mia)
	h.SetManager(o.Pax, o.Max)
	o.VeraGoal = h.ActiveGoal(o.Vera, "Vera's goal", "It matters.")
	o.MiaGoal = h.ActiveGoal(o.Mia, "Mia's goal", "It matters.")
	o.MaxGoal = h.ActiveGoal(o.Max, "Max's goal", "It matters.")
	o.PiaGoal = h.ActiveGoal(o.Pia, "Pia's goal", "It matters.")
	o.PaxGoal = h.ActiveGoal(o.Pax, "Pax's goal", "It matters.")
	o.OzGoal = h.ActiveGoal(o.Oz, "Oz's goal", "It matters.")
	return o
}

// goals is every Goal in the fixture, Oz's included.
func (o scopeOrgFixture) goals() []domain.Goal {
	return []domain.Goal{o.VeraGoal, o.MiaGoal, o.MaxGoal, o.PiaGoal, o.PaxGoal, o.OzGoal}
}

// scopeOrgURL is path scoped to who's Org (?org=).
func scopeOrgURL(base, path string, who domain.Account) string {
	return base + path + "?org=" + strconv.FormatInt(who.ID, 10)
}

// scopeSelect returns the page's one Scope select (CONTEXT.md: Org), failing
// the test unless the page has exactly one control labelled Scope and it is
// the select carrying testID.
func scopeSelect(t *testing.T, page, testID string) string {
	t.Helper()
	scopeSelectIn(t, parsePage(t, page), testID)
	return pageElement(t, page, "select", testID)
}

// scopeSelectIn is scopeSelect read from a parsed page: the page's one Scope
// select, failing the test unless the page has exactly one control labelled
// Scope and it is the select carrying testID.
func scopeSelectIn(t *testing.T, root *html.Node, testID string) *html.Node {
	t.Helper()
	labelled := findAll(root, func(n *html.Node) bool { return attrOf(n, "aria-label") == "Scope" })
	if len(labelled) != 1 {
		t.Fatalf("page has %d controls labelled Scope, want 1", len(labelled))
	}
	if n := labelled[0]; n.Data != "select" || attrOf(n, "data-testid") != testID {
		t.Fatalf("the control labelled Scope isn't the %s select: %s", testID, renderNode(n))
	}
	return labelled[0]
}

// selectChoices reads a parsed select, or any element holding one, such as a
// Scope select (CONTEXT.md: Org): its options in order, each by its label,
// those in a group led by the group's label ("Other orgs: Mia Moss
// (mia@example.com)"), and the value and label of the one selected, if any.
func selectChoices(sel *html.Node) (choices []string, selectedValue, selectedLabel string) {
	for n := range sel.Descendants() {
		if n.Type != html.ElementNode || n.Data != "option" {
			continue
		}
		label, group := textOf(n), ""
		if p := n.Parent; p != nil && p.Data == "optgroup" {
			group = attrOf(p, "label") + ": "
		}
		choices = append(choices, group+label)
		if hasAttr(n, "selected") {
			selectedValue, selectedLabel = attrOf(n, "value"), label
		}
	}
	return choices, selectedValue, selectedLabel
}

// newPasswordServer serves the app with password sign-in.
func newPasswordServer(t *testing.T, h *testsupport.Harness) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(web.NewServer(h.Service, web.WithPasswordSignIn()))
	t.Cleanup(ts.Close)
	return ts
}

// passwordLinkToken issues a set-password link for emailAddr and returns its
// token, failing the test on error.
func passwordLinkToken(t *testing.T, h *testsupport.Harness, emailAddr string) string {
	t.Helper()
	link, err := h.Service.IssueOperatorPasswordLink(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("IssueOperatorPasswordLink(%q): %v", emailAddr, err)
	}
	return link.Token
}

// givePassword sets emailAddr's password through a fresh set-password link,
// failing the test on error.
func givePassword(t *testing.T, h *testsupport.Harness, emailAddr, password string) {
	t.Helper()
	if _, err := h.Service.SetPassword(t.Context(), passwordLinkToken(t, h, emailAddr), password); err != nil {
		t.Fatalf("SetPassword(%q): %v", emailAddr, err)
	}
}

// passwordSignInClient returns an HTTP client with its own cookie jar, signed
// in with emailAddr's password, which it first sets through a set-password
// link.
func passwordSignInClient(t *testing.T, h *testsupport.Harness, baseURL, emailAddr string) *http.Client {
	t.Helper()
	const password = "a password for tests"
	givePassword(t, h, emailAddr, password)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	resp := postForm(t, client, baseURL+"/signin", url.Values{"email": {emailAddr}, "password": {password}})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Fatalf("password sign-in as %q: status %d at %s:\n%s", emailAddr, resp.StatusCode, resp.Request.URL.Path, body)
	}
	return client
}

// assertTermHelp checks that within holds a help disclosure for a glossary
// term: a disclose help button reading label, starting collapsed, carrying
// text as its title, and controlling a hidden explain paragraph on page that
// reads text. It returns the button's opening tag. It finds the button by
// html's tokenizer and the paragraph by its id in the parsed page, so their
// attributes may come in any order.
func assertTermHelp(t *testing.T, page, within, label, text string) string {
	t.Helper()
	button := termHelpButton(within, label)
	if button == "" {
		t.Errorf("no button reads %q", label)
		return ""
	}
	if !slices.Contains(strings.Fields(attr(button, "class")), "disclose") || !slices.Contains(strings.Fields(attr(button, "class")), "help") {
		t.Errorf("%q is not a disclose help button: %s", label, button)
	}
	if attr(button, "type") != "button" || attr(button, "aria-expanded") != "false" {
		t.Errorf("%q button does not start collapsed: %s", label, button)
	}
	if got, want := attr(button, "title"), html.EscapeString(text); got != want {
		t.Errorf("%q button's title = %q, want %q", label, got, want)
	}
	id := attr(button, "aria-controls")
	explains := findAll(parsePage(t, page), func(n *html.Node) bool { return id != "" && attrOf(n, "id") == id })
	if len(explains) != 1 || !termHelpExplains(explains[0], text) {
		t.Errorf("%q button controls %q, want the collapsed explanation, a hidden explain paragraph reading %q", label, id, text)
	}
	return button
}

// termHelpButton is the opening tag of the first button in markup whose last
// words, just before it closes, read label, or "" when none does.
func termHelpButton(markup, label string) string {
	z := html.NewTokenizer(strings.NewReader(markup))
	button, labelled := "", false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return ""
		}
		raw := string(z.Raw())
		tok := z.Token()
		switch {
		case tt == html.StartTagToken && tok.Data == "button":
			button, labelled = raw, false
		case tt == html.TextToken:
			labelled = tok.Data == label
		case tt == html.EndTagToken && tok.Data == "button" && labelled && button != "":
			return button
		default:
			labelled = false
		}
	}
}

// termHelpExplains reports whether n is a hidden explain paragraph reading
// text.
func termHelpExplains(n *html.Node, text string) bool {
	return n.Data == "p" && slices.Contains(strings.Fields(attrOf(n, "class")), "explain") && isHiddenElement(n) &&
		runningText(n) == strings.Join(strings.Fields(text), " ")
}

// assertInFlatFrame checks page sits in the flat frame (DESIGN.md § Page
// header, § Components: Sections): its header strip's h1 is title,
// and nothing on it is a card. It reads classes from the parsed page, so a
// class merely named in the page's text, or one only ending in "card", isn't
// a card.
func assertInFlatFrame(t *testing.T, page, title string) {
	t.Helper()
	doc := parsePage(t, page)
	headers := findAll(doc, func(n *html.Node) bool { return flatFrameHasClass(n, "page-header") })
	if len(headers) == 0 {
		t.Errorf("the %s page has no header strip:\n%s", title, page)
	} else if h1s := findAll(headers[0], func(n *html.Node) bool { return n.Data == "h1" }); len(h1s) == 0 || textOf(h1s[0]) != title {
		t.Errorf("the %s page's header strip doesn't hold its title as its h1:\n%s", title, renderNode(headers[0]))
	}
	if cards := findAll(doc, func(n *html.Node) bool { return flatFrameHasClass(n, "card") }); len(cards) > 0 {
		t.Errorf("the %s page still has a card, class=%q:\n%s", title, attrOf(cards[0], "class"), page)
	}
}

// flatFrameHasClass reports whether n's class attribute names class on its
// own, not as part of another class's name.
func flatFrameHasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attrOf(n, "class")), class)
}

// The helpers below read a page as a person meets it, through a parsed DOM:
// by test ID, by the text it reads, by a form field's name and label, by a
// link's address and a button's text. An assertion made through them holds
// whatever the markup around it, so renaming a class or wrapping a section in
// a <div> breaks no test. Style rules are tested in styles_test.go.

// parsePage parses a page, or a fragment of one such as an htmx re-render, to
// query with the helpers below.
func parsePage(t *testing.T, page string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	return doc
}

// getPage fetches rawURL, failing the test unless it answers 200, and parses
// the page.
func getPage(t *testing.T, client *http.Client, rawURL string) *html.Node {
	t.Helper()
	return parsePage(t, getBody(t, client, rawURL))
}

// findAll returns every element under root that match reports, root
// included, in document order.
func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	if root.Type == html.ElementNode && match(root) {
		out = append(out, root)
	}
	for n := range root.Descendants() {
		if n.Type == html.ElementNode && match(n) {
			out = append(out, n)
		}
	}
	return out
}

// findAllByTestID returns every element under root carrying data-testid, in
// document order.
func findAllByTestID(root *html.Node, testID string) []*html.Node {
	return findAll(root, func(n *html.Node) bool { return attrOf(n, "data-testid") == testID })
}

// hasTestID reports whether any element under root carries data-testid.
func hasTestID(root *html.Node, testID string) bool {
	return len(findAllByTestID(root, testID)) > 0
}

// findByTestID returns the one element under root carrying data-testid,
// failing the test unless there is exactly one.
func findByTestID(t *testing.T, root *html.Node, testID string) *html.Node {
	t.Helper()
	found := findAllByTestID(root, testID)
	if len(found) != 1 {
		t.Fatalf("page has %d elements with test ID %q, want 1:\n%s", len(found), testID, renderNode(root))
	}
	return found[0]
}

// attrOf is the value of n's named attribute, or "".
func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// hasAttr reports whether n carries the named attribute, whatever its value.
func hasAttr(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

// isHidden reports whether n is out of sight: it or an element around it
// carries the hidden attribute.
func isHidden(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && hasAttr(p, "hidden") {
			return true
		}
	}
	return false
}

// textOf is what n reads: its text with the markup dropped, each element's
// text set apart from the next by a space, whitespace collapsed and entities
// decoded. A select reads its chosen option. Script, styles, templates and
// hidden elements inside n read nothing.
func textOf(n *html.Node) string {
	return strings.Join(nodeWords(n, n, isHiddenElement), " ")
}

// textsOf is what each of nodes reads (textOf), in order.
func textsOf(nodes []*html.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, textOf(n))
	}
	return out
}

// runningText is n's shown text as a browser runs it together: its text nodes
// joined as they stand, with no space added between elements, whitespace
// collapsed. It tells "Mia Chen Answer" from "Mia ChenAnswer", which textOf
// reads alike.
func runningText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		switch {
		case c.Type == html.TextNode:
			b.WriteString(c.Data)
			return
		case c.Type == html.ElementNode && c != n && isHiddenElement(c):
			return
		case c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style" || c.Data == "template"):
			return
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// shownTextOf is what n shows a sighted reader: what it reads (textOf) less
// its screen-reader text, the elements marked sr-only.
func shownTextOf(n *html.Node) string {
	return strings.Join(nodeWords(n, n, func(c *html.Node) bool {
		return isHiddenElement(c) || flatFrameHasClass(c, "sr-only")
	}), " ")
}

// assertHealthChip fails unless chip is a Health chip as the screen draws it:
// its shape and colour with no word shown, the word read out to a screen
// reader and given as its tooltip (DESIGN.md § Health).
func assertHealthChip(t *testing.T, chip *html.Node, health string) {
	t.Helper()
	if shown, read, tip := shownTextOf(chip), textOf(chip), attrOf(chip, "title"); shown != "" || read != health || tip != health {
		t.Errorf("Health chip shows %q, reads %q and is titled %q, want nothing shown, and %q read and titled: %s", shown, read, tip, health, renderNode(chip))
	}
	if len(findAll(chip, func(n *html.Node) bool { return flatFrameHasClass(n, "dot") })) != 1 {
		t.Errorf("Health chip doesn't draw its shape: %s", renderNode(chip))
	}
}

// milestoneMarkTint is the tint each Milestone mark is shown in: Overdue in
// Red's and Slipped in Yellow's, though neither is a Health, and the rest
// neutral (DESIGN.md § Pages: Goal page sections, Milestones).
var milestoneMarkTint = map[string]string{"Overdue": "r", "Slipped": "y", "Done": "lc", "Removed": "lc", "New": "lc"}

// assertMilestoneMarksShowTheirWords fails unless the Milestone marks under
// root read want, in order, each showing its word in its tint with no Health
// shape drawn: a mark says what it means, unlike a Health chip (#461).
func assertMilestoneMarksShowTheirWords(t *testing.T, root *html.Node, want []string) {
	t.Helper()
	marks := findAllByTestID(root, "milestone-mark")
	if got := textsOf(marks); !slices.Equal(got, want) {
		t.Errorf("Milestone marks read %q, want %q", got, want)
	}
	for _, mark := range marks {
		word := textOf(mark)
		if shown := shownTextOf(mark); shown != word {
			t.Errorf("Milestone mark %q shows %q, want its word shown: %s", word, shown, renderNode(mark))
		}
		if tint, ok := milestoneMarkTint[word]; !ok || !flatFrameHasClass(mark, "badge") || !flatFrameHasClass(mark, tint) {
			t.Errorf("Milestone mark %q is not a badge in tint %q: %s", word, tint, renderNode(mark))
		}
		if dots := findAll(mark, func(n *html.Node) bool { return flatFrameHasClass(n, "dot") }); len(dots) != 0 {
			t.Errorf("Milestone mark %q draws a Health shape: %s", word, renderNode(mark))
		}
	}
}

// healthChipsIn returns each Health badge under root, Red, Yellow or Green,
// in document order.
func healthChipsIn(root *html.Node) []*html.Node {
	return findAll(root, func(n *html.Node) bool {
		return flatFrameHasClass(n, "badge") && (flatFrameHasClass(n, "r") || flatFrameHasClass(n, "y") || flatFrameHasClass(n, "g"))
	})
}

// healthChipIn returns the one Health badge under root, failing the test
// unless there is exactly one.
func healthChipIn(t *testing.T, root *html.Node) *html.Node {
	t.Helper()
	chips := healthChipsIn(root)
	if len(chips) != 1 {
		t.Fatalf("found %d Health badges, want 1:\n%s", len(chips), renderNode(root))
	}
	return chips[0]
}

// struckThrough is what each date or word under root shown struck through
// reads, in order: a <del> or <s>, as an earlier date that slipped shows.
func struckThrough(root *html.Node) []string {
	return textsOf(findAll(root, func(n *html.Node) bool { return n.Data == "del" || n.Data == "s" }))
}

// isHiddenElement reports whether n itself carries the hidden attribute.
func isHiddenElement(n *html.Node) bool { return hasAttr(n, "hidden") }

// nodeWords returns the words n reads, as textOf reads them, skipping the
// elements under root that skip reports.
func nodeWords(root, n *html.Node, skip func(*html.Node) bool) []string {
	var words []string
	switch n.Type {
	case html.TextNode:
		return strings.Fields(n.Data)
	case html.ElementNode:
		if n != root && skip != nil && skip(n) {
			return nil
		}
		switch n.Data {
		case "script", "style", "template":
			return nil
		case "select":
			if opt := chosenOption(n); opt != nil {
				return nodeWords(root, opt, skip)
			}
			return nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		words = append(words, nodeWords(root, c, skip)...)
	}
	return words
}

// chosenOption is the option a select shows: the one selected, else its first.
func chosenOption(sel *html.Node) *html.Node {
	opts := findAll(sel, func(n *html.Node) bool { return n.Data == "option" })
	for _, o := range opts {
		if hasAttr(o, "selected") {
			return o
		}
	}
	if len(opts) > 0 {
		return opts[0]
	}
	return nil
}

// pageField is a form field as a person meets it.
type pageField struct {
	Node *html.Node
	// Type is an input's type ("text" when it names none), or "select" or
	// "textarea".
	Type string
	// Value is what the field submits as it stands: an input's value, a
	// textarea's text, or a select's chosen option's value.
	Value string
	// Checked reports a checkbox or radio ticked.
	Checked     bool
	Placeholder string
	// Label is what the field is called: its aria-label, else the text of
	// the label around it or naming it, without the fields inside.
	Label string
	// Description is the text of what its aria-describedby names.
	Description string
}

// formFields returns every field under root named name, in document order.
func formFields(root *html.Node, name string) []pageField {
	var out []pageField
	for _, n := range findAll(root, func(n *html.Node) bool {
		return (n.Data == "input" || n.Data == "select" || n.Data == "textarea") && attrOf(n, "name") == name
	}) {
		f := pageField{Node: n, Type: n.Data, Checked: hasAttr(n, "checked"), Placeholder: attrOf(n, "placeholder")}
		switch n.Data {
		case "input":
			f.Type, f.Value = cmp.Or(attrOf(n, "type"), "text"), attrOf(n, "value")
		case "textarea":
			// The parser keeps a textarea's text as typed, a leading newline
			// dropped as a browser drops it.
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f.Value += c.Data
			}
		case "select":
			if opt := chosenOption(n); opt != nil {
				f.Value = attrOf(opt, "value")
			}
		}
		f.Label = fieldLabel(n)
		for _, id := range strings.Fields(attrOf(n, "aria-describedby")) {
			for _, d := range findAll(topOf(n), func(m *html.Node) bool { return attrOf(m, "id") == id }) {
				f.Description = strings.TrimSpace(f.Description + " " + textOf(d))
			}
		}
		out = append(out, f)
	}
	return out
}

// formSubmission is what a browser posts from form as it stands when
// submitted without a named button: each named field's value, a checkbox or
// radio only when ticked, buttons and disabled fields left out.
func formSubmission(form *html.Node) url.Values {
	values := url.Values{}
	seen := map[string]bool{}
	for _, n := range findAll(form, func(n *html.Node) bool {
		return (n.Data == "input" || n.Data == "select" || n.Data == "textarea") && attrOf(n, "name") != ""
	}) {
		name := attrOf(n, "name")
		if seen[name] {
			continue
		}
		seen[name] = true
		for _, f := range formFields(form, name) {
			switch {
			case hasAttr(f.Node, "disabled"),
				f.Type == "submit", f.Type == "button", f.Type == "image", f.Type == "reset",
				(f.Type == "checkbox" || f.Type == "radio") && !f.Checked:
				continue
			case (f.Type == "checkbox" || f.Type == "radio") && !hasAttr(f.Node, "value"):
				values.Add(name, "on")
			default:
				values.Add(name, f.Value)
			}
		}
	}
	return values
}

// formField returns the one field under root named name, failing the test
// unless there is exactly one.
func formField(t *testing.T, root *html.Node, name string) pageField {
	t.Helper()
	fields := formFields(root, name)
	if len(fields) != 1 {
		t.Fatalf("page has %d fields named %q, want 1:\n%s", len(fields), name, renderNode(root))
	}
	return fields[0]
}

// checkedRadio is the label of the radio named name ticked under root, the
// choice a person sees picked, or "" when none is.
func checkedRadio(root *html.Node, name string) string {
	for _, f := range formFields(root, name) {
		if f.Type == "radio" && f.Checked {
			return f.Label
		}
	}
	return ""
}

// fieldLabel is what a field is called (pageField.Label).
func fieldLabel(n *html.Node) string {
	if label := attrOf(n, "aria-label"); label != "" {
		return label
	}
	skip := func(m *html.Node) bool {
		return isHiddenElement(m) || m.Data == "input" || m.Data == "select" || m.Data == "textarea" || m.Data == "button"
	}
	labelText := func(l *html.Node) string { return strings.Join(nodeWords(l, l, skip), " ") }
	if id := attrOf(n, "id"); id != "" {
		for _, l := range findAll(topOf(n), func(m *html.Node) bool { return m.Data == "label" && attrOf(m, "for") == id }) {
			return labelText(l)
		}
	}
	for p := range n.Ancestors() {
		if p.Type == html.ElementNode && p.Data == "label" {
			return labelText(p)
		}
	}
	return ""
}

// topOf is the root of the tree n sits in.
func topOf(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// linkTo returns the first link under root to href, or nil.
func linkTo(root *html.Node, href string) *html.Node {
	if links := findAll(root, func(n *html.Node) bool { return n.Data == "a" && attrOf(n, "href") == href }); len(links) > 0 {
		return links[0]
	}
	return nil
}

// buttonNamed returns the first button under root reading text, or nil.
func buttonNamed(root *html.Node, text string) *html.Node {
	if buttons := findAll(root, func(n *html.Node) bool { return n.Data == "button" && textOf(n) == text }); len(buttons) > 0 {
		return buttons[0]
	}
	return nil
}

// submitButtons returns the buttons under root that submit their form, in
// document order.
func submitButtons(root *html.Node) []*html.Node {
	return findAll(root, func(n *html.Node) bool {
		return n.Data == "button" && cmp.Or(attrOf(n, "type"), "submit") == "submit" ||
			n.Data == "input" && attrOf(n, "type") == "submit"
	})
}

// headingNamed returns the first heading, h1 to h6, under root reading text,
// or nil.
func headingNamed(root *html.Node, text string) *html.Node {
	if found := findAll(root, func(n *html.Node) bool {
		return len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' && textOf(n) == text
	}); len(found) > 0 {
		return found[0]
	}
	return nil
}

// assertRefusalPage checks page is a refused action's page inside the app:
// heading names what was refused, its one refused-reason says why, as reason,
// and its Back links to back.
func assertRefusalPage(t *testing.T, page *html.Node, heading, reason, back string) {
	t.Helper()
	if !hasTestID(page, "nav-home") {
		t.Errorf("refusal page is not inside the app's layout:\n%s", renderNode(page))
	}
	refusal := findByTestID(t, page, "refusal")
	if headingNamed(refusal, heading) == nil {
		t.Errorf("refusal page has no heading %q:\n%s", heading, renderNode(page))
	}
	if got := textOf(findByTestID(t, refusal, "refused-reason")); got != reason {
		t.Errorf("refused-reason = %q, want %q", got, reason)
	}
	if link := linkTo(refusal, back); link == nil || textOf(link) != "Back" {
		t.Errorf("refusal page has no Back link to %s:\n%s", back, renderNode(page))
	}
}

// precedes reports whether a comes before b reading the page top to bottom:
// a opens first.
func precedes(a, b *html.Node) bool {
	for n := range topOf(a).Descendants() {
		switch n {
		case a:
			return true
		case b:
			return false
		}
	}
	return false
}

// within reports whether n sits inside root, or is root.
func within(n, root *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p == root {
			return true
		}
	}
	return false
}

// renderNode is n's markup, for a failure message.
func renderNode(n *html.Node) string {
	var b strings.Builder
	if err := html.Render(&b, n); err != nil {
		return fmt.Sprintf("<unrenderable: %v>", err)
	}
	return b.String()
}

// postImport uploads a spreadsheet and returns the rendered body, failing on a
// non-2xx status.
func postImport(t *testing.T, client *http.Client, rawURL, filename, content, action string) string {
	t.Helper()
	resp := postImportRaw(t, client, rawURL, filename, content, action)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d", rawURL, resp.StatusCode)
	}
	return readBody(t, resp)
}

// postImportRaw uploads a spreadsheet as multipart form data with the given
// action, returning the raw response.
func postImportRaw(t *testing.T, client *http.Client, rawURL, filename, content, action string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.WriteField("action", action); err != nil {
		t.Fatalf("write action field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	resp, err := ctxPost(t, client, rawURL, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}

// The string helpers below read a page's markup as a string, for the tests in
// other areas that still call them (drafthighlights_test.go, goalform_test.go,
// handoffs_test.go, links_test.go, reports_test.go, styles_test.go,
// suggestions_test.go). goals_test.go and home_test.go read their pages
// through the parser helpers instead.

// homeRow returns the row of a Home section that links to g, failing the test
// when the section has none.
func homeRow(t *testing.T, section string, g domain.Goal) string {
	t.Helper()
	at := strings.Index(section, navTo(g.ID))
	if at < 0 {
		t.Fatalf("section has no row for %q:\n%s", g.Title, section)
	}
	start := strings.LastIndex(section[:at], "<li")
	end := strings.Index(section[at:], "</li>")
	return section[start : at+end]
}

// hasClass reports whether an open tag's class attribute holds class.
func hasClass(openTag, class string) bool {
	_, rest, ok := strings.Cut(openTag, `class="`)
	if !ok {
		return false
	}
	list, _, _ := strings.Cut(rest, `"`)
	return slices.Contains(strings.Fields(list), class)
}

// shownPlainAs is how a page shows a person inside another control, where a
// second control can't nest: by label, with their email on hover only.
func shownPlainAs(emailAddr, label string) string {
	return `<span class="person" title="` + emailAddr + `">` + label + `</span>`
}

// interactiveTag matches the open and close tags of the elements a control
// can't nest inside.
var interactiveTag = regexp.MustCompile(`<(/?)(a|button|label|summary)[\s>]`)

// nestedControls returns every interactive element a page opens inside
// another one, which HTML forbids.
func nestedControls(page string) []string {
	var nested []string
	depth := 0
	for _, at := range interactiveTag.FindAllStringSubmatchIndex(page, -1) {
		if at[3] > at[2] {
			depth = max(depth-1, 0)
			continue
		}
		if depth > 0 {
			tag, _, _ := strings.Cut(page[at[0]:], ">")
			nested = append(nested, tag+">")
		}
		depth++
	}
	return nested
}

// activationItems reads the activation checklist on a Proposed Goal's page as
// each item's label mapped to whether it is done. Each item reads as a mark,
// its label, and "done" or "to do", so it never relies on the mark alone: ✓
// for done and a neutral ○ for to do.
func activationItems(t *testing.T, page string) map[string]bool {
	t.Helper()
	list := pageElement(t, page, "ul", "activation-checklist")
	items := map[string]bool{}
	for _, li := range strings.Split(list, "<li ")[1:] {
		mark, label, state := activationItemParts(t, li)
		done := strings.Contains(openTag(li), `data-done="true"`)
		if want := map[bool]string{true: "done", false: "to do"}[done]; state != want {
			t.Errorf("checklist item says %q against data-done=%v, want %q: %s", state, done, want, li)
		}
		if want := map[bool]string{true: "✓", false: "○"}[done]; mark != want {
			t.Errorf("checklist item is marked %q against data-done=%v, want %q: %s", mark, done, want, li)
		}
		items[label] = done
	}
	return items
}

// activationItemParts splits an activation checklist item's text into its
// mark, its label, and its trailing state, "done" or the two words "to do".
func activationItemParts(t *testing.T, li string) (mark, label, state string) {
	t.Helper()
	words := strings.Fields(regexpTags.ReplaceAllString(li[strings.Index(li, ">")+1:], " "))
	stateWords := 1
	if n := len(words); n >= 2 && words[n-2] == "to" && words[n-1] == "do" {
		stateWords = 2
	}
	if len(words) < 2+stateWords {
		t.Fatalf("checklist item has no mark, label and state: %s", li)
	}
	n := len(words) - stateWords
	return words[0], strings.Join(words[1:n], " "), strings.Join(words[n:], " ")
}

var regexpTags = regexp.MustCompile(`<[^>]*>`)

// actionMenu returns the Goal page header strip's action menu: from its
// <details> to the end of the strip's actions, where it sits last.
func actionMenu(t *testing.T, page string) string {
	t.Helper()
	return between(t, between(t, page, `data-testid="goal-actions"`, `data-testid="goal-main"`), `data-testid="goal-more"`, "")
}

// menuLink matches one of the action menu's items: a plain link and its label.
var menuLink = regexp.MustCompile(`<a [^>]*href="([^"]*)"[^>]*>([^<]*)</a>`)

// menuItems returns the action menu's items, label to link, failing when the
// menu holds anything but links: no form, control or nested disclosure.
func menuItems(t *testing.T, page string) map[string]string {
	t.Helper()
	list := between(t, actionMenu(t, page), "<ul", "</ul>")
	for _, control := range []string{"<form", "<input", "<button", "<select", "<textarea", "<details"} {
		if strings.Contains(list, control) {
			t.Errorf("the action menu holds a %s>, not only links:\n%s", control, list)
		}
	}
	items := map[string]string{}
	for _, m := range menuLink.FindAllStringSubmatch(list, -1) {
		items[html.UnescapeString(m[2])] = html.UnescapeString(m[1])
	}
	if strings.Count(list, "<li") != len(items) {
		t.Errorf("an action menu item is not a plain link:\n%s", list)
	}
	return items
}

// assertOpenIn checks the open form sits in the page's block from the element
// marked start up to the one marked next, the part of the page it concerns.
func assertOpenIn(t *testing.T, page, form, start, next string) {
	t.Helper()
	at := strings.Index(page, `data-open-form="`+form+`"`)
	from, to := strings.Index(page, start), strings.Index(page, next)
	if from < 0 || to < 0 || at < from || at > to {
		t.Errorf("%q does not open between %s and %s", form, start, next)
	}
}

// The markup helpers read a tag whatever order its attributes render in, and
// past a > inside a quoted attribute value (#437).
func TestMarkupHelpersOpenTagEndsAtTheTagNotAQuotedGreaterThan(t *testing.T) {
	t.Parallel()
	const element = `<a title="Due > Today" href="/goals/1">Plan</a>`
	if got, want := openTag(element), `<a title="Due > Today" href="/goals/1"`; got != want {
		t.Errorf("openTag = %q, want %q", got, want)
	}
}

func TestMarkupHelpersTagAroundHoldsAQuotedGreaterThanInAnyAttributeOrder(t *testing.T) {
	t.Parallel()
	const want = `<a href="/goals/1" title="Due > Today">`
	page := `<p class="plan">` + want + `Plan</a></p>`
	for _, marker := range []string{`href="/goals/1"`, `title="Due`, `>Plan</a>`} {
		if got := tagAround(t, page, marker); got != want {
			t.Errorf("tagAround(%q) = %q, want %q", marker, got, want)
		}
	}
}

func TestMarkupHelpersAttrReadsTheAttributeNotTextLikeIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ tag, name, want string }{
		{`<a title='see href="/wrong"' href="/right">`, "href", "/right"},
		{openTag(`<a href="/right" title="Due > Today">Plan</a>`), "title", "Due > Today"},
		{`<a data-name="other" name="plan">`, "name", "plan"},
		{`<a title="Plan &amp; Do &#39;now&#39;">`, "title", "Plan &amp; Do &#39;now&#39;"},
		{`<input hidden name="plan">`, "hidden", ""},
		{`<input name="plan">`, "value", ""},
	} {
		if got := attr(tc.tag, tc.name); got != tc.want {
			t.Errorf("attr(%s, %q) = %q, want %q", tc.tag, tc.name, got, tc.want)
		}
	}
}

func TestMarkupHelpersOpenFormRunsFromItsWrapperToItsCancelLink(t *testing.T) {
	t.Parallel()
	const want = `<section aria-label="Hand off > Owner" data-open-form="handoff"><form method="post"><button>Start</button></form>` +
		`<a href="/goals/1" title="Back > Goal" data-testid="cancel-form">`
	page := `<p>Open a form with data-open-form="handoff".</p>` + want + `Cancel</a></section>`
	if n := openForms(page); n != 1 {
		t.Errorf("openForms = %d, want 1", n)
	}
	if got := openForm(t, page, "handoff"); got != want {
		t.Errorf("openForm = %q, want %q", got, want)
	}
}

func TestMarkupHelpersScopeSelectCountsControlsNotTextLikeThem(t *testing.T) {
	t.Parallel()
	const sel = `<select title="Org > Team" aria-label="Scope" data-testid="scope"><option value="all">Everyone</option>`
	page := `<script>document.querySelector('[aria-label="Scope"]')</script>` + sel + `</select>`
	if got := scopeSelect(t, page, "scope"); got != sel {
		t.Errorf("scopeSelect = %q, want %q", got, sel)
	}
}

func TestMarkupHelpersAssertSubmitsAtOnceReadsHandlersNotTextLikeThem(t *testing.T) {
	t.Parallel()
	page := `<form method="post" onsubmit="return confirm('Sure?')" action="/links/1/accept"><button type="submit">Accept</button></form>` +
		`<form method="post" title="No onsubmit= or confirm( here" action="/links/1/reject">` +
		`<button type="submit" title="No hx-confirm= or onclick=confirm( here">Reject</button></form>` +
		`<button type="button" onclick="confirm('Sure?')">Help</button>`
	assertSubmitsAtOnce(t, "Reject", page, "/links/1/reject")
}

// Each child run fails the helper on purpose; the parent passes only if it did.
func TestMarkupHelpersAssertSubmitsAtOnceFailsAConfirmDialog(t *testing.T) {
	t.Parallel()
	const asks = "Reject asks for confirmation"
	for _, tc := range []struct{ name, form, want string }{
		{"hx-on on the form", `<form method="post" hx-on:htmx:confirm="if (!confirm('Sure?')) event.preventDefault()" action="/links/1/reject"><button type="submit">Reject</button></form>`, asks},
		{"hx-on on the button", `<form method="post" action="/links/1/reject"><button type="submit" hx-on:click="if (!confirm('Sure?')) event.preventDefault()">Reject</button></form>`, asks},
		{"onclick on the button", `<form method="post" action="/links/1/reject"><span><button type="submit" onclick="return confirm('Sure?')">Reject</button></span></form>`, asks},
		{"hx-confirm on the form", `<form method="post" hx-confirm="Sure?" action="/links/1/reject"><button type="submit">Reject</button></form>`, asks},
		{"data-hx-confirm on the form", `<form method="post" data-hx-confirm="Sure?" action="/links/1/reject"><button type="submit">Reject</button></form>`, asks},
		{"data-hx-on on the button", `<form method="post" action="/links/1/reject"><button type="submit" data-hx-on:click="if (!confirm('Sure?')) event.preventDefault()">Reject</button></form>`, asks},
		{"marker text in another form's title", `<form title='action="/links/1/reject"' action="/x"></form><form method="post" hx-confirm="Sure?" action="/links/1/reject"><button type="submit">Reject</button></form>`, asks},
		{"unclosed form", `<form method="post" action="/links/1/reject"><button type="submit">Reject</button>`, "no </form> closes the form"},
		{"no form", `<a href="/x" data-action="/links/1/reject" action="/links/1/reject">Reject</a>`, "no form's action ends in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if os.Getenv("ASSERT_SUBMITS_AT_ONCE_CHILD") == "1" {
				assertSubmitsAtOnce(t, "Reject", `<p>Reject</p>`+tc.form, "/links/1/reject")
				return
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
			cmd.Env = append(os.Environ(), "ASSERT_SUBMITS_AT_ONCE_CHILD=1")
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Errorf("assertSubmitsAtOnce passed without %q: %s\n%s", tc.want, tc.form, out)
			}
		})
	}
}

func TestMarkupHelpersAssertStyledByReadsTheStyleAttributeNotTextLikeIt(t *testing.T) {
	t.Parallel()
	assertStyledBy(t, `<form title="Keep a style=x off" class="inline-form" action="/handoffs/1/accept">`, `.inline-form{display:inline}`, "inline-form", "display:inline")
}

func TestMarkupHelpersAssertTermHelpReadsTheDisclosureInAnyAttributeOrder(t *testing.T) {
	t.Parallel()
	const text = "A Dated Goal has a delivery date."
	within := `<span data-testid="goal-kind"><button aria-controls="kind-help" title="` + text + `" aria-expanded="false" class="help disclose" type="button">Dated</button></span>`
	page := `<p>Dated</p>` + within + `<p hidden class="explain" id="kind-help">` + text + `</p>`
	if button := assertTermHelp(t, page, within, "Dated", text); attr(button, "aria-controls") != "kind-help" {
		t.Errorf("assertTermHelp = %q, want the Dated button's opening tag", button)
	}
}

func TestMarkupHelpersAssertInFlatFrameReadsClassesNotTextLikeThem(t *testing.T) {
	t.Parallel()
	page := `<main><div data-testid="header" class="page-header"><h1>Plan &amp; Do</h1></div>` +
		`<p class="scorecard">Cards, once class="card", are gone.</p></main>`
	assertInFlatFrame(t, page, "Plan & Do")
}
