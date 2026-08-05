#!/usr/bin/env bash
#
# smoke_release.sh — execute every release binary and check what it reports.
#
# This is the last gate before publication. It answers the one question the
# build gates cannot: does the artifact we are about to publish actually RUN,
# and does it identify itself as the release we think it is?
#
# It is bash, not PowerShell, on purpose: the same script must run on the Linux
# packaging runner and, through Git Bash, on the windows-latest runner — which
# is the only place a Windows EXE can be executed. Docker e2e does NOT cover
# this: the e2e suite runs Linux containers (constitution V), so it validates
# Linux behavior only.
#
# Usage:
#   bash scripts/smoke_release.sh <dir> [expected-version]
#
#   <dir>              directory holding the extracted release binaries
#   [expected-version] if given, every binary must report exactly this version
#                      (the tag). Omit it only for a local dry run.
#
# Exit 0 only when every expected command was found, ran, and reported a
# stamped version, commit and build time.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

BIN_DIR="${1:-}"
EXPECTED_VERSION="${2:-}"

if [ -z "$BIN_DIR" ]; then
  echo "usage: bash scripts/smoke_release.sh <dir> [expected-version]" >&2
  exit 2
fi
if [ ! -d "$BIN_DIR" ]; then
  echo "smoke_release.sh: '$BIN_DIR' is not a directory" >&2
  exit 2
fi

# Discover the expected commands from the source tree rather than from the
# directory being tested: a smoke test that only checks the files it happens to
# find would pass on an archive that is missing a command entirely.
CMDS=()
for main_file in "$ROOT"/cmd/*/main.go; do
  [ -f "$main_file" ] || continue
  CMDS+=("$(basename "$(dirname "$main_file")")")
done
if [ "${#CMDS[@]}" -eq 0 ]; then
  echo "smoke_release.sh: found no cmd/*/main.go — refusing to pass a vacuous check" >&2
  exit 1
fi

failures=0
checked=0

fail() {
  echo "  FAIL: $*" >&2
  failures=$((failures + 1))
}

echo "Smoke-testing release binaries in '$BIN_DIR' ..."

for cmd in "${CMDS[@]}"; do
  bin="$BIN_DIR/$cmd"
  [ -f "$bin" ] || bin="$BIN_DIR/$cmd.exe"
  checked=$((checked + 1))

  if [ ! -f "$bin" ]; then
    fail "$cmd is missing from $BIN_DIR"
    continue
  fi
  chmod +x "$bin" 2>/dev/null || true

  if ! out="$("$bin" --version 2>&1)"; then
    fail "$cmd --version did not run: $out"
    continue
  fi

  first_line="$(printf '%s\n' "$out" | head -1)"
  reported_version="${first_line#"$cmd" version }"

  if [ "$first_line" = "$reported_version" ]; then
    fail "$cmd --version does not start with '$cmd version <version>': $first_line"
    continue
  fi

  # An unstamped build reports the documented placeholders. Publishing one means
  # the release cannot be traced to a commit — exactly the defect issue #70 was
  # opened for, and the reason this check exists.
  if [ "$reported_version" = "dev" ]; then
    fail "$cmd reports version 'dev': the release ldflags did not reach this binary"
    continue
  fi
  if printf '%s\n' "$out" | grep -q '^commit:  unknown$'; then
    fail "$cmd reports an unknown commit: the release ldflags did not reach this binary"
    continue
  fi
  if printf '%s\n' "$out" | grep -q '^built:   unknown$'; then
    fail "$cmd reports an unknown build time: the release ldflags did not reach this binary"
    continue
  fi
  if printf '%s\n' "$out" | grep -q 'modified working tree'; then
    fail "$cmd was built from a modified working tree; it is not reproducible from a commit"
    continue
  fi
  if [ -n "$EXPECTED_VERSION" ] && [ "$reported_version" != "$EXPECTED_VERSION" ]; then
    fail "$cmd reports version '$reported_version', but this release is '$EXPECTED_VERSION'"
    continue
  fi

  echo "  ok: $cmd -> $first_line"
done

# Fail-open guard, matching scripts/verify_dist.sh: a gate that inspected
# nothing must not report success, because it is trusted.
if [ "$checked" -eq 0 ]; then
  echo "smoke_release.sh: executed 0 binaries — refusing to pass a vacuous check" >&2
  exit 1
fi

if [ "$failures" -ne 0 ]; then
  echo "release smoke test FAILED: $failures problem(s) across $checked command(s)." >&2
  exit 1
fi

echo "release smoke test passed: $checked commands ran and reported a stamped version."
