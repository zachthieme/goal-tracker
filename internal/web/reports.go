package web

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
)

// reportsListData is what the reports page needs: a summary of each saved
// definition, in the order the status table lists them, and the org's
// timezone to date their publications in. YourActionItems and
// OrgActionItems are the open Action Items the viewer has reason to chase:
// those they are party to, and those only their Org is. Each published
// Report's row downloads its last publication.
type reportsListData struct {
	Reports         []domain.ReportSummary
	Loc             *time.Location
	YourActionItems []domain.ActionItemListing
	OrgActionItems  []domain.ActionItemListing
	// PDFFile is whether a publication downloads as a PDF file, or as its
	// print page to print from the browser.
	PDFFile bool
}

// handleReports lists the open Action Items the viewer has reason to chase,
// then shows every saved Report Definition as a row of a status table and
// links to the builder for a new one (CONTEXT.md: Report Definition). Anyone
// signed in may save one.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request, current domain.Account) {
	data, err := s.reportsList(r, current)
	if err != nil {
		s.serverError(w, r, "could not load reports", err)
		return
	}
	render(w, r, http.StatusOK, reportsPage(&current, data))
}

// handleViewReport shows a saved Report Definition, its draft Report, and its
// publications. The draft is recomputed on each view against the baseline the
// reader picks (the baseline query parameter, a date) or by default the
// previous publication, or 30 days ago when there is none.
func (s *Server) handleViewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.reportFromPath(w, r)
	if !ok {
		return
	}
	// Closing an Action Item comes back to this draft, baseline and all, and
	// a refused one to its form here.
	r = takeActionItemRefusal(w, r)
	if v, ok := s.loadReportDraftView(w, r, current, def, r.URL.RequestURI()); ok {
		render(w, r, http.StatusOK, reportDraftPage(&current, v))
	}
}

// reportFromPath loads the Report Definition the path names, writing Not found
// when there is none.
func (s *Server) reportFromPath(w http.ResponseWriter, r *http.Request) (domain.ReportDefinition, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return domain.ReportDefinition{}, false
	}
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return domain.ReportDefinition{}, false
		}
		s.serverError(w, r, "could not load report", err)
		return domain.ReportDefinition{}, false
	}
	return def, true
}

// reportDraftView is everything a Report's draft page shows: the draft Report
// and, as of AsOf (now), the Paths to Green it judges overdue; its discussion,
// the open Action Items its forms come back from; the company's timezone, Loc,
// its times are shown in; the baseline chip; what its definition selects,
// Panel; what the reader may do to it, Management; its History; and the
// compose form and save status.
type reportDraftView struct {
	Report     domain.Report
	AsOf       time.Time
	Discussion discussion
	Loc        *time.Location
	Chip       baselineChip
	Panel      goalsPanel
	Management reportManagement
	History    []domain.ReportHistoryEntry
	Form       narrativeForm
	Status     string
}

// reportDraftView reads def's draft page as current sees it, against
// baseline (zero for the default), through its loader, once (ADR 0010). Its
// forms come back to back. The compose form shows the narrative as saved, and
// the save status says nothing.
func (s *Server) reportDraftView(r *http.Request, current domain.Account, def domain.ReportDefinition, baseline time.Time, back string) (reportDraftView, error) {
	p, err := s.svc.ReportDraftPage(r.Context(), current.ID, def, baseline)
	if err != nil {
		return reportDraftView{}, err
	}
	m := newReportManagement(def, p.Permissions)
	chip := newBaselineChip(p.Report, r.URL.Query().Get("baseline") != "", len(p.Publications) > 0, s.svc.Now(), s.svc.Timezone())
	return reportDraftView{
		Report:     p.Report,
		AsOf:       s.svc.Now(),
		Discussion: s.draftDiscussion(current, p.Report.ActionItems, p.ActionItemPermissions, back),
		Loc:        s.svc.Timezone(),
		Chip:       chip,
		Panel:      draftGoalsPanel(def, p.Scope, m),
		Management: m,
		History:    domain.ReportHistory(p.Publications, p.Log),
		Form:       savedNarrativeForm(p.Report, chip),
	}, nil
}

// loadReportDraftView is reportDraftView against the baseline the reader
// picked, its forms coming back to draft: refused, written to w, for a
// baseline that isn't a date (reportBaseline), and the server-error page when
// the page can't be read. It reports whether the request may go on.
func (s *Server) loadReportDraftView(w http.ResponseWriter, r *http.Request, current domain.Account, def domain.ReportDefinition, draft string) (reportDraftView, bool) {
	baseline, ok := s.reportBaseline(w, r, "Can't show Report", def.ID)
	if !ok {
		return reportDraftView{}, false
	}
	v, err := s.reportDraftView(r, current, def, baseline, draft)
	if err != nil {
		s.serverError(w, r, "could not load the draft", err)
		return reportDraftView{}, false
	}
	return v, true
}

// reportManagement is what a Report's draft page lets its reader do to the
// Report Definition: change, curate and publish it (its creator, a Report
// Delegate or an Admin), and add and remove its Report Delegates (its creator
// or an Admin); and who those Report Delegates are (CONTEXT.md: Report
// Delegate).
type reportManagement struct {
	DefID           int64
	Manage          bool
	ManageDelegates bool
	Delegates       []domain.Account
	// Email and Refused are a refused add: the email posted, and why.
	Email   string
	Refused string
}

// newReportManagement is what p, the viewer's permissions on def, let them do
// to it, as its draft page offers it.
func newReportManagement(def domain.ReportDefinition, p domain.ReportPermissions) reportManagement {
	return reportManagement{
		DefID:           def.ID,
		Manage:          p.Manage,
		ManageDelegates: p.ManageDelegates,
		Delegates:       p.Delegates,
	}
}

// handleEditReportForm shows the builder filled with the saved Report
// Definition, to its creator, a Report Delegate or an Admin (CONTEXT.md:
// Report Definition).
func (s *Server) handleEditReportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.editableReport(w, r, current)
	if !ok {
		return
	}
	v := builderFromDefinition(def)
	if err := s.loadReportBuilder(r, &v); err != nil {
		s.serverError(w, r, "could not load the builder", err)
		return
	}
	render(w, r, http.StatusOK, reportBuilderPage(&current, v))
}

// handleEditReport saves the builder's changes to a Report Definition and
// lands on its draft, as submitReportBuilder answers. Only the draft changes;
// its publications stay as they were published.
func (s *Server) handleEditReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.editableReport(w, r, current)
	if !ok {
		return
	}
	s.submitReportBuilder(w, r, current, def, func(in domain.SaveReportDefinitionInput) (domain.ReportDefinition, error) {
		return s.svc.UpdateReportDefinition(r.Context(), current.ID, def.ID, in)
	})
}

// editableReport loads the Report Definition the path names for current to
// edit, writing Not found when there is none and the refusal page, 403,
// linking Back to its draft, when the domain refuses current as its editor
// (domain.Service.RequireReportEditor).
func (s *Server) editableReport(w http.ResponseWriter, r *http.Request, current domain.Account) (domain.ReportDefinition, bool) {
	def, ok := s.reportFromPath(w, r)
	if !ok {
		return domain.ReportDefinition{}, false
	}
	if err := s.svc.RequireReportEditor(r.Context(), current.ID, def); err != nil {
		s.refuse(w, r, "Can't edit Report", "/reports/"+strconv.FormatInt(def.ID, 10), err)
		return domain.ReportDefinition{}, false
	}
	return def, true
}

// builderFromDefinition is the builder as the saved def fills it: its name,
// introduction, mode, rule rows, listed Goals and Fields.
func builderFromDefinition(def domain.ReportDefinition) reportBuilderView {
	v := reportBuilderView{
		ID:            def.ID,
		Saved:         def.Name,
		Name:          def.Name,
		Introduction:  def.Introduction,
		Mode:          def.Mode,
		PickedIDs:     def.Picked,
		IncludeIDs:    def.Include,
		ExcludeIDs:    def.Exclude,
		FieldIDs:      def.FieldIDs,
		SavedFieldIDs: def.FieldIDs,
	}
	for _, rule := range def.Rules {
		row := reportRuleRow{Attribute: rule.Attribute, Op: rule.Op}
		if rule.Attribute == domain.RuleDimension {
			row.Attribute = dimensionAttribute(rule.DimensionID)
		}
		for _, value := range rule.Values {
			row.Values = append(row.Values, row.Attribute+"="+value)
		}
		v.Rules = append(v.Rules, row)
	}
	return v
}

// goalsPanel is what the draft says of the Goals its definition selects: the
// definition's scope in plain words, which its meta line reads, and Edit,
// where the reader edits the definition, which the draft's header links to,
// and "" for a reader who may not.
type goalsPanel struct {
	Scope string
	Edit  string
}

// draftGoalsPanel says what def selects, scope, linking to Edit definition
// for a reader m lets manage it.
func draftGoalsPanel(def domain.ReportDefinition, scope string, m reportManagement) goalsPanel {
	panel := goalsPanel{Scope: scope}
	if m.Manage {
		panel.Edit = "/reports/" + strconv.FormatInt(def.ID, 10) + "/edit"
	}
	return panel
}

// baselineChip is the draft header's "Changes since …" menu, and the baseline
// the draft's forms carry on their action so it stays in the URL.
type baselineChip struct {
	// Label is the chip's bold part: "last publication · Sep 22", "30 days
	// ago · Sep 4" or, for a chosen date, "Sep 4".
	Label string
	// Draft is the draft's path with no baseline: the Last publication item.
	Draft string
	// Published offers the Last publication item; there is none to offer
	// before the first publication.
	Published bool
	// ThirtyDaysAgo is today minus 30 days in the org's calendar, as a date.
	ThirtyDaysAgo string
	// Query is "?baseline=<date>" when the reader chose a date, and empty for
	// the default, so the draft's forms read against the same baseline.
	Query string
}

// newBaselineChip builds the chip for a draft read against a date the reader
// chose (chosen) or the default, which is the previous publication when one
// exists (published) and 30 days before now in loc otherwise.
func newBaselineChip(r domain.Report, chosen, published bool, now time.Time, loc *time.Location) baselineChip {
	y, m, d := now.In(loc).Date()
	c := baselineChip{
		Draft:         "/reports/" + strconv.FormatInt(r.Definition.ID, 10),
		Published:     published,
		ThirtyDaysAgo: fmtDate(time.Date(y, m, d-30, 0, 0, 0, 0, time.UTC)),
	}
	switch {
	case chosen:
		c.Label = showDateYear(r.Baseline)
		c.Query = "?baseline=" + url.QueryEscape(fmtDate(r.Baseline))
	case published:
		c.Label = "last publication · " + showDateYear(r.Baseline)
	default:
		c.Label = "30 days ago · " + showDateYear(r.Baseline)
	}
	return c
}

// publishSummary is what Publish… says the publication will freeze: the Goals
// in the draft, its exceptions, the Highlights picked into its saved
// narrative, and the baseline as the chip labels it.
func publishSummary(r domain.Report, chip baselineChip) string {
	highlights := 0
	for _, s := range r.Narrative {
		highlights += len(s.Highlights)
	}
	return fmt.Sprintf("%s · %s · %s · changes since %s. Readers can comment on the frozen copy.",
		plural(len(r.Exceptions)+len(r.Lines), "Goal"), plural(len(r.Exceptions), "exception"), plural(highlights, "Highlight"), chip.Label)
}

// handleCurateNarrative sets the narrative of a Report Definition's next
// publication from the draft page's form (see narrativeFormInput): Save
// narrative. It redirects back to the draft, against the baseline the reader
// picked (the baseline query parameter), if any. An htmx post gets the
// Narrative section as saved, the save status and what else the save changed
// instead (see renderNarrativeSaved), and a refusal changes only the save status, so the
// author's typing stays. Add another (add_note={section}) saves nothing: it
// re-renders the form as posted with one more note box in that section (see
// renderNarrativeFormAgain).
func (s *Server) handleCurateNarrative(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	in, ok := narrativeFormInput(w, r)
	if !ok {
		return
	}
	if addNote := r.Form.Get("add_note"); addNote != "" {
		if !slices.Contains(narrativeSections, addNote) {
			http.Error(w, "unknown narrative section", http.StatusBadRequest)
			return
		}
		s.renderNarrativeFormAgain(w, r, current, id, in, addNote)
		return
	}
	hx := isHTMX(r)
	target := narrativeDraftPath(r, id)
	if err := s.svc.CurateNarrative(r.Context(), current.ID, id, in); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			s.notFound(w, r)
		case hx && errors.Is(err, domain.ErrValidation):
			answerUnswapped(w, r, saveStatus("Not saved: "+refusalSentence(err), true))
		default:
			s.refuse(w, r, "Can't save narrative", target, err)
		}
		return
	}
	if hx {
		s.renderNarrativeSaved(w, r, id)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// narrativeFormInput reads the narrative the compose form posts: the in-scope
// Highlights the author ticked (include-{highlight}), each with its section
// (pick-{highlight}, which is ignored for a Highlight left unticked), the
// author's notes in each section (text-{section}, once per note box, in
// order), and the Highlights the form offered (offered, once each), so saved
// picks of those it didn't offer stay as saved. A form it can't read is Bad
// Request, written to w.
func narrativeFormInput(w http.ResponseWriter, r *http.Request) (domain.CurateNarrativeInput, bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return domain.CurateNarrativeInput{}, false
	}
	// A form offering no Highlights posts no offered, and offered nothing.
	in := domain.CurateNarrativeInput{Notes: map[string][]string{}, Offered: []int64{}}
	for _, raw := range r.Form["offered"] {
		hlID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "unknown Highlight", http.StatusBadRequest)
			return domain.CurateNarrativeInput{}, false
		}
		in.Offered = append(in.Offered, hlID)
	}
	for key := range r.Form {
		raw, ok := strings.CutPrefix(key, "include-")
		if !ok {
			continue
		}
		hlID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "unknown Highlight", http.StatusBadRequest)
			return domain.CurateNarrativeInput{}, false
		}
		// A missing section reaches the domain empty, which refuses it.
		in.Picks = append(in.Picks, domain.NarrativePick{HighlightID: hlID, Section: r.Form.Get("pick-" + raw)})
	}
	for _, section := range narrativeSections {
		in.Notes[section] = r.Form["text-"+section]
	}
	return in, true
}

// reportBaseline is the baseline the reader picked for the Report Definition
// id's Report, the baseline query parameter, a date, or the zero time for
// none. One that isn't a date is refused, written to w on the refusal page
// headed heading with Back to the draft without it, and ok is false.
func (s *Server) reportBaseline(w http.ResponseWriter, r *http.Request, heading string, id int64) (baseline time.Time, ok bool) {
	var unparsed formProblems
	baseline = unparsed.date("baseline", r.URL.Query().Get("baseline"), "")
	if err := unparsed.err(); err != nil {
		s.refuse(w, r, heading, "/reports/"+strconv.FormatInt(id, 10), err)
		return time.Time{}, false
	}
	return baseline, true
}

// narrativeDraftPath is the draft of the Report Definition id, against the
// baseline the reader picked (the baseline query parameter), if any.
func narrativeDraftPath(r *http.Request, id int64) string {
	path := "/reports/" + strconv.FormatInt(id, 10)
	if baseline := r.URL.Query().Get("baseline"); baseline != "" {
		path += "?baseline=" + url.QueryEscape(baseline)
	}
	return path
}

// narrativeStatus is what the save status says of the compose form posting
// in as the Report Definition defID's narrative: Unsaved changes when saving
// it would change the saved one, and Saved when not.
func (s *Server) narrativeStatus(r *http.Request, defID int64, in domain.CurateNarrativeInput) (string, error) {
	unsaved, err := s.svc.NarrativeUnsaved(r.Context(), defID, in)
	if unsaved {
		return narrativeUnsavedStatus, err
	}
	return narrativeSavedStatus, err
}

// renderNarrativeSaved answers Save narrative over htmx with the draft's
// Narrative section, against the baseline the reader picked, its compose form
// showing the narrative as saved, and the save status saying Saved,
// Publish…'s summary and the History, with the save's entry.
func (s *Server) renderNarrativeSaved(w http.ResponseWriter, r *http.Request, id int64) {
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if err != nil {
		s.serverError(w, r, "could not load report", err)
		return
	}
	baseline, ok := s.reportBaseline(w, r, "Can't show Report", id)
	if !ok {
		return
	}
	report, err := s.svc.DraftReport(r.Context(), def, baseline)
	if err != nil {
		s.serverError(w, r, "could not build the draft", err)
		return
	}
	pubs, err := s.svc.ListPublications(r.Context(), def.ID)
	if err != nil {
		s.serverError(w, r, "could not load publications", err)
		return
	}
	log, err := s.svc.ReportLog(r.Context(), def.ID)
	if err != nil {
		s.serverError(w, r, "could not load the Report log", err)
		return
	}
	chip := newBaselineChip(report, r.URL.Query().Get("baseline") != "", len(pubs) > 0, s.svc.Now(), s.svc.Timezone())
	render(w, r, http.StatusOK, narrativeSaved(savedNarrativeForm(report, chip), domain.ReportHistory(pubs, log), s.svc.Timezone()))
}

// renderNarrativeFormAgain answers Add another, saving nothing: the compose
// form straight from the posted form, in, with one more note box in section
// addNote, and the save status saying whether the form has unsaved changes
// or, when the narrative couldn't be saved as posted, why. It never
// redirects: the draft rebuilt from what was saved would drop what was typed
// and the blank boxes, the new one among them. htmx swaps in the Narrative
// section alone, with the save status out of band; a plain post gets the whole
// draft page, Publish… showing what is saved, as 422 when the narrative
// couldn't be saved. The page's forms come back to the draft.
func (s *Server) renderNarrativeFormAgain(w http.ResponseWriter, r *http.Request, current domain.Account, id int64, in domain.CurateNarrativeInput, addNote string) {
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "could not load report", err)
		return
	}
	baseline, ok := s.reportBaseline(w, r, "Can't show Report", id)
	if !ok {
		return
	}
	// The form offers the Report's Highlights whatever the narrative, so
	// the draft with the posted narrative serves it, or, when refused, the
	// draft as saved, which a plain post's page reads anyway.
	status, code := "", http.StatusOK
	posted, err := s.svc.PreviewNarrative(r.Context(), current.ID, def, baseline, in)
	refused := errors.Is(err, domain.ErrValidation)
	switch {
	case refused:
		status, code = "Not saved: "+refusalSentence(err), http.StatusUnprocessableEntity
	case err != nil:
		s.refuse(w, r, "Can't save narrative", narrativeDraftPath(r, def.ID), err)
		return
	default:
		if status, err = s.narrativeStatus(r, def.ID, in); err != nil {
			s.serverError(w, r, "could not compare the narrative", err)
			return
		}
	}
	if isHTMX(r) {
		if refused {
			if posted, err = s.svc.DraftReport(r.Context(), def, baseline); err != nil {
				s.serverError(w, r, "could not build the draft", err)
				return
			}
		}
		pubs, err := s.svc.ListPublications(r.Context(), def.ID)
		if err != nil {
			s.serverError(w, r, "could not load publications", err)
			return
		}
		chip := newBaselineChip(posted, r.URL.Query().Get("baseline") != "", len(pubs) > 0, s.svc.Now(), s.svc.Timezone())
		answerSwap(w, r, narrativeFormAgain(postedNarrativeForm(posted, chip, r.Form, addNote), status))
		return
	}
	// The page shows the narrative as saved, and the form as posted.
	v, err := s.reportDraftView(r, current, def, baseline, narrativeDraftPath(r, def.ID))
	if err != nil {
		s.serverError(w, r, "could not load the draft", err)
		return
	}
	if refused {
		posted = v.Report
	}
	v.Form, v.Status = postedNarrativeForm(posted, v.Chip, r.Form, addNote), status
	render(w, r, code, reportDraftPage(&current, v))
}

// narrativeForm is what the draft's compose form shows: each Highlight in the
// Report's scope as a choice, and each narrative section's note boxes.
type narrativeForm struct {
	Report  domain.Report
	Chip    baselineChip
	Choices []curationChoice
	// Notes holds each section's note boxes, keyed by section, in order; a
	// box may be blank.
	Notes map[string][]string
}

// curationChoice is one Highlight on the compose form: whether its box is
// ticked, and the section its radios select (none when empty).
type curationChoice struct {
	Highlight domain.NarrativeHighlight
	Included  bool
	Section   string
}

// included counts the form's ticked Highlights.
func (f narrativeForm) included() int {
	n := 0
	for _, c := range f.Choices {
		if c.Included {
			n++
		}
	}
	return n
}

// in is the form's Highlights whose section control selects section, the
// section the author picked or else the Highlight's own kind, in order.
func (f narrativeForm) in(section string) []curationChoice {
	var out []curationChoice
	for _, c := range f.Choices {
		if cmp.Or(c.Section, c.Highlight.Highlight.Kind) == section {
			out = append(out, c)
		}
	}
	return out
}

// savedNarrativeForm is the compose form for the narrative as saved: the
// Highlights picked into it ticked, each selecting its section (or its own
// kind when left out), and each section's notes with one empty box after them.
func savedNarrativeForm(r domain.Report, chip baselineChip) narrativeForm {
	f := narrativeForm{Report: r, Chip: chip, Notes: map[string][]string{}}
	for _, nh := range r.Highlights {
		f.Choices = append(f.Choices, curationChoice{Highlight: nh, Included: nh.Section != "", Section: curatedSection(nh)})
	}
	for _, section := range narrativeSections {
		f.Notes[section] = append(narrativeNotes(r, section), "")
	}
	return f
}

// postedNarrativeForm is the compose form exactly as posted (form), whatever
// was saved: each of the Report's Highlights ticked and selecting the section
// it was posted with, and each section's boxes as posted, blank ones too —
// with one more empty box in section addNote. A section posted with no box
// gets one.
func postedNarrativeForm(r domain.Report, chip baselineChip, form url.Values, addNote string) narrativeForm {
	f := narrativeForm{Report: r, Chip: chip, Notes: map[string][]string{}}
	for _, nh := range r.Highlights {
		id := strconv.FormatInt(nh.Highlight.ID, 10)
		f.Choices = append(f.Choices, curationChoice{Highlight: nh, Included: form.Has("include-" + id), Section: form.Get("pick-" + id)})
	}
	for _, section := range narrativeSections {
		notes := slices.Clone(form["text-"+section])
		if section == addNote || len(notes) == 0 {
			notes = append(notes, "")
		}
		f.Notes[section] = notes
	}
	return f
}

// handlePublishReport publishes a Report Definition, freezing its Report
// against the baseline the reader picked (the baseline query parameter, a
// date) or by default the previous publication, and redirects to the
// publication.
func (s *Server) handlePublishReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	baseline, ok := s.reportBaseline(w, r, "Can't publish Report", id)
	if !ok {
		return
	}
	pub, err := s.svc.PublishReport(r.Context(), current.ID, id, baseline)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.refuse(w, r, "Can't publish Report", narrativeDraftPath(r, id), err)
		return
	}
	http.Redirect(w, r, publicationPath(pub), http.StatusSeeOther)
}

// handleViewPublication shows a published Report exactly as it was frozen,
// with its discussion: comments and Action Items raised since.
func (s *Server) handleViewPublication(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	d, err := s.publicationDiscussion(r, current, pub)
	if err != nil {
		s.serverError(w, r, "could not load the discussion", err)
		return
	}
	r = takeActionItemRefusal(w, r)
	render(w, r, http.StatusOK, publicationPage(&current, pub, d, s.pdf != nil))
}

// handlePrintPublication shows a published Report on a print-friendly page, to
// print to PDF from the browser.
func (s *Server) handlePrintPublication(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	render(w, r.WithContext(withoutHover(r.Context())), http.StatusOK, publicationPrintPage(pub, s.svc.Timezone()))
}

// handleExportMarkdown downloads a published Report as Markdown, with the same
// content as its publication page.
func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%d-publication-%d.md"`, pub.DefinitionID, pub.ID))
	_, _ = io.WriteString(w, export.Markdown(pub))
}

// How many PDFs render at once, and how long a download waits for its turn
// and its render together before it gives up. The HTTP server's write timeout
// has to outlast PDFRenderTimeout, so it is exported.
const (
	pdfRendersAtOnce = 2
	PDFRenderTimeout = 30 * time.Second
)

// pdfDownloads renders publications to PDF files: through renderer, at most
// cap(slots) at once, each within timeout.
type pdfDownloads struct {
	renderer PDFRenderer
	slots    chan struct{}
	timeout  time.Duration
}

// handleDownloadPDF downloads a published Report as a PDF file: its print page,
// rendered. Without a renderer it sends the reader to the print page, to print
// to PDF from the browser. A download that can't start rendering in time is
// told the app is busy; one whose render fails gets the server-error page,
// never a broken PDF.
func (s *Server) handleDownloadPDF(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	printPath := publicationPath(pub) + "/print"
	if s.pdf == nil {
		http.Redirect(w, r, printPath, http.StatusSeeOther)
		return
	}
	var page bytes.Buffer
	if err := publicationPrintPage(pub, s.svc.Timezone()).Render(withPath(withoutHover(r.Context()), printPath), &page); err != nil {
		s.serverError(w, r, "could not draw the print page", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.pdf.timeout)
	defer cancel()
	select {
	case s.pdf.slots <- struct{}{}:
		defer func() { <-s.pdf.slots }()
	case <-ctx.Done():
		if r.Context().Err() != nil {
			return // the reader went away
		}
		s.requestLog(r).WarnContext(r.Context(), "PDF renders all busy", "method", r.Method, "path", r.URL.Path)
		http.Error(w, "Too many PDFs are being made right now. Try again in a minute.", http.StatusServiceUnavailable)
		return
	}
	b, err := s.pdf.renderer.Render(ctx, page.Bytes(), selfURL(r))
	if err != nil {
		s.serverError(w, r, "could not make the PDF", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%d-publication-%d.pdf"`, pub.DefinitionID, pub.ID))
	_, _ = w.Write(b)
}

// selfURL is the app's own address as r reached it: the address it listens on
// that r's connection arrived at, which the host the app runs on can always
// reach, unlike a public base URL behind a proxy.
func selfURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return scheme + "://" + addr.String()
	}
	return scheme + "://" + r.Host
}

// publication loads the published Report the path names, writing the Not found
// page when there is none. A publication is found only under its own Report
// Definition.
func (s *Server) publication(w http.ResponseWriter, r *http.Request) (domain.Publication, bool) {
	defID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return domain.Publication{}, false
	}
	pubID, err := strconv.ParseInt(r.PathValue("pub"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return domain.Publication{}, false
	}
	pub, err := s.svc.GetPublication(r.Context(), pubID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return domain.Publication{}, false
		}
		s.serverError(w, r, "could not load the publication", err)
		return domain.Publication{}, false
	}
	if pub.DefinitionID != defID {
		s.notFound(w, r)
		return domain.Publication{}, false
	}
	return pub, true
}

// publicationPath is where a published Report is viewed.
func publicationPath(p domain.Publication) string {
	return fmt.Sprintf("/reports/%d/publications/%d", p.DefinitionID, p.ID)
}

// reportsList summarises the saved definitions and gathers the open Action
// Items current has reason to chase.
func (s *Server) reportsList(r *http.Request, current domain.Account) (reportsListData, error) {
	sums, err := s.svc.ReportSummaries(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	slices.SortFunc(sums, domain.ByPublishingNeed)
	yours, org, err := s.svc.OpenActionItemsToChase(r.Context(), current.ID)
	if err != nil {
		return reportsListData{}, err
	}
	return reportsListData{Reports: sums, Loc: s.svc.Timezone(), YourActionItems: yours, OrgActionItems: org, PDFFile: s.pdf != nil}, nil
}

// distinctOwners returns the Goals' Owners, one each, in first-seen order — the
// candidates an Owner rule offers, without reaching into the accounts area for
// a full account list.
func distinctOwners(goals []domain.Goal) []domain.Account {
	seen := make(map[int64]bool, len(goals))
	out := make([]domain.Account, 0, len(goals))
	for _, g := range goals {
		if g.Owner.ID == 0 || seen[g.Owner.ID] {
			continue
		}
		seen[g.Owner.ID] = true
		out = append(out, g.Owner)
	}
	return out
}

// handleAddReportDelegate makes the Account named by email a Report Delegate on
// the Report Definition in the path. Only its creator or an Admin may, so
// anyone else's attempt is 403; an email the domain refuses comes back on the
// draft page, 422, with the reason beside the Delegates form (CONTEXT.md:
// Report Delegate).
func (s *Server) handleAddReportDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.reportFromPath(w, r)
	if !ok {
		return
	}
	email := r.FormValue("email")
	err := s.svc.AddReportDelegateByEmail(r.Context(), current.ID, def.ID, email)
	s.writeReportDelegateResult(w, r, current, def, email, err)
}

// handleRemoveReportDelegate revokes the Report Delegate the form names by
// account id on the Report Definition in the path. Only its creator or an
// Admin may.
func (s *Server) handleRemoveReportDelegate(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.reportFromPath(w, r)
	if !ok {
		return
	}
	accountID, err := strconv.ParseInt(r.FormValue("account"), 10, 64)
	if err != nil {
		http.Error(w, "unknown Report Delegate", http.StatusBadRequest)
		return
	}
	err = s.svc.RemoveReportDelegate(r.Context(), current.ID, def.ID, accountID)
	s.writeReportDelegateResult(w, r, current, def, "", err)
}

// writeReportDelegateResult redirects back to def's draft on success; a refusal
// for who is asking is 403, and a refused email (posted) the draft page again,
// 422, saying why.
func (s *Server) writeReportDelegateResult(w http.ResponseWriter, r *http.Request, current domain.Account, def domain.ReportDefinition, email string, err error) {
	draft := "/reports/" + strconv.FormatInt(def.ID, 10)
	switch {
	case err == nil:
		http.Redirect(w, r, draft, http.StatusSeeOther)
	case errors.Is(err, domain.ErrValidation):
		v, ok := s.loadReportDraftView(w, r, current, def, draft)
		if !ok {
			return
		}
		v.Management.Email, v.Management.Refused = email, refusalSentence(err)
		render(w, r, http.StatusUnprocessableEntity, reportDraftPage(&current, v))
	default:
		s.refuse(w, r, "Can't change Report Delegates", draft, err)
	}
}
