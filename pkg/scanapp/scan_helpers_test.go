package scanapp

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestLoadRunInputs_WhenDependenciesInjected_UsesConfigPathsAndColumns(t *testing.T) {
	var gotCIDRPath, gotIPCol, gotCIDRCol, gotPortPath string
	wantCIDRs := []input.CIDRRecord{{CIDR: "10.0.0.0/24"}}
	wantPorts := []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}}

	deps := runDependencies{
		loadCIDRRecords: func(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error) {
			gotCIDRPath, gotIPCol, gotCIDRCol = path, ipCol, ipCidrCol
			return wantCIDRs, nil
		},
		loadPortSpecs: func(path string) ([]input.PortSpec, error) {
			gotPortPath = path
			return wantPorts, nil
		},
	}

	cfg := inputConfiguration{
		cidrFile:      "cidr.csv",
		portFile:      "ports.csv",
		cidrIPCol:     "source_ip",
		cidrIPCidrCol: "source_cidr",
	}

	got, err := loadRunInputs(cfg, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCIDRPath != "cidr.csv" || gotIPCol != "source_ip" || gotCIDRCol != "source_cidr" {
		t.Fatalf("unexpected cidr loader args: path=%s ip=%s cidr=%s", gotCIDRPath, gotIPCol, gotCIDRCol)
	}
	if gotPortPath != "ports.csv" {
		t.Fatalf("unexpected port loader path: %s", gotPortPath)
	}
	if len(got.cidrRecords) != 1 || got.cidrRecords[0].CIDR != wantCIDRs[0].CIDR {
		t.Fatalf("unexpected cidr records: %#v", got.cidrRecords)
	}
	if len(got.portSpecs) != 1 || got.portSpecs[0].Raw != wantPorts[0].Raw {
		t.Fatalf("unexpected port specs: %#v", got.portSpecs)
	}
}

func TestLoadRunInputs_WhenRichInputsAndPortFileMissing_SkipsPortLoader(t *testing.T) {
	deps := runDependencies{
		loadCIDRRecords: func(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error) {
			return []input.CIDRRecord{{IsRich: true, IsValid: true}}, nil
		},
		loadPortSpecs: func(path string) ([]input.PortSpec, error) {
			t.Fatalf("port loader should not be called when rich mode and port file missing")
			return nil, nil
		},
	}

	cfg := inputConfiguration{
		cidrFile:      "rich.csv",
		portFile:      "",
		cidrIPCol:     "ip",
		cidrIPCidrCol: "ip_cidr",
	}
	got, err := loadRunInputs(cfg, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.cidrRecords) != 1 || !got.cidrRecords[0].IsRich {
		t.Fatalf("unexpected cidr records: %#v", got.cidrRecords)
	}
	if len(got.portSpecs) != 0 {
		t.Fatalf("expected empty port specs, got %#v", got.portSpecs)
	}
}

func TestIndexToRuntimeTarget_WhenInputsInvalid_ReturnsErrors(t *testing.T) {
	targets := []scanTarget{{ip: "10.0.0.1"}}
	ports := []int{80}

	if _, _, err := indexToRuntimeTarget(nil, ports, 0); err == nil {
		t.Fatal("expected empty targets error")
	}
	if _, _, err := indexToRuntimeTarget(targets, nil, 0); err == nil {
		t.Fatal("expected empty ports error")
	}
	if _, _, err := indexToRuntimeTarget(targets, ports, -1); err == nil {
		t.Fatal("expected negative index error")
	}
	if _, _, err := indexToRuntimeTarget(targets, ports, 2); err == nil {
		t.Fatal("expected out of range error")
	}

	gotTarget, gotPort, err := indexToRuntimeTarget(targets, ports, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTarget.ip != "10.0.0.1" || gotPort != 80 {
		t.Fatalf("unexpected mapping: %+v port=%d", gotTarget, gotPort)
	}
}

func TestBuildCIDRGroups_WhenInputsVary_ReturnsErrorsAndSortedTargets(t *testing.T) {
	if _, err := buildCIDRGroups([]input.CIDRRecord{{IPRaw: "10.0.0.1"}}); err == nil {
		t.Fatal("expected missing ip_cidr error")
	}

	_, ipNet, _ := net.ParseCIDR("10.0.0.0/24")
	if _, err := buildCIDRGroups([]input.CIDRRecord{{CIDR: "10.0.0.0/24", Net: ipNet}}); err != nil {
		t.Fatalf("expected fallback selector from net, got err=%v", err)
	}

	if _, err := buildCIDRGroups([]input.CIDRRecord{{CIDR: "10.0.0.0/24", IPRaw: "bad-selector"}}); err == nil {
		t.Fatal("expected expand selector error")
	}

	rows := []input.CIDRRecord{
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.3/32"), FabName: "fab", CIDRName: "name"},
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), FabName: "fab", CIDRName: "name"},
	}
	groups, err := buildCIDRGroups(rows)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := groups["10.0.0.0/24"].targets
	if len(got) != 2 || got[0].ip != "10.0.0.1" || got[1].ip != "10.0.0.3" {
		t.Fatalf("unexpected sorted targets: %#v", got)
	}
}

func TestBuildCIDRGroups_WhenTargetIsBoundaryBroadcast_ExcludesIt(t *testing.T) {
	_, net24, _ := net.ParseCIDR("10.0.0.0/24")

	// An explicitly listed single IP that is the boundary broadcast is excluded.
	explicit := []input.CIDRRecord{{CIDR: "10.0.0.0/24", IPRaw: "10.0.0.255", Net: net24, FabName: "f", CIDRName: "c"}}
	groups, err := buildCIDRGroups(explicit)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups["10.0.0.0/24"].targets) != 0 {
		t.Fatalf("explicit broadcast must be excluded, got %#v", groups["10.0.0.0/24"].targets)
	}

	// A full /24 selector drops the broadcast but keeps the network address.
	full := []input.CIDRRecord{{CIDR: "10.0.0.0/24", IPRaw: "10.0.0.0/24", Net: net24, FabName: "f", CIDRName: "c"}}
	groups, err = buildCIDRGroups(full)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := groups["10.0.0.0/24"].targets
	if len(got) != 255 {
		t.Fatalf("expected 255 targets (256 minus broadcast), got %d", len(got))
	}
	if got[0].ip != "10.0.0.0" {
		t.Fatalf("network .0 must be kept, got %s", got[0].ip)
	}
	for _, tg := range got {
		if tg.ip == "10.0.0.255" {
			t.Fatal("broadcast 10.0.0.255 must be excluded")
		}
	}

	// Boundary-relative: 10.0.0.255 is a valid host inside a /23 and is kept.
	_, net23, _ := net.ParseCIDR("10.0.0.0/23")
	inRange := []input.CIDRRecord{{CIDR: "10.0.0.0/23", IPRaw: "10.0.0.255", Net: net23, FabName: "f", CIDRName: "c"}}
	groups, err = buildCIDRGroups(inRange)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got = groups["10.0.0.0/23"].targets
	if len(got) != 1 || got[0].ip != "10.0.0.255" {
		t.Fatalf("10.0.0.255 is a valid /23 host and must be kept, got %#v", got)
	}
}

func TestBuildRuntime_WhenTotalCountMismatch_ReturnsError(t *testing.T) {
	rows := []input.CIDRRecord{
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32")},
	}
	chunks := []task.Chunk{{
		CIDR:       "10.0.0.0/24",
		Ports:      []string{"80/tcp"},
		TotalCount: 2, // expected should be 1
	}}
	_, err := buildRuntimeWithPredicate(chunks, rows, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err == nil {
		t.Fatal("expected total_count mismatch error")
	}
	if !strings.Contains(err.Error(), "incompatible with the current target set") ||
		!strings.Contains(err.Error(), "fresh scan") {
		t.Fatalf("expected actionable resume-incompatibility message, got: %v", err)
	}
}

func TestBuildRichGroups_WhenDuplicateExecutionKey_PreservesMergedContext(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "127.0.0.1:8080/tcp",
			DstIP:             "127.0.0.1",
			DstNetworkSegment: "127.0.0.0/24",
			Port:              8080,
			FabName:           "10.0.0.10",
			CIDRName:          "web",
			ServiceLabel:      "web",
			Decision:          "accept",
			PolicyID:          "P-1",
			Reason:            "allow",
			SrcIP:             "10.0.0.10",
			SrcNetworkSegment: "10.0.0.0/24",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "127.0.0.1:8080/tcp",
			DstIP:             "127.0.0.1",
			DstNetworkSegment: "127.0.0.0/24",
			Port:              8080,
			FabName:           "10.0.0.11",
			CIDRName:          "web",
			ServiceLabel:      "web",
			Decision:          "deny",
			PolicyID:          "P-2",
			Reason:            "audit",
			SrcIP:             "10.0.0.11",
			SrcNetworkSegment: "10.0.0.0/24",
		},
	}
	groups, err := buildRichGroups(rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	got := groups["127.0.0.0/24"]
	if len(got.targets) != 1 {
		t.Fatalf("expected single runtime target, got %d", len(got.targets))
	}
	if got.targets[0].port != 8080 {
		t.Fatalf("unexpected target port: %d", got.targets[0].port)
	}
	if got.targets[0].meta.policyID != "P-1|P-2" {
		t.Fatalf("unexpected merged policy id: %s", got.targets[0].meta.policyID)
	}
	if got.targets[0].meta.decision != "accept|deny" {
		t.Fatalf("unexpected merged decision: %s", got.targets[0].meta.decision)
	}
}

func TestBuildRichGroups_WhenExecutionKeyAppearsAcrossCIDRs_DedupGloballyToFirstCIDR(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "127.0.0.1:8080/tcp",
			DstIP:             "127.0.0.1",
			DstNetworkSegment: "127.0.0.0/24",
			Port:              8080,
			PolicyID:          "P-1",
			Decision:          "accept",
			Reason:            "allow",
			SrcIP:             "10.0.0.10",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "127.0.0.1:8080/tcp",
			DstIP:             "127.0.0.1",
			DstNetworkSegment: "127.0.0.0/25",
			Port:              8080,
			PolicyID:          "P-2",
			Decision:          "deny",
			Reason:            "audit",
			SrcIP:             "10.0.0.11",
		},
	}
	groups, err := buildRichGroups(rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 cidr group after global dedup, got %d", len(groups))
	}
	if _, ok := groups["127.0.0.0/24"]; !ok {
		t.Fatalf("expected key to stay at first cidr owner")
	}
	if _, ok := groups["127.0.0.0/25"]; ok {
		t.Fatalf("unexpected second cidr group created for duplicated execution key")
	}
	got := groups["127.0.0.0/24"]
	if len(got.targets) != 1 {
		t.Fatalf("expected one dedup target, got %d", len(got.targets))
	}
	if got.targets[0].meta.policyID != "P-1|P-2" {
		t.Fatalf("unexpected merged policy id: %s", got.targets[0].meta.policyID)
	}
}

func TestBuildRichGroups_WhenReasonIsPrecheckAllowAll_ExpandsDstNetworkSegmentTargets(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/30",
			Port:              443,
			PolicyID:          "P-ALL",
			Decision:          "accept",
			Reason:            "PRECHECK_ALLOW_ALL",
			SrcIP:             "192.168.1.10",
			SrcNetworkSegment: "192.168.1.0/24",
		},
	}
	groups, err := buildRichGroups(rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := groups["10.0.0.0/30"]
	if len(got.targets) != 3 {
		t.Fatalf("expected 3 expanded targets from /30 (broadcast .3 excluded), got %d", len(got.targets))
	}
	wantIPs := []string{"10.0.0.0", "10.0.0.1", "10.0.0.2"}
	for i, wantIP := range wantIPs {
		target := got.targets[i]
		if target.ip != wantIP {
			t.Fatalf("unexpected target ip at idx=%d: want=%s got=%s", i, wantIP, target.ip)
		}
		if target.port != 443 {
			t.Fatalf("unexpected target port at idx=%d: %d", i, target.port)
		}
		if target.meta.executionKey != wantIP+":443/tcp" {
			t.Fatalf("unexpected execution key at idx=%d: %s", i, target.meta.executionKey)
		}
	}
}

func TestBuildRichGroups_WhenReasonIsMatchPolicyAccept_UsesDstIPOnly(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/30",
			Port:              443,
			PolicyID:          "P-1",
			Decision:          "accept",
			Reason:            "MATCH_POLICY_ACCEPT",
			SrcIP:             "192.168.1.10",
			SrcNetworkSegment: "192.168.1.0/24",
		},
	}
	groups, err := buildRichGroups(rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := groups["10.0.0.0/30"]
	if len(got.targets) != 1 {
		t.Fatalf("expected 1 target for MATCH_POLICY_ACCEPT, got %d", len(got.targets))
	}
	if got.targets[0].ip != "10.0.0.1" {
		t.Fatalf("unexpected target ip: %s", got.targets[0].ip)
	}
	if got.targets[0].meta.executionKey != "10.0.0.1:443/tcp" {
		t.Fatalf("unexpected execution key: %s", got.targets[0].meta.executionKey)
	}
}

func TestBuildRichGroups_WhenPrecheckAndMatchOverlap_MergesByExpandedExecutionKey(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/30",
			Port:              443,
			PolicyID:          "P-ALL",
			Decision:          "accept",
			Reason:            "PRECHECK_ALLOW_ALL",
			SrcIP:             "192.168.1.10",
			SrcNetworkSegment: "192.168.1.0/24",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/30",
			Port:              443,
			PolicyID:          "P-ONE",
			Decision:          "accept",
			Reason:            "MATCH_POLICY_ACCEPT",
			SrcIP:             "192.168.1.11",
			SrcNetworkSegment: "192.168.1.0/24",
		},
	}
	groups, err := buildRichGroups(rows)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	got := groups["10.0.0.0/30"]
	if len(got.targets) != 3 {
		t.Fatalf("expected 3 targets after overlap merge (broadcast .3 excluded), got %d", len(got.targets))
	}

	var merged scanTarget
	found := false
	for _, target := range got.targets {
		if target.ip == "10.0.0.1" {
			merged = target
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected merged target for 10.0.0.1 in %+v", got.targets)
	}
	if merged.meta.policyID != "P-ALL|P-ONE" {
		t.Fatalf("unexpected merged policy id: %s", merged.meta.policyID)
	}
	if merged.meta.reason != "PRECHECK_ALLOW_ALL|MATCH_POLICY_ACCEPT" {
		t.Fatalf("unexpected merged reason: %s", merged.meta.reason)
	}
}

func TestLoadOrBuildChunks_WhenRichRecordsProvided_BuildsCIDRScopedChunks(t *testing.T) {
	rows := []input.CIDRRecord{
		{IsRich: true, IsValid: true, ExecutionKey: "127.0.0.1:8080/tcp", Port: 8080, CIDRName: "web", DstIP: "127.0.0.1", DstNetworkSegment: "127.0.0.0/24"},
		{IsRich: true, IsValid: true, ExecutionKey: "127.0.0.1:8080/tcp", Port: 8080, CIDRName: "web", DstIP: "127.0.0.1", DstNetworkSegment: "127.0.0.0/24"},
		{IsRich: true, IsValid: true, ExecutionKey: "127.0.0.1:1/tcp", Port: 1, CIDRName: "web", DstIP: "127.0.0.1", DstNetworkSegment: "127.0.0.0/24"},
	}
	chunks, err := buildFreshChunksForTest(rows, nil, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 cidr chunk, got %d", len(chunks))
	}
	if chunks[0].CIDR != "127.0.0.0/24" {
		t.Fatalf("unexpected cidr chunk key: %#v", chunks)
	}
	if chunks[0].TotalCount != 2 {
		t.Fatalf("expected dedup targets count=2 under one cidr, got %#v", chunks)
	}
}

func TestIndexToRuntimeTarget_WhenRichTargetsHaveDedicatedPorts_MapsOneTaskPerTarget(t *testing.T) {
	targets := []scanTarget{
		{ip: "10.0.0.1", port: 443},
		{ip: "10.0.0.2", port: 8443},
	}
	ports := []int{1}

	t0, p0, err := indexToRuntimeTarget(targets, ports, 0)
	if err != nil {
		t.Fatalf("unexpected err on idx 0: %v", err)
	}
	if t0.ip != "10.0.0.1" || p0 != 443 {
		t.Fatalf("unexpected target@0: %s:%d", t0.ip, p0)
	}

	t1, p1, err := indexToRuntimeTarget(targets, ports, 1)
	if err != nil {
		t.Fatalf("unexpected err on idx 1: %v", err)
	}
	if t1.ip != "10.0.0.2" || p1 != 8443 {
		t.Fatalf("unexpected target@1: %s:%d", t1.ip, p1)
	}

	if _, _, err := indexToRuntimeTarget(targets, ports, 2); err == nil {
		t.Fatal("expected out-of-range error for idx 2")
	}
}

func TestBuildRichChunks_WhenNoUsableRows_ReturnsError(t *testing.T) {
	_, err := buildRichChunks([]input.CIDRRecord{{IsRich: true, IsValid: false}})
	if err == nil {
		t.Fatal("expected no usable row error")
	}
}

func TestDefaultString_WhenPrimaryEmpty_UsesFallback(t *testing.T) {
	if got := defaultString("", "x"); got != "x" {
		t.Fatalf("unexpected value: %s", got)
	}
	if got := defaultString(" y ", "x"); got != " y " {
		t.Fatalf("unexpected primary-preserved value: %s", got)
	}
}

func TestReadCIDRFileAndReadPortFile_WhenFileMissing_ReturnsError(t *testing.T) {
	if _, err := readCIDRFile("/not-exist", "ip", "ip_cidr"); err == nil {
		t.Fatal("expected read cidr file error")
	}
	if _, err := readPortFile("/not-exist"); err == nil {
		t.Fatal("expected read port file error")
	}
}

func TestOpenBatchOutputs_WhenCreated_WritesHeadersAndSupportsCIDRFallback(t *testing.T) {
	dir := t.TempDir()
	outputs, err := openBatchOutputs(filepath.Join(dir, "scan.csv"), filepath.Join(dir, "opened.csv"), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := writeScanRecord(outputs.scanWriter, outputs.openOnlyWriter, writer.Record{
		IP:     "10.0.0.1",
		CIDR:   "10.0.0.0/24",
		Port:   80,
		Status: "open",
	}); err != nil {
		t.Fatalf("write scan record failed: %v", err)
	}
	if err := outputs.Finalize(); err != nil {
		t.Fatalf("finalize outputs failed: %v", err)
	}

	scanBytes, err := os.ReadFile(filepath.Join(dir, "scan.csv"))
	if err != nil {
		t.Fatalf("read scan output failed: %v", err)
	}
	if !strings.Contains(string(scanBytes), "ip,ip_cidr,port,status,response_time_ms") {
		t.Fatalf("missing header in scan output: %s", string(scanBytes))
	}
	if !strings.Contains(string(scanBytes), "10.0.0.1,10.0.0.0/24,80,open") {
		t.Fatalf("expected CIDR fallback row, got: %s", string(scanBytes))
	}

	openBytes, err := os.ReadFile(filepath.Join(dir, "opened.csv"))
	if err != nil {
		t.Fatalf("read opened output failed: %v", err)
	}
	if !strings.Contains(string(openBytes), "10.0.0.1,10.0.0.0/24,80,open") {
		t.Fatalf("expected open row in opened output, got: %s", string(openBytes))
	}
}

func TestRecordFromScanTask_WhenMapped_PreservesTaskMetadata(t *testing.T) {
	record := recordFromScanTask(scanTask{
		chunkIdx: 3,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     443,
		meta: targetMeta{
			fabName:           "fab-1",
			cidrName:          "web-tier",
			serviceLabel:      "https",
			decision:          "accept",
			policyID:          "P-1",
			reason:            "approved",
			executionKey:      "10.0.0.8:443/tcp",
			srcIP:             "192.168.1.10",
			srcNetworkSegment: "192.168.1.0/24",
		},
	}, scanner.Result{
		IP:             "10.0.0.8",
		Port:           443,
		Status:         "open",
		ResponseTimeMS: 7,
	})

	if record.IP != "10.0.0.8" || record.IPCidr != "10.0.0.0/24" || record.Port != 443 {
		t.Fatalf("unexpected primary fields: %+v", record)
	}
	if record.FabName != "fab-1" || record.CIDRName != "web-tier" || record.ServiceLabel != "https" {
		t.Fatalf("unexpected metadata fields: %+v", record)
	}
	if record.Decision != "accept" || record.PolicyID != "P-1" || record.Reason != "approved" {
		t.Fatalf("unexpected policy fields: %+v", record)
	}
	if record.ExecutionKey != "10.0.0.8:443/tcp" || record.SrcIP != "192.168.1.10" || record.SrcNetworkSegment != "192.168.1.0/24" {
		t.Fatalf("unexpected execution metadata: %+v", record)
	}
	if record.Status != "open" || record.ResponseMS != 7 {
		t.Fatalf("unexpected scan result mapping: %+v", record)
	}
}

func TestChunkStateHelpers_WhenRuntimesMixed_ReturnExpectedSnapshots(t *testing.T) {
	ch0 := &task.Chunk{CIDR: "10.0.0.0/24", ScannedCount: 1, TotalCount: 2}
	ch1 := &task.Chunk{CIDR: "10.0.1.0/24", ScannedCount: 2, TotalCount: 2}
	runtimes := []*chunkRuntime{
		{state: ch0, tracker: newChunkStateTracker(ch0)},
		{state: ch1, tracker: newChunkStateTracker(ch1)},
	}

	if !hasIncomplete(runtimes) {
		t.Fatal("expected incomplete runtimes")
	}

	states := collectChunkStates(runtimes)
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	if states[0].CIDR != "10.0.0.0/24" || states[1].CIDR != "10.0.1.0/24" {
		t.Fatalf("unexpected states: %#v", states)
	}
	if states[0].ScannedCount != 1 || states[1].ScannedCount != 2 {
		t.Fatalf("unexpected scanned counts: %#v", states)
	}

	runtimes[0].tracker.IncrementScanned()
	if hasIncomplete(runtimes) {
		t.Fatal("expected all runtimes complete")
	}
}

func TestPollPressureAPI_WhenFirstTwoRequestsFail_DoesNotReturnFatalError(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	logOut := &lockedBuffer{}
	logger := newLogger("info", false, logOut)
	poller := startTestPressurePoller(t, scanConfigFixture{
		PressureAPI:      server.server.URL,
		PressureInterval: 5 * time.Millisecond,
	}, RunOptions{PressureLimit: 90}, ctrl, logger)

	for range 2 {
		server.respond(t, scriptedPressureHTTPResponse{
			statusCode: http.StatusInternalServerError,
			body:       "fail",
		})
	}
	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":10}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume after low pressure", func() bool { return !ctrl.APIPaused() })

	poller.stop(t)
	poller.makeSureNoError(t)

	if ctrl.APIPaused() {
		t.Fatal("expected not paused at low pressure")
	}
	logs := logOut.String()
	if !strings.Contains(logs, "(1/3)") || !strings.Contains(logs, "(2/3)") {
		t.Fatalf("expected first two failure logs, got: %s", logs)
	}
}

func mustSelectorNet(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatalf("parse cidr failed: %v", err)
	}
	return n
}
