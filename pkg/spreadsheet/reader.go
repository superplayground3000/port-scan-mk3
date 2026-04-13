// Package spreadsheet provides a common interface for reading spreadsheet data.
// Currently it supports CSV files; the sheetName parameter on OpenSheet allows
// future extension to Excel (.xlsx) or Google Sheets backends without changing
// call sites.
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

// ErrNotCSV is returned by OpenSheet when the file extension is not .csv.
var ErrNotCSV = errors.New("file is not a valid csv file")

// Reader opens a spreadsheet file and returns rows. For CSV files, the sheetName
// parameter is ignored; for future backends (e.g., Excel), it selects a sheet.
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
//	sheetName: Target sheet name. For CSV files this parameter is ignored
//	           and the entire file is returned.
//
// # Returns
//
//	[][]string where each slice is one row (header at index 0).
//	Error if the file cannot be opened, is not a CSV, or has parse errors.
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
