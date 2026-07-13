#!/usr/bin/env bash
set -euo pipefail

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
  "$OUT_DIR/report.html" \
  "$OUT_DIR/report.txt" \
  "$OUT_DIR"/resume_state*.json \
  "$OUT_DIR"/scan_results_*.csv \
  "$OUT_DIR"/scenario_*.log

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

run_scan() {
  docker compose -f "$COMPOSE_FILE" run --rm -w /out scanner scan "$@"
}

run_scan \
  -cidr-file /inputs/cidr_normal.csv \
  -port-file /inputs/ports.csv \
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

run_expected_failure() {
  local scenario="$1"
  local pressure_api="$2"
  local pressure_interval="$3"

  rm -f "$OUT_DIR/resume_state.json"
  set +e
  # -disable-pre-scan-ping makes the run duration deterministic: every selector
  # IP becomes a TCP task paced at -bucket-rate 1 (~1/s), so the scan reliably
  # outlasts the pressure fail-fast window (3 failures x ~2s client timeout).
  # Otherwise the duration is dominated by flaky ARP timing of unreachable IPs,
  # and a small change in target count (e.g. broadcast exclusion) can tip the
  # scan below the fail-fast threshold and make it exit 0.
  run_scan \
    -cidr-file /inputs/cidr_fail.csv \
    -port-file /inputs/ports.csv \
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
    -disable-pre-scan-ping \
    -log-level error \
    >"$OUT_DIR/scenario_${scenario}.log" 2>&1
  local code=$?
  set -e

  if [[ "$code" -eq 0 ]]; then
    echo "e2e assertion failed: scenario ${scenario} should fail but exited 0" >&2
    exit 1
  fi
  if [[ ! -f "$OUT_DIR/resume_state.json" ]]; then
    echo "e2e assertion failed: scenario ${scenario} missing resume_state.json" >&2
    exit 1
  fi
  mv "$OUT_DIR/resume_state.json" "$OUT_DIR/resume_state_${scenario}.json"
}

run_expected_failure "api_5xx" "http://pressure-api-5xx:8080/api/pressure" "200ms"
run_expected_failure "api_timeout" "http://pressure-api-timeout:8080/api/pressure" "200ms"
run_expected_failure "api_conn_fail" "http://127.0.0.1:9/api/pressure" "200ms"

go test ./tests/integration -v

echo "e2e report generated at $OUT_DIR"
