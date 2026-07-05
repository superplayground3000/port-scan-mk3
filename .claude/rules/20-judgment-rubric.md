# 20 — Judgment Rubric (read before declaring work complete)

Turns strong-model judgment into checks a weaker model can execute. Each rule
has a criterion, a positive example, a negative example, and a required action.

---

## R1 — When is work truly complete?
**Criterion.** Work is complete only when the relevant quality gate has been run
and its passing output observed. For any code change that means `make verify`
exited 0; for scan-pipeline/writer/pressure-control changes it also means
`make verify-e2e` exited 0.

**Positive.** You changed `pkg/scanner`, ran `make verify`, saw
"All selected quality gates passed", and pasted the coverage line. → Done.

**Negative.** You edited code, it "looks right", and you write "done" without
running anything. → Not done. Compiling is not passing; reading the diff is not
testing.

**Required action.** Run the gate. Paste the final result line. If you cannot
run it (e.g. no Docker for e2e), say so explicitly and state what is unverified.
Never write "done", "fixed", or "passing" without command output.

---

## R2 — When to write a test first
**Criterion.** Any change to production behavior (anything under `pkg/` or
`cmd/` that changes what the program does) MUST start with a failing test
(constitution III, NON-NEGOTIABLE). Concurrency fixes count: write a `-race`
test that is red before the fix.

**Positive.** Fixing the logger race: add `scan_logger_race_test.go`, confirm it
is red under `-race`, add the mutex, confirm green. → Correct order.

**Negative.** Add the mutex first, then maybe a test if time permits. → Wrong
order; you cannot prove the test would have caught the bug.

**Required action.** Red test → minimal fix → green test → `make verify`.

---

## R3 — When to upgrade the model / get a second opinion
**Criterion.** Upgrade when the current model cannot explain, using evidence
from the repo/tests/docs, why its approach is correct — or when it has failed
the same subtask twice.

**Positive.** A model edits pressure-control retry logic but cannot point to the
test that proves failures still trigger resume-state persistence. → Upgrade /
get a Codex second opinion before merging.

**Negative.** A model makes a gofmt error and fixes it after `make fmt`. → Do
not upgrade; trivial and self-corrected.

**Required action.** Escalate one tier (see `10-model-dispatch.md` C5) and, for
code-quality/final review, use cross-model review (Codex) per C6.

---

## R4 — When to stop and ask the user
**Criterion.** Stop for decisions that are destructive, irreversible, or change
scope/cost/privacy/external behavior, and that the evidence cannot resolve.
Do not stop for reversible choices with a sane default — pick it, document it,
continue.

**Positive.** The only way to hit 85% coverage is to lower the threshold or
exclude a package. That weakens a constitution gate → stop and ask.

**Negative.** Choosing a test file name, or whether a helper is exported. →
Pick the idiomatic option and continue.

**Required action.** When stopping, present the specific decision, the options,
and your recommendation — not an open-ended question.

---

## R5 — Signals the current direction is wrong (stop and rethink)
**Criterion.** Any of these means stop and re-read the goal, do not push harder:
- You are about to edit `scripts/coverage_gate.sh`, the constitution, or a rule
  file to make a gate pass.
- You are adding `//nolint`, `t.Skip`, or deleting assertions to get green.
- The same test has failed twice for reasons you cannot explain.
- You are touching more than ~10 files for a change described as small.
- You are about to scan a real, non-mock network target.

**Positive.** Coverage gate is red; you go add tests to the package you changed.
→ Right direction.

**Negative.** Coverage gate is red; you add the package to `EXCLUDE_PATTERN`. →
Wrong direction; this defeats the gate and needs user approval.

**Required action.** Revert the shortcut, return to the actual goal, and if
stuck after two honest attempts, escalate or ask (R3/R4).

---

## R6 — The quality floor (minimum before any "done")
All must hold. If you cannot verify one, say which and why.
- [ ] `make verify` exited 0 (gofmt, vet, build, `-race` tests, coverage ≥85%).
- [ ] New production behavior has a test that was red before the fix (R2).
- [ ] Docs touched by the change are updated (`.claude/rules/documents.md`).
- [ ] No gate, threshold, or test was weakened to pass (R5).
- [ ] For pipeline/writer/pressure changes: `make verify-e2e` exited 0.
- [ ] Cross-platform intact: no new hardcoded OS paths; uses `filepath`/`t.TempDir()`.
