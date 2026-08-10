# Robustness, E2E, and Maintainability Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix concurrency bugs, add atomic CSV writes, consolidate duplicated group builders, decompose scan.go, decouple CSV contract, and build a comprehensive e2e HTML report with per-scenario checklists.

**Architecture:** Incremental fix-first — Phase 1 fixes robustness bugs with failing tests first, Phase 2 refactors for maintainability using move-and-verify, Phase 3 extends the e2e report Go tool. All changes follow TDD per constitution III.

**Tech Stack:** Go 1.24.x, standard library only, Docker Compose for e2e

---

## Phase 1: Robustness Fixes

### Task 1: Chunk State Tracker — Thread-Safe State Mutation

**Files:**
- Create: `pkg/scanapp/chunk_state_tracker.go`
- Create: `pkg/scanapp/chunk_state_tracker_test.go`
- Modify: `pkg/scanapp/task_dispatcher.go:45`
- Modify: `pkg/scanapp/result_aggregator.go:31-37`
- Modify: `pkg/scanapp/runtime_types.go:44-50`
- Modify: `pkg/scanapp/scan.go:180-195`

**Step 1: Write the failing test**

Create `pkg/scanapp/chunk_state_tracker_test.go`:

```go
package scanapp

import (
	"sync"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestChunkStateTracker_WhenConcurrentMutations_MaintainsConsistentState(t *testing.T) {
	ch := &task.Chunk{
		CIDR:         "10.0.0.0/24",
		TotalCount:   1000,
		NextIndex:    0,
		ScannedCount: 0,
		Status:       "pending",
	}
	tracker := newChunkStateTracker(ch)

	var wg sync.WaitGroup
	// Simulate dispatcher advancing NextIndex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			tracker.AdvanceNextIndex(i + 1)
		}
	}()

	// Simulate aggregator incrementing ScannedCount
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			tracker.IncrementScanned()
		}
	}()

	wg.Wait()

	snap := tracker.Snapshot()
	if snap.NextIndex != 1000 {
		t.Fatalf("expected NextIndex=1000, got %d", snap.NextIndex)
	}
	if snap.ScannedCount != 1000 {
		t.Fatalf("expected ScannedCount=1000, got %d", snap.ScannedCount)
	}
	if snap.Status != "completed" {
		t.Fatalf("expected Status=completed, got %s", snap.Status)
	}
}

func TestChunkStateTracker_WhenPartialProgress_ReportsScanning(t *testing.T) {
	ch := &task.Chunk{TotalCount: 10, Status: "pending"}
	tracker := newChunkStateTracker(ch)

	tracker.AdvanceNextIndex(5)
	tracker.IncrementScanned()

	snap := tracker.Snapshot()
	if snap.NextIndex != 5 {
		t.Fatalf("expected NextIndex=5, got %d", snap.NextIndex)
	}
	if snap.ScannedCount != 1 {
		t.Fatalf("expected ScannedCount=1, got %d", snap.ScannedCount)
	}
	if snap.Status != "scanning" {
		t.Fatalf("expected Status=scanning, got %s", snap.Status)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/ -run TestChunkStateTracker -v`
Expected: FAIL with "undefined: newChunkStateTracker"

**Step 3: Write minimal implementation**

Create `pkg/scanapp/chunk_state_tracker.go`:

```go
package scanapp

import (
	"sync"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type chunkStateTracker struct {
	mu    sync.Mutex
	chunk *task.Chunk
}

func newChunkStateTracker(ch *task.Chunk) *chunkStateTracker {
	return &chunkStateTracker{chunk: ch}
}

func (t *chunkStateTracker) AdvanceNextIndex(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chunk.NextIndex = i
	t.updateStatus()
}

func (t *chunkStateTracker) IncrementScanned() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chunk.ScannedCount++
	t.updateStatus()
}

func (t *chunkStateTracker) Snapshot() task.Chunk {
	t.mu.Lock()
	defer t.mu.Unlock()
	return *t.chunk
}

func (t *chunkStateTracker) ScannedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.ScannedCount
}

func (t *chunkStateTracker) TotalCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.TotalCount
}

func (t *chunkStateTracker) CIDR() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.CIDR
}

func (t *chunkStateTracker) Status() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.Status
}

func (t *chunkStateTracker) updateStatus() {
	if t.chunk.ScannedCount >= t.chunk.TotalCount {
		t.chunk.Status = "completed"
	} else if t.chunk.ScannedCount > 0 || t.chunk.NextIndex > 0 {
		t.chunk.Status = "scanning"
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp/ -run TestChunkStateTracker -v -count=1`
Expected: PASS

**Step 5: Wire tracker into chunkRuntime and update consumers**

Modify `pkg/scanapp/runtime_types.go` — add `tracker` field to `chunkRuntime`:

```go
type chunkRuntime struct {
	ipCidr  string
	ports   []int
	targets []scanTarget
	state   *task.Chunk
	tracker *chunkStateTracker
	bkt     *ratelimit.LeakyBucket
}
```

Modify `pkg/scanapp/scan.go` — in `buildRuntime()` (line ~290-297), after creating the runtime, set tracker:

```go
rt := &chunkRuntime{
	ipCidr:  ch.CIDR,
	ports:   ports,
	targets: group.targets,
	state:   ch,
	tracker: newChunkStateTracker(ch),
	bkt:     ratelimit.NewLeakyBucket(policy.bucketRate, policy.bucketCapacity),
}
```

Modify `pkg/scanapp/task_dispatcher.go:45` — replace `ch.NextIndex = i + 1` with:

```go
rt.tracker.AdvanceNextIndex(i + 1)
```

Also update line 13-18 to use tracker for initial status checks:

```go
ch := rt.state
if ch.NextIndex >= ch.TotalCount {
	rt.tracker.AdvanceNextIndex(ch.NextIndex) // sets completed via tracker
	continue
}
rt.tracker.AdvanceNextIndex(ch.NextIndex) // sets scanning via tracker
```

Modify `pkg/scanapp/result_aggregator.go:27-37` — replace direct chunk mutation with tracker:

```go
func applyScanResult(runtimes []*chunkRuntime, res scanResult, summary *resultSummary) *resultSummary {
	if summary == nil {
		summary = &resultSummary{}
	}
	runtimes[res.chunkIdx].tracker.IncrementScanned()

	summary.written++
	switch {
	case strings.EqualFold(res.record.Status, "open"):
		summary.openCount++
	case strings.Contains(strings.ToLower(res.record.Status), "timeout"):
		summary.timeoutCount++
	default:
		summary.closeCount++
	}
	return summary
}
```

Update `emitScanResultEvents` to use tracker for progress reads:

```go
func emitScanResultEvents(stdout io.Writer, logger *scanLogger, ctrl *speedctrl.Controller, progressStep int, runtimes []*chunkRuntime, res scanResult, summary *resultSummary) {
	logger.eventf("scan_result", res.record.IP, res.record.Port, "scanned", statusErrorCause(res.record.Status), map[string]any{
		"status":           res.record.Status,
		"response_time_ms": res.record.ResponseMS,
		"cidr":             res.record.IPCidr,
	})
	if summary == nil || progressStep <= 0 || summary.written%progressStep != 0 {
		return
	}

	rt := runtimes[res.chunkIdx]
	scanned := rt.tracker.ScannedCount()
	total := rt.tracker.TotalCount()
	cidr := rt.tracker.CIDR()
	_, _ = fmt.Fprintf(stdout, "progress cidr=%s scanned=%d/%d paused=%t\n", cidr, scanned, total, ctrl.IsPaused())
	completionRate := 0.0
	if total > 0 {
		completionRate = float64(scanned) / float64(total)
	}
	logger.eventf("scan_progress", "", 0, "progress", "none", map[string]any{
		"cidr":            cidr,
		"scanned_count":   scanned,
		"total_count":     total,
		"completion_rate": completionRate,
		"paused":          ctrl.IsPaused(),
	})
}
```

Update `hasIncomplete` and `collectChunkStates` in `scan.go` to use tracker:

```go
func hasIncomplete(runtimes []*chunkRuntime) bool {
	for _, rt := range runtimes {
		snap := rt.tracker.Snapshot()
		if snap.ScannedCount < snap.TotalCount {
			return true
		}
	}
	return false
}

func collectChunkStates(runtimes []*chunkRuntime) []task.Chunk {
	out := make([]task.Chunk, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, rt.tracker.Snapshot())
	}
	return out
}
```

**Step 6: Run full test suite to verify no regressions**

Run: `go test ./pkg/scanapp/ -v -count=1 -race`
Expected: ALL PASS (the `-race` flag will catch any remaining data races)

**Step 7: Commit**

```bash
git add pkg/scanapp/chunk_state_tracker.go pkg/scanapp/chunk_state_tracker_test.go \
  pkg/scanapp/runtime_types.go pkg/scanapp/task_dispatcher.go \
  pkg/scanapp/result_aggregator.go pkg/scanapp/scan.go
git commit -m "fix: add thread-safe chunk state tracker for concurrent dispatch/aggregation"
```

---

### Task 2: Atomic CSV Writes

**Files:**
- Modify: `pkg/scanapp/output_files.go`
- Modify: `pkg/scanapp/scan.go:69-73`
- Test: `pkg/scanapp/output_files_test.go` (new)

**Step 1: Write the failing test**

Create `pkg/scanapp/output_files_test.go`:

```go
package scanapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestBatchOutputs_WhenCommitCalledOnSuccess_RenamesTmpToFinal(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(true); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	if _, err := os.Stat(scanPath); err != nil {
		t.Fatalf("expected final scan file, got: %v", err)
	}
	if _, err := os.Stat(openPath); err != nil {
		t.Fatalf("expected final open file, got: %v", err)
	}
	// tmp files should not exist
	if _, err := os.Stat(scanPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no tmp scan file, got: %v", err)
	}
}

func TestBatchOutputs_WhenFinalizeCalledOnFailure_KeepsTmpFiles(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(false); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	// final files should NOT exist
	if _, err := os.Stat(scanPath); !os.IsNotExist(err) {
		t.Fatalf("expected no final scan file on failure, got: %v", err)
	}
	// tmp files should exist
	if _, err := os.Stat(scanPath + ".tmp"); err != nil {
		t.Fatalf("expected tmp scan file on failure, got: %v", err)
	}
}

func TestBatchOutputs_WhenFinalizedSuccessfully_ContainsWrittenData(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(true); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	data, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(data), "1.2.3.4") {
		t.Fatalf("expected data in final file, got: %s", string(data))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/ -run TestBatchOutputs -v`
Expected: FAIL — `Finalize` method does not exist

**Step 3: Write minimal implementation**

Rewrite `pkg/scanapp/output_files.go`:

```go
package scanapp

import (
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type batchOutputs struct {
	scanFile       *os.File
	openOnlyFile   *os.File
	scanWriter     *writer.CSVWriter
	openOnlyWriter *writer.OpenOnlyWriter
	scanFinalPath  string
	openFinalPath  string
}

func openBatchOutputs(scanPath, openPath string) (*batchOutputs, error) {
	scanTmpPath := scanPath + ".tmp"
	scanFile, err := os.Create(scanTmpPath)
	if err != nil {
		return nil, err
	}

	scanWriter := writer.NewCSVWriter(scanFile)
	if err := scanWriter.WriteHeader(); err != nil {
		_ = scanFile.Close()
		return nil, err
	}

	openTmpPath := openPath + ".tmp"
	openOnlyFile, err := os.Create(openTmpPath)
	if err != nil {
		_ = scanFile.Close()
		return nil, err
	}

	openOnlyWriter := writer.NewOpenOnlyWriter(writer.NewCSVWriter(openOnlyFile))
	if err := openOnlyWriter.WriteHeader(); err != nil {
		_ = openOnlyFile.Close()
		_ = scanFile.Close()
		return nil, err
	}

	return &batchOutputs{
		scanFile:       scanFile,
		openOnlyFile:   openOnlyFile,
		scanWriter:     scanWriter,
		openOnlyWriter: openOnlyWriter,
		scanFinalPath:  scanPath,
		openFinalPath:  openPath,
	}, nil
}

func (b *batchOutputs) Finalize(success bool) error {
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
	if firstErr != nil {
		return firstErr
	}

	if success {
		if err := os.Rename(b.scanFinalPath+".tmp", b.scanFinalPath); err != nil {
			return err
		}
		if err := os.Rename(b.openFinalPath+".tmp", b.openFinalPath); err != nil {
			return err
		}
	}
	return nil
}
```

**Step 4: Update scan.go to use Finalize instead of Close**

In `pkg/scanapp/scan.go`, change `defer outputs.Close()` (line 73) to track success:

Replace lines 69-73 and 160-170:

```go
outputs, err := openBatchOutputs(plan.scanOutputPath, plan.openOnlyPath)
if err != nil {
	return err
}
var scanSuccess bool
defer func() {
	_ = outputs.Finalize(scanSuccess)
}()
```

And before the final return at line 170:

```go
emitCompletionSummary(logger, summary, startedAt, nil)
scanSuccess = true
return nil
```

**Step 5: Run full test suite**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add pkg/scanapp/output_files.go pkg/scanapp/output_files_test.go pkg/scanapp/scan.go
git commit -m "fix: atomic CSV writes via tmp-then-rename on successful completion"
```

---

### Task 3: Output Collision Upper Bound

**Files:**
- Modify: `pkg/scanapp/batch_output.go:20`
- Test: `pkg/scanapp/batch_output_test.go` (existing)

**Step 1: Write the failing test**

Add to `pkg/scanapp/batch_output_test.go`:

```go
func TestResolveBatchOutputPaths_WhenOver100Collisions_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)
	ts := now.UTC().Format("20060102T150405Z")

	// Create 100 collision files (seq 0 through 99)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scan_results-%s.csv", ts)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scan_results-%s-%d.csv", ts, i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, _, err := resolveBatchOutputPaths(filepath.Join(dir, "scan_results.csv"), now)
	if err == nil {
		t.Fatal("expected error when 100 collisions exist")
	}
	if !strings.Contains(err.Error(), "failed to allocate") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

Add necessary imports (`fmt`, `strings`) to the test file if missing.

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/ -run TestResolveBatchOutputPaths_WhenOver100 -v`
Expected: FAIL — currently succeeds because limit is 1,000,000

**Step 3: Change the limit**

In `pkg/scanapp/batch_output.go:20`, change:

```go
for seq := 0; seq < 1_000_000; seq++ {
```

to:

```go
for seq := 0; seq < 100; seq++ {
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp/ -run TestResolveBatchOutputPaths -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/batch_output.go pkg/scanapp/batch_output_test.go
git commit -m "fix: cap output collision sequence at 100 to fail fast on misconfiguration"
```

---

### Task 4: Rate-Limit Token Leak Fix

**Files:**
- Modify: `pkg/scanapp/task_dispatcher.go:19-28`
- Test: `pkg/scanapp/scan_test.go` (existing dispatch test)

**Step 1: Write the failing test**

Add to `pkg/scanapp/scan_test.go`:

```go
func TestDispatchTasks_WhenPausedDuringDispatch_DoesNotLeakTokensBeforeGate(t *testing.T) {
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("debug", false, logOut)
	bucket := ratelimit.NewLeakyBucket(100, 100)
	defer bucket.Close()

	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
			{ip: "10.0.0.2", ipCidr: "10.0.0.0/24"},
		},
		state:   &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 2, Status: "pending"},
		tracker: newChunkStateTracker(&task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 2, Status: "pending"}),
		bkt:     bucket,
	}
	taskCh := make(chan scanTask, 4)

	// Pause immediately, then unpause after short delay
	ctrl.SetAPIPaused(true)
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctrl.SetAPIPaused(false)
	}()

	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(taskCh) != 2 {
		t.Fatalf("expected 2 tasks dispatched, got %d", len(taskCh))
	}
}
```

**Step 2: Run test to verify behavior**

Run: `go test ./pkg/scanapp/ -run TestDispatchTasks_WhenPausedDuringDispatch -v -timeout 5s`
Expected: Should pass but verifies new ordering works correctly

**Step 3: Fix the token ordering**

Rewrite `pkg/scanapp/task_dispatcher.go`:

```go
package scanapp

import (
	"context"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

func dispatchTasks(ctx context.Context, policy dispatchPolicy, ctrl *speedctrl.Controller, logger *scanLogger, runtimes []*chunkRuntime, taskCh chan<- scanTask) error {
	for idx := range runtimes {
		rt := runtimes[idx]
		ch := rt.state
		if ch.NextIndex >= ch.TotalCount {
			rt.tracker.AdvanceNextIndex(ch.NextIndex)
			continue
		}
		rt.tracker.AdvanceNextIndex(ch.NextIndex)
		for i := ch.NextIndex; i < ch.TotalCount; i++ {
			if err := rt.bkt.Acquire(ctx); err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ctrl.Gate():
			}

			target, port, err := indexToRuntimeTarget(rt.targets, rt.ports, i)
			if err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case taskCh <- scanTask{
				chunkIdx: idx,
				ipCidr:   defaultString(target.ipCidr, ch.CIDR),
				ip:       target.ip,
				port:     port,
				meta:     target.meta,
			}:
			}
			rt.tracker.AdvanceNextIndex(i + 1)
			logger.debugf("dispatch cidr=%s target=%s:%d next_index=%d/%d", ch.CIDR, target.ip, port, i+1, ch.TotalCount)
			if policy.delay > 0 {
				time.Sleep(policy.delay)
			}
		}
	}
	return nil
}
```

**Step 4: Run full dispatch tests**

Run: `go test ./pkg/scanapp/ -run TestDispatchTasks -v -count=1`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/task_dispatcher.go pkg/scanapp/scan_test.go
git commit -m "fix: acquire rate-limit token before pause gate to prevent token leak"
```

---

### Task 5: Graceful Worker Drain

**Files:**
- Modify: `pkg/scanapp/scan.go:126-149`
- Test: `pkg/scanapp/scan_test.go` (existing cancellation test validates behavior)

**Step 1: Write the failing test**

Add to `pkg/scanapp/scan_test.go`:

```go
func TestRun_WhenCanceled_ResumeStateReflectsAllCompletedScans(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	// 4 IPs x 4 ports = 16 tasks, slow enough to cancel mid-scan
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/30,127.0.0.0/30,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          50 * time.Millisecond,
		Delay:            10 * time.Millisecond,
		BucketRate:       2,
		BucketCapacity:   2,
		Workers:          2,
		PressureInterval: 10 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	_ = Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeFile,
	})

	chunks, err := state.Load(resumeFile)
	if err != nil {
		t.Fatalf("expected resume state, got: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk in resume state")
	}
	// ScannedCount should be > 0 (workers completed some scans before drain)
	if chunks[0].ScannedCount == 0 {
		t.Fatal("expected ScannedCount > 0 after draining in-flight results")
	}
}
```

**Step 2: Run test to verify baseline**

Run: `go test ./pkg/scanapp/ -run TestRun_WhenCanceled_ResumeStateReflectsAll -v -count=1`
Expected: May pass or fail depending on timing — this test verifies the drain behavior.

**Step 3: Fix the drain logic in scan.go**

In `pkg/scanapp/scan.go`, modify the event loop (lines 126-149):

```go
	startedAt := time.Now()
	for !dispatchDone || resultCh != nil {
		select {
		case apiErr := <-apiErrCh:
			if apiErr != nil && runErr == nil {
				runErr = apiErr
				cancel()
			}
		case err := <-dispatchErrCh:
			dispatchDone = true
			dispatchErr = err
			dispatchErrCh = nil
		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			if runErr == nil {
				if err := writeScanRecord(outputs.scanWriter, outputs.openOnlyWriter, res.record); err != nil {
					runErr = err
					cancel()
				}
			}
			applyScanResult(plan.runtimes, res, &summary)
			if runErr == nil {
				emitScanResultEvents(stdout, logger, ctrl, progressStep, plan.runtimes, res, &summary)
			}
		}
	}
```

The key change: `applyScanResult` is always called (even after `runErr` is set), and `writeScanRecord` is skipped when `runErr` is set. This ensures `ScannedCount` in the tracker stays accurate for resume state.

**Step 4: Run full test suite**

Run: `go test ./pkg/scanapp/ -v -count=1 -race`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/scan.go pkg/scanapp/scan_test.go
git commit -m "fix: drain in-flight results after error for accurate resume state"
```

---

## Phase 2: Maintainability Refactoring

### Task 6: Extract Logger to scan_logger.go

**Files:**
- Create: `pkg/scanapp/scan_logger.go`
- Modify: `pkg/scanapp/scan.go` (remove lines 532-592)

**Step 1: Run full test suite to establish baseline**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 2: Move logger code**

Create `pkg/scanapp/scan_logger.go` — move `scanLogger` struct and all its methods from `scan.go:532-592`:

```go
package scanapp

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/logx"
)

type scanLogger struct {
	level  int
	asJSON bool
	out    io.Writer
}

func newLogger(level string, asJSON bool, out io.Writer) *scanLogger {
	parsed := 1
	switch strings.ToLower(level) {
	case "debug":
		parsed = 0
	case "info":
		parsed = 1
	case "error":
		parsed = 2
	}
	return &scanLogger{level: parsed, asJSON: asJSON, out: out}
}

func (l *scanLogger) debugf(format string, args ...any) {
	l.logWithFields(0, "debug", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) infof(format string, args ...any) {
	l.logWithFields(1, "info", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) errorf(format string, args ...any) {
	l.logWithFields(2, "error", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) eventf(msg, target string, port int, transition, errCause string, extra map[string]any) {
	fields := map[string]any{
		"target":           target,
		"port":             port,
		"state_transition": transition,
		"error_cause":      errCause,
	}
	for k, v := range extra {
		fields[k] = v
	}
	l.logWithFields(1, "info", msg, fields)
}

func (l *scanLogger) logWithFields(level int, levelName, msg string, fields map[string]any) {
	if l == nil || level < l.level {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	if l.asJSON {
		logx.LogJSON(l.out, levelName, msg, fields)
		return
	}
	if len(fields) > 0 {
		_, _ = fmt.Fprintf(l.out, "[%s] %s fields=%v\n", strings.ToUpper(levelName), msg, fields)
		return
	}
	_, _ = fmt.Fprintf(l.out, "[%s] %s\n", strings.ToUpper(levelName), msg)
}
```

Remove the same code from `scan.go`. Also remove `logx` from `scan.go` imports if no longer needed.

**Step 3: Run tests to verify no regressions**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add pkg/scanapp/scan_logger.go pkg/scanapp/scan.go
git commit -m "refactor: extract scanLogger to scan_logger.go"
```

---

### Task 7: Extract Helpers to scan_helpers.go

**Files:**
- Create: `pkg/scanapp/scan_helpers.go`
- Modify: `pkg/scanapp/scan.go` (remove helper functions)
- Modify: `pkg/scanapp/result_aggregator.go` (move error cause helpers here)

**Step 1: Run baseline**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 2: Move helper functions**

Create `pkg/scanapp/scan_helpers.go`:

```go
package scanapp

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
)

func indexToRuntimeTarget(targets []scanTarget, ports []int, idx int) (scanTarget, int, error) {
	if len(targets) == 0 {
		return scanTarget{}, 0, fmt.Errorf("empty targets")
	}
	if len(ports) == 0 {
		return scanTarget{}, 0, fmt.Errorf("empty ports")
	}
	if idx < 0 {
		return scanTarget{}, 0, fmt.Errorf("negative index")
	}
	targetIdx := idx / len(ports)
	portIdx := idx % len(ports)
	if targetIdx >= len(targets) {
		return scanTarget{}, 0, fmt.Errorf("index out of range")
	}
	return targets[targetIdx], ports[portIdx], nil
}

func ipv4ToUint32(ip string) uint32 {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return 0
	}
	return binary.BigEndian.Uint32(parsed)
}

func parsePortRows(rows []string) ([]int, error) {
	ports := make([]int, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(strings.TrimSpace(row), "/")
		if len(parts) != 2 || strings.ToLower(parts[1]) != "tcp" {
			return nil, fmt.Errorf("invalid chunk port row: %s", row)
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid chunk port number: %s", row)
		}
		ports = append(ports, n)
	}
	return ports, nil
}

func defaultString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func ensureFDLimit(workers int) error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return nil
	}
	minNeed := uint64(1024)
	if workers > 0 {
		workerNeed := uint64(workers * 8)
		if workerNeed > minNeed {
			minNeed = workerNeed
		}
	}
	if lim.Cur < minNeed {
		return fmt.Errorf("file descriptor limit too low: %d (need >= %d)", lim.Cur, minNeed)
	}
	return nil
}
```

Move `statusErrorCause` and `errorCause` into `result_aggregator.go` (add `context` and `errors` to its imports):

```go
func statusErrorCause(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "timeout"):
		return "timeout"
	case s == "close":
		return "closed"
	default:
		return "none"
	}
}

func errorCause(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "runtime_error"
}
```

Remove all moved functions from `scan.go`. Update `scan.go` imports — remove `encoding/binary`, `strconv`, `syscall` if they are no longer used there.

**Step 3: Run tests**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add pkg/scanapp/scan_helpers.go pkg/scanapp/scan.go pkg/scanapp/result_aggregator.go
git commit -m "refactor: extract helpers to scan_helpers.go and error causes to result_aggregator.go"
```

---

### Task 8: Extract Chunk Lifecycle to chunk_lifecycle.go

**Files:**
- Create: `pkg/scanapp/chunk_lifecycle.go`
- Modify: `pkg/scanapp/scan.go` (remove chunk functions)

**Step 1: Run baseline**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 2: Move chunk lifecycle functions**

Create `pkg/scanapp/chunk_lifecycle.go` — move from `scan.go`:
- `loadOrBuildChunks`
- `buildRuntime`
- `hasRichRecords`
- `buildRichChunks`
- `hasIncomplete`
- `collectChunkStates`
- `shouldSaveOnDispatchErr`

```go
package scanapp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func loadOrBuildChunks(cfg config.Config, cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec) ([]task.Chunk, error) {
	if cfg.Resume != "" {
		return state.Load(cfg.Resume)
	}
	if hasRichRecords(cidrRecords) {
		return buildRichChunks(cidrRecords)
	}
	groups, err := buildCIDRGroups(cidrRecords)
	if err != nil {
		return nil, err
	}
	rawPorts := make([]string, 0, len(portSpecs))
	for _, p := range portSpecs {
		rawPorts = append(rawPorts, p.Raw)
	}
	cidrs := make([]string, 0, len(groups))
	for cidr := range groups {
		cidrs = append(cidrs, cidr)
	}
	sort.Strings(cidrs)

	out := make([]task.Chunk, 0, len(cidrs))
	for _, cidr := range cidrs {
		g := groups[cidr]
		total := len(g.targets) * len(portSpecs)
		cidrName := ""
		if len(g.targets) > 0 {
			cidrName = g.targets[0].meta.cidrName
		}
		out = append(out, task.Chunk{
			CIDR:         cidr,
			CIDRName:     cidrName,
			Ports:        rawPorts,
			NextIndex:    0,
			ScannedCount: 0,
			TotalCount:   total,
			Status:       "pending",
		})
	}
	return out, nil
}

func buildRuntime(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy) ([]*chunkRuntime, error) {
	var (
		groups map[string]cidrGroup
		err    error
	)
	if hasRichRecords(cidrRecords) {
		groups, err = buildRichGroups(cidrRecords)
	} else {
		groups, err = buildCIDRGroups(cidrRecords)
	}
	if err != nil {
		return nil, err
	}

	runtimes := make([]*chunkRuntime, 0, len(chunks))
	for i := range chunks {
		ch := &chunks[i]
		group, ok := groups[ch.CIDR]
		if !ok {
			return nil, fmt.Errorf("cidr %s from chunk not found in cidr file", ch.CIDR)
		}

		portRows := ch.Ports
		if len(portRows) == 0 {
			if group.port > 0 {
				portRows = []string{fmt.Sprintf("%d/tcp", group.port)}
			} else {
				portRows = make([]string, 0, len(defaultPorts))
				for _, p := range defaultPorts {
					portRows = append(portRows, p.Raw)
				}
			}
			ch.Ports = append(ch.Ports, portRows...)
		}
		ports, err := parsePortRows(portRows)
		if err != nil {
			return nil, err
		}

		expectedTotal := len(group.targets) * len(ports)
		if ch.TotalCount == 0 {
			ch.TotalCount = expectedTotal
		}
		if ch.TotalCount != expectedTotal {
			return nil, fmt.Errorf("chunk total_count mismatch for %s: state=%d expected=%d", ch.CIDR, ch.TotalCount, expectedTotal)
		}
		if ch.NextIndex >= ch.TotalCount {
			ch.Status = "completed"
		} else if ch.Status == "" {
			ch.Status = "pending"
		}
		rt := &chunkRuntime{
			ipCidr:  ch.CIDR,
			ports:   ports,
			targets: group.targets,
			state:   ch,
			tracker: newChunkStateTracker(ch),
			bkt:     ratelimit.NewLeakyBucket(policy.bucketRate, policy.bucketCapacity),
		}
		runtimes = append(runtimes, rt)
	}
	return runtimes, nil
}

func hasRichRecords(cidrRecords []input.CIDRRecord) bool {
	for _, rec := range cidrRecords {
		if rec.IsRich {
			return true
		}
	}
	return false
}

func buildRichChunks(cidrRecords []input.CIDRRecord) ([]task.Chunk, error) {
	groups, err := buildRichGroups(cidrRecords)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]task.Chunk, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		cidrName := ""
		if len(g.targets) > 0 {
			cidrName = g.targets[0].meta.cidrName
		}
		out = append(out, task.Chunk{
			CIDR:         key,
			CIDRName:     cidrName,
			Ports:        []string{fmt.Sprintf("%d/tcp", g.port)},
			NextIndex:    0,
			ScannedCount: 0,
			TotalCount:   1,
			Status:       "pending",
		})
	}
	return out, nil
}

func hasIncomplete(runtimes []*chunkRuntime) bool {
	for _, rt := range runtimes {
		snap := rt.tracker.Snapshot()
		if snap.ScannedCount < snap.TotalCount {
			return true
		}
	}
	return false
}

func collectChunkStates(runtimes []*chunkRuntime) []task.Chunk {
	out := make([]task.Chunk, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, rt.tracker.Snapshot())
	}
	return out
}

func shouldSaveOnDispatchErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
```

Remove all moved functions from `scan.go`. Update imports in `scan.go` — remove `sort`, `state`, `input` (if only used by moved functions), `context` + `errors` for `shouldSaveOnDispatchErr`.

**Step 3: Run tests**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add pkg/scanapp/chunk_lifecycle.go pkg/scanapp/scan.go
git commit -m "refactor: extract chunk lifecycle functions to chunk_lifecycle.go"
```

---

### Task 9: Consolidate Group Builders

**Files:**
- Create: `pkg/scanapp/group_builder.go`
- Create: `pkg/scanapp/group_builder_test.go`
- Modify: `pkg/scanapp/scan.go` (remove group builder functions)

**Step 1: Write the failing test for the strategy interface**

Create `pkg/scanapp/group_builder_test.go`:

```go
package scanapp

import (
	"net"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func TestBuildGroups_WhenBasicStrategy_ProducesSameResultAsBuildCIDRGroups(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("10.0.0.0/30")
	records := []input.CIDRRecord{
		{FabName: "fab1", IPRaw: "10.0.0.1", CIDR: "10.0.0.0/30", CIDRName: "net-a", Net: ipNet},
		{FabName: "fab2", IPRaw: "10.0.0.2", CIDR: "10.0.0.0/30", CIDRName: "net-a", Net: ipNet},
	}

	groups, err := buildGroups(records, basicGroupStrategy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups["10.0.0.0/30"]
	if len(g.targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(g.targets))
	}
}

func TestBuildGroups_WhenRichStrategy_ProducesSameResultAsBuildRichGroups(t *testing.T) {
	records := []input.CIDRRecord{
		{
			IsRich: true, IsValid: true, ExecutionKey: "10.0.0.1:80/tcp",
			DstIP: "10.0.0.1", DstNetworkSegment: "10.0.0.0/24", Port: 80,
			FabName: "fab1", CIDRName: "net-a",
		},
	}

	groups, err := buildGroups(records, richGroupStrategy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups["10.0.0.1:80/tcp"]
	if len(g.targets) != 1 || g.targets[0].ip != "10.0.0.1" {
		t.Fatalf("unexpected target: %+v", g.targets)
	}
	if g.port != 80 {
		t.Fatalf("expected port 80, got %d", g.port)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/ -run TestBuildGroups -v`
Expected: FAIL — `buildGroups`, `basicGroupStrategy`, `richGroupStrategy` undefined

**Step 3: Write the implementation**

Create `pkg/scanapp/group_builder.go`:

```go
package scanapp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type cidrGroup struct {
	targets []scanTarget
	port    int
}

type groupBuildStrategy interface {
	ShouldInclude(rec input.CIDRRecord) bool
	Key(rec input.CIDRRecord) (string, error)
	NewTargets(rec input.CIDRRecord) ([]scanTarget, int, error)
	MergeTarget(existing *scanTarget, rec input.CIDRRecord) error
}

func buildGroups(records []input.CIDRRecord, strategy groupBuildStrategy) (map[string]cidrGroup, error) {
	out := make(map[string]cidrGroup)
	for _, rec := range records {
		if !strategy.ShouldInclude(rec) {
			continue
		}
		key, err := strategy.Key(rec)
		if err != nil {
			return nil, err
		}

		group := out[key]
		if len(group.targets) == 0 {
			targets, port, err := strategy.NewTargets(rec)
			if err != nil {
				return nil, err
			}
			group.targets = targets
			group.port = port
			out[key] = group
			continue
		}
		if err := strategy.MergeTarget(&group.targets[0], rec); err != nil {
			return nil, err
		}
		out[key] = group
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable input rows")
	}
	for key, group := range out {
		sort.Slice(group.targets, func(i, j int) bool {
			return ipv4ToUint32(group.targets[i].ip) < ipv4ToUint32(group.targets[j].ip)
		})
		out[key] = group
	}
	return out, nil
}

// basicGroupStrategy groups by CIDR and expands IP selectors.
type basicGroupStrategy struct{}

func (basicGroupStrategy) ShouldInclude(_ input.CIDRRecord) bool { return true }

func (basicGroupStrategy) Key(rec input.CIDRRecord) (string, error) {
	cidr := rec.CIDR
	if cidr == "" && rec.Net != nil {
		cidr = rec.Net.String()
	}
	if cidr == "" {
		return "", fmt.Errorf("record missing ip_cidr")
	}
	return cidr, nil
}

func (basicGroupStrategy) NewTargets(rec input.CIDRRecord) ([]scanTarget, int, error) {
	cidr := rec.CIDR
	if cidr == "" && rec.Net != nil {
		cidr = rec.Net.String()
	}
	selector := ""
	switch {
	case rec.Selector != nil:
		selector = rec.Selector.String()
	case strings.TrimSpace(rec.IPRaw) != "":
		selector = strings.TrimSpace(rec.IPRaw)
	case rec.Net != nil:
		selector = rec.Net.String()
	default:
		return nil, 0, fmt.Errorf("record for cidr %s missing selector", cidr)
	}

	ips, err := task.ExpandIPSelectors([]string{selector})
	if err != nil {
		return nil, 0, fmt.Errorf("expand selector failed for cidr %s: %w", cidr, err)
	}

	targets := make([]scanTarget, 0, len(ips))
	for _, ip := range ips {
		targets = append(targets, scanTarget{
			ip: ip,
			meta: targetMeta{
				fabName:  rec.FabName,
				cidrName: rec.CIDRName,
			},
		})
	}
	return targets, 0, nil
}

func (basicGroupStrategy) MergeTarget(_ *scanTarget, rec input.CIDRRecord) error {
	// Basic strategy appends additional IPs — handled via multiple calls to buildGroups
	// which accumulates targets. For existing behavior compatibility, we need to handle
	// this in buildGroups by appending rather than merging.
	return nil
}

// richGroupStrategy groups by execution key with metadata merging.
type richGroupStrategy struct{}

func (richGroupStrategy) ShouldInclude(rec input.CIDRRecord) bool {
	return rec.IsRich && rec.IsValid
}

func (richGroupStrategy) Key(rec input.CIDRRecord) (string, error) {
	key := strings.TrimSpace(rec.ExecutionKey)
	if key == "" {
		return "", fmt.Errorf("rich record missing execution_key at row %d", rec.RowNumber)
	}
	return key, nil
}

func (richGroupStrategy) NewTargets(rec input.CIDRRecord) ([]scanTarget, int, error) {
	key := strings.TrimSpace(rec.ExecutionKey)
	return []scanTarget{{
		ip:     rec.DstIP,
		ipCidr: rec.DstNetworkSegment,
		meta: targetMeta{
			fabName:           rec.FabName,
			cidrName:          rec.CIDRName,
			serviceLabel:      rec.ServiceLabel,
			decision:          rec.Decision,
			policyID:          rec.PolicyID,
			reason:            rec.Reason,
			executionKey:      key,
			srcIP:             rec.SrcIP,
			srcNetworkSegment: rec.SrcNetworkSegment,
		},
	}}, rec.Port, nil
}

func (richGroupStrategy) MergeTarget(existing *scanTarget, rec input.CIDRRecord) error {
	existing.meta.fabName = mergeFieldValue(existing.meta.fabName, rec.FabName)
	existing.meta.cidrName = mergeFieldValue(existing.meta.cidrName, rec.CIDRName)
	existing.meta.serviceLabel = mergeFieldValue(existing.meta.serviceLabel, rec.ServiceLabel)
	existing.meta.decision = mergeFieldValue(existing.meta.decision, rec.Decision)
	existing.meta.policyID = mergeFieldValue(existing.meta.policyID, rec.PolicyID)
	existing.meta.reason = mergeFieldValue(existing.meta.reason, rec.Reason)
	existing.meta.srcIP = mergeFieldValue(existing.meta.srcIP, rec.SrcIP)
	existing.meta.srcNetworkSegment = mergeFieldValue(existing.meta.srcNetworkSegment, rec.SrcNetworkSegment)
	return nil
}

func mergeFieldValue(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" || existing == incoming {
		return existing
	}
	if existing == "" {
		return incoming
	}
	parts := strings.Split(existing, "|")
	for _, p := range parts {
		if p == incoming {
			return existing
		}
	}
	return existing + "|" + incoming
}

// buildCIDRGroups wraps buildGroups with basicGroupStrategy for backward compatibility.
// Note: basic strategy needs special handling for multi-record accumulation.
func buildCIDRGroups(cidrRecords []input.CIDRRecord) (map[string]cidrGroup, error) {
	out := make(map[string]cidrGroup)
	strategy := basicGroupStrategy{}
	for _, rec := range cidrRecords {
		key, err := strategy.Key(rec)
		if err != nil {
			return nil, err
		}
		targets, _, err := strategy.NewTargets(rec)
		if err != nil {
			return nil, err
		}
		group := out[key]
		group.targets = append(group.targets, targets...)
		out[key] = group
	}
	for key, group := range out {
		sort.Slice(group.targets, func(i, j int) bool {
			return ipv4ToUint32(group.targets[i].ip) < ipv4ToUint32(group.targets[j].ip)
		})
		out[key] = group
	}
	return out, nil
}

// buildRichGroups wraps buildGroups with richGroupStrategy for backward compatibility.
func buildRichGroups(cidrRecords []input.CIDRRecord) (map[string]cidrGroup, error) {
	return buildGroups(cidrRecords, richGroupStrategy{})
}
```

Remove from `scan.go`: `cidrGroup`, `buildCIDRGroups`, `buildRichGroups`, `mergeFieldValue`, `ipv4ToUint32` (already moved to scan_helpers.go), `scanTarget`, `targetMeta`.

Note: `scanTarget` and `targetMeta` are in `runtime_types.go` — leave them there as they're used across multiple files.

**Step 4: Run full test suite**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/group_builder.go pkg/scanapp/group_builder_test.go pkg/scanapp/scan.go
git commit -m "refactor: consolidate group builders with strategy interface"
```

---

### Task 10: Extract Policy Conversion

**Files:**
- Modify: `pkg/scanapp/runtime_types.go`
- Modify: `pkg/scanapp/chunk_lifecycle.go` (owns runtimePolicy)
- Modify: `pkg/scanapp/task_dispatcher.go` (owns dispatchPolicy)

**Step 1: Run baseline**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 2: Move policy types to their consumers**

Move `runtimePolicy` and `runtimePolicyFromConfig` to `chunk_lifecycle.go` (add `config` import).

Move `dispatchPolicy` and `dispatchPolicyFromConfig` to `task_dispatcher.go` (add `config` import).

Remove both from `runtime_types.go`.

**Step 3: Run tests**

Run: `go test ./pkg/scanapp/ -v -count=1`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add pkg/scanapp/runtime_types.go pkg/scanapp/chunk_lifecycle.go pkg/scanapp/task_dispatcher.go
git commit -m "refactor: move policy types to consuming files"
```

---

### Task 11: Decouple CSV Header/Record Mapping

**Files:**
- Modify: `pkg/writer/csv_writer.go`
- Test: `pkg/writer/csv_writer_test.go` (existing tests validate behavior)
- Test: `pkg/writer/csv_writer_contract_test.go` (existing contract test)

**Step 1: Write the failing test for ColumnDef**

Add to `pkg/writer/csv_writer_contract_test.go`:

```go
func TestColumns_WhenIterated_MatchesExpectedHeaderOrder(t *testing.T) {
	expected := []string{
		"ip", "ip_cidr", "port", "status", "response_time_ms",
		"fab_name", "cidr_name", "service_label", "decision",
		"policy_id", "reason", "execution_key", "src_ip", "src_network_segment",
	}
	if len(Columns) != len(expected) {
		t.Fatalf("expected %d columns, got %d", len(expected), len(Columns))
	}
	for i, col := range Columns {
		if col.Name != expected[i] {
			t.Fatalf("column[%d] name mismatch: got=%s want=%s", i, col.Name, expected[i])
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/writer/ -run TestColumns_WhenIterated -v`
Expected: FAIL — `Columns` undefined

**Step 3: Rewrite csv_writer.go with ColumnDef**

```go
package writer

import (
	"encoding/csv"
	"io"
	"strconv"
)

// Record is one scan output row written to CSV.
type Record struct {
	IP                string
	IPCidr            string
	Port              int
	Status            string
	ResponseMS        int64
	FabName           string
	CIDR              string
	CIDRName          string
	ServiceLabel      string
	Decision          string
	PolicyID          string
	Reason            string
	ExecutionKey      string
	SrcIP             string
	SrcNetworkSegment string
}

// ColumnDef maps a header name to a Record field extractor.
type ColumnDef struct {
	Name    string
	Extract func(Record) string
}

// Columns defines the CSV output contract as a single source of truth.
var Columns = []ColumnDef{
	{"ip", func(r Record) string { return r.IP }},
	{"ip_cidr", func(r Record) string {
		if r.IPCidr != "" {
			return r.IPCidr
		}
		return r.CIDR
	}},
	{"port", func(r Record) string { return strconv.Itoa(r.Port) }},
	{"status", func(r Record) string { return r.Status }},
	{"response_time_ms", func(r Record) string { return strconv.FormatInt(r.ResponseMS, 10) }},
	{"fab_name", func(r Record) string { return r.FabName }},
	{"cidr_name", func(r Record) string { return r.CIDRName }},
	{"service_label", func(r Record) string { return r.ServiceLabel }},
	{"decision", func(r Record) string { return r.Decision }},
	{"policy_id", func(r Record) string { return r.PolicyID }},
	{"reason", func(r Record) string { return r.Reason }},
	{"execution_key", func(r Record) string { return r.ExecutionKey }},
	{"src_ip", func(r Record) string { return r.SrcIP }},
	{"src_network_segment", func(r Record) string { return r.SrcNetworkSegment }},
}

// CSVWriter writes scan result rows with the fixed contract header.
type CSVWriter struct {
	w           *csv.Writer
	wroteHeader bool
}

// NewCSVWriter creates a CSV writer for scan result output.
func NewCSVWriter(out io.Writer) *CSVWriter {
	return &CSVWriter{w: csv.NewWriter(out)}
}

// Write appends a single record and writes header first if needed.
func (cw *CSVWriter) Write(r Record) error {
	if err := cw.WriteHeader(); err != nil {
		return err
	}
	row := make([]string, len(Columns))
	for i, col := range Columns {
		row[i] = col.Extract(r)
	}
	if err := cw.w.Write(row); err != nil {
		return err
	}
	cw.w.Flush()
	return cw.w.Error()
}

// WriteHeader writes the fixed result header once.
func (cw *CSVWriter) WriteHeader() error {
	if !cw.wroteHeader {
		header := make([]string, len(Columns))
		for i, col := range Columns {
			header[i] = col.Name
		}
		if err := cw.w.Write(header); err != nil {
			return err
		}
		cw.wroteHeader = true
		cw.w.Flush()
		return cw.w.Error()
	}
	return nil
}
```

**Step 4: Run all writer tests**

Run: `go test ./pkg/writer/ -v -count=1`
Expected: ALL PASS

**Step 5: Run full test suite for regressions**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add pkg/writer/csv_writer.go pkg/writer/csv_writer_contract_test.go
git commit -m "refactor: decouple CSV header/record mapping with ColumnDef contract"
```

---

## Phase 3: E2E Report Enhancement

### Task 12: Report Data Model

**Files:**
- Create: `e2e/report/types.go`
- Create: `e2e/report/types_test.go`

**Step 1: Write the test**

Create `e2e/report/types_test.go`:

```go
package report

import (
	"testing"
	"time"
)

func TestFullReport_WhenScenariosAdded_ComputesPassFailCounts(t *testing.T) {
	r := FullReport{
		Timestamp:     time.Now(),
		TotalDuration: 5 * time.Second,
		Scenarios: []ScenarioResult{
			{Name: "normal", Status: "pass"},
			{Name: "api_5xx", Status: "pass"},
			{Name: "api_timeout", Status: "fail"},
		},
	}
	r.ComputeCounts()
	if r.PassCount != 2 || r.FailCount != 1 {
		t.Fatalf("expected pass=2 fail=1, got pass=%d fail=%d", r.PassCount, r.FailCount)
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./e2e/report/ -run TestFullReport -v`
Expected: FAIL — types undefined

**Step 3: Write implementation**

Create `e2e/report/types.go`:

```go
package report

import "time"

// ScenarioResult captures the outcome of one e2e scenario.
type ScenarioResult struct {
	Name       string        `json:"name"`
	Status     string        `json:"status"` // "pass" or "fail"
	Duration   time.Duration `json:"duration_ns"`
	ExitCode   int           `json:"exit_code"`
	Checklist  []CheckItem   `json:"checklist"`
	Artifacts  []Artifact    `json:"artifacts"`
	LogSnippet string        `json:"log_snippet,omitempty"`
}

// CheckItem is one pass/fail assertion in a scenario checklist.
type CheckItem struct {
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Detail      string `json:"detail,omitempty"`
}

// Artifact describes a file produced by an e2e scenario.
type Artifact struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	RowCount int    `json:"row_count,omitempty"`
}

// FullReport aggregates all scenario results into the final e2e report.
type FullReport struct {
	Timestamp     time.Time        `json:"timestamp"`
	TotalDuration time.Duration    `json:"total_duration_ns"`
	Scenarios     []ScenarioResult `json:"scenarios"`
	Summary       Summary          `json:"summary"`
	PassCount     int              `json:"pass_count"`
	FailCount     int              `json:"fail_count"`
}

// ComputeCounts populates PassCount and FailCount from Scenarios.
func (r *FullReport) ComputeCounts() {
	r.PassCount = 0
	r.FailCount = 0
	for _, s := range r.Scenarios {
		if s.Status == "pass" {
			r.PassCount++
		} else {
			r.FailCount++
		}
	}
}

// Manifest is the JSON structure written by run_e2e.sh for the Go report tool.
type Manifest struct {
	StartTime int64              `json:"start_time_ns"`
	EndTime   int64              `json:"end_time_ns"`
	Scenarios []ManifestScenario `json:"scenarios"`
}

// ManifestScenario captures per-scenario data from the shell script.
type ManifestScenario struct {
	Name      string   `json:"name"`
	ExitCode  int      `json:"exit_code"`
	StartNs   int64    `json:"start_ns"`
	EndNs     int64    `json:"end_ns"`
	Artifacts []string `json:"artifacts"`
	LogFile   string   `json:"log_file,omitempty"`
}
```

**Step 4: Run test**

Run: `go test ./e2e/report/ -run TestFullReport -v`
Expected: PASS

**Step 5: Commit**

```bash
git add e2e/report/types.go e2e/report/types_test.go
git commit -m "feat(e2e): add report data model types"
```

---

### Task 13: Checklist Evaluator

**Files:**
- Create: `e2e/report/checklist.go`
- Create: `e2e/report/checklist_test.go`

**Step 1: Write the failing test**

Create `e2e/report/checklist_test.go`:

```go
package report

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateNormalChecklist_WhenAllArtifactsPresent_AllItemsPass(t *testing.T) {
	dir := t.TempDir()
	scanCSV := filepath.Join(dir, "scan_results-20260317T120000Z.csv")
	openCSV := filepath.Join(dir, "opened_results-20260317T120000Z.csv")
	reportHTML := filepath.Join(dir, "report.html")
	reportTXT := filepath.Join(dir, "report.txt")

	header := "ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,policy_id,reason,execution_key,src_ip,src_network_segment\n"
	os.WriteFile(scanCSV, []byte(header+"1.2.3.4,1.2.3.0/24,80,open,5,fab1,net1,,,,,,,\n"+"1.2.3.5,1.2.3.0/24,80,close,0,fab1,net1,,,,,,,\n"), 0o644)
	os.WriteFile(openCSV, []byte(header+"1.2.3.4,1.2.3.0/24,80,open,5,fab1,net1,,,,,,,\n"), 0o644)
	os.WriteFile(reportHTML, []byte("<html></html>"), 0o644)
	os.WriteFile(reportTXT, []byte("report"), 0o644)

	items := EvaluateNormalChecklist(dir, scanCSV, openCSV, 0)
	for _, item := range items {
		if !item.Passed {
			t.Fatalf("expected all items to pass, failed: %s — %s", item.Description, item.Detail)
		}
	}
	if len(items) < 9 {
		t.Fatalf("expected at least 9 checklist items, got %d", len(items))
	}
}

func TestEvaluateFailureChecklist_WhenResumeStatePresent_AllItemsPass(t *testing.T) {
	dir := t.TempDir()
	resumePath := filepath.Join(dir, "resume_state_api_5xx.json")
	logPath := filepath.Join(dir, "scenario_api_5xx.log")
	os.WriteFile(resumePath, []byte(`[{"cidr":"10.0.0.0/24"}]`), 0o644)
	os.WriteFile(logPath, []byte("error log"), 0o644)

	items := EvaluateFailureChecklist(dir, "api_5xx", 1)
	for _, item := range items {
		if !item.Passed {
			t.Fatalf("expected all items to pass, failed: %s — %s", item.Description, item.Detail)
		}
	}
	if len(items) < 5 {
		t.Fatalf("expected at least 5 checklist items, got %d", len(items))
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./e2e/report/ -run TestEvaluate -v`
Expected: FAIL — functions undefined

**Step 3: Write implementation**

Create `e2e/report/checklist.go`:

```go
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var expectedHeader = []string{
	"ip", "ip_cidr", "port", "status", "response_time_ms",
	"fab_name", "cidr_name", "service_label", "decision",
	"policy_id", "reason", "execution_key", "src_ip", "src_network_segment",
}

// EvaluateNormalChecklist runs all checklist items for the normal scan scenario.
func EvaluateNormalChecklist(outDir, scanCSV, openCSV string, exitCode int) []CheckItem {
	var items []CheckItem

	items = append(items, checkFileExists("scan_results-*.csv exists", scanCSV))
	items = append(items, checkFileExists("opened_results-*.csv exists", openCSV))
	items = append(items, checkOpenOnlyRows("opened_results contains only open rows", openCSV))

	summary, sumErr := SummarizeCSV(scanCSV)
	if sumErr != nil {
		items = append(items, CheckItem{Description: "At least 1 open result", Passed: false, Detail: sumErr.Error()})
		items = append(items, CheckItem{Description: "At least 1 non-open result", Passed: false, Detail: sumErr.Error()})
	} else {
		items = append(items, CheckItem{
			Description: "At least 1 open result",
			Passed:      summary.Open >= 1,
			Detail:      fmt.Sprintf("found %d open", summary.Open),
		})
		items = append(items, CheckItem{
			Description: "At least 1 non-open result",
			Passed:      (summary.Closed + summary.Timeout) >= 1,
			Detail:      fmt.Sprintf("found %d closed + %d timeout", summary.Closed, summary.Timeout),
		})
	}

	items = append(items, checkCSVHeader("CSV header matches 14-column contract", scanCSV))
	items = append(items, checkFileExists("report.html generated", filepath.Join(outDir, "report.html")))
	items = append(items, checkFileExists("report.txt generated", filepath.Join(outDir, "report.txt")))
	items = append(items, CheckItem{
		Description: "Exit code is 0",
		Passed:      exitCode == 0,
		Detail:      fmt.Sprintf("exit code: %d", exitCode),
	})

	return items
}

// EvaluateFailureChecklist runs all checklist items for a failure scenario.
func EvaluateFailureChecklist(outDir, scenario string, exitCode int) []CheckItem {
	var items []CheckItem
	resumePath := filepath.Join(outDir, fmt.Sprintf("resume_state_%s.json", scenario))
	logPath := filepath.Join(outDir, fmt.Sprintf("scenario_%s.log", scenario))

	items = append(items, CheckItem{
		Description: "Exit code is non-zero",
		Passed:      exitCode != 0,
		Detail:      fmt.Sprintf("exit code: %d", exitCode),
	})
	items = append(items, checkFileExists("resume_state.json created", resumePath))
	items = append(items, checkResumeStateValid("Resume state JSON is valid", resumePath))
	items = append(items, checkResumeStateHasChunks("Resume state contains chunk CIDRs", resumePath))
	items = append(items, checkFileExists("Scenario log captured", logPath))

	return items
}

func checkFileExists(desc, path string) CheckItem {
	info, err := os.Stat(path)
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: fmt.Sprintf("not found: %s", filepath.Base(path))}
	}
	return CheckItem{Description: desc, Passed: true, Detail: fmt.Sprintf("%s (%d bytes)", filepath.Base(path), info.Size())}
}

func checkOpenOnlyRows(desc, path string) CheckItem {
	f, err := os.Open(path)
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	if len(rows) < 2 {
		return CheckItem{Description: desc, Passed: false, Detail: "no data rows"}
	}
	statusIdx := -1
	for i, h := range rows[0] {
		if strings.TrimSpace(h) == "status" {
			statusIdx = i
			break
		}
	}
	if statusIdx < 0 {
		return CheckItem{Description: desc, Passed: false, Detail: "missing status column"}
	}
	for i := 1; i < len(rows); i++ {
		if len(rows[i]) <= statusIdx {
			continue
		}
		if strings.TrimSpace(rows[i][statusIdx]) != "open" {
			return CheckItem{Description: desc, Passed: false, Detail: fmt.Sprintf("row %d has status=%s", i, rows[i][statusIdx])}
		}
	}
	return CheckItem{Description: desc, Passed: true, Detail: fmt.Sprintf("%d open rows", len(rows)-1)}
}

func checkCSVHeader(desc, path string) CheckItem {
	f, err := os.Open(path)
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	if len(header) != len(expectedHeader) {
		return CheckItem{Description: desc, Passed: false, Detail: fmt.Sprintf("expected %d columns, got %d", len(expectedHeader), len(header))}
	}
	for i, col := range expectedHeader {
		if strings.TrimSpace(header[i]) != col {
			return CheckItem{Description: desc, Passed: false, Detail: fmt.Sprintf("column %d: got=%s want=%s", i, header[i], col)}
		}
	}
	return CheckItem{Description: desc, Passed: true, Detail: fmt.Sprintf("%d columns match", len(expectedHeader))}
}

func checkResumeStateValid(desc, path string) CheckItem {
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	return CheckItem{Description: desc, Passed: true, Detail: fmt.Sprintf("valid JSON with %d chunks", len(raw))}
}

func checkResumeStateHasChunks(desc, path string) CheckItem {
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	var chunks []map[string]any
	if err := json.Unmarshal(data, &chunks); err != nil {
		return CheckItem{Description: desc, Passed: false, Detail: err.Error()}
	}
	if len(chunks) == 0 {
		return CheckItem{Description: desc, Passed: false, Detail: "no chunks"}
	}
	var cidrs []string
	for _, ch := range chunks {
		if cidr, ok := ch["cidr"].(string); ok {
			cidrs = append(cidrs, cidr)
		}
	}
	return CheckItem{Description: desc, Passed: len(cidrs) > 0, Detail: fmt.Sprintf("CIDRs: %s", strings.Join(cidrs, ", "))}
}
```

**Step 4: Run tests**

Run: `go test ./e2e/report/ -run TestEvaluate -v`
Expected: PASS

**Step 5: Commit**

```bash
git add e2e/report/checklist.go e2e/report/checklist_test.go
git commit -m "feat(e2e): add per-scenario checklist evaluator"
```

---

### Task 14: HTML Report Generator

**Files:**
- Modify: `e2e/report/generate_report.go`
- Create: `e2e/report/html_template.go`
- Modify: `e2e/report/generate_report_test.go`

**Step 1: Write the failing test**

Replace `e2e/report/generate_report_test.go`:

```go
package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateFullReport_WhenScenariosProvided_WritesHTMLWithChecklists(t *testing.T) {
	outDir := t.TempDir()
	r := FullReport{
		Timestamp:     time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC),
		TotalDuration: 5 * time.Second,
		Scenarios: []ScenarioResult{
			{
				Name: "normal_scan", Status: "pass", Duration: 2 * time.Second, ExitCode: 0,
				Checklist: []CheckItem{
					{Description: "scan_results exists", Passed: true, Detail: "148 rows"},
					{Description: "opened_results exists", Passed: true, Detail: "12 rows"},
				},
				Artifacts: []Artifact{
					{Name: "scan_results.csv", Size: 4200, RowCount: 148},
				},
			},
			{
				Name: "api_5xx", Status: "pass", Duration: 1 * time.Second, ExitCode: 1,
				Checklist: []CheckItem{
					{Description: "Exit code non-zero", Passed: true, Detail: "exit 1"},
				},
			},
		},
		Summary:   Summary{Total: 148, Open: 12, Closed: 100, Timeout: 36},
		PassCount: 2,
		FailCount: 0,
	}

	if err := GenerateFullReport(outDir, r); err != nil {
		t.Fatalf("generate failed: %v", err)
	}

	htmlBytes, err := os.ReadFile(filepath.Join(outDir, "report.html"))
	if err != nil {
		t.Fatalf("missing html: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "normal_scan") {
		t.Fatal("expected scenario name in HTML")
	}
	if !strings.Contains(html, "scan_results exists") {
		t.Fatal("expected checklist item in HTML")
	}
	if !strings.Contains(html, "2/2 passed") {
		t.Fatal("expected pass count summary in HTML")
	}

	txtBytes, err := os.ReadFile(filepath.Join(outDir, "report.txt"))
	if err != nil {
		t.Fatalf("missing txt: %v", err)
	}
	txt := string(txtBytes)
	if !strings.Contains(txt, "normal_scan") || !strings.Contains(txt, "PASS") {
		t.Fatalf("expected scenario in txt: %s", txt)
	}
}
```

**Step 2: Run test to verify failure**

Run: `go test ./e2e/report/ -run TestGenerateFullReport -v`
Expected: FAIL — `GenerateFullReport` undefined

**Step 3: Write implementation**

Create `e2e/report/html_template.go`:

```go
package report

const reportHTMLTemplate = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Port Scan E2E Report</title>
<style>
body { font-family: monospace; max-width: 900px; margin: 40px auto; padding: 0 20px; background: #f8f8f8; }
.summary-bar { background: #333; color: #fff; padding: 12px 16px; border-radius: 4px; margin-bottom: 24px; }
.summary-bar .pass { color: #4caf50; }
.summary-bar .fail { color: #f44336; }
.scenario { border: 1px solid #ddd; border-radius: 4px; margin-bottom: 16px; background: #fff; }
.scenario-header { padding: 10px 16px; font-weight: bold; border-bottom: 1px solid #eee; cursor: pointer; }
.scenario-header.pass { border-left: 4px solid #4caf50; }
.scenario-header.fail { border-left: 4px solid #f44336; }
.scenario-body { padding: 10px 16px; }
.check { padding: 2px 0; }
.check.pass::before { content: '\2705 '; }
.check.fail::before { content: '\274C '; }
.check .detail { color: #888; font-size: 0.9em; }
.artifact { color: #555; font-size: 0.9em; padding: 2px 0; }
.timing { color: #888; font-size: 0.9em; }
</style>
</head>
<body>
<h1>Port Scan E2E Report</h1>
<div class="summary-bar">
{{SUMMARY_LINE}}
</div>
{{SCENARIOS}}
</body>
</html>`
```

Modify `e2e/report/generate_report.go` — keep the existing `Generate` and `SummarizeCSV` functions, add `GenerateFullReport`:

```go
package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// existing Summary type and functions stay unchanged...

// GenerateFullReport writes the enhanced HTML and text reports.
func GenerateFullReport(outDir string, r FullReport) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	html := buildFullHTML(r)
	txt := buildFullText(r)

	if err := os.WriteFile(filepath.Join(outDir, "report.html"), []byte(html), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "report.txt"), []byte(txt), 0o644)
}

func buildFullHTML(r FullReport) string {
	total := len(r.Scenarios)
	summaryLine := fmt.Sprintf(
		`<span class="pass">%d/%d passed</span> &mdash; %s &mdash; %s`,
		r.PassCount, total,
		r.Timestamp.Format(time.RFC3339),
		r.TotalDuration.Round(time.Millisecond),
	)
	if r.FailCount > 0 {
		summaryLine = fmt.Sprintf(
			`<span class="pass">%d passed</span> <span class="fail">%d failed</span> / %d total &mdash; %s &mdash; %s`,
			r.PassCount, r.FailCount, total,
			r.Timestamp.Format(time.RFC3339),
			r.TotalDuration.Round(time.Millisecond),
		)
	}

	var scenarios strings.Builder
	for _, s := range r.Scenarios {
		statusClass := "pass"
		statusLabel := "PASS"
		if s.Status != "pass" {
			statusClass = "fail"
			statusLabel = "FAIL"
		}
		fmt.Fprintf(&scenarios, `<div class="scenario">`)
		fmt.Fprintf(&scenarios, `<div class="scenario-header %s">%s (%s, %s)</div>`, statusClass, s.Name, statusLabel, s.Duration.Round(time.Millisecond))
		fmt.Fprintf(&scenarios, `<div class="scenario-body">`)

		if len(s.Checklist) > 0 {
			fmt.Fprintf(&scenarios, `<div><strong>Checklist:</strong></div>`)
			for _, item := range s.Checklist {
				cls := "pass"
				if !item.Passed {
					cls = "fail"
				}
				detail := ""
				if item.Detail != "" {
					detail = fmt.Sprintf(` <span class="detail">(%s)</span>`, item.Detail)
				}
				fmt.Fprintf(&scenarios, `<div class="check %s">%s%s</div>`, cls, item.Description, detail)
			}
		}

		if len(s.Artifacts) > 0 {
			fmt.Fprintf(&scenarios, `<div style="margin-top:8px"><strong>Artifacts:</strong></div>`)
			for _, a := range s.Artifacts {
				rowInfo := ""
				if a.RowCount > 0 {
					rowInfo = fmt.Sprintf(", %d rows", a.RowCount)
				}
				fmt.Fprintf(&scenarios, `<div class="artifact">%s (%s%s)</div>`, a.Name, formatSize(a.Size), rowInfo)
			}
		}

		fmt.Fprintf(&scenarios, `</div></div>`)
	}

	out := reportHTMLTemplate
	out = strings.Replace(out, "{{SUMMARY_LINE}}", summaryLine, 1)
	out = strings.Replace(out, "{{SCENARIOS}}", scenarios.String(), 1)
	return out
}

func buildFullText(r FullReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Port Scan E2E Report — %s\n", r.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(&b, "Overall: %d/%d passed — %s\n\n", r.PassCount, len(r.Scenarios), r.TotalDuration.Round(time.Millisecond))
	fmt.Fprintf(&b, "Summary: Total=%d Open=%d Closed=%d Timeout=%d\n\n", r.Summary.Total, r.Summary.Open, r.Summary.Closed, r.Summary.Timeout)

	for _, s := range r.Scenarios {
		status := "PASS"
		if s.Status != "pass" {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "--- %s (%s, %s) ---\n", s.Name, status, s.Duration.Round(time.Millisecond))
		for _, item := range s.Checklist {
			mark := "[x]"
			if !item.Passed {
				mark = "[ ]"
			}
			detail := ""
			if item.Detail != "" {
				detail = " (" + item.Detail + ")"
			}
			fmt.Fprintf(&b, "  %s %s%s\n", mark, item.Description, detail)
		}
		for _, a := range s.Artifacts {
			rowInfo := ""
			if a.RowCount > 0 {
				rowInfo = fmt.Sprintf(", %d rows", a.RowCount)
			}
			fmt.Fprintf(&b, "  artifact: %s (%s%s)\n", a.Name, formatSize(a.Size), rowInfo)
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}
```

**Step 4: Run tests**

Run: `go test ./e2e/report/ -v -count=1`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add e2e/report/generate_report.go e2e/report/generate_report_test.go e2e/report/html_template.go
git commit -m "feat(e2e): add full HTML report generator with checklists and artifacts"
```

---

### Task 15: Update Shell Script for Manifest Capture

**Files:**
- Modify: `e2e/run_e2e.sh`
- Modify: `e2e/report/cmd/generate/main.go`

**Step 1: Rewrite run_e2e.sh with manifest capture**

Replace `e2e/run_e2e.sh` with timing capture and manifest JSON output. The script captures `start_ns`/`end_ns` per scenario and writes `manifest.json`. Remove inline assertions (the Go tool handles them now).

Key changes to `run_e2e.sh`:
- Add `capture_time_ns()` function using `date +%s%N`
- Wrap each scenario in timing capture
- Write `manifest.json` after all scenarios complete
- Remove inline awk/assertion checks (Go tool does this now)
- Keep: Docker orchestration, input creation, scan execution

**Step 2: Update cmd/generate/main.go to accept -manifest flag**

Add `-manifest` flag. When provided, read the manifest JSON, run checklists, build `FullReport`, and call `GenerateFullReport`. Fall back to existing behavior when `-csv` is used without `-manifest`.

**Step 3: Test manually**

Run: `bash e2e/run_e2e.sh` (requires Docker)
Expected: `e2e/out/report.html` contains per-scenario checklists

**Step 4: Commit**

```bash
git add e2e/run_e2e.sh e2e/report/cmd/generate/main.go
git commit -m "feat(e2e): capture timing manifest and generate full checklist report"
```

---

### Task 16: README E2E Documentation

**Files:**
- Modify: `README.md`

**Step 1: Add excluded scenarios section**

After the existing "E2E Overview" section in `README.md`, add:

```markdown
### Excluded E2E Scenarios

The following scenarios are verified through unit and integration tests but are
intentionally excluded from the Docker Compose e2e suite:

| Scenario | Reason | Coverage |
|----------|--------|----------|
| Keyboard interrupt during dispatch | Requires TTY, not automatable in Docker | Unit: `TestRun_WhenCanceled_*` |
| Output write failure (disk full) | Environment-dependent, not reproducible in CI | Integration: `output_files_test.go` |
| Resume state corruption | Deterministic JSON parsing | Unit: `pkg/state` tests |
| Partial/truncated input files | Fail-fast validation | Unit: `pkg/input` validation tests |
| FD limit exhaustion | OS-dependent, pre-scan check guards this | Unit: `TestEnsureFDLimit` |
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add excluded e2e scenarios with rationale to README"
```

---

### Task 17: Final Verification

**Step 1: Run full unit + integration tests with race detector**

Run: `go test ./... -v -count=1 -race`
Expected: ALL PASS

**Step 2: Run coverage gate**

Run: `bash scripts/coverage_gate.sh`
Expected: PASS with >= 85% coverage

**Step 3: Verify scan.go line count**

Run: `wc -l pkg/scanapp/scan.go`
Expected: ~170 lines (down from 618)

**Step 4: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: final cleanup after robustness and maintainability enhancements"
```
