# SOLID Refactor + TDD Enforcement Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 背景

本 feature 的目標不是新增 CLI 功能，而是在不破壞既有 operator contract 的前提下，
把目前集中於 `cmd/port-scan/main.go` 與 `pkg/scanapp/scan.go` 的混合責任拆成可測試、
可審查、可增量交付的邊界，並以專案憲章要求的 SOLID 原則與 TDD 流程作為硬約束。

目前的主要 hotspot 很明確：

- `cmd/port-scan/main.go`
  同時處理命令路由、參數解析、輸入驗證、scan 啟動與錯誤碼映射。
- `pkg/scanapp/scan.go`
  單檔承載輸入載入、runtime 建立、worker fan-out、dispatch、pause/pressure control、
  result aggregation、progress reporting、resume persistence、logger 與 helper function。

這種結構的風險不是抽象不夠多，而是責任過多、依賴方向不清楚、任何小改動都容易牽連
多個不相干 concerns，讓 reviewer 很難判斷重構是否安全。

## 目標

- 把 `cmd/port-scan` 收斂成 composition root 與 CLI glue。
- 把 `pkg/scanapp` 拆成小型、單一責任、可由消費端擁有的協作者邊界。
- 以文件先定義「不能破壞的契約」與「可接受的 TDD 證據」。
- 後續實作嚴格依照 `specs/001-solid-refactor-tdd/tasks.md` 的順序：
  先完成 `T001-T006` 文件與契約，再進入 `T007+` 的基線測試與重構。

## 非目標

- 不在此 feature 中擴充新 CLI 指令或新業務能力。
- 不做全專案一次性大重寫。
- 不在沒有 failing test 的前提下直接改 production code。
- 不因為內部拆分而默默改變 exit code、輸出語意、progress/resume 可見行為。

## 設計總覽

### 一、T001-T006 的文件角色分工

這六個文件不是輔助說明，而是後續重構的操作規範。

- `specs/001-solid-refactor-tdd/research.md`
  - 用途：記錄 hotspot、拆分順序、延後處理區域與取捨理由。
  - 作用：避免後續重構失焦或把範圍擴成全專案清理。

- `specs/001-solid-refactor-tdd/contracts/cli-stability-contract.md`
  - 用途：定義 `validate` / `scan` 這次不能偷偷改掉的外部契約。
  - 作用：作為 `T007`、`T008`、`T013` baseline tests 的依據。

- `specs/001-solid-refactor-tdd/contracts/tdd-evidence-contract.md`
  - 用途：定義每個增量必須保存的 red/green/refactor 證據。
  - 作用：讓 reviewer 能審查 TDD 是否真的發生，而不是憑口頭說明。

- `specs/001-solid-refactor-tdd/contracts/runtime-boundaries.md`
  - 用途：定義 `scanapp` 內部的目標責任邊界與依賴方向。
  - 作用：作為後續 `input_loader.go`、`runtime_builder.go`、
    `task_dispatcher.go`、`result_aggregator.go`、`resume_manager.go`
    等拆分的設計基準。

- `specs/001-solid-refactor-tdd/data-model.md`
  - 用途：定義這次重構中會反覆使用的核心設計實體與語彙。
  - 作用：確保 tasks、review 與 quickstart 的用語一致。

- `specs/001-solid-refactor-tdd/quickstart.md`
  - 用途：成為 reviewer 與 maintainer 的驗證手冊。
  - 作用：後續每個 user story 完成時，都要回填 red/green/refactor
    命令與預期結果，最後也要記錄 gate 證據。

### 二、文件之間的銜接順序

文件的工作流固定如下：

1. `research.md` 定義問題範圍與第一波 hotspot。
2. `runtime-boundaries.md` 與 `cli-stability-contract.md` 定義內外邊界。
3. `tdd-evidence-contract.md` 定義可接受的工作方式與審查標準。
4. `data-model.md` 統一設計語彙。
5. `quickstart.md` 承接所有後續可執行驗證。

這個順序的目的，是先鎖定不能破壞的東西，再開始寫 baseline tests。

### 三、每份文件的最小可執行骨架

- `research.md`
  - `Current Hotspots`
  - `Refactor Order`
  - `Deferred Areas`

- `cli-stability-contract.md`
  - `Protected Commands`
  - `Protected Exit Codes`
  - `Protected Output Semantics`
  - `Allowed Internal Changes`

- `tdd-evidence-contract.md`
  - `Required Red Evidence`
  - `Required Green Evidence`
  - `Required Refactor Evidence`
  - `Invalid Shortcuts`

- `runtime-boundaries.md`
  - `Current Mixed Responsibilities`
  - `Target Boundaries`
  - `Dependency Direction`
  - `Boundary Ownership Rules`

- `data-model.md`
  - `Responsibility Boundary`
  - `Refactor Increment`
  - `Protected Contract`
  - `Verification Evidence`
  - `Runtime Collaborator`

- `quickstart.md`
  - `Baseline Verification`
  - `US1 Verification`
  - `US2 Verification`
  - `US3 Verification`
  - `Final Gates`

## 後續實作約束

完成 `T001-T006` 後，`T007+` 的實作必須遵守以下規則：

- 先補 baseline tests，再抽 collaborator。
- 抽 collaborator 不可順便改 CLI 契約。
- 每個 user story 都要在 `quickstart.md` 補 red/green/refactor 命令。
- 只有當 scan flow、writer 或 resume 行為受影響時，才升級到 e2e gate。
- 第一波實作只聚焦：
  - `cmd/port-scan/main.go`
  - `pkg/scanapp/scan.go`
  - 與其直接相關的現有測試檔

## 驗收標準

- `T001-T006` 產出的文件能直接作為 `T007+` 的測試與重構依據。
- reviewer 可以從文件中看出：
  - 哪些 operator contract 被保護
  - 哪些 runtime 責任邊界要被建立
  - 什麼才算可接受的 TDD 證據
- 後續 user story 任務不需要重新發明邊界命名與審查標準。
