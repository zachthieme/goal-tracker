package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Report's draft page, and a plain Add another that answers it around the
// compose form as posted, read through one page loader, once (ADR 0010,
// #606): Service.ReportDraftPage reads the draft, the viewer, the Report
// Delegates, the publications and the Report log once each, judging the
// viewer and the open Action Items against what it read, and the page, an
// Add another the domain accepts and one it refuses each run it once, and as
// many queries for 50 Goals as for 10.
//
// Some queries run more than once all the same:
//   - DraftReport's own repeats, which stay as it is: it drafts for
//     publishing and the Report list too;
//   - an Account looked up one at a time: the session's viewer, the loader's,
//     and in DraftReport an exception block's Ask for help addressee and each
//     open Action Item's owner and raiser;
//   - on an Add another, what PreviewNarrative and, for one it accepts,
//     NarrativeUnsaved read: the posted narrative's draft beside the page's
//     saved one.
//
// No other query runs twice.
func TestReportDraftPageReadsEachThingOnce(t *testing.T) {
	t.Parallel()
	// reportDraftLoaderMax is what Service.ReportDraftPage reads for the
	// Weekly Report shown to its Admin creator, with one open Action Item:
	// DraftReport's reads, the viewer, the Report Delegates, the publications
	// and the Report log.
	const reportDraftLoaderMax = 31
	// The draft page adds the Report Definition the path names and the
	// session's and the sidebar's reads.
	const reportDraftPageMax = 49
	// A plain Add another adds what PreviewNarrative and NarrativeUnsaved
	// read: the narrative's check, its permission and the posted draft.
	const reportAddAnotherMax = 87
	// One the domain refuses stops PreviewNarrative at the narrative's check,
	// before it drafts.
	const reportAddAnotherRefusedMax = 57
	// draftOwn is why DraftReport's own repeats stay as they are.
	const draftOwn = "DraftReport's own repeat, which stays: it drafts for publishing and the Report list too"
	draftRereads := []reportDraftReread{
		{"LatestReportPublication", 2, draftOwn},
		{"ListCheckinsForGoals", 3, draftOwn},
		{"ListLatestCheckinsForGoals", 2, draftOwn},
		{"ListMilestonesForGoals", 2, draftOwn},
	}
	loaderAccounts := reportDraftReread{"GetAccount", 4, "people looked up one at a time: the viewer, and DraftReport's exception block's Ask for help addressee and its Action Item's owner and raiser"}
	pageAccounts := reportDraftReread{"GetAccount", 5, "people looked up one at a time: the session's viewer, the loader's, and DraftReport's Ask for help addressee and Action Item owner and raiser"}
	// What PreviewNarrative and NarrativeUnsaved read for an Add another,
	// the page runs again, as many more times as they do.
	const postedDraft = "PreviewNarrative or NarrativeUnsaved reads it again: the posted narrative's draft beside the page's saved one"

	type draftPath struct {
		name   string
		max    int
		status int
		// post is the Add another posted, nil for the page itself, and in
		// the narrative it posts, accepted when the domain accepts it, so
		// the answer reads NarrativeUnsaved too.
		post     url.Values
		in       domain.CurateNarrativeInput
		accepted bool
	}
	// paths are the paths for hl, a Highlight the Report offers.
	paths := func(hl int64) []draftPath {
		id := strconv.FormatInt(hl, 10)
		return []draftPath{
			{name: "the page", max: reportDraftPageMax, status: http.StatusOK},
			{
				name: "an Add another the domain accepts", max: reportAddAnotherMax, status: http.StatusOK,
				post: url.Values{
					"offered": {id}, "include-" + id: {"on"}, "pick-" + id: {domain.HighlightMiss},
					"text-" + domain.HighlightMiss: {"The campaign underdelivered."}, "add_note": {domain.HighlightMiss},
				},
				in: domain.CurateNarrativeInput{
					Picks:   []domain.NarrativePick{{HighlightID: hl, Section: domain.HighlightMiss}},
					Notes:   map[string][]string{domain.HighlightMiss: {"The campaign underdelivered."}},
					Offered: []int64{hl},
				},
				accepted: true,
			},
			// Ticked with no section, which the narrative refuses.
			{
				name: "an Add another the domain refuses", max: reportAddAnotherRefusedMax, status: http.StatusUnprocessableEntity,
				post: url.Values{"offered": {id}, "include-" + id: {"on"}, "add_note": {domain.HighlightMiss}},
				in: domain.CurateNarrativeInput{
					Picks:   []domain.NarrativePick{{HighlightID: hl}},
					Notes:   map[string][]string{},
					Offered: []int64{hl},
				},
			},
		}
	}

	type counts struct {
		loader int
		paths  map[string]int
	}
	countFor := func(n int) counts {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedQueryCountFixture(t, h, n)
		admin := h.SignIn(queryCountAdmin)
		pubs, err := h.Service.ListPublications(context.Background(), fx.report.ID)
		if err != nil || len(pubs) != 1 {
			t.Fatalf("ListPublications = %d publications, %v; want the fixture's 1", len(pubs), err)
		}
		if _, err := h.Service.RaiseActionItem(context.Background(), admin.ID, domain.RaiseActionItemInput{
			PublicationID: pubs[0].ID, Text: "Rerun the campaign.", OwnerID: h.SignIn("priya@example.com").ID,
			DueDate: h.Clock.Now().AddDate(0, 0, 7),
		}); err != nil {
			t.Fatalf("RaiseActionItem: %v", err)
		}
		hl := draftHighlightID(t, h, fx.report, "The campaign for Goal 02 underdelivered.")
		ts, svc, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()

		if _, err := svc.ReportDraftPage(context.Background(), admin.ID, fx.report, time.Time{}); err != nil {
			t.Fatalf("ReportDraftPage: %v", err)
		}
		loader := queries.take()
		reportDraftAssertRereads(t, fmt.Sprintf("ReportDraftPage for %d Goals", n), countQueryNames(loader),
			reportDraftRereadsAllowed(append(slices.Clone(draftRereads), loaderAccounts)))
		out := counts{loader: len(loader), paths: map[string]int{}}

		draft := fmt.Sprintf("%s/reports/%d", ts.URL, fx.report.ID)
		for _, p := range paths(hl) {
			allowed := reportDraftRereadsAllowed(append(slices.Clone(draftRereads), pageAccounts))
			var resp *http.Response
			if p.post == nil {
				resp, err = ctxGet(t, client, draft)
				if err != nil {
					t.Fatalf("GET the draft page: %v", err)
				}
			} else {
				_, err := svc.PreviewNarrative(context.Background(), admin.ID, fx.report, time.Time{}, p.in)
				if p.accepted != (err == nil) {
					t.Fatalf("%s: PreviewNarrative = %v", p.name, err)
				}
				if p.accepted {
					if _, err := svc.NarrativeUnsaved(context.Background(), fx.report.ID, p.in); err != nil {
						t.Fatalf("NarrativeUnsaved: %v", err)
					}
				}
				for name, times := range countQueryNames(queries.take()) {
					allowed[name] = reportDraftReread{name, max(allowed[name].times, 1) + times, postedDraft}
				}
				resp = postForm(t, client, draft+"/narrative", p.post)
			}
			body := readBody(t, resp)
			_ = resp.Body.Close()
			if resp.StatusCode != p.status {
				t.Fatalf("%s for %d Goals: status %d, want %d", p.name, n, resp.StatusCode, p.status)
			}
			if !hasTestID(parsePage(t, body), "report-draft") {
				t.Errorf("%s for %d Goals doesn't answer the whole draft page", p.name, n)
			}
			ran := countQueryNames(queries.take())
			reportDraftAssertRereads(t, fmt.Sprintf("%s for %d Goals", p.name, n), ran, allowed)
			total := 0
			for _, times := range ran {
				total += times
			}
			// Only the loader reads the Report log, so it ran once.
			if got := ran["ListReportLog"]; got != 1 {
				t.Errorf("%s read the draft page %d times for %d Goals; want once", p.name, got, n)
			}
			out.paths[p.name] = total
		}
		return out
	}
	ten, fifty := countFor(10), countFor(50)
	if ten.loader > reportDraftLoaderMax {
		t.Errorf("ReportDraftPage ran %d queries; want at most %d", ten.loader, reportDraftLoaderMax)
	}
	if ten.loader != fifty.loader {
		t.Errorf("ReportDraftPage ran %d queries for 10 Goals and %d for 50; want the same", ten.loader, fifty.loader)
	}
	for _, p := range paths(0) {
		if got := ten.paths[p.name]; got > p.max {
			t.Errorf("%s ran %d queries; want at most %d", p.name, got, p.max)
		}
		if ten.paths[p.name] != fifty.paths[p.name] {
			t.Errorf("%s ran %d queries for 10 Goals and %d for 50; want the same", p.name, ten.paths[p.name], fifty.paths[p.name])
		}
	}
}

// reportDraftReread is a query a path on a Report's draft page may run more
// than once, how many times at most, and why.
type reportDraftReread struct {
	query string
	times int
	why   string
}

// reportDraftRereadsAllowed is rereads by their query's name.
func reportDraftRereadsAllowed(rereads []reportDraftReread) map[string]reportDraftReread {
	out := make(map[string]reportDraftReread, len(rereads))
	for _, r := range rereads {
		out[r.query] = r
	}
	return out
}

// reportDraftAssertRereads fails t for each query what ran, by name, more
// often than once and than allowed lets it.
func reportDraftAssertRereads(t *testing.T, what string, ran map[string]int, allowed map[string]reportDraftReread) {
	t.Helper()
	for name, times := range ran {
		r, ok := allowed[name]
		switch {
		case !ok && times > 1:
			t.Errorf("%s ran %s %d times; want it once", what, name, times)
		case ok && times > r.times:
			t.Errorf("%s ran %s %d times; want it at most %d (%s)", what, name, times, r.times, r.why)
		}
	}
}

// The draft's htmx narrative fragments read what they show and no more
// (#636): an htmx Save narrative answers the Narrative section as saved, the
// save status, Publish…'s summary and the History from the draft, its
// publications and its Report log, after CurateNarrative has saved, and an
// htmx Add another answers the Narrative section as posted from the
// narrative PreviewNarrative drafts, or the draft as saved when it refuses
// the narrative, and the publications. Neither reads through
// Service.ReportDraftPage: it would add the scope, the viewer's permissions
// and the Action Items' that the fragments don't show. Each runs as many
// queries for 50 Goals as for 10, and repeats only what the draft page may
// and what the domain call it answers reads before it.
func TestReportDraftNarrativeFragmentsReadEachThingOnce(t *testing.T) {
	t.Parallel()
	// An htmx Save narrative runs the session's and the sidebar's reads,
	// CurateNarrative's, and the Report Definition, the draft, the
	// publications and the Report log.
	const narrativeSavedMax = 64
	// An htmx Add another the domain accepts runs the session's and the
	// sidebar's reads, the Report Definition, PreviewNarrative's and
	// NarrativeUnsaved's, and the publications.
	const narrativeAddAnotherMax = 57
	// One it refuses stops PreviewNarrative at the narrative's check, and
	// reads the draft as saved in its stead.
	const narrativeAddAnotherRefusedMax = 54
	const draftOwn = "DraftReport's own repeat, which stays: it drafts for publishing and the Report list too"
	rereads := []reportDraftReread{
		{"LatestReportPublication", 2, draftOwn},
		{"ListCheckinsForGoals", 3, draftOwn},
		{"ListLatestCheckinsForGoals", 2, draftOwn},
		{"ListMilestonesForGoals", 2, draftOwn},
		{"GetAccount", 4, "people looked up one at a time: the session's viewer, and DraftReport's Ask for help addressee and Action Item owner and raiser"},
	}
	// What the domain call a fragment answers reads before it, the fragment
	// may run again, as many more times as it does.
	const domainFirst = "the domain call the fragment answers reads it first: CurateNarrative, or PreviewNarrative and NarrativeUnsaved"

	type fragmentPath struct {
		name string
		max  int
		post url.Values
		in   domain.CurateNarrativeInput
		// save is whether it saves the narrative, and accepted, for an Add
		// another, whether the domain accepts it.
		save, accepted bool
	}
	// paths are the paths for hl, a Highlight the Report offers.
	paths := func(hl int64) []fragmentPath {
		id := strconv.FormatInt(hl, 10)
		picked := domain.CurateNarrativeInput{
			Picks:   []domain.NarrativePick{{HighlightID: hl, Section: domain.HighlightMiss}},
			Notes:   map[string][]string{domain.HighlightMiss: {"The campaign underdelivered."}},
			Offered: []int64{hl},
		}
		return []fragmentPath{
			{
				name: "an htmx Save narrative", max: narrativeSavedMax,
				post: url.Values{
					"offered": {id}, "include-" + id: {"on"}, "pick-" + id: {domain.HighlightMiss},
					"text-" + domain.HighlightMiss: {"The campaign underdelivered."},
				},
				in: picked, save: true,
			},
			{
				name: "an htmx Add another the domain accepts", max: narrativeAddAnotherMax,
				post: url.Values{
					"offered": {id}, "include-" + id: {"on"}, "pick-" + id: {domain.HighlightMiss},
					"text-" + domain.HighlightMiss: {"The campaign underdelivered."}, "add_note": {domain.HighlightMiss},
				},
				in: picked, accepted: true,
			},
			// Ticked with no section, which the narrative refuses.
			{
				name: "an htmx Add another the domain refuses", max: narrativeAddAnotherRefusedMax,
				post: url.Values{"offered": {id}, "include-" + id: {"on"}, "add_note": {domain.HighlightMiss}},
				in: domain.CurateNarrativeInput{
					Picks:   []domain.NarrativePick{{HighlightID: hl}},
					Notes:   map[string][]string{},
					Offered: []int64{hl},
				},
			},
		}
	}

	countFor := func(n int) map[string]int {
		h := testsupport.New(t, queryCountAdmin)
		fx := seedQueryCountFixture(t, h, n)
		admin := h.SignIn(queryCountAdmin)
		pubs, err := h.Service.ListPublications(context.Background(), fx.report.ID)
		if err != nil || len(pubs) != 1 {
			t.Fatalf("ListPublications = %d publications, %v; want the fixture's 1", len(pubs), err)
		}
		if _, err := h.Service.RaiseActionItem(context.Background(), admin.ID, domain.RaiseActionItemInput{
			PublicationID: pubs[0].ID, Text: "Rerun the campaign.", OwnerID: h.SignIn("priya@example.com").ID,
			DueDate: h.Clock.Now().AddDate(0, 0, 7),
		}); err != nil {
			t.Fatalf("RaiseActionItem: %v", err)
		}
		hl := draftHighlightID(t, h, fx.report, "The campaign for Goal 02 underdelivered.")
		ts, svc, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		queries.take()

		out := map[string]int{}
		narrative := fmt.Sprintf("%s/reports/%d/narrative", ts.URL, fx.report.ID)
		for _, p := range paths(hl) {
			allowed := reportDraftRereadsAllowed(slices.Clone(rereads))
			// The domain call first, on the counting Service. Saving first
			// makes the posted Save a re-save that changes nothing, which
			// reads as one that does.
			if p.save {
				if err := svc.CurateNarrative(context.Background(), admin.ID, fx.report.ID, p.in); err != nil {
					t.Fatalf("CurateNarrative: %v", err)
				}
			} else {
				_, err := svc.PreviewNarrative(context.Background(), admin.ID, fx.report, time.Time{}, p.in)
				if p.accepted != (err == nil) {
					t.Fatalf("%s: PreviewNarrative = %v", p.name, err)
				}
				if p.accepted {
					if _, err := svc.NarrativeUnsaved(context.Background(), fx.report.ID, p.in); err != nil {
						t.Fatalf("NarrativeUnsaved: %v", err)
					}
				}
			}
			for name, times := range countQueryNames(queries.take()) {
				allowed[name] = reportDraftReread{name, max(allowed[name].times, 1) + times, domainFirst}
			}
			body, status := postFormHX(t, client, narrative, p.post)
			if status != http.StatusOK {
				t.Fatalf("%s for %d Goals: status %d, want %d", p.name, n, status, http.StatusOK)
			}
			if !hasTestID(parsePage(t, body), "report-curation") {
				t.Errorf("%s for %d Goals doesn't answer the Narrative section", p.name, n)
			}
			ran := countQueryNames(queries.take())
			reportDraftAssertRereads(t, fmt.Sprintf("%s for %d Goals", p.name, n), ran, allowed)
			total := 0
			for _, times := range ran {
				total += times
			}
			out[p.name] = total
		}
		return out
	}
	ten, fifty := countFor(10), countFor(50)
	for _, p := range paths(0) {
		if got := ten[p.name]; got > p.max {
			t.Errorf("%s ran %d queries; want at most %d", p.name, got, p.max)
		}
		if ten[p.name] != fifty[p.name] {
			t.Errorf("%s ran %d queries for 10 Goals and %d for 50; want the same", p.name, ten[p.name], fifty[p.name])
		}
	}
}
