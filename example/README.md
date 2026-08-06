# Examples

Ready-to-run example inputs for every CLI tool under [`cmd/`](../cmd). Each tool has its
own folder with sample input files. You can **copy each command below into a terminal at
the repository root** and run it without a change. The generated output goes to
[`out/`](out) (git-ignored).

```
example/
├── port-scan/       # cidr.csv, ports.csv          → port-scan validate | scan
├── csv-transform/   # scan-results.csv             → csv-transform
├── enrich-targets/  # opened-targets.csv, ...      → enrich-targets
├── preprocess/      # rich-targets.csv, ...        → preprocess
├── cidr-compare/    # deny.csv, open.csv           → cidr-compare
└── out/             # generated output (git-ignored)
```

Prerequisite: Go `1.24.x` installed (`go version`). You do not need Docker or network access —
the only command that touches the network (`port-scan scan`) targets `127.0.0.1` by default.

---

## How the tools fit together

```
                 enrich-targets ─┐
  (host,port) ───────────────────┤
                                  ├─► rich CSV ─► preprocess ─► port-scan scan
  (spreadsheet) ── csv-transform ─┘                 ▲
                                                     │ (drop targets in closed CIDRs)
  cidr-compare: standalone overlap report (deny vs open CIDR lists)
```

- **`csv-transform`** and **`enrich-targets`** both produce the canonical **rich CSV** that the
  scanner consumes. Use the tool that matches your source data.
- **`preprocess`** filters a rich CSV. It removes each target whose `dst_network_segment` is
  inside a *closed* CIDR.
- **`port-scan`** validates the inputs and runs the TCP scan.
- **`cidr-compare`** is a standalone utility that reports which *open* CIDRs overlap a *deny* list.

---

## 1. `port-scan` — validate inputs and scan

`port-scan` takes a **CIDR CSV** (`ip`, `ip_cidr` columns) and a **port file** (one `<port>/tcp`
per line).

Input files: [`port-scan/cidr.csv`](port-scan/cidr.csv), [`port-scan/ports.csv`](port-scan/ports.csv)

```csv
ip,ip_cidr
127.0.0.1,127.0.0.1/32
127.0.0.2,127.0.0.2/32
```
```
22/tcp
80/tcp
443/tcp
```

### Validate only (no network traffic)

```bash
go run ./cmd/port-scan validate \
  -cidr-file example/port-scan/cidr.csv \
  -port-file example/port-scan/ports.csv \
  -format human
```

Output:

```
valid=true detail=ok
```

JSON format:

```bash
go run ./cmd/port-scan validate \
  -cidr-file example/port-scan/cidr.csv \
  -port-file example/port-scan/ports.csv \
  -format json
```

```json
{"detail":"ok","valid":true}
```

### Run a scan against localhost

`-disable-api` skips the pressure-control API. `-disable-pre-scan-ping` skips the reachability
pre-check. `-output` sends the timestamped result files to `example/out/`:

```bash
go run ./cmd/port-scan scan \
  -cidr-file example/port-scan/cidr.csv \
  -port-file example/port-scan/ports.csv \
  -disable-api=true \
  -disable-pre-scan-ping=true \
  -timeout 500ms \
  -output example/out/scan.csv
```

Structured progress logs stream to stderr. The last line is a summary:

```
[INFO] scan_completion fields=map[close_count:4 ... open_count:2 ... total_tasks:6]
```

`port-scan` writes three timestamped CSVs to `example/out/`:
`scan_results-<ts>.csv` (all probes), `opened_results-<ts>.csv` (open ports only), and
`unreachable_results-<ts>.csv`. Example `opened_results`:

```csv
ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,matched_policy_id,reason,execution_key,src_ip,src_network_segment
127.0.0.1,127.0.0.1/32,22,open,0,,,,,,,,,
127.0.0.2,127.0.0.2/32,22,open,0,,,,,,,,,
```

> The open and closed results depend on the services that listen on `127.0.0.1`. To stop the
> per-probe logs, add `-quiet=true`.

---

## 2. `csv-transform` — spreadsheet results → rich CSV

`csv-transform` converts a human spreadsheet export (columns `Host`, `Port`, `Pass the test`)
into a rich CSV. It keeps only the rows where **`Pass the test` is `FALSE`** (that is, the
targets that failed and need a new scan). The `Port` column can hold several ports separated
by `/` (for example, `443/8443`). Each of these ports becomes its own output row.

Input file: [`csv-transform/scan-results.csv`](csv-transform/scan-results.csv)

```csv
Host,Port,Pass the test
192.168.10.5,8080,FALSE
192.168.10.6,443/8443,FALSE
192.168.10.7,22,TRUE
10.20.0.9,53,FALSE
```

```bash
go run ./cmd/csv-transform \
  --input example/csv-transform/scan-results.csv \
  --output example/out/csv-transform.rich.csv
```

`csv-transform` skips the `TRUE` row and writes a message to stderr. `443/8443` expands to two rows:

```csv
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
10.0.0.1,10.0.0.0/24,192.168.10.5,10.0.0.0/24,unknown,tcp,8080,accept,transformed,MATCH_POLICY_ACCEPT
10.0.0.1,10.0.0.0/24,192.168.10.6,10.0.0.0/24,unknown,tcp,443,accept,transformed,MATCH_POLICY_ACCEPT
10.0.0.1,10.0.0.0/24,192.168.10.6,10.0.0.0/24,unknown,tcp,8443,accept,transformed,MATCH_POLICY_ACCEPT
10.0.0.1,10.0.0.0/24,10.20.0.9,10.0.0.0/24,unknown,tcp,53,accept,transformed,MATCH_POLICY_ACCEPT
```

You can override the column names with `--host-col`, `--port-col`, and `--pass-col`.

---

## 3. `enrich-targets` — minimal `host,port` → rich CSV

`enrich-targets` makes a rich CSV from a minimal `host,port` list. It reads the network segment
of each host from a **CIDR reference list**. It also maps each port to a **service label**.

Input files:
[`enrich-targets/opened-targets.csv`](enrich-targets/opened-targets.csv),
[`enrich-targets/cidr-list.csv`](enrich-targets/cidr-list.csv),
[`enrich-targets/service-map.csv`](enrich-targets/service-map.csv)

```csv
host,port            cidr            port,service_label
172.30.0.10,8080     172.30.0.0/24   8080,http-alt
172.30.0.11,443      10.0.0.0/8      443,https
172.30.0.12,53                       53,dns
```

```bash
go run ./cmd/enrich-targets \
  --input example/enrich-targets/opened-targets.csv \
  --cidr-list example/enrich-targets/cidr-list.csv \
  --service-map example/enrich-targets/service-map.csv \
  --output example/out/enrich-targets.rich.csv
```

`enrich-targets` fills the `dst_network_segment` of each host from the matching CIDR, and each
port gets its service label. `src_ip` and `src_network_segment` are fixed placeholder defaults
(`10.59.42.39`, from `pkg/preprocesscfg`). Thus the output is the same on every run:

```csv
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
10.59.42.39,10.59.42.39/32,172.30.0.10,172.30.0.0/24,http-alt,tcp,8080,accept,enriched,MATCH_POLICY_ACCEPT
10.59.42.39,10.59.42.39/32,172.30.0.11,172.30.0.0/24,https,tcp,443,accept,enriched,MATCH_POLICY_ACCEPT
10.59.42.39,10.59.42.39/32,172.30.0.12,172.30.0.0/24,dns,tcp,53,accept,enriched,MATCH_POLICY_ACCEPT
```

---

## 4. `preprocess` — filter out targets in closed CIDRs

`preprocess` filters a rich CSV. It removes each row whose `dst_network_segment` is inside a
CIDR marked `close` for the given fab. `preprocess` writes the output to
`<output-dir>/<fab-name>/<timestamp>/input.csv` (a scan-ready rich CSV).

Input files:
[`preprocess/rich-targets.csv`](preprocess/rich-targets.csv),
[`preprocess/cleaned-cidrs.csv`](preprocess/cleaned-cidrs.csv)

```csv
# cleaned-cidrs.csv (fab,segment,status)
fab,segment,status
fab-east,172.30.0.12/32,close
fab-east,172.30.0.20/32,open
```

```bash
go run ./cmd/preprocess \
  --input example/preprocess/rich-targets.csv \
  --cleaned-cidrs example/preprocess/cleaned-cidrs.csv \
  --fab-name fab-east \
  --output-dir example/out/preprocess
```

`preprocess` removes the target on the closed `172.30.0.12/32` segment. It keeps the other two:

```
Filter summary:
  Total input rows:  3
  Rows kept:         2
  Rows dropped:      1
Output written to: example/out/preprocess/fab-east/<timestamp>/input.csv
```

Feed that `input.csv` directly into the scanner. A rich CSV has no `ip` or `ip_cidr` columns.
Instead, `port-scan` detects the rich header (`src_ip`, `dst_ip`, `dst_network_segment`,
…) and reads the target IPs from it. Thus you do not need the `-cidr-ip-col` flags:

```bash
# Replace <timestamp> with the directory printed above
go run ./cmd/port-scan scan \
  -cidr-file example/out/preprocess/fab-east/<timestamp>/input.csv \
  -port-file example/port-scan/ports.csv \
  -disable-api=true \
  -disable-pre-scan-ping=true \
  -output example/out/scan.csv
```

---

## 5. `cidr-compare` — find open CIDRs that overlap a deny list

`cidr-compare` is a standalone utility. It takes a **deny list** (`dst_network_segment`,
`decision`) and an **open list** (`segment`, `status`). It prints every open CIDR that
overlaps a denied CIDR.

Input files:
[`cidr-compare/deny.csv`](cidr-compare/deny.csv),
[`cidr-compare/open.csv`](cidr-compare/open.csv)

```csv
# deny.csv                       # open.csv
dst_network_segment,decision     segment,status
172.30.0.0/24,deny               172.30.0.12/32,open
10.0.0.0/8,deny                  10.5.5.5/32,open
192.168.1.0/24,allow             8.8.8.0/24,open
```

```bash
go run ./cmd/cidr-compare \
  --deny-file example/cidr-compare/deny.csv \
  --open-file example/cidr-compare/open.csv
```

`cidr-compare` reports on stdout each open CIDR that a denied CIDR contains. It omits
`8.8.8.0/24`, because that CIDR matches nothing:

```csv
deny_cidr,open_cidr
172.30.0.0/24,172.30.0.12/32
10.0.0.0/8,10.5.5.5/32
```

---

## Run everything at once

```bash
go run ./cmd/csv-transform   --input example/csv-transform/scan-results.csv --output example/out/csv-transform.rich.csv
go run ./cmd/enrich-targets  --input example/enrich-targets/opened-targets.csv --cidr-list example/enrich-targets/cidr-list.csv --service-map example/enrich-targets/service-map.csv --output example/out/enrich-targets.rich.csv
go run ./cmd/preprocess      --input example/preprocess/rich-targets.csv --cleaned-cidrs example/preprocess/cleaned-cidrs.csv --fab-name fab-east --output-dir example/out/preprocess
go run ./cmd/cidr-compare    --deny-file example/cidr-compare/deny.csv --open-file example/cidr-compare/open.csv
go run ./cmd/port-scan       validate -cidr-file example/port-scan/cidr.csv -port-file example/port-scan/ports.csv -format human
go run ./cmd/port-scan       scan -cidr-file example/port-scan/cidr.csv -port-file example/port-scan/ports.csv -disable-api=true -disable-pre-scan-ping=true -timeout 500ms -output example/out/scan.csv
```

See [`../docs/cli/flags.md`](../docs/cli/flags.md) for the full flag reference and
[`../docs/cli/scenarios.md`](../docs/cli/scenarios.md) for advanced scenarios (resume,
pressure control, authenticated API).
