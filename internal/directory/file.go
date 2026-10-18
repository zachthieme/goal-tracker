package directory

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
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
// the header lacks one of those three columns or names one twice, or a row has
// the wrong number of fields (the error gives its line).
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
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("directory file %s: %w", f.path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("directory file %s is empty; its first row must be the header %s,%s,%s", f.path, fileEmailColumn, fileNameColumn, fileManagerColumn)
	}
	columns := map[string]int{}
	for i, name := range records[0] {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != fileEmailColumn && name != fileNameColumn && name != fileManagerColumn {
			continue
		}
		if _, dup := columns[name]; dup {
			return nil, fmt.Errorf("directory file %s: the header names the column %q twice", f.path, name)
		}
		columns[name] = i
	}
	var at [3]int
	for i, name := range []string{fileEmailColumn, fileNameColumn, fileManagerColumn} {
		col, ok := columns[name]
		if !ok {
			return nil, fmt.Errorf("directory file %s: the header has no %q column", f.path, name)
		}
		at[i] = col
	}
	people := make([]domain.DirectoryPerson, 0, len(records)-1)
	for _, rec := range records[1:] {
		people = append(people, domain.DirectoryPerson{Email: rec[at[0]], Name: rec[at[1]], ManagerEmail: rec[at[2]]})
	}
	return people, nil
}
