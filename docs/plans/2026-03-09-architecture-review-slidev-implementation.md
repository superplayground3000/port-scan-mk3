# Architecture Review Slidev Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Slidev-based architecture review deck for `port-scan-mk3` that explains requirements, architecture, bottlenecks, and trade-offs with draw.io-backed visuals.

**Architecture:** Create a self-contained Slidev deck under `docs/slides/architecture-review`, split the deck into section files for maintainability, and keep review diagrams as `.drawio` source plus exported SVG assets. Reuse existing architecture documentation where accurate, but add review-specific diagrams that emphasize design rationale rather than generic system overview.

**Tech Stack:** Slidev Markdown, draw.io `.drawio` source, SVG exports, shell verification with `rg`, `find`, and draw.io CLI.

---

### Task 1: Scaffold the Slidev deck structure

**Files:**
- Create: `docs/slides/architecture-review/slides.md`
- Create: `docs/slides/architecture-review/slides/01-cover.md`
- Create: `docs/slides/architecture-review/slides/02-context.md`
- Create: `docs/slides/architecture-review/slides/03-requirements.md`
- Create: `docs/slides/architecture-review/slides/04-decision-map.md`
- Create: `docs/slides/architecture-review/slides/05-overview.md`
- Create: `docs/slides/architecture-review/slides/06-happy-path.md`
- Create: `docs/slides/architecture-review/slides/07-failure-path.md`
- Create: `docs/slides/architecture-review/slides/08-boundaries.md`
- Create: `docs/slides/architecture-review/slides/09-bottlenecks.md`
- Create: `docs/slides/architecture-review/slides/10-tradeoffs.md`
- Create: `docs/slides/architecture-review/slides/11-quality.md`
- Create: `docs/slides/architecture-review/slides/12-verdict.md`
- Create: `docs/slides/architecture-review/public/diagrams/.gitkeep`
- Create: `docs/slides/architecture-review/public/diagrams-src/.gitkeep`

**Step 1: Write the failing test**

Check that the deck entry file and slide files do not yet exist:

```bash
find docs/slides/architecture-review -maxdepth 3 -type f | sort
```

**Step 2: Run test to verify it fails**

Run:

```bash
find docs/slides/architecture-review -maxdepth 3 -type f | sort
```

Expected: directory missing or files absent.

**Step 3: Write minimal implementation**

Create the Slidev folder structure and a `slides.md` entry file that imports section files in order.

**Step 4: Run test to verify it passes**

Run:

```bash
find docs/slides/architecture-review -maxdepth 3 -type f | sort
```

Expected: entry file, section markdown files, and asset directories exist.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review
git commit -m "docs(slidev): scaffold architecture review deck"
```

### Task 2: Author the review narrative slides

**Files:**
- Modify: `docs/slides/architecture-review/slides.md`
- Modify: `docs/slides/architecture-review/slides/*.md`
- Reference: `README.md`
- Reference: `docs/plans/2026-03-09-architecture-review-slidev-design.md`
- Reference: `docs/plans/2026-03-09-solid-refactor-tdd-design.md`
- Reference: `docs/architecture/diagram.html`

**Step 1: Write the failing test**

Define required review anchors that must appear in the deck:
- `requirements`
- `why this design`
- `bottlenecks`
- `trade-offs`
- `review verdict`

**Step 2: Run test to verify it fails**

Run:

```bash
rg -n "requirements|why this design|bottlenecks|trade-offs|review verdict" docs/slides/architecture-review
```

Expected: missing or partial matches only.

**Step 3: Write minimal implementation**

Fill each slide with concise review-focused Traditional Chinese copy, speaker notes, and layout metadata. Keep slides image-led and use short bullets that support the diagrams.

**Step 4: Run test to verify it passes**

Run:

```bash
rg -n "requirements|why this design|bottlenecks|trade-offs|review verdict" docs/slides/architecture-review
```

Expected: all anchors appear in the appropriate slide files.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review
git commit -m "docs(slidev): draft architecture review narrative"
```

### Task 3: Build review-specific draw.io diagrams

**Files:**
- Create: `docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio`
- Reference: `docs/architecture/port-scan-mk3-architecture.drawio`
- Reference: `docs/plans/2026-03-06-detailed-architecture-drawio-design.md`

**Step 1: Write the failing test**

Require a draw.io source file containing pages for:
- requirement decision map
- architecture overview
- happy path
- failure recovery
- trade-off comparison

**Step 2: Run test to verify it fails**

Run:

```bash
rg -n "Requirement|Overview|Happy|Failure|Trade" docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio
```

Expected: file missing or required page names absent.

**Step 3: Write minimal implementation**

Create a multi-page `.drawio` file with stable page names and reviewer-oriented labels. Prefer simple, import-safe shapes over dense decoration.

**Step 4: Run test to verify it passes**

Run:

```bash
rg -n "Requirement|Overview|Happy|Failure|Trade" docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio
```

Expected: page names and core labels are present.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio
git commit -m "docs(drawio): add architecture review diagram source"
```

### Task 4: Export SVG assets from draw.io for Slidev

**Files:**
- Modify: `docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio`
- Create: `docs/slides/architecture-review/public/diagrams/requirements-decision-map.svg`
- Create: `docs/slides/architecture-review/public/diagrams/architecture-overview.svg`
- Create: `docs/slides/architecture-review/public/diagrams/happy-path.svg`
- Create: `docs/slides/architecture-review/public/diagrams/failure-recovery.svg`
- Create: `docs/slides/architecture-review/public/diagrams/tradeoffs.svg`

**Step 1: Write the failing test**

Check that the exported SVGs do not yet exist.

**Step 2: Run test to verify it fails**

Run:

```bash
find docs/slides/architecture-review/public/diagrams -maxdepth 1 -name '*.svg' | sort
```

Expected: no exported SVG files.

**Step 3: Write minimal implementation**

Use draw.io CLI to export each page to a named SVG file, preserving the `.drawio` file as the editable source of truth.

**Step 4: Run test to verify it passes**

Run:

```bash
find docs/slides/architecture-review/public/diagrams -maxdepth 1 -name '*.svg' | sort
```

Expected: all required SVG assets exist.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review/public/diagrams docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio
git commit -m "docs(drawio): export architecture review slide assets"
```

### Task 5: Embed diagrams into the Slidev deck and polish layouts

**Files:**
- Modify: `docs/slides/architecture-review/slides/*.md`
- Modify: `docs/slides/architecture-review/slides.md`
- Reference: `docs/slides/architecture-review/public/diagrams/*.svg`

**Step 1: Write the failing test**

Require the deck to reference all exported SVG assets.

**Step 2: Run test to verify it fails**

Run:

```bash
rg -n "requirements-decision-map|architecture-overview|happy-path|failure-recovery|tradeoffs" docs/slides/architecture-review
```

Expected: one or more assets not referenced.

**Step 3: Write minimal implementation**

Place each SVG on the most relevant slide using image-friendly layouts and add a short rationale caption near the figure.

**Step 4: Run test to verify it passes**

Run:

```bash
rg -n "requirements-decision-map|architecture-overview|happy-path|failure-recovery|tradeoffs" docs/slides/architecture-review
```

Expected: every asset is referenced by the deck.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review
git commit -m "docs(slidev): embed architecture review diagrams"
```

### Task 6: Verify deck completeness and usability

**Files:**
- Verify: `docs/slides/architecture-review/slides.md`
- Verify: `docs/slides/architecture-review/slides/*.md`
- Verify: `docs/slides/architecture-review/public/diagrams/*.svg`
- Verify: `docs/slides/architecture-review/public/diagrams-src/architecture-review.drawio`

**Step 1: Write the failing test**

Define completeness checks:
- 12 section files referenced from `slides.md`
- all 5 SVG diagrams exist
- deck includes explicit reviewer language for requirements, bottlenecks, and trade-offs

**Step 2: Run test to verify it fails**

Run:

```bash
rg -n "^src: ./slides/" docs/slides/architecture-review/slides.md
find docs/slides/architecture-review/public/diagrams -maxdepth 1 -name '*.svg' | wc -l
rg -n "需求|瓶頸|取捨|為什麼這樣設計" docs/slides/architecture-review
```

Expected: any missing reference, asset, or phrase causes failure.

**Step 3: Write minimal implementation**

Fix any missing imports, assets, captions, or reviewer-oriented copy. Add a short `README` note only if deck usage would otherwise be ambiguous.

**Step 4: Run test to verify it passes**

Run:

```bash
rg -n "^src: ./slides/" docs/slides/architecture-review/slides.md
find docs/slides/architecture-review/public/diagrams -maxdepth 1 -name '*.svg' | wc -l
rg -n "需求|瓶頸|取捨|為什麼這樣設計" docs/slides/architecture-review
```

Expected: 12 slide imports, 5 SVG assets, and all reviewer language present.

**Step 5: Commit**

```bash
git add docs/slides/architecture-review
git commit -m "docs(slidev): finalize architecture review deck"
```
