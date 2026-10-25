package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Defining a Dimension from the form, with several values from an Extendable
// list, writes one entry to the Definition log, not one per choice.
func TestDefiningADimensionOverHTTPWritesOneEntry(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, boss, ts.URL+"/dimensions", url.Values{
		"name": {"Customer"}, "values": {"Acme, Globex"}, "selection": {"several"}, "list": {"extendable"},
	})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("define Customer: status %d; body:\n%s", resp.StatusCode, body)
	}

	log := pageElement(t, getBody(t, boss, ts.URL+"/definition-log"), "ol", "definition-log")
	if got := strings.Count(log, `data-testid="definition-change"`); got != 1 {
		t.Errorf("log has %d entries, want 1:\n%s", got, log)
	}
	if want := "Created the Dimension Customer with Acme, Globex, taking several values from an Extendable list."; !strings.Contains(log, want) {
		t.Errorf("log lacks %q:\n%s", want, log)
	}
}

// The Admin page links to the Definition log.
func TestAdminPageLinksToTheDefinitionLog(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	row := pageElement(t, getBody(t, boss, ts.URL+"/admin"), "a", "admin-definition-log")
	for _, want := range []string{`href="/definition-log"`, "Definition log"} {
		if !strings.Contains(row, want) {
			t.Errorf("Admin page's Definition log link lacks %s: %s", want, row)
		}
	}
}

// Anyone signed in reads the log, newest first, each entry with who made the
// change and when; a rename shows the old and new name and a merge both values.
func TestANonAdminReadsTheLogNewestFirst(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	pillar := h.CreateDimension(h.SignIn("boss@example.com"), "Pillar", "Growth", "Trust", "Scale")
	growth, scale := pillar.Values[0], pillar.Values[2]
	h.Clock.Advance(time.Hour)
	for _, step := range []struct{ path, field, value string }{
		{fmt.Sprintf("/dimension-values/%d/rename", growth.ID), "value", "Expansion"},
		{fmt.Sprintf("/dimension-values/%d/merge", scale.ID), "into", fmt.Sprint(growth.ID)},
	} {
		resp := postForm(t, boss, ts.URL+step.path, url.Values{step.field: {step.value}})
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s: status %d; body:\n%s", step.path, resp.StatusCode, body)
		}
	}

	pat := signInClient(t, ts.URL, "pat@example.com")
	log := pageElement(t, getBody(t, pat, ts.URL+"/definition-log"), "ol", "definition-log")

	entries := strings.Split(log, `data-testid="definition-change"`)[1:]
	want := []string{
		"Merged Scale into Expansion in Pillar.",
		"Renamed Growth to Expansion in Pillar.",
		"Created the Dimension Pillar with Growth, Trust, Scale, taking one value from a Fixed list.",
	}
	if len(entries) != len(want) {
		t.Fatalf("log has %d entries, want %d:\n%s", len(entries), len(want), log)
	}
	for i, e := range entries {
		if !strings.Contains(e, want[i]) || !strings.Contains(e, "boss") {
			t.Errorf("entry %d lacks boss's %q:\n%s", i, want[i], e)
		}
	}
	if when := testsupport.Epoch.Add(time.Hour).Format("Jan 2, 15:04"); !strings.Contains(entries[0], when) {
		t.Errorf("newest entry lacks its time %s:\n%s", when, entries[0])
	}
}

// The log narrows to one Dimension or Field, its form showing which; anything
// naming neither is not found.
func TestTheLogNarrowsToOneDimensionOrFieldOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	admin := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	h.CreateDimension(admin, "Quarter", "Q1")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	pat := signInClient(t, ts.URL, "pat@example.com")

	for _, tc := range []struct {
		about, want string
		not         []string
	}{
		{fmt.Sprintf("dimension:%d", pillar.ID), "Created the Dimension Pillar", []string{"Quarter", "Budget"}},
		{fmt.Sprintf("field:%d", budget.ID), "Created the Field Budget", []string{"Pillar", "Quarter"}},
	} {
		page := getBody(t, pat, ts.URL+"/definition-log?about="+url.QueryEscape(tc.about))
		log := pageElement(t, page, "ol", "definition-log")
		if got := strings.Count(log, `data-testid="definition-change"`); got != 1 || !strings.Contains(log, tc.want) {
			t.Errorf("about=%s: log has %d entries, want just %q:\n%s", tc.about, got, tc.want, log)
		}
		for _, other := range tc.not {
			if strings.Contains(log, other) {
				t.Errorf("about=%s: log shows %s:\n%s", tc.about, other, log)
			}
		}
		form := pageElement(t, page, "form", "definition-log-scope")
		if !strings.Contains(form, `value="`+tc.about+`" selected`) {
			t.Errorf("about=%s: form doesn't show it chosen:\n%s", tc.about, form)
		}
	}

	for _, about := range []string{"dimension:999", "field:999", "goal:1", "dimension:x"} {
		resp, err := pat.Get(ts.URL + "/definition-log?about=" + url.QueryEscape(about))
		if err != nil {
			t.Fatalf("GET about=%s: %v", about, err)
		}
		if _ = readBody(t, resp); resp.StatusCode != http.StatusNotFound {
			t.Errorf("about=%s: status %d, want %d", about, resp.StatusCode, http.StatusNotFound)
		}
	}
}

// Nobody, not even an Admin, can write to, change or clear the log.
func TestNobodyCanEditTheLog(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	h.CreateDimension(h.SignIn("boss@example.com"), "Pillar", "Growth")
	boss := signInClient(t, ts.URL, "boss@example.com")

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, err := http.NewRequest(method, ts.URL+"/definition-log", strings.NewReader("summary=Nothing+happened"))
		if err != nil {
			t.Fatalf("new %s request: %v", method, err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := boss.Do(req)
		if err != nil {
			t.Fatalf("%s /definition-log: %v", method, err)
		}
		if _ = readBody(t, resp); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /definition-log: status %d, want %d", method, resp.StatusCode, http.StatusMethodNotAllowed)
		}
	}

	log := pageElement(t, getBody(t, boss, ts.URL+"/definition-log"), "ol", "definition-log")
	if got := strings.Count(log, `data-testid="definition-change"`); got != 1 || strings.Contains(log, "Nothing happened") {
		t.Errorf("log changed, want just Pillar's creation:\n%s", log)
	}
}

// A value an Owner adds on their Goal's page to an Extendable list is logged
// with that Owner, and a non-Admin's refused change to a Dimension writes
// nothing.
func TestAnOwnersAdditionIsLoggedAndARefusedChangeIsNotOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	admin := h.SignIn("boss@example.com")
	customer := h.CreateExtendableDimension(admin, "Customer", "Acme")
	goal := h.CreateGoal(h.SignIn("pat@example.com"), "Reduce outages", "Outages cost trust.")
	pat := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, pat, fmt.Sprintf("%s/goals/%d/dimensions", ts.URL, goal.ID), url.Values{
		"dimension_id": {fmt.Sprint(customer.ID)}, "new_value": {"Globex"},
	})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("add Globex: status %d; body:\n%s", resp.StatusCode, body)
	}
	resp = postForm(t, pat, fmt.Sprintf("%s/dimensions/%d/retire", ts.URL, customer.ID), nil)
	if _ = readBody(t, resp); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pat retiring Customer: status %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	log := pageElement(t, getBody(t, pat, ts.URL+"/definition-log"), "ol", "definition-log")
	entries := strings.Split(log, `data-testid="definition-change"`)[1:]
	if len(entries) != 3 {
		t.Fatalf("log has %d entries, want the creation, the switch to Extendable and pat's addition:\n%s", len(entries), log)
	}
	if !strings.Contains(entries[0], "Added Globex to Customer.") || !strings.Contains(entries[0], "pat") {
		t.Errorf("newest entry isn't pat adding Globex:\n%s", entries[0])
	}
}

// The Definition log sits in the flat frame: its name in the header strip,
// and its list a plain section of hairline rows, with no card.
func TestDefinitionLogIsInTheFlatFrame(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	assertInFlatFrame(t, getBody(t, boss, ts.URL+"/definition-log"), "Definition log")
}

// The whole log shows its newest 100 changes; when there are older ones, an
// Older changes link leads to the next 100 before them. A log of 100 or fewer
// has no such link, and a page bound that isn't a number is not found.
func TestTheWholeLogPagesThroughOlderChangesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	admin := h.SignIn("boss@example.com")
	for i := 1; i <= 105; i++ {
		h.CreateField(admin, fmt.Sprintf("Field %d", i), domain.FieldNumber, "")
	}
	pat := signInClient(t, ts.URL, "pat@example.com")

	newest := getPage(t, pat, ts.URL+"/definition-log")
	entries := textsOf(findAllByTestID(newest, "definition-change"))
	if len(entries) != 100 || !strings.HasPrefix(entries[0], "Created the Field Field 105,") ||
		!strings.HasPrefix(entries[99], "Created the Field Field 6,") {
		t.Fatalf("newest page = %d entries, want Field 105 down to Field 6: %q", len(entries), entries)
	}
	older := findByTestID(t, newest, "definition-log-older")
	if got := textOf(older); got != "Older changes" {
		t.Errorf("older link reads %q, want Older changes", got)
	}
	page, err := h.Service.DefinitionLog(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("DefinitionLog: %v", err)
	}
	href := fmt.Sprintf("/definition-log?before=%d", page[99].ID)
	if linkTo(newest, href) != older {
		t.Errorf("older link goes to %q, want %q", attrOf(older, "href"), href)
	}

	next := getPage(t, pat, ts.URL+href)
	entries = textsOf(findAllByTestID(next, "definition-change"))
	if len(entries) != 5 {
		t.Fatalf("older page = %d entries, want Field 5 down to Field 1: %q", len(entries), entries)
	}
	for i, e := range entries {
		if want := fmt.Sprintf("Created the Field Field %d,", 5-i); !strings.HasPrefix(e, want) {
			t.Errorf("older entry %d = %q, want it to start %q", i, e, want)
		}
	}
	if hasTestID(next, "definition-log-older") {
		t.Errorf("the oldest page links to older changes")
	}

	resp, err := pat.Get(ts.URL + "/definition-log?before=x")
	if err != nil {
		t.Fatalf("GET before=x: %v", err)
	}
	if _ = readBody(t, resp); resp.StatusCode != http.StatusNotFound {
		t.Errorf("before=x: status %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// A log of exactly 100 changes fits its one page, so has no Older changes
// link.
func TestALogOfOnePageHasNoOlderChangesLinkOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	admin := h.SignIn("boss@example.com")
	for i := 1; i <= 100; i++ {
		h.CreateField(admin, fmt.Sprintf("Field %d", i), domain.FieldNumber, "")
	}

	page := getPage(t, signInClient(t, ts.URL, "pat@example.com"), ts.URL+"/definition-log")
	if got := len(findAllByTestID(page, "definition-change")); got != 100 {
		t.Errorf("log shows %d entries, want all 100", got)
	}
	if hasTestID(page, "definition-log-older") {
		t.Errorf("a log of 100 changes links to older changes")
	}
}
