# pre-scan-ping-timeout

## What this demonstrates

`-pre-scan-ping-timeout` is the budget the scanner waits for a pre-scan ICMP reply before
excluding a host from the TCP scan: an ICMP-unanswered host is dropped from the scan and
written to `unreachable_results` with reason `ping failed within <flag>`, and that reason
text tracks the flag value (100ms default, 200ms, 1s) across runs.

Topology: one **server** (`target-open`, `mock-target/`) — pingable, TCP 8080 open — and one
**client** (`scanner/`, the `port-scan` binary built from this repo). The unreachable case is
the unassigned IP `172.31.0.99` on the lab subnet, which never answers ICMP.

## Run it

```bash
cp .env.example .env        # only if you want to override defaults
docker compose up -d --wait
docker compose logs scanner

# Drive the demonstration (also run automatically by validate_lab.sh):
bash scripts/smoke-test.sh
```

## Expected output

`scripts/smoke-test.sh` runs three scans and prints, for each:

```
  OK [/lab/out-default]: 172.31.0.10 scanned (open); 172.31.0.99 gated, reason 'ping failed within 100ms'
  OK [/lab/out-200ms]: 172.31.0.10 scanned (open); 172.31.0.99 gated, reason 'ping failed within 200ms'
  OK [/lab/out-1s]: 172.31.0.10 scanned (open); 172.31.0.99 gated, reason 'ping failed within 1s'

SMOKE PASS: -pre-scan-ping-timeout gates the real scan and its value (100ms/200ms/1s)
            flows verbatim into the unreachable reason; the pingable open host is
            scanned while the ICMP-unanswered host is excluded before TCP.
```

To inspect the artifacts directly:

```bash
docker compose exec scanner sh -c 'cat /lab/out-200ms/unreachable_results-*.csv'
# ip,ip_cidr,status,reason,...
# 172.31.0.99,172.31.0.99/32,unreachable,ping failed within 200ms,...
docker compose exec scanner sh -c 'cat /lab/out-200ms/scan_results-*.csv'   # 172.31.0.10:8080 open; no 172.31.0.99
```

## Teardown

```bash
docker compose down -v
```

## Further reading

- `RESEARCH.md` (this lab) — concept summary and design decisions.
- `docs/superpowers/specs/2026-06-30-configurable-pre-scan-ping-timeout-design.md` — the flag's design.
- `docs/release-notes/1.3.0.md` — release note for the flag.
