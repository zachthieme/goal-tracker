package web_test

import (
	"context"
	"fmt"
	"html"
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

// The Goal page gives its Owner and a Delegate an input of each Field's type,
// and through it they set and clear each value; a Contributor sees the values
// read-only and is refused.
func TestOwnerAndDelegateSetAndClearFieldsOnTheGoalPage(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	dee := h.SignIn("dee@example.com")
	cal := h.SignIn("cal@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(pat, dee, goal.ID)
	if err := h.Service.AddContributor(context.Background(), goal.ID, cal.ID); err != nil {
		t.Fatalf("AddContributor: %v", err)
	}
	fields := []struct {
		field domain.Field
		input string
		value string
	}{
		{h.CreateField(boss, "Budget", domain.FieldNumber, "$"), `type="number"`, "1200"},
		{h.CreateField(boss, "Design doc", domain.FieldShortText, ""), `type="text"`, "In review"},
		{h.CreateField(boss, "Notes", domain.FieldLongText, ""), `<textarea`, "Owned by infra."},
		{h.CreateField(boss, "Kickoff", domain.FieldDate, ""), `type="date"`, "2026-03-01"},
	}
	ts := newServer(t, h)
	action := fmt.Sprintf(`action="/goals/%d/fields"`, goal.ID)
	post := ts.URL + fmt.Sprintf("/goals/%d/fields", goal.ID)

	for _, email := range []string{"pat@example.com", "dee@example.com"} {
		client := signInClient(t, ts.URL, email)
		edit := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "details", "edit-fields")
		for _, f := range fields {
			form := between(t, edit, fmt.Sprintf(`name="field_id" value="%d"`, f.field.ID), "</form>")
			if !strings.Contains(form, f.input) {
				t.Errorf("%s: %s's input isn't %s:\n%s", email, f.field.Name, f.input, form)
			}
			if !strings.Contains(between(t, edit, "", fmt.Sprintf(`name="field_id" value="%d"`, f.field.ID)), action) {
				t.Errorf("%s: %s's form doesn't post to the Goal's Fields", email, f.field.Name)
			}
		}
		for _, f := range fields {
			resp := postForm(t, client, post, url.Values{"field_id": {fmt.Sprint(f.field.ID)}, "value": {f.value}})
			if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
				t.Fatalf("%s sets %s: status %d; body:\n%s", email, f.field.Name, resp.StatusCode, body)
			}
		}
		shown := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "ul", "goal-fields")
		for _, f := range fields {
			if !strings.Contains(shown, f.value) {
				t.Errorf("%s: the Goal page doesn't show %s = %s:\n%s", email, f.field.Name, f.value, shown)
			}
		}
		for _, f := range fields {
			resp := postForm(t, client, post, url.Values{"field_id": {fmt.Sprint(f.field.ID)}, "value": {f.value}, "clear": {"1"}})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s clears %s: status %d", email, f.field.Name, resp.StatusCode)
			}
		}
		values, err := h.Service.GoalFields(context.Background(), goal.ID)
		if err != nil {
			t.Fatalf("GoalFields: %v", err)
		}
		if len(values) != 0 {
			t.Errorf("after %s clears them, values = %+v, want none", email, values)
		}
	}

	h.SetGoalField(pat, goal, fields[0].field, "1200")
	cory := signInClient(t, ts.URL, "cal@example.com")
	page := getBody(t, cory, goalPageURL(ts.URL, goal))
	if !strings.Contains(pageElement(t, page, "ul", "goal-fields"), "1200") {
		t.Errorf("a Contributor doesn't see the Budget value:\n%s", page)
	}
	if strings.Contains(page, action) {
		t.Errorf("a Contributor gets a Field input:\n%s", page)
	}
	resp := postForm(t, cory, post, url.Values{"field_id": {fmt.Sprint(fields[0].field.ID)}, "value": {"9"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a Contributor sets a Field: status %d, want 403", resp.StatusCode)
	}
}

// "abc" in a number Field and "tomorrow" in a date Field are refused, the
// refusal naming the Field.
func TestGoalPageRefusesAFieldValueOfTheWrongType(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	kickoff := h.CreateField(boss, "Kickoff", domain.FieldDate, "")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "pat@example.com")

	for _, tc := range []struct {
		field domain.Field
		value string
	}{{budget, "abc"}, {kickoff, "tomorrow"}} {
		resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/fields", ts.URL, goal.ID),
			url.Values{"field_id": {fmt.Sprint(tc.field.ID)}, "value": {tc.value}})
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, tc.field.Name) {
			t.Errorf("%s = %q: status %d, body %q; want 422 naming %s", tc.field.Name, tc.value, resp.StatusCode, body, tc.field.Name)
		}
	}
}

// The Goal page shows a number with its unit, a short text that is an http(s)
// URL as a link (and one that isn't as plain text), and a long text with its
// line breaks kept.
func TestGoalPageShowsFieldValuesByType(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.SetGoalField(pat, goal, h.CreateField(boss, "Headcount", domain.FieldNumber, "FTE"), "2.5")
	h.SetGoalField(pat, goal, h.CreateField(boss, "Design doc", domain.FieldShortText, ""), "https://example.com/doc?id=7")
	h.SetGoalField(pat, goal, h.CreateField(boss, "Codename", domain.FieldShortText, ""), "javascript:alert(1)")
	h.SetGoalField(pat, goal, h.CreateField(boss, "Notes", domain.FieldLongText, ""), "First line\nSecond line")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "pat@example.com"), goalPageURL(ts.URL, goal))
	shown := pageElement(t, page, "ul", "goal-fields")
	if !strings.Contains(shown, `<span class="num">2.5</span> FTE`) {
		t.Errorf("Headcount isn't shown with its unit:\n%s", shown)
	}
	if !strings.Contains(shown, `href="https://example.com/doc?id=7"`) {
		t.Errorf("the Design doc URL isn't a link:\n%s", shown)
	}
	if plain := tagAround(t, shown, "javascript:alert(1)"); strings.HasPrefix(plain, "<a") {
		t.Errorf("a short text that isn't an http(s) URL should show as plain text:\n%s", shown)
	}
	notes := tagAround(t, shown, `First line`)
	assertStyledBy(t, notes, page, "gp-field-long", "white-space:pre-line")
	if !strings.Contains(shown, "First line\nSecond line") {
		t.Errorf("Notes lost its line break:\n%s", shown)
	}
}

// A Retired Field isn't offered for entry, still shows on a Goal that has a
// value in it, and doesn't show on one that hasn't; restored, it is offered
// again.
func TestRetiredFieldOnTheGoalPage(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	withValue := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	without := h.CreateGoal(pat, "Ship faster", "Releases are slow.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.CreateField(boss, "Notes", domain.FieldLongText, "")
	h.SetGoalField(pat, withValue, budget, "1200")
	if err := h.Service.RetireField(context.Background(), boss.ID, budget.ID); err != nil {
		t.Fatalf("RetireField: %v", err)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "pat@example.com")
	offered := fmt.Sprintf(`name="field_id" value="%d"`, budget.ID)

	page := getBody(t, client, goalPageURL(ts.URL, withValue))
	shown := pageElement(t, page, "ul", "goal-fields")
	if !strings.Contains(shown, "Budget") || !strings.Contains(shown, "1200") || !strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("the Retired Budget isn't shown, marked retired, where it has a value:\n%s", shown)
	}
	if edit := pageElement(t, page, "details", "edit-fields"); strings.Contains(edit, offered) {
		t.Errorf("the Retired Budget is offered for entry:\n%s", edit)
	}
	page = getBody(t, client, goalPageURL(ts.URL, without))
	if strings.Contains(pageElement(t, page, "ul", "goal-fields"), "Budget") {
		t.Errorf("the Retired Budget shows on a Goal without a value in it")
	}

	if err := h.Service.RestoreField(context.Background(), boss.ID, budget.ID); err != nil {
		t.Fatalf("RestoreField: %v", err)
	}
	page = getBody(t, client, goalPageURL(ts.URL, without))
	if edit := pageElement(t, page, "details", "edit-fields"); !strings.Contains(edit, offered) {
		t.Errorf("the restored Budget isn't offered for entry:\n%s", edit)
	}
}

// An Admin marks a Field required from its card, and the card says so, then
// unmarks it; a non-Admin gets no control and is refused (CONTEXT.md:
// Incomplete).
func TestAdminMarksFieldRequiredOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	requiredURL := fmt.Sprintf("%s/fields/%d/required", ts.URL, budget.ID)
	action := fmt.Sprintf(`action="/fields/%d/required"`, budget.ID)

	if page := getBody(t, boss, ts.URL+"/fields"); !strings.Contains(page, action) {
		t.Fatalf("Admin's Field card has no control to mark Budget required:\n%s", page)
	}
	if page := getBody(t, sam, ts.URL+"/fields"); strings.Contains(page, action) {
		t.Errorf("non-Admin gets the required control:\n%s", page)
	}
	if resp := postForm(t, sam, requiredURL, url.Values{"required": {"1"}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin mark required: status %d, want 403", resp.StatusCode)
	}
	if resp := postForm(t, boss, requiredURL, url.Values{"required": {"1"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mark required: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
	if got := pageElement(t, getBody(t, sam, ts.URL+"/fields"), "p", "field-type"); !strings.Contains(got, "Required") {
		t.Errorf("card doesn't say Budget is required: %s", got)
	}

	postForm(t, boss, requiredURL, url.Values{"required": {"0"}})
	if got := pageElement(t, getBody(t, sam, ts.URL+"/fields"), "p", "field-type"); strings.Contains(got, "Required") {
		t.Errorf("card still says Budget is required: %s", got)
	}
}

// Defining a Field named like a Dimension, in another case, is refused saying
// the Dimension has the name, and defines nothing: Dimensions and Fields share
// one namespace.
func TestFieldNamedLikeADimensionRefusedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/fields", url.Values{"name": {"PILLAR"}, "type": {domain.FieldShortText}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(body), "Pillar is already a Dimension's name") {
		t.Errorf("define PILLAR: status %d, body %q; want 422 naming the Dimension", resp.StatusCode, body)
	}
	if fields, err := h.Service.ListFields(context.Background()); err != nil || len(fields) != 0 {
		t.Errorf("Fields = %+v (%v), want none", fields, err)
	}
}
