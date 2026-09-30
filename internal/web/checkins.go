package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleSubmitCheckin records a Check-in on the Goal in the path, written by the
// current Account. The form is htmx-driven: on a validation error the form is
// re-rendered in place with the message next to the field it is about; on
// success htmx is told to reload the Goal page so the new history and current
// Health show.
func (s *Server) handleSubmitCheckin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	health := r.FormValue("health")
	status := r.FormValue("status")
	path := r.FormValue("path_to_green")
	explanation := r.FormValue("explanation")
	rawDate := r.FormValue("path_target_date")
	highlight := highlightFromForm(r)

	// The reading inputs are named reading_<metricID>, one per Metric on the
	// Goal, so parsing them needs the Goal's current Metrics.
	metrics, err := s.svc.ListMetrics(r.Context(), goalID)
	if err != nil {
		http.Error(w, "could not load metrics", http.StatusInternalServerError)
		return
	}
	// Values the reader typed, kept so an error re-render shows them again.
	rawReadings := rawReadingsFromForm(r, metrics)

	// The delivery date and Milestone fields are keyed to the Goal's current
	// state: whether it is Dated, and which Milestones are still Planned.
	goal, err := s.svc.ViewGoal(r.Context(), goalID)
	if err != nil {
		writeCheckinError(w, err)
		return
	}
	milestones, err := s.svc.ListMilestones(r.Context(), goalID)
	if err != nil {
		http.Error(w, "could not load milestones", http.StatusInternalServerError)
		return
	}
	dates := datesFromForm(r, goal, milestones)

	lifecycle := lifecycleFromForm(r, goal)

	formData := func(msg string) checkinFormData {
		return checkinFormData{
			GoalID: goalID, Health: health, Status: status, PathToGreen: path, PathTargetDate: rawDate, Explanation: explanation,
			Metrics: metrics, Readings: rawReadings, Highlight: highlight, Dates: dates, Lifecycle: lifecycle,
			Error: checkinError{Field: checkinErrorField(msg), Message: msg},
		}
	}

	date, err := parseDate(rawDate)
	if err != nil {
		s.renderCheckinFormError(w, r, goalID, formData("invalid target date"))
		return
	}
	readings, err := readingsFromForm(metrics, rawReadings)
	if err != nil {
		s.renderCheckinFormError(w, r, goalID, formData(err.Error()))
		return
	}

	in := domain.SubmitCheckinInput{
		GoalID:         goalID,
		AuthorID:       current.ID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: date,
		Explanation:    explanation,
		Readings:       readings,
		// The Lifecycle fields go through as typed; the domain decides which
		// changes a Check-in may make and what each needs.
		Lifecycle:       lifecycle.To,
		LifecycleReason: lifecycle.Reason,
		Outcome:         lifecycle.Outcome,
	}
	if err := dates.applyTo(&in); err != nil {
		s.renderCheckinFormError(w, r, goalID, formData(err.Error()))
		return
	}
	if highlight.Kind != "" {
		in.Highlight = &domain.HighlightInput{Kind: highlight.Kind, Note: highlight.Note}
	}

	if _, err = s.svc.SubmitCheckin(r.Context(), in); err != nil {
		if errors.Is(err, domain.ErrValidation) {
			s.renderCheckinFormError(w, r, goalID, formData(err.Error()))
			return
		}
		writeCheckinError(w, err)
		return
	}
	s.checkinRedirect(w, r, goalID)
}

// checkinErrorField names the form field a Check-in validation message is
// about. The domain reports a Check-in's validation errors as messages, so the
// field is read from the message's wording.
func checkinErrorField(msg string) string {
	switch {
	case strings.Contains(msg, "differs from the Rolled-up Health"):
		return "explanation"
	case strings.Contains(msg, "cancelling a Goal needs a reason"), strings.Contains(msg, "On Hold needs a reason"):
		return "lifecycle_reason"
	case strings.Contains(msg, "can't move a"), strings.Contains(msg, "can only be resumed or Cancelled"):
		return "lifecycle"
	case strings.Contains(msg, "needs an outcome"), strings.Contains(msg, "outcome is one line"):
		return "outcome"
	default:
		return "path_to_green"
	}
}

// rawReadingsFromForm reads the reading_<metricID> field for each of the Goal's
// Metrics, keyed by Metric ID, keeping the raw text so an error re-render can
// show what the reader typed. A blank field means no reading for that Metric.
func rawReadingsFromForm(r *http.Request, metrics []domain.Metric) map[int64]string {
	out := make(map[int64]string, len(metrics))
	for _, m := range metrics {
		out[m.ID] = strings.TrimSpace(r.FormValue("reading_" + strconv.FormatInt(m.ID, 10)))
	}
	return out
}

// readingsFromForm parses the Goal's non-blank raw readings into domain input. A
// field that is not a number is rejected with a message naming the Metric.
func readingsFromForm(metrics []domain.Metric, raw map[int64]string) ([]domain.MetricReadingInput, error) {
	out := make([]domain.MetricReadingInput, 0, len(raw))
	for _, m := range metrics {
		text := raw[m.ID]
		if text == "" {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%q needs a number", m.Name)
		}
		out = append(out, domain.MetricReadingInput{MetricID: m.ID, Value: value})
	}
	return out, nil
}

// datesFromForm reads the Check-in's date and Milestone fields as typed: the
// delivery date (for a Dated Goal) and its reason, a row per Planned Milestone
// (milestone_*_<id>), and the new-Milestone rows (new_milestone_name and
// new_milestone_date, paired by position).
func datesFromForm(r *http.Request, goal domain.Goal, milestones []domain.Milestone) checkinDatesFormData {
	d := checkinDatesFormData{Dated: goal.Kind == domain.GoalDated}
	if d.Dated {
		d.DeliveryDate = strings.TrimSpace(r.FormValue("delivery_date"))
		d.DeliveryDateReason = r.FormValue("delivery_date_reason")
	}
	for _, m := range milestones {
		if m.Status != domain.MilestonePlanned {
			continue
		}
		id := strconv.FormatInt(m.ID, 10)
		d.Milestones = append(d.Milestones, milestoneFormRow{
			Milestone:     m,
			Date:          strings.TrimSpace(r.FormValue("milestone_date_" + id)),
			DateReason:    r.FormValue("milestone_date_reason_" + id),
			Status:        r.FormValue("milestone_status_" + id),
			RemovedReason: r.FormValue("milestone_removed_reason_" + id),
		})
	}
	names := r.Form["new_milestone_name"]
	newDates := r.Form["new_milestone_date"]
	for i, name := range names {
		row := newMilestoneFormRow{Name: name}
		if i < len(newDates) {
			row.Date = strings.TrimSpace(newDates[i])
		}
		if strings.TrimSpace(row.Name) == "" && row.Date == "" {
			continue
		}
		d.NewMilestones = append(d.NewMilestones, row)
	}
	return d
}

// applyTo parses the typed dates into the Check-in's date and Milestone
// changes. A date that isn't a date is rejected with a message naming the field.
func (d checkinDatesFormData) applyTo(in *domain.SubmitCheckinInput) error {
	delivery, err := parseDate(d.DeliveryDate)
	if err != nil {
		return errors.New("invalid delivery date")
	}
	in.DeliveryDate = delivery
	in.DeliveryDateReason = d.DeliveryDateReason
	for _, row := range d.Milestones {
		date, err := parseDate(row.Date)
		if err != nil {
			return fmt.Errorf("invalid date for Milestone %q", row.Milestone.Name)
		}
		in.Milestones = append(in.Milestones, domain.MilestoneChangeInput{
			MilestoneID:   row.Milestone.ID,
			TargetDate:    date,
			DateReason:    row.DateReason,
			Status:        row.Status,
			RemovedReason: row.RemovedReason,
		})
	}
	for _, row := range d.NewMilestones {
		date, err := parseDate(row.Date)
		if err != nil {
			return fmt.Errorf("invalid date for new Milestone %q", row.Name)
		}
		in.NewMilestones = append(in.NewMilestones, domain.NewMilestoneInput{Name: row.Name, TargetDate: date})
	}
	return nil
}

// lifecycleFromForm reads the Check-in's Lifecycle fields: the Lifecycle to move
// the Goal to (empty to leave it alone), and the reason or outcome that change
// needs. Current is the Goal's Lifecycle, which decides the choices offered.
func lifecycleFromForm(r *http.Request, goal domain.Goal) lifecycleFormData {
	return lifecycleFormData{
		Current: goal.Lifecycle,
		To:      r.FormValue("lifecycle"),
		Reason:  r.FormValue("lifecycle_reason"),
		Outcome: r.FormValue("outcome"),
	}
}

// highlightFromForm reads the optional Highlight fields; an empty kind means the
// reader flagged nothing.
func highlightFromForm(r *http.Request) highlightFormData {
	return highlightFormData{
		Kind: r.FormValue("highlight_kind"),
		Note: r.FormValue("highlight_note"),
	}
}

// handleNoChangeCheckin records a Check-in repeating the Goal's previous values
// in one click.
func (s *Server) handleNoChangeCheckin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	goalID, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	if _, err := s.svc.SubmitNoChangeCheckin(r.Context(), goalID, current.ID); err != nil {
		writeCheckinError(w, err)
		return
	}
	s.checkinRedirect(w, r, goalID)
}

// renderCheckinFormError re-renders the Check-in form with a validation message.
// For an htmx request it returns 200 so htmx swaps the form in place (htmx does
// not swap error statuses by default); a plain post gets 422. It reloads the
// Goal's Rolled-up Health so the re-rendered form still shows it beside the
// Owner's Health (ADR-0003).
func (s *Server) renderCheckinFormError(w http.ResponseWriter, r *http.Request, goalID int64, data checkinFormData) {
	if rollup, err := s.svc.RolledUpHealth(r.Context(), goalID); err == nil {
		data.RolledUp = rollup
	}
	status := http.StatusUnprocessableEntity
	if r.Header.Get("HX-Request") == "true" {
		status = http.StatusOK
	}
	render(w, r, status, checkinForm(data))
}

// checkinRedirect returns the reader to the Goal page after a successful
// Check-in: htmx reloads via HX-Redirect (so history and current Health refresh),
// a plain post follows a 303.
func (s *Server) checkinRedirect(w http.ResponseWriter, r *http.Request, goalID int64) {
	target := "/goals/" + strconv.FormatInt(goalID, 10)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// writeCheckinError maps a domain Check-in error to an HTTP status.
func writeCheckinError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "check-in failed", http.StatusInternalServerError)
	}
}
