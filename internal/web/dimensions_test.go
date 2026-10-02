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

// An Admin defines a Dimension, adds a value, renames one, and retires one from
// the Dimensions page; a retired value stays listed, flagged retired.
func TestAdminManagesDimensionsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	// Create a Dimension with two values.
	resp := postForm(t, boss, ts.URL+"/dimensions", url.Values{
		"name":   {"Pillar"},
		"values": {"Growth, Reliability"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create dimension: status %d", resp.StatusCode)
	}
	page := readBody(t, resp)
	if !strings.Contains(page, "Pillar") || !strings.Contains(page, "Growth") || !strings.Contains(page, "Reliability") {
		t.Fatalf("Dimensions page missing the new Dimension; body:\n%s", page)
	}

	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	dim := dims[0]
	var growth, reliability int64
	for _, v := range dim.Values {
		switch v.Value {
		case "Growth":
			growth = v.ID
		case "Reliability":
			reliability = v.ID
		}
	}

	// Add a value.
	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/values", ts.URL, dim.ID), url.Values{"value": {"Efficiency"}})
	// Rename a value.
	postForm(t, boss, fmt.Sprintf("%s/dimension-values/%d/rename", ts.URL, growth), url.Values{"value": {"Expansion"}})
	// Retire a value.
	postForm(t, boss, fmt.Sprintf("%s/dimension-values/%d/retire", ts.URL, reliability), url.Values{})

	page = getBody(t, boss, ts.URL+"/dimensions")
	for _, want := range []string{"Efficiency", "Expansion"} {
		if !strings.Contains(page, want) {
			t.Errorf("Dimensions page missing %q after edits; body:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Growth") {
		t.Errorf("renamed-away value Growth still shown; body:\n%s", page)
	}
	if !strings.Contains(page, `data-testid="retired"`) {
		t.Errorf("retired value not flagged; body:\n%s", page)
	}
}

// A non-Admin can see Dimensions but gets no management controls, and the API
// refuses their attempt to create one.
func TestNonAdminCannotManageDimensions(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Pillar", "Growth")
	ts := newServer(t, h)

	sam := signInClient(t, ts.URL, "sam@example.com")
	page := getBody(t, sam, ts.URL+"/dimensions")
	if !strings.Contains(page, "Pillar") {
		t.Errorf("non-Admin cannot see Dimensions; body:\n%s", page)
	}
	if strings.Contains(page, `data-testid="create-dimension"`) {
		t.Errorf("non-Admin should not see the create-Dimension form; body:\n%s", page)
	}
	resp := postForm(t, sam, ts.URL+"/dimensions", url.Values{"name": {"Quarter"}, "values": {"Q1"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin create dimension: status %d, want 403", resp.StatusCode)
	}
}

// Each Dimension is a card with its values as tags, a retired one struck
// through. An Admin's rename, retire, and add-value controls sit behind the
// card's Edit toggle, and Retire asks for confirmation; a non-Admin gets no
// toggle.
func TestDimensionCardsHideAdminControlsBehindEdit(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	dim := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, dim.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/dimensions")
	card := between(t, page, `<li data-testid="dimension"`, "")
	if !strings.Contains(openTag(card), `class="card`) {
		t.Errorf("the Dimension isn't a card: %s", openTag(card))
	}
	if !strings.Contains(card, `class="tag">Growth<`) {
		t.Errorf("the value Growth isn't a tag:\n%s", card)
	}
	if !strings.Contains(card, `<del class="tag`) || !strings.Contains(card, ">Reliability</del>") {
		t.Errorf("the retired value Reliability isn't a struck tag:\n%s", card)
	}
	at := strings.Index(card, "<details")
	if at < 0 {
		t.Fatalf("the Admin's Dimension card has no Edit toggle:\n%s", card)
	}
	edit := between(t, card, "<details", "</details>")
	if !strings.Contains(edit, ">Edit</summary>") {
		t.Errorf("the Edit toggle isn't labelled Edit:\n%s", edit)
	}
	for _, action := range []string{
		fmt.Sprintf("/dimension-values/%d/rename", dim.Values[0].ID),
		fmt.Sprintf("/dimension-values/%d/retire", dim.Values[0].ID),
		fmt.Sprintf("/dimensions/%d/values", dim.ID),
	} {
		if !strings.Contains(edit, `action="`+action+`"`) {
			t.Errorf("the Edit toggle lacks the %s form:\n%s", action, edit)
		}
	}
	if strings.Contains(card[:at], "<form") {
		t.Errorf("an Admin control sits outside the Edit toggle:\n%s", card)
	}
	retire := between(t, edit, fmt.Sprintf(`action="/dimension-values/%d/retire"`, dim.Values[0].ID), "")
	if !strings.Contains(openTag(retire), `onsubmit="return confirm(`) {
		t.Errorf("Retire doesn't ask for confirmation: %s", openTag(retire))
	}

	page = getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/dimensions")
	if strings.Contains(page, "<details") {
		t.Errorf("a non-Admin gets the Edit toggle:\n%s", page)
	}
}

// The new Dimension form sits a gap below its button through the page's own
// class rather than a style attribute.
func TestNewDimensionFormSpacingComesFromAClass(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/dimensions")
	assertStyledBy(t, tagAround(t, page, `action="/dimensions"`), page, "dm-new", "margin-top:12px")
}

// An Admin chooses on the create form whether a Goal takes one value or
// several, defaulting to one, and switches a one-value Dimension to several
// from its card; each card says which it takes.
func TestAdminChoosesOneOrSeveralValuesOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	form := pageElement(t, getBody(t, boss, ts.URL+"/dimensions"), "details", "create-dimension")
	if !strings.Contains(form, `name="selection" value="one" checked`) || !strings.Contains(form, `name="selection" value="several"`) {
		t.Fatalf("create form lacks a one/several choice defaulting to one:\n%s", form)
	}

	postForm(t, boss, ts.URL+"/dimensions", url.Values{"name": {"Team"}, "values": {"Core, Infra"}, "selection": {"several"}})
	postForm(t, boss, ts.URL+"/dimensions", url.Values{"name": {"Pillar"}, "values": {"Growth"}})
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	selection := map[string]string{}
	var pillarID int64
	for _, d := range dims {
		selection[d.Name] = d.Selection
		if d.Name == "Pillar" {
			pillarID = d.ID
		}
	}
	if selection["Team"] != domain.SelectionSeveral || selection["Pillar"] != domain.SelectionOne {
		t.Fatalf("selections = %v, want Team several and Pillar one", selection)
	}

	page := getBody(t, boss, ts.URL+"/dimensions")
	if !strings.Contains(page, `data-testid="dimension-selection">several values`) || !strings.Contains(page, `data-testid="dimension-selection">one value`) {
		t.Errorf("cards don't say one value / several values:\n%s", page)
	}
	action := fmt.Sprintf(`action="/dimensions/%d/selection"`, pillarID)
	if !strings.Contains(page, action) {
		t.Fatalf("Pillar card lacks a form posting to %s:\n%s", action, page)
	}

	resp := postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/selection", ts.URL, pillarID), url.Values{"selection": {"several"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch to several: status %d", resp.StatusCode)
	}
	_ = readBody(t, resp)
	dims, err = h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.Name == "Pillar" && d.Selection != domain.SelectionSeveral {
			t.Errorf("Pillar Selection = %q after switch, want several", d.Selection)
		}
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	resp = postForm(t, sam, fmt.Sprintf("%s/dimensions/%d/selection", ts.URL, pillarID), url.Values{"selection": {"one"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin switch: status %d, want 403", resp.StatusCode)
	}
}

// Switching several to one is refused while a Goal carries more than one value,
// and the refusal names those Goals; it succeeds once none does.
func TestSwitchToOneValueRefusalNamesGoalsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	teams := h.CreateSeveralValuesDimension(boss, "Team", "Core", "Infra")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, teams.Values[0])
	h.AssignGoalValue(goal, teams.Values[1])
	ts := newServer(t, h)
	bossClient := signInClient(t, ts.URL, "boss@example.com")
	switchURL := fmt.Sprintf("%s/dimensions/%d/selection", ts.URL, teams.ID)

	resp := postForm(t, bossClient, switchURL, url.Values{"selection": {"one"}})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("refused switch: status %d, want 422", resp.StatusCode)
	}
	refusal := pageElement(t, body, "section", "selection-refusal")
	if !strings.Contains(refusal, fmt.Sprintf(`href="/goals/%d"`, goal.ID)) || !strings.Contains(refusal, "Reduce outages") {
		t.Errorf("refusal doesn't name the Goal:\n%s", refusal)
	}

	if err := h.Service.SetGoalValues(context.Background(), sam.ID, goal.ID, teams.ID, []int64{teams.Values[0].ID}); err != nil {
		t.Fatalf("SetGoalValues: %v", err)
	}
	resp = postForm(t, bossClient, switchURL, url.Values{"selection": {"one"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("switch once no Goal carries several: status %d, want 200", resp.StatusCode)
	}
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if dims[0].Selection != domain.SelectionOne {
		t.Errorf("Team Selection = %q, want one", dims[0].Selection)
	}
}
