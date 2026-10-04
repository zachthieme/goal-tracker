package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
)

// reportsListData is what the reports page needs: the saved definitions.
type reportsListData struct {
	Defs []domain.ReportDefinition
}

// handleReports shows every saved Report Definition and links to the builder
// for a new one (CONTEXT.md: Report Definition). Anyone signed in may save one.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request, current domain.Account) {
	data, err := s.reportsList(r)
	if err != nil {
		http.Error(w, "could not load reports", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, reportsPage(&current, data))
}

// handleViewReport shows a saved Report Definition, its draft Report, and its
// publications. The draft is recomputed on each view against the baseline the
// reader picks (the baseline query parameter, a date) or by default the
// previous publication, or 30 days ago when there is none.
func (s *Server) handleViewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		http.Error(w, "could not load report", http.StatusInternalServerError)
		return
	}
	baseline, err := parseDate(r.URL.Query().Get("baseline"))
	if err != nil {
		http.Error(w, "the baseline must be a date", http.StatusBadRequest)
		return
	}
	report, err := s.svc.DraftReport(r.Context(), def, baseline)
	if err != nil {
		http.Error(w, "could not build the draft", http.StatusInternalServerError)
		return
	}
	pubs, err := s.svc.ListPublications(r.Context(), def.ID)
	if err != nil {
		http.Error(w, "could not load publications", http.StatusInternalServerError)
		return
	}
	d, err := s.draftDiscussion(r, current, def.ID)
	if err != nil {
		http.Error(w, "could not load Action Items", http.StatusInternalServerError)
		return
	}
	// Closing an Action Item comes back to this draft, baseline and all.
	d.Return = r.URL.RequestURI()
	panel, err := s.draftGoalsPanel(r, report)
	if err != nil {
		http.Error(w, "could not summarise the report", http.StatusInternalServerError)
		return
	}
	if domain.CanEditReportDefinition(current, def) {
		panel.Edit = "/reports/" + strconv.FormatInt(def.ID, 10) + "/edit"
	}
	chip := newBaselineChip(report, r.URL.Query().Get("baseline") != "", len(pubs) > 0, s.svc.Now(), s.svc.Timezone())
	render(w, r, http.StatusOK, reportDraftPage(&current, report, s.svc.Now(), pubs, d, s.svc.Timezone(), chip, panel))
}

// handleEditReportForm shows the builder filled with the saved Report
// Definition, to its creator or an Admin (CONTEXT.md: Report Definition).
func (s *Server) handleEditReportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	def, ok := s.editableReport(w, r, current)
	if !ok {
		return
	}
	v := builderFromDefinition(def)
	if err := s.loadReportBuilder(r, &v); err != nil {
		http.Error(w, "could not load the builder", http.StatusInternalServerError)
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
	s.submitReportBuilder(w, r, current, def.ID, func(tx *domain.Service, in domain.SaveReportDefinitionInput) (domain.ReportDefinition, error) {
		return tx.UpdateReportDefinition(r.Context(), current.ID, def.ID, in)
	})
}

// editableReport loads the Report Definition the path names for current to
// edit, writing Not found when there is none and 403 when current is neither
// its creator nor an Admin.
func (s *Server) editableReport(w http.ResponseWriter, r *http.Request, current domain.Account) (domain.ReportDefinition, bool) {
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
		http.Error(w, "could not load report", http.StatusInternalServerError)
		return domain.ReportDefinition{}, false
	}
	if !domain.CanEditReportDefinition(current, def) {
		http.Error(w, "only its creator or an Admin may edit "+def.Name, http.StatusForbidden)
		return domain.ReportDefinition{}, false
	}
	return def, true
}

// builderFromDefinition is the builder as the saved def fills it: its name,
// introduction, mode, rule rows, listed Goals and Fields.
func builderFromDefinition(def domain.ReportDefinition) reportBuilderView {
	v := reportBuilderView{
		ID:           def.ID,
		Name:         def.Name,
		Introduction: def.Introduction,
		Mode:         def.Mode,
		PickedIDs:    def.Picked,
		IncludeIDs:   def.Include,
		ExcludeIDs:   def.Exclude,
		FieldIDs:     def.FieldIDs,
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

// goalsPanel is the draft's Goals panel: the Goals the draft selects, and its
// definition's scope in plain words. Edit is where the reader edits the
// definition, which the panel and the draft's header link to, and "" for a
// reader who may not.
type goalsPanel struct {
	Goals []reportGoal
	Scope string
	Edit  string
}

// draftGoalsPanel builds the Goals panel of the draft report, marking the
// Goals it selects only because of Also include, at the draft's baseline.
func (s *Server) draftGoalsPanel(r *http.Request, report domain.Report) (goalsPanel, error) {
	sum, err := s.svc.ReportSummary(r.Context(), report.Definition)
	if err != nil {
		return goalsPanel{}, err
	}
	added, err := s.svc.AlsoIncludedOnly(r.Context(), report)
	if err != nil {
		return goalsPanel{}, err
	}
	return goalsPanel{Goals: reportGoals(report, added), Scope: sum.Scope}, nil
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
		ThirtyDaysAgo: time.Date(y, m, d-30, 0, 0, 0, 0, time.UTC).Format(dateLayout),
	}
	switch {
	case chosen:
		c.Label = fmtMonthDay(r.Baseline)
		c.Query = "?baseline=" + url.QueryEscape(fmtDate(r.Baseline))
	case published:
		c.Label = "last publication · " + fmtMonthDay(r.Baseline)
	default:
		c.Label = "30 days ago · " + fmtMonthDay(r.Baseline)
	}
	return c
}

// publishSummary is what Publish… says the publication will freeze: the Goals
// in the draft, those needing attention, the Highlights picked into its saved
// narrative, and the baseline as the chip labels it.
func publishSummary(r domain.Report, chip baselineChip) string {
	highlights := 0
	for _, s := range r.Narrative {
		highlights += len(s.Highlights)
	}
	return fmt.Sprintf("%s · %d need attention · %s · changes since %s. Readers can comment on the frozen copy.",
		plural(len(r.Exceptions)+len(r.Lines), "Goal"), len(r.Exceptions), plural(highlights, "Highlight"), chip.Label)
}

// fmtMonthDay renders a calendar date as "Sep 22". A Report's baseline is
// already the org's calendar date at midnight UTC, so it is not converted.
func fmtMonthDay(t time.Time) string {
	return t.Format("Jan 2")
}

// handleCurateNarrative sets the narrative of a Report Definition's next
// publication from the draft page's form: the in-scope Highlights the author
// ticked (include-{highlight}), each with its section (pick-{highlight}, which
// is ignored for a Highlight left unticked), and the section text for each
// section (text-{section}). It redirects back to the draft, against the
// baseline the reader picked (the baseline query parameter), if any. An htmx
// post is the compose form autosaving: it gets the preview and the save status
// instead, and a refusal changes only the save status, so what the author typed
// stays.
func (s *Server) handleCurateNarrative(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	in := domain.CurateNarrativeInput{Text: map[string]string{}}
	for key := range r.Form {
		raw, ok := strings.CutPrefix(key, "include-")
		if !ok {
			continue
		}
		hlID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "unknown Highlight", http.StatusBadRequest)
			return
		}
		// A missing section reaches the domain empty, which refuses it.
		in.Picks = append(in.Picks, domain.NarrativePick{HighlightID: hlID, Section: r.Form.Get("pick-" + raw)})
	}
	for _, section := range narrativeSections {
		in.Text[section] = r.FormValue("text-" + section)
	}
	hx := r.Header.Get("HX-Request") == "true"
	if err := s.svc.CurateNarrative(r.Context(), id, in); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			s.notFound(w, r)
		case hx && errors.Is(err, domain.ErrValidation):
			w.Header().Set("HX-Reswap", "none")
			render(w, r, http.StatusOK, saveStatus("Not saved: "+plainReason(err), true))
		default:
			writeReportError(w, err)
		}
		return
	}
	target := "/reports/" + strconv.FormatInt(id, 10)
	if baseline := r.URL.Query().Get("baseline"); baseline != "" {
		target += "?baseline=" + url.QueryEscape(baseline)
	}
	if hx {
		s.renderNarrativeSaved(w, r, current, id, target)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// renderNarrativeSaved answers an autosave with the draft's preview, against
// the baseline the reader picked, and the save status saying Saved. The
// preview's forms come back to draft, the draft's path.
func (s *Server) renderNarrativeSaved(w http.ResponseWriter, r *http.Request, current domain.Account, id int64, draft string) {
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load report", http.StatusInternalServerError)
		return
	}
	baseline, err := parseDate(r.URL.Query().Get("baseline"))
	if err != nil {
		http.Error(w, "the baseline must be a date", http.StatusBadRequest)
		return
	}
	report, err := s.svc.DraftReport(r.Context(), def, baseline)
	if err != nil {
		http.Error(w, "could not build the draft", http.StatusInternalServerError)
		return
	}
	d, err := s.draftDiscussion(r, current, def.ID)
	if err != nil {
		http.Error(w, "could not load Action Items", http.StatusInternalServerError)
		return
	}
	d.Return = draft
	render(w, r, http.StatusOK, narrativeSaved(report, s.svc.Now(), d))
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
	baseline, err := parseDate(r.URL.Query().Get("baseline"))
	if err != nil {
		http.Error(w, "the baseline must be a date", http.StatusBadRequest)
		return
	}
	pub, err := s.svc.PublishReport(r.Context(), current.ID, id, baseline)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		writeReportError(w, err)
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
		http.Error(w, "could not load the discussion", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, publicationPage(&current, pub, d))
}

// handlePrintPublication shows a published Report on a print-friendly page, to
// print to PDF from the browser.
func (s *Server) handlePrintPublication(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	render(w, r.WithContext(withoutHover(r.Context())), http.StatusOK, publicationPrintPage(pub))
}

// handleExportMarkdown downloads a published Report as Markdown, with the same
// content as its snapshot.
func (s *Server) handleExportMarkdown(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%d-publication-%d.md"`, pub.DefinitionID, pub.ID))
	_, _ = io.WriteString(w, export.Markdown(pub))
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
		http.Error(w, "could not load the publication", http.StatusInternalServerError)
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

// reportsList gathers the saved definitions.
func (s *Server) reportsList(r *http.Request) (reportsListData, error) {
	defs, err := s.svc.ListReportDefinitions(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	return reportsListData{Defs: defs}, nil
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

// formInt64s reads a repeated form field as a slice of ids, dropping blanks and
// unparseable values.
func formInt64s(r *http.Request, key string) []int64 {
	raw := r.Form[key]
	out := make([]int64, 0, len(raw))
	for _, v := range raw {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// writeReportError maps a domain Report error to an HTTP status.
func writeReportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, "report action failed", http.StatusInternalServerError)
	}
}
