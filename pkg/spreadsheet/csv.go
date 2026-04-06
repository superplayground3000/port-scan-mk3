package spreadsheet

import (
	"encoding/csv"
	"os"
)

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
