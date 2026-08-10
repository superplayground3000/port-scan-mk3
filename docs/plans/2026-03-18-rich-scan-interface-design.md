# Rich Scan Interface Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 背景

目前 `port-scan scan` 在 `stdout` 只輸出簡化 progress line（`progress cidr=...`），
缺少即時整體視角。需求是新增「rich terminal interface」在掃描期間提供可讀性高的即時儀表板，
包含：

- current progress
- current CIDR being scanned 與 bucket status
- current global scan speed
- current global controller status
- API response status（router pressure percentage）

## 需求決策摘要（已確認）

- 介面型態：ANSI 即時覆蓋儀表板（非逐行日誌）
- 啟用策略：預設啟用，非 TTY 自動退回 plain
- bucket status：顯示狀態階段（`waiting_bucket` / `waiting_gate` / `enqueued`）
- global speed：同時顯示 `dispatch/sec` 與 `results/sec`
- CIDR 顯示範圍：只顯示目前 CIDR
- API status：顯示最新 pressure、last update、health（含 fail streak）
- controller status：簡潔單欄 `RUNNING`/`PAUSED(...)`
- `-format=json`：自動關閉 rich 介面，保持機器可讀輸出
- 刷新頻率：500ms
- rich 介面輸出位置：只輸出到 `stderr`

## 方案比較

### 方案 1（採用）：In-process Dashboard Renderer

在 `pkg/scanapp` 增加獨立 dashboard state/renderer，掃描流程以事件更新狀態，
renderer 週期性刷新畫面。

優點：

- 最小侵入，風險最低
- 不改 public CLI 契約
- 符合 `cmd` 僅做組裝、可重用邏輯放 `pkg/`

缺點：

- 需要新增一層狀態聚合與併發保護

### 方案 2：流程節點直接操作 UI

dispatch/poll/result 模組直接呼叫 UI render。

優點：

- 實作直覺

缺點：

- 耦合高，不利維護與測試
- 容易破壞單一責任

### 方案 3：通用 telemetry bus + subscriber

先建立通用 event bus，再以 rich dashboard 訂閱事件。

優點：

- 長期擴充性高

缺點：

- 目前需求下屬於過度設計（YAGNI）

## 架構設計

### 高層原則

- `cmd/port-scan` 不承擔 dashboard 邏輯
- `scanapp.Run` 僅組裝與啟停 dashboard
- dashboard failure 不得中止掃描主流程

### 元件

- `dashboard_state`：
  - thread-safe 聚合執行期狀態
  - 提供快照讀取
- `dashboard_observer`：
  - 接收 dispatch/result/pressure/controller 事件
  - 更新 `dashboard_state`
- `dashboard_renderer`：
  - 將狀態快照轉為 ANSI 覆蓋畫面
  - 輸出到 `stderr`
- `dashboard_loop`：
  - 每 500ms 刷新一次
  - 受 `runCtx` 控制生命週期

### 啟用條件

僅在下列條件同時成立時啟動：

- command = `scan`
- `stderr` is TTY
- `cfg.Format != "json"`

其他情況全部退回既有輸出行為。

## 資料流設計

1. `dispatchTasks`（透過 `dispatchObserver`）上報：
   - `OnBucketWaitStart` => `waiting_bucket`
   - `OnGateWaitStart` => `waiting_gate`
   - `OnTaskEnqueued` => `enqueued`
2. result 路徑（每筆 probe 完成）上報 result event，更新：
   - global progress
   - `results/sec`
3. pressure polling 上報：
   - `pressure`、`lastUpdatedAt`
   - `failStreak`、`api health`
4. dashboard loop 每 500ms 從 state snapshot render 至 `stderr`

## 狀態模型

`DashboardState`（內部 thread-safe）包含：

- `Progress`
  - `TotalTasks`
  - `ScannedTasks`
  - `Percent`
- `CurrentCIDR`
  - `CIDR`
  - `BucketStatus`（`waiting_bucket` / `waiting_gate` / `enqueued`）
- `Speed`
  - `DispatchPerSec`
  - `ResultsPerSec`
- `Controller`
  - `ManualPaused`
  - `APIPaused`
  - `StatusText`（`RUNNING` / `PAUSED(API)` / `PAUSED(MANUAL)` / `PAUSED(API+MANUAL)`）
- `API`
  - `PressurePercent`
  - `LastUpdatedAt`
  - `HealthText`（`ok` / `fail streak N`）

## 速率計算

- 使用 5 秒滑動視窗
- `dispatch/sec = enqueue_count_in_window / window_seconds`
- `results/sec = result_count_in_window / window_seconds`
- 視窗內無事件顯示 `0.0/s`

## 錯誤處理與退化策略

- renderer 寫 `stderr` 失敗：
  - 記錄 log
  - 停用 dashboard
  - 掃描流程繼續
- dashboard 任何 goroutine 受 `runCtx` 關閉，避免 goroutine 洩漏
- dashboard 不參與 dispatch/scanner/writer 的錯誤決策

## 測試策略

### Unit

- `dashboard_state_test.go`
  - 事件更新對應欄位正確
  - controller status mapping 正確
  - speed 視窗計算正確
- `dashboard_renderer_test.go`
  - 渲染內容含必要欄位
  - ANSI 覆蓋刷新格式穩定

### Integration / Scanapp behavior

- TTY + human：dashboard 啟用
- non-TTY：dashboard 不啟用
- `-format=json`：dashboard 不啟用
- pressure 成功/失敗 streak 反映到 API health
- 保持既有 `scan` 行為（exit code、resume、CSV 輸出）

## 非目標

- 本版不新增 `-ui` / `-ui-refresh` flag
- 本版不顯示所有 CIDR 清單
- 本版不揭露 bucket token 水位

## 驗收標準

- rich dashboard 在預設 TTY human 模式可即時顯示 6 類資訊
- 非 TTY 或 JSON 模式不受影響
- 既有整合測試與 scan 契約測試維持通過
- 新增測試可驗證 dashboard 啟停、狀態更新與渲染輸出
