# Port Scan MK3

Developer-first TCP port scanner CLI in Go with fail-fast input validation, pressure-aware pacing, resumable scanning, and e2e verification.

## Prerequisites

- Go `1.24.0` (toolchain `go1.24.4`)
- Docker + `docker compose` (required for e2e only)

## Quick Start

Validate input only (no network scan):

```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -format human
```

Run a real scan — `port-scan` is a **three-step pipeline** (`preping` ->
`generate-buckets` -> `scan`). `scan` requires a bucket snapshot via `-resume`;
it never pings and no longer accepts ping flags (this changed in 2.0.0 — see
[release notes](docs/release-notes/2.0.0.md)):

```bash
# 1. Ping unique targets; capture the printed unreachable CSV path (stdout)
UNREACHABLE=$(go run ./cmd/port-scan preping \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -output e2e/out/scan_results.csv)

# 2. Build the bucket snapshot (targets minus the unreachable blocklist)
go run ./cmd/port-scan generate-buckets \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -port-file e2e/inputs/ports.csv \
  -unreachable-file "$UNREACHABLE" \
  -buckets-out e2e/out/buckets.json

# 3. Scan the buckets
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv
```

Skip the pre-scan reachability check (the old `-disable-pre-scan-ping=true`):
omit the `preping` step and run `generate-buckets` without `-unreachable-file`.
The snapshot then covers all targets and still stamps
`pre_scan_ping.enabled=true`, so `scan` never pings:

```bash
go run ./cmd/port-scan generate-buckets \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -port-file e2e/inputs/ports.csv \
  -buckets-out e2e/out/buckets.json
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv -disable-api
```

## Pre-processing Workflows

Two tools prepare input files for `port-scan`. Both output a port-scan-ready rich CSV at
`<output-dir>/<fab_name>/<timestamp>/input.csv`.

### From-Scratch Flow

Use when scanning a data center for the first time or starting fresh.
`preprocess` filters a firewall-policy rich CSV, removing any target whose
`dst_network_segment` falls inside a closed CIDR.

```bash
# Step 1 — filter targets
go run ./cmd/preprocess \
  --input filtered-targets/dc-east/20260503T120000Z/opened_targets.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./scan-input

# Step 2 — run the pipeline on the filtered rich input (rich mode: no -port-file)
# Replace <timestamp> with the path printed by step 1.
IN=scan-input/dc-east/<timestamp>/input.csv
go run ./cmd/port-scan generate-buckets -cidr-file "$IN" -buckets-out out/buckets.json
go run ./cmd/port-scan scan -cidr-file "$IN" -resume out/buckets.json -output out/ -disable-api=true
```

### Re-scan Flow

Use when re-scanning previously discovered open targets from a `host,port` CSV.
`enrich-targets` promotes it to rich format; `preprocess` then applies the same
CIDR filter.

```bash
# Step 1 — enrich minimal CSV to rich format
go run ./cmd/enrich-targets \
  --input previous-scanned/dc-east/20260503T120000Z/opened_targets.csv \
  --cidr-list cidrs.csv \
  --service-map services.csv \
  --output enriched.csv

# Step 2 — filter enriched targets
go run ./cmd/preprocess \
  --input enriched.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./scan-input

# Step 3 — run the pipeline (rich mode: no -port-file)
# Replace <timestamp> with the path printed by step 2.
IN=scan-input/dc-east/<timestamp>/input.csv
go run ./cmd/port-scan generate-buckets -cidr-file "$IN" -buckets-out out/buckets.json
go run ./cmd/port-scan scan -cidr-file "$IN" -resume out/buckets.json -output out/ -disable-api=true
```

## Input Contracts

- CIDR CSV (default mode):
  - Required columns: `ip`, `ip_cidr`
  - Optional columns: `fab_name`, `cidr_name`
  - Column mapping flags are case-sensitive: `-cidr-ip-col`, `-cidr-ip-cidr-col`
- Rich CSV mode is auto-detected when all rich fields exist:
  - `src_ip`, `src_network_segment`, `dst_ip`, `dst_network_segment`
  - `service_label`, `protocol`, `port`, `decision`, `policy_id`, `reason`
- Port file format: one line per port in `<port>/tcp` (for example `443/tcp`)
  - Required in default CIDR mode
  - Optional in rich CSV mode

## Commands

- `preping`: ping unique target IPs, write `unreachable_results-<ts>.csv`, print its path
- `generate-buckets`: build the resume bucket snapshot from targets minus an optional blocklist (`-buckets-out` required)
- `scan`: pure TCP scan of a bucket snapshot (`-resume` required; dispatch, probe, output, in-place resume persistence)
- `validate`: parse and validate input files only

Exit code behavior:

- `0`: success
- `1`: validation failed (`validate`) or scan runtime error (`scan`)
- `2`: CLI parsing/config error
- `130`: scan canceled by `SIGINT` (`Ctrl+C`)

## Architecture and Data Flow

### port-scan Pipeline (three steps)

Each arrow between steps is a durable file, so the pipeline can stop and restart
at any boundary. `rich.csv` (`-cidr-file`) feeds all three steps.

```
        rich.csv
           │
           ▼
   ┌───────────────┐
   │    preping    │  ping unique IPs (progress → stderr)
   └───────┬───────┘
           │  unreachable_results-<ts>.csv   (path printed to stdout)
           ▼
 rich.csv + unreachable.csv (optional)
           │
           ▼
   ┌───────────────────┐
   │  generate-buckets │  subtract blocklist, group per CIDR,
   │   (parallel)      │  build chunks, stamp pre_scan_ping.enabled=true
   └─────────┬─────────┘
             │  bucket snapshot JSON  (== resume Snapshot; -buckets-out)
             ▼
   rich.csv + bucket snapshot
             │
             ▼
   ┌───────────────┐
   │     scan      │  pure TCP scan; NO checker; -resume <bucket> (required)
   │               │  rate control + pressure gate → scanner.ScanTCP()
   └───────┬───────┘
           │  scan_results-<ts>.csv / opened_results-<ts>.csv
           ▼   (on cancel/error: bucket snapshot updated in place at -resume)
```

### Pre-processing Pipeline

```
Mode 1 — From Scratch:

  Filtered targets CSV (rich) ──────────────────────────────> preprocess ──> output/<fab>/<ts>/input.csv
                                                                   │
                                                          cleaned_cidrs.csv
                                                       (drops closed-CIDR targets)

Mode 2 — Re-scan:

  Opened targets CSV (host,port) ──> enrich-targets ──> enriched rich CSV ──> preprocess ──> output/<fab>/<ts>/input.csv
                                           │                                       │
                                  cidrs.csv + services.csv               cleaned_cidrs.csv
                                  (fills 10 rich columns)             (drops closed-CIDR targets)
```

### cidr-compare Pipeline

```
Deny CSV ──> IntervalTree.Insert() ──┐
                                     ├──> IntervalTree.Query() ──> Matching pairs
Open CSV ──> OpenCSVReader ──────────┘         │
                                              v
                                     stdout: "deny_cidr,open_cidr\ndeny,open\n..."
```

### csv-transform Pipeline

```
Input CSV ──> spreadsheet.Reader ──> Column index ──> Filter rows ──> Host resolve ──> Port expand ──> Output CSV
              OpenSheet()            (host, port,   (Pass != FALSE)  (DNS lookup)      (ranges to    (Rich CSV
                                  pass columns)                                     individuals)   format)
```

## How the Pipeline Works

The pipeline is three separately-runnable steps with durable file hand-offs.
`rich.csv` (`-cidr-file`) is threaded into all three as the single source of
truth for target metadata.

**Step 1 — `preping`** (optional; skip it to skip reachability filtering):
1. Load the CIDR/rich CSV and collect unique IPv4 targets.
2. Ping each unique target (`-pre-scan-ping-timeout`, default `100ms`), emitting
   percentage progress to stderr every `-progress-interval` units.
3. Finalize `unreachable_results-YYYYMMDDTHHMMSSZ[-n].csv` and print its path to
   stdout for chaining.

**Step 2 — `generate-buckets`** (no network I/O):
1. Load targets and ports; parse the optional `-unreachable-file` blocklist.
2. Subtract the blocklist, group by CIDR, and build one chunk per group in
   parallel over `-workers` (deterministic, CIDR-sorted). The broadcast address
   of each row's boundary subnet (`ip_cidr` / `dst_network_segment`, prefix /30
   or larger) is never included — whether it came from CIDR expansion or an
   explicitly listed IP — while the network address is kept.
3. Write the resume bucket snapshot to `-buckets-out`, stamping
   `pre_scan_ping.enabled=true` so `scan` never pings.

**Step 3 — `scan`** (requires `-resume <bucket file>`):
1. Load the bucket snapshot; derive the reachable set from its embedded blocklist
   (no reachability checker is constructed — pinging is impossible here).
2. Dispatch tasks with rate control and optional pressure-based pause.
3. Run TCP probes in a worker pool and stream progress events.
4. Write timestamped batch output files:
   - `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`
   - `opened_results-YYYYMMDDTHHMMSSZ[-n].csv`
5. On cancel/error, save progress **in place at the `-resume` path** (the bucket
   file is overwritten with updated progress; re-running the same command
   continues from there).

The "unreachable results are finalized before any TCP dial" guarantee is
enforced by this **step sequencing** — `preping` completes before `scan` runs.

## Output and Resume Behavior

- `-output` controls output directory; result files are always timestamped batches.
- Default batch naming is collision-safe within the same second (`-1`, `-2`, ... suffix).
- `preping` writes `unreachable_results-*` even when all targets are reachable; in that case it contains the header only. `scan` no longer writes this file.
- To skip the reachability gate, skip the `preping` step and run `generate-buckets` without `-unreachable-file`.
- The bucket snapshot **is** the resume state: `scan` requires `-resume <bucket file>`, reads it at start, and on cancel/error saves progress back to that exact path (in place). Re-running the same `scan` command continues from there.
- The snapshot's `pre_scan_ping` envelope carries the unreachable blocklist so `scan` reuses the same filtering decision without pinging.

## Dashboard and Logging

- `scan` enables the rich dashboard by default when `stderr` is attached to a TTY and `-format human` is used.
- Rich dashboard output is written to `stderr`.
- If `stderr` is not a TTY, or if `-format json` is selected, `scan` falls back to non-rich output.
- No new CLI flags are added for the UI in this version.

## Tools Reference

### port-scan

TCP port scanner with pressure-aware pacing and resume support.

**Commands** (each parses only its own flag surface):
- `port-scan preping [flags]` - Ping unique target IPs; write `unreachable_results-<ts>.csv` and print its path
- `port-scan generate-buckets [flags]` - Build the resume bucket snapshot (`-buckets-out` required); no network I/O
- `port-scan scan [flags]` - Pure TCP scan of a bucket snapshot (`-resume` required); no ping flags
- `port-scan validate [flags]` - Validate input files only (no network scan)

**Flags** (which flag lives on which subcommand). Full per-command tables and
defaults are in [All flags](docs/cli/flags.md).

| Flag | Subcommand(s) | Notes |
|------|---------------|-------|
| `-cidr-file` (required) | all | Rich/basic CSV; source of truth for target metadata |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | all | Case-sensitive column mapping (defaults `ip` / `ip_cidr`) |
| `-workers` | `preping`, `generate-buckets`, `scan` | Also parallelizes bucket generation (default `10`) |
| `-progress-interval` | `preping`, `generate-buckets`, `scan` | Progress line cadence, count-based (default `100`) — **NEW** |
| `-log-level` / `-format` / `-quiet` | all | Shared observability flags |
| `-pre-scan-ping-timeout` | `preping` | Ping reply-wait (default `100ms`); removed from `scan` |
| `-output` | `preping`, `scan` | Output anchor: unreachable CSV (`preping`), scan/opened CSVs (`scan`) |
| `-port-file` | `generate-buckets` (primary), `scan` (fallback) | Required in basic mode; ignored in rich mode |
| `-unreachable-file` | `generate-buckets` | Optional blocklist to subtract (a `preping` output) — **NEW** |
| `-buckets-out` (required) | `generate-buckets` | Bucket snapshot output path — **NEW** |
| `-resume` (required) | `scan` | Bucket snapshot to scan; updated in place on cancel/error |
| `-timeout` / `-delay` / `-bucket-rate` / `-bucket-capacity` | `scan` | Dial/dispatch tuning |
| `-disable-api`, `-pressure-*` | `scan` | Pressure-API control (auth flags required with `-pressure-use-auth`) |

### enrich-targets

Enriches a minimal `host,port` CSV into rich CSV format required by `port-scan` rich mode and `preprocess`.

**Usage:**
```bash
enrich-targets --input <file> --cidr-list <file> --service-map <file> --output <file>
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--input` | string | required | Path to opened targets CSV (`host,port`) |
| `--cidr-list` | string | required | Path to CIDR reference CSV for host-to-CIDR mapping |
| `--service-map` | string | required | Path to port-to-service-label CSV |
| `--output` | string | required | Path to write enriched rich CSV |

**Output format:** Rich CSV with all ten required columns: `src_ip`, `src_network_segment`, `dst_ip`, `dst_network_segment`, `service_label`, `protocol`, `port`, `decision`, `matched_policy_id`, `reason`

### preprocess

Filters a rich CSV by removing targets whose `dst_network_segment` is contained within a closed CIDR, then writes a port-scan-ready input file.

**Usage:**
```bash
preprocess --input <file> --cleaned-cidrs <file> --fab-name <name> --output-dir <dir>
```

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--input` | string | required | Path to rich CSV (from `enrich-targets` or filtered targets) |
| `--cleaned-cidrs` | string | required | Path to cleaned CIDRs CSV (`fab`, `segment`, `status`) |
| `--fab-name` | string | required | Data center / fabric name (filters CIDRs for this fabric) |
| `--output-dir` | string | required | Base output directory; writes to `<output-dir>/<fab-name>/<timestamp>/input.csv` |

**Output:** Timestamped CSV at `<output-dir>/<fab-name>/<timestamp>/input.csv` plus a summary (total / kept / dropped) on stderr.

### cidr-compare

Compare open CIDRs against deny CIDRs using an interval tree for efficient lookup.

**Usage:**
```bash
cidr-compare -deny-file <file> -open-file <file>
```

**Environment Variables:**
- `CIDR_COMPARE_DENY_FILE` - Path to deny CSV file
- `CIDR_COMPARE_OPEN_FILE` - Path to open CSV file

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-deny-file` | string | required | Path to deny CSV file (or CIDR_COMPARE_DENY_FILE env var) |
| `-open-file` | string | required | Path to open CSV file (or CIDR_COMPARE_OPEN_FILE env var) |

**Input format (deny CSV):** CSV with columns `dst_network_segment` (CIDR notation, e.g., `10.0.0.0/24`) and `decision`. Falls back to columns 0 and 1 if headers are missing.

**Input format (open CSV):** CSV with columns `segment` (CIDR notation) and `status`. Falls back to columns 0 and 1 if headers are missing.

**Output format:** CSV with header `deny_cidr,open_cidr` followed by matching pairs where the deny CIDR contains the open CIDR.

### csv-transform

Transforms a spreadsheet-style CSV (e.g. an Excel export with `Host`, `Port`, `Pass the test` columns) into rich CSV format via host resolution and port expansion. This is a separate preparation path for spreadsheet-sourced inputs and is not part of the `enrich-targets`/`preprocess` workflow.

**Usage:**
```bash
csv-transform -input <file> -output <file>
```

**Environment Variables:**
- `TRANSFORM_INPUT` - Path to input CSV file
- `TRANSFORM_OUTPUT` - Path to output CSV file
- `TRANSFORM_SHEET_NAME` - Worksheet name (default: `all-runs`)
- `TRANSFORM_HOST_COL` - Host column name (default: `Host`)
- `TRANSFORM_PORT_COL` - Port column name (default: `Port`)
- `TRANSFORM_PASS_COL` - Pass/fail column name (default: `Pass the test`)

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-input` | string | required | Path to input CSV file (or TRANSFORM_INPUT) |
| `-output` | string | required | Path to output CSV file (or TRANSFORM_OUTPUT) |
| `-sheet` | string | `all-runs` | Worksheet name (ignored for CSV) |
| `-host-col` | string | `Host` | Host column name |
| `-port-col` | string | `Port` | Port column name |
| `-pass-col` | string | `Pass the test` | Pass/fail column name |

**Output format:** Rich CSV with columns: `src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason`

## Flags Quick Reference

This section lists high-impact flags. Full definitions are in [All flags](docs/cli/flags.md).

| Flag | Typical Use |
|------|-------------|
| `-cidr-file` | CIDR/rich input CSV path (required on every subcommand) |
| `-port-file` | Port list path — `generate-buckets` in basic mode; `scan` fallback |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | Map custom CSV column names |
| `-unreachable-file` | `generate-buckets`: blocklist to subtract (a `preping` output) |
| `-buckets-out` | `generate-buckets`: bucket snapshot output (required) |
| `-resume` | `scan`: bucket snapshot to scan (required; updated in place) |
| `-output` | `preping` / `scan`: output directory anchor |
| `-pre-scan-ping-timeout` | `preping`: reachability timeout (default `100ms`) |
| `-progress-interval` | Pipeline steps: progress line cadence (default `100`) |
| `-disable-api` | `scan`: disable pressure API polling |
| `-pressure-api` / `-pressure-interval` | `scan`: pressure-based pause control |
| `-workers` / `-timeout` / `-delay` | Tune concurrency and probe pacing |
| `-log-level` / `-format` | Runtime visibility (`human` or `json`) |

## Repository Map

- `cmd/port-scan`: CLI composition root, command routing, user I/O, exit codes
- `cmd/enrich-targets`: promotes `host,port` CSV to rich CSV format for re-scan workflows
- `cmd/preprocess`: filters rich CSV by closed-CIDR containment, writes port-scan input
- `cmd/cidr-compare`: CIDR interval-tree comparison utility
- `cmd/csv-transform`: spreadsheet-to-rich-CSV transformer (Excel-export path)
- `pkg/config`: flag parsing and configuration validation
- `pkg/cli`: CLI composition utilities bridging domain types to writers and formats
- `pkg/input`: CIDR/rich input loading and row-level validation
- `pkg/validate`: input validation service for the `validate` command
- `pkg/cidrutil`: CIDR CSV parsing and selector construction
- `pkg/netutil`: IPv4 range, execution-key, and IPv4-to-uint32 utilities
- `pkg/task`: selector expansion and execution-key helpers
- `pkg/scanapp`: scan orchestration (load, plan, dispatch, execute, aggregate, resume, outputs)
- `pkg/scanner`: single TCP probe primitive
- `pkg/ratelimit`: leaky-bucket rate limiter for dispatch throttling
- `pkg/writer`: fixed CSV output contract and open-only projection
- `pkg/speedctrl`: manual/API pause controller
- `pkg/state`: resume state persistence and signal helpers
- `pkg/logx`: structured NDJSON logging
- `pkg/enrich`: minimal `host,port` to rich CSV transformation (enrich-targets core)
- `pkg/preprocess`: closed-CIDR containment filtering (preprocess core)
- `pkg/preprocesscfg`: shared column names, status values, and placeholder defaults for enrich/preprocess
- `pkg/spreadsheet`: spreadsheet/CSV reader abstraction (csv-transform core)
- `tests/integration`: integration contracts
- `e2e`: dockerized end-to-end verification and artifact checks

## Operational Notes and Constraints

- IPv4 only (selectors, CIDR parsing, and expansion paths).
- Port input accepts `<port>/tcp` only.
- Pressure API polling fails hard after 3 consecutive failures.
- Pressure threshold defaults to `60` and is not exposed as CLI flag.
- Pause gate blocks new dispatch only; in-flight worker probes continue.
- Dispatch order is chunk-serial (not cross-CIDR fair round-robin).
- E2E requires Docker runtime and `docker compose`.

## Testing and Verification

- **Full quality gate (run before every "done"): `make verify`** — gofmt, `go vet`,
  build, `go test -race -shuffle=on`, and the 85% coverage gate. Mirrors CI.
- Full gate **plus** isolated Docker e2e: `make verify-e2e`
- Individual steps: `make test` · `make cover` · `make e2e` · `make fmt` (`make help` lists all)
- Speed-control verification report: `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`
- CI runs these gates on every push and PR (`.github/workflows/ci.yml`).
- See [Maintainability Baseline](docs/MAINTENANCE.md) for the full contract, cross-platform
  notes, and a complete runnable example.

## Secret Scanning (gitleaks)

- Install gitleaks (example on macOS): `brew install gitleaks`
- Enable pre-commit hook (one-time): `bash scripts/setup-githooks.sh`
- Manual staged scan (same as hook): `gitleaks git --staged --redact --config=.gitleaks.toml .`
- CI scan is enforced on every `push` and `pull_request` by `.github/workflows/gitleaks.yml`.

## Docs

- [Maintainability Baseline](docs/MAINTENANCE.md) — quality gates, cross-platform, runnable example
- [All flags](docs/cli/flags.md)
- [Scenario cookbook](docs/cli/scenarios.md)
- [Pre-processing workflow spec](docs/specs/2026-04-16-preprocess-workflow-spec.md)
- [E2E overview](docs/e2e/overview.md)
- [Speed-control E2E](docs/e2e/speedcontrol.md)
- [Architecture diagram](docs/architecture/diagram.html)

### Per-tool specifications and design

Each `cmd/` tool has a dedicated specification and design document:

| Tool | Spec | Design |
|------|------|--------|
| port-scan | [SPEC](docs/apps/port-scan/SPEC.md) | [DESIGN](docs/apps/port-scan/DESIGN.md) |
| enrich-targets | [SPEC](docs/apps/enrich-targets/SPEC.md) | [DESIGN](docs/apps/enrich-targets/DESIGN.md) |
| preprocess | [SPEC](docs/apps/preprocess/SPEC.md) | [DESIGN](docs/apps/preprocess/DESIGN.md) |
| cidr-compare | [SPEC](docs/apps/cidr-compare/SPEC.md) | [DESIGN](docs/apps/cidr-compare/DESIGN.md) |
| csv-transform | [SPEC](docs/apps/csv-transform/SPEC.md) | [DESIGN](docs/apps/csv-transform/DESIGN.md) |

---
**Revised**: 2026-07-22 | **Author**: docs-team