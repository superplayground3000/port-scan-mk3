# SPEC-11: State System Specification

## Overview

`pkg/state` reads and writes scan snapshots. `pkg/scanapp` decides when to load,
rewind, and save them.

```text
generate-buckets -> state.SaveSnapshot -> snapshot JSON
scan             -> state.LoadSnapshot -> private scan runtime
scan failure     -> state.SaveSnapshot -> corrected snapshot JSON
```

## 1. Snapshot model

```go
type Snapshot struct {
    Chunks      []task.Chunk
    PreScanPing PreScanPingState
    Output      *OutputState
}
```

`PreScanPingState` stores the enabled flag, timeout metadata, and unreachable
IPv4 values. `OutputState` stores the all-results and open-only CSV paths.

A nil `Output` means that the snapshot has no recorded output paths. The scan
runtime then creates new timestamped paths.

## 2. Snapshot loading

```go
func LoadSnapshot(path string) (Snapshot, error)
```

`LoadSnapshot` accepts both supported formats:

- The current object envelope with `chunks`, `pre_scan_ping`, and `output`.
- The legacy top-level array of chunks.

JSON decoding rejects unknown fields and trailing content. The current envelope
must contain `chunks`. A present `pre_scan_ping` object must contain `enabled`
and `timeout_ms`.

## 3. Snapshot saving

```go
func SaveSnapshot(path string, snapshot Snapshot) error
```

The save operation creates a temporary file in the destination directory. It
writes, syncs, closes, and renames that file over the destination.

Before rename, a failure leaves the prior snapshot unchanged. A cleanup failure
is joined to the original error. Replacement keeps the destination file mode.

The rename is atomic on POSIX filesystems when the filesystem supports that
operation. Go does not promise atomic rename on Windows. The package does not
promise power-loss durability because it does not sync the parent directory.

## 4. Scan integration

The scan parser requires `config.ScanValues.ResumeInput`. The scan runtime uses
that path for both load and save. Scan has no fresh-build fallback.

The runtime saves when work is incomplete after cancellation or failure. It
does not save after a clean, complete run.

The runtime loads recorded output paths before it opens output files. It
validates existing CSV headers and appends new rows to those files.

## 5. Output-failure rewind

Dispatch can advance before an output write completes. When a required writer
fails, the result loop records each unwritten task index.

Before save, the resume manager rewinds each affected chunk to its lowest
unwritten index. It sets `ScannedCount` to the corrected `NextIndex`.

This policy can repeat a row that reached disk after an earlier task failed.
It cannot skip a task that did not reach every required writer.

## 6. Error order

A snapshot-save error is the final run error. It replaces a pressure, executor,
output, or dispatcher error. If save succeeds, a runtime error replaces a
dispatcher error.

## 7. Signal handling

```go
func WithSIGINTCancel(ctx context.Context) (context.Context, func())
```

`WithSIGINTCancel` returns a context that cancels after `SIGINT`. Its cleanup
function restores the signal handler. The command layer uses this context for
the scan workflow.

## 8. Compatibility rules

- Keep current and legacy snapshot decoding.
- Keep snapshots that omit `output` readable.
- Keep recovery for older chunks that omit `total_count`.
- Resolve recorded relative output paths with the documented compatibility
  rule in `pkg/scanapp`.
- Do not change JSON fields without a version and migration decision.

## 9. Main files

| File | Responsibility |
| --- | --- |
| `pkg/state/state.go` | Snapshot format, strict decode, and safe replacement |
| `pkg/state/signal.go` | `SIGINT` cancellation |
| `pkg/scanapp/scan_runtime.go` | Snapshot load and lifecycle order |
| `pkg/scanapp/resume_manager.go` | Rewind and save decision |
| `pkg/scanapp/output_path_upgrade.go` | Legacy relative output paths |
