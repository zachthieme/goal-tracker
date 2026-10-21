package domain_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// reportLogLines is the Report log of def, newest first, each entry as
// "<actor's label>: <what changed>".
func reportLogLines(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) []string {
	t.Helper()
	entries, err := h.Service.ReportLog(context.Background(), def.ID)
	if err != nil {
		t.Fatalf("ReportLog: %v", err)
	}
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, e.Actor.Label()+": "+e.Detail)
	}
	return lines
}

// Creating a Report Definition opens its Report log with who created it.
func TestReportLogRecordsTheDefinitionsCreation(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	g := h.CreateGoal(priya, "Grow revenue", "Revenue funds the roadmap.")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	})

	if got, want := reportLogLines(t, h, def), []string{"Priya: Created"}; !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// reportLogUpdate edits def as actor to in, failing the test on error.
func reportLogUpdate(t *testing.T, h *testsupport.Harness, actor domain.Account, def domain.ReportDefinition, in domain.SaveReportDefinitionInput) domain.ReportDefinition {
	t.Helper()
	got, err := h.Service.UpdateReportDefinition(context.Background(), actor.ID, def.ID, in)
	if err != nil {
		t.Fatalf("UpdateReportDefinition: %v", err)
	}
	return got
}

// reportLogInput is def as the input that saves it unchanged.
func reportLogInput(def domain.ReportDefinition) domain.SaveReportDefinitionInput {
	return domain.SaveReportDefinitionInput{
		Name: def.Name, Introduction: def.Introduction, Mode: def.Mode, Rules: def.Rules,
		Include: def.Include, Exclude: def.Exclude, Picked: def.Picked, FieldIDs: def.FieldIDs,
	}
}

// Renaming a Report Definition logs its title before and after, with who
// renamed it: here a Report Delegate.
func TestReportLogRecordsATitleChange(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	dana := h.SignInNamed("dana@example.com", "Dana")
	g := h.CreateGoal(priya, "Grow revenue", "Revenue funds the roadmap.")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	})
	h.AddReportDelegate(priya, dana, def)

	in := reportLogInput(def)
	in.Name = "EU monthly review"
	reportLogUpdate(t, h, dana, def, in)

	if got, want := reportLogLines(t, h, def)[0], "Dana: Title: EU MBR → EU monthly review"; got != want {
		t.Errorf("newest Report log entry = %q, want %q", got, want)
	}
}

// Changing a rules Definition's rules logs each rule added and each removed,
// in the words its selection reads in.
func TestReportLogRecordsRulesAddedAndRemoved(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	lifecycleActive := domain.ReportRule{Attribute: domain.RuleLifecycle, Op: domain.RuleIs, Values: []string{domain.LifecycleActive}}
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModeRules, Rules: []domain.ReportRule{lifecycleActive},
	})

	in := reportLogInput(def)
	in.Rules = []domain.ReportRule{{Attribute: domain.RuleOrg, Op: domain.RuleIs, Values: []string{strconv.FormatInt(priya.ID, 10)}}}
	reportLogUpdate(t, h, priya, def, in)

	got := reportLogLines(t, h, def)
	want := []string{
		"Priya: Rule removed: Active",
		"Priya: Rule added: Owner is in the org of Priya",
		"Priya: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// Changing what a rules Definition lists to Also include or to Leave out
// logs each Goal added to or removed from either list, by its title.
func TestReportLogRecordsAlsoIncludeAndLeaveOutChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	alpha := h.CreateGoal(priya, "Alpha", "why")
	beta := h.CreateGoal(priya, "Beta", "why")
	gamma := h.CreateGoal(priya, "Gamma", "why")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}},
		Include: []int64{alpha.ID},
	})

	in := reportLogInput(def)
	in.Include = []int64{beta.ID}
	in.Exclude = []int64{gamma.ID}
	reportLogUpdate(t, h, priya, def, in)

	got := reportLogLines(t, h, def)
	want := []string{
		"Priya: Leave out added: Gamma",
		"Priya: Also include removed: Alpha",
		"Priya: Also include added: Beta",
		"Priya: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// Changing a picked Definition's Goals logs each Goal picked and each no
// longer picked, and their reordering.
func TestReportLogRecordsPickedGoalChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	alpha := h.CreateGoal(priya, "Alpha", "why")
	beta := h.CreateGoal(priya, "Beta", "why")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{alpha.ID},
	})

	in := reportLogInput(def)
	in.Picked = []int64{beta.ID}
	def = reportLogUpdate(t, h, priya, def, in)
	// The order picked is the order the Report lists them in.
	in.Picked = []int64{alpha.ID, beta.ID}
	def = reportLogUpdate(t, h, priya, def, in)
	in.Picked = []int64{beta.ID, alpha.ID}
	reportLogUpdate(t, h, priya, def, in)

	got := reportLogLines(t, h, def)
	want := []string{
		"Priya: Picked Goals reordered",
		"Priya: Picked Goal added: Alpha",
		"Priya: Picked Goal removed: Alpha",
		"Priya: Picked Goal added: Beta",
		"Priya: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// Saving a Definition with nothing changed logs nothing, however its rules'
// values and its lists are ordered or repeated.
func TestReportLogSkipsAnUnchangedSave(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	alpha := h.CreateGoal(priya, "Alpha", "why")
	beta := h.CreateGoal(priya, "Beta", "why")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Introduction: "How the EU did.", Mode: domain.ReportModeRules,
		Rules:   []domain.ReportRule{{Attribute: domain.RuleLifecycle, Op: domain.RuleIsAnyOf, Values: []string{domain.LifecycleActive, domain.LifecycleOnHold}}},
		Include: []int64{alpha.ID, beta.ID},
	})

	in := reportLogInput(def)
	in.Name = " EU MBR "
	in.Introduction = "How the EU did.\n"
	in.Rules = []domain.ReportRule{{Attribute: domain.RuleLifecycle, Op: domain.RuleIsAnyOf, Values: []string{domain.LifecycleOnHold, domain.LifecycleActive, domain.LifecycleOnHold}}}
	in.Include = []int64{beta.ID, alpha.ID, beta.ID}
	reportLogUpdate(t, h, priya, def, in)

	if got, want := reportLogLines(t, h, def), []string{"Priya: Created"}; !equalStrings(got, want) {
		t.Errorf("Report log after an unchanged save = %q, want %q", got, want)
	}
}

// Changing a Definition's introduction, how it selects and the Fields it
// shows logs each; the introduction is summarised, not quoted.
func TestReportLogRecordsIntroductionModeAndFieldChanges(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Boss")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	alpha := h.CreateGoal(boss, "Alpha", "why")
	def := h.SaveReportDefinition(boss, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Introduction: "How the EU did.", Mode: domain.ReportModeRules,
		Rules: []domain.ReportRule{{Attribute: domain.RuleTopLevel, Op: domain.RuleIs}},
	})

	reportLogUpdate(t, h, boss, def, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Introduction: "How the EU did this month.", Mode: domain.ReportModePicked,
		Picked: []int64{alpha.ID}, FieldIDs: []int64{budget.ID},
	})

	got := reportLogLines(t, h, def)
	want := []string{
		"Boss: Field shown added: Budget",
		"Boss: Picked Goal added: Alpha",
		"Boss: Rule removed: Top-level",
		"Boss: Selects by: rules → picked Goals",
		"Boss: Introduction changed",
		"Boss: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}
