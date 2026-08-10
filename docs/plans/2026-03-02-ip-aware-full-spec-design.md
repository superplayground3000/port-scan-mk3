# Port Scan MK3 IP-Aware Full Spec Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. Scope

This design finalizes full implementation of `plan/design.md` with additional requirements:

1. CIDR input schema now includes `ip` and `ip_cidr`, and actual scan targets come only from `ip` entries.
2. Input CSV can contain many columns; parser must select columns by header names.
3. Add `opened_results.csv` output, fixed at the same directory as `-output`, containing only `open` records.
4. e2e must include API server scenarios (normal, HTTP 5xx, timeout/connection failure) and must not support skip mode.

Design must comply with `.specify/memory/constitution.md`:
- Library-first
- CLI-first
- Test-first
- Integration + e2e required
- Coverage gate > 85%

---

## 2. CLI Contract Changes

### 2.1 New flags

Add to scan/validate path:

- `-cidr-ip-col` (default `ip`): header name used as target selector column.
- `-cidr-ip-cidr-col` (default `ip_cidr`): header name used as containment/rate-group boundary column.

### 2.2 Existing flags (keep)

Keep all existing flags from `plan/design.md` and current implementation:
- `-cidr-file`, `-port-file`, `-output`, `-timeout`, `-delay`, `-bucket-rate`, `-bucket-capacity`, `-workers`, `-pressure-api`, `-pressure-interval`, `-disable-api`, `-resume`, `-log-level`, `-format`

### 2.3 Output files

- Primary output: `<output>` (e.g. `scan_results.csv`)
- New fixed output: `<dirname(output)>/opened_results.csv`
- Resume output: `resume_state.json` (or custom path when resume path is explicitly provided for save/load path)

---

## 3. Input Model

### 3.1 CIDR CSV behavior

CSV may contain many columns. System only relies on:
- column named by `-cidr-ip-col` => `ip`
- column named by `-cidr-ip-cidr-col` => `ip_cidr`

Each row:
- `ip`: IPv4 single IP or CIDR
- `ip_cidr`: IPv4 CIDR boundary

### 3.2 Port CSV behavior

Unchanged:
- No header
- each line like `80/tcp`
- only TCP accepted

---

## 4. Validation Rules (Fail-Fast)

Validation stops startup immediately on first rule violation with explicit error context.

1. Required headers not found (`ip` / `ip_cidr` mapped names): Fatal.
2. `ip` parse failure (not IP/CIDR): Fatal.
3. `ip_cidr` parse failure (not CIDR): Fatal.
4. Any expanded target from `ip` outside corresponding `ip_cidr`: Fatal.
5. Different `ip_cidr` values overlapping each other: Fatal.
6. Duplicate `(ip, ip_cidr)` row combination: Fatal.
7. Within the same `ip_cidr`, different `ip` rows whose expanded sets overlap: Fatal.
8. CIDR/port file missing or unreadable: Fatal.
9. FD limit below required minimum: Fatal.

Notes:
- Same `ip_cidr` repeated across rows is allowed.
- Overlap across different `ip_cidr` values is not allowed.

---

## 5. Runtime Architecture

## 5.1 Grouping and chunking

Chunk key is `ip_cidr`.
For each chunk:
- target list built only from `ip` rows under that `ip_cidr`
- port list from port file
- total tasks = `len(expanded_unique_ips_in_chunk) * len(ports)`
- linear index remains chunk-local (`NextIndex`) for resume stability

### 5.2 Dispatch and workers

- Task generator waits on OR-gate (`api_paused || manual_paused`)
- per-chunk leaky bucket throttles dispatch
- worker pool consumes tasks from shared queue
- scanner uses `net.DialTimeout`
- result writer is single goroutine

### 5.3 Pause and pressure logic

Two pause sources:
- API pressure polling
- keyboard space toggle (raw mode)

Rules:
- Pause if either source paused
- Resume only when both clear

API error policy:
- 1st and 2nd consecutive failures: Error log, keep last known pause state
- 3rd consecutive failure: Fatal terminate, trigger graceful shutdown and resume save

### 5.4 Graceful shutdown and resume

On SIGINT/context cancel/fatal API:
1. stop dispatch
2. persist latest `NextIndex` per chunk
3. wait in-flight workers
4. write `resume_state.json`

On `-resume`:
- load chunk state
- continue from `NextIndex`
- guarantee no duplicate/missing tasks within chunk linear sequence

---

## 6. Output Contract

### 6.1 scan_results.csv

Write all scan results with header including both `ip` and `ip_cidr`.

Required columns:
- `ip`
- `ip_cidr`
- `port`
- `status`
- `response_time_ms`

Keep existing metadata columns already in use (`fab_name`, `cidr_name`, etc.) where available.

### 6.2 opened_results.csv

- fixed path: same dir as `-output`, filename `opened_results.csv`
- same header schema as `scan_results.csv`
- only records with `status == open`

---

## 7. Logging and Observability

Log-level behavior:
- `debug`: include low-level scan/dispatch diagnostics
- `info`: lifecycle, pause/resume, resume save/load
- `error`: failures only

Required key logs:
- API pause/resume transitions
- manual pause/resume transitions
- resume load/save path and counts
- API failure streak and fatal event on 3rd failure

CLI output format behavior:
- human-readable for `-format human`
- structured JSON records for `-format json`

---

## 8. Testing Strategy

### 8.1 Unit tests

Add/expand tests for:
- header-name based column parsing (`ip`, `ip_cidr` custom names)
- validation matrix for all fail-fast rules
- index mapping from chunk targets (`ip` subset only)
- dual-output writer (`scan_results` + `opened_results`)
- pressure failure streak 1/2/3 behavior

### 8.2 Integration tests

- end-to-end internal runner path with resume and cancellation
- ensure only `ip` subsets scanned, not full `ip_cidr`
- verify opened-only sink correctness

### 8.3 e2e tests (Docker required, no skip)

Scenarios:
1. API normal:
   - scan succeeds
   - `scan_results.csv` + `opened_results.csv` + report generated
2. API HTTP 5xx:
   - fail after 3 consecutive API failures
   - non-zero exit
   - `resume_state.json` generated
3. API timeout/connection failure:
   - fail after 3 consecutive API failures
   - non-zero exit
   - `resume_state.json` generated

Environment constraints:
- isolated docker-compose network only
- includes open target, closed target, and pressure API service

### 8.4 Quality gates

Must pass all:
- `go test ./...`
- integration tests
- docker e2e scenarios (all)
- coverage gate `> 85%`

---

## 9. Implementation Boundaries

Keep current architecture and extend incrementally (YAGNI):
- do not rewrite entire pipeline
- focus on missing specification behaviors
- preserve existing tested modules unless behavior change is required

Primary modules to evolve:
- `pkg/config` (new flags)
- `pkg/input` (header-name parsing + validations)
- `pkg/scanapp` (ip subset chunking, dual output sink, strict API failure policy)
- `pkg/writer` (second open-only sink)
- `e2e/` stack (API scenarios + assertions)

---

## 10. Acceptance Criteria

Implementation is complete when all are true:

1. Input uses named columns for `ip` and `ip_cidr` regardless of extra columns.
2. Scanner only scans addresses derived from `ip` rows and validates containment in `ip_cidr`.
3. All specified fail-fast validations enforced.
4. `opened_results.csv` produced at fixed location with open-only rows.
5. API 5xx and timeout/connection failures cause fatal after 3 consecutive failures.
6. Resume state saved correctly on interrupt/fatal and resume works from `NextIndex`.
7. Docker e2e includes API normal and both failure scenarios with explicit assertions.
8. Constitution gates satisfied: TDD workflow, integration/e2e pass, coverage > 85%.
