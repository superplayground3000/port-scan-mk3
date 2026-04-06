package xlsx

import (
	"errors"
	"os"

	"github.com/xuri/excelize/v2"
)

var (
	// ErrNotXLSX is returned when the file is not a valid xlsx file.
	ErrNotXLSX = errors.New("file is not a valid xlsx workbook")
)

// Reader opens xlsx files and reads worksheet data.
type Reader struct {
	path string
}

// NewReader returns a Reader for the given xlsx file path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// detectFormat checks the file magic bytes to determine if it is a valid xlsx file.
// xlsx files start with the PK (ZIP) magic bytes (0x50, 0x4B).
func detectFormat(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read the first 2 bytes to check for PK (ZIP) magic.
	header := make([]byte, 2)
	if _, err := f.Read(header); err != nil {
		return err
	}

	if header[0] != 0x50 || header[1] != 0x4B {
		return ErrNotXLSX
	}
	return nil
}

// OpenSheet opens the named worksheet and returns its rows as [][]string.
// Each row is a slice of cell values in column order.
func (r *Reader) OpenSheet(name string) ([][]string, error) {
	if err := detectFormat(r.path); err != nil {
		return nil, err
	}

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
