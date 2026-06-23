#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Exclude e2e and internal/testkit packages from coverage gate
# These are integration/e2e test utilities with inherently lower coverage
EXCLUDE_PATTERN="e2e|internal/testkit"

PACKAGES=$(go list ./... | grep -v -E "$EXCLUDE_PATTERN")
go test $PACKAGES -coverprofile=coverage.out
COVER=$(go tool cover -func=coverage.out | awk '/total:/ {print substr($3, 1, length($3)-1)}')
awk -v c="$COVER" 'BEGIN { if (c+0 < 85) { exit 1 } }'
echo "coverage gate passed: ${COVER}%"
