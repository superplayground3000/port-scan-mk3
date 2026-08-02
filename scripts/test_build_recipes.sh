#!/usr/bin/env bash
#
# test_build_recipes.sh — retained regression tests for the release-build
# recipes in the Makefile (issue #65).
#
# WHY THIS EXISTS
# ---------------
# The #65 fixes live in Makefile *recipes*, not in Go code, so `go test` cannot
# reach them and a narrative "I reproduced it once" paste in a PR body protects
# nothing: the next edit to those recipes can silently reintroduce either
# defect. These are the automated, retained checks that keep them fixed. Each
# test states the defect it pins and is written so that it FAILS against the
# pre-#65 recipes (see docs/MAINTENANCE.md section 2, "Release artifact rules").
#
# The tests drive the REAL Makefile targets — they never reconstruct a private
# copy of the recipe, because a copy would keep passing after someone edits the
# real one.
#
# Nothing here writes to the tracked `dist/` tree: every test builds into its
# own temporary DIST_DIR, so running the suite never dirties the working tree.
#
# Usage:
#   bash scripts/test_build_recipes.sh
#
# Exit 0 means every recipe invariant holds. Any non-zero exit names the
# invariant that broke and the defect it would let back in.
#
# Windows: run from Git Bash or WSL (same requirement as scripts/verify.sh).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

failures=0
pass() { echo "  PASS: $*"; }
fail() { echo "  FAIL: $*" >&2; failures=$((failures + 1)); }

# The command list, discovered exactly the way the Makefile's $(CMDS) does.
CMDS=()
for main_file in cmd/*/main.go; do
  [ -f "$main_file" ] || continue
  CMDS+=("$(basename "$(dirname "$main_file")")")
done
if [ "${#CMDS[@]}" -lt 2 ]; then
  echo "test_build_recipes.sh: need >=2 commands in cmd/*/main.go to test" \
       "fail-fast ordering; found ${#CMDS[@]}" >&2
  exit 1
fi
TOTAL_CMDS="${#CMDS[@]}"

# ---------------------------------------------------------------------------
# A fake `go` that the Makefile will call via its overridable GOCMD.
#
# It records every command it was asked to build, one per line, and fails for
# exactly one of them. That lets a test observe not just "did make fail" but
# "did make STOP" — which is the actual fail-fast property. A real `go` cannot
# be used here: we need a deterministic mid-loop compile failure without
# corrupting any source file.
# ---------------------------------------------------------------------------
FAKE_GO="$WORK/fake-go"
cat > "$FAKE_GO" <<'FAKEGO'
#!/usr/bin/env bash
set -uo pipefail
out=""; pkg=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="${2:-}"; shift 2 ;;
    ./cmd/*) pkg="$1"; shift ;;
    *) shift ;;
  esac
done
name="${pkg##*/}"
printf '%s\n' "$name" >> "$FAKE_GO_LOG"
if [ "$name" = "$FAKE_GO_FAIL_CMD" ]; then
  echo "fake go: simulated compile failure in ./cmd/$name" >&2
  exit 1
fi
[ -n "$out" ] || exit 0
mkdir -p "$(dirname "$out")"
printf 'fake artifact\n' > "$out"
FAKEGO
chmod +x "$FAKE_GO"

header_magic() { od -An -N4 -tx1 < "$1" | tr -d ' \n'; }

# ===========================================================================
# TEST 1 — the build loops are fail-fast.
#
# Defect pinned (#65): a shell `for` loop's exit status is the status of its
# LAST iteration. Without `set -e` a command that fails to compile in the
# middle of the loop is masked by a later success, so `make build-linux` exits
# 0 with an artifact missing from dist/.
#
# Discriminating because it asserts make STOPPED at the failure. A recipe that
# merely propagated the last iteration's status would still build every later
# command, and the invocation log would hold all $TOTAL_CMDS entries.
# ===========================================================================
echo "TEST 1: build recipes are fail-fast (stop at the first failing command)"
FAIL_CMD="${CMDS[1]}"   # 2nd of >=2, so at least one command must NOT be built
EXPECTED_INVOCATIONS=2  # the 1st command, then the failing 2nd

for target in build-linux build-windows; do
  log="$WORK/log-$target"
  : > "$log"
  rc=0
  FAKE_GO_LOG="$log" FAKE_GO_FAIL_CMD="$FAIL_CMD" \
    make "$target" GOCMD="$FAKE_GO" DIST_DIR="$WORK/dist-$target" \
    > "$WORK/out-$target" 2>&1 || rc=$?

  if [ "$rc" -eq 0 ]; then
    fail "make $target exited 0 even though ./cmd/$FAIL_CMD failed to build" \
         "(not fail-fast: this is the #65 defect)"
  else
    pass "make $target exited non-zero ($rc) when ./cmd/$FAIL_CMD failed"
  fi

  invoked="$(wc -l < "$log" | tr -d ' ')"
  if [ "$invoked" -ne "$EXPECTED_INVOCATIONS" ]; then
    fail "make $target invoked the compiler $invoked time(s), want" \
         "$EXPECTED_INVOCATIONS: it did not STOP at ./cmd/$FAIL_CMD" \
         "(of $TOTAL_CMDS commands). Invocations: $(tr '\n' ' ' < "$log")"
  else
    pass "make $target stopped at ./cmd/$FAIL_CMD ($invoked of $TOTAL_CMDS built)"
  fi

  last_invoked="$(tail -n 1 "$log")"
  if [ "$last_invoked" != "$FAIL_CMD" ]; then
    fail "make $target continued past the failure: last compiler invocation" \
         "was '$last_invoked', want '$FAIL_CMD'"
  fi
done

# ===========================================================================
# TEST 2 — the cross-builds never inherit GOOS/GOARCH from the build host.
#
# Defect pinned (#65): `build-linux` did not set GOOS/GOARCH, so on a Windows
# host it wrote Windows PE binaries into dist/linux. Exporting GOOS/GOARCH into
# make's environment is exactly how a foreign host leaks into the recipe, so
# this reproduces the real mechanism rather than a proxy for it.
#
# Discriminating because the assertion is on the on-disk executable header of
# the produced binary: against the pre-#65 recipe these are PE ("MZ", 4d5a),
# not ELF.
# ===========================================================================
echo "TEST 2: cross-builds pin GOOS/GOARCH and ignore the host's environment"

hostile_dist="$WORK/dist-hostile"
before="$failures"
if env GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
     make build-linux DIST_DIR="$hostile_dist" > "$WORK/out-hostile-linux" 2>&1; then
  for cmd in "${CMDS[@]}"; do
    artifact="$hostile_dist/linux/$cmd"
    if [ ! -f "$artifact" ]; then
      fail "$artifact was not produced"
      continue
    fi
    magic="$(header_magic "$artifact")"
    if [ "$magic" != "7f454c46" ]; then
      fail "$artifact has header '$magic', want ELF (7f454c46): the host's" \
           "GOOS=windows leaked into build-linux (the #65 defect)"
    fi
  done
  if [ "$failures" -eq "$before" ]; then
    pass "build-linux produced ELF binaries with GOOS=windows in the environment"
  fi
else
  fail "make build-linux failed under a hostile host environment; see $WORK/out-hostile-linux"
  cat "$WORK/out-hostile-linux" >&2 || true
fi

# The mirror image: a Linux host must not leak into build-windows.
before="$failures"
if env GOOS=linux GOARCH=amd64 CGO_ENABLED=1 \
     make build-windows DIST_DIR="$hostile_dist" > "$WORK/out-hostile-win" 2>&1; then
  for cmd in "${CMDS[@]}"; do
    artifact="$hostile_dist/windows/$cmd.exe"
    if [ ! -f "$artifact" ]; then
      fail "$artifact was not produced"
      continue
    fi
    magic="$(header_magic "$artifact")"
    if [ "${magic:0:4}" != "4d5a" ]; then
      fail "$artifact has header '$magic', want PE 'MZ' (4d5a): the host's" \
           "GOOS=linux leaked into build-windows"
    fi
  done
  if [ "$failures" -eq "$before" ]; then
    pass "build-windows produced PE binaries with GOOS=linux in the environment"
  fi
else
  fail "make build-windows failed under a hostile host environment; see $WORK/out-hostile-win"
  cat "$WORK/out-hostile-win" >&2 || true
fi

# ===========================================================================
# TEST 3 — `make build` runs the artifact gate over the directory it actually
# built into.
#
# Defect pinned: build-all invoked `scripts/verify_dist.sh` with no argument,
# so it always inspected the default `dist/`. With DIST_DIR overridden that
# means `make build` verified a directory it had not written — passing on stale
# artifacts, or failing on artifacts it never produced. A gate that inspects
# the wrong tree is worse than no gate, because it reports on something else.
# ===========================================================================
echo "TEST 3: 'make build' gates the DIST_DIR it built into"

gated_dist="$WORK/dist-gated"
rc=0
make build DIST_DIR="$gated_dist" > "$WORK/out-gated" 2>&1 || rc=$?

if [ "$rc" -ne 0 ]; then
  fail "make build DIST_DIR=$gated_dist exited $rc; see below"
  cat "$WORK/out-gated" >&2 || true
else
  pass "make build DIST_DIR=<tmp> exited 0"
fi

if grep -qF "Verifying release artifacts in '$gated_dist'" "$WORK/out-gated"; then
  pass "the artifact gate inspected the overridden DIST_DIR"
else
  fail "the artifact gate did not inspect '$gated_dist'. It reported:" \
       "$(grep -F 'Verifying release artifacts' "$WORK/out-gated" || echo '<no gate output at all>')"
fi

# And the tracked dist/ must be untouched by any of the above.
if [ -n "$(git status --porcelain -- dist 2>/dev/null)" ]; then
  fail "the recipe tests modified the tracked dist/ tree; they must build only" \
       "into a temporary DIST_DIR"
else
  pass "the tracked dist/ tree was not modified"
fi

# ===========================================================================
# TEST 4 — the artifact gate refuses to pass vacuously.
#
# Defect pinned: scripts/verify_dist.sh decided success on `failures -eq 0`
# alone. If its TARGETS list is ever emptied, the inspection loop never runs,
# no failure is recorded, and the gate reports success having checked NOTHING.
# It already guarded the CMDS half of that ("refusing to pass a vacuous
# check"); `checked` closes the other half.
#
# This is a mutation test: the only way to empty TARGETS is to edit the script,
# so the check runs against a COPY with TARGETS blanked out. The copy is
# derived from the real script on every run, so it cannot rot away from it —
# if the guard is deleted, this test goes red.
# ===========================================================================
echo "TEST 4: the artifact gate refuses to pass when it inspected nothing"

# The mutant must run inside a directory that looks like a repo root, because
# verify_dist.sh derives ROOT from its own location and already refuses to pass
# when it finds no cmd/*/main.go. Running the mutant from a scratch directory
# would trip THAT guard instead and the test would pass for the wrong reason —
# it would stay green even with the `checked` guard deleted. So: a minimal fake
# root with one discoverable command, which leaves the empty TARGETS list as
# the only thing that can make `checked` zero.
fakeroot="$WORK/fakeroot"
mkdir -p "$fakeroot/scripts" "$fakeroot/cmd/port-scan"
: > "$fakeroot/cmd/port-scan/main.go"
mutant="$fakeroot/scripts/verify_dist_no_targets.sh"

# Replace the TARGETS array literal with an empty one.
awk '
  /^TARGETS=\(/ { print "TARGETS=()"; skipping = 1; next }
  skipping && /^\)/ { skipping = 0; next }
  skipping { next }
  { print }
' scripts/verify_dist.sh > "$mutant"

if ! grep -q '^TARGETS=()$' "$mutant"; then
  fail "could not build the empty-TARGETS mutant — the TARGETS literal in" \
       "scripts/verify_dist.sh no longer matches this test's expectation"
else
  rc=0
  bash "$mutant" "$fakeroot/dist" > "$WORK/out-mutant" 2>&1 || rc=$?
  if [ "$rc" -eq 0 ]; then
    fail "verify_dist.sh with an empty TARGETS list exited 0 — it passes" \
         "vacuously after inspecting 0 artifacts. Output:" \
         "$(cat "$WORK/out-mutant")"
  elif grep -q 'found no cmd/\*/main.go' "$WORK/out-mutant"; then
    fail "the empty-TARGETS mutant tripped the CMDS guard instead of the" \
         "'inspected 0 artifacts' guard — this test is not discriminating." \
         "Output: $(cat "$WORK/out-mutant")"
  else
    pass "verify_dist.sh with an empty TARGETS list exited non-zero ($rc)"
  fi
fi

echo
if [ "$failures" -ne 0 ]; then
  echo "build recipe regression tests FAILED: $failures problem(s)." >&2
  exit 1
fi
echo "build recipe regression tests passed."
