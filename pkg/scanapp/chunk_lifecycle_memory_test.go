package scanapp

import (
	"context"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func TestSelectRuntimeRecords_AllChunksIncompleteReusesInputTable(t *testing.T) {
	records := []input.CIDRRecord{
		{CIDR: "10.0.0.0/24"},
		{CIDR: "10.0.1.0/24"},
	}
	selected, err := selectRuntimeRecords(
		context.Background(),
		records,
		map[string]struct{}{"10.0.0.0/24": {}, "10.0.1.0/24": {}},
		true,
		2,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != len(records) || &selected[0] != &records[0] {
		t.Fatal("all-incomplete runtime copied the complete record table")
	}
}
