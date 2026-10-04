package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
)

// reportsListData is what the reports page needs: the saved definitions and the
// building blocks a new definition picks from — Goals as roots, their Owners as
// the Owner filter, the Dimensions still offered (none Retired) as the
// Dimension-value filter, and the Fields still offered to show beside each Goal.
type reportsListData struct {
	Defs   []domain.ReportDefinition
	Goals  []domain.Goal
	Owners []domain.Account
	Dims   []domain.Dimension
	Fields []domain.Field
}

// handleReports shows every saved Report Definition and the form to build a new
// one (CONTEXT.md: Report Definition). Anyone signed in may save one.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request, current domain.Account) {
	data, err := s.reportsList(r)
	if err != nil {
		http.Error(w, "could not load reports", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, reportsPage(&current, data))
}

// handleSaveReport saves a Report Definition from the form and redirects to its
// live draft. Until the builder (#149), the form posts root Goals, an Owner
// filter and Dimension-value filters: with roots, it saves the roots that pass
// the filters now as a picked definition; with none, the filters as rules.
func (s *Server) handleSaveReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	rules, err := s.formReportRules(r)
	if err != nil {
		http.Error(w, "could not read the filters", http.StatusInternalServerError)
		return
	}
	in := domain.SaveReportDefinitionInput{
		Name:         r.FormValue("name"),
		Introduction: r.FormValue("introduction"),
		Mode:         domain.ReportModeRules,
		Rules:        rules,
		FieldIDs:     formInt64s(r, "field"),
	}
	if roots := formInt64s(r, "root"); len(roots) > 0 {
		if in.Picked, err = s.rootsPassing(r, roots, rules); err != nil {
			http.Error(w, "could not apply the filters", http.StatusInternalServerError)
			return
		}
		in.Mode, in.Rules = domain.ReportModePicked, nil
	}
	def, err := s.svc.SaveReportDefinition(r.Context(), current.ID, in)
	if err != nil {
		writeReportError(w, err)
		return
	}
	http.Redirect(w, r, "/reports/"+strconv.FormatInt(def.ID, 10), http.StatusSeeOther)
}

// formReportRules reads the form's Owner filter as an "Owner is" rule and its
// Dimension-value filters as one "is any of" rule per Dimension, as the
// migration converts a saved definition's filters.
func (s *Server) formReportRules(r *http.Request) ([]domain.ReportRule, error) {
	var rules []domain.ReportRule
	if owner := r.FormValue("owner"); owner != "" {
		rules = append(rules, domain.ReportRule{Attribute: domain.RuleOwner, Op: domain.RuleIs, Values: []string{owner}})
	}
	filters := formInt64s(r, "filter")
	if len(filters) == 0 {
		return rules, nil
	}
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		return nil, err
	}
	dimensionOf := map[int64]int64{}
	for _, d := range dims {
		for _, v := range d.Values {
			dimensionOf[v.ID] = d.ID
		}
	}
	ruleOf := map[int64]int{}
	for _, id := range filters {
		// An unknown value gets a rule naming no Dimension, which saving refuses.
		dimID := dimensionOf[id]
		i, ok := ruleOf[dimID]
		if !ok {
			i = len(rules)
			ruleOf[dimID] = i
			rules = append(rules, domain.ReportRule{Attribute: domain.RuleDimension, DimensionID: dimID, Op: domain.RuleIsAnyOf})
		}
		rules[i].Values = append(rules[i].Values, strconv.FormatInt(id, 10))
	}
	return rules, nil
}

// rootsPassing returns the roots that meet every rule now, in the order
// given.
func (s *Server) rootsPassing(r *http.Request, roots []int64, rules []domain.ReportRule) ([]int64, error) {
	if len(rules) == 0 {
		return roots, nil
	}
	everything := func(time.Time) bool { return true }
	matched, err := s.svc.SelectGoals(r.Context(), domain.ReportDefinition{Mode: domain.ReportModeRules, Rules: rules}, everything)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(roots))
	for _, id := range roots {
		if slices.ContainsFunc(matched, func(sg domain.SelectedGoal) bool { return sg.Goal.ID == id }) {
			out = append(out, id)
		}
	}
	return out, nil
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
	render(w, r, http.StatusOK, reportDraftPage(&current, report, s.svc.Now(), pubs, d, s.svc.Timezone()))
}

// handleCurateNarrative sets the narrative of a Report Definition's next
// publication from the draft page's form: the in-scope Highlights the author
// ticked (include-{highlight}), each with its section (pick-{highlight}, which
// is ignored for a Highlight left unticked), and the section text for each
// section (text-{section}). It redirects back to the draft, against the
// baseline the reader picked (the baseline form field), if any.
func (s *Server) handleCurateNarrative(w http.ResponseWriter, r *http.Request, _ domain.Account) {
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
	if err := s.svc.CurateNarrative(r.Context(), id, in); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		writeReportError(w, err)
		return
	}
	target := "/reports/" + strconv.FormatInt(id, 10)
	if baseline := r.FormValue("baseline"); baseline != "" {
		target += "?baseline=" + url.QueryEscape(baseline)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handlePublishReport publishes a Report Definition, freezing its Report
// against the baseline the reader picked (the baseline form field, a date) or
// by default the previous publication, and redirects to the publication.
func (s *Server) handlePublishReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	baseline, err := parseDate(r.FormValue("baseline"))
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

// reportsList gathers the saved definitions and the choices a new one picks from.
func (s *Server) reportsList(r *http.Request) (reportsListData, error) {
	defs, err := s.svc.ListReportDefinitions(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		return reportsListData{}, err
	}
	return reportsListData{
		Defs:   defs,
		Goals:  topLevelFirst(goals),
		Owners: distinctOwners(goals),
		Dims:   domain.OfferedDimensions(dims),
		Fields: domain.OfferedFields(fields),
	}, nil
}

// topLevelFirst orders Goals for the root picker: the org's Top-level Goals
// first, each group keeping its order.
func topLevelFirst(goals []domain.Goal) []domain.Goal {
	out := slices.Clone(goals)
	slices.SortStableFunc(out, func(a, b domain.Goal) int {
		switch {
		case a.TopLevel == b.TopLevel:
			return 0
		case a.TopLevel:
			return -1
		}
		return 1
	})
	return out
}

// distinctOwners returns the Goals' Owners, one each, in first-seen order — the
// candidates the Owner filter offers, without reaching into the accounts area for
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
