# Draw.io CLI Export Skill Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立一個全域可重用的 `drawio-cli-export` skill，讓未來 session 能穩定使用本機 draw.io CLI 匯出 `.drawio` 檔為圖片或 PDF。

**Architecture:** 以 `~/.codex/skills/drawio-cli-export/` 作為 skill 根目錄。`SKILL.md` 保持精簡，負責描述何時使用、標準流程、常用情境與排查；詳細的 CLI 旗標、已驗證範例與觀察結果放在 `cli-reference.md`。整體內容只收錄已在本機實測過的行為。

**Tech Stack:** draw.io desktop CLI、Markdown、Shell 驗證命令（`test`, `find`, `sed`, `file`）。

---

### Task 1: 建立 skill 目錄與最小骨架

**Files:**
- Create: `/Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md`
- Create: `/Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md`
- Test: `/Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md`

**Step 1: Write the failing test**

先定義骨架完成條件：
- skill 目錄存在
- `SKILL.md` 與 `cli-reference.md` 都存在

**Step 2: Run test to verify it fails**

Run:
```bash
find /Users/xuxiping/.codex/skills/drawio-cli-export -maxdepth 1 -type f
```
Expected: 目錄不存在或檔案不足（FAIL，屬預期）。

**Step 3: Write minimal implementation**

建立目錄與兩個檔案，先放最小 frontmatter 與標題。

**Step 4: Run test to verify it passes**

Run:
```bash
test -f /Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md
test -f /Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md
```
Expected: 兩個檔案都存在（PASS）。

**Step 5: Commit**

```bash
git add docs/plans/2026-03-09-drawio-cli-export-skill-design.md docs/plans/2026-03-09-drawio-cli-export-skill-implementation.md
git commit -m "docs(plan): add drawio cli export skill design and implementation plan"
```

### Task 2: 撰寫 `SKILL.md` 的觸發條件與標準流程

**Files:**
- Modify: `/Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md`
- Test: `/Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md`

**Step 1: Write the failing test**

定義主文件最少必須包含：
- frontmatter `name` / `description`
- `When to Use`
- `Workflow`
- `Common Scenarios`
- `Troubleshooting`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "^name:|^description:|## When to Use|## Workflow|## Common Scenarios|## Troubleshooting" /Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md
```
Expected: 關鍵段落缺漏（FAIL）。

**Step 3: Write minimal implementation**

補齊精簡可掃描的 skill 主體，避免把長旗標說明塞進主文件。

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "^name:|^description:|## When to Use|## Workflow|## Common Scenarios|## Troubleshooting" /Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md
```
Expected: 所有段落都存在（PASS）。

**Step 5: Commit**

```bash
git add /Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md
git commit -m "docs(skill): add drawio cli export workflow skill"
```

### Task 3: 撰寫 `cli-reference.md` 的實測資料

**Files:**
- Modify: `/Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md`
- Test: `/Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md`

**Step 1: Write the failing test**

定義參考檔必須包含：
- binary path
- verified flags
- verified commands
- observed behaviors

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Binary Path|Verified Flags|Verified Commands|Observed Behaviors" /Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md
```
Expected: 任一段落缺漏則 FAIL。

**Step 3: Write minimal implementation**

填入本機已驗證過的 `draw.io` CLI 旗標與命令，並記錄像 `page-index` 為 1-based 等觀察。

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Binary Path|Verified Flags|Verified Commands|Observed Behaviors" /Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md
```
Expected: 所有段落存在（PASS）。

**Step 5: Commit**

```bash
git add /Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md
git commit -m "docs(skill): add verified drawio cli export reference"
```

### Task 4: 以實際 draw.io CLI 驗證高頻情境

**Files:**
- Verify: `/Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md`
- Verify: `/Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md`
- Reference: `/Users/xuxiping/tsmc/port-scan-mk3/docs/architecture/port-scan-mk3-architecture.drawio`

**Step 1: Write the failing test**

定義至少要成功驗證以下情境：
- 單頁 PNG
- 單頁 SVG
- 多頁 PDF
- 透明 PNG
- 未指定輸出檔名時的預設輸出推導

**Step 2: Run test to verify it fails**

Run:
```bash
find "$TMPDIR" -maxdepth 1 \( -name '*.png' -o -name '*.svg' -o -name '*.pdf' \)
```
Expected: 尚未執行驗證前找不到本次輸出（FAIL）。

**Step 3: Write minimal implementation**

用實際指令驗證上述情境，將成功案例與觀察結果補進 skill 參考檔。

**Step 4: Run test to verify it passes**

Run:
```bash
test -f <png-output>
test -f <svg-output>
test -f <pdf-output>
```
Expected: 各情境輸出檔存在且格式正確（PASS）。

**Step 5: Commit**

```bash
git add /Users/xuxiping/.codex/skills/drawio-cli-export/SKILL.md /Users/xuxiping/.codex/skills/drawio-cli-export/cli-reference.md
git commit -m "test(skill): verify drawio cli export scenarios"
```
