package spreadsheet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

var (
	// ErrNotXLSX is returned when the file is not a valid xlsx workbook.
	ErrNotXLSX = errors.New("file is not a valid xlsx workbook")
	// ErrNotCSV is returned when the file is not a valid csv file.
	ErrNotCSV = errors.New("file is not a valid csv file")
	// ErrExtensionMismatch is returned when the file extension does not match the detected format.
	ErrExtensionMismatch = errors.New("file extension does not match detected format")
)

// Reader reads both .xlsx and .csv files, returning worksheet rows as [][]string.
// Format is auto-detected by content; extension must match detected format.
type Reader struct {
	path string
}

// NewReader returns a Reader for the given file path.
// The file must have a .xlsx or .csv extension.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// IsExtensionMismatch reports whether err is an extension mismatch error.
func IsExtensionMismatch(err error) bool {
	return errors.Is(err, ErrExtensionMismatch)
}

// detectFormat reads the first 2 bytes of the file to determine its format.
// Returns "xlsx" or "csv", or an error.
func detectFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, 2)
	if _, err := f.Read(header); err != nil {
		return "", err
	}

	if header[0] == 0x50 && header[1] == 0x4B {
		return "xlsx", nil
	}
	return "csv", nil
}

// OpenSheet opens the file and returns all rows.
// For .xlsx: sheetName selects the worksheet.
// For .csv: sheetName is ignored.
func (r *Reader) OpenSheet(sheetName string) ([][]string, error) {
	detected, err := detectFormat(r.path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(r.path))
	isXLSX := ext == ".xlsx"
	isCSV := ext == ".csv"

	if detected == "xlsx" && !isXLSX {
		return nil, ErrExtensionMismatch
	}
	if detected == "csv" && !isCSV {
		return nil, ErrExtensionMismatch
	}

	if detected == "csv" {
		return readCSV(r.path)
	}

	// xlsx
	f, err := excelize.OpenFile(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.GetRows(sheetName)
}