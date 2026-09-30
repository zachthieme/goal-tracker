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

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/export"
)

// reportsListData is what the reports page needs: the saved definitions and the
// building blocks a new definition picks from — Goals as roots, their Owners as
// the Owner filter, and Dimensions as the Dimension-value filter.
type reportsListData struct {
	Defs   []domain.ReportDefinition
	Goals  []domain.Goal
	Owners []domain.Account
	Dims   []domain.Dimension
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
// live draft.
func (s *Server) handleSaveReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	depth, _ := strconv.Atoi(r.FormValue("depth"))
	owner, _ := strconv.ParseInt(r.FormValue("owner"), 10, 64)
	def, err := s.svc.SaveReportDefinition(r.Context(), current.ID, domain.SaveReportDefinitionInput{
		Name:              r.FormValue("name"),
		Introduction:      r.FormValue("introduction"),
		RootIDs:           formInt64s(r, "root"),
		Depth:             depth,
		OwnerFilterID:     owner,
		DimensionValueIDs: formInt64s(r, "filter"),
	})
	if err != nil {
		writeReportError(w, err)
		return
	}
	http.Redirect(w, r, "/reports/"+strconv.FormatInt(def.ID, 10), http.StatusSeeOther)
}

// handleViewReport shows a saved Report Definition, its draft Report, and its
// publications. The draft is recomputed on each view against the baseline the
// reader picks (the baseline query parameter, a date) or by default the
// previous publication, or 30 days ago when there is none.
func (s *Server) handleViewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	def, err := s.svc.GetReportDefinition(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
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
	render(w, r, http.StatusOK, reportDraftPage(&current, report, s.svc.Now(), pubs, d))
}

// handleCurateNarrative sets the narrative of a Report Definition's next
// publication from the draft page's form: each in-scope Highlight's section
// (pick-{highlight}, empty to leave it out) and the author's text for each
// section (text-{section}). It redirects back to the draft, against the
// baseline the reader picked (the baseline form field), if any.
func (s *Server) handleCurateNarrative(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	in := domain.CurateNarrativeInput{Text: map[string]string{}}
	for key, values := range r.Form {
		raw, ok := strings.CutPrefix(key, "pick-")
		if !ok || len(values) == 0 || values[0] == "" {
			continue
		}
		hlID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "unknown Highlight", http.StatusBadRequest)
			return
		}
		in.Picks = append(in.Picks, domain.NarrativePick{HighlightID: hlID, Section: values[0]})
	}
	for _, section := range narrativeSections {
		in.Text[section] = r.FormValue("text-" + section)
	}
	if err := s.svc.CurateNarrative(r.Context(), id, in); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
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
		http.NotFound(w, r)
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
			http.NotFound(w, r)
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
	render(w, r, http.StatusOK, publicationPrintPage(pub))
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

// publication loads the published Report the path names, writing a 404 when
// there is none. A publication is found only under its own Report Definition.
func (s *Server) publication(w http.ResponseWriter, r *http.Request) (domain.Publication, bool) {
	defID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return domain.Publication{}, false
	}
	pubID, err := strconv.ParseInt(r.PathValue("pub"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return domain.Publication{}, false
	}
	pub, err := s.svc.GetPublication(r.Context(), pubID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			http.NotFound(w, r)
			return domain.Publication{}, false
		}
		http.Error(w, "could not load the publication", http.StatusInternalServerError)
		return domain.Publication{}, false
	}
	if pub.DefinitionID != defID {
		http.NotFound(w, r)
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
	return reportsListData{Defs: defs, Goals: topLevelFirst(goals), Owners: distinctOwners(goals), Dims: dims}, nil
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
