package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/importer"
)

// maxImportBytes caps an uploaded spreadsheet, so a stray large file can't
// exhaust memory.
const maxImportBytes = 5 << 20 // 5 MiB

// maxImportRequestBytes caps an import's whole request body: the spreadsheet,
// plus room for the multipart form's boundaries, part headers and action.
const maxImportRequestBytes = maxImportBytes + 64<<10

// handleImportForm shows the spreadsheet-import page. Anyone signed in can see
// it, but only an Admin gets the upload form (ticket #22: an Admin runs the
// import).
func (s *Server) handleImportForm(w http.ResponseWriter, r *http.Request, current domain.Account) {
	render(w, r, http.StatusOK, importsPage(&current, nil, "", ""))
}

// handleImportTemplate downloads the import's CSV template: the header row of
// the import format's own columns, to fill in and upload (#676).
func (s *Server) handleImportTemplate(w http.ResponseWriter, r *http.Request, _ domain.Account) {
	var buf bytes.Buffer
	if err := importer.WriteTemplate(&buf); err != nil {
		s.serverError(w, r, "could not write the import template", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="goal-import-template.csv"`)
	_, _ = w.Write(buf.Bytes())
}

// errNoImportFile refuses an import posting no file, which the page says
// beside the file input.
var errNoImportFile = domain.Refuse(domain.ErrValidation, "Choose a file to import.")

// handleRunImport reads an uploaded CSV or XLSX and runs a dry run or a commit,
// then re-renders the page with the row-by-row report. Only an Admin may.
func (s *Server) handleRunImport(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		s.refuseNonAdmin(w, r, "only an Admin may import Goals")
		return
	}
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		render(w, r, http.StatusBadRequest, importsPage(&current, nil, "Could not read the upload.", ""))
		return
	}
	// A refused import comes back as the page, saying why: beside the file
	// input when there's no file, and above the form otherwise.
	refuse := func(err error) {
		s.answerRefused(w, r, refused{Err: err, Heading: "Admins only", Back: "/home",
			Form: asPage(func(_ context.Context, err error) (templ.Component, error) {
				if errors.Is(err, errNoImportFile) {
					return importsPage(&current, nil, "", refusalSentence(err)), nil
				}
				return importsPage(&current, nil, refusalSentence(err), ""), nil
			})})
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		refuse(errNoImportFile)
		return
	}
	defer func() { _ = file.Close() }()
	// One byte past the cap, so a file over it is refused rather than cut short
	// and imported.
	data, err := io.ReadAll(io.LimitReader(file, maxImportBytes+1))
	if err != nil {
		s.serverError(w, r, "could not read the upload", err)
		return
	}
	if len(data) > maxImportBytes {
		bodyTooLarge(w)
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
	if errors.Is(err, domain.ErrNotAuthorized) {
		err = domain.Refuse(domain.ErrNotAuthorized, "only an Admin may import Goals")
	}
	if err != nil {
		refuse(err)
		return
	}
	render(w, r, http.StatusOK, importsPage(&current, &report, "", ""))
}

// importRowLabel is the title shown for a report row, or a placeholder when the
// row named no Goal.
func importRowLabel(row importer.RowResult) string {
	if strings.TrimSpace(row.Title) == "" {
		return "(untitled)"
	}
	return row.Title
}
