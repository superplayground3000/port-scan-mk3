#!/usr/bin/env bash
#
# Property under test: port-scan-mk3's TCP scan performs only an RFC 9293
# three-way handshake on an open port and a FIN/RST teardown on close — proven
# by a packet capture taken inside the scanner container.
#
# validate_lab.sh has already brought the target up healthy. Here we run the
# scanner one-shot (it captures + scans + analyzes) and assert its verdict.
set -euo pipefail

LAB_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$LAB_DIR"

# Clean up the scanner container + its named volume on exit. validate_lab.sh's
# own `docker compose down -v` does not include the `scan` profile, so the lab
# tidies after itself here and leaves zero artifacts on the host.
cleanup() { docker compose --profile scan down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

# Build the scanner image first (compiles the real port-scan-mk3 + tshark).
# Kept out of the timed run below because it is one-time infrastructure.
docker compose --profile scan build scanner

echo "== running scanner (capture + scan + tshark analysis) =="
set +e
OUT="$(timeout 120s docker compose --profile scan run --rm -T scanner 2>&1)"
code=$?
set -e

echo "$OUT"

if echo "$OUT" | grep -q "VERDICT: PASS"; then
  echo
  echo "SMOKE PASS: captured packets show a full three-way handshake (SYN, SYN-ACK, ACK) on the open port, a scanner-initiated FIN teardown with no application data, and a RST refusal on the closed port — RFC 9293 connect scan confirmed."
  exit 0
fi

echo "SMOKE FAIL: scanner did not report VERDICT: PASS (exit ${code})." >&2
echo "---- target logs ----" >&2
docker compose logs --tail=50 target >&2 || true
exit 1
