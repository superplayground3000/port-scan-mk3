.PHONY: build build-linux build-windows build-all verify-dist clean test lint \
        fmt fmt-check vet cover verify verify-e2e e2e help

VERSION := $(shell git describe --always --dirty 2>/dev/null || echo "dev")
# The full commit, stamped SEPARATELY from VERSION. A build made exactly on a
# tag describes as just "v2.2.0" and would otherwise carry no commit at all,
# which is the one thing a release asset must be traceable to. Derived from the
# commit, so it is constant for a given checkout and does not disturb the
# byte-for-byte reproducibility guarantee below (issue #65/#73). Falls back
# outside a git checkout, where there is no commit to name.
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
# Derived from the COMMIT, not the wall clock, and normalized to UTC. Two
# builds of the same commit must produce byte-identical artifacts (issue #65 —
# "deterministic artifacts"); a `date -u` stamp broke that on every rebuild, and
# an un-normalized local timestamp would break it between two builders in
# different timezones. Falls back to the wall clock outside a git checkout
# (e.g. a source tarball), where there is no commit to derive from.
BUILD_TIME := $(shell TZ=UTC0 git log -1 --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd 2>/dev/null || date -u '+%Y-%m-%dT%H:%M:%SZ')
# -trimpath strips absolute source paths from the binary, so the artifact does
# not depend on WHERE it was built. Required for reproducibility across
# machines and CI runners.
BUILDFLAGS := -trimpath
# `-X main.<var>` can only write to a variable declared in the linked binary's
# own main package, and the linker says NOTHING when the target does not exist.
# Every command therefore declares version/buildTime/commit in package main;
# tests/release rebuilds them with these exact flags and asserts the values come
# back out, so a rename here fails a test rather than silently shipping "dev".
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.commit=$(COMMIT)"

GOCMD ?= go
GOARCH_AMD64 := amd64
GOOS_LINUX := linux
GOOS_WINDOWS := windows

# Release artifacts are built with cgo disabled. Rationale (docs/MAINTENANCE.md
# section 2): with CGO_ENABLED=1 the artifact depends on whether the *build
# host* happens to have a C toolchain, and the Linux binary links dynamically
# against the host glibc — the same source produces materially different
# artifacts on different machines. Pinning it to 0 makes `make build` produce
# the same kind of binary everywhere, gives a statically linked Linux binary,
# and selects the pure-Go (netgo) resolver, which is sufficient because the
# scanner uses only stdlib `net` (constitution "Technology Stack").
CGO_ENABLED_RELEASE := 0

DIST_DIR := dist

# Discover all commands in cmd/
CMDS := $(patsubst cmd/%/main.go,%,$(wildcard cmd/*/main.go))

# Default target
build: build-all

## build: Build all targets (linux and windows x64) and verify the artifacts
build-all: build-linux build-windows
	@bash scripts/verify_dist.sh $(DIST_DIR)

# Every cross-build below sets GOOS/GOARCH/CGO_ENABLED explicitly so the output
# directory always matches the binary's real target, whatever the build host is
# (issue #65: `build-linux` used to inherit the host's GOOS and dropped Windows
# PE binaries into dist/linux when run on Windows).
#
# `set -e` is what makes the loop fail-fast: without it the recipe's exit status
# is the status of the LAST iteration, so a failed build in the middle was
# masked by a later success and the target exited 0 with an artifact missing.

## build-linux: Build Linux x64 binaries for all commands
build-linux:
	@echo "Building Linux x64..."
	@mkdir -p $(DIST_DIR)/linux
	@set -e; for cmd in $(CMDS); do \
		echo "  Building $$cmd..."; \
		CGO_ENABLED=$(CGO_ENABLED_RELEASE) GOOS=$(GOOS_LINUX) GOARCH=$(GOARCH_AMD64) \
			$(GOCMD) build $(BUILDFLAGS) $(LDFLAGS) -o $(DIST_DIR)/linux/$$cmd ./cmd/$$cmd; \
	done

## build-windows: Build Windows x64 binaries for all commands
build-windows:
	@echo "Building Windows x64..."
	@mkdir -p $(DIST_DIR)/windows
	@set -e; for cmd in $(CMDS); do \
		echo "  Building $$cmd..."; \
		CGO_ENABLED=$(CGO_ENABLED_RELEASE) GOOS=$(GOOS_WINDOWS) GOARCH=$(GOARCH_AMD64) \
			$(GOCMD) build $(BUILDFLAGS) $(LDFLAGS) -o $(DIST_DIR)/windows/$$cmd.exe ./cmd/$$cmd; \
	done

## verify-dist: Check every dist/ artifact exists and targets the right OS/ARCH
# Always gate the directory this Makefile actually builds into: passing
# $(DIST_DIR) explicitly keeps `make build DIST_DIR=x` from verifying `dist/`,
# i.e. reporting on a tree it never wrote.
verify-dist:
	bash scripts/verify_dist.sh $(DIST_DIR)

## clean: Remove build artifacts
clean:
	rm -rf $(DIST_DIR)

## test: Run tests (race detector + shuffled order)
test:
	$(GOCMD) test -race -shuffle=on ./...

## fmt: Format all Go files in place
fmt:
	gofmt -w $(shell git ls-files '*.go')

## fmt-check: Fail if any Go file is not gofmt-clean
fmt-check:
	@files="$$(git ls-files '*.go')"; \
	if [ -z "$$files" ]; then echo "gofmt: no Go files"; exit 0; fi; \
	unformatted="$$(gofmt -l -- $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Not gofmt-clean (run 'make fmt'):"; echo "$$unformatted"; exit 1; \
	fi; \
	echo "gofmt: clean"

## vet: Run go vet static analysis
vet:
	$(GOCMD) vet ./...

## lint: Run golangci-lint if installed, else fall back to go vet
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; falling back to 'go vet'"; \
		$(GOCMD) vet ./...; \
	fi

## cover: Run the coverage gate (>= 85% total)
cover:
	bash scripts/coverage_gate.sh

## verify: Run the full local quality gate (fmt, vet, build, test, coverage)
verify:
	bash scripts/verify.sh

## verify-e2e: Run the full quality gate plus the isolated Docker e2e suite
verify-e2e:
	bash scripts/verify.sh --e2e

## e2e: Run only the isolated Docker e2e suite
e2e:
	bash e2e/run_e2e.sh

## help: List available make targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
