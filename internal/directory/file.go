package directory

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// File reads people from a CSV file the Company exports from its HR system, for a
// Company without Authentik. It is a domain.Directory.
type File struct {
	path string
}

// NewFile reads the directory in the CSV file at path, afresh on each sync.
func NewFile(path string) *File {
	return &File{path: path}
}

// The columns File.People reads from the header.
const (
	fileEmailColumn   = "email"
	fileNameColumn    = "name"
	fileManagerColumn = "manager_email"
)

// People lists one person per row of the file. Its first row is the header,
// naming the email, name and manager_email columns in any order and case;
// other columns, empty or repeated, are ignored. An empty name gives no Name,
// and an empty manager_email no Manager. Blank lines and a leading UTF-8 BOM
// are skipped, and a stray quote in an unquoted field is kept as part of it.
// It returns an error naming the file, and no one, if the file can't be read,
// the header lacks one of those three columns or names one twice, a row has the
// wrong number of fields, or one of those three fields runs onto the next line,
// the sign of a quote that isn't closed (for a bad row the error gives its
// line).
func (f *File) People(_ context.Context) ([]domain.DirectoryPerson, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return nil, fmt.Errorf("directory file: %w", err)
	}
	defer func() { _ = file.Close() }()
	r := bufio.NewReader(file)
	if bom, _ := r.Peek(3); string(bom) == "\ufeff" {
		_, _ = r.Discard(3)
	}
	// FieldsPerRecord stays 0, so a row with a different number of fields
	// from the header is an error. LazyQuotes keeps a stray quote in an
	// unquoted field, as HR exports write names like Ada "the" L.
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("directory file %s is empty; its first row must be the header %s,%s,%s", f.path, fileEmailColumn, fileNameColumn, fileManagerColumn)
	}
	if err != nil {
		return nil, fmt.Errorf("directory file %s: %w", f.path, err)
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != fileEmailColumn && name != fileNameColumn && name != fileManagerColumn {
			continue
		}
		if _, dup := columns[name]; dup {
			return nil, fmt.Errorf("directory file %s: the header names the column %q twice", f.path, name)
		}
		columns[name] = i
	}
	readColumns := []string{fileEmailColumn, fileNameColumn, fileManagerColumn}
	var at [3]int
	for i, name := range readColumns {
		col, ok := columns[name]
		if !ok {
			return nil, fmt.Errorf("directory file %s: the header has no %q column", f.path, name)
		}
		at[i] = col
	}
	people := []domain.DirectoryPerson{}
	for {
		// Read returns a ragged row's fields along with its error, so the
		// error comes first.
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("directory file %s: %w", f.path, err)
		}
		// A quoted field may hold a newline, so a quote that is never closed
		// swallows the rows after it into one of the fields People reads.
		for i, col := range at {
			if strings.ContainsAny(rec[col], "\r\n") {
				line, _ := cr.FieldPos(col)
				return nil, fmt.Errorf("directory file %s: line %d: the %s field runs onto the next line; check for a quote that isn't closed", f.path, line, readColumns[i])
			}
		}
		people = append(people, domain.DirectoryPerson{Email: rec[at[0]], Name: rec[at[1]], ManagerEmail: rec[at[2]]})
	}
	return people, nil
}
