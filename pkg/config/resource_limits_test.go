package config_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type resourceLimitConfig interface {
	ResolveResourceLimits() (config.ResourceLimitValues, error)
}

func TestCommandResourceLimitFlagsHaveDefaultsOverridesAndIndependentBypass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parse func([]string) (resourceLimitConfig, error)
		base  []string
	}{
		{name: "validate", parse: func(args []string) (resourceLimitConfig, error) { return config.ParseValidate(args) }, base: []string{"-cidr-file", "targets.csv"}},
		{name: "pre-ping", parse: func(args []string) (resourceLimitConfig, error) { return config.ParsePrePing(args) }, base: []string{"-cidr-file", "targets.csv"}},
		{name: "generate-buckets", parse: func(args []string) (resourceLimitConfig, error) { return config.ParseGenerateBuckets(args) }, base: []string{"-cidr-file", "targets.csv", "-buckets-out", "resume.json"}},
		{name: "scan", parse: func(args []string) (resourceLimitConfig, error) { return config.ParseScan(args) }, base: []string{"-cidr-file", "targets.csv", "-resume", "resume.json", "-disable-api"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			cfg, err := test.parse(test.base)
			if err != nil {
				t.Fatal(err)
			}
			defaults, err := cfg.ResolveResourceLimits()
			if err != nil {
				t.Fatal(err)
			}
			if defaults.CIDR.MaxBytes != 1_000_000_000 || defaults.CIDR.MaxRecords != 10_000_000 {
				t.Fatalf("CIDR defaults = %+v", defaults.CIDR)
			}

			cfg, err = test.parse(append(append([]string{}, test.base...), "-cidr-input-size-limit-gb", "0", "-cidr-input-record-limit", "7"))
			if err != nil {
				t.Fatal(err)
			}
			values, err := cfg.ResolveResourceLimits()
			if err != nil {
				t.Fatal(err)
			}
			if values.CIDR.MaxBytes != 0 || values.CIDR.MaxRecords != 7 {
				t.Fatalf("CIDR override values = %+v", values.CIDR)
			}
		})
	}
}

func TestResourceLimitFlagsRejectNegativeAndOverflowValues(t *testing.T) {
	t.Parallel()

	for _, pair := range [][2]string{
		{"-cidr-input-size-limit-gb", "-1"},
		{"-cidr-input-record-limit", "-1"},
		{"-port-input-size-limit-mb", "-1"},
		{"-port-input-record-limit", "-1"},
		{"-snapshot-size-limit-gb", "-1"},
		{"-snapshot-chunk-limit", "-1"},
		{"-snapshot-port-entry-limit", "-1"},
		{"-snapshot-unreachable-ip-limit", "-1"},
		{"-pressure-response-size-limit-mb", "-1"},
		{"-pressure-response-entry-limit", "-1"},
		{"-cidr-input-size-limit-gb", strconv.FormatInt(math.MaxInt64, 10)},
		{"-port-input-size-limit-mb", strconv.FormatInt(math.MaxInt64, 10)},
		{"-snapshot-size-limit-gb", strconv.FormatInt(math.MaxInt64, 10)},
		{"-pressure-response-size-limit-mb", strconv.FormatInt(math.MaxInt64, 10)},
	} {
		_, err := config.ParseScan([]string{"-cidr-file", "unopened.csv", "-resume", "unopened.json", "-disable-api", pair[0], pair[1]})
		if err == nil {
			t.Fatalf("ParseScan(%s %s) accepted invalid value", pair[0], pair[1])
		}
	}
}

func TestProgrammaticConstructorsAcceptExplicitDisabledLimits(t *testing.T) {
	t.Parallel()

	limits := config.DefaultResourceLimitValues()
	limits.CIDR.MaxBytes = 0
	limits.Port.MaxRecords = 0
	limits.Snapshot.MaxChunks = 0
	limits.Pressure.MaxEntries = 0
	cfg, err := config.NewScanWithResourceLimits(config.ScanValues{
		CIDRFile:       "targets.csv",
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		ResumeInput:    "resume.json",
		Workers:        1,
		BucketRate:     1,
		BucketCapacity: 1,
		Format:         "human",
		Pressure:       config.PressureDisabled(),
	}, limits)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	if got.CIDR.MaxBytes != 0 || got.Port.MaxRecords != 0 || got.Snapshot.MaxChunks != 0 || got.Pressure.MaxEntries != 0 {
		t.Fatalf("ResolveResourceLimits() = %+v, want independent disabled values", got)
	}

	validateCfg, err := config.NewValidateWithResourceLimits(config.ValidateValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", Format: "human"}, limits)
	if err != nil {
		t.Fatal(err)
	}
	prePingCfg, err := config.NewPrePingWithResourceLimits(config.PrePingValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", Workers: 1, PingTimeout: time.Second}, limits)
	if err != nil {
		t.Fatal(err)
	}
	bucketCfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", SnapshotOutput: "resume.json", Workers: 1}, limits)
	if err != nil {
		t.Fatal(err)
	}
	for name, resolver := range map[string]resourceLimitConfig{"validate": validateCfg, "pre-ping": prePingCfg, "generate-buckets": bucketCfg} {
		got, resolveErr := resolver.ResolveResourceLimits()
		if resolveErr != nil {
			t.Fatalf("%s: %v", name, resolveErr)
		}
		if got.CIDR.MaxBytes != 0 {
			t.Fatalf("%s CIDR byte limit = %d, want 0", name, got.CIDR.MaxBytes)
		}
	}
}
