# tcp-handshake-validation

## What this demonstrates

A port-scan-mk3 scan of an open port performs a complete RFC 9293 three-way
handshake (SYN → SYN,ACK → ACK) and then a scanner-initiated FIN teardown with
no application data, while a closed port is answered by a RST — proven by a
packet capture taken inside the scanner container.

## How it works

- `target` — a tiny Go TCP listener on `9000` (open). It blocks on `Read`, so
  the *scanner* initiates the connection close.
- `scanner` — an image built from the **repository root**, so it runs the real
  `port-scan-mk3` binary. Its entrypoint starts `tcpdump` on its own `eth0`,
  runs one scan of the target (ports `9000` open and `9001` closed), then uses
  `tshark` to decode the pcap and assert the RFC 9293 behaviour.
- Nothing touches the host network: a private bridge network, no published host
  ports, and `cap_drop: [ALL]` + `cap_add: [NET_RAW]` (the minimum for
  `tcpdump`; `NET_ADMIN`/`privileged` are not used).

## Run it

```bash
# From the repository root:
bash /home/hp/.claude/skills/research-lab/scripts/validate_lab.sh labs/tcp-handshake-validation

# Or drive it directly:
cd labs/tcp-handshake-validation
docker compose up -d --wait                       # start the target (healthy)
docker compose --profile scan run --rm --build scanner   # capture + scan + analyze
docker compose --profile scan down -v             # teardown
```

## Expected output

The scanner prints the captured packets and per-assertion results, ending with:

```
1  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [SYN]
2  172.31.199.10 → 172.31.199.2   TCP  9000  → 48323 [SYN, ACK]
3  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [ACK]
4  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [FIN, ACK]
5  172.31.199.2  → 172.31.199.10  TCP  55491 → 9001  [SYN]
6  172.31.199.10 → 172.31.199.2   TCP  9001  → 55491 [RST, ACK]
7  172.31.199.10 → 172.31.199.2   TCP  9000  → 48323 [FIN, ACK]
8  172.31.199.2  → 172.31.199.10  TCP  48323 → 9000  [ACK]
...
VERDICT: PASS — scan performed only RFC 9293 three-way connect + FIN/RST teardown.
```

Full analysis and the RFC mapping are in [RESEARCH.md](RESEARCH.md) ("Observed
results").

## Teardown

```bash
cd labs/tcp-handshake-validation
docker compose --profile scan down -v
```

## Further reading

- [RFC 9293 — TCP](https://www.rfc-editor.org/rfc/rfc9293) — §3.5 (three-way
  handshake), §3.6 (close / reset).
- Scanner core under test: `pkg/scanner/scanner.go` (`ScanTCP`).
- [tshark manual](https://www.wireshark.org/docs/man-pages/tshark.html) ·
  [tcpdump manual](https://www.tcpdump.org/manpages/tcpdump.1.html)
