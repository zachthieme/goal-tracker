package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
	s.fieldAction(w, r, current, s.svc.RetireField)
}

// handleRestoreField reverses the retirement of the Field in the path. Only an
// Admin may.
func (s *Server) handleRestoreField(w http.ResponseWriter, r *http.Request, current domain.Account) {
	s.fieldAction(w, r, current, s.svc.RestoreField)
}

// handleSetFieldRequired marks the Field in the path required when the form's
// required is 1, and unmarks it otherwise (CONTEXT.md: Incomplete). Only an
// Admin may.
func (s *Server) handleSetFieldRequired(w http.ResponseWriter, r *http.Request, current domain.Account) {
	s.fieldAction(w, r, current, func(ctx context.Context, actorID, fieldID int64) error {
		return s.svc.SetFieldRequired(ctx, actorID, fieldID, r.FormValue("required") == "1")
	})
}

// fieldAction runs set on the Field in the path for the signed-in Admin, then
// goes back to the Fields page.
func (s *Server) fieldAction(w http.ResponseWriter, r *http.Request, current domain.Account, set func(ctx context.Context, actorID, fieldID int64) error) {
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

// handleSetGoalField sets the Goal's value in the form's field_id, or clears it
// when the form's Clear button sent it or the value is blank (CONTEXT.md:
// Field). A value that doesn't parse as the Field's type is refused naming the
// Field. Anyone but the Owner, a Delegate or an Admin is refused.
func (s *Server) handleSetGoalField(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, ok := goalIDFromPath(w, r)
	if !ok {
		return
	}
	fieldID, err := strconv.ParseInt(r.FormValue("field_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid field", http.StatusUnprocessableEntity)
		return
	}
	value := r.FormValue("value")
	if r.FormValue("clear") != "" {
		value = ""
	}
	s.writeFormResult(w, r, id, current, formFields, s.svc.SetGoalField(r.Context(), current.ID, id, fieldID, value))
}

// fieldValue returns the Goal's value in the given Field, and whether it has
// one.
func (v goalView) fieldValue(fieldID int64) (string, bool) {
	for _, fv := range v.FieldValues {
		if fv.Field.ID == fieldID {
			return fv.Value, true
		}
	}
	return "", false
}

// fieldFormValue is what the Goal page's form for the Field shows: what was
// typed when that form's save was just refused, else the Goal's value.
func (v goalView) fieldFormValue(fieldID int64) string {
	if v.FormInput.Get("field_id") == strconv.FormatInt(fieldID, 10) {
		return v.FormInput.Get("value")
	}
	value, _ := v.fieldValue(fieldID)
	return value
}

// isWebURL reports whether a short text is an http(s) URL, which the Goal page
// shows as a link.
func isWebURL(text string) bool {
	u, err := url.Parse(text)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(text, " \t\n")
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
