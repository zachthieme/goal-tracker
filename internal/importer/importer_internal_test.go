package importer

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// parseXLSX closes a workbook that excelize hands back with an error, as it
// does on four paths in v2.11.0, so the temp files the workbook unpacked are
// removed then, not only with the per-parse temp directory. The stand-in open
// here unpacks a real workbook's worksheet to a temp file in a directory the
// test owns, out of reach of that clean-up, and then fails: only the
// workbook's Close removes the file.
//
//nolint:paralleltest // replaces the package's openXLSXWorkbook
func TestParseXLSXClosesAWorkbookThatFailedToOpen(t *testing.T) {
	wb := excelize.NewFile()
	if err := wb.SetCellValue("Sheet1", "A1", "Title"); err != nil {
		t.Fatal(err)
	}
	buf, err := wb.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	_ = wb.Close()

	unpacked := t.TempDir()
	errOpen := errors.New("the workbook's styles are malformed")
	realOpen := openXLSXWorkbook
	t.Cleanup(func() { openXLSXWorkbook = realOpen })
	openXLSXWorkbook = func(r io.Reader, opts ...excelize.Options) (*excelize.File, error) {
		o := opts[0]
		o.TmpDir = unpacked
		o.UnzipXMLSizeLimit = 1 // any worksheet unpacks to a temp file
		f, err := realOpen(r, o)
		if err != nil {
			t.Fatalf("opening the workbook: %v", err)
		}
		if entries, _ := os.ReadDir(unpacked); len(entries) == 0 {
			t.Fatal("the workbook unpacked no temp file, so its Close would remove nothing")
		}
		return f, errOpen
	}

	_, err = parseXLSX(buf.Bytes())
	if !errors.Is(err, domain.ErrValidation) || !strings.Contains(err.Error(), errOpen.Error()) {
		t.Errorf("parseXLSX: %v, want a validation error saying %q", err, errOpen)
	}
	entries, err := os.ReadDir(unpacked)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("parseXLSX left %s behind: the workbook that failed to open wasn't closed", e.Name())
	}
}
