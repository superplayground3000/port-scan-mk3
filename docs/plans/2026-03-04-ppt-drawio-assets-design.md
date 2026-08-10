# 2026-03-04 PPT Draw.io Assets Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. 背景與目標

本設計要補強既有 `port-scan-mk3` 專案簡報稿（19 頁），新增一份 **draw.io 相容** 且可讀的 HTML 圖資檔，供團隊：
- 直接閱讀圖像化內容（可講解）
- 從原生 mxGraph XML 片段匯入 draw.io（可轉製圖）

本次使用者已確認需求：
- 範圍：19 張（幾乎每頁投影片都對應一張）
- 交付形態：單一 HTML 檔案
- 目標：可讀性 + 可轉製圖兩者都要

## 2. 問題定義

目前專案已有：
- 文字逐字稿：`docs/plans/2026-03-03-project-intro-ppt-script.md`
- 簡報檔：`docs/plans/2026-03-03-project-intro-ppt-script.pptx`
- 架構圖頁：`docs/architecture/diagram.html`

但缺少一份針對 19 頁完整覆蓋、可直接對接 draw.io 的圖資中介。結果是：
- 新成員難以快速把逐字稿轉成可編修圖
- 後續投影片調整時，圖稿與文字容易失步

## 3. 方案比較與決策

### 方案 A（採用）：draw.io 原生 XML 為主 + HTML 展示層

- 在單一 HTML 中，對每頁提供：Preview + mxGraph XML + Mapping Notes。
- 優點：匯入 draw.io 最穩；同時保留可讀性。
- 缺點：單檔較長。

### 方案 B：純 SVG/HTML 視覺稿 + 規格表

- 優點：閱讀體驗好。
- 缺點：不是 draw.io 原生資料，重建成本高。

### 方案 C：JSON 中介 + 轉換器

- 優點：可程式化擴充。
- 缺點：初期複雜度高，超出目前需求（YAGNI）。

## 4. 目標交付與檔案布局

### 4.1 交付檔案

- `docs/architecture/drawio-assets.html`

### 4.2 HTML 章節結構

單一檔案包含：
1. 封面與使用說明（如何匯入 draw.io）
2. 目錄（`#slide-01` ~ `#slide-19`）
3. 每頁 section（固定模板）

每個 section 固定 4 塊：
- `Diagram Preview`
- `mxGraph XML`
- `Mapping Notes`
- `Validation Checklist`

## 5. 圖元語言（Visual Grammar）

### 5.1 節點類型

- `Process`：主流程步驟（圓角矩形）
- `Data`：輸入/輸出（平行四邊形）
- `Control`：控制策略（六角形）
- `Decision`：判斷（菱形）
- `Artifact`：檔案產物（文件形）
- `Boundary`：邊界/非目標（虛線框）

### 5.2 連線類型

- `Solid Arrow`：主流程
- `Dashed Arrow`：控制訊號（如 pressure API）
- `Dotted Arrow`：衍生輸出（如 report、open-only）

### 5.3 命名與可追蹤性

- 節點 ID：`Sxx-Nyy`（例：`S09-N03`）
- 邊 ID：`Sxx-Eyy`（例：`S09-E02`）

## 6. 內容映射策略（19 頁）

### 6.1 Group A：Slide 1-3（目標/價值）

- 模型：`Problem -> Strategy -> Outcome`
- 目的：建立背景與效益，不過度下鑽 runtime。

### 6.2 Group B：Slide 4-10（架構）

- 模型：分層 + pipeline。
- Slide 9 特殊：TCP 連線生命週期圖，明確標示
  - `net.DialTimeout("tcp", target, timeout)`
  - 連線成功後 `conn.Close()`
  - 非 raw SYN/RST 掃描、非主動 RST 強制斷線

### 6.3 Group C：Slide 11-14（功能）

- 模型：功能模組 + 輸入輸出 + 失敗分支。

### 6.4 Group D：Slide 15-17（使用）

- 模型：操作流程圖 + 排障 decision tree。

### 6.5 Group E：Slide 18-19（品質/邊界）

- 模型：Quality gate pipeline + Scope boundary。

## 7. 資料流設計

1. Source of Truth：
- `docs/plans/2026-03-03-project-intro-ppt-script.md`
2. 映射：
- 每頁 `On-slide bullets` -> 節點與連線
- 每頁 `逐字講稿` -> Mapping Notes
3. 產出：
- 同一 section 內提供 Preview 與 mxGraph XML
4. 匯入：
- 使用者可複製 XML 到 draw.io（Arrange/Insert XML 或匯入流程）

## 8. 錯誤處理與降級策略

- 若某頁難以圖像化，標記 `Narrative-Only`，仍保留最小可匯入圖（1-2 節點）。
- XML 片段若過大或不穩定，提供 `Simplified XML`（保底版本）。
- HTML 與 XML 中特殊字元必須 escape，避免破版或匯入失敗。

## 9. 測試與驗收

### 9.1 結構檢查

- 必須存在 19 個 slide section。
- 每 section 必須有 `Preview/XML/Mapping/Checklist`。

### 9.2 關鍵語意檢查

- Slide 9 / Slide 19 必須出現：
  - `DialTimeout`
  - `conn.Close()`
  - `非 RST 強制斷線`（或同義明確敘述）

### 9.3 手動可用性驗證

- 在瀏覽器可完整閱讀。
- 隨機抽 3 張（含 Slide 9）在 draw.io 成功匯入。

## 10. 非目標

- 不在本次導入自動化 HTML->PPT 或 XML 轉檔 pipeline。
- 不在本次重做既有 PPT 文字內容。
- 不引入額外前端框架或 build step。

## 11. 風險與對策

- 風險：單檔太大難維護。
  - 對策：統一 section 模板 + 目錄錨點 + 命名規範。
- 風險：圖與講稿漂移。
  - 對策：每 section 保留 Mapping Notes，明確指向 slide 意圖。
- 風險：draw.io 匯入差異。
  - 對策：每頁提供可匯入的簡化保底 XML。

## 12. 實作前置結論

設計已由使用者核准。下一步僅銜接 `writing-plans`，產出可執行的 implementation plan，不直接進入實作。
