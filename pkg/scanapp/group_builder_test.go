package scanapp

import (
	"net"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func TestBuildGroups_WhenBasicStrategy_ProducesSameResultAsBuildCIDRGroups(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("10.0.0.0/30")
	records := []input.CIDRRecord{
		{FabName: "fab1", IPRaw: "10.0.0.1", CIDR: "10.0.0.0/30", CIDRName: "net-a", Net: ipNet},
		{FabName: "fab2", IPRaw: "10.0.0.2", CIDR: "10.0.0.0/30", CIDRName: "net-a", Net: ipNet},
	}

	groups, err := buildGroups(records, basicGroupStrategy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups["10.0.0.0/30"]
	if len(g.targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(g.targets))
	}
}

func TestBuildGroups_WhenRichStrategy_ProducesSameResultAsBuildRichGroups(t *testing.T) {
	records := []input.CIDRRecord{
		{
			IsRich: true, IsValid: true, ExecutionKey: "10.0.0.1:80/tcp",
			DstIP: "10.0.0.1", DstNetworkSegment: "10.0.0.0/24", Port: 80,
			FabName: "fab1", CIDRName: "net-a",
		},
	}

	groups, err := buildGroups(records, richGroupStrategy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups["10.0.0.0/24"]
	if len(g.targets) != 1 || g.targets[0].ip != "10.0.0.1" {
		t.Fatalf("unexpected target: %+v", g.targets)
	}
	if g.targets[0].port != 80 {
		t.Fatalf("expected target port 80, got %d", g.targets[0].port)
	}
}

func TestBuildRichChunks_WhenRowDecisionIsDeny_ReturnsNoTarget(t *testing.T) {
	records := []input.CIDRRecord{{
		IsRich:            true,
		IsValid:           true,
		DstIP:             "10.0.0.8",
		DstNetworkSegment: "10.0.0.0/24",
		Protocol:          "tcp",
		Port:              443,
		Decision:          "deny",
		ExecutionKey:      "10.0.0.8:443/tcp",
		RowNumber:         2,
	}}

	chunks, err := buildRichChunks(records)
	if err != nil {
		t.Fatalf("buildRichChunks() error = %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("buildRichChunks() returned %d chunks, want no denied targets", len(chunks))
	}
}

func TestBuildRichChunks_WhenAcceptedAndDeniedRowsShareExecutionKey_DenyWins(t *testing.T) {
	accepted := input.CIDRRecord{
		IsRich:            true,
		IsValid:           true,
		DstIP:             "10.0.0.8",
		DstNetworkSegment: "10.0.0.0/24",
		Protocol:          "tcp",
		Port:              443,
		Decision:          "accept",
		ExecutionKey:      "10.0.0.8:443/tcp",
		RowNumber:         2,
	}
	denied := accepted
	denied.Decision = "deny"
	denied.RowNumber = 3

	for _, records := range [][]input.CIDRRecord{
		{accepted, denied},
		{denied, accepted},
	} {
		chunks, err := buildRichChunks(records)
		if err != nil {
			t.Fatalf("buildRichChunks() error = %v", err)
		}
		if len(chunks) != 0 {
			t.Fatalf("buildRichChunks() returned %d chunks, want deny to override accept", len(chunks))
		}
	}
}

func TestBuildRichChunks_WhenRowDecisionIsAccept_ReturnsTarget(t *testing.T) {
	records := []input.CIDRRecord{{
		IsRich:            true,
		IsValid:           true,
		DstIP:             "10.0.0.8",
		DstNetworkSegment: "10.0.0.0/24",
		Protocol:          "tcp",
		Port:              443,
		Decision:          "accept",
		ExecutionKey:      "10.0.0.8:443/tcp",
		RowNumber:         2,
	}}

	chunks, err := buildRichChunks(records)
	if err != nil {
		t.Fatalf("buildRichChunks() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0].TotalCount != 1 {
		t.Fatalf("buildRichChunks() returned %+v, want one accepted target", chunks)
	}
}
