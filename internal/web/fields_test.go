package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Admin page links to the Fields page, where an Admin defines a Field of
// each type, a number one with its unit, and each is listed with its type.
func TestAdminDefinesFieldsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	card := pageElement(t, getBody(t, boss, ts.URL+"/admin"), "a", "admin-fields")
	for _, want := range []string{`href="/fields"`, `class="card`, "Fields"} {
		if !strings.Contains(card, want) {
			t.Errorf("Admin page's Fields card lacks %s: %s", want, card)
		}
	}

	form := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "form", "create-field")
	for _, typ := range []string{domain.FieldNumber, domain.FieldShortText, domain.FieldLongText, domain.FieldDate} {
		if !strings.Contains(form, `value="`+typ+`"`) {
			t.Errorf("create form doesn't offer the %s type:\n%s", typ, form)
		}
	}
	for _, f := range []url.Values{
		{"name": {"Budget"}, "type": {domain.FieldNumber}, "unit": {"$"}},
		{"name": {"Design doc"}, "type": {domain.FieldShortText}},
		{"name": {"Notes"}, "type": {domain.FieldLongText}},
		{"name": {"Kickoff"}, "type": {domain.FieldDate}},
	} {
		resp := postForm(t, boss, ts.URL+"/fields", f)
		if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
			t.Fatalf("define %s: status %d; body:\n%s", f.Get("name"), resp.StatusCode, body)
		}
	}

	list := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "ul", "fields")
	for _, want := range []string{"Budget", "Number in $", "Design doc", "Short text", "Notes", "Long text", "Kickoff", "Date"} {
		if !strings.Contains(list, want) {
			t.Errorf("Fields list lacks %q:\n%s", want, list)
		}
	}
}

// A non-Admin sees the Fields but no form to define one, and defining one is
// refused.
func TestNonAdminCannotDefineFields(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)
	sam := signInClient(t, ts.URL, "sam@example.com")

	page := getBody(t, sam, ts.URL+"/fields")
	if !strings.Contains(page, "Budget") {
		t.Errorf("non-Admin can't see the Fields:\n%s", page)
	}
	if strings.Contains(page, `data-testid="create-field"`) {
		t.Errorf("non-Admin gets the define-a-Field form:\n%s", page)
	}
	resp := postForm(t, sam, ts.URL+"/fields", url.Values{"name": {"Notes"}, "type": {domain.FieldLongText}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin define a Field: status %d, want 403", resp.StatusCode)
	}
	fields, err := h.Service.ListFields(context.Background())
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) != 1 {
		t.Errorf("Fields = %+v, want only Budget", fields)
	}
}

// An Admin retires a Field from its card, after confirming, and it stays
// listed flagged retired with a Restore button that brings it back. A
// non-Admin gets neither button and is refused.
func TestAdminRetiresAndRestoresAFieldOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	budget := h.CreateField(h.SignIn("boss@example.com"), "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	retire := fmt.Sprintf("/fields/%d/retire", budget.ID)
	restore := fmt.Sprintf("/fields/%d/restore", budget.ID)

	if page := getBody(t, sam, ts.URL+"/fields"); strings.Contains(page, retire) {
		t.Errorf("non-Admin gets the Retire button:\n%s", page)
	}
	if resp := postForm(t, sam, ts.URL+retire, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin retire: status %d, want 403", resp.StatusCode)
	}

	card := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if form := openTag(between(t, card, `action="`+retire+`"`, "")); !strings.Contains(form, `onsubmit="return confirm(`) {
		t.Errorf("Retire doesn't ask for confirmation: %s", form)
	}
	if resp := postForm(t, boss, ts.URL+retire, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("retire: status %d", resp.StatusCode)
	}
	card = pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if !strings.Contains(card, `data-testid="field-retired"`) || !strings.Contains(card, `action="`+restore+`"`) {
		t.Errorf("a Retired Field's card isn't flagged retired with a Restore button:\n%s", card)
	}

	if resp := postForm(t, sam, ts.URL+restore, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin restore: status %d, want 403", resp.StatusCode)
	}
	if resp := postForm(t, boss, ts.URL+restore, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: status %d", resp.StatusCode)
	}
	card = pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if strings.Contains(card, `data-testid="field-retired"`) || !strings.Contains(card, `action="`+retire+`"`) {
		t.Errorf("a restored Field's card is still retired:\n%s", card)
	}
}
