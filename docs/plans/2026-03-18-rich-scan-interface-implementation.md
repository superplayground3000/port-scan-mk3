# Rich Scan Interface Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `scan` 執行期間提供預設啟用的終端 rich dashboard，顯示 progress、current CIDR/bucket status、dispatch/results 速率、controller 狀態與 pressure API 狀態，且不破壞既有 CLI 契約。

**Architecture:** 以 `pkg/scanapp` 內的「state + observer + renderer + loop」組合實作。掃描流程只發送事件，dashboard 自行聚合快照並每 500ms 刷新到 `stderr`。啟用條件為 `TTY(stderr) && format=human`，其餘情況自動退回既有輸出。

**Tech Stack:** Go 1.24.x、Go standard library（`sync`, `time`, `io`, `fmt`）、既有 `pkg/scanapp` / `pkg/speedctrl`、`golang.org/x/term`

---

## File Structure (Lock Before Tasks)

- Create: `pkg/scanapp/dashboard_state.go`
  - 單一責任：保存並計算 dashboard runtime snapshot（thread-safe）
- Create: `pkg/scanapp/dashboard_state_test.go`
  - 單一責任：驗證 state 更新、controller status mapping、速率計算
- Create: `pkg/scanapp/dashboard_renderer.go`
  - 單一責任：把 snapshot 渲染成 ANSI 覆蓋畫面字串
- Create: `pkg/scanapp/dashboard_renderer_test.go`
  - 單一責任：驗證渲染欄位與 ANSI 刷新控制碼
- Create: `pkg/scanapp/dashboard_runtime.go`
  - 單一責任：dashboard 啟停生命週期、ticker loop、observer 綁定
- Create: `pkg/scanapp/dashboard_runtime_test.go`
  - 單一責任：啟用條件與 loop 行為測試
- Modify: `pkg/scanapp/scan.go`
  - 單一責任：在 `Run` 組裝 dashboard（不承擔渲染細節）
- Modify: `pkg/scanapp/task_dispatcher.go`
  - 單一責任：沿用 observer hook，接 dashboard observer
- Modify: `pkg/scanapp/pressure_monitor.go`
  - 單一責任：增加 pressure telemetry callback（sample/failure）
- Modify: `pkg/speedctrl/controller.go`
  - 單一責任：提供 API pause 狀態讀取（consumer-owned accessor）
- Modify: `pkg/speedctrl/controller_test.go`
  - 單一責任：驗證新增 accessor 行為
- Modify: `pkg/scanapp/scan_observability_test.go`
  - 單一責任：驗證 rich 啟用/停用條件與 fallback 契約
- Modify: `README.md`
  - 單一責任：補充 rich dashboard 預設行為與 fallback 說明
- Modify: `docs/cli/scenarios.md`
  - 單一責任：新增 rich dashboard 可視化場景

### Task 1: 補齊 Controller API 狀態可觀測性

**Files:**
- Modify: `pkg/speedctrl/controller.go`
- Modify: `pkg/speedctrl/controller_test.go`

- [ ] **Step 1: Write the failing test**

在 `controller_test.go` 新增：

```go
func TestController_APIPausedAccessor_ReflectsLatestState(t *testing.T) {
	c := NewController()
	if c.APIPaused() {
		t.Fatal("expected api paused false initially")
	}
	c.SetAPIPaused(true)
	if !c.APIPaused() {
		t.Fatal("expected api paused true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/speedctrl -run TestController_APIPausedAccessor_ReflectsLatestState -v`
Expected: FAIL (`c.APIPaused undefined`)

- [ ] **Step 3: Write minimal implementation**

在 `controller.go` 加入：

```go
func (c *Controller) APIPaused() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPaused
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/speedctrl -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/speedctrl/controller.go pkg/speedctrl/controller_test.go
git commit -m "feat(speedctrl): expose api paused accessor for runtime observers"
```

### Task 2: 建立 Dashboard State（progress/cidr/bucket/speed/controller/api）

**Files:**
- Create: `pkg/scanapp/dashboard_state.go`
- Create: `pkg/scanapp/dashboard_state_test.go`

- [ ] **Step 1: Write failing tests**

新增測試覆蓋：
- progress 更新（`ScannedTasks/TotalTasks/Percent`）
- current CIDR + bucket status 轉換
- controller status mapping（四種狀態）
- 5 秒視窗內 `dispatch/sec` 與 `results/sec` 計算
- API health（`ok` / `fail streak N`）與 last update

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'TestDashboardState_' -v`
Expected: FAIL（type/function 尚未存在）

- [ ] **Step 3: Write minimal implementation**

在 `dashboard_state.go` 建立：
- `type dashboardState struct { ... }`
- `func newDashboardState(total int, now func() time.Time) *dashboardState`
- `func (s *dashboardState) OnTaskEnqueued(cidr string)`
- `func (s *dashboardState) OnResult()`
- `func (s *dashboardState) OnBucketStatus(cidr, status string)`
- `func (s *dashboardState) OnController(manualPaused, apiPaused bool)`
- `func (s *dashboardState) OnPressureSample(pressure int, t time.Time)`
- `func (s *dashboardState) OnPressureFailure(streak int, t time.Time)`
- `func (s *dashboardState) Snapshot() dashboardSnapshot`

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/scanapp -run 'TestDashboardState_' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/dashboard_state.go pkg/scanapp/dashboard_state_test.go
git commit -m "feat(scanapp): add thread-safe rich dashboard state model"
```

### Task 3: 建立 Renderer（ANSI 覆蓋刷新到 stderr）

**Files:**
- Create: `pkg/scanapp/dashboard_renderer.go`
- Create: `pkg/scanapp/dashboard_renderer_test.go`

- [ ] **Step 1: Write failing tests**

新增測試驗證 `Render(snapshot)` 內容包含：
- `Progress`
- `Current CIDR`
- `Bucket`
- `Dispatch/s`、`Results/s`
- `Controller`
- `API Pressure`
- `Last Update`
- `Health`

並驗證 output 含 ANSI 覆蓋控制碼（例如清屏/游標歸位）。

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'TestDashboardRenderer_' -v`
Expected: FAIL（renderer 尚未實作）

- [ ] **Step 3: Write minimal implementation**

實作：

```go
type dashboardRenderer struct {}
func (r dashboardRenderer) Render(w io.Writer, snap dashboardSnapshot) error
```

輸出固定欄位順序，避免每次刷新欄位跳動。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/scanapp -run 'TestDashboardRenderer_' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/dashboard_renderer.go pkg/scanapp/dashboard_renderer_test.go
git commit -m "feat(scanapp): add ansi rich dashboard renderer"
```

### Task 4: 在 scan runtime 掛載 dashboard lifecycle（TTY + human 才啟用）

**Files:**
- Create: `pkg/scanapp/dashboard_runtime.go`
- Create: `pkg/scanapp/dashboard_runtime_test.go`
- Modify: `pkg/scanapp/scan.go`

- [ ] **Step 1: Write failing tests**

新增測試覆蓋：
- `format=json` 時不啟 dashboard
- non-TTY `stderr` 時不啟 dashboard
- TTY + human 時啟 dashboard 並定時刷新

測試可透過 `RunOptions` 注入：
- terminal detector（可控 true/false）
- dashboard refresh interval（縮短至 10ms）

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'TestDashboardRuntime_|TestRun_WhenRichDashboard' -v`
Expected: FAIL（尚無 lifecycle 與注入點）

- [ ] **Step 3: Write minimal implementation**

在 `scan.go` 與 `dashboard_runtime.go`：
- 建立 `shouldEnableDashboard(cfg, stderr, opts)`
- 啟動 ticker loop（預設 500ms）
- loop 只寫 `stderr`
- run 結束時關閉 dashboard loop

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/scanapp -run 'TestDashboardRuntime_|TestRun_WhenRichDashboard' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/dashboard_runtime.go pkg/scanapp/dashboard_runtime_test.go pkg/scanapp/scan.go
git commit -m "feat(scanapp): wire rich dashboard lifecycle into scan run"
```

### Task 5: 串接 dispatch/result/pressure 事件到 dashboard state

**Files:**
- Modify: `pkg/scanapp/task_dispatcher.go`
- Modify: `pkg/scanapp/result_aggregator.go`
- Modify: `pkg/scanapp/pressure_monitor.go`
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/scan_observability_test.go`

- [ ] **Step 1: Write failing tests**

新增/調整測試驗證：
- dispatch observer 事件可更新 current CIDR + bucket status
- result 事件可更新 `results/sec` 與 progress
- pressure sample/failure 可更新 API 區塊（pressure、last update、health/fail streak）

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'TestRun_WhenRichDashboard|TestPollPressureAPI_|TestDispatchTasks_WhenObserverInjected' -v`
Expected: FAIL（dashboard telemetry 尚未接通）

- [ ] **Step 3: Write minimal implementation**

- 建立 dashboard observer，對接既有 `dispatchObserver` hook
- 在 result 處理路徑呼叫 `OnResult`
- 在 pressure polling 成功/失敗路徑發送 telemetry callback
- 每次 loop render 前同步 controller（manual/api paused）

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./pkg/scanapp -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/task_dispatcher.go pkg/scanapp/result_aggregator.go pkg/scanapp/pressure_monitor.go pkg/scanapp/scan.go pkg/scanapp/scan_observability_test.go
git commit -m "feat(scanapp): connect dispatch result and pressure telemetry to rich dashboard"
```

### Task 6: 文件同步與契約驗證

**Files:**
- Modify: `README.md`
- Modify: `docs/cli/scenarios.md`

- [ ] **Step 1: Write failing doc assertions (lightweight)**

以 `rg` 檢查關鍵字存在，先驗證目前尚未記錄 rich dashboard 行為。

- [ ] **Step 2: Run check to verify fail**

Run: `rg -n "rich dashboard|TTY|format=json|stderr" README.md docs/cli/scenarios.md`
Expected: 無完整說明或匹配不足

- [ ] **Step 3: Write minimal docs update**

- README 新增 rich dashboard 預設行為與 fallback 條件
- scenarios 新增一節示範 TTY rich dashboard 觀察重點

- [ ] **Step 4: Run check to verify pass**

Run: `rg -n "rich dashboard|TTY|format=json|stderr" README.md docs/cli/scenarios.md`
Expected: 命中新增段落

- [ ] **Step 5: Commit**

```bash
git add README.md docs/cli/scenarios.md
git commit -m "docs: document rich dashboard behavior and fallback rules"
```

### Task 7: 全量驗證與整體提交

**Files:**
- Modify: `pkg/scanapp/*`
- Modify: `pkg/speedctrl/*`
- Modify: docs files above

- [ ] **Step 1: Run focused package tests**

Run: `go test ./pkg/speedctrl ./pkg/scanapp -count=1`
Expected: PASS

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 3: Optional coverage gate (if touched behavior is broad)**

Run: `bash scripts/coverage_gate.sh`
Expected: PASS（>= 專案門檻）

- [ ] **Step 4: Final commit (if any remaining staged changes)**

```bash
git add -A
git commit -m "feat(scanapp): add tty rich scan dashboard with safe fallback"
```

- [ ] **Step 5: Prepare handoff notes**

產出變更摘要（啟用條件、欄位、fallback、測試證據），並附上關鍵測試指令結果。

## Guardrails During Execution

- 不變更 public flag contract（本次不新增 `-ui` / `-ui-refresh`）
- 不改 CSV output schema、exit code、resume 邏輯
- `cmd/port-scan` 維持組裝角色；核心邏輯放 `pkg/`
- rich dashboard failure 不可中止 scan 主流程
- 僅在 `stderr` TTY + human 模式啟用 rich，其他模式完全 fallback

