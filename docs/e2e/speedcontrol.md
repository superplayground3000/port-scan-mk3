# Speed Control E2E

This document explains how to run the speed-control verification flow and how it makes a readable report.

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

Output path: `e2e/out/speedcontrol/`

- `report.md`: a text report for humans (Expected/Observed/Verdict/Explanation)
- `report.html`: an HTML report that you can share
- `raw_metrics.json`: the raw scenario events and the verdict

## Pass Criteria

1. The script exit code is `0`
1. All three artifacts exist
1. The report shows the verdict and the explanation of each scenario

---
**Revised**: 2026-04-13 | **Author**: docs-team

