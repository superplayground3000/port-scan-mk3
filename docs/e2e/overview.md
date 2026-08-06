# E2E Overview

This document explains how e2e operates in `port-scan-mk3`, and which behavior it verifies.

## How e2e works

The e2e entrypoint is:

```bash
bash e2e/run_e2e.sh
```

The flow is:

1. Prepare `e2e/out/` and `e2e/inputs/`.
2. Start the isolated Docker Compose services on `e2e-net`:
   - `scanner`
   - `mock-target-open`
   - `mock-target-closed`
   - `pressure-api-ok`
   - `pressure-api-5xx`
   - `pressure-api-timeout`
3. Run the normal scan scenario against `pressure-api-ok`.
4. Assert the timestamped batch outputs and the open-only filtering.
5. Make the report artifacts from the newest `scan_results-*.csv`.
6. Run the expected-failure scenarios (`api_5xx`, `api_timeout`, `api_conn_fail`).
7. Assert that each failure scenario exits non-zero and makes a `resume_state` artifact.
8. Run the integration tests as part of the e2e gate.

## Speed Control E2E

In addition to the docker-based scan e2e, there is a dedicated entry point for
speed-control verification:

```bash
bash e2e/speedcontrol/run_speedcontrol_e2e.sh
```

This flow runs the global/CIDR/combined scenario matrix. It writes:

- `e2e/out/speedcontrol/report.md`
- `e2e/out/speedcontrol/report.html`
- `e2e/out/speedcontrol/raw_metrics.json`

## What is tested

### Scenario matrix

| Scenario | Input/Pressure Mode | Expected outcome |
|----------|---------------------|------------------|
| `normal` | normal CIDR input + `pressure-api-ok` | The scan succeeds, the run makes a report, and both open and non-open rows are present |
| `api_5xx` | fail CIDR input + `pressure-api-5xx` | The scan fails after the pressure API failure escalation, and the run saves `resume_state` |
| `api_timeout` | fail CIDR input + `pressure-api-timeout` | The scan fails after repeated timeout polling failures, and the run saves `resume_state` |
| `api_conn_fail` | fail CIDR input + unreachable API endpoint | The scan fails on connection errors after the retry budget, and the run saves `resume_state` |

### Contract-level checks included in e2e

- Timestamped output naming:
  - `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`
  - `opened_results-YYYYMMDDTHHMMSSZ[-n].csv`
- `opened_results-*` contains only `open` rows.
- The run makes a report from the latest scan result CSV.
- A failure scenario must never be reported as passed.

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

- The script exits with code `0`.
- `report.html` and `report.txt` exist.
- At least one `open` result and one non-open result are present in the report summary.
- Each expected failure scenario exits non-zero and emits a corresponding `resume_state` artifact.

## Quick troubleshooting

- Docker unavailable: make sure that the Docker daemon runs and that `docker compose` works.
- Missing `resume_state` artifacts: inspect scenario logs under `e2e/out/scenario_*.log`.
- Missing report files: make sure that the scanner completed the normal scenario and that the report generator ran correctly.

---
**Revised**: 2026-04-13 | **Author**: docs-team
