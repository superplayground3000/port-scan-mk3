package config_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestParseGenerateBucketsReturnsDefaults(t *testing.T) {
	cfg, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", "targets.csv",
		"-buckets-out", "buckets.json",
	})
	if err != nil {
		t.Fatalf("ParseGenerateBuckets() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := config.GenerateBucketsValues{
		CIDRFile:         "targets.csv",
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		SnapshotOutput:   "buckets.json",
		Workers:          10,
		ProgressInterval: 100,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestNewGenerateBucketsReturnsValidatedValues(t *testing.T) {
	want := config.GenerateBucketsValues{
		CIDRFile:         "custom.csv",
		CIDRIPCol:        "address",
		CIDRIPCidrCol:    "network",
		PortFile:         "ports.csv",
		BlocklistFile:    "unreachable.csv",
		SnapshotOutput:   "custom.json",
		Workers:          24,
		ProgressInterval: 17,
	}

	cfg, err := config.NewGenerateBuckets(want)
	if err != nil {
		t.Fatalf("NewGenerateBuckets() error = %v", err)
	}
	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestNewGenerateBucketsRequiresSnapshotOutput(t *testing.T) {
	_, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:      "targets.csv",
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
		Workers:       10,
	})
	if err == nil {
		t.Fatal("NewGenerateBuckets() error = nil, want snapshot output error")
	}
}

func TestNewGenerateBucketsRejectsWorkerBounds(t *testing.T) {
	valid := config.GenerateBucketsValues{
		CIDRFile:       "targets.csv",
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: "buckets.json",
		Workers:        10,
	}

	for _, workers := range []int{0, config.MaxWorkers + 1} {
		values := valid
		values.Workers = workers
		if _, err := config.NewGenerateBuckets(values); err == nil {
			t.Fatalf("NewGenerateBuckets() with Workers = %d returned nil error", workers)
		}
	}
}

func TestNewGenerateBucketsRequiresCIDRFile(t *testing.T) {
	_, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: "buckets.json",
		Workers:        10,
	})
	if err == nil {
		t.Fatal("NewGenerateBuckets() error = nil, want CIDR file error")
	}
}

func TestNewGenerateBucketsRejectsBlankColumnNames(t *testing.T) {
	valid := config.GenerateBucketsValues{
		CIDRFile:       "targets.csv",
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		SnapshotOutput: "buckets.json",
		Workers:        10,
	}

	for _, change := range []func(*config.GenerateBucketsValues){
		func(values *config.GenerateBucketsValues) { values.CIDRIPCol = " " },
		func(values *config.GenerateBucketsValues) { values.CIDRIPCidrCol = " " },
	} {
		values := valid
		change(&values)
		if _, err := config.NewGenerateBuckets(values); err == nil {
			t.Fatal("NewGenerateBuckets() error = nil, want blank column error")
		}
	}
}

func TestGenerateBucketsConfigResolveRejectsZeroValue(t *testing.T) {
	var cfg config.GenerateBucketsConfig
	_, err := cfg.Resolve()
	if !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("Resolve() error = %v, want ErrUninitializedConfiguration", err)
	}
}

func TestParseGenerateBucketsPreservesAcceptedFlags(t *testing.T) {
	cfg, err := config.ParseGenerateBuckets([]string{
		"-cidr-file", "custom.csv",
		"-cidr-ip-col", "address",
		"-cidr-ip-cidr-col", "network",
		"-port-file", "ports.csv",
		"-unreachable-file", "unreachable.csv",
		"-buckets-out", "custom.json",
		"-workers", "24",
		"-progress-interval", "17",
		"-log-level", "debug",
		"-format", "json",
		"-quiet",
	})
	if err != nil {
		t.Fatalf("ParseGenerateBuckets() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := config.GenerateBucketsValues{
		CIDRFile:         "custom.csv",
		CIDRIPCol:        "address",
		CIDRIPCidrCol:    "network",
		PortFile:         "ports.csv",
		BlocklistFile:    "unreachable.csv",
		SnapshotOutput:   "custom.json",
		Workers:          24,
		ProgressInterval: 17,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestParseGenerateBucketsRejectsForeignFlags(t *testing.T) {
	for _, flag := range []string{"-resume", "-pre-scan-ping-timeout", "-pressure-api"} {
		_, err := config.ParseGenerateBuckets([]string{
			"-cidr-file", "targets.csv",
			"-buckets-out", "buckets.json",
			flag, "value",
		})
		if err == nil {
			t.Fatalf("ParseGenerateBuckets() with %s returned nil error", flag)
		}
	}
}
