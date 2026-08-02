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

# Fingerprint of the default `dist/` tree, taken before any test runs.
#
# Every test builds into its own temporary DIST_DIR and must leave the default
# `dist/` alone. This is deliberately a content fingerprint taken from the
# FILESYSTEM rather than `git status -- dist`: dist/ is gitignored (issue #65
# untracked the prebuilt binaries), so a git-based check would report "clean"
# unconditionally and the assertion would silently stop binding — the same
# vacuous-pass failure mode TEST 4 exists to prevent.
dist_fingerprint() {
  if [ -d dist ]; then
    find dist -type f -exec sha256sum {} + 2>/dev/null | sort
  else
    echo "<no dist/ directory>"
  fi
}
DIST_BEFORE="$WORK/dist-fingerprint-before"
dist_fingerprint > "$DIST_BEFORE"

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

# And the default dist/ tree must be untouched by any of the above: these tests
# build only into temporary directories. Compared by content fingerprint, so
# this keeps binding now that dist/ is gitignored (see dist_fingerprint above).
dist_fingerprint > "$WORK/dist-fingerprint-after"
if ! diff -q "$DIST_BEFORE" "$WORK/dist-fingerprint-after" >/dev/null 2>&1; then
  fail "the recipe tests created or modified the default dist/ tree; they must" \
       "build only into a temporary DIST_DIR. Difference:" \
       "$(diff "$DIST_BEFORE" "$WORK/dist-fingerprint-after" | head -5 | tr '\n' ' ')"
else
  pass "the default dist/ tree was not created or modified"
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

# ===========================================================================
# TEST 5 — release artifacts are byte-for-byte reproducible.
#
# Defect pinned: the recipes stamped `-X main.buildTime=$(BUILD_TIME)` from the
# WALL CLOCK (`date -u`), so two builds of the same commit produced different
# binaries, and `-trimpath` was absent so the absolute path of the build
# directory was baked into every binary. Either defect makes the artifacts
# unverifiable: you cannot confirm a published binary was built from the commit
# it claims. Issue #65 is titled "deterministic artifacts", so this is the
# invariant that gives the title meaning.
#
# The two defects have INDEPENDENT inputs, so a single build pair cannot expose
# both — this test varies them separately:
#   5a varies WALL-CLOCK TIME and TIMEZONE while building in the same directory
#      (the buildTime axis): the >=1s gap makes `date -u '+...%SZ'` produce a
#      different stamp, and the differing TZ makes a naive
#      `git log --date=format-local` (no TZ normalization) produce a different
#      stamp for the SAME commit.
#   5b varies the BUILD DIRECTORY while holding buildTime and VERSION constant
#      (the -trimpath axis): without `-trimpath` the absolute source path is
#      baked in, so the same commit built under two different paths diverges.
#      5a alone cannot catch a removed `-trimpath` — it builds both times in
#      $ROOT, so the SAME path leaks into both and they still match (this is
#      exactly the gap Codex flagged reviewing PR #73).
#
# Note the guarantee is per clean checkout of a commit: VERSION comes from
# `git describe --always --dirty`, so a dirty tree intentionally stamps
# differently, and tag metadata that differs between clones changes VERSION too
# (see docs/MAINTENANCE.md). 5a holds the tree state constant; 5b pins VERSION
# and buildTime so the build directory is its only variable.
# ===========================================================================
echo "TEST 5: release artifacts are byte-for-byte reproducible"

digest() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

# Compare every built artifact between two DIST_DIRs. $1 label, $2 dir a, $3 dir
# b. Records a failure per divergent artifact and returns non-zero if any
# diverged, so callers can gate a single pass line on it.
compare_dists() {
  local label="$1" dir_a="$2" dir_b="$3" before="$failures"
  local spec subdir suffix cmd a b da db
  for spec in "linux|" "windows|.exe"; do
    IFS='|' read -r subdir suffix <<<"$spec"
    for cmd in "${CMDS[@]}"; do
      a="$dir_a/$subdir/$cmd$suffix"
      b="$dir_b/$subdir/$cmd$suffix"
      if [ ! -f "$a" ] || [ ! -f "$b" ]; then
        fail "$label: $subdir/$cmd$suffix missing from one of the builds"
        continue
      fi
      da="$(digest "$a")"
      db="$(digest "$b")"
      if [ "$da" != "$db" ]; then
        fail "$label: $subdir/$cmd$suffix is NOT reproducible: $da vs $db"
      fi
    done
  done
  [ "$failures" -eq "$before" ]
}

# ---- 5a — reproducible across a wall-clock gap and a timezone change --------
# buildTime is derived naturally here (not pinned): that is exactly what a
# reintroduced `date -u`, or a `git --date=format-local` with no TZ
# normalization, would corrupt.
repro_a="$WORK/dist-repro-a"
repro_b="$WORK/dist-repro-b"
rc=0
TZ=UTC make build DIST_DIR="$repro_a" > "$WORK/out-repro-a" 2>&1 || rc=$?
# Cross a wall-clock second boundary; see the header note.
sleep 1.1
TZ=Asia/Tokyo make build DIST_DIR="$repro_b" > "$WORK/out-repro-b" 2>&1 || rc=$?

if [ "$rc" -ne 0 ]; then
  fail "a 5a reproducibility build failed (exit $rc); see $WORK/out-repro-*"
  cat "$WORK/out-repro-a" "$WORK/out-repro-b" >&2 || true
elif compare_dists "5a (time/timezone)" "$repro_a" "$repro_b"; then
  pass "5a: all $(( ${#CMDS[@]} * 2 )) artifacts are byte-identical across a" \
       "wall-clock gap and a timezone change"
fi

# ---- 5b — reproducible from a different absolute build path -----------------
# The -trimpath axis. Both builds run from a COPY of the working tree (via
# `git ls-files` through tar) placed at a DIFFERENT absolute path, with an
# identical pinned buildTime and VERSION, so the build directory is the only
# variable. With `-trimpath` the source path is stripped and the two match;
# without it, the two copies' paths leak into the binaries and they diverge.
#
# NEITHER build runs in $ROOT, and both copies are placed OUTSIDE the repo (under
# $WORK) so they are not git checkouts: `go build` then stamps no VCS revision
# into either. That symmetry is load-bearing — building one artifact in $ROOT (a
# real checkout, so VCS-stamped by some Go toolchains) against a .git-less copy
# would diverge on the VCS stamp ALONE, independent of -trimpath, which is a
# false failure that CI's Go toolchain actually produced. Copying the WORKING
# TREE (not HEAD) keeps the recipe under test as currently edited and excludes
# untracked build output.
repro_pin_v="repro-test-pinned-version"
repro_pin_t="2026-01-01T00:00:00Z"
repro_root_a="$WORK/repro-path-a/checkout"
repro_root_b="$WORK/repro-path-b-longer-name/checkout"
mkdir -p "$repro_root_a" "$repro_root_b"
repro_copy_ok=1
for dst in "$repro_root_a" "$repro_root_b"; do
  git ls-files -z | tar --null -T - -cf - | tar -xf - -C "$dst" || repro_copy_ok=0
done
if [ "$repro_copy_ok" -ne 1 ]; then
  fail "5b: could not copy the working tree to alternate paths for the" \
       "-trimpath check"
else
  rc=0
  ( cd "$repro_root_a" && make build DIST_DIR="$repro_root_a/dist" \
      VERSION="$repro_pin_v" BUILD_TIME="$repro_pin_t" ) \
    > "$WORK/out-repro-path-a" 2>&1 || rc=$?
  ( cd "$repro_root_b" && make build DIST_DIR="$repro_root_b/dist" \
      VERSION="$repro_pin_v" BUILD_TIME="$repro_pin_t" ) \
    > "$WORK/out-repro-path-b" 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then
    fail "a 5b reproducibility build failed (exit $rc); see" \
         "$WORK/out-repro-path-*"
    cat "$WORK/out-repro-path-a" "$WORK/out-repro-path-b" >&2 || true
  elif compare_dists "5b (build path / -trimpath)" "$repro_root_a/dist" \
       "$repro_root_b/dist"; then
    pass "5b: all $(( ${#CMDS[@]} * 2 )) artifacts are byte-identical when the" \
         "same commit is built from two different absolute paths"
  fi
fi

echo
if [ "$failures" -ne 0 ]; then
  echo "build recipe regression tests FAILED: $failures problem(s)." >&2
  exit 1
fi
echo "build recipe regression tests passed."
