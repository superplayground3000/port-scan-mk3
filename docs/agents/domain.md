# Domain Docs

This document tells the engineering skills how to read this repo's domain documentation during an exploration of the codebase.

This repo is **single-context**: one `CONTEXT.md` at the root, and one `docs/adr/` directory for all decisions.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root — the domain glossary and the context overview.
- **`docs/adr/`** — read the ADRs that touch the area of your work.

If one of these files does not exist, **continue silently**. Do not report the missing file, and do not propose to create it first. The `/domain-modeling` skill (reached through `/grill-with-docs` and `/improve-codebase-architecture`) creates these files later, when a term or a decision is resolved.

## File structure

```
/
├── CONTEXT.md
├── docs/adr/
│   └── 0001-<decision-slug>.md
├── cmd/          ← CLI entrypoints
└── pkg/          ← reusable domain logic
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term that `CONTEXT.md` defines. Do not use a synonym that the glossary explicitly avoids.

A concept that is not yet in the glossary is a signal. Either you invent language that the project does not use (think again), or a real gap exists (record it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, report the contradiction explicitly. Do not override the ADR silently:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_

ADRs complement `.claude/rules/constitution.md`. The constitution holds MUST-level project law. Each ADR records one architecture decision and its context. If the two conflict, the constitution wins.
