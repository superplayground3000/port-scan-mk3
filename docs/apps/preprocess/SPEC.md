# preprocess Specification

**Tool**: `cmd/preprocess` | **Revised**: 2026-08-09

## Overview

`preprocess` filters a rich CSV by removing targets whose `dst_network_segment` falls within any closed CIDR. It is used in the port-scan-mk3 pipeline between enrichment and scanning.

## Usage

```bash
preprocess --input=<path> --cleaned-cidrs=<path> --fab-name=<name> --output-dir=<path>
```

## CLI Flags

All four flags are required.

| Flag | Description |
|------|-------------|
| `--input` | Path to rich CSV input |
| `--cleaned-cidrs` | Path to cleaned CIDRs CSV (fab,segment,status) |
| `--fab-name` | Data center / fabric name (used to filter closed CIDRs) |
| `--output-dir` | Base output directory |

### `--fab-name` constraints

The fab name becomes a directory under `--output-dir`, so it must be a single
safe path component. It is validated before any input file is opened, and an
unusable value is rejected with an error naming the flag rather than being
sanitized. The rules are the strictest of Linux and Windows and apply on every
platform, because output written on one is routinely read on the other.

Rejected:

- Path separators (`/` or `\`), absolute, drive-relative and UNC paths, and the
  relative elements `.` and `..`.
- Characters Windows forbids in a name: `< > : " | ? *` and control characters
  `0x00`–`0x1F`.
- A trailing dot or space — Windows strips them, silently renaming the output
  directory.
- Windows reserved device names: `CON`, `PRN`, `AUX`, `NUL`, `CONIN$`,
  `CONOUT$`, `COM0`–`COM9`, `LPT0`–`LPT9`, `COM¹`, `COM²`, `COM³`, `LPT¹`,
  `LPT²`, and `LPT³`.
- The reserved-name rule ignores case and includes extension forms. Thus,
  `com¹.txt` is rejected as well as `COM¹`.
- Windows removes trailing spaces and dots from the stem before it matches a
  device. Thus, the tool also rejects padded forms such as `CONOUT$ .log`.

Microsoft lists [`CONIN$` and `CONOUT$`](https://learn.microsoft.com/en-us/windows/win32/devnotes/rtlisdosdevicename_u)
as DOS device names. [`CreateFile`](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilea#consoles)
also opens them as console devices. Therefore, the validator rejects them and
their extension or padded-stem forms on every platform.

Microsoft also lists the [superscript COM and LPT forms](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file#naming-conventions)
as reserved names in each directory.

Accepted: letters (including non-ASCII, e.g. `fab 12 東京`), digits, interior
spaces and dots, and the usual punctuation — `dc-east`, `fab_01`, `fab.v2`.

## Input Formats

### Rich CSV Input

Expects a header row with `dst_network_segment` column. Column name matching is case-insensitive.

### Cleaned CIDRs CSV

| Column | Description |
|--------|-------------|
| `fab` | Fabric/data center name |
| `segment` | CIDR notation |
| `status` | `open` or `close` |

Only rows with `status=close` (case-insensitive) and matching `fab-name` are loaded into the filter tree.

**Example:**
```csv
fab,segment,status
dc-east,10.0.0.0/8,close
dc-east,192.168.0.0/16,close
dc-west,10.0.0.0/8,open
```

## Output

Written to:
```
<output-dir>/<fab-name>/<YYYYMMDDTHHMMSSZ>/input.csv
```

The header row is passed through from the input. Only rows whose `dst_network_segment` is **not** contained within any closed CIDR are written.

## Filter Logic

For each input row:
1. Parse `dst_network_segment` as CIDR
2. Query closed CIDR tree for containment
3. If contained in any closed CIDR → **drop**
4. If not contained → **keep**

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Filter succeeded, output file written |
| `1` | Runtime error (file open, parse, write failure) |

## Building and Testing

```bash
# Build
go build -o preprocess ./cmd/preprocess

# Test
go test ./pkg/preprocess/...
```
