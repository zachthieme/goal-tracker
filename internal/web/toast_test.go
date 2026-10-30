package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
	"golang.org/x/net/html"
)

// The toast is fixed to the bottom of the window, above the page content, and
// its message reads at WCAG AA's 4.5:1 on its fill in every theme (#83).
func TestToastSitsAtTheBottomAboveThePageInEveryTheme(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// undoCase is one of the actions that offer an Undo in a toast: act does
// it over HTTP and returns the person's client and the page they land on, and
// restored says whether the Undo has put things back. back is the page a
// refused Undo links back to, again what a second Undo is refused with, and
// notYours what someone else posting the toast's fields is refused with.
// writes is the kind of write the Undo makes to the table writesTo, for a test
// to make fail.
type undoCase struct {
	name     string
	admins   []string
	act      func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string)
	restored func(t *testing.T, h *testsupport.Harness) bool
	back     func(t *testing.T, h *testsupport.Harness) string
	again    string
	notYours string
	writes   testsupport.Write
	writesTo string
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
		again:    "This removal has already been undone.",
		notYours: "Only the person who removed a link may undo it.",
		writes:   testsupport.Insert,
		writesTo: "links",
	},
	{
		name: "link rejection",
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			_, _, pat, landed := rejectRequest(t, h, ts, "/home")
			return pat, landed
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			pending, err := h.Service.PendingLinkRequests(context.Background(), h.SignIn("pat@example.com").ID)
			if err != nil {
				t.Fatalf("PendingLinkRequests: %v", err)
			}
			return len(pending) == 1
		},
		back:     func(*testing.T, *testsupport.Harness) string { return "/home" },
		again:    "This rejection has already been undone.",
		notYours: "Only the person who rejected a request may undo it.",
		writes:   testsupport.Insert,
		writesTo: "links",
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
		back:     func(*testing.T, *testsupport.Harness) string { return "/handoffs" },
		again:    "This Handoff isn't rejected, so there is nothing to undo.",
		notYours: "Only the person who rejected a Handoff may undo it.",
		writes:   testsupport.Update,
		writesTo: "handoffs",
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
		back:     func(*testing.T, *testsupport.Harness) string { return "/dimensions" },
		again:    "This Undo is no longer available.",
		notYours: "This Undo isn't yours to use.",
		writes:   testsupport.Update,
		writesTo: "dimension_values",
	},
	{
		name: "Parent suggestion decline",
		act: func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string) {
			_, _, s := suggestionScene(t, h)
			sam := signInClient(t, ts.URL, "sam@example.com")
			resp := postForm(t, sam, fmt.Sprintf("%s/parent-suggestions/%d/decline", ts.URL, s.ID), url.Values{"return": {"/home"}})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("decline: status %d", resp.StatusCode)
			}
			return sam, readBody(t, resp)
		},
		restored: func(t *testing.T, h *testsupport.Harness) bool {
			open, err := h.Service.OpenParentSuggestions(context.Background(), goalTitled(t, h, "Migrate displays").ID)
			if err != nil {
				t.Fatalf("OpenParentSuggestions: %v", err)
			}
			return len(open) == 1
		},
		back:     func(*testing.T, *testsupport.Harness) string { return "/home" },
		again:    "This suggestion is no longer declined.",
		notYours: "This Undo isn't yours to use.",
		writes:   testsupport.Update,
		writesTo: "parent_suggestions",
	},
}

// Every Undo goes the whole way round: the decision, made from the page its
// kind is offered on (with a query, where the decision posts its return),
// lands back there, whose toast's Undo restores the record and lands on that
// same page, query and all (#605).
func TestEachUndoReturnsToThePageItWasOfferedOn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		undo undoCase
		// decide arranges the record and makes the decision from the page it is
		// offered on, returning who decided, the page, and its form.
		decide func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (client *http.Client, page string, form *html.Node)
	}{
		{undoCases[0], func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string, *html.Node) {
			child, _, link := linkedAcrossOwners(t, h)
			sam := signInClient(t, ts.URL, "sam@example.com")
			page := fmt.Sprintf("/goals/%d", child.ID)
			return sam, page, decisionForm(t, sam, ts.URL+page, fmt.Sprintf("/links/%d/remove", link.ID))
		}},
		{undoCases[1], func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string, *html.Node) {
			pat, sam := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
			parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
			child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
			link := h.RequestLink(sam, child, parent, "displays cause outages")
			client := signInClient(t, ts.URL, "pat@example.com")
			return client, "/home?x=1", decisionForm(t, client, ts.URL+"/home?x=1", fmt.Sprintf("/links/%d/reject", link.ID))
		}},
		{undoCases[2], func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string, *html.Node) {
			sam, pat := h.SignIn("sam@example.com"), h.SignIn("pat@example.com")
			goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
			ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, pat.Email, sam.ID)
			if err != nil {
				t.Fatalf("StartHandoff: %v", err)
			}
			client := signInClient(t, ts.URL, "pat@example.com")
			return client, "/home?x=1", decisionForm(t, client, ts.URL+"/home?x=1", fmt.Sprintf("/handoffs/%d/reject", ho.ID))
		}},
		{undoCases[3], func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string, *html.Node) {
			pillar := h.CreateDimension(h.SignIn("boss@example.com"), "Pillar", "Growth", "Trust")
			admin := signInClient(t, ts.URL, "boss@example.com")
			return admin, "/dimensions", decisionForm(t, admin, ts.URL+"/dimensions", fmt.Sprintf("/dimension-values/%d/retire", pillar.Values[1].ID))
		}},
		{undoCases[4], func(t *testing.T, h *testsupport.Harness, ts *httptest.Server) (*http.Client, string, *html.Node) {
			_, _, s := suggestionScene(t, h)
			sam := signInClient(t, ts.URL, "sam@example.com")
			return sam, "/home?x=1", decisionForm(t, sam, ts.URL+"/home?x=1", fmt.Sprintf("/parent-suggestions/%d/decline", s.ID))
		}},
	} {
		t.Run(tc.undo.name, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t, tc.undo.admins...)
			ts := newServer(t, h)
			client, page, form := tc.decide(t, h, ts)
			decision := formSubmission(form)
			if decision.Has("return") {
				decision.Set("return", page)
			}

			decided := postForm(t, client, ts.URL+attrOf(form, "action"), decision)
			defer func() { _ = decided.Body.Close() }()
			if decided.StatusCode != http.StatusOK {
				t.Fatalf("decision: status %d", decided.StatusCode)
			}
			if got := decided.Request.URL.RequestURI(); got != page {
				t.Fatalf("decision landed on %s, want %s", got, page)
			}
			toast := findByTestID(t, parsePage(t, readBody(t, decided)), "toast")
			forms := findAll(toast, func(n *html.Node) bool { return n.Data == "form" })
			if len(forms) != 1 || buttonNamed(forms[0], "Undo") == nil {
				t.Fatalf("the toast has no Undo form:\n%s", renderNode(toast))
			}

			undone := postForm(t, client, ts.URL+attrOf(forms[0], "action"), formSubmission(forms[0]))
			defer func() { _ = undone.Body.Close() }()
			if undone.StatusCode != http.StatusOK {
				t.Fatalf("Undo: status %d", undone.StatusCode)
			}
			if !tc.undo.restored(t, h) {
				t.Error("not restored by the Undo")
			}
			if got := undone.Request.URL.RequestURI(); got != page {
				t.Errorf("Undo landed on %s, want %s, where its toast was shown", got, page)
			}
		})
	}
}

// decisionForm is the one form on the page at rawURL posting to action.
func decisionForm(t *testing.T, client *http.Client, rawURL, action string) *html.Node {
	t.Helper()
	forms := findAll(getPage(t, client, rawURL), func(n *html.Node) bool { return n.Data == "form" && attrOf(n, "action") == action })
	if len(forms) != 1 {
		t.Fatalf("%s has %d forms posting to %s, want 1", rawURL, len(forms), action)
	}
	return forms[0]
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

// assertUndoRefused checks a refused Undo came back as status on the refusal
// page inside the app: headed "Can't undo", saying why as reason, plainly,
// with a Back link to back.
func assertUndoRefused(t *testing.T, resp *http.Response, status int, reason, back string) {
	t.Helper()
	page := readBody(t, resp)
	if resp.StatusCode != status {
		t.Errorf("status %d, want %d; body:\n%s", resp.StatusCode, status, page)
	}
	assertRefusalPage(t, parsePage(t, page), "Can't undo", reason, back)
}

// Each Undo succeeds from its toast, once: the same Undo submitted a second
// time is refused with 422, on a page saying why with a link back.
func TestEachUndoWorksOnceFromItsToast(t *testing.T) {
	t.Parallel()

	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			action, fields := undoAction(t, landed), toastFields(t, landed)

			undone := postForm(t, client, ts.URL+action, fields)
			defer func() { _ = undone.Body.Close() }()
			if undone.StatusCode != http.StatusOK {
				t.Fatalf("Undo: status %d %s", undone.StatusCode, readBody(t, undone))
			}
			if !tc.restored(t, h) {
				t.Fatal("not restored by the Undo")
			}
			resp := postForm(t, client, ts.URL+action, fields)
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusUnprocessableEntity, tc.again, tc.back(t, h))
		})
	}
}

// An Undo lasts 15 minutes: posted from its toast any later, it is refused
// with 422, on a page saying it is no longer available with a link back, and
// restores nothing.
func TestAnUndoIsNoLongerAvailableAfter15Minutes(t *testing.T) {
	t.Parallel()

	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			h.Clock.Advance(15*time.Minute + time.Second)

			resp := postForm(t, client, ts.URL+undoAction(t, landed), toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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
					defer func() { _ = resp.Body.Close() }()
					assertUndoRefused(t, resp, http.StatusForbidden, "This Undo isn't yours to use.", tc.back(t, h))
				})
			}
			if tc.restored(t, h) {
				t.Fatal("restored by an Undo without its token")
			}
			if status := postFormStatus(t, client, ts.URL+action, fields); status != http.StatusOK {
				t.Errorf("Undo with its token after refused ones: status %d", status)
			}
		})
	}
}

// An Undo of something that never happened is refused with 404, on a page
// saying there's nothing to undo, linking back to the page it names: the Goal
// page for a link removal (Home without one), Home for a link rejection, and
// Home or the pending page for a Handoff's.
func TestAnUndoOfNothingSaysThereIsNothingToUndo(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	goal := h.CreateGoal(h.SignIn("sam@example.com"), "Migrate displays", "Displays fail often.")
	sam := signInClient(t, ts.URL, "sam@example.com")

	for _, tc := range []struct {
		path string
		form url.Values
		back string
	}{
		{"/link-removals/4242/undo", url.Values{"return": {fmt.Sprintf("/goals/%d", goal.ID)}}, fmt.Sprintf("/goals/%d", goal.ID)},
		{"/link-removals/4242/undo", url.Values{}, "/home"},
		{"/link-removals/nothing/undo", url.Values{"return": {"nothing"}}, "/home"},
		{"/link-rejections/4242/undo", url.Values{}, "/home"},
		{"/link-rejections/4242/undo", url.Values{"return": {"/home"}}, "/home"},
		{"/handoffs/4242/restore", url.Values{}, "/handoffs"},
		{"/handoffs/4242/restore", url.Values{"return": {"/home"}}, "/home"},
	} {
		t.Run(tc.path+"?"+tc.form.Encode(), func(t *testing.T) {
			resp := postForm(t, sam, ts.URL+tc.path, tc.form)
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusNotFound, "There's nothing here to undo.", tc.back)
		})
	}
}

// An Undo that would now close a cycle is refused with 409, on a page saying
// so as a refused link would, with a link back, and restores nothing.
func TestAnUndoThatWouldMakeACycleSaysSo(t *testing.T) {
	t.Parallel()

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
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusConflict, "Linking these Goals would make a cycle: “Reduce outages” already contributes to “Migrate displays”.", tc.back(t, h))
			if tc.restored(t, h) {
				t.Error("restored by an Undo that would make a cycle")
			}
		})
	}
}

// An Undo that fails part way is refused with 500, on a page saying nothing
// changed and to try again, with a link back, and restores nothing.
func TestAnUndoThatFailsSaysNothingChanged(t *testing.T) {
	t.Parallel()

	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			h.FailWrites(tc.writes, tc.writesTo)

			resp := postForm(t, client, ts.URL+undoAction(t, landed), toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusInternalServerError, "The Undo couldn't be done, so nothing changed. Try again.", tc.back(t, h))
			if tc.restored(t, h) {
				t.Error("restored by a failed Undo")
			}
		})
	}
}

// An Undo posted by someone other than the person it was offered to, with the
// toast's own fields, is refused with 403, on a page saying whose Undo it is
// with a link back, and restores nothing; it doesn't spend the real Undo.
func TestSomeoneElsesUndoIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range undoCases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.admins...)
			ts := newServer(t, h)
			client, landed := tc.act(t, h, ts)
			action, fields := undoAction(t, landed), toastFields(t, landed)
			h.SignIn("lee@example.com")
			lee := signInClient(t, ts.URL, "lee@example.com")

			resp := postForm(t, lee, ts.URL+action, fields)
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusForbidden, tc.notYours, tc.back(t, h))
			if tc.restored(t, h) {
				t.Fatal("restored by someone else's Undo")
			}
			if status := postFormStatus(t, client, ts.URL+action, fields); status != http.StatusOK {
				t.Errorf("Undo by its own person after someone else's: status %d", status)
			}
		})
	}
}

// An Undo that no longer fits what has happened since is refused with 422, on
// a page saying what changed with a link back: the link has been requested
// again, or the Goal has another pending Handoff or has changed hands.
func TestAnUndoThatNoLongerFitsSaysWhatChanged(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		undo   undoCase
		since  func(t *testing.T, h *testsupport.Harness)
		reason string
	}{
		{"link removal requested again", undoCases[0], func(t *testing.T, h *testsupport.Harness) {
			again := h.RequestLink(h.SignIn("sam@example.com"), goalTitled(t, h, "Migrate displays"), goalTitled(t, h, "Reduce outages"), "")
			if _, err := h.Service.AcceptLink(context.Background(), again.ID, h.SignIn("pat@example.com").ID); err != nil {
				t.Fatalf("AcceptLink: %v", err)
			}
			if _, err := h.Service.RemoveLink(context.Background(), again.ID, h.SignIn("pat@example.com").ID); err != nil {
				t.Fatalf("RemoveLink: %v", err)
			}
		}, "This link has been requested again since it was removed."},
		{"link rejection requested again", undoCases[1], func(t *testing.T, h *testsupport.Harness) {
			h.RequestLink(h.SignIn("sam@example.com"), goalTitled(t, h, "Migrate displays"), goalTitled(t, h, "Reduce outages"), "second try")
		}, "This link has been requested again since it was rejected."},
		{"another Handoff pending", undoCases[2], func(t *testing.T, h *testsupport.Harness) {
			goal := goalTitled(t, h, "Reduce outages")
			h.SignIn("mel@example.com")
			if _, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, "mel@example.com", goal.Owner.ID); err != nil {
				t.Fatalf("StartHandoff: %v", err)
			}
		}, "Another Handoff of this Goal is pending."},
		{"Goal changed hands", undoCases[2], func(t *testing.T, h *testsupport.Harness) {
			goal, mel := goalTitled(t, h, "Reduce outages"), h.SignIn("mel@example.com")
			ho, err := h.Service.StartHandoffByEmail(context.Background(), goal.ID, mel.Email, goal.Owner.ID)
			if err != nil {
				t.Fatalf("StartHandoff: %v", err)
			}
			if _, err := h.Service.AcceptHandoff(context.Background(), ho.ID, mel.ID, nil); err != nil {
				t.Fatalf("AcceptHandoff: %v", err)
			}
		}, "The Goal has changed hands since this Handoff was rejected."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.New(t, tc.undo.admins...)
			ts := newServer(t, h)
			client, landed := tc.undo.act(t, h, ts)
			tc.since(t, h)

			resp := postForm(t, client, ts.URL+undoAction(t, landed), toastFields(t, landed))
			defer func() { _ = resp.Body.Close() }()
			assertUndoRefused(t, resp, http.StatusUnprocessableEntity, tc.reason, tc.undo.back(t, h))
		})
	}
}

// An Undo offer made to a page whose return carries a query is scoped to that
// page's path alone, so the page, query and all, shows the toast; its Undo
// puts the request back as pending and lands on the page the toast was shown
// on, query and all (#597, #605).
func TestAnUndoOfferReachesAReturnWithAQuery(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ts := newServer(t, h)
	pat, sam := h.SignIn("pat@example.com"), h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Displays fail often.")
	link := h.RequestLink(sam, child, parent, "displays cause outages")
	client := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, noRedirects(client), fmt.Sprintf("%s/links/%d/reject", ts.URL, link.ID), url.Values{"return": {"/home?x=1"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reject: status %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/home?x=1" {
		t.Errorf("reject redirects to %q, want /home?x=1", got)
	}
	var offer *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "gt_undo" {
			offer = c
		}
	}
	if offer == nil {
		t.Fatal("reject offers no Undo")
	}
	if offer.Path != "/home" {
		t.Errorf("Undo offer's Path = %q, want /home", offer.Path)
	}

	toast := findByTestID(t, getPage(t, client, ts.URL+"/home?x=1"), "toast")
	forms := findAll(toast, func(n *html.Node) bool { return n.Data == "form" })
	if len(forms) != 1 || buttonNamed(forms[0], "Undo") == nil {
		t.Fatalf("the toast has no Undo form:\n%s", renderNode(toast))
	}

	undone := postForm(t, client, ts.URL+attrOf(forms[0], "action"), formSubmission(forms[0]))
	defer func() { _ = undone.Body.Close() }()
	if undone.StatusCode != http.StatusOK {
		t.Fatalf("Undo: status %d", undone.StatusCode)
	}
	if got := undone.Request.URL.RequestURI(); got != "/home?x=1" {
		t.Errorf("Undo landed on %s, want /home?x=1", got)
	}
	pending, err := h.Service.PendingLinkRequests(context.Background(), pat.ID)
	if err != nil {
		t.Fatalf("PendingLinkRequests: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("%d pending link requests after the Undo, want 1", len(pending))
	}
}
