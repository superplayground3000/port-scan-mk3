# Configurable Pre-Scan Ping Timeout Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../../apps/port-scan/DESIGN.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hardcoded 100ms pre-scan ping timeout with a `-pre-scan-ping-timeout` CLI flag, defaulting to 100ms so existing behavior is unchanged.

**Architecture:** Add a `PreScanPingTimeout time.Duration` field + flag to `pkg/config`, then thread it from `cfg` through `runPreScanPing` into the reachability checks, the unreachable-row reason text, and the persisted resume state. No new types or interfaces — the existing `ReachabilityChecker.Check(ctx, ip, timeout)` already accepts a per-call duration.

**Tech Stack:** Go 1.24, standard `flag`, `time`, and `fmt` packages. Tests use the standard `testing` package and the existing `fakePreScanChecker` test double.

**Spec:** `docs/superpowers/specs/2026-06-30-configurable-pre-scan-ping-timeout-design.md`

## Global Constraints

- Go 1.24.x; TCP/ping behavior uses standard-library primitives only (no new deps).
- Default `-pre-scan-ping-timeout` is `100*time.Millisecond`; omitting the flag MUST preserve current behavior.
- Reusable logic stays in `pkg/`; `cmd/port-scan` is CLI composition only.
- Test-First (NON-NEGOTIABLE): write the failing test before implementation.
- `go test ./...` MUST pass; `bash scripts/coverage_gate.sh` MUST keep total coverage ≥ 85%.
- Validation error string for non-positive input MUST be exactly `-pre-scan-ping-timeout must be > 0` (mirrors the `-pressure-interval` convention).

---

### Task 1: Config flag and validation

Adds the flag, the `Config` field, and `>0` validation in `pkg/config`. Independently testable — `pkg/config` has no dependency on `pkg/scanapp`, and `pkg/scanapp` still compiles (it keeps using its constant until Task 2).

**Files:**
- Modify: `pkg/config/config.go` (struct field ~line 58, flag registration ~line 128, validation ~line 169)
- Test: `pkg/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Config.PreScanPingTimeout time.Duration`, populated by `Parse`, defaulting to `100*time.Millisecond`, guaranteed `> 0` on success.

- [ ] **Step 1: Write the failing tests**

Append to `pkg/config/config_test.go` (ensure `time` is imported in that file):

```go
func TestParse_PreScanPingTimeout_DefaultsTo100ms(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "targets.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PreScanPingTimeout != 100*time.Millisecond {
		t.Fatalf("expected default 100ms, got %v", cfg.PreScanPingTimeout)
	}
}

func TestParse_PreScanPingTimeout_AcceptsDurationString(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", "250ms"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PreScanPingTimeout != 250*time.Millisecond {
		t.Fatalf("expected 250ms, got %v", cfg.PreScanPingTimeout)
	}
}

func TestParse_PreScanPingTimeout_RejectsNonPositive(t *testing.T) {
	for _, val := range []string{"0", "0s", "-5ms"} {
		if _, err := Parse([]string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", val}); err == nil {
			t.Fatalf("expected error for -pre-scan-ping-timeout=%s", val)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./pkg/config/ -run TestParse_PreScanPingTimeout -v`
Expected: FAIL — compile error `cfg.PreScanPingTimeout undefined` (field does not exist yet).

- [ ] **Step 3: Add the struct field**

In `pkg/config/config.go`, immediately after the `DisablePreScanPing bool` field (currently ~line 58):

```go
	// PreScanPingTimeout bounds each pre-scan ping reachability check.
	PreScanPingTimeout time.Duration
```

- [ ] **Step 4: Register the flag**

In `Parse`, immediately after the `fs.BoolVar(&cfg.DisablePreScanPing, "disable-pre-scan-ping", false, "disable pre-scan ping")` line (~line 128):

```go
	fs.DurationVar(&cfg.PreScanPingTimeout, "pre-scan-ping-timeout", 100*time.Millisecond, "pre-scan ping timeout (duration like 100ms or 2s)")
```

- [ ] **Step 5: Add validation**

In `Parse`, immediately after the existing `-pressure-interval` `<= 0` check (the block ending `return Config{}, errors.New("-pressure-interval must be > 0")`, ~line 169):

```go
	if cfg.PreScanPingTimeout <= 0 {
		return Config{}, errors.New("-pre-scan-ping-timeout must be > 0")
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./pkg/config/ -run TestParse_PreScanPingTimeout -v`
Expected: PASS (all three).

- [ ] **Step 7: Confirm the whole repo still builds and tests green**

Run: `go build ./... && go test ./pkg/config/`
Expected: build succeeds; `pkg/config` tests PASS. (`pkg/scanapp` still uses its constant, so it is unaffected.)

- [ ] **Step 8: Commit**

```bash
git add pkg/config/config.go pkg/config/config_test.go
git commit -m "feat(config): add -pre-scan-ping-timeout flag with validation"
```

---

### Task 2: Thread configurable timeout through the pre-scan ping

Removes the hardcoded constants and sources the timeout from `cfg.PreScanPingTimeout`. The timeout flows to the reachability checker, the persisted `PreScanPingState.TimeoutMS`, and the unreachable-row failure text. Three internal function signatures change, so their callers (including one test) are updated in the same task to keep the package compiling.

**Files:**
- Modify: `pkg/scanapp/pre_scan_ping.go`
- Test: `pkg/scanapp/pre_scan_ping_test.go`

**Interfaces:**
- Consumes: `Config.PreScanPingTimeout` (from Task 1).
- Produces (internal, used only within `pkg/scanapp`):
  - `runReachabilityChecks(ctx context.Context, checker ReachabilityChecker, ips []string, workers int, timeout time.Duration) ([]uint32, error)`
  - `collectUnreachableRows(inputs runInputs, reachable func(string) bool, reason string) ([]writer.UnreachableRecord, error)`
  - `buildPreScanPingState(unreachable []uint32, timeout time.Duration) state.PreScanPingState`

- [ ] **Step 1: Add the new behavioral test (configured timeout flows through)**

Append to `pkg/scanapp/pre_scan_ping_test.go`:

```go
func TestPreScanPing_Run_UsesConfiguredTimeoutForCheckStateAndReason(t *testing.T) {
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.7": {IP: "10.0.0.7", Reachable: false, FailureText: "timeout"},
		},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.7/32"), FabName: "fab-a", CIDRName: "cidr-a"},
		},
		portSpecs: []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}},
	}, config.Config{
		PreScanPingTimeout: 300 * time.Millisecond,
		Workers:            1,
	}, checker, state.PreScanPingState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := checker.timeoutFor("10.0.0.7"); got != 300*time.Millisecond {
		t.Fatalf("expected configured 300ms timeout passed to checker, got %v", got)
	}
	if outcome.State.TimeoutMS != 300 {
		t.Fatalf("expected state timeout 300ms, got %+v", outcome.State)
	}
	if len(outcome.UnreachableRows) != 1 || outcome.UnreachableRows[0].Reason != "ping failed within 300ms" {
		t.Fatalf("expected reason to reflect configured timeout, got %+v", outcome.UnreachableRows)
	}
}
```

- [ ] **Step 2: Update existing tests that encoded the fixed-100ms contract**

These edits make the existing tests express "configured timeout flows through" instead of "fixed internal timeout." Apply each:

1. In `TestPreScanPing_Run_DedupesCheckerCallsAcrossDuplicateIPs`, change the config literal from `config.Config{Timeout: 250 * time.Millisecond, Workers: 4}` to:

```go
	}, config.Config{
		PreScanPingTimeout: 300 * time.Millisecond,
		Workers:            4,
	}, checker, state.PreScanPingState{})
```

   and change the two timeout assertions to expect the configured value:

```go
	if outcome.State.TimeoutMS != 300 {
		t.Fatalf("unexpected timeout ms: %+v", outcome.State)
	}
	if got := checker.timeoutFor("10.0.0.1"); got != 300*time.Millisecond {
		t.Fatalf("expected configured pre-scan timeout, got %v", got)
	}
```

2. In `TestPreScanPing_Run_AggregatesUnreachableRowsPerContextWithoutPortExpansion`, change the config literal `config.Config{Timeout: 100 * time.Millisecond, Workers: 2}` to:

```go
	}, config.Config{
		PreScanPingTimeout: 100 * time.Millisecond,
		Workers:            2,
	}, checker, state.PreScanPingState{})
```

   (The existing assertion `got.Reason != "ping failed within 100ms"` stays valid because 100ms is configured.)

3. In `TestPreScanPing_Run_ReusesSavedUnreachableStateWithoutCallingChecker`, change the config literal `config.Config{Timeout: 100 * time.Millisecond, Workers: 4}` to:

```go
	}, config.Config{
		PreScanPingTimeout: 100 * time.Millisecond,
		Workers:            4,
	}, checker, saved)
```

   (The existing assertion `outcome.State.TimeoutMS != 100` stays valid.)

4. In `TestPreScanPing_Run_RichRowsAggregateToSingleUnreachableRowWithDistinctMergedMetadata`, change the config literal `config.Config{Timeout: 5 * time.Second, Workers: 2}` to:

```go
	}, config.Config{
		PreScanPingTimeout: 100 * time.Millisecond,
		Workers:            2,
	}, checker, state.PreScanPingState{})
```

   (The existing assertion `got.Reason != "ping failed within 100ms"` stays valid.)

5. In `TestRunReachabilityChecks_FailsFastOnFatalCheckerError`, add the new `timeout` argument to the direct call:

```go
	_, err := runReachabilityChecks(context.Background(), checker, []string{"10.0.0.1", "10.0.0.2"}, 1, 100*time.Millisecond)
```

- [ ] **Step 3: Run the scanapp tests to verify they fail**

Run: `go test ./pkg/scanapp/ -run 'TestPreScanPing|TestRunReachabilityChecks' -v`
Expected: FAIL — compile errors (`runReachabilityChecks`/`collectUnreachableRows`/`buildPreScanPingState` called with too many arguments; `preScanPingTimeout`/`preScanPingReason` still drive the old values), and the new test fails on the 300ms expectations.

- [ ] **Step 4: Remove the constants and source the timeout from config**

In `pkg/scanapp/pre_scan_ping.go`, delete the constant block:

```go
const (
	preScanPingTimeout = 100 * time.Millisecond
	preScanPingReason  = "ping failed within 100ms"
)
```

Then rewrite `runPreScanPing` to compute `timeout`/`reason` once and pass them down:

```go
func runPreScanPing(ctx context.Context, inputs runInputs, cfg config.Config, checker ReachabilityChecker, saved state.PreScanPingState) (preScanOutcome, error) {
	if cfg.DisablePreScanPing {
		return preScanOutcome{}, nil
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	timeout := cfg.PreScanPingTimeout
	reason := fmt.Sprintf("ping failed within %s", timeout)

	if hasSavedPreScanPingState(saved) {
		unreachable := sortedUniqueIPv4U32(saved.UnreachableIPv4U32)
		rows, err := collectUnreachableRows(inputs, reachablePredicate(unreachable), reason)
		if err != nil {
			return preScanOutcome{}, err
		}
		if err := ctx.Err(); err != nil {
			return preScanOutcome{}, err
		}
		return preScanOutcome{
			State:              buildPreScanPingState(unreachable, timeout),
			UnreachableIPv4U32: unreachable,
			UnreachableRows:    rows,
		}, nil
	}

	if checker == nil {
		return preScanOutcome{}, fmt.Errorf("reachability checker is required")
	}

	uniqueIPs, err := collectUniquePreScanIPs(inputs)
	if err != nil {
		return preScanOutcome{}, err
	}
	unreachable, err := runReachabilityChecks(ctx, checker, uniqueIPs, cfg.Workers, timeout)
	if err != nil {
		return preScanOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	rows, err := collectUnreachableRows(inputs, reachablePredicate(unreachable), reason)
	if err != nil {
		return preScanOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	return preScanOutcome{
		State:              buildPreScanPingState(unreachable, timeout),
		UnreachableIPv4U32: unreachable,
		UnreachableRows:    rows,
	}, nil
}
```

- [ ] **Step 5: Update the three internal function signatures**

In `runReachabilityChecks`, add the `timeout` parameter and use it at the check call:

```go
func runReachabilityChecks(ctx context.Context, checker ReachabilityChecker, ips []string, workers int, timeout time.Duration) ([]uint32, error) {
```

and change the line `result, err := checkReachability(runCtx, checker, ip, preScanPingTimeout)` to:

```go
					result, err := checkReachability(runCtx, checker, ip, timeout)
```

In `collectUnreachableRows`, add the `reason` parameter and use it:

```go
func collectUnreachableRows(inputs runInputs, reachable func(string) bool, reason string) ([]writer.UnreachableRecord, error) {
```

and change the row field `Reason: preScanPingReason,` to:

```go
				Reason:            reason,
```

In `buildPreScanPingState`, add the `timeout` parameter and derive `TimeoutMS` from it:

```go
func buildPreScanPingState(unreachable []uint32, timeout time.Duration) state.PreScanPingState {
	return state.PreScanPingState{
		Enabled:            true,
		TimeoutMS:          int(timeout / time.Millisecond),
		UnreachableIPv4U32: unreachable,
	}
}
```

- [ ] **Step 6: Run the scanapp tests to verify they pass**

Run: `go test ./pkg/scanapp/ -run 'TestPreScanPing|TestRunReachabilityChecks' -v`
Expected: PASS (including the new `TestPreScanPing_Run_UsesConfiguredTimeoutForCheckStateAndReason`).

- [ ] **Step 7: Run the full package suite**

Run: `go test ./pkg/scanapp/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/scanapp/pre_scan_ping.go pkg/scanapp/pre_scan_ping_test.go
git commit -m "feat(scanapp): source pre-scan ping timeout from config"
```

---

### Task 3: Documentation and release note

Updates user-facing docs to describe the new flag and supersede the "fixed internal 100ms" wording, plus a release note for the CLI contract change (Constitution II/VII). No code changes.

**Files:**
- Modify: `cmd/port-scan/command_handlers.go:86` (usage flags line)
- Modify: `docs/cli/flags.md` (lines 20–21 and notes at 47–49)
- Modify: `cmd/port-scan/README.md` (flag table near line 94)
- Modify: `README.md` (flag table near line 362)
- Modify: `docs/superpowers/specs/2026-03-20-pre-scan-ping-design.md` (the "internal for v1 / not exposed as a flag" note)
- Create: `docs/release-notes/1.3.0.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update the CLI usage text**

In `cmd/port-scan/command_handlers.go:86`, insert `-pre-scan-ping-timeout` right after `-disable-pre-scan-ping` in the flags string, so the segment reads:

```go
	fmt.Fprintln(w, "Flags: -cidr-ip-col -cidr-ip-cidr-col -resume -disable-pre-scan-ping -pre-scan-ping-timeout -disable-api -pressure-api -pressure-interval -pressure-auth-url -pressure-data-url -pressure-client-id -pressure-client-secret -pressure-use-auth -quiet -bucket-rate -bucket-capacity -workers -timeout -delay -log-level -format")
```

- [ ] **Step 2: Update `docs/cli/flags.md`**

Change the `-timeout` row note (line 20) to drop the "fixed internally" clause:

```
| `-timeout` | duration | `100ms` | `validate`, `scan` | TCP dial timeout per probe. This does not control the pre-scan ping; use `-pre-scan-ping-timeout` for that. |
```

Add a new row immediately after the `-disable-pre-scan-ping` row (line 21):

```
| `-pre-scan-ping-timeout` | duration | `100ms` | `validate`, `scan` | Timeout for each pre-scan ping reachability check. Must be > 0. |
```

Replace the note line 48 (`Pre-scan ping uses a fixed internal timeout of 100ms.`) with:

```
- Pre-scan ping timeout defaults to `100ms` and is configurable via `-pre-scan-ping-timeout`.
```

- [ ] **Step 3: Update `cmd/port-scan/README.md`**

In the flag table, add a row immediately after the `-timeout` row (near line 94):

```
| `-pre-scan-ping-timeout` | `100ms` | Pre-scan ping reachability timeout (duration string, must be > 0) |
```

- [ ] **Step 4: Update top-level `README.md`**

In the flag table, add a row immediately after the `-disable-pre-scan-ping` row (near line 362):

```
| `-pre-scan-ping-timeout` | Pre-scan ping reachability timeout (duration string, default `100ms`) |
```

- [ ] **Step 5: Supersede the design note in the original pre-scan ping spec**

In `docs/superpowers/specs/2026-03-20-pre-scan-ping-design.md`, find the line stating the ping timeout is internal/not exposed as a flag and replace it with:

```
Ping timeout defaulted to a fixed 100ms in v1; as of the 2026-06-30 design it is configurable
via the `-pre-scan-ping-timeout` flag (default `100ms`). See
[2026-06-30-configurable-pre-scan-ping-timeout-design.md](2026-06-30-configurable-pre-scan-ping-timeout-design.md).
```

- [ ] **Step 6: Create the release note**

Create `docs/release-notes/1.3.0.md`:

```markdown
# 1.3.0 Release Notes

## New features
- Add `-pre-scan-ping-timeout` flag (duration, default `100ms`) to configure the per-host
  pre-scan ping reachability timeout. The timeout was previously fixed at `100ms` internally.

## Compatibility notes
- Non-breaking: omitting `-pre-scan-ping-timeout` preserves the prior `100ms` behavior.
- Values must be `> 0`; `0` or negative durations are rejected at startup.
- The unreachable-output `reason` column now reflects the configured timeout
  (e.g. `ping failed within 300ms`) instead of always reporting `100ms`.
```

- [ ] **Step 7: Verify docs reference the flag consistently**

Run: `grep -rn 'pre-scan-ping-timeout' README.md cmd/port-scan/README.md docs/cli/flags.md docs/release-notes/1.3.0.md cmd/port-scan/command_handlers.go`
Expected: a match in each file.

- [ ] **Step 8: Commit**

```bash
git add cmd/port-scan/command_handlers.go cmd/port-scan/README.md README.md docs/cli/flags.md docs/superpowers/specs/2026-03-20-pre-scan-ping-design.md docs/release-notes/1.3.0.md
git commit -m "docs: document configurable -pre-scan-ping-timeout flag"
```

---

### Final Verification

- [ ] **Full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Coverage gate**

Run: `bash scripts/coverage_gate.sh`
Expected: total coverage ≥ 85%, gate PASS.

- [ ] **Lint**

Run: `golangci-lint run`
Expected: no new findings.

- [ ] **Manual smoke check of the flag**

Run: `go run ./cmd/port-scan scan -h 2>&1 | grep pre-scan-ping-timeout`
Expected: the flag and its `100ms` default appear in help output.
