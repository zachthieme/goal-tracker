package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The Admin page links to the Fields page, where an Admin defines a Field of
// each type, a number one with its unit, and each is listed with its type.
func TestAdminDefinesFieldsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	row := pageElement(t, getBody(t, boss, ts.URL+"/admin"), "a", "admin-fields")
	for _, want := range []string{`href="/fields"`, "Fields"} {
		if !strings.Contains(row, want) {
			t.Errorf("Admin page's Fields link lacks %s: %s", want, row)
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
		defer func() { _ = resp.Body.Close() }()
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

// The Fields page sits in the flat frame (DESIGN.md § Fields page): its title
// in the page header, and its Fields and the new Field form as plain sections,
// none of them a card.
func TestFieldsPageIsInTheFlatFrame(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	card := regexp.MustCompile(`class="(?:[^"]* )?card(?: [^"]*)?"`)

	empty := getBody(t, client, ts.URL+"/fields")
	if header := between(t, empty, `<div class="page-header">`, "</h1>"); !strings.HasSuffix(header, "<h1>Fields") {
		t.Errorf("the Fields page's header doesn't hold its title:\n%s", header)
	}
	pageElement(t, empty, "li", "no-fields")
	if c := card.FindString(empty); c != "" {
		t.Errorf("the Fields page with none yet has a card, %s:\n%s", c, empty)
	}

	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	page := getBody(t, client, ts.URL+"/fields")
	pageElement(t, page, "li", "field")
	if c := card.FindString(page); c != "" {
		t.Errorf("the Fields page has a card, %s:\n%s", c, page)
	}
}

// A non-Admin sees the Fields but no form to define one, and defining one is
// refused.
func TestNonAdminCannotDefineFields(t *testing.T) {
	t.Parallel()

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
	defer func() { _ = resp.Body.Close() }()
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
	t.Parallel()

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
	if status := postFormStatus(t, sam, ts.URL+retire, nil); status != http.StatusForbidden {
		t.Errorf("non-Admin retire: status %d, want 403", status)
	}

	card := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if form := openTag(between(t, card, `action="`+retire+`"`, "")); !strings.Contains(form, `onsubmit="return confirm(`) {
		t.Errorf("Retire doesn't ask for confirmation: %s", form)
	}
	if status := postFormStatus(t, boss, ts.URL+retire, nil); status != http.StatusOK {
		t.Fatalf("retire: status %d", status)
	}
	card = pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if !strings.Contains(card, `data-testid="field-retired"`) || !strings.Contains(card, `action="`+restore+`"`) {
		t.Errorf("a Retired Field's card isn't flagged retired with a Restore button:\n%s", card)
	}

	if status := postFormStatus(t, sam, ts.URL+restore, nil); status != http.StatusForbidden {
		t.Errorf("non-Admin restore: status %d, want 403", status)
	}
	if status := postFormStatus(t, boss, ts.URL+restore, nil); status != http.StatusOK {
		t.Fatalf("restore: status %d", status)
	}
	card = pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if strings.Contains(card, `data-testid="field-retired"`) || !strings.Contains(card, `action="`+retire+`"`) {
		t.Errorf("a restored Field's card is still retired:\n%s", card)
	}
}

// The Goal page gives its Owner and a Delegate an input of each Field's type,
// and through it they set and clear each value; an Account with no role on the
// Goal sees the values read-only and is refused.
func TestOwnerAndDelegateSetAndClearFieldsOnTheGoalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	dee := h.SignIn("dee@example.com")
	h.SignIn("cal@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	h.AddDelegate(pat, dee, goal.ID)
	fields := []struct {
		field domain.Field
		input string
		value string
		shown string
	}{
		{h.CreateField(boss, "Budget", domain.FieldNumber, "$"), `type="number"`, "1200", "1200"},
		{h.CreateField(boss, "Design doc", domain.FieldShortText, ""), `type="text"`, "In review", "In review"},
		{h.CreateField(boss, "Notes", domain.FieldLongText, ""), `<textarea`, "Owned by infra.", "Owned by infra."},
		{h.CreateField(boss, "Kickoff", domain.FieldDate, ""), `type="date"`, "2026-03-01", "Mar 1"},
	}
	ts := newServer(t, h)
	action := fmt.Sprintf(`action="/goals/%d/fields"`, goal.ID)
	post := ts.URL + fmt.Sprintf("/goals/%d/fields", goal.ID)

	for _, email := range []string{"pat@example.com", "dee@example.com"} {
		client := signInClient(t, ts.URL, email)
		edit := openForm(t, getBody(t, client, goalPageURL(ts.URL, goal)+"?open=fields"), "fields")
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
			defer func() { _ = resp.Body.Close() }()
			if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
				t.Fatalf("%s sets %s: status %d; body:\n%s", email, f.field.Name, resp.StatusCode, body)
			}
		}
		shown := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "ul", "goal-fields")
		for _, f := range fields {
			if !strings.Contains(shown, f.shown) {
				t.Errorf("%s: the Goal page doesn't show %s = %s:\n%s", email, f.field.Name, f.shown, shown)
			}
		}
		for _, f := range fields {
			resp := postForm(t, client, post, url.Values{"field_id": {fmt.Sprint(f.field.ID)}, "value": {f.value}, "clear": {"1"}})
			defer func() { _ = resp.Body.Close() }()
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
	cal := signInClient(t, ts.URL, "cal@example.com")
	page := getBody(t, cal, goalPageURL(ts.URL, goal))
	if !strings.Contains(pageElement(t, page, "ul", "goal-fields"), "1200") {
		t.Errorf("a stranger doesn't see the Budget value:\n%s", page)
	}
	if strings.Contains(page, action) {
		t.Errorf("a stranger gets a Field input:\n%s", page)
	}
	resp := postForm(t, cal, post, url.Values{"field_id": {fmt.Sprint(fields[0].field.ID)}, "value": {"9"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a stranger sets a Field: status %d, want 403", resp.StatusCode)
	}
}

// "abc" in a number Field and "tomorrow" in a date Field are refused, back on
// the Goal page with the Fields form open, the refusal naming the Field
// (#687).
func TestGoalPageRefusesAFieldValueOfTheWrongType(t *testing.T) {
	t.Parallel()

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
		back := fmt.Sprintf("/goals/%d", goal.ID)
		postRefusedBack(t, client, ts.URL+back+"/fields",
			url.Values{"field_id": {fmt.Sprint(tc.field.ID)}, "value": {tc.value}}, back)
		open := goalOpenFormIn(t, getPage(t, client, ts.URL+back), "fields")
		if reason := textOf(findByTestID(t, open, "form-error")); !strings.Contains(reason, tc.field.Name) {
			t.Errorf("%s = %q: the open form's error %q doesn't name %s", tc.field.Name, tc.value, reason, tc.field.Name)
		}
	}
}

// The Goal page shows a number with its unit, a short text that is an http(s)
// URL as a link (and one that isn't as plain text), and a long text with its
// line breaks kept.
func TestGoalPageShowsFieldValuesByType(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

	page := getBody(t, client, goalPageURL(ts.URL, withValue)+"?open=fields")
	shown := pageElement(t, page, "ul", "goal-fields")
	if !strings.Contains(shown, "Budget") || !strings.Contains(shown, "1200") || !strings.Contains(shown, `data-testid="retired"`) {
		t.Errorf("the Retired Budget isn't shown, marked retired, where it has a value:\n%s", shown)
	}
	if edit := openForm(t, page, "fields"); strings.Contains(edit, offered) {
		t.Errorf("the Retired Budget is offered for entry:\n%s", edit)
	}
	page = getBody(t, client, goalPageURL(ts.URL, without))
	if strings.Contains(pageElement(t, page, "ul", "goal-fields"), "Budget") {
		t.Errorf("the Retired Budget shows on a Goal without a value in it")
	}

	if err := h.Service.RestoreField(context.Background(), boss.ID, budget.ID); err != nil {
		t.Fatalf("RestoreField: %v", err)
	}
	page = getBody(t, client, goalPageURL(ts.URL, without)+"?open=fields")
	if edit := openForm(t, page, "fields"); !strings.Contains(edit, offered) {
		t.Errorf("the restored Budget isn't offered for entry:\n%s", edit)
	}
}

// An Admin marks a Field required from its card, and the card says so, then
// unmarks it; a non-Admin gets no control and is refused (CONTEXT.md:
// Incomplete).
func TestAdminMarksFieldRequiredOverHTTP(t *testing.T) {
	t.Parallel()

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
	if status := postFormStatus(t, sam, requiredURL, url.Values{"required": {"1"}}); status != http.StatusForbidden {
		t.Errorf("non-Admin mark required: status %d, want 403", status)
	}
	resp := postForm(t, boss, requiredURL, url.Values{"required": {"1"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark required: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
	if got := pageElement(t, getBody(t, sam, ts.URL+"/fields"), "p", "field-type"); !strings.Contains(got, "Required") {
		t.Errorf("card doesn't say Budget is required: %s", got)
	}

	postFormStatus(t, boss, requiredURL, url.Values{"required": {"0"}})
	if got := pageElement(t, getBody(t, sam, ts.URL+"/fields"), "p", "field-type"); strings.Contains(got, "Required") {
		t.Errorf("card still says Budget is required: %s", got)
	}
}

// Defining a Field named like a Dimension, in another case, comes back to
// the Fields page with Define a Field open, saying the Dimension has the name
// above it and holding what was typed and chosen; a reload shows it closed,
// and nothing is defined: Dimensions and Fields share one namespace (#702).
func TestFieldNamedLikeADimensionRefusedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	postRefusedBack(t, client, ts.URL+"/fields", url.Values{"name": {"PILLAR"}, "type": {domain.FieldDate}, "unit": {"days"}}, "/fields")
	page := getPage(t, client, ts.URL+"/fields")
	form := findByTestID(t, page, "create-field")
	if d := detailsAround(form); d == nil || !hasAttr(d, "open") {
		t.Fatalf("Define a Field isn't open:\n%s", renderNode(page))
	}
	if got := textOf(findByTestID(t, form, "form-error")); got != "Pillar is already a Dimension's name." {
		t.Errorf("Define a Field says %q, want Pillar is already a Dimension's name.", got)
	}
	if name, unit := formField(t, form, "name").Value, formField(t, form, "unit").Value; name != "PILLAR" || unit != "days" {
		t.Errorf("Name holds %q and Unit %q, want PILLAR and days as typed", name, unit)
	}
	if got := checkedRadio(form, "type"); got != "a date" {
		t.Errorf("It holds %q, want a date as chosen", got)
	}

	page = getPage(t, client, ts.URL+"/fields")
	if d := detailsAround(findByTestID(t, page, "create-field")); d == nil || hasAttr(d, "open") || hasTestID(page, "form-error") {
		t.Errorf("a reload still shows Define a Field open, or its reason:\n%s", renderNode(page))
	}
	if fields, err := h.Service.ListFields(context.Background()); err != nil || len(fields) != 0 {
		t.Errorf("Fields = %+v (%v), want none", fields, err)
	}
}

// When the Goal's history can't be written, setting a Field on the Goal page
// fails and the Goal keeps the value it had.
func TestFailedHistoryLeavesTheFieldOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.SetGoalField(pat, goal, budget, "200")
	h.FailWrites(testsupport.Insert, "goal_value_changes")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "pat@example.com")

	resp := postForm(t, client, fmt.Sprintf("%s/goals/%d/fields", ts.URL, goal.ID), url.Values{"field_id": {fmt.Sprint(budget.ID)}, "value": {"350"}})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("setting Budget: status %d, want 500; body:\n%s", resp.StatusCode, body)
	}
	if shown := pageElement(t, getBody(t, client, goalPageURL(ts.URL, goal)), "ul", "goal-fields"); !strings.Contains(shown, "200") || strings.Contains(shown, "350") {
		t.Errorf("the Goal doesn't keep Budget 200 after a failed change:\n%s", shown)
	}
}

// A Retired Field's card shows neither the Required mark nor the control to
// change it, since required means nothing on it; restoring the Field shows
// both again with the setting it had.
func TestRetiredFieldCardHasNoRequiredControlOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(admin, budget, true)
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	action := fmt.Sprintf(`action="/fields/%d/required"`, budget.ID)

	postFormStatus(t, boss, fmt.Sprintf("%s/fields/%d/retire", ts.URL, budget.ID), url.Values{})
	card := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if strings.Contains(card, `data-testid="field-required"`) || strings.Contains(card, action) || strings.Contains(card, "Make it") {
		t.Errorf("Retired Budget's card shows the required mark or control:\n%s", card)
	}

	postFormStatus(t, boss, fmt.Sprintf("%s/fields/%d/restore", ts.URL, budget.ID), url.Values{})
	card = pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field")
	if !strings.Contains(card, `data-testid="field-required"`) || !strings.Contains(between(t, card, action, "</form>"), "Make it optional") {
		t.Errorf("restored Budget's card doesn't show it required, with a control to make it optional:\n%s", card)
	}
}

// An Admin who is neither the Goal's Owner nor a Delegate gets the Field inputs
// on the Goal page and can set a Field there (ticket #73).
func TestAdminWhoIsNeitherOwnerNorDelegateSetsAFieldOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pat := h.SignIn("pat@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	goal := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")

	edit := openForm(t, getBody(t, admin, goalPageURL(ts.URL, goal)+"?open=fields"), "fields")
	if !strings.Contains(edit, fmt.Sprintf(`name="field_id" value="%d"`, budget.ID)) {
		t.Fatalf("the Admin isn't offered the Budget input:\n%s", edit)
	}

	resp := postForm(t, admin, fmt.Sprintf("%s/goals/%d/fields", ts.URL, goal.ID), url.Values{"field_id": {fmt.Sprint(budget.ID)}, "value": {"1200"}})
	defer func() { _ = resp.Body.Close() }()
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("the Admin sets Budget: status %d; body:\n%s", resp.StatusCode, body)
	}
	shown := pageElement(t, getBody(t, admin, goalPageURL(ts.URL, goal)), "ul", "goal-fields")
	if !strings.Contains(shown, "1200") {
		t.Errorf("the Goal page doesn't show the Admin's Budget = 1200:\n%s", shown)
	}
}

// Posting a Retired Field's required setting is refused with 422, even a post
// that would change nothing, and leaves it required once restored.
func TestRetiredFieldsRequiredSettingIsRefusedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	budget := h.CreateField(admin, "Budget", domain.FieldNumber, "$")
	h.SetFieldRequired(admin, budget, true)
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	requiredURL := fmt.Sprintf("%s/fields/%d/required", ts.URL, budget.ID)

	postFormStatus(t, boss, fmt.Sprintf("%s/fields/%d/retire", ts.URL, budget.ID), url.Values{})
	for _, required := range []string{"0", "1"} {
		if status := postFormStatus(t, boss, requiredURL, url.Values{"required": {required}}); status != http.StatusUnprocessableEntity {
			t.Errorf("required=%s on Retired Budget: status %d, want 422", required, status)
		}
	}

	postFormStatus(t, boss, fmt.Sprintf("%s/fields/%d/restore", ts.URL, budget.ID), url.Values{})
	if card := pageElement(t, getBody(t, boss, ts.URL+"/fields"), "li", "field"); !strings.Contains(card, `data-testid="field-required"`) {
		t.Errorf("restored Budget isn't required:\n%s", card)
	}
}

// A date Field's value shows as a date: "Mar 1" on the Goal page and in the
// Goal table, "Mar 1, 2026" on a Report, while its input still holds it as
// typed (ticket #276).
func TestADateFieldShowsInTheDateFormat(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	kickoff := h.CreateField(boss, "Kickoff", domain.FieldDate, "")
	goal := h.ActiveGoal(boss, "Launch in EU", "Expand the market.")
	h.SetGoalField(boss, goal, kickoff, "2026-03-01")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{goal.ID}, FieldIDs: []int64{kickoff.ID}})
	pub := h.PublishReport(boss, def)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	page := getBody(t, client, goalPageURL(ts.URL, goal)+"?open=fields")
	if got := pageElement(t, page, "span", "goal-field-value"); !strings.HasSuffix(got, ">Mar 1") {
		t.Errorf("the Goal page shows the Kickoff as %s, want Mar 1", got)
	}
	if form := between(t, openForm(t, page, "fields"), fmt.Sprintf(`name="field_id" value="%d"`, kickoff.ID), "</form>"); !strings.Contains(form, `value="2026-03-01"`) {
		t.Errorf("the Kickoff input doesn't hold 2026-03-01:\n%s", form)
	}
	if row := between(t, getBody(t, client, ts.URL+"/goals?layout=table"), "<tbody", "</tbody>"); !strings.Contains(row, ">Mar 1<") {
		t.Errorf("the Goal table doesn't show the Kickoff as Mar 1:\n%s", row)
	}
	pubPage := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/publications/"+strconv.FormatInt(pub.ID, 10))
	if fields := pageElement(t, pubPage, "dl", "report-fields"); !strings.Contains(fields, "Mar 1, 2026") {
		t.Errorf("the Report doesn't show the Kickoff as Mar 1, 2026:\n%s", fields)
	}
}

// A refused Field change answers with the refusal page, headed Can't change
// Field and linking Back to the Fields page, at its refusal's status: one
// that isn't there is 422, as the domain refuses it for its input, and
// someone not an Admin is 403 (#573). A button with nothing typed, such as
// Retire, answers so, and so does Define a Field refused with more typed than
// can be carried back to it (#702).
func TestARefusedFieldChangeAnswersWithTheRefusalPage(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.SignIn("sam@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)

	for _, c := range []struct {
		name, who, path string
		posted          url.Values
		status          int
		reason          string
	}{
		{"a Field that isn't there", "boss@example.com", "/fields/99999/retire", url.Values{}, http.StatusUnprocessableEntity, "Field does not exist."},
		{"someone not an Admin", "sam@example.com", fmt.Sprintf("/fields/%d/retire", budget.ID), url.Values{}, http.StatusForbidden, "Only an Admin may do this."},
		{"a definition too long to carry", "boss@example.com", "/fields", url.Values{"name": {strings.Repeat("Budget ", 500)}, "type": {"money"}},
			http.StatusUnprocessableEntity, "A Field holds a number, a short text, a long text or a date."},
	} {
		resp := postForm(t, noRedirects(signInClient(t, ts.URL, c.who)), ts.URL+c.path, c.posted)
		defer func() { _ = resp.Body.Close() }()
		page := parsePage(t, readBody(t, resp))
		if resp.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.status)
		}
		assertRefusalPage(t, page, "Can't change Field", c.reason, "/fields")
	}
}
