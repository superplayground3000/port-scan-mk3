# 40 — Maintenance Protocol (read before editing any rule file)

Keeps this governance system accurate over time without letting it drift or be
gamed. Applies to everything under `.claude/rules/` and to `AGENTS.md`.

## What a future agent MAY change autonomously
No user approval needed (but still back up — see below):
- Append a dated lesson to `50-lessons.md`.
- Add a narrow example to an existing rubric in `20-judgment-rubric.md`.
- Fix a broken file path or link after verifying the target exists.
- Clarify ambiguous wording without changing the intent of a rule.
- Update `AGENTS.md` build/test commands to match reality *after* verifying each
  command runs (this is required whenever the build/test flow changes).

## What REQUIRES asking the user first
Stop and ask before you:
- Change the model escalation policy (`10-model-dispatch.md`).
- Remove or weaken any safety, verification, or coverage requirement — including
  editing `scripts/coverage_gate.sh`, lowering the 85% floor, or adding packages
  to its `EXCLUDE_PATTERN`.
- Edit `.claude/rules/constitution.md` (it has its own amendment procedure).
- Rewrite `AGENTS.md`/`CLAUDE.md` structure (not just command fixes).
- Delete lessons, delete a rule file, or change the `.claude/rules/` layout.
- Change anything affecting cost, autonomy, privacy, or external actions.

## Backup requirement
Before modifying any existing tracked file under `.claude/rules/` or `AGENTS.md`:
- This repo is a git repository, so **git history is the primary backup** —
  ensure your change is a clean diff (no unrelated edits) so it can be reverted
  with `git revert`/`git checkout`.
- For edits made outside a commit (e.g. a long autonomous session that may end
  before committing), first copy the file to `<name>.bak-YYYYMMDD-HHMMSS` so the
  prior version survives even without a commit. Remove the `.bak` file once the
  change is committed.

## Where lessons go and their format
Record failures and their fixes in `50-lessons.md`, newest first, using:
```
## YYYY-MM-DD — <short title>
- Symptom: <what went wrong / what was observed>
- Root cause: <why>
- Fix / rule: <what to do instead; link the enforcing rule with [[file]] style>
- Evidence: <command + result, or file:line>
```
Compress when `50-lessons.md` exceeds ~40 entries or ~400 lines: merge duplicate
lessons into the relevant rubric rule and keep only the distilled guidance. Do
not delete lessons wholesale (that needs user approval); fold them in.

## Review requirement
- Any change to a rule file should be sanity-checked by re-reading the file after
  editing (read-back) to confirm it is complete and paths are correct.
- For changes to `10`/`20`/`40` (dispatch, judgment, this file), prefer a
  fresh-context or cross-model review before considering them settled, because
  these steer every future session.

## Consistency checks to run after editing rules
- Every `.claude/rules/*.md` referenced by `AGENTS.md` exists.
- Every command shown in `AGENTS.md` and `00-diagnostic.md` actually runs.
- `make verify` still exits 0 (rule edits should not touch code, but confirm).
