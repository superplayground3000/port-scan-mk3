package integration

import (
	"bytes"
	"encoding/csv"
	"fmt"
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
