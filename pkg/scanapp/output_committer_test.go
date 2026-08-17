package scanapp

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type flushCountingWriter struct {
	writes      int
	flushes     int
	flushAt     []int
	failWriteAt int
	flushErr    error
}

func (w *flushCountingWriter) Write(writer.Record) error {
	w.writes++
	if w.writes == w.failWriteAt {
		return errInjectedWriteFailure
	}
	return nil
}

func (w *flushCountingWriter) WriteHeader() error { return nil }

func (w *flushCountingWriter) Flush() error {
	w.flushes++
	w.flushAt = append(w.flushAt, w.writes)
	return w.flushErr
}

func TestOutputCommitterWriteFailureRewindsTheWholeBatchAndStopsLaterWrites(t *testing.T) {
	scanWriter := &flushCountingWriter{failWriteAt: 3}
	openWriter := &flushCountingWriter{}
	chunk := &task.Chunk{CIDR: "192.0.2.0/24", TotalCount: 4, NextIndex: 4, Status: "scanning"}
	runtimes := []*chunkRuntime{{state: chunk, tracker: newChunkStateTracker(chunk)}}
	var logs bytes.Buffer
	committer := newOutputCommitter(outputCommitterConfig{
		outputs: &batchOutputs{
			scanPath:       "scan.csv",
			openOnlyPath:   "opened.csv",
			scanWriter:     scanWriter,
			openOnlyWriter: openWriter,
		},
		flushInterval: 1000,
		runtimes:      runtimes,
		logger:        newLogger("info", true, &logs),
	})

	for index := 0; index < 2; index++ {
		if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: index, record: writer.Record{Status: "open"}}); err != nil {
			t.Fatalf("Accept(%d) error = %v", index, err)
		}
	}
	err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: 2, record: writer.Record{Status: "open"}})
	if !errors.Is(err, errInjectedWriteFailure) || !strings.Contains(err.Error(), "file scan.csv stage write") || !strings.Contains(err.Error(), "uncommitted result count 3") {
		t.Fatalf("write error = %v", err)
	}
	if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: 3, record: writer.Record{Status: "open"}}); err != nil {
		t.Fatalf("Accept() after failure error = %v", err)
	}
	if scanWriter.writes != 3 || openWriter.writes != 2 {
		t.Fatalf("writers accepted later output: scan=%d open=%d", scanWriter.writes, openWriter.writes)
	}
	if got := committer.Summary().written; got != 0 {
		t.Fatalf("committed results = %d, want 0", got)
	}
	if len(committer.pending) != 1 {
		t.Fatalf("pending bookkeeping entries = %d, want one per chunk", len(committer.pending))
	}
	if !runtimes[0].tracker.RewindUnwritten() {
		t.Fatal("batch did not mark the chunk for rewind")
	}
	if snapshot := runtimes[0].tracker.Snapshot(); snapshot.NextIndex != 0 || snapshot.ScannedCount != 0 {
		t.Fatalf("rewound snapshot = %+v, want cursor 0", snapshot)
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"msg":"output_batch_failed"`)) ||
		!bytes.Contains(logs.Bytes(), []byte(`"uncommitted_result_count":3`)) {
		t.Fatalf("failure telemetry is incomplete:\n%s", logs.String())
	}
}

func TestOutputCommitterFlushFailureRewindsOnlyTheCurrentBatch(t *testing.T) {
	scanWriter := &flushCountingWriter{}
	openWriter := &flushCountingWriter{}
	chunk := &task.Chunk{CIDR: "192.0.2.0/24", TotalCount: 4, NextIndex: 4, Status: "scanning"}
	runtimes := []*chunkRuntime{{state: chunk, tracker: newChunkStateTracker(chunk)}}
	committer := newOutputCommitter(outputCommitterConfig{
		outputs: &batchOutputs{
			scanPath:       "scan.csv",
			openOnlyPath:   "opened.csv",
			scanWriter:     scanWriter,
			openOnlyWriter: openWriter,
		},
		flushInterval: 2,
		runtimes:      runtimes,
		logger:        newLogger("error", false, &bytes.Buffer{}),
	})

	for index := 0; index < 2; index++ {
		if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: index, record: writer.Record{Status: "open"}}); err != nil {
			t.Fatalf("first batch result %d error = %v", index, err)
		}
	}
	openWriter.flushErr = errInjectedWriteFailure
	if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: 2, record: writer.Record{Status: "open"}}); err != nil {
		t.Fatalf("second batch first result error = %v", err)
	}
	err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: 3, record: writer.Record{Status: "open"}})
	if !errors.Is(err, errInjectedWriteFailure) || !strings.Contains(err.Error(), "file opened.csv stage flush") || !strings.Contains(err.Error(), "uncommitted result count 2") {
		t.Fatalf("flush error = %v", err)
	}
	if scanWriter.flushes != 2 || openWriter.flushes != 2 {
		t.Fatalf("both writers did not receive one flush attempt per batch: scan=%d open=%d", scanWriter.flushes, openWriter.flushes)
	}
	if got := committer.Summary().written; got != 2 {
		t.Fatalf("committed results = %d, want first batch size 2", got)
	}
	if !runtimes[0].tracker.RewindUnwritten() {
		t.Fatal("failed batch did not mark the chunk for rewind")
	}
	if snapshot := runtimes[0].tracker.Snapshot(); snapshot.NextIndex != 2 || snapshot.ScannedCount != 2 {
		t.Fatalf("rewound snapshot = %+v, want cursor 2", snapshot)
	}
}

func TestOutputCommitterFinalFlushFailureRewindsToTheEarliestPendingTask(t *testing.T) {
	scanWriter := &flushCountingWriter{flushErr: errInjectedWriteFailure}
	chunk := &task.Chunk{CIDR: "192.0.2.0/24", TotalCount: 3, NextIndex: 3, Status: "scanning"}
	runtime := &chunkRuntime{state: chunk, tracker: newChunkStateTracker(chunk)}
	committer := newOutputCommitter(outputCommitterConfig{
		outputs: &batchOutputs{
			scanPath:       "scan.csv",
			openOnlyPath:   "opened.csv",
			scanWriter:     scanWriter,
			openOnlyWriter: &flushCountingWriter{},
		},
		flushInterval: 1000,
		runtimes:      []*chunkRuntime{runtime},
		logger:        newLogger("error", false, &bytes.Buffer{}),
	})
	for _, taskIndex := range []int{2, 1} {
		if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: taskIndex, record: writer.Record{Status: scanner.StatusLocalError}}); err != nil {
			t.Fatalf("Accept(%d) error = %v", taskIndex, err)
		}
	}
	if err := committer.Finish(); !errors.Is(err, errInjectedWriteFailure) {
		t.Fatalf("Finish() error = %v", err)
	}
	if !runtime.tracker.RewindUnwritten() {
		t.Fatal("failed final batch did not request rewind")
	}
	if snapshot := runtime.tracker.Snapshot(); snapshot.NextIndex != 1 {
		t.Fatalf("rewound cursor = %d, want 1", snapshot.NextIndex)
	}
	if err := committer.Finish(); err != nil {
		t.Fatalf("second Finish() error = %v", err)
	}
}

func TestOutputCommitterSupportsEveryFlushMode(t *testing.T) {
	checks := []struct {
		name                  string
		interval              int
		flushesBeforeFinish   int
		committedBeforeFinish int
	}{
		{name: "disabled", interval: 0, flushesBeforeFinish: 0, committedBeforeFinish: 0},
		{name: "each result", interval: 1, flushesBeforeFinish: 3, committedBeforeFinish: 3},
		{name: "default", interval: 1000, flushesBeforeFinish: 0, committedBeforeFinish: 0},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			scanWriter := &flushCountingWriter{}
			openWriter := &flushCountingWriter{}
			chunk := &task.Chunk{CIDR: "192.0.2.0/24", TotalCount: 3, NextIndex: 3, Status: "scanning"}
			committer := newOutputCommitter(outputCommitterConfig{
				outputs: &batchOutputs{
					scanPath:       "scan.csv",
					openOnlyPath:   "opened.csv",
					scanWriter:     scanWriter,
					openOnlyWriter: openWriter,
				},
				flushInterval: check.interval,
				runtimes:      []*chunkRuntime{{state: chunk, tracker: newChunkStateTracker(chunk)}},
				logger:        newLogger("error", false, &bytes.Buffer{}),
			})
			for index := 0; index < 3; index++ {
				if err := committer.Accept(scanResult{chunkIdx: 0, taskIdx: index, record: writer.Record{Status: "open"}}); err != nil {
					t.Fatalf("Accept(%d) error = %v", index, err)
				}
			}
			if scanWriter.flushes != check.flushesBeforeFinish || committer.Summary().written != check.committedBeforeFinish {
				t.Fatalf("before Finish() flushes=%d committed=%d", scanWriter.flushes, committer.Summary().written)
			}
			if err := committer.Finish(); err != nil {
				t.Fatalf("Finish() error = %v", err)
			}
			if committer.Summary().written != 3 {
				t.Fatalf("final committed count = %d, want 3", committer.Summary().written)
			}
		})
	}
}

func TestOutputCommitterCommitsAtTheBoundaryAndFlushesTheFinalBatch(t *testing.T) {
	scanWriter := &flushCountingWriter{}
	openWriter := &flushCountingWriter{}
	chunk := &task.Chunk{CIDR: "192.0.2.0/24", TotalCount: 3, NextIndex: 3, Status: "scanning"}
	runtimes := []*chunkRuntime{{state: chunk, tracker: newChunkStateTracker(chunk)}}
	var logs bytes.Buffer
	var observed []uint64
	committer := newOutputCommitter(outputCommitterConfig{
		outputs: &batchOutputs{
			scanPath:       "scan.csv",
			openOnlyPath:   "opened.csv",
			scanWriter:     scanWriter,
			openOnlyWriter: openWriter,
		},
		flushInterval: 2,
		runtimes:      runtimes,
		logger:        newLogger("info", true, &logs),
		ctrl:          speedctrl.NewController(),
		progressStep:  100,
		resultObserverCallback: func(completed uint64) {
			observed = append(observed, completed)
		},
	})

	first := scanResult{chunkIdx: 0, taskIdx: 0, record: writer.Record{IP: "192.0.2.1", Port: 80, Status: "open"}}
	if err := committer.Accept(first); err != nil {
		t.Fatalf("Accept(first) error = %v", err)
	}
	if got := runtimes[0].tracker.ScannedCount(); got != 0 {
		t.Fatalf("scanned count before commit = %d, want 0", got)
	}
	if committer.Summary().written != 0 || scanWriter.flushes != 0 || openWriter.flushes != 0 {
		t.Fatalf("first result committed early: summary=%+v scan=%+v open=%+v", committer.Summary(), scanWriter, openWriter)
	}
	if len(observed) != 0 {
		t.Fatalf("observer received pending progress: %v", observed)
	}

	second := scanResult{chunkIdx: 0, taskIdx: 1, record: writer.Record{IP: "192.0.2.2", Port: 80, Status: "close"}}
	if err := committer.Accept(second); err != nil {
		t.Fatalf("Accept(second) error = %v", err)
	}
	if got := committer.Summary().written; got != 2 {
		t.Fatalf("written after boundary = %d, want 2", got)
	}
	if fmt.Sprint(observed) != "[1 2]" {
		t.Fatalf("committed observer values = %v, want [1 2]", observed)
	}
	if scanWriter.flushes != 1 || openWriter.flushes != 1 {
		t.Fatalf("boundary flushes = scan:%d open:%d, want one each", scanWriter.flushes, openWriter.flushes)
	}

	third := scanResult{chunkIdx: 0, taskIdx: 2, record: writer.Record{IP: "192.0.2.3", Port: 443, Status: "open"}}
	if err := committer.Accept(third); err != nil {
		t.Fatalf("Accept(third) error = %v", err)
	}
	if err := committer.Finish(); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if got := committer.Summary().written; got != 3 {
		t.Fatalf("written after final flush = %d, want 3", got)
	}
	if fmt.Sprint(observed) != "[1 2 3]" {
		t.Fatalf("final observer values = %v, want [1 2 3]", observed)
	}
	if scanWriter.flushes != 2 || openWriter.flushes != 2 {
		t.Fatalf("all flushes = scan:%d open:%d, want two each", scanWriter.flushes, openWriter.flushes)
	}
	if !bytes.Contains(logs.Bytes(), []byte(`"msg":"scan_result"`)) ||
		!bytes.Contains(logs.Bytes(), []byte(`"output_state":"pending"`)) ||
		!bytes.Contains(logs.Bytes(), []byte(`"batch_id":1`)) ||
		!bytes.Contains(logs.Bytes(), []byte(`"msg":"output_batch_committed"`)) {
		t.Fatalf("batch telemetry is incomplete:\n%s", logs.String())
	}
}
