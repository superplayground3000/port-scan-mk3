# PPT Draw.io HTML Assets Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 建立一份 `docs/architecture/drawio-assets.html`，完整覆蓋 19 張簡報頁面，且每頁都同時具備可讀預覽與可匯入 draw.io 的 mxGraph XML 片段。

**Architecture:** 以單一靜態 HTML 為交付主體，採統一 section 模板（Preview/XML/Mapping/Checklist）。內容來源對齊 `docs/plans/2026-03-03-project-intro-ppt-script.md`，並以固定 ID 命名規則（`Sxx-Nyy`, `Sxx-Eyy`）維持可維護性。匯入相容性由保底簡化 XML 與抽樣匯入驗證確保。

**Tech Stack:** HTML5、CSS3、少量內嵌 SVG、mxGraph XML（draw.io 匯入格式）、Shell 驗證命令（`rg`, `wc`, `sed`）。

---

### Task 1: 建立 HTML 骨架與 19 個 section 錨點

**Files:**
- Create: `docs/architecture/drawio-assets.html`
- Test: `docs/architecture/drawio-assets.html`

**Step 1: Write the failing test**

先定義最小骨架驗收條件（尚未實作前應 fail）：
- 檔案存在
- 含 `#slide-01` 到 `#slide-19` 錨點
- 含目錄 `<nav>` 區塊

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "slide-01|slide-19|<nav" docs/architecture/drawio-assets.html
```
Expected: 檔案不存在或找不到關鍵字（FAIL，屬預期）。

**Step 3: Write minimal implementation**

建立骨架：
```html
<!doctype html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8" />
  <title>port-scan-mk3 draw.io assets</title>
</head>
<body>
  <header><h1>Draw.io Compatible Assets</h1></header>
  <nav>
    <a href="#slide-01">Slide 01</a>
    ...
    <a href="#slide-19">Slide 19</a>
  </nav>
  <main>
    <section id="slide-01"></section>
    ...
    <section id="slide-19"></section>
  </main>
</body>
</html>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "slide-01|slide-19|<nav" docs/architecture/drawio-assets.html
```
Expected: 三個條件都能匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html
git commit -m "docs(architecture): scaffold drawio assets html with 19 anchors"
```

### Task 2: 套用統一模板（Preview/XML/Mapping/Checklist）

**Files:**
- Modify: `docs/architecture/drawio-assets.html`
- Test: `docs/architecture/drawio-assets.html`

**Step 1: Write the failing test**

定義每個 section 必須有四個區塊標題：
- `Diagram Preview`
- `mxGraph XML`
- `Mapping Notes`
- `Validation Checklist`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/drawio-assets.html
```
Expected: 數量不足，無法覆蓋 19 個 section（FAIL，屬預期）。

**Step 3: Write minimal implementation**

新增可重複 section 模板（示意）：
```html
<section id="slide-09" class="slide-block">
  <h2>Slide 09 - TCP Probe Lifecycle</h2>
  <h3>Diagram Preview</h3>
  <svg><!-- preview --></svg>
  <h3>mxGraph XML</h3>
  <pre class="xml-block"><code>&lt;mxGraphModel&gt;...&lt;/mxGraphModel&gt;</code></pre>
  <h3>Mapping Notes</h3>
  <ul><li>對應逐字稿第 9 頁重點...</li></ul>
  <h3>Validation Checklist</h3>
  <ul><li>[ ] draw.io import tested</li></ul>
</section>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/drawio-assets.html
```
Expected: 四種標題均出現且覆蓋所有 section（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html
git commit -m "docs(architecture): apply section template for drawio asset blocks"
```

### Task 3: 實作 Slide 1-10 的預覽與 XML（含 TCP 生命週期）

**Files:**
- Modify: `docs/architecture/drawio-assets.html`
- Test: `docs/architecture/drawio-assets.html`
- Reference: `docs/plans/2026-03-03-project-intro-ppt-script.md`

**Step 1: Write the failing test**

先驗證必要關鍵字（尚未補齊前應 fail）：
- `Slide 04` pipeline
- `Slide 09` 的 `DialTimeout`、`conn.Close()`、`非 RST 強制斷線`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Slide 04|Slide 09|DialTimeout|conn.Close\(|非 RST 強制斷線" docs/architecture/drawio-assets.html
```
Expected: 任一關鍵項缺漏即 FAIL（屬預期）。

**Step 3: Write minimal implementation**

補齊 Slide 1-10：
- Slide 1-3：Problem/Strategy/Outcome 圖
- Slide 4-8：pipeline + layer 圖
- Slide 9：TCP lifecycle 圖
- Slide 10：output + resume 圖

Slide 9 XML 需至少含以下語意節點：
```xml
<mxCell id="S09-N01" value="DialTimeout(tcp,target,timeout)" .../>
<mxCell id="S09-N02" value="Connected" .../>
<mxCell id="S09-N03" value="conn.Close()" .../>
<mxCell id="S09-N04" value="Not SYN/RST raw scan" .../>
<mxCell id="S09-N05" value="No active RST forced teardown" .../>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Slide 04|Slide 09|DialTimeout|conn.Close\(|非 RST 強制斷線" docs/architecture/drawio-assets.html
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html
git commit -m "docs(architecture): add drawio-ready diagrams for slides 1-10"
```

### Task 4: 實作 Slide 11-19 的預覽與 XML（功能/使用/品質/邊界）

**Files:**
- Modify: `docs/architecture/drawio-assets.html`
- Test: `docs/architecture/drawio-assets.html`
- Reference: `docs/plans/2026-03-03-project-intro-ppt-script.md`

**Step 1: Write the failing test**

定義驗收關鍵字：
- `Slide 18` quality gates
- `Slide 19` boundary / 非目標
- `go test ./...`, `coverage_gate.sh`, `e2e/run_e2e.sh`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Slide 18|Slide 19|go test ./...|coverage_gate\.sh|e2e/run_e2e\.sh|Boundary" docs/architecture/drawio-assets.html
```
Expected: 未齊全前 FAIL。

**Step 3: Write minimal implementation**

補齊 Slide 11-19：
- 功能頁：fail-fast / rich input / pressure-aware / observability
- 使用頁：最小流程 + 常用參數 + 排障
- 品質頁：3-level gate pipeline
- 邊界頁：scope 與非目標

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Slide 18|Slide 19|go test ./...|coverage_gate\.sh|e2e/run_e2e\.sh|Boundary" docs/architecture/drawio-assets.html
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html
git commit -m "docs(architecture): complete drawio-ready diagrams for slides 11-19"
```

### Task 5: 加入樣式、可讀性與匯入說明

**Files:**
- Modify: `docs/architecture/drawio-assets.html`
- Test: `docs/architecture/drawio-assets.html`

**Step 1: Write the failing test**

定義可讀性條件：
- 具目錄 sticky 導航
- code block 可捲動
- 顯示匯入 draw.io 的操作步驟

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Import to draw.io|position: sticky|overflow: auto" docs/architecture/drawio-assets.html
```
Expected: 若缺任一條件則 FAIL。

**Step 3: Write minimal implementation**

加入基本樣式與說明：
```html
<style>
nav { position: sticky; top: 0; }
.xml-block { overflow: auto; max-height: 240px; }
</style>
<p><strong>Import to draw.io:</strong> copy XML block and use draw.io Insert/Import XML.</p>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Import to draw.io|position: sticky|overflow: auto" docs/architecture/drawio-assets.html
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html
git commit -m "docs(architecture): improve readability and drawio import instructions"
```

### Task 6: 最終驗證與交付說明

**Files:**
- Modify: `docs/architecture/drawio-assets.html`（若驗證後需微調）
- Modify: `docs/plans/2026-03-03-project-intro-ppt-script.md`（僅在需要 cross-link 時）
- Test: `docs/architecture/drawio-assets.html`

**Step 1: Write the failing test**

定義最終 gate：
- 19 個 slide section
- Slide 9/19 關鍵語意完整
- 每頁都有四段模板

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "^<section id=\"slide-" docs/architecture/drawio-assets.html | wc -l
rg -n "DialTimeout|conn.Close\(|非 RST 強制斷線" docs/architecture/drawio-assets.html
rg -n "Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/drawio-assets.html
```
Expected: 任一不符則 FAIL。

**Step 3: Write minimal implementation**

補齊缺漏 section、語意或模板內容，確保全部 gate 通過。

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "^<section id=\"slide-" docs/architecture/drawio-assets.html | wc -l
rg -n "DialTimeout|conn.Close\(|非 RST 強制斷線" docs/architecture/drawio-assets.html
rg -n "Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/drawio-assets.html
```
Expected:
- section 行數 = 19
- 關鍵語意存在
- 模板關鍵字完整

**Step 5: Commit**

```bash
git add docs/architecture/drawio-assets.html docs/plans/2026-03-03-project-intro-ppt-script.md
git commit -m "docs: finalize drawio-compatible html assets for 19-slide deck"
```

## 執行注意事項

- 建議採 `@superpowers/test-driven-development` 節奏實作每個 Task。
- 完工前必做 `@superpowers/verification-before-completion` 檢核，避免口頭宣稱完成。
- 如要多工切分 Slide 群組，可再用 `@superpowers/subagent-driven-development`。
