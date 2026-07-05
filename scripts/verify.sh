#!/usr/bin/env bash
#
# verify.sh — the single source of truth for the constitution's Quality Gates.
#
# Runs the same checks locally that CI runs on every pull request, so "green
# locally" means "green in CI". Referenced by .claude/rules/constitution.md
# (Quality Gates) and by CLAUDE.md.
#
# Usage:
#   bash scripts/verify.sh            # fast gates: fmt, vet, build, test+coverage
#   bash scripts/verify.sh --e2e      # also run the isolated Docker e2e suite
#   bash scripts/verify.sh --only-e2e # run only the e2e suite
#
# Exit code 0 means every selected gate passed. Any non-zero exit means a gate
# failed; the failing gate prints why. Do NOT declare work complete on a
# non-zero exit.
#
# Windows: run from Git Bash or WSL. The product itself cross-compiles for
# Windows (see `make build-windows`); this dev script needs a POSIX shell.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

RUN_FAST=1
RUN_E2E=0
for arg in "$@"; do
  case "$arg" in
    --e2e)      RUN_E2E=1 ;;
    --only-e2e) RUN_E2E=1; RUN_FAST=0 ;;
    -h|--help)
      sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *)
      echo "verify.sh: unknown argument '$arg' (try --help)" >&2
      exit 2 ;;
  esac
done

step() { printf '\n=== %s ===\n' "$1"; }

if [[ "$RUN_FAST" -eq 1 ]]; then
  step "gofmt (formatting)"
  # gofmt -l lists files that are NOT formatted. Any output is a failure.
  # Capture the file list first: gofmt with no path args would read stdin and
  # hang, so skip the check when there are no Go files.
  go_files="$(git ls-files '*.go')"
  unformatted=""
  if [[ -n "$go_files" ]]; then
    unformatted="$(gofmt -l -- $go_files 2>/dev/null || true)"
  fi
  if [[ -n "$unformatted" ]]; then
    echo "The following files are not gofmt-clean. Run: gofmt -w <file>" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  echo "gofmt: clean"

  step "go vet (static analysis)"
  go vet ./...
  echo "go vet: clean"

  step "go build (compile everything)"
  go build ./...
  echo "go build: ok"

  step "go test -race (all packages)"
  go test -race -shuffle=on ./...

  step "coverage gate (>= 85% total)"
  bash "$ROOT/scripts/coverage_gate.sh"
fi

if [[ "$RUN_E2E" -eq 1 ]]; then
  step "isolated e2e (Docker Compose)"
  bash "$ROOT/e2e/run_e2e.sh"
fi

step "RESULT"
echo "All selected quality gates passed."
