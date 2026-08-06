// Package spreadsheet provides a common interface to read spreadsheet data.
// It supports CSV files today. The sheetName parameter of OpenSheet makes a
// future extension possible. A future backend for Excel (.xlsx) or Google Sheets
// needs no change at the call sites.
//
// # Example
//
//	reader := spreadsheet.NewReader("data.csv")
//	rows, err := reader.OpenSheet("")  // sheetName ignored for CSV
package spreadsheet

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotCSV is the error that OpenSheet returns when the file extension is
// not .csv.
var ErrNotCSV = errors.New("file is not a valid csv file")

// Reader opens a spreadsheet file and returns rows. For a CSV file, OpenSheet
// ignores the sheetName parameter. For a future backend, for example Excel, the
// parameter selects a sheet.
type Reader struct {
	path string
}

// NewReader creates a Reader for the given file path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// OpenSheet opens the underlying file and returns all rows as string slices.
//
// # Parameters
//
//	sheetName: Target sheet name. For a CSV file, OpenSheet ignores this
//	           parameter and returns the whole file.
//
// # Returns
//
//	[][]string where each slice is one row (header at index 0).
//	An error when OpenSheet cannot open the file, when the file is not a CSV,
//	or when the file has parse errors.
//
// # Example
//
//	reader := spreadsheet.NewReader("ports.csv")
//	rows, err := reader.OpenSheet("")
func (r *Reader) OpenSheet(_ string) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(r.path))
	if ext != ".csv" {
		return nil, ErrNotCSV
	}
	return readCSV(r.path)
}

// readCSV opens and reads a CSV file, returning all rows.
func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)

	return r.ReadAll()
}
