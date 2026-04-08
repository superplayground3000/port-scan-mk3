.PHONY: build build-linux build-windows build-all clean test lint

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

## test: Run tests
test:
	$(GOCMD) test -race -shuffle=on ./...

## lint: Run linter
lint:
	$(GOCMD) vet ./...
