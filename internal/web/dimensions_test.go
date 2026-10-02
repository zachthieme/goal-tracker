package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
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

// An Admin chooses on the create form whether a Dimension's list is Fixed or
// Extendable, defaulting to Fixed, and switches it either way from its card;
// each card says which it is. A non-Admin can't switch it (CONTEXT.md: Fixed,
// Extendable).
func TestAdminMarksDimensionExtendableAndFixedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")

	form := pageElement(t, getBody(t, boss, ts.URL+"/dimensions"), "details", "create-dimension")
	if !strings.Contains(form, `name="list" value="fixed" checked`) || !strings.Contains(form, `name="list" value="extendable"`) {
		t.Fatalf("create form lacks a Fixed/Extendable choice defaulting to Fixed:\n%s", form)
	}

	postForm(t, boss, ts.URL+"/dimensions", url.Values{"name": {"Customer"}, "values": {"Acme"}, "list": {"extendable"}})
	postForm(t, boss, ts.URL+"/dimensions", url.Values{"name": {"Pillar"}, "values": {"Growth"}})
	lists := map[string]string{}
	var pillarID int64
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		lists[d.Name] = d.List
		if d.Name == "Pillar" {
			pillarID = d.ID
		}
	}
	if lists["Customer"] != domain.ListExtendable || lists["Pillar"] != domain.ListFixed {
		t.Fatalf("lists = %v, want Customer extendable and Pillar fixed", lists)
	}

	page := getBody(t, boss, ts.URL+"/dimensions")
	if !strings.Contains(page, `data-testid="dimension-list">Extendable`) || !strings.Contains(page, `data-testid="dimension-list">Fixed`) {
		t.Errorf("cards don't say Fixed / Extendable:\n%s", page)
	}
	listURL := fmt.Sprintf("%s/dimensions/%d/list", ts.URL, pillarID)
	if !strings.Contains(page, fmt.Sprintf(`action="/dimensions/%d/list"`, pillarID)) {
		t.Fatalf("Pillar card lacks a form posting to its list switch:\n%s", page)
	}

	for _, want := range []string{domain.ListExtendable, domain.ListFixed} {
		resp := postForm(t, boss, listURL, url.Values{"list": {want}})
		_ = readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("switch Pillar to %s: status %d", want, resp.StatusCode)
		}
		if d := dimensionByName(t, h, "Pillar"); d.List != want {
			t.Errorf("Pillar List = %q, want %q", d.List, want)
		}
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	resp := postForm(t, sam, listURL, url.Values{"list": {"extendable"}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin switch: status %d, want 403", resp.StatusCode)
	}
	if d := dimensionByName(t, h, "Pillar"); d.Extendable() {
		t.Errorf("Pillar became Extendable after a non-Admin's switch")
	}
}

func dimensionByName(t *testing.T, h *testsupport.Harness, name string) domain.Dimension {
	t.Helper()
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no Dimension named %q", name)
	return domain.Dimension{}
}

// Behind a Dimension card's Edit toggle an Admin moves each value up or down
// and sorts the list alphabetically, and the card lists the values in that
// order; a non-Admin's attempts are refused.
func TestAdminReordersDimensionValuesOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	dim := h.CreateDimension(boss, "Pillar", "Reliability", "Growth", "Efficiency")
	reliability, growth, efficiency := dim.Values[0], dim.Values[1], dim.Values[2]
	ts := newServer(t, h)
	bossClient := signInClient(t, ts.URL, "boss@example.com")
	order := func() string {
		card := between(t, getBody(t, bossClient, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "<details")
		return nameOrder(card, "Reliability", "Growth", "Efficiency")
	}

	edit := between(t, getBody(t, bossClient, ts.URL+"/dimensions"), `<details class="dm-edit"`, "</details>")
	for _, v := range dim.Values {
		move := fmt.Sprintf(`action="/dimension-values/%d/move"`, v.ID)
		if !strings.Contains(edit, move) {
			t.Errorf("the Edit toggle lacks %s's move forms:\n%s", v.Value, edit)
		}
	}
	for _, label := range []string{`aria-label="Move Growth up"`, `aria-label="Move Growth down"`, ">Sort alphabetically<"} {
		if !strings.Contains(edit, label) {
			t.Errorf("the Edit toggle lacks %q:\n%s", label, edit)
		}
	}
	if !strings.Contains(edit, fmt.Sprintf(`action="/dimensions/%d/sort"`, dim.ID)) {
		t.Errorf("the Edit toggle lacks the sort form:\n%s", edit)
	}

	resp := postForm(t, bossClient, fmt.Sprintf("%s/dimension-values/%d/move", ts.URL, efficiency.ID), url.Values{"direction": {"up"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("move up: status %d: %s", resp.StatusCode, body)
	}
	if got, want := order(), "Reliability Efficiency Growth"; got != want {
		t.Errorf("after moving Efficiency up = %q, want %q", got, want)
	}
	postForm(t, bossClient, fmt.Sprintf("%s/dimension-values/%d/move", ts.URL, reliability.ID), url.Values{"direction": {"down"}})
	if got, want := order(), "Efficiency Reliability Growth"; got != want {
		t.Errorf("after moving Reliability down = %q, want %q", got, want)
	}
	postForm(t, bossClient, fmt.Sprintf("%s/dimensions/%d/sort", ts.URL, dim.ID), url.Values{})
	if got, want := order(), "Efficiency Growth Reliability"; got != want {
		t.Errorf("after sorting = %q, want %q", got, want)
	}

	sam := signInClient(t, ts.URL, "sam@example.com")
	if resp := postForm(t, sam, fmt.Sprintf("%s/dimension-values/%d/move", ts.URL, growth.ID), url.Values{"direction": {"up"}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin move: status %d, want 403", resp.StatusCode)
	}
	if resp := postForm(t, sam, fmt.Sprintf("%s/dimensions/%d/sort", ts.URL, dim.ID), url.Values{}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin sort: status %d, want 403", resp.StatusCode)
	}
}

// nameOrder returns which of names appear in page, in the order each first
// appears, space-separated. The names must not contain one another.
func nameOrder(page string, names ...string) string {
	type seen struct {
		at   int
		name string
	}
	var found []seen
	for _, n := range names {
		if at := strings.Index(page, n); at >= 0 {
			found = append(found, seen{at, n})
		}
	}
	slices.SortFunc(found, func(a, b seen) int { return a.at - b.at })
	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, f.name)
	}
	return strings.Join(out, " ")
}

// The Admin's order of a Dimension's values is the order they are listed in
// on the Goal page, in the Goal list's filter, and as its groups.
func TestValuesListInAdminsOrderEverywhere(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	dim := h.CreateSeveralValuesDimension(boss, "Pillar", "Reliability", "Growth", "Efficiency")
	if err := h.Service.MoveDimensionValue(context.Background(), boss.ID, dim.Values[2].ID, domain.MoveUp); err != nil {
		t.Fatalf("MoveDimensionValue: %v", err)
	}
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	for _, v := range dim.Values {
		h.AssignGoalValue(goal, v)
	}
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "sam@example.com")
	const want = "Reliability Efficiency Growth"

	goalPage := getBody(t, client, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID))
	if got := nameOrder(pageElement(t, goalPage, "section", "goal-dimensions"), "Reliability", "Growth", "Efficiency"); got != want {
		t.Errorf("Goal page values = %q, want %q", got, want)
	}
	filter := pageElement(t, getBody(t, client, ts.URL+"/goals"), "details", "more-filters")
	if got := nameOrder(filter, "Reliability", "Growth", "Efficiency"); got != want {
		t.Errorf("filter values = %q, want %q", got, want)
	}
	grouped := getBody(t, client, fmt.Sprintf("%s/goals?group=%d", ts.URL, dim.ID))
	var labels []string
	for _, group := range strings.Split(grouped, `<tbody data-testid="goal-group"`)[1:] {
		labels = append(labels, nameOrder(group[:strings.Index(group, "</th>")+5], "Reliability", "Growth", "Efficiency"))
	}
	if got := strings.Join(labels, " "); got != want {
		t.Errorf("groups = %q, want %q", got, want)
	}
}

// Behind a Dimension card's Edit toggle an Admin merges a value into another
// of the same Dimension's values, after confirming: the merged value leaves
// the list and the Goals carrying it show the target instead, once if they had
// both. Merging across Dimensions is refused, and so is a non-Admin's merge.
func TestAdminMergesDimensionValueOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	customer := h.CreateSeveralValuesDimension(boss, "Customer", "Acme", "ACME Corp", "Globex")
	acme, acmeCorp, globex := customer.Values[0], customer.Values[1], customer.Values[2]
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, acme)
	h.AssignGoalValue(goal, acmeCorp)
	ts := newServer(t, h)
	bossClient := signInClient(t, ts.URL, "boss@example.com")
	mergeURL := fmt.Sprintf("%s/dimension-values/%d/merge", ts.URL, acmeCorp.ID)

	edit := between(t, getBody(t, bossClient, ts.URL+"/dimensions"), `<details class="dm-edit"`, "</details>")
	merge := between(t, edit, fmt.Sprintf(`action="/dimension-values/%d/merge"`, acmeCorp.ID), "</form>")
	if !strings.Contains(openTag(merge), `onsubmit="return confirm(`) {
		t.Errorf("Merge doesn't ask for confirmation: %s", openTag(merge))
	}
	for _, v := range []domain.DimensionValue{acme, globex} {
		if !strings.Contains(merge, fmt.Sprintf(`<option value="%d">%s</option>`, v.ID, v.Value)) {
			t.Errorf("ACME Corp's merge form doesn't offer %s:\n%s", v.Value, merge)
		}
	}
	if strings.Contains(merge, fmt.Sprintf(`<option value="%d">`, acmeCorp.ID)) {
		t.Errorf("ACME Corp's merge form offers merging it into itself:\n%s", merge)
	}

	sam2 := signInClient(t, ts.URL, "sam@example.com")
	if resp := postForm(t, sam2, mergeURL, url.Values{"into": {fmt.Sprint(acme.ID)}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin merge: status %d, want 403", resp.StatusCode)
	}
	if resp := postForm(t, bossClient, mergeURL, url.Values{"into": {fmt.Sprint(pillar.Values[0].ID)}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("merge across Dimensions: status %d, want 422", resp.StatusCode)
	}

	resp := postForm(t, bossClient, mergeURL, url.Values{"into": {fmt.Sprint(acme.ID)}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("merge: status %d: %s", resp.StatusCode, body)
	}
	card := between(t, getBody(t, bossClient, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "<details")
	if strings.Contains(card, "ACME Corp") {
		t.Errorf("the merged value is still listed:\n%s", card)
	}
	section := pageElement(t, getBody(t, sam2, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "section", "goal-dimensions")
	if n := strings.Count(section, `data-testid="goal-dimension-value">Acme<`); n != 1 || strings.Contains(section, "ACME Corp") {
		t.Errorf("Goal page shows Acme %d times (want once) or still ACME Corp:\n%s", n, section)
	}
}

// When a merge fails part-way, the request fails and nothing changes: the
// merged value stays listed and on its Goals.
func TestFailedMergeChangesNothingOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	customer := h.CreateDimension(boss, "Customer", "Acme", "Globex")
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	h.AssignGoalValue(goal, customer.Values[1])
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_merge BEFORE DELETE ON dimension_values
		BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	ts := newServer(t, h)
	bossClient := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, bossClient, fmt.Sprintf("%s/dimension-values/%d/merge", ts.URL, customer.Values[1].ID),
		url.Values{"into": {fmt.Sprint(customer.Values[0].ID)}})
	_ = readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("failed merge: status %d, want 500", resp.StatusCode)
	}

	card := between(t, getBody(t, bossClient, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "<details")
	if got, want := nameOrder(card, "Acme", "Globex"), "Acme Globex"; got != want {
		t.Errorf("list after a failed merge = %q, want %q", got, want)
	}
	section := pageElement(t, getBody(t, bossClient, fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)), "section", "goal-dimensions")
	if !strings.Contains(section, `data-testid="goal-dimension-value">Globex<`) {
		t.Errorf("the Goal lost Globex after a failed merge:\n%s", section)
	}
}
