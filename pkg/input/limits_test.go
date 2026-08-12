package input_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func TestLoadCIDRsWithLimitsEnforcesBytesAndDataRecords(t *testing.T) {
	t.Parallel()

	const data = "ip,ip_cidr\n\n192.0.2.1,192.0.2.0/24\n192.0.2.2,192.0.2.0/24\n"
	exact := input.CIDRLimits{Path: "targets.csv", MaxBytes: uint64(len(data)), MaxRecords: 2}
	rows, err := input.LoadCIDRsWithColumnsContextAndLimits(context.Background(), strings.NewReader(data), "ip", "ip_cidr", exact)
	if err != nil {
		t.Fatalf("exact limits rejected: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("record count = %d, want 2", len(rows))
	}

	_, err = input.LoadCIDRsWithColumnsContextAndLimits(context.Background(), strings.NewReader(data), "ip", "ip_cidr", input.CIDRLimits{
		Path: "targets.csv", MaxBytes: uint64(len(data) - 1), MaxRecords: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "targets.csv") || !strings.Contains(err.Error(), "-cidr-input-size-limit-gb") {
		t.Fatalf("byte limit error = %v, want path and override flag", err)
	}

	_, err = input.LoadCIDRsWithColumnsContextAndLimits(context.Background(), strings.NewReader(data), "ip", "ip_cidr", input.CIDRLimits{
		Path: "targets.csv", MaxBytes: 0, MaxRecords: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "record 3") || !strings.Contains(err.Error(), "count 2") || !strings.Contains(err.Error(), "-cidr-input-record-limit") {
		t.Fatalf("record limit error = %v, want record, count, limit, and override flag", err)
	}
}

func TestLoadPortsWithLimitsCountsNonblankDuplicateRecords(t *testing.T) {
	t.Parallel()

	const data = "80/tcp\n\n80/tcp\n"
	exact := input.PortLimits{Path: "ports.csv", MaxBytes: uint64(len(data)), MaxRecords: 2}
	ports, err := input.LoadPortsContextWithLimits(context.Background(), strings.NewReader(data), exact)
	if err != nil {
		t.Fatalf("exact limits rejected: %v", err)
	}
	if len(ports) != 2 {
		t.Fatalf("record count = %d, want 2", len(ports))
	}

	_, err = input.LoadPortsContextWithLimits(context.Background(), strings.NewReader(data), input.PortLimits{
		Path: "ports.csv", MaxBytes: uint64(len(data) - 1), MaxRecords: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "ports.csv") || !strings.Contains(err.Error(), "-port-input-size-limit-mb") {
		t.Fatalf("byte limit error = %v, want path and override flag", err)
	}

	_, err = input.LoadPortsContextWithLimits(context.Background(), strings.NewReader(data), input.PortLimits{
		Path: "ports.csv", MaxBytes: 0, MaxRecords: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "record 3") || !strings.Contains(err.Error(), "count 2") || !strings.Contains(err.Error(), "-port-input-record-limit") {
		t.Fatalf("record limit error = %v, want record, count, limit, and override flag", err)
	}
}
