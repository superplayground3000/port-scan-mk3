#!/usr/bin/env bash
#
# verify_dist.sh — the release-artifact gate.
#
# Asserts that every binary `make build` is supposed to produce actually
# exists AND was really compiled for the OS/ARCH that its directory name
# claims. A `dist/linux/port-scan` that is secretly a Windows PE binary (what
# happened when `build-linux` did not set GOOS — issue #65) fails here.
#
# Two independent signals are checked per artifact:
#   1. the toolchain's own build info (`go version -m`), which records the
#      GOOS/GOARCH/CGO_ENABLED the binary was compiled with; and
#   2. the executable header magic on disk (ELF vs. PE "MZ"), which does not
#      depend on the build info being present or truthful.
#
# CGO_ENABLED is pinned to 0 for release artifacts; see docs/MAINTENANCE.md
# section 2 for the reasoning. This gate enforces that decision instead of
# leaving it to a comment.
#
# Usage:
#   bash scripts/verify_dist.sh            # verify ./dist
#   bash scripts/verify_dist.sh <DIR>      # verify an alternate dist root
#
# Exit 0 means every expected artifact is present and correctly targeted.
# Any non-zero exit lists every artifact that failed and why.
#
# Windows: run from Git Bash or WSL (same requirement as scripts/verify.sh).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DIST_DIR="${1:-dist}"

# Release artifacts are built with CGO_ENABLED=0 (see the Makefile and
# docs/MAINTENANCE.md section 2). Keep these three in sync.
EXPECTED_CGO_ENABLED=0

# One entry per release target: <subdir>|<GOOS>|<GOARCH>|<binary suffix>
# Windows ARM64 is deliberately out of scope (issue #65).
TARGETS=(
  "linux|linux|amd64|"
  "windows|windows|amd64|.exe"
)

# Discover the commands independently of the Makefile: this gate must notice a
# command that the Makefile forgot to build, so it may not reuse the Makefile's
# list.
CMDS=()
for main_file in cmd/*/main.go; do
  [ -f "$main_file" ] || continue
  cmd_dir="$(dirname "$main_file")"
  CMDS+=("$(basename "$cmd_dir")")
done

if [ "${#CMDS[@]}" -eq 0 ]; then
  echo "verify_dist.sh: found no cmd/*/main.go — refusing to pass a vacuous check" >&2
  exit 1
fi

failures=0

fail() {
  echo "  FAIL: $*" >&2
  failures=$((failures + 1))
}

# build_setting <go-version-m-output> <key>
# Prints the value of a `build <key>=<value>` line from `go version -m`.
# Exits non-zero when the key is absent.
build_setting() {
  printf '%s\n' "$1" | awk -v key="$2" '
    $1 == "build" && index($2, key "=") == 1 {
      print substr($2, length(key) + 2)
      found = 1
      exit
    }
    END { if (!found) exit 1 }
  '
}

# header_magic <path>
# Prints the first four bytes of a file as lowercase hex, e.g. "7f454c46".
header_magic() {
  od -An -N4 -tx1 < "$1" | tr -d ' \n'
}

echo "Verifying release artifacts in '$DIST_DIR' ..."

checked=0
for target in "${TARGETS[@]}"; do
  IFS='|' read -r subdir want_goos want_goarch suffix <<<"$target"
  for cmd in "${CMDS[@]}"; do
    artifact="$DIST_DIR/$subdir/$cmd$suffix"
    checked=$((checked + 1))

    if [ ! -f "$artifact" ]; then
      fail "$artifact is missing (expected $want_goos/$want_goarch)"
      continue
    fi
    if [ ! -s "$artifact" ]; then
      fail "$artifact is empty"
      continue
    fi

    if ! info="$(go version -m "$artifact" 2>&1)"; then
      fail "$artifact: 'go version -m' failed: $info"
      continue
    fi

    got_goos="$(build_setting "$info" GOOS || true)"
    got_goarch="$(build_setting "$info" GOARCH || true)"
    got_cgo="$(build_setting "$info" CGO_ENABLED || true)"

    if [ "$got_goos" != "$want_goos" ]; then
      fail "$artifact: GOOS is '${got_goos:-<unset>}', want '$want_goos'"
    fi
    if [ "$got_goarch" != "$want_goarch" ]; then
      fail "$artifact: GOARCH is '${got_goarch:-<unset>}', want '$want_goarch'"
    fi
    if [ "$got_cgo" != "$EXPECTED_CGO_ENABLED" ]; then
      fail "$artifact: CGO_ENABLED is '${got_cgo:-<unset>}', want '$EXPECTED_CGO_ENABLED'"
    fi

    # Independent cross-check: the on-disk executable header.
    magic="$(header_magic "$artifact")"
    case "$want_goos" in
      linux)
        # ELF: 0x7F 'E' 'L' 'F'
        if [ "$magic" != "7f454c46" ]; then
          fail "$artifact: header magic '$magic' is not ELF (7f454c46)"
        fi
        ;;
      windows)
        # PE/COFF starts with the DOS stub 'MZ' (0x4D 0x5A).
        if [ "${magic:0:4}" != "4d5a" ]; then
          fail "$artifact: header magic '$magic' does not start with PE 'MZ' (4d5a)"
        fi
        ;;
      *)
        fail "$artifact: verify_dist.sh has no header check for GOOS '$want_goos'"
        ;;
    esac
  done
done

if [ "$failures" -ne 0 ]; then
  echo "dist artifact gate FAILED: $failures problem(s) across $checked expected artifact(s)." >&2
  echo "Run 'make clean && make build' and re-run this gate." >&2
  exit 1
fi

echo "dist artifact gate passed: $checked artifacts, all CGO_ENABLED=$EXPECTED_CGO_ENABLED and correctly targeted."
