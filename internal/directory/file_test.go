package directory_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// writeDirectoryFile writes contents to a CSV file in a fresh directory and
// returns its path.
func writeDirectoryFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "people.csv")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileListsOnePersonPerRow(t *testing.T) {
	t.Parallel()
	path := writeDirectoryFile(t, "email,name,manager_email\n"+
		"ada@example.com,Ada Lovelace,grace@example.com\n"+
		"grace@example.com,Grace Hopper,\n")

	people, err := directory.NewFile(path).People(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DirectoryPerson{
		{Email: "ada@example.com", Name: "Ada Lovelace", ManagerEmail: "grace@example.com"},
		{Email: "grace@example.com", Name: "Grace Hopper"},
	}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("People() = %+v, want %+v", people, want)
	}
}

// The header's columns may come in any order, in any case, with spaces around
// them, after a UTF-8 BOM.
func TestFileReadsColumnsInAnyOrderAfterABOM(t *testing.T) {
	t.Parallel()
	path := writeDirectoryFile(t, "\ufeff Manager_Email ,NAME, email\n"+
		"grace@example.com,Ada Lovelace,ada@example.com\n"+
		",Grace Hopper,grace@example.com\n")

	people, err := directory.NewFile(path).People(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DirectoryPerson{
		{Email: "ada@example.com", Name: "Ada Lovelace", ManagerEmail: "grace@example.com"},
		{Email: "grace@example.com", Name: "Grace Hopper"},
	}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("People() = %+v, want %+v", people, want)
	}
}

// Each call reads the file afresh, so an edit shows up on the next sync.
func TestFileRereadsTheFileEachTime(t *testing.T) {
	t.Parallel()
	path := writeDirectoryFile(t, "email,name,manager_email\nada@example.com,Ada,grace@example.com\n")
	dir := directory.NewFile(path)
	if _, err := dir.People(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("email,name,manager_email\nada@example.com,Ada,alan@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	people, err := dir.People(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DirectoryPerson{{Email: "ada@example.com", Name: "Ada", ManagerEmail: "alan@example.com"}}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("People() after the rewrite = %+v, want %+v", people, want)
	}
}

func TestFileIgnoresOtherColumns(t *testing.T) {
	t.Parallel()
	path := writeDirectoryFile(t, "employee_id,email,name,department,manager_email\n"+
		"17,ada@example.com,Ada,Engineering,grace@example.com\n")

	people, err := directory.NewFile(path).People(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DirectoryPerson{{Email: "ada@example.com", Name: "Ada", ManagerEmail: "grace@example.com"}}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("People() = %+v, want %+v", people, want)
	}
}

// Columns People doesn't read may repeat, empty ones included, as spreadsheet
// exports often leave trailing empty cells.
func TestFileIgnoresRepeatedOtherColumns(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		contents string
	}{
		{
			name: "trailing empty cells",
			contents: "email,name,manager_email,,\n" +
				"ada@example.com,Ada,grace@example.com,,\n" +
				"grace@example.com,Grace,,,\n",
		},
		{
			name: "an ignored column named twice",
			contents: "notes,email,name,manager_email,Notes\n" +
				"first,ada@example.com,Ada,grace@example.com,second\n" +
				",grace@example.com,Grace,,\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeDirectoryFile(t, tc.contents)

			people, err := directory.NewFile(path).People(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := []domain.DirectoryPerson{
				{Email: "ada@example.com", Name: "Ada", ManagerEmail: "grace@example.com"},
				{Email: "grace@example.com", Name: "Grace"},
			}
			if !reflect.DeepEqual(people, want) {
				t.Fatalf("People() = %+v, want %+v", people, want)
			}
		})
	}
}

// A stray quote in an unquoted field is kept as part of it, while a quoted
// field still reads its commas and "" escapes.
func TestFileReadsQuotesInFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		row      string
		wantName string
	}{
		{name: "bare quote", row: `ada@x.com,Ada "the" L,`, wantName: `Ada "the" L`},
		{name: "quoted field", row: `ada@x.com,"Okafor, Ada ""A""",`, wantName: `Okafor, Ada "A"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeDirectoryFile(t, "email,name,manager_email\n"+tc.row+"\n")

			people, err := directory.NewFile(path).People(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := []domain.DirectoryPerson{{Email: "ada@x.com", Name: tc.wantName}}
			if !reflect.DeepEqual(people, want) {
				t.Fatalf("People() = %+v, want %+v", people, want)
			}
		})
	}
}

// One of the columns People reads, named twice whatever its case, is an error
// naming it.
func TestFileRejectsADuplicatedColumn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		contents string
		want     string
	}{
		{
			name: "name",
			contents: "email,name,manager_email,Name\n" +
				"ada@example.com,Ada,grace@example.com,Ada L\n",
			want: `the header names the column "name" twice`,
		},
		{
			name: "email",
			contents: "email,name,manager_email, EMAIL\n" +
				"ada@example.com,Ada,grace@example.com,ada@example.org\n",
			want: `the header names the column "email" twice`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeDirectoryFile(t, tc.contents)

			people, err := directory.NewFile(path).People(t.Context())
			if err == nil {
				t.Fatalf("People() accepted a header naming %s twice: %+v", tc.name, people)
			}
			if people != nil {
				t.Errorf("People() listed %+v along with the error", people)
			}
			if !strings.Contains(err.Error(), "people.csv") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q doesn't name the file and say %s", err, tc.want)
			}
		})
	}
}

// A file that can't be read whole lists no one, so a sync from it changes
// nothing. The error names the file, and for a bad row its line.
func TestFileThatCantBeReadListsNoOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		contents string // empty for no file at all
		want     []string
	}{
		{name: "missing file", want: []string{"people.csv"}},
		{name: "empty file", contents: "\n", want: []string{"people.csv", "email,name,manager_email"}},
		{
			name:     "header missing a column",
			contents: "email,name\nada@example.com,Ada\n",
			want:     []string{"people.csv", `"manager_email"`},
		},
		{
			name:     "row with the wrong number of fields",
			contents: "email,name,manager_email\nada@example.com,Ada,grace@example.com\ngrace@example.com,Grace\n",
			want:     []string{"people.csv", "line 3"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "people.csv")
			if tc.contents != "" {
				path = writeDirectoryFile(t, tc.contents)
			}

			people, err := directory.NewFile(path).People(t.Context())
			if err == nil {
				t.Fatalf("People() = %+v, want an error", people)
			}
			if people != nil {
				t.Errorf("People() listed %+v along with the error", people)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q doesn't contain %s", err, w)
				}
			}
		})
	}
}

// An empty name gives no Name and an empty manager_email no Manager; blank
// lines are skipped.
func TestFileLeavesEmptyFieldsEmptyAndSkipsBlankLines(t *testing.T) {
	t.Parallel()
	path := writeDirectoryFile(t, "email,name,manager_email\n\n"+
		"ada@example.com,,grace@example.com\n\n"+
		"grace@example.com,Grace,\n\n")

	people, err := directory.NewFile(path).People(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.DirectoryPerson{
		{Email: "ada@example.com", ManagerEmail: "grace@example.com"},
		{Email: "grace@example.com", Name: "Grace"},
	}
	if !reflect.DeepEqual(people, want) {
		t.Fatalf("People() = %+v, want %+v", people, want)
	}
}
