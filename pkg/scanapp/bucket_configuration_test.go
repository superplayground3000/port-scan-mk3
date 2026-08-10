package scanapp_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
)

func TestGenerateBucketsRejectsUninitializedConfigurationBeforeSideEffects(t *testing.T) {
	var cfg config.GenerateBucketsConfig
	err := scanapp.GenerateBuckets(
		context.Background(),
		cfg,
		&bytes.Buffer{},
		scanapp.GenerateBucketsOptions{},
	)
	if !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("GenerateBuckets() error = %v, want ErrUninitializedConfiguration", err)
	}
}

func TestGenerateBucketsRejectsBlankSnapshotOutputBeforeReadingInputs(t *testing.T) {
	cfg, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:       filepath.Join(t.TempDir(), "missing.csv"),
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: " ",
		Workers:        10,
	})
	if err != nil {
		t.Fatalf("NewGenerateBuckets() error = %v", err)
	}

	err = scanapp.GenerateBuckets(
		context.Background(),
		cfg,
		&bytes.Buffer{},
		scanapp.GenerateBucketsOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "generate-buckets requires -buckets-out") {
		t.Fatalf("GenerateBuckets() error = %v, want snapshot output error", err)
	}
}
