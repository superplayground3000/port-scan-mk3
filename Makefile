.PHONY: build build-linux build-windows build-all clean test lint \
        fmt fmt-check vet cover verify verify-e2e e2e help

VERSION := $(shell git describe --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

GOCMD ?= go
GOARCH_AMD64 := amd64
GOOS_LINUX := linux
GOOS_WINDOWS := windows

DIST_DIR := dist

# Discover all commands in cmd/
CMDS := $(patsubst cmd/%/main.go,%,$(wildcard cmd/*/main.go))

# Default target
build: build-all

## build: Build all targets (linux and windows x64)
build-all: build-linux build-windows

## build-linux: Build Linux x64 binaries for all commands
build-linux:
	@echo "Building Linux x64..."
	@mkdir -p $(DIST_DIR)/linux
	@for cmd in $(CMDS); do \
		echo "  Building $$cmd..."; \
		$(GOCMD) build $(LDFLAGS) -o $(DIST_DIR)/linux/$$cmd ./cmd/$$cmd; \
	done

## build-windows: Build Windows x64 binaries for all commands
build-windows:
	@echo "Building Windows x64..."
	@mkdir -p $(DIST_DIR)/windows
	@for cmd in $(CMDS); do \
		echo "  Building $$cmd..."; \
		GOOS=$(GOOS_WINDOWS) GOARCH=$(GOARCH_AMD64) $(GOCMD) build $(LDFLAGS) -o $(DIST_DIR)/windows/$$cmd.exe ./cmd/$$cmd; \
	done

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
