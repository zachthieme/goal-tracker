package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A reader comments on a Goal's block from the publication page, the Owner is
// emailed, and the Owner replies in the thread (ticket #19).
func TestSmokeCommentAndReplyOnPublicationOverHTTP(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != strings.TrimPrefix(pubURL, ts.URL) {
		t.Fatalf("comment: status %d at %s, want back on the publication", resp.StatusCode, resp.Request.URL)
	}
	if thread := pageElement(t, readBody(t, resp), "div", "comment-thread"); !strings.Contains(thread, "Why did the vendor slip?") ||
		!strings.Contains(thread, "reader@example.com") {
		t.Errorf("publication does not show the comment and its author; thread:\n%s", thread)
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
	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("raise action item: status %d", resp.StatusCode)
	}
	if item := pageElement(t, readBody(t, resp), "li", "raised-action-item"); !strings.Contains(item, "Get a second vendor quote.") ||
		!strings.Contains(item, "owner@example.com") || !strings.Contains(item, "2026-02-01") {
		t.Errorf("publication does not list the raised Action Item; item:\n%s", item)
	}
	resp = postForm(t, ownerClient, firstURL+"/action-items", url.Values{
		"text": {"Sneaky"}, "owner": {"owner@example.com"}, "due": {"2026-02-01"},
	})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("raised by someone who is not the author: status %d, want 403", resp.StatusCode)
	}

	resp = postForm(t, authorClient, reportURL+"/publications", url.Values{"baseline": {""}})
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
	if item := pageElement(t, getBody(t, ownerClient, firstURL), "li", "raised-action-item"); !strings.Contains(item, "Quote is in: 20% cheaper.") {
		t.Errorf("the publication it was raised on does not show the closing note; item:\n%s", item)
	}

	resp = postForm(t, authorClient, reportURL+"/publications", url.Values{"baseline": {""}})
	if page := readBody(t, resp); strings.Contains(page, `data-testid="open-action-item"`) {
		t.Errorf("publication after closing still carries the Action Item")
	}
}

// Closing an Action Item comes back only to a page on this site: a return
// address that a browser would read as another site is ignored.
func TestCloseActionItemNeverRedirectsOffSite(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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
		_ = readBody(t, resp)
		want := fmt.Sprintf("/reports/%d/publications/%d", def.ID, pub.ID)
		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("return %q: redirected to %q, want %q", back, got, want)
		}
	}
}

// Under each Goal the discussion is a footer row: the comment count opens the
// threads, a Comment button opens the form, and the author's form to make a
// comment an Action Item sits behind its own toggle — all collapsed until the
// reader opens them, without JS. Replies are indented under the comment.
func TestPublicationDiscussionIsCollapsedOverHTTP(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	author := h.SignIn("author@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(author, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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

// A publication lists its open Action Items as text · owner · due date; the
// owner's close form sits behind a small Close button until they open it.
func TestOpenActionItemCloseFormIsCollapsedOverHTTP(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
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
	if want := "Get a second vendor quote. · " + shownAs("owner@example.com", "owner") + " · due " + h.Clock.Now().Format("2006-01-02"); !strings.Contains(item, want) {
		t.Errorf("open Action Item does not read %q; item:\n%s", want, item)
	}
	toggle := pageElement(t, item, "details", "close-toggle")
	if strings.HasPrefix(toggle, "<details data-testid=\"close-toggle\" open") || !strings.Contains(toggle, `data-testid="close-action-item"`) {
		t.Errorf("the close form is not behind a collapsed toggle; toggle:\n%s", toggle)
	}
}
