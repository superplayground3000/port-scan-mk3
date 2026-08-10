package scanapp

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func mustPrePingConfig(t *testing.T, values config.PrePingValues) config.PrePingConfig {
	t.Helper()

	cfg, err := config.NewPrePing(values)
	if err != nil {
		t.Fatalf("NewPrePing() error = %v", err)
	}
	return cfg
}
