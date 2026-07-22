package scanapp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

func bucketConfig(cidrFile, blocklist, bucketsOut string, workers int) config.Config {
	return config.Config{
		CIDRFile:        cidrFile,
		UnreachableFile: blocklist,
		BucketsOut:      bucketsOut,
		Workers:         workers,
		CIDRIPCol:       "ip",
		CIDRIPCidrCol:   "ip_cidr",
	}
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

	cfg := bucketConfig(cidrFile, blocklist, out, 4)
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

	cfg := bucketConfig(cidrFile, "", out, 4)
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

func TestGenerateBuckets_StampsEnabledTrue(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	// zero unreachable rows -> empty blocklist
	blocklist := writeUnreachableCSV(t, tmp)
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(cidrFile, blocklist, out, 2)
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
}

func TestGenerateBuckets_Deterministic_AcrossWorkers(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.0.0.10")

	out1 := filepath.Join(tmp, "buckets-w1.json")
	out8 := filepath.Join(tmp, "buckets-w8.json")

	cfg1 := bucketConfig(cidrFile, blocklist, out1, 1)
	cfg8 := bucketConfig(cidrFile, blocklist, out8, 8)
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

func TestGenerateBuckets_RaceFree(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := writeRichBucketCSV(t, tmp)
	blocklist := writeUnreachableCSV(t, tmp, "10.2.0.6")
	out := filepath.Join(tmp, "buckets.json")

	cfg := bucketConfig(cidrFile, blocklist, out, 32)
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
	cfg := bucketConfig(cidrFile, "", out, 4)
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

	cfg := bucketConfig(cidrFile, blocklist, out, 8)
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
	records, err := readCIDRFile(cfg.CIDRFile, cfg.CIDRIPCol, cfg.CIDRIPCidrCol)
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	reachable := reachablePredicate(snap.PreScanPing.UnreachableIPv4U32)
	if _, err := buildRuntimeWithPredicate(snap.Chunks, records, nil, runtimePolicyFromConfig(cfg), reachable); err != nil {
		t.Fatalf("snapshot rejected by runtime (invariant violated): %v", err)
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
