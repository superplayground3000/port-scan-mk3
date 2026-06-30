#!/usr/bin/env bash
# Exercises the property end-to-end (black-box, via `docker compose exec`):
#
#   -pre-scan-ping-timeout is the budget the scanner waits for a pre-scan ICMP reply
#   before excluding a host from the TCP scan. Observable as:
#     (a) 172.31.0.10 (pingable, TCP 8080 open) IS scanned and reported open;
#     (b) 172.31.0.99 (no container -> ICMP never answered) is EXCLUDED from the
#         scan and written to unreachable_results with reason "ping failed within <flag>";
#     (c) the reason text tracks the flag value across runs (100ms default, 200ms, 1s).
#
# Each run writes to its own output dir so the timestamped batch files are unambiguous.
set -euo pipefail

EXEC="docker compose exec -T scanner"
OPEN_IP="172.31.0.10"
DEAD_IP="172.31.0.99"

fail() { echo "SMOKE FAIL: $*" >&2; docker compose logs --tail=50 || true; exit 1; }

echo "[setup] copying fixtures into scanner:/lab"
docker compose cp ./fixtures/cidrs.csv scanner:/lab/cidrs.csv
docker compose cp ./fixtures/ports.csv scanner:/lab/ports.csv

# run_scan <flag-or-empty> <outdir>
run_scan() {
  local flag="$1" outdir="$2" extra=""
  [ -n "$flag" ] && extra="-pre-scan-ping-timeout=$flag"
  # 45s hard ceiling so a wiring bug can never hang the lab.
  $EXEC sh -c "rm -rf $outdir && mkdir -p $outdir && \
    timeout 45 port-scan scan -cidr-file /lab/cidrs.csv -port-file /lab/ports.csv \
      -output $outdir/scan_results.csv -disable-api -workers 4 -quiet $extra"
}

cat_glob() { $EXEC sh -c "cat $1 2>/dev/null || true"; }

# Assert the unreachable batch gates DEAD_IP with the expected reason, and the
# scan results include OPEN_IP:8080 while never listing DEAD_IP.
assert_run() {
  local outdir="$1" want_reason="$2"
  local unreach scan
  unreach=$(cat_glob "$outdir/unreachable_results-*.csv")
  scan=$(cat_glob "$outdir/scan_results-*.csv")

  echo "$unreach" | grep -q "$DEAD_IP" \
    || fail "[$outdir] $DEAD_IP missing from unreachable_results; got:\n$unreach"
  echo "$unreach" | grep -q "ping failed within ${want_reason}" \
    || fail "[$outdir] reason did not echo '${want_reason}'; got:\n$unreach"

  echo "$scan" | grep -q "$OPEN_IP" \
    || fail "[$outdir] $OPEN_IP missing from scan_results (pingable host should be scanned); got:\n$scan"
  echo "$scan" | grep -q "open" \
    || fail "[$outdir] no open port in scan_results for $OPEN_IP; got:\n$scan"
  if echo "$scan" | grep -q "$DEAD_IP"; then
    fail "[$outdir] $DEAD_IP appeared in scan_results but should have been gated by pre-scan ping; got:\n$scan"
  fi
  echo "  OK [$outdir]: $OPEN_IP scanned (open); $DEAD_IP gated, reason 'ping failed within ${want_reason}'"
}

echo "[run 1] default timeout (no flag) -> expect reason 'ping failed within 100ms'"
run_scan ""      /lab/out-default
assert_run /lab/out-default "100ms"

echo "[run 2] -pre-scan-ping-timeout=200ms -> expect reason 'ping failed within 200ms'"
run_scan "200ms" /lab/out-200ms
assert_run /lab/out-200ms "200ms"

echo "[run 3] -pre-scan-ping-timeout=1s -> expect reason 'ping failed within 1s'"
run_scan "1s"    /lab/out-1s
assert_run /lab/out-1s "1s"

# Cross-run check: the reason text genuinely differs by flag (not a constant).
r_def=$(cat_glob /lab/out-default/unreachable_results-*.csv)
r_200=$(cat_glob /lab/out-200ms/unreachable_results-*.csv)
r_1s=$(cat_glob  /lab/out-1s/unreachable_results-*.csv)
echo "$r_def" | grep -q "within 100ms" && echo "$r_200" | grep -q "within 200ms" && echo "$r_1s" | grep -q "within 1s" \
  || fail "reason text did not vary with the flag across runs"

echo
echo "SMOKE PASS: -pre-scan-ping-timeout gates the real scan and its value (100ms/200ms/1s)"
echo "            flows verbatim into the unreachable reason; the pingable open host is"
echo "            scanned while the ICMP-unanswered host is excluded before TCP."
