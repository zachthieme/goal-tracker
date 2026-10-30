package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleDimensions shows every Dimension and, for an Admin, the controls to
// define one and to add, rename, and retire its values (CONTEXT.md: Admin
// defines Dimensions). A form just refused for what was typed comes back
// open, as it was left, this once (onPage).
func (s *Server) handleDimensions(w http.ResponseWriter, r *http.Request, current domain.Account) {
	r = takeFormRefusal(w, r)
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		s.serverError(w, r, "could not list dimensions", err)
		return
	}
	render(w, r, http.StatusOK, dimensionsPage(&current, dims, nil, s.undoToast(w, r, current)))
}

// handleCreateDimension defines a Dimension from a name, a comma-separated
// value list, whether a Goal takes one of its values or several (one when the
// form doesn't say), and whether its list is Fixed or Extendable (Fixed when
// the form doesn't say). Only an Admin may. Refused for what was typed, it
// comes back to the Dimensions page, one form of several there, with Define
// a Dimension open, saying why above it (onPage).
func (s *Server) handleCreateDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.DefineDimension(r.Context(), current.ID, domain.DimensionDefinition{
		Name:      r.FormValue("name"),
		Values:    splitValues(r.FormValue("values")),
		Selection: r.FormValue("selection"),
		List:      r.FormValue("list"),
	})
	if err != nil {
		s.writeDimensionFormError(w, r, err, onPage("/dimensions", "create-dimension", 0, []string{"name", "values", "selection", "list"}).inOneSentence())
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionSelection switches the Dimension in the path between a Goal
// taking one of its values and several. Switching to one is refused while Goals
// carry several, and the page comes back naming them. Only an Admin may.
//
// That refusal is answered here, not through answerRefused, on purpose: its
// list of the Goals still carrying several values is a designed refusal on the
// Dimensions page (DESIGN.md, .dm-refusal), not a reason beside a form.
func (s *Server) handleSetDimensionSelection(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	err := s.svc.SetDimensionSelection(r.Context(), current.ID, id, r.FormValue("selection"))
	var refusal *domain.SeveralValuesError
	if errors.As(err, &refusal) {
		dims, listErr := s.svc.ListDimensions(r.Context())
		if listErr != nil {
			s.serverError(w, r, "could not list dimensions", listErr)
			return
		}
		render(w, r, http.StatusUnprocessableEntity, dimensionsPage(&current, dims, refusal, nil))
		return
	}
	if err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionList makes the Dimension in the path Fixed or Extendable.
// Only an Admin may.
func (s *Server) handleSetDimensionList(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SetDimensionList(r.Context(), current.ID, id, r.FormValue("list")); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSetDimensionRequired marks the Dimension in the path required when the
// form's required is 1, and unmarks it otherwise (CONTEXT.md: Incomplete). Only
// an Admin may.
func (s *Server) handleSetDimensionRequired(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SetDimensionRequired(r.Context(), current.ID, id, r.FormValue("required") == "1"); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleAddDimensionValue adds a value to the Dimension in the path. Only an
// Admin may. Refused for what was typed, it comes back to the Dimension's card
// on the Dimensions page, one form of several there, its Edit open and the
// reason beside Add a value (onPage).
func (s *Server) handleAddDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.AddDimensionValue(r.Context(), current.ID, id, r.FormValue("value")); err != nil {
		s.writeDimensionFormError(w, r, err, s.addDimensionValueForm(id))
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRenameDimensionValue renames the value in the path. Only an Admin may.
// Refused for what was typed, it comes back to the value's row on the
// Dimensions page, one form of several there, its Dimension's Edit open and
// the reason beside the rename (onPage).
func (s *Server) handleRenameDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.RenameDimensionValue(r.Context(), current.ID, id, r.FormValue("value")); err != nil {
		s.writeDimensionFormError(w, r, err, s.renameDimensionValueForm(id))
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRetireDimensionValue retires the value in the path so it is no longer
// offered for new assignments, and has the Dimensions page offer Undo once in a
// toast. Only an Admin may.
func (s *Server) handleRetireDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	token, err := s.svc.RetireDimensionValueWithUndo(r.Context(), current.ID, id)
	if err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.offerUndo(w, r, undoValueRetire, id, token, "/dimensions")
}

// handleRestoreDimensionValue reverses the retirement of the value in the path,
// so it is offered for new assignments again. Only an Admin may.
func (s *Server) handleRestoreDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RestoreDimensionValue(r.Context(), current.ID, id); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRetireDimension retires the Dimension in the path, withdrawing it from
// setting Goals' values, the Goal list's filter and grouping, and the Report
// Definition form. Only an Admin may.
func (s *Server) handleRetireDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RetireDimension(r.Context(), current.ID, id); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleRestoreDimension reverses the retirement of the Dimension in the path,
// returning it everywhere. Only an Admin may.
func (s *Server) handleRestoreDimension(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.RestoreDimension(r.Context(), current.ID, id); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleMoveDimensionValue moves the value in the path one place up or down its
// Dimension's list. Only an Admin may.
func (s *Server) handleMoveDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.MoveDimensionValue(r.Context(), current.ID, id, r.FormValue("direction")); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleSortDimensionValues puts the values of the Dimension in the path in
// alphabetical order. Only an Admin may.
func (s *Server) handleSortDimensionValues(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	if err := s.svc.SortDimensionValues(r.Context(), current.ID, id); err != nil {
		s.writeDimensionError(w, r, err)
		return
	}
	s.redirectToDimensions(w, r)
}

// handleMergeDimensionValue merges the value in the path into the value named
// by the form's into, in the same Dimension. Only an Admin may. Nothing is
// typed: into is chosen from the page's own list, so a refusal means the page
// is out of date, and it is the refusal page.
func (s *Server) handleMergeDimensionValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.dimensionIDFromPath(w, r)
	if !ok {
		return
	}
	into, err := strconv.ParseInt(r.FormValue("into"), 10, 64)
	if err != nil {
		http.Error(w, "choose a value to merge into", http.StatusUnprocessableEntity)
		return
	}
	if err := s.svc.MergeDimensionValue(r.Context(), current.ID, id, into); err != nil {
		s.writeDimensionError(w, r, err)
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

func (s *Server) dimensionIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// writeDimensionError answers a refused Dimension change from a button with
// nothing typed, such as Retire, with the refusal page, linking Back to the
// Dimensions page. The domain refuses a Dimension or value that isn't there
// for its input, but a missing record would be the Not found page.
func (s *Server) writeDimensionError(w http.ResponseWriter, r *http.Request, err error) {
	s.writeDimensionFormError(w, r, err, nil)
}

// writeDimensionFormError answers a refused Dimension change posted from
// form, a form with typed input on the Dimensions page: a refusal of what was
// typed comes back there, in form, and anything else, or a form the page no
// longer offers, is writeDimensionError's refusal page.
func (s *Server) writeDimensionFormError(w http.ResponseWriter, r *http.Request, err error, form refusedForm) {
	s.answerRefused(w, r, refused{Err: err, Heading: "Can't change Dimension", Back: "/dimensions", Form: form})
}

// addDimensionValueForm is Add a value to the Dimension id, on its card on
// the Dimensions page, saying why beside its input; the page offers it while
// the Dimension is listed.
func (s *Server) addDimensionValueForm(id int64) refusedForm {
	return onPage(fmt.Sprintf("/dimensions#dimension-%d", id), "add-dimension-value", id, []string{"value"}).
		besideInput("value").
		offeredWhen(func(ctx context.Context) (bool, error) {
			dims, err := s.svc.ListDimensions(ctx)
			return slices.ContainsFunc(dims, func(d domain.Dimension) bool { return d.ID == id }), err
		})
}

// renameDimensionValueForm is the rename of the value id, at its row on the
// Dimensions page, saying why beside its input; the page offers it while the
// value is listed and not Retired.
func (s *Server) renameDimensionValueForm(id int64) refusedForm {
	return onPage(fmt.Sprintf("/dimensions#dimension-value-%d", id), "rename-dimension-value", id, []string{"value"}).
		besideInput("value").
		offeredWhen(func(ctx context.Context) (bool, error) {
			dims, err := s.svc.ListDimensions(ctx)
			if err != nil {
				return false, err
			}
			for _, d := range dims {
				for _, v := range d.Values {
					if v.ID == id {
						return !v.Retired, nil
					}
				}
			}
			return false, nil
		})
}

// refusalOpensDimensionEdit reports whether the page carries a refusal, not
// yet taken, of a form behind d's Edit toggle, without taking it: its Add
// value, or the rename of one of its values. That Edit comes back open.
func refusalOpensDimensionEdit(ctx context.Context, d domain.Dimension) bool {
	refusal, ok := formRefusalUntaken(ctx)
	if !ok {
		return false
	}
	switch refusal.Form {
	case "add-dimension-value":
		return refusal.ID == d.ID
	case "rename-dimension-value":
		return slices.ContainsFunc(d.Values, func(v domain.DimensionValue) bool { return v.ID == refusal.ID })
	}
	return false
}

// refusedChoice is the radio choice the refused form posted as input, or
// fallback, the form's own default, when it wasn't refused or chose none.
func refusedChoice(refused formRefusal, input, fallback string) string {
	if typed := refused.typed(input); typed != "" {
		return typed
	}
	return fallback
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
