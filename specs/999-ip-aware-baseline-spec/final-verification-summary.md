# Final Verification Summary (001-ip-aware-baseline-spec)

## Gate Results

- `go test ./...` PASS (`verification/full-test.log`)
- `bash scripts/coverage_gate.sh` PASS, total coverage `85.4%` (`verification/coverage.log`)
- `bash e2e/run_e2e.sh` PASS (`verification/e2e.log`)
- US3 focused test command PASS (`verification/us3-test.log`)

## Batch Output Samples

- `e2e/out/scan_results-20260302T033401Z.csv`
- `e2e/out/opened_results-20260302T033401Z.csv`

Verified by: `verification/e2e-artifacts.log`

## Resume State Artifacts

- `e2e/out/resume_state_api_5xx.json`
- `e2e/out/resume_state_api_timeout.json`
- `e2e/out/resume_state_api_conn_fail.json`

Verified by: `verification/e2e-artifacts.log`

## Observability Event Samples

Validated event schema fields include `target`, `port`, `state_transition`, `error_cause`.

Sample structured events:

```json
{"fields":{"cidr":"127.0.0.1/32","error_cause":"none","port":57407,"response_time_ms":0,"state_transition":"scanned","status":"open","target":"127.0.0.1"},"level":"info","msg":"scan_result","ts":"2026-03-02T03:35:01Z"}
{"fields":{"cidr":"127.0.0.1/32","completion_rate":0.5,"error_cause":"none","paused":false,"port":0,"scanned_count":1,"state_transition":"progress","target":"","total_count":2},"level":"info","msg":"scan_progress","ts":"2026-03-02T03:35:01Z"}
{"fields":{"close_count":1,"duration_ms":0,"error_cause":"none","open_count":1,"port":0,"state_transition":"completion_summary","success":true,"target":"","timeout_count":0,"total_tasks":2},"level":"info","msg":"scan_completion","ts":"2026-03-02T03:35:01Z"}
```

(Events captured from a local sample run using `scanapp.Run` with `ProgressInterval=1`.)

> Note added 2026-08-18: this record is kept as written. The capture used
> `RunOptions.ProgressInterval`, which no longer exists — issue #158 deleted it
> and made the resolved configuration the only source of the cadence. To
> reproduce this capture today, set `ProgressInterval` on the scan configuration
> instead. See `.agent-evidence/issue-158.md`.
