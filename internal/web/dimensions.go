package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleDimensions shows every Dimension and, for an Admin, the controls to
// define one and to add, rename, and retire its values (CONTEXT.md: Admin
// defines Dimensions).
func (s *Server) handleDimensions(w http.ResponseWriter, r *http.Request, current domain.Account) {
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		http.Error(w, "could not list dimensions", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, dimensionsPage(&current, dims, nil))
}

// handleCreateDimension defines a Dimension from a name, a comma-separated
// value list, whether a Goal takes one of its values or several (one when the
// form doesn't say), and whether its list is Fixed or Extendable (Fixed when
// the form doesn't say). Only an Admin may.
func (s *Server) handleCreateDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	err := s.svc.WithinTx(r.Context(), func(tx *domain.Service) error {
		dim, err := tx.CreateDimension(r.Context(), current.ID, r.FormValue("name"), splitValues(r.FormValue("values")))
		if err != nil {
			return err
		}
		if selection := r.FormValue("selection"); selection != "" && selection != dim.Selection {
			if err := tx.SetDimensionSelection(r.Context(), current.ID, dim.ID, selection); err != nil {
				return err
			}
		}
		if list := r.FormValue("list"); list != "" && list != dim.List {
			return tx.SetDimensionList(r.Context(), current.ID, dim.ID, list)
		}
		return nil
	})
	if err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionSelection switches the Dimension in the path between a Goal
// taking one of its values and several. Switching to one is refused while Goals
// carry several, and the page comes back naming them. Only an Admin may.
func (s *Server) handleSetDimensionSelection(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.SetDimensionSelection(r.Context(), current.ID, id, r.FormValue("selection"))
	var refusal *domain.SeveralValuesError
	if errors.As(err, &refusal) {
		dims, listErr := s.svc.ListDimensions(r.Context())
		if listErr != nil {
			http.Error(w, "could not list dimensions", http.StatusInternalServerError)
			return
		}
		render(w, r, http.StatusUnprocessableEntity, dimensionsPage(&current, dims, refusal))
		return
	}
	if err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionList makes the Dimension in the path Fixed or Extendable.
// Only an Admin may.
func (s *Server) handleSetDimensionList(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SetDimensionList(r.Context(), current.ID, id, r.FormValue("list")); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionRequired marks the Dimension in the path required when the
// form's required is 1, and unmarks it otherwise (CONTEXT.md: Incomplete). Only
// an Admin may.
func (s *Server) handleSetDimensionRequired(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SetDimensionRequired(r.Context(), current.ID, id, r.FormValue("required") == "1"); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleAddDimensionValue adds a value to the Dimension in the path. Only an
// Admin may.
func (s *Server) handleAddDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AddDimensionValue(r.Context(), current.ID, id, r.FormValue("value")); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRenameDimensionValue renames the value in the path. Only an Admin may.
func (s *Server) handleRenameDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.RenameDimensionValue(r.Context(), current.ID, id, r.FormValue("value")); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRetireDimensionValue retires the value in the path so it is no longer
// offered for new assignments. Only an Admin may.
func (s *Server) handleRetireDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RetireDimensionValue(r.Context(), current.ID, id); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRestoreDimensionValue reverses the retirement of the value in the path,
// so it is offered for new assignments again. Only an Admin may.
func (s *Server) handleRestoreDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RestoreDimensionValue(r.Context(), current.ID, id); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRetireDimension retires the Dimension in the path, withdrawing it from
// setting Goals' values, the Goal list's filter and grouping, and the Report
// Definition form. Only an Admin may.
func (s *Server) handleRetireDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RetireDimension(r.Context(), current.ID, id); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRestoreDimension reverses the retirement of the Dimension in the path,
// returning it everywhere. Only an Admin may.
func (s *Server) handleRestoreDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RestoreDimension(r.Context(), current.ID, id); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleMoveDimensionValue moves the value in the path one place up or down its
// Dimension's list. Only an Admin may.
func (s *Server) handleMoveDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.MoveDimensionValue(r.Context(), current.ID, id, r.FormValue("direction")); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSortDimensionValues puts the values of the Dimension in the path in
// alphabetical order. Only an Admin may.
func (s *Server) handleSortDimensionValues(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SortDimensionValues(r.Context(), current.ID, id); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleMergeDimensionValue merges the value in the path into the value named
// by the form's into, in the same Dimension. Only an Admin may.
func (s *Server) handleMergeDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	into, err := strconv.ParseInt(r.FormValue("into"), 10, 64)
	if err != nil {
		http.Error(w, "choose a value to merge into", http.StatusUnprocessableEntity)
		return
	}
	if err := s.svc.MergeDimensionValue(r.Context(), current.ID, id, into); err != nil {
		writeDimensionError(w, err)
		return
	}
	s.redirectToDimensions(w, r)
}

func (s *Server) redirectToDimensions(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dimensions", http.StatusSeeOther)
}

// splitValues turns a comma-separated value list into a slice; blanks are
// dropped by the domain.
func splitValues(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dimensionIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// writeDimensionError maps a domain Dimension error to an HTTP status.
func writeDimensionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, "dimension action failed", http.StatusInternalServerError)
	}
}

// selectionLabel says whether a Goal takes one of d's values or several.
func selectionLabel(d domain.Dimension) string {
	if d.TakesSeveral() {
		return "several values"
	}
	return "one value"
}

// listLabel says whether d's list is Fixed or Extendable.
func listLabel(d domain.Dimension) string {
	if d.Extendable() {
		return "Extendable"
	}
	return "Fixed"
}
