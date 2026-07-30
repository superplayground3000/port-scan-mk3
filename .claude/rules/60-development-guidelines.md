# 60 — Development Guidelines (act like the strong model)

This file distills how the strong model that wrote this baseline thinks and
acts, into steps any future agent — including a smaller or cheaper one — can
execute. It combines `constitution.md` (project law) with working discipline.
It does not replace the other rules; it sequences them. When this file and the
constitution conflict, the constitution wins.

Three non-negotiables this file enforces:
1. **All development is TDD** (red test before production code).
2. **Every change is reviewed by a different provider/model — at minimum a
   different, fresh-context agent** than the one that wrote it.
3. **Validation covers unit tests, e2e tests, and performance tests** where
   each applies (see G3 for the exact triggers).

---

## G0 — The operating loop (how to think)

Run every task through this loop. Do not skip steps because the task "looks
simple" — the loop is cheap; a wrong assumption is not.

1. **Understand before acting.** Read the actual files (never rely on the file
   name or a doc summary alone). Restate the goal in one sentence and write
   down what evidence will prove it is done (which test, which gate output).
2. **Decompose to the smallest verifiable step.** One behavior change at a
   time. If the change is described as small but touches >~10 files, stop —
   your direction is probably wrong (`20-judgment-rubric.md` R5).
3. **Red test first** (G1).
4. **Minimal implementation to green.** Resist scope growth; note follow-ups
   instead of doing them.
5. **Run the gates and read the output** (G3). Compiling is not passing;
   reading the diff is not testing.
6. **Independent review** (G2) before the change is called complete.
7. **Report with evidence** (G5): outcome first, command output pasted,
   anything unverified named explicitly.

## G1 — TDD is mandatory, with proof (no exceptions)

Constitution III is NON-NEGOTIABLE; this section makes it operational.

- Order is always: **red → minimal fix → green → refactor → `make verify`**.
- A production behavior change is anything under `pkg/` or `cmd/` that changes
  what the program does. New feature, bug fix, concurrency fix — all count.
- **Proof of red is required.** You must run the new test before the fix and
  observe it fail. Keep that output; your final report cites it (e.g. "test X
  failed with Y before the change, passes after"). A test written after the
  code, that has never been red, proves nothing.
- Bug fix ⇒ first a regression test that reproduces the bug.
  Concurrency fix ⇒ the red test must fail under `-race` before the fix
  (see `50-lessons.md` 2026-07-05 — plain `go test` hid a real race).
- New behavior tests both the success path and at least one failure path
  (constitution I).
- If you cannot see how to write the test first, that is a design problem, not
  a reason to skip TDD: extract a seam, use `internal/testkit`, or escalate
  per `10-model-dispatch.md` C5. Writing code first and back-filling tests is
  prohibited.

## G2 — Independent review is mandatory for every change

Never deliver a change reviewed only by the agent that wrote it. Same-model
self-review has known blind spots; same-context self-review is worthless.

Reviewer selection, in strict priority order (take the first available):
1. **Different provider** — Codex via the `codex:codex-rescue` Agent
   `subagent_type`; MiniMax via the `minimax-subagent` skill for very large
   diffs. Use only names verified in `10-model-dispatch.md`, and confirm the
   name exists in YOUR session before using it.
2. **Different Claude model** — an Agent call with an explicit different
   `model` than the one that implemented.
3. **Minimum floor: a different, fresh-context agent** — a new
   `general-purpose` agent that has NOT seen the implementation conversation.
   The implementer summarizing its own work to itself does not count.

Rules of engagement:
- Use the Review template in `30-delegation-prompts.md`. The reviewer is
  adversarial, returns approve/block with file:line issues, and runs
  `make verify` itself — it does not trust the implementer's paste.
- The reviewer MUST also check the G3 triggers and block when their evidence
  is missing: no approval for a pipeline/writer/pressure-control change
  without `make verify-e2e` evidence, and no approval for a hot-path change
  without before/after benchmark evidence — unless the change is explicitly
  reported to the user as e2e- or perf-unverified with the reason.
- Announce any non-Claude dispatch in one sentence (user routing rule).
- Review happens on the finished change, after gates pass — a review of
  broken code wastes the cross-model budget.
- Blocked ⇒ fix ⇒ re-review. If implementer and reviewer still disagree after
  two rounds, stop and ask the user with both positions stated (R4).
- Exception: a user statement like "all Claude" or "no external CLIs"
  relaxes ONLY the provider choice (steps 1–2 above); the step-3 floor — an
  independent fresh-context review — remains mandatory. This file grants no
  path to skip independent review entirely; that would require the user to
  explicitly amend this rule (`40-maintenance-protocol.md`). Never infer
  permission to skip from silence, a small diff, or time pressure.

## G3 — Validation pyramid: unit + e2e + performance

All three layers, each triggered by what the change touches. "It passed the
layer I ran" is not done — check each trigger explicitly.

**Unit (always).**
- Tests live in the same package as the change, cover success and failure
  paths, and run via `make verify` (gofmt, vet, build, `-race -shuffle=on`
  tests, coverage ≥85%).
- The coverage floor is thin (~85.5%, see `00-diagnostic.md` Problem 3): add
  tests in the same change as the code. Never lower the threshold, exclude a
  package, or delete tests to get green — that needs user approval
  (`40-maintenance-protocol.md`).

**e2e (when the change touches scan pipeline, writers, or pressure control).**
- Run `make verify-e2e` (Docker Compose, isolated networks, mock services
  only — constitution V). Never scan a real host.
- If Docker is unavailable, say so explicitly and mark the change as
  e2e-unverified; do not silently skip.

**Performance (when the change touches a hot path).**
- Hot paths: the worker pool / scan loop, rate limiting and pressure control,
  dial/reachability code, writers, and large-input parsing or expansion.
- There is currently **no benchmark suite and no perf gate in CI** (verified
  2026-07-30: zero `func Benchmark` in the repo). So the rule is:
  1. If the touched hot path has no benchmark, **add one** in the same change
     (`func BenchmarkXxx` in the package's `_test.go`) — the perf equivalent
     of a missing test.
  2. Run it before and after your change:
     `go test -bench=BenchmarkXxx -benchmem -count=6 ./pkg/<pkg>/`
     (before = stash your change or use the base commit). Use `benchstat` to
     compare if installed; otherwise compare ns/op and allocs/op by eye
     across the 6 runs.
  3. A regression worse than ~10% on ns/op or allocs/op is a **blocker**:
     either fix it or report it to the user as an explicit trade-off. Never
     ship a silent regression.
- Benchmarks use synthetic in-process targets (`net.Listen` on 127.0.0.1,
  `internal/testkit`) — the isolation rule applies to benchmarks too.

**Evidence.** Paste the final result line of every layer you ran. Name every
layer you could not run and why.

## G4 — Resolving ambiguity (the decision ladder)

When a requirement is ambiguous, do not stall and do not guess wildly.
Resolve top-down and record the choice:

1. **Repo evidence** — existing code, tests, docs, release notes. The repo's
   established pattern beats your preference.
2. **The rules** — constitution, then `20-judgment-rubric.md`, then this file.
3. **Idiomatic Go / stdlib convention** (Go 1.24.x, stdlib `net`).
4. **The conservative, reversible default** — smallest surface, no new
   dependencies, no contract change. State the choice and its alternative in
   your report so a human can veto cheaply.

Stop and ask the user only for R4-class decisions: destructive or
irreversible actions, scope/cost/privacy changes, breaking a public CLI
contract, or anything that would weaken a gate. Present options plus your
recommendation, never an open-ended question.

Never resolve ambiguity by weakening a gate, skipping a layer of G3, or
silently expanding scope.

## G5 — Honest reporting (how to act)

- Lead with the outcome; evidence follows. Every claim of "done", "fixed", or
  "passing" is accompanied by the command output that proves it (R1).
- Keep the vocabulary exact: *compiles* < *tests pass* < *all gates pass* <
  *gates pass and independently reviewed*. Only the last one is complete.
- Failures are reported as faithfully as successes: paste the failing output,
  say what you tried, and what remains unknown.
- Stuck twice on the same subtask ⇒ change strategy or escalate
  (`10-model-dispatch.md` C5), optionally via `codex:codex-rescue`. Grinding a
  third time on the same approach is prohibited.
- New durable failure + fix ⇒ append to `50-lessons.md` in the required
  format.

## Completion checklist (all boxes, before any "done")

- [ ] Red-first proof exists for every behavior change (G1).
- [ ] `make verify` exited 0 — paste the final line.
- [ ] e2e trigger checked; `make verify-e2e` run and 0 if triggered (G3).
- [ ] Perf trigger checked; benchmark before/after run if triggered, no
      silent regression (G3).
- [ ] Independent review obtained from a different provider/model/agent,
      verdict: approve (G2).
- [ ] No gate, threshold, or test weakened to pass (R5).
- [ ] Docs touched by the change updated (`documents.md`); cross-platform
      intact (`filepath`, `t.TempDir()`, no hardcoded paths).
- [ ] Anything unverified is named explicitly in the final report.
