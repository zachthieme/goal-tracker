package web_test

import (
	"context"
	"fmt"
	"net/http"
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

// A reader comments on a Goal's block from the publication page, the Owner is
// emailed, and the Owner replies in the thread (ticket #19).
func TestSmokeCommentAndReplyOnPublicationOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)

	ts := newServer(t, h)
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	reader := signInClient(t, ts.URL, "reader@example.com")

	page := getBody(t, reader, pubURL)
	if form := pageElement(t, page, "form", "comment-form"); !strings.Contains(form, `value="`+strconv.FormatInt(g.ID, 10)+`"`) {
		t.Fatalf("publication offers no comment form for the Goal; form:\n%s", form)
	}
	resp := postForm(t, reader, pubURL+"/comments", url.Values{
		"goal": {strconv.FormatInt(g.ID, 10)},
		"body": {"Why did the vendor slip?"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != strings.TrimPrefix(pubURL, ts.URL) {
		t.Fatalf("comment: status %d at %s, want back on the publication", resp.StatusCode, resp.Request.URL)
	}
	if thread := pageElement(t, readBody(t, resp), "div", "comment-thread"); !strings.Contains(thread, "Why did the vendor slip?") ||
		!strings.Contains(thread, "reader@example.com") {
		t.Errorf("publication does not show the comment and its author; thread:\n%s", thread)
	} else if !strings.Contains(thread, " · Jan 2, 2026, 15:04") {
		t.Errorf("the comment's time doesn't read Jan 2, 2026, 15:04, with the year as a Report shows it; thread:\n%s", thread)
	}
	if sent := h.Email.Sent(); len(sent) != 1 || sent[0].To != "owner@example.com" {
		t.Errorf("emails %+v, want one comment alert to the Owner", sent)
	}

	threads, err := h.Service.ListThreads(context.Background(), pub.ID)
	if err != nil || len(threads) != 1 {
		t.Fatalf("ListThreads: %+v, %v", threads, err)
	}
	ownerClient := signInClient(t, ts.URL, "owner@example.com")
	resp = postForm(t, ownerClient, fmt.Sprintf("%s/comments/%d/replies", ts.URL, threads[0].Comment.ID), url.Values{
		"body": {"Their factory flooded."},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reply: status %d", resp.StatusCode)
	}
	if thread := pageElement(t, readBody(t, resp), "div", "comment-thread"); !strings.Contains(thread, "Their factory flooded.") {
		t.Errorf("thread does not show the Owner's reply; thread:\n%s", thread)
	}

	resp = postForm(t, reader, pubURL+"/comments", url.Values{"goal": {strconv.FormatInt(g.ID, 10)}, "body": {" "}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("blank comment: status %d, want 422", resp.StatusCode)
	}
}

// The author turns a comment into an Action Item from the publication page; it
// appears at the top of the next publication until its owner closes it there
// with a note (CONTEXT.md: Action Item).
func TestSmokeActionItemCarriesUntilClosedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	first := h.PublishReport(author, def)
	question, err := h.Service.AddComment(context.Background(), owner.ID, first.ID, g.ID, "Get a second vendor quote.")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	ts := newServer(t, h)
	reportURL := fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)
	firstURL := fmt.Sprintf("%s/publications/%d", reportURL, first.ID)
	authorClient := signInClient(t, ts.URL, "author@example.com")
	ownerClient := signInClient(t, ts.URL, "owner@example.com")

	if page := getBody(t, ownerClient, firstURL); strings.Contains(page, `data-testid="comment-action-item-form"`) {
		t.Errorf("a reader who is not the author is offered to raise Action Items")
	}
	pageElement(t, getBody(t, authorClient, firstURL), "form", "comment-action-item-form")
	resp := postForm(t, authorClient, firstURL+"/action-items", url.Values{
		"comment": {strconv.FormatInt(question.ID, 10)},
		"owner":   {"owner@example.com"},
		"due":     {"2026-02-01"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raise action item: status %d", resp.StatusCode)
	}
	if item := pageElement(t, readBody(t, resp), "li", "raised-action-item"); !strings.Contains(item, "Get a second vendor quote.") ||
		!strings.Contains(item, "owner@example.com") || !strings.Contains(item, "due Feb 1, 2026") {
		t.Errorf("publication does not list the raised Action Item; item:\n%s", item)
	}
	resp = postForm(t, ownerClient, firstURL+"/action-items", url.Values{
		"text": {"Sneaky"}, "owner": {"owner@example.com"}, "due": {"2026-02-01"},
	})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("raised by someone who is not the author: status %d, want 403", resp.StatusCode)
	}

	resp = postForm(t, authorClient, reportURL+"/publications", url.Values{})
	secondURL := resp.Request.URL.String()
	if item := pageElement(t, readBody(t, resp), "li", "open-action-item"); !strings.Contains(item, "Get a second vendor quote.") {
		t.Errorf("next publication does not carry the open Action Item at its top; item:\n%s", item)
	}

	page := getBody(t, ownerClient, secondURL)
	form := pageElement(t, page, "form", "close-action-item")
	_, action, _ := strings.Cut(form, `action="`)
	action, _, _ = strings.Cut(action, `"`)
	resp = postForm(t, ownerClient, ts.URL+action, url.Values{
		"note":   {"Quote is in: 20% cheaper."},
		"return": {strings.TrimPrefix(secondURL, ts.URL)},
	})
	if resp.StatusCode != http.StatusOK || resp.Request.URL.String() != secondURL {
		t.Fatalf("close: status %d at %s, want back on %s", resp.StatusCode, resp.Request.URL, secondURL)
	}
	page = readBody(t, resp)
	if strings.Contains(page, `data-testid="close-action-item"`) {
		t.Errorf("a closed Action Item still offers the close form")
	}
	// The publication stays as frozen, but says the item has closed since.
	if item := pageElement(t, page, "li", "open-action-item"); !strings.Contains(item, "Get a second vendor quote.") ||
		!strings.Contains(item, `data-testid="action-item-closed-since"`) {
		t.Errorf("a frozen Action Item closed since publication is not marked so; item:\n%s", item)
	}
	if item := pageElement(t, getBody(t, ownerClient, firstURL), "li", "raised-action-item"); !strings.Contains(item, "Quote is in: 20% cheaper.") ||
		!strings.Contains(item, "[Closed Jan 2, 2026]") {
		t.Errorf("the publication it was raised on does not show the closing note; item:\n%s", item)
	}

	resp = postForm(t, authorClient, reportURL+"/publications", url.Values{})
	if page := readBody(t, resp); strings.Contains(page, `data-testid="open-action-item"`) {
		t.Errorf("publication after closing still carries the Action Item")
	}
}

// The Report's author names the Goal an Action Item raised directly is about,
// from the Goals the publication covers, or leaves it about the whole Report;
// the publication then says which Goal it is about (CONTEXT.md: Action Item).
func TestDirectActionItemNamesAGoalOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	v2 := h.ActiveGoal(author, "Ship v2", "Win back churned accounts.")
	eu := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{v2.ID, eu.ID}})
	pub := h.PublishReport(author, def)
	if _, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, v2.ID, "Is pricing settled?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	ts := newServer(t, h)
	pubURL := fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)
	client := signInClient(t, ts.URL, "author@example.com")

	page := getBody(t, client, pubURL)
	form := pageElement(t, page, "form", "action-item-form")
	sel := pageElement(t, form, "select", "action-item-goal")
	if attr(openTag(sel), "name") != "goal" {
		t.Errorf("the Goal select isn't named goal: %s", openTag(sel))
	}
	var options []string
	for _, m := range regexp.MustCompile(`(<option[^>]*>)([^<]*)</option>`).FindAllStringSubmatch(sel, -1) {
		option := attr(m[1], "value") + " " + m[2]
		if regexp.MustCompile(`\sselected[\s/>]`).MatchString(m[1]) {
			option += " (selected)"
		}
		options = append(options, option)
	}
	want := []string{
		"0 The whole Report (selected)",
		fmt.Sprintf("%d Launch in EU", eu.ID),
		fmt.Sprintf("%d Ship v2", v2.ID),
	}
	if !slices.Equal(options, want) {
		t.Errorf("Goal options = %q, want %q", options, want)
	}
	if strings.Contains(pageElement(t, page, "form", "comment-action-item-form"), `name="goal"`) {
		t.Errorf("the form raising an Action Item from a comment asks for a Goal")
	}

	resp := postForm(t, client, pubURL+"/action-items", url.Values{
		"text": {"Book the EU legal review."}, "goal": {strconv.FormatInt(eu.ID, 10)}, "owner": {"author@example.com"}, "due": {"2026-02-01"},
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raise action item: status %d", resp.StatusCode)
	}
	if item := elementTexts(readBody(t, resp), "li", "raised-action-item"); len(item) != 1 || !strings.Contains(item[0], "due Feb 1, 2026 · about Launch in EU") {
		t.Errorf("raised Action Items %q, want the one about Launch in EU", item)
	}

	postRefusedBack(t, client, pubURL+"/action-items", url.Values{
		"text": {"Book the EU legal review."}, "goal": {"Launch in EU"}, "owner": {"author@example.com"}, "due": {"2026-02-01"},
	}, strings.TrimPrefix(pubURL, ts.URL))
	if items, err := h.Service.PublicationActionItems(context.Background(), pub.ID); err != nil || len(items) != 1 {
		t.Errorf("Action Items = %+v (%v), want only the one about Launch in EU", items, err)
	}
}

// An Action Item due on a day that isn't a date, about a Goal that isn't a
// Goal's id, comes back to the publication's form with both reasons, each
// beside its field, quoting what was typed, and what else was typed kept; no
// Action Item is raised (#660).
func TestActionItemDueAndGoalThatDontParseComeBackToTheirFieldsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	v2 := h.ActiveGoal(author, "Ship v2", "Win back churned accounts.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{v2.ID}})
	pub := h.PublishReport(author, def)
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	client := signInClient(t, ts.URL, "author@example.com")

	postRefusedBack(t, client, ts.URL+pubPath+"/action-items", url.Values{
		"text": {"Book the EU legal review."}, "owner": {"author@example.com"}, "due": {"someday"}, "goal": {"Launch in EU"},
	}, pubPath)

	form := findByTestID(t, getPage(t, client, ts.URL+pubPath), "action-item-form")
	for name, want := range map[string]string{"due": `"someday" isn't a date.`, "goal": "The Goal must be a Goal's id."} {
		if f := formField(t, form, name); attrOf(f.Node, "aria-invalid") != "true" || f.Description != want {
			t.Errorf("%s is aria-invalid %q, described %q; want it invalid, described %q", name, attrOf(f.Node, "aria-invalid"), f.Description, want)
		}
	}
	for name, want := range map[string]string{"text": "Book the EU legal review.", "owner": "author@example.com"} {
		if got := formField(t, form, name).Value; got != want {
			t.Errorf("%s holds %q, want %q as posted", name, got, want)
		}
	}
	if items, err := h.Service.PublicationActionItems(context.Background(), pub.ID); err != nil || len(items) != 0 {
		t.Errorf("Action Items = %+v (%v), want none raised", items, err)
	}
}

// Closing an Action Item comes back only to a page on this site: a return
// address that a browser would read as another site is ignored.
func TestCloseActionItemNeverRedirectsOffSite(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(owner, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	for _, back := range []string{"//evil.example", "/\t/evil.example", "/\\evil.example", "https://evil.example/", "/\n/evil.example"} {
		item, err := h.Service.RaiseActionItem(context.Background(), owner.ID, domain.RaiseActionItemInput{
			PublicationID: pub.ID, Text: "Follow up.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
		})
		if err != nil {
			t.Fatalf("RaiseActionItem: %v", err)
		}
		resp := postForm(t, client, fmt.Sprintf("%s/action-items/%d/close", ts.URL, item.ID), url.Values{
			"note": {"Done."}, "return": {back},
		})
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		want := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("return %q: redirected to %q, want %q", back, got, want)
		}
	}
}

// A form's return is the page it sends someone back to, but only when it is
// a path on this site: anything a browser could read as another site, or that
// isn't a path at all, answers the route's fallback instead.
func TestReturnToAcceptsOnlyAPathOnThisSite(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		back, want string
	}{
		{"/goals/5", "/goals/5"},
		{"/follow-ups?org=all", "/follow-ups?org=all"},
		{"/home", "/home"},
		{"", "/fallback"},
		{"goals", "/fallback"},
		{"//evil.example", "/fallback"},
		{`/\evil.example`, "/fallback"},
		{"https://evil.example/x", "/fallback"},
		{"/goals/5\n", "/fallback"},
		{"/goals/5\t", "/fallback"},
		{"/a\x7f", "/fallback"},
	} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/anywhere",
			strings.NewReader(url.Values{"return": {tc.back}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := web.ReturnTo(r, "/fallback"); got != tc.want {
			t.Errorf("return %q: answered %q, want %q", tc.back, got, tc.want)
		}
	}
}

// Under each Goal the discussion is a footer row: the comment count opens the
// threads, a Comment button opens the form, and the author's form to make a
// comment an Action Item sits behind its own toggle — all collapsed until the
// reader opens them, without JS. Replies are indented under the comment.
func TestPublicationDiscussionIsCollapsedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	question, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := h.Service.ReplyToComment(context.Background(), owner.ID, question.ID, "Their factory flooded."); err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "author@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))

	footer := pageElement(t, page, "footer", "goal-discussion")
	if strings.Contains(footer, "<details open") {
		t.Errorf("discussion opens something by default; footer:\n%s", footer)
	}
	if count := pageElement(t, footer, "summary", "comment-count"); !strings.HasSuffix(count, ">2 comments") {
		t.Errorf("comment count %q, want 2 comments", count)
	}
	if toggle := pageElement(t, footer, "details", "comment-toggle"); !strings.Contains(toggle, `data-testid="comment-form"`) {
		t.Errorf("the comment form is not behind the Comment button; toggle:\n%s", toggle)
	}
	if toggle := pageElement(t, footer, "details", "make-action-item"); !strings.Contains(toggle, `data-testid="comment-action-item-form"`) {
		t.Errorf("Make Action Item is not behind its own toggle; toggle:\n%s", toggle)
	}
	if toggle := pageElement(t, footer, "details", "reply-toggle"); !strings.Contains(toggle, `data-testid="reply-form"`) {
		t.Errorf("the reply form is not behind a toggle; toggle:\n%s", toggle)
	}
	if _, reply, ok := strings.Cut(footer, `<p data-testid="comment" class="rp-reply">`); !ok || !strings.Contains(reply, "Their factory flooded.") {
		t.Errorf("the reply is not indented under the comment; footer:\n%s", footer)
	}
}

// A comment thread is set apart by its indent and a 1px neutral rule, not a
// coloured side stripe (#67).
func TestCommentThreadIsSetApartByANeutralRuleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	if _, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, g.ID, "Why did the vendor slip?"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "author@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))

	pageElement(t, page, "div", "comment-thread")
	assertNeutralRule(t, cssRule(t, page, ".rp-thread"))
}

// A publication lists its open Action Items as text · owner · due date; the
// owner's close form sits behind a small Close button until they open it.
func TestOpenActionItemCloseFormIsCollapsedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	first := h.PublishReport(owner, def)
	if _, err := h.Service.RaiseActionItem(context.Background(), owner.ID, domain.RaiseActionItemInput{
		PublicationID: first.ID, Text: "Get a second vendor quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	second := h.PublishReport(owner, def)

	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "owner@example.com"), fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, second.ID))

	item := pageElement(t, page, "li", "open-action-item")
	if want := "Get a second vendor quote. · " + shownAs("owner@example.com", "owner") + " · due " + h.Clock.Now().Format("Jan 2, 2006"); !strings.Contains(item, want) {
		t.Errorf("open Action Item does not read %q; item:\n%s", want, item)
	}
	toggle := pageElement(t, item, "details", "close-toggle")
	if strings.HasPrefix(toggle, "<details data-testid=\"close-toggle\" open") || !strings.Contains(toggle, `data-testid="close-action-item"`) {
		t.Errorf("the close form is not behind a collapsed toggle; toggle:\n%s", toggle)
	}
}

// The Report's author reassigns an open Action Item from the publication it
// was raised on; the item then names its new owner and who had it before.
// Its owner, who isn't an author, isn't offered to, and can't (CONTEXT.md:
// Action Item).
func TestAuthorReassignsAnActionItemOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignInNamed("owner@example.com", "Olive Owner")
	author := h.SignIn("author@example.com")
	h.SignInNamed("newbie@example.com", "Nina Newbie")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Book the EU legal review.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	authorClient := signInClient(t, ts.URL, "author@example.com")
	ownerClient := signInClient(t, ts.URL, "owner@example.com")
	reassignURL := fmt.Sprintf("%s/action-items/%d/reassign", ts.URL, item.ID)

	row := pageElement(t, getBody(t, authorClient, ts.URL+pubPath), "li", "raised-action-item")
	toggle := pageElement(t, row, "details", "reassign-toggle")
	form := pageElement(t, toggle, "form", "reassign-action-item")
	if !strings.Contains(toggle, `<summary class="btn sm">Reassign</summary>`) || !strings.Contains(form, "New owner") ||
		!strings.Contains(form, `type="email" name="owner"`) || attr(openTag(form), "action") != fmt.Sprintf("/action-items/%d/reassign", item.ID) {
		t.Errorf("the author's reassign toggle isn't a Reassign button over a New owner form; toggle:\n%s", toggle)
	}
	if row := pageElement(t, getBody(t, ownerClient, ts.URL+pubPath), "li", "raised-action-item"); strings.Contains(row, "reassign-toggle") {
		t.Errorf("its owner, who isn't an author, is offered to reassign it; row:\n%s", row)
	}

	resp := postForm(t, ownerClient, reassignURL, url.Values{"owner": {"newbie@example.com"}, "return": {pubPath}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("reassigned by its owner: status %d, want 403", resp.StatusCode)
	}

	back := pubPath + "#action-items"
	resp = postForm(t, authorClient, reassignURL, url.Values{"owner": {"newbie@example.com"}, "return": {back}})
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != pubPath {
		t.Fatalf("reassign: status %d at %s, want back on %s", resp.StatusCode, resp.Request.URL, pubPath)
	}
	row = pageElement(t, readBody(t, resp), "li", "raised-action-item")
	if !strings.Contains(row, "Book the EU legal review. · "+shownAs("newbie@example.com", "Nina Newbie")) {
		t.Errorf("the reassigned item doesn't name its new owner; row:\n%s", row)
	}
	_, afterOwner, _ := strings.Cut(row, shownAs("newbie@example.com", "Nina Newbie"))
	if want := `<span data-testid="action-item-was">(was ` + shownAs("owner@example.com", "Olive Owner") + `&#39;s)</span>`; !strings.Contains(afterOwner, want) {
		t.Errorf("the reassigned item doesn't say who had it before, after its owner; row:\n%s", row)
	}

	postRefusedBack(t, authorClient, reassignURL, url.Values{"owner": {"nobody@example.com"}, "return": {pubPath}}, pubPath)
	resp = postForm(t, authorClient, fmt.Sprintf("%s/action-items/%d/reassign", ts.URL, item.ID+100), url.Values{"owner": {"owner@example.com"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("reassigning a missing item: status %d, want 404", resp.StatusCode)
	}
}

// Reassigning an Action Item comes back only to a page on this site, as
// closing one does.
func TestReassignActionItemNeverRedirectsOffSite(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	first := h.SignIn("first@example.com")
	second := h.SignIn("second@example.com")
	g := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Follow up.", OwnerID: first.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	client := noRedirects(signInClient(t, ts.URL, "author@example.com"))

	for i, back := range []string{"//evil.example", "/\t/evil.example", "/\\evil.example", "https://evil.example/", "/\n/evil.example"} {
		to := []domain.Account{second, first}[i%2]
		resp := postForm(t, client, fmt.Sprintf("%s/action-items/%d/reassign", ts.URL, item.ID), url.Values{
			"owner": {to.Email}, "return": {back},
		})
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		want := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("return %q: status %d to %q, want 303 to %q", back, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
}

// A later publication lists a carried Action Item as it froze, with the owner
// it had then, but its forms follow the live item: after a reassignment the
// new owner gets the close form and the old owner doesn't.
func TestFrozenActionItemFollowsItsReassignmentOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	owner := h.SignIn("owner@example.com")
	h.SignIn("newbie@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	first := h.PublishReport(author, def)
	if _, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: first.ID, Text: "Book the EU legal review.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	}); err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	second := h.PublishReport(author, def)
	ts := newServer(t, h)
	secondPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, second.ID)
	authorClient := signInClient(t, ts.URL, "author@example.com")
	ownerClient := signInClient(t, ts.URL, "owner@example.com")
	newbieClient := signInClient(t, ts.URL, "newbie@example.com")

	row := pageElement(t, getBody(t, authorClient, ts.URL+secondPath), "li", "open-action-item")
	form := pageElement(t, pageElement(t, row, "details", "reassign-toggle"), "form", "reassign-action-item")
	if strings.Contains(row, "close-toggle") {
		t.Errorf("the author, who doesn't own the carried item, is offered to close it; row:\n%s", row)
	}
	resp := postForm(t, authorClient, ts.URL+attr(openTag(form), "action"), url.Values{"owner": {"newbie@example.com"}, "return": {secondPath}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != secondPath {
		t.Fatalf("reassign: status %d at %s, want back on %s", resp.StatusCode, resp.Request.URL, secondPath)
	}
	if row := pageElement(t, readBody(t, resp), "li", "open-action-item"); !strings.Contains(row, `title="owner@example.com"`) ||
		strings.Contains(row, "action-item-closed-since") {
		t.Errorf("the frozen row doesn't still show its frozen owner, open; row:\n%s", row)
	}

	if row := pageElement(t, getBody(t, newbieClient, ts.URL+secondPath), "li", "open-action-item"); !strings.Contains(row, `data-testid="close-toggle"`) {
		t.Errorf("the new owner isn't offered to close the carried item; row:\n%s", row)
	}
	if row := pageElement(t, getBody(t, ownerClient, ts.URL+secondPath), "li", "open-action-item"); strings.Contains(row, "close-toggle") {
		t.Errorf("the old owner is still offered to close the carried item; row:\n%s", row)
	}
}

// Anyone signed in comments on a Goal from its page, the Owner is emailed,
// and a reply lands in the same thread there; the Report's discussion doesn't
// show it (ticket #350).
func TestSmokeCommentAndReplyOnTheGoalPageOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)

	ts := newServer(t, h)
	goalURL := goalPageURL(ts.URL, g)
	wantBack := fmt.Sprintf("/goals/%d#comments", g.ID)
	reader := signInClient(t, ts.URL, "reader@example.com")

	page := getBody(t, reader, goalURL)
	block := pageElement(t, page, "section", "goal-comments")
	if !strings.Contains(openTag(block), `id="comments"`) {
		t.Errorf("Comments block is not the #comments anchor: %s", openTag(block))
	}
	if c, hist := strings.Index(page, `data-testid="goal-comments"`), strings.Index(page, `data-testid="goal-history"`); c > hist {
		t.Errorf("Comments block sits below History, want it above")
	}
	if !strings.Contains(block, `data-testid="no-goal-comments"`) {
		t.Errorf("Goal with no Comments doesn't say so; block:\n%s", block)
	}
	if form := pageElement(t, block, "form", "goal-comment-form"); !strings.Contains(form, fmt.Sprintf(`action="/goals/%d/comments"`, g.ID)) ||
		!strings.Contains(form, `name="body"`) {
		t.Errorf("comment form doesn't post a body to the Goal; form:\n%s", form)
	}

	resp := postForm(t, noRedirects(reader), goalURL+"/comments", url.Values{"body": {"Is the EU date still real?"}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != wantBack {
		t.Fatalf("comment: status %d to %q, want 303 to %q", resp.StatusCode, resp.Header.Get("Location"), wantBack)
	}
	block = pageElement(t, getBody(t, reader, goalURL), "section", "goal-comments")
	thread := pageElement(t, block, "div", "comment-thread")
	if !strings.Contains(thread, "Is the EU date still real?") || !strings.Contains(thread, "reader@example.com") {
		t.Errorf("Goal page doesn't show the comment and its author; thread:\n%s", thread)
	}
	if strings.Contains(block, `data-testid="make-action-item"`) || strings.Contains(block, `data-testid="no-goal-comments"`) {
		t.Errorf("Comments block offers an Action Item or still says there are none; block:\n%s", block)
	}
	if sent := h.Email.Sent(); len(sent) != 1 || sent[0].To != "owner@example.com" {
		t.Errorf("emails %+v, want one comment alert to the Owner", sent)
	}

	threads, err := h.Service.GoalThreads(context.Background(), g.ID)
	if err != nil || len(threads) != 1 {
		t.Fatalf("GoalThreads: %+v, %v", threads, err)
	}
	if !strings.Contains(thread, fmt.Sprintf(`action="/comments/%d/replies"`, threads[0].Comment.ID)) {
		t.Errorf("thread has no reply form; thread:\n%s", thread)
	}
	ownerClient := signInClient(t, ts.URL, "owner@example.com")
	resp = postForm(t, noRedirects(ownerClient), fmt.Sprintf("%s/comments/%d/replies", ts.URL, threads[0].Comment.ID), url.Values{
		"body": {"Yes, as of today."},
	})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != wantBack {
		t.Fatalf("reply: status %d to %q, want 303 to %q", resp.StatusCode, resp.Header.Get("Location"), wantBack)
	}
	block = pageElement(t, getBody(t, reader, goalURL), "section", "goal-comments")
	if got := strings.Count(block, `data-testid="comment-thread"`); got != 1 {
		t.Errorf("Comments block has %d threads, want the one", got)
	}
	if thread := pageElement(t, block, "div", "comment-thread"); !strings.Contains(thread, "Yes, as of today.") {
		t.Errorf("thread doesn't show the Owner's reply; thread:\n%s", thread)
	}

	pubPage := getBody(t, reader, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID))
	if discussion := pageElement(t, pubPage, "footer", "goal-discussion"); strings.Contains(discussion, "Is the EU date still real?") {
		t.Errorf("the Report's discussion shows the Goal's own comment; discussion:\n%s", discussion)
	}
}

// A comment on a Goal refused by the domain is a plain error status, as on a
// Report: blank is 422, an unknown or non-numeric Goal 404.
func TestGoalCommentRefusalsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")

	for _, tc := range []struct {
		name, path, body string
		want             int
	}{
		{"blank", fmt.Sprintf("/goals/%d/comments", g.ID), "  ", http.StatusUnprocessableEntity},
		{"unknown Goal", fmt.Sprintf("/goals/%d/comments", g.ID+100), "Why?", http.StatusNotFound},
		{"non-numeric id", "/goals/eu/comments", "Why?", http.StatusNotFound},
	} {
		resp := postForm(t, noRedirects(client), ts.URL+tc.path, url.Values{"body": {tc.body}})
		defer func() { _ = resp.Body.Close() }()
		_ = readBody(t, resp)
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
}

// A reply to a Report's comment still comes back to its publication.
func TestReplyToAReportCommentReturnsToThePublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	question, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	ts := newServer(t, h)
	client := noRedirects(signInClient(t, ts.URL, "owner@example.com"))

	resp := postForm(t, client, fmt.Sprintf("%s/comments/%d/replies", ts.URL, question.ID), url.Values{"body": {"Their factory flooded."}})
	defer func() { _ = resp.Body.Close() }()
	_ = readBody(t, resp)
	want := fmt.Sprintf("/reports/%d/publications/%d#goal-%d", def.ID, pub.ID, g.ID)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
		t.Errorf("reply: status %d to %q, want 303 to %q", resp.StatusCode, resp.Header.Get("Location"), want)
	}
}

// A refused Comment answers with the refusal page, headed Can't comment, and
// an Action Item refused for who is asking with one headed by what was
// refused, Can't add, close or reassign Action Item (#660). Each is at its
// refusal's status, linking Back to the page the form posted as its return,
// when that is a page of the app's own, or else the page the Comment or
// Action Item is on: the Goal page for one on the Goal, the publication's
// page for one on a publication (#573).
func TestARefusedDiscussionActionAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	h.SignIn("other@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	onPub, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	onGoal, err := h.Service.AddGoalComment(context.Background(), author.ID, g.ID, "Who is the vendor?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Get a second quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	goalPath := fmt.Sprintf("/goals/%d", g.ID)
	draftPath := fmt.Sprintf("/reports/%d", def.ID)

	for _, c := range []struct {
		name, who, path       string
		form                  url.Values
		status                int
		heading, reason, back string
	}{
		{"a blank comment on a publication", "owner@example.com", pubPath + "/comments", url.Values{"goal": {fmt.Sprint(g.ID)}, "body": {" "}},
			http.StatusUnprocessableEntity, "Can't comment", "A comment cannot be blank.", pubPath},
		{"an Action Item raised by someone not the author", "owner@example.com", pubPath + "/action-items", url.Values{"text": {"Sneaky"}, "owner": {"owner@example.com"}, "due": {"2026-02-01"}},
			http.StatusForbidden, "Can't add Action Item", "Only the Report's author raises Action Items.", pubPath},
		{"an Action Item for an unknown email raised by someone not the author", "owner@example.com", pubPath + "/action-items", url.Values{"text": {"Sneaky"}, "owner": {"nobody@example.com"}, "due": {"2026-02-01"}},
			http.StatusForbidden, "Can't add Action Item", "Only the Report's author raises Action Items.", pubPath},
		{"a blank comment on the Goal", "owner@example.com", goalPath + "/comments", url.Values{"body": {" "}},
			http.StatusUnprocessableEntity, "Can't comment", "A comment cannot be blank.", goalPath},
		{"a blank reply on the Goal", "owner@example.com", fmt.Sprintf("/comments/%d/replies", onGoal.ID), url.Values{"body": {" "}},
			http.StatusUnprocessableEntity, "Can't comment", "A reply cannot be blank.", goalPath},
		{"a blank reply on a publication", "owner@example.com", fmt.Sprintf("/comments/%d/replies", onPub.ID), url.Values{"body": {" "}},
			http.StatusUnprocessableEntity, "Can't comment", "A reply cannot be blank.", pubPath},
		{"a close by someone not its owner, from the draft", "other@example.com", fmt.Sprintf("/action-items/%d/close", item.ID), url.Values{"note": {"Done."}, "return": {draftPath}},
			http.StatusForbidden, "Can't close Action Item", "Only the Action Item's owner closes it.", draftPath},
		{"a reassign by someone who may not, from the draft", "other@example.com", fmt.Sprintf("/action-items/%d/reassign", item.ID), url.Values{"owner": {"other@example.com"}, "return": {draftPath}},
			http.StatusForbidden, "Can't reassign Action Item", "Only the Report's creator, a Report Delegate or its publisher, an Admin, or the creator's Manager reassigns an Action Item.", draftPath},
	} {
		resp := postForm(t, noRedirects(signInClient(t, ts.URL, c.who)), ts.URL+c.path, c.form)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		assertRefusalPage(t, page, c.heading, c.reason, c.back)
	}
	if items, err := h.Service.PublicationActionItems(context.Background(), pub.ID); err != nil || len(items) != 1 {
		t.Errorf("publication's Action Items %+v (err %v), want only the one raised before", items, err)
	}
}

// A reply, an Action Item's close or reassign, or a comment on a Goal, that
// names a Comment, Action Item or Goal that isn't there answers with the Not
// found page (#573).
func TestADiscussionActionOnAMissingRecordIsNotFound(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	h.SignIn("owner@example.com")
	ts := newServer(t, h)
	client := noRedirects(signInClient(t, ts.URL, "owner@example.com"))

	for _, path := range []string{"/comments/99999/replies", "/action-items/99999/close", "/action-items/99999/reassign", "/goals/99999/comments"} {
		resp := postForm(t, client, ts.URL+path, url.Values{"body": {"Why?"}, "note": {"Done."}, "owner": {"owner@example.com"}})
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
		if !hasTestID(page, "not-found") {
			t.Errorf("%s: not the Not found page:\n%s", path, renderNode(page))
		}
	}
}

// detailsAround is the <details> nearest around n, or nil.
func detailsAround(n *html.Node) *html.Node {
	for p := range n.Ancestors() {
		if p.Type == html.ElementNode && p.Data == "details" {
			return p
		}
	}
	return nil
}

// Adding an Action Item for an email no one has comes back to the
// publication with its form open, holding what was typed, and the reason
// beside Owner; a reload shows the page plainly, and nothing is raised (#660).
func TestARefusedAddActionItemComesBackToItsFormOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	client := signInClient(t, ts.URL, "author@example.com")

	posted := url.Values{
		"text": {"Book the EU legal review."}, "owner": {"nobody@example.com"}, "due": {"2026-02-01"}, "goal": {strconv.FormatInt(g.ID, 10)},
	}
	postRefusedBack(t, client, ts.URL+pubPath+"/action-items", posted, pubPath)

	page := getPage(t, client, ts.URL+pubPath)
	if !hasTestID(page, "report-name") || hasTestID(page, "refusal") {
		t.Fatalf("not the publication page:\n%s", renderNode(page))
	}
	form := findByTestID(t, page, "action-item-form")
	if d := detailsAround(form); d == nil || !hasAttr(d, "open") {
		t.Errorf("the Add Action Item form isn't open:\n%s", renderNode(form))
	}
	for _, name := range []string{"text", "owner", "due", "goal"} {
		if got := formField(t, form, name).Value; got != posted.Get(name) {
			t.Errorf("%s holds %q, want %q as posted", name, got, posted.Get(name))
		}
	}
	owner := formField(t, form, "owner")
	if attrOf(owner.Node, "aria-invalid") != "true" || !strings.HasPrefix(owner.Description, `No account with email "nobody@example.com"`) {
		t.Errorf("owner is aria-invalid %q, described %q; want it invalid, saying no account has the email", attrOf(owner.Node, "aria-invalid"), owner.Description)
	}

	page = getPage(t, client, ts.URL+pubPath)
	if d := detailsAround(findByTestID(t, page, "action-item-form")); d == nil || hasAttr(d, "open") || hasTestID(page, "form-error") {
		t.Errorf("a reload still shows the refused form open, or its error:\n%s", renderNode(page))
	}
	if items, err := h.Service.PublicationActionItems(context.Background(), pub.ID); err != nil || len(items) != 0 {
		t.Errorf("Action Items = %+v (%v), want none raised", items, err)
	}
}

// A Close with a blank note comes back to the page it was posted from — Home,
// the Goal page, the draft or the publication, or the publication when its
// return isn't a page on this site — with that item's one Close open there,
// and the reason beside its note. A reload shows it closed, and the item stays
// open (#660).
func TestARefusedCloseComesBackToItsFormOnEachPageOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, GoalID: g.ID, Text: "Get a second quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	closePath := fmt.Sprintf("/action-items/%d/close", item.ID)

	for _, c := range []struct{ name, posted, back string }{
		{"Home", "/home", "/home"},
		{"the Goal page", fmt.Sprintf("/goals/%d#action-items", g.ID), fmt.Sprintf("/goals/%d#action-items", g.ID)},
		{"the draft", fmt.Sprintf("/reports/%d", def.ID), fmt.Sprintf("/reports/%d", def.ID)},
		{"the publication", pubPath, pubPath},
		{"a return off the site", "//evil.example", pubPath},
	} {
		postRefusedBack(t, client, ts.URL+closePath, url.Values{"note": {" "}, "return": {c.posted}}, c.back)
		page := getPage(t, client, ts.URL+c.back)
		open := openDetails(page, "close-toggle")
		if len(open) != 1 {
			t.Errorf("%s: %d Close toggles open, want 1:\n%s", c.name, len(open), renderNode(page))
			continue
		}
		form := findByTestID(t, open[0], "close-action-item")
		if attrOf(form, "action") != closePath {
			t.Errorf("%s: the open Close is %q, want the item's %q", c.name, attrOf(form, "action"), closePath)
		}
		if note := formField(t, form, "note"); attrOf(note.Node, "aria-invalid") != "true" || note.Description != "Closing an Action Item needs a note." {
			t.Errorf("%s: note is aria-invalid %q, described %q; want it invalid, needing a note", c.name, attrOf(note.Node, "aria-invalid"), note.Description)
		}
		if page := getPage(t, client, ts.URL+c.back); len(openDetails(page, "close-toggle")) != 0 || hasTestID(page, "form-error") {
			t.Errorf("%s: a reload still shows the refused Close open, or its error", c.name)
		}
	}
	if open, err := h.Service.OpenActionItems(context.Background(), def.ID); err != nil || len(open) != 1 {
		t.Errorf("open Action Items %+v (err %v), want the item still open", open, err)
	}
}

// Making a comment an Action Item for an email no one has comes back to the
// publication with that comment's form open, in its open thread, holding the
// owner typed and saying why beside it; no other comment's form opens (#660).
func TestARefusedMakeActionItemComesBackToItsCommentOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	eu := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	v2 := h.ActiveGoal(author, "Ship v2", "Win back churned accounts.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID, v2.ID}})
	pub := h.PublishReport(author, def)
	var comments []domain.Comment
	for _, g := range []domain.Goal{eu, eu, v2} {
		c, err := h.Service.AddComment(context.Background(), author.ID, pub.ID, g.ID, "Why did the vendor slip?")
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		comments = append(comments, c)
	}
	refused := comments[1]
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
	client := signInClient(t, ts.URL, "author@example.com")

	postRefusedBack(t, client, ts.URL+pubPath+"/action-items", url.Values{
		"comment": {strconv.FormatInt(refused.ID, 10)}, "text": {""}, "owner": {"nobody@example.com"}, "due": {"2026-02-01"},
	}, pubPath)

	page := getPage(t, client, ts.URL+pubPath)
	open := openDetails(page, "make-action-item")
	if len(open) != 1 {
		t.Fatalf("%d Make action item toggles open, want 1:\n%s", len(open), renderNode(page))
	}
	form := findByTestID(t, open[0], "comment-action-item-form")
	if got := formField(t, form, "comment").Value; got != strconv.FormatInt(refused.ID, 10) {
		t.Errorf("the open form makes comment %s an Action Item, want %d", got, refused.ID)
	}
	if thread := detailsAround(open[0]); thread == nil || !hasAttr(thread, "open") {
		t.Errorf("the thread holding the comment isn't open")
	}
	owner := formField(t, form, "owner")
	if owner.Value != "nobody@example.com" || attrOf(owner.Node, "aria-invalid") != "true" || !strings.HasPrefix(owner.Description, `No account with email "nobody@example.com"`) {
		t.Errorf("owner holds %q, aria-invalid %q, described %q; want it kept, invalid, saying no account has it", owner.Value, attrOf(owner.Node, "aria-invalid"), owner.Description)
	}
	if errors := findAllByTestID(page, "form-error"); len(errors) != 1 {
		t.Errorf("page has %d form errors, want 1", len(errors))
	}
}

// Reassigning an Action Item to an email no one has comes back to the draft
// or the publication it was posted from, with its Reassign open, holding the
// owner typed and saying why beside it (#660).
func TestARefusedReassignComesBackToItsFormOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Get a second quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "author@example.com")
	reassignPath := fmt.Sprintf("/action-items/%d/reassign", item.ID)

	for _, back := range []string{fmt.Sprintf("/reports/%d", def.ID), fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)} {
		postRefusedBack(t, client, ts.URL+reassignPath, url.Values{"owner": {"nobody@example.com"}, "return": {back}}, back)
		page := getPage(t, client, ts.URL+back)
		open := openDetails(page, "reassign-toggle")
		if len(open) != 1 {
			t.Errorf("%s: %d Reassign toggles open, want 1:\n%s", back, len(open), renderNode(page))
			continue
		}
		newOwner := formField(t, findByTestID(t, open[0], "reassign-action-item"), "owner")
		if newOwner.Value != "nobody@example.com" || attrOf(newOwner.Node, "aria-invalid") != "true" || !strings.HasPrefix(newOwner.Description, `No account with email "nobody@example.com"`) {
			t.Errorf("%s: owner holds %q, aria-invalid %q, described %q; want it kept, invalid, saying no account has it", back, newOwner.Value, attrOf(newOwner.Node, "aria-invalid"), newOwner.Description)
		}
	}
}

// A Reassign of an item closed since it was shown is refused as a whole: the
// refusal page, headed Can't reassign Action Item, with Back to the page it
// came from, and no redirect (#660).
func TestAReassignOfAnItemClosedSinceAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	h.SignIn("other@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Get a second quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	if _, err := h.Service.CloseActionItem(context.Background(), owner.ID, item.ID, "Done."); err != nil {
		t.Fatalf("CloseActionItem: %v", err)
	}
	ts := newServer(t, h)
	draftPath := fmt.Sprintf("/reports/%d", def.ID)

	resp := postForm(t, noRedirects(signInClient(t, ts.URL, "author@example.com")), fmt.Sprintf("%s/action-items/%d/reassign", ts.URL, item.ID),
		url.Values{"owner": {"other@example.com"}, "return": {draftPath}})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", resp.StatusCode)
	}
	assertRefusalPage(t, page, "Can't reassign Action Item", "The Action Item is already closed.", draftPath)
}

// A refusal sent back to a Report's draft is the draft's alone: its
// publication's page, whose address the draft's holds, neither shows nor
// clears it (#660).
func TestARefusalStaysOnThePageItWasSentBackToOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	item, err := h.Service.RaiseActionItem(context.Background(), author.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID, Text: "Get a second quote.", OwnerID: owner.ID, DueDate: h.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("RaiseActionItem: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "owner@example.com")
	draftPath := fmt.Sprintf("/reports/%d", def.ID)

	postRefusedBack(t, client, fmt.Sprintf("%s/action-items/%d/close", ts.URL, item.ID), url.Values{"note": {" "}, "return": {draftPath}}, draftPath)
	if page := getPage(t, client, fmt.Sprintf("%s/reports/%d/publications/%d", ts.URL, def.ID, pub.ID)); len(openDetails(page, "close-toggle")) != 0 {
		t.Errorf("the publication took the draft's refusal:\n%s", renderNode(page))
	}
	if page := getPage(t, client, ts.URL+draftPath); len(openDetails(page, "close-toggle")) != 1 {
		t.Errorf("the draft doesn't show its refused Close open:\n%s", renderNode(page))
	}
}

// A refused Add Action Item too long to carry back to its form answers with
// the refusal page instead, headed Can't add Action Item (#660).
func TestARefusedActionItemTooLongToCarryBackAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(author, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(author, def)
	ts := newServer(t, h)
	pubPath := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)

	resp := postForm(t, noRedirects(signInClient(t, ts.URL, "author@example.com")), ts.URL+pubPath+"/action-items", url.Values{
		"text": {strings.Repeat("Book the EU legal review. ", 120)}, "owner": {"nobody@example.com"}, "due": {"2026-02-01"},
	})
	defer func() { _ = resp.Body.Close() }()
	page := parsePage(t, readBody(t, resp))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", resp.StatusCode)
	}
	assertRefusalPage(t, page, "Can't add Action Item", `No account with email "nobody@example.com".`, pubPath)
}
