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
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

type waitingFailingRecordWriter struct {
	inner RecordWriter
	ready <-chan struct{}
}

func (w *waitingFailingRecordWriter) Write(writer.Record) error {
	<-w.ready
	return errInjectedWriteFailure
}

func (w *waitingFailingRecordWriter) WriteHeader() error { return w.inner.WriteHeader() }

type releaseThenFailRecordWriter struct {
	inner   RecordWriter
	release chan struct{}
	writes  int
}

func (w *releaseThenFailRecordWriter) Write(record writer.Record) error {
	w.writes++
	if w.writes == 2 {
		return errInjectedWriteFailure
	}
	err := w.inner.Write(record)
	if w.writes == 1 {
		close(w.release)
	}
	return err
}

func (w *releaseThenFailRecordWriter) WriteHeader() error { return w.inner.WriteHeader() }

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

func newTwoChunkWriteFailureConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")

	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\n"+
			"fab1,10.0.0.1,10.0.0.1/32,first\n"+
			"fab2,10.0.0.2,10.0.0.2/32,second\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           filepath.Join(tmp, "out.csv"),
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          2,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	return cfg, tmp
}

func TestRun_WhenOutputWriteFails_RewindsEveryAffectedChunk(t *testing.T) {
	cfg, tmp := newTwoChunkWriteFailureConfig(t)
	resumeOut := filepath.Join(tmp, "resume-out.json")
	secondChunkDialed := make(chan struct{})
	var signalSecondChunk sync.Once

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeOut,
		Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			if strings.HasPrefix(address, "10.0.0.2:") {
				signalSecondChunk.Do(func() { close(secondChunkDialed) })
			}
			return nil, errors.New("connection refused")
		},
		batchOutputsOpener: func(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
			outputs, err := openBatchOutputs(scanPath, openPath, appendMode)
			if err != nil {
				return nil, err
			}
			outputs.scanWriter = &waitingFailingRecordWriter{
				inner: outputs.scanWriter,
				ready: secondChunkDialed,
			}
			return outputs, nil
		},
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", runErr)
	}

	snapshot, err := state.LoadSnapshot(resumeOut)
	if err != nil {
		t.Fatalf("load corrected resume snapshot: %v", err)
	}
	if len(snapshot.Chunks) != 2 {
		t.Fatalf("expected two saved chunks, got %d", len(snapshot.Chunks))
	}
	for _, chunk := range snapshot.Chunks {
		if chunk.NextIndex != 0 {
			t.Errorf("chunk %s cursor=%d, want 0 for its first unwritten task", chunk.CIDR, chunk.NextIndex)
		}
		if chunk.ScannedCount != 0 || chunk.Status != "pending" {
			t.Errorf("chunk %s state=(scanned %d, status %s), want (0, pending)",
				chunk.CIDR, chunk.ScannedCount, chunk.Status)
		}
	}
}

func TestRun_WhenOutputWriteFails_KeepsCursorForFullyPersistedChunk(t *testing.T) {
	cfg, tmp := newTwoChunkWriteFailureConfig(t)
	resumeOut := filepath.Join(tmp, "resume-out.json")
	releaseSecondDial := make(chan struct{})

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeOut,
		Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			if strings.HasPrefix(address, "10.0.0.2:") {
				<-releaseSecondDial
			}
			return nil, errors.New("connection refused")
		},
		batchOutputsOpener: func(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
			outputs, err := openBatchOutputs(scanPath, openPath, appendMode)
			if err != nil {
				return nil, err
			}
			outputs.scanWriter = &releaseThenFailRecordWriter{
				inner:   outputs.scanWriter,
				release: releaseSecondDial,
			}
			return outputs, nil
		},
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", runErr)
	}

	snapshot, err := state.LoadSnapshot(resumeOut)
	if err != nil {
		t.Fatalf("load corrected resume snapshot: %v", err)
	}
	if len(snapshot.Chunks) != 2 {
		t.Fatalf("expected two saved chunks, got %d", len(snapshot.Chunks))
	}
	first, second := snapshot.Chunks[0], snapshot.Chunks[1]
	if first.NextIndex != 1 || first.ScannedCount != 1 || first.Status != "completed" {
		t.Fatalf("fully persisted chunk state=%+v, want completed cursor 1", first)
	}
	if second.NextIndex != 0 || second.ScannedCount != 0 || second.Status != "pending" {
		t.Fatalf("unwritten chunk state=%+v, want pending cursor 0", second)
	}
}

func TestRun_WhenOutputWriteFails_AlignsScannedCountWithRewoundCursor(t *testing.T) {
	cfg, tmp, _ := newInterruptibleScanConfig(t)
	inputSnapshot, err := state.LoadSnapshot(cfg.Resume)
	if err != nil {
		t.Fatalf("load input snapshot: %v", err)
	}
	inputSnapshot.Chunks[0].NextIndex = 1
	inputSnapshot.Chunks[0].ScannedCount = 0
	inputSnapshot.Chunks[0].Status = "scanning"
	if err := state.SaveSnapshot(cfg.Resume, inputSnapshot); err != nil {
		t.Fatalf("save input snapshot: %v", err)
	}
	resumeOut := filepath.Join(tmp, "resume-out.json")

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		ResumeStatePath:    resumeOut,
		batchOutputsOpener: failingScanWriterOpener(3),
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", runErr)
	}

	corrected, err := state.LoadSnapshot(resumeOut)
	if err != nil {
		t.Fatalf("load corrected resume snapshot: %v", err)
	}
	chunk := corrected.Chunks[0]
	if chunk.NextIndex != 3 || chunk.ScannedCount != 3 {
		t.Fatalf("corrected progress=(cursor %d, scanned %d), want (3, 3)",
			chunk.NextIndex, chunk.ScannedCount)
	}
}

func TestRun_WhenResumedAfterOutputWriteFailure_CoversEveryTaskAndDuplicatesPersistedRows(t *testing.T) {
	cfg, _, _ := newInterruptibleScanConfig(t)
	cfg.Workers = 2
	releaseFirstDial := make(chan struct{})

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			if strings.HasPrefix(address, "10.9.0.0:") {
				<-releaseFirstDial
			}
			return nil, errors.New("connection refused")
		},
		batchOutputsOpener: func(scanPath, openPath string, appendMode bool) (*batchOutputs, error) {
			outputs, err := openBatchOutputs(scanPath, openPath, appendMode)
			if err != nil {
				return nil, err
			}
			outputs.scanWriter = &releaseThenFailRecordWriter{
				inner:   outputs.scanWriter,
				release: releaseFirstDial,
			}
			return outputs, nil
		},
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", runErr)
	}

	failedSnapshot, err := state.LoadSnapshot(cfg.Resume)
	if err != nil {
		t.Fatalf("load corrected resume snapshot: %v", err)
	}
	if failedSnapshot.Output == nil {
		t.Fatal("corrected resume snapshot must record the output paths")
	}
	if len(failedSnapshot.Chunks) != 1 {
		t.Fatalf("expected one saved chunk, got %d", len(failedSnapshot.Chunks))
	}
	totalTasks := failedSnapshot.Chunks[0].TotalCount
	if failedSnapshot.Chunks[0].NextIndex != 0 {
		t.Fatalf("saved cursor=%d, want 0 after the first task finished after the failure",
			failedSnapshot.Chunks[0].NextIndex)
	}

	_, partialRows := readCSVRows(t, failedSnapshot.Output.ScanPath)
	if len(partialRows) != 1 {
		t.Fatalf("failed run wrote %d rows, want 1 persisted row", len(partialRows))
	}

	if err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            refusingDial,
	}); err != nil {
		t.Fatalf("resume corrected snapshot: %v", err)
	}

	header, rows := readCSVRows(t, failedSnapshot.Output.ScanPath)
	columnIndex := make(map[string]int, len(header))
	for idx, name := range header {
		columnIndex[name] = idx
	}
	identityCounts := make(map[string]int, totalTasks)
	for _, row := range rows {
		identity := row[columnIndex["ip"]] + ":" + row[columnIndex["port"]]
		identityCounts[identity]++
	}
	if len(identityCounts) != totalTasks {
		t.Fatalf("resume covered %d unique tasks, want %d", len(identityCounts), totalTasks)
	}
	if len(rows) != totalTasks+1 {
		t.Fatalf("resume wrote %d total rows, want %d tasks plus one duplicate", len(rows), totalTasks)
	}
	duplicates := 0
	for _, count := range identityCounts {
		if count == 2 {
			duplicates++
		}
	}
	if duplicates != 1 {
		t.Fatalf("resume produced %d duplicated task rows, want 1", duplicates)
	}
}

// TestRun_WhenOutputWriteFails_PersistsRewoundResumeSnapshot verifies both
// supported save locations. The corrected cursor preserves safe progress.
func TestRun_WhenOutputWriteFails_PersistsRewoundResumeSnapshot(t *testing.T) {
	t.Run("explicit save path", func(t *testing.T) {
		cfg, tmp, _ := newInterruptibleScanConfig(t)
		resumeOut := filepath.Join(tmp, "resume-out.json")

		err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
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
		snapshot, loadErr := state.LoadSnapshot(resumeOut)
		if loadErr != nil {
			t.Fatalf("load corrected snapshot: %v", loadErr)
		}
		if len(snapshot.Chunks) != 1 {
			t.Fatalf("expected one saved chunk, got %d", len(snapshot.Chunks))
		}
		chunk := snapshot.Chunks[0]
		if chunk.NextIndex != 2 || chunk.ScannedCount != 2 {
			t.Fatalf("saved progress=(cursor %d, scanned %d), want (2, 2)", chunk.NextIndex, chunk.ScannedCount)
		}
		if snapshot.Output == nil {
			t.Fatal("corrected snapshot must record output paths")
		}
	})

	t.Run("input bucket path", func(t *testing.T) {
		cfg, _, bucketsFile := newInterruptibleScanConfig(t)
		runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
			DisableKeyboard:    true,
			Dial:               refusingDial,
			batchOutputsOpener: failingScanWriterOpener(3),
		})
		if !errors.Is(runErr, errInjectedWriteFailure) {
			t.Fatalf("run error must identify the write failure, got: %v", runErr)
		}
		snapshot, loadErr := state.LoadSnapshot(bucketsFile)
		if loadErr != nil {
			t.Fatalf("load corrected input bucket: %v", loadErr)
		}
		if len(snapshot.Chunks) != 1 || snapshot.Chunks[0].NextIndex != 2 {
			t.Fatalf("unexpected corrected chunks: %+v", snapshot.Chunks)
		}
		if snapshot.Output == nil {
			t.Fatal("corrected input bucket must record output paths")
		}
	})
}

func TestRun_WhenSnapshotSaveFails_ReturnsSaveErrorBeforeRuntimeError(t *testing.T) {
	cfg, tmp, _ := newInterruptibleScanConfig(t)
	missingDir := filepath.Join(tmp, "missing")
	resumeOut := filepath.Join(missingDir, "resume.json")

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		ResumeStatePath:    resumeOut,
		batchOutputsOpener: failingScanWriterOpener(1),
	})
	if runErr == nil {
		t.Fatal("expected the snapshot save to fail")
	}
	if errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("snapshot save error must replace the output error: %v", runErr)
	}
	if !strings.Contains(runErr.Error(), "create temp snapshot file") {
		t.Fatalf("expected snapshot save error, got: %v", runErr)
	}
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
	err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &stderr, RunOptions{
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

func TestRun_WhenOutputWriteFails_LogsCorrectedResumeSnapshot(t *testing.T) {
	cfg, tmp, _ := newInterruptibleScanConfig(t)
	cfg.LogLevel = "info"
	cfg.Format = "json"
	resumeOut := filepath.Join(tmp, "resume-out.json")
	var stderr bytes.Buffer

	runErr := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &stderr, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		ResumeStatePath:    resumeOut,
		batchOutputsOpener: failingScanWriterOpener(3),
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run error must identify the write failure, got: %v", runErr)
	}
	logOutput := stderr.String()
	for _, line := range strings.Split(logOutput, "\n") {
		var entry struct {
			Msg    string `json:"msg"`
			Fields struct {
				Reason     string `json:"reason"`
				ResumePath string `json:"resume_path"`
			} `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Msg != "resume_state_rewound" {
			continue
		}
		if entry.Fields.Reason != "scan_output_write_failed" {
			t.Fatalf("rewind reason = %q, want scan_output_write_failed", entry.Fields.Reason)
		}
		if entry.Fields.ResumePath != resumeOut {
			t.Fatalf("rewind resume path = %q, want %q", entry.Fields.ResumePath, resumeOut)
		}
		return
	}
	t.Fatalf("no resume_state_rewound event in log output:\n%s", logOutput)
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

	err := Run(ctx, testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
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
