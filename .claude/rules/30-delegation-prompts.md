# 30 — Delegation Prompt Templates

Fill every field. Empty fields cause vague subagent work. Keep the return format
tight (see `10-model-dispatch.md` C4). Copy a block, replace the `<...>`.

Common fields:
- **Goal:** what to accomplish and why it matters.
- **Context:** files/dirs, constraints, the constitution rules in play.
- **Scope / Out-of-scope:** exactly what to touch and what NOT to touch.
- **Acceptance criteria:** the command output or file state that proves success.
- **Return format:** conclusion + evidence + file:line + commands+results +
  risks; long output → write to a file and return the path.
- **Escalation trigger:** when to stop and report instead of pushing on.

---

## Search / Explore (use `Explore` or `general-purpose`)
```
Goal: Find <what> so I can <why>.
Context: Repo port-scan-mk3. Look under <dirs>. Naming conventions: <hints>.
Scope: Read-only. Do not edit.
Acceptance: A list of every match with file path + line number and a one-line
  note on relevance.
Return format: Bullet list "path:line — note". No file dumps. If >30 matches,
  write the full list to scratch and return the path + the top 10.
Escalation: If the pattern is ambiguous, return the candidate interpretations
  rather than guessing.
```

## Implementation (use `claude`/`general-purpose`; MiniMax if >10 files)
```
Goal: Implement <behavior> because <reason>.
Context: Domain logic lives in pkg/. Constitution I (library-first) and III
  (test-first) apply. Cross-platform (Linux + Windows) required.
Scope: Edit <files/pkg>. Out-of-scope: <do not touch>, do not weaken any gate.
Acceptance: A failing test written first, then `make verify` exits 0. Paste the
  final result line and the coverage total.
Return format: Summary + changed file:line list + the make verify result line +
  remaining risks.
Escalation: If make verify still fails after two honest attempts, stop and
  return the full failure output; do not skip/quarantine tests.
```

## Refactor (use `claude`)
```
Goal: Refactor <target> to <goal> with NO behavior change.
Context: Constitution VIII (SOLID); keep public contracts stable unless a
  breaking change is explicitly approved.
Scope: <files>. Out-of-scope: behavior changes, API changes.
Acceptance: `make verify` exits 0 and no test assertions were changed (tests
  prove behavior held). Paste the result line.
Return format: What moved where (file:line), why it is safe, make verify result.
Escalation: If a test must change to pass, stop — that means behavior changed;
  report it instead.
```

## Research (use `general-purpose`; MiniMax for large context)
```
Goal: Answer <question> to decide <decision>.
Context: <constraints, versions, e.g. Go 1.24.x stdlib net only>.
Scope: <sources allowed>. Out-of-scope: making code changes.
Acceptance: A recommendation backed by cited sources or repo evidence, with the
  trade-offs named.
Return format: Recommendation + evidence (URL or file:line) + risks. Long notes
  to a file; return the path.
Escalation: If sources conflict or evidence is thin, say so; do not present a
  guess as a fact.
```

## Review (cross-model — prefer Codex `codex:codex-rescue`; announce the switch)
```
Goal: Independently review <change> for correctness, concurrency safety, SOLID
  boundaries, and constitution compliance.
Context: Diff / files: <list>. This is cross-model review; be adversarial and
  try to find what is wrong.
Scope: Read + run gates only; do not fix. Out-of-scope: rewriting the change.
Acceptance: A verdict (approve / block) with each issue as file:line + why + a
  suggested fix, confirmation that `make verify` was run, and confirmation
  that the validation triggers in `60-development-guidelines.md` G3 were
  checked: `make verify-e2e` evidence when scan pipeline/writers/pressure
  control changed; before/after benchmark evidence when a hot path changed.
  Missing trigger evidence (and not explicitly declared unverified) → block.
Return format: Verdict + numbered issues (file:line, severity, fix) + gate
  result. Default to "block" if a MUST-level constitution rule is unmet.
Escalation: If the change touches the scan pipeline, require `make verify-e2e`
  evidence before approving.
```
