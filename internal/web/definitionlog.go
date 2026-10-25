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

// definitionLogPageSize is how many changes one page of the whole Definition
// log shows.
const definitionLogPageSize = 100

// handleDefinitionLog shows the Definition log, newest first: every change to
// what Dimensions and Fields exist and how they're shaped, with who made it and
// when. The about query parameter, dimension:<id> or field:<id>, narrows it to
// one Dimension or Field. The whole log shows a page of its changes: the
// newest, or with before=<id> those before that change, and links to the
// older ones when there are any. Anyone signed in may read it; nothing edits
// it.
func (s *Server) handleDefinitionLog(w http.ResponseWriter, r *http.Request, current domain.Account) {
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		s.serverError(w, r, "could not list dimensions", err)
		return
	}
	fields, err := s.svc.ListFields(r.Context())
	if err != nil {
		s.serverError(w, r, "could not list fields", err)
		return
	}
	scope, ok := parseLogScope(r.URL.Query().Get("about"), dims, fields)
	if !ok {
		s.notFound(w, r)
		return
	}
	var changes []domain.DefinitionChange
	var olderHref string
	switch {
	case scope.DimensionID != 0:
		changes, err = s.svc.DimensionLog(r.Context(), scope.DimensionID)
	case scope.FieldID != 0:
		changes, err = s.svc.FieldLog(r.Context(), scope.FieldID)
	default:
		before, ok := parseLogBefore(r.URL.Query().Get("before"))
		if !ok {
			s.notFound(w, r)
			return
		}
		// One more than a page says whether there are older changes.
		changes, err = s.svc.DefinitionLog(r.Context(), before, definitionLogPageSize+1)
		if len(changes) > definitionLogPageSize {
			changes = changes[:definitionLogPageSize]
			olderHref = fmt.Sprintf("/definition-log?before=%d", changes[len(changes)-1].ID)
		}
	}
	if err != nil {
		s.serverError(w, r, "could not read the definition log", err)
		return
	}
	render(w, r, http.StatusOK, definitionLogPage(&current, dims, fields, scope, changes, olderHref, s.displayDates()))
}

// parseLogBefore reads the before query parameter, the ID of the change a page
// of the whole log starts before. Empty is 0, the newest page; ok is false for
// anything but a positive whole number.
func parseLogBefore(raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	before, err := strconv.ParseInt(raw, 10, 64)
	return before, err == nil && before > 0
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
