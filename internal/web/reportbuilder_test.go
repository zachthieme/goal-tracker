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
	parent := h.ActiveGoal(boss, "Grow revenue", "The company needs to grow.")
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
	defer func() { _ = resp.Body.Close() }()
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
// row comes back offering only "is Top-level" and "is not Top-level", and
// holding no values.
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
	defer func() { _ = resp.Body.Close() }()
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
	if values := pageElement(t, topLevel, "select", "rule-values"); strings.Contains(values, " selected") {
		t.Errorf("the Top-level row holds values:\n%s", values)
	}
	if strings.Contains(rows[2], " selected") {
		t.Errorf("the added row isn't blank:\n%s", rows[2])
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("Add rule saved %d definitions", len(defs))
	}
}

// A Top-level row that comes back from the server still carries every
// attribute's values, which script hides until the row tests one, so switching
// it to a Dimension offers that Dimension's values and the rule saves.
func TestBuilderTopLevelRowCanBeSwitchedToADimensionOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Identity")
	platform := team.Values[0]
	reliability := h.ActiveGoal(boss, "Platform reliability", "Matters.")
	h.AssignGoalValue(reliability, platform)
	h.ActiveGoal(boss, "Cut churn", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	dim := "dimension:" + strconv.FormatInt(team.ID, 10)

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"Platform MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"do":                 {"add-rule"},
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Add rule: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	builder := pageElement(t, page, "form", "report-builder")
	topLevel := strings.Split(builder, `data-testid="report-rule"`)[1]
	if !strings.Contains(topLevel, `name="rules[0].value"`) {
		t.Fatalf("the Top-level row has no rules[0].value select:\n%s", topLevel)
	}
	values := pageElement(t, topLevel, "select", "rule-values")
	if regexp.MustCompile(`<select[^>]*\shidden`).MatchString(values) {
		t.Errorf("the Top-level row's values are hidden in the markup, not by script:\n%s", values)
	}
	for _, want := range []string{`label="Team"`, `label="Owner"`, `label="Lifecycle"`, `label="Health"`} {
		if !strings.Contains(values, want) {
			t.Errorf("the Top-level row's values have no %s group:\n%s", want, values)
		}
	}
	option := regexp.MustCompile(`<option value="([^"]*)">Platform</option>`).FindStringSubmatch(values)
	if option == nil {
		t.Fatalf("the Top-level row's values offer no Platform:\n%s", values)
	}

	resp = postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"Platform MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {html.UnescapeString(option[1])},
	})
	draft := assertSavedReport(t, client, ts.URL, resp)
	if got, want := draftGoalTitles(t, draft), []string{reliability.Title}; !slices.Equal(got, want) {
		t.Errorf("the switched rule selects %q, want %q", got, want)
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
	defer func() { _ = resp.Body.Close() }()
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
// for an Owner rule, is dropped: the Owner rule saves with only its Owner
// value (ticket #314).
func TestBuilderDropsAValueFromAnotherAttributeFromItsRuleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	bossID := strconv.FormatInt(boss.ID, 10)

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleLifecycle},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {"lifecycle=" + domain.LifecycleActive},
		"rules[1].attribute": {domain.RuleOwner},
		"rules[1].op":        {domain.RuleIsAnyOf},
		"rules[1].value":     {"owner=" + bossID, "health=" + domain.HealthRed},
	})
	defer func() { _ = resp.Body.Close() }()
	assertSavedReport(t, client, ts.URL, resp)
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 1 {
		t.Fatalf("ListReportDefinitions = %v, %v; want the one saved", defs, err)
	}
	if rules := defs[0].Rules; len(rules) != 2 || rules[1].Attribute != domain.RuleOwner || !slices.Equal(rules[1].Values, []string{bossID}) {
		t.Errorf("the Owner rule with a Health is stored as %+v, want Owner is any of %s alone", rules, bossID)
	}
}

// Without script every attribute's values stay selected, so an author who
// switches the started Owner's org row to Lifecycle still posts their org
// beside Active. The row keeps only its own attribute's values: the save
// stores Lifecycle is Active (ticket #314).
func TestBuilderSavesAChangedStartingRowWithoutItsStaleValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	h.SetManager(h.SignIn("mia@example.com"), vp)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleLifecycle},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10), domain.RuleLifecycle + "=" + domain.LifecycleActive},
	})
	defer func() { _ = resp.Body.Close() }()
	assertSavedReport(t, client, ts.URL, resp)
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 1 {
		t.Fatalf("ListReportDefinitions = %v, %v; want the one saved", defs, err)
	}
	want := []domain.ReportRule{{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive}}}
	if got := defs[0].Rules; len(got) != 1 || got[0].Attribute != want[0].Attribute || got[0].Op != want[0].Op || !slices.Equal(got[0].Values, want[0].Values) {
		t.Errorf("the changed row is stored as %+v, want %+v", got, want)
	}
}

// Show matches and a refused save re-render a switched row with only its own
// attribute's values selected, not the stale one it posted (ticket #314).
func TestBuilderRerendersAChangedRowWithOnlyItsOwnValuesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	h.SetManager(h.SignIn("mia@example.com"), vp)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")
	stale := domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)

	for _, tc := range []struct {
		what, name, do string
		status         int
	}{
		{"Show matches", "MBR", "show-matches", http.StatusOK},
		{"a save with no name", "", "", http.StatusUnprocessableEntity},
	} {
		resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
			"name":               {tc.name},
			"do":                 {tc.do},
			"mode":               {domain.ReportModeRules},
			"rules[0].attribute": {domain.RuleLifecycle},
			"rules[0].op":        {domain.RuleIs},
			"rules[0].value":     {stale, domain.RuleLifecycle + "=" + domain.LifecycleActive},
		})
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: status %d, want %d; body:\n%s", tc.what, resp.StatusCode, tc.status, page)
		}
		values := pageElement(t, pageElement(t, page, "form", "report-builder"), "select", "rule-values")
		if got := regexp.MustCompile(`value="([^"]*)" selected`).FindAllStringSubmatch(values, -1); len(got) != 1 || got[0][1] != domain.RuleLifecycle+"="+domain.LifecycleActive {
			t.Errorf("after %s the row's values select %v, want only lifecycle=Active:\n%s", tc.what, got, values)
		}
	}
}

// A row whose only posted value is another attribute's is left with none, and
// refused as a row with no value is: 422 beside the row, nothing saved
// (ticket #314).
func TestBuilderRefusesARowLeftWithNoValueOfItsOwnOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	h.SetManager(h.SignIn("mia@example.com"), vp)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleLifecycle},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)},
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a Lifecycle row with only an org value: status %d, want 422; body:\n%s", resp.StatusCode, page)
	}
	row := pageElement(t, pageElement(t, page, "form", "report-builder"), "div", "report-rule")
	if bad := between(t, row, `id="rules[0]-error"`, `</span>`); !strings.Contains(bad, "a rule needs a value") {
		t.Errorf("the row's error reads %q, want \"a rule needs a value\"", bad)
	}
	if defs, _ := h.Service.ListReportDefinitions(context.Background()); len(defs) != 0 {
		t.Errorf("the refused save saved %d definitions", len(defs))
	}
}

// A row with no attribute keeps no value, so one posting only a stale value
// is left out as a blank row is, and the rules beside it save (ticket #314).
func TestBuilderLeavesOutARowWithNoAttributeAndAStaleValueOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	h.SetManager(h.SignIn("mia@example.com"), vp)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {""},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)},
		"rules[1].attribute": {domain.RuleLifecycle},
		"rules[1].op":        {domain.RuleIs},
		"rules[1].value":     {domain.RuleLifecycle + "=" + domain.LifecycleActive},
	})
	defer func() { _ = resp.Body.Close() }()
	assertSavedReport(t, client, ts.URL, resp)
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 1 {
		t.Fatalf("ListReportDefinitions = %v, %v; want the one saved", defs, err)
	}
	if rules := defs[0].Rules; len(rules) != 1 || rules[0].Attribute != domain.RuleLifecycle || !slices.Equal(rules[0].Values, []string{domain.LifecycleActive}) {
		t.Errorf("the rules are stored as %+v, want only Lifecycle is Active", rules)
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

// Each picker's search swaps its matches into its results list rather than
// inheriting the builder's outerHTML swap, which would replace the list and
// lose the id the next search targets, and syncs only with itself, so a search
// and the rail's refresh don't abort each other. It sends the chips already
// picked by including their list, which is there even with no chips, so htmx
// never finds nothing to include (#180). The search answers bare items, with
// no list of its own to nest or id to collide.
func TestBuilderGoalSearchKeepsItsResultsListOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Spike the auth flow", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	builder := getBody(t, client, ts.URL+"/reports/new")
	for _, list := range []string{"picked", "include", "exclude"} {
		picker := pageElement(t, builder, "fieldset", list+"-picker")
		search := tagAround(t, picker, "data-goal-search")
		for name, want := range map[string]string{
			"hx-target":  "#" + list + "-results",
			"hx-swap":    "innerHTML",
			"hx-sync":    "this:replace",
			"hx-include": "#" + list + "-chips",
		} {
			if got := html.UnescapeString(attr(search, name)); got != want {
				t.Errorf("the %s search's %s = %q, want %q", list, name, got, want)
			}
		}
		if !strings.Contains(picker, `id="`+list+`-chips"`) {
			t.Errorf("the %s picker with no chips has no #%s-chips list to include:\n%s", list, list, picker)
		}
	}

	for _, q := range []string{"spike", "nothing like it"} {
		page := getBody(t, client, ts.URL+"/reports/goals/search?"+url.Values{"list": {"picked"}, "q": {q}}.Encode())
		if body := strings.TrimSpace(page); !strings.HasPrefix(body, "<li") || strings.Contains(body, "<ul") || attr(body, "id") != "" {
			t.Errorf("searching %q answers more than bare items:\n%s", q, page)
		}
	}
}

// Picking a search result empties the search box, so the next search starts
// afresh rather than appending to the last one ("SpikeMob", #180).
func TestBuilderPickClearsTheSearchBoxOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	h.SignIn("boss@example.com")
	ts := newServer(t, h)

	builder := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/new")
	script := between(t, builder, "var pick = e.target.closest(\"[data-pick]\")", "return;")
	if !strings.Contains(script, `picker.querySelector("[data-goal-search]")`) || !strings.Contains(script, `.value = "";`) {
		t.Errorf("picking a result leaves the search box as typed:\n%s", script)
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
	defer func() { _ = resp.Body.Close() }()
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

// draftGoalTitles are the titles of the Goals the draft Report holds, each
// exception's block and each On track row, sorted.
func draftGoalTitles(t *testing.T, draft string) []string {
	t.Helper()
	main := findByTestID(t, parsePage(t, draft), "report-draft")
	var titles []string
	for _, testID := range []string{"report-exception", "selected-goal"} {
		for _, g := range findAllByTestID(main, testID) {
			titles = append(titles, textOf(linkTo(g, "/goals/"+strings.TrimPrefix(attrOf(g, "id"), "goal-"))))
		}
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
	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", form)
	defer func() { _ = resp.Body.Close() }()
	draft := assertSavedReport(t, client, ts.URL, resp)
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
	if m := matches[reliability.Title]; strings.Contains(m, "match-added") {
		t.Errorf("the rule match is marked added:\n%s", m)
	}
	assertHealthChip(t, findByTestID(t, parsePage(t, matches[reliability.Title]), "match-badge"), "Green")
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

// The rail selects once, taking each Goal's "added" mark and the left-out
// count from that one selection: for a Definition with both Also include and
// Leave out, it marks only the Goal Also include alone brings in, and counts
// only the Leave out Goal the rules would have selected. Every Goal's
// Dimension values, which only selecting reads, are read once per render.
func TestBuilderMatchesSelectOnceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	team := h.CreateDimension(boss, "Team", "Platform", "Mobile")
	matched := h.ActiveGoal(boss, "Platform reliability", "Matters.")
	listed := h.ActiveGoal(boss, "Platform latency", "Matters.")
	added := h.ActiveGoal(sam, "Mobile SDK auth update", "Matters.")
	leftOut := h.ActiveGoal(boss, "Platform migration", "Matters.")
	unmatched := h.ActiveGoal(sam, "Mobile onboarding", "Matters.")
	for _, g := range []domain.Goal{matched, listed, leftOut} {
		h.AssignGoalValue(g, team.Values[0])
	}
	for _, g := range []domain.Goal{added, unmatched} {
		h.AssignGoalValue(g, team.Values[1])
	}
	ts, _, queries := newQueryCountingServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	dim := "dimension:" + id(team.ID)
	form := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {dim},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {dim + "=" + id(team.Values[0].ID)},
		"include":            {id(listed.ID), id(added.ID)},
		"exclude":            {id(leftOut.ID), id(unmatched.ID)},
	}

	queries.take()
	rail, status := postFormHX(t, client, ts.URL+"/reports/new/matches", form)
	if status != http.StatusOK {
		t.Fatalf("matches: status %d, want 200; body:\n%s", status, rail)
	}
	if got := countQueryNames(queries.take())["ListAllGoalValues"]; got != 1 {
		t.Errorf("the rail read every Goal's Dimension values %d times, want once", got)
	}
	page := parsePage(t, rail)
	if got := textOf(findByTestID(t, page, "matches-count")); got != "Matches 3 Goals" {
		t.Errorf("the rail's count is %q, want Matches 3 Goals", got)
	}
	addedBy := map[string]bool{}
	for _, li := range findAllByTestID(page, "match") {
		for _, g := range []domain.Goal{matched, listed, added} {
			if strings.Contains(textOf(li), g.Title) {
				addedBy[g.Title] = len(findAllByTestID(li, "match-added")) > 0
			}
		}
	}
	want := map[string]bool{matched.Title: false, listed.Title: false, added.Title: true}
	if !maps.Equal(addedBy, want) {
		t.Errorf("the rail lists Goals marked added %v, want %v", addedBy, want)
	}
	if got := textOf(findByTestID(t, page, "matches-more")); got != "1 left out" {
		t.Errorf("the rail's closing line is %q, want 1 left out", got)
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
	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", form)
	defer func() { _ = resp.Body.Close() }()
	draft := assertSavedReport(t, client, ts.URL, resp)
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
//
// The post comes from report-builder-inputs, inside the form, not the form:
// htmx validates a form it posts, so with Name, which is required, still empty
// it would halt every refresh (#179). The wrapper is display:contents, so the
// form's gap still spaces the sections, and the script's change for a chip
// picked or removed is dispatched where the wrapper hears it.
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
		"method":  "post",
		"action":  "/reports/new",
		"hx-post": "",
	} {
		if got := html.UnescapeString(attr(form, name)); got != want {
			t.Errorf("the builder's %s = %q, want %q", name, got, want)
		}
	}
	builder := pageElement(t, page, "form", "report-builder")
	if !strings.Contains(builder, `data-testid="report-builder-inputs"`) {
		t.Fatalf("the builder's inputs have no wrapper inside the form:\n%s", builder)
	}
	inputs := tagAround(t, builder, `data-testid="report-builder-inputs"`)
	for name, want := range map[string]string{
		"hx-post":    "/reports/new/matches",
		"hx-include": "closest form",
		"hx-trigger": "change[target.type!='search'], input[target.type!='search'] delay:400ms",
		"hx-target":  "#report-matches",
		"hx-swap":    "outerHTML",
		"hx-sync":    "this:replace",
	} {
		if got := html.UnescapeString(attr(inputs, name)); got != want {
			t.Errorf("the builder's inputs' %s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(between(t, builder, `data-testid="report-builder-inputs"`, ""), `name="name"`) {
		t.Errorf("the builder's inputs' wrapper doesn't hold Name")
	}
	if name := tagAround(t, builder, `name="name"`); !regexp.MustCompile(`\srequired[\s/>]`).MatchString(name) {
		t.Errorf("Name is no longer required, so Save doesn't prompt for it: %s", name)
	}
	if rule := cssRule(t, page, ".rb-inputs"); !strings.Contains(rule, "display:contents") {
		t.Errorf("the inputs' wrapper isn't display:contents, so the form's gap no longer spaces its sections; rule: %s", rule)
	}
	if !strings.Contains(attr(inputs, "class"), "rb-inputs") {
		t.Errorf("the inputs' wrapper isn't .rb-inputs: %s", inputs)
	}
	script := between(t, page, "var pick = e.target.closest(\"[data-pick]\")", "</script>")
	if strings.Contains(script, "form.dispatchEvent(") {
		t.Errorf("picking or removing a chip dispatches change on the form, which the inputs' wrapper inside it never hears:\n%s", script)
	}
	if got := strings.Count(script, `inputs.dispatchEvent(new Event("change"))`); got != 2 {
		t.Errorf("picking and removing a chip dispatch change on the inputs' wrapper %d times, want 2:\n%s", got, script)
	}
	rail := tagAround(t, page, `data-testid="report-matches"`)
	if attr(rail, "id") != "report-matches" {
		t.Errorf("the rail has no id to swap: %s", rail)
	}
	// The boss owns a Goal, so the new builder starts with their own Goals.
	if got := railCount(t, pageElement(t, page, "aside", "report-matches")); got != 1 {
		t.Errorf("the new builder's rail matches %d Goals, want the boss's own 1", got)
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
		page := postFormBody(t, client, ts.URL+"/reports/new", form)
		if got := railCount(t, pageElement(t, page, "aside", "report-matches")); got != 1 {
			t.Errorf("after %s the rail matches %d Goals, want 1", what, got)
		}
	}
}

// Without script, a chip's Remove checkbox takes its Goal out of the list on
// the builder's next submit of any kind: Add rule, a rule's ×, Show matches
// and Save (ticket #168).
func TestBuilderChipRemoveDropsItsGoalOnAnySubmitOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	kept := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	gone := h.ActiveGoal(boss, "Cut churn", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	chips := func(page, list string) []string {
		var ids []string
		for _, m := range regexp.MustCompile(`name="`+list+`" value="(\d+)"`).FindAllStringSubmatch(pageElement(t, page, "ul", list+"-chips"), -1) {
			ids = append(ids, m[1])
		}
		return ids
	}

	for _, list := range []string{"include", "exclude", "picked"} {
		for _, submit := range []url.Values{{"do": {"add-rule"}}, {"remove-rule": {"1"}}, {"do": {"show-matches"}}} {
			form := url.Values{
				"name":               {"Exec weekly"},
				"mode":               {domain.ReportModeRules},
				"rules[0].attribute": {domain.RuleTopLevel},
				"rules[0].op":        {domain.RuleIs},
				"rules[1].attribute": {domain.RuleLifecycle},
				list:                 {id(kept.ID), id(gone.ID)},
				"remove-" + list:     {id(gone.ID)},
			}
			maps.Copy(form, submit)
			resp := postForm(t, client, ts.URL+"/reports/new", form)
			defer func() { _ = resp.Body.Close() }()
			page := readBody(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s with a ticked Remove in %s: status %d, want 200; body:\n%s", submit, list, resp.StatusCode, page)
			}
			if got, want := chips(page, list), []string{id(kept.ID)}; !slices.Equal(got, want) {
				t.Errorf("%s with %s's Remove ticked for %d leaves chips %q, want %q", submit, list, gone.ID, got, want)
			}
		}
	}

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"Exec weekly"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"include":            {id(kept.ID), id(gone.ID)},
		"remove-include":     {id(gone.ID)},
		"exclude":            {id(gone.ID), id(kept.ID)},
		"remove-exclude":     {id(gone.ID)},
	})
	defer func() { _ = resp.Body.Close() }()
	assertSavedReport(t, client, ts.URL, resp)
	resp = postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":          {"Picked weekly"},
		"mode":          {domain.ReportModePicked},
		"picked":        {id(kept.ID), id(gone.ID)},
		"remove-picked": {id(gone.ID)},
	})
	assertSavedReport(t, client, ts.URL, resp)
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 2 {
		t.Fatalf("saving made %d definitions (%v), want 2", len(defs), err)
	}
	for _, def := range defs {
		def, _ = h.Service.GetReportDefinition(context.Background(), def.ID)
		want := []int64{kept.ID}
		if def.Mode == domain.ReportModePicked && !slices.Equal(def.Picked, want) {
			t.Errorf("Save with Remove ticked picks %v, want %v", def.Picked, want)
		}
		if def.Mode == domain.ReportModeRules && (!slices.Equal(def.Include, want) || !slices.Equal(def.Exclude, want)) {
			t.Errorf("Save with Remove ticked includes %v and leaves out %v, want %v for each", def.Include, def.Exclude, want)
		}
	}
}

// Without script, a Top-level rule row offers only "is Top-level" and "is not
// Top-level": its values list, kept in the markup so script can switch the row
// to another attribute, is hidden by a style that applies only before script
// runs. Other rows keep theirs.
func TestBuilderTopLevelRowHidesItsValuesWithoutScriptOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	h.CreateDimension(boss, "Team", "Platform")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	resp := postForm(t, client, ts.URL+"/reports/new", url.Values{
		"name":               {"Platform MBR"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleTopLevel},
		"rules[0].op":        {domain.RuleIs},
		"do":                 {"add-rule"},
	})
	defer func() { _ = resp.Body.Close() }()
	page := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Add rule: status %d, want 200; body:\n%s", resp.StatusCode, page)
	}
	rows := strings.Split(pageElement(t, page, "form", "report-builder"), `data-testid="report-rule"`)[1:]
	if len(rows) != 2 {
		t.Fatalf("got %d rule rows, want the Top-level row and a new one", len(rows))
	}
	if !strings.Contains(openTag(rows[0]), "data-top-level") {
		t.Errorf("the Top-level row isn't marked data-top-level: %s", openTag(rows[0]))
	}
	if strings.Contains(openTag(rows[1]), "data-top-level") {
		t.Errorf("the new, empty row is marked data-top-level: %s", openTag(rows[1]))
	}
	if got := cssRule(t, page, `.rb-form:not(.rb-js) [data-top-level] [data-testid="rule-values"]`); got != "display:none" {
		t.Errorf("without script, a Top-level row's values are styled %q, want display:none", got)
	}
}

// Each chip carries a Remove checkbox for its Goal, which the builder's
// script hides, as its × removes the chip instead (ticket #168).
func TestBuilderChipHasARemoveCheckboxScriptHidesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}},
		Include: []int64{g.ID}, Exclude: []int64{g.ID},
	})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")

	page := getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/edit")
	for _, list := range []string{"include", "exclude"} {
		chip := pageElement(t, page, "li", list+"-chip")
		box := regexp.MustCompile(`<label class="([^"]*)"><input type="checkbox" name="remove-` + list + `" value="` + strconv.FormatInt(g.ID, 10) + `"> Remove</label>`).FindStringSubmatch(chip)
		if box == nil {
			t.Errorf("the %s chip has no Remove checkbox for its Goal:\n%s", list, chip)
			continue
		}
		if !slices.Contains(strings.Fields(box[1]), "rb-no-js") {
			t.Errorf("the %s chip's Remove is class %q, which script doesn't hide", list, box[1])
		}
	}
	if !strings.Contains(page, ".rb-form.rb-js .rb-no-js{display:none}") {
		t.Errorf("the builder's styles don't hide .rb-no-js when script runs")
	}
}

// Without script a chip's × is hidden and its Remove checkbox ends the pill,
// so the chip keeps padding on its right, as on its left, and above and below
// (ticket #314).
func TestBuilderChipKeepsItsPaddingWithoutScriptOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/new")

	css := cssRule(t, page, ".rb-form:not(.rb-js) .rb-chip")
	padding, ok := strings.CutPrefix(css, "padding:")
	sides := strings.Fields(padding)
	if !ok || len(sides) != 2 || sides[0] == "0" || sides[1] != "12px" {
		t.Errorf("without script a chip is styled %q, want padding with 12px left and right and some above and below", css)
	}
}

// The builder's header strip holds its breadcrumb, the last crumb the page
// itself, and the page's centred column opens with its title (ticket #277,
// DESIGN.md § Report builder).
func TestBuilderHeadStacksTheBreadcrumbAboveTheTitleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)

	for _, tc := range []struct {
		path, crumb, title string
		links              []string
	}{
		{"/reports/new", "Reports › New", "New report", []string{`<a class="ph-parent" href="/reports">Reports</a>`}},
		{draft + "/edit", "Reports › MBR › Edit", "Edit report", []string{`<a class="ph-parent" href="/reports">Reports</a>`, `<a class="ph-parent rp-crumb" href="` + draft + `">MBR</a>`}},
	} {
		crumb, title := reportBuilderHead(t, getBody(t, client, ts.URL+tc.path))
		if got := strings.Join(strings.Fields(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(crumb, " "))), " "); got != tc.crumb {
			t.Errorf("%s: the breadcrumb reads %q, want %q", tc.path, got, tc.crumb)
		}
		for _, link := range tc.links {
			if !strings.Contains(crumb, link) {
				t.Errorf("%s: the breadcrumb lacks %s:\n%s", tc.path, link, crumb)
			}
		}
		if last := tc.crumb[strings.LastIndex(tc.crumb, "› ")+len("› "):]; !strings.Contains(crumb, `<span class="rp-crumb" aria-current="page">`+last+`</span>`) {
			t.Errorf("%s: the last crumb %q isn't marked the current page:\n%s", tc.path, last, crumb)
		}
		if title != tc.title {
			t.Errorf("%s: the title is %q, want %q", tc.path, title, tc.title)
		}
	}
}

// reportBuilderHead checks the builder page's header strip, <header
// class="page-header">, holds only a breadcrumb nav, and that the page's
// column opens with an <h1>, and returns the nav and the title.
func reportBuilderHead(t *testing.T, page string) (crumb, title string) {
	t.Helper()
	head := between(t, page, `<header class="page-header">`, `</header>`)
	rest, ok := strings.CutPrefix(head, `<header class="page-header"><nav `)
	if !ok {
		t.Fatalf("the header strip doesn't open with a <nav>:\n%s", head)
	}
	crumb, rest, _ = strings.Cut("<nav "+rest, "</nav>")
	if !strings.Contains(openTag(crumb), `aria-label="Breadcrumb"`) {
		t.Errorf("the header strip's nav isn't the breadcrumb: %s", openTag(crumb))
	}
	if rest != "" {
		t.Errorf("the header strip holds more than the breadcrumb:\n%s", head)
	}
	title, ok = strings.CutPrefix(between(t, page, `<div class="rb-layout">`, "</h1>"), `<div class="rb-layout"><h1>`)
	if !ok {
		t.Fatalf("the builder's column doesn't open with an <h1>:\n%s", page)
	}
	return crumb, strings.TrimSuffix(title, "</h1>")
}

// The edit page's breadcrumb names the definition as saved, not as typed, on
// every round trip: a refusal, Add rule, a rule's × and Show matches (ticket
// #168).
func TestEditBuilderBreadcrumbShowsTheSavedNameOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "boss@example.com")
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)
	crumb := `<a class="ph-parent rp-crumb" href="` + draft + `">MBR</a> <span class="ph-sep" aria-hidden="true">›</span> <span class="rp-crumb" aria-current="page">Edit</span>`

	for _, tc := range []struct {
		name, typed string
		submit      url.Values
	}{
		{"a refusal", "  ", url.Values{}},
		{"Add rule", "Renamed", url.Values{"do": {"add-rule"}}},
		{"a rule's ×", "Renamed", url.Values{"remove-rule": {"0"}}},
		{"Show matches", "Renamed", url.Values{"do": {"show-matches"}}},
	} {
		form := url.Values{
			"name":               {tc.typed},
			"mode":               {domain.ReportModeRules},
			"rules[0].attribute": {domain.RuleTopLevel},
			"rules[0].op":        {domain.RuleIs},
		}
		maps.Copy(form, tc.submit)
		page := postFormBody(t, client, ts.URL+draft+"/edit", form)
		if nav, _ := reportBuilderHead(t, page); !strings.Contains(nav, crumb) {
			t.Errorf("after %s the breadcrumb isn't %s:\n%s", tc.name, crumb, nav)
		}
	}
}

// The builder offers the Owner's org beside the Owner, picking people the
// same way: an Org rule saves, its draft selects the Goals of the person and
// everyone below them, and the edit page reloads it as "Owner's org is in
// the org of <Name>" (CONTEXT.md: Org).
func TestBuilderSavesAndReloadsAnOrgRuleOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	mia := h.SignInNamed("mia@example.com", "Mia Lund")
	out := h.SignInNamed("out@example.com", "Oona Hart")
	h.SetManager(mia, vp)
	vps := h.ActiveGoal(vp, "Grow revenue", "Matters.")
	mias := h.ActiveGoal(mia, "Platform reliability", "Matters.")
	h.ActiveGoal(out, "Edge cache rollout", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")
	person := domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)

	attribute := pageElement(t, pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "form", "report-builder"), "select", "rule-attribute")
	// Priya has Mia under her, so a new definition starts with her org chosen.
	if want := `<option value="` + domain.RuleOrg + `" selected>` + reportOrgAttributeLabel + `</option>`; !strings.Contains(attribute, want) {
		t.Errorf("the attribute select doesn't offer the Owner's org %s:\n%s", want, attribute)
	}

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"Priya's org"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleOrg},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {person},
	})
	defer func() { _ = resp.Body.Close() }()
	draft := assertSavedReport(t, client, ts.URL, resp)
	want := []string{vps.Title, mias.Title}
	slices.Sort(want)
	if got := draftGoalTitles(t, draft); !slices.Equal(got, want) {
		t.Errorf("the Org rule's draft selects %q, want %q", got, want)
	}

	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 1 {
		t.Fatalf("ListReportDefinitions = %v, %v; want the one saved", defs, err)
	}
	if rules := defs[0].Rules; len(rules) != 1 || rules[0].Attribute != "org" {
		t.Errorf("the Org rule is stored as %+v, want attribute \"org\"", rules)
	}
	edit := pageElement(t, getBody(t, client, ts.URL+"/reports/"+strconv.FormatInt(defs[0].ID, 10)+"/edit"), "div", "report-rule")
	for _, want := range []string{
		`<option value="` + domain.RuleOrg + `" selected>` + reportOrgAttributeLabel + `</option>`,
		`<option value="` + domain.RuleIs + `" selected>is in the org of</option>`,
		`<option value="` + domain.RuleIsAnyOf + `">is in the org of any of</option>`,
		`<option value="` + domain.RuleIsNot + `">is not in the org of</option>`,
		`<option value="` + person + `" selected>Priya Raman</option>`,
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("the reloaded rule lacks %s:\n%s", want, edit)
		}
	}
}

// With no one's Manager known, the builder doesn't offer the Owner's org.
// A saved Org rule still loads and applies: it matches only the named
// people's own Goals.
func TestBuilderOffersTheOrgOnlyWhenAnyoneHasAManagerOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	mia := h.SignInNamed("mia@example.com", "Mia Lund")
	vps := h.ActiveGoal(vp, "Grow revenue", "Matters.")
	h.ActiveGoal(mia, "Platform reliability", "Matters.")
	def := h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{Name: "Priya's org", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
		{Attribute: domain.RuleOrg, Op: domain.RuleIs, Values: []string{strconv.FormatInt(vp.ID, 10)}},
	}})
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	attribute := pageElement(t, pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "form", "report-builder"), "select", "rule-attribute")
	if strings.Contains(attribute, reportOrgAttributeLabel) {
		t.Errorf("with no Manager known, the attribute select offers the Owner's org:\n%s", attribute)
	}

	draftURL := ts.URL + "/reports/" + strconv.FormatInt(def.ID, 10)
	edit := pageElement(t, getBody(t, client, draftURL+"/edit"), "div", "report-rule")
	for _, want := range []string{
		`<option value="` + domain.RuleOrg + `" selected>` + reportOrgAttributeLabel + `</option>`,
		`<option value="` + domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10) + `" selected>Priya Raman</option>`,
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("the saved Org rule doesn't load, lacking %s:\n%s", want, edit)
		}
	}
	if got, want := draftGoalTitles(t, getBody(t, client, draftURL)), []string{vps.Title}; !slices.Equal(got, want) {
		t.Errorf("with no Manager known, the Org rule's draft selects %q, want %q", got, want)
	}
}

// A saved Owner rule keeps a person who owns no Goal, as after a Handoff: the
// edit page still offers and selects them, so saving it back keeps them in
// the rule rather than refusing it or dropping them (ticket #265). A new
// definition still offers only the current Owners.
func TestEditBuilderKeepsAnOwnerRulesPersonWhoOwnsNoGoalOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	sam := h.SignInNamed("sam@example.com", "Sam Ito")
	h.ActiveGoal(vp, "Grow revenue", "Matters.")
	samID, vpID := strconv.FormatInt(sam.ID, 10), strconv.FormatInt(vp.ID, 10)
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")
	samOption := `<option value="` + domain.RuleOwner + "=" + samID + `" selected>Sam Ito</option>`

	for _, c := range []struct {
		op  string
		ids []string
	}{
		{domain.RuleIs, []string{samID}},
		{domain.RuleIsAnyOf, []string{samID, vpID}},
	} {
		def := h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{Name: "Owner " + c.op, Mode: domain.ReportModeRules, Rules: []domain.ReportRule{
			{Attribute: domain.RuleOwner, Op: c.op, Values: c.ids},
		}})
		draftPath := "/reports/" + strconv.FormatInt(def.ID, 10)
		if row := pageElement(t, getBody(t, client, ts.URL+draftPath+"/edit"), "div", "report-rule"); !strings.Contains(row, samOption) {
			t.Errorf("Owner %s: the edit page's rule lacks %s:\n%s", c.op, samOption, row)
		}

		form := url.Values{
			"name":               {def.Name},
			"mode":               {domain.ReportModeRules},
			"rules[0].attribute": {domain.RuleOwner},
			"rules[0].op":        {c.op},
		}
		for _, id := range c.ids {
			form.Add("rules[0].value", domain.RuleOwner+"="+id)
		}
		resp := postForm(t, noRedirects(client), ts.URL+draftPath+"/edit", form)
		defer func() { _ = resp.Body.Close() }()
		if body := readBody(t, resp); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("Owner %s: saving the edit form gives %d, want 303; body:\n%s", c.op, resp.StatusCode, body)
		}
		saved, err := h.Service.GetReportDefinition(context.Background(), def.ID)
		if err != nil {
			t.Fatalf("GetReportDefinition: %v", err)
		}
		if rules := saved.Rules; len(rules) != 1 || rules[0].Attribute != domain.RuleOwner || !slices.Equal(rules[0].Values, c.ids) {
			t.Errorf("Owner %s: the saved rule is %+v, want Owner values %q", c.op, rules, c.ids)
		}
	}

	if fresh := pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "form", "report-builder"); strings.Contains(fresh, `value="`+domain.RuleOwner+"="+samID+`"`) {
		t.Errorf("a new definition offers Sam, who owns no Goal, as an Owner:\n%s", fresh)
	}
}

// A self-reference, as a directory can give (ADR 0008), puts no one under its
// person, so as the only Manager it doesn't make the builder offer the
// Owner's org.
func TestBuilderOffersNoOrgForASelfReferenceOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	h.SetManager(vp, vp)
	h.ActiveGoal(vp, "Grow revenue", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	attribute := pageElement(t, pageElement(t, getBody(t, client, ts.URL+"/reports/new"), "form", "report-builder"), "select", "rule-attribute")
	if strings.Contains(attribute, reportOrgAttributeLabel) {
		t.Errorf("with only a self-referencing Manager, the attribute select offers the Owner's org:\n%s", attribute)
	}
}

// reportOrgAttributeLabel is the Owner's org attribute's label as the builder's HTML
// escapes it.
var reportOrgAttributeLabel = html.EscapeString("Owner's org")

// A new Report Definition starts with the author's org when anyone is under
// them: one rule row, the Owner's org "is in the org of" the author, a note
// saying so under the rules, and a rail already listing their org's Goals
// (ticket #285).
func TestNewBuilderStartsWithTheAuthorsOrgOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignInNamed("vp@example.com", "Priya Raman")
	mia := h.SignInNamed("mia@example.com", "Mia Lund")
	out := h.SignInNamed("out@example.com", "Oona Hart")
	h.SetManager(mia, vp)
	vps := h.ActiveGoal(vp, "Grow revenue", "Matters.")
	mias := h.ActiveGoal(mia, "Platform reliability", "Matters.")
	outs := h.ActiveGoal(out, "Edge cache rollout", "Matters.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "vp@example.com"), ts.URL+"/reports/new")
	rules := pageElement(t, page, "section", "report-rules")
	rows := strings.Split(rules, `data-testid="report-rule"`)[1:]
	if len(rows) != 1 {
		t.Fatalf("the new builder has %d rule rows, want 1:\n%s", len(rows), rules)
	}
	for _, want := range []string{
		`<option value="` + domain.RuleOrg + `" selected>` + reportOrgAttributeLabel + `</option>`,
		`<option value="` + domain.RuleIs + `" selected>is in the org of</option>`,
		`<option value="` + domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10) + `" selected>Priya Raman</option>`,
	} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("the new builder's rule lacks %s:\n%s", want, rows[0])
		}
	}
	if note := pageElement(t, rules, "p", "rules-start-note"); !strings.Contains(note, "Started with your org. Change or remove it.") {
		t.Errorf("the note under the rules reads %s", note)
	}
	rail := pageElement(t, page, "aside", "report-matches")
	if got := railCount(t, rail); got != 2 || !strings.Contains(rail, vps.Title) || !strings.Contains(rail, mias.Title) || strings.Contains(rail, outs.Title) {
		t.Errorf("the rail doesn't list just %q and %q:\n%s", vps.Title, mias.Title, rail)
	}
	refresh := url.Values{
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleOrg},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)},
	}
	if live, _ := postFormHX(t, signInClient(t, ts.URL, "vp@example.com"), ts.URL+"/reports/new/matches", refresh); !strings.HasPrefix(live, rail) {
		t.Errorf("the first rail lists\n%s\nbut the live rail for the started rule lists\n%s", rail, live)
	}
}

// With no one under them, a new Report Definition starts with the author's
// own Goals when they own any: Owner "is" the author, and a note saying so
// (ticket #285).
func TestNewBuilderStartsWithTheAuthorsOwnGoalsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "sam@example.com")
	sam := h.SignInNamed("sam@example.com", "Sam Rivera")
	boss := h.SignInNamed("boss@example.com", "Bo Ss")
	h.SetManager(sam, boss)
	sams := h.ActiveGoal(sam, "Edge cache rollout", "Matters.")
	bosses := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/reports/new")
	rules := pageElement(t, page, "section", "report-rules")
	rows := strings.Split(rules, `data-testid="report-rule"`)[1:]
	if len(rows) != 1 {
		t.Fatalf("the new builder has %d rule rows, want 1:\n%s", len(rows), rules)
	}
	for _, want := range []string{
		`<option value="` + domain.RuleOwner + `" selected>Owner</option>`,
		`<option value="` + domain.RuleIs + `" selected>is</option>`,
		`<option value="` + domain.RuleOwner + "=" + strconv.FormatInt(sam.ID, 10) + `" selected>Sam Rivera</option>`,
	} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("the new builder's rule lacks %s:\n%s", want, rows[0])
		}
	}
	if note := pageElement(t, rules, "p", "rules-start-note"); !strings.Contains(note, "Started with your own Goals. Change or remove it.") {
		t.Errorf("the note under the rules reads %s", note)
	}
	rail := pageElement(t, page, "aside", "report-matches")
	if got := railCount(t, rail); got != 1 || !strings.Contains(rail, sams.Title) || strings.Contains(rail, bosses.Title) {
		t.Errorf("the rail doesn't list just %q:\n%s", sams.Title, rail)
	}
}

// An author with no one under them and no Goal of their own starts with the
// blank row, and no note (ticket #285).
func TestNewBuilderStartsBlankForAnAuthorWithNoOrgOrGoalsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "sam@example.com")
	h.SignIn("sam@example.com")
	boss := h.SignIn("boss@example.com")
	h.ActiveGoal(boss, "Grow revenue", "Matters.")
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "sam@example.com"), ts.URL+"/reports/new")
	rules := pageElement(t, page, "section", "report-rules")
	rows := strings.Split(rules, `data-testid="report-rule"`)[1:]
	if len(rows) != 1 || strings.Contains(rows[0], " selected") {
		t.Errorf("the new builder doesn't start with one blank row:\n%s", rules)
	}
	if strings.Contains(rules, "rules-start-note") {
		t.Errorf("the blank start has a note:\n%s", rules)
	}
	if !strings.Contains(pageElement(t, page, "aside", "report-matches"), "Add a rule to see matching Goals") {
		t.Errorf("the blank start's rail doesn't ask for a rule")
	}
}

// Editing a saved definition shows its saved rules and no note: only a new
// one starts with a rule (ticket #285).
func TestEditBuilderShowsTheSavedRulesAndNoStartNoteOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	mia := h.SignIn("mia@example.com")
	h.SetManager(mia, vp)
	h.ActiveGoal(vp, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(vp, domain.SaveReportDefinitionInput{Name: "Top", Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}}})
	ts := newServer(t, h)

	page := getBody(t, signInClient(t, ts.URL, "vp@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/edit")
	rules := pageElement(t, page, "section", "report-rules")
	rows := strings.Split(rules, `data-testid="report-rule"`)[1:]
	if len(rows) != 1 || !strings.Contains(rows[0], `<option value="`+domain.RuleTopLevel+`" selected>Top-level</option>`) {
		t.Errorf("the edit page doesn't show just the saved Top-level rule:\n%s", rules)
	}
	if strings.Contains(rules, "rules-start-note") {
		t.Errorf("the edit page has the new builder's note:\n%s", rules)
	}
}

// The note and the started rule belong to the new builder's first render:
// Add rule keeps the rows as typed without the note, and removing the last
// row leaves the blank row, not the started rule again (ticket #285).
func TestNewBuilderStartNoteIsOnlyOnTheFirstRenderOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	mia := h.SignIn("mia@example.com")
	h.SetManager(mia, vp)
	h.ActiveGoal(vp, "Grow revenue", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")
	started := url.Values{
		"name":               {"Mine"},
		"mode":               {domain.ReportModeRules},
		"rules[0].attribute": {domain.RuleOrg},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)},
	}

	for _, tc := range []struct {
		name   string
		submit url.Values
		rows   int
	}{
		{"Add rule", url.Values{"do": {"add-rule"}}, 2},
		{"removing the last row", url.Values{"remove-rule": {"0"}}, 1},
		{"Show matches", url.Values{"do": {"show-matches"}}, 1},
	} {
		form := url.Values{}
		maps.Copy(form, started)
		maps.Copy(form, tc.submit)
		resp := postForm(t, client, ts.URL+"/reports/new", form)
		defer func() { _ = resp.Body.Close() }()
		page := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d, want 200; body:\n%s", tc.name, resp.StatusCode, page)
		}
		rules := pageElement(t, page, "section", "report-rules")
		if strings.Contains(rules, "rules-start-note") {
			t.Errorf("after %s the note is back:\n%s", tc.name, rules)
		}
		rows := strings.Split(rules, `data-testid="report-rule"`)[1:]
		if len(rows) != tc.rows {
			t.Errorf("after %s there are %d rule rows, want %d", tc.name, len(rows), tc.rows)
		}
		if tc.submit.Has("remove-rule") && len(rows) > 0 && strings.Contains(rows[0], " selected") {
			t.Errorf("after removing the last row the started rule is back:\n%s", rows[0])
		}
	}
}

// Switching a new definition to Goals I pick with the started rule still in
// the form saves a picked definition with no rules: only rules mode reads
// the rule rows (ticket #285).
func TestNewBuilderSavesPickedWithTheStartedRuleStillThereOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "vp@example.com")
	vp := h.SignIn("vp@example.com")
	mia := h.SignIn("mia@example.com")
	h.SetManager(mia, vp)
	h.ActiveGoal(vp, "Grow revenue", "Matters.")
	picked := h.ActiveGoal(mia, "Platform reliability", "Matters.")
	ts := newServer(t, h)
	client := signInClient(t, ts.URL, "vp@example.com")

	resp := postForm(t, noRedirects(client), ts.URL+"/reports/new", url.Values{
		"name":               {"Picked"},
		"mode":               {domain.ReportModePicked},
		"picked":             {strconv.FormatInt(picked.ID, 10)},
		"rules[0].attribute": {domain.RuleOrg},
		"rules[0].op":        {domain.RuleIs},
		"rules[0].value":     {domain.RuleOrg + "=" + strconv.FormatInt(vp.ID, 10)},
	})
	defer func() { _ = resp.Body.Close() }()
	assertSavedReport(t, client, ts.URL, resp)
	defs, err := h.Service.ListReportDefinitions(context.Background())
	if err != nil || len(defs) != 1 {
		t.Fatalf("ListReportDefinitions = %v, %v; want the one saved", defs, err)
	}
	def, err := h.Service.GetReportDefinition(context.Background(), defs[0].ID)
	if err != nil {
		t.Fatalf("GetReportDefinition: %v", err)
	}
	if def.Mode != domain.ReportModePicked || len(def.Rules) != 0 || !slices.Equal(def.Picked, []int64{picked.ID}) {
		t.Errorf("saved %s with rules %+v picking %v, want picked with no rules picking [%d]", def.Mode, def.Rules, def.Picked, picked.ID)
	}
}

// A rule row's × and a chip's × are 40px-square icon buttons with a
// --color-border-strong edge, matching the selects beside them, and 44px on a
// coarse pointer, not bare quiet small buttons. Each keeps its aria-label and
// title, and its behaviour: the rule's submits remove-rule, and the chip's is
// script-only (ticket #285).
func TestBuilderRemoveControlsAreBorderedIconButtonsOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	g := h.ActiveGoal(boss, "Grow revenue", "Matters.")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "MBR", Mode: domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}},
		Include: []int64{g.ID}, Exclude: []int64{g.ID},
	})
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, "boss@example.com"), ts.URL+"/reports/"+strconv.FormatInt(def.ID, 10)+"/edit")

	iconButton := func(what, button string) {
		t.Helper()
		classes := strings.Fields(attr(button, "class"))
		if !slices.Contains(classes, "btn") || !slices.Contains(classes, "rb-remove") || slices.Contains(classes, "quiet") || slices.Contains(classes, "sm") {
			t.Errorf("the %s is class %q, want btn rb-remove, not quiet or sm", what, attr(button, "class"))
		}
	}
	rule := tagAround(t, pageElement(t, page, "div", "report-rule"), `aria-label="Remove rule"`)
	iconButton("rule's remove", rule)
	if attr(rule, "title") != "Remove rule" || attr(rule, "type") != "submit" || attr(rule, "name") != "remove-rule" {
		t.Errorf("the rule's remove lost its title or its submit: %s", rule)
	}
	for _, list := range []string{"include", "exclude"} {
		chip := tagAround(t, pageElement(t, page, "li", list+"-chip"), `aria-label="Remove `+g.Title+`"`)
		iconButton(list+" chip's remove", chip)
		if attr(chip, "title") != "Remove "+g.Title || attr(chip, "type") != "button" || !slices.Contains(strings.Fields(attr(chip, "class")), "rb-js-only") {
			t.Errorf("the %s chip's remove lost its title or isn't script-only: %s", list, chip)
		}
	}

	css := cssRule(t, page, ".rb-remove")
	for _, want := range []string{"width:40px", "padding:0", "border-color:var(--color-border-strong)"} {
		if !strings.Contains(css, want) {
			t.Errorf(".rb-remove lacks %s: %s", want, css)
		}
	}
	if coarse := mediaBlock(t, between(t, page, ".rb-layout{", "</style>"), "@media (pointer:coarse)"); !strings.Contains(coarse, ".rb-remove{min-width:44px}") {
		t.Errorf("on a coarse pointer .rb-remove isn't 44px wide: %s", coarse)
	}
}

// The builder explains Also include and Leave out with a help button after
// each legend, leaving the legends, and so the pickers' names, as they were;
// Goals I pick needs no help. Its rules hint says in plain words what a rule
// can check.
func TestBuilderExplainsAlsoIncludeLeaveOutAndItsRulesOverHTTP(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	ts := newServer(t, h)
	page := getBody(t, signInClient(t, ts.URL, boss.Email), ts.URL+"/reports/new")

	for _, tc := range []struct{ picker, legend, label, id, text string }{
		{"include-picker", "Also include", "What does Also include do?", "include-help", "Goals to add by hand, even if they don't meet the rules."},
		{"exclude-picker", "Leave out", "What does Leave out do?", "exclude-help", "Goals to drop, even if they meet the rules."},
	} {
		picker := pageElement(t, page, "fieldset", tc.picker)
		if button := assertTermHelp(t, page, picker, tc.label, tc.text); attr(button, "aria-controls") != tc.id {
			t.Errorf("%q controls %q, want %s", tc.label, attr(button, "aria-controls"), tc.id)
		}
		if want := "<legend>" + tc.legend + "</legend>"; !strings.Contains(picker, want) {
			t.Errorf("%s's legend isn't exactly %s:\n%s", tc.picker, want, picker)
		}
		if !strings.Contains(picker, `<p id="`+tc.id+`" class="explain" hidden>`) {
			t.Errorf("%s's explanation isn't inside its fieldset:\n%s", tc.picker, picker)
		}
	}
	if picked := pageElement(t, page, "fieldset", "picked-picker"); strings.Contains(picked, "disclose help") {
		t.Errorf("Goals I pick has a help button:\n%s", picked)
	}

	hint := pageElement(t, page, "p", "rules-hint")
	const want = `A rule can check a Dimension value, the Owner or whose org they're in, the Lifecycle, the Health, or whether the Goal is Top-level. To let Owners opt their own Goals in, an Admin can add a Dimension such as "In report"; you still decide with Leave out.`
	if got := html.UnescapeString(strings.Join(strings.Fields(hint[len(openTag(hint))+1:]), " ")); got != want {
		t.Errorf("rules hint reads %q, want %q", got, want)
	}
	if strings.Contains(hint, "Rules can test") {
		t.Errorf("rules hint still reads the old shorthand: %s", hint)
	}
}
