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
