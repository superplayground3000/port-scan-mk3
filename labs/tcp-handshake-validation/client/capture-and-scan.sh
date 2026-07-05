#!/usr/bin/env bash
#
# Capture the port-scan-mk3 TCP scan on this container's eth0, then use tshark to
# prove the on-the-wire behaviour matches RFC 9293:
#   - open port  : three-way handshake (SYN -> SYN,ACK -> ACK) then a FIN-based
#                  teardown initiated by the scanner, with NO application data.
#   - closed port: SYN answered by RST (connection refused), no SYN,ACK.
#
# tcpdump does the live capture (needs CAP_NET_RAW). tshark only reads the pcap
# file afterwards, which needs no privilege.
set -euo pipefail

TARGET_IP="${TARGET_IP:-172.31.199.10}"
OPEN_PORT="${OPEN_PORT:-9000}"
CLOSED_PORT="${CLOSED_PORT:-9001}"
IFACE="${IFACE:-eth0}"

OUT_DIR="/work/out"
PCAP="$OUT_DIR/scan.pcap"
FIELDS="$OUT_DIR/packets.tsv"
REPORT="$OUT_DIR/REPORT.txt"
TCPDUMP_ERR="$OUT_DIR/tcpdump.stderr"
mkdir -p "$OUT_DIR"
: >"$REPORT"

log() { echo "$@" | tee -a "$REPORT"; }

# ---- 1. start the sniffer -------------------------------------------------
FILTER="tcp and host ${TARGET_IP} and (port ${OPEN_PORT} or port ${CLOSED_PORT})"
log "== sniffer =="
log "interface : ${IFACE}"
log "filter    : ${FILTER}"
tcpdump -i "$IFACE" -n -w "$PCAP" "$FILTER" 2>"$TCPDUMP_ERR" &
TCPDUMP_PID=$!

ready=0
for _ in $(seq 1 40); do
  if grep -q "listening on" "$TCPDUMP_ERR" 2>/dev/null; then ready=1; break; fi
  if ! kill -0 "$TCPDUMP_PID" 2>/dev/null; then break; fi
  sleep 0.25
done
if [[ "$ready" -ne 1 ]]; then
  echo "FAIL: tcpdump did not start capturing" >&2
  cat "$TCPDUMP_ERR" >&2 || true
  exit 1
fi

# ---- 2. run the real scan -------------------------------------------------
log ""
log "== scan =="
log "target ${TARGET_IP}: open=${OPEN_PORT} closed=${CLOSED_PORT}"
set +e
port-scan scan \
  -cidr-file /work/inputs/cidr.csv \
  -port-file /work/inputs/ports.csv \
  -disable-api=true \
  -disable-pre-scan-ping=true \
  -workers 1 \
  -timeout 2s \
  -delay 0ms \
  -output "$OUT_DIR/scan.csv" \
  -log-level info 2>&1 | tee -a "$REPORT"
set -e

# Let the teardown packets land, then stop the sniffer cleanly.
sleep 1
kill -INT "$TCPDUMP_PID" 2>/dev/null || true
wait "$TCPDUMP_PID" 2>/dev/null || true

# ---- 3. decode the capture ------------------------------------------------
# tshark's default one-line-per-packet summary is the human-readable table; it
# shows the TCP flag names ([SYN], [SYN, ACK], [FIN, ACK], [RST, ACK]).
log ""
log "== captured packets =="
tshark -r "$PCAP" 2>/dev/null | tee -a "$REPORT"

# Also keep a flag-tagged field dump alongside the pcap for inspection.
tshark -r "$PCAP" -Y tcp -T fields \
  -e frame.number -e ip.src -e tcp.srcport -e ip.dst -e tcp.dstport \
  -e tcp.flags.str -e tcp.len -E separator=/t >"$FIELDS" 2>/dev/null || true

# ---- 4. assert RFC 9293 behaviour -----------------------------------------
# Count packets with tshark DISPLAY FILTERS (robust to field-format quirks:
# tcp.flags.syn prints True/False as a field but compares as ==1 in a filter).
tcount() { tshark -r "$PCAP" -Y "$1" 2>/dev/null | grep -c . || true; }
T="$TARGET_IP"

# open-port stream
syn_out=$(tcount        "ip.dst==$T && tcp.dstport==$OPEN_PORT && tcp.flags.syn==1 && tcp.flags.ack==0")
synack_in=$(tcount      "ip.src==$T && tcp.srcport==$OPEN_PORT && tcp.flags.syn==1 && tcp.flags.ack==1")
ack_out=$(tcount        "ip.dst==$T && tcp.dstport==$OPEN_PORT && tcp.flags.syn==0 && tcp.flags.ack==1 && tcp.flags.fin==0 && tcp.flags.reset==0")
fin_out=$(tcount        "ip.dst==$T && tcp.dstport==$OPEN_PORT && tcp.flags.fin==1")
data_open=$(tcount      "(tcp.dstport==$OPEN_PORT || tcp.srcport==$OPEN_PORT) && tcp.len>0")
rst_out_open=$(tcount   "ip.dst==$T && tcp.dstport==$OPEN_PORT && tcp.flags.reset==1")

# closed-port stream
syn_out_closed=$(tcount   "ip.dst==$T && tcp.dstport==$CLOSED_PORT && tcp.flags.syn==1 && tcp.flags.ack==0")
rst_in_closed=$(tcount    "ip.src==$T && tcp.srcport==$CLOSED_PORT && tcp.flags.reset==1")
synack_in_closed=$(tcount "ip.src==$T && tcp.srcport==$CLOSED_PORT && tcp.flags.syn==1 && tcp.flags.ack==1")

log ""
log "== RFC 9293 assertions =="
fails=0
check() { # name  actual  op  expected
  local name="$1" actual="$2" op="$3" exp="$4" ok
  if [[ "$op" == "ge" ]]; then [[ "$actual" -ge "$exp" ]] && ok=1 || ok=0; fi
  if [[ "$op" == "eq" ]]; then [[ "$actual" -eq "$exp" ]] && ok=1 || ok=0; fi
  if [[ "$ok" -eq 1 ]]; then
    log "  PASS  ${name} (got ${actual})"
  else
    log "  FAIL  ${name} (got ${actual}, want ${op} ${exp})"
    fails=$((fails+1))
  fi
}

log "-- open port ${OPEN_PORT}: three-way handshake --"
check "scanner sent SYN"                     "$syn_out"      ge 1
check "target replied SYN,ACK"               "$synack_in"    ge 1
check "scanner sent completing ACK (full connect, not half-open)" "$ack_out" ge 1
log "-- open port ${OPEN_PORT}: teardown + hygiene --"
check "scanner initiated FIN teardown"       "$fin_out"      ge 1
check "no application data sent (pure connect scan)" "$data_open" eq 0
check "scanner did NOT abort with RST on open port"  "$rst_out_open" eq 0
log "-- closed port ${CLOSED_PORT}: refusal --"
check "scanner sent SYN"                      "$syn_out_closed"   ge 1
check "target answered with RST (refused)"    "$rst_in_closed"    ge 1
check "target sent NO SYN,ACK on closed port" "$synack_in_closed" eq 0

log ""
if [[ "$fails" -eq 0 ]]; then
  log "VERDICT: PASS — scan performed only RFC 9293 three-way connect + FIN/RST teardown."
  exit 0
fi
log "VERDICT: FAIL — ${fails} assertion(s) failed."
exit 1
