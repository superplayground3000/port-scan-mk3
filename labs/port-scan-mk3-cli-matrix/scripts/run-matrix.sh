#!/usr/bin/env bash
# run-matrix.sh — executes the 36-case port-scan-mk3 CLI flag matrix INSIDE the scanner
# container and asserts observable output. Exits 0 only if every case passes.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/assert.sh
source "$HERE/lib/assert.sh"

FIX=/lab/fixtures
OUT=/lab/out
rm -rf "$OUT"; mkdir -p "$OUT"

OPEN=172.30.0.10; CLOSED=172.30.0.11; FILTERED=172.30.0.12; UNREACH=172.30.0.99
POK=pressure-ok; PHI=pressure-high; P5=pressure-5xx; PTO=pressure-timeout
PA1=pressure-auth-1; PA2=pressure-auth-2

# ---------------- A: validate ----------------
A1() { local d="$OUT/A1"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" -format human >"$d/o" 2>"$d/e"
  assert_eq "A1 validate basic human exit0" 0 "$?"
  assert_contains "A1 validate human valid=true" "$d/o" '^valid=true'; }
A2() { local d="$OUT/A2"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" -format json >"$d/o" 2>"$d/e"
  assert_eq "A2 validate basic json exit0" 0 "$?"
  assert_contains "A2 validate json valid:true" "$d/o" '"valid":true'; }
A3() { local d="$OUT/A3"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/rich.csv" -format json >"$d/o" 2>"$d/e"
  assert_eq "A3 validate rich json exit0" 0 "$?"
  assert_contains "A3 validate rich valid:true" "$d/o" '"valid":true'; }
A4() { local d="$OUT/A4"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -format json >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "A4 validate missing port-file exit1" 1 "$rc"
  assert_contains "A4 validate valid:false" "$d/o" '"valid":false'; }

# ---------------- B: scan modes/IO (TCP-state group => -disable-pre-scan-ping) ----------------
B_states() { local d="$OUT/B_states"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -timeout 300ms -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B scan states exit0" 0 "$?"
  local sr op; sr="$(latest "$d" scan_results)"; op="$(latest "$d" opened_results)"
  assert_contains    "B1 open in opened_results"  "$op" "^$OPEN,$OPEN/32,8080,open,"
  assert_contains    "B2 closed in scan_results"  "$sr" "^$CLOSED,$CLOSED/32,8080,close,"
  assert_contains    "B3 filtered close(timeout)" "$sr" "^$FILTERED,$FILTERED/32,8080,close\\(timeout\\),"; }
B4() { local d="$OUT/B4"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/rich.csv" -disable-api -disable-pre-scan-ping -timeout 300ms -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B4 rich scan exit0" 0 "$?"
  local sr; sr="$(latest "$d" scan_results)"
  assert_contains     "B4 rich accept .10 scanned open" "$sr" "^$OPEN,$OPEN/32,8080,open,"
  assert_not_contains "B4 rich deny .11 skipped"        "$sr" "^$CLOSED,"
  assert_not_contains "B4 rich udp .12 skipped"         "$sr" "$FILTERED"; }
B5() { local d="$OUT/B5"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-custom-headers.csv" -port-file "$FIX/ports.csv" \
    -cidr-ip-col source_ip -cidr-ip-cidr-col source_range \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B5 custom-col scan exit0" 0 "$?"
  assert_contains "B5 custom-col open .10" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
B6() { local d="$OUT/B6"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -bucket-rate 500 -bucket-capacity 500 -delay 5ms -workers 20 \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B6 rate-control scan exit0" 0 "$?"
  assert_contains "B6 open found with rate flags" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
B7() { local sub="$OUT/B7/nested"; mkdir -p "$sub"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$sub/run.csv" >"$OUT/B7.o" 2>"$OUT/B7.e"
  assert_eq "B7 custom-output scan exit0" 0 "$?"
  assert_file_exists "B7 scan_results under custom dir" "$(latest "$sub" scan_results)"; }
B8() { local d="$OUT/B8"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -log-level debug -format json -disable-api -disable-pre-scan-ping -output "$d/dbg.csv" >"$d/o1" 2>"$d/dbg.err"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -log-level error -disable-api -disable-pre-scan-ping -output "$d/err.csv" >"$d/o2" 2>"$d/err.err"
  assert_gt "B8 debug more verbose than error level" "$(wc -l <"$d/dbg.err")" "$(wc -l <"$d/err.err")"; }
B9() { local d="$OUT/B9"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -quiet -disable-api -disable-pre-scan-ping -output "$d/q.csv" >"$d/qo" 2>"$d/q.err"
  assert_eq "B9 quiet scan exit0" 0 "$?"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$d/n.csv" >"$d/no" 2>"$d/n.err"
  assert_gt "B9 normal noisier than quiet" "$(wc -l <"$d/n.err")" "$(wc -l <"$d/q.err")"
  assert_file_exists "B9 results still written under quiet" "$(latest "$d" scan_results)"; }

# ---------------- C: reachability (default ping; needs NET_RAW) ----------------
C1() { local d="$OUT/C1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" -disable-api -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "C1 ping-enabled reachable exit0" 0 "$?"
  assert_contains "C1 reachable .10 scanned open" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
C2() { local d="$OUT/C2"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/unreachable.csv" -port-file "$FIX/ports.csv" -disable-api -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "C2 ping-enabled scan exit0" 0 "$?"
  assert_contains "C2 .99 marked unreachable" "$(latest "$d" unreachable_results)" "^$UNREACH,$UNREACH/32,unreachable,"; }

# ---------------- D: pressure control ----------------
D1() { local d="$OUT/D1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -pressure-api "http://$POK:8080/api/pressure" -pressure-interval 1s \
    -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D1 simple pressure ok exit0" 0 "$?"
  assert_contains "D1 open found under pressure-ok" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
D2() { local d="$OUT/D2"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$PHI:8080/api/pressure" -pressure-interval 1s \
    -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D2 high->low pressure exit0" 0 "$?"
  assert_contains "D2 scan paused on high pressure" "$d/e" 'router pressure overload.*scan automatically paused'
  assert_contains "D2 scan resumed on low pressure" "$d/e" 'router pressure recovered.*scan automatically resumed'; }
D3() { local d="$OUT/D3"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$P5:8080/api/pressure" -pressure-interval 1s \
    -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D3 5xx fail-safe abort exit1" 1 "$rc"
  assert_contains "D3 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }
D4() { local d="$OUT/D4"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$PTO:8080/api/pressure" -pressure-interval 1s \
    -timeout 3s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D4 timeout fail-safe abort exit1" 1 "$rc"
  assert_contains "D4 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }
D5() { local d="$OUT/D5"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D5 disable-api exit0" 0 "$?"
  assert_not_contains "D5 no pressure polling logs" "$d/e" '\[API\] pressure api status'
  assert_contains "D5 open found" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
D6() { local d="$OUT/D6"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" -pressure-data-url "http://$PA1:8080/data" \
    -pressure-client-id test-client -pressure-client-secret test-secret \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D6 auth single-source exit0" 0 "$?"
  assert_contains "D6 authenticated poll succeeded" "$d/e" 'pressure api status=ok'; }
D7() { local d="$OUT/D7"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" \
    -pressure-data-url "http://$PA1:8080/data,http://$PA2:8080/data" \
    -pressure-client-id test-client -pressure-client-secret test-secret \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D7 auth multi-source exit0" 0 "$?"
  assert_contains "D7 multi-source poll succeeded" "$d/e" 'pressure api status=ok'; }
D8() { local d="$OUT/D8"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" -pressure-data-url "http://$PA1:8080/data" \
    -pressure-client-id test-client -pressure-client-secret WRONG-SECRET \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D8 bad-auth fail-safe abort exit1" 1 "$rc"
  assert_contains "D8 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }

# ---------------- E: resume ----------------
E1() { local d="$OUT/E1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -disable-api -disable-pre-scan-ping -timeout 1s -workers 1 -output "$d/scan.csv" >"$d/r1o" 2>"$d/r1e" &
  local pid=$!; sleep 3; kill -INT "$pid" 2>/dev/null; wait "$pid"; local rc1=$?
  assert_eq "E1 interrupted exit130" 130 "$rc1"
  assert_file_exists "E1 resume_state.json written" "$d/resume_state.json"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -disable-api -disable-pre-scan-ping -timeout 1s -workers 1 -resume "$d/resume_state.json" -output "$d/scan.csv" >"$d/r2o" 2>"$d/r2e"
  assert_eq "E1 resumed completes exit0" 0 "$?"; }
E2() { local d="$OUT/E2"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -resume "$FIX/resume-mismatch.json" -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_ne "E2 mismatch nonzero exit" 0 "$rc"
  assert_contains "E2 total_count mismatch error" "$d/e" 'chunk total_count mismatch'; }

# ---------------- F: preprocess ----------------
F1() { local d="$OUT/F1"; mkdir -p "$d"
  preprocess --input "$FIX/rich.csv" --cleaned-cidrs "$FIX/cleaned-cidrs.csv" --fab-name fab1 --output-dir "$d" >"$d/o" 2>"$d/e"
  assert_eq "F1 preprocess exit0" 0 "$?"
  local out; out="$(find "$d/fab1" -name input.csv 2>/dev/null | head -n1)"
  assert_file_exists  "F1 output produced"         "$out"
  assert_contains     "F1 keeps non-closed .10"    "$out" "$OPEN"
  assert_not_contains "F1 removes closed .12"      "$out" "$FILTERED"; }
F2() { local d="$OUT/F2"; mkdir -p "$d"
  preprocess --input "$FIX/rich.csv" --cleaned-cidrs "$FIX/cleaned-cidrs.csv" --fab-name NOPE --output-dir "$d" >"$d/o" 2>"$d/e"
  assert_eq "F2 preprocess no-match exit0" 0 "$?"
  local out; out="$(find "$d/NOPE" -name input.csv 2>/dev/null | head -n1)"
  assert_contains "F2 all rows kept when fab no-match" "$out" "$FILTERED"; }

# ---------------- G: enrich-targets ----------------
G1() { local d="$OUT/G1"; mkdir -p "$d"
  enrich-targets --input "$FIX/minimal.csv" --cidr-list "$FIX/cidrs.csv" --service-map "$FIX/services.csv" --output "$d/enriched.csv" >"$d/o" 2>"$d/e"
  assert_eq "G1 enrich exit0" 0 "$?"
  assert_contains "G1 enriched .10 accept tcp" "$d/enriched.csv" "$OPEN,.*,8080,accept,"
  assert_contains "G1 service_label mapped"    "$d/enriched.csv" "http-test"; }
G2() { local d="$OUT/G2"; mkdir -p "$d"
  enrich-targets --input "$FIX/minimal-mixed.csv" --cidr-list "$FIX/cidrs.csv" --service-map "$FIX/services.csv" --output "$d/enriched.csv" >"$d/o" 2>"$d/e"
  assert_eq "G2 enrich exit0 (skips bad rows)" 0 "$?"
  assert_contains     "G2 valid row enriched"   "$d/enriched.csv" "$OPEN"
  assert_not_contains "G2 invalid host skipped" "$d/enriched.csv" "not-an-ip"; }

# ---------------- H: cidr-compare ----------------
H1() { local d="$OUT/H1"; mkdir -p "$d"
  cidr-compare -deny-file "$FIX/deny.csv" -open-file "$FIX/open.csv" >"$d/o" 2>"$d/e"
  assert_eq "H1 cidr-compare exit0" 0 "$?"
  assert_contains "H1 header line"      "$d/o" '^deny_cidr,open_cidr'
  assert_contains "H1 containment row"  "$d/o" "172.30.0.0/24,$FILTERED/32"; }
H2() { local d="$OUT/H2"; mkdir -p "$d"
  CIDR_COMPARE_DENY_FILE="$FIX/deny.csv" CIDR_COMPARE_OPEN_FILE="$FIX/open.csv" cidr-compare >"$d/o" 2>"$d/e"
  assert_eq "H2 env-form exit0" 0 "$?"
  assert_contains "H2 env-form containment row" "$d/o" "172.30.0.0/24,$FILTERED/32"; }
H3() { local d="$OUT/H3"; mkdir -p "$d"
  cidr-compare -deny-file "$FIX/deny-none.csv" -open-file "$FIX/open.csv" >"$d/o" 2>"$d/e"
  assert_eq "H3 no-overlap exit0" 0 "$?"
  assert_eq "H3 only header (no rows)" "1" "$(wc -l <"$d/o" | tr -d ' ')"; }

# ---------------- I: csv-transform ----------------
I1() { local d="$OUT/I1"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy.csv" --output "$d/t.csv" >"$d/o" 2>"$d/e"
  assert_eq "I1 csv-transform exit0" 0 "$?"
  assert_contains "I1 FALSE row .10 included" "$d/t.csv" "$OPEN"; }
I2() { local d="$OUT/I2"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy-custom.csv" --output "$d/t.csv" --host-col H --port-col P --pass-col Result >"$d/o" 2>"$d/e"
  assert_eq "I2 custom-cols exit0" 0 "$?"
  assert_contains "I2 custom-cols .10 included" "$d/t.csv" "$OPEN"; }
I3() { local d="$OUT/I3"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy.csv" --output "$d/t.csv" >"$d/o" 2>"$d/e"
  assert_eq "I3 csv-transform exit0" 0 "$?"
  assert_not_contains "I3 TRUE row .11 skipped" "$d/t.csv" "$CLOSED"; }
I4() { local d="$OUT/I4"; mkdir -p "$d"
  TRANSFORM_INPUT="$FIX/legacy.csv" TRANSFORM_OUTPUT="$d/t.csv" csv-transform >"$d/o" 2>"$d/e"
  assert_eq "I4 env-form exit0" 0 "$?"
  assert_contains     "I4 env-form .10 included" "$d/t.csv" "$OPEN"
  assert_not_contains "I4 env-form .11 skipped"  "$d/t.csv" "$CLOSED"; }

for c in A1 A2 A3 A4 B_states B4 B5 B6 B7 B8 B9 C1 C2 \
         D1 D2 D3 D4 D5 D6 D7 D8 E1 E2 F1 F2 G1 G2 H1 H2 H3 I1 I2 I3 I4; do
  "$c"
done

summary
