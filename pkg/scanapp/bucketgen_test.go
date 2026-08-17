package scanapp

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

const richBucketCSVHeader = "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"

// writeRichBucketCSV writes a rich CSV fixture with three CIDR groups and six
// distinct dst targets and returns its path.
func writeRichBucketCSV(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "rich.csv")
	body := richBucketCSVHeader +
		"10.1.0.10,10.1.0.0/24,10.0.0.9,10.0.0.0/24,svc-a,tcp,443,accept,P-1,allow\n" +
		"10.1.0.11,10.1.0.0/24,10.0.0.10,10.0.0.0/24,svc-b,tcp,443,accept,P-2,allow\n" +
		"10.1.0.12,10.1.0.0/24,10.0.0.11,10.0.0.0/24,svc-c,tcp,443,accept,P-3,allow\n" +
		"10.1.1.10,10.1.1.0/24,10.2.0.5,10.2.0.0/24,svc-d,tcp,80,accept,P-4,allow\n" +
		"10.1.1.11,10.1.1.0/24,10.2.0.6,10.2.0.0/24,svc-e,tcp,80,accept,P-5,allow\n" +
		"10.1.2.10,10.1.2.0/24,10.3.0.7,10.3.0.0/24,svc-f,tcp,8080,accept,P-6,allow\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeUnreachableCSV writes a blocklist CSV using the fixed unreachable writer
// schema (first column is ip) with the supplied unreachable IPs.
func writeUnreachableCSV(t *testing.T, dir string, ips ...string) string {
	t.Helper()
	path := filepath.Join(dir, "unreachable.csv")
	body := "ip,ip_cidr,status,reason,fab_name,cidr_name,service_label,decision,matched_policy_id,execution_key,src_ip,src_network_segment\n"
	for _, ip := range ips {
		body += ip + ",10.0.0.0/24,unreachable,ping failed within 100ms,,,,,,,,\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func bucketConfig(t *testing.T, cidrFile, blocklist, bucketsOut string, workers int) config.GenerateBucketsConfig {
	t.Helper()
	return mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		BlocklistFile:  blocklist,
		SnapshotOutput: bucketsOut,
		Workers:        workers,
	})
}

type spyReporter struct {
	mu    sync.Mutex
	incs  int
	added int
	done  bool
}

func (s *spyReporter) Inc() {
	s.mu.Lock()
	s.incs++
	s.mu.Unlock()
}

func (s *spyReporter) Add(n int) {
	s.mu.Lock()
	s.added += n
	s.mu.Unlock()
}

func (s *spyReporter) Done() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
}

func sumTotalCount(snap state.Snapshot) int {
	total := 0
	for _, ch := range snap.Chunks {
		total += ch.TotalCount
	}
	return total
}

func chunkByCIDR(snap state.Snapshot, cidr string) (int, bool) {
	for _, ch := range snap.Chunks {
		if ch.CIDR == cidr {
			return ch.TotalCount, true
		}
	}
	return 0, false
}

func TestGenerateBuckets_SubtractsBlocklist(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.0.0.10", "10.2.0.6")
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(t, cidrFile, blocklist, out, 4)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets failed: %v", err)
	}

	snap, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	// Reachable targets: group1 has 2 (10.0.0.9, 10.0.0.11), group2 has 1
	// (10.2.0.5), group3 has 1 (10.3.0.7). Blocked: 10.0.0.10, 10.2.0.6.
	if got := sumTotalCount(snap); got != 4 {
		t.Fatalf("expected total reachable targets 4, got %d", got)
	}
	if tc, ok := chunkByCIDR(snap, "10.0.0.0/24"); !ok || tc != 2 {
		t.Fatalf("expected 10.0.0.0/24 total_count 2, got %d (present=%v)", tc, ok)
	}
	if tc, ok := chunkByCIDR(snap, "10.2.0.0/24"); !ok || tc != 1 {
		t.Fatalf("expected 10.2.0.0/24 total_count 1, got %d (present=%v)", tc, ok)
	}
	if len(snap.PreScanPing.UnreachableIPv4U32) != 2 {
		t.Fatalf("expected blocklist of 2 stored, got %d", len(snap.PreScanPing.UnreachableIPv4U32))
	}
}

func TestGenerateBuckets_NoBlocklist_ScansAll(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(t, cidrFile, "", out, 4)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets failed: %v", err)
	}

	snap, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if got := sumTotalCount(snap); got != 6 {
		t.Fatalf("expected all 6 targets covered, got %d", got)
	}
	if len(snap.PreScanPing.UnreachableIPv4U32) != 0 {
		t.Fatalf("expected empty blocklist, got %d", len(snap.PreScanPing.UnreachableIPv4U32))
	}
	if !snap.PreScanPing.Enabled {
		t.Fatalf("expected pre_scan_ping.enabled=true even with no blocklist")
	}
}

func TestGenerateBuckets_BasicRowPortOverridesPortFile(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("ip,ip_cidr,port\n192.0.2.1,192.0.2.0/24,443\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: out,
		Workers:        1,
	})
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if len(snapshot.Chunks) != 1 {
		t.Fatalf("len(snapshot.Chunks) = %d, want 1", len(snapshot.Chunks))
	}
	if got := snapshot.Chunks[0].Ports; len(got) != 1 || got[0] != "443/tcp" {
		t.Fatalf("snapshot ports = %v, want [443/tcp]", got)
	}
	if got := snapshot.Chunks[0].TotalCount; got != 1 {
		t.Fatalf("snapshot total_count = %d, want 1", got)
	}
}

func TestGenerateBuckets_BasicPureFallbackPreservesCartesianChunkOrder(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port,cidr_name\n"+
			"198.51.100.2,198.51.100.0/24,,second\n"+
			"192.0.2.2,192.0.2.0/24,,first\n"+
			"192.0.2.1,192.0.2.0/24,,first\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("443/tcp\n80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: out,
		Workers:        8,
	})
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	want := []task.Chunk{
		{CIDR: "192.0.2.0/24", CIDRName: "first", Ports: []string{"443/tcp", "80/tcp"}, TotalCount: 4, Status: "pending"},
		{CIDR: "198.51.100.0/24", CIDRName: "second", Ports: []string{"443/tcp", "80/tcp"}, TotalCount: 2, Status: "pending"},
	}
	if !reflect.DeepEqual(snapshot.Chunks, want) {
		t.Fatalf("snapshot chunks = %+v, want %+v", snapshot.Chunks, want)
	}
}

func TestGenerateBuckets_BasicMixedRowPortsResumeWithoutCrossProduct(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port\n"+
			"192.0.2.1,192.0.2.0/24,443\n"+
			"192.0.2.2,192.0.2.0/24,8443\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: out,
		Workers:        2,
	})
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	records, err := readCIDRFile(cidrFile, "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("readCIDRFile() error = %v", err)
	}
	ports, err := inputPortSpecsFromRows(snapshot.BasicPortFallback)
	if err != nil {
		t.Fatalf("inputPortSpecsFromRows() error = %v", err)
	}
	runtimes, err := buildRuntimeWithPredicate(snapshot.Chunks, records, ports, runtimePolicy{}, nil, nil)
	if err != nil {
		t.Fatalf("buildRuntimeWithPredicate() error = %v", err)
	}
	defer func() {
		for _, runtime := range runtimes {
			if runtime.bkt != nil {
				runtime.bkt.Close()
			}
		}
	}()

	got := make([]string, 0, 2)
	for _, runtime := range runtimes {
		for index := 0; index < runtime.state.TotalCount; index++ {
			target, port, mapErr := indexToRuntimeTarget(runtime.targets, runtime.ports, index)
			if mapErr != nil {
				t.Fatalf("indexToRuntimeTarget() error = %v", mapErr)
			}
			got = append(got, fmt.Sprintf("%s:%d/tcp", target.ip, port))
		}
	}
	want := []string{"192.0.2.1:443/tcp", "192.0.2.2:8443/tcp"}
	if !slices.Equal(got, want) {
		t.Fatalf("runtime targets = %v, want %v", got, want)
	}
}

func TestGenerateBuckets_BasicRowsWithPortsDoNotNeedPortFile(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port\n"+
			"192.0.2.1,192.0.2.0/24,443\n"+
			"192.0.2.2,192.0.2.0/24,8443\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: out,
		Workers:        1,
	})
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got := sumTotalCount(snapshot); got != 2 {
		t.Fatalf("snapshot total_count sum = %d, want 2", got)
	}
}

func TestGenerateBuckets_BasicRowWithoutAnyPortSourceReturnsRowError(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("ip,ip_cidr,port\n192.0.2.1,192.0.2.0/24,\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: out,
		Workers:        1,
	})
	err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{})
	if err == nil || !strings.Contains(err.Error(), "basic row 2 has no port source") {
		t.Fatalf("GenerateBuckets() error = %v, want row port-source error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("snapshot stat error = %v, want no snapshot", statErr)
	}
}

func TestGenerateBuckets_BasicCIDRRowPortsFallbackAndDedup(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	out := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port\n"+
			"192.0.2.0/30,192.0.2.0/29,443\n"+
			"192.0.2.1,192.0.2.0/29,443\n"+
			"192.0.2.1,192.0.2.0/29,\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n8443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: out,
		Workers:        4,
	})
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got := sumTotalCount(snapshot); got != 6 {
		t.Fatalf("snapshot total_count sum = %d, want 6 unique tasks", got)
	}
	records, err := readCIDRFile(cidrFile, "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("readCIDRFile() error = %v", err)
	}
	ports, err := inputPortSpecsFromRows(snapshot.BasicPortFallback)
	if err != nil {
		t.Fatalf("inputPortSpecsFromRows() error = %v", err)
	}
	runtimes, err := buildRuntimeWithPredicate(snapshot.Chunks, records, ports, runtimePolicy{}, nil, nil)
	if err != nil {
		t.Fatalf("buildRuntimeWithPredicate() error = %v", err)
	}
	defer func() {
		for _, runtime := range runtimes {
			if runtime.bkt != nil {
				runtime.bkt.Close()
			}
		}
	}()
	gotCounts := make(map[int]int)
	gotTasks := make(map[string]struct{})
	for _, runtime := range runtimes {
		for index := 0; index < runtime.state.TotalCount; index++ {
			target, port, mapErr := indexToRuntimeTarget(runtime.targets, runtime.ports, index)
			if mapErr != nil {
				t.Fatalf("indexToRuntimeTarget() error = %v", mapErr)
			}
			gotCounts[port]++
			gotTasks[fmt.Sprintf("%s:%d/tcp", target.ip, port)] = struct{}{}
		}
	}
	wantCounts := map[int]int{80: 1, 443: 4, 8443: 1}
	if !maps.Equal(gotCounts, wantCounts) {
		t.Fatalf("task counts by port = %v, want %v", gotCounts, wantCounts)
	}
	if len(gotTasks) != 6 {
		t.Fatalf("unique runtime tasks = %v, want 6", gotTasks)
	}
}

func TestGenerateBuckets_WhenRichInputIsDenied_WritesEmptySnapshot(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich-denied.csv")
	if err := os.WriteFile(cidrFile, []byte(richBucketCSVHeader+
		"10.1.0.10,10.1.0.0/24,10.0.0.8,10.0.0.0/24,https,tcp,443,deny,P-1,MATCH_POLICY_DENY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "buckets.json")

	if err := GenerateBuckets(context.Background(), bucketConfig(t, cidrFile, "", out, 1), &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	snapshot, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if len(snapshot.Chunks) != 0 {
		t.Fatalf("GenerateBuckets() wrote %d chunks, want an empty snapshot", len(snapshot.Chunks))
	}
	if !snapshot.RichDenyExcluded {
		t.Fatal("GenerateBuckets() did not mark the snapshot as deny-filtered")
	}
}

func TestGenerateBuckets_StampsEnabledTrue(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	// zero unreachable rows -> empty blocklist
	blocklist := writeUnreachableCSV(t, tmp)
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(t, cidrFile, blocklist, out, 2)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets failed: %v", err)
	}

	snap, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !snap.PreScanPing.Enabled {
		t.Fatalf("expected Snapshot.PreScanPing.Enabled == true with zero unreachable")
	}
	if snap.PreScanPing.TimeoutMS != 0 {
		t.Fatalf("expected explicit pre-ping timeout metadata 0, got %d", snap.PreScanPing.TimeoutMS)
	}
}

func TestGenerateBuckets_Deterministic_AcrossWorkers(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.0.0.10")

	out1 := filepath.Join(tmp, "buckets-w1.json")
	out8 := filepath.Join(tmp, "buckets-w8.json")

	cfg1 := bucketConfig(t, cidrFile, blocklist, out1, 1)
	cfg8 := bucketConfig(t, cidrFile, blocklist, out8, 8)
	if err := GenerateBuckets(context.Background(), cfg1, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("workers=1: %v", err)
	}
	if err := GenerateBuckets(context.Background(), cfg8, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("workers=8: %v", err)
	}

	b1, err := os.ReadFile(out1)
	if err != nil {
		t.Fatal(err)
	}
	b8, err := os.ReadFile(out8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b8) {
		t.Fatalf("serialized snapshots differ across worker counts:\n--- workers=1 ---\n%s\n--- workers=8 ---\n%s", b1, b8)
	}
}

func TestGenerateBuckets_BasicRowPortsDeterministicAcrossWorkers(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port\n"+
			"192.0.2.0/30,192.0.2.0/29,443\n"+
			"192.0.2.1,192.0.2.0/29,\n"+
			"198.51.100.1,198.51.100.0/24,8443\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out1 := filepath.Join(tmp, "basic-w1.json")
	out8 := filepath.Join(tmp, "basic-w8.json")
	for _, test := range []struct {
		workers int
		output  string
	}{{workers: 1, output: out1}, {workers: 8, output: out8}} {
		cfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
			CIDRFile:       cidrFile,
			CIDRIPCol:      "ip",
			CIDRIPCidrCol:  "ip_cidr",
			PortFile:       portFile,
			SnapshotOutput: test.output,
			Workers:        test.workers,
		})
		if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
			t.Fatalf("workers=%d: GenerateBuckets() error = %v", test.workers, err)
		}
	}
	b1, err := os.ReadFile(out1)
	if err != nil {
		t.Fatal(err)
	}
	b8, err := os.ReadFile(out8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b8) {
		t.Fatalf("basic row-port snapshots differ across worker counts:\nworkers=1:\n%s\nworkers=8:\n%s", b1, b8)
	}
	snapshot, err := state.LoadSnapshot(out1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetSemanticsVersion != state.CurrentTargetSemanticsVersion {
		t.Fatalf("target semantics version = %d, want %d", snapshot.TargetSemanticsVersion, state.CurrentTargetSemanticsVersion)
	}
}

func TestGenerateBuckets_RaceFree(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.2.0.6")
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(t, cidrFile, blocklist, out, 32)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets failed under high worker count: %v", err)
	}
	if _, err := state.LoadSnapshot(out); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
}

func TestGenerateBuckets_ReportsProgressOverGroups(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	out := filepath.Join(tmp, "buckets.json")

	spy := &spyReporter{}
	cfg := bucketConfig(t, cidrFile, "", out, 4)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{Reporter: spy}); err != nil {
		t.Fatalf("GenerateBuckets failed: %v", err)
	}
	// Three CIDR groups -> three increments, and a final Done().
	if spy.incs+spy.added != 3 {
		t.Fatalf("expected progress increments totaling 3 groups, got inc=%d add=%d", spy.incs, spy.added)
	}
	if !spy.done {
		t.Fatalf("expected reporter.Done() to be called")
	}
}

func TestGenerateBuckets_SnapshotAcceptedByRuntime(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.0.0.10", "10.2.0.6")
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(t, cidrFile, blocklist, out, 8)
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets failed: %v", err)
	}

	snap, err := state.LoadSnapshot(out)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	// Re-derive runtimes the SAME way scan does, with the SAME records and the
	// reachable predicate reconstructed from the snapshot blocklist. This is the
	// invariant: no total_count mismatch is raised.
	records, err := readCIDRFile(cidrFile, "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	reachable := reachablePredicate(snap.PreScanPing.UnreachableIPv4U32)
	if _, err := buildRuntimeWithPredicate(snap.Chunks, records, nil, runtimePolicy{}, reachable, nil); err != nil {
		t.Fatalf("snapshot rejected by runtime (invariant violated): %v", err)
	}
}

// writeLargeRichBucketCSV writes a rich CSV with n distinct dst CIDR groups
// (two targets each), so the fan-out pool is genuinely exercised rather than
// capped to a handful of goroutines by workers > len(keys).
func writeLargeRichBucketCSV(t *testing.T, dir string, n int) string {
	t.Helper()
	path := filepath.Join(dir, "rich-large.csv")
	var b bytes.Buffer
	b.WriteString(richBucketCSVHeader)
	for g := 0; g < n; g++ {
		seg := 100 + g // stays < 256 for reasonable n
		for _, host := range []int{5, 6} {
			fmt.Fprintf(&b,
				"10.60.%d.1,10.60.0.0/16,10.%d.0.%d,10.%d.0.0/24,svc-%d,tcp,443,accept,P-%d-%d,allow\n",
				g, seg, host, seg, g, g, host)
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestGenerateBuckets_Deterministic_LargeFanOut stresses real worker fan-out
// (dozens of groups, 16 workers) and asserts byte-identical output vs the
// single-worker baseline. Run under -race, this also guards fanOutGroupChunks
// against races that the small (3-group) fixtures cannot surface.
func TestGenerateBuckets_Deterministic_LargeFanOut(t *testing.T) {
	tmp := t.TempDir()
	const groups = 40
	cidrFile := writeLargeRichBucketCSV(t, tmp, groups)

	out1 := filepath.Join(tmp, "large-w1.json")
	out16 := filepath.Join(tmp, "large-w16.json")

	cfg1 := bucketConfig(t, cidrFile, "", out1, 1)
	cfg16 := bucketConfig(t, cidrFile, "", out16, 16)
	if err := GenerateBuckets(context.Background(), cfg1, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("workers=1: %v", err)
	}
	if err := GenerateBuckets(context.Background(), cfg16, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("workers=16: %v", err)
	}

	b1, err := os.ReadFile(out1)
	if err != nil {
		t.Fatal(err)
	}
	b16, err := os.ReadFile(out16)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b16) {
		t.Fatalf("serialized snapshots differ across worker counts at %d groups", groups)
	}

	snap, err := state.LoadSnapshot(out16)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(snap.Chunks) != groups {
		t.Fatalf("expected %d chunks, got %d", groups, len(snap.Chunks))
	}
	if got := sumTotalCount(snap); got != groups*2 {
		t.Fatalf("expected %d total targets, got %d", groups*2, got)
	}
}

func TestParseUnreachableBlocklist_IPColumnToU32(t *testing.T) {
	tmp := t.TempDir()
	blocklist := writeUnreachableCSV(t, tmp, "10.0.0.10", "10.2.0.6", "10.0.0.10")

	got, err := parseUnreachableBlocklist(blocklist)
	if err != nil {
		t.Fatalf("parse blocklist: %v", err)
	}
	want := []uint32{ipv4ToUint32("10.0.0.10"), ipv4ToUint32("10.2.0.6")}
	// sorted-unique expected
	if len(got) != len(want) {
		t.Fatalf("expected %d unique u32, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blocklist[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseUnreachableBlocklist_MissingFileIsEmpty(t *testing.T) {
	// empty path
	if got, err := parseUnreachableBlocklist(""); err != nil || len(got) != 0 {
		t.Fatalf("empty path: expected empty no error, got %v err=%v", got, err)
	}
	// non-existent path
	if got, err := parseUnreachableBlocklist(filepath.Join(t.TempDir(), "nope.csv")); err != nil || len(got) != 0 {
		t.Fatalf("missing file: expected empty no error, got %v err=%v", got, err)
	}
}
