package web_test

import (
	"context"
	"html"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// The builder saves a picked definition: the Goals picked by hand, whatever
// links them, and the post answers 303 to the new Report's draft, which
// selects exactly those Goals (ADR 0007).
func TestBuilderSavesAPickedDefinitionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	parent := h.ActiveGoal(boss, "Grow revenue", "The org needs to grow.")
	child := h.ActiveChildOf(boss, parent, "Launch in EU", "Expand the market.")
	h.ActiveGoal(boss, "Cut churn", "Keep customers.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	if form := getBody(t, client, ts.URL+"/reports/new"); !strings.Contains(form, `action="/reports/new"`) {
		t.Fatalf("/reports/new has no builder form; body:\n%s", form)
	}
	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":   {"EU MBR"},
		"mode":   {domain.ReportModePicked},
		"picked": {strconv.FormatInt(child.ID, 10)},
	})
	draft := assertSavedReport(t, client, ts.URL, resp)
	if got, want := draftGoalTitles(t, draft), []string{child.Title}; !slices.Equal(got, want) {
		t.Errorf("the picked draft selects %q, want %q", got, want)
	}
}

// The builder saves a rules definition: the Goals meeting every rule, any
// value of a rule, with Also include added and Leave out taken away. A Leave
// out Goal that no rule matches is kept, and harmless (CONTEXT.md: Report
// rule).
func TestBuilderSavesARulesDefinitionWithAlsoIncludeAndLeaveOutOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Identity", "Mobile")
	platform, identity, mobile := team.Values[0], team.Values[1], team.Values[2]
	goal := func(owner domain.Account, title string, value domain.DimensionValue) domain.Goal {
		g := h.ActiveGoal(owner, title, "Matters.")
		h.AssignGoalValue(g, value)
		return g
	}
	reliability := goal(boss, "Platform reliability", platform)
	sso := goal(boss, "Legacy SSO cleanup", identity)
	sdk := goal(boss, "Mobile SDK auth update", mobile)
	goal(sam, "Edge cache rollout", platform)
	app := goal(boss, "Mobile app redesign", mobile)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	dim := "dimension:" + id(team.ID)

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"Platform MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIsAnyOf},
		"rules[0].value":     {dim + "=" + id(platform.ID), dim + "=" + id(identity.ID)},
		"rules[1].attribute": {domain.RuleOwner},
		"rules[1].op":        {domain.RuleIs},
		"rules[1].value":     {domain.RuleOwner + "=" + id(boss.ID)},
		"include":            {id(sdk.ID)},
		"exclude":            {id(sso.ID), id(app.ID)},
	})
	draft := assertSavedReport(t, client, ts.URL, resp)
	want := []string{sdk.Title, reliability.Title}
	slices.Sort(want)
	if got := draftGoalTitles(t, draft); !slices.Equal(got, want) {
		t.Errorf("the rules draft selects %q, want %q", got, want)
	}
}

// A refused save answers 422 and comes back as typed: the name, the
// introduction, the mode, each rule row and each listed Goal, with every
// problem beside its input and nothing saved.
func TestBuilderRefusalKeepsValuesAndShowsEachErrorBesideItsInputOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Identity")
	platform := team.Values[0]
	sdk := h.ActiveGoal(boss, "Mobile SDK auth update", "Matters.")
	sso := h.ActiveGoal(boss, "Legacy SSO cleanup", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	dim := "dimension:" + id(team.ID)

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"   "},
		"introduction":       {"Where Platform stands."},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {dim + "=" + id(platform.ID)},
		"rules[1].attribute": {domain.RuleLifecycle},
		"rules[1].op":        {domain.RuleIs},
		"include":            {id(sdk.ID)},
		"exclude":            {id(sso.ID)},
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused save: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	builder := pageElement(t, page, "form", "report-builder")
	for _, want := range []string{
		`name="introduction">Where Platform stands.</textarea>`,
		`value="rules" checked`,
		`<option value="` + dim + `" selected>Team</option>`,
		`<option value="` + dim + "=" + id(platform.ID) + `" selected>Platform</option>`,
		`<option value="lifecycle" selected>Lifecycle</option>`,
		`<input type="hidden" name="include" value="` + id(sdk.ID) + `">`,
		`<input type="hidden" name="exclude" value="` + id(sso.ID) + `">`,
	} {
		if !strings.Contains(builder, want) {
			t.Errorf("the refused builder lost %s:\n%s", want, builder)
		}
	}
	nameField := between(t, builder, `<span>Name</span>`, `</label>`)
	if !strings.Contains(nameField, `data-testid="input-error"`) {
		t.Errorf("the blank name's error isn't beside it:\n%s", nameField)
	}
	rows := strings.Split(builder, `data-testid="report-rule"`)[1:]
	if len(rows) != 2 {
		t.Fatalf("the refused builder shows %d rule rows, want 2:\n%s", len(rows), builder)
	}
	if strings.Contains(rows[0], `data-testid="input-error"`) {
		t.Errorf("the good Team rule is marked refused:\n%s", rows[0])
	}
	if !strings.Contains(rows[1], `id="rules[1]-error"`) || !strings.Contains(rows[1], "needs a value") {
		t.Errorf("the Lifecycle rule with no value isn't refused beside it:\n%s", rows[1])
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("a refused save saved %d definitions", len(defs))
	}
}

// A save in a mode that is neither rules nor picked is refused beside the
// mode.
func TestBuilderRefusesAnUnknownModeBesideTheModeOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{"name": {"MBR"}, "mode": {"graph"}})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("refused save: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	mode := pageElement(t, page, "fieldset", "report-mode")
	if !strings.Contains(mode, `id="mode-error"`) {
		t.Errorf("the unknown mode isn't refused beside the mode:\n%s", mode)
	}
}

// Without script, Add rule submits the builder and it comes back, 200, with
// one more rule row and every value kept, having saved nothing. A Top-level
// row comes back offering only "is Top-level" and "is not Top-level", and no
// values.
func TestBuilderAddRuleKeepsEveryValueAndSavesNothingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sdk := h.ActiveGoal(boss, "Mobile SDK auth update", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"Exec weekly"},
		"introduction":       {"Every Monday."},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"rules[1].attribute": {domain.RuleHealth},
		"rules[1].op":        {domain.RuleIsAnyOf},
		"rules[1].value":     {"health=" + domain.HealthRed, "health=" + domain.HealthYellow},
		"include":            {id(sdk.ID)},
		"do":                 {"add-rule"},
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Add rule: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	builder := pageElement(t, page, "form", "report-builder")
	for _, want := range []string{
		`name="name" value="Exec weekly"`,
		`name="introduction">Every Monday.</textarea>`,
		`<option value="health=Red" selected>Red</option>`,
		`<option value="health=Yellow" selected>Yellow</option>`,
		`<input type="hidden" name="include" value="` + id(sdk.ID) + `">`,
	} {
		if !strings.Contains(builder, want) {
			t.Errorf("Add rule lost %s:\n%s", want, builder)
		}
	}
	rows := strings.Split(builder, `data-testid="report-rule"`)[1:]
	if len(rows) != 3 {
		t.Fatalf("Add rule shows %d rule rows, want 3:\n%s", len(rows), builder)
	}
	topLevel := rows[0]
	if !strings.Contains(topLevel, `<option value="top-level" selected>Top-level</option>`) {
		t.Errorf("the Top-level row lost its attribute:\n%s", topLevel)
	}
	if ops := regexp.MustCompile(`<option value="(is|is not|is any of)"[^>]*>([^<]*)</option>`).FindAllStringSubmatch(topLevel, -1); len(ops) != 2 ||
		ops[0][2] != "is Top-level" || ops[1][2] != "is not Top-level" {
		t.Errorf("the Top-level row offers operators %q, want only is Top-level and is not Top-level", ops)
	}
	if strings.Contains(topLevel, `data-testid="rule-values"`) {
		t.Errorf("the Top-level row offers values:\n%s", topLevel)
	}
	if strings.Contains(rows[2], " selected") {
		t.Errorf("the added row isn't blank:\n%s", rows[2])
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("Add rule saved %d definitions", len(defs))
	}
}

// A rule row's × submits the builder and it comes back without that row,
// saving nothing; Enter in the name saves, as the builder's first button is
// Save.
func TestBuilderRemovesARuleRowOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"Exec weekly"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"rules[1].attribute": {domain.RuleLifecycle},
		"rules[1].op":        {domain.RuleIs},
		"rules[1].value":     {"lifecycle=" + domain.LifecycleActive},
		"remove-rule":        {"0"},
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove rule: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	builder := pageElement(t, page, "form", "report-builder")
	rows := strings.Split(builder, `data-testid="report-rule"`)[1:]
	if len(rows) != 1 || !strings.Contains(rows[0], `<option value="lifecycle" selected>`) {
		t.Errorf("removing the Top-level row left %d rows:\n%s", len(rows), builder)
	}
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(builder)
	if strings.Contains(first, `name=`) {
		t.Errorf("the builder's first submit, which Enter presses, isn't Save: %s", first)
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("removing a rule saved %d definitions", len(defs))
	}
}

// A rule tests any live Dimension, Owner, Lifecycle, Health or Top-level,
// never a Field, which only describes a Goal (ADR 0005). A Retired Dimension,
// and a Retired value, isn't offered for a new rule (CONTEXT.md: Retired).
func TestBuilderAttributeSelectOffersEveryLiveAttributeAndNoFieldOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Rivera")
	team := h.CreateDimension(boss, "Team", "Platform", "Legacy")
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	h.ActiveGoal(sam, "Edge cache rollout", "Matters.")
	ctx := context.Background()
	if err := h.Service.RetireDimension(ctx, boss.ID, pillar.ID); err != nil {
		t.Fatalf("RetireDimension: %v", err)
	}
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, team.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	ts := newServer(t, h)
	builder := pageElement(t, getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/new"), "form", "report-builder")

	attribute := pageElement(t, builder, "select", "rule-attribute")
	var offered []string
	for _, m := range regexp.MustCompile(`<option value="[^"]*">([^<]*)</option>`).FindAllStringSubmatch(attribute, -1) {
		offered = append(offered, m[1])
	}
	if want := []string{"Choose…", "Team", "Owner", "Lifecycle", "Health", "Top-level"}; !slices.Equal(offered, want) {
		t.Errorf("the attribute select offers %q, want %q", offered, want)
	}
	values := pageElement(t, builder, "select", "rule-values")
	for _, want := range []string{">Platform<", ">Sam Rivera<", ">" + domain.LifecycleOnHold + "<", ">" + domain.HealthYellow + "<"} {
		if !strings.Contains(values, want) {
			t.Errorf("the values don't offer %s:\n%s", want, values)
		}
	}
	for _, gone := range []string{"Legacy", "Growth", "Budget"} {
		if strings.Contains(values, gone) {
			t.Errorf("the values offer %s:\n%s", gone, values)
		}
	}
}

// A value posted for a rule that tests another attribute, such as a Health
// for an Owner rule, is refused as that rule, and nothing is saved.
func TestBuilderRefusesAValueFromAnotherAttributeAsItsRuleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleLifecycle},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {"lifecycle=" + domain.LifecycleActive},
		"rules[1].attribute": {domain.RuleOwner},
		"rules[1].op":        {domain.RuleIsAnyOf},
		"rules[1].value":     {"owner=" + strconv.FormatInt(boss.ID, 10), "health=" + domain.HealthRed},
	})
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a Health in an Owner rule: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	rows := strings.Split(pageElement(t, page, "form", "report-builder"), `data-testid="report-rule"`)[1:]
	if len(rows) != 2 || strings.Contains(rows[0], "input-error") || !strings.Contains(rows[1], `id="rules[1]-error"`) {
		t.Errorf("the Owner rule with a Health isn't refused as rules[1]:\n%s", strings.Join(rows, "\n---\n"))
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("the refused save saved %d definitions", len(defs))
	}
}

// GET /reports/goals/search answers a builder picker's search: every Goal at
// any level whose title contains q, ignoring case, less those already in the
// picker's list. Leave out searches every Goal, not just the current
// matches. Each result holds the chip picking it adds to that list, and the
// builder's pickers search there.
func TestBuilderGoalSearchFindsEveryGoalLessThoseListedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	parent := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Secure every login", "Matters."))
	sdk := h.ActiveChildOf(boss, parent, "Mobile SDK auth update", "Matters.")
	listed := h.ActiveGoal(boss, "Migrate AUTH service", "Matters.")
	h.ActiveGoal(boss, "Edge cache rollout", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	builder := getBody(t, client, ts.URL+"/reports/new")
	for _, list := range []string{"picked", "include", "exclude"} {
		picker := pageElement(t, builder, "fieldset", list+"-picker")
		if !strings.Contains(picker, `hx-get="/reports/goals/search?list=`+list+`"`) {
			t.Errorf("the %s picker doesn't search /reports/goals/search:\n%s", list, picker)
		}
	}

	q := url.Values{"list": {"exclude"}, "q": {"auth"}, "exclude": {strconv.FormatInt(listed.ID, 10)}}
	page := getBody(t, client, ts.URL+"/reports/goals/search?"+q.Encode())
	results := strings.Split(page, `data-testid="report-goal-result"`)[1:]
	if len(results) != 1 || !strings.Contains(between(t, results[0], "data-pick", "</button>"), sdk.Title) {
		t.Fatalf("searching Leave out for %q found %d results, want only %q:\n%s", "auth", len(results), sdk.Title, page)
	}
	chip := between(t, results[0], "<template>", "</template>")
	if !strings.Contains(chip, `<input type="hidden" name="exclude" value="`+strconv.FormatInt(sdk.ID, 10)+`">`) {
		t.Errorf("picking %q adds the chip %s", sdk.Title, chip)
	}
}

// Without script, Show matches submits the builder and it comes back, 200, as
// typed, its rail listing the Goals the definition would select, and saves
// nothing.
func TestBuilderShowMatchesListsTheGoalsAndSavesNothingOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	top := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "Matters."))
	other := h.ActiveGoal(boss, "Cut churn", "Matters.")
	left := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Earn trust", "Matters."))
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	form := url.Values{
		"name":               {"Exec weekly"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"exclude":            {strconv.FormatInt(left.ID, 10)},
		"do":                 {"show-matches"},
	}
	resp := postForm(t, client, ts.URL+"/reports/new", form)
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Show matches: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	matches := pageElement(t, page, "aside", "report-matches")
	if !strings.Contains(matches, "1 Goal") || !strings.Contains(matches, top.Title) || strings.Contains(matches, other.Title) || strings.Contains(matches, left.Title) {
		t.Errorf("Show matches doesn't list just %q:\n%s", top.Title, matches)
	}
	if rail, _ := postFormHX(t, client, ts.URL+"/reports/new/matches", form); !strings.HasPrefix(rail, matches) {
		t.Errorf("Show matches lists\n%s\nbut the live rail lists\n%s", matches, rail)
	}
	if !strings.Contains(pageElement(t, page, "form", "report-builder"), `name="name" value="Exec weekly"`) {
		t.Errorf("Show matches lost the name")
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("Show matches saved %d definitions", len(defs))
	}
}

// assertSavedReport checks resp, a builder's save, answered 303 to a Report's
// draft, and returns the draft as client sees it.
func assertSavedReport(t *testing.T, client *http.Client, baseURL string, resp *http.Response) string {
	t.Helper()
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save report: status %d, want 303; body:\n%s", resp.StatusCode, body)
	}
	where := resp.Header.Get("Location")
	if !regexp.MustCompile(`^/reports/\d+$`).MatchString(where) {
		t.Fatalf("save report redirects to %q, want /reports/{id}", where)
	}
	return getBody(t, client, baseURL+where)
}

// draftGoalTitles are the titles the draft's Goals panel lists, sorted.
func draftGoalTitles(t *testing.T, draft string) []string {
	t.Helper()
	panel := pageElement(t, draft, "section", "report-goals")
	var titles []string
	for _, m := range regexp.MustCompile(`href="#goal-\d+">([^<]*)</a>`).FindAllStringSubmatch(panel, -1) {
		titles = append(titles, m[1])
	}
	slices.Sort(titles)
	return titles
}

// The builder's rail answers its form at /reports/new/matches with only the
// rail: how many Goals the definition would select, the same count its saved
// draft lists.
func TestBuilderMatchesCountWhatTheSavedDraftSelectsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "Matters."))
	h.MarkTopLevel(boss, h.ActiveGoal(boss, "Earn trust", "Matters."))
	h.ActiveGoal(boss, "Cut churn", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	form := url.Values{
		"name":               {"Exec weekly"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
	}

	rail, status := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if status != http.StatusOK {
		t.Fatalf("matches: status %d, want 200; body:\n%s", status, rail)
	}
	if strings.Contains(rail, "<html") || strings.Contains(rail, "<form") {
		t.Errorf("matches answers more than the rail:\n%s", rail)
	}
	draft := assertSavedReport(t, client, ts.URL, postForm(t, noRedirects(client), ts.URL+"/reports/new", form))
	if got, want := railCount(t, rail), len(draftGoalTitles(t, draft)); got != want {
		t.Errorf("the rail matches %d Goals, the saved draft selects %d:\n%s", got, want, rail)
	}
}

// railCount is the N of the rail's "Matches N Goals".
func railCount(t *testing.T, rail string) int {
	t.Helper()
	m := regexp.MustCompile(`Matches (\d+) Goals?`).FindStringSubmatch(rail)
	if m == nil {
		t.Fatalf("the rail has no count:\n%s", rail)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// Until a rules definition has a usable rule — one with a value, or a
// Top-level one — the rail asks for one and counts nothing; its name isn't
// checked. An id naming no saved definition answers 404.
func TestBuilderMatchesAskForARuleAndRefuseAnUnknownDefinitionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Grow revenue", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	form := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleHealth},
		"rules[0].op":        {domain.RuleIsAnyOf},
	}

	rail, status := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if status != http.StatusOK {
		t.Fatalf("matches: status %d, want 200; body:\n%s", status, rail)
	}
	if !strings.Contains(rail, "Add a rule to see matching Goals") || strings.Contains(rail, "Matches 0") || strings.Contains(rail, `data-testid="matches-count"`) {
		t.Errorf("a rule with no values gets a count, not a request for a rule:\n%s", rail)
	}
	form.Set("id", "9999")
	if body, status := postFormHX(t, client, ts.URL+"/reports/new/matches", form); status != http.StatusNotFound {
		t.Errorf("matches for an unknown definition: status %d, want 404; body:\n%s", status, body)
	}
}

// In rules mode the rail marks "added" a Goal only Also include selects; in
// picked mode, which ignores rules and Also include, it marks none. Each Goal
// shows its Health, or with none yet its Lifecycle.
func TestBuilderMatchesMarkAlsoIncludeAndShowHealthOrLifecycleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Mobile")
	reliability := h.ActiveGoal(boss, "Platform reliability", "Matters.")
	h.AssignGoalValue(reliability, team.Values[0])
	h.Checkin(boss, reliability.ID, domain.HealthGreen, "On track.", "", time.Time{})
	sdk := h.ActiveGoal(boss, "Mobile SDK auth update", "Matters.")
	h.AssignGoalValue(sdk, team.Values[1])
	proposed := h.CreateGoal(boss, "Retire the job scheduler", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	dim := "dimension:" + id(team.ID)
	form := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {dim + "=" + id(team.Values[0].ID)},
		"include":            {id(sdk.ID)},
		"picked":             {id(sdk.ID), id(proposed.ID)},
	}

	rail, _ := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	matches := railMatches(rail)
	if len(matches) != 2 {
		t.Fatalf("the rules rail lists %d Goals, want 2:\n%s", len(matches), rail)
	}
	if m := matches[reliability.Title]; strings.Contains(m, "match-added") || !strings.Contains(m, `class="badge g"><span class="dot"></span>Green</span>`) {
		t.Errorf("the rule match isn't shown Green and unmarked:\n%s", m)
	}
	if m := matches[sdk.Title]; !strings.Contains(m, `data-testid="match-added"`) || !strings.Contains(m, `class="badge lc">Active</span>`) {
		t.Errorf("the Also include Goal isn't marked added with its Lifecycle:\n%s", m)
	}

	form.Set("mode", domain.ReportModePicked)
	rail, _ = postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	matches = railMatches(rail)
	if len(matches) != 2 || matches[sdk.Title] == "" || matches[proposed.Title] == "" {
		t.Fatalf("the picked rail doesn't list just the picked Goals:\n%s", rail)
	}
	if strings.Contains(rail, "match-added") {
		t.Errorf("the picked rail marks a Goal added:\n%s", rail)
	}
	if m := matches[proposed.Title]; !strings.Contains(m, `class="badge lc">Proposed</span>`) {
		t.Errorf("the Proposed Goal doesn't show its Lifecycle:\n%s", m)
	}
}

// railMatches are the rail's listed Goals, each its markup by title.
func railMatches(rail string) map[string]string {
	out := map[string]string{}
	for _, li := range strings.Split(rail, `<li data-testid="match"`)[1:] {
		li, _, _ = strings.Cut(li, "</li>")
		if m := regexp.MustCompile(`class="rb-match-title">([^<]*)</span>`).FindStringSubmatch(li); m != nil {
			out[m[1]] = li
		}
	}
	return out
}

// The rail lists the first 10 Goals by title, then "+ n more" for the rest and
// "m left out" for the rule matches Leave out takes away — not one no rule
// matches — each part only when it isn't 0.
func TestBuilderMatchesListTenByTitleThenCountTheRestOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	var top []domain.Goal
	for _, title := range []string{"Mobile", "kilo", "lima", "Juliet", "India", "Hotel", "Golf", "Foxtrot", "Echo", "Delta", "Charlie", "Bravo", "Alpha"} {
		top = append(top, h.MarkTopLevel(boss, h.ActiveGoal(boss, title, "Matters.")))
	}
	other := h.ActiveGoal(boss, "Zulu", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	form := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"exclude":            {id(top[1].ID), id(top[3].ID), id(other.ID)},
		"include":            {id(other.ID)},
	}

	rail, _ := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if got := railCount(t, rail); got != 11 {
		t.Errorf("the rail matches %d Goals, want 11", got)
	}
	var titles []string
	for _, m := range regexp.MustCompile(`class="rb-match-title">([^<]*)</span>`).FindAllStringSubmatch(rail, -1) {
		titles = append(titles, m[1])
	}
	if want := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel", "India", "lima"}; !slices.Equal(titles, want) {
		t.Errorf("the rail lists %q, want %q", titles, want)
	}
	if more := pageElement(t, rail, "p", "matches-more"); !strings.HasSuffix(more, ">+ 1 more · 2 left out") {
		t.Errorf("the rail's closing line is %q, want + 1 more · 2 left out", more)
	}

	form["exclude"] = []string{id(top[1].ID), id(top[3].ID), id(top[5].ID), id(top[7].ID)}
	rail, _ = postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if more := pageElement(t, rail, "p", "matches-more"); !strings.HasSuffix(more, ">4 left out") {
		t.Errorf("with 10 matches the closing line is %q, want just 4 left out", more)
	}
	form.Del("exclude")
	form.Del("include")
	form.Set("rules[0].op", domain.RuleIsNot)
	rail, _ = postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if railCount(t, rail) != 1 || strings.Contains(rail, "matches-more") {
		t.Errorf("one match and none left out still has a closing line:\n%s", rail)
	}
}

// The rail reads the matches against the default baseline: with no id a new
// definition's, 30 days ago, and with the id of a saved one its last
// publication. Either way it counts what the saved draft lists, so a Goal
// finished between the two counts only for the new one.
func TestBuilderMatchesReadAgainstTheDefinitionsBaselineOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "Matters."))
	shipped := h.MarkTopLevel(boss, h.ActiveGoal(boss, "Ship the SDK", "Matters."))
	rule := domain.ReportRule{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "Exec weekly", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{rule}})
	h.Clock.Advance(5 * 24 * time.Hour)
	if _, err := h.Service.SubmitCheckin(context.Background(), domain.SubmitCheckinInput{
		GoalID: shipped.ID, AuthorID: boss.ID, Status: "Shipped.", Lifecycle: domain.LifecycleDone, Outcome: "Shipped.",
	}); err != nil {
		t.Fatalf("SubmitCheckin to Done: %v", err)
	}
	h.Clock.Advance(5 * 24 * time.Hour)
	h.PublishReport(boss, def)
	h.Clock.Advance(24 * time.Hour)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	form := url.Values{
		"name":               {"Exec weekly"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
	}

	rail, _ := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	draft := assertSavedReport(t, client, ts.URL, postForm(t, noRedirects(client), ts.URL+"/reports/new", form))
	if got, want := railCount(t, rail), len(draftGoalTitles(t, draft)); got != 2 || got != want {
		t.Errorf("for a new definition the rail matches %d Goals, its saved draft %d, want 2", got, want)
	}
	form.Set("id", strconv.FormatInt(def.ID, 10))
	rail, _ = postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	draft = getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10))
	if got, want := railCount(t, rail), len(draftGoalTitles(t, draft)); got != 1 || got != want {
		t.Errorf("for the published definition the rail matches %d Goals, its draft %d, want 1", got, want)
	}
}

// The builder posts itself to the rail as it is typed — on change, and on
// input after a pause, but not a picker's search — swapping the rail, which
// sits outside the form. The rail is there from the first render, and after
// Add rule and a refused save, which re-render the builder.
func TestBuilderFormRefreshesItsRailAsItIsTypedOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.MarkTopLevel(boss, h.ActiveGoal(boss, "Grow revenue", "Matters."))
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	page := getBody(t, client, ts.URL+"/reports/new")
	form := tagAround(t, page, `data-testid="report-builder"`)
	for name, want := range map[string]string{
		"method":     "post",
		"action":     "/reports/new",
		"hx-post":    "/reports/new/matches",
		"hx-trigger": "change[target.type!='search'], input[target.type!='search'] delay:400ms",
		"hx-target":  "#report-matches",
		"hx-swap":    "outerHTML",
		"hx-sync":    "this:replace",
	} {
		if got := html.UnescapeString(attr(form, name)); got != want {
			t.Errorf("the builder's %s = %q, want %q", name, got, want)
		}
	}
	rail := tagAround(t, page, `data-testid="report-matches"`)
	if attr(rail, "id") != "report-matches" {
		t.Errorf("the rail has no id to swap: %s", rail)
	}
	if !strings.Contains(pageElement(t, page, "aside", "report-matches"), "Add a rule to see matching Goals") {
		t.Errorf("the new builder's rail doesn't ask for a rule")
	}
	if builder := pageElement(t, page, "form", "report-builder"); strings.Contains(builder, `data-testid="report-matches"`) {
		t.Errorf("the rail sits inside the form, so its swap could replace an input")
	}

	rules := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
	}
	for what, do := range map[string]string{"Add rule": "add-rule", "a refused save": ""} {
		form := url.Values{"do": {do}}
		maps.Copy(form, rules)
		page := readBody(t, postForm(t, client, ts.URL+"/reports/new", form))
		if got := railCount(t, pageElement(t, page, "aside", "report-matches")); got != 1 {
			t.Errorf("after %s the rail matches %d Goals, want 1", what, got)
		}
	}
}
