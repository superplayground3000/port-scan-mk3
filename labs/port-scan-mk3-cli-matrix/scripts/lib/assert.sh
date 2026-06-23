# shellcheck shell=bash
# Assertion helpers for the CLI matrix. Source this; it tracks PASS/FAIL and prints a summary.

PASS=0
FAIL=0

_grn=$'\033[32m'; _red=$'\033[31m'; _rst=$'\033[0m'

pass() { PASS=$((PASS+1)); printf '%sPASS%s %s\n' "$_grn" "$_rst" "$1"; }
fail() { FAIL=$((FAIL+1)); printf '%sFAIL%s %s\n' "$_red" "$_rst" "$1" >&2; }

# assert_eq <name> <expected> <actual>
assert_eq() { if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (expected '$2' got '$3')"; fi; }
# assert_ne <name> <not-expected> <actual>
assert_ne() { if [ "$2" != "$3" ]; then pass "$1"; else fail "$1 (got disallowed '$3')"; fi; }
# assert_gt <name> <a> <b>   (pass if a > b)
assert_gt() { if [ "${2:-0}" -gt "${3:-0}" ]; then pass "$1"; else fail "$1 (expected $2 > $3)"; fi; }
# assert_contains <name> <file> <ere>
assert_contains() { if [ -f "$2" ] && grep -Eq -- "$3" "$2"; then pass "$1"; else fail "$1 (missing /$3/ in ${2:-<none>})"; fi; }
# assert_not_contains <name> <file> <ere>
assert_not_contains() { if [ ! -f "$2" ] || ! grep -Eq -- "$3" "$2"; then pass "$1"; else fail "$1 (unexpected /$3/ in $2)"; fi; }
# assert_file_exists <name> <path>
assert_file_exists() { if [ -n "${2:-}" ] && [ -f "$2" ]; then pass "$1"; else fail "$1 (file missing: ${2:-<empty>})"; fi; }

# latest <dir> <prefix>  -> newest <prefix>-*.csv in dir (empty if none)
latest() { ls -1t "$1/$2"-*.csv 2>/dev/null | head -n1; }

summary() {
	echo "------------------------------------------------------------"
	echo "RESULT: PASS=$PASS FAIL=$FAIL TOTAL=$((PASS+FAIL))"
	[ "$FAIL" -eq 0 ]
}
