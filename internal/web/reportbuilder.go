package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// reportBuilderView is the builder page for a new Report Definition: what was
// typed and picked, the problems a refused save found, and the choices it
// offers (CONTEXT.md: Report Definition).
type reportBuilderView struct {
	Name         string
	Introduction string
	// Mode is domain.ReportModeRules or domain.ReportModePicked.
	Mode string
	// PickedIDs are the Goals picked by hand, in the order picked, and Picked
	// those Goals.
	PickedIDs []int64
	Picked    []domain.Goal
	// Problems are a refused save's problems, each naming its input.
	Problems []*domain.InputError

	// Goals are every Goal, at any level, the pickers offer.
	Goals []domain.Goal
}

// The builder's Goal lists, each posted once per Goal in it.
const (
	inputPicked = "picked"
)

// handleNewReportForm shows the builder for a new Report Definition, selecting
// by rules until the author chooses to pick Goals by hand.
func (s *Server) handleNewReportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	v := reportBuilderView{Mode: domain.ReportModeRules}
	if err := s.loadReportBuilder(r, &v); err != nil {
		http.Error(w, "could not load the builder", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, reportBuilderPage(&current, v))
}

// handleNewReport saves the builder's Report Definition and lands on its
// draft. A refused save comes back as the builder, 422, as typed, with each
// problem beside its input.
func (s *Server) handleNewReport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	v, in := readReportBuilder(r)
	def, err := s.svc.SaveReportDefinition(r.Context(), current.ID, in)
	if err == nil {
		http.Redirect(w, r, "/reports/"+strconv.FormatInt(def.ID, 10), http.StatusSeeOther)
		return
	}
	if !errors.Is(err, domain.ErrValidation) {
		http.Error(w, "could not save the report", http.StatusInternalServerError)
		return
	}
	v.Problems = domain.InputErrors(err)
	if err := s.loadReportBuilder(r, &v); err != nil {
		http.Error(w, "could not load the builder", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, reportBuilderPage(&current, v))
}

// readReportBuilder reads the builder's form as typed, both modes' inputs
// kept, and the definition it saves: only what the chosen mode selects by.
func readReportBuilder(r *http.Request) (reportBuilderView, domain.SaveReportDefinitionInput) {
	v := reportBuilderView{
		Name:         r.PostFormValue("name"),
		Introduction: r.PostFormValue("introduction"),
		Mode:         r.PostFormValue("mode"),
		PickedIDs:    dedupeInt64s(formInt64s(r, inputPicked)),
	}
	in := domain.SaveReportDefinitionInput{Name: v.Name, Introduction: v.Introduction, Mode: v.Mode}
	if v.Mode == domain.ReportModePicked {
		in.Picked = v.PickedIDs
	}
	return v, in
}

// dedupeInt64s is ids without repeats, in first-seen order: a Goal both
// chipped and chosen in the no-script select posts twice.
func dedupeInt64s(ids []int64) []int64 {
	var out []int64
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// loadReportBuilder fills in the choices the builder offers, and the listed
// Goals as chips from their ids.
func (s *Server) loadReportBuilder(r *http.Request, v *reportBuilderView) error {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return err
	}
	v.Goals = goals
	v.Picked = goalsByID(goals, v.PickedIDs)
	return nil
}

// goalsByID are the Goals with the given ids, in the order given, leaving out
// any id no Goal has.
func goalsByID(goals []domain.Goal, ids []int64) []domain.Goal {
	var out []domain.Goal
	for _, id := range ids {
		if at := slices.IndexFunc(goals, func(g domain.Goal) bool { return g.ID == id }); at >= 0 {
			out = append(out, goals[at])
		}
	}
	return out
}
