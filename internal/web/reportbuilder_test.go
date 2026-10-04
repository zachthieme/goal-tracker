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
