# 90 — Letter to Future Sessions

You are likely a smaller or cheaper model than the one that wrote this. That is
fine. This system is built so you can do excellent, safe work by following
explicit checks instead of relying on deep judgment. Start at
`00-diagnostic.md`, gate on `make verify`, and when unsure, escalate or ask.

## Three things the user did not ask for but this repo needs
1. **CI that actually runs the gates.** Before this baseline, the only GitHub
   workflow was gitleaks; nothing enforced `go test`, the coverage gate, or
   e2e. `.github/workflows/ci.yml` now does. Keep it wired to
   `scripts/verify.sh` / `scripts/coverage_gate.sh` / `e2e/run_e2e.sh` so local
   and CI never diverge. If you add a new gate, add it in the script first, then
   the workflow calls the script.
2. **`-race` in the default test path.** The one bug this baseline fixed was a
   data race invisible to plain `go test`. Never remove `-race` from
   `make test` / `scripts/verify.sh`.
3. **A single completion gate (`make verify`).** Weak agents thrash when there
   are five different "is it done?" commands. There is one. Protect it.

## Most likely ways this system degrades
- **Docs drift from code** — commands in `AGENTS.md` go stale (that is exactly
  what caused Lesson 2). Re-verify commands whenever the build flow changes.
- **The coverage floor gets gamed** instead of met — someone lowers 85, excludes
  a package, or deletes tests to get green. `40-maintenance-protocol.md` makes
  that a user-approval action; hold that line.
- **CI and the local scripts diverge** — someone edits the workflow inline
  instead of the script. Keep the workflow thin; logic lives in the scripts.
- **Rules bloat** — every session adds prose until no weak model reads it all.
  Compress lessons into rubrics (see `40`), keep `AGENTS.md` short.

## How to prevent that degradation
- Treat `scripts/verify.sh` as the single source of truth; CI just calls it.
- Run the consistency checks at the end of `40-maintenance-protocol.md` after
  any rule edit.
- Log real failures in `50-lessons.md` and fold them into rubrics when they
  recur.

## Incomplete work / known gaps (honest)
- **Windows CI is unverified.** The Windows build+test job in `ci.yml` was
  written but never executed here — only Linux gates and Windows *cross-compile*
  (from Linux) were verified locally. The first CI run on GitHub is its real
  verification; if the Windows `go test` job is red, fix the offending tests
  (likely hardcoded paths) rather than deleting the job.
- **No `.golangci.yml` / golangci-lint not installed here.** `make lint` falls
  back to `go vet`. If the team adopts golangci-lint, add a config and wire it
  into CI.
- **Coverage margin is thin (~85.5%).** Six packages are below 85% individually
  (see `00-diagnostic.md` Problem 3). Raising the low packages would make the
  gate robust; this was left as follow-up to avoid unrelated churn in this
  baseline change.

## Honest limitations of this harness
- No verified per-call "effort" parameter on the `Agent` tool; choose the model
  tier instead (`10-model-dispatch.md`).
- This system improves *execution reliability* (decomposition, gates, read-back,
  cross-model review). It cannot resolve vague taste calls, unstated user
  intent, or missing external facts — for those, escalate or ask (constitution
  and `20-judgment-rubric.md` R4).
