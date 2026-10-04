package web_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

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
