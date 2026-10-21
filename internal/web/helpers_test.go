package web_test

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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
	if n := strings.Count(page, `aria-label="Scope"`); n != 1 {
		t.Fatalf("page has %d controls labelled Scope, want 1", n)
	}
	sel := pageElement(t, page, "select", testID)
	if !strings.Contains(openTag(sel), `aria-label="Scope"`) {
		t.Fatalf("the %s select isn't labelled Scope: %s", testID, openTag(sel))
	}
	return sel
}

// scopeChoices reads a Scope select's options in order, each by its label,
// those in a group led by the group's label ("Other orgs: Mia Moss
// (mia@example.com)"), and the value and label of the one selected, if any.
func scopeChoices(sel string) (choices []string, selectedValue, selectedLabel string) {
	doc, err := html.Parse(strings.NewReader(sel))
	if err != nil {
		return nil, "", ""
	}
	for n := range doc.Descendants() {
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
	token, _, err := h.Service.IssuePasswordLink(t.Context(), emailAddr)
	if err != nil {
		t.Fatalf("IssuePasswordLink(%q): %v", emailAddr, err)
	}
	return token
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
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/home" {
		t.Fatalf("password sign-in as %q: status %d at %s:\n%s", emailAddr, resp.StatusCode, resp.Request.URL.Path, body)
	}
	return client
}

// assertTermHelp checks that within holds a help disclosure for a glossary
// term: a disclose help button reading label, starting collapsed, carrying
// text as its title, and controlling a hidden explain paragraph on page that
// holds text. It returns the button's opening tag.
func assertTermHelp(t *testing.T, page, within, label, text string) string {
	t.Helper()
	end := strings.Index(within, ">"+label+"</button>")
	if end < 0 {
		t.Errorf("no button reads %q", label)
		return ""
	}
	start := strings.LastIndex(within[:end], "<button")
	if start < 0 {
		t.Errorf("%q is not inside a button", label)
		return ""
	}
	button := within[start:end]
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
	if want := `<p id="` + id + `" class="explain" hidden>` + html.EscapeString(text) + `</p>`; id == "" || !strings.Contains(page, want) {
		t.Errorf("%q button controls %q, want the collapsed explanation %s", label, id, want)
	}
	return button
}

// cardClass matches a class attribute holding the card class on its own, not
// as part of another class's name.
var cardClass = regexp.MustCompile(`class="(?:[^"]* )?card(?: [^"]*)?"`)

// assertInFlatFrame checks page sits in the flat frame (DESIGN.md § Page
// header, § Components: Sections): its header strip's h1 is title,
// and nothing on it is a card.
func assertInFlatFrame(t *testing.T, page, title string) {
	t.Helper()
	if !strings.Contains(page, `<div class="page-header">`) {
		t.Errorf("the %s page has no header strip:\n%s", title, page)
	} else if header := between(t, page, `<div class="page-header">`, "</h1>"); !strings.HasSuffix(header, "<h1>"+html.EscapeString(title)) {
		t.Errorf("the %s page's header strip doesn't hold its title as its h1:\n%s", title, header)
	}
	if c := cardClass.FindString(page); c != "" {
		t.Errorf("the %s page still has a card, %s:\n%s", title, c, page)
	}
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
	resp, err := client.Post(rawURL, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", rawURL, err)
	}
	return resp
}
