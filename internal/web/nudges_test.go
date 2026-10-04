package web_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"github.com/zachthieme/goal-tracker/internal/web"
)

// nudgeForm returns a Risks row's Nudge form, failing the test when its Fix
// isn't one.
func nudgeForm(t *testing.T, row string) string {
	t.Helper()
	cell := pageElement(t, row, "td", "risk-fix")
	if !strings.Contains(cell, `<form data-testid="nudge"`) {
		t.Fatalf("the Fix is no Nudge form: %s", cell)
	}
	return pageElement(t, cell, "form", "nudge")
}

// Someone else's Stale Goal on the Risks page has a Nudge form as its Fix.
// Once posted, the page returns to the Risks page as it was filtered, and
// anyone else who can't check in sees a disabled "Nudged today by Pat" in its
// place, while its Owner still sees Check in.
func TestNudgingFromTheRisksFix(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignInNamed("pat@example.com", "Pat Okafor")
	h.SignIn("kim@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)

	pat := signInClient(t, ts.URL, "pat@example.com")
	form := nudgeForm(t, riskRowOf(t, getBody(t, pat, ts.URL+"/risks?group=owner"), silent))
	action := attr(openTag(form)+">", "action")
	if want := "/goals/" + strconv.FormatInt(silent.ID, 10) + "/nudge"; action != want || !strings.Contains(openTag(form), `method="post"`) {
		t.Fatalf("Nudge form posts to %q, want %s: %s", action, want, form)
	}
	if !strings.Contains(form, `<input type="hidden" name="return" value="/risks?group=owner">`) {
		t.Errorf("the Nudge form doesn't return to the Risks page as filtered: %s", form)
	}
	if !strings.Contains(form, ">Nudge</button>") {
		t.Errorf("the Nudge form's button doesn't say Nudge: %s", form)
	}

	resp := postForm(t, pat, ts.URL+action, url.Values{"return": {"/risks?group=owner"}})
	_ = readBody(t, resp)
	if got := resp.Request.URL.String(); got != ts.URL+"/risks?group=owner" {
		t.Errorf("nudging landed on %s, want the Risks page as filtered", got)
	}
	if n := len(h.Email.Sent()); n != 1 {
		t.Errorf("sent %d emails, want 1 to the Owner", n)
	}

	for _, viewer := range []string{"pat@example.com", "kim@example.com"} {
		cell := pageElement(t, riskRowOf(t, getBody(t, signInClient(t, ts.URL, viewer), ts.URL+"/risks"), silent), "td", "risk-fix")
		if !strings.Contains(cell, "disabled") || !strings.Contains(cell, "Nudged today by Pat Okafor") || strings.Contains(cell, "<form") {
			t.Errorf("%s sees Fix %s, want a disabled Nudged today by Pat Okafor", viewer, cell)
		}
	}
	label, _, _ := riskFix(t, riskRowOf(t, getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/risks"), silent))
	if label != "Check in" {
		t.Errorf("the Owner sees Fix %q, want Check in", label)
	}
}

// A row that is both someone else's Stale Goal and Unaligned gets Nudge, not
// Suggest a parent: freshness outranks alignment.
func TestNudgeOutranksSuggestAParent(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	sam := h.SignIn("sam@example.com")
	h.SignIn("kim@example.com")
	loner := h.ActiveGoal(sam, "Silent side project", "Nobody asked.")
	h.Clock.Advance(10 * day)

	row := riskRowOf(t, getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/risks"), loner)
	riskChip(t, row, "stale")
	riskChip(t, row, "unaligned")
	nudgeForm(t, row)
}

// An Ownerless Stale Goal is nudged to its Delegates; with none left who can
// check in, its row keeps Open Goal for anyone but an Admin.
func TestNudgeOfAnOwnerlessGoalOnlyWithAPresentDelegate(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("kim@example.com")
	delegated := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Delegated work", "It matters."))
	h.AddDelegate(sam, dee, delegated.ID)
	alone := h.MarkTopLevel(ada, h.ActiveGoal(sam, "Lonely work", "It matters."))
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, sam.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)

	page := getBody(t, signInClient(t, ts.URL, "kim@example.com"), ts.URL+"/risks")
	nudgeForm(t, riskRowOf(t, page, delegated))
	if label, href, _ := riskFix(t, riskRowOf(t, page, alone)); label != "Open Goal" || href != "/goals/"+strconv.FormatInt(alone.ID, 10) {
		t.Errorf("the Ownerless Goal with nobody to nudge has Fix %q → %s, want Open Goal", label, href)
	}
}

// A refused Nudge answers with the app's own page, its reason and a Back link
// to the Risks page: 422 when the Goal was already nudged today or nobody can
// check in, 403 for its Owner.
func TestARefusedNudgeSaysWhy(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "ada@example.com")
	ts := newServer(t, h)
	ada := h.SignIn("ada@example.com")
	sam := h.SignIn("sam@example.com")
	gus := h.SignIn("gus@example.com")
	pat := h.SignInNamed("pat@example.com", "Pat Okafor")
	h.SignIn("kim@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	alone := h.ActiveGoal(gus, "Lonely work", "It matters.")
	if err := h.Service.MarkDeparted(t.Context(), ada.ID, gus.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	h.Clock.Advance(10 * day)
	if _, err := h.Service.Nudge(t.Context(), pat.ID, silent.ID); err != nil {
		t.Fatalf("Nudge: %v", err)
	}

	for _, c := range []struct {
		viewer string
		goal   domain.Goal
		status int
		reason string
	}{
		{"kim@example.com", silent, 422, "Pat Okafor already nudged this Goal today."},
		{"kim@example.com", alone, 422, "Nobody can check in on this Goal; it needs an Admin to reassign it."},
		{"sam@example.com", silent, 403, "You can check in on this Goal yourself, so there's no one to nudge."},
	} {
		resp := postForm(t, signInClient(t, ts.URL, c.viewer), ts.URL+"/goals/"+strconv.FormatInt(c.goal.ID, 10)+"/nudge", url.Values{"return": {"/risks"}})
		body := html.UnescapeString(readBody(t, resp))
		if resp.StatusCode != c.status {
			t.Errorf("%s nudging %q: status %d, want %d", c.viewer, c.goal.Title, resp.StatusCode, c.status)
		}
		if !strings.Contains(body, "<h1>Can't nudge</h1>") || !strings.Contains(body, `<a href="/risks">Back</a>`) {
			t.Errorf("%s nudging %q: no Can't nudge page with a Back link:\n%s", c.viewer, c.goal.Title, body)
		}
		reason := pageElement(t, body, "p", "nudge-refused")
		if !strings.HasSuffix(reason, ">"+c.reason) {
			t.Errorf("%s nudging %q: reason %s, want %q", c.viewer, c.goal.Title, reason, c.reason)
		}
	}
}

// failingSender is an email sender whose every send fails.
type failingSender struct{}

func (failingSender) Send(context.Context, email.Message) error { return errors.New("mail is down") }

// A Nudge whose email can't go out still stands: the page says so with a 502,
// naming the Owner it didn't reach, and the Goal keeps its Nudge.
func TestANudgeNotEmailedSaysItWasRecorded(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	h.SignIn("pat@example.com")
	silent := h.ActiveGoal(sam, "Silent work", "It matters.")
	h.Clock.Advance(10 * day)
	ts := httptest.NewServer(web.NewServer(domain.NewService(h.DB, h.Clock, failingSender{}, nil)))
	t.Cleanup(ts.Close)

	resp := postForm(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/goals/"+strconv.FormatInt(silent.ID, 10)+"/nudge", url.Values{"return": {"/risks"}})
	body := html.UnescapeString(readBody(t, resp))
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status %d, want 502", resp.StatusCode)
	}
	if !strings.Contains(body, "<title>Nudge not emailed · Goal Tracker</title>") || !strings.Contains(body, "<h1>Nudge not emailed</h1>") || !strings.Contains(body, `<a href="/risks">Back</a>`) {
		t.Errorf("no Nudge not emailed page with a Back link:\n%s", body)
	}
	reason := pageElement(t, body, "p", "nudge-refused")
	if want := "The Nudge was recorded, but the email to sam@example.com couldn't be sent."; !strings.HasSuffix(reason, ">"+want) {
		t.Errorf("reason %s, want %q", reason, want)
	}
	if kept, err := h.Service.Nudges(t.Context(), silent.ID); err != nil || len(kept) != 1 {
		t.Errorf("kept %d Nudges (err %v), want 1", len(kept), err)
	}
}
