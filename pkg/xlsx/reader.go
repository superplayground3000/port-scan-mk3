package xlsx

import "github.com/xuri/excelize/v2"

// Reader opens xlsx files and reads worksheet data.
type Reader struct {
	path string
}

// NewReader returns a Reader for the given xlsx file path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// OpenSheet opens the named worksheet and returns its rows as [][]string.
// Each row is a slice of cell values in column order.
func (r *Reader) OpenSheet(name string) ([][]string, error) {
	f, err := excelize.OpenFile(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := f.GetRows(name)
	if err != nil {
		return nil, err
	}
	return rows, nil
}