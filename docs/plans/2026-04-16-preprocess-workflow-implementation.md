# Preprocess Workflow Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build two CLI tools (`enrich-targets` and `preprocess`) that prepare input CSVs for the port-scan tool, with a shared configuration package for all column names and constants.

**Architecture:** Three new packages under `pkg/` — `preprocesscfg` (centralized constants), `enrich` (host,port enrichment to rich CSV), `preprocess` (closed-CIDR filtering). Two thin CLI wrappers under `cmd/`. Both domain packages depend on the existing `pkg/cidrutil` interval tree for CIDR containment queries.

**Tech Stack:** Go 1.24, `encoding/csv`, `flag`, `pkg/cidrutil.IntervalTree`

---

### Task 1: Create `pkg/preprocesscfg` — Centralized Constants

**Files:**
- Create: `pkg/preprocesscfg/config.go`

**Step 1: Create the constants package**

```go
package preprocesscfg

// Rich CSV output columns.
var (
	ColSrcIP             = "src_ip"
	ColSrcNetworkSegment = "src_network_segment"
	ColDstIP             = "dst_ip"
	ColDstNetworkSegment = "dst_network_segment"
	ColServiceLabel      = "service_label"
	ColProtocol          = "protocol"
	ColPort              = "port"
	ColDecision          = "decision"
	ColMatchedPolicyID   = "matched_policy_id"
	ColReason            = "reason"
)

// RichHeader returns the canonical rich CSV header row in column order.
func RichHeader() []string {
	return []string{
		ColSrcIP, ColSrcNetworkSegment,
		ColDstIP, ColDstNetworkSegment,
		ColServiceLabel, ColProtocol, ColPort,
		ColDecision, ColMatchedPolicyID, ColReason,
	}
}

// Opened targets input columns.
var (
	ColHost      = "host"
	ColPortInput = "port"
)

// Cleaned CIDRs columns.
var (
	ColFab    = "fab"
	ColCIDR   = "segment"
	ColStatus = "status"
)

// Service map columns.
var (
	ColServicePort = "port"
	ColServiceName = "service_label"
)

// CIDR status values.
var (
	StatusOpen  = "open"
	StatusClose = "close"
)

// Placeholder and default values for enrichment.
var (
	DefaultSrcIP             = "10.59.42.39"
	DefaultSrcNetworkSegment = "10.59.42.39/32"
	DefaultProtocol          = "tcp"
	DefaultDecision          = "accept"
	DefaultPolicyID          = "enriched"
	DefaultReason            = "MATCH_POLICY_ACCEPT"
	FallbackServiceLabel     = "unknown"
)
```

**Step 2: Run build to verify**

Run: `go build ./pkg/preprocesscfg/`
Expected: no errors

**Step 3: Commit**

```
git add pkg/preprocesscfg/config.go
git commit -m "feat(preprocesscfg): add centralized column names and constants"
```

---

### Task 2: Create `pkg/enrich` — Service Map Loader

**Files:**
- Create: `pkg/enrich/loader.go`
- Create: `pkg/enrich/loader_test.go`

**Step 1: Write the failing tests**

```go
package enrich

import (
	"strings"
	"testing"
)

func TestLoadServiceMap_ValidCSV(t *testing.T) {
	csv := "port,service_label\n22,SSH\n80,HTTP\n443,HTTPS\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m))
	}
	if m[22] != "SSH" {
		t.Errorf("port 22: expected SSH, got %s", m[22])
	}
	if m[80] != "HTTP" {
		t.Errorf("port 80: expected HTTP, got %s", m[80])
	}
	if m[443] != "HTTPS" {
		t.Errorf("port 443: expected HTTPS, got %s", m[443])
	}
}

func TestLoadServiceMap_MissingHeader(t *testing.T) {
	csv := "a,b\n22,SSH\n"
	_, err := LoadServiceMap(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing header columns")
	}
}

func TestLoadServiceMap_InvalidPort(t *testing.T) {
	csv := "port,service_label\nabc,SSH\n80,HTTP\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 entry (skipping invalid), got %d", len(m))
	}
	if m[80] != "HTTP" {
		t.Errorf("port 80: expected HTTP, got %s", m[80])
	}
}

func TestLoadServiceMap_Empty(t *testing.T) {
	csv := "port,service_label\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(m))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestLoadServiceMap ./pkg/enrich/`
Expected: FAIL — `LoadServiceMap` not defined

**Step 3: Write the loader functions**

In `pkg/enrich/loader.go`:

```go
package enrich

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// LoadServiceMap reads a CSV with port and service_label columns and returns
// a map from port number to service label. Rows with invalid port numbers are
// skipped with a log warning.
func LoadServiceMap(r io.Reader) (map[int]string, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading service map CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("service map CSV is empty")
	}

	header := rows[0]
	portIdx, labelIdx := -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColServicePort):
			portIdx = i
		case strings.ToLower(preprocesscfg.ColServiceName):
			labelIdx = i
		}
	}
	if portIdx < 0 || labelIdx < 0 {
		return nil, fmt.Errorf("service map CSV missing required columns %q and %q",
			preprocesscfg.ColServicePort, preprocesscfg.ColServiceName)
	}

	m := make(map[int]string)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) <= portIdx || len(row) <= labelIdx {
			log.Printf("service map row %d: too few columns, skipping", i+1)
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(row[portIdx]))
		if err != nil {
			log.Printf("service map row %d: invalid port %q, skipping", i+1, row[portIdx])
			continue
		}
		m[port] = strings.TrimSpace(row[labelIdx])
	}
	return m, nil
}

// LoadCIDRList reads a CSV listing CIDRs and returns them as an IntervalTree.
// The first column is used as the CIDR value. Malformed CIDRs are skipped with
// a log warning.
func LoadCIDRList(r io.Reader) (*cidrutil.IntervalTree, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CIDR list CSV: %w", err)
	}

	tree := &cidrutil.IntervalTree{}
	start := 0
	if len(rows) > 0 {
		// Skip header if first cell looks like a header (not a valid CIDR)
		if _, parseErr := cidrutil.ParseCIDR(strings.TrimSpace(rows[0][0])); parseErr != nil {
			start = 1
		}
	}

	for i := start; i < len(rows); i++ {
		if len(rows[i]) == 0 {
			continue
		}
		raw := strings.TrimSpace(rows[i][0])
		if raw == "" {
			continue
		}
		entry, err := cidrutil.ParseCIDR(raw)
		if err != nil {
			log.Printf("CIDR list row %d: invalid CIDR %q, skipping", i+1, raw)
			continue
		}
		tree.Insert(entry)
	}
	return tree, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestLoadServiceMap ./pkg/enrich/`
Expected: PASS

**Step 5: Write CIDR list loader tests**

Add to `pkg/enrich/loader_test.go`:

```go
func TestLoadCIDRList_ValidCSV(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Query for an IP inside 10.0.0.0/8
	q, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	matches := tree.Query(q)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", matches[0].Network)
	}
}

func TestLoadCIDRList_NoHeader(t *testing.T) {
	// First row is a valid CIDR — treated as data, not header
	csv := "10.0.0.0/8\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	matches := tree.Query(q)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestLoadCIDRList_MalformedSkipped(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\nnot-a-cidr\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have 2 valid entries
	q1, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	q2, _ := cidrutil.ParseCIDR("192.168.1.1/32")
	if len(tree.Query(q1)) != 1 {
		t.Error("expected match for 10.1.2.3 in 10.0.0.0/8")
	}
	if len(tree.Query(q2)) != 1 {
		t.Error("expected match for 192.168.1.1 in 192.168.1.0/24")
	}
}
```

**Step 6: Run all loader tests**

Run: `go test -v -run "TestLoad" ./pkg/enrich/`
Expected: PASS

**Step 7: Commit**

```
git add pkg/enrich/loader.go pkg/enrich/loader_test.go
git commit -m "feat(enrich): add service map and CIDR list loaders"
```

---

### Task 3: Create `pkg/enrich` — Enricher Core

**Files:**
- Create: `pkg/enrich/enricher.go`
- Create: `pkg/enrich/enricher_test.go`

**Step 1: Write the failing tests**

```go
package enrich

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func buildTree(cidrs ...string) *cidrutil.IntervalTree {
	tree := &cidrutil.IntervalTree{}
	for _, c := range cidrs {
		entry, _ := cidrutil.ParseCIDR(c)
		tree.Insert(entry)
	}
	return tree
}

func TestEnrich_FullMatch(t *testing.T) {
	tree := buildTree("10.0.0.0/8", "10.1.0.0/16")
	svc := map[int]string{22: "SSH", 80: "HTTP"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.1.2.3", 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.DstIP != "10.1.2.3" {
		t.Errorf("DstIP: expected 10.1.2.3, got %s", row.DstIP)
	}
	// Should pick most specific CIDR: 10.1.0.0/16
	if row.DstNetworkSegment != "10.1.0.0/16" {
		t.Errorf("DstNetworkSegment: expected 10.1.0.0/16, got %s", row.DstNetworkSegment)
	}
	if row.ServiceLabel != "SSH" {
		t.Errorf("ServiceLabel: expected SSH, got %s", row.ServiceLabel)
	}
	if row.Port != "22" {
		t.Errorf("Port: expected 22, got %s", row.Port)
	}
	if row.SrcIP != preprocesscfg.DefaultSrcIP {
		t.Errorf("SrcIP: expected %s, got %s", preprocesscfg.DefaultSrcIP, row.SrcIP)
	}
	if row.SrcNetworkSegment != preprocesscfg.DefaultSrcNetworkSegment {
		t.Errorf("SrcNetworkSegment: expected %s, got %s", preprocesscfg.DefaultSrcNetworkSegment, row.SrcNetworkSegment)
	}
	if row.Protocol != preprocesscfg.DefaultProtocol {
		t.Errorf("Protocol: expected %s, got %s", preprocesscfg.DefaultProtocol, row.Protocol)
	}
	if row.Decision != preprocesscfg.DefaultDecision {
		t.Errorf("Decision: expected %s, got %s", preprocesscfg.DefaultDecision, row.Decision)
	}
	if row.MatchedPolicyID != preprocesscfg.DefaultPolicyID {
		t.Errorf("MatchedPolicyID: expected %s, got %s", preprocesscfg.DefaultPolicyID, row.MatchedPolicyID)
	}
	if row.Reason != preprocesscfg.DefaultReason {
		t.Errorf("Reason: expected %s, got %s", preprocesscfg.DefaultReason, row.Reason)
	}
}

func TestEnrich_NoCIDRMatch_FallbackSlash32(t *testing.T) {
	tree := buildTree("192.168.0.0/16")
	svc := map[int]string{80: "HTTP"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.5.6.7", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.DstNetworkSegment != "10.5.6.7/32" {
		t.Errorf("expected fallback 10.5.6.7/32, got %s", row.DstNetworkSegment)
	}
}

func TestEnrich_NoServiceMatch_FallbackUnknown(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{22: "SSH"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.1.2.3", 9999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.ServiceLabel != preprocesscfg.FallbackServiceLabel {
		t.Errorf("expected %s, got %s", preprocesscfg.FallbackServiceLabel, row.ServiceLabel)
	}
}

func TestEnrich_InvalidHost(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("not-an-ip", 80)
	if err == nil {
		t.Fatal("expected error for invalid host IP")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestEnrich ./pkg/enrich/`
Expected: FAIL — types and functions not defined

**Step 3: Implement the enricher**

In `pkg/enrich/enricher.go`:

```go
package enrich

import (
	"fmt"
	"net"
	"strconv"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// RichRow represents a single row of the 10-column rich CSV output.
type RichRow struct {
	SrcIP             string
	SrcNetworkSegment string
	DstIP             string
	DstNetworkSegment string
	ServiceLabel      string
	Protocol          string
	Port              string
	Decision          string
	MatchedPolicyID   string
	Reason            string
}

// ToSlice returns the row fields in canonical rich CSV column order.
func (r RichRow) ToSlice() []string {
	return []string{
		r.SrcIP, r.SrcNetworkSegment,
		r.DstIP, r.DstNetworkSegment,
		r.ServiceLabel, r.Protocol, r.Port,
		r.Decision, r.MatchedPolicyID, r.Reason,
	}
}

// Enricher transforms host,port pairs into full rich CSV rows using a CIDR
// reference tree and a port-to-service-label map.
type Enricher struct {
	cidrTree   *cidrutil.IntervalTree
	serviceMap map[int]string
}

// NewEnricher creates an Enricher with the given CIDR tree and service map.
func NewEnricher(cidrTree *cidrutil.IntervalTree, serviceMap map[int]string) *Enricher {
	return &Enricher{cidrTree: cidrTree, serviceMap: serviceMap}
}

// Enrich produces a RichRow from a host IP and port number.
// It never skips rows — missing CIDR matches fall back to host/32, missing
// service labels fall back to the configured fallback value.
func (e *Enricher) Enrich(host string, port int) (RichRow, error) {
	ip := net.ParseIP(host)
	if ip == nil {
		return RichRow{}, fmt.Errorf("invalid host IP: %q", host)
	}
	ip = ip.To4()
	if ip == nil {
		return RichRow{}, fmt.Errorf("host %q is not IPv4", host)
	}

	dstSegment := e.findSmallestCIDR(host)

	svcLabel, ok := e.serviceMap[port]
	if !ok {
		svcLabel = preprocesscfg.FallbackServiceLabel
	}

	return RichRow{
		SrcIP:             preprocesscfg.DefaultSrcIP,
		SrcNetworkSegment: preprocesscfg.DefaultSrcNetworkSegment,
		DstIP:             host,
		DstNetworkSegment: dstSegment,
		ServiceLabel:      svcLabel,
		Protocol:          preprocesscfg.DefaultProtocol,
		Port:              strconv.Itoa(port),
		Decision:          preprocesscfg.DefaultDecision,
		MatchedPolicyID:   preprocesscfg.DefaultPolicyID,
		Reason:            preprocesscfg.DefaultReason,
	}, nil
}

// findSmallestCIDR queries the CIDR tree for all CIDRs containing the host
// and returns the most specific (largest prefix length). Falls back to host/32.
func (e *Enricher) findSmallestCIDR(host string) string {
	hostCIDR := host + "/32"
	entry, err := cidrutil.ParseCIDR(hostCIDR)
	if err != nil {
		return hostCIDR
	}

	matches := e.cidrTree.Query(entry)
	if len(matches) == 0 {
		return hostCIDR
	}

	// Find the match with the smallest range (most specific).
	best := matches[0]
	bestRange := best.EndIP - best.StartIP
	for _, m := range matches[1:] {
		r := m.EndIP - m.StartIP
		if r < bestRange {
			best = m
			bestRange = r
		}
	}
	return best.Network
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestEnrich ./pkg/enrich/`
Expected: PASS

**Step 5: Commit**

```
git add pkg/enrich/enricher.go pkg/enrich/enricher_test.go
git commit -m "feat(enrich): add enricher core with CIDR lookup and service mapping"
```

---

### Task 4: Create `pkg/preprocess` — Cleaned CIDRs Loader

**Files:**
- Create: `pkg/preprocess/loader.go`
- Create: `pkg/preprocess/loader_test.go`

**Step 1: Write the failing tests**

```go
package preprocess

import (
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func TestLoadCleanedCIDRs_FiltersByFabAndClose(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,close\ndc-east,10.1.0.0/16,open\ndc-west,192.168.0.0/24,close\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 10.0.0.0/16 is closed in dc-east — should be in tree
	q1, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q1)) != 1 {
		t.Error("expected 10.0.1.0/24 to match closed 10.0.0.0/16")
	}
	// 10.1.0.0/16 is open — should NOT be in tree
	q2, _ := cidrutil.ParseCIDR("10.1.1.0/24")
	if len(tree.Query(q2)) != 0 {
		t.Error("expected no match for open CIDR 10.1.0.0/16")
	}
	// 192.168.0.0/24 is closed but in dc-west — should NOT be in tree
	q3, _ := cidrutil.ParseCIDR("192.168.0.1/32")
	if len(tree.Query(q3)) != 0 {
		t.Error("expected no match for dc-west CIDR when filtering dc-east")
	}
}

func TestLoadCleanedCIDRs_MissingColumns(t *testing.T) {
	csv := "a,b,c\n1,2,3\n"
	_, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
}

func TestLoadCleanedCIDRs_EmptyResult(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,open\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 0 {
		t.Error("expected no matches when all CIDRs are open")
	}
}

func TestLoadCleanedCIDRs_CaseInsensitiveStatus(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,CLOSE\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 1 {
		t.Error("expected match for case-insensitive CLOSE status")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestLoadCleanedCIDRs ./pkg/preprocess/`
Expected: FAIL — `LoadCleanedCIDRs` not defined

**Step 3: Implement the loader**

In `pkg/preprocess/loader.go`:

```go
package preprocess

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// LoadCleanedCIDRs reads a cleaned CIDRs CSV, filters to rows matching fabName
// with status "close", and returns an IntervalTree of the closed CIDRs.
func LoadCleanedCIDRs(r io.Reader, fabName string) (*cidrutil.IntervalTree, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading cleaned CIDRs CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("cleaned CIDRs CSV is empty")
	}

	header := rows[0]
	fabIdx, cidrIdx, statusIdx := -1, -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColFab):
			fabIdx = i
		case strings.ToLower(preprocesscfg.ColCIDR):
			cidrIdx = i
		case strings.ToLower(preprocesscfg.ColStatus):
			statusIdx = i
		}
	}
	if fabIdx < 0 || cidrIdx < 0 || statusIdx < 0 {
		return nil, fmt.Errorf("cleaned CIDRs CSV missing required columns %q, %q, %q",
			preprocesscfg.ColFab, preprocesscfg.ColCIDR, preprocesscfg.ColStatus)
	}

	tree := &cidrutil.IntervalTree{}
	fabLower := strings.ToLower(strings.TrimSpace(fabName))

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		maxIdx := max(fabIdx, cidrIdx, statusIdx)
		if len(row) <= maxIdx {
			log.Printf("cleaned CIDRs row %d: too few columns, skipping", i+1)
			continue
		}

		fab := strings.TrimSpace(strings.ToLower(row[fabIdx]))
		if fab != fabLower {
			continue
		}

		status := strings.TrimSpace(strings.ToLower(row[statusIdx]))
		if status != strings.ToLower(preprocesscfg.StatusClose) {
			continue
		}

		raw := strings.TrimSpace(row[cidrIdx])
		entry, err := cidrutil.ParseCIDR(raw)
		if err != nil {
			log.Printf("cleaned CIDRs row %d: invalid CIDR %q, skipping", i+1, raw)
			continue
		}
		tree.Insert(entry)
	}

	return tree, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestLoadCleanedCIDRs ./pkg/preprocess/`
Expected: PASS

**Step 5: Commit**

```
git add pkg/preprocess/loader.go pkg/preprocess/loader_test.go
git commit -m "feat(preprocess): add cleaned CIDRs loader with fab filtering"
```

---

### Task 5: Create `pkg/preprocess` — Filter Core

**Files:**
- Create: `pkg/preprocess/filter.go`
- Create: `pkg/preprocess/filter_test.go`

**Step 1: Write the failing tests**

```go
package preprocess

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func buildClosedTree(cidrs ...string) *cidrutil.IntervalTree {
	tree := &cidrutil.IntervalTree{}
	for _, c := range cidrs {
		entry, _ := cidrutil.ParseCIDR(c)
		tree.Insert(entry)
	}
	return tree
}

func TestFilter_KeepWhenNotInClosedCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	keep, err := f.Keep("192.168.1.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !keep {
		t.Error("expected keep=true for CIDR not in closed tree")
	}
}

func TestFilter_DropWhenContainedInClosedCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	keep, err := f.Keep("10.1.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keep {
		t.Error("expected keep=false for CIDR contained in closed 10.0.0.0/8")
	}
}

func TestFilter_DropOnExactMatch(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/16")
	f := NewFilter(tree)

	keep, err := f.Keep("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keep {
		t.Error("expected keep=false for exact CIDR match")
	}
}

func TestFilter_KeepAllWhenNoClosedCIDRs(t *testing.T) {
	tree := &cidrutil.IntervalTree{}
	f := NewFilter(tree)

	keep, err := f.Keep("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !keep {
		t.Error("expected keep=true when no closed CIDRs exist")
	}
}

func TestFilter_InvalidCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	_, err := f.Keep("not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestFilter ./pkg/preprocess/`
Expected: FAIL — `Filter` and `NewFilter` not defined

**Step 3: Implement the filter**

In `pkg/preprocess/filter.go`:

```go
package preprocess

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

// Filter checks whether targets should be kept or dropped based on a tree
// of closed CIDRs.
type Filter struct {
	closedTree *cidrutil.IntervalTree
}

// NewFilter creates a Filter from an IntervalTree of closed CIDRs.
func NewFilter(closedTree *cidrutil.IntervalTree) *Filter {
	return &Filter{closedTree: closedTree}
}

// Keep returns true if dstNetworkSegment is NOT contained within any closed CIDR.
// Returns an error if the segment string cannot be parsed.
func (f *Filter) Keep(dstNetworkSegment string) (bool, error) {
	entry, err := cidrutil.ParseCIDR(dstNetworkSegment)
	if err != nil {
		return false, fmt.Errorf("parsing dst_network_segment %q: %w", dstNetworkSegment, err)
	}
	matches := f.closedTree.Query(entry)
	return len(matches) == 0, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestFilter ./pkg/preprocess/`
Expected: PASS

**Step 5: Commit**

```
git add pkg/preprocess/filter.go pkg/preprocess/filter_test.go
git commit -m "feat(preprocess): add CIDR containment filter"
```

---

### Task 6: Create `pkg/preprocess` — Output Path & Writer

**Files:**
- Create: `pkg/preprocess/output.go`
- Create: `pkg/preprocess/output_test.go`

**Step 1: Write the failing tests**

```go
package preprocess

import (
	"testing"
	"time"
)

func TestOutputPath(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	got := OutputPath("/data/out", "dc-east", ts)
	expected := "/data/out/dc-east/20260416T153000Z/input.csv"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestOutputPath_SpecialCharsInFab(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := OutputPath("/out", "fab-1", ts)
	expected := "/out/fab-1/20260102T030405Z/input.csv"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run TestOutputPath ./pkg/preprocess/`
Expected: FAIL — `OutputPath` not defined

**Step 3: Implement output path and writer**

In `pkg/preprocess/output.go`:

```go
package preprocess

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

// OutputPath returns the canonical output file path for a given base directory,
// fab name, and timestamp.
func OutputPath(baseDir, fabName string, ts time.Time) string {
	return filepath.Join(baseDir, fabName, ts.Format("20060102T150405Z"), "input.csv")
}

// CreateOutputWriter creates the output directory and returns a csv.Writer
// for the output file. The caller must call Flush and close the file.
func CreateOutputWriter(path string) (*csv.Writer, *os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating output directory %s: %w", dir, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("creating output file %s: %w", path, err)
	}
	return csv.NewWriter(f), f, nil
}

// WriteRichCSV writes a header and rows to a CSV writer using the canonical
// rich column order.
func WriteRichCSV(w *csv.Writer, rows [][]string) error {
	if err := w.Write(preprocesscfg.RichHeader()); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	for i, row := range rows {
		if err := w.Write(row); err != nil {
			return fmt.Errorf("writing row %d: %w", i+1, err)
		}
	}
	w.Flush()
	return w.Error()
}

// PrintSummary writes a human-readable filter summary to the given writer.
func PrintSummary(w io.Writer, total, kept, dropped int) {
	fmt.Fprintf(w, "Filter summary:\n")
	fmt.Fprintf(w, "  Total input rows:  %d\n", total)
	fmt.Fprintf(w, "  Rows kept:         %d\n", kept)
	fmt.Fprintf(w, "  Rows dropped:      %d\n", dropped)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -run TestOutputPath ./pkg/preprocess/`
Expected: PASS

**Step 5: Commit**

```
git add pkg/preprocess/output.go pkg/preprocess/output_test.go
git commit -m "feat(preprocess): add output path construction and CSV writer"
```

---

### Task 7: Create `cmd/enrich-targets` — CLI Wiring

**Files:**
- Create: `cmd/enrich-targets/main.go`

**Step 1: Implement the CLI**

```go
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/enrich"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func runMain(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("enrich-targets", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: enrich-targets [flags]\n\n")
		fmt.Fprintf(stderr, "Enriches a minimal host,port CSV into a full rich CSV.\n\n")
		fs.PrintDefaults()
	}

	input := fs.String("input", "", "Path to opened targets CSV (host,port) [required]")
	cidrList := fs.String("cidr-list", "", "Path to CIDR reference CSV [required]")
	serviceMap := fs.String("service-map", "", "Path to port-to-service-label CSV [required]")
	output := fs.String("output", "", "Path to write enriched rich CSV [required]")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *input == "" || *cidrList == "" || *serviceMap == "" || *output == "" {
		fs.Usage()
		return errors.New("all flags --input, --cidr-list, --service-map, --output are required")
	}

	// Load CIDR reference list.
	cidrFile, err := os.Open(*cidrList)
	if err != nil {
		return fmt.Errorf("opening CIDR list: %w", err)
	}
	defer cidrFile.Close()
	tree, err := enrich.LoadCIDRList(cidrFile)
	if err != nil {
		return fmt.Errorf("loading CIDR list: %w", err)
	}

	// Load service map.
	svcFile, err := os.Open(*serviceMap)
	if err != nil {
		return fmt.Errorf("opening service map: %w", err)
	}
	defer svcFile.Close()
	svcMap, err := enrich.LoadServiceMap(svcFile)
	if err != nil {
		return fmt.Errorf("loading service map: %w", err)
	}

	// Open input CSV.
	inFile, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer inFile.Close()

	cr := csv.NewReader(inFile)
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("reading input header: %w", err)
	}

	hostIdx, portIdx := -1, -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case strings.ToLower(preprocesscfg.ColHost):
			hostIdx = i
		case strings.ToLower(preprocesscfg.ColPortInput):
			portIdx = i
		}
	}
	if hostIdx < 0 || portIdx < 0 {
		return fmt.Errorf("input CSV missing required columns %q and %q",
			preprocesscfg.ColHost, preprocesscfg.ColPortInput)
	}

	// Create output file.
	outFile, err := os.Create(*output)
	if err != nil {
		return fmt.Errorf("creating output: %w", err)
	}
	defer outFile.Close()
	cw := csv.NewWriter(outFile)

	// Write header.
	if err := cw.Write(preprocesscfg.RichHeader()); err != nil {
		return fmt.Errorf("writing output header: %w", err)
	}

	enricher := enrich.NewEnricher(tree, svcMap)
	rowNum := 1
	enriched := 0

	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading input row %d: %w", rowNum+1, err)
		}
		rowNum++

		if len(row) <= hostIdx || len(row) <= portIdx {
			fmt.Fprintf(stderr, "row %d: too few columns, skipping\n", rowNum)
			continue
		}

		host := strings.TrimSpace(row[hostIdx])
		port, err := strconv.Atoi(strings.TrimSpace(row[portIdx]))
		if err != nil {
			fmt.Fprintf(stderr, "row %d: invalid port %q, skipping\n", rowNum, row[portIdx])
			continue
		}

		rich, err := enricher.Enrich(host, port)
		if err != nil {
			fmt.Fprintf(stderr, "row %d: enrichment failed: %v, skipping\n", rowNum, err)
			continue
		}

		if err := cw.Write(rich.ToSlice()); err != nil {
			return fmt.Errorf("writing output row %d: %w", rowNum, err)
		}
		enriched++
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	fmt.Fprintf(stderr, "Enriched %d rows from %d input rows\n", enriched, rowNum-1)
	return nil
}

func main() {
	if err := runMain(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 2: Build to verify compilation**

Run: `go build -o /dev/null ./cmd/enrich-targets/`
Expected: no errors

**Step 3: Commit**

```
git add cmd/enrich-targets/main.go
git commit -m "feat(enrich-targets): add CLI tool for enriching host,port CSV to rich format"
```

---

### Task 8: Create `cmd/preprocess` — CLI Wiring

**Files:**
- Create: `cmd/preprocess/main.go`

**Step 1: Implement the CLI**

```go
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocess"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func runMain(args []string, stdout, stderr io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("preprocess", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: preprocess [flags]\n\n")
		fmt.Fprintf(stderr, "Filters a rich CSV by removing targets in closed CIDRs.\n\n")
		fs.PrintDefaults()
	}

	input := fs.String("input", "", "Path to rich CSV [required]")
	cleanedCIDRs := fs.String("cleaned-cidrs", "", "Path to cleaned CIDRs CSV (fab,segment,status) [required]")
	fabName := fs.String("fab-name", "", "Data center / fabric name [required]")
	outputDir := fs.String("output-dir", "", "Base output directory [required]")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *input == "" || *cleanedCIDRs == "" || *fabName == "" || *outputDir == "" {
		fs.Usage()
		return errors.New("all flags --input, --cleaned-cidrs, --fab-name, --output-dir are required")
	}

	// Load closed CIDRs for this fab.
	cidrsFile, err := os.Open(*cleanedCIDRs)
	if err != nil {
		return fmt.Errorf("opening cleaned CIDRs: %w", err)
	}
	defer cidrsFile.Close()
	closedTree, err := preprocess.LoadCleanedCIDRs(cidrsFile, *fabName)
	if err != nil {
		return fmt.Errorf("loading cleaned CIDRs: %w", err)
	}

	filter := preprocess.NewFilter(closedTree)

	// Open input rich CSV.
	inFile, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("opening input: %w", err)
	}
	defer inFile.Close()

	cr := csv.NewReader(inFile)
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("reading input header: %w", err)
	}

	// Find dst_network_segment column.
	segIdx := -1
	for i, col := range header {
		if strings.TrimSpace(strings.ToLower(col)) == strings.ToLower(preprocesscfg.ColDstNetworkSegment) {
			segIdx = i
			break
		}
	}
	if segIdx < 0 {
		return fmt.Errorf("input CSV missing required column %q", preprocesscfg.ColDstNetworkSegment)
	}

	// Create output.
	outPath := preprocess.OutputPath(*outputDir, *fabName, now)
	cw, outFile, err := preprocess.CreateOutputWriter(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	// Write header (pass through input header).
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("writing output header: %w", err)
	}

	total, kept, dropped := 0, 0, 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading input row: %w", err)
		}
		total++

		if len(row) <= segIdx {
			fmt.Fprintf(stderr, "row %d: too few columns, skipping\n", total+1)
			dropped++
			continue
		}

		seg := strings.TrimSpace(row[segIdx])
		ok, err := filter.Keep(seg)
		if err != nil {
			fmt.Fprintf(stderr, "row %d: filter error: %v, dropping\n", total+1, err)
			dropped++
			continue
		}

		if ok {
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("writing output row: %w", err)
			}
			kept++
		} else {
			dropped++
		}
	}

	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flushing output: %w", err)
	}

	preprocess.PrintSummary(stderr, total, kept, dropped)
	fmt.Fprintf(stderr, "Output written to: %s\n", outPath)
	return nil
}

func main() {
	if err := runMain(os.Args[1:], os.Stdout, os.Stderr, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 2: Build to verify compilation**

Run: `go build -o /dev/null ./cmd/preprocess/`
Expected: no errors

**Step 3: Commit**

```
git add cmd/preprocess/main.go
git commit -m "feat(preprocess): add CLI tool for filtering rich CSV by closed CIDRs"
```

---

### Task 9: Integration Tests

**Files:**
- Create: `tests/integration/enrich_test.go`
- Create: `tests/integration/preprocess_test.go`

**Step 1: Write enrich integration test**

```go
package integration

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/enrich"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func TestEnrichEndToEnd(t *testing.T) {
	// Setup: CIDR list, service map, input
	cidrCSV := "cidr\n10.0.0.0/8\n10.1.0.0/16\n192.168.0.0/16\n"
	svcCSV := "port,service_label\n22,SSH\n80,HTTP\n"
	inputCSV := "host,port\n10.1.2.3,22\n10.5.6.7,80\n192.168.1.1,9999\n"

	tree, err := enrich.LoadCIDRList(strings.NewReader(cidrCSV))
	if err != nil {
		t.Fatalf("loading CIDR list: %v", err)
	}
	svcMap, err := enrich.LoadServiceMap(strings.NewReader(svcCSV))
	if err != nil {
		t.Fatalf("loading service map: %v", err)
	}

	enricher := enrich.NewEnricher(tree, svcMap)

	// Parse input
	cr := csv.NewReader(strings.NewReader(inputCSV))
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("reading input: %v", err)
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	cw.Write(preprocesscfg.RichHeader())

	for i := 1; i < len(rows); i++ {
		host := rows[i][0]
		port := 0
		fmt.Sscanf(rows[i][1], "%d", &port)

		rich, err := enricher.Enrich(host, port)
		if err != nil {
			t.Fatalf("enriching row %d: %v", i, err)
		}
		cw.Write(rich.ToSlice())
	}
	cw.Flush()

	output := buf.String()

	// Verify: 10.1.2.3 should get 10.1.0.0/16 (most specific)
	if !strings.Contains(output, "10.1.0.0/16") {
		t.Error("expected most specific CIDR 10.1.0.0/16 for 10.1.2.3")
	}
	// Verify: 10.5.6.7 should get 10.0.0.0/8 (only match)
	if !strings.Contains(output, "10.0.0.0/8") {
		t.Error("expected CIDR 10.0.0.0/8 for 10.5.6.7")
	}
	// Verify: port 9999 should get "unknown" service label
	if !strings.Contains(output, preprocesscfg.FallbackServiceLabel) {
		t.Error("expected fallback service label for port 9999")
	}
	// Verify header
	if !strings.Contains(output, strings.Join(preprocesscfg.RichHeader(), ",")) {
		t.Error("expected rich CSV header in output")
	}
}
```

**Step 2: Write preprocess integration test**

```go
package integration

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocess"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func TestPreprocessEndToEnd(t *testing.T) {
	cleanedCSV := "fab,segment,status\ndc-east,10.0.0.0/8,close\ndc-east,192.168.0.0/16,open\ndc-west,172.16.0.0/12,close\n"

	inputCSV := fmt.Sprintf("%s\n%s\n%s\n%s\n",
		strings.Join(preprocesscfg.RichHeader(), ","),
		"10.59.42.39,10.59.42.39/32,10.1.2.3,10.1.0.0/16,SSH,tcp,22,accept,enriched,MATCH_POLICY_ACCEPT",
		"10.59.42.39,10.59.42.39/32,192.168.1.1,192.168.1.0/24,HTTP,tcp,80,accept,enriched,MATCH_POLICY_ACCEPT",
		"10.59.42.39,10.59.42.39/32,172.16.1.1,172.16.1.0/24,HTTPS,tcp,443,accept,enriched,MATCH_POLICY_ACCEPT",
	)

	closedTree, err := preprocess.LoadCleanedCIDRs(strings.NewReader(cleanedCSV), "dc-east")
	if err != nil {
		t.Fatalf("loading cleaned CIDRs: %v", err)
	}

	filter := preprocess.NewFilter(closedTree)

	cr := csv.NewReader(strings.NewReader(inputCSV))
	allRows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("reading input: %v", err)
	}

	header := allRows[0]
	segIdx := -1
	for i, col := range header {
		if strings.TrimSpace(strings.ToLower(col)) == strings.ToLower(preprocesscfg.ColDstNetworkSegment) {
			segIdx = i
			break
		}
	}
	if segIdx < 0 {
		t.Fatal("missing dst_network_segment column")
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	cw.Write(header)

	kept, dropped := 0, 0
	for _, row := range allRows[1:] {
		ok, err := filter.Keep(strings.TrimSpace(row[segIdx]))
		if err != nil {
			t.Fatalf("filter error: %v", err)
		}
		if ok {
			cw.Write(row)
			kept++
		} else {
			dropped++
		}
	}
	cw.Flush()

	// 10.1.0.0/16 is inside closed 10.0.0.0/8 → dropped
	// 192.168.1.0/24 is inside open 192.168.0.0/16 → kept
	// 172.16.1.0/24 is inside closed 172.16.0.0/12 but that's dc-west → kept
	if kept != 2 {
		t.Errorf("expected 2 kept, got %d", kept)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}

	output := buf.String()
	if strings.Contains(output, "10.1.2.3") {
		t.Error("10.1.2.3 should have been filtered out (closed CIDR)")
	}
	if !strings.Contains(output, "192.168.1.1") {
		t.Error("192.168.1.1 should have been kept (open CIDR)")
	}
	if !strings.Contains(output, "172.16.1.1") {
		t.Error("172.16.1.1 should have been kept (closed in dc-west, not dc-east)")
	}
}
```

**Step 3: Run integration tests**

Run: `go test -v ./tests/integration/ -run "TestEnrich|TestPreprocess"`
Expected: PASS

**Step 4: Commit**

```
git add tests/integration/enrich_test.go tests/integration/preprocess_test.go
git commit -m "test: add integration tests for enrich and preprocess workflows"
```

---

### Task 10: Run Full Test Suite & Coverage Gate

**Step 1: Run all tests**

Run: `go test ./...`
Expected: PASS

**Step 2: Run coverage gate**

Run: `bash scripts/coverage_gate.sh`
Expected: PASS with coverage >= 85%

**Step 3: Build both tools**

Run: `go build -o /dev/null ./cmd/enrich-targets/ && go build -o /dev/null ./cmd/preprocess/`
Expected: no errors

**Step 4: Final commit if any adjustments needed**

If coverage or lint issues require fixes, address them and commit.
