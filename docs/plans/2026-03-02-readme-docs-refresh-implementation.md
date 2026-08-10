# README and Documentation Refresh Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver developer-focused documentation that explains how `port-scan-mk3` works, fully documents all CLI flags, adds scenario-based command guides, explains e2e coverage, and ships a static HTML+CSS architecture diagram.

**Architecture:** Keep `README.md` concise as the navigation hub, then move complete details into dedicated docs under `docs/cli/`, `docs/e2e/`, and `docs/architecture/`. The plan intentionally avoids runtime code changes and focuses on content correctness backed by current code (`pkg/config/config.go`, `cmd/port-scan/main.go`) and e2e scripts.

**Tech Stack:** Markdown, HTML5, CSS3, existing Go CLI contracts for verification, shell-based validation (`rg`, `go run`, `test`, `wc`).

---

## Skill References

- @superpowers:executing-plans
- @superpowers:verification-before-completion

---

### Task 1: Build README as Navigation Hub

**Files:**
- Modify: `/Users/xuxiping/tsmc/port-scan-mk3/README.md`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/pkg/config/config.go`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/cmd/port-scan/main.go`

**Step 1: Write a failing content check**

Run:
```bash
rg -n "How It Works|Scenario Cookbook|E2E overview|Architecture Diagram" /Users/xuxiping/tsmc/port-scan-mk3/README.md
```
Expected: Missing one or more headings (non-zero exit).

**Step 2: Write minimal README structure update**

Insert sections:
```md
## How It Works
## Commands at a Glance
## Flags Quick Reference
## Scenario Cookbook
## E2E Overview
## Architecture Diagram
```

**Step 3: Add navigation links to deep docs**

Add links:
```md
- [All flags](docs/cli/flags.md)
- [Scenario cookbook](docs/cli/scenarios.md)
- [E2E overview](docs/e2e/overview.md)
- [Architecture diagram](docs/architecture/diagram.html)
```

**Step 4: Verify README now contains required sections**

Run:
```bash
rg -n "How It Works|Commands at a Glance|Flags Quick Reference|Scenario Cookbook|E2E Overview|Architecture Diagram" /Users/xuxiping/tsmc/port-scan-mk3/README.md
```
Expected: PASS with all section hits.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/README.md
git commit -m "docs: reorganize README as docs navigation hub"
```

---

### Task 2: Create Full CLI Flags Reference

**Files:**
- Create: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/pkg/config/config.go`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/cmd/port-scan/main.go`

**Step 1: Write failing existence check**

Run:
```bash
test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md
```
Expected: FAIL (exit code 1) if file is not created yet.

**Step 2: Write minimal flags table skeleton**

Create content:
```md
# CLI Flags Reference

| Flag | Type | Default | Command | Description |
|------|------|---------|---------|-------------|
```

**Step 3: Fill all flags from code as source of truth**

Include at least:
```md
-cidr-file
-port-file
-output
-timeout
-delay
-bucket-rate
-bucket-capacity
-workers
-pressure-api
-pressure-interval
-disable-api
-resume
-log-level
-format
-cidr-ip-col
-cidr-ip-cidr-col
```
Also include behavior notes and common mistakes sections.

**Step 4: Verify full coverage of expected flags**

Run:
```bash
for f in cidr-file port-file output timeout delay bucket-rate bucket-capacity workers pressure-api pressure-interval disable-api resume log-level format cidr-ip-col cidr-ip-cidr-col; do rg -n "-$f" /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md >/dev/null || echo "missing -$f"; done
```
Expected: No output.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md
git commit -m "docs: add complete CLI flags reference"
```

---

### Task 3: Create Scenario Cookbook (8-10 Scenarios)

**Files:**
- Create: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/e2e/run_e2e.sh`

**Step 1: Write failing scenario-count check**

Run:
```bash
test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md && rg -n "^## Scenario" /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md | wc -l
```
Expected: file missing or count < 8.

**Step 2: Write minimal scenario template**

Template:
```md
## Scenario N: <name>
Goal:
Command:
Expected:
Troubleshooting:
```

**Step 3: Add 10 scenarios with runnable commands**

Must include:
- basic scan
- column mapping
- validate human
- validate json
- pressure control
- pressure api failures
- explicit resume path
- cancellation/SIGINT workflow
- same-second output naming collision
- e2e parity execution

**Step 4: Verify scenario coverage count and required keywords**

Run:
```bash
rg -n "^## Scenario" /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md | wc -l
rg -n "resume|pressure|validate|output|e2e|SIGINT" /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md
```
Expected: Scenario count >= 8 and all keyword checks present.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md
git commit -m "docs: add scenario cookbook for CLI workflows"
```

---

### Task 4: Document E2E Architecture and Coverage

**Files:**
- Create: `/Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/e2e/run_e2e.sh`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/e2e/docker-compose.yml`

**Step 1: Write failing section check**

Run:
```bash
test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md && rg -n "What is tested|How e2e works|Artifacts" /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md
```
Expected: missing sections or file not found.

**Step 2: Write minimal e2e document skeleton**

Skeleton:
```md
# E2E Overview
## How e2e works
## What is tested
## Artifacts and pass criteria
```

**Step 3: Fill scenario matrix from e2e script behavior**

Cover:
- normal scan path
- api_5xx expected failure + resume artifact
- api_timeout expected failure + resume artifact
- api_conn_fail expected failure + resume artifact
- report generation and artifact checks under `e2e/out/`

**Step 4: Verify all required scenarios are mentioned**

Run:
```bash
for s in normal api_5xx api_timeout api_conn_fail report.html report.txt resume_state; do rg -n "$s" /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md >/dev/null || echo "missing $s"; done
```
Expected: No output.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md
git commit -m "docs: add e2e workflow and coverage documentation"
```

---

### Task 5: Build Static HTML + CSS Architecture Diagram

**Files:**
- Create: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html`
- Create: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css`

**Step 1: Write failing existence check**

Run:
```bash
test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html && test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css
```
Expected: FAIL before files are created.

**Step 2: Create minimal HTML skeleton**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>port-scan-mk3 Architecture</title>
    <link rel="stylesheet" href="diagram.css" />
  </head>
  <body>
    <main class="diagram"></main>
  </body>
</html>
```

**Step 3: Add static block-flow layout in HTML+CSS**

Must render blocks for:
- CLI layer
- Core package layer
- External/artifact layer

Must show arrows for:
- input → parse/validate → task → scan orchestration → writer output
- pressure API → speed control gate
- interruption/failure → resume state
- e2e execution → report artifacts

**Step 4: Verify key architecture labels exist**

Run:
```bash
for k in "CLI" "scanapp" "input" "writer" "state" "speedctrl" "pressure API" "resume_state" "e2e/out"; do rg -n "$k" /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html >/dev/null || echo "missing $k"; done
```
Expected: No output.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css
git commit -m "docs: add static HTML/CSS architecture diagram"
```

---

### Task 6: Cross-Link and Consistency Validation

**Files:**
- Modify: `/Users/xuxiping/tsmc/port-scan-mk3/README.md`
- Verify: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md`
- Verify: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md`
- Verify: `/Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md`
- Verify: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html`

**Step 1: Write failing link check**

Run:
```bash
for p in docs/cli/flags.md docs/cli/scenarios.md docs/e2e/overview.md docs/architecture/diagram.html; do rg -n "$p" /Users/xuxiping/tsmc/port-scan-mk3/README.md >/dev/null || echo "missing link $p"; done
```
Expected: any missing links are printed.

**Step 2: Add/fix README links to all new docs**

Ensure README references all four target docs.

**Step 3: Validate command examples against current CLI behavior**

Run:
```bash
go run ./cmd/port-scan -h >/tmp/ps_help.txt
rg -n "-cidr-file|-port-file|-cidr-ip-col|-cidr-ip-cidr-col|-resume|-pressure-api|-pressure-interval" /tmp/ps_help.txt
```
Expected: all key flags listed.

**Step 4: Final sanity checks**

Run:
```bash
git diff --name-only
```
Expected: only documentation files changed for this workstream.

**Step 5: Commit**

```bash
git add /Users/xuxiping/tsmc/port-scan-mk3/README.md /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css
git commit -m "docs: finalize readme refresh with flags, scenarios, e2e, and architecture"
```

---

### Task 7: Pre-Completion Verification Gate

**Files:**
- Verify logs only (no source changes required)

**Step 1: Run docs structure checks**

```bash
test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md && test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md && test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md && test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html && test -f /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css
```

**Step 2: Run link/reference checks**

```bash
rg -n "docs/cli/flags.md|docs/cli/scenarios.md|docs/e2e/overview.md|docs/architecture/diagram.html" /Users/xuxiping/tsmc/port-scan-mk3/README.md
```

**Step 3: Run quick formatting/readability pass**

```bash
rg -n "TODO|TBD|<placeholder>" /Users/xuxiping/tsmc/port-scan-mk3/README.md /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md /Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md /Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md /Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html
```
Expected: no unresolved placeholders.

**Step 4: Confirm clean working tree**

```bash
git status --short
```
Expected: clean after final commit.

**Step 5: Final commit (if verification notes/log added)**

```bash
# Only if extra docs were adjusted during verification
git add <adjusted-doc-files>
git commit -m "docs: finalize verification touch-ups"
```

---

## Deliverables

- Updated: `/Users/xuxiping/tsmc/port-scan-mk3/README.md`
- New: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/flags.md`
- New: `/Users/xuxiping/tsmc/port-scan-mk3/docs/cli/scenarios.md`
- New: `/Users/xuxiping/tsmc/port-scan-mk3/docs/e2e/overview.md`
- New: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.html`
- New: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/diagram.css`

## Acceptance Criteria

- Developer can understand architecture and data flow from README + diagram.
- All current CLI flags are documented in one complete reference doc.
- Scenario cookbook provides 8+ runnable examples with expected outcomes.
- E2E documentation clearly states how it works and what it validates.
- README remains concise and links to deep docs.
