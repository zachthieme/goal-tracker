package web

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
)

// maxImportBytes caps an uploaded spreadsheet, so a stray large file can't
// exhaust memory.
const maxImportBytes = 5 << 20 // 5 MiB

// handleImportForm shows the spreadsheet-import page. Anyone signed in can see
// it, but only an Admin gets the upload form (ticket #22: an Admin runs the
// import).
func (s *Server) handleImportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	render(w, r, http.StatusOK, importsPage(&current, nil, ""))
}

// handleRunImport reads an uploaded CSV or XLSX and runs a dry run or a commit,
// then re-renders the page with the row-by-row report. Only an Admin may.
func (s *Server) handleRunImport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may import Goals", http.StatusForbidden)
		return
	}
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		render(w, r, http.StatusBadRequest, importsPage(&current, nil, "Could not read the upload."))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		render(w, r, http.StatusUnprocessableEntity, importsPage(&current, nil, "Choose a CSV or XLSX file to import."))
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxImportBytes))
	if err != nil {
		s.serverError(w, r, "could not read the upload", err)
		return
	}

	imp := importer.New(s.svc)
	commit := r.FormValue("action") == "commit"
	var report importer.Report
	if commit {
		report, err = imp.Commit(r.Context(), current.ID, header.Filename, data)
	} else {
		report, err = imp.DryRun(r.Context(), current.ID, header.Filename, data)
	}
	if err != nil {
		status, flash := http.StatusInternalServerError, "The import failed."
		switch {
		case errors.Is(err, domain.ErrNotAuthorized):
			status, flash = http.StatusForbidden, "Only an Admin may import Goals."
		case errors.Is(err, domain.ErrValidation):
			status, flash = http.StatusUnprocessableEntity, importFlash(err)
		default:
			s.logServerError(r, err)
		}
		render(w, r, status, importsPage(&current, nil, flash))
		return
	}
	render(w, r, http.StatusOK, importsPage(&current, &report, ""))
}

// importFlash renders a whole-file error (a bad header, an unknown Dimension
// column) plainly, dropping the internal "validation failed:" prefix.
func importFlash(err error) string {
	return strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")
}

// importRowLabel is the title shown for a report row, or a placeholder when the
// row named no Goal.
func importRowLabel(row importer.RowResult) string {
	if strings.TrimSpace(row.Title) == "" {
		return "(untitled)"
	}
	return row.Title
}
