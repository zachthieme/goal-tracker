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
// re-rendered in place with the message next to the Path to Green field; on
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

	formData := func(msg string) checkinFormData {
		return checkinFormData{
			GoalID: goalID, Health: health, Status: status, PathToGreen: path, PathTargetDate: rawDate, Explanation: explanation,
			Metrics: metrics, Readings: rawReadings, Highlight: highlight, Error: msg,
		}
	}

	date, err := parseDate(rawDate)
	if err != nil {
		s.renderCheckinFormError(w, r, goalID, formData("invalid target date"))
		return
	}
	readings, err := readingsFromForm(rawReadings)
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

// readingsFromForm parses the non-blank raw readings into domain input. A field
// that is not a number is rejected with a message naming the Metric.
func readingsFromForm(raw map[int64]string) ([]domain.MetricReadingInput, error) {
	out := make([]domain.MetricReadingInput, 0, len(raw))
	for metricID, text := range raw {
		if text == "" {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid reading for metric %d", metricID)
		}
		out = append(out, domain.MetricReadingInput{MetricID: metricID, Value: value})
	}
	return out, nil
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
