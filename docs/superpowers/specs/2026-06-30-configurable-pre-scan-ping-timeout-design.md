# Configurable Pre-Scan Ping Timeout — Design

**Status:** Historical

**Current architecture:** [port-scan design](../../apps/port-scan/DESIGN.md)

**Date:** 2026-06-30
**Status:** Approved

## Problem

The pre-scan ping reachability check uses a hardcoded timeout
(`preScanPingTimeout = 100 * time.Millisecond` in `pkg/scanapp/pre_scan_ping.go`).
Operators on slower or higher-latency networks cannot widen this window, so hosts
that would respond within, say, 300ms are incorrectly classified as unreachable and
skipped. The timeout must be configurable from the CLI.

The original pre-scan ping design
([2026-03-20-pre-scan-ping-design.md](2026-03-20-pre-scan-ping-design.md)) deliberately
kept the timeout internal for v1. This change supersedes that decision.

## Goal

Expose the pre-scan ping timeout as a CLI flag, defaulting to 100ms so existing
behavior is unchanged when the flag is omitted.

## Non-Goals (YAGNI)

- No change to the resume-state schema (`PreScanPingState.TimeoutMS` already exists).
- No per-host or adaptive timeouts.
- No warning/diff when a resumed run's configured timeout differs from the saved one.

## Design

### 1. Config and flag (`pkg/config/config.go`)

- Add field to `Config`:
  ```go
  // PreScanPingTimeout bounds each pre-scan ping reachability check.
  PreScanPingTimeout time.Duration
  ```
- Register the flag, matching the existing `-timeout` (dial) flag's `DurationVar` style:
  ```go
  fs.DurationVar(&cfg.PreScanPingTimeout, "pre-scan-ping-timeout",
      100*time.Millisecond, "pre-scan ping timeout (duration like 100ms or 2s)")
  ```
- Validate after parse, mirroring the `-pressure-interval` check:
  ```go
  if cfg.PreScanPingTimeout <= 0 {
      return Config{}, errors.New("-pre-scan-ping-timeout must be > 0")
  }
  ```

Accepted input is any Go duration string (`100ms`, `250ms`, `1s`, `2s`). Default
`100ms`.

### 2. Wiring (`pkg/scanapp/pre_scan_ping.go`)

- Remove the `preScanPingTimeout` constant.
- Read `cfg.PreScanPingTimeout` inside `runPreScanPing` (the `cfg` value is already
  threaded into the function) and pass it to `checkReachability(...)` and into the
  persisted `PreScanPingState.TimeoutMS` (still `int(timeout / time.Millisecond)`).
- Replace the hardcoded reason constant
  (`preScanPingReason = "ping failed within 100ms"`) with a value derived from the
  configured timeout:
  ```go
  reason := fmt.Sprintf("ping failed within %s", cfg.PreScanPingTimeout)
  ```
  so the unreachable-row failure text reflects the real timeout instead of always
  claiming 100ms.

### 3. Tests (test-first)

`pkg/config`:
- Parsing `-pre-scan-ping-timeout=250ms` yields `250 * time.Millisecond`.
- Omitting the flag yields the `100ms` default.
- `0` and negative durations are rejected with the validation error.

`pkg/scanapp`:
- `runPreScanPing` passes the configured timeout to the `ReachabilityChecker`
  (assert the `timeout` argument received by a fake checker).
- The unreachable failure text reflects the configured timeout (e.g. contains
  `"within 300ms"` when configured to 300ms).
- Existing pre-scan ping and reachability tests remain green.

### 4. Documentation

- Add `-pre-scan-ping-timeout` to the flags line in
  `cmd/port-scan/command_handlers.go` usage text.
- Update the README and the per-tool spec flag tables to list the new flag.
- Note in [2026-03-20-pre-scan-ping-design.md](2026-03-20-pre-scan-ping-design.md)
  that the timeout is now configurable (superseding the "internal for v1" note).

## Constitution Alignment

- **III. Test-First Delivery:** failing tests for config parsing/validation and the
  scanapp timeout threading are written before implementation.
- **II. CLI Contract-First:** new flag is documented in usage text and release notes
  on merge; default preserves the existing contract.
- **VIII. SOLID Boundaries:** no new types or interfaces; reuses the existing
  `ReachabilityChecker.Check(ctx, ip, timeout)` contract, which already takes a
  per-call `time.Duration`. The change only moves the timeout's source from a
  constant to config.
