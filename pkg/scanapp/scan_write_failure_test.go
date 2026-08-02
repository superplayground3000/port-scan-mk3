package scanapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// errInjectedWriteFailure is the disk-failure stand-in the tests below inject
// into the scan result writer. It is a distinct sentinel so assertions can
// prove the run's error really carries the write failure through (errors.Is),
// rather than matching on message text.
var errInjectedWriteFailure = errors.New("injected disk write failure")

// nthWriteFailingRecordWriter delegates to inner except on the failOn-th Write
// (1-based), which fails without writing anything. Run's result loop is the
// only caller and it is single-goroutine, so no locking is needed.
type nthWriteFailingRecordWriter struct {
	inner  RecordWriter
	failOn int
	writes int
}

func (w *nthWriteFailingRecordWriter) Write(record writer.Record) error {
	w.writes++
	if w.writes == w.failOn {
		return errInjectedWriteFailure
	}
	return w.inner.Write(record)
}

func (w *nthWriteFailingRecordWriter) WriteHeader() error { return w.inner.WriteHeader() }

// failingScanWriterOpener returns a batchOutputs opener that builds the real
// outputs and then wraps the full-results writer so the failOn-th record write
// fails. Only the scan_results writer is wrapped: the open-only writer filters
// records, so failing it would not correlate with rows in the scan CSV.
func failingScanWriterOpener(failOn int) batchOutputsOpenFunc {
	return func(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
		outputs, err := openBatchOutputs(scanPath, openPath, appendMode)
		if err != nil {
			return nil, err
		}
		outputs.scanWriter = &nthWriteFailingRecordWriter{inner: outputs.scanWriter, failOn: failOn}
		return outputs, nil
	}
}

// refusingDial makes every probe fail fast so the scan produces results as
// quickly as possible without touching any real network target (constitution V).
func refusingDial(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("connection refused")
}

// TestRun_WhenOutputWriteFails_DoesNotPersistResumeSnapshot is the issue #51
// guarantee: an output-write failure must not leave a resume snapshot behind.
// The dispatch cursor (NextIndex) advanced at enqueue time for rows that were
// never written, so any snapshot saved here would make the next -resume skip
// them silently. The run must instead fail loudly with the write error.
func TestRun_WhenOutputWriteFails_DoesNotPersistResumeSnapshot(t *testing.T) {
	t.Run("no snapshot is created at the save path", func(t *testing.T) {
		cfg, tmp, _ := newInterruptibleScanConfig(t)
		resumeOut := filepath.Join(tmp, "resume-out.json")

		err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
			DisableKeyboard:    true,
			Dial:               refusingDial,
			ResumeStatePath:    resumeOut,
			batchOutputsOpener: failingScanWriterOpener(3),
		})
		if err == nil {
			t.Fatal("expected the run to fail when writing scan output fails")
		}
		if !errors.Is(err, errInjectedWriteFailure) {
			t.Fatalf("run error must identify the write failure, got: %v", err)
		}
		if _, statErr := os.Stat(resumeOut); !os.IsNotExist(statErr) {
			t.Fatalf("expected NO resume snapshot at %s after an output-write failure, stat err: %v", resumeOut, statErr)
		}
	})

	t.Run("the input bucket snapshot is left untouched", func(t *testing.T) {
		cfg, _, bucketsFile := newInterruptibleScanConfig(t)
		before, err := os.ReadFile(bucketsFile)
		if err != nil {
			t.Fatalf("read bucket file: %v", err)
		}

		runErr := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
			DisableKeyboard:    true,
			Dial:               refusingDial,
			batchOutputsOpener: failingScanWriterOpener(3),
		})
		if !errors.Is(runErr, errInjectedWriteFailure) {
			t.Fatalf("run error must identify the write failure, got: %v", runErr)
		}

		after, err := os.ReadFile(bucketsFile)
		if err != nil {
			t.Fatalf("re-read bucket file: %v", err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("the bucket snapshot at %s was rewritten after an output-write failure;\nbefore: %s\nafter:  %s",
				bucketsFile, string(before), string(after))
		}
	})
}

// TestRun_WhenOutputWriteFails_ReportedScannedCountMatchesWrittenRows is the
// companion guarantee: results whose write failed or was skipped must not be
// counted as scanned, so the reported total never claims more rows than the
// output CSV actually holds. Rows are counted with encoding/csv (not by lines)
// because rich records may carry quoted embedded newlines.
func TestRun_WhenOutputWriteFails_ReportedScannedCountMatchesWrittenRows(t *testing.T) {
	cfg, tmp, _ := newInterruptibleScanConfig(t)
	cfg.LogLevel = "info"
	cfg.Format = "json"
	const failOn = 3

	var stderr bytes.Buffer
	err := Run(context.Background(), cfg, &bytes.Buffer{}, &stderr, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		ResumeStatePath:    filepath.Join(tmp, "resume-out.json"),
		batchOutputsOpener: failingScanWriterOpener(failOn),
	})
	if !errors.Is(err, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", err)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	_, rows := readCSVRows(t, scanPath)
	if len(rows) == 0 {
		t.Fatalf("expected the rows written before the failure to be on disk at %s", scanPath)
	}

	reported := completionTotalTasks(t, stderr.String())
	if reported > len(rows) {
		t.Fatalf("reported scanned count %d exceeds the %d data rows actually present in %s",
			reported, len(rows), scanPath)
	}
	if reported != len(rows) {
		t.Fatalf("reported scanned count %d should equal the %d data rows in %s", reported, len(rows), scanPath)
	}
}

// completionTotalTasks extracts fields.total_tasks from the scan_completion
// NDJSON event in log output.
func completionTotalTasks(t *testing.T, logOutput string) int {
	t.Helper()
	// The stream also carries plain-text progress lines, so parse line by line and
	// ignore anything that is not a log record.
	for _, line := range strings.Split(logOutput, "\n") {
		var entry struct {
			Msg    string `json:"msg"`
			Fields struct {
				TotalTasks *int `json:"total_tasks"`
			} `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Msg == "scan_completion" && entry.Fields.TotalTasks != nil {
			return *entry.Fields.TotalTasks
		}
	}
	t.Fatalf("no scan_completion event with total_tasks in log output:\n%s", logOutput)
	return 0
}

// TestRun_WhenCanceledWithoutWriteFailure_StillPersistsResumeSnapshot guards the
// 2.1.0 graceful-interrupt durability contract against the fix above
// over-reaching: a Ctrl+C style cancellation is NOT an output-write failure, so
// its snapshot must still be saved with an advanced-but-incomplete cursor.
func TestRun_WhenCanceledWithoutWriteFailure_StillPersistsResumeSnapshot(t *testing.T) {
	cfg, tmp, _ := newInterruptibleScanConfig(t)
	resumeOut := filepath.Join(tmp, "resume-out.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	dial := func(context.Context, string, string) (net.Conn, error) {
		once.Do(cancel)
		return nil, errors.New("connection refused")
	}

	err := Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            dial,
		ResumeStatePath: resumeOut,
		// The writers are wrapped but never fail, so the only difference from the
		// write-failure test is the origin of the terminating error.
		batchOutputsOpener: failingScanWriterOpener(0),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from the interrupted scan, got: %v", err)
	}

	snap, err := state.LoadSnapshot(resumeOut)
	if err != nil {
		t.Fatalf("expected a resume snapshot after a graceful cancel, got: %v", err)
	}
	if len(snap.Chunks) != 1 {
		t.Fatalf("expected 1 saved chunk, got %d", len(snap.Chunks))
	}
	if ch := snap.Chunks[0]; ch.NextIndex < 1 || ch.NextIndex >= ch.TotalCount {
		t.Fatalf("expected an advanced-but-incomplete cursor, got NextIndex=%d TotalCount=%d", ch.NextIndex, ch.TotalCount)
	}
}

// TestPersistResumeSnapshot_WhenRunErrIsOutputWriteFailure_DeclinesToSave pins
// the decision at its owner: the snapshot writer distinguishes an output-write
// error from every other run error and refuses to save for the former, while a
// graceful cancel still saves.
func TestPersistResumeSnapshot_WhenRunErrIsOutputWriteFailure_DeclinesToSave(t *testing.T) {
	newRuntimes := func() []*chunkRuntime {
		ch := &task.Chunk{
			CIDR:         "10.0.0.0/24",
			NextIndex:    4,
			ScannedCount: 2,
			TotalCount:   8,
			Status:       "scanning",
		}
		return []*chunkRuntime{{state: ch, tracker: newChunkStateTracker(ch)}}
	}

	t.Run("output-write error declines and reports", func(t *testing.T) {
		resumeFile := filepath.Join(t.TempDir(), "resume.json")
		var logs bytes.Buffer
		logger := newLogger("info", true, &logs)

		runErr := writeScanRecord(&alwaysFailingRecordWriter{}, &alwaysFailingRecordWriter{}, writer.Record{})
		err := persistResumeSnapshot(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger,
			newRuntimes(), state.PreScanPingState{}, nil, nil, runErr)
		if err == nil {
			t.Fatal("expected persistResumeSnapshot to report that it declined to save")
		}
		if !errors.Is(err, errInjectedWriteFailure) {
			t.Fatalf("the reported error must carry the underlying write failure, got: %v", err)
		}
		if _, statErr := os.Stat(resumeFile); !os.IsNotExist(statErr) {
			t.Fatalf("expected no snapshot file at %s, stat err: %v", resumeFile, statErr)
		}
		if logs.Len() == 0 {
			t.Fatal("expected a structured log entry explaining that resume state was not saved")
		}
	})

	t.Run("graceful cancel still saves", func(t *testing.T) {
		resumeFile := filepath.Join(t.TempDir(), "resume.json")
		logger := newLogger("error", false, &bytes.Buffer{})

		if err := persistResumeSnapshot(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger,
			newRuntimes(), state.PreScanPingState{}, nil, nil, context.Canceled); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		snap, err := state.LoadSnapshot(resumeFile)
		if err != nil {
			t.Fatalf("expected a saved snapshot, got: %v", err)
		}
		if len(snap.Chunks) != 1 || snap.Chunks[0].NextIndex != 4 {
			t.Fatalf("unexpected saved chunks: %+v", snap.Chunks)
		}
	})
}

// alwaysFailingRecordWriter fails every Write; used to produce a genuine
// writeScanRecord error rather than hand-constructing the wrapped sentinel.
type alwaysFailingRecordWriter struct{}

func (alwaysFailingRecordWriter) Write(writer.Record) error { return errInjectedWriteFailure }
func (alwaysFailingRecordWriter) WriteHeader() error        { return nil }
