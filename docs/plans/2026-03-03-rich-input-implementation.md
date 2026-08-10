# Rich Input Parsing and Pipeline Mapping Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Support rich CIDR input rows with 10 required fields, row-level validation summary, execution-key dedup mapping, and writer contract preservation.

**Architecture:** Extend `pkg/input` to parse canonical rich headers and validate row semantics, then update `pkg/scanapp` runtime building so execution scheduling is deduplicated by `dst_ip+port+protocol` while retaining source-row context. Extend `pkg/writer` output contract with context columns and keep compatibility for existing fields. Use TDD with unit/integration coverage and e2e/coverage gates before merge.

**Tech Stack:** Go 1.24, standard library (`encoding/csv`, `strings`, `net`, `strconv`, `sort`), existing packages (`pkg/input`, `pkg/scanapp`, `pkg/task`, `pkg/writer`), Go test + integration + e2e scripts.

---

### Task 1: Rich input schema and parser primitives

**Files:**
- Create: `pkg/input/rich_types.go`
- Create: `pkg/input/header_match.go`
- Create: `pkg/input/validation_errors.go`
- Create: `pkg/input/rich_parser.go`
- Test: `pkg/input/rich_parser_test.go`

**Step 1: Write failing tests**
- header canonicalization: trim + case-insensitive
- missing required 10 fields should fail row
- protocol only `tcp`, decision only `accept/deny`
- port range and integer validation
- src/dst containment validation

**Step 2: Run targeted test and verify fail**
- Run: `go test ./pkg/input -run Rich -count=1`

**Step 3: Implement minimal parser and validators**
- parse CSV header once and map canonical required fields
- produce `RichRow` and `ParsedTargetRecord`
- collect row validation failures with reason code/message

**Step 4: Run test to verify pass**
- Run: `go test ./pkg/input -run Rich -count=1`

**Step 5: Commit**
- `git add pkg/input/rich_types.go pkg/input/header_match.go pkg/input/validation_errors.go pkg/input/rich_parser.go pkg/input/rich_parser_test.go`
- `git commit -m "feat(input): add rich parser and row validation primitives"`

### Task 2: Integrate rich parser into input loading

**Files:**
- Modify: `pkg/input/cidr.go`
- Modify: `pkg/input/types.go`
- Test: `pkg/input/cidr_columns_test.go`

**Step 1: Write failing tests**
- rich input rows parsed from `LoadCIDRsWithColumns`
- legacy schema still works

**Step 2: Run targeted test and verify fail**
- Run: `go test ./pkg/input -run "LoadCIDRsWithColumns|Rich" -count=1`

**Step 3: Implement integration switch**
- detect rich schema by canonical required headers
- parse rich rows and map into runtime-compatible records

**Step 4: Run test to verify pass**
- Run: `go test ./pkg/input -run "LoadCIDRsWithColumns|Rich" -count=1`

**Step 5: Commit**
- `git add pkg/input/cidr.go pkg/input/types.go pkg/input/cidr_columns_test.go`
- `git commit -m "feat(input): integrate rich schema into CIDR loader"`

### Task 3: Execution key helper and dedup mapping

**Files:**
- Create: `pkg/task/execution_key.go`
- Create: `pkg/task/execution_key_test.go`
- Modify: `pkg/scanapp/scan.go`
- Test: `pkg/scanapp/scan_helpers_test.go`

**Step 1: Write failing tests**
- execution key generation from `dst_ip`, `port`, `protocol`
- duplicate rows map to one execution target but retain row refs

**Step 2: Run targeted test and verify fail**
- Run: `go test ./pkg/task ./pkg/scanapp -run "ExecutionKey|buildCIDRGroups" -count=1`

**Step 3: Implement dedup + mapping**
- add helper for key generation
- build runtime targets from dedup keys
- keep row-context back references for writer fields

**Step 4: Run test to verify pass**
- Run: `go test ./pkg/task ./pkg/scanapp -run "ExecutionKey|buildCIDRGroups" -count=1`

**Step 5: Commit**
- `git add pkg/task/execution_key.go pkg/task/execution_key_test.go pkg/scanapp/scan.go pkg/scanapp/scan_helpers_test.go`
- `git commit -m "feat(scanapp): dedup dispatch by execution key with row context mapping"`

### Task 4: Writer contract extension

**Files:**
- Modify: `pkg/writer/csv_writer.go`
- Modify: `pkg/writer/csv_writer_contract_test.go`
- Modify: `pkg/writer/csv_writer_test.go`

**Step 1: Write failing tests**
- writer header includes context columns
- rows emit service_label/decision/policy_id/reason

**Step 2: Run targeted test and verify fail**
- Run: `go test ./pkg/writer -run "contract|WriteHeader|Write" -count=1`

**Step 3: Implement minimal writer changes**
- extend `writer.Record`
- update header and row serialization

**Step 4: Run test to verify pass**
- Run: `go test ./pkg/writer -run "contract|WriteHeader|Write" -count=1`

**Step 5: Commit**
- `git add pkg/writer/csv_writer.go pkg/writer/csv_writer_contract_test.go pkg/writer/csv_writer_test.go`
- `git commit -m "feat(writer): include rich context fields in output contract"`

### Task 5: Integration and observability coverage

**Files:**
- Create: `tests/integration/testdata/rich_input/valid_mixed.csv`
- Create: `tests/integration/testdata/rich_input/invalid_rows.csv`
- Create: `tests/integration/testdata/rich_input/dedup_context.csv`
- Create: `tests/integration/rich_input_parse_test.go`
- Create: `tests/integration/rich_input_mapping_test.go`
- Create: `tests/integration/rich_input_rejection_test.go`
- Create: `tests/integration/rich_input_pipeline_boundary_test.go`
- Modify: `pkg/scanapp/scan_observability_test.go`

**Step 1: Write failing integration tests**
- mixed parse success
- dedup with context retention
- all-invalid stop behavior
- pipeline boundary coverage parser->task->pipeline->writer

**Step 2: Run targeted tests and verify fail**
- Run: `go test ./tests/integration ./pkg/scanapp -run "rich|observability" -count=1`

**Step 3: Implement minimal glue fixes**
- update scan summary path and error bucket outputs as needed

**Step 4: Run targeted tests and verify pass**
- Run: `go test ./tests/integration ./pkg/scanapp -run "rich|observability" -count=1`

**Step 5: Commit**
- `git add tests/integration pkg/scanapp/scan_observability_test.go`
- `git commit -m "test(integration): add rich input boundary and rejection coverage"`

### Task 6: Contracts/docs sync and quality gates

**Files:**
- Modify: `specs/001-enhance-input-parser/contracts/input-schema-contract.md`
- Modify: `specs/001-enhance-input-parser/contracts/parser-output-contract.md`
- Modify: `specs/001-enhance-input-parser/data-model.md`
- Modify: `specs/001-enhance-input-parser/quickstart.md`
- Modify: `docs/release-notes/1.2.0.md`

**Step 1: Update docs for implemented behavior**
- align contracts/entities/examples with final code
- include SC-003/SC-004 measurement outputs in quickstart

**Step 2: Run all mandatory gates**
- `go test ./...`
- `bash scripts/coverage_gate.sh`
- `bash e2e/run_e2e.sh`

**Step 3: Commit docs + evidence**
- `git add specs/001-enhance-input-parser docs/release-notes/1.2.0.md`
- `git commit -m "docs(spec): sync rich parser contracts and verification evidence"`

### Task 7: Merge and push workflow

**Files:**
- No file changes (git operations)

**Step 1: Credential/config exclusion check**
- ensure no `.env*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `id_rsa*`, `*secret*`, `*token*`, `*credential*` staged

**Step 2: Merge to master**
- `git checkout master`
- `git merge --no-ff codex/001-enhance-input-parser-integration`

**Step 3: Push remote**
- `git push origin master`

