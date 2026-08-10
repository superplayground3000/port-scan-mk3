# ScanApp 測試命名可讀性設計

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

- 日期：2026-03-02
- 狀態：已確認（使用者核准）
- 範圍：`pkg/scanapp` 測試命名統一

## 背景與目標

目前 `pkg/scanapp` 測試命名同時存在多種風格，例如：
- `TestRun_...`
- `TestFetchPressure`
- `Test_observability_progress_summary_events`

命名風格不一致會降低可讀性與搜尋效率。此設計目標是將 `pkg/scanapp` 測試名稱統一為可快速理解「被測目標 / 情境 / 預期結果」的格式。

## 目標

- 將 `pkg/scanapp` 測試名稱統一為：`Test<Function>_<Scenario>_<Expected>`
- 提升測試名稱的可讀性、可搜尋性與維護一致性

## 非目標

- 不改測試邏輯
- 不改測試斷言內容
- 不改測試資料、helper、production code
- 不擴大到 `pkg/scanapp` 以外目錄

## 變更範圍

僅調整以下檔案中的 `func Test...` 名稱：
- `pkg/scanapp/scan_test.go`
- `pkg/scanapp/scan_helpers_test.go`
- `pkg/scanapp/scan_observability_test.go`
- `pkg/scanapp/resume_path_test.go`
- `pkg/scanapp/batch_output_test.go`

## 命名規範

### 命名格式

`Test<Function>_<Scenario>_<Expected>`

### 欄位定義

- `Function`：主要被測函式或入口行為（例如 `Run`、`FetchPressure`）
- `Scenario`：測試條件或情境（例如 `WhenResumeStateProvided`）
- `Expected`：預期結果（例如 `ScansRemainingTargets`）

### 命名準則

- 使用 PascalCase + `_` 分段
- 不使用 snake_case
- 單一測試涵蓋多分支時，以「主意圖」命名，不拆測試
- 名稱需在同檔內唯一，避免重名

## 示例（示意）

- `TestFetchPressure` -> `TestFetchPressure_WhenResponseContainsPressure_ReturnsValue`
- `TestShouldSaveOnDispatchErr` -> `TestShouldSaveOnDispatchErr_WhenCanceledOrDeadlineExceeded_ReturnsTrue`
- `Test_observability_progress_summary_events` -> `TestRun_WhenJSONFormatEnabled_EmitsProgressAndCompletionObservabilityEvents`

## 實作步驟（僅規劃）

1. 盤點 `pkg/scanapp` 所有 `Test...` 名稱
2. 產出「舊名 -> 新名」對照
3. 只修改測試函式名稱
4. 確認無重名與編譯錯誤

## 驗證策略

- 必跑：`go test ./pkg/scanapp`
- 可選擴充：`go test ./...`

## 風險與對策

- 風險：一次性改名可能造成 merge 衝突
- 對策：只變更函式名、維持最小 diff，並以單一 commit 提交

## 成功判準

- `pkg/scanapp` 全部測試名稱符合命名規範
- 測試邏輯與斷言內容無變更
- `go test ./pkg/scanapp` 通過
