#!/usr/bin/env bash
set -euo pipefail

# Isolated Docker e2e for the three-step pipeline (constitution IV/V):
#   1. preping           — ping unique targets, write unreachable_results-<ts>.csv
#   2. generate-buckets  — subtract the unreachable blocklist, write a resume snapshot
#   3. scan -resume      — scan the snapshot's buckets (never pings; no checker)
# All steps run inside the scanner container against mock-only services on an
# isolated bridge network. No real external host is ever contacted.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/e2e/out"
INPUT_DIR="$ROOT/e2e/inputs"
COMPOSE_FILE="$ROOT/e2e/docker-compose.yml"
mkdir -p "$OUT_DIR"
mkdir -p "$INPUT_DIR"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required for e2e test" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose is required for e2e test" >&2
  exit 1
fi

rm -f "$OUT_DIR"/scan_results-*.csv \
  "$OUT_DIR"/opened_results-*.csv \
  "$OUT_DIR"/unreachable_results-*.csv \
  "$OUT_DIR"/buckets_*.json \
  "$OUT_DIR/report.html" \
  "$OUT_DIR/report.txt" \
  "$OUT_DIR"/resume_state*.json \
  "$OUT_DIR"/scan_results_*.csv \
  "$OUT_DIR"/scenario_*.log
rm -rf "$OUT_DIR/interrupt"

cat > "$INPUT_DIR/cidr_normal.csv" <<'EOF'
asset_id,fab_name,source_ip,source_cidr,cidr_name,owner
asset-1,fab-open,172.28.0.10,172.28.0.0/24,mock-target-open,team-a
asset-2,fab-closed,172.28.0.11,172.28.0.0/24,mock-target-closed,team-b
EOF

cat > "$INPUT_DIR/cidr_fail.csv" <<'EOF'
asset_id,fab_name,source_ip,source_cidr,cidr_name,owner
asset-3,fab-fail,172.28.0.0/28,172.28.0.0/24,mock-target-fail,team-c
EOF

cat > "$INPUT_DIR/ports.csv" <<'EOF'
8080/tcp
EOF

# Ten distinct targets for the interrupt-and-resume scenario. Most have no
# listener (they time out), which is irrelevant — a row is written for every
# probe regardless of state. Ten paced probes give a wide enough window to
# interrupt the scan with work still pending.
cat > "$INPUT_DIR/cidr_interrupt.csv" <<'EOF'
asset_id,fab_name,source_ip,source_cidr,cidr_name,owner
i-0,fab-int,172.28.0.10,172.28.0.0/24,mock-target-open,team-a
i-1,fab-int,172.28.0.11,172.28.0.0/24,mock-target-closed,team-a
i-2,fab-int,172.28.0.20,172.28.0.0/24,mock-target-none,team-a
i-3,fab-int,172.28.0.21,172.28.0.0/24,mock-target-none,team-a
i-4,fab-int,172.28.0.22,172.28.0.0/24,mock-target-none,team-a
i-5,fab-int,172.28.0.23,172.28.0.0/24,mock-target-none,team-a
i-6,fab-int,172.28.0.24,172.28.0.0/24,mock-target-none,team-a
i-7,fab-int,172.28.0.25,172.28.0.0/24,mock-target-none,team-a
i-8,fab-int,172.28.0.26,172.28.0.0/24,mock-target-none,team-a
i-9,fab-int,172.28.0.27,172.28.0.0/24,mock-target-none,team-a
EOF

docker compose -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
docker compose -f "$COMPOSE_FILE" up -d --build \
  mock-target-open \
  mock-target-closed \
  pressure-api-ok \
  pressure-api-5xx \
  pressure-api-timeout
docker compose -f "$COMPOSE_FILE" build scanner
trap 'docker compose -f "$COMPOSE_FILE" down -v --remove-orphans' EXIT

OPEN_READY=0
for _ in {1..30}; do
  if docker compose -f "$COMPOSE_FILE" exec -T mock-target-open sh -lc "netstat -lnt | grep -q ':8080'" >/dev/null 2>&1; then
    OPEN_READY=1
    break
  fi
  sleep 1
done
if [[ "$OPEN_READY" -ne 1 ]]; then
  echo "mock-target-open did not become ready on port 8080" >&2
  exit 1
fi

# Each pipeline step is a fresh scanner container invocation. -w /out makes
# relative output paths resolve into the bind-mounted results directory.
run_preping()          { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner preping "$@"; }
run_generate_buckets() { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner generate-buckets "$@"; }
run_scan()             { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan "$@"; }

# ---------------------------------------------------------------------------
# Step 1 — preping: ping the two mock targets, write unreachable_results-<ts>.csv.
# Both targets are live containers, so both answer ICMP and the blocklist is
# empty; the step still exercises the real ping path and the durable CSV writer.
# ---------------------------------------------------------------------------
run_preping \
  -cidr-file /inputs/cidr_normal.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -output /out/scan_results.csv \
  -pre-scan-ping-timeout 1s \
  -workers 2 \
  -log-level error

UNREACHABLE_HOST="$(ls "$OUT_DIR"/unreachable_results-*.csv 2>/dev/null | sort | tail -n1 || true)"
if [[ -z "${UNREACHABLE_HOST}" ]]; then
  echo "e2e assertion failed: preping did not write unreachable_results-*.csv" >&2
  exit 1
fi
UNREACHABLE_CONTAINER="/out/$(basename "$UNREACHABLE_HOST")"

# ---------------------------------------------------------------------------
# Step 2 — generate-buckets: build the resume snapshot over targets minus the
# preping blocklist. No network I/O; the snapshot is the durable hand-off to scan.
# ---------------------------------------------------------------------------
run_generate_buckets \
  -cidr-file /inputs/cidr_normal.csv \
  -port-file /inputs/ports.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -unreachable-file "$UNREACHABLE_CONTAINER" \
  -buckets-out /out/buckets_normal.json \
  -log-level error

if [[ ! -f "$OUT_DIR/buckets_normal.json" ]]; then
  echo "e2e assertion failed: generate-buckets did not write buckets_normal.json" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Step 3 — scan: consume the snapshot via -resume. Scan never pings and builds
# no reachability checker; targets come straight from the buckets.
# ---------------------------------------------------------------------------
run_scan \
  -cidr-file /inputs/cidr_normal.csv \
  -port-file /inputs/ports.csv \
  -resume /out/buckets_normal.json \
  -output /out/scan_results.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -pressure-api http://pressure-api-ok:8080/api/pressure \
  -pressure-interval 200ms \
  -workers 2 \
  -delay 0ms \
  -timeout 200ms \
  -log-level error

SCAN_RESULTS_FILE="$(ls "$OUT_DIR"/scan_results-*.csv 2>/dev/null | sort | tail -n1 || true)"
OPENED_RESULTS_FILE="$(ls "$OUT_DIR"/opened_results-*.csv 2>/dev/null | sort | tail -n1 || true)"

if [[ -z "${OPENED_RESULTS_FILE}" ]]; then
  echo "e2e assertion failed: opened_results-*.csv not found" >&2
  exit 1
fi
if awk -F, 'NR>1 && $4 != "open" {exit 1}' "$OPENED_RESULTS_FILE"; then
  :
else
  echo "e2e assertion failed: opened_results-*.csv contains non-open row" >&2
  exit 1
fi

if [[ -z "${SCAN_RESULTS_FILE}" ]]; then
  echo "e2e assertion failed: scan_results-*.csv not found" >&2
  exit 1
fi

go run ./e2e/report/cmd/generate -out "$OUT_DIR" -csv "$SCAN_RESULTS_FILE"

OPEN_COUNT=$(awk -F= '/^Open=/{print $2}' "$OUT_DIR/report.txt")
CLOSED_COUNT=$(awk -F= '/^Closed=/{print $2}' "$OUT_DIR/report.txt")
TIMEOUT_COUNT=$(awk -F= '/^Timeout=/{print $2}' "$OUT_DIR/report.txt")

if [[ "${OPEN_COUNT:-0}" -lt 1 ]]; then
  echo "e2e assertion failed: expected at least 1 open result" >&2
  exit 1
fi
if [[ $(( ${CLOSED_COUNT:-0} + ${TIMEOUT_COUNT:-0} )) -lt 1 ]]; then
  echo "e2e assertion failed: expected at least 1 non-open result" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Pressure-control failure handling. generate-buckets with NO -unreachable-file
# turns every selector IP into a TCP task (scan never pings), paced at
# -bucket-rate 1 (~1/s), so the scan cannot finish before the pressure poller
# aborts it. scan persists progress back into its -resume snapshot
# (resumePath == cfg.Resume), so a non-zero exit with the snapshot still present
# proves the scan is resumable after the abort.
#
# run_expected_failure covers the two FAST failure modes — api_5xx and
# api_conn_fail — which the scanner sees as an error on every poll, so it aborts
# after three consecutive failures in well under a second, long before the ~15s
# scan could finish. api_timeout is different (its fatal trigger is slow) and has
# its own event-driven scenario below; see run_api_timeout_failure.
# ---------------------------------------------------------------------------
run_expected_failure() {
  local scenario="$1"
  local pressure_api="$2"
  local pressure_interval="$3"

  local bucket="/out/resume_state_${scenario}.json"
  rm -f "$OUT_DIR/resume_state_${scenario}.json"

  run_generate_buckets \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -buckets-out "$bucket" \
    -log-level error

  if [[ ! -f "$OUT_DIR/resume_state_${scenario}.json" ]]; then
    echo "e2e assertion failed: scenario ${scenario} could not build its bucket snapshot" >&2
    exit 1
  fi

  set +e
  run_scan \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
    -resume "$bucket" \
    -output "/out/scan_results_${scenario}.csv" \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -pressure-api "$pressure_api" \
    -pressure-interval "$pressure_interval" \
    -workers 1 \
    -bucket-rate 1 \
    -bucket-capacity 1 \
    -delay 0ms \
    -timeout 200ms \
    -log-level error \
    >"$OUT_DIR/scenario_${scenario}.log" 2>&1
  local code=$?
  set -e

  if [[ "$code" -eq 0 ]]; then
    echo "e2e assertion failed: scenario ${scenario} should fail but exited 0" >&2
    exit 1
  fi
  if [[ ! -f "$OUT_DIR/resume_state_${scenario}.json" ]]; then
    echo "e2e assertion failed: scenario ${scenario} missing resumable snapshot after abort" >&2
    exit 1
  fi
}

# Number of consecutive pressure-poll failures after which the scanner aborts
# the whole scan (pkg/scanapp/pressure_monitor.go: consecutiveFailures > 2).
PRESSURE_FATAL_THRESHOLD=3

# pressure_timeout_failures echoes the cumulative pressure_failures counter the
# timeout mock has served (GET /admin/stats), queried from inside the mock
# container so nothing needs a published port. Empty output if the mock cannot
# be reached yet; callers default it to 0.
pressure_timeout_failures() {
  docker compose -f "$COMPOSE_FILE" exec -T pressure-api-timeout \
    wget -qO- http://localhost:8080/admin/stats 2>/dev/null |
    grep -o '"pressure_failures":[0-9]*' | grep -o '[0-9]*' || true
}

# ---------------------------------------------------------------------------
# api_timeout — the one pressure failure mode with a SLOW fatal trigger: the
# scanner's pressure HTTP client only gives up after a 2s per-poll timeout and
# aborts after three of them (~6s), whereas api_5xx/api_conn_fail abort in well
# under a second. A scan that happened to outrun that window would exit 0 and
# the assertion would be a coin flip (issue #71).
#
# Determinism here does NOT come from hoping the paced scan is slow enough: it
# comes from WAITING on an observable event. The mock counts every pressure
# failure it serves; we launch the scan in the background and block until the
# mock has served the scanner's fatal threshold before judging the outcome. Only
# then do we assert BOTH a non-zero scan exit AND a resume snapshot that is a
# genuine mid-flight abort — assert-resume-snapshot -require-remaining proves the
# scan was aborted with work still undispatched, not quietly finished.
# ---------------------------------------------------------------------------
run_api_timeout_failure() {
  local scenario="api_timeout"
  local bucket="/out/resume_state_${scenario}.json"
  local host_snapshot="$OUT_DIR/resume_state_${scenario}.json"
  rm -f "$host_snapshot"

  run_generate_buckets \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -buckets-out "$bucket" \
    -log-level error

  if [[ ! -f "$host_snapshot" ]]; then
    echo "e2e assertion failed: scenario ${scenario} could not build its bucket snapshot" >&2
    exit 1
  fi

  # Baseline the cumulative counter first, so the wait is correct even if the
  # mock was already polled (the counters live for the container's lifetime).
  local baseline
  baseline="$(pressure_timeout_failures)"
  baseline="${baseline:-0}"

  # Launch the scan in the background, paced at -bucket-rate 1 so it cannot
  # finish before the poller aborts it. The outer timeout guards against a hung
  # scanner (a different bug) instead of letting CI block forever.
  set +e
  timeout 90 docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
    -resume "$bucket" \
    -output "/out/scan_results_${scenario}.csv" \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -pressure-api http://pressure-api-timeout:8080/api/pressure \
    -pressure-interval 200ms \
    -workers 1 \
    -bucket-rate 1 \
    -bucket-capacity 1 \
    -delay 0ms \
    -timeout 200ms \
    -log-level error \
    >"$OUT_DIR/scenario_${scenario}.log" 2>&1 &
  local scan_pid=$!
  set -e

  # EVENT WAIT: block until the mock has served the fatal threshold of pressure
  # failures. This — not the scan's pacing — is what makes the scenario
  # deterministic. Bounded (~10x the ~6s the real path needs) so a mock that
  # never serves the failures fails loudly instead of hanging.
  local want=$(( baseline + PRESSURE_FATAL_THRESHOLD ))
  local served=0 waited=0
  local deadline=60
  while (( waited < deadline )); do
    served="$(pressure_timeout_failures)"
    served="${served:-0}"
    if (( served >= want )); then
      break
    fi
    # Stop early if the scan already exited without serving the failures.
    kill -0 "$scan_pid" 2>/dev/null || break
    sleep 1
    waited=$(( waited + 1 ))
  done

  if (( served < want )); then
    echo "e2e assertion failed: scenario ${scenario} — mock served only $(( served - baseline )) pressure failures, expected >= ${PRESSURE_FATAL_THRESHOLD}; the fatal pressure-timeout path was never exercised" >&2
    kill "$scan_pid" 2>/dev/null || true
    wait "$scan_pid" 2>/dev/null || true
    cat "$OUT_DIR/scenario_${scenario}.log" >&2 || true
    exit 1
  fi

  # The fatal path has been exercised; the scan must now abort on its own.
  # Guard the wait: a non-zero exit is the EXPECTED outcome here, so it must not
  # trip set -e before we capture it.
  set +e
  wait "$scan_pid"
  local code=$?
  set -e

  if [[ "$code" -eq 0 ]]; then
    echo "e2e assertion failed: scenario ${scenario} served ${PRESSURE_FATAL_THRESHOLD}+ pressure failures but the scan still exited 0 instead of aborting" >&2
    cat "$OUT_DIR/scenario_${scenario}.log" >&2 || true
    exit 1
  fi

  if [[ ! -f "$host_snapshot" ]]; then
    echo "e2e assertion failed: scenario ${scenario} missing resumable snapshot after abort" >&2
    exit 1
  fi

  # A non-zero exit plus a file is not enough — the OLD assertion. Prove the
  # snapshot is a resumable mid-flight abort: it decodes through pkg/state and
  # still has undispatched work (so the scan was aborted, not finished).
  if ! go run ./e2e/tools/assert-resume-snapshot -file "$host_snapshot" -require-remaining; then
    echo "e2e assertion failed: scenario ${scenario} resume snapshot is not a resumable mid-flight abort" >&2
    exit 1
  fi
}

run_expected_failure "api_5xx" "http://pressure-api-5xx:8080/api/pressure" "200ms"
run_api_timeout_failure
run_expected_failure "api_conn_fail" "http://127.0.0.1:9/api/pressure" "200ms"

# ---------------------------------------------------------------------------
# Interrupt-and-resume (requirements 3 + 4). Scan a paced 10-target bucket,
# SIGINT it mid-flight, then resume. Because scan writes rows straight to the
# final file and records the output path in the snapshot, the resumed run must
# APPEND to the SAME file — one continuous scan_results with a single header and
# all ten rows, no lost or duplicated work. An isolated /out/interrupt directory
# keeps this scenario's timestamped files from colliding with the others.
# ---------------------------------------------------------------------------
interrupt_and_resume() {
  local bucket="/out/interrupt/buckets.json"
  local out_anchor="/out/interrupt/scan_results.csv"
  local host_dir="$OUT_DIR/interrupt"
  mkdir -p "$host_dir"

  run_generate_buckets \
    -cidr-file /inputs/cidr_interrupt.csv \
    -port-file /inputs/ports.csv \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -buckets-out "$bucket" \
    -log-level error

  if [[ ! -f "$host_dir/buckets.json" ]]; then
    echo "e2e assertion failed: interrupt scenario could not build its bucket" >&2
    exit 1
  fi

  # Interrupt: SIGINT after 5s while the scan is still dialing (10 targets at
  # ~1/s). timeout forwards SIGINT through `docker compose run`; scan exits 130.
  set +e
  timeout -s INT 5 docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan \
    -cidr-file /inputs/cidr_interrupt.csv \
    -port-file /inputs/ports.csv \
    -resume "$bucket" \
    -output "$out_anchor" \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -workers 1 \
    -bucket-rate 1 \
    -bucket-capacity 1 \
    -delay 0ms \
    -timeout 200ms \
    -disable-api \
    -log-level error \
    >"$OUT_DIR/scenario_interrupt.log" 2>&1
  local code=$?
  set -e

  # 130 == graceful SIGINT cancel; 124 == timeout had to hard-kill (still an
  # interruption, just less graceful). Either proves the scan did not finish.
  if [[ "$code" -eq 0 ]]; then
    echo "e2e assertion failed: interrupt scan should not complete cleanly (exit 0)" >&2
    cat "$OUT_DIR/scenario_interrupt.log" >&2
    exit 1
  fi

  local partial_file
  partial_file="$(ls "$host_dir"/scan_results-*.csv 2>/dev/null | sort | tail -n1 || true)"
  if [[ -z "$partial_file" ]]; then
    echo "e2e assertion failed: interrupt run wrote no scan_results file" >&2
    exit 1
  fi
  local n_files
  n_files="$(ls "$host_dir"/scan_results-*.csv 2>/dev/null | wc -l)"
  if [[ "$n_files" -ne 1 ]]; then
    echo "e2e assertion failed: expected exactly one scan_results file after interrupt, got $n_files" >&2
    exit 1
  fi

  # Resume to completion — must append to the SAME file (no new timestamp).
  run_scan \
    -cidr-file /inputs/cidr_interrupt.csv \
    -port-file /inputs/ports.csv \
    -resume "$bucket" \
    -output "$out_anchor" \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -workers 1 \
    -bucket-rate 100 \
    -bucket-capacity 100 \
    -delay 0ms \
    -timeout 200ms \
    -disable-api \
    -log-level error

  n_files="$(ls "$host_dir"/scan_results-*.csv 2>/dev/null | wc -l)"
  if [[ "$n_files" -ne 1 ]]; then
    echo "e2e assertion failed: resume must append to the same file, but found $n_files scan_results files" >&2
    ls -l "$host_dir" >&2
    exit 1
  fi
  local final_file
  final_file="$(ls "$host_dir"/scan_results-*.csv 2>/dev/null | sort | tail -n1)"
  if [[ "$final_file" != "$partial_file" ]]; then
    echo "e2e assertion failed: resume wrote a new file ($final_file) instead of appending to $partial_file" >&2
    exit 1
  fi

  # Exactly one header line.
  local headers
  headers="$(grep -c '^ip,ip_cidr,port,status' "$final_file" || true)"
  if [[ "$headers" -ne 1 ]]; then
    echo "e2e assertion failed: expected exactly one header line, got $headers" >&2
    exit 1
  fi

  # All ten targets present exactly once, no duplicate data rows.
  local data_rows unique_ips
  data_rows="$(awk 'NR>1' "$final_file" | wc -l)"
  unique_ips="$(awk -F, 'NR>1 {print $1}' "$final_file" | sort -u | wc -l)"
  if [[ "$data_rows" -ne 10 ]]; then
    echo "e2e assertion failed: expected 10 continuous data rows, got $data_rows" >&2
    cat "$final_file" >&2
    exit 1
  fi
  if [[ "$unique_ips" -ne 10 ]]; then
    echo "e2e assertion failed: expected 10 distinct target IPs (no dupes), got $unique_ips" >&2
    cat "$final_file" >&2
    exit 1
  fi
}

interrupt_and_resume

go test ./tests/integration -v

echo "e2e report generated at $OUT_DIR"
