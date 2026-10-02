package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Defining a Dimension from the form, with several values from an Extendable
// list, writes one entry to the Definition log, not one per choice.
func TestDefiningADimensionOverHTTPWritesOneEntry(t *testing.T) {
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
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	card := pageElement(t, getBody(t, boss, ts.URL+"/admin"), "a", "admin-definition-log")
	for _, want := range []string{`href="/definition-log"`, `class="card`, "Definition log"} {
		if !strings.Contains(card, want) {
			t.Errorf("Admin page's Definition log card lacks %s: %s", want, card)
		}
	}
}
