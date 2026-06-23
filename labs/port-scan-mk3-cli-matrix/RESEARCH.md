# port-scan-mk3 CLI Matrix Lab — Research

## Property demonstrated

port-scan-mk3's five CLI binaries produce contract-conformant output across their full
documented flag surface — correct port-state classification (open / close / close(timeout) /
unreachable), correct pressure-driven pause/resume and fail-safe abort, and correct CSV/CIDR
transforms — when run against deterministic mock targets and pressure APIs.

## Essential vs accidental complexity

- **Essential:** TCP connect classification; pre-scan ICMP reachability gating; pressure
  polling (simple + OAuth, single + multi-source) with pause/resume and 3-strike fail-safe;
  resume-state round-trip; the four CSV/CIDR helper transforms.
- **Accidental (mocked away):** real network targets, real pressure/OAuth providers, the
  dashboard TTY rendering. The lab proves the client-side contract, not third-party behavior.

## Key source contracts

See the plan (`docs/superpowers/plans/2026-06-23-port-scan-mk3-cli-matrix-lab.md`, "Exact
contracts") for verified status strings, CSV headers, pressure log lines, validate JSON shape,
rich-mode detection, resume schema, and helper IO.

## Design

Topology: 3 images → 9 services on bridge 172.30.0.0/24 with static IPs. Filtered ports via
in-container `iptables DROP` (cap NET_ADMIN) for genuine connect timeouts; scanner gets cap
NET_RAW so pre-scan `ping` works (else reachable hosts misclassify as unreachable). Pressure
healthchecks hit a dedicated non-consuming `/healthz` so they never advance the pressure
sequence. Multi-source auth needs two distinct `/data` URLs ⇒ two auth containers.

Rejected: stock images (can't deterministically produce close(timeout)); single auth container
(won't exercise MultiSourcePressureFetcher); exhaustive cartesian flag product (infeasible).

## Deliberately excluded

Exhaustive flag product; performance/throughput; UDP & IPv6; corrupted-chunk resume; modifying
the repo's existing `e2e/` suite.
