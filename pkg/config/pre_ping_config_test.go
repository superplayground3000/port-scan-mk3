package config_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestParsePrePingReturnsDefaults(t *testing.T) {
	cfg, err := config.ParsePrePing([]string{"-cidr-file", "targets.csv"})
	if err != nil {
		t.Fatalf("ParsePrePing() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := config.PrePingValues{
		CIDRFile:         "targets.csv",
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           "scan_results.csv",
		Workers:          10,
		PingTimeout:      100 * time.Millisecond,
		ProgressInterval: 100,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestNewPrePingReturnsValidatedValues(t *testing.T) {
	want := config.PrePingValues{
		CIDRFile:         "custom.csv",
		CIDRIPCol:        "address",
		CIDRIPCidrCol:    "network",
		Output:           "reports",
		Workers:          24,
		PingTimeout:      2 * time.Second,
		ProgressInterval: 17,
	}

	cfg, err := config.NewPrePing(want)
	if err != nil {
		t.Fatalf("NewPrePing() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestNewPrePingRejectsMissingCIDRFile(t *testing.T) {
	_, err := config.NewPrePing(config.PrePingValues{
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           "reports",
		Workers:          10,
		PingTimeout:      100 * time.Millisecond,
		ProgressInterval: 100,
	})
	if err == nil {
		t.Fatal("NewPrePing() error = nil, want missing CIDR file error")
	}
}

func TestNewPrePingRejectsBlankColumnNames(t *testing.T) {
	valid := config.PrePingValues{
		CIDRFile:         "targets.csv",
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           "reports",
		Workers:          10,
		PingTimeout:      100 * time.Millisecond,
		ProgressInterval: 100,
	}

	for _, change := range []func(*config.PrePingValues){
		func(values *config.PrePingValues) { values.CIDRIPCol = " " },
		func(values *config.PrePingValues) { values.CIDRIPCidrCol = " " },
	} {
		values := valid
		change(&values)
		if _, err := config.NewPrePing(values); err == nil {
			t.Fatal("NewPrePing() error = nil, want blank column error")
		}
	}
}

func TestNewPrePingRejectsWorkerBounds(t *testing.T) {
	valid := config.PrePingValues{
		CIDRFile:         "targets.csv",
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           "reports",
		Workers:          10,
		PingTimeout:      100 * time.Millisecond,
		ProgressInterval: 100,
	}

	for _, workers := range []int{0, config.MaxWorkers + 1} {
		values := valid
		values.Workers = workers
		if _, err := config.NewPrePing(values); err == nil {
			t.Fatalf("NewPrePing() with Workers = %d returned nil error", workers)
		}
	}
}

func TestNewPrePingRejectsNonPositivePingTimeout(t *testing.T) {
	valid := config.PrePingValues{
		CIDRFile:         "targets.csv",
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           "reports",
		Workers:          10,
		PingTimeout:      100 * time.Millisecond,
		ProgressInterval: 100,
	}

	for _, timeout := range []time.Duration{0, -time.Millisecond} {
		values := valid
		values.PingTimeout = timeout
		if _, err := config.NewPrePing(values); err == nil {
			t.Fatalf("NewPrePing() with PingTimeout = %s returned nil error", timeout)
		}
	}
}

func TestPrePingConfigResolveRejectsZeroValue(t *testing.T) {
	var cfg config.PrePingConfig
	_, err := cfg.Resolve()
	if !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("Resolve() error = %v, want ErrUninitializedConfiguration", err)
	}
}

func TestParsePrePingPreservesAcceptedFlags(t *testing.T) {
	cfg, err := config.ParsePrePing([]string{
		"-cidr-file", "custom.csv",
		"-cidr-ip-col", "address",
		"-cidr-ip-cidr-col", "network",
		"-output", "reports",
		"-workers", "24",
		"-pre-scan-ping-timeout", "2s",
		"-progress-interval", "17",
		"-log-level", "debug",
		"-format", "json",
		"-quiet",
	})
	if err != nil {
		t.Fatalf("ParsePrePing() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := config.PrePingValues{
		CIDRFile:         "custom.csv",
		CIDRIPCol:        "address",
		CIDRIPCidrCol:    "network",
		Output:           "reports",
		Workers:          24,
		PingTimeout:      2 * time.Second,
		ProgressInterval: 17,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestParsePrePingRejectsForeignFlags(t *testing.T) {
	for _, flag := range []string{"-port-file", "-resume", "-disable-pre-scan-ping"} {
		_, err := config.ParsePrePing([]string{"-cidr-file", "targets.csv", flag, "value"})
		if err == nil {
			t.Fatalf("ParsePrePing() with %s returned nil error", flag)
		}
	}
}
