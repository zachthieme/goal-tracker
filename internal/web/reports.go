package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
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

// handleViewReport shows a saved Report Definition and its draft Report,
// recomputed on each view against the baseline the reader picks (the baseline
// query parameter, a date) or 30 days ago by default.
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
	render(w, r, http.StatusOK, reportDraftPage(&current, report))
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
	return reportsListData{Defs: defs, Goals: goals, Owners: distinctOwners(goals), Dims: dims}, nil
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
