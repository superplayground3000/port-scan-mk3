package scanapp

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func mustGenerateBucketsConfig(t *testing.T, values config.GenerateBucketsValues) config.GenerateBucketsConfig {
	t.Helper()
	cfg, err := config.NewGenerateBuckets(values)
	if err != nil {
		t.Fatalf("NewGenerateBuckets() error = %v", err)
	}
	return cfg
}
