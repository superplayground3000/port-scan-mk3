# port-scan-mk3 CLI Matrix Lab — Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../../apps/port-scan/DESIGN.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Errata (2026-06-30):** this plan documents the original 36-case build. The shipped lab was
> later extended to **38 cases** — group C gained C3/C4 covering the `-pre-scan-ping-timeout`
> flag. The "36" counts below are historical; see the lab's `README.md`/`run-matrix.sh` and
> `2026-06-23-port-scan-mk3-cli-matrix-lab-design.md` for the current 38-case matrix.

**Goal:** Build a self-contained docker-compose lab under `labs/port-scan-mk3-cli-matrix/` that comprehensively exercises all 5 port-scan-mk3 binaries and ~50 CLI flags across 36 property-asserting test cases, validated to exit 0 via the research-lab `validate_lab.sh`.

**Architecture:** Three custom images (`mock-target` Go, `mock-pressure` Go adapted from `e2e/mock-pressure-api`, `scanner` built from the repo with all 5 binaries) run as 9 services on a static-IP bridge network. A long-lived `scanner` container holds the binaries + fixtures; `scripts/smoke-test.sh` (run on the host by `validate_lab.sh` after `docker compose up -d --wait`) executes `scripts/run-matrix.sh` inside the scanner via `docker compose exec`, which runs the 36-case matrix and asserts observable output (CSV rows, stdout, log lines, exit codes). Filtered ports use in-container `iptables DROP` (`cap_add: NET_ADMIN`) for real connect timeouts; pre-scan ping needs `cap_add: NET_RAW` on the scanner.

**Tech Stack:** Go 1.24 (mocks + binaries), Docker Compose, Bash (matrix driver), Alpine 3.20 runtime images.

---

## Spec source

Design spec: `docs/superpowers/specs/2026-06-23-port-scan-mk3-cli-matrix-lab-design.md`. Read it first.

## Exact contracts (verified against source — do not re-guess)

- **Status strings** (`pkg/scanner/scanner.go`, `pre_scan_ping.go:259`): `open`, `close`, `close(timeout)`, `unreachable`.
- **scan_results / opened_results header** (`pkg/writer/csv_writer.go:58`): `ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,matched_policy_id,reason,execution_key,src_ip,src_network_segment`. Files are always named `scan_results-<ts>.csv` / `opened_results-<ts>.csv` / `unreachable_results-<ts>.csv` in `dir(-output)`; the `-output` basename is otherwise ignored (`pkg/scanapp/batch_output.go`).
- **unreachable_results header** (`pkg/writer/unreachable_writer.go:29`): `ip,ip_cidr,status,reason,fab_name,cidr_name,service_label,decision,matched_policy_id,execution_key,src_ip,src_network_segment`.
- **Pressure** (`pkg/scanapp/pressure_monitor.go`): threshold default 60, pause when `pressure >= 60`. Per-poll success log: `[API] pressure api status=ok pressure=%.1f%% threshold=%.1f`. Pause: `[API] router pressure overload — scan automatically paused …`. Resume: `[API] router pressure recovered — scan automatically resumed …`. On fetch error: failures 1–2 log `pressure api request failed (N/3): …` and continue; the **3rd consecutive** failure sends `pressure api failed 3 times: …` to the error channel and the scan aborts (exit 1).
- **validate output** (`pkg/cli/output.go`): JSON `{"valid":<bool>,"detail":<string>}`; human `valid=%t detail=%s`. Exit codes (`cmd/port-scan/command_handlers.go`): validate valid→0, invalid→1, parse error→2; scan ok→0, error→1, SIGINT→130; unknown subcommand→2. Basic-mode validation requires `-port-file` (`detail` = `-port-file is required when cidr input is not rich mode`).
- **Rich-mode auto-detect** (`pkg/input/header_match.go`, `rich_types.go`): triggered when all 10 headers present (in order): `src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason`. In rich mode `-cidr-ip-col`/`-cidr-ip-cidr-col` are ignored; all `protocol=tcp` rows are scanned **regardless of `decision`** (the `decision` value is carried through as output metadata, not used as a scan filter), and non-`tcp` rows are excluded. Basic-mode required headers default `ip`,`ip_cidr`. (Corrected post-implementation — see Errata.)
- **Resume** (`pkg/task/types.go`, `pkg/scanapp/chunk_lifecycle.go:147-152`, `resume_path.go`): chunk JSON tags `cidr,cidr_name,ports,next_index,scanned_count,total_count,status`; ports in `<port>/tcp` form; resume file written to `dir(-output)/resume_state.json` on SIGINT; on `-resume`, if a chunk's `total_count` ≠ `targets×ports` the run aborts with `chunk total_count mismatch for <cidr>: state=N expected=M`; if a chunk CIDR is absent from the input it aborts with `cidr <c> from chunk not found in cidr file`.
- **preprocess** (`pkg/preprocess/filter.go:28`, `loader.go`): `Filter.Keep` returns true when `dst_network_segment` is **NOT** within a closed CIDR ⇒ preprocess **removes** rows whose `dst_network_segment` falls in a closed CIDR. cleaned-cidrs columns `fab,segment,status`; only `status=close` rows matching `--fab-name` form the closed tree. Output path `<output-dir>/<fab-name>/<ts>/input.csv` (pass-through columns).
- **enrich-targets**: input cols `host,port`; `--cidr-list` first column CIDR; `--service-map` cols `port,service_label`; output 10-col rich schema (`src_ip` default `10.59.42.39`, `decision=accept`, `matched_policy_id=enriched`, `reason=MATCH_POLICY_ACCEPT`); invalid-host rows skipped.
- **cidr-compare**: `-deny-file`/`CIDR_COMPARE_DENY_FILE` cols `dst_network_segment,decision` (keep `decision=deny`); `-open-file`/`CIDR_COMPARE_OPEN_FILE` cols `segment,status` (keep `status=open`); stdout header `deny_cidr,open_cidr` then one `<deny>,<open>` row per containment.
- **csv-transform**: flags/env `--input`/`TRANSFORM_INPUT`, `--output`/`TRANSFORM_OUTPUT`, `--host-col`/`TRANSFORM_HOST_COL` (default `Host`), `--port-col`/`TRANSFORM_PORT_COL` (default `Port`), `--pass-col`/`TRANSFORM_PASS_COL` (default `Pass the test`); a row is **included only if** pass value == `FALSE` (case-insensitive); `/`-separated ports expand to multiple rows; output 10-col rich schema.

## File structure

```
labs/port-scan-mk3-cli-matrix/
├── RESEARCH.md
├── README.md
├── .env.example
├── .gitignore
├── docker-compose.yml
├── mock-target/        { go.mod, main.go, Dockerfile }
├── mock-pressure/      { go.mod, main.go (adapted), Dockerfile }
├── scanner/            { Dockerfile }   # built from repo root context
├── fixtures/           { *.csv, resume-mismatch.json }
└── scripts/
    ├── smoke-test.sh           # host black-box driver (called by validate_lab.sh)
    ├── run-matrix.sh           # 36-case matrix, runs inside scanner
    └── lib/assert.sh           # assertion helpers
```

**Network:** bridge `lab`, subnet `172.30.0.0/24`. Targets `.10`(open) `.11`(closed) `.12`(filtered); pressure `.20`(ok) `.21`(high) `.22`(5xx) `.23`(timeout) `.24`(auth-1) `.25`(auth-2); scanner dynamic. No published host ports (fully internal).

---

### Task 1: Scaffolding — directories, .gitignore, .env.example, RESEARCH.md

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/.gitignore`
- Create: `labs/port-scan-mk3-cli-matrix/.env.example`
- Create: `labs/port-scan-mk3-cli-matrix/RESEARCH.md`

- [ ] **Step 1: Create directory tree**

Run:
```bash
mkdir -p labs/port-scan-mk3-cli-matrix/{mock-target,mock-pressure,scanner,fixtures,scripts/lib}
```

- [ ] **Step 2: Write `.gitignore`**

```gitignore
# Lab runtime artifacts (matrix output lives inside the scanner container; nothing
# should be written into the repo, but ignore these defensively).
out/
*.log
.env
```

- [ ] **Step 3: Write `.env.example`** (documents every tunable env var; compose has working defaults so a real `.env` is optional)

```dotenv
# mock-target (per target instance, set in docker-compose.yml)
#   OPEN_PORTS      comma list of TCP ports the target accepts on (open => status "open")
#   FILTERED_PORTS  comma list of TCP ports DROPped via iptables (=> status "close(timeout)")
#   HEALTH_PORT     always-open port used only by the container healthcheck (default 19999)

# mock-pressure (per pressure instance, set in docker-compose.yml)
#   MODE                    ok | fail | timeout            (default ok)
#   ADDR                    listen address                 (default :8080)
#   PRESSURE                static pressure value           (default 20)
#   DELAY_MS                sleep for MODE=timeout          (default 5000)
#   PRESSURE_SEQUENCE       comma list, advances per GET    (e.g. 90,90,10)
#   PRESSURE_SEQUENCE_LOOP  loop the sequence               (default false)
#   USE_AUTH                enable OAuth /auth + /data       (default false)
#   AUTH_CLIENT_ID          expected client id              (default test-client)
#   AUTH_CLIENT_SECRET      expected client secret          (default test-secret)
#   PRESSURE_VALUE_1/2      Percent values from /data        (default 85 / 72)
```

- [ ] **Step 4: Write `RESEARCH.md`** (research-lab Stage 2 artifact)

```markdown
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
```

- [ ] **Step 5: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/.gitignore labs/port-scan-mk3-cli-matrix/.env.example labs/port-scan-mk3-cli-matrix/RESEARCH.md
git commit -m "feat(lab): scaffold port-scan-mk3 CLI matrix lab"
```

---

### Task 2: mock-target (Go) — open/closed/filtered + healthcheck

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/mock-target/go.mod`
- Create: `labs/port-scan-mk3-cli-matrix/mock-target/main.go`
- Create: `labs/port-scan-mk3-cli-matrix/mock-target/Dockerfile`

- [ ] **Step 1: Write `go.mod`**

```go
module mocktarget

go 1.24
```

- [ ] **Step 2: Write `main.go`**

```go
// mock-target opens a configurable set of TCP ports (OPEN_PORTS), optionally installs
// iptables DROP rules for FILTERED_PORTS (requires NET_ADMIN) to produce real connect
// timeouts, and always listens on HEALTH_PORT for the container healthcheck.
// Invoked with -healthcheck it dials HEALTH_PORT and exits 0/1.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe local health port and exit")
	flag.Parse()

	healthPort := getenv("HEALTH_PORT", "19999")

	if *healthcheck {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+healthPort, 2*time.Second)
		if err != nil {
			os.Exit(1)
		}
		_ = conn.Close()
		os.Exit(0)
	}

	for _, p := range splitPorts(os.Getenv("FILTERED_PORTS")) {
		out, err := exec.Command("iptables", "-A", "INPUT", "-p", "tcp", "--dport", p, "-j", "DROP").CombinedOutput()
		if err != nil {
			log.Fatalf("iptables DROP tcp/%s failed: %v: %s", p, err, out)
		}
		log.Printf("installed DROP rule for tcp/%s", p)
	}

	startListener(healthPort)
	for _, p := range splitPorts(os.Getenv("OPEN_PORTS")) {
		startListener(p)
	}
	log.Printf("mock-target ready health=%s open=%q filtered=%q",
		healthPort, os.Getenv("OPEN_PORTS"), os.Getenv("FILTERED_PORTS"))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func startListener(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen on %s failed: %v", port, err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
}

func splitPorts(raw string) []string {
	var out []string
	for _, p := range strings.Split(strings.TrimSpace(raw), ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 3: Write `Dockerfile`**

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /bin/mock-target .

FROM alpine:3.20
RUN apk add --no-cache iptables && adduser -D -u 10001 app
COPY --from=build /bin/mock-target /usr/local/bin/mock-target
USER app
ENTRYPOINT ["/usr/local/bin/mock-target"]
```

- [ ] **Step 4: Verify it builds and the open/closed behavior works**

Run:
```bash
cd labs/port-scan-mk3-cli-matrix/mock-target
docker build -t psmk3-mock-target-test .
docker run -d --name mt-test -e OPEN_PORTS=8080 -e HEALTH_PORT=19999 psmk3-mock-target-test
sleep 1
docker exec mt-test /usr/local/bin/mock-target -healthcheck && echo "HEALTHCHECK_OK"
docker rm -f mt-test
```
Expected: prints `HEALTHCHECK_OK`.

- [ ] **Step 5: Commit**

```bash
cd ../../.. && git add labs/port-scan-mk3-cli-matrix/mock-target
git commit -m "feat(lab): add mock-target with open/closed/filtered + healthcheck"
```

---

### Task 3: mock-pressure (Go) — adapt e2e mock + non-consuming /healthz

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/mock-pressure/go.mod`
- Create: `labs/port-scan-mk3-cli-matrix/mock-pressure/main.go` (copied from repo, one edit)
- Create: `labs/port-scan-mk3-cli-matrix/mock-pressure/Dockerfile`

- [ ] **Step 1: Write `go.mod`**

```go
module mockpressure

go 1.24
```

- [ ] **Step 2: Copy the existing mock source verbatim**

Run (from repo root):
```bash
cp e2e/mock-pressure-api/main.go labs/port-scan-mk3-cli-matrix/mock-pressure/main.go
```

- [ ] **Step 3: Add a non-consuming `/healthz` handler**

In `labs/port-scan-mk3-cli-matrix/mock-pressure/main.go`, find the `newMux` function:

```go
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", handleAuth)
	mux.HandleFunc("/data", handleData)
	mux.HandleFunc("/admin/config", handleConfigInfo)
	mux.HandleFunc("/admin/config/reload", handleConfigReload)
	return mux
}
```

Replace it with (adds `/healthz`, which never touches pressure state so healthchecks don't advance `PRESSURE_SEQUENCE`):

```go
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", handleAuth)
	mux.HandleFunc("/data", handleData)
	mux.HandleFunc("/admin/config", handleConfigInfo)
	mux.HandleFunc("/admin/config/reload", handleConfigReload)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
```

- [ ] **Step 4: Write `Dockerfile`**

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /bin/mock-pressure .

FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /bin/mock-pressure /usr/local/bin/mock-pressure
USER app
ENTRYPOINT ["/usr/local/bin/mock-pressure"]
```

- [ ] **Step 5: Verify build + /healthz is non-consuming**

Run:
```bash
cd labs/port-scan-mk3-cli-matrix/mock-pressure
docker build -t psmk3-mock-pressure-test .
docker run -d --name mp-test -e ADDR=:8080 -e PRESSURE_SEQUENCE=90,10 psmk3-mock-pressure-test
sleep 1
docker exec mp-test wget -qO- http://localhost:8080/healthz; echo
docker exec mp-test wget -qO- http://localhost:8080/api/pressure; echo   # expect pressure=90 (seq[0])
docker rm -f mp-test
```
Expected: `/healthz` prints `ok`; first `/api/pressure` GET returns `{"pressure":90}` (proves healthz did not consume the sequence).

- [ ] **Step 6: Commit**

```bash
cd ../../.. && git add labs/port-scan-mk3-cli-matrix/mock-pressure
git commit -m "feat(lab): add mock-pressure (adapted) with non-consuming /healthz"
```

---

### Task 4: scanner image — all 5 binaries + bash + ping

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/scanner/Dockerfile`

- [ ] **Step 1: Write `Dockerfile`** (build context is the repo root; see compose in Task 6)

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/port-scan      ./cmd/port-scan \
 && CGO_ENABLED=0 go build -o /bin/preprocess     ./cmd/preprocess \
 && CGO_ENABLED=0 go build -o /bin/enrich-targets ./cmd/enrich-targets \
 && CGO_ENABLED=0 go build -o /bin/cidr-compare   ./cmd/cidr-compare \
 && CGO_ENABLED=0 go build -o /bin/csv-transform  ./cmd/csv-transform

FROM alpine:3.20
# bash: matrix driver. iputils: real `ping` for pre-scan reachability (busybox ping lacks the
# flags the scanner uses). Runs as root + cap NET_RAW (compose) so ping can open ICMP sockets.
RUN apk add --no-cache bash iputils
COPY --from=build /bin/port-scan /bin/preprocess /bin/enrich-targets /bin/cidr-compare /bin/csv-transform /usr/local/bin/
WORKDIR /lab
```

- [ ] **Step 2: Verify all 5 binaries build (from repo root)**

Run:
```bash
docker build -t psmk3-scanner-test -f labs/port-scan-mk3-cli-matrix/scanner/Dockerfile .
docker run --rm psmk3-scanner-test sh -c 'for b in port-scan preprocess enrich-targets cidr-compare csv-transform; do command -v $b || exit 1; done; bash --version | head -1; ping -V 2>&1 | head -1'
```
Expected: all five binary paths print, bash version prints, ping version prints.

- [ ] **Step 3: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/scanner/Dockerfile
git commit -m "feat(lab): add scanner image with all 5 binaries + bash + ping"
```

---

### Task 5: Fixtures

**Files:** create each under `labs/port-scan-mk3-cli-matrix/fixtures/`.

- [ ] **Step 1: Write `basic.csv`** (3 targets, basic mode)

```csv
ip,ip_cidr
172.30.0.10,172.30.0.10/32
172.30.0.11,172.30.0.11/32
172.30.0.12,172.30.0.12/32
```

- [ ] **Step 2: Write `basic-open.csv`** (open target only, for fast scans)

```csv
ip,ip_cidr
172.30.0.10,172.30.0.10/32
```

- [ ] **Step 3: Write `basic-filtered-many.csv`** (filtered target, for long scans)

```csv
ip,ip_cidr
172.30.0.12,172.30.0.12/32
```

- [ ] **Step 4: Write `basic-custom-headers.csv`** (custom column names for B5)

```csv
source_ip,source_range
172.30.0.10,172.30.0.10/32
```

- [ ] **Step 5: Write `unreachable.csv`** (mixed reachable + unreachable for C)

```csv
ip,ip_cidr
172.30.0.10,172.30.0.10/32
172.30.0.99,172.30.0.99/32
```

- [ ] **Step 6: Write `ports.csv`** (two ports)

```csv
8080/tcp
9000/tcp
```

- [ ] **Step 7: Write `ports-many.csv`** (six filtered ports → long, deterministic timeouts)

```csv
8080/tcp
9000/tcp
9001/tcp
9002/tcp
9003/tcp
9004/tcp
```

- [ ] **Step 8: Write `rich.csv`** (rich mode for B4 + preprocess input for F)

```csv
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
10.0.0.1,10.0.0.1/32,172.30.0.10,172.30.0.10/32,http-test,tcp,8080,accept,policy-1,MATCH_POLICY_ACCEPT
10.0.0.1,10.0.0.1/32,172.30.0.11,172.30.0.11/32,http-test,tcp,8080,deny,policy-2,MATCH_POLICY_DENY
10.0.0.1,10.0.0.1/32,172.30.0.12,172.30.0.12/32,dns,udp,53,accept,policy-3,MATCH_POLICY_ACCEPT
```

- [ ] **Step 9: Write `cleaned-cidrs.csv`** (preprocess closed tree; only `close` rows for `fab1` count)

```csv
fab,segment,status
fab1,172.30.0.12/32,close
fab1,172.30.0.99/32,open
```

- [ ] **Step 10: Write `minimal.csv`, `minimal-mixed.csv`, `cidrs.csv`, `services.csv`** (enrich-targets)

`minimal.csv`:
```csv
host,port
172.30.0.10,8080
```
`minimal-mixed.csv`:
```csv
host,port
172.30.0.10,8080
not-an-ip,8080
```
`cidrs.csv`:
```csv
172.30.0.0/24
```
`services.csv`:
```csv
port,service_label
8080,http-test
```

- [ ] **Step 11: Write `deny.csv`, `deny-none.csv`, `open.csv`** (cidr-compare)

`deny.csv`:
```csv
dst_network_segment,decision
172.30.0.0/24,deny
10.0.0.0/8,deny
```
`deny-none.csv`:
```csv
dst_network_segment,decision
10.0.0.0/8,deny
```
`open.csv`:
```csv
segment,status
172.30.0.12/32,open
192.168.1.0/24,open
```

- [ ] **Step 12: Write `legacy.csv`, `legacy-custom.csv`** (csv-transform; only `FALSE` rows are kept)

`legacy.csv`:
```csv
Host,Port,Pass the test
172.30.0.10,8080,FALSE
172.30.0.11,9000,TRUE
```
`legacy-custom.csv`:
```csv
H,P,Result
172.30.0.10,8080,FALSE
```

- [ ] **Step 13: Write `resume-mismatch.json`** (E2 — total_count deliberately wrong; expected = 1 IP × 2 ports = 2)

```json
{"chunks":[{"cidr":"172.30.0.10/32","cidr_name":"","ports":["8080/tcp","9000/tcp"],"next_index":0,"scanned_count":0,"total_count":999,"status":"pending"}]}
```

- [ ] **Step 14: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/fixtures
git commit -m "feat(lab): add CLI matrix fixtures"
```

---

### Task 6: docker-compose.yml

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/docker-compose.yml`

- [ ] **Step 1: Write `docker-compose.yml`**

```yaml
name: lab-port-scan-mk3-cli-matrix

networks:
  lab:
    driver: bridge
    ipam:
      config:
        - subnet: 172.30.0.0/24

services:
  target-open:
    build: ./mock-target
    image: psmk3-mock-target
    restart: "no"
    environment:
      OPEN_PORTS: "8080,9000"
      HEALTH_PORT: "19999"
    networks:
      lab: { ipv4_address: 172.30.0.10 }
    healthcheck:
      test: ["CMD", "/usr/local/bin/mock-target", "-healthcheck"]
      interval: 3s
      timeout: 3s
      retries: 10

  target-closed:
    build: ./mock-target
    image: psmk3-mock-target
    restart: "no"
    environment:
      OPEN_PORTS: ""
      HEALTH_PORT: "19999"
    networks:
      lab: { ipv4_address: 172.30.0.11 }
    healthcheck:
      test: ["CMD", "/usr/local/bin/mock-target", "-healthcheck"]
      interval: 3s
      timeout: 3s
      retries: 10

  target-filtered:
    build: ./mock-target
    image: psmk3-mock-target
    restart: "no"
    user: "0:0"             # root needed to manage THIS container's own netns iptables
    cap_add: ["NET_ADMIN"]  # container-namespace capability only; no host change
    environment:
      OPEN_PORTS: ""
      FILTERED_PORTS: "8080,9000,9001,9002,9003,9004"
      HEALTH_PORT: "19999"
    networks:
      lab: { ipv4_address: 172.30.0.12 }
    healthcheck:
      test: ["CMD", "/usr/local/bin/mock-target", "-healthcheck"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-ok:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment: { MODE: "ok", PRESSURE: "20", ADDR: ":8080" }
    networks:
      lab: { ipv4_address: 172.30.0.20 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-high:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment:
      MODE: "ok"
      ADDR: ":8080"
      PRESSURE_SEQUENCE: "90,90,10,10,10,10,10,10,10,10,10,10"
      PRESSURE_SEQUENCE_LOOP: "false"
    networks:
      lab: { ipv4_address: 172.30.0.21 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-5xx:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment: { MODE: "fail", ADDR: ":8080" }
    networks:
      lab: { ipv4_address: 172.30.0.22 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-timeout:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment: { MODE: "timeout", DELAY_MS: "5000", PRESSURE: "20", ADDR: ":8080" }
    networks:
      lab: { ipv4_address: 172.30.0.23 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-auth-1:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment:
      ADDR: ":8080"
      USE_AUTH: "true"
      AUTH_CLIENT_ID: "test-client"
      AUTH_CLIENT_SECRET: "test-secret"
      PRESSURE_VALUE_1: "10"
      PRESSURE_VALUE_2: "20"
    networks:
      lab: { ipv4_address: 172.30.0.24 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  pressure-auth-2:
    build: ./mock-pressure
    image: psmk3-mock-pressure
    restart: "no"
    environment:
      ADDR: ":8080"
      USE_AUTH: "true"
      AUTH_CLIENT_ID: "test-client"
      AUTH_CLIENT_SECRET: "test-secret"
      PRESSURE_VALUE_1: "15"
      PRESSURE_VALUE_2: "25"
    networks:
      lab: { ipv4_address: 172.30.0.25 }
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/healthz"]
      interval: 3s
      timeout: 3s
      retries: 10

  scanner:
    build:
      context: ../..
      dockerfile: labs/port-scan-mk3-cli-matrix/scanner/Dockerfile
    image: psmk3-scanner
    restart: "no"
    cap_add: ["NET_RAW"]            # pre-scan ICMP ping
    command: ["sleep", "infinity"]  # long-lived; matrix runs via `docker compose exec`
    volumes:
      - ./scripts:/lab/scripts:ro
      - ./fixtures:/lab/fixtures:ro
    networks:
      lab: {}
    healthcheck:
      test: ["CMD", "test", "-f", "/usr/local/bin/port-scan"]
      interval: 3s
      timeout: 3s
      retries: 5
    depends_on:
      target-open: { condition: service_healthy }
      target-closed: { condition: service_healthy }
      target-filtered: { condition: service_healthy }
      pressure-ok: { condition: service_healthy }
      pressure-high: { condition: service_healthy }
      pressure-5xx: { condition: service_healthy }
      pressure-timeout: { condition: service_healthy }
      pressure-auth-1: { condition: service_healthy }
      pressure-auth-2: { condition: service_healthy }
```

- [ ] **Step 2: Validate compose schema + host-isolation grep**

Run (from the lab dir):
```bash
cd labs/port-scan-mk3-cli-matrix
docker compose config -q && echo "CONFIG_OK"
grep -nE 'privileged:[[:space:]]*true|network_mode:|pid:|ipc:|docker\.sock' docker-compose.yml || echo "NO_FORBIDDEN_FLAGS"
cd ../..
```
Expected: `CONFIG_OK` and `NO_FORBIDDEN_FLAGS`.

- [ ] **Step 3: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/docker-compose.yml
git commit -m "feat(lab): add 9-service docker-compose topology"
```

---

### Task 7: assertion helpers — scripts/lib/assert.sh

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/scripts/lib/assert.sh`

- [ ] **Step 1: Write `assert.sh`**

```bash
# shellcheck shell=bash
# Assertion helpers for the CLI matrix. Source this; it tracks PASS/FAIL and prints a summary.

PASS=0
FAIL=0

_grn=$'\033[32m'; _red=$'\033[31m'; _rst=$'\033[0m'

pass() { PASS=$((PASS+1)); printf '%sPASS%s %s\n' "$_grn" "$_rst" "$1"; }
fail() { FAIL=$((FAIL+1)); printf '%sFAIL%s %s\n' "$_red" "$_rst" "$1" >&2; }

# assert_eq <name> <expected> <actual>
assert_eq() { if [ "$2" = "$3" ]; then pass "$1"; else fail "$1 (expected '$2' got '$3')"; fi; }
# assert_ne <name> <not-expected> <actual>
assert_ne() { if [ "$2" != "$3" ]; then pass "$1"; else fail "$1 (got disallowed '$3')"; fi; }
# assert_gt <name> <a> <b>   (pass if a > b)
assert_gt() { if [ "${2:-0}" -gt "${3:-0}" ]; then pass "$1"; else fail "$1 (expected $2 > $3)"; fi; }
# assert_contains <name> <file> <ere>
assert_contains() { if [ -f "$2" ] && grep -Eq -- "$3" "$2"; then pass "$1"; else fail "$1 (missing /$3/ in ${2:-<none>})"; fi; }
# assert_not_contains <name> <file> <ere>
assert_not_contains() { if [ ! -f "$2" ] || ! grep -Eq -- "$3" "$2"; then pass "$1"; else fail "$1 (unexpected /$3/ in $2)"; fi; }
# assert_file_exists <name> <path>
assert_file_exists() { if [ -n "${2:-}" ] && [ -f "$2" ]; then pass "$1"; else fail "$1 (file missing: ${2:-<empty>})"; fi; }

# latest <dir> <prefix>  -> newest <prefix>-*.csv in dir (empty if none)
latest() { ls -1t "$1/$2"-*.csv 2>/dev/null | head -n1; }

summary() {
	echo "------------------------------------------------------------"
	echo "RESULT: PASS=$PASS FAIL=$FAIL TOTAL=$((PASS+FAIL))"
	[ "$FAIL" -eq 0 ]
}
```

- [ ] **Step 2: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/scripts/lib/assert.sh
git commit -m "feat(lab): add assertion helpers"
```

---

### Task 8: matrix driver — scripts/run-matrix.sh (all 36 cases)

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/scripts/run-matrix.sh`

- [ ] **Step 1: Write `run-matrix.sh`** (runs inside the scanner container; full content)

```bash
#!/usr/bin/env bash
# run-matrix.sh — executes the 36-case port-scan-mk3 CLI flag matrix INSIDE the scanner
# container and asserts observable output. Exits 0 only if every case passes.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/assert.sh
source "$HERE/lib/assert.sh"

FIX=/lab/fixtures
OUT=/lab/out
rm -rf "$OUT"; mkdir -p "$OUT"

OPEN=172.30.0.10; CLOSED=172.30.0.11; FILTERED=172.30.0.12; UNREACH=172.30.0.99
POK=pressure-ok; PHI=pressure-high; P5=pressure-5xx; PTO=pressure-timeout
PA1=pressure-auth-1; PA2=pressure-auth-2

# ---------------- A: validate ----------------
A1() { local d="$OUT/A1"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" -format human >"$d/o" 2>"$d/e"
  assert_eq "A1 validate basic human exit0" 0 "$?"
  assert_contains "A1 validate human valid=true" "$d/o" '^valid=true'; }
A2() { local d="$OUT/A2"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" -format json >"$d/o" 2>"$d/e"
  assert_eq "A2 validate basic json exit0" 0 "$?"
  assert_contains "A2 validate json valid:true" "$d/o" '"valid":true'; }
A3() { local d="$OUT/A3"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/rich.csv" -format json >"$d/o" 2>"$d/e"
  assert_eq "A3 validate rich json exit0" 0 "$?"
  assert_contains "A3 validate rich valid:true" "$d/o" '"valid":true'; }
A4() { local d="$OUT/A4"; mkdir -p "$d"
  port-scan validate -cidr-file "$FIX/basic.csv" -format json >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "A4 validate missing port-file exit1" 1 "$rc"
  assert_contains "A4 validate valid:false" "$d/o" '"valid":false'; }

# ---------------- B: scan modes/IO (TCP-state group => -disable-pre-scan-ping) ----------------
B_states() { local d="$OUT/B_states"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -timeout 300ms -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B scan states exit0" 0 "$?"
  local sr op; sr="$(latest "$d" scan_results)"; op="$(latest "$d" opened_results)"
  assert_contains    "B1 open in opened_results"  "$op" "^$OPEN,$OPEN/32,8080,open,"
  assert_contains    "B2 closed in scan_results"  "$sr" "^$CLOSED,$CLOSED/32,8080,close,"
  assert_contains    "B3 filtered close(timeout)" "$sr" "^$FILTERED,$FILTERED/32,8080,close\\(timeout\\),"; }
B4() { local d="$OUT/B4"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/rich.csv" -disable-api -disable-pre-scan-ping -timeout 300ms -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B4 rich scan exit0" 0 "$?"
  local sr; sr="$(latest "$d" scan_results)"
  assert_contains     "B4 rich accept .10 scanned open" "$sr" "^$OPEN,$OPEN/32,8080,open,"
  assert_not_contains "B4 rich deny .11 skipped"        "$sr" "^$CLOSED,"
  assert_not_contains "B4 rich udp .12 skipped"         "$sr" "$FILTERED"; }
B5() { local d="$OUT/B5"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-custom-headers.csv" -port-file "$FIX/ports.csv" \
    -cidr-ip-col source_ip -cidr-ip-cidr-col source_range \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B5 custom-col scan exit0" 0 "$?"
  assert_contains "B5 custom-col open .10" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
B6() { local d="$OUT/B6"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -bucket-rate 500 -bucket-capacity 500 -delay 5ms -workers 20 \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "B6 rate-control scan exit0" 0 "$?"
  assert_contains "B6 open found with rate flags" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
B7() { local sub="$OUT/B7/nested"; mkdir -p "$sub"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$sub/run.csv" >"$OUT/B7.o" 2>"$OUT/B7.e"
  assert_eq "B7 custom-output scan exit0" 0 "$?"
  assert_file_exists "B7 scan_results under custom dir" "$(latest "$sub" scan_results)"; }
B8() { local d="$OUT/B8"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -log-level debug -format json -disable-api -disable-pre-scan-ping -output "$d/dbg.csv" >"$d/o1" 2>"$d/dbg.err"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -log-level error -disable-api -disable-pre-scan-ping -output "$d/err.csv" >"$d/o2" 2>"$d/err.err"
  assert_gt "B8 debug more verbose than error level" "$(wc -l <"$d/dbg.err")" "$(wc -l <"$d/err.err")"; }
B9() { local d="$OUT/B9"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -quiet -disable-api -disable-pre-scan-ping -output "$d/q.csv" >"$d/qo" 2>"$d/q.err"
  assert_eq "B9 quiet scan exit0" 0 "$?"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$d/n.csv" >"$d/no" 2>"$d/n.err"
  assert_gt "B9 normal noisier than quiet" "$(wc -l <"$d/n.err")" "$(wc -l <"$d/q.err")"
  assert_file_exists "B9 results still written under quiet" "$(latest "$d" scan_results)"; }

# ---------------- C: reachability (default ping; needs NET_RAW) ----------------
C1() { local d="$OUT/C1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" -disable-api -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "C1 ping-enabled reachable exit0" 0 "$?"
  assert_contains "C1 reachable .10 scanned open" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
C2() { local d="$OUT/C2"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/unreachable.csv" -port-file "$FIX/ports.csv" -disable-api -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "C2 ping-enabled scan exit0" 0 "$?"
  assert_contains "C2 .99 marked unreachable" "$(latest "$d" unreachable_results)" "^$UNREACH,$UNREACH/32,unreachable,"; }

# ---------------- D: pressure control ----------------
D1() { local d="$OUT/D1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -pressure-api "http://$POK:8080/api/pressure" -pressure-interval 1s \
    -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D1 simple pressure ok exit0" 0 "$?"
  assert_contains "D1 open found under pressure-ok" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
D2() { local d="$OUT/D2"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$PHI:8080/api/pressure" -pressure-interval 1s \
    -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D2 high->low pressure exit0" 0 "$?"
  assert_contains "D2 scan paused on high pressure" "$d/e" 'router pressure overload.*scan automatically paused'
  assert_contains "D2 scan resumed on low pressure" "$d/e" 'router pressure recovered.*scan automatically resumed'; }
D3() { local d="$OUT/D3"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$P5:8080/api/pressure" -pressure-interval 1s \
    -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D3 5xx fail-safe abort exit1" 1 "$rc"
  assert_contains "D3 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }
D4() { local d="$OUT/D4"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-api "http://$PTO:8080/api/pressure" -pressure-interval 1s \
    -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D4 timeout fail-safe abort exit1" 1 "$rc"
  assert_contains "D4 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }
D5() { local d="$OUT/D5"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D5 disable-api exit0" 0 "$?"
  assert_not_contains "D5 no pressure polling logs" "$d/e" '\[API\] pressure api status'
  assert_contains "D5 open found" "$(latest "$d" opened_results)" "^$OPEN,$OPEN/32,8080,open,"; }
D6() { local d="$OUT/D6"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" -pressure-data-url "http://$PA1:8080/data" \
    -pressure-client-id test-client -pressure-client-secret test-secret \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D6 auth single-source exit0" 0 "$?"
  assert_contains "D6 authenticated poll succeeded" "$d/e" 'pressure api status=ok'; }
D7() { local d="$OUT/D7"; mkdir -p "$d"
  timeout 90s port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" \
    -pressure-data-url "http://$PA1:8080/data,http://$PA2:8080/data" \
    -pressure-client-id test-client -pressure-client-secret test-secret \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"
  assert_eq "D7 auth multi-source exit0" 0 "$?"
  assert_contains "D7 multi-source poll succeeded" "$d/e" 'pressure api status=ok'; }
D8() { local d="$OUT/D8"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -pressure-use-auth -pressure-auth-url "http://$PA1:8080/auth" -pressure-data-url "http://$PA1:8080/data" \
    -pressure-client-id test-client -pressure-client-secret WRONG-SECRET \
    -pressure-interval 1s -timeout 1s -workers 1 -disable-pre-scan-ping -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_eq "D8 bad-auth fail-safe abort exit1" 1 "$rc"
  assert_contains "D8 pressure failed 3 times" "$d/e" 'pressure api failed 3 times'; }

# ---------------- E: resume ----------------
E1() { local d="$OUT/E1"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -disable-api -disable-pre-scan-ping -timeout 1s -workers 1 -output "$d/scan.csv" >"$d/r1o" 2>"$d/r1e" &
  local pid=$!; sleep 3; kill -INT "$pid" 2>/dev/null; wait "$pid"; local rc1=$?
  assert_eq "E1 interrupted exit130" 130 "$rc1"
  assert_file_exists "E1 resume_state.json written" "$d/resume_state.json"
  port-scan scan -cidr-file "$FIX/basic-filtered-many.csv" -port-file "$FIX/ports-many.csv" \
    -disable-api -disable-pre-scan-ping -timeout 1s -workers 1 -resume "$d/resume_state.json" -output "$d/scan.csv" >"$d/r2o" 2>"$d/r2e"
  assert_eq "E1 resumed completes exit0" 0 "$?"; }
E2() { local d="$OUT/E2"; mkdir -p "$d"
  port-scan scan -cidr-file "$FIX/basic-open.csv" -port-file "$FIX/ports.csv" \
    -disable-api -disable-pre-scan-ping -resume "$FIX/resume-mismatch.json" -output "$d/s.csv" >"$d/o" 2>"$d/e"; local rc=$?
  assert_ne "E2 mismatch nonzero exit" 0 "$rc"
  assert_contains "E2 total_count mismatch error" "$d/e" 'chunk total_count mismatch'; }

# ---------------- F: preprocess ----------------
F1() { local d="$OUT/F1"; mkdir -p "$d"
  preprocess --input "$FIX/rich.csv" --cleaned-cidrs "$FIX/cleaned-cidrs.csv" --fab-name fab1 --output-dir "$d" >"$d/o" 2>"$d/e"
  assert_eq "F1 preprocess exit0" 0 "$?"
  local out; out="$(find "$d/fab1" -name input.csv 2>/dev/null | head -n1)"
  assert_file_exists  "F1 output produced"         "$out"
  assert_contains     "F1 keeps non-closed .10"    "$out" "$OPEN"
  assert_not_contains "F1 removes closed .12"      "$out" "$FILTERED"; }
F2() { local d="$OUT/F2"; mkdir -p "$d"
  preprocess --input "$FIX/rich.csv" --cleaned-cidrs "$FIX/cleaned-cidrs.csv" --fab-name NOPE --output-dir "$d" >"$d/o" 2>"$d/e"
  assert_eq "F2 preprocess no-match exit0" 0 "$?"
  local out; out="$(find "$d/NOPE" -name input.csv 2>/dev/null | head -n1)"
  assert_contains "F2 all rows kept when fab no-match" "$out" "$FILTERED"; }

# ---------------- G: enrich-targets ----------------
G1() { local d="$OUT/G1"; mkdir -p "$d"
  enrich-targets --input "$FIX/minimal.csv" --cidr-list "$FIX/cidrs.csv" --service-map "$FIX/services.csv" --output "$d/enriched.csv" >"$d/o" 2>"$d/e"
  assert_eq "G1 enrich exit0" 0 "$?"
  assert_contains "G1 enriched .10 accept tcp" "$d/enriched.csv" "$OPEN,.*,8080,accept,"
  assert_contains "G1 service_label mapped"    "$d/enriched.csv" "http-test"; }
G2() { local d="$OUT/G2"; mkdir -p "$d"
  enrich-targets --input "$FIX/minimal-mixed.csv" --cidr-list "$FIX/cidrs.csv" --service-map "$FIX/services.csv" --output "$d/enriched.csv" >"$d/o" 2>"$d/e"
  assert_eq "G2 enrich exit0 (skips bad rows)" 0 "$?"
  assert_contains     "G2 valid row enriched"   "$d/enriched.csv" "$OPEN"
  assert_not_contains "G2 invalid host skipped" "$d/enriched.csv" "not-an-ip"; }

# ---------------- H: cidr-compare ----------------
H1() { local d="$OUT/H1"; mkdir -p "$d"
  cidr-compare -deny-file "$FIX/deny.csv" -open-file "$FIX/open.csv" >"$d/o" 2>"$d/e"
  assert_eq "H1 cidr-compare exit0" 0 "$?"
  assert_contains "H1 header line"      "$d/o" '^deny_cidr,open_cidr'
  assert_contains "H1 containment row"  "$d/o" "172.30.0.0/24,$FILTERED/32"; }
H2() { local d="$OUT/H2"; mkdir -p "$d"
  CIDR_COMPARE_DENY_FILE="$FIX/deny.csv" CIDR_COMPARE_OPEN_FILE="$FIX/open.csv" cidr-compare >"$d/o" 2>"$d/e"
  assert_eq "H2 env-form exit0" 0 "$?"
  assert_contains "H2 env-form containment row" "$d/o" "172.30.0.0/24,$FILTERED/32"; }
H3() { local d="$OUT/H3"; mkdir -p "$d"
  cidr-compare -deny-file "$FIX/deny-none.csv" -open-file "$FIX/open.csv" >"$d/o" 2>"$d/e"
  assert_eq "H3 no-overlap exit0" 0 "$?"
  assert_eq "H3 only header (no rows)" "1" "$(wc -l <"$d/o" | tr -d ' ')"; }

# ---------------- I: csv-transform ----------------
I1() { local d="$OUT/I1"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy.csv" --output "$d/t.csv" >"$d/o" 2>"$d/e"
  assert_eq "I1 csv-transform exit0" 0 "$?"
  assert_contains "I1 FALSE row .10 included" "$d/t.csv" "$OPEN"; }
I2() { local d="$OUT/I2"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy-custom.csv" --output "$d/t.csv" --host-col H --port-col P --pass-col Result >"$d/o" 2>"$d/e"
  assert_eq "I2 custom-cols exit0" 0 "$?"
  assert_contains "I2 custom-cols .10 included" "$d/t.csv" "$OPEN"; }
I3() { local d="$OUT/I3"; mkdir -p "$d"
  csv-transform --input "$FIX/legacy.csv" --output "$d/t.csv" >"$d/o" 2>"$d/e"
  assert_not_contains "I3 TRUE row .11 skipped" "$d/t.csv" "$CLOSED"; }
I4() { local d="$OUT/I4"; mkdir -p "$d"
  TRANSFORM_INPUT="$FIX/legacy.csv" TRANSFORM_OUTPUT="$d/t.csv" csv-transform >"$d/o" 2>"$d/e"
  assert_eq "I4 env-form exit0" 0 "$?"
  assert_contains     "I4 env-form .10 included" "$d/t.csv" "$OPEN"
  assert_not_contains "I4 env-form .11 skipped"  "$d/t.csv" "$CLOSED"; }

for c in A1 A2 A3 A4 B_states B4 B5 B6 B7 B8 B9 C1 C2 \
         D1 D2 D3 D4 D5 D6 D7 D8 E1 E2 F1 F2 G1 G2 H1 H2 H3 I1 I2 I3 I4; do
  "$c"
done

summary
```

- [ ] **Step 2: Make executable + commit**

```bash
chmod +x labs/port-scan-mk3-cli-matrix/scripts/run-matrix.sh
git add labs/port-scan-mk3-cli-matrix/scripts/run-matrix.sh
git commit -m "feat(lab): add 36-case CLI matrix driver"
```

---

### Task 9: host smoke-test.sh

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/scripts/smoke-test.sh`

- [ ] **Step 1: Write `smoke-test.sh`** (black-box; invoked by `validate_lab.sh` from the lab dir after `up -d --wait`)

```bash
#!/usr/bin/env bash
# smoke-test.sh — property check run from the HOST. Executes the full CLI matrix inside the
# long-lived scanner container. Bounded by `timeout` so it never hangs (the matrix includes
# several deliberate multi-second pressure/resume scans, hence > the 60s default).
set -uo pipefail

echo "=== port-scan-mk3 CLI matrix smoke test ==="
RC=0
timeout 300s docker compose exec -T scanner bash /lab/scripts/run-matrix.sh || RC=$?

if [ "$RC" -ne 0 ]; then
  echo "smoke test FAILED (rc=$RC) — recent service logs:" >&2
  docker compose logs --tail=50
  exit 1
fi
echo "smoke test PASSED — all 36 CLI matrix cases observed expected output."
exit 0
```

- [ ] **Step 2: Make executable + commit**

```bash
chmod +x labs/port-scan-mk3-cli-matrix/scripts/smoke-test.sh
git add labs/port-scan-mk3-cli-matrix/scripts/smoke-test.sh
git commit -m "feat(lab): add host smoke-test driver"
```

---

### Task 10: README.md

**Files:**
- Create: `labs/port-scan-mk3-cli-matrix/README.md`

- [ ] **Step 1: Write `README.md`**

````markdown
# port-scan-mk3-cli-matrix

## What this demonstrates

port-scan-mk3's five CLI binaries produce contract-conformant output across their full
documented flag surface — correct port-state classification (open / close / close(timeout) /
unreachable), correct pressure-driven pause/resume and fail-safe abort, and correct CSV/CIDR
transforms — when run against deterministic mock targets and pressure APIs.

## Run it

```bash
docker compose up -d --wait
docker compose exec -T scanner bash /lab/scripts/run-matrix.sh   # 36-case matrix
```

Or run the full validated cycle from the repo root:

```bash
bash ~/.claude/skills/research-lab/scripts/validate_lab.sh labs/port-scan-mk3-cli-matrix
```

## Expected output

```
PASS A1 validate basic human exit0
PASS B1 open in opened_results
PASS B3 filtered close(timeout)
PASS C2 .99 marked unreachable
PASS D2 scan paused on high pressure
PASS D3 pressure failed 3 times
...
RESULT: PASS=NN FAIL=0 TOTAL=NN
smoke test PASSED — all 36 CLI matrix cases observed expected output.
```

## What's covered

| Group | Binary / feature | Cases |
|---|---|---|
| A | `port-scan validate` (human/json/rich/invalid) | 4 |
| B | `port-scan scan` modes & I/O flags | 9 |
| C | pre-scan ping reachability gating | 2 |
| D | pressure control (simple/auth/multi-source/fail-safe) | 8 |
| E | resume (authentic SIGINT + mismatch guard) | 2 |
| F | `preprocess` | 2 |
| G | `enrich-targets` | 2 |
| H | `cidr-compare` (flag + env forms) | 3 |
| I | `csv-transform` (custom cols + env forms) | 4 |

## Notes

- `target-filtered` uses in-container `iptables DROP` (`cap_add: NET_ADMIN`) to produce real
  connect timeouts; the scanner uses `cap_add: NET_RAW` so pre-scan `ping` works. Both are
  container-namespace capabilities — no host changes.
- Pressure healthchecks hit a non-consuming `/healthz` so they never advance `PRESSURE_SEQUENCE`.
- See `.env.example` for every tunable env var and `RESEARCH.md` for design rationale.

## Teardown

```bash
docker compose down -v --remove-orphans
```
````

- [ ] **Step 2: Commit**

```bash
git add labs/port-scan-mk3-cli-matrix/README.md
git commit -m "docs(lab): add README"
```

---

### Task 11: End-to-end validation

**Files:** none (validation only).

- [ ] **Step 1: Run the skill validator**

Run (from repo root):
```bash
bash ~/.claude/skills/research-lab/scripts/validate_lab.sh labs/port-scan-mk3-cli-matrix
```
Expected: ends with `=== validate_lab.sh: PASS ===` and exit code 0. The teardown trap removes all containers/volumes.

- [ ] **Step 2: On failure, diagnose without masking**

If a step fails:
- `docker compose -f labs/port-scan-mk3-cli-matrix/docker-compose.yml logs <service>` for the failing service.
- Re-run a single case interactively: `cd labs/port-scan-mk3-cli-matrix && docker compose up -d --wait && docker compose exec -T scanner bash -c 'source /lab/scripts/lib/assert.sh; ...'`.
- Common root causes to check (do NOT fix by weakening assertions/healthchecks/timeouts):
  - C1 fails (reachable host shows unreachable) → scanner missing `cap_add: NET_RAW` or `iputils`.
  - B3/D2/D3 timing → confirm `target-filtered` `FILTERED_PORTS` matches `ports-many.csv` exactly.
  - D2 no pause/resume → confirm healthcheck uses `/healthz`, not `/api/pressure` (sequence consumption).
  - Pressure log assertions → the pause/resume lines use an em dash; the patterns use `.*` to avoid matching it byte-for-byte. Keep that.
- After 3 failed attempts on the same root cause, invoke `superpowers:systematic-debugging`.

- [ ] **Step 3: Confirm clean teardown**

Run:
```bash
docker ps -a | grep lab-port-scan-mk3-cli-matrix || echo "NO_LEFTOVER_CONTAINERS"
docker volume ls | grep lab-port-scan-mk3-cli-matrix || echo "NO_LEFTOVER_VOLUMES"
git -C labs/port-scan-mk3-cli-matrix status --porcelain
```
Expected: `NO_LEFTOVER_CONTAINERS`, `NO_LEFTOVER_VOLUMES`, and no untracked debug artifacts (only intended files tracked).

- [ ] **Step 4: Commit any fixes made during validation**

```bash
git add -A labs/port-scan-mk3-cli-matrix
git commit -m "fix(lab): validation adjustments" || echo "nothing to commit"
```

---

### Task 12: Finish the branch

- [ ] **Step 1:** Invoke `superpowers:finishing-a-development-branch` to choose merge / PR / cleanup for branch `lab/port-scan-mk3-cli-matrix`.

---

## Self-review (performed against the spec)

**Spec coverage:** Every spec section maps to a task — topology→Task 6; 3 images→Tasks 2/3/4; 5 Codex decisions→ NET_ADMIN (Task 6 target-filtered), ping/TCP split (matrix groups B vs C), authentic+mismatch resume (E1/E2), two auth containers (Task 6 pressure-auth-1/2), `/healthz` (Task 3); 36-case matrix→Task 8 (counts A4 B9 C2 D8 E2 F2 G2 H3 I4 = 4+9+2+8+2+2+2+3+4 = 36); fixtures→Task 5; validation→Task 11; host-isolation→compose has no privileged/host-net/system-mounts/published-ports, caps are container-scoped.

**Placeholder scan:** No TBD/TODO; all file contents and commands are concrete.

**Type/name consistency:** Service DNS names (`pressure-ok`, `pressure-auth-1`, …, `target-open`) are identical across compose and `run-matrix.sh`; image names (`psmk3-mock-target`, `psmk3-mock-pressure`, `psmk3-scanner`) consistent; CSV headers/status strings/log patterns match the verified contracts; resume JSON tags match `pkg/task/types.go`; chunk `total_count` 999 vs expected 2 (1 IP × 2 ports in `basic-open.csv`/`ports.csv`) yields the mismatch error asserted in E2.

**Known design choices (intentional):** D3/D4/D8 assert the 3-strike fail-safe abort (exit 1), not silent tolerance, per `pressure_monitor.go`. Long pressure/resume scans use the filtered target with `-workers 1 -timeout 1s` for deterministic multi-second runtime; the host smoke test uses `timeout 300s` (above the 60s default) because of these deliberate waits.

---

## Errata — changes made during implementation/validation (2026-06-23)

The shipped lab differs from the task text above in these ways (the running lab is the source of truth; `validate_lab.sh` → 81/81, exit 0):

1. **B4 rich-mode contract corrected.** Rich mode scans **all `tcp` rows regardless of `decision`** and carries `decision` as output metadata; only non-`tcp` rows are excluded. B4 now asserts deny `.11` IS scanned (`close`, decision=deny) and udp `.12` is excluded. (The "Exact contracts" rich-mode bullet was corrected accordingly.) Scanning `decision=deny` paths is flagged for product review.
2. **D4** uses `-timeout 3s` (was `1s`) so the scan outlives the ~7s to the 3rd timeout-mode pressure failure; otherwise the 6s scan finished first.
3. **D2** depends on `pressure-high` oscillating `90,10` with `loop=true` (compose) so pause+resume are deterministic regardless of prior GET consumption of the sequence.
4. **mock-pressure** gained stateless bearer-token validation in `/data` (accept any `mock-token-*`) so the two auth containers share a trust domain (required for D7 multi-source). D8 still fails correctly (bad creds rejected at `/auth`).
5. **I3** gained an explicit `exit 0` assertion (previously could false-pass on a missing output file).
6. **Product fix (`cmd/csv-transform`)**, commit `2c58399`: `runMain` now strips `argv[0]` before flag parsing, fixing the broken `--input/--output` CLI form (only env vars worked before). Added regression test `TestRunMain_FlagArgsParsed` (RED→GREEN). This was a real bug the lab discovered.
