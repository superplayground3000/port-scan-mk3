# Speed Control E2E

本文件說明如何執行 speed-control 驗證流程，並產生可讀報告。

## Entry Point

```bash
bash e2e/speedcontrol/run_speedcontrol_e2e.sh
```

## What It Verifies

1. Global pause gating
1. OR-gate behavior (`apiPaused || manualPaused`)
1. Single CIDR steady-rate behavior
1. Single CIDR burst behavior
1. Combined global pause + CIDR bucket behavior

## Artifacts

輸出路徑：`e2e/out/speedcontrol/`

- `report.md`: 人類可讀文字報告（Expected/Observed/Verdict/Explanation）
- `report.html`: 可分享 HTML 報告
- `raw_metrics.json`: 原始場景事件與 verdict

## Pass Criteria

1. Script exit code 為 `0`
1. 三個 artifacts 都存在
1. 報告可看到每個 scenario 的 verdict 與 explanation

---
**Revised**: 2026-04-13 | **Author**: docs-team

