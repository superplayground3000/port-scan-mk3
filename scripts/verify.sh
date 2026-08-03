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
  step "line endings (.gitattributes)"
  # Runs BEFORE gofmt on purpose. Without repository-owned line-ending rules,
  # a Windows checkout with core.autocrlf=true (the Git for Windows installer
  # default) arrives as CRLF and every Go file fails the gofmt gate below —
  # a confusing "your pristine clone is unformatted" (issue #64). Catching it
  # here names the real cause in one line.
  #
  # This checks THIS working tree against the rules the repository declares.
  # The deeper check — simulating a core.autocrlf=true checkout and asserting
  # it stays LF and gofmt-clean — lives in tests/repohygiene/line_endings_test.go
  # so that it also runs in CI, on Linux and on Windows, via `go test ./...`.
  if [[ ! -f .gitattributes ]]; then
    echo "Missing .gitattributes: line endings would fall back to each developer's" >&2
    echo "core.autocrlf, and a Windows checkout would fail the gofmt gate. See issue #64." >&2
    exit 1
  fi
  eol_mismatch="$(git ls-files --eol | awk -F'\t' '
    {
      info = $1; path = $2
      w = ""
      if (match(info, /w\/[a-z-]+/)) w = substr(info, RSTART + 2, RLENGTH - 2)
      want = ""
      if (info ~ /eol=crlf/)    want = "crlf"
      else if (info ~ /eol=lf/) want = "lf"
      # Skip binaries, empty files and symlinks: no line endings to disagree on.
      if (want == "" || w == "" || w == "none" || w == "-text") next
      if (w != want) printf "%s (on disk: %s, declared: eol=%s)\n", path, w, want
    }')"
  if [[ -n "$eol_mismatch" ]]; then
    echo "These files do not have the line endings .gitattributes declares for them." >&2
    echo "Fix with: git add --renormalize . && git checkout-index -f -a" >&2
    echo "$eol_mismatch" >&2
    exit 1
  fi
  echo "line endings: match .gitattributes"

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

  # `go build ./...` above only proves the packages compile for the HOST. This
  # exercises the real release path: the fail-fast cross-build recipes and the
  # dist artifact gate (issue #65). It lives here, in the script, rather than
  # inline in .github/workflows/ci.yml, so that a green `make verify` locally
  # still predicts a green CI run — see .claude/rules/90-letter-to-future-
  # sessions.md ("CI and the local scripts diverge ... keep the workflow thin;
  # logic lives in the scripts"). It builds into a temporary DIST_DIR, so it
  # never rewrites the tracked dist/ tree.
  step "release build recipes (cross-build + artifact gate)"
  bash "$ROOT/scripts/test_build_recipes.sh"

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
