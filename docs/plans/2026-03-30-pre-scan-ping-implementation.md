# Pre-Scan Ping Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 為 `scan` 增加預設啟用的 pre-scan ping；若 IP 在 ping 階段不可達，先完整落檔 `unreachable_results-<suffix>.csv`，再進入 TCP scanning，且流程必須支援 Windows、resume 相容與 `-disable-pre-scan-ping` 關閉開關。

**Architecture:** 掃描流程改成兩個明確 stage。Stage 1 先收斂唯一目標 IP、以受控併發做跨平台 ping、寫入並 finalize `unreachable_results`，同時產生可 resume 的 pre-ping 狀態；Stage 2 才以 reachable predicate 建立 runtime/chunks、打開 scan/opened outputs 並執行既有 TCP pipeline。resume 以新的 state envelope 保存 `chunks + pre_scan_ping`，舊 `[]task.Chunk` 格式仍可讀。

**Tech Stack:** Go 1.24.x、Go standard library（`context`, `os/exec`, `encoding/json`, `encoding/csv`, `sort`, `runtime`, `time`）、既有 `pkg/config` / `pkg/scanapp` / `pkg/state` / `pkg/writer`

---

## File Structure (Lock Before Tasks)

- Create: `pkg/writer/unreachable_writer.go`
  - 單一責任：輸出 `unreachable_results` CSV 與 header contract
- Create: `pkg/writer/unreachable_writer_test.go`
  - 單一責任：驗證 unreachable writer header / row contract
- Create: `pkg/scanapp/reachability.go`
  - 單一責任：定義 `ReachabilityChecker`、result type、ping command builder
- Create: `pkg/scanapp/reachability_test.go`
  - 單一責任：驗證 Windows / non-Windows command path 與 checker 行為
- Create: `pkg/scanapp/pre_scan_ping.go`
  - 單一責任：pre-scan ping orchestration、唯一 IP 收斂、unreachable row 聚合、predicate 建構
- Create: `pkg/scanapp/pre_scan_ping_test.go`
  - 單一責任：驗證 pre-scan barrier、row 聚合、dedup、resume reuse
- Modify: `pkg/config/config.go`
  - 單一責任：新增 `-disable-pre-scan-ping`
- Modify: `pkg/config/config_test.go`
  - 單一責任：驗證 flag default 與 disable 行為
- Modify: `pkg/state/state.go`
  - 單一責任：引入 resume envelope 並保留 legacy `[]task.Chunk` 相容
- Modify: `pkg/state/state_test.go`
  - 單一責任：驗證 envelope round-trip 與 legacy load
- Modify: `pkg/state/state_extra_test.go`
  - 單一責任：補 envelope error-path 測試
- Modify: `pkg/scanapp/batch_output.go`
  - 單一責任：一次配置 scan/open/unreachable 三個 batch path，共用同一 suffix
- Modify: `pkg/scanapp/output_files.go`
  - 單一責任：拆出 pre-scan unreachable output 與 TCP scan outputs 的兩段 lifecycle
- Modify: `pkg/scanapp/output_files_test.go`
  - 單一責任：驗證 unreachable finalize barrier 與 scan/open finalize 行為
- Modify: `pkg/scanapp/group_builder.go`
  - 單一責任：讓 group building 接受 reachable predicate，不把 ping 邏輯塞進 builder
- Modify: `pkg/scanapp/chunk_lifecycle.go`
  - 單一責任：讓 chunks/runtime 按 reachable targets 計數與建構
- Modify: `pkg/scanapp/runtime_builder.go`
  - 單一責任：把 output path allocation 從 run plan 拆開，保留 runtime planning 專責
- Modify: `pkg/scanapp/resume_manager.go`
  - 單一責任：保存新的 state envelope
- Modify: `pkg/scanapp/scan.go`
  - 單一責任：串接 pre-scan stage、hard barrier、resume reuse、TCP scan stage
- Modify: `pkg/scanapp/scan_test.go`
  - 單一責任：驗證 end-to-end stage barrier、resume、disabled mode、all unreachable 成功路徑
- Modify: `cmd/port-scan/main_scan_test.go`
  - 單一責任：驗證 CLI disable flag 與 scan contract 不被破壞
- Modify: `docs/cli/flags.md`
  - 單一責任：文件化 `-disable-pre-scan-ping`
- Modify: `docs/cli/scenarios.md`
  - 單一責任：新增 pre-scan ping / unreachable output 場景與 Windows 說明

### Task 1: 加入 CLI toggle 並鎖定預設行為

**Files:**
- Modify: `pkg/config/config.go`
- Modify: `pkg/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

在 `config_test.go` 新增：

```go
func TestParseConfig_WhenRequiredFlagsProvided_PreScanPingIsEnabledByDefault(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisablePreScanPing {
		t.Fatal("expected pre-scan ping enabled by default")
	}
}

func TestParseConfig_WhenDisablePreScanPingFlagProvided_TurnsFeatureOff(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv", "-disable-pre-scan-ping=true"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DisablePreScanPing {
		t.Fatal("expected disable-pre-scan-ping to be true")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/config -run 'TestParseConfig_WhenRequiredFlagsProvided_PreScanPingIsEnabledByDefault|TestParseConfig_WhenDisablePreScanPingFlagProvided_TurnsFeatureOff' -v`
Expected: FAIL (`DisablePreScanPing` field / flag not found)

- [ ] **Step 3: Write minimal implementation**

在 `config.go`：

```go
type Config struct {
    // ...
    DisablePreScanPing bool
}

fs.BoolVar(&cfg.DisablePreScanPing, "disable-pre-scan-ping", false, "disable pre-scan ping")
```

- [ ] **Step 4: Run package tests**

Run: `go test ./pkg/config -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/config/config.go pkg/config/config_test.go
git commit -m "feat(config): add pre-scan ping toggle"
```

### Task 2: 建立 unreachable writer 與 shared batch path allocation

**Files:**
- Create: `pkg/writer/unreachable_writer.go`
- Create: `pkg/writer/unreachable_writer_test.go`
- Modify: `pkg/scanapp/batch_output.go`
- Modify: `pkg/scanapp/output_files.go`
- Modify: `pkg/scanapp/output_files_test.go`

- [ ] **Step 1: Write the failing tests**

新增測試覆蓋：
- `resolveBatchOutputPaths()` 回傳 `scan/opened/unreachable` 三個 path，且 suffix 相同
- unreachable writer 會輸出固定 header
- pre-scan unreachable output finalize 成功時 rename `.tmp -> final`
- 若 unreachable finalize 失敗，scan/open outputs 不應被打開

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/writer ./pkg/scanapp -run 'Test(UnreachableWriter_|ResolveBatchOutputPaths_|UnreachableOutput_)' -v -count=1`
Expected: FAIL（writer/type/function 尚未存在）

- [ ] **Step 3: Write minimal implementation**

在 `pkg/writer/unreachable_writer.go`：

```go
type UnreachableRecord struct {
    IP                string
    IPCidr            string
    Status            string
    Reason            string
    FabName           string
    CIDRName          string
    ServiceLabel      string
    Decision          string
    PolicyID          string
    ExecutionKey      string
    SrcIP             string
    SrcNetworkSegment string
}

type UnreachableWriter struct { /* csv writer wrapper */ }
```

在 `pkg/scanapp`：

```go
type batchOutputPaths struct {
    scanPath        string
    openPath        string
    unreachablePath string
}
```

- [ ] **Step 4: Run focused tests**

Run: `go test ./pkg/writer ./pkg/scanapp -run 'Test(UnreachableWriter_|ResolveBatchOutputPaths_|UnreachableOutput_)' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/writer/unreachable_writer.go pkg/writer/unreachable_writer_test.go pkg/scanapp/batch_output.go pkg/scanapp/output_files.go pkg/scanapp/output_files_test.go
git commit -m "feat(scanapp): add unreachable batch output plumbing"
```

### Task 3: 導入可回溯的 resume envelope

**Files:**
- Modify: `pkg/state/state.go`
- Modify: `pkg/state/state_test.go`
- Modify: `pkg/state/state_extra_test.go`

- [ ] **Step 1: Write the failing tests**

新增測試：

```go
func TestSaveAndLoadSnapshot_WhenPreScanPingStatePresent_RoundTrips(t *testing.T) {
    snap := Snapshot{
        Chunks: []task.Chunk{{CIDR: "10.0.0.0/24", TotalCount: 2}},
        PreScanPing: PreScanPingState{
            Enabled: true,
            TimeoutMS: 100,
            UnreachableIPv4U32: []uint32{167772167, 167772168},
        },
    }
    // save + load, assert sorted set preserved
}

func TestLoadSnapshot_WhenLegacyChunkArrayProvided_PreservesCompatibility(t *testing.T) {
    // write raw []task.Chunk JSON and verify loader upgrades it into Snapshot
}
```

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/state -run 'Test(SaveAndLoadSnapshot_|LoadSnapshot_)' -v -count=1`
Expected: FAIL（Snapshot / PreScanPingState / LoadSnapshot 尚未存在）

- [ ] **Step 3: Write minimal implementation**

在 `state.go`：

```go
type PreScanPingState struct {
    Enabled            bool     `json:"enabled"`
    TimeoutMS          int      `json:"timeout_ms"`
    UnreachableIPv4U32 []uint32 `json:"unreachable_ipv4_u32,omitempty"`
}

type Snapshot struct {
    Chunks      []task.Chunk      `json:"chunks"`
    PreScanPing PreScanPingState  `json:"pre_scan_ping,omitempty"`
}

func SaveSnapshot(path string, snap Snapshot) error
func LoadSnapshot(path string) (Snapshot, error)
```

保留既有 `Save/Load` 作為 legacy wrapper，避免一次把所有 caller 打碎。

- [ ] **Step 4: Run package tests**

Run: `go test ./pkg/state -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/state/state.go pkg/state/state_test.go pkg/state/state_extra_test.go
git commit -m "feat(state): add resume envelope for pre-scan ping"
```

### Task 4: 實作跨平台 reachability checker

**Files:**
- Create: `pkg/scanapp/reachability.go`
- Create: `pkg/scanapp/reachability_test.go`

- [ ] **Step 1: Write the failing tests**

新增測試覆蓋：
- `buildPingCommand("windows", "10.0.0.7")` 產生 Windows count/timeout flags
- `buildPingCommand("linux", "10.0.0.7")` 與 `buildPingCommand("darwin", "10.0.0.7")` 走 non-Windows 路徑
- checker 在 runner 回傳 `nil` 時標示 reachable
- checker 在 deadline / exit error 時標示 unreachable

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'Test(BuildPingCommand_|CommandReachabilityChecker_)' -v -count=1`
Expected: FAIL（builder/checker 尚未存在）

- [ ] **Step 3: Write minimal implementation**

在 `reachability.go`：

```go
type ReachabilityResult struct {
    IP          string
    Reachable   bool
    FailureText string
}

type ReachabilityChecker interface {
    Check(ctx context.Context, ip string, timeout time.Duration) ReachabilityResult
}

func buildPingCommand(goos, ip string, timeout time.Duration) (string, []string, error)
```

`Check()` 用 `context.WithTimeout` 包住 command execution，避免把 100ms 契約綁死在不同平台的 CLI flag 細節上。

- [ ] **Step 4: Run focused tests**

Run: `go test ./pkg/scanapp -run 'Test(BuildPingCommand_|CommandReachabilityChecker_)' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/reachability.go pkg/scanapp/reachability_test.go
git commit -m "feat(scanapp): add cross-platform reachability checker"
```

### Task 5: 建立 pre-scan ping orchestration 與 reachable predicate

**Files:**
- Create: `pkg/scanapp/pre_scan_ping.go`
- Create: `pkg/scanapp/pre_scan_ping_test.go`
- Modify: `pkg/scanapp/group_builder.go`
- Modify: `pkg/scanapp/chunk_lifecycle.go`

- [ ] **Step 1: Write the failing tests**

新增測試覆蓋：
- 同一 IP 只 ping 一次
- unreachable row 依 scan context 聚合，不依 port 展開
- reachable predicate 會讓 `buildCIDRGroups` / `buildRichGroups` 跳過 unreachable targets
- rich metadata 仍以 `|`-joined distinct values` 聚合

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp -run 'Test(PreScanPing_|BuildCIDRGroups_|BuildRichGroups_)' -v -count=1`
Expected: FAIL（pre-scan orchestration / predicate hooks 尚未存在）

- [ ] **Step 3: Write minimal implementation**

在 `pre_scan_ping.go` 建立：

```go
type preScanOutcome struct {
    State                state.PreScanPingState
    UnreachableIPv4U32   []uint32
    UnreachableRows      []writer.UnreachableRecord
}

func runPreScanPing(ctx context.Context, inputs runInputs, cfg config.Config, checker ReachabilityChecker, paths batchOutputPaths, saved state.PreScanPingState) (preScanOutcome, error)
func reachablePredicate(sortedUnreachable []uint32) func(string) bool
```

在 group builder / chunk lifecycle 導入 predicate 參數，而不是把 ping 決策塞進 builder 內部。

- [ ] **Step 4: Run focused tests**

Run: `go test ./pkg/scanapp -run 'Test(PreScanPing_|BuildCIDRGroups_|BuildRichGroups_)' -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/scanapp/pre_scan_ping.go pkg/scanapp/pre_scan_ping_test.go pkg/scanapp/group_builder.go pkg/scanapp/chunk_lifecycle.go
git commit -m "feat(scanapp): add pre-scan reachability filtering"
```

### Task 6: 把 hard barrier 與 resume reuse 串進 `Run`

**Files:**
- Modify: `pkg/scanapp/runtime_builder.go`
- Modify: `pkg/scanapp/resume_manager.go`
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/scan_test.go`
- Modify: `cmd/port-scan/main_scan_test.go`

- [ ] **Step 1: Write the failing tests**

新增或調整測試：
- 第一次 TCP dial 發生前，`unreachable_results-*.csv` 已存在且為 final path，不是 `.tmp`
- resume 若已有 `pre_scan_ping` state，第二次 run 不重跑 checker
- `-disable-pre-scan-ping=true` 時完全不走 pre-scan stage
- all-unreachable run 仍成功，scan/opened 只有 header，unreachable 有資料

可在 `scan_test.go` 注入：
- fake checker
- spy dialer
- first-dial hook（檢查 unreachable final file 是否已落地）

- [ ] **Step 2: Run tests to verify fail**

Run: `go test ./pkg/scanapp ./cmd/port-scan -run 'Test(Run_|RunMain_)' -v -count=1`
Expected: FAIL（barrier / resume reuse / disable path 尚未接通）

- [ ] **Step 3: Write minimal implementation**

在 `scan.go` 重排為：

```go
// 1. load inputs
// 2. resolve shared batch paths
// 3. load resume snapshot (if any)
// 4. run or reuse pre-scan ping
// 5. finalize unreachable_results
// 6. build runtime plan with reachable predicate
// 7. open scan/opened outputs
// 8. run TCP scanning
// 9. persist snapshot on failure/cancel
```

同時把 `prepareRunPlan()` 從 output path allocation 中拆乾淨，避免 pre-scan barrier 再次被混回 runtime planning。

- [ ] **Step 4: Run package tests**

Run: `go test ./pkg/scanapp ./cmd/port-scan -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `go test ./... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/scanapp/runtime_builder.go pkg/scanapp/resume_manager.go pkg/scanapp/scan.go pkg/scanapp/scan_test.go cmd/port-scan/main_scan_test.go
git commit -m "feat(scanapp): enforce pre-ping output barrier before tcp scan"
```

### Task 7: 更新 CLI 文件

**Files:**
- Modify: `docs/cli/flags.md`
- Modify: `docs/cli/scenarios.md`

- [ ] **Step 1: Write doc assertions as failing review checklist**

先列出必須出現在文件中的三點：
- `-disable-pre-scan-ping`
- `unreachable_results-*.csv` 與 pre-scan barrier 行為
- Windows 相容與 timeout 契約說明

- [ ] **Step 2: Update docs minimally**

在 `flags.md` 增加 flag table row 與 behavior notes。  
在 `scenarios.md` 新增一個 pre-scan ping 場景，說明：
- reachable/unreachable outputs
- disabled mode
- Windows 與 Unix 高層行為一致

- [ ] **Step 3: Sanity-check docs**

Run: `rg -n "disable-pre-scan-ping|unreachable_results|Windows" docs/cli/flags.md docs/cli/scenarios.md`
Expected: 皆有命中

- [ ] **Step 4: Commit**

```bash
git add docs/cli/flags.md docs/cli/scenarios.md
git commit -m "docs(cli): document pre-scan ping behavior"
```

## Final Verification Gate

- [ ] Run: `go test ./... -count=1`
- [ ] Confirm: Windows-specific command-path tests pass without requiring Windows host networking
- [ ] Confirm: at least one test proves `unreachable_results` final file exists before first TCP dial
- [ ] Confirm: legacy `[]task.Chunk` resume files still load
- [ ] Confirm: disabled mode preserves prior scan behavior

## Notes for Execution

- Follow @/Users/xuxiping/.codex/superpowers/skills/test-driven-development/SKILL.md strictly: no production code before a failing test.
- Keep commits small and aligned to tasks above; do not batch Task 2-6 into one commit.
- Do not introduce ping logic into `cmd/port-scan`; CLI only parses flags and wires dependencies.
- Prefer injected fakes/spies in `pkg/scanapp` tests over real `ping` execution.
