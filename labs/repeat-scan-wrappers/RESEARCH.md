# Research: port-scan-mk3 repeat-scan wrappers (Linux + Windows)

## Topic

Operators want a small, repeatable way to scan a fixed set of `ip:port` targets without
hand-writing the scanner's CSV inputs each time — and to do it from both Linux and Windows
hosts. port-scan-mk3 already implements the hard parts (TCP connect scan, an optional
pre-scan ICMP ping, and a leaky-bucket rate limiter); what is missing is an ergonomic
wrapper that exposes the two knobs operators actually reach for — **scan speed** and
**whether to run the pre-scan ping** — and loops the scan on demand.

## Property demonstrated

Two cross-platform operator wrapper scripts (`scan-loop.sh` for Linux, `scan-loop.ps1` for
Windows) drive *repeated* scans of a user-defined `ip:port` target set, and their two knobs
— scan speed (`--rate`) and pre-scan-ping on/off (`--ping` / `--no-ping`) — produce
observably different scan behavior against a fixed target set.

## Concept summary

- The wrappers are thin command builders, not scanners. They translate a comma-separated
  `--targets "ip:port,..."` list into the `cidr-file` (`ip,ip_cidr`) and `port-file`
  (`<port>/tcp`) CSVs the `port-scan` binary consumes, then invoke `port-scan scan` in a
  loop. All scan/ping/rate logic stays in the tested binary.
- **Target model:** the scan covers the cross product of `{distinct IPs} x {distinct ports}`
  drawn from `--targets`, matching the binary's `cidr-file x port-file` contract. Listing a
  single port per host yields exactly those pairs; the lab's targets are chosen so the cross
  product equals the intended set.
- **Speed knob:** `--rate N` sets both `-bucket-rate N` and `-bucket-capacity N`. The
  dispatcher acquires one bucket token per `(target, port)` task before enqueuing it, so the
  refill rate caps dispatch throughput. The refill is time-governed (the dispatcher waits on
  tokens, it is not CPU-bound), so a low rate yields a deterministically longer wall-clock
  scan independent of host load — the basis for a robust timing assertion.
- **Ping knob:** with the pre-scan ping enabled (default), an ICMP-unanswered host is
  excluded before any TCP dial and written to `unreachable_results-<ts>.csv`. With
  `-disable-pre-scan-ping`, that host is instead dialled and recorded in
  `scan_results-<ts>.csv` with a `close(timeout)` status. The same dead host therefore flips
  files depending on the knob — a clean, deterministic observable.
- **Repeat:** each round writes its own batch under `<out>/r<NN>/`, so `--count N` produces
  N independent, non-colliding result sets.

## Wire / API contract

- Transports under test: ICMP echo (pre-scan ping) + TCP connect (scan), standard library.
- Scanner CLI (`port-scan scan`), flags the wrappers drive:
  - `-cidr-file <csv>` (`ip,ip_cidr`), `-port-file <csv>` (`<port>/tcp`) — generated targets.
  - `-output <dir>/scan_results.csv` — batch base dir; `scan_results-<ts>.csv`,
    `opened_results-<ts>.csv`, `unreachable_results-<ts>.csv` are written beside it.
  - `-bucket-rate N` / `-bucket-capacity N` — leaky-bucket rate + burst (the `--rate` knob).
  - `-disable-pre-scan-ping` — present iff `--no-ping` (the ping knob).
  - `-pre-scan-ping-timeout <d>` / `-timeout <d>` — ping budget / TCP dial timeout.
  - `-workers N`, `-disable-api`, `-quiet` — concurrency, drop pressure API, quiet logs.
- Wrapper CLI surface (identical on both scripts): `--targets --rate --workers
  --ping/--no-ping --ping-timeout --timeout --count --interval --out --bin`.

## Design

Topology (mirrors the proven `pre-scan-ping-timeout` lab):

- `target-open` — mock-target, pingable, TCP 8080 open (8081-8085 closed → instant RST for
  the speed demo); healthchecked.
- `172.32.0.99` — unassigned IP on the lab subnet → never answers ICMP; the unreachable host
  for the ping-toggle demo (no extra service or `NET_ADMIN` needed).
- `scanner` — built from the repo, carries the `port-scan` binary **and** the Linux
  `scan-loop` wrapper on PATH; long-lived idle driver, scans run via `docker compose exec`.
- `ps-lint` — `pwsh` + PSScriptAnalyzer; profile-gated (excluded from `up`), run as a
  one-shot `docker compose run` to statically validate the Windows `.ps1`.

Design decisions:

- **Pinned images:** `golang:1.24-alpine` / `alpine:3.20` (repo's runtime), and
  `mcr.microsoft.com/powershell:7.4-alpine-3.17` for the lint container.
- **`cap_add: NET_RAW` on `scanner`** — required so the pre-scan ICMP ping can open raw
  sockets. Container-scoped, no host change; the minimum capability for the ping path, and
  the demo's ping knob is meaningless without it. (Same justification as the pre-scan-ping
  lab.)
- **Unreachable host = unassigned IP**, not an ICMP-blackhole container — ARP never resolves
  so the host is reliably unreachable, with no extra service or `NET_ADMIN`.
- **Cross-product target model** (over per-pair grouping) — keeps a multi-port speed scan in
  a *single* bucket-gated invocation (so the rate knob actually bites) and maps 1:1 to the
  binary's CSV contract, giving simple bash/PowerShell parity.
- **Speed observable = wall-clock**, justified because the leaky bucket is a deterministic
  time-governed rate limiter; corroborated by asserting equal scan coverage across the slow
  and fast runs so the delta is pacing, not work.
- **Windows validated by static lint only** — the `.ps1` cannot execute against Linux
  targets under the containers-only rule, so PSScriptAnalyzer (Error+Warning, clean) is the
  in-container proof that it parses and is well-formed.
- **Inline build (no implementation plan).** Three images are involved, but `ps-lint` is an
  independent profile-gated one-shot with no cross-service ordering; the running topology is
  the proven one-server/one-client shape, so this stayed inline per the skill's escalation
  rule.

Rejected alternatives:

- **Per-pair invocation (one `port-scan` run per distinct port)** — faithful to "exact pairs"
  but breaks the speed demo: each process gets a fresh full bucket, so a 1-token/s rate never
  throttles a 1-task run. Cross-product keeps the demo coherent.
- **Self-contained shell scanners (`/dev/tcp`, `Test-NetConnection`)** — zero-dependency but
  re-implements (and diverges from) the binary's tested scan/ping/rate logic. Rejected:
  wrapping the binary reuses coverage.
- **Timing-free speed observable (throttle log lines)** — the binary emits `bucket_wait_start`
  / `bucket_acquired` events on *every* dispatch regardless of rate, so their presence does
  not distinguish fast from slow; the time *between* them (i.e. wall-clock) is the real
  signal.

## Deliberately excluded

- **Live Windows execution** — out of scope under containers-only; static lint is the boundary.
- **`-pre-scan-ping-timeout` value sweep** — the on/off toggle is this lab's concern; the
  timeout *value* already has its own lab (`pre-scan-ping-timeout`).
- **Pressure-control / resume / auth paths** — orthogonal to the two operator knobs; the
  wrappers pass `-disable-api` and leave resume off.
- **Filtered-port (DROP) targets** — the closed-port (RST) targets are enough to drive the
  speed demo; real connect-timeout behavior is covered by the dead-host ping path.

## References

- Pre-scan ping design: `docs/superpowers/specs/2026-03-20-pre-scan-ping-design.md`
- Configurable ping timeout: `docs/superpowers/specs/2026-06-30-configurable-pre-scan-ping-timeout-design.md`
- Sibling lab (reused topology): `labs/pre-scan-ping-timeout/`
- Implementation: `pkg/scanapp/task_dispatcher.go` (bucket gating),
  `pkg/scanapp/pre_scan_ping.go`, `pkg/scanapp/batch_output.go`, `pkg/config/config.go`
