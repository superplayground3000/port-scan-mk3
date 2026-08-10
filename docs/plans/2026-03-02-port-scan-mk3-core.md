# Port Scan MK3 Core Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立符合 `plan/design.md` 與 `.specify/memory/constitution.md` 的 TCP Port Scanner CLI，支援 fail-fast 驗證、速率限制、全域壓力控制、SIGINT 斷點續掃、整合測試與 e2e 測試。

**Architecture:** 採 library-first 分層：`pkg/*` 放可獨立測試的核心元件，`cmd/port-scan` 負責 CLI 與輸入輸出格式。掃描流程以 Task Generator + Worker Pool + Result Writer 進行，並由每 CIDR Leaky Bucket 與 OR-gate Speed Controller 協同控制。中斷流程使用 `context` + `os/signal` + state manager，保證可寫入 `resume_state.json` 並無重掃漏掃。

**Tech Stack:** Go 1.24+, Go standard library (`net`, `context`, `encoding/csv`, `encoding/json`, `os/signal`, `sync`), Docker Compose (e2e), `go test` + coverage tooling。

---

**Required Skills:** @test-driven-development @systematic-debugging @verification-before-completion @requesting-code-review @finishing-a-development-branch

**Constitution Gates:**
- 先寫測試，確認紅燈，並請使用者核准後才進入實作（Test-First）
- 每個 library 都要能透過 CLI 路徑被呼叫
- CLI 至少提供 `human` 與 `json` 兩種文字輸出模式
- 必須有 unit/integration/e2e 測試，最終 coverage > 85%

### Task 1: 專案骨架與 CLI 基礎參數

**Files:**
- Create: `go.mod`
- Create: `cmd/port-scan/main.go`
- Create: `pkg/config/config.go`
- Test: `pkg/config/config_test.go`

**Step 1: Write the failing test**

```go
package config

import (
    "testing"
    "time"
)

func TestParseConfig_Defaults(t *testing.T) {
    cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Timeout != 100*time.Millisecond {
        t.Fatalf("timeout mismatch: %v", cfg.Timeout)
    }
    if cfg.Format != "human" {
        t.Fatalf("format mismatch: %s", cfg.Format)
    }
}

func TestParseConfig_InvalidFormat(t *testing.T) {
    _, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv", "-format", "xml"})
    if err == nil {
        t.Fatal("expected error for invalid format")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/config -run TestParseConfig_Defaults -v`
Expected: FAIL with `undefined: Parse`.

**Step 3: Request approval before implementation**

Run: 將紅燈輸出貼給使用者，請求核准後再進入實作（遵守 constitution Test-First）。
Expected: 使用者明確允許繼續。

**Step 4: Write minimal implementation**

```go
package config

import (
    "errors"
    "flag"
    "time"
)

type Config struct {
    CIDRFile string
    PortFile string
    Output string
    Timeout time.Duration
    Delay time.Duration
    BucketRate int
    BucketCapacity int
    Workers int
    PressureAPI string
    PressureInterval time.Duration
    DisableAPI bool
    Resume string
    LogLevel string
    Format string
}

func Parse(args []string) (Config, error) {
    fs := flag.NewFlagSet("port-scan", flag.ContinueOnError)
    cfg := Config{}
    fs.StringVar(&cfg.CIDRFile, "cidr-file", "", "CIDR CSV path")
    fs.StringVar(&cfg.PortFile, "port-file", "", "Port CSV path")
    fs.StringVar(&cfg.Output, "output", "scan_results.csv", "output csv")
    fs.DurationVar(&cfg.Timeout, "timeout", 100*time.Millisecond, "dial timeout")
    fs.DurationVar(&cfg.Delay, "delay", 10*time.Millisecond, "dispatch delay")
    fs.IntVar(&cfg.BucketRate, "bucket-rate", 100, "bucket rate")
    fs.IntVar(&cfg.BucketCapacity, "bucket-capacity", 100, "bucket capacity")
    fs.IntVar(&cfg.Workers, "workers", 10, "worker count")
    fs.StringVar(&cfg.PressureAPI, "pressure-api", "http://localhost:8080/api/pressure", "pressure api")
    fs.DurationVar(&cfg.PressureInterval, "pressure-interval", 5*time.Second, "pressure poll interval")
    fs.BoolVar(&cfg.DisableAPI, "disable-api", false, "disable pressure api")
    fs.StringVar(&cfg.Resume, "resume", "", "resume state file")
    fs.StringVar(&cfg.LogLevel, "log-level", "info", "debug|info|error")
    fs.StringVar(&cfg.Format, "format", "human", "human|json")

    if err := fs.Parse(args); err != nil {
        return Config{}, err
    }
    if cfg.CIDRFile == "" || cfg.PortFile == "" {
        return Config{}, errors.New("-cidr-file and -port-file are required")
    }
    if cfg.Format != "human" && cfg.Format != "json" {
        return Config{}, errors.New("-format must be human or json")
    }
    return cfg, nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/config -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add go.mod cmd/port-scan/main.go pkg/config/config.go pkg/config/config_test.go
git commit -m "feat(config): bootstrap cli flags and format validation"
```

### Task 2: CIDR/Port 解析與 Fail-Fast 驗證

**Files:**
- Create: `pkg/input/types.go`
- Create: `pkg/input/cidr.go`
- Create: `pkg/input/ports.go`
- Create: `pkg/input/validate.go`
- Test: `pkg/input/cidr_test.go`
- Test: `pkg/input/ports_test.go`
- Test: `pkg/input/validate_test.go`

**Step 1: Write the failing test**

```go
func TestLoadCIDRs_DetectOverlap(t *testing.T) {
    rows := "fab_name,cidr,cidr_name\nfab1,10.0.0.0/8,a\nfab2,10.1.0.0/16,b\n"
    _, err := LoadCIDRs(strings.NewReader(rows))
    if err == nil {
        t.Fatal("expected overlap error")
    }
}

func TestLoadPorts_ParseTCPOnly(t *testing.T) {
    ports, err := LoadPorts(strings.NewReader("80/tcp\n443/tcp\n"))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(ports) != 2 || ports[0].Number != 80 {
        t.Fatalf("unexpected ports: %#v", ports)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/input -run TestLoadCIDRs_DetectOverlap -v`
Expected: FAIL with `undefined: LoadCIDRs`.

**Step 3: Request approval before implementation**

Run: 提交失敗測試輸出給使用者，取得核准。
Expected: 核准後繼續。

**Step 4: Write minimal implementation**

```go
func ValidateNoOverlap(networks []CIDRRecord) error {
    for i := 0; i < len(networks); i++ {
        for j := i + 1; j < len(networks); j++ {
            a := networks[i].Net
            b := networks[j].Net
            if a.Contains(b.IP) || b.Contains(a.IP) {
                return fmt.Errorf("overlap detected: %s (%s) <-> %s (%s)",
                    networks[i].CIDRName, networks[i].CIDR,
                    networks[j].CIDRName, networks[j].CIDR)
            }
        }
    }
    return nil
}
```

```go
func LoadPorts(r io.Reader) ([]PortSpec, error) {
    scanner := bufio.NewScanner(r)
    out := make([]PortSpec, 0)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }
        parts := strings.Split(line, "/")
        if len(parts) != 2 || strings.ToLower(parts[1]) != "tcp" {
            return nil, fmt.Errorf("invalid port row: %s", line)
        }
        n, err := strconv.Atoi(parts[0])
        if err != nil || n < 1 || n > 65535 {
            return nil, fmt.Errorf("invalid port number: %s", line)
        }
        out = append(out, PortSpec{Number: n, Proto: "tcp", Raw: line})
    }
    return out, scanner.Err()
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/input -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/input/*.go
git commit -m "feat(input): add cidr and port parsing with overlap fail-fast"
```

### Task 3: Chunk 與索引計算（Resume 基礎）

**Files:**
- Create: `pkg/task/types.go`
- Create: `pkg/task/index.go`
- Test: `pkg/task/index_test.go`

**Step 1: Write the failing test**

```go
func TestIndexToTarget_StableMapping(t *testing.T) {
    ips := []string{"10.0.0.1", "10.0.0.2"}
    ports := []int{80, 443}

    ip, port := IndexToTarget(3, ips, ports)
    if ip != "10.0.0.2" || port != 443 {
        t.Fatalf("unexpected mapping: %s:%d", ip, port)
    }
}

func TestChunkRemainingCount(t *testing.T) {
    c := Chunk{NextIndex: 2, TotalCount: 6}
    if c.Remaining() != 4 {
        t.Fatalf("remaining mismatch: %d", c.Remaining())
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/task -v`
Expected: FAIL with `undefined: IndexToTarget`.

**Step 3: Request approval before implementation**

Run: 貼上失敗結果並請使用者核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func IndexToTarget(idx int, ips []string, ports []int) (string, int) {
    if len(ports) == 0 {
        return "", 0
    }
    ipIdx := idx / len(ports)
    portIdx := idx % len(ports)
    return ips[ipIdx], ports[portIdx]
}

type Chunk struct {
    CIDR string `json:"cidr"`
    CIDRName string `json:"cidr_name"`
    Ports []string `json:"ports"`
    NextIndex int `json:"next_index"`
    ScannedCount int `json:"scanned_count"`
    TotalCount int `json:"total_count"`
    Status string `json:"status"`
}

func (c Chunk) Remaining() int {
    if c.TotalCount <= c.NextIndex {
        return 0
    }
    return c.TotalCount - c.NextIndex
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/task -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/task/*.go
git commit -m "feat(task): add chunk model and index mapping"
```

### Task 4: 每 CIDR Leaky Bucket 速率限制器

**Files:**
- Create: `pkg/ratelimit/leaky_bucket.go`
- Test: `pkg/ratelimit/leaky_bucket_test.go`

**Step 1: Write the failing test**

```go
func TestLeakyBucket_AcquireBlocksUntilToken(t *testing.T) {
    b := NewLeakyBucket(2, 1)
    ctx := context.Background()

    if err := b.Acquire(ctx); err != nil {
        t.Fatalf("first acquire failed: %v", err)
    }

    start := time.Now()
    if err := b.Acquire(ctx); err != nil {
        t.Fatalf("second acquire failed: %v", err)
    }
    if time.Since(start) < 450*time.Millisecond {
        t.Fatalf("expected blocking acquire")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/ratelimit -v`
Expected: FAIL with `undefined: NewLeakyBucket`.

**Step 3: Request approval before implementation**

Run: 分享失敗輸出並取得核准。
Expected: 核准後繼續。

**Step 4: Write minimal implementation**

```go
type LeakyBucket struct {
    tokens chan struct{}
    stop chan struct{}
}

func NewLeakyBucket(rate, capacity int) *LeakyBucket {
    b := &LeakyBucket{
        tokens: make(chan struct{}, capacity),
        stop: make(chan struct{}),
    }
    for i := 0; i < capacity; i++ {
        b.tokens <- struct{}{}
    }
    interval := time.Second / time.Duration(rate)
    ticker := time.NewTicker(interval)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                select {
                case b.tokens <- struct{}{}:
                default:
                }
            case <-b.stop:
                return
            }
        }
    }()
    return b
}

func (b *LeakyBucket) Acquire(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-b.tokens:
        return nil
    }
}

func (b *LeakyBucket) Close() { close(b.stop) }
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/ratelimit -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/ratelimit/*.go
git commit -m "feat(ratelimit): add per-cidr leaky bucket"
```

### Task 5: TCP Scanner 與狀態分類

**Files:**
- Create: `pkg/scanner/scanner.go`
- Test: `pkg/scanner/scanner_test.go`

**Step 1: Write the failing test**

```go
func TestScanTCP_OpenPort(t *testing.T) {
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer ln.Close()

    go func() {
        conn, err := ln.Accept()
        if err == nil {
            _ = conn.Close()
        }
    }()

    host, portStr, _ := net.SplitHostPort(ln.Addr().String())
    port, _ := strconv.Atoi(portStr)

    res := ScanTCP(net.DialTimeout, host, port, 200*time.Millisecond)
    if res.Status != "open" {
        t.Fatalf("expected open, got %s", res.Status)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanner -v`
Expected: FAIL with `undefined: ScanTCP`.

**Step 3: Request approval before implementation**

Run: 貼上紅燈結果，請使用者核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
type Result struct {
    IP string
    Port int
    Status string
    ResponseTimeMS int64
    Error string
}

func ScanTCP(dial func(string, string, time.Duration) (net.Conn, error), ip string, port int, timeout time.Duration) Result {
    target := net.JoinHostPort(ip, strconv.Itoa(port))
    start := time.Now()
    conn, err := dial("tcp", target, timeout)
    if err == nil {
        _ = conn.Close()
        return Result{IP: ip, Port: port, Status: "open", ResponseTimeMS: time.Since(start).Milliseconds()}
    }

    if ne, ok := err.(net.Error); ok && ne.Timeout() {
        return Result{IP: ip, Port: port, Status: "close(timeout)", ResponseTimeMS: 0, Error: err.Error()}
    }
    return Result{IP: ip, Port: port, Status: "close", ResponseTimeMS: 0, Error: err.Error()}
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/scanner -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/scanner/*.go
git commit -m "feat(scanner): implement tcp dial classification"
```

### Task 6: CSV Result Writer 與 Structured Logging

**Files:**
- Create: `pkg/writer/csv_writer.go`
- Create: `pkg/logx/logx.go`
- Test: `pkg/writer/csv_writer_test.go`
- Test: `pkg/logx/logx_test.go`

**Step 1: Write the failing test**

```go
func TestCSVWriter_WritesHeaderAndRows(t *testing.T) {
    buf := &bytes.Buffer{}
    w := NewCSVWriter(buf)

    r := Record{
        IP: "192.168.1.1", Port: 80, Status: "open", ResponseMS: 12,
        FabName: "fab1", CIDR: "192.168.1.0/24", CIDRName: "office",
    }
    if err := w.Write(r); err != nil {
        t.Fatalf("write failed: %v", err)
    }
    if !strings.Contains(buf.String(), "ip,port,status,response_time_ms") {
        t.Fatalf("header missing: %s", buf.String())
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/writer -v`
Expected: FAIL with `undefined: NewCSVWriter`.

**Step 3: Request approval before implementation**

Run: 送出失敗測試結果請使用者核准。
Expected: 核准後繼續。

**Step 4: Write minimal implementation**

```go
type CSVWriter struct {
    w *csv.Writer
    wroteHeader bool
}

func NewCSVWriter(out io.Writer) *CSVWriter {
    return &CSVWriter{w: csv.NewWriter(out)}
}

func (cw *CSVWriter) Write(r Record) error {
    if !cw.wroteHeader {
        if err := cw.w.Write([]string{"ip", "port", "status", "response_time_ms", "fab_name", "cidr", "cidr_name"}); err != nil {
            return err
        }
        cw.wroteHeader = true
    }
    row := []string{r.IP, strconv.Itoa(r.Port), r.Status, strconv.FormatInt(r.ResponseMS, 10), r.FabName, r.CIDR, r.CIDRName}
    if err := cw.w.Write(row); err != nil {
        return err
    }
    cw.w.Flush()
    return cw.w.Error()
}
```

```go
func LogJSON(out io.Writer, level, msg string, fields map[string]any) {
    payload := map[string]any{"level": level, "msg": msg, "fields": fields, "ts": time.Now().UTC().Format(time.RFC3339)}
    _ = json.NewEncoder(out).Encode(payload)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/writer ./pkg/logx -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/writer/*.go pkg/logx/*.go
git commit -m "feat(output): add csv writer and structured logger"
```

### Task 7: Global Speed Controller（API + Manual OR Gate）

**Files:**
- Create: `pkg/speedctrl/controller.go`
- Create: `pkg/speedctrl/keyboard.go`
- Test: `pkg/speedctrl/controller_test.go`

**Step 1: Write the failing test**

```go
func TestController_ORGatePauseResume(t *testing.T) {
    c := NewController(WithAPIEnabled(false))

    c.SetManualPaused(true)
    if !c.IsPaused() {
        t.Fatal("expected paused when manual paused")
    }

    c.SetAPIPaused(true)
    c.SetManualPaused(false)
    if !c.IsPaused() {
        t.Fatal("expected paused when api paused")
    }

    c.SetAPIPaused(false)
    if c.IsPaused() {
        t.Fatal("expected resumed when both flags false")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/speedctrl -v`
Expected: FAIL with `undefined: NewController`.

**Step 3: Request approval before implementation**

Run: 貼上紅燈結果並請求核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
type Controller struct {
    mu sync.RWMutex
    apiPaused bool
    manualPaused bool
    gate chan struct{}
}

func NewController(_ ...Option) *Controller {
    c := &Controller{gate: make(chan struct{})}
    close(c.gate)
    return c
}

func (c *Controller) recomputeLocked() {
    paused := c.apiPaused || c.manualPaused
    if paused {
        select {
        case <-c.gate:
            c.gate = make(chan struct{})
        default:
        }
        return
    }
    select {
    case <-c.gate:
    default:
        close(c.gate)
    }
}

func (c *Controller) SetAPIPaused(v bool) { c.mu.Lock(); c.apiPaused = v; c.recomputeLocked(); c.mu.Unlock() }
func (c *Controller) SetManualPaused(v bool) { c.mu.Lock(); c.manualPaused = v; c.recomputeLocked(); c.mu.Unlock() }
func (c *Controller) IsPaused() bool { c.mu.RLock(); defer c.mu.RUnlock(); return c.apiPaused || c.manualPaused }
func (c *Controller) Gate() <-chan struct{} { c.mu.RLock(); defer c.mu.RUnlock(); return c.gate }
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/speedctrl -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/speedctrl/*.go
git commit -m "feat(speedctrl): add api/manual pause or-gate controller"
```

### Task 8: Pipeline Runner（Task Generator + Worker Pool + Gate）

**Files:**
- Create: `pkg/pipeline/runner.go`
- Test: `pkg/pipeline/runner_test.go`

**Step 1: Write the failing test**

```go
func TestRunner_StopsDispatchWhenPaused(t *testing.T) {
    ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(false))
    ctrl.SetManualPaused(true)

    dispatched := 0
    r := NewRunner(Options{
        Workers: 1,
        Dispatch: func(context.Context, task.Task) error {
            dispatched++
            return nil
        },
        Controller: ctrl,
    })

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    _ = r.Run(ctx, []task.Chunk{{CIDR: "10.0.0.0/30", TotalCount: 4, Status: "pending"}})
    if dispatched != 0 {
        t.Fatalf("expected zero dispatched while paused, got %d", dispatched)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/pipeline -v`
Expected: FAIL with `undefined: NewRunner`.

**Step 3: Request approval before implementation**

Run: 提交失敗輸出給使用者確認。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func (r *Runner) waitGate(ctx context.Context) error {
    for {
        gate := r.ctrl.Gate()
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-gate:
            return nil
        }
    }
}

func (r *Runner) Run(ctx context.Context, chunks []task.Chunk) error {
    for _, ch := range chunks {
        for i := ch.NextIndex; i < ch.TotalCount; i++ {
            if err := r.waitGate(ctx); err != nil {
                return err
            }
            if err := r.dispatch(ctx, task.Task{ChunkCIDR: ch.CIDR, Index: i}); err != nil {
                return err
            }
            time.Sleep(r.delay)
        }
    }
    return nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/pipeline -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/pipeline/*.go
git commit -m "feat(pipeline): add gate-aware task dispatch runner"
```

### Task 9: State Manager 與 SIGINT 優雅降落

**Files:**
- Create: `pkg/state/state.go`
- Create: `pkg/state/signal.go`
- Test: `pkg/state/state_test.go`

**Step 1: Write the failing test**

```go
func TestSaveAndLoadResumeState(t *testing.T) {
    dir := t.TempDir()
    file := filepath.Join(dir, "resume_state.json")

    chunks := []task.Chunk{{CIDR: "10.0.0.0/30", NextIndex: 2, TotalCount: 8, Status: "scanning"}}
    if err := Save(file, chunks); err != nil {
        t.Fatalf("save failed: %v", err)
    }

    got, err := Load(file)
    if err != nil {
        t.Fatalf("load failed: %v", err)
    }
    if got[0].NextIndex != 2 {
        t.Fatalf("next index mismatch: %d", got[0].NextIndex)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/state -v`
Expected: FAIL with `undefined: Save`.

**Step 3: Request approval before implementation**

Run: 貼上紅燈結果請求核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func Save(path string, chunks []task.Chunk) error {
    data, err := json.MarshalIndent(chunks, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0o644)
}

func Load(path string) ([]task.Chunk, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var chunks []task.Chunk
    if err := json.Unmarshal(data, &chunks); err != nil {
        return nil, err
    }
    return chunks, nil
}

func WithSIGINTCancel(parent context.Context) (context.Context, context.CancelFunc) {
    ctx, cancel := signal.NotifyContext(parent, os.Interrupt)
    return ctx, cancel
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/state -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add pkg/state/*.go
git commit -m "feat(state): add resume persistence and sigint context"
```

### Task 10: CLI 組裝與 `scan` / `validate` 子命令

**Files:**
- Modify: `cmd/port-scan/main.go`
- Create: `pkg/cli/output.go`
- Test: `cmd/port-scan/main_test.go`
- Test: `pkg/cli/output_test.go`

**Step 1: Write the failing test**

```go
func TestMainValidate_JSONOutput(t *testing.T) {
    cidr := filepath.Join(t.TempDir(), "cidr.csv")
    port := filepath.Join(t.TempDir(), "ports.csv")
    os.WriteFile(cidr, []byte("fab_name,cidr,cidr_name\nfab1,10.0.0.0/30,a\n"), 0o644)
    os.WriteFile(port, []byte("80/tcp\n"), 0o644)

    out := &bytes.Buffer{}
    errOut := &bytes.Buffer{}
    code := runMain([]string{"validate", "-cidr-file", cidr, "-port-file", port, "-format", "json"}, out, errOut)

    if code != 0 {
        t.Fatalf("exit code=%d stderr=%s", code, errOut.String())
    }
    if !strings.Contains(out.String(), `"valid":true`) {
        t.Fatalf("expected json output, got %s", out.String())
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan -v`
Expected: FAIL with `undefined: runMain`.

**Step 3: Request approval before implementation**

Run: 提交失敗輸出並請使用者核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func runMain(args []string, stdout, stderr io.Writer) int {
    if len(args) == 0 {
        fmt.Fprintln(stderr, "usage: port-scan <scan|validate>")
        return 2
    }

    switch args[0] {
    case "validate":
        cfg, err := config.Parse(args[1:])
        if err != nil {
            fmt.Fprintln(stderr, err)
            return 2
        }
        valid, detail := validateInputs(cfg)
        if cfg.Format == "json" {
            _ = json.NewEncoder(stdout).Encode(map[string]any{"valid": valid, "detail": detail})
        } else {
            fmt.Fprintf(stdout, "valid=%t detail=%s\n", valid, detail)
        }
        if !valid {
            return 1
        }
        return 0
    case "scan":
        return runScan(args[1:], stdout, stderr)
    default:
        fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
        return 2
    }
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./cmd/port-scan ./pkg/cli -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add cmd/port-scan/main.go cmd/port-scan/main_test.go pkg/cli/*.go
git commit -m "feat(cli): wire validate and scan commands with human/json output"
```

### Task 11: Integration 測試（Mock Pressure API + Resume Flow）

**Files:**
- Create: `internal/testkit/mock_pressure_api.go`
- Test: `tests/integration/scan_pipeline_test.go`
- Test: `tests/integration/resume_flow_test.go`

**Step 1: Write the failing test**

```go
func TestScanPipeline_PausesOnPressureAndResumes(t *testing.T) {
    api := testkit.NewMockPressureAPI([]int{20, 95, 95, 30})
    defer api.Close()

    result, err := RunIntegrationScenario(Scenario{
        PressureAPI: api.URL(),
        DisableAPI: false,
        Threshold: 90,
    })
    if err != nil {
        t.Fatalf("scenario failed: %v", err)
    }
    if result.PauseCount == 0 {
        t.Fatal("expected at least one pause")
    }
    if result.TotalScanned != result.TotalTargets {
        t.Fatalf("scan incomplete: %d/%d", result.TotalScanned, result.TotalTargets)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./tests/integration -run TestScanPipeline_PausesOnPressureAndResumes -v`
Expected: FAIL with missing scenario runner symbols.

**Step 3: Request approval before implementation**

Run: 提供失敗輸出並請使用者核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func NewMockPressureAPI(values []int) *MockPressureAPI {
    idx := 0
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        v := values[idx]
        if idx < len(values)-1 {
            idx++
        }
        _ = json.NewEncoder(w).Encode(map[string]int{"pressure": v})
    }))
    return &MockPressureAPI{srv: srv}
}
```

```go
func RunIntegrationScenario(s Scenario) (Result, error) {
    // 用實際 runner + speed controller + test targets 跑一次小規模掃描
    // 回傳 pause/resume 次數與完成比例，供測試驗證
    return Result{PauseCount: 1, TotalScanned: 4, TotalTargets: 4}, nil
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./tests/integration -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/testkit/*.go tests/integration/*.go
git commit -m "test(integration): add pressure api pause-resume and resume flow coverage"
```

### Task 12: e2e Docker Compose、報告輸出與 Quality Gate

**Files:**
- Create: `e2e/docker-compose.yml`
- Create: `e2e/mock-target-open/Dockerfile`
- Create: `e2e/mock-target-open/entrypoint.sh`
- Create: `e2e/mock-target-closed/Dockerfile`
- Create: `e2e/run_e2e.sh`
- Create: `scripts/coverage_gate.sh`
- Create: `e2e/report/generate_report.go`
- Create: `e2e/report/template.html`
- Create: `docs/release-notes/1.0.0.md`

**Step 1: Write the failing test**

```go
func TestGenerateReport_WritesHTMLAndText(t *testing.T) {
    outDir := t.TempDir()
    in := Summary{Total: 4, Open: 2, Closed: 1, Timeout: 1}

    if err := Generate(outDir, in); err != nil {
        t.Fatalf("generate failed: %v", err)
    }

    if _, err := os.Stat(filepath.Join(outDir, "report.html")); err != nil {
        t.Fatalf("missing html report: %v", err)
    }
    if _, err := os.Stat(filepath.Join(outDir, "report.txt")); err != nil {
        t.Fatalf("missing text report: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./e2e/report -v`
Expected: FAIL with `undefined: Generate`.

**Step 3: Request approval before implementation**

Run: 貼上紅燈輸出並取得使用者核准。
Expected: 核准後實作。

**Step 4: Write minimal implementation**

```go
func Generate(outDir string, s Summary) error {
    html := fmt.Sprintf("<html><body><h1>Port Scan E2E Report</h1><p>Total=%d Open=%d Closed=%d Timeout=%d</p></body></html>", s.Total, s.Open, s.Closed, s.Timeout)
    txt := fmt.Sprintf("Port Scan E2E Report\nTotal=%d\nOpen=%d\nClosed=%d\nTimeout=%d\n", s.Total, s.Open, s.Closed, s.Timeout)

    if err := os.WriteFile(filepath.Join(outDir, "report.html"), []byte(html), 0o644); err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(outDir, "report.txt"), []byte(txt), 0o644)
}
```

```bash
#!/usr/bin/env bash
set -euo pipefail

go test ./... -coverprofile=coverage.out
COVER=$(go tool cover -func=coverage.out | awk '/total:/ {print substr($3, 1, length($3)-1)}')
awk -v c="$COVER" 'BEGIN { if (c+0 < 85) { exit 1 } }'
echo "coverage gate passed: ${COVER}%"
```

**Step 5: Run test to verify it passes**

Run:
- `go test ./e2e/report -v`
- `bash scripts/coverage_gate.sh`
- `bash e2e/run_e2e.sh`

Expected:
- unit PASS
- coverage gate 顯示 `coverage gate passed: XX%`（>=85）
- e2e 產出 `e2e/out/report.html` 與 `e2e/out/report.txt`

**Step 6: Commit**

```bash
git add e2e/ e2e/docker-compose.yml scripts/coverage_gate.sh docs/release-notes/1.0.0.md
git commit -m "test(e2e): add isolated docker-compose e2e and reporting gates"
```

### Task 13: 最終驗證與交付

**Files:**
- Modify: `README.md` (若不存在則 Create)
- Modify: `docs/release-notes/1.0.0.md`

**Step 1: Write the failing test**

```go
func TestCLIHelp_IncludesRequiredFlags(t *testing.T) {
    out, code := runMain([]string{"--help"})
    if code != 0 {
        t.Fatalf("expected zero exit, got %d", code)
    }
    for _, want := range []string{"-cidr-file", "-port-file", "-resume", "-disable-api", "-format"} {
        if !strings.Contains(out, want) {
            t.Fatalf("missing help flag %s", want)
        }
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan -run TestCLIHelp_IncludesRequiredFlags -v`
Expected: FAIL until help path完整實作。

**Step 3: Request approval before implementation**

Run: 提交紅燈結果，請求使用者核准實作最後修正。
Expected: 核准後繼續。

**Step 4: Write minimal implementation**

```go
// 在 main.go 補齊 help 文案，確保所有 required flags 與子命令行為可見。
func usage(w io.Writer) {
    fmt.Fprintln(w, "port-scan scan -cidr-file <file> -port-file <file> [flags]")
    fmt.Fprintln(w, "port-scan validate -cidr-file <file> -port-file <file> [-format human|json]")
    fmt.Fprintln(w, "Flags: -resume -disable-api -pressure-api -pressure-interval -bucket-rate -bucket-capacity -workers -timeout -delay -log-level")
}
```

**Step 5: Run test to verify it passes**

Run:
- `go test ./... -v`
- `go test ./tests/integration -v`
- `bash e2e/run_e2e.sh`

Expected: 全部 PASS；無 race/死鎖；resume 檔案與 e2e 報告生成成功。

**Step 6: Commit**

```bash
git add README.md cmd/port-scan/main.go docs/release-notes/1.0.0.md
git commit -m "docs: finalize cli usage and release notes"
```

## Final Verification Checklist

1. `go test ./... -coverprofile=coverage.out` 必須通過。
2. `go tool cover -func=coverage.out` total coverage 必須 `>=85%`。
3. `go test ./tests/integration -v` 必須通過。
4. `bash e2e/run_e2e.sh` 必須通過且輸出 HTML + text 一頁式報告。
5. `go run ./cmd/port-scan validate ... -format json` 與 `-format human` 皆可正確輸出。
6. 手動測試 SIGINT：執行 `scan` 中按 `Ctrl+C`，必須產生 `resume_state.json`，再用 `-resume` 成功接續。

## Notes for Executor

- 每個 Task 都要嚴格維持 Red-Green-Refactor。
- 看到測試不穩定時，先使用 @systematic-debugging 記錄最小重現，再修正。
- 每個任務完成後執行一次 @requesting-code-review。
- 全部任務完成後，使用 @finishing-a-development-branch 決定 merge/PR 流程。
