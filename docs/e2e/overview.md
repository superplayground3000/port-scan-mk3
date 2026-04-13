# E2E Overview

This document explains how e2e works in `port-scan-mk3` and what behavior is verified.

## How e2e works

The e2e entrypoint is:

```bash
bash e2e/run_e2e.sh
```

Execution flow:

1. Prepare `e2e/out/` and `e2e/inputs/`.
2. Start Docker Compose isolated services on `e2e-net`:
   - `scanner`
   - `mock-target-open`
   - `mock-target-closed`
   - `pressure-api-ok`
   - `pressure-api-5xx`
   - `pressure-api-timeout`
3. Run normal scan scenario against `pressure-api-ok`.
4. Assert timestamped batch outputs and open-only filtering.
5. Generate report artifacts from latest `scan_results-*.csv`.
6. Run expected-failure scenarios (`api_5xx`, `api_timeout`, `api_conn_fail`).
7. Assert each failure scenario exits non-zero and produces `resume_state` artifact.
8. Run integration tests as part of e2e gate.

## Speed Control E2E

除了 docker-based 掃描 e2e，另有 speed-control 專用驗證入口：

```bash
bash e2e/speedcontrol/run_speedcontrol_e2e.sh
```

此流程會執行 global/CIDR/combined 場景矩陣，並輸出：

- `e2e/out/speedcontrol/report.md`
- `e2e/out/speedcontrol/report.html`
- `e2e/out/speedcontrol/raw_metrics.json`

## What is tested

### Scenario matrix

| Scenario | Input/Pressure Mode | Expected outcome |
|----------|---------------------|------------------|
| `normal` | normal CIDR input + `pressure-api-ok` | Scan succeeds, report generated, open and non-open rows both present |
| `api_5xx` | fail CIDR input + `pressure-api-5xx` | Scan fails after pressure API failure escalation, `resume_state` saved |
| `api_timeout` | fail CIDR input + `pressure-api-timeout` | Scan fails after repeated timeout polling failures, `resume_state` saved |
| `api_conn_fail` | fail CIDR input + unreachable API endpoint | Scan fails on connection errors after retry budget, `resume_state` saved |

### Contract-level checks included in e2e

- Timestamped output naming:
  - `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`
  - `opened_results-YYYYMMDDTHHMMSSZ[-n].csv`
- `opened_results-*` contains only `open` rows.
- Report generation against latest scan result CSV.
- Failure scenarios must not silently pass.

## Artifacts and pass criteria

Artifacts written to `e2e/out/`:

- `scan_results-*.csv`
- `opened_results-*.csv`
- `report.html`
- `report.txt`
- `scenario_api_5xx.log`
- `scenario_api_timeout.log`
- `scenario_api_conn_fail.log`
- `resume_state_api_5xx.json`
- `resume_state_api_timeout.json`
- `resume_state_api_conn_fail.json`

Pass criteria:

- Script exits with code `0`.
- `report.html` and `report.txt` exist.
- At least one `open` and one non-open result are present in report summary.
- Each expected failure scenario exits non-zero and emits a corresponding `resume_state` artifact.

## Quick troubleshooting

- Docker unavailable: ensure Docker daemon is running and `docker compose` works.
- Missing `resume_state` artifacts: inspect scenario logs under `e2e/out/scenario_*.log`.
- Missing report files: confirm scanner completed normal scenario and report generator ran successfully.

---
**Revised**: 2026-04-13 | **Author**: docs-team
