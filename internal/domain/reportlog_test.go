package domain_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

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

// Adding and removing a Report Delegate logs each, naming the Delegate and
// who added or removed them; removing someone who isn't one logs nothing.
func TestReportLogRecordsReportDelegatesAddedAndRemoved(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t, "boss@example.com")
	boss := h.SignInNamed("boss@example.com", "Boss")
	priya := h.SignInNamed("priya@example.com", "Priya")
	dana := h.SignInNamed("dana@example.com", "Dana")
	sam := h.SignInNamed("sam@example.com", "Sam")
	g := h.CreateGoal(priya, "Grow revenue", "why")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{
		Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	})
	ctx := context.Background()

	h.AddReportDelegate(priya, dana, def)
	if err := h.Service.RemoveReportDelegate(ctx, boss.ID, def.ID, dana.ID); err != nil {
		t.Fatalf("RemoveReportDelegate: %v", err)
	}
	if err := h.Service.RemoveReportDelegate(ctx, boss.ID, def.ID, sam.ID); err != nil {
		t.Fatalf("RemoveReportDelegate of someone who isn't one: %v", err)
	}

	got := reportLogLines(t, h, def)
	want := []string{
		"Boss: Report Delegate removed: Dana",
		"Priya: Report Delegate added: Dana",
		"Priya: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// Curating the narrative logs one entry saying which Highlights were picked
// or dropped and which sections' notes changed, summarised rather than
// quoted; re-saving the same narrative logs nothing.
func TestReportLogRecordsNarrativeCuration(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	priya := h.SignInNamed("priya@example.com", "Priya")
	eu := h.ActiveGoal(priya, "Launch in EU", "Expand the market.")
	h.CheckinWithHighlights(priya, eu.ID,
		domain.HighlightInput{Kind: domain.HighlightAccomplishment, Note: "Signed the first EU customer."},
		domain.HighlightInput{Kind: domain.HighlightMiss, Note: "Lost Globex."},
	)
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{eu.ID}})
	r, err := h.Service.DraftReport(ctx, def, time.Time{})
	if err != nil {
		t.Fatalf("DraftReport: %v", err)
	}
	signed := highlightID(t, r, "Signed the first EU customer.")
	lost := highlightID(t, r, "Lost Globex.")
	curate := func(in domain.CurateNarrativeInput) {
		t.Helper()
		if err := h.Service.CurateNarrative(ctx, priya.ID, def.ID, in); err != nil {
			t.Fatalf("CurateNarrative: %v", err)
		}
	}

	curate(domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: signed, Section: domain.HighlightAccomplishment}},
		Notes: map[string][]string{domain.HighlightInsight: {"EU is steady."}},
	})
	second := domain.CurateNarrativeInput{
		Picks: []domain.NarrativePick{{HighlightID: lost, Section: domain.HighlightMiss}},
		Notes: map[string][]string{
			domain.HighlightInsight:        {"EU is steady.", "Pricing holds."},
			domain.HighlightAccomplishment: {"  "},
		},
	}
	curate(second)
	curate(second)

	got := reportLogLines(t, h, def)
	want := []string{
		"Priya: Narrative: picked “Lost Globex.” into Misses; dropped “Signed the first EU customer.”; Insights notes changed",
		"Priya: Narrative: picked “Signed the first EU customer.” into Accomplishments; Insights notes changed",
		"Priya: Created",
	}
	if !equalStrings(got, want) {
		t.Errorf("Report log = %q, want %q", got, want)
	}
}

// Publishing a Report logs the publication with who published it.
func TestReportLogRecordsAPublication(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	priya := h.SignInNamed("priya@example.com", "Priya")
	dana := h.SignInNamed("dana@example.com", "Dana")
	g := h.ActiveGoal(priya, "Grow revenue", "why")
	def := h.SaveReportDefinition(priya, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	h.AddReportDelegate(priya, dana, def)

	h.PublishReport(dana, def)

	entries, err := h.Service.ReportLog(context.Background(), def.ID)
	if err != nil {
		t.Fatalf("ReportLog: %v", err)
	}
	if got := entries[0]; got.Kind != domain.ReportLogPublished || got.Actor.ID != dana.ID || got.Detail != "Published" {
		t.Errorf("newest Report log entry = %+v, want Dana's publication", got)
	}
}

// A change whose Report log entry can't be written is rolled back: a change
// never commits without its entries.
func TestReportLogFailureRollsTheChangeBack(t *testing.T) {
	t.Parallel()

	// world is a picked Definition Priya created, with a Highlight in scope
	// and Lee a Report Delegate on it.
	type world struct {
		h                *testsupport.Harness
		priya, dana, lee domain.Account
		def              domain.ReportDefinition
		highlight        int64
	}
	newWorld := func(t *testing.T) world {
		t.Helper()
		h := testsupport.New(t)
		w := world{h: h, priya: h.SignIn("priya@example.com"), dana: h.SignIn("dana@example.com"), lee: h.SignIn("lee@example.com")}
		g := h.ActiveGoal(w.priya, "Grow revenue", "why")
		h.CheckinWithHighlight(w.priya, g.ID, domain.HighlightAccomplishment, "Signed Acme.")
		w.def = h.SaveReportDefinition(w.priya, domain.SaveReportDefinitionInput{Name: "EU MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
		h.AddReportDelegate(w.priya, w.lee, w.def)
		r, err := h.Service.DraftReport(context.Background(), w.def, time.Time{})
		if err != nil {
			t.Fatalf("DraftReport: %v", err)
		}
		w.highlight = highlightID(t, r, "Signed Acme.")
		if _, err := h.DB.Exec(`CREATE TRIGGER fail_report_log BEFORE INSERT ON report_log
			BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
			t.Fatalf("install failing trigger: %v", err)
		}
		return w
	}
	ctx := context.Background()

	for _, change := range []struct {
		what      string
		change    func(w world) error
		unchanged func(t *testing.T, w world)
	}{
		{
			what: "creating a Definition",
			change: func(w world) error {
				_, err := w.h.Service.SaveReportDefinition(ctx, w.priya.ID, domain.SaveReportDefinitionInput{Name: "Another", Mode: domain.ReportModePicked, Picked: w.def.Picked})
				return err
			},
			unchanged: func(t *testing.T, w world) {
				if defs, err := w.h.Service.ListReportDefinitions(ctx); err != nil || len(defs) != 1 {
					t.Errorf("Report Definitions = %d (%v), want only the first", len(defs), err)
				}
			},
		},
		{
			what: "editing it",
			change: func(w world) error {
				in := reportLogInput(w.def)
				in.Name = "EU monthly review"
				_, err := w.h.Service.UpdateReportDefinition(ctx, w.priya.ID, w.def.ID, in)
				return err
			},
			unchanged: func(t *testing.T, w world) {
				if got, err := w.h.Service.GetReportDefinition(ctx, w.def.ID); err != nil || got.Name != "EU MBR" {
					t.Errorf("name = %q (%v), want EU MBR", got.Name, err)
				}
			},
		},
		{
			what:   "adding a Report Delegate",
			change: func(w world) error { return w.h.Service.AddReportDelegate(ctx, w.priya.ID, w.def.ID, w.dana.ID) },
			unchanged: func(t *testing.T, w world) {
				if got := reportDelegateEmails(t, w.h, w.def); !equalStrings(got, []string{"lee@example.com"}) {
					t.Errorf("Report Delegates = %v, want [lee@example.com]", got)
				}
			},
		},
		{
			what:   "removing a Report Delegate",
			change: func(w world) error { return w.h.Service.RemoveReportDelegate(ctx, w.priya.ID, w.def.ID, w.lee.ID) },
			unchanged: func(t *testing.T, w world) {
				if got := reportDelegateEmails(t, w.h, w.def); !equalStrings(got, []string{"lee@example.com"}) {
					t.Errorf("Report Delegates = %v, want [lee@example.com]", got)
				}
			},
		},
		{
			what: "curating the narrative",
			change: func(w world) error {
				return w.h.Service.CurateNarrative(ctx, w.priya.ID, w.def.ID, domain.CurateNarrativeInput{
					Picks: []domain.NarrativePick{{HighlightID: w.highlight, Section: domain.HighlightAccomplishment}},
				})
			},
			unchanged: func(t *testing.T, w world) {
				if r, err := w.h.Service.DraftReport(ctx, w.def, time.Time{}); err != nil || len(r.Narrative) != 0 {
					t.Errorf("narrative = %+v (%v), want none", r.Narrative, err)
				}
			},
		},
		{
			what: "publishing it",
			change: func(w world) error {
				_, err := w.h.Service.PublishReport(ctx, w.priya.ID, w.def.ID, time.Time{})
				return err
			},
			unchanged: func(t *testing.T, w world) {
				if pubs, err := w.h.Service.ListPublications(ctx, w.def.ID); err != nil || len(pubs) != 0 {
					t.Errorf("publications = %d (%v), want none", len(pubs), err)
				}
			},
		},
	} {
		t.Run(change.what, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			if err := change.change(w); err == nil || !strings.Contains(err.Error(), "injected failure") {
				t.Fatalf("%s: err = %v, want the injected failure", change.what, err)
			}
			change.unchanged(t, w)
		})
	}
}
