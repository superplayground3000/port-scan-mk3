# Speed Control Testing Framework Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立可驗證 Global speed control 與 CIDR speed control 的測試框架，並輸出具完整文字解釋的人類可讀報告。

**Architecture:** 採 Hybrid 分層：先在 integration 層收集 deterministic telemetry，再由 analyzer 進行規則判定，最後由 report generator 輸出 `report.md` / `report.html` / `raw_metrics.json`。核心邏輯放在 `internal/testkit/speedcontrol` 與 `e2e/report`，CLI 保持組裝角色。

**Tech Stack:** Go 1.24.x、Go standard library、既有 `pkg/scanapp` / `pkg/speedctrl` / `pkg/ratelimit`、既有 `e2e/report`

---

### Task 1: 建立 Speed Control Telemetry 型別與 Collector

**Files:**
- Create: `internal/testkit/speedcontrol/types.go`
- Create: `internal/testkit/speedcontrol/collector.go`
- Create: `internal/testkit/speedcontrol/collector_test.go`

**Step 1: Write the failing test**

在 `collector_test.go` 新增：

```go
func TestCollector_WhenEventsRecorded_KeepsOrderAndFields(t *testing.T) {
	c := NewCollector("G1")
	c.Record(Event{Kind: EventGateWaitStart, CIDR: "10.0.0.0/24", TaskIndex: 0, TimestampNS: 100})
	c.Record(Event{Kind: EventTaskEnqueued, CIDR: "10.0.0.0/24", TaskIndex: 0, TimestampNS: 120})

	got := c.Events()
	if len(got) != 2 || got[0].Kind != EventGateWaitStart || got[1].Kind != EventTaskEnqueued {
		t.Fatalf("unexpected events: %#v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/testkit/speedcontrol -run TestCollector_WhenEventsRecorded_KeepsOrderAndFields -v`

Expected: FAIL (`undefined: NewCollector`, `undefined: Event`)

**Step 3: Write minimal implementation**

在 `types.go` 定義 `EventKind`、`Event`、`ScenarioMetrics`；在 `collector.go` 實作 thread-safe `Collector`：

```go
type Collector struct {
	mu       sync.Mutex
	scenario string
	events   []Event
}
```

提供：
- `NewCollector(scenario string) *Collector`
- `Record(e Event)`
- `Events() []Event`（回傳 copy）

**Step 4: Run tests to verify pass**

Run: `go test ./internal/testkit/speedcontrol -v -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/testkit/speedcontrol/types.go internal/testkit/speedcontrol/collector.go internal/testkit/speedcontrol/collector_test.go
git commit -m "test: add speed control telemetry collector primitives"
```

### Task 2: 在 dispatch 流程注入 test observer（不影響 production 行為）

**Files:**
- Modify: `pkg/scanapp/task_dispatcher.go`
- Create: `pkg/scanapp/dispatch_observer.go`
- Modify: `pkg/scanapp/scan_test.go`

**Step 1: Write the failing test**

在 `scan_test.go` 新增測試，驗證 dispatch 會觸發順序事件：
- `gate_wait_start`
- `gate_released`
- `bucket_wait_start`
- `bucket_acquired`
- `task_enqueued`

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run TestDispatchTasks_WhenObserverInjected_EmitsOrderedEvents -v`

Expected: FAIL（尚無 observer 注入點）

**Step 3: Write minimal implementation**

新增 `dispatchObserver` 介面（預設 no-op）：

```go
type dispatchObserver interface {
	OnGateWaitStart(cidr string, taskIndex int)
	OnGateReleased(cidr string, taskIndex int)
	OnBucketWaitStart(cidr string, taskIndex int)
	OnBucketAcquired(cidr string, taskIndex int)
	OnTaskEnqueued(cidr string, taskIndex int)
}
```

在 `dispatchTasks` 中每個關鍵點呼叫 observer。未提供 observer 時使用 no-op，避免污染 production code path。

**Step 4: Run tests to verify pass**

Run: `go test ./pkg/scanapp -run TestDispatchTasks_WhenObserverInjected_EmitsOrderedEvents -v -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/dispatch_observer.go pkg/scanapp/task_dispatcher.go pkg/scanapp/scan_test.go
git commit -m "test: add dispatch observer hooks for speed control telemetry"
```

### Task 3: 實作 Analyzer（Global/CIDR 判定規則 + 可解釋原因）

**Files:**
- Create: `internal/testkit/speedcontrol/analyzer.go`
- Create: `internal/testkit/speedcontrol/analyzer_test.go`

**Step 1: Write the failing tests**

新增至少三類測試：
1. Global gate pause 時新任務不得 enqueue。
2. CIDR steady-state tps 與預期值誤差在容忍範圍內。
3. 報告說明文字包含預期/實測/歸因。

**Step 2: Run tests to verify fail**

Run: `go test ./internal/testkit/speedcontrol -run TestAnalyze -v`

Expected: FAIL（尚無 `Analyze`）

**Step 3: Write minimal implementation**

在 `analyzer.go` 實作：

```go
type RuleExpectation struct {
	ExpectedTPS float64
	Tolerance   float64
}

type ScenarioVerdict struct {
	Name        string
	Pass        bool
	Expected    string
	Observed    string
	Attribution string
	Explanation string
}

func Analyze(events []Event, expectation RuleExpectation) ScenarioVerdict
```

核心邏輯：
- 以 `task_enqueued` 間隔推估實測 TPS
- 套用 `min(bucket_rate, 1/delay, workers/avg_task_seconds)` 產生 expected reference
- 超出容忍帶則 fail 並標註 probable bottleneck

**Step 4: Run tests to verify pass**

Run: `go test ./internal/testkit/speedcontrol -v -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add internal/testkit/speedcontrol/analyzer.go internal/testkit/speedcontrol/analyzer_test.go
git commit -m "test: add speed control analyzer with explainable verdicts"
```

### Task 4: 建立 Global/CIDR/Combined Integration Scenarios

**Files:**
- Create: `tests/integration/speedcontrol_global_test.go`
- Create: `tests/integration/speedcontrol_cidr_test.go`
- Create: `tests/integration/speedcontrol_combined_test.go`
- Create: `tests/integration/testdata/speedcontrol/cidr_small.csv`
- Create: `tests/integration/testdata/speedcontrol/ports_small.csv`

**Step 1: Write failing integration tests**

情境最小集合：
1. G1 manual pause gate
2. G3 OR-gate correctness
3. C1 steady rate
4. C2 burst then steady
5. X1 global pause during cidr-limited dispatch

每個測試都應輸出 `ScenarioVerdict` 並 assert `Pass == true`。

**Step 2: Run tests to verify fail**

Run: `go test ./tests/integration -run 'SpeedControl|speedcontrol' -v`

Expected: FAIL（缺 harness 或 analyzer 串接）

**Step 3: Write minimal implementation**

在測試中使用：
- `scanapp` 現有 runtime 建構流程
- Task 2 observer 收集事件
- Task 3 analyzer 判定結果

將 `expected` 與 `observed` 一起寫入測試 log，便於 CI 除錯。

**Step 4: Run tests to verify pass**

Run: `go test ./tests/integration -run 'SpeedControl|speedcontrol' -v -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add tests/integration/speedcontrol_global_test.go tests/integration/speedcontrol_cidr_test.go tests/integration/speedcontrol_combined_test.go tests/integration/testdata/speedcontrol/cidr_small.csv tests/integration/testdata/speedcontrol/ports_small.csv
git commit -m "test: add integration scenario matrix for global and cidr speed control"
```

### Task 5: 擴充 e2e/report 產出可讀 speed-control 報告

**Files:**
- Create: `e2e/report/speedcontrol_generate.go`
- Create: `e2e/report/speedcontrol_generate_test.go`
- Create: `e2e/report/speedcontrol_template.html`
- Modify: `e2e/report/types.go`

**Step 1: Write failing report tests**

測試 `GenerateSpeedControlReport` 後，必須同時產出：
- `report.md`
- `report.html`
- `raw_metrics.json`

且 `report.md` 需包含：
- `Expected`
- `Observed`
- `Verdict`
- `Explanation`

**Step 2: Run tests to verify fail**

Run: `go test ./e2e/report -run SpeedControl -v`

Expected: FAIL（函式未實作）

**Step 3: Write minimal implementation**

實作：

```go
func GenerateSpeedControlReport(outDir string, scenarios []ScenarioVerdict, raw any) error
```

流程：
1. 寫 `raw_metrics.json`
2. 組 Markdown（summary + per scenario deep dive）
3. 套 HTML template 輸出 `report.html`

**Step 4: Run tests to verify pass**

Run: `go test ./e2e/report -run SpeedControl -v -count=1`

Expected: PASS

**Step 5: Commit**

```bash
git add e2e/report/speedcontrol_generate.go e2e/report/speedcontrol_generate_test.go e2e/report/speedcontrol_template.html e2e/report/types.go
git commit -m "feat: generate human-readable speed control verification reports"
```

### Task 6: 新增 speed-control e2e runner 與文件

**Files:**
- Create: `e2e/speedcontrol/run_speedcontrol_e2e.sh`
- Create: `docs/e2e/speedcontrol.md`
- Modify: `docs/e2e/overview.md`
- Modify: `README.md`

**Step 1: Write failing script smoke test**

新增簡易 smoke test（或 Go test）驗證 script 會輸出：
- `e2e/out/speedcontrol/report.md`
- `e2e/out/speedcontrol/report.html`

**Step 2: Run smoke test to verify fail**

Run: `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`

Expected: FAIL（腳本尚未存在）

**Step 3: Write minimal implementation**

腳本流程：
1. 準備 input/輸出資料夾
2. 執行 speedcontrol integration matrix
3. 生成 raw metrics
4. 呼叫 report generator 產出 md/html/json

**Step 4: Run full verification**

Run:
- `go test ./internal/testkit/speedcontrol ./pkg/scanapp ./tests/integration ./e2e/report -count=1`
- `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`

Expected:
- 測試全綠
- `e2e/out/speedcontrol/` 產出完整三種報告檔

**Step 5: Commit**

```bash
git add e2e/speedcontrol/run_speedcontrol_e2e.sh docs/e2e/speedcontrol.md docs/e2e/overview.md README.md
git commit -m "docs: add speed control verification workflow and report artifacts"
```

### Task 7: 最終驗證與交付證據

**Files:**
- Create: `specs/999-ip-aware-baseline-spec/verification/speedcontrol-framework-verification.md`

**Step 1: Run final gate commands**

Run:
- `go test ./... -count=1`
- `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`

Expected: 全部 PASS

**Step 2: Capture evidence**

在驗證文件記錄：
- 測試命令
- 測試時間
- 報告檔案路徑
- 關鍵結果摘要

**Step 3: Commit**

```bash
git add specs/999-ip-aware-baseline-spec/verification/speedcontrol-framework-verification.md
git commit -m "test: add speed control verification evidence"
```

