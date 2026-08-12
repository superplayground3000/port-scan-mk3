package scanapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestEstimateAuthorizedExpansion_CountsRowsBeforeDedup(t *testing.T) {
	_, segment, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	_, selector, err := net.ParseCIDR("192.0.2.0/30")
	if err != nil {
		t.Fatal(err)
	}
	limits, err := task.NewExpansionLimits(8, 0)
	if err != nil {
		t.Fatal(err)
	}
	records := []input.CIDRRecord{
		{RowNumber: 2, CIDR: segment.String(), Net: segment, IPRaw: "192.0.2.0/30", Selector: selector},
		{RowNumber: 3, CIDR: segment.String(), Net: segment, IPRaw: "192.0.2.0/30", Selector: selector},
		{RowNumber: 4, CIDR: segment.String(), Net: segment, IPRaw: "192.0.2.1"},
	}

	_, err = task.EstimateAuthorizedCIDRRecords(records, limits, nil)
	if err == nil || !strings.Contains(err.Error(), "row 4") || !strings.Contains(err.Error(), "candidate count 9") {
		t.Fatalf("estimate error = %v, want row 4 count 9", err)
	}
}

func writeScanExpansionFixture(t *testing.T, rows string, chunks []task.Chunk, expansion *state.TargetExpansionState) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	snapshotFile := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\n"+rows), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSnapshot(snapshotFile, state.Snapshot{
		Chunks:           chunks,
		RichDenyExcluded: true,
		TargetExpansion:  expansion,
	}); err != nil {
		t.Fatal(err)
	}
	return cidrFile, snapshotFile
}

func scanExpansionConfig(t *testing.T, cidrFile, snapshotFile string, extra ...string) config.ScanConfig {
	t.Helper()
	args := []string{
		"-cidr-file", cidrFile,
		"-resume", snapshotFile,
		"-disable-api",
		"-workers", "1",
		"-delay", "0s",
	}
	args = append(args, extra...)
	cfg, err := config.ParseScan(args)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunScan_StoredLimitStopsBeforeOutputAndExplicitFlagReplacesIt(t *testing.T) {
	cidrFile, snapshotFile := writeScanExpansionFixture(t,
		"fab,192.0.2.0/30,192.0.2.0/30,small\n",
		[]task.Chunk{{CIDR: "192.0.2.0/30", Ports: []string{"443/tcp"}, TotalCount: 3, Status: "pending"}},
		&state.TargetExpansionState{CandidateCount: 4, CandidateLimit: 3, MemoryLimitGB: 16},
	)

	outputOpened := false
	dialCalled := false
	openErr := errors.New("output opener reached")
	opts := RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCalled = true
			return nil, errors.New("dial reached")
		},
		DisableKeyboard: true,
		batchOutputsOpener: func(string, string, bool) (*batchOutputs, error) {
			outputOpened = true
			return nil, openErr
		},
	}

	err := Run(context.Background(), scanExpansionConfig(t, cidrFile, snapshotFile), &bytes.Buffer{}, &bytes.Buffer{}, opts)
	if err == nil || !strings.Contains(err.Error(), "count limit 3") {
		t.Fatalf("Run(stored limit) error = %v, want stored count limit", err)
	}
	if outputOpened || dialCalled {
		t.Fatalf("side effects = output:%t dial:%t, want none", outputOpened, dialCalled)
	}

	err = Run(context.Background(), scanExpansionConfig(t, cidrFile, snapshotFile, "-target-count-limit", "0"), &bytes.Buffer{}, &bytes.Buffer{}, opts)
	if !errors.Is(err, openErr) {
		t.Fatalf("Run(explicit override) error = %v, want output opener sentinel", err)
	}
	if !outputOpened || dialCalled {
		t.Fatalf("override side effects = output:%t dial:%t, want output only", outputOpened, dialCalled)
	}
}

func TestRunScan_CountsOnlyIncompleteChunks(t *testing.T) {
	cidrFile, snapshotFile := writeScanExpansionFixture(t,
		"fab,192.0.2.0/30,192.0.2.0/30,done\n"+
			"fab,198.51.100.0/30,198.51.100.0/30,pending\n",
		[]task.Chunk{
			{CIDR: "192.0.2.0/30", Ports: []string{"443/tcp"}, NextIndex: 3, ScannedCount: 3, TotalCount: 3, Status: "completed"},
			{CIDR: "198.51.100.0/30", Ports: []string{"443/tcp"}, TotalCount: 3, Status: "pending"},
		},
		&state.TargetExpansionState{CandidateCount: 8, CandidateLimit: 4, MemoryLimitGB: 16},
	)

	openErr := errors.New("output opener reached")
	err := Run(context.Background(), scanExpansionConfig(t, cidrFile, snapshotFile), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		batchOutputsOpener: func(string, string, bool) (*batchOutputs, error) {
			return nil, openErr
		},
	})
	if !errors.Is(err, openErr) {
		t.Fatalf("Run() error = %v, want incomplete-only estimate to reach output opener", err)
	}
}

func TestPersistResumeSnapshot_PreservesTargetExpansionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.json")
	chunk := task.Chunk{CIDR: "192.0.2.0/30", Ports: []string{"443/tcp"}, TotalCount: 3, Status: "pending"}
	runtime := &chunkRuntime{state: &chunk, tracker: newChunkStateTracker(&chunk)}
	expansion := &state.TargetExpansionState{CandidateCount: 4, CandidateLimit: 0, MemoryLimitGB: 16}

	_, err := persistResumeSnapshot(
		path,
		newLogger("error", false, &bytes.Buffer{}),
		[]*chunkRuntime{runtime},
		state.PreScanPingState{},
		nil,
		true,
		expansion,
		context.Canceled,
		nil,
	)
	if err != nil {
		t.Fatalf("persistResumeSnapshot() error = %v", err)
	}
	snapshot, err := state.LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetExpansion == nil || *snapshot.TargetExpansion != *expansion {
		t.Fatalf("target expansion = %#v, want %#v", snapshot.TargetExpansion, expansion)
	}
}

func TestRunPrePing_ExpansionLimitStopsBeforePingAndOutputCreation(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab,10.0.0.0/8,10.0.0.0/8,large\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParsePrePing([]string{
		"-cidr-file", cidrFile,
		"-output", filepath.Join(tmp, "results.csv"),
	})
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakePreScanChecker{}
	err = RunPrePing(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{ReachabilityChecker: checker})
	if err == nil || !strings.Contains(err.Error(), "candidate count 16777216") {
		t.Fatalf("RunPrePing() error = %v, want expansion limit", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("reachability calls = %v, want none", calls)
	}
	files, err := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("unreachable outputs = %v, want none", files)
	}
}

func TestGenerateBuckets_ExpansionLimitStopsBeforeSnapshotCreation(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	snapshotFile := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab,10.0.0.0/8,10.0.0.0/8,large\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-buckets-out", snapshotFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{})
	if err == nil || !strings.Contains(err.Error(), "candidate count 16777216") {
		t.Fatalf("GenerateBuckets() error = %v, want expansion limit", err)
	}
	if _, err := os.Stat(snapshotFile); !os.IsNotExist(err) {
		t.Fatalf("snapshot stat error = %v, want no file", err)
	}
}

func TestPrePingAndGenerateBuckets_UseExplicitCountLimit(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab,192.0.2.0/30,192.0.2.0/24,small\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prePingConfig, err := config.ParsePrePing([]string{
		"-cidr-file", cidrFile,
		"-output", filepath.Join(tmp, "results.csv"),
		"-target-count-limit", "3",
		"-target-memory-limit-gb", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakePreScanChecker{}
	err = RunPrePing(context.Background(), prePingConfig, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{ReachabilityChecker: checker})
	if err == nil || !strings.Contains(err.Error(), "count limit 3") {
		t.Fatalf("RunPrePing() error = %v, want explicit count limit", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("reachability calls = %v, want none", calls)
	}

	snapshotFile := filepath.Join(tmp, "buckets.json")
	bucketConfig, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-buckets-out", snapshotFile,
		"-target-count-limit", "3",
		"-target-memory-limit-gb", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = GenerateBuckets(context.Background(), bucketConfig, &bytes.Buffer{}, GenerateBucketsOptions{})
	if err == nil || !strings.Contains(err.Error(), "count limit 3") {
		t.Fatalf("GenerateBuckets() error = %v, want explicit count limit", err)
	}
	if _, err := os.Stat(snapshotFile); !os.IsNotExist(err) {
		t.Fatalf("snapshot stat error = %v, want no file", err)
	}

	bypassPrePing, err := config.ParsePrePing([]string{
		"-cidr-file", cidrFile,
		"-output", filepath.Join(tmp, "bypass.csv"),
		"-target-count-limit", "0",
		"-target-memory-limit-gb", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	bypassChecker := &fakePreScanChecker{}
	if err := RunPrePing(context.Background(), bypassPrePing, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{ReachabilityChecker: bypassChecker}); err != nil {
		t.Fatalf("RunPrePing(bypass) error = %v", err)
	}
	if calls := bypassChecker.calls(); len(calls) != 4 {
		t.Fatalf("bypass reachability calls = %v, want four", calls)
	}

	bypassBuckets, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-buckets-out", snapshotFile,
		"-target-count-limit", "0",
		"-target-memory-limit-gb", "0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateBuckets(context.Background(), bypassBuckets, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets(bypass) error = %v", err)
	}
	bypassSnapshot, err := state.LoadSnapshot(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	if bypassSnapshot.TargetExpansion == nil || bypassSnapshot.TargetExpansion.CandidateLimit != 0 || bypassSnapshot.TargetExpansion.MemoryLimitGB != 0 {
		t.Fatalf("bypass snapshot target expansion = %#v", bypassSnapshot.TargetExpansion)
	}
}

func TestGenerateBuckets_StoresEffectiveLimitsAndPreFilterCandidateCount(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	blocklistFile := filepath.Join(tmp, "unreachable.csv")
	snapshotFile := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab,192.0.2.0/30,192.0.2.0/30,small\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocklistFile, []byte("ip\n192.0.2.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-unreachable-file", blocklistFile,
		"-buckets-out", snapshotFile,
		"-target-count-limit", "5",
		"-target-memory-limit-gb", "32",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}
	snapshot, err := state.LoadSnapshot(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetExpansion == nil {
		t.Fatal("snapshot target expansion = nil")
	}
	if got := *snapshot.TargetExpansion; got.CandidateCount != 4 || got.CandidateLimit != 5 || got.MemoryLimitGB != 32 {
		t.Fatalf("snapshot target expansion = %#v, want count 4 and limits 5/32", got)
	}
}

func TestEffectiveScanExpansionLimits_UsesStoredDefaultsAndIndependentOverrides(t *testing.T) {
	stored := &state.TargetExpansionState{CandidateCount: 4, CandidateLimit: 5, MemoryLimitGB: 6}
	defaults := config.TargetExpansionValues{Limits: task.DefaultExpansionLimits()}

	limits, err := effectiveScanExpansionLimits(stored, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if limits.CandidateLimit() != 5 || limits.MemoryLimitGB() != 6 {
		t.Fatalf("stored limits = (%d, %d), want (5, 6)", limits.CandidateLimit(), limits.MemoryLimitGB())
	}

	overrideLimits, err := task.NewExpansionLimits(0, 32)
	if err != nil {
		t.Fatal(err)
	}
	limits, err = effectiveScanExpansionLimits(stored, config.TargetExpansionValues{
		Limits:    overrideLimits,
		CountSet:  true,
		MemorySet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if limits.CandidateLimit() != 0 || limits.MemoryLimitGB() != 32 {
		t.Fatalf("override limits = (%d, %d), want (0, 32)", limits.CandidateLimit(), limits.MemoryLimitGB())
	}

	limits, err = effectiveScanExpansionLimits(nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if limits.CandidateLimit() != 10_000_000 || limits.MemoryLimitGB() != 16 {
		t.Fatalf("legacy limits = (%d, %d), want defaults", limits.CandidateLimit(), limits.MemoryLimitGB())
	}

	if _, err := effectiveScanExpansionLimits(&state.TargetExpansionState{CandidateLimit: -1, MemoryLimitGB: 16}, defaults); err == nil {
		t.Fatal("negative stored count limit error = nil")
	}
}

func TestEstimateAuthorizedExpansion_DeniedRichRowsContributeZero(t *testing.T) {
	limits, err := task.NewExpansionLimits(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	records := []input.CIDRRecord{
		{
			RowNumber:         2,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/24",
			Decision:          "accept",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
		},
		{
			RowNumber:         3,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/24",
			Decision:          "deny",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
		},
		{
			RowNumber:         4,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.2",
			DstNetworkSegment: "192.0.2.0/24",
			Decision:          "accept",
			ExecutionKey:      "192.0.2.2:443/tcp",
			Port:              443,
		},
	}

	estimate, err := task.EstimateAuthorizedCIDRRecords(records, limits, nil)
	if err != nil {
		t.Fatalf("estimateAuthorizedExpansion() error = %v", err)
	}
	if estimate.CandidateCount != 1 {
		t.Fatalf("authorized candidate count = %d, want 1", estimate.CandidateCount)
	}
}

func TestEstimateAuthorizedExpansion_PrecheckCountsOnlyAuthorizedAddresses(t *testing.T) {
	limits, err := task.NewExpansionLimits(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	records := []input.CIDRRecord{
		{
			RowNumber:         2,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/30",
			Decision:          "accept",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
			Protocol:          "tcp",
			Reason:            "PRECHECK_ALLOW_ALL",
		},
		{
			RowNumber:         3,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/30",
			Decision:          "deny",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
			Protocol:          "tcp",
		},
	}

	estimate, err := task.EstimateAuthorizedCIDRRecords(records, limits, nil)
	if err != nil {
		t.Fatalf("estimateAuthorizedExpansion() error = %v", err)
	}
	if estimate.CandidateCount != 3 {
		t.Fatalf("authorized precheck candidate count = %d, want 3", estimate.CandidateCount)
	}
}
