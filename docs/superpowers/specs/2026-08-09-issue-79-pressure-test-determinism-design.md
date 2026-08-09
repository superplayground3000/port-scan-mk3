# Issue 79 Pressure Poll Test Determinism Design

## Status

The user approved this design on 2026-08-09. This design implements GitHub issue #79.

## Summary

The current pressure poll tests use fixed sleeps to wait for asynchronous events. These sleeps race Windows timers and goroutine scheduling.

The change replaces test synchronization sleeps with observable events. The change does not modify production behavior or public APIs.

## Goals

- Make all `TestPollPressureAPI_*` tests wait for observable events.
- Make each test stop its polling goroutine before the test returns.
- Preserve each existing pressure-control assertion.
- Provide deterministic red, green, and mutation evidence.
- Record required Native Windows work in one tracking issue.

## Non-Goals

- Do not change `pollPressureAPI` behavior.
- Do not add a production clock or ticker abstraction.
- Do not change pressure limits, retry rules, logging, or telemetry.
- Do not use longer sleeps as synchronization.
- Do not modify tests that do not call `pollPressureAPI`.

## Scope

Add test helpers in `pkg/scanapp/pressure_test_helpers_test.go`.

Update these test files:

- `pkg/scanapp/pressure_monitor_test.go`
- `pkg/scanapp/scan_observability_test.go`
- `pkg/scanapp/scan_test.go`
- `pkg/scanapp/scan_helpers_test.go`

Keep the existing `internal/testkit.WaitFor` helper unchanged.

If a test exposes a helper defect, stop and revise the design.

## Chosen Approach

Use test-side event synchronization. A scripted HTTP server controls each response, and channels report each request.

A poller harness owns the context cancel function and a completion channel. The harness stops the poller during test cleanup.

This approach keeps all timing control in test code. It also preserves the real HTTP and ticker paths that caused the Windows failures.

## Test Helper Design

### Scripted Pressure Server

The server exposes a request event and accepts one response step for each request. A response step contains an HTTP status and response body.

The handler waits for a response step or request cancellation. Therefore, poller cancellation cannot leave the handler blocked.

Tests release one response at a time. Then each test waits for the related controller, log, telemetry, or error state.

### Poller Harness

The harness starts `pollPressureAPI` in one goroutine. It records completion by closing a `done` channel.

The harness supplies one idempotent stop operation. This operation cancels the context and waits for completion with a bounded timeout.

When the poller does not stop, the timeout reports a failure. The timeout does not control a successful event sequence.

## Event Flows

### Positive State Changes

Tests for threshold and overload values start from the active state. They release one response and wait until `ctrl.IsPaused()` returns true.

### Negative State Changes

Tests for low, zero, and negative values start with API pause enabled. They release one response and wait until API pause becomes false.

This opposite initial state proves that the poller processed the response. It also prevents an immediate false success.

### Multiple State Changes

Pause, resume, and oscillation tests release one response per expected transition. Each test waits for the current state before the next response.

The tests do not count states with periodic sampling. Periodic sampling can miss valid transitions between samples.

### Failures and Recovery

Failure tests wait for request events, error-channel results, or recorded logs. Recovery tests release failures followed by one successful response.

The successful response proves that the failure counter reset. The test then stops the poller and makes sure that no fatal error exists.

### Logs and Telemetry

Log tests wait until the output contains each required message. Telemetry tests wait until the recorder contains all required callbacks.

After the expected data appears, each test stops the poller. The test reads the final data only after poller completion.

## Red-Green Proof

First, delay the first response beyond the current fixed assertion budget. Run the unchanged fixed-sleep assertion and record its deterministic failure.

Then replace the sleep with event synchronization. Run the same scenario and record its success.

For mutation evidence, temporarily disable the pause transition in the local worktree. The repaired test must fail before the temporary change is removed.

No temporary mutation can remain in the final diff.

## Validation

Run the focused family under the race detector and shuffle mode:

```text
go test -race -shuffle=on -count=100 ./pkg/scanapp -run '^TestPollPressureAPI_'
```

Run the full repository gate:

```text
make verify
```

Run the isolated pressure-control gate:

```text
make verify-e2e
```

This change does not modify a production hot path. Therefore, the performance gate does not apply.

An independent fresh-context agent must review the finished change. The review must include the final gate evidence.

## Native Windows Tracking

Create one long-term GitHub issue named `Track changes that require Native Windows validation`.

Each entry in the issue contains:

- The source issue, pull request, or commit.
- The reason that Native Windows is required.
- The exact command or CI job.
- The expected result.
- The result evidence and run URL.
- The current status.

Issue #79 is the first pending entry. A Windows agent will update the entry and add an evidence comment.

If one or more entries are pending, use `ready-for-agent`.

If no entry is pending, remove this label.

Keep the tracking issue open so later changes can add entries.

## Documentation

The code change modifies tests only. No user-facing behavior or product document changes.

The design document and implementation plan are the only repository documents for this change.

## Constitution Alignment

- The red-green sequence obeys Constitution III.
- The race and repository gates preserve the Quality Gates.
- The isolated e2e run obeys Constitution V.
- The test-only helper keeps production interfaces unchanged.
- The helper has one responsibility and obeys Constitution VIII.

## Risks

When a handler cannot observe cancellation, the scripted server can deadlock. The handler must select on the request context for every channel wait.

When its expected state equals the initial state, a test can report false success. Negative-state tests must start from the opposite state.

When cleanup runs first, a timeout can hide the original failure. Tests must report the event failure before they stop the poller.
