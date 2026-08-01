package scanapp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// batchOutputs holds file handles and writers for scan result output.
// The writer fields use the RecordWriter interface to decouple from concrete types.
//
// Scan and open-only results are written DIRECTLY to their final paths (no
// intermediate ".tmp"): rows already written survive a graceful Ctrl+C
// (design §3.6) and a -resume run reopens the same files in append mode
// (design §3.7). Finalize only closes the handles.
type batchOutputs struct {
	scanFile       *os.File
	openOnlyFile   *os.File
	scanWriter     RecordWriter
	openOnlyWriter RecordWriter
}

type unreachableOutput struct {
	file      *os.File
	writer    *writer.UnreachableWriter
	finalPath string
}

// openResultCSV opens path for writing scan results. It always ensures the
// parent directory exists first (A1): the recorded output directory of a prior
// run may have been removed before -resume, and both the fresh-create and the
// append-reopen paths must recreate it rather than fail with ENOENT.
//
// When appendMode is false it truncates/creates the file fresh (header needed).
//
// When appendMode is true (a -resume run) it reopens the file for appending:
//   - A missing or empty file (e.g. the prior output was deleted) reports
//     needsHeader so the caller writes a fresh header.
//   - A non-empty file must carry the canonical header as its first line;
//     otherwise the file was written by a different or older schema and appending
//     would corrupt it, so it fails loudly (A2).
//
// It does NOT count or truncate data rows. Row-level crash reconciliation was
// intentionally dropped: it required CSV-record-aware parsing (result rows can
// carry rich fields with embedded newlines that encoding/csv quotes, so a
// line-based cut can split a record) and a write-durability invariant the result
// loop does not provide. Graceful Ctrl+C is exact without it (the file ends at a
// complete record and the snapshot cursor matches); the residual hard-crash edge
// is documented in the release notes rather than half-handled here.
func openResultCSV(path string, appendMode bool) (file *os.File, needsHeader bool, err error) {
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
		return nil, false, mkErr
	}
	if !appendMode {
		f, createErr := os.Create(path)
		if createErr != nil {
			return nil, false, createErr
		}
		return f, true, nil
	}
	// O_RDWR (not O_APPEND): we read the first line to validate the header, then
	// seek to end so subsequent writes append. A single goroutine owns the writer,
	// so plain end-seeked writes are equivalent to O_APPEND here.
	f, openErr := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if openErr != nil {
		return nil, false, openErr
	}
	needsHeader, err = validateAppendHeader(f, writer.CanonicalHeader())
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close result file: %v\n", closeErr)
		}
		return nil, false, err
	}
	if _, seekErr := f.Seek(0, io.SeekEnd); seekErr != nil {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close result file: %v\n", closeErr)
		}
		return nil, false, seekErr
	}
	return f, needsHeader, nil
}

// validateAppendHeader checks that an existing result file about to be appended
// to carries the canonical header (A2). It returns needsHeader=true only when the
// file is empty (a fresh header must be written). For a non-empty file it reads
// only the first line (the header is always a single physical line, so this is
// safe regardless of embedded newlines in later data rows) and requires it to
// equal expectHeader; a mismatch fails loudly rather than appending under a
// foreign/older schema. It does not read or modify the data rows.
func validateAppendHeader(f *os.File, expectHeader string) (needsHeader bool, err error) {
	info, statErr := f.Stat()
	if statErr != nil {
		return false, statErr
	}
	if info.Size() == 0 {
		return true, nil
	}
	if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
		return false, seekErr
	}
	firstLine, readErr := bufio.NewReader(f).ReadString('\n')
	if readErr != nil && readErr != io.EOF {
		return false, readErr
	}
	headerLine := strings.TrimRight(firstLine, "\r\n")
	if headerLine != expectHeader {
		return false, fmt.Errorf(
			"cannot append to existing result file %s: its header %q does not match the current output schema %q; the file was written by a different or older build (or hand-edited). Move it aside and resume into a fresh output file",
			f.Name(), headerLine, expectHeader)
	}
	return false, nil
}

// scanWriterFor returns a CSVWriter for file: a fresh writer with a header
// written when needsHeader, or an appending writer (header assumed present)
// otherwise.
func scanWriterFor(file *os.File, needsHeader bool) (*writer.CSVWriter, error) {
	if !needsHeader {
		return writer.NewCSVWriterAppending(file), nil
	}
	w := writer.NewCSVWriter(file)
	if err := w.WriteHeader(); err != nil {
		return nil, err
	}
	return w, nil
}

// openBatchOutputs opens the scan_results and opened_results writers. In append
// mode (a -resume run) each file's header is validated (A2) and appended to; a
// graceful Ctrl+C leaves both files ending at a complete record with the snapshot
// cursor matching, so the append continues exactly. appendMode false creates them
// fresh with a header.
func openBatchOutputs(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
	scanFile, scanNeedsHeader, err := openResultCSV(scanPath, appendMode)
	if err != nil {
		return nil, err
	}

	scanWriter, err := scanWriterFor(scanFile, scanNeedsHeader)
	if err != nil {
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	openOnlyFile, openNeedsHeader, err := openResultCSV(openPath, appendMode)
	if err != nil {
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	openInner, err := scanWriterFor(openOnlyFile, openNeedsHeader)
	if err != nil {
		if closeErr := openOnlyFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close open-only file: %v\n", closeErr)
		}
		if closeErr := scanFile.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to close scan file: %v\n", closeErr)
		}
		return nil, err
	}

	return &batchOutputs{
		scanFile:       scanFile,
		openOnlyFile:   openOnlyFile,
		scanWriter:     scanWriter,
		openOnlyWriter: writer.NewOpenOnlyWriter(openInner),
	}, nil
}

func openUnreachableOutput(finalPath string) (*unreachableOutput, error) {
	tmpPath := finalPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}

	unreachableWriter := writer.NewUnreachableWriter(file)
	if err := unreachableWriter.WriteHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &unreachableOutput{
		file:      file,
		writer:    unreachableWriter,
		finalPath: finalPath,
	}, nil
}

// Finalize closes the output file handles. Because scan/open results are
// written directly to their final paths, there is no promotion step: whatever
// rows were written are already durable at the final path (design §3.6).
func (b *batchOutputs) Finalize() error {
	if b == nil {
		return nil
	}
	var firstErr error
	if b.openOnlyFile != nil {
		if err := b.openOnlyFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if b.scanFile != nil {
		if err := b.scanFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (u *unreachableOutput) Finalize(success bool) error {
	if u == nil {
		return nil
	}
	if u.file != nil {
		if err := u.file.Close(); err != nil {
			return err
		}
	}
	if success {
		if err := os.Rename(u.finalPath+".tmp", u.finalPath); err != nil {
			return err
		}
	}
	return nil
}

func openBatchOutputsAfterUnreachable(paths batchOutputPaths) (*batchOutputs, error) {
	output, err := openUnreachableOutput(paths.unreachablePath)
	if err != nil {
		return nil, err
	}
	if err := output.Finalize(true); err != nil {
		return nil, err
	}
	return openBatchOutputs(paths.scanPath, paths.openPath, false)
}
