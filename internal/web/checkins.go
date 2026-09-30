package web

import (
	"errors"
	"net/http"
	"strconv"

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
	date, err := parseDate(rawDate)
	if err != nil {
		s.renderCheckinFormError(w, r, goalID, checkinFormData{
			GoalID: goalID, Health: health, Status: status, PathToGreen: path, PathTargetDate: rawDate, Explanation: explanation,
			Error: "invalid target date",
		})
		return
	}

	_, err = s.svc.SubmitCheckin(r.Context(), domain.SubmitCheckinInput{
		GoalID:         goalID,
		AuthorID:       current.ID,
		Health:         health,
		Status:         status,
		PathToGreen:    path,
		PathTargetDate: date,
		Explanation:    explanation,
	})
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			s.renderCheckinFormError(w, r, goalID, checkinFormData{
				GoalID: goalID, Health: health, Status: status, PathToGreen: path, PathTargetDate: rawDate, Explanation: explanation,
				Error: err.Error(),
			})
			return
		}
		writeCheckinError(w, err)
		return
	}
	s.checkinRedirect(w, r, goalID)
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
