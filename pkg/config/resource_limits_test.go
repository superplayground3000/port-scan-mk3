package config_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestCommandResourceLimitFlagsHaveDefaultsOverridesAndIndependentBypass(t *testing.T) {
	t.Parallel()

	defaults := resolveScanLimits(t, []string{"-cidr-file", "targets.csv", "-resume", "resume.json", "-disable-api"})
	if defaults.maxBytes != 1_000_000_000 || defaults.maxRecords != 10_000_000 {
		t.Fatalf("scan CIDR defaults = %+v", defaults)
	}
	for name, values := range map[string]cidrLimitValues{
		"validate":         resolveValidateLimits(t, []string{"-cidr-file", "targets.csv", "-cidr-input-size-limit-gb", "0", "-cidr-input-record-limit", "7"}),
		"pre-ping":         resolvePrePingLimits(t, []string{"-cidr-file", "targets.csv", "-cidr-input-size-limit-gb", "0", "-cidr-input-record-limit", "7"}),
		"generate-buckets": resolveGenerateLimits(t, []string{"-cidr-file", "targets.csv", "-buckets-out", "resume.json", "-cidr-input-size-limit-gb", "0", "-cidr-input-record-limit", "7"}),
		"scan":             resolveScanLimits(t, []string{"-cidr-file", "targets.csv", "-resume", "resume.json", "-disable-api", "-cidr-input-size-limit-gb", "0", "-cidr-input-record-limit", "7"}),
	} {
		if values.maxBytes != 0 || values.maxRecords != 7 {
			t.Fatalf("%s CIDR limits = %+v", name, values)
		}
	}
}

type cidrLimitValues struct {
	maxBytes   uint64
	maxRecords uint64
}

func resolveValidateLimits(t *testing.T, args []string) cidrLimitValues {
	t.Helper()
	cfg, err := config.ParseValidate(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	return cidrLimitValues{maxBytes: got.CIDR.MaxBytes, maxRecords: got.CIDR.MaxRecords}
}

func resolvePrePingLimits(t *testing.T, args []string) cidrLimitValues {
	t.Helper()
	cfg, err := config.ParsePrePing(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	return cidrLimitValues{maxBytes: got.CIDR.MaxBytes, maxRecords: got.CIDR.MaxRecords}
}

func resolveGenerateLimits(t *testing.T, args []string) cidrLimitValues {
	t.Helper()
	cfg, err := config.ParseGenerateBuckets(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	return cidrLimitValues{maxBytes: got.CIDR.MaxBytes, maxRecords: got.CIDR.MaxRecords}
}

func resolveScanLimits(t *testing.T, args []string) cidrLimitValues {
	t.Helper()
	cfg, err := config.ParseScan(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	return cidrLimitValues{maxBytes: got.CIDR.MaxBytes, maxRecords: got.CIDR.MaxRecords}
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
		{"-cidr-input-record-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
		{"-port-input-record-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
		{"-snapshot-chunk-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
		{"-snapshot-port-entry-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
		{"-snapshot-unreachable-ip-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
		{"-pressure-response-entry-limit", strconv.FormatUint(uint64(math.MaxInt64)+1, 10)},
	} {
		_, err := config.ParseScan([]string{"-cidr-file", "unopened.csv", "-resume", "unopened.json", "-disable-api", pair[0], pair[1]})
		if err == nil {
			t.Fatalf("ParseScan(%s %s) accepted invalid value", pair[0], pair[1])
		}
	}
}

func TestProgrammaticConstructorsAcceptExplicitDisabledLimits(t *testing.T) {
	t.Parallel()

	limits := config.ScanResourceLimits{}
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

	validateCfg, err := config.NewValidateWithResourceLimits(config.ValidateValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", Format: "human"}, config.ValidateResourceLimits{CIDR: limits.CIDR, Port: limits.Port})
	if err != nil {
		t.Fatal(err)
	}
	prePingCfg, err := config.NewPrePingWithResourceLimits(config.PrePingValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", Workers: 1, PingTimeout: time.Second}, config.PrePingResourceLimits{CIDR: limits.CIDR})
	if err != nil {
		t.Fatal(err)
	}
	bucketCfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{CIDRFile: "targets.csv", CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", SnapshotOutput: "resume.json", Workers: 1}, config.GenerateBucketsResourceLimits{CIDR: limits.CIDR, Port: limits.Port, Snapshot: limits.Snapshot})
	if err != nil {
		t.Fatal(err)
	}
	validateGot, err := validateCfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	prePingGot, err := prePingCfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	bucketGot, err := bucketCfg.ResolveResourceLimits()
	if err != nil {
		t.Fatal(err)
	}
	if validateGot.CIDR.MaxBytes != 0 || prePingGot.CIDR.MaxBytes != 0 || bucketGot.CIDR.MaxBytes != 0 {
		t.Fatal("workflow-specific constructors did not keep disabled CIDR limits")
	}
}
