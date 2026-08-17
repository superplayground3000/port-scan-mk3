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

// batchOutputs holds the file handles and writers for scan result output.
// Both result files use their final paths. The output committer flushes them as
// one batch. A resume run opens the same files in append mode.
type batchOutputs struct {
	scanFile       *os.File
	openOnlyFile   *os.File
	scanPath       string
	openOnlyPath   string
	scanWriter     recordWriter
	openOnlyWriter recordWriter
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
// graceful Ctrl+C flushes the final batch before snapshot persistence.
// appendMode false creates both files with a header.
func openBatchOutputs(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
	return openBatchOutputsWithMode(scanPath, openPath, appendMode, false)
}

func openBufferedBatchOutputs(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
	return openBatchOutputsWithMode(scanPath, openPath, appendMode, true)
}

func openBatchOutputsWithMode(scanPath, openPath string, appendMode, buffered bool) (*batchOutputs, error) {
	scanFile, scanNeedsHeader, err := openResultCSV(scanPath, appendMode)
	if err != nil {
		return nil, err
	}

	scanWriter, err := scanWriterForMode(scanFile, scanNeedsHeader, buffered)
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

	openInner, err := scanWriterForMode(openOnlyFile, openNeedsHeader, buffered)
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
		scanPath:       scanPath,
		openOnlyPath:   openPath,
		scanWriter:     scanWriter,
		openOnlyWriter: writer.NewOpenOnlyWriter(openInner),
	}, nil
}

func scanWriterForMode(file *os.File, needsHeader, buffered bool) (*writer.CSVWriter, error) {
	if !buffered {
		return scanWriterFor(file, needsHeader)
	}
	var result *writer.CSVWriter
	if needsHeader {
		result = writer.NewBufferedCSVWriter(file)
	} else {
		result = writer.NewBufferedCSVWriterAppending(file)
	}
	if err := result.WriteHeader(); err != nil {
		return nil, err
	}
	if err := result.Flush(); err != nil {
		return nil, err
	}
	return result, nil
}

type recordFlusher interface {
	Flush() error
}

type outputFailureRecordWriter struct {
	inner        recordWriter
	failOnResult uint64
	written      uint64
}

func (w *outputFailureRecordWriter) Write(record writer.Record) error {
	w.written++
	if w.written == w.failOnResult {
		return ErrInjectedOutputFailure
	}
	return w.inner.Write(record)
}

func (w *outputFailureRecordWriter) WriteHeader() error {
	return w.inner.WriteHeader()
}

func (w *outputFailureRecordWriter) Flush() error {
	if flusher, ok := w.inner.(recordFlusher); ok {
		return flusher.Flush()
	}
	return nil
}

func outputFailureBatchOpener(failOnResult uint64) batchOutputsOpenFunc {
	return func(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
		outputs, err := openBufferedBatchOutputs(scanPath, openPath, appendMode)
		if err != nil {
			return nil, err
		}
		outputs.scanWriter = &outputFailureRecordWriter{inner: outputs.scanWriter, failOnResult: failOnResult}
		return outputs, nil
	}
}

func (b *batchOutputs) write(record writer.Record) error {
	if err := b.scanWriter.Write(record); err != nil {
		return fmt.Errorf("%w: file %s stage write: %w", errScanOutputWrite, b.scanPath, err)
	}
	if err := b.openOnlyWriter.Write(record); err != nil {
		return fmt.Errorf("%w: file %s stage write: %w", errScanOutputWrite, b.openOnlyPath, err)
	}
	return nil
}

func (b *batchOutputs) flush() error {
	var firstErr error
	if flusher, ok := b.scanWriter.(recordFlusher); ok {
		if err := flusher.Flush(); err != nil {
			firstErr = fmt.Errorf("%w: file %s stage flush: %w", errScanOutputWrite, b.scanPath, err)
		}
	}
	if flusher, ok := b.openOnlyWriter.(recordFlusher); ok {
		if err := flusher.Flush(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%w: file %s stage flush: %w", errScanOutputWrite, b.openOnlyPath, err)
		}
	}
	return firstErr
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

// Finalize closes the output file handles. It does not flush a failed batch.
// The output committer flushes successful batches before Finalize runs.
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
