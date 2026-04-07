package spreadsheet

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotCSV is returned when the file is not a valid csv file.
var ErrNotCSV = errors.New("file is not a valid csv file")

// Reader reads .csv files, returning rows as [][]string.
type Reader struct {
	path string
}

// NewReader returns a Reader for the given CSV file path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// OpenSheet opens the CSV file and returns all rows.
// The sheetName parameter is ignored for CSV files.
func (r *Reader) OpenSheet(_ string) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(r.path))
	if ext != ".csv" {
		return nil, ErrNotCSV
	}
	return readCSV(r.path)
}

// readCSV reads a CSV file with a header row and returns all records.
func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)

	return r.ReadAll()
}
