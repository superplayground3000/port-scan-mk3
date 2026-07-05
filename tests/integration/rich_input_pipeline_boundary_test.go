package integration

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
)

func TestRichInputPipelineBoundary_WhenRowsShareExecutionKey_DispatchesOnceAndPreservesContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, p, _ := net.SplitHostPort(ln.Addr().String())
	openPort, _ := strconv.Atoi(p)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	csvData := fmt.Sprintf("src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
		"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,%d,accept,P-1,allow\n"+
		"10.0.0.11,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,%d,deny,P-2,audit\n"+
		"10.0.0.12,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,1,accept,P-3,secondary\n", openPort, openPort)
	if err := os.WriteFile(cidrFile, []byte(csvData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            100 * time.Millisecond,
		PreScanPingTimeout: 100 * time.Millisecond,
		Delay:              0,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureAPI:        "http://127.0.0.1:1",
		PressureInterval:   time.Second,
		DisableAPI:         true,
		Format:             "human",
	}

	if err := scanapp.Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, scanapp.RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("scan run failed: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmp, "scan_results-*.csv"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one scan result file, got %v err=%v", matches, err)
	}

	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected header + 2 result rows (dedup), got %d", len(rows))
	}

	header := rows[0]
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	if _, ok := idx["matched_policy_id"]; !ok {
		t.Fatalf("missing matched_policy_id column in header: %v", header)
	}
	if _, ok := idx["execution_key"]; !ok {
		t.Fatalf("missing execution_key column in header: %v", header)
	}

	mergedSeen := false
	for _, row := range rows[1:] {
		if row[idx["port"]] == strconv.Itoa(openPort) {
			if row[idx["matched_policy_id"]] != "P-1|P-2" {
				t.Fatalf("expected merged matched_policy_id, got %q", row[idx["matched_policy_id"]])
			}
			mergedSeen = true
		}
	}
	if !mergedSeen {
		t.Fatalf("expected row for open port %d", openPort)
	}
}
