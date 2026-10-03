package web_test

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

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
// card's Edit toggle, and Retire retires a value at once, with no confirmation
// (#56); a non-Admin gets no toggle.
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
	retire := fmt.Sprintf("/dimension-values/%d/retire", dim.Values[0].ID)
	assertSubmitsAtOnce(t, "Retire", tagAround(t, edit, `action="`+retire+`"`))
	if page := readBody(t, postForm(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+retire, url.Values{})); !strings.Contains(page, ">Growth</del>") {
		t.Errorf("Growth isn't struck through as Retired after Retire:\n%s", page)
	}

	page = getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/dimensions")
	if _, main, _ := strings.Cut(page, "<main"); strings.Contains(main, "<details") {
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

// Behind a Dimension card's Edit toggle an Admin retires the whole Dimension,
// after confirming, and its card is flagged retired with a Restore button that
// brings it back. A Retired value gets a Restore button too. A non-Admin can
// retire or restore neither (CONTEXT.md: Retired).
func TestAdminRetiresAndRestoresDimensionOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	trust := pillar.Values[1]
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, trust.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	retire := fmt.Sprintf("%s/dimensions/%d/retire", ts.URL, pillar.ID)
	restore := fmt.Sprintf("%s/dimensions/%d/restore", ts.URL, pillar.ID)
	restoreValue := fmt.Sprintf("%s/dimension-values/%d/restore", ts.URL, trust.ID)

	card := between(t, getBody(t, admin, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "")
	if !strings.Contains(card, fmt.Sprintf(`action="/dimensions/%d/retire"`, pillar.ID)) || !strings.Contains(card, "confirm(") {
		t.Errorf("Pillar's card has no confirmed Retire for the Dimension; card:\n%s", card)
	}
	if !strings.Contains(card, fmt.Sprintf(`action="/dimension-values/%d/restore"`, trust.ID)) {
		t.Errorf("Retired Trust has no Restore; card:\n%s", card)
	}

	for _, path := range []string{retire, restore, restoreValue} {
		if resp := postForm(t, sam, path, url.Values{}); resp.StatusCode != http.StatusForbidden {
			t.Errorf("non-Admin POST %s: status %d, want 403", path, resp.StatusCode)
		}
	}

	if resp := postForm(t, admin, retire, url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("retire Pillar: status %d", resp.StatusCode)
	}
	card = between(t, getBody(t, admin, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "")
	if !strings.Contains(card, `data-testid="dimension-retired"`) {
		t.Errorf("Retired Pillar's card not flagged retired; card:\n%s", card)
	}
	if !strings.Contains(card, fmt.Sprintf(`action="/dimensions/%d/restore"`, pillar.ID)) {
		t.Errorf("Retired Pillar's card has no Restore; card:\n%s", card)
	}

	postForm(t, admin, restore, url.Values{})
	postForm(t, admin, restoreValue, url.Values{})
	card = between(t, getBody(t, admin, ts.URL+"/dimensions"), `<li data-testid="dimension"`, "")
	if strings.Contains(card, `data-testid="dimension-retired"`) || strings.Contains(card, `data-testid="retired"`) {
		t.Errorf("Pillar and Trust still flagged retired after Restore; card:\n%s", card)
	}
}

// An Admin marks a Dimension required from its card's Edit toggle, and the card
// says so, then unmarks it; a non-Admin is refused (CONTEXT.md: Incomplete).
func TestAdminMarksDimensionRequiredOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	sam := signInClient(t, ts.URL, "sam@example.com")
	requiredURL := fmt.Sprintf("%s/dimensions/%d/required", ts.URL, pillar.ID)

	edit := getBody(t, boss, ts.URL+"/dimensions")
	if !strings.Contains(edit, `action="/dimensions/`+fmt.Sprint(pillar.ID)+`/required"`) {
		t.Fatalf("Edit toggle has no control to mark Pillar required:\n%s", edit)
	}
	if resp := postForm(t, sam, requiredURL, url.Values{"required": {"1"}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-Admin mark required: status %d, want 403", resp.StatusCode)
	}
	if resp := postForm(t, boss, requiredURL, url.Values{"required": {"1"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mark required: status %d; body:\n%s", resp.StatusCode, readBody(t, resp))
	}
	card := pageElement(t, getBody(t, sam, ts.URL+"/dimensions"), "p", "dimension-rules")
	if !strings.Contains(card, "Required") {
		t.Errorf("card doesn't say Pillar is required:\n%s", card)
	}

	postForm(t, boss, requiredURL, url.Values{"required": {"0"}})
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if dims[0].Required {
		t.Errorf("Pillar still required after unmarking")
	}
	card = pageElement(t, getBody(t, sam, ts.URL+"/dimensions"), "p", "dimension-rules")
	if strings.Contains(card, "Required") {
		t.Errorf("card still says Pillar is required:\n%s", card)
	}
}

// On the Dimensions page, defining a Dimension, adding a value or renaming one
// to a name containing a semicolon is refused, saying why. A value named with
// one before that was refused still shows, and an Admin renames it to a name
// without (#101).
func TestDimensionValueContainingASemicolonOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Reliability")
	rnd := h.NameValueWithSemicolon(pillar.Values[1], "R&D; Ops")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	const reason = "can't contain a semicolon, because the import format uses it to separate values"

	if page := getBody(t, client, ts.URL+"/dimensions"); !strings.Contains(html.UnescapeString(page), "R&D; Ops") {
		t.Errorf("Dimensions page doesn't show R&D; Ops:\n%s", page)
	}

	for what, post := range map[string]struct {
		path string
		form url.Values
	}{
		"define": {"/dimensions", url.Values{"name": {"Quarter"}, "values": {"Q1; Q2"}}},
		"add":    {fmt.Sprintf("/dimensions/%d/values", pillar.ID), url.Values{"value": {"Ops; Infra"}}},
		"rename": {fmt.Sprintf("/dimension-values/%d/rename", pillar.Values[0].ID), url.Values{"value": {"Growth; Expansion"}}},
	} {
		resp := postForm(t, client, ts.URL+post.path, post.form)
		if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, reason) {
			t.Errorf("%s: status %d, body %q; want 422 saying why", what, resp.StatusCode, body)
		}
	}

	resp := postForm(t, client, fmt.Sprintf("%s/dimension-values/%d/rename", ts.URL, rnd.ID), url.Values{"value": {"R&D and Ops"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("renaming R&D; Ops: status %d: %s", resp.StatusCode, body)
	}
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if got := dimensionValueNames(dims[0]); len(dims) != 1 || !slices.Equal(got, []string{"Growth", "R&D and Ops"}) {
		t.Errorf("Dimensions = %+v, want only Pillar with [Growth, R&D and Ops]", dims)
	}
}

// Defining a Dimension named like a Field, in another case, is refused saying
// the Field has the name, and defines nothing: Dimensions and Fields share one
// namespace.
func TestDimensionNamedLikeAFieldRefusedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/dimensions", url.Values{"name": {"budget"}, "values": {"Low, High"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(html.UnescapeString(body), "Budget is already a Field's name") {
		t.Errorf("define budget: status %d, body %q; want 422 naming the Field", resp.StatusCode, body)
	}
	if dims, err := h.Service.ListDimensions(context.Background()); err != nil || len(dims) != 0 {
		t.Errorf("Dimensions = %+v (%v), want none", dims, err)
	}
}

// Renaming a value to another value's name in its list, in another case, is
// refused saying to merge them, while changing a value's own capitalisation is
// a rename.
func TestRenamingAValueIntoAnotherRefusedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Payments")
	payments := pillar.Values[1]
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	rename := fmt.Sprintf("%s/dimension-values/%d/rename", ts.URL, payments.ID)

	resp := postForm(t, client, rename, url.Values{"value": {"growth"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "merge Payments into Growth instead") {
		t.Errorf("rename to growth: status %d, body %q; want 422 saying to merge", resp.StatusCode, body)
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Pillar")); !slices.Equal(got, []string{"Growth", "Payments"}) {
		t.Errorf("Pillar after a refused rename = %v, want [Growth Payments]", got)
	}

	resp = postForm(t, client, rename, url.Values{"value": {"payments"}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename to payments: status %d: %s", resp.StatusCode, body)
	}
	if got := dimensionValueNames(dimensionByName(t, h, "Pillar")); !slices.Equal(got, []string{"Growth", "payments"}) {
		t.Errorf("Pillar after recasing Payments = %v, want [Growth payments]", got)
	}
}

// A Retired Dimension's card shows neither the Required mark nor the control
// to change it, since required means nothing on it; restoring the Dimension
// shows both again with the setting it had.
func TestRetiredDimensionCardHasNoRequiredControlOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	h.SetDimensionRequired(admin, pillar, true)
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	action := fmt.Sprintf(`action="/dimensions/%d/required"`, pillar.ID)

	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/retire", ts.URL, pillar.ID), url.Values{})
	page := getBody(t, boss, ts.URL+"/dimensions")
	if strings.Contains(page, `data-testid="dimension-required"`) || strings.Contains(page, action) {
		t.Errorf("Retired Pillar's card shows the required mark or control:\n%s", page)
	}

	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/restore", ts.URL, pillar.ID), url.Values{})
	page = getBody(t, boss, ts.URL+"/dimensions")
	if !strings.Contains(page, `data-testid="dimension-required"`) || !strings.Contains(between(t, page, action, "</form>"), "Make it optional") {
		t.Errorf("restored Pillar's card doesn't show it required, with a control to make it optional:\n%s", page)
	}
}

// After retiring a value, the Dimensions page carries a toast saying so with an
// Undo button, a plain form post to the value's own Undo, not its Restore
// button's route, that restores the value; the toast isn't shown on a later
// visit.
func TestRetiringADimensionValueOffersUndoOnce(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	trust := pillar.Values[1]
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, admin, fmt.Sprintf("%s/dimension-values/%d/retire", ts.URL, trust.ID), url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire Trust: status %d", resp.StatusCode)
	}
	landed := readBody(t, resp)
	toast := pageElement(t, landed, "aside", "toast")
	for _, want := range []string{"Trust", "Pillar", "Undo"} {
		if !strings.Contains(toast, want) {
			t.Errorf("toast missing %q:\n%s", want, toast)
		}
	}
	action := undoAction(t, landed)
	if want := fmt.Sprintf("/dimension-values/%d/undo-retire", trust.ID); action != want {
		t.Errorf("Undo posts to %s, want the value's Undo %s", action, want)
	}

	if later := getBody(t, admin, ts.URL+"/dimensions"); strings.Contains(later, `data-testid="toast"`) {
		t.Errorf("toast shown again on a later visit:\n%s", later)
	}

	if resp := postForm(t, admin, ts.URL+action, toastFields(t, landed)); resp.StatusCode != http.StatusOK {
		t.Fatalf("undo: status %d", resp.StatusCode)
	}
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if dims[0].Values[1].Retired {
		t.Errorf("Trust is still Retired after Undo")
	}
}

// After an Admin merges a value into another from the Dimensions page, a saved
// Report Definition that filtered on the merged value still drafts the same
// Goals: the Goal that carried it, and not the one carrying neither value
// (ticket #71).
func TestMergedValueKeepsASavedReportFilterSelectingTheSameGoalsOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	customer := h.CreateDimension(boss, "Customer", "Acme", "ACME Corp", "Globex")
	acme, acmeCorp, globex := customer.Values[0], customer.Values[1], customer.Values[2]
	carrier := h.ActiveGoal(boss, "Renew ACME", "Revenue depends on renewals.")
	h.AssignGoalValue(carrier, acmeCorp)
	other := h.ActiveGoal(boss, "Pilot Globex", "A new customer.")
	h.AssignGoalValue(other, globex)
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "ACME MBR", DimensionValueIDs: []int64{acmeCorp.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draftURL := fmt.Sprintf("%s/reports/%d", ts.URL, def.ID)

	before := getBody(t, client, draftURL)
	if !strings.Contains(before, carrier.Title) || strings.Contains(before, other.Title) {
		t.Fatalf("before the merge the draft doesn't select just %q; body:\n%s", carrier.Title, before)
	}

	resp := postForm(t, client, fmt.Sprintf("%s/dimension-values/%d/merge", ts.URL, acmeCorp.ID), url.Values{"into": {fmt.Sprint(acme.ID)}})
	if body := readBody(t, resp); resp.StatusCode != http.StatusOK {
		t.Fatalf("merge: status %d: %s", resp.StatusCode, body)
	}

	after := getBody(t, client, draftURL)
	if !strings.Contains(after, carrier.Title) {
		t.Errorf("after the merge the saved filter no longer selects %q; body:\n%s", carrier.Title, after)
	}
	if strings.Contains(after, other.Title) {
		t.Errorf("after the merge the saved filter selects %q; body:\n%s", other.Title, after)
	}
}

// A Retired value isn't offered on the Goal page; once an Admin restores it
// from the Dimensions page it is offered again, and the Owner can set it
// (ticket #72).
func TestRestoredValueIsOfferedAgainOnTheGoalPageOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	trust := pillar.Values[1]
	goal := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	if err := h.Service.RetireDimensionValue(context.Background(), boss.ID, trust.ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")
	owner := signInClient(t, ts.URL, "sam@example.com")
	goalURL := fmt.Sprintf("%s/goals/%d", ts.URL, goal.ID)
	trustOption := fmt.Sprintf(`<option value="%d">Trust</option>`, trust.ID)

	edit := openForm(t, getBody(t, owner, goalURL+"?open=dimensions"), "dimensions")
	if strings.Contains(edit, fmt.Sprintf(`value="%d"`, trust.ID)) {
		t.Fatalf("the Retired Trust is offered on the Goal page:\n%s", edit)
	}

	if resp := postForm(t, admin, fmt.Sprintf("%s/dimension-values/%d/restore", ts.URL, trust.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("restore Trust: status %d", resp.StatusCode)
	}

	edit = openForm(t, getBody(t, owner, goalURL+"?open=dimensions"), "dimensions")
	if !strings.Contains(edit, trustOption) {
		t.Fatalf("the restored Trust isn't offered on the Goal page:\n%s", edit)
	}
	postForm(t, owner, goalURL+"/dimensions", url.Values{"dimension_id": {fmt.Sprint(pillar.ID)}, "value_id": {fmt.Sprint(trust.ID)}})
	shown := pageElement(t, getBody(t, owner, goalURL), "section", "goal-dimensions")
	if !strings.Contains(shown, `data-testid="goal-dimension-value">Trust<`) {
		t.Errorf("the Owner couldn't set the restored Trust:\n%s", shown)
	}
}

// Posting a Retired Dimension's required setting is refused with 422, even a
// post that would change nothing, and leaves it required once restored.
func TestRetiredDimensionsRequiredSettingIsRefusedOverHTTP(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	admin := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(admin, "Pillar", "Growth")
	h.SetDimensionRequired(admin, pillar, true)
	ts := newServer(t, h)
	boss := signInClient(t, ts.URL, "boss@example.com")
	requiredURL := fmt.Sprintf("%s/dimensions/%d/required", ts.URL, pillar.ID)

	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/retire", ts.URL, pillar.ID), url.Values{})
	for _, required := range []string{"0", "1"} {
		if resp := postForm(t, boss, requiredURL, url.Values{"required": {required}}); resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("required=%s on Retired Pillar: status %d, want 422", required, resp.StatusCode)
		}
	}

	postForm(t, boss, fmt.Sprintf("%s/dimensions/%d/restore", ts.URL, pillar.ID), url.Values{})
	if page := getBody(t, boss, ts.URL+"/dimensions"); !strings.Contains(page, `data-testid="dimension-required"`) {
		t.Errorf("restored Pillar isn't required:\n%s", page)
	}
}

// The Retired value's Restore button on the Dimensions page needs no token and
// works at any time, long after the toast's Undo has expired.
func TestTheRetiredValuesRestoreButtonNeedsNoToken(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	trust := pillar.Values[1]
	ts := newServer(t, h)
	admin := signInClient(t, ts.URL, "boss@example.com")
	if resp := postForm(t, admin, fmt.Sprintf("%s/dimension-values/%d/retire", ts.URL, trust.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("retire Trust: status %d", resp.StatusCode)
	}
	h.Clock.Advance(30 * 24 * time.Hour)

	page := getBody(t, admin, ts.URL+"/dimensions")
	restore := fmt.Sprintf(`action="/dimension-values/%d/restore"`, trust.ID)
	if !strings.Contains(page, restore) {
		t.Fatalf("the Dimensions page has no Restore button for Trust:\n%s", page)
	}
	if resp := postForm(t, admin, fmt.Sprintf("%s/dimension-values/%d/restore", ts.URL, trust.ID), url.Values{}); resp.StatusCode != http.StatusOK {
		t.Fatalf("restore Trust: status %d", resp.StatusCode)
	}
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if dims[0].Values[1].Retired {
		t.Error("Trust is still Retired after its Restore button")
	}
}
