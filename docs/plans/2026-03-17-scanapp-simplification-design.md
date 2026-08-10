# Scanapp Simplification Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 背景

這次工作的目標不是新增功能，而是基於現有程式碼提出一條更簡潔、且嚴格符合
`constitution.md` 的重構路徑。重點在於降低責任集中、縮小變更影響面、保護 CLI
契約，並讓後續重構能以 TDD 穩定推進。

## 現況觀察

主要複雜度集中在兩個位置：

- `pkg/scanapp/scan.go`
  - `Run` 同時持有 input 預設值、runtime 建立、輸出檔建立、worker lifecycle、
    pressure/keyboard 控制、result aggregation、resume 決策、logger helper。
  - 即使已經有 `input_loader.go`、`runtime_builder.go`、`task_dispatcher.go`、
    `pressure_monitor.go`、`result_aggregator.go`、`resume_manager.go`，核心流程仍然
    過於集中。

- `cmd/port-scan/command_handlers.go`
  - CLI 已拆出 command handler，但 `validateInputs` 仍直接握有 input parsing 細節。
  - 這讓 `cmd/port-scan` 不只是 composition root，也成了 validation 實作持有者。

次要觀察：

- `pkg/pipeline/runner.go` 目前沒有被 production code 使用，屬於可疑抽象。
- `scanTarget`、`scanTask`、`writer.Record` 之間存在大量重複 metadata 欄位。
- 某些 helper 仍直接依賴整個 `config.Config`，而不是窄化後的 policy 資料。

## 目標

- 讓 `cmd/port-scan` 僅保留 CLI 組裝、參數解析、stdout/stderr wiring、exit code mapping。
- 讓 `pkg/scanapp` 成為單一入口 facade，而不是混合所有 runtime policy 的大型檔案。
- 移除沒有實際價值的抽象與重複資料搬運。
- 維持現有 CLI 契約、輸出檔格式、resume 行為、progress/observability 行為不變。

## 非目標

- 不新增 CLI 子命令。
- 不修改使用者可見的 CSV schema、exit code 或 flag contract。
- 不做一次性 package 大搬遷。
- 不在沒有 failing test 的前提下直接改 production behavior。

## 方案比較

### 方案 A: 保守拆檔

只進一步拆薄 `pkg/scanapp/scan.go`，把 worker pool、output file setup、record mapping
等 helper 再拆到新檔案。

優點：

- 風險低
- 可快速降低單檔行數

缺點：

- `scanapp` 內部責任邊界仍不夠清楚
- 只是搬動程式碼，未必真正降低 change surface

### 方案 B: Facade + Collaborators

保留 `scanapp.Run` 作為唯一公開入口，內部收斂成幾個單一責任協作者，由 `Run`
負責組裝。

協作者分工：

- `planner`
  - input 載入
  - chunk/runtime 準備
  - output path 決定

- `executor`
  - dispatch loop
  - worker pool
  - result channel lifecycle

- `sink`
  - result write
  - progress emission
  - completion summary

- `pause`
  - keyboard pause
  - pressure API polling

- `resume`
  - resume path
  - persistence policy

- `validation service`
  - 將 CLI validation 從 `cmd/port-scan` 移入 `pkg/`

優點：

- 最符合 SOLID 與 consumer-owned narrow seam
- 保留現有 package 邊界，避免過早 package 分裂
- 對 `cmd/port-scan` 與 `pkg/scanapp` 的責任切割最清楚

缺點：

- 需要補更多 baseline tests
- 需要重新整理部分內部型別與測試命名

### 方案 C: 新 package 拆分

把 runtime orchestration 再拆成 `pkg/scanplan`、`pkg/scanexec`、`pkg/scanobs`
之類的新 package。

優點：

- 長期邊界最明顯

缺點：

- 以目前專案規模來說偏重
- 容易引入搬運型 abstraction 與跨 package noise

## 建議方案

採用方案 B。

理由：

- 符合 `constitution.md` 的 Library-First、CLI Contract-First、SOLID Structural
  Boundaries。
- 保持 `cmd/port-scan` 的 product boundary 穩定。
- 避免為了「看起來乾淨」而過早新增 package。
- 能在不改外部契約的前提下，實質降低 `scan.go` 的責任密度。

## 目標結構

### CLI

`cmd/port-scan`

- `main.go`: command routing
- `command_handlers.go`: config parse、scan 啟動、exit code mapping
- `validation` 呼叫移轉到 `pkg/` 服務，不保留 parser 細節

### Scanapp

`pkg/scanapp`

- `run.go`
  - facade orchestration only
  - 組裝 planner/executor/sink/pause/resume

- `planner.go`
  - 合併與整理目前的 input/runtime/output path planning

- `executor.go`
  - worker pool 啟動與回收
  - dispatch coordination

- `sink.go`
  - record 寫入
  - summary/progress

- `pause.go`
  - keyboard/API pause logic

- `resume.go`
  - resume policy 與 persistence

- `runtime_types.go`
  - `chunkRuntime`、`scanTarget`、`scanTask` 等內部型別整理

## 簡化原則

### 1. 不傳整包 config

協作者只接自己需要的欄位。例如 dispatch logic 不應依賴整個 `config.Config`，
只應接 pacing 與 gate 所需資料。

### 2. 減少 metadata 重複搬運

改成較清楚的資料形狀，例如：

- `TargetMeta`
- `ScanEnvelope`
- `RecordMapper`

目的不是新增抽象層，而是避免 `scanTarget -> scanTask -> writer.Record` 三份重複欄位。

### 3. facade 只做組裝

`scanapp.Run` 必須只負責：

- 初始化外部依賴
- 組裝協作者
- 串接高階流程
- 統一錯誤收斂

它不應再直接持有 parsing、dispatch 細節、logger utility 與 record mapping。

### 4. 先刪除可疑抽象，再新增必要邊界

像 `pkg/pipeline/runner.go` 這類未被 production 使用的抽象，需要先確認是否保留。
若沒有明確 consumer，就不應強行把新設計套進去。

## 測試與交付策略

重構順序必須遵守 Test-First Delivery：

1. 鎖住 CLI baseline tests
2. 鎖住 scanapp orchestration behavior tests
3. 逐段抽出 collaborator
4. 每次只改一個責任面
5. `go test ./...` 作為基本 gate
6. 若變更影響 scan pipeline/writer/resume，再跑 coverage/e2e gate

## Constitution Alignment

- Library-First Design
  - validation 與 runtime collaborator 必須留在 `pkg/`，不能回流到 CLI glue。

- CLI Contract-First
  - `validate` / `scan` 的 flags、exit codes、output semantics 必須維持不變。

- Test-First Delivery
  - 每一波 extraction 都要先補 failing test，再做最小重構。

- Integration Coverage for Contract Boundaries
  - 若 validation contract、writer contract、resume contract 受影響，必須更新 integration tests。

- SOLID Structural Boundaries
  - 每個 collaborator 僅有一個主要 reason to change。
  - 介面由 consumer 擁有，避免 mega-interface。
  - 禁止 CLI 依賴 runtime 細節，也禁止 runtime 依賴 CLI glue。

## 決策

後續若要落地實作，建議以「中度結構整理但不新增太多 package」為主軸，先完成
方案 B 的協作者重整與 CLI validation 下沉，再視結果決定是否需要更進一步的 package
切分。

### Task 6 Decision Update

2026-03-17 檢查結果顯示 `pkg/pipeline` 沒有任何 production consumer；目前實際使用的
dispatch 與 execution seam 已經由 `pkg/scanapp/task_dispatcher.go` 與
`pkg/scanapp/executor.go` 承接。基於 YAGNI 與 SOLID，本次決定移除 `pkg/pipeline`
這個未接線的抽象，而不是為了保留它去扭曲 `scanapp` 的實際責任邊界。
