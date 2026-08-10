# Scanner DialContext Timeout + Local Port 0 Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate TCP connect logic to `net.Dialer` + `DialContext` with context timeout and `LocalAddr` port `0`, while preserving status behavior and testability.

**Architecture:** Keep `pkg/scanner` as the classification boundary and preserve injected dial behavior, but switch injection signature to `DialContext` style. `pkg/scanapp` will own the default runtime dialer construction (`LocalAddr: &net.TCPAddr{Port: 0}`) and pass its `DialContext` into scanner workers. Timeout classification in scanner will treat both `net.Error` timeout and `context.DeadlineExceeded` as `close(timeout)`.

**Tech Stack:** Go 1.24.x, standard library (`context`, `net`, `errors`, `time`, `strconv`), existing packages (`pkg/scanner`, `pkg/scanapp`).

---

### Task 1: Update scanner tests to DialContext signature and timeout semantics

**Files:**
- Modify: `pkg/scanner/scanner_test.go`
- Modify: `pkg/scanner/scanner_extra_test.go`

**Step 1: Write the failing test updates (@test-driven-development)**

Update call sites and mocks to the new dial signature before implementation changes.

```go
// scanner_test.go
res := ScanTCP((&net.Dialer{}).DialContext, host, port, 200*time.Millisecond)

// scanner_extra_test.go
func TestScanTCP_WhenContextDeadlineExceeded_ReturnsCloseTimeoutStatus(t *testing.T) {
    dial := func(context.Context, string, string) (net.Conn, error) {
        return nil, context.DeadlineExceeded
    }
    res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
    if res.Status != "close(timeout)" {
        t.Fatalf("expected close(timeout), got %s", res.Status)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanner -run 'TestScanTCP' -count=1`
Expected: FAIL (signature mismatch and/or timeout classification mismatch before implementation update).

**Step 3: Commit test-first changes**

```bash
git add pkg/scanner/scanner_test.go pkg/scanner/scanner_extra_test.go
git commit -m "test(scanner): migrate ScanTCP tests to DialContext signature"
```

### Task 2: Implement ScanTCP with context timeout + deadline classification

**Files:**
- Modify: `pkg/scanner/scanner.go`
- Test: `pkg/scanner/scanner_extra_test.go`

**Step 1: Write minimal implementation change**

- Change `ScanTCP` dial param to `func(context.Context, string, string) (net.Conn, error)`.
- Create `ctx, cancel := context.WithTimeout(context.Background(), timeout)`.
- Call `dial(ctx, "tcp", target)`.
- Timeout detection must include:

```go
if ne, ok := err.(net.Error); ok && ne.Timeout() {
    // close(timeout)
}
if errors.Is(err, context.DeadlineExceeded) {
    // close(timeout)
}
```

**Step 2: Run scanner tests to verify pass**

Run: `go test ./pkg/scanner -count=1`
Expected: PASS.

**Step 3: Commit implementation**

```bash
git add pkg/scanner/scanner.go
git commit -m "feat(scanner): use DialContext with timeout context"
```

### Task 3: Migrate scanapp dial abstraction to DialContext and default net.Dialer

**Files:**
- Modify: `pkg/scanapp/scan.go`
- Test impact check: `pkg/scanapp/scan_test.go`

**Step 1: Update dial abstraction types**

In `pkg/scanapp/scan.go`, change:

```go
type DialFunc func(context.Context, string, string) (net.Conn, error)
```

and keep `RunOptions.Dial DialFunc`.

**Step 2: Set runtime default dialer with LocalAddr port 0**

When `opts.Dial == nil`, build and bind:

```go
dialer := &net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}
dial = dialer.DialContext
```

**Step 3: Verify worker call path compiles and uses new signature**

Ensure existing call remains:

```go
res := scanner.ScanTCP(dial, t.ip, t.port, cfg.Timeout)
```

**Step 4: Run targeted scanapp tests**

Run: `go test ./pkg/scanapp -count=1`
Expected: PASS.

**Step 5: Commit scanapp migration**

```bash
git add pkg/scanapp/scan.go
git commit -m "feat(scanapp): default to net.Dialer DialContext with local port 0"
```

### Task 4: Regression verification and call-site audit

**Files:**
- Verify references only (no required edits): `pkg/scanner/scanner.go`, `pkg/scanapp/scan.go`, scanner/scanapp tests

**Step 1: Audit references**

Run: `rg -n 'ScanTCP\(|DialFunc|RunOptions\{|DialContext|DialTimeout' pkg tests cmd`
Expected: no stale `DialTimeout`-signature call site remains for scanner pipeline.

**Step 2: Run focused package regression**

Run: `go test ./pkg/scanner ./pkg/scanapp -count=1`
Expected: PASS.

**Step 3: Commit (if any audit-driven cleanups were made)**

```bash
git add -A
git commit -m "chore: clean up remaining dial signature references"
```

### Task 5: Full verification before completion

**Files:**
- No direct file changes required

**Step 1: Run full suite (@verification-before-completion)**

Run: `go test ./... -count=1`
Expected: PASS.

**Step 2: Capture verification evidence in final summary**

Include exact command set and pass/fail status in handoff notes.

**Step 3: Final commit if needed**

```bash
git add -A
git commit -m "test: finalize DialContext timeout migration verification"
```
