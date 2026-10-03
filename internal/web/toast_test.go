package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The toast is fixed to the bottom of the window, above the page content, and
// its message reads at WCAG AA's 4.5:1 on its fill in every theme (#83).
func TestToastSitsAtTheBottomAboveThePageInEveryTheme(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")
	toast := cssRule(t, css, "\n.toast")

	for property, want := range map[string]string{"position": "fixed", "bottom": "24px", "z-index": "30"} {
		if got := declValue(t, toast, property); got != want {
			t.Errorf(".toast %s = %q, want %q", property, got, want)
		}
	}
	themes := darkThemes(t, css)
	themes["light"] = tokens(tokenBlock(t, css, ":root"))
	for name, theme := range themes {
		ink := resolve(t, theme, declValue(t, toast, "color"))
		fill := resolve(t, theme, declValue(t, toast, "background"))
		if ratio := contrast(t, ink, fill); ratio < 4.5 {
			t.Errorf("%s: toast text %s on %s is %.2f:1, under 4.5:1", name, ink, fill, ratio)
		}
	}
}

// At phone width the toast spans the window less a 16px margin each side, so
// its message wraps rather than running off the screen.
func TestToastFitsAPhoneWidth(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	css := getBody(t, http.DefaultClient, ts.URL+"/static/app.css")

	m := regexp.MustCompile(`@media \(max-width:600px\)\{\.toast\{([^}]*)\}`).FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no phone-width .toast rule")
	}
	for _, want := range []string{"left:16px", "right:16px", "transform:none"} {
		if !strings.Contains(m[1], want) {
			t.Errorf("phone-width .toast rule %q lacks %s", m[1], want)
		}
	}
}

// undoCase is one of the four actions that offer an Undo in a toast: act does
// it over HTTP and returns the person's client and the page they land on, and
// restored says whether the Undo has put things back. back is the page a
// refused Undo links back to, and again what a second Undo is refused with.
type undoCase struct {
	name     string
	admins   []string
	act      func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string)
	restored func(t *testing.T, h *testsupport.Harness) bool
	back     func(t *testing.T, h *testsupport.Harness) string
	again    string
}

var undoCases = []undoCase{
	{
		name: "link removal",
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			_, _, sam, landed := removeAcrossOwners(t, h, ts)
			return sam, landed
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			goals, err := h.Service.ListGoals(context.Background())
			if err != nil {
				t.Fatalf("ListGoals: %v", err)
			}
			for _, g := range goals {
				if g.Title == "Migrate displays" {
					return len(h.ParentsOf(g)) == 1
				}
			}
			t.Fatal("no child Goal")
			return false
		},
		back: func(t *testing.T, h *testsupport.Harness) string {
			return fmt.Sprintf("/goals/%d", goalTitled(t, h, "Migrate displays").ID)
		},
		again: "This removal has already been undone.",
	},
	{
		name: "link rejection",
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			_, _, pat, landed := rejectRequest(t, h, ts, "/links")
			return pat, landed
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			pending, err := h.Service.PendingLinkRequests(context.Background(), h.SignIn("pat@example.com").ID)
			if err != nil {
				t.Fatalf("PendingLinkRequests: %v", err)
			}
			return len(pending) == 1
		},
		back:  func(*testing.T, *testsupport.Harness) string { return "/links" },
		again: "This rejection has already been undone.",
	},
	{
		name: "Handoff rejection",
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			_, _, pat, landed := rejectHandoff(t, h, ts, "/handoffs")
			return pat, landed
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			pending, err := h.Service.PendingHandoffs(context.Background(), h.SignIn("pat@example.com").ID)
			if err != nil {
				t.Fatalf("PendingHandoffs: %v", err)
			}
			return len(pending) == 1
		},
		back:  func(*testing.T, *testsupport.Harness) string { return "/handoffs" },
		again: "This Handoff isn't rejected, so there is nothing to undo.",
	},
	{
		name:   "value retirement",
		admins: []string{"boss@example.com"},
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			pillar := h.CreateDimension(h.SignIn("boss@example.com"), "Pillar", "Growth", "Trust")
			admin := signInClient(t, ts.URL, "boss@example.com")
			resp := postForm(t, admin, fmt.Sprintf("%s/dimension-values/%d/retire", ts.URL, pillar.Values[1].ID), url.Values{})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("retire Trust: status %d", resp.StatusCode)
			}
			return admin, readBody(t, resp)
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			dims, err := h.Service.ListDimensions(context.Background())
			if err != nil {
				t.Fatalf("ListDimensions: %v", err)
			}
			return !dims[0].Values[1].Retired
		},
		back:  func(*testing.T, *testsupport.Harness) string { return "/dimensions" },
		again: "This Undo is no longer available.",
	},
}

// goalTitled is the Goal called title.
func goalTitled(t *testing.T, h *testsupport.Harness, title string) domain.Goal {
	t.Helper()
	goals, err := h.Service.ListGoals(context.Background())
	if err != nil {
		t.Fatalf("ListGoals: %v", err)
	}
	for _, g := range goals {
		if g.Title == title {
			return g
		}
	}
	t.Fatalf("no Goal called %q", title)
	return domain.Goal{}
}

// assertUndoRefused checks a refused Undo came back as status inside the
// site's chrome: a "Can't undo" page whose one sentence says why, plainly,
// containing reason, with a Back link to back.
func assertUndoRefused(t *testing.T, resp *http.Response, status int, reason, back string) {
	t.Helper()
	page := readBody(t, resp)
	if resp.StatusCode != status {
		t.Errorf("status %d, want %d; body:\n%s", resp.StatusCode, status, page)
	}
	if !strings.Contains(page, `data-testid="nav-home"`) || !strings.Contains(page, "Can't undo") {
		t.Fatalf("refusal is not a Can't undo page inside the site's chrome; body:\n%s", page)
	}
	element := pageElement(t, page, "p", "undo-refused")
	sentence := html.UnescapeString(element[strings.Index(element, ">")+1:])
	if !strings.Contains(sentence, reason) {
		t.Errorf("refusal says %q, want it to say %q", sentence, reason)
	}
	for _, internal := range []string{"validation failed", "not authorized", "not found:"} {
		if strings.Contains(sentence, internal) {
			t.Errorf("refusal %q carries the internal %q", sentence, internal)
		}
	}
	if regexp.MustCompile(`[0-9]`).MatchString(sentence) {
		t.Errorf("refusal %q carries a raw ID", sentence)
	}
	if !strings.Contains(page, `<a href="`+back+`">Back</a>`) {
		t.Errorf("refusal has no Back link to %s; body:\n%s", back, page)
	}
}

// Each Undo succeeds from its toast, once: the same Undo submitted a second
// time is refused with 422, on a page saying why with a link back.
func TestEachUndoWorksOnceFromItsToast(t *testing.T) {
	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			action, fields := undoAction(t, landed), toastFields(t, landed)

			if resp := postForm(t, client, ts.URL+action, fields); resp.StatusCode != http.StatusOK {
				t.Fatalf("Undo: status %d %s", resp.StatusCode, readBody(t, resp))
			}
			if !tc.restored(t, h) {
				t.Fatal("not restored by the Undo")
			}
			resp := postForm(t, client, ts.URL+action, fields)
			assertUndoRefused(t, resp, http.StatusUnprocessableEntity, tc.again, tc.back(t, h))
		})
	}
}

// An Undo lasts 15 minutes: posted from its toast any later, it is refused
// with 422, on a page saying it is no longer available with a link back, and
// restores nothing.
func TestAnUndoIsNoLongerAvailableAfter15Minutes(t *testing.T) {
	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			h.Clock.Advance(15*time.Minute + time.Second)

			resp := postForm(t, client, ts.URL+undoAction(t, landed), toastFields(t, landed))
			assertUndoRefused(t, resp, http.StatusUnprocessableEntity, "This Undo is no longer available.", tc.back(t, h))
			if tc.restored(t, h) {
				t.Error("restored by a late Undo")
			}
		})
	}
}

// An Undo without its toast's token, or with a made-up one, is refused with
// 403, on a page saying why with a link back, and restores nothing; it doesn't
// spend the real Undo.
func TestAnUndoWithoutItsTokenIsRefused(t *testing.T) {
	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			action, fields := undoAction(t, landed), toastFields(t, landed)
			if fields.Get("undo") == "" {
				t.Fatalf("the toast carries no Undo token: %v", fields)
			}

			missing := url.Values{}
			for k, v := range fields {
				if k != "undo" {
					missing[k] = v
				}
			}
			forged := url.Values{"undo": {strings.Repeat("0", len(fields.Get("undo")))}}
			for k, v := range missing {
				forged[k] = v
			}
			for name, form := range map[string]url.Values{"missing": missing, "forged": forged} {
				t.Run(name, func(t *testing.T) {
					resp := postForm(t, client, ts.URL+action, form)
					assertUndoRefused(t, resp, http.StatusForbidden, "This Undo isn't yours to use.", tc.back(t, h))
				})
			}
			if tc.restored(t, h) {
				t.Fatal("restored by an Undo without its token")
			}
			if resp := postForm(t, client, ts.URL+action, fields); resp.StatusCode != http.StatusOK {
				t.Errorf("Undo with its token after refused ones: status %d", resp.StatusCode)
			}
		})
	}
}

// An Undo of something that never happened is refused with 404, on a page
// saying there's nothing to undo, linking back to the page it names: the Goal
// page for a link removal (Home without one), Home or the pending page for a
// rejection.
func TestAnUndoOfNothingSaysThereIsNothingToUndo(t *testing.T) {
	h := testsupport.New(t)
	ts := newServer(t, h)
	goal := h.CreateGoal(h.SignIn("sam@example.com"), "Migrate displays", "Displays fail often.")
	sam := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		path string
		form url.Values
		back string
	}{
		{"/link-removals/4242/undo", url.Values{"goal_id": {fmt.Sprint(goal.ID)}}, fmt.Sprintf("/goals/%d", goal.ID)},
		{"/link-removals/4242/undo", url.Values{}, "/home"},
		{"/link-removals/nothing/undo", url.Values{"goal_id": {"nothing"}}, "/home"},
		{"/link-rejections/4242/undo", url.Values{}, "/links"},
		{"/link-rejections/4242/undo", url.Values{"from": {"home"}}, "/home"},
		{"/handoffs/4242/restore", url.Values{}, "/handoffs"},
		{"/handoffs/4242/restore", url.Values{"from": {"home"}}, "/home"},
	} {
		t.Run(tc.path+"?"+tc.form.Encode(), func(t *testing.T) {
			resp := postForm(t, sam, ts.URL+tc.path, tc.form)
			assertUndoRefused(t, resp, http.StatusNotFound, "There's nothing here to undo.", tc.back)
		})
	}
}

// An Undo that would now close a cycle is refused with 409, on a page saying
// so with a link back, and restores nothing.
func TestAnUndoThatWouldMakeACycleSaysSo(t *testing.T) {
	for _, tc := range undoCases[:2] {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			// Pat links the Goals the other way round, and Sam accepts.
			pat, sam := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
			back := h.RequestLink(pat, goalTitled(t, h, "Reduce outages"), goalTitled(t, h, "Migrate displays"), "")
			if _, err := h.Service.AcceptLink(context.Background(), back.ID, sam.ID); err != nil {
				t.Fatalf("AcceptLink: %v", err)
			}

			resp := postForm(t, client, ts.URL+undoAction(t, landed), toastFields(t, landed))
			assertUndoRefused(t, resp, http.StatusConflict, "Restoring it now would make a cycle.", tc.back(t, h))
			if tc.restored(t, h) {
				t.Error("restored by an Undo that would make a cycle")
			}
		})
	}
}
