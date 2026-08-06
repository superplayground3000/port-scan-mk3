package preprocess

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// OutputPath returns the canonical output file path for a given base directory,
// fab name, and timestamp. The path has this layout:
//
//	<baseDir>/<fabName>/<YYYYMMDDTHHMMSSz>/input.csv
func OutputPath(baseDir, fabName string, ts time.Time) string {
	return filepath.Join(baseDir, fabName, ts.Format("20060102T150405Z"), "input.csv")
}

// CreateOutputWriter creates the output directory and returns a csv.Writer
// for the output file. The caller must call Flush and close the file.
func CreateOutputWriter(path string) (*csv.Writer, *os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating output directory %s: %w", dir, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("creating output file %s: %w", path, err)
	}
	return csv.NewWriter(f), f, nil
}

// WriteRichCSV writes a header and rows to a CSV writer in the canonical rich
// column order.
func WriteRichCSV(w *csv.Writer, rows [][]string) error {
	if err := w.Write(preprocesscfg.RichHeader()); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	for i, row := range rows {
		if err := w.Write(row); err != nil {
			return fmt.Errorf("writing row %d: %w", i+1, err)
		}
	}
	w.Flush()
	return w.Error()
}

// PrintSummary writes a human-readable filter summary to the given writer.
func PrintSummary(w io.Writer, total, kept, dropped int) error {
	if _, err := fmt.Fprintf(w, "Filter summary:\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Total input rows:  %d\n", total); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Rows kept:         %d\n", kept); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "  Rows dropped:      %d\n", dropped)
	return err
}
