# 10 — Model Dispatch & Delegation

How to split work between the commander (the main conversation agent) and
subagents, which model to use, and how to make delegation reliable. Read this
before delegating or choosing a model.

## Verified environment (as of the baseline audit)
These were confirmed present in the harness where this file was written.
Harnesses differ — before using any name below, confirm it exists in YOUR
session's tool/agent list. Do not invent names that are not in your session.

- **Agent tool `subagent_type` values:** `claude` (default, all tools),
  `Explore` (read-only search), `Plan` (read-only planning), `general-purpose`,
  `codex:codex-rescue` (Codex second opinion / deep debugging via Bash). Pass
  one of these exact strings as `subagent_type`.
- **MiniMax and other providers are invoked as *skills*, not `subagent_type`
  values:** use the `minimax-subagent` skill (or `/minimax`) for MiniMax's
  large-context provider. Do not pass `minimax-subagent` as a `subagent_type`.
- **Model identifiers:** `claude-fable-5`, `claude-opus-4-8`,
  `claude-sonnet-4-6`, `claude-haiku-4-5`. Use exact ids; do not guess. The
  tier guidance in C3 is a task-fit heuristic, not an official ranking.
- **Effort control:** the `Agent` tool exposes a `model` override but **no
  verified per-call effort parameter**. Some harnesses also expose a `Workflow`
  orchestration tool with per-agent `model`/`effort` — use it only if it is
  actually listed in your session. If you cannot confirm an effort control
  exists, do not pretend to set one — pick the model tier instead.
- **User's global routing rule** (`~/.claude/rules/subagent-provider-routing.md`)
  also applies and is summarized below. If it conflicts with this file, the
  user's global rule wins.

## C1 — The commander delegates bulk work
The commander should NOT personally do: large multi-file reads, repo-wide
scans, batch edits, broad web research, or long log inspection. Delegate those
to `Explore` (search) or `general-purpose`/`claude` (multi-step) subagents.

The commander DOES own: interpreting the goal, decomposing tasks, writing
delegation prompts, final synthesis, risk decisions, and the user-facing
answer. In a small repo task (one file, < ~200 LOC) doing it directly is fine —
delegation overhead is not worth it.

## C2 — Every delegated task carries a three-part contract
1. **Goal & motivation** — what to do, why it matters, what context matters.
2. **Acceptance criteria** — what must be true to succeed; which files, tests,
   or command outputs prove it.
3. **Return format** — exact shape of the reply; file paths + line numbers; no
   long prose unless requested; long artifacts written to a file, path returned.

Use the templates in `30-delegation-prompts.md`.

## C3 — Explicit model & effort selection
- Name the exact model id. Default new subagents to the session model unless a
  tier clearly fits better.
- **Mechanical / high-volume / cheap:** `claude-haiku-4-5` or `claude-sonnet-4-6`.
- **Normal implementation & review:** `claude-sonnet-4-6`.
- **Hard reasoning, ambiguous design, final risk calls:** `claude-opus-4-8` or
  `claude-fable-5`.
- **Large context (>10 files, huge codebase, >200K tokens):** MiniMax via
  `minimax-subagent` (per the user's global rule).
- If an effort control is available in the tool you are using, use the highest
  effort for high-risk reasoning; otherwise select a higher model tier.

## C4 — Subagent return contract
Subagents return only: conclusion, evidence, file paths + line numbers,
commands run and their results, remaining risks, and the path to any long
artifact written to disk. No massive prose dumps in chat.

## C5 — Escalation & de-escalation
- Small model wrong once on a subtask → escalate one tier immediately.
- Mid-tier model fails the same subtask twice → escalate with the full failure
  trail (commands, errors, what was tried).
- Once a stronger model finds the correct pattern → downgrade to a cheaper model
  for mechanical batch application.
- Retry the same approach at most two rounds; then change strategy, do not keep
  retrying because it is cheap.

## C6 — Verification must be independent (cross-model for quality)
Do not self-verify high-risk work. Per the user's global rule:
- **Code-quality review and final whole-change review → Codex**
  (`codex:codex-rescue`), because same-model self-review has blind spots. Fall
  back to a fresh-context Claude `general-purpose` agent (or the `/code-review`
  skill) only if Codex is unavailable.
- **Spec-compliance review** → Claude is fine.
- **Stuck on root cause** → `codex:codex-rescue`.
- Announce any non-Claude switch in one sentence ("Dispatching Codex for
  cross-model code-quality review").
- Verification methods by artifact: files → read-back; code → `make verify`
  (tests/build/vet/coverage) and `make verify-e2e` when the pipeline changed;
  research → check sources; ambiguous output → compare line-by-line against the
  original request.

## Suspension
If the user says "all Claude" or "no external CLIs" for a session, the
cross-model requirements in C6 are suspended for that session; use fresh-context
Claude agents for review instead.
