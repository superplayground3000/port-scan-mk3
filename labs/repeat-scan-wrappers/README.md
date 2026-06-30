# repeat-scan-wrappers

## What this demonstrates

Two cross-platform operator wrapper scripts (`scan-loop.sh` for Linux, `scan-loop.ps1` for
Windows) drive *repeated* scans of a user-defined `ip:port` target set, and their two knobs
— scan speed (`--rate`) and pre-scan-ping on/off (`--ping` / `--no-ping`) — produce
observably different scan behavior against a fixed target set.

## The deliverables

| Script | Host | Validated by |
|---|---|---|
| `linux/scan-loop.sh` | Linux (bash) | runtime, against the lab targets |
| `windows/scan-loop.ps1` | Windows (PowerShell 7) | PSScriptAnalyzer (static), in a `pwsh` container |

Both expose the **same flags**. They are thin wrappers around this repo's `port-scan`
binary: they turn `--targets "ip:port,..."` into the cidr-file / port-file CSVs the binary
expects and loop the scan. All scan logic stays in the tested binary.

```
--targets LIST     Required. Comma-separated ip:port pairs.
--rate N           Scan speed: leaky-bucket tokens/sec (also burst). Default 100.
--workers N        Concurrent scan workers. Default 10.
--ping | --no-ping Run the pre-scan ICMP ping (default) or skip it.
--ping-timeout D   Pre-scan ping budget per host (e.g. 100ms, 1s). Default 100ms.
--timeout D        TCP dial timeout (e.g. 100ms, 2s). Default 100ms.
--count N          Repeat the whole scan N times. Default 1.
--interval S       Seconds between repeats. Default 0.
--out DIR          Output base directory. Default ./scan-out.
--bin PATH         Path to the port-scan binary. Default: port-scan (on PATH).
```

Targets are scanned as the cross product of `{distinct IPs} x {distinct ports}`. List one
port per host to scan exactly those pairs. Each repeat writes its own batch under
`<out>/r<NN>/`.

### Examples

```bash
# Linux: scan two hosts, gentle rate, no pre-scan ping, 3 times a minute apart
linux/scan-loop.sh --targets "10.0.0.1:80,10.0.0.2:443" --rate 50 --no-ping \
  --count 3 --interval 60 --out ./scan-out
```

```powershell
# Windows: same surface
windows\scan-loop.ps1 -Targets "10.0.0.1:80,10.0.0.2:443" -Rate 50 -NoPing `
  -Count 3 -Interval 60 -Out .\scan-out
```

## Run it

```bash
cp .env.example .env       # only if you want to override defaults
docker compose up -d --wait
bash scripts/smoke-test.sh
```

Or run the whole validation (up → smoke → teardown) from the repo root:

```bash
bash ~/.claude/skills/research-lab/scripts/validate_lab.sh labs/repeat-scan-wrappers
```

You can also drive the Linux wrapper by hand against the lab's mock target:

```bash
docker compose exec scanner scan-loop \
  --targets "172.32.0.10:8080,172.32.0.99:8080" --no-ping --out /lab/demo
docker compose exec scanner sh -c 'cat /lab/demo/r01/scan_results-*.csv'
```

## Expected output

The smoke test prints, on success:

```
[1/4] pre-scan ping toggle
  OK: --ping gates 172.32.0.99 to unreachable; --no-ping dials it into scan_results
[2/4] scan speed knob (low --rate vs high --rate over 6 ports)
  slow(rate=1)=5s  fast(rate=1000)=0s
  OK: rate=1 took 5s vs rate=1000 0s for the same 6-port scan
[3/4] repeat knob (--count 3)
  OK: --count 3 produced 3 independent round batches (r01..r03)
[4/4] Windows wrapper static analysis (PSScriptAnalyzer)
  OK: scan-loop.ps1 passed PSScriptAnalyzer (Error+Warning) clean

SMOKE PASS: ...
```

The two key observables:

- **Ping toggle** — the unreachable host `172.32.0.99` appears in `unreachable_results-*.csv`
  with `--ping`, but in `scan_results-*.csv` (timed-out) with `--no-ping`.
- **Speed** — the `--rate 1` scan of 6 ports takes several seconds (1 token/s); the same scan
  at `--rate 1000` finishes effectively instantly.

## Teardown

```bash
docker compose down -v
```

## Further reading

- `RESEARCH.md` — design decisions and the rejected alternatives.
- `labs/pre-scan-ping-timeout/` — sibling lab; the pre-scan ping *timeout value*.
- `pkg/scanapp/task_dispatcher.go` — the leaky-bucket dispatch gate behind `--rate`.
- `pkg/scanapp/pre_scan_ping.go` — the pre-scan reachability stage behind `--ping`.
