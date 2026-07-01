#!/usr/bin/env bash
#
# scan-loop.sh — repeat a port scan over a user-defined set of ip:port targets,
# with two operator knobs: scan speed and whether to run the pre-scan ping.
#
# It is a thin, dependency-free wrapper around this project's `port-scan` binary:
# it turns a comma-separated --targets list into the cidr-file / port-file CSVs the
# binary expects, then drives the scan in a loop. All scan logic (TCP connect,
# pre-scan ICMP ping, leaky-bucket rate limiting) lives in the binary; this script
# only constructs the command line and repeats it.
#
# Target model: the scan covers the cross product of {distinct IPs} x {distinct
# ports} drawn from --targets, matching port-scan's cidr-file x port-file contract.
# List one port per host and you get exactly those pairs.
#
# Each repeat writes its own batch under <out>/r<NN>/ so rounds never collide.
#
# Usage:
#   scan-loop.sh --targets "10.0.0.1:80,10.0.0.2:443" [flags]
#
# Flags:
#   --targets LIST     Required. Comma-separated ip:port pairs.
#   --rate N           Scan speed: leaky-bucket tokens/sec (also burst). Default 100.
#                      Lower = slower/gentler; higher = faster.
#   --workers N        Concurrent scan workers. Default 10.
#   --ping             Run the pre-scan ICMP ping; unreachable hosts are skipped. (default)
#   --no-ping          Skip the pre-scan ping; every host is dialled directly.
#   --ping-timeout D   Pre-scan ping budget per host, e.g. 100ms, 1s. Default 100ms.
#   --timeout D        TCP dial timeout, e.g. 100ms, 2s. Default 100ms.
#   --count N          Repeat the whole scan N times. Default 1.
#   --interval S       Seconds to wait between repeats. Default 0.
#   --out DIR          Output base directory. Default ./scan-out.
#   --bin PATH         Path to the port-scan binary. Default: port-scan (on PATH).
#   -h, --help         Show this help and exit.
set -euo pipefail

# --- defaults ---
TARGETS=""
RATE=100
WORKERS=10
PING=1
PING_TIMEOUT="100ms"
TIMEOUT="100ms"
COUNT=1
INTERVAL=0
OUT="./scan-out"
BIN="port-scan"

usage() { sed -n '2,/^set -euo/p' "$0" | sed 's/^# \{0,1\}//; s/^#//' | sed '$d'; }

die() { echo "scan-loop: $*" >&2; exit 2; }

# --- parse args ---
while [ $# -gt 0 ]; do
  case "$1" in
    --targets)      TARGETS="${2:-}"; shift 2 ;;
    --rate)         RATE="${2:-}"; shift 2 ;;
    --workers)      WORKERS="${2:-}"; shift 2 ;;
    --ping)         PING=1; shift ;;
    --no-ping)      PING=0; shift ;;
    --ping-timeout) PING_TIMEOUT="${2:-}"; shift 2 ;;
    --timeout)      TIMEOUT="${2:-}"; shift 2 ;;
    --count)        COUNT="${2:-}"; shift 2 ;;
    --interval)     INTERVAL="${2:-}"; shift 2 ;;
    --out)          OUT="${2:-}"; shift 2 ;;
    --bin)          BIN="${2:-}"; shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    *)              die "unknown flag: $1 (try --help)" ;;
  esac
done

[ -n "$TARGETS" ] || die "--targets is required (e.g. --targets \"10.0.0.1:80,10.0.0.2:443\")"
command -v "$BIN" >/dev/null 2>&1 || [ -x "$BIN" ] || die "port-scan binary not found: $BIN"
case "$COUNT" in ''|*[!0-9]*) die "--count must be a non-negative integer" ;; esac
[ "$COUNT" -ge 1 ] || die "--count must be >= 1"

# --- split targets into unique IPs and unique ports ---
declare -A seen_ip seen_port
ips=() ports=()
IFS=',' read -ra pairs <<< "$TARGETS"
for pair in "${pairs[@]}"; do
  pair="$(echo "$pair" | tr -d '[:space:]')"
  [ -n "$pair" ] || continue
  ip="${pair%:*}"; port="${pair##*:}"
  [ -n "$ip" ] && [ -n "$port" ] || die "bad target '$pair' (want ip:port)"
  case "$port" in ''|*[!0-9]*) die "bad port in '$pair'" ;; esac
  if [ -z "${seen_ip[$ip]:-}" ];   then seen_ip[$ip]=1;   ips+=("$ip"); fi
  if [ -z "${seen_port[$port]:-}" ]; then seen_port[$port]=1; ports+=("$port"); fi
done
[ "${#ips[@]}" -gt 0 ] || die "no valid targets parsed"

# --- materialise the CSVs the binary consumes ---
mkdir -p "$OUT"
CIDR_FILE="$OUT/_targets-cidr.csv"
PORT_FILE="$OUT/_targets-port.csv"
{ echo "ip,ip_cidr"; for ip in "${ips[@]}"; do echo "$ip,$ip/32"; done; } > "$CIDR_FILE"
{ for p in "${ports[@]}"; do echo "$p/tcp"; done; } > "$PORT_FILE"

# --- build the static part of the command ---
ping_flag=()
[ "$PING" -eq 0 ] && ping_flag=(-disable-pre-scan-ping)

echo "scan-loop: ${#ips[@]} host(s) x ${#ports[@]} port(s), rate=$RATE workers=$WORKERS \
ping=$([ "$PING" -eq 1 ] && echo on || echo off) count=$COUNT"

# --- repeat loop ---
n=0
while [ "$n" -lt "$COUNT" ]; do
  n=$((n + 1))
  round_dir="$(printf '%s/r%02d' "$OUT" "$n")"
  mkdir -p "$round_dir"
  echo "scan-loop: round $n/$COUNT -> $round_dir"
  "$BIN" scan \
    -cidr-file "$CIDR_FILE" \
    -port-file "$PORT_FILE" \
    -output "$round_dir/scan_results.csv" \
    -bucket-rate "$RATE" \
    -bucket-capacity "$RATE" \
    -workers "$WORKERS" \
    -timeout "$TIMEOUT" \
    -pre-scan-ping-timeout "$PING_TIMEOUT" \
    -disable-api \
    -quiet \
    "${ping_flag[@]}"
  if [ "$n" -lt "$COUNT" ] && [ "$INTERVAL" != "0" ]; then
    sleep "$INTERVAL"
  fi
done

echo "scan-loop: done ($COUNT round(s) under $OUT)"
