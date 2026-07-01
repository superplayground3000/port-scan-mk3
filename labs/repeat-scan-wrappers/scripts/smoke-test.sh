#!/usr/bin/env bash
# Exercises the property end-to-end (black-box, via `docker compose exec` / `run`):
#
#   The cross-platform scan-loop wrappers drive *repeated* scans of a user-defined
#   ip:port set, and their two knobs produce observably different behavior:
#
#   (1) Pre-scan ping toggle. With --ping, the unreachable host 172.32.0.99 is gated
#       before TCP and lands in unreachable_results (never scan_results). With
#       --no-ping, that same host is dialled directly and lands in scan_results with a
#       timeout status (never unreachable_results). A clean, deterministic flip.
#   (2) Scan speed. A low --rate run (leaky-bucket = 1 token/s over 6 ports) takes
#       observably longer wall-clock than a high --rate run over the same targets.
#   (3) Repeat. --count 3 produces 3 independent round batches (r01..r03).
#   (4) Windows parity. PSScriptAnalyzer confirms scan-loop.ps1 parses and is clean.
set -euo pipefail

EXEC="docker compose exec -T scanner"
OPEN_IP="172.32.0.10"
DEAD_IP="172.32.0.99"
SPEED_TARGETS="$OPEN_IP:8080,$OPEN_IP:8081,$OPEN_IP:8082,$OPEN_IP:8083,$OPEN_IP:8084,$OPEN_IP:8085"

fail() { echo "SMOKE FAIL: $*" >&2; docker compose logs --tail=50 || true; exit 1; }
cat_glob() { $EXEC sh -c "cat $1 2>/dev/null || true"; }

run_loop() { $EXEC sh -c "timeout 45 scan-loop $*"; }

# ---------------------------------------------------------------------------
echo "[1/4] pre-scan ping toggle"
# --- ping ON: dead host gated before TCP ---
run_loop "--targets '$OPEN_IP:8080,$DEAD_IP:8080' --ping --out /lab/ping-on" >/dev/null
on_scan=$(cat_glob "/lab/ping-on/r01/scan_results-*.csv")
on_unreach=$(cat_glob "/lab/ping-on/r01/unreachable_results-*.csv")
echo "$on_scan" | grep -q "$OPEN_IP" && echo "$on_scan" | grep -q "open" \
  || fail "[ping on] open host $OPEN_IP not reported open; got:\n$on_scan"
echo "$on_unreach" | grep -q "$DEAD_IP" \
  || fail "[ping on] dead host $DEAD_IP not gated to unreachable_results; got:\n$on_unreach"
if echo "$on_scan" | grep -q "$DEAD_IP"; then
  fail "[ping on] dead host $DEAD_IP leaked into scan_results (should be ping-gated); got:\n$on_scan"
fi

# --- ping OFF: dead host dialled directly ---
run_loop "--targets '$OPEN_IP:8080,$DEAD_IP:8080' --no-ping --out /lab/ping-off" >/dev/null
off_scan=$(cat_glob "/lab/ping-off/r01/scan_results-*.csv")
off_unreach=$(cat_glob "/lab/ping-off/r01/unreachable_results-*.csv")
echo "$off_scan" | grep -q "$OPEN_IP" \
  || fail "[ping off] open host $OPEN_IP missing from scan_results; got:\n$off_scan"
echo "$off_scan" | grep -q "$DEAD_IP" \
  || fail "[ping off] dead host $DEAD_IP not dialled (should appear in scan_results); got:\n$off_scan"
if echo "$off_unreach" | grep -q "$DEAD_IP"; then
  fail "[ping off] dead host $DEAD_IP in unreachable_results, but ping was disabled; got:\n$off_unreach"
fi
echo "  OK: --ping gates $DEAD_IP to unreachable; --no-ping dials it into scan_results"

# ---------------------------------------------------------------------------
echo "[2/4] scan speed knob (low --rate vs high --rate over 6 ports)"
t0=$(date +%s); run_loop "--targets '$SPEED_TARGETS' --rate 1 --out /lab/slow"  >/dev/null; t1=$(date +%s)
t2=$(date +%s); run_loop "--targets '$SPEED_TARGETS' --rate 1000 --out /lab/fast" >/dev/null; t3=$(date +%s)
slow=$((t1 - t0)); fast=$((t3 - t2))
echo "  slow(rate=1)=${slow}s  fast(rate=1000)=${fast}s"
[ "$slow" -ge 3 ] || fail "[speed] slow run was ${slow}s; expected >= 3s under a 1 token/s bucket over 6 ports"
[ "$slow" -gt "$fast" ] || fail "[speed] slow(${slow}s) not greater than fast(${fast}s); rate knob had no effect"
[ $((slow - fast)) -ge 2 ] || fail "[speed] slow-fast gap ${slow}-${fast} < 2s; rate knob effect too weak to trust"
# Corroborate both scanned the same work (so the difference is pacing, not coverage).
slow_rows=$(cat_glob "/lab/slow/r01/scan_results-*.csv" | grep -c "$OPEN_IP" || true)
fast_rows=$(cat_glob "/lab/fast/r01/scan_results-*.csv" | grep -c "$OPEN_IP" || true)
[ "$slow_rows" -eq "$fast_rows" ] && [ "$fast_rows" -ge 6 ] \
  || fail "[speed] coverage differed (slow=$slow_rows fast=$fast_rows rows); expected equal, >=6"
echo "  OK: rate=1 took ${slow}s vs rate=1000 ${fast}s for the same $fast_rows-port scan"

# ---------------------------------------------------------------------------
echo "[3/4] repeat knob (--count 3)"
run_loop "--targets '$OPEN_IP:8080' --count 3 --interval 1 --out /lab/repeat" >/dev/null
rounds=0
for r in 1 2 3; do
  rd=$(printf '/lab/repeat/r%02d' "$r")
  body=$(cat_glob "$rd/scan_results-*.csv")
  echo "$body" | grep -q "$OPEN_IP" || fail "[repeat] round $r missing scan_results for $OPEN_IP"
  rounds=$((rounds + 1))
done
[ "$rounds" -eq 3 ] || fail "[repeat] expected 3 round batches, found $rounds"
echo "  OK: --count 3 produced 3 independent round batches (r01..r03)"

# ---------------------------------------------------------------------------
echo "[4/4] Windows wrapper static analysis (PSScriptAnalyzer)"
lint_out=$(docker compose run --rm ps-lint 2>&1) || fail "[ps-lint] PSScriptAnalyzer failed:\n$lint_out"
echo "$lint_out" | grep -q "clean" || fail "[ps-lint] did not report clean:\n$lint_out"
echo "  OK: scan-loop.ps1 passed PSScriptAnalyzer (Error+Warning) clean"

echo
echo "SMOKE PASS: scan-loop wrappers drive repeated user-defined scans; --ping/--no-ping"
echo "            flips the dead host between unreachable_results and scan_results; --rate"
echo "            visibly paces the scan (${slow}s vs ${fast}s); --count 3 yields 3 batches;"
echo "            and the Windows .ps1 is PSScriptAnalyzer-clean."
