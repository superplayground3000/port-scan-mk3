# Speed Control Testing Framework Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

**Date:** 2026-03-18
**Approach:** Hybrid Verification (Deterministic Integration + Scenario E2E + Explainable Report)
**Constitution:** v1.2.0 compliance required

## Overview

本設計目標是建立一套可重現、可歸因、可閱讀的測試框架，驗證兩層速度控制是否符合契約：
1. **Global speed control**（`apiPaused || manualPaused` OR-gate）
2. **CIDR speed control**（每個 CIDR chunk 擁有獨立 leaky bucket）

框架輸出必須包含：
- 機器可解析原始度量（JSON）
- 人類可讀報告（Markdown + HTML）
- 每個情境的「預期 vs 實測 vs 判定 + 說明」

## Problem Statement

目前專案已有：
- Global gate 局部行為測試（`pkg/speedctrl/controller_test.go`）
- Leaky bucket 局部阻塞測試（`pkg/ratelimit/leaky_bucket_test.go`）
- 少量 dispatch 行為測試（`pkg/scanapp/scan_test.go`）

缺口是：
- 沒有一個統一框架同時覆蓋 **Global + CIDR** 的聯合行為
- 沒有穩定的速度指標計算與容忍區間
- e2e 報告僅有簡易總數，缺少可解釋診斷

## Design Goals

1. 驗證 FR-009（雙來源 pause OR-gate）與 bucket 實際限速效果。
2. 將「是否正確」拆成可解釋判定，不只 PASS/FAIL。
3. 報告能清楚指出瓶頸來源（gate、bucket、delay、workers）。
4. 保持 SOLID：CLI 僅組裝，不把核心判定邏輯塞進 `cmd/port-scan`。

## Non-Goals

1. 不追求毫秒級絕對效能 benchmark。
2. 不變更掃描協定（TCP dial 邏輯）本身。
3. 不改變既有輸出 CSV 契約欄位。

## Architecture

### 1. Speed Control Scenario Harness

新增「測試情境驅動器」執行固定矩陣，每個情境輸出一致的 metrics 物件：
- 情境名稱、配置、預期規則
- 事件時間線（gate wait、bucket acquire、task enqueue）
- 聚合後指標（TPS、pause latency、steady-state 誤差）

### 2. Telemetry Model

新增內部事件模型（測試使用）：
- `gate_wait_started_ns`
- `gate_released_ns`
- `bucket_wait_started_ns`
- `bucket_acquired_ns`
- `task_enqueued_ns`

每筆事件保留：
- `cidr`
- `task_index`
- `scenario`
- `timestamp_ns`

### 3. Analyzer

Analyzer 讀取 telemetry，輸出每情境判定：
- `verdict`: pass/fail
- `expected`: 規則摘要
- `observed`: 關鍵數值
- `explanation`: 為什麼過/不過
- `attribution`: 瓶頸歸因（bucket / gate / delay / workers）

穩態速率使用下列基準：

```text
steady_state_tps ~= min(
  bucket_rate,
  1 / delay_seconds,
  workers / avg_task_seconds
)
```

並採容忍誤差帶（預設 ±15%）。

### 4. Report Generator

延伸 `e2e/report`，新增 speed-control 專用報告輸出：
- `e2e/out/speedcontrol/report.md`
- `e2e/out/speedcontrol/report.html`
- `e2e/out/speedcontrol/raw_metrics.json`

報告結構：
1. Executive Summary
2. Scenario Matrix
3. Per-Scenario Deep Dive（Expected/Observed/Verdict/Explanation）
4. Bottleneck Attribution
5. Regression Notes（與基準比較）

## Scenario Matrix

### Global Control Scenarios

1. **G1 Manual Pause Gate**
- 目標：手動 pause 時停止新 dispatch；resume 後繼續。

2. **G2 API Pause Gate**
- 目標：壓力值跨越 threshold 時觸發 pause/resume。

3. **G3 OR-Gate Correctness**
- 目標：`manual=true` 或 `api=true` 任一成立即阻塞；僅在兩者都 false 時放行。

4. **G4 In-Flight Continuation**
- 目標：pause 不中止 in-flight task，只阻擋新任務派發。

### CIDR Control Scenarios

5. **C1 Single CIDR Steady Rate**
- 目標：穩態 dispatch 速率接近 `bucket_rate`。

6. **C2 Single CIDR Burst Then Steady**
- 目標：初始 burst 約等於 `bucket_capacity`，其後回穩。

7. **C3 Multi-CIDR Independent Buckets**
- 目標：不同 CIDR 各自遵守各自節奏，不互相吞 token。

### Combined Scenario

8. **X1 Global Pause During CIDR-Limited Dispatch**
- 目標：驗證兩層控制同時存在時仍符合各自契約。

## Data Contracts

### Raw Metrics Contract

`raw_metrics.json` 至少包含：
- `scenario`
- `config`
- `events[]`
- `metrics`（tps、wait_ms、pause_windows）
- `verdict`
- `explanation`

### Human Report Contract

`report.md` / `report.html` 每情境至少包含：
- 測試目的
- 參數
- 預期規則
- 實測數據
- 判定結果
- 文字解釋與疑似原因

## File Layout (Proposed)

- `tests/integration/speedcontrol_global_test.go`
- `tests/integration/speedcontrol_cidr_test.go`
- `tests/integration/speedcontrol_combined_test.go`
- `internal/testkit/speedcontrol/collector.go`
- `internal/testkit/speedcontrol/analyzer.go`
- `internal/testkit/speedcontrol/types.go`
- `e2e/report/speedcontrol_generate.go`
- `e2e/report/speedcontrol_template.html`
- `e2e/speedcontrol/run_speedcontrol_e2e.sh`
- `docs/e2e/speedcontrol.md`

## Verification Strategy

1. Unit：Analyzer 規則、容忍誤差計算、報告組裝。
2. Integration：deterministic harness 驗證 gate/bucket 邏輯。
3. E2E：真實 scanner 流程 + 報告輸出與可讀性驗證。

## Risks and Mitigations

1. **時間抖動造成不穩定**
- 緩解：使用穩態窗口、warmup 排除、容忍區間。

2. **環境速度差異造成誤判**
- 緩解：以比例與趨勢判定，不用單點絕對值。

3. **測試觀測點侵入生產程式**
- 緩解：以依賴注入與 test-only hook，預設 no-op。

## Rollout Plan

1. 先落地 integration harness + analyzer。
2. 再接 e2e 場景與 report generator。
3. 最後加文件與 CI gate（至少執行核心矩陣）。

