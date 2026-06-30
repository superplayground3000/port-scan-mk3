# Research: port-scan-mk3 pre-scan ping timeout

## Topic

port-scan-mk3 runs an optional pre-scan reachability stage that ICMP-pings each unique
target IP and excludes the unreachable ones before the TCP connect scan. This lab
exercises the new `-pre-scan-ping-timeout` flag that makes that ping budget configurable.

## Property demonstrated

`-pre-scan-ping-timeout` is the budget the scanner waits for a pre-scan ICMP reply before
excluding a host from the TCP scan: an ICMP-unanswered host (with no container behind its
IP) is dropped from the scan and written to `unreachable_results` with reason
`ping failed within <flag>`, and that reason text tracks the flag value (100ms default,
200ms, 1s) across runs.

## Concept summary

- The pre-scan stage collects the unique target IPs, pings each (`ping -c 1 <ip>` on Unix),
  and builds a "reachable" predicate; unreachable IPs are filtered out before any TCP dial.
- The per-IP ping is bounded by a Go `context` deadline equal to `cfg.PreScanPingTimeout`.
  When the deadline fires first, the host is classified **unreachable** (not a fatal error).
- The unreachable rows are written to a timestamped `unreachable_results-<ts>.csv` batch with
  a `reason` column; the scan's `scan_results-*.csv` only ever contains reachable hosts.
- The timeout default lives solely in `config.Parse` (`100ms`, validated `> 0`); the domain
  code reads `cfg.PreScanPingTimeout` and formats the reason as `ping failed within <d>`.
- A same-subnet IP with no host never answers ARP, so `ping` blocks until the context
  deadline kills it — making the wait and the reason text track the flag, as long as the
  flag is below the kernel's ~3s ARP-resolution window (this lab uses ≤ 1s).

## Wire / API contract

- Transport under test: ICMP echo (pre-scan ping) + TCP connect (scan), standard library.
- Scanner CLI (`port-scan scan`), flags used by this lab:
  - `-cidr-file <csv>` — targets, columns `ip,ip_cidr` (basic mode).
  - `-port-file <csv>` — ports, e.g. `8080/tcp`.
  - `-output <dir>/scan_results.csv` — batch base dir; `scan_results-<ts>.csv`,
    `opened_results-<ts>.csv`, and `unreachable_results-<ts>.csv` are written beside it.
  - `-pre-scan-ping-timeout <duration>` — pre-scan ping budget (default `100ms`, must be `> 0`).
  - `-disable-api -workers N -quiet` — drop pressure API, set concurrency, quiet logs.
- Output schema (unreachable batch): `ip,ip_cidr,status,reason,...`; `status=unreachable`,
  `reason=ping failed within <flag>`.

## Design decisions

- Pinned images: `golang:1.24-alpine` (build), `alpine:3.20` (runtime) — match the repo's
  existing scanner/mock-target images; Go 1.24 is the project's required runtime.
- Single server (`target-open`, pingable, TCP 8080 open) + single client (`scanner`, built
  from the repo) — the minimum topology that shows both sides of the gate.
- Unreachable host = the unassigned IP `172.31.0.99` on the lab subnet (chosen over an
  ICMP-blackhole container): no extra service or `NET_ADMIN`; ARP never resolves, so the
  wait is governed by the flag deadline for flag values < ~3s.
- Scanner keeps `NET_RAW` only — needed for ICMP sockets; container-scoped, no host change.
- Three runs (default / 200ms / 1s) rather than one — proves the reason text *varies* with
  the flag, not that it merely contains a constant.
- Deliberately excluded:
  - **Wall-clock timing assertions** — real but environment-sensitive; the reason text is the
    deterministic observable, so timing is not asserted.
  - **`-disable-pre-scan-ping` path** — that flag already has its own coverage; this lab is
    scoped to the *timeout* value, not the on/off toggle.
  - **Resume behavior** — the saved-state path reuses persisted unreachable IPs; out of scope
    for demonstrating the timeout flag on a fresh scan.
  - **ICMP-blackhole via iptables** — a more faithful "host present but silent" model, but it
    needs a `NET_ADMIN` service; the unassigned IP is simpler and sufficient here.

## References

- Flag + design: `docs/superpowers/specs/2026-06-30-configurable-pre-scan-ping-timeout-design.md`
- Original pre-scan ping design: `docs/superpowers/specs/2026-03-20-pre-scan-ping-design.md`
- Release note: `docs/release-notes/1.3.0.md`
- Implementation: `pkg/scanapp/pre_scan_ping.go`, `pkg/scanapp/reachability.go`, `pkg/config/config.go`
