# IP-Aware Full Spec Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 完整實作 `plan/design.md` 與新增需求：`ip/ip_cidr` 欄位模型、欄位名稱指定解析、`opened_results.csv`、Docker e2e 含 API 正常/異常場景。

**Architecture:** 以現有 `pkg/scanapp` 為核心，擴充 `pkg/config`、`pkg/input`、`pkg/task`、`pkg/writer` 與 `e2e/`。掃描分組仍用 `ip_cidr`，但任務只由 `ip` 展開；輸出採雙 sink（all/open-only）。API 壓力控制維持 OR-gate，嚴格實作 3 次連續失敗 Fatal 規則。

**Tech Stack:** Go 1.24+, standard library net/context/csv/json/signal, Docker Compose, go test + coverage gate.

---

**Required Skills:** @test-driven-development @systematic-debugging @verification-before-completion @requesting-code-review @finishing-a-development-branch

**Constitution Gates:**
- 測試先行（紅燈確認）後才寫實作
- CLI 與 library 同步維持可測可用
- Unit + Integration + e2e 全部通過
- coverage gate > 85%

### Task 1: Config 新旗標與參數約束

**Files:**
- Modify: `pkg/config/config.go`
- Modify: `pkg/config/config_test.go`

**Step 1: Write the failing test**

```go
func TestParseConfig_CIDRColumnFlags(t *testing.T) {
    cfg, err := Parse([]string{
        "-cidr-file", "cidr.csv",
        "-port-file", "ports.csv",
        "-cidr-ip-col", "source_ip",
        "-cidr-ip-cidr-col", "source_cidr",
    })
    if err != nil {
        t.Fatalf("unexpected err: %v", err)
    }
    if cfg.CIDRIPCol != "source_ip" || cfg.CIDRIPCidrCol != "source_cidr" {
        t.Fatalf("unexpected cols: %#v", cfg)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/config -run TestParseConfig_CIDRColumnFlags -v`
Expected: FAIL with missing fields/flags.

**Step 3: Request approval before implementation**

Run: 貼紅燈結果請使用者核准。
Expected: 使用者核准後繼續。

**Step 4: Write minimal implementation**

```go
type Config struct {
    // ...existing fields...
    CIDRIPCol     string
    CIDRIPCidrCol string
}

fs.StringVar(&cfg.CIDRIPCol, "cidr-ip-col", "ip", "cidr csv ip column")
fs.StringVar(&cfg.CIDRIPCidrCol, "cidr-ip-cidr-col", "ip_cidr", "cidr csv ip_cidr column")
if strings.TrimSpace(cfg.CIDRIPCol) == "" || strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
    return Config{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/config -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/config/config.go pkg/config/config_test.go
git commit -m "feat(config): add named cidr ip column flags"
```

### Task 2: Input Parser 改為欄位名稱解析

**Files:**
- Modify: `pkg/input/types.go`
- Modify: `pkg/input/cidr.go`
- Add: `pkg/input/cidr_columns_test.go`

**Step 1: Write the failing test**

```go
func TestLoadCIDRs_WithNamedColumns(t *testing.T) {
    csv := "foo,source_ip,bar,source_cidr\n" +
        "x,10.0.0.1,y,10.0.0.0/24\n"
    rows, err := LoadCIDRsWithColumns(strings.NewReader(csv), "source_ip", "source_cidr")
    if err != nil { t.Fatalf("unexpected err: %v", err) }
    if len(rows) != 1 || rows[0].IPRaw != "10.0.0.1" || rows[0].IPCidrRaw != "10.0.0.0/24" {
        t.Fatalf("unexpected rows: %#v", rows)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/input -run TestLoadCIDRs_WithNamedColumns -v`
Expected: FAIL with undefined symbol.

**Step 3: Request approval before implementation**

Run: 提交紅燈輸出。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
type CIDRRow struct {
    FabName   string
    IPRaw     string
    IPCidrRaw string
    IPNet     *net.IPNet
}

func LoadCIDRsWithColumns(r io.Reader, ipCol, ipCidrCol string) ([]CIDRRow, error) {
    // parse header indexes by name and map rows
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/input -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/input/types.go pkg/input/cidr.go pkg/input/cidr_columns_test.go
git commit -m "feat(input): parse cidr csv by named ip columns"
```

### Task 3: Fail-Fast 驗證矩陣（ip/ip_cidr）

**Files:**
- Modify: `pkg/input/validate.go`
- Add: `pkg/input/validate_ip_rules_test.go`

**Step 1: Write the failing test**

```go
func TestValidateRows_DuplicateIPIPCidrPairFatal(t *testing.T) {
    rows := []CIDRRow{{IPRaw:"10.0.0.1", IPCidrRaw:"10.0.0.0/24"}, {IPRaw:"10.0.0.1", IPCidrRaw:"10.0.0.0/24"}}
    err := ValidateIPRows(rows)
    if err == nil { t.Fatal("expected duplicate pair error") }
}

func TestValidateRows_IPNotInsideIPCidrFatal(t *testing.T) {
    rows := []CIDRRow{{IPRaw:"10.1.0.1", IPCidrRaw:"10.0.0.0/24"}}
    err := ValidateIPRows(rows)
    if err == nil { t.Fatal("expected containment error") }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/input -run TestValidateRows_DuplicateIPIPCidrPairFatal -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 貼紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
func ValidateIPRows(rows []CIDRRow) error {
    // enforce:
    // 1) duplicate (ip,ip_cidr) fatal
    // 2) ip expansion must be contained by ip_cidr
    // 3) different ip_cidr overlap fatal
    // 4) overlap within same ip_cidr from different ip selectors fatal
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/input -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/input/validate.go pkg/input/validate_ip_rules_test.go
git commit -m "feat(input): enforce ip/ip_cidr fail-fast validation rules"
```

### Task 4: Task 展開改為只掃描 ip 欄位列出的目標

**Files:**
- Modify: `pkg/task/ipv4.go`
- Add: `pkg/task/selector_expand.go`
- Add: `pkg/task/selector_expand_test.go`

**Step 1: Write the failing test**

```go
func TestExpandSelectors_OnlyListedTargets(t *testing.T) {
    sels := []string{"10.0.0.1", "10.0.0.8/30"}
    got, err := ExpandIPSelectors(sels)
    if err != nil { t.Fatal(err) }
    if len(got) != 5 { t.Fatalf("expected 5 targets, got %d", len(got)) }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/task -run TestExpandSelectors_OnlyListedTargets -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 提交紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
func ExpandIPSelectors(selectors []string) ([]string, error) {
    // selector can be single IP or CIDR
    // expand to unique sorted IP strings
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/task -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/task/ipv4.go pkg/task/selector_expand.go pkg/task/selector_expand_test.go
git commit -m "feat(task): expand ip selectors for subset scanning"
```

### Task 5: CSV 輸出加入 ip_cidr 與 open-only sink

**Files:**
- Modify: `pkg/writer/csv_writer.go`
- Modify: `pkg/writer/csv_writer_test.go`
- Add: `pkg/writer/open_writer.go`
- Add: `pkg/writer/open_writer_test.go`

**Step 1: Write the failing test**

```go
func TestOpenWriter_WritesOnlyOpenRows(t *testing.T) {
    buf := &bytes.Buffer{}
    w := NewOpenOnlyWriter(NewCSVWriter(buf))
    _ = w.Write(Record{IP:"10.0.0.1", IPCidr:"10.0.0.0/24", Port:80, Status:"open"})
    _ = w.Write(Record{IP:"10.0.0.2", IPCidr:"10.0.0.0/24", Port:80, Status:"close"})
    out := buf.String()
    if strings.Count(out, "\n") != 2 { t.Fatalf("expected header+1 row, got: %s", out) }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/writer -run TestOpenWriter_WritesOnlyOpenRows -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 貼紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
type Record struct {
    IP string
    IPCidr string
    Port int
    Status string
    ResponseMS int64
    FabName string
    CIDRName string
}

type OpenOnlyWriter struct { inner *CSVWriter }
func (w *OpenOnlyWriter) Write(r Record) error {
    if r.Status != "open" { return nil }
    return w.inner.Write(r)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/writer -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/writer/*.go
git commit -m "feat(writer): add ip_cidr field and opened-only csv sink"
```

### Task 6: scanapp 整合 ip 子集掃描與 opened_results.csv

**Files:**
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/scan_test.go`
- Modify: `cmd/port-scan/main_scan_test.go`

**Step 1: Write the failing test**

```go
func TestRun_WritesOpenedResultsCSV(t *testing.T) {
    // setup open + closed targets
    // run scanapp
    // assert scan_results.csv exists
    // assert opened_results.csv exists in output dir
    // assert opened_results only has open rows
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run TestRun_WritesOpenedResultsCSV -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 貼紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
allWriter := writer.NewCSVWriter(scanResultsFile)
openWriter := writer.NewOpenOnlyWriter(writer.NewCSVWriter(openedResultsFile))
// each result: write allWriter then openWriter
// openedResultsFile path = filepath.Join(filepath.Dir(cfg.Output), "opened_results.csv")
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/scanapp ./cmd/port-scan -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/scanapp/scan.go pkg/scanapp/scan_test.go cmd/port-scan/main_scan_test.go
git commit -m "feat(scanapp): output opened_results.csv in output directory"
```

### Task 7: API 失敗策略完整化（1/2 error, 3 fatal）

**Files:**
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/scan_test.go`

**Step 1: Write the failing test**

```go
func TestPollPressureAPI_ThirdFailureFatal(t *testing.T) {
    // mock api always 500
    // assert first two are logged error
    // third pushes fatal error channel
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run TestPollPressureAPI_ThirdFailureFatal -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 提交紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
if err != nil {
    consecutiveFailures++
    if consecutiveFailures <= 2 { logger.errorf(...); continue }
    errCh <- fmt.Errorf("pressure api failed 3 times: %w", err)
    return
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/scanapp -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/scanapp/scan.go pkg/scanapp/scan_test.go
git commit -m "feat(scanapp): enforce pressure api 3-strike fatal policy"
```

### Task 8: CLI `validate` 路徑支援新欄位旗標

**Files:**
- Modify: `cmd/port-scan/main.go`
- Modify: `cmd/port-scan/main_test.go`

**Step 1: Write the failing test**

```go
func TestMainValidate_UsesCustomIPColumns(t *testing.T) {
    // csv header uses source_ip/source_cidr
    // validate with -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr
    // expect success
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan -run TestMainValidate_UsesCustomIPColumns -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 貼紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
// validateInputs uses LoadCIDRsWithColumns(..., cfg.CIDRIPCol, cfg.CIDRIPCidrCol)
```

**Step 5: Run test to verify it passes**

Run: `go test ./cmd/port-scan -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add cmd/port-scan/main.go cmd/port-scan/main_test.go
git commit -m "feat(cli): validate path supports custom ip/ip_cidr column names"
```

### Task 9: Docker e2e 納入 API server 正常/異常兩大類

**Files:**
- Modify: `e2e/docker-compose.yml`
- Modify: `e2e/run_e2e.sh`
- Add/Modify: `e2e/api/` (normal/5xx/timeout behavior)
- Modify: `e2e/report/generate_report.go`
- Modify: `e2e/report/cmd/generate/main.go`

**Step 1: Write the failing test**

```go
func TestSummarizeCSV_FromRealScanResults(t *testing.T) {
    // parse generated scan csv and assert totals/status counts
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./e2e/report -run TestSummarizeCSV_FromRealScanResults -v`
Expected: FAIL.

**Step 3: Request approval before implementation**

Run: 貼紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```bash
# run_e2e.sh scenarios
# A) API normal => scan success + output assertions
# B) API 5xx => expect non-zero exit + resume_state.json
# C) API timeout/conn fail => expect non-zero exit + resume_state.json
# all via docker compose only (no skip path)
```

**Step 5: Run test to verify it passes**

Run:
- `go test ./e2e/report ./e2e/report/cmd/generate -v`
- `bash e2e/run_e2e.sh`

Expected:
- report tests PASS
- all e2e scenarios pass with explicit assertions

**Step 6: Commit**

```bash
git add e2e/
git commit -m "test(e2e): add pressure api normal/5xx/timeout scenario coverage"
```

### Task 10: 文件與最終驗證

**Files:**
- Modify: `README.md`
- Modify: `docs/release-notes/1.0.0.md`

**Step 1: Write the failing test**

```go
func TestCLIHelp_IncludesNewCIDRColumnFlags(t *testing.T) {
    out, code := runMain([]string{"--help"})
    if code != 0 { t.Fatalf("unexpected code: %d", code) }
    for _, f := range []string{"-cidr-ip-col", "-cidr-ip-cidr-col"} {
        if !strings.Contains(out, f) { t.Fatalf("missing %s", f) }
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan -run TestCLIHelp_IncludesNewCIDRColumnFlags -v`
Expected: FAIL until help text updated.

**Step 3: Request approval before implementation**

Run: 提交紅燈。
Expected: 核准。

**Step 4: Write minimal implementation**

```go
func usage(w io.Writer) {
    // include -cidr-ip-col and -cidr-ip-cidr-col in help text
}
```

**Step 5: Run test to verify it passes**

Run:
- `go test ./...`
- `bash scripts/coverage_gate.sh`
- `bash e2e/run_e2e.sh`

Expected:
- all PASS
- coverage > 85%
- docker e2e all scenarios PASS

**Step 6: Commit**

```bash
git add README.md docs/release-notes/1.0.0.md cmd/port-scan/main.go cmd/port-scan/main_extra_test.go
git commit -m "docs: finalize ip-aware scan behavior and verification guide"
```

## Final Verification Checklist

1. `go test ./...` must pass.
2. `bash scripts/coverage_gate.sh` must pass with total > 85%.
3. `bash e2e/run_e2e.sh` must pass all required scenarios:
   - API normal
   - API HTTP 5xx (fatal after 3 failures)
   - API timeout/connection failure (fatal after 3 failures)
4. `scan_results.csv` and `opened_results.csv` both generated; open file contains only `open` rows.
5. Resume path on cancel/fatal writes `resume_state.json` and `-resume` path can continue.

## Notes for Executor

- 每一個任務都要保留 Red-Green-Refactor 證據。
- 發現測試 flakiness 時，先用 @systematic-debugging 做最小重現。
- 每個 Task 完成後執行 @requesting-code-review。
- 全部完成後使用 @finishing-a-development-branch 收尾。
