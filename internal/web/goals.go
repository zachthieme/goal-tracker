package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// dateLayout is the format the HTML date input (<input type="date">) submits and
// that the domain's calendar dates round-trip through.
const dateLayout = "2006-01-02"

func (s *Server) handleCreateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	_, err := s.svc.CreateGoal(r.Context(), domain.CreateGoalInput{
		Title:        r.FormValue("title"),
		WhyItMatters: r.FormValue("why_it_matters"),
		OwnerID:      current.ID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			// No Goal exists yet, so the way back is the new Goal form.
			s.refuse(w, r, "Can't create Goal", "/goals/new", err)
			return
		}
		s.serverError(w, r, "could not create goal", err)
		return
	}
	http.Redirect(w, r, "/goals", http.StatusSeeOther)
}

// goalIDFromPath parses the {id} path value, writing the Not found page and
// returning ok false when it is not a number.
func (s *Server) goalIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

// fmtDate renders a calendar date as ISO, or "" for the zero time: for a date
// input's value, a value compared as a string, or a query string. A page shows
// a date through dates.go instead (DESIGN.md: Dates and times).
func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

// fmtNum renders a Metric's baseline or target without trailing zeros.
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseDate reads a date form field in the HTML date-input format. An empty
// field is the zero time, which the domain treats as "no date".
func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, value)
}

// writeCommandResult redirects back to the Goal on success and answers a
// refusal with the refusal page, linking Back to the Goal: a validation error
// (e.g. an activation gate failure) as 422, a refusal to act on someone else's
// Goal as 403, and a missing record as the Not found page.
func (s *Server) writeCommandResult(w http.ResponseWriter, r *http.Request, goalID int64, err error) {
	if err != nil {
		s.refuse(w, r, "Can't change Goal", goalPath(goalID), err)
		return
	}
	http.Redirect(w, r, "/goals/"+strconv.FormatInt(goalID, 10), http.StatusSeeOther)
}

// writeFormResult is writeCommandResult for a form the Goal page opens in
// place: a refused value re-renders the page with that form open, the reason
// beside it and what was sent still in it.
func (s *Server) writeFormResult(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, form goalForm, err error) {
	if errors.Is(err, domain.ErrValidation) {
		s.renderRefusedForm(w, r, goalID, current, form, http.StatusUnprocessableEntity, err)
		return
	}
	s.writeCommandResult(w, r, goalID, err)
}

// renderRefusedForm re-renders the Goal page with status and form open,
// carrying why its submit was refused and what it sent. When the page no
// longer offers the viewer that form, such as Add Milestone once the Goal is
// Done, there is nowhere beside it to say why, so the refusal page says it,
// at the status refused's kind has, linking Back to the Goal. Either way the
// reason is a sentence without its internal prefix.
func (s *Server) renderRefusedForm(w http.ResponseWriter, r *http.Request, goalID int64, current domain.Account, form goalForm, status int, refused error) {
	view, err := s.goalPageView(r.Context(), goalID, current)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, "could not load goal", err)
		return
	}
	if !view.offers(form) {
		s.refuse(w, r, "Can't change Goal", goalPath(goalID), refused)
		return
	}
	view.Open, view.FormError, view.FormInput = form, sentence(domain.Reason(refused)), r.PostForm
	render(w, r, status, goalPage(&current, view))
}

// handleEditWhyItMatters saves the Edit title and Why it matters form, for the Goal's Owner
// or an Admin, as EditTitleAndWhyItMatters does: the title only when the form
// sends one.
func (s *Server) handleEditWhyItMatters(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	var title *string
	if _, sent := r.PostForm["title"]; sent {
		t := r.PostFormValue("title")
		title = &t
	}
	_, err := s.svc.EditTitleAndWhyItMatters(r.Context(), current.ID, id, title, r.PostFormValue("why_it_matters"))
	s.writeFormResult(w, r, id, current, formTitleWhyItMatters, err)
}

// handleAddMilestone adds a Milestone from the Goal page's Add Milestone form,
// as its Owner or a Delegate. A date that doesn't parse is sent on as no
// date, so the domain refuses it beside the form like any other.
func (s *Server) handleAddMilestone(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	date, _ := parseDate(r.FormValue("target_date"))
	_, err := s.svc.AddMilestoneAsAuthor(r.Context(), current.ID, domain.AddMilestoneInput{
		GoalID:     id,
		Name:       r.FormValue("name"),
		TargetDate: date,
	})
	s.writeFormResult(w, r, id, current, formMilestones, err)
}

// handleEditMilestone saves a Milestone's Edit form on the Goal page. A date
// that doesn't parse is sent on unparsed, so EditMilestone refuses it only
// once it has checked who sent it.
func (s *Server) handleEditMilestone(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	var unparsed unparsedGoalFields
	m, err := s.svc.EditMilestone(r.Context(), current.ID, domain.EditMilestoneInput{
		MilestoneID: id,
		Name:        r.FormValue("name"),
		TargetDate:  unparsed.date(r, "target_date"),
		Unparsed:    unparsed,
	})
	if err != nil {
		s.writeCommandResult(w, r, id, err)
		return
	}
	s.writeCommandResult(w, r, m.GoalID, nil)
}

// handleEditMetric saves a Metric's Edit form on the Goal page. A number or
// date that doesn't parse is sent on unparsed, so EditMetric refuses it only
// once it has checked who sent it.
func (s *Server) handleEditMetric(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	var unparsed unparsedGoalFields
	m, err := s.svc.EditMetric(r.Context(), current.ID, domain.EditMetricInput{
		MetricID:   id,
		Name:       r.FormValue("name"),
		Unit:       r.FormValue("unit"),
		Direction:  r.FormValue("direction"),
		Baseline:   unparsed.metricNumber(r, "baseline"),
		Target:     unparsed.metricNumber(r, "target"),
		TargetDate: unparsed.date(r, "target_date"),
		Unparsed:   unparsed,
	})
	if err != nil {
		s.writeCommandResult(w, r, id, err)
		return
	}
	s.writeCommandResult(w, r, m.GoalID, nil)
}

// unparsedGoalFields are the values a Goal page form sent that don't parse,
// each refused under its input's name, for the domain command to refuse once
// it has checked who sent them, as DefinedGoalInput.Unparsed are.
type unparsedGoalFields []*domain.InputError

func (u *unparsedGoalFields) refuse(input, format string, args ...any) {
	*u = append(*u, &domain.InputError{Input: input, Message: fmt.Sprintf(format, args...)})
}

// date reads input as a date, blank being no date. One that doesn't parse is
// refused and read as no date.
func (u *unparsedGoalFields) date(r *http.Request, input string) time.Time {
	value := r.FormValue(input)
	d, err := parseDate(strings.TrimSpace(value))
	if err != nil {
		u.refuse(input, "%q isn't a date", value)
	}
	return d
}

// metricNumber reads input, a Metric's baseline or target, as a number. One
// that is blank or doesn't parse is refused and read as 0.
func (u *unparsedGoalFields) metricNumber(r *http.Request, input string) float64 {
	value := strings.TrimSpace(r.FormValue(input))
	if value == "" {
		u.refuse(input, "a Metric needs a %s", input)
		return 0
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		u.refuse(input, "the %s must be a number, not %q", input, value)
		return 0
	}
	return n
}

func (s *Server) handleActivateGoal(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	_, err := s.svc.ActivateGoal(r.Context(), current.ID, id)
	s.writeCommandResult(w, r, id, err)
}

// handleAssignGoalValue sets the Goal's Dimension values (CONTEXT.md: Owners
// and their Delegates set a Goal's Dimension values). A form naming a
// dimension_id and a new_value, an Extendable Dimension's add-a-value input,
// sets the value so named, adding it to the list when it's new. One naming a
// dimension_id without a new_value, a several-values Dimension's checkboxes or
// a one-value Dimension's select, saves its chosen value_ids together, so an
// unchecked value is removed and the select's "None" clears the Dimension. A
// single value_id without a dimension_id replaces any value the Goal already
// carries in its Dimension, and an empty one is a no-op.
// Anyone but the Owner, a Delegate or an Admin is refused.
func (s *Server) handleAssignGoalValue(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := s.goalIDFromPath(w, r)
	if !ok {
		return
	}
	if rawDim := r.FormValue("dimension_id"); rawDim != "" {
		dimensionID, err := strconv.ParseInt(rawDim, 10, 64)
		if err != nil {
			http.Error(w, "invalid dimension", http.StatusUnprocessableEntity)
			return
		}
		if newValue, ok := r.Form["new_value"]; ok {
			_, err := s.svc.AssignGoalValueByName(r.Context(), current.ID, id, dimensionID, newValue[0])
			s.writeFormResult(w, r, id, current, formDimensions, err)
			return
		}
		var valueIDs []int64
		for _, raw := range r.Form["value_id"] {
			if raw == "" {
				continue
			}
			valueID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid value", http.StatusUnprocessableEntity)
				return
			}
			valueIDs = append(valueIDs, valueID)
		}
		s.writeFormResult(w, r, id, current, formDimensions, s.svc.SetGoalValues(r.Context(), current.ID, id, dimensionID, valueIDs))
		return
	}
	raw := r.FormValue("value_id")
	if raw == "" {
		s.writeCommandResult(w, r, id, nil)
		return
	}
	valueID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		http.Error(w, "invalid value", http.StatusUnprocessableEntity)
		return
	}
	s.writeFormResult(w, r, id, current, formDimensions, s.svc.AssignGoalValue(r.Context(), current.ID, id, valueID))
}
