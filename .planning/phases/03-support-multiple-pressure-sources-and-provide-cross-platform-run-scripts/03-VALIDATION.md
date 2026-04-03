---
phase: 03
slug: support-multiple-pressure-sources-and-provide-cross-platform-run-scripts
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-03
---

# Phase 03 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none |
| **Quick run command** | `go test ./pkg/config ./pkg/scanapp -run Pressure -count=1` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~120 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/config ./pkg/scanapp -run Pressure -count=1`
- **After every plan wave:** Run `go test ./...`
- **Before `$gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 03-01-01 | 01 | 1 | PRESSURE-03 | unit | `go test ./pkg/config -run Pressure -count=1` | ❌ W0 | ⬜ pending |
| 03-01-02 | 01 | 1 | PRESSURE-03 | unit | `go test ./pkg/scanapp -run Pressure -count=1` | ❌ W0 | ⬜ pending |
| 03-02-01 | 02 | 2 | OPS-01 | integration | `bash -n scripts/examples/run_multi_pressure_linux.sh` | ❌ W0 | ⬜ pending |
| 03-02-02 | 02 | 2 | OPS-01 | manual+lint | `cmd /c scripts\\examples\\run_multi_pressure_windows.bat` (Windows host) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/config/config_multi_pressure_test.go` — CLI parser and backward-compat behavior for multi-source flags
- [ ] `pkg/scanapp/multi_pressure_fetcher_test.go` — aggregation policy and failure semantics tests
- [ ] `scripts/examples/run_multi_pressure_linux.sh` — shell syntax check target
- [ ] `scripts/examples/run_multi_pressure_windows.bat` — Windows sample script target

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Windows batch execution path | OPS-01 | Current host has no `cmd.exe`; cannot execute `.bat` natively | On Windows runner/host, set placeholder env/args and run `cmd /c scripts\\examples\\run_multi_pressure_windows.bat`; verify non-zero exit on bad args and expected command invocation on valid args |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
