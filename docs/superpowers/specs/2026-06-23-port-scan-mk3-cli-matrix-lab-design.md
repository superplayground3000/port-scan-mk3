# Design Spec — port-scan-mk3 CLI Matrix Lab

**Date:** 2026-06-23
**Topic slug:** `port-scan-mk3-cli-matrix`
**Lab location:** `labs/port-scan-mk3-cli-matrix/`
**Skill:** research-lab (Stage 3 escalates to writing-plans — 3+ distinct service types)

---

## Property demonstrated (one sentence)

> port-scan-mk3's five CLI binaries produce contract-conformant output across their full documented flag surface — correct port-state classification (`open` / `close` / `close(timeout)` / `unreachable`), correct pressure-driven pause/resume, and correct CSV/CIDR transforms — when run against deterministic mock targets and pressure APIs.

**Coverage interpretation (user-confirmed):** *Comprehensive* — every flag exercised at least once, plus all meaningful interaction combinations. Not an exhaustive cartesian product (combinatorially infeasible, many invalid combos), not per-flag isolation only.

---

## Subject under test (5 binaries, ~50 flags)

Source of truth: `pkg/config/config.go:112-134` (port-scan) and `cmd/*/main.go` for the helpers.

| Binary | Subcommands | Notes |
|---|---|---|
| `port-scan` | `validate`, `scan` | 24 flags; basic mode (cidr+port file) vs rich mode (auto-detected by firewall-policy columns) |
| `preprocess` | — | filter rich CSV by closed CIDRs (`--input/--cleaned-cidrs/--fab-name/--output-dir`) |
| `enrich-targets` | — | minimal CSV → 10-col rich CSV (`--input/--cidr-list/--service-map/--output`) |
| `cidr-compare` | — | deny vs open CIDR containment (`-deny-file/-open-file`, env `CIDR_COMPARE_*`) |
| `csv-transform` | — | legacy results → rich CSV (`--input/--output/--host-col/--port-col/--pass-col/--sheet`, env `TRANSFORM_*`) |

---

## Topology

3 distinct images, instantiated as **9 services** on a named bridge network with static IPs (so fixture CSVs can reference target IPs). All host ports ≥ 15000. Healthcheck on every service. `scanner` uses `depends_on: { condition: service_healthy }` for all dependencies.

### Images

1. **`mock-target`** (Go) — opens a configurable set of TCP ports; optional `FILTERED_PORTS` env installs in-container `iptables DROP` rules (requires `cap_add: [NET_ADMIN]`) to produce a real connect timeout.
2. **`mock-pressure`** (Go) — adapted from `e2e/mock-pressure-api/main.go`. Adds a dedicated **non-consuming** health endpoint (`/healthz`) so healthchecks never advance the pressure sequence (`main.go:58-74` advances on every GET to the data path).
3. **`scanner`** (built from repo root, multi-stage) — all 5 binaries on PATH; **installs `bash` + `iputils-ping`** (existing `e2e/scanner/Dockerfile` lacks both); non-root user; mounts `fixtures/` read-only and `out/` read-write; entrypoint runs `scripts/smoke-test.sh` (the matrix driver).

### Services

| Service | Image | Static IP | Role / config | Drives |
|---|---|---|---|---|
| `target-open` | mock-target | .10 | listens on test ports | `status=open` |
| `target-closed` | mock-target | .11 | no listener (RST) | `status=close` |
| `target-filtered` | mock-target | .12 | `NET_ADMIN` + iptables DROP | `status=close(timeout)` |
| `pressure-ok` | mock-pressure | .20 | low static pressure | scan proceeds |
| `pressure-high` | mock-pressure | .21 | sequence high→low | pause **then** resume |
| `pressure-5xx` | mock-pressure | .22 | `MODE=fail` | resilience |
| `pressure-timeout` | mock-pressure | .23 | `MODE=timeout` | resilience |
| `pressure-auth-1` | mock-pressure | .24 | `USE_AUTH=true`, OAuth + `/data` | auth single-source |
| `pressure-auth-2` | mock-pressure | .25 | `USE_AUTH=true`, OAuth + `/data` | auth **multi-source** (with -1) |

> **Why two auth containers:** the mock exposes a single `/data` path (`mock-pressure-api/main.go:212-217,399-430`). `MultiSourcePressureFetcher` only activates when `-pressure-data-url` is comma-separated with **distinct** URLs, so two containers are required to exercise that code path and its max-aggregation + per-source health behavior.

---

## Design decisions (Codex-reviewed)

These five were raised in an early cross-model (Codex) design review, grounded in source. Each is resolved in this design:

1. **Timeout simulation = `cap_add: NET_ADMIN` + `iptables -A INPUT -p tcp --dport <p> -j DROP`** inside `target-filtered`. `DROP` (not `REJECT`) is required so the SYN is silently swallowed → real `connect()` timeout → `close(timeout)`. The image must include `iptables`. This is container-namespace only — no host OS change. Rules applied at container entrypoint before the listener loop.
2. **Pre-scan ping interferes with timeout tests.** `reachability.go:69-79` treats a nonzero ping exit as `unreachable`, short-circuiting the TCP phase. Therefore the matrix is split:
   - **Reachability group** — exercises the default ping filter and asserts `status=unreachable` for an unreachable host.
   - **TCP-state group** — always passes `-disable-pre-scan-ping` so `open` / `close` / `close(timeout)` are actually reached and assertable.
3. **Resume.** `state.go:81-145` persists no `execution_key`; `chunk_lifecycle.go:123-141` aborts when `total_count` ≠ input fixture size. Two cases:
   - **(a) authentic** — start a real scan in background, send `SIGINT`, let it write `resume_state.json`, then re-run with `-resume <that file>`; assert completed chunks are skipped. Faithful.
   - **(b) crafted** — a checked-in resume file whose `total_count` matches its fixture exactly; assert resume loads and skips. Deterministic backstop.
4. **Multi-source auth** → two distinct auth containers (see topology note).
5. **Healthchecks must not consume the pressure sequence** (`main.go:58-74`). All pressure healthchecks hit `/healthz` (added to the mock), never `/api/pressure` or `/data`.

---

## Flag matrix (36 named cases; each asserts the *property*, not just exit code)

Case counts: A=4, B=9, C=2, D=8, E=2, F=2, G=2, H=3, I=4 → **36**.

Driver: `scripts/smoke-test.sh`. Each case is a named function that runs a binary, then greps/parses the produced artifact (CSV / JSON / stdout) and asserts the expected observable. A single failed assertion fails the lab.

### A. `port-scan validate` (4)
| # | Flags | Assertion |
|---|---|---|
| A1 | `validate -cidr-file basic.csv -port-file ports.csv -format human` | human report, exit 0 |
| A2 | `validate ... -format json` | stdout parses as JSON, `valid:true` |
| A3 | `validate -cidr-file rich.csv -format json` | rich mode recognized, exit 0 |
| A4 | `validate -cidr-file malformed.csv -format json` | nonzero exit, error surfaced in JSON |

### B. `port-scan scan` — modes & I/O (TCP-state group, all `-disable-pre-scan-ping`) (9)
| # | Flags | Assertion |
|---|---|---|
| B1 | basic mode, `-disable-api -disable-pre-scan-ping` | `target-open` ports → `status=open` in `opened_results-*.csv` |
| B2 | same | `target-closed` → `status=close` |
| B3 | `-timeout 300ms -disable-api -disable-pre-scan-ping` | `target-filtered` → `status=close(timeout)` |
| B4 | rich-mode fixture (auto-detect), `-disable-api -disable-pre-scan-ping` | only `decision=accept` tcp rows scanned; 14-col schema correct |
| B5 | `-cidr-ip-col source_ip -cidr-ip-cidr-col source_range` + custom-header fixture | custom columns parsed; open found |
| B6 | `-bucket-rate 500 -bucket-capacity 500 -delay 5ms -workers 20` | completes; open found (rate flags accepted) |
| B7 | `-output /out/custom/run.csv` | files written as `run-<ts>.csv` + `opened_results-<ts>.csv` under that dir |
| B8 | `-log-level debug -format json -disable-api` | debug logs present on stderr |
| B9 | `-quiet -disable-api` | console logs suppressed; results still written |

### C. Reachability group (default ping) (2)
| # | Flags | Assertion |
|---|---|---|
| C1 | default (ping enabled), target reachable | `target-open` scanned, open found |
| C2 | default (ping enabled), unreachable host in fixture | `status=unreachable`, TCP phase skipped |

### D. Pressure control (8)
| # | Flags | Assertion |
|---|---|---|
| D1 | `-pressure-api http://pressure-ok:.../api/pressure -pressure-interval 1s` | scan completes, open found |
| D2 | `-pressure-api http://pressure-high/... ` (seq high→low) | stderr shows pause then resume; scan completes |
| D3 | `-pressure-api http://pressure-5xx/...` | scan handles 5xx without crash (defined resilience) |
| D4 | `-pressure-api http://pressure-timeout/...` | scan handles timeout without crash |
| D5 | `-disable-api` | no pressure polling; completes on local rate control |
| D6 | `-pressure-use-auth -pressure-auth-url .../auth -pressure-data-url http://pressure-auth-1/data -pressure-client-id ID -pressure-client-secret SECRET` | OAuth exchange succeeds; scan completes |
| D7 | multi-source: `-pressure-data-url "http://pressure-auth-1/data,http://pressure-auth-2/data"` | both polled; max aggregation; per-source health visible |
| D8 | auth with bad credentials | startup fails clearly (negative case) |

### E. Resume (2)
| # | Flags | Assertion |
|---|---|---|
| E1 | authentic: scan → SIGINT → `-resume resume_state.json` | resumed run skips completed chunks |
| E2 | crafted resume file (total_count matched) | loads, skips, completes |

### F. `preprocess` (2)
| # | Flags | Assertion |
|---|---|---|
| F1 | `--input rich.csv --cleaned-cidrs policy.csv --fab-name fab1 --output-dir /out` | rows in closed CIDRs removed; output is timestamped `fab1-<ts>.csv` |
| F2 | fab-name with no matches | output empty/headers only, exit 0 |

### G. `enrich-targets` (2)
| # | Flags | Assertion |
|---|---|---|
| G1 | `--input minimal.csv --cidr-list cidrs.csv --service-map services.csv --output /out/enriched.csv` | 10-col rich CSV; segment + service_label populated |
| G2 | minimal CSV missing required `host`/`port` | clear error, nonzero exit |

### H. `cidr-compare` (3)
| # | Flags | Assertion |
|---|---|---|
| H1 | `-deny-file deny.csv -open-file open.csv` | stdout `deny_cidr,open_cidr` containment rows correct |
| H2 | env form `CIDR_COMPARE_DENY_FILE=... CIDR_COMPARE_OPEN_FILE=...` (no flags) | same output as H1 |
| H3 | no containment overlap | header only, exit 0 |

### I. `csv-transform` (4)
| # | Flags | Assertion |
|---|---|---|
| I1 | `--input legacy.csv --output /out/t.csv` (defaults) | 10-col rich CSV produced |
| I2 | `--host-col H --port-col P --pass-col Result` custom columns | custom columns honored |
| I3 | rows with `Pass the test=TRUE` (case-insensitive) | those rows skipped in output |
| I4 | env form `TRANSFORM_INPUT/TRANSFORM_OUTPUT/...` | same as I1 |

**Flag coverage check:** every flag in `pkg/config/config.go:112-134` and every helper flag appears in at least one case above. Multi-value/interaction flags (`-pressure-data-url` comma list, rich-vs-basic auto-detect, env-vs-flag forms, ping-on-vs-off) each have dedicated cases.

---

## Fixtures (`fixtures/`)

- `basic.csv` — `ip,ip_cidr,port` referencing `target-open/.10`, `target-closed/.11`, `target-filtered/.12`.
- `basic-custom-headers.csv` — same data, columns `source_ip,source_range` (for B5).
- `ports.csv` — `port/tcp` lines for the ports the targets open + filtered ports.
- `rich.csv` — full 10-col firewall-policy schema, mix of `decision=accept`/`deny`, tcp/udp (for B4, F1).
- `unreachable.csv` — includes an IP with no container (for C2).
- `minimal.csv`, `cidrs.csv`, `services.csv` — for enrich-targets.
- `deny.csv`, `open.csv` — for cidr-compare.
- `legacy.csv` — for csv-transform (incl. a `Pass the test=TRUE` row).
- `resume_state.json` (+ matching fixture) — for E2.
- `policy.csv` — cleaned CIDRs for preprocess.

---

## Validation

- `scripts/smoke-test.sh` **is** the matrix driver and the lab's property check (not a liveness probe). Prints a per-case PASS/FAIL table; nonzero exit on any failure.
- `bash <skill-root>/scripts/validate_lab.sh labs/port-scan-mk3-cli-matrix` must exit 0.
- Post-run: no leftover containers/volumes; `git status` clean of debug artifacts; `out/` is the only generated tree and is gitignored.
- `.env.example` documents every env var (target ports, filtered ports, pressure values/sequences, auth credentials).
- `README.md` per lab-layout: what property, how to run, how to read the matrix table, teardown.

---

## Host-isolation compliance

- Containers only. No `sudo`, no host package installs, no writes to `/etc /usr /var`, no sysctl/kernel/systemd/shell-rc changes.
- `cap_add: [NET_ADMIN]` is a **container** capability (network namespace scoped), not a host modification — used solely by `target-filtered` for in-container iptables DROP.
- All host ports ≥ 15000. Pinned image tags (no `:latest`). Non-root users in custom images.

---

## Deliberately excluded (obvious next questions this lab does not address)

- **Exhaustive cartesian product** of all flag values — infeasible and largely invalid; comprehensive interaction coverage chosen instead.
- **Real external pressure APIs / real OAuth providers** — mocked; the lab proves the client contract, not third-party behavior.
- **Performance / throughput benchmarking** of the scanner — this lab proves correctness, not speed; rate-control flags are exercised for acceptance, not load-tested.
- **UDP scanning** — the tool scans TCP only (`protocol` must be `tcp`); UDP rows are filtered, asserted in B4, not otherwise covered.
- **IPv6 targets** — out of scope; fixtures are IPv4.
- **Resume after partial chunk corruption** — only clean SIGINT resume (E1) and matched crafted state (E2) are covered.
- **The repo's own `e2e/` suite** — this lab is additive and self-contained; it does not modify or replace `e2e/`.
