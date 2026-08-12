#!/usr/bin/env bash
set -euo pipefail

# Isolated Docker e2e for the three-step pipeline (constitution IV/V):
#   1. pre-ping          — ping unique targets, write unreachable_results-<ts>.csv
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
  "$OUT_DIR"/scenario_*.log \
  "$OUT_DIR"/scenario_*.exit
rm -rf "$OUT_DIR/interrupt"

cat > "$INPUT_DIR/cidr_normal.csv" <<'EOF'
asset_id,fab_name,source_ip,source_cidr,cidr_name,owner
asset-1,fab-open,172.28.0.10,172.28.0.0/24,mock-target-open,team-a
asset-2,fab-closed,172.28.0.11,172.28.0.0/24,mock-target-closed,team-b
EOF

# Failure-scenario workload. A /24 (254 targets) is deliberate: scanned by a
# single worker with a 200ms dial timeout, its floor is ~50s of wall clock even
# with rate limiting removed, while pressure control needs ~0.6-6s to declare
# the API fatal. No runner is fast enough to finish it first, so the scenarios
# can never win the race that made api_timeout flaky (issue #71).
cat > "$INPUT_DIR/cidr_fail.csv" <<'EOF'
asset_id,fab_name,source_ip,source_cidr,cidr_name,owner
asset-3,fab-fail,172.28.0.0/24,172.28.0.0/24,mock-target-fail,team-c
EOF

cat > "$INPUT_DIR/ports.csv" <<'EOF'
8080/tcp
EOF

cat > "$INPUT_DIR/ports_oversize.csv" <<'EOF'
8080/tcp
8081/tcp
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
  pressure-api-timeout \
  pressure-api-oversize
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
run_pre_ping()         { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner pre-ping "$@"; }
run_generate_buckets() { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner generate-buckets "$@"; }
run_scan()             { docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan "$@"; }

# A port limit failure must happen before snapshot creation.
set +e
RESOURCE_LIMIT_LOG="$(run_generate_buckets \
  -cidr-file /inputs/cidr_normal.csv \
  -port-file /inputs/ports_oversize.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -port-input-record-limit 1 \
  -buckets-out /out/buckets_resource_reject.json \
  -log-level error 2>&1)"
RESOURCE_LIMIT_CODE=$?
set -e
if [[ "$RESOURCE_LIMIT_CODE" -eq 0 ]]; then
  echo "e2e assertion failed: oversized port input exited 0" >&2
  exit 1
fi
if [[ -f "$OUT_DIR/buckets_resource_reject.json" ]]; then
  echo "e2e assertion failed: oversized port input created a snapshot" >&2
  exit 1
fi
if [[ "$RESOURCE_LIMIT_LOG" != *"-port-input-record-limit"* ]]; then
  echo "e2e assertion failed: port limit error did not identify its override flag" >&2
  echo "$RESOURCE_LIMIT_LOG" >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Step 1 — pre-ping: ping the two mock targets, write unreachable_results-<ts>.csv.
# Both targets are live containers, so both answer ICMP and the blocklist is
# empty; the step still exercises the real ping path and the durable CSV writer.
# ---------------------------------------------------------------------------
run_pre_ping \
  -cidr-file /inputs/cidr_normal.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -output /out/scan_results.csv \
  -pre-scan-ping-timeout 1s \
  -workers 2 \
  -log-level error

UNREACHABLE_HOST="$(ls "$OUT_DIR"/unreachable_results-*.csv 2>/dev/null | sort | tail -n1 || true)"
if [[ -z "${UNREACHABLE_HOST}" ]]; then
  echo "e2e assertion failed: pre-ping did not write unreachable_results-*.csv" >&2
  exit 1
fi
UNREACHABLE_CONTAINER="/out/$(basename "$UNREACHABLE_HOST")"

# ---------------------------------------------------------------------------
# Step 2 — generate-buckets: build the resume snapshot over targets minus the
# pre-ping blocklist. No network I/O; the snapshot is the durable hand-off to scan.
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
# Pressure-control failure handling (issue #71).
#
# These scenarios must never depend on a race between the scan and the pressure
# poller. Determinism rests on two independent mechanisms:
#
#   1. Structural — the fail workload is a /24 whose scan floor (~50s) is an
#      order of magnitude past the window pressure control needs to turn fatal.
#      The scan physically cannot finish first, whatever the runner's speed.
#   2. Event-driven — for scenarios backed by a mock we WAIT for that mock to
#      report it has actually SERVED enough failing pressure responses to cross
#      the scanner's threshold (GET /admin/stats), rather than inferring it from
#      elapsed time. Sleeps are used only to pace polling, never as the
#      correctness mechanism.
#
# Every assertion then names the path it proves: a non-zero exit that is NOT the
# hard-limit kill, the fatal pressure message in the log, and a resume snapshot
# that the aborted run actually advanced and still has work left in.
# ---------------------------------------------------------------------------

# pkg/scanapp/pressure_monitor.go aborts the run on the 3rd consecutive failure.
PRESSURE_FATAL_FAILURES=3
# Budget for waiting on mock-served events. Generous on purpose: it bounds a
# hang so the suite fails with a diagnosis instead of blocking forever, and it
# is never reached on a healthy run.
PRESSURE_EVENT_TIMEOUT="${PRESSURE_EVENT_TIMEOUT:-90}"
# Last-resort ceiling on a failure scenario's scan. Reaching it means pressure
# control never aborted the run, which is a FAILURE, not a pass — the exit-code
# assertion below rejects the 124 that `timeout` returns.
SCAN_HARD_LIMIT="${SCAN_HARD_LIMIT:-180}"

# Read the mock's cumulative served-failure counter. The mocks publish no host
# port (they are reachable only on the isolated bridge), so ask from inside.
# Prints nothing when the value cannot be read, which callers treat as "not yet".
pressure_failures_served() {
  local service="$1"
  local raw
  raw="$(docker compose -f "$COMPOSE_FILE" exec -T "$service" \
    wget -q -O - "http://127.0.0.1:8080/admin/stats" 2>/dev/null |
    tr -d '\r' |
    sed -n 's/.*"pressure_failures":[[:space:]]*\([0-9][0-9]*\).*/\1/p')" || true
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    echo "$raw"
  fi
}

# Wait until the counter is readable and echo it. Used to take a baseline before
# the scan starts, because the counters are cumulative per mock process and the
# mocks are shared across scenarios.
wait_for_pressure_stats() {
  local scenario="$1" service="$2"
  local deadline=$((SECONDS + PRESSURE_EVENT_TIMEOUT))
  local served=""

  while true; do
    served="$(pressure_failures_served "$service")"
    if [[ -n "$served" ]]; then
      echo "$served"
      return 0
    fi
    if ((SECONDS >= deadline)); then
      echo "e2e assertion failed: scenario ${scenario} could not read /admin/stats from ${service} within ${PRESSURE_EVENT_TIMEOUT}s" >&2
      return 1
    fi
    sleep 0.5
  done
}

# Block until the mock has served `want` failing responses, or fail loudly.
# Watching the scan's PID matters: if it exits early the counter will never
# reach the target, and waiting the full timeout would hide the real reason.
wait_for_pressure_failures() {
  local scenario="$1" service="$2" want="$3" scan_pid="$4"
  local deadline=$((SECONDS + PRESSURE_EVENT_TIMEOUT))
  local served=""

  while true; do
    served="$(pressure_failures_served "$service")"
    if [[ -n "$served" ]] && ((served >= want)); then
      return 0
    fi
    if ! kill -0 "$scan_pid" 2>/dev/null; then
      # Re-read once: the scan exiting and the counter reaching the target are
      # the same event, and we may simply have sampled between them.
      served="$(pressure_failures_served "$service")"
      if [[ -n "$served" ]] && ((served >= want)); then
        return 0
      fi
      echo "e2e assertion failed: scenario ${scenario} scan exited before ${service} served ${want} failing pressure responses (served=${served:-unreadable})" >&2
      return 1
    fi
    if ((SECONDS >= deadline)); then
      echo "e2e assertion failed: scenario ${scenario} waited ${PRESSURE_EVENT_TIMEOUT}s for ${service} to serve ${want} failing pressure responses (served=${served:-unreadable})" >&2
      return 1
    fi
    sleep 0.5
  done
}

# run_expected_failure <scenario> <pressure-api-url> <pressure-interval> <watch-service>
# watch-service is the compose service backing the URL, or "" when there is no
# mock to watch (the connection-refused scenario dials a dead local port).
run_expected_failure() {
  local scenario="$1"
  local pressure_api="$2"
  local pressure_interval="$3"
  local watch_service="${4:-}"

  local bucket="/out/resume_state_${scenario}.json"
  local host_bucket="$OUT_DIR/resume_state_${scenario}.json"
  local log="$OUT_DIR/scenario_${scenario}.log"
  local exit_file="$OUT_DIR/scenario_${scenario}.exit"
  rm -f "$host_bucket" "$exit_file"

  run_generate_buckets \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
    -cidr-ip-col source_ip \
    -cidr-ip-cidr-col source_cidr \
    -buckets-out "$bucket" \
    -log-level error

  if [[ ! -f "$host_bucket" ]]; then
    echo "e2e assertion failed: scenario ${scenario} could not build its bucket snapshot" >&2
    exit 1
  fi

  # Baseline BEFORE the scan starts: the mocks are shared between scenarios and
  # their counters never reset, so only the delta belongs to this scan.
  local baseline=0
  if [[ -n "$watch_service" ]]; then
    if ! baseline="$(wait_for_pressure_stats "$scenario" "$watch_service")"; then
      exit 1
    fi
  fi

  # Run the scan in the background so the harness can watch mock-served events
  # while it is still running.
  (
    set +e
    timeout "$SCAN_HARD_LIMIT" docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan \
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
      >"$log" 2>&1
    echo $? >"$exit_file"
  ) &
  local scan_pid=$!

  if [[ -n "$watch_service" ]]; then
    if ! wait_for_pressure_failures "$scenario" "$watch_service" \
      "$((baseline + PRESSURE_FATAL_FAILURES))" "$scan_pid"; then
      wait "$scan_pid" 2>/dev/null || true
      cat "$log" >&2 || true
      exit 1
    fi
  fi

  wait "$scan_pid" 2>/dev/null || true
  local code
  code="$(cat "$exit_file" 2>/dev/null || echo "missing")"

  if [[ "$code" == "missing" ]]; then
    echo "e2e assertion failed: scenario ${scenario} recorded no exit code" >&2
    exit 1
  fi
  if [[ "$code" -eq 0 ]]; then
    echo "e2e assertion failed: scenario ${scenario} should fail but exited 0" >&2
    cat "$log" >&2
    exit 1
  fi
  if [[ "$code" -eq 124 ]]; then
    echo "e2e assertion failed: scenario ${scenario} hit the ${SCAN_HARD_LIMIT}s hard limit instead of aborting on pressure failure" >&2
    cat "$log" >&2
    exit 1
  fi
  # A non-zero exit is not enough — it must be THIS failure. Without this the
  # scenario would pass on any unrelated error (bad flag, missing input).
  if ! grep -q "pressure api failed ${PRESSURE_FATAL_FAILURES} times" "$log"; then
    echo "e2e assertion failed: scenario ${scenario} exited ${code} but not via the fatal pressure path" >&2
    cat "$log" >&2
    exit 1
  fi
  # -require-progress: the snapshot path IS the scan's -resume input, so mere
  # existence proves nothing; requiring an advanced cursor proves the aborted
  # run persisted its progress. -require-remaining proves it was aborted with
  # work still pending rather than having quietly finished.
  if ! go run ./e2e/tools/assert-resume-snapshot \
    -file "$host_bucket" -require-progress -require-remaining; then
    echo "e2e assertion failed: scenario ${scenario} left no resumable snapshot after abort" >&2
    exit 1
  fi
}

run_expected_failure "api_5xx" "http://pressure-api-5xx:8080/api/pressure" "200ms" "pressure-api-5xx"
run_expected_failure "api_timeout" "http://pressure-api-timeout:8080/api/pressure" "200ms" "pressure-api-timeout"
run_expected_failure "api_oversize" "http://pressure-api-oversize:8080/api/pressure" "200ms" "pressure-api-oversize"
run_expected_failure "api_conn_fail" "http://127.0.0.1:9/api/pressure" "200ms" ""

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
