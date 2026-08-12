package scanapp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestResolveBasicTargetsContext_WrapsCancellationWithStage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolveBasicTargetsContext(ctx, []input.CIDRRecord{{
		CIDR:      "192.0.2.0/24",
		IPRaw:     "192.0.2.1",
		RowNumber: 1,
		Port:      443,
	}}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolve error = %v, want context.Canceled in the chain", err)
	}
	if err == nil || !strings.Contains(err.Error(), "resolve basic targets") {
		t.Fatalf("resolve error = %v, want resolve stage context", err)
	}
}

func TestBuildRuntimeWithPredicateContext_WrapsResolverCancellationWithStage(t *testing.T) {
	_, network, err := net.ParseCIDR("192.0.0.0/19")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reachable := func(string) bool {
		cancel()
		return true
	}

	_, err = buildRuntimeWithPredicateContext(ctx,
		[]task.Chunk{{CIDR: network.String(), Ports: []string{"443/tcp"}, TotalCount: 8190}},
		[]input.CIDRRecord{{
			CIDR:      network.String(),
			IPRaw:     network.String(),
			Net:       network,
			Selector:  network,
			RowNumber: 1,
			Port:      443,
		}},
		nil,
		runtimePolicy{bucketRate: 1, bucketCapacity: 1},
		reachable,
		nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime rebuild error = %v, want context.Canceled in the chain", err)
	}
	if err == nil || !strings.Contains(err.Error(), "rebuild basic target resolution") {
		t.Fatalf("runtime rebuild error = %v, want rebuild stage context", err)
	}
}
