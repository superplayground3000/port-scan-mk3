# Detailed Architecture Drawings (Draw.io + HTML) Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 產出一套可直接用於工程與架構評審的詳細架構圖，包含 `.drawio` SSOT 與 `.html` 展示稿，完整覆蓋 component interaction、I/O 契約、happy path、sad path。

**Architecture:** 先建立多 page 的 `.drawio` 作為唯一真實來源（P01-P04），再同步輸出對映的 `.html` 檔，每頁包含預覽、可匯入 mxGraph XML、以及 mapping notes。最後以結構檢查與語意檢查驗證頁面完整性與關鍵邊界（TCP dial 模型與非 RST 強制斷線）。

**Tech Stack:** draw.io mxGraph XML、HTML5/CSS3、Shell 驗證（`rg`, `sed`, `wc`）、既有專案文件（`README.md`, `docs/cli/flags.md`, `docs/e2e/overview.md`, `pkg/scanapp/scan.go`, `pkg/scanner/scanner.go`）。

---

### Task 1: 建立 `.drawio` SSOT 檔案與 4 個 page 骨架

**Files:**
- Create: `docs/architecture/port-scan-mk3-architecture.drawio`
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`

**Step 1: Write the failing test**

定義骨架完成條件：
- 檔案存在
- 含 4 個 page：`P01-System-Overview`, `P02-Happy-Path-Dataflow`, `P03-Sad-Path-Error-Recovery`, `P04-Component-Contracts`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "P01-System-Overview|P02-Happy-Path-Dataflow|P03-Sad-Path-Error-Recovery|P04-Component-Contracts" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 檔案不存在或匹配不足（FAIL，屬預期）。

**Step 3: Write minimal implementation**

建立最小 `.drawio` XML（示意）：
```xml
<mxfile host="app.diagrams.net" version="26.0.0">
  <diagram id="p01" name="P01-System-Overview"><mxGraphModel>...</mxGraphModel></diagram>
  <diagram id="p02" name="P02-Happy-Path-Dataflow"><mxGraphModel>...</mxGraphModel></diagram>
  <diagram id="p03" name="P03-Sad-Path-Error-Recovery"><mxGraphModel>...</mxGraphModel></diagram>
  <diagram id="p04" name="P04-Component-Contracts"><mxGraphModel>...</mxGraphModel></diagram>
</mxfile>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "P01-System-Overview|P02-Happy-Path-Dataflow|P03-Sad-Path-Error-Recovery|P04-Component-Contracts" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 4 頁名稱全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio
git commit -m "docs(architecture): scaffold 4-page drawio architecture source"
```

### Task 2: 完成 P01 系統總覽互動圖（component + I/O 主線）

**Files:**
- Modify: `docs/architecture/port-scan-mk3-architecture.drawio`
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`
- Reference: `docs/architecture/diagram.html`, `README.md`

**Step 1: Write the failing test**

定義 P01 必含 component：
- `CLI/config`, `input`, `task`, `scanapp`, `scanner`, `speedctrl`, `writer`, `state`, `logx`
- 外部節點：`CIDR CSV`, `Port Input`, `pressure API`, `Targets`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "CLI/config|input|task|scanapp|scanner|speedctrl|writer|state|logx|CIDR CSV|Port Input|pressure API|Targets" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 若任一節點缺漏則 FAIL。

**Step 3: Write minimal implementation**

在 P01 補齊節點與主線連接，示意：
```xml
<mxCell id="P01-N03" value="input" .../>
<mxCell id="P01-N04" value="task" .../>
<mxCell id="P01-E03" source="P01-N03" target="P01-N04" .../>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "CLI/config|input|task|scanapp|scanner|speedctrl|writer|state|logx|CIDR CSV|Port Input|pressure API|Targets" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio
git commit -m "docs(architecture): complete P01 system overview interactions"
```

### Task 3: 完成 P02 Happy Path Dataflow（含 TCP dial 邊界）

**Files:**
- Modify: `docs/architecture/port-scan-mk3-architecture.drawio`
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`
- Reference: `pkg/scanapp/scan.go`, `pkg/scanner/scanner.go`

**Step 1: Write the failing test**

定義 P02 必含：
- `validate success`
- `task expand + dedup`
- `dispatch -> scan`
- `net.DialTimeout("tcp", target, timeout)`
- `conn.Close()`
- `scan_results-*` / `opened_results-*`
- 邊界聲明：非 SYN/RST raw packet 掃描、非 RST 強制斷線

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "validate success|task expand|dedup|DialTimeout|conn.Close\(|scan_results|opened_results|SYN/RST|非 RST 強制斷線" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 未齊全前 FAIL。

**Step 3: Write minimal implementation**

補齊 P02 節點（12-16 個）及連線，示意：
```xml
<mxCell id="P02-N08" value="net.DialTimeout(tcp,target,timeout)" .../>
<mxCell id="P02-N09" value="Connected -> conn.Close()" .../>
<mxCell id="P02-N12" value="Boundary: 非 SYN/RST raw scan, 非 RST 強制斷線" .../>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "validate success|task expand|dedup|DialTimeout|conn.Close\(|scan_results|opened_results|SYN/RST|非 RST 強制斷線" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio
git commit -m "docs(architecture): add P02 happy path with tcp dial boundary"
```

### Task 4: 完成 P03 Sad Path（3 類失敗分支 + recovery）

**Files:**
- Modify: `docs/architecture/port-scan-mk3-architecture.drawio`
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`
- Reference: `docs/e2e/overview.md`, `pkg/scanapp/scan.go`

**Step 1: Write the failing test**

定義 P03 必含 3 分支：
1. `validation fail -> early exit`
2. `pressure API repeated failure -> escalation -> save resume -> fail exit`
3. `cancel/signal -> save resume -> rerun with -resume`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "validation fail|early exit|pressure API|repeated failure|escalation|resume_state|cancel|signal|rerun|\-resume" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 缺漏時 FAIL。

**Step 3: Write minimal implementation**

補齊 P03 節點（14-18 個）與決策分支，示意：
```xml
<mxCell id="P03-N05" value="pressure API failure count >= 3" style="rhombus;..." .../>
<mxCell id="P03-N06" value="save resume_state" .../>
<mxCell id="P03-N07" value="exit non-zero" .../>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "validation fail|early exit|pressure API|repeated failure|escalation|resume_state|cancel|signal|rerun|\-resume" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 全部分支要件都匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio
git commit -m "docs(architecture): add P03 sad paths and recovery flows"
```

### Task 5: 完成 P04 Component Contracts（責任 + I/O 結構）

**Files:**
- Modify: `docs/architecture/port-scan-mk3-architecture.drawio`
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`
- Reference: `pkg/input`, `pkg/task`, `pkg/scanapp`, `pkg/writer`, `pkg/state`

**Step 1: Write the failing test**

定義每個核心 component 需含 3 欄：
- `Input`
- `Process`
- `Output`

核心 component：`input`, `task`, `scanapp`, `scanner`, `writer`, `state`, `speedctrl`, `logx`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "Input:|Process:|Output:|input|task|scanapp|scanner|writer|state|speedctrl|logx" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 若任何 component 的三欄缺漏則 FAIL。

**Step 3: Write minimal implementation**

以契約卡片方式補齊 P04，示意：
```xml
<mxCell id="P04-N03" value="input\nInput: CIDR/Port rows\nProcess: fail-fast validate\nOutput: normalized records" .../>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "Input:|Process:|Output:|input|task|scanapp|scanner|writer|state|speedctrl|logx" docs/architecture/port-scan-mk3-architecture.drawio
```
Expected: 所有 component 契約資訊完整（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio
git commit -m "docs(architecture): add P04 component contracts and IO structures"
```

### Task 6: 建立 HTML 對映展示檔（4 sections + XML blocks）

**Files:**
- Create: `docs/architecture/port-scan-mk3-architecture.html`
- Test: `docs/architecture/port-scan-mk3-architecture.html`
- Reference: `docs/architecture/drawio-assets.html`

**Step 1: Write the failing test**

定義 HTML 必含：
- `#p01`~`#p04` 四個 section
- 每 section 有 `Diagram Preview`, `mxGraph XML`, `Mapping Notes`, `Validation Checklist`

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "id=\"p01\"|id=\"p02\"|id=\"p03\"|id=\"p04\"|Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/port-scan-mk3-architecture.html
```
Expected: 檔案不存在或匹配不足（FAIL）。

**Step 3: Write minimal implementation**

建立 HTML 並貼入對應 page XML，示意：
```html
<section id="p02">
  <h2>P02-Happy-Path-Dataflow</h2>
  <h3>Diagram Preview</h3>
  <div>...</div>
  <h3>mxGraph XML</h3>
  <textarea readonly>...</textarea>
  <h3>Mapping Notes</h3>
  <ul>...</ul>
  <h3>Validation Checklist</h3>
  <ul>...</ul>
</section>
```

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "id=\"p01\"|id=\"p02\"|id=\"p03\"|id=\"p04\"|Diagram Preview|mxGraph XML|Mapping Notes|Validation Checklist" docs/architecture/port-scan-mk3-architecture.html
```
Expected: 全部匹配（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.html
git commit -m "docs(architecture): add html mapping for 4-page drawio architecture set"
```

### Task 7: 最終驗收（結構、語意、匯入相容）

**Files:**
- Modify: `docs/architecture/port-scan-mk3-architecture.drawio`（若需修正）
- Modify: `docs/architecture/port-scan-mk3-architecture.html`（若需修正）
- Test: `docs/architecture/port-scan-mk3-architecture.drawio`, `docs/architecture/port-scan-mk3-architecture.html`

**Step 1: Write the failing test**

定義最終 gate：
- 4 pages / 4 sections 一致
- P02/P03 關鍵語意完整
- P03 含三類 sad path
- XML 可抽樣匯入 draw.io

**Step 2: Run test to verify it fails**

Run:
```bash
rg -n "P01-System-Overview|P02-Happy-Path-Dataflow|P03-Sad-Path-Error-Recovery|P04-Component-Contracts" docs/architecture/port-scan-mk3-architecture.drawio
rg -n "id=\"p01\"|id=\"p02\"|id=\"p03\"|id=\"p04\"" docs/architecture/port-scan-mk3-architecture.html
rg -n "DialTimeout|conn.Close\(|非 RST 強制斷線|validation fail|pressure API|resume_state|\-resume" docs/architecture/port-scan-mk3-architecture.drawio docs/architecture/port-scan-mk3-architecture.html
```
Expected: 若任何關鍵語意缺漏則 FAIL。

**Step 3: Write minimal implementation**

修補缺漏並同步 `.drawio` 與 `html`。

**Step 4: Run test to verify it passes**

Run:
```bash
rg -n "P01-System-Overview|P02-Happy-Path-Dataflow|P03-Sad-Path-Error-Recovery|P04-Component-Contracts" docs/architecture/port-scan-mk3-architecture.drawio
rg -n "id=\"p01\"|id=\"p02\"|id=\"p03\"|id=\"p04\"" docs/architecture/port-scan-mk3-architecture.html
rg -n "DialTimeout|conn.Close\(|非 RST 強制斷線|validation fail|pressure API|resume_state|\-resume" docs/architecture/port-scan-mk3-architecture.drawio docs/architecture/port-scan-mk3-architecture.html
```
Expected: 全部 gate 通過（PASS）。

**Step 5: Commit**

```bash
git add docs/architecture/port-scan-mk3-architecture.drawio docs/architecture/port-scan-mk3-architecture.html
git commit -m "docs(architecture): finalize detailed architecture drawio and html diagrams"
```

## Execution Notes

- 實作時優先使用 `@superpowers/subagent-driven-development`。
- 每個 Task 實作後先做 spec compliance review，再做 code quality review。
- 完成前執行 `@superpowers/verification-before-completion`，不可只憑目測宣稱完成。
