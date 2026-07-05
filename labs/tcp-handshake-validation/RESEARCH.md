# Research: port-scan-mk3 TCP connect-scan wire behaviour

## Topic

port-scan-mk3 is a TCP port scanner; its scanner core (`pkg/scanner.ScanTCP`)
opens a connection with the Go standard library dialer and closes it. This lab
inspects what that actually puts on the wire.

## Property demonstrated

A port-scan-mk3 scan of an open port performs a complete RFC 9293 three-way
handshake (SYN → SYN,ACK → ACK) and then a scanner-initiated FIN teardown with
no application data, while a closed port is answered by a RST — proven by a
packet capture taken inside the scanner container.

## Concept summary

- **Connect scan, not half-open.** `ScanTCP` calls `dial(ctx,"tcp",target)`
  (`net.Dialer.DialContext`), which performs a full kernel `connect()`. That
  completes the three-way handshake — unlike a "SYN"/half-open scan that would
  send a RST after receiving SYN,ACK.
- **Graceful teardown.** On a successful dial the code immediately calls
  `conn.Close()` with no `SetLinger`, so the OS sends a FIN and the connection
  is torn down the normal way (FIN/ACK exchange). No abortive RST is forced.
- **No payload.** The scanner never writes to the socket, so an open-port probe
  carries zero application bytes — only handshake and teardown segments.
- **Closed port = RST.** When nothing listens, the target kernel answers the SYN
  with RST,ACK (connection refused). This is standard TCP, not scanner logic.
- **RFC 9293** is the current TCP specification; §3.5 defines connection
  establishment (three-way handshake) and §3.6 defines the FIN-based close and
  the RST reset. The behaviours above are exactly those state transitions.

## Wire / API contract

- **Protocol:** TCP (IPv4) over an isolated Docker bridge network.
- **Open-port exchange (RFC 9293 §3.5, §3.6):**
  `scanner → SYN`, `target → SYN,ACK`, `scanner → ACK` (established), then
  `scanner → FIN,ACK` … `target → FIN,ACK`, `scanner → ACK` (closed).
- **Closed-port exchange (RFC 9293 §3.6 reset):** `scanner → SYN`,
  `target → RST,ACK`.
- **Observation tooling:** `tcpdump` captures the scanner's `eth0`; `tshark`
  decodes TCP flag bits (`tcp.flags.syn|ack|fin|reset|push`, `tcp.len`) from the
  pcap and the smoke test asserts on the counts.

## Design decisions

- **Software under test is the real binary.** The scanner image's build context
  is the repository root and it compiles `./cmd/port-scan`; the lab does not
  re-implement or mock the scan.
- **The sniffer runs inside the scanner container**, capturing its own `eth0`.
  A separate sniffer container on a Docker bridge network would not see the
  unicast scan traffic (bridges are switched, not hubs), so co-locating the
  capture with the scanner is the reliable option.
- **`cap_drop: [ALL]` + `cap_add: [NET_RAW]`.** `tcpdump`'s live capture needs
  `CAP_NET_RAW`; nothing here needs `NET_ADMIN` or `privileged` (we capture the
  container's own traffic, not in promiscuous mode). `tshark` only reads the
  pcap file, which needs no capability. This is the minimum privilege and stays
  within the lab's host-isolation contract — no host network changes.
- **Static target IP `172.31.199.10`** on a private `/24` so the scan input CSV
  and the tcpdump host filter are deterministic. No host ports are published.
- **Target blocks on `Read` instead of closing first**, so the scanner is the
  side that initiates the FIN teardown and the capture attributes it correctly.
- **`-disable-pre-scan-ping` and `-disable-api`** so the only traffic on the
  wire is the connect scan itself — no reachability pre-probe, no pressure API.
- **Pinned images:** `golang:1.24-alpine` (matches the module's Go line),
  `alpine:3.20` (tcpdump + tshark), `gcr.io/distroless/static-debian12:nonroot`
  (target).
- **Deliberately excluded:** UDP scanning (the tool is TCP-only via
  `net.DialTimeout`); IPv6 (same stack path, adds no new evidence); throughput,
  rate-limiting, and pressure-control behaviour (covered by the repo's own e2e
  suite, and orthogonal to per-connection wire behaviour); timeout/filtered
  ports (would need a drop rule, i.e. a firewall change — out of scope here).

## Observed results

Captured on the scanner's `eth0` during one `port-scan scan` of the target
(open `9000`, closed `9001`). Scanner is `172.31.199.2`, target `172.31.199.10`.
Verbatim `tshark` summary of the eight packets:

```
1  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [SYN]       Seq=0        Len=0
2  172.31.199.10 → 172.31.199.2   TCP  9000  → 48323 [SYN, ACK]  Seq=0 Ack=1  Len=0
3  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [ACK]       Seq=1 Ack=1  Len=0
4  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [FIN, ACK]  Seq=1 Ack=1  Len=0
5  172.31.199.2  → 172.31.199.10  TCP  55491 → 9001  [SYN]       Seq=0        Len=0
6  172.31.199.10 → 172.31.199.2   TCP  9001  → 55491 [RST, ACK]  Seq=1 Ack=1  Len=0
7  172.31.199.10 → 172.31.199.2   TCP  9000  → 48323 [FIN, ACK]  Seq=1 Ack=2  Len=0
8  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [ACK]       Seq=2 Ack=2  Len=0
```

Reading it against RFC 9293:

- **Open port `9000` — establishment (§3.5):** packets 1–3 are the exact
  three-way handshake `SYN → SYN,ACK → ACK`. The scanner *sends the third ACK*,
  so the connection reaches ESTABLISHED — this is a full connect, not a
  half-open/SYN scan (a SYN scan would send RST after packet 2 and never ACK).
- **Open port `9000` — teardown (§3.6):** packets 4, 7, 8 are a normal
  FIN-based close (`FIN,ACK` from scanner → `FIN,ACK` from target → final
  `ACK`), and the FIN is **initiated by the scanner** (packet 4). No RST.
- **Closed port `9001` — reset (§3.6):** packet 5 is a lone `SYN`; the target
  kernel answers with `RST,ACK` (packet 6) — connection refused. No `SYN,ACK`.
- **No application data:** every segment is `Len=0`. The scanner writes nothing
  to the socket — it is purely a connect/close probe.

port-scan-mk3's own result for the same run: `open_count:1` (port 9000),
`close_count:1` (port 9001, "connection refused"), `success:true`.

All nine assertions in `scripts/smoke-test.sh` passed; `validate_lab.sh` exits 0.
This confirms the scan performs **only** the RFC 9293 three-way connect handshake
and a standard FIN/RST teardown — no stealth scanning, no raw-packet crafting,
no payload.

## References

- RFC 9293 — Transmission Control Protocol (Aug 2022):
  https://www.rfc-editor.org/rfc/rfc9293 (§3.5 establishment, §3.6 close/reset).
- port-scan-mk3 scanner core: `pkg/scanner/scanner.go` (`ScanTCP`).
- tshark manual (field extraction): https://www.wireshark.org/docs/man-pages/tshark.html
- tcpdump manual: https://www.tcpdump.org/manpages/tcpdump.1.html
