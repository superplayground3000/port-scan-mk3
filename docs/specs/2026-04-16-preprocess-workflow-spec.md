# Pre-process Workflow Spec

**Date**: 2026-04-16

## Background

Pre-processing prepares input files for the port-scan tool. It handles two
scan modes with different starting points but the same output: a per-data-center
rich CSV ready for port-scan consumption.

### Input Files

| File | Format | Scope |
|------|--------|-------|
| Filtered targets CSV | Rich CSV (10 columns: `src_ip`, `src_network_segment`, `dst_ip`, `dst_network_segment`, `service_label`, `protocol`, `port`, `decision`, `matched_policy_id`, `reason`) | One per data center, stored at `filtered-targets/<DC>/<timestamp>/opened_targets.csv` |
| Previous opened targets CSV | Minimal CSV (`host`, `port`) | One per data center, stored at `previous-scanned/<DC>/<timestamp>/opened_targets.csv` |
| Cleaned CIDRs CSV | Multiple columns, the columns we need are: `fab`,`segment` (values: CIDR), `status` (values: `open` / `close`) | Covers all data centers |
| CIDR reference list | CSV listing CIDRs | Used for enrichment: maps host IPs to their containing CIDR |
| Service map CSV | Columns: `port`, `service_label` | Used for enrichment: maps port numbers to service names |

### Output

Port-scan-ready input CSV written to:
```
<output-dir>/<fab_name>/<timestamp>/input.csv
```

One file per data center per invocation.

## Scan Modes

### Mode 1: From Scratch

Use when scanning a data center for the first time or starting fresh.

**Flow**: Filtered targets CSV (rich) + cleaned CIDRs CSV -> preprocess filter -> output.

The preprocess tool filters out any target whose `dst_network_segment` is
contained within a CIDR marked `close` in the cleaned CIDRs file. Containment
is checked using interval tree queries (a closed `10.0.0.0/16` drops targets in
`10.0.1.0/24`).

### Mode 2: Re-scan

Use when re-scanning previously discovered open targets.

**Flow**: Opened targets CSV (`host,port`) -> enrich-targets tool -> enriched
rich CSV -> preprocess filter -> output.

The enrichment step fills in the missing rich CSV fields using reference data
and placeholder values before the same filtering logic applies.

#### Enrichment Field Mapping

| Field | Source |
|-------|--------|
| `src_ip` | Placeholder: `10.59.42.39` |
| `src_network_segment` | Placeholder: `10.59.42.39/32` |
| `dst_ip` | `host` column from opened targets |
| `dst_network_segment` | Smallest containing CIDR from reference list (fallback: `<host>/32`) |
| `service_label` | Lookup from service map (fallback: `unknown`) |
| `protocol` | `tcp` |
| `port` | `port` column from opened targets |
| `decision` | `accept` |
| `matched_policy_id` | `enriched` |
| `reason` | `MATCH_POLICY_ACCEPT` |

## Tools

### `enrich-targets`

Enriches minimal `host,port` CSV into rich CSV format.

```
enrich-targets \
  --input opened_targets.csv \
  --cidr-list cidrs.csv \
  --service-map services.csv \
  --output enriched.csv
```

### `preprocess`

Filters rich CSV by removing targets in closed CIDRs, writes port-scan input.

```
preprocess \
  --input rich_targets.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./output
```

## Design Reference

Full design with package layout, types, and testing strategy:
[2026-04-16-preprocess-workflow-design.md](../plans/2026-04-16-preprocess-workflow-design.md)
