# Port Scan MK3

Port Scan MK3 is a TCP port scanner command-line tool for developers. It validates input before a scan and supports pressure-aware pacing and resume snapshots.

## Current Release

Version 4.0.0 keeps the command-line interface stable and changes the public Go API. Existing CLI commands do not need migration.

The Go API uses workflow-specific configuration values. Library users can use `pkg/pressure` adapters or implement `scanapp.PressureSource` for a custom source.

If you migrate a Go application, read the [4.0.0 release notes](docs/release-notes/4.0.0.md).

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

Run a scan with the three-step pipeline: `pre-ping`, `generate-buckets`, and `scan`.

The `scan` command requires a bucket snapshot through `-resume`. It does not ping targets and does not accept ping flags.

This behavior changed in version 2.0.0. Read the [2.0.0 release notes](docs/release-notes/2.0.0.md) for migration information.

```bash
# 1. Ping unique targets; capture the printed unreachable CSV path (stdout)
UNREACHABLE=$(go run ./cmd/port-scan pre-ping \
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

To skip the pre-scan reachability check (the old `-disable-pre-scan-ping=true`),
omit the `pre-ping` step and run `generate-buckets` without `-unreachable-file`.
The snapshot then covers all targets and still stamps
`pre_scan_ping.enabled=true`. As a result, `scan` never pings:

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

Two tools prepare input files for `port-scan`. Both write a port-scan-ready rich CSV to
`<output-dir>/<fab_name>/<timestamp>/input.csv`.

### From-Scratch Flow

Use this flow when you scan a data center for the first time or start fresh.
`preprocess` filters a firewall-policy rich CSV. It removes each target whose
`dst_network_segment` is inside a closed CIDR.

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

Use this flow to re-scan open targets that an earlier scan discovered, supplied
as a `host,port` CSV. `enrich-targets` promotes the CSV to rich format.
`preprocess` then applies the same CIDR filter.

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
- `port-scan` selects rich CSV mode automatically when all rich fields exist:
  - `src_ip`, `src_network_segment`, `dst_ip`, `dst_network_segment`
  - `service_label`, `protocol`, `port`, `decision`, `policy_id`, `reason`
  - A valid `decision=deny` row never becomes a network target.
  - A deny row overrides an accept row with the same normalized `execution_key`.
  - The authorization rule applies before ICMP, bucket generation, resume rebuild, and TCP dispatch.
  - `validate` still rejects each malformed deny row.
- Port file format: one line per port in `<port>/tcp` (for example `443/tcp`)
  - Required in default CIDR mode
  - Optional in rich CSV mode

## Commands

- `pre-ping`: ping unique target IPs, write `unreachable_results-<ts>.csv`, print its path
- `generate-buckets`: build the resume bucket snapshot from targets minus an optional blocklist (`-buckets-out` required)
- `scan`: pure TCP scan of a bucket snapshot (`-resume` required. It dispatches, probes, writes output, and persists resume state in place)
- `validate`: parse and validate input files only

## Scan Output Batches

`scan` writes both result files as one commit batch. The
`-output-flush-results` flag sets the number of probe results in each batch.

The default is `1000`. A value of `1` flushes each result. A value of `0`
disables periodic flushes and keeps the final flush.

Positive values have no fixed maximum. A resumed run uses its current flag
value because the snapshot does not store this value.

The scan updates progress after both writers flush. If output fails, a resumed
scan repeats the complete uncommitted batch.

## Target Expansion Limits

All four commands verify the complete target expansion before network or output work.

- `-target-count-limit` has a default value of `10000000` candidate addresses.
- `-target-memory-limit-gb` has a default value of `16` decimal GB.
- The memory estimate is `1000000000 + candidate count * 1500` bytes.
- Set either flag to `0` to disable that limit.
- A negative value is an error before the command reads an input file.

The count includes each authorized input row before de-duplication, broadcast removal, and blocklist filtering.
Rich deny rows contribute zero candidates. With default limits, an IPv4 `/9` is permitted and an IPv4 `/8` is rejected.

`generate-buckets` stores the effective limits and candidate count in the snapshot.
`scan` uses these stored limits unless an explicit scan flag replaces one limit.
A legacy snapshot uses the new defaults.

CAUTION: If both flags are `0`, the command has no target expansion limit.

## Input, Snapshot, and Pressure Limits

`port-scan` applies independent limits to files and HTTP responses.

| Data | Default byte limit | Default item limit | Commands |
|---|---:|---:|---|
| CIDR CSV | `1` decimal GB | `10000000` records | All commands |
| Port file | `1` decimal MB | `65535` records | `validate`, `generate-buckets`, `scan` |
| Snapshot | `2` decimal GB | `10000000` chunks, ports, and unreachable IPs | `generate-buckets`, `scan` |
| Pressure response | `1` decimal MB | `10000` OAuth data entries | `scan` |

The related flags end in `-limit-gb`, `-limit-mb`, or `-limit`.
A positive value replaces its default. A negative value is an error.

Set one flag to `0` to disable only that limit.
The command does not apply a hidden replacement limit.

CAUTION: A disabled limit can exhaust memory or terminate the process.
The operating system can terminate the process when the available memory is not sufficient.

`generate-buckets` marks snapshots that exclude rich deny rows. If an unmarked snapshot has rich deny input, `scan` stops before TCP dispatch.

Exit code behavior:

- `0`: success
- `1`: validation failed (`validate`) or scan runtime error (`scan`)
- `2`: CLI parsing/config error
- `130`: scan canceled by `SIGINT` (`Ctrl+C`)

## Version Contract

All five commands — `port-scan`, `preprocess`, `enrich-targets`, `cidr-compare`,
`csv-transform` — answer the same version request. The token must be the **first
argument**. All three spellings are equivalent:

```
port-scan version
port-scan --version
port-scan -version
```

The report goes to stdout, and the command exits `0`:

```
port-scan version v4.0.0
commit:  <tagged commit SHA>
built:   <tagged commit time in UTC>
go:      go1.24.4 windows/amd64
```

| Field | Source | Policy |
|-------|--------|--------|
| version | `git describe --always --dirty` at build time | This field contains the nearest annotated tag. Without a tag, it contains the commit. |
| commit | `git rev-parse HEAD` at build time | This field contains the full source commit. |
| built | The commit timestamp in UTC | This field contains the commit time in UTC, not the wall-clock time. |
| go | The binary toolchain and `GOOS/GOARCH` | This field identifies the build platform of the artifact. |

**Dirty builds.** A working tree with uncommitted changes makes `git describe`
append `-dirty`, and the report then carries an extra line:

```
warning: built from a modified working tree; this artifact cannot be reproduced from a commit and is not a published release
```

The release workflow refuses to package such a build, and
`scripts/smoke_release.sh` refuses to pass one.

**Unstamped builds.** A binary built without the release ldflags — `go build
./cmd/...`, `go run`, or `go test` — reports `dev` for the version and `unknown`
for the commit and build time. That report is the documented fallback, not an
error. Only artifacts from `make build` or the release workflow carry real
values.

## Architecture and Data Flow

### port-scan Pipeline (three steps)

Each arrow between steps is a durable file, so the pipeline can stop and restart
at any boundary. `rich.csv` (`-cidr-file`) feeds all three steps.

```
        rich.csv
           │
           ▼
   ┌───────────────┐
   │    pre-ping   │  ping unique IPs (progress → stderr)
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
`rich.csv` (`-cidr-file`) feeds all three steps as the single source of
truth for target metadata.

**Step 1 — `pre-ping`** (optional: skip it to skip reachability filtering):
1. Load the CIDR/rich CSV and collect unique IPv4 targets.
2. Ping each unique target (`-pre-scan-ping-timeout`, default `100ms`) and write
   percentage progress to stderr every `-progress-interval` units.
3. Finalize `unreachable_results-YYYYMMDDTHHMMSSZ[-n].csv` and print its path to
   stdout for chaining.

**Step 2 — `generate-buckets`** (no network I/O):
1. Load targets and ports. Parse the optional `-unreachable-file` blocklist.
2. Subtract the blocklist, group by CIDR, and build one chunk per group in
   parallel over `-workers` (deterministic, CIDR-sorted). The chunk never
   includes the broadcast address of each row's boundary subnet (`ip_cidr` /
   `dst_network_segment`, prefix /30 or larger). This exclusion applies to
   addresses from CIDR expansion and to explicitly listed IPs. The chunk keeps
   the network address.
3. Write the resume bucket snapshot to `-buckets-out` and stamp
   `pre_scan_ping.enabled=true`. As a result, `scan` never pings.

**Step 3 — `scan`** (requires `-resume <bucket file>`):
1. Load the bucket snapshot. Derive the reachable set from its embedded blocklist
   (the command constructs no reachability checker — a ping is impossible here).
2. Dispatch tasks with rate control and optional pressure-based pause.
3. Run TCP probes in a worker pool and stream progress events.
4. Write timestamped batch output files:
   - `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`
   - `opened_results-YYYYMMDDTHHMMSSZ[-n].csv`
5. On cancel or error, save progress **in place at the `-resume` path** (the
   command overwrites the bucket file with updated progress, and a re-run of
   the same command continues from there). If an output write fails, `scan`
   rewinds the affected chunk cursors before it saves progress. The command
   then exits with the original write error. See "Output and Resume Behavior"
   below.

This **step sequencing** enforces the "unreachable results are finalized before
any TCP dial" guarantee — `pre-ping` completes before `scan` runs.

## Output and Resume Behavior

- `-output` controls the output directory. Result files are always timestamped batches.
- Default batch naming is collision-safe within the same second (`-1`, `-2`, ... suffix).
- `pre-ping` writes `unreachable_results-*` even when all targets are reachable. In that case the file contains the header only. `scan` no longer writes this file.
- To skip the reachability gate, skip the `pre-ping` step and run `generate-buckets` without `-unreachable-file`.
- The bucket snapshot **is** the resume state: `scan` requires `-resume <bucket file>`, reads it at start, and on cancel or error saves progress back to that exact path (in place). A re-run of the same `scan` command continues from there.
- The snapshot's `pre_scan_ping` envelope carries the unreachable blocklist, so `scan` reuses the same filtering decision without a ping.
- **If an output write fails, `scan` saves corrected resume progress.** Each result carries its zero-based task index. `scan` rewinds each affected chunk to its first dispatched task that did not reach all required writers. A chunk with no unwritten result keeps its cursor. The command logs `resume_state_rewound`, saves the corrected snapshot, and exits with the write error.

  Recovery: run the same `scan -resume` command. The resumed run covers every target and appends to the recorded output files. It can write some persisted rows again because results finish out of order. Duplicate rows can occur in both `scan_results-*.csv` and `opened_results-*.csv`. Use a CSV parser and the `ip` plus `port` columns to remove duplicates. Do not use line-based tools because quoted fields can contain newlines. The [3.0.1 release notes](docs/release-notes/3.0.1.md) include a standard-library script that keeps the last result for each target.

## Dashboard and Logging

- `scan` enables the rich dashboard by default when `stderr` is a TTY and you use `-format human`.
- `scan` writes rich dashboard output to `stderr`.
- If `stderr` is not a TTY, or if you select `-format json`, `scan` uses non-rich output instead.
- This version adds no new CLI flags for the UI.

## Tools Reference

### port-scan

`port-scan` is a TCP port scanner with pressure-aware pacing and resume support.

**Commands:**

Each command uses a command-specific flag parser. For compatibility, `validate` accepts all legacy flags.

- `port-scan pre-ping [flags]` - Ping unique target IPs. Write `unreachable_results-<ts>.csv` and print its path
- `port-scan generate-buckets [flags]` - Build the resume bucket snapshot (`-buckets-out` required). No network I/O
- `port-scan scan [flags]` - Pure TCP scan of a bucket snapshot (`-resume` required). No ping flags
- `port-scan validate [flags]` - Validate input files only (no network scan)

**Flags** (which flag lives on which subcommand). Full per-command tables and
defaults are in [All flags](docs/cli/flags.md).

| Flag | Subcommand(s) | Notes |
|------|---------------|-------|
| `-cidr-file` (required) | all | Rich/basic CSV. The source of truth for target metadata |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | all | Case-sensitive column mapping (defaults `ip` / `ip_cidr`) |
| `-workers` | `pre-ping`, `generate-buckets`, `scan` | Also parallelizes bucket generation (default `10`, accepted range `1`-`1024`) |
| `-progress-interval` | `pre-ping`, `generate-buckets`, `scan` | Progress line cadence, count-based (default `100`) — **NEW** |
| `-log-level` / `-format` / `-quiet` | all | Shared observability flags |
| `-pre-scan-ping-timeout` | `pre-ping` | Ping reply-wait (default `100ms`). Removed from `scan` |
| `-output` | `pre-ping`, `scan` | Output anchor: unreachable CSV (`pre-ping`), scan/opened CSVs (`scan`) |
| `-port-file` | `generate-buckets` (primary), `scan` (legacy fallback) | Basic rows with blank `port` values require this file. Rich mode ignores it |
| `-unreachable-file` | `generate-buckets` | Optional blocklist to subtract (a `pre-ping` output) — **NEW** |
| `-buckets-out` (required) | `generate-buckets` | Bucket snapshot output path — **NEW** |
| `-resume` (required) | `scan` | Bucket snapshot to scan. Updated in place on cancel or error |
| `-timeout` / `-delay` / `-bucket-rate` / `-bucket-capacity` | `scan` | Dial/dispatch tuning (`-bucket-rate` and `-bucket-capacity` accept `1`-`1000000`) |
| `-disable-api`, `-pressure-*` | `scan` | Pressure-API control (auth flags required with `-pressure-use-auth`) |

### enrich-targets

`enrich-targets` enriches a minimal `host,port` CSV into the rich CSV format that `port-scan` rich mode and `preprocess` require.

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

`preprocess` filters a rich CSV: it removes targets whose `dst_network_segment` is inside a closed CIDR. Then it writes a port-scan-ready input file.

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
| `--output-dir` | string | required | Base output directory. The tool writes to `<output-dir>/<fab-name>/<timestamp>/input.csv` |

**Output:** Timestamped CSV at `<output-dir>/<fab-name>/<timestamp>/input.csv` plus a summary (total / kept / dropped) on stderr.

`--fab-name` must be one safe directory name. The tool validates it before it
reads an input file. The tool rejects these values without sanitization:

- Path separators, `.` or `..`, absolute paths, control characters, and the
  characters `< > : " | ? *`.
- Names that end with a dot or space.
- These Windows device names: `CON`, `PRN`, `AUX`, `NUL`, `CONIN$`, `CONOUT$`,
  `COM0`–`COM9`, and `LPT0`–`LPT9`.
- These superscript device names: `COM¹`, `COM²`, `COM³`, `LPT¹`, `LPT²`, and
  `LPT³`.
- Matching extension or padded-stem forms, such as `com¹.txt` and
  `CONOUT$ .log`. This rule ignores case.

For the full list, see
[docs/apps/preprocess/SPEC.md](docs/apps/preprocess/SPEC.md).

### cidr-compare

`cidr-compare` compares open CIDRs against deny CIDRs with an interval tree for efficient lookup.

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

**Input format (deny CSV):** CSV with official columns `dst_network_segment` and `decision`. If neither header exists, the tool reads columns 0 and 1.

**Input format (open CSV):** CSV with official columns `segment` and `status`. If neither header exists, the tool reads columns 0 and 1.

If only one official header exists, the tool stops with exit code 1. Invalid nonblank records also cause exit code 1 and empty stdout.

**Output format:** CSV with header `deny_cidr,open_cidr` followed by matching pairs where the deny CIDR contains the open CIDR.

### csv-transform

`csv-transform` transforms a spreadsheet-style CSV (for example an Excel export with `Host`, `Port`, `Pass the test` columns) into rich CSV format through host resolution and port expansion. This tool is a separate preparation path for spreadsheet-sourced inputs. It is not part of the `enrich-targets`/`preprocess` workflow.

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
| `-port-file` | Port list path — `generate-buckets` in basic mode, `scan` fallback |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | Map custom CSV column names |
| `-unreachable-file` | `generate-buckets`: blocklist to subtract (a `pre-ping` output) |
| `-buckets-out` | `generate-buckets`: bucket snapshot output (required) |
| `-resume` | `scan`: bucket snapshot to scan (required, updated in place) |
| `-output` | `pre-ping` / `scan`: output directory anchor |
| `-pre-scan-ping-timeout` | `pre-ping`: reachability timeout (default `100ms`) |
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
- `pkg/config`: command-specific flag parsing and configuration values
- `pkg/cli`: CLI composition utilities bridging domain types to writers and formats
- `pkg/input`: CIDR/rich input loading and row-level validation
- `pkg/validate`: input validation service for the `validate` command
- `pkg/cidrutil`: CIDR CSV parsing and selector construction
- `pkg/netutil`: IPv4 range, execution-key, and IPv4-to-uint32 utilities
- `pkg/task`: selector expansion and execution-key helpers
- `pkg/scanapp`: private scan runtime and scan workflow orchestration
- `pkg/pressure`: pressure samples and HTTP or OAuth pressure adapters
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
- The pressure threshold defaults to `60`. No CLI flag exposes it.
- The pause gate blocks new dispatch only. In-flight worker probes continue.
- Dispatch order is chunk-serial (not cross-CIDR fair round-robin).
- E2E requires the Docker runtime and `docker compose`.

## Windows Install, Upgrade and Rollback (PowerShell)

Release assets are Windows x64 (`amd64`) only — Windows ARM64 is deliberately
out of scope. The workflow
[`.github/workflows/release.yml`](.github/workflows/release.yml) builds them
from the tagged source on a clean runner. It runs every `.exe` on a native
Windows runner before it publishes the release. No binary is ever committed to
this repository.

All commands below are PowerShell 5.1 or 7+. Set `$Version` once:

```powershell
$Version = 'v4.0.0'
$Archive = "port-scan-mk3_${Version}_windows_amd64.zip"
```

### 1. Download and verify the checksum

Download `$Archive` and `SHA256SUMS.txt` from the release page. Then verify the
checksum before you extract anything:

```powershell
$expected = (Select-String -Path SHA256SUMS.txt -Pattern $Archive).Line.Split(' ')[0]
$actual   = (Get-FileHash -Algorithm SHA256 $Archive).Hash.ToLower()
if ($actual -ne $expected) { throw "checksum mismatch: $actual != $expected" }
'checksum ok'
```

The archive contains a second `SHA256SUMS.txt` for the individual `.exe`
files, so you can verify them again after extraction.

### 2. Install

A per-user install needs no administrator rights (recommended):

```powershell
$Install = "$env:LOCALAPPDATA\Programs\port-scan-mk3"
New-Item -ItemType Directory -Force -Path $Install | Out-Null
Expand-Archive -Path $Archive -DestinationPath $Install -Force
```

For a machine-wide install, use `"$env:ProgramFiles\port-scan-mk3"` instead and
run PowerShell as Administrator. Do not install into a directory that a scan
writes output to. Keep binaries and scan output separate.

### 3. Add it to PATH

```powershell
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$Install*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$Install", 'User')
}
```

This change affects **new** shells only. Open a new PowerShell window. Then make
sure that the installed build is the one that you verified:

```powershell
port-scan --version
```

The first line must read `port-scan version v4.0.0`. If it says `dev`, the
binary is a local build, not a release asset.

### 4. Upgrade (snapshot backup first)

Always make a snapshot of the current install before you overwrite it. This
snapshot makes the rollback in step 5 possible:

```powershell
$Stamp    = Get-Date -Format 'yyyyMMdd-HHmmss'
$Snapshot = "$env:LOCALAPPDATA\Programs\port-scan-mk3.backup-$Stamp"
Copy-Item -Recurse -Force $Install $Snapshot

Expand-Archive -Path $Archive -DestinationPath $Install -Force
port-scan --version
```

Stop any running scan before you upgrade. Windows keeps the `.exe` locked while
it runs, and `Expand-Archive` fails — it does not replace the locked file. A
scan that you stop with `Ctrl+C` writes its resume snapshot. You can continue
that scan with `-resume` after the upgrade (see [Output and Resume
Behavior](#output-and-resume-behavior)).

### 5. Rollback

```powershell
Remove-Item -Recurse -Force $Install
Copy-Item -Recurse -Force $Snapshot $Install
port-scan --version   # must report the version you rolled back to
```

Verify the version after the rollback. That check is the only proof of which
build you run. A rollback does not change scan output that the newer version
wrote. Before you reuse those files, read the release notes of that version for
output schema changes.

### What Windows validation does and does not cover

The isolated Docker e2e suite (`make verify-e2e`) runs **Linux** containers, so
it validates Linux behavior only. It says nothing about the Windows binaries.
Two separate gates cover Windows: the native Windows job in
`.github/workflows/ci.yml` (build and `go test` on `windows-latest`), and the
release workflow's smoke job. The smoke job runs every published `.exe` on a
`windows-latest` runner. It makes sure that each `.exe` reports the release
version before anything is published.

## Testing and Verification

- **Full quality gate (run before every "done"): `make verify`** — gofmt, `go vet`,
  build, `go test -race -shuffle=on`, and the 85% coverage gate. It mirrors CI.
- Full gate **plus** isolated Docker e2e: `make verify-e2e`
- Individual steps: `make test` · `make cover` · `make e2e` · `make fmt` (`make help` lists all)
- Speed-control verification report: `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`
- CI runs these gates on every push and PR (`.github/workflows/ci.yml`).
- On a `v*` tag push, `.github/workflows/release.yml` builds, checksums, and
  smoke-tests the release assets. See
  [Release evidence](docs/MAINTENANCE.md#7-release-evidence).
- See [Maintainability Baseline](docs/MAINTENANCE.md) for the full contract, cross-platform
  notes, and a complete runnable example.

## Secret Scanning (gitleaks)

- Install gitleaks (example on macOS): `brew install gitleaks`
- Enable pre-commit hook (one-time): `bash scripts/setup-githooks.sh`
- Manual staged scan (same as hook): `gitleaks git --staged --redact --config=.gitleaks.toml .`
- `.github/workflows/gitleaks.yml` enforces the CI scan on every `push` and `pull_request`.

## Docs

- [Maintainability Baseline](docs/MAINTENANCE.md) — quality gates, cross-platform, runnable example
- [4.0.0 release notes](docs/release-notes/4.0.0.md) — Go API changes and migration examples
- [All flags](docs/cli/flags.md)
- [Scenario cookbook](docs/cli/scenarios.md)
- [Interrupt handling](docs/interrupt-handling.md) — which terminations stop a scan gracefully (Ctrl+C, Ctrl+Break) and which do not
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
**Revised**: 2026-08-11 | **Author**: docs-team
