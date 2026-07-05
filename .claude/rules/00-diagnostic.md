# 00 — Quick Diagnostic (read first)

This file names the failure modes most likely to waste tokens, lose focus, or
produce wrong results in *this* repository, with a concrete fix and the rule
that enforces each fix. It was written from a live audit of the repo, not from
guesswork. Each finding lists whether it is already fixed or still a standing
risk.

Golden rule for every session: **the completion gate is `make verify` (exit 0).
Never say done, fixed, or passing without pasting its result.** See
`20-judgment-rubric.md`.

---

## Problem 1 — Trusting `go test ./...` instead of the race gate (ERRORS)

**Symptom.** Plain `go test ./...` and the coverage gate (no `-race`) pass while
production code has a real concurrency bug. A live audit found a data race in
`pkg/scanapp/scan_logger.go`: worker goroutines wrote to one shared buffer with
no lock. `go test ./...` was green; `go test -race` was red.

**Why it matters.** An agent that runs only `go test ./...` will believe the
code is correct and ship a race that corrupts logs or crashes under load. The
whole point of an observability-heavy scanner (constitution VI) is defeated.

**Concrete fix.** Always gate on `make verify` (which runs `go test -race
-shuffle=on ./...`), never bare `go test ./...`. Anything shared across scan
worker goroutines needs a mutex; the fixed logger shows the pattern. (Fixed in
the baseline; the mutex and a regression test `scan_logger_race_test.go` are in
place.)

**Enforced by.** `scripts/verify.sh` + `.github/workflows/ci.yml` (race job) +
`20-judgment-rubric.md` (Definition of Done).

---

## Problem 2 — Stale always-loaded instructions (WASTES TOKENS)

**Symptom.** The always-loaded `CLAUDE.md`/`AGENTS.md` used to say
`go build -o app ./cmd/app`. There is no `cmd/app`. Every agent that trusted it
would run a failing build and burn tokens diagnosing a non-bug.

**Why it matters.** The always-loaded file is the highest-leverage text in the
repo. One wrong command there multiplies across every future session.

**Concrete fix.** `AGENTS.md` now lists real commands (`make build`,
`go build ./cmd/port-scan`, `make verify`). When you change how the project is
built or tested, update `AGENTS.md` in the same change and verify each command
you list actually runs. (Fixed in the baseline.)

**Enforced by.** `40-maintenance-protocol.md` (docs must match reality) +
`.claude/rules/documents.md` (keep docs up to date with code).

---

## Problem 3 — Razor-thin coverage floor (ERRORS / LOSS OF FOCUS)

**Symptom.** Total coverage is ~85.5% against an 85% floor, and the total is
carried by a few high-coverage packages. Six packages are already below 85%
(`cmd/preprocess` ~73%, `cmd/enrich-targets` ~74%, `cmd/port-scan` ~82%,
`pkg/input`, `pkg/validate`, `pkg/state` all ~84%). Adding untested code to any
package can push the *total* below 85% and turn CI red.

**Why it matters.** A weak agent that sees a red coverage gate but does not
understand the margin may thrash — deleting tests, lowering the threshold, or
excluding packages — instead of adding the missing tests. That is a focus sink
and a constitution violation (III forbids weakening the gate).

**Concrete fix.** When you add or change production code, add tests in the same
package in the same change. Before claiming done, run
`go tool cover -func=coverage.out | tail -1` and confirm the total is at or
above 85%. Never lower the threshold, delete tests, or add packages to the
`EXCLUDE_PATTERN` in `scripts/coverage_gate.sh` to pass the gate — that requires
user approval (`40-maintenance-protocol.md`).

**Enforced by.** `scripts/coverage_gate.sh` + CI + `20-judgment-rubric.md`.

---

## Standing quick-reference
- Run the gate: `make verify` (add `-e2e` when touching scan pipeline/writers/
  pressure control).
- Format before committing: `make fmt` (CI fails on non-gofmt-clean files).
- Never scan real external hosts; e2e is Docker-isolated only (constitution V).
- Go 1.24.x, stdlib `net` only, unless a documented complexity exception exists.
