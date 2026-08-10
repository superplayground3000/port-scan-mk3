# Makefile Update: Build All Commands

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update Makefile to compile all Go binaries in `cmd/` and output them to the `dist/` folder.

**Architecture:** Use a loop-based approach to build all binaries discovered from `cmd/*/main.go`. Each binary gets its own subdirectory in `dist/` matching its command name.

**Tech Stack:** GNU Make, Go 1.24+

---

### Task 1: Update Makefile to build all commands

**Files:**
- Modify: `Makefile`

**Step 1: Write the updated Makefile**

Replace the existing Makefile with:

```makefile
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
CMDS := $(notdir $(wildcard cmd/*/main.go))
CMD_DIRS := $(addprefix cmd/,$(CMDS))

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
		$(GOCMD) build $(LDFLAGS) -o $(DIST_DIR)/linux/$$cmd cmd/$$cmd; \
	done

## build-windows: Build Windows x64 binaries for all commands
build-windows:
	@echo "Building Windows x64..."
	@mkdir -p $(DIST_DIR)/windows
	@for cmd in $(CMDS); do \
		echo "  Building $$cmd..."; \
		GOOS=$(GOOS_WINDOWS) GOARCH=$(GOARCH_AMD64) $(GOCMD) build $(LDFLAGS) -o $(DIST_DIR)/windows/$$cmd.exe cmd/$$cmd; \
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
```

**Step 2: Verify the Makefile syntax**

Run: `make -n build-linux`
Expected: Dry-run showing build commands for all 3 binaries (cidr-compare, csv-transform, port-scan)

**Step 3: Run the actual build**

Run: `make build`
Expected: Builds all binaries to `dist/linux/` and `dist/windows/`

**Step 4: Verify output**

Run: `ls -la dist/linux/ && ls -la dist/windows/`
Expected: All 3 binaries present in both directories

**Step 5: Commit**

```bash
git add Makefile
git commit -m "feat: update Makefile to build all commands from cmd/"
```
