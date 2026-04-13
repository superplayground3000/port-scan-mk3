# port-scan

CLI entry point for the TCP port scanner. See the [main README](../README.md) for full documentation.

## Commands

### validate

Parse and validate input files without running a network scan.

```bash
port-scan validate -cidr-file targets.csv -port-file ports.csv
port-scan validate -cidr-file targets.csv -port-file ports.csv -format json
```

### scan

Run the full scan pipeline with pressure-aware pacing and resume support.

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv
port-scan scan -cidr-file targets.csv -port-file ports.csv -output ./results
port-scan scan -cidr-file targets.csv -port-file ports.csv -format json -log-level debug
```

## Architecture

```
CLI entry point (main.go)
    │
    ├── handleValidateCommand → config.Parse → validate.Inputs → cli.WriteValidation
    │
    └── handleScanCommand → config.Parse → scanapp.Run
                                 │
                                 ├── Load CIDR/port inputs
                                 ├── Expand tasks (CIDR × Port)
                                 ├── Worker pool with rate control
                                 ├── Pressure-aware pacing
                                 └── Batch writer → scan_results-*.csv, opened_results-*.csv
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Validation failed (validate) or scan runtime error (scan) |
| `2` | CLI parsing or configuration error |
| `130` | Scan canceled by SIGINT (Ctrl+C) |

## Quick Reference

```bash
# Validate only (no scan)
port-scan validate -cidr-file targets.csv -port-file ports.csv -format human

# Full scan with custom workers and timeout
port-scan scan -cidr-file targets.csv -port-file ports.csv -workers 20 -timeout 500ms

# Scan with pressure control
port-scan scan -cidr-file targets.csv -port-file ports.csv -pressure-api http://localhost:8080/api/pressure -pressure-interval 5s

# Resume from interrupted scan
port-scan scan -cidr-file targets.csv -port-file ports.csv -resume ./results/resume_state.json
```

For the complete flag reference and pipeline details, see [README.md](../README.md).

---
**Revised**: 2026-04-13 | **Author**: docs-team