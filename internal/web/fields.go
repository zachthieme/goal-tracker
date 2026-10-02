package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// handleFields shows every Field and, for an Admin, the form to define one
// (CONTEXT.md: Admin defines Fields).
func (s *Server) handleFields(w http.ResponseWriter, r *http.Request, current domain.Account) {
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		http.Error(w, "could not list fields", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, fieldsPage(&current, fields))
}

// handleCreateField defines a Field from a name, a type and, for a number, an
// optional unit. Only an Admin may.
func (s *Server) handleCreateField(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if _, err := s.svc.CreateField(r.Context(), current.ID, r.FormValue("name"), r.FormValue("type"), r.FormValue("unit")); err != nil {
		writeFieldError(w, err)
		return
	}
	http.Redirect(w, r, "/fields", http.StatusSeeOther)
}

// handleRetireField retires the Field in the path, so it is no longer offered
// for entry on a Goal. Only an Admin may.
func (s *Server) handleRetireField(w http.ResponseWriter, r *http.Request, current domain.Account) {
	s.setFieldRetired(w, r, current, s.svc.RetireField)
}

// handleRestoreField reverses the retirement of the Field in the path. Only an
// Admin may.
func (s *Server) handleRestoreField(w http.ResponseWriter, r *http.Request, current domain.Account) {
	s.setFieldRetired(w, r, current, s.svc.RestoreField)
}

func (s *Server) setFieldRetired(w http.ResponseWriter, r *http.Request, current domain.Account, set func(ctx context.Context, actorID, fieldID int64) error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := set(r.Context(), current.ID, id); err != nil {
		writeFieldError(w, err)
		return
	}
	http.Redirect(w, r, "/fields", http.StatusSeeOther)
}

// writeFieldError maps a domain Field error to an HTTP status.
func writeFieldError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	default:
		http.Error(w, "field action failed", http.StatusInternalServerError)
	}
}

// fieldTypeLabel says what f holds, with a number Field's unit.
func fieldTypeLabel(f domain.Field) string {
	switch f.Type {
	case domain.FieldNumber:
		if f.Unit != "" {
			return "Number in " + f.Unit
		}
		return "Number"
	case domain.FieldShortText:
		return "Short text"
	case domain.FieldLongText:
		return "Long text"
	case domain.FieldDate:
		return "Date"
	}
	return f.Type
}
