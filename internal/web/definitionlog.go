package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// logScope is what the Definition log page is narrowed to: one Dimension, one
// Field, or nothing (the whole log). Name is the Dimension's or Field's
// current name.
type logScope struct {
	DimensionID int64
	FieldID     int64
	Name        string
}

// param is the scope as the page's about query parameter writes it.
func (sc logScope) param() string {
	switch {
	case sc.DimensionID != 0:
		return fmt.Sprintf("dimension:%d", sc.DimensionID)
	case sc.FieldID != 0:
		return fmt.Sprintf("field:%d", sc.FieldID)
	}
	return ""
}

// handleDefinitionLog shows the Definition log, newest first: every change to
// what Dimensions and Fields exist and how they're shaped, with who made it and
// when. The about query parameter, dimension:<id> or field:<id>, narrows it to
// one Dimension or Field. Anyone signed in may read it; nothing edits it.
func (s *Server) handleDefinitionLog(w http.ResponseWriter, r *http.Request, current domain.Account) {
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		http.Error(w, "could not list dimensions", http.StatusInternalServerError)
		return
	}
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		http.Error(w, "could not list fields", http.StatusInternalServerError)
		return
	}
	scope, ok := parseLogScope(r.URL.Query().Get("about"), dims, fields)
	if !ok {
		s.notFound(w, r)
		return
	}
	var changes []domain.DefinitionChange
	switch {
	case scope.DimensionID != 0:
		changes, err = s.svc.DimensionLog(r.Context(), scope.DimensionID)
	case scope.FieldID != 0:
		changes, err = s.svc.FieldLog(r.Context(), scope.FieldID)
	default:
		changes, err = s.svc.DefinitionLog(r.Context())
	}
	if err != nil {
		http.Error(w, "could not read the definition log", http.StatusInternalServerError)
		return
	}
	render(w, r, http.StatusOK, definitionLogPage(&current, dims, fields, scope, changes))
}

// parseLogScope reads the about query parameter against the Dimensions and
// Fields there are. Empty is the whole log; ok is false for anything naming
// neither an existing Dimension nor an existing Field.
func parseLogScope(about string, dims []domain.Dimension, fields []domain.Field) (logScope, bool) {
	if about == "" {
		return logScope{}, true
	}
	kind, rawID, _ := strings.Cut(about, ":")
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return logScope{}, false
	}
	switch kind {
	case "dimension":
		for _, d := range dims {
			if d.ID == id {
				return logScope{DimensionID: id, Name: d.Name}, true
			}
		}
	case "field":
		for _, f := range fields {
			if f.ID == id {
				return logScope{FieldID: id, Name: f.Name}, true
			}
		}
	}
	return logScope{}, false
}
