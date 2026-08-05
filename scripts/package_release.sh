#!/usr/bin/env bash
#
# package_release.sh — turn built artifacts into a publishable release archive.
#
# What it produces, under --out:
#   port-scan-mk3_<version>_<target>_amd64.zip
#       every command's binary, flat at the archive root, plus an inner
#       SHA256SUMS.txt so an operator can verify individual binaries AFTER
#       extracting them.
#   SHA256SUMS.txt
#       the checksum of the archive(s) sitting next to it, with relative paths,
#       so `sha256sum --check SHA256SUMS.txt` works in the download directory.
#
# It does NOT publish anything and it does not run the artifacts: a Windows EXE
# cannot be executed on the Linux packaging runner. Executing them is
# scripts/smoke_release.sh, which the release workflow runs on a Windows runner
# before anything is published.
#
# Reproducibility (issues #65/#73): the binaries are already byte-identical for
# a given commit, and this script keeps the ARCHIVE identical too, by stamping
# every entry with the commit's timestamp instead of the wall clock, sorting the
# entries, and asking zip not to store platform extra fields. Two packaging runs
# of one commit therefore produce the same archive checksum, which is what makes
# a published checksum worth anything.
#
# Usage:
#   bash scripts/package_release.sh [options]
#
# Options:
#   --dist DIR         dist root holding <target>/ artifacts (default: dist)
#   --out DIR          where to write the archive and manifest
#                      (default: <dist>/release)
#   --target OS        which built target to package: windows | linux
#                      (default: windows)
#   --version V        version string used in the archive name
#                      (default: git describe --always --dirty, else "dev")
#   --build-time T     archive entry timestamp, UTC RFC3339
#                      (default: the commit timestamp, as in the Makefile)
#   --skip-build       package what is already in <dist>; do not run `make build`
#
# Exit 0 only when a complete archive and its manifest exist.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DIST_DIR="dist"
OUT_DIR=""
TARGET="windows"
VERSION=""
BUILD_TIME=""
SKIP_BUILD=0

die() {
  echo "package_release.sh: $*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dist) DIST_DIR="${2:-}"; shift 2 ;;
    --out) OUT_DIR="${2:-}"; shift 2 ;;
    --target) TARGET="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --build-time) BUILD_TIME="${2:-}"; shift 2 ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help) sed -n '2,40p' "$0"; exit 0 ;;
    *) die "unknown argument '$1' (try --help)" ;;
  esac
done

case "$TARGET" in
  windows) SUFFIX=".exe" ;;
  linux) SUFFIX="" ;;
  *) die "unsupported --target '$TARGET' (windows or linux). Windows ARM64 is deliberately out of scope (issue #65)." ;;
esac

[ -n "$DIST_DIR" ] || die "--dist requires a directory"
OUT_DIR="${OUT_DIR:-$DIST_DIR/release}"

# Same derivation as the Makefile, so an archive name matches what the binaries
# inside it report. Kept in sync deliberately; tests/release asserts the
# binaries' stamps, and the workflow passes --version explicitly from the tag.
if [ -z "$VERSION" ]; then
  VERSION="$(git describe --always --dirty 2>/dev/null || echo "dev")"
fi
if [ -z "$BUILD_TIME" ]; then
  BUILD_TIME="$(TZ=UTC0 git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>/dev/null \
    || date -u '+%Y-%m-%dT%H:%M:%SZ')"
fi

# Discover the commands from the source tree, exactly as verify_dist.sh does, so
# a command that the build forgot is a packaging failure rather than a silently
# thinner archive.
CMDS=()
for main_file in cmd/*/main.go; do
  [ -f "$main_file" ] || continue
  CMDS+=("$(basename "$(dirname "$main_file")")")
done
[ "${#CMDS[@]}" -gt 0 ] || die "found no cmd/*/main.go — refusing to build a vacuous archive"

if [ "$SKIP_BUILD" -eq 0 ]; then
  echo "Building release artifacts into '$DIST_DIR' ..."
  # `make build` builds every target and then runs scripts/verify_dist.sh, which
  # is the gate that proves each artifact really is the OS/ARCH its directory
  # claims and was built with CGO_ENABLED=0. This script deliberately does not
  # re-implement that check.
  make build DIST_DIR="$DIST_DIR"
fi

SRC_DIR="$DIST_DIR/$TARGET"
[ -d "$SRC_DIR" ] || die "'$SRC_DIR' does not exist; run without --skip-build, or point --dist at a built tree"

missing=()
for cmd in "${CMDS[@]}"; do
  [ -s "$SRC_DIR/$cmd$SUFFIX" ] || missing+=("$cmd$SUFFIX")
done
if [ "${#missing[@]}" -ne 0 ]; then
  die "refusing to package an incomplete release: $SRC_DIR is missing ${missing[*]}"
fi

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

ARCHIVE_NAME="port-scan-mk3_${VERSION}_${TARGET}_amd64.zip"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$OUT_DIR"
OUT_ABS="$(cd "$OUT_DIR" && pwd)"

# Stage the payload flat: operators drop these straight into a directory on PATH.
for cmd in "${CMDS[@]}"; do
  cp "$SRC_DIR/$cmd$SUFFIX" "$STAGE/$cmd$SUFFIX"
  chmod 0755 "$STAGE/$cmd$SUFFIX"
done

# Inner manifest: checksums of the extracted binaries, relative to the archive
# root, so `sha256sum --check SHA256SUMS.txt` works after unzipping.
(
  cd "$STAGE"
  : > SHA256SUMS.txt
  for cmd in $(printf '%s\n' "${CMDS[@]}" | LC_ALL=C sort); do
    printf '%s  %s\n' "$(sha256_of "$cmd$SUFFIX")" "$cmd$SUFFIX" >> SHA256SUMS.txt
  done
)

# Normalize every entry's timestamp so the archive depends only on its contents.
# `touch -d` accepts the RFC3339 stamp on both GNU and BSD coreutils.
find "$STAGE" -type f -exec touch -d "$BUILD_TIME" {} +

rm -f "$OUT_ABS/$ARCHIVE_NAME"
(
  cd "$STAGE"
  # -X drops uid/gid and platform extra fields; the sorted file list fixes entry
  # order. Both are required for a byte-identical archive.
  # shellcheck disable=SC2046
  zip -q -X -9 "$OUT_ABS/$ARCHIVE_NAME" $(find . -type f | sed 's|^\./||' | LC_ALL=C sort)
)

# Outer manifest: the archive itself, relative path only, so it verifies in
# whatever directory the user downloads it to.
(
  cd "$OUT_ABS"
  printf '%s  %s\n' "$(sha256_of "$ARCHIVE_NAME")" "$ARCHIVE_NAME" > SHA256SUMS.txt
)

echo "Packaged $ARCHIVE_NAME (${#CMDS[@]} commands, $TARGET/amd64, version $VERSION)"
echo "  archive:  $OUT_ABS/$ARCHIVE_NAME"
echo "  checksum: $(cat "$OUT_ABS/SHA256SUMS.txt")"
