# Issue 148 Documentation Evidence

Branch: `integration/125-p1`

Base commit: `4febd75330d1ba979ba90dc38d2bf7f0b1776c95`

## Document dispositions

The review covered every live document in issue 148.

| Document | Disposition |
| --- | --- |
| `README.md` | Changed cancellation exit text and completed-chunk input-revision guidance. |
| `cmd/port-scan/README.md` | Changed command flow, count terms, pause terms, resume terms, and interrupt behavior. |
| `docs/cli/flags.md` | No change. Earlier implementation work already used the approved limit, commit, rewind, and interrupt terms. |
| `docs/cli/scenarios.md` | Changed resume behavior to permit repeated committed rows while prohibiting omitted probe tasks. |
| `docs/apps/port-scan/SPEC.md` | No change. Earlier implementation work already defined candidate addresses, probe tasks, committed results, rewind, and emergency exit. |
| `docs/apps/port-scan/DESIGN.md` | Changed the stale keyboard description from `p` and `r` to the space bar. |
| `docs/specs/SPEC-01-CLI-LAYER.md` | Changed exit-code and signal-adapter contracts. |
| `docs/specs/SPEC-03-INPUT-SYSTEM.md` | No change. The document does not state the rejected count, resume, or cancellation contracts. |
| `docs/specs/SPEC-04-TASK-SYSTEM.md` | Changed `Chunk` fields and removed the universal task-count formula. |
| `docs/specs/SPEC-06-SCAN-ORCHESTRATION.md` | No change. Earlier implementation work already defined queued work, rewind, committed results, and pause behavior. |
| `docs/specs/SPEC-09-WRITER-SYSTEM.md` | No change. Earlier implementation work already defined batch commit and output-failure rewind. |
| `docs/specs/SPEC-11-STATE-SYSTEM.md` | Changed resume cost, completed-chunk input revisions, snapshot-save cost, and interrupt ownership. |
| `docs/specs/SPEC-13-RICH-DASHBOARD.md` | Changed status, count terms, telemetry terms, and current file structure. |
| `docs/interrupt-handling.md` | No change. Earlier implementation work already defined graceful cancellation, emergency exit, rewind, and durability limits. |
| `docs/explain/speed-control.md` | No change. The document already separates a resumable pause from cancellation and uses the space bar. |

## Contract result

The live documents now use `candidate address` for expansion limits.
They use `probe task` for scan work and `committed result` for durable progress.

The task specification does not use `len(Targets) * len(Ports)` as a universal formula.
The state specification separates input parsing, incomplete rebuild, and snapshot-save costs.

The resume documentation states that completed chunks can use an earlier input revision.
It tells the operator to start a fresh run when one input revision is required.

The cancellation documentation separates pause, graceful cancellation, and emergency exit.
It also describes queued-task abandonment, rewind, late in-flight results, and snapshot-save errors.

The [performance harness](../docs/performance-harness.md#accepted-linux-report) links the accepted full Linux report evidence.
The report includes 1 MB, 10 MB, 100 MB, and 1 GB mixed snapshot cases.

## Simplified Technical English self-check

Pragmatic mode applies to the changed documentation.
The selected comparison verb is `make sure`.

- The three longest new prose sentences contain 21, 15, and 15 words.
- New prose contains no contractions, `has been`, `have been`, `should`, or semicolons.
- New prose contains no comma followed by an `-ing` verb.
- Procedural conditions occur before their commands.
- New prose does not mix `check`, `verify`, `confirm`, or `validate` as comparison verbs.

## Final quality gate

Command: `make verify`

The command exited with status 0 and reported 85.1 percent coverage.
Its final result was:

```text
All selected quality gates passed.
```
