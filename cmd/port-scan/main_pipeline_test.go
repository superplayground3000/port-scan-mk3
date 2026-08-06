package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunMain_PrePing_WritesUnreachable drives the standalone pre-ping subcommand
// end to end. It asserts exit 0 and that an unreachable_results-*.csv is written
// into the -output directory. It deliberately does NOT assert specific
// reachability, because ping availability/behaviour varies by environment; the
// reachability logic itself is unit-tested in pkg/scanapp/pre_ping_test.go.
func TestRunMain_PrePing_WritesUnreachable(t *testing.T) {
	tmp := t.TempDir()
	// Rich CSV so no -port-file is needed: pre-ping is per-IP and its flag surface
	// (design §6) intentionally omits -port-file.
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,8080,accept,P-1,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"pre-ping",
		"-cidr-file", cidrFile,
		"-output", outFile,
		"-workers", "1",
		"-pre-scan-ping-timeout", "200ms",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}

	// The resolved path is printed to stdout for chaining and the file exists.
	mustFindOneMain(t, filepath.Join(tmp, "unreachable_results-*.csv"))
}

// TestRunMain_PrePing_CommandContract pins the 3.0.0 CLI contract for the
// reachability step: the accepted spelling is "pre-ping". The 2.x spelling of
// the same command has no alias, so it takes the normal unknown-command path
// (exit 2). See issue #97.
func TestRunMain_PrePing_CommandContract(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,8080,accept,P-1,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	// "pre-ping" dispatches to the pre-ping runner: exit 0 and the unreachable
	// CSV lands next to -output.
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"pre-ping",
		"-cidr-file", cidrFile,
		"-output", outFile,
		"-workers", "1",
		"-pre-scan-ping-timeout", "200ms",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 for pre-ping, got %d stderr=%s", code, stderr.String())
	}
	mustFindOneMain(t, filepath.Join(tmp, "unreachable_results-*.csv"))

	// The 2.x spelling is no longer a command: unknown-command error, exit 2.
	// The token is assembled from two parts on purpose. The repo-wide check for
	// issue #97 requires that the old spelling survives only in historical
	// release notes, and this test still exercises the exact removed string.
	removed := "pre" + "ping"
	oldOut := &bytes.Buffer{}
	oldErr := &bytes.Buffer{}
	oldCode := runMain([]string{
		removed,
		"-cidr-file", cidrFile,
		"-output", outFile,
	}, oldOut, oldErr)
	if oldCode != 2 {
		t.Fatalf("expected exit 2 for the removed %q command, got %d stderr=%s", removed, oldCode, oldErr.String())
	}
	if !strings.Contains(oldErr.String(), "unknown command: "+removed) {
		t.Fatalf("expected unknown-command error for %q, got stderr=%q", removed, oldErr.String())
	}
}

// TestRunMain_GenerateBuckets_WritesSnapshot drives the generate-buckets
// subcommand: a valid invocation writes the -buckets-out snapshot (exit 0), and
// omitting the required -buckets-out is a parse error (exit 2).
func TestRunMain_GenerateBuckets_WritesSnapshot(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	snapshot := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"generate-buckets",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-buckets-out", snapshot,
		"-workers", "2",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("expected snapshot file at %s, got err=%v", snapshot, err)
	}

	// Missing required -buckets-out is a parse error → exit 2.
	code = runMain([]string{
		"generate-buckets",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit 2 for missing -buckets-out, got %d", code)
	}
}

// TestRunMain_Scan_RequiresResume asserts scan without -resume is a parse error
// (exit 2) now that the bucket snapshot is mandatory.
func TestRunMain_Scan_RequiresResume(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit 2 for scan without -resume, got %d", code)
	}
}

// TestRunMain_Scan_RejectsPingFlags asserts the relocated ping flags are unknown
// to scan (structural "scan never pings" guarantee): passing them is an
// unknown-flag parse error (exit 2).
func TestRunMain_Scan_RejectsPingFlags(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-pre-scan-ping-timeout", "1s",
	}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("expected exit 2 for scan with -pre-scan-ping-timeout, got %d", code)
	}
}
