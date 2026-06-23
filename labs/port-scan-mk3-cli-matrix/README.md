# port-scan-mk3-cli-matrix

## What this demonstrates

port-scan-mk3's five CLI binaries produce contract-conformant output across their full
documented flag surface — correct port-state classification (open / close / close(timeout) /
unreachable), correct pressure-driven pause/resume and fail-safe abort, and correct CSV/CIDR
transforms — when run against deterministic mock targets and pressure APIs.

## Run it

```bash
docker compose up -d --wait
docker compose exec -T scanner bash /lab/scripts/run-matrix.sh   # 36-case matrix
```

Or run the full validated cycle from the repo root:

```bash
bash ~/.claude/skills/research-lab/scripts/validate_lab.sh labs/port-scan-mk3-cli-matrix
```

## Expected output

```
PASS A1 validate basic human exit0
PASS B1 open in opened_results
PASS B3 filtered close(timeout)
PASS C2 .99 marked unreachable
PASS D2 scan paused on high pressure
PASS D3 pressure failed 3 times
...
RESULT: PASS=NN FAIL=0 TOTAL=NN
smoke test PASSED — all 36 CLI matrix cases observed expected output.
```

## What's covered

| Group | Binary / feature | Cases |
|---|---|---|
| A | `port-scan validate` (human/json/rich/invalid) | 4 |
| B | `port-scan scan` modes & I/O flags | 9 |
| C | pre-scan ping reachability gating | 2 |
| D | pressure control (simple/auth/multi-source/fail-safe) | 8 |
| E | resume (authentic SIGINT + mismatch guard) | 2 |
| F | `preprocess` | 2 |
| G | `enrich-targets` | 2 |
| H | `cidr-compare` (flag + env forms) | 3 |
| I | `csv-transform` (custom cols + env forms) | 4 |

## Notes

- `target-filtered` uses in-container `iptables DROP` (`cap_add: NET_ADMIN`) to produce real
  connect timeouts; the scanner uses `cap_add: NET_RAW` so pre-scan `ping` works. Both are
  container-namespace capabilities — no host changes.
- Pressure healthchecks hit a non-consuming `/healthz` so they never advance `PRESSURE_SEQUENCE`.
- See `.env.example` for every tunable env var and `RESEARCH.md` for design rationale.

## Teardown

```bash
docker compose down -v --remove-orphans
```
