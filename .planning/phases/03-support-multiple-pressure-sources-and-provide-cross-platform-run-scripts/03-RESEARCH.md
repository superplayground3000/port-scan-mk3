# Phase 3: Multi-Source Pressure Inputs and Cross-Platform Run Scripts - Research

**Researched:** 2026-04-03  
**Domain:** Go CLI multi-source pressure control, runtime aggregation, cross-platform run scripts  
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
### Locked by user
- Support multiple pressure sources in one run; current single-source limitation must be removed.
- Provide sample run scripts for Linux and Windows.
- Script formats are explicitly:
  - Linux: Bash (`.sh`)
  - Windows: Batch (`.bat`)

### Locked by project guardrails
- Follow SOLID and keep `cmd/port-scan` focused on CLI assembly/argument parsing/I/O only.
- Reusable logic must remain in `pkg/` (no business logic in CLI command handlers).
- Keep interfaces minimal and owned by consumers; avoid god interfaces and cyclic dependencies.

### Claude's Discretion
- Exact CLI syntax for multiple pressure sources (repeatable flags vs CSV-encoded list vs config file reference).
- Pressure aggregation policy defaults (e.g., max/min/average/any-fail) and user override strategy.
- Concrete script filenames and directory layout, as long as one `.sh` and one `.bat` sample are provided and documented.
- Backward-compatibility migration notes for existing users.

### Deferred Ideas (OUT OF SCOPE)
- Dynamic plugin-based pressure source loading.
- Non-HTTP pressure adapters (Kafka, file watcher, metrics backends).
- Centralized secret-management integration for sample scripts.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PRESSURE-03 | Support multiple pressure sources in one scan run (config + runtime aggregation) | `flag.FlagSet.Func` repeatable source input + `pkg/scanapp` aggregate fetcher/policy + backward compatibility plan |
| OPS-01 | Provide example execution scripts for Linux (bash) and Windows (bat) for multi-source pressure mode | Cross-platform script contract (`.sh` + `.bat`), argument/exit-code handling, environment audit and fallback strategy |
</phase_requirements>

## Summary

目前系統已具備可插拔 `PressureFetcher` 介面與 polling 生命週期（`pollPressureAPI`），但只支援單一 fetcher 來源。Phase 3 的最佳做法不是把多來源邏輯塞進 `cmd/port-scan` 或 `pollPressureAPI` if/else，而是在 `pkg/config` 產生「多來源設定陣列」，再由 `pkg/scanapp` 封裝一個聚合 fetcher（composite）將多來源壓力讀值統一成單一決策輸入，保持現有 pause/resume 與 3 次失敗升級流程不變。

為了維持相容與降低腳本 quoting 風險，建議新增 repeatable `-pressure-source`（可多次指定），同時保留舊旗標（`-pressure-api`、`-pressure-use-auth` 一組）作為 fallback。聚合策略預設使用 `max`（最保守，任一來源高壓即暫停），並提供 `-pressure-policy` 可選值（至少 `max|min|avg`）。對錯誤處理，建議採「每來源 failure streak + 全部來源失效才 fatal」以避免單一來源短暫故障直接中止整批掃描。

`OPS-01` 建議落在 `scripts/` 下提供 `run_multi_pressure_example.sh` 與 `run_multi_pressure_example.bat`，兩者都以同一組參數語意示範混合來源（simple + auth），並包含 endpoint/credential/interval/output placeholders。Linux 腳本可本機語法驗證；Windows `.bat` 在本環境無 `cmd.exe`，需透過 Windows runner 或人工驗證做 phase gate。

**Primary recommendation:** 在 `pkg/scanapp` 實作 `MultiPressureFetcher + AggregationPolicy(max default)`，CLI 以 repeatable `-pressure-source` 供多來源輸入，並保留既有單來源旗標完全相容。

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go toolchain | go1.24.4 | 編譯與測試基礎 | 專案已固定於 `go 1.24.0` + `toolchain go1.24.4` |
| Go stdlib `flag` (`FlagSet.Func`/`Var`) | stdlib (Go 1.24) | repeatable CLI flag 解析 | 官方支援「每次看到旗標都回呼」，最適合多來源輸入 |
| Existing `pkg/scanapp` `PressureFetcher` abstraction | in-repo | 壓力來源抽象與注入 | 既有設計已符合 SOLID，可直接擴充 composite |
| Existing `pkg/config` parse/validation | in-repo | CLI 到 runtime config mapping | 維持 `cmd/` 僅組裝、商業邏輯留在 `pkg/` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/term` | v0.35.0 (published 2025-09-08) | 既有互動控制能力 | 本 phase 不需新擴充，但需保持相容 |
| `golang.org/x/sys` | v0.36.0 (published 2025-09-05) | 既有系統層支援 | 本 phase 不新增依賴，僅避免破壞 |
| Bash (`.sh`) | GNU bash 3.2.57 (local) | Linux 範例腳本 | `OPS-01` Linux runnable example |
| Windows CMD batch (`.bat`) | N/A on current host | Windows 範例腳本 | `OPS-01` Windows runnable example |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Repeatable `-pressure-source` | 單一 JSON config file path | JSON 對 secret/複雜字元更穩，但操作門檻較高、快速試跑較慢 |
| Aggregation in dedicated fetcher | 直接把多來源邏輯寫進 `pollPressureAPI` | 會讓 monitor 函式膨脹，難測且違反單一職責 |
| `max` default policy | `avg` default policy | `avg` 可能掩蓋單點高壓，風險較高 |

**Installation:**
```bash
# No new dependency required for Phase 3 baseline implementation.
go mod tidy
```

**Version verification:**  
Verified with:
```bash
go list -m all
go list -m -json golang.org/x/term@v0.35.0
go list -m -json golang.org/x/sys@v0.36.0
```
Also checked newest upstream tags:
```bash
go list -m -versions golang.org/x/term
go list -m -versions golang.org/x/sys
```

## Architecture Patterns

### Recommended Project Structure
```
cmd/port-scan/
├── command_handlers.go            # CLI assembly only
pkg/config/
├── config.go                      # flag registration + Parse
├── pressure_sources.go            # NEW: source spec parsing/validation
pkg/scanapp/
├── pressure.go                    # existing fetcher implementations
├── pressure_aggregate.go          # NEW: MultiPressureFetcher + policies
├── pressure_monitor.go            # keep polling lifecycle; call one fetcher
scripts/
├── run_multi_pressure_example.sh  # NEW: Linux sample
└── run_multi_pressure_example.bat # NEW: Windows sample
```

### Pattern 1: Consumer-Owned Composite PressureFetcher
**What:** 以 `PressureFetcher` 作為唯一 polling 輸入，新增 `MultiPressureFetcher` 包裝多個來源與 policy。  
**When to use:** 任何需要多來源聚合但不想改動既有 polling state machine。  
**Example:**
```go
// Source: existing interface pattern in pkg/scanapp/pressure.go
type PressureFetcher interface {
	Fetch(ctx context.Context) (float64, error)
}

type AggregationPolicy interface {
	Name() string
	Combine(values []float64) (float64, error)
}

type MultiPressureFetcher struct {
	sources []PressureFetcher
	policy  AggregationPolicy
}
```

### Pattern 2: Repeatable Flag Parsing in `pkg/config`
**What:** 用 `flag.FlagSet.Func` 收集多次出現的 `-pressure-source`。  
**When to use:** CLI 需接受 1..N 個來源，且維持標準 flag 行為。  
**Example:**
```go
// Source: https://pkg.go.dev/flag#FlagSet.Func
var sourceSpecs []string
fs.Func("pressure-source", "repeatable pressure source spec", func(v string) error {
	sourceSpecs = append(sourceSpecs, v)
	return nil
})
```

### Pattern 3: Backward-Compatible Source Resolution
**What:** 優先使用新多來源設定；若未提供則回退舊旗標邏輯。  
**When to use:** brownfield migration，避免破壞現有自動化或文件。  
**Example:**
```go
// Source: project behavior in cmd/port-scan/command_handlers.go + pkg/config/config.go
if len(cfg.PressureSources) > 0 {
	opts.PressureFetcher = scanapp.NewMultiPressureFetcher(...)
} else if cfg.PressureUseAuth {
	opts.PressureFetcher = scanapp.NewAuthenticatedPressureFetcher(...)
} else {
	opts.PressureFetcher = scanapp.NewSimplePressureFetcher(cfg.PressureAPI, nil)
}
```

### Anti-Patterns to Avoid
- **把多來源 policy 寫在 `cmd/port-scan`**: 違反 guardrail（CLI 只能組裝/IO）。
- **在 `pollPressureAPI` 直接處理所有來源細節**: 會讓 polling 生命週期與來源聚合耦合，測試困難。
- **移除舊旗標或改語意**: 會破壞 Phase 1/2 既有流程與文件。

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Repeatable CLI source parsing | 自製 argv 迴圈 parser | `flag.FlagSet.Func` / `FlagSet.Var` | 官方支援重複旗標與錯誤傳遞，降低 parser bug |
| Multi-source pause decision | 大量 if/else scattered across monitor | `AggregationPolicy` + `MultiPressureFetcher` | 可測、可替換、符合 SOLID |
| Cross-platform wrapper generator | 動態產生腳本工具 | 兩份明確範例腳本（`.sh` + `.bat`） | 需求是「可跑範例」，非腳本框架 |

**Key insight:** 多來源壓力控制的真正複雜度在「錯誤語義 + 相容性」，不是 HTTP call 本身；把複雜度封裝在可測抽象層，planning 才可控。

## Common Pitfalls

### Pitfall 1: Multi-Source Policy 未定義清楚
**What goes wrong:** 不同開發者對 pause 條件理解不同（any/highest/average）。  
**Why it happens:** 需求只寫「explicit selection policy」但未固定 default。  
**How to avoid:** 在 config 定義 `PressurePolicy`，default 明確為 `max`，並在 logs 印出 policy 名稱。  
**Warning signs:** 測試名稱/期望值互相矛盾；同樣輸入在不同環境 pause 行為不同。

### Pitfall 2: Failure Escalation 直接沿用單來源語義到每個來源
**What goes wrong:** 任一來源連續失敗 3 次就中止整體掃描，造成高誤殺。  
**Why it happens:** 現行實作是單來源設計。  
**How to avoid:** 追蹤每來源 streak，僅在「全部來源皆不可用」時回傳 fatal。  
**Warning signs:** 某一來源短暫故障即出現 `pressure api failed 3 times` 並終止。

### Pitfall 3: `.bat` 腳本引用與 exit code 處理不正確
**What goes wrong:** 路徑含空白或上一個命令失敗卻沒傳遞 ERRORLEVEL。  
**Why it happens:** cmd 變數展開與 `exit /b` 規則與 bash 不同。  
**How to avoid:** `setlocal enabledelayedexpansion`、全程雙引號、`if errorlevel` 與 `exit /b <code>`。  
**Warning signs:** Windows 上腳本顯示成功但實際掃描未執行，或 CI 不可重現。

## Code Examples

Verified patterns from official sources:

### Repeatable Multi-Source Flags
```go
// Source: https://pkg.go.dev/flag#FlagSet.Func
// Each -pressure-source occurrence appends one source definition.
var sourceSpecs []string
fs.Func("pressure-source", "repeatable source spec", func(v string) error {
	sourceSpecs = append(sourceSpecs, v)
	return nil
})
```

### Policy-Driven Aggregation
```go
// Source: project abstraction style in pkg/scanapp/pressure.go
type MaxPolicy struct{}

func (MaxPolicy) Name() string { return "max" }

func (MaxPolicy) Combine(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, errors.New("no pressure samples")
	}
	maxV := values[0]
	for _, v := range values[1:] {
		if v > maxV {
			maxV = v
		}
	}
	return maxV, nil
}
```

### Batch Exit Code Propagation
```bat
@echo off
setlocal enabledelayedexpansion
go run .\cmd\port-scan scan ...
if errorlevel 1 exit /b %errorlevel%
```
Source: `if ERRORLEVEL` / `exit /b` behavior from Microsoft command docs.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Single-source only (`-pressure-api` or one auth source) | Multi-source list + explicit aggregation policy | Planned in Phase 3 (2026-04) | 支援混合來源並可明確控制 pause semantics |
| Manual one-off flag handling | Repeatable flag callbacks (`FlagSet.Func`) | Available since Go 1.16 | 較少 parser bug，擴充來源數量更容易 |
| Linux-only script assumptions | Parallel `.sh` + `.bat` examples | Required by OPS-01 | 提升跨平台可運行性與交付完整度 |

**Deprecated/outdated:**
- 「`-pressure-use-auth` 為唯一模式切換」在多來源情境已不足；應保留作 backward compatibility fallback。
- 「把多來源邏輯塞進 `pollPressureAPI`」屬短期做法，後續難維護。

## Open Questions

1. **`-pressure-source` 具體字串格式要不要支援 escaping（`=`、`;`）？**
   - What we know: repeatable flag 是最直覺輸入管道。
   - What's unclear: secret 可能包含分隔符號。
   - Recommendation: Phase 3 先定義「安全字元 contract」，若需完整 escaping 再進下一 phase。

2. **多來源失敗策略是否要新增可配置模式（fail-open/fail-closed）？**
   - What we know: 現有單來源是連續 3 次失敗即 fatal。
   - What's unclear: 多來源下是否允許部分來源長期失效。
   - Recommendation: Phase 3 先固定「all sources unavailable => fatal」，先達成 deterministic 行為。

3. **Windows 實機驗證責任歸屬（CI runner vs 人工）**
   - What we know: 目前環境無 `cmd.exe`。
   - What's unclear: 專案是否已有 Windows CI。
   - Recommendation: plan 要明確加一個 gate 任務，避免 `.bat` 僅靜態存在未執行。

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | PRESSURE-03 implementation/tests | ✓ | go1.24.4 | — |
| Bash | OPS-01 Linux script validation | ✓ | 3.2.57 | `bash -n` syntax check + local run |
| Windows `cmd.exe` | OPS-01 Windows `.bat` runtime validation | ✗ | — | Windows host/runner manual execution |
| Docker | Optional pressure API scenario rehearsal | ✓ | 29.1.3 | local `httptest` unit tests |
| `shellcheck` | Optional shell lint quality | ✗ | — | `bash -n` + peer review |

**Missing dependencies with no fallback:**
- None.

**Missing dependencies with fallback:**
- `cmd.exe` missing on current machine; require Windows execution environment before phase gate.
- `shellcheck` missing; use `bash -n` and tests as fallback.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + `go test` (Go 1.24.4) |
| Config file | none — standard Go test discovery |
| Quick run command | `go test ./pkg/config ./pkg/scanapp ./cmd/port-scan -count=1` |
| Full suite command | `go test ./... && bash scripts/coverage_gate.sh` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PRESSURE-03 | 多來源輸入解析 + 聚合 policy + 舊旗標相容 | unit + integration | `go test ./pkg/config ./pkg/scanapp ./cmd/port-scan -run 'Pressure|MultiSource' -count=1` | ❌ Wave 0 |
| OPS-01 | Linux/Windows 範例腳本存在且參數可用 | smoke + manual | `bash -n scripts/run_multi_pressure_example.sh && test -f scripts/run_multi_pressure_example.bat` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/config ./pkg/scanapp ./cmd/port-scan -count=1`
- **Per wave merge:** `go test ./...`
- **Phase gate:** `go test ./... && bash scripts/coverage_gate.sh` plus one Windows `.bat` run evidence

### Wave 0 Gaps
- [ ] `pkg/config/config_multi_pressure_test.go` — covers PRESSURE-03 CLI parsing and compatibility matrix.
- [ ] `pkg/scanapp/pressure_aggregate_test.go` — covers policy (`max|min|avg`) and per-source failure semantics.
- [ ] `pkg/scanapp/pressure_monitor_multi_source_test.go` — covers pause/resume transitions under mixed source samples.
- [ ] `cmd/port-scan/main_multi_pressure_test.go` — covers CLI wiring to `RunOptions` with multiple sources.
- [ ] `scripts/run_multi_pressure_example.sh` + `scripts/run_multi_pressure_example.bat` — covers OPS-01 artifact requirement.

## Sources

### Primary (HIGH confidence)
- In-repo code and docs:
  - `pkg/config/config.go`
  - `cmd/port-scan/command_handlers.go`
  - `pkg/scanapp/pressure.go`
  - `pkg/scanapp/pressure_monitor.go`
  - `pkg/scanapp/scan.go`
  - `pkg/config/config_test.go`
  - `pkg/scanapp/pressure_test.go`
  - `pkg/scanapp/scan_observability_test.go`
  - `e2e/run_e2e.sh`
  - `.planning/phases/03-support-multiple-pressure-sources-and-provide-cross-platform-run-scripts/03-CONTEXT.md`
  - `.planning/REQUIREMENTS.md`
- Go `flag` package docs (`FlagSet.Func`, `FlagSet.Var`): https://pkg.go.dev/flag
- Microsoft CMD docs:
  - `if`: https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/if
  - `exit`: https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/exit
  - `set`: https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/set_1
- GNU Bash `set` builtin (including `-e`, `-u`, `pipefail`): https://www.gnu.org/software/bash/manual/html_node/The-Set-Builtin.html
- Module metadata verification via `go list -m -json` / `go list -m -versions` (golang.org/x/term, golang.org/x/sys)

### Secondary (MEDIUM confidence)
- None.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - 基於現有 `go.mod`、實機 `go list -m` 與官方 `flag` 文件。
- Architecture: HIGH - 直接對齊現有 `PressureFetcher` 抽象與可運行測試基線（`go test ./...` pass）。
- Pitfalls: MEDIUM - 來自現有實作模式與跨平台腳本文件，部分需實作後驗證。

**Research date:** 2026-04-03  
**Valid until:** 2026-05-03 (30 days)
