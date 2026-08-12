package config

import (
	"flag"
	"fmt"
	"math"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

const (
	decimalGB int64 = 1_000_000_000
	decimalMB int64 = 1_000_000
)

// ResourceLimitValues contains verified limits for each responsible module.
type ResourceLimitValues struct {
	CIDR     input.CIDRLimits
	Port     input.PortLimits
	Snapshot state.SnapshotLimits
	Pressure pressure.ResponseLimits
}

type resourceLimitFlagValues struct {
	cidrSizeGB          int64
	cidrRecords         int64
	portSizeMB          int64
	portRecords         int64
	snapshotSizeGB      int64
	snapshotChunks      int64
	snapshotPorts       int64
	snapshotUnreachable int64
	pressureSizeMB      int64
	pressureEntries     int64
}

func defaultResourceLimitFlagValues() resourceLimitFlagValues {
	return resourceLimitFlagValues{
		cidrSizeGB:          1,
		cidrRecords:         int64(input.DefaultCIDRRecordLimit),
		portSizeMB:          1,
		portRecords:         int64(input.DefaultPortRecordLimit),
		snapshotSizeGB:      2,
		snapshotChunks:      int64(state.DefaultSnapshotChunkLimit),
		snapshotPorts:       int64(state.DefaultSnapshotPortEntryLimit),
		snapshotUnreachable: int64(state.DefaultSnapshotUnreachableIPLimit),
		pressureSizeMB:      1,
		pressureEntries:     int64(pressure.DefaultResponseEntryLimit),
	}
}

func defaultResourceLimitValues() ResourceLimitValues {
	values, err := defaultResourceLimitFlagValues().resolve()
	if err != nil {
		panic(err)
	}
	return values
}

// DefaultResourceLimitValues returns the default limits for all data modules.
func DefaultResourceLimitValues() ResourceLimitValues {
	return defaultResourceLimitValues()
}

func registerCIDRLimitFlags(fs *flag.FlagSet, values *resourceLimitFlagValues) {
	fs.Int64Var(&values.cidrSizeGB, "cidr-input-size-limit-gb", values.cidrSizeGB, "maximum CIDR input size in decimal GB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.cidrRecords, "cidr-input-record-limit", values.cidrRecords, "maximum CIDR data records. 0 disables this limit and can exhaust memory")
}

func registerPortLimitFlags(fs *flag.FlagSet, values *resourceLimitFlagValues) {
	fs.Int64Var(&values.portSizeMB, "port-input-size-limit-mb", values.portSizeMB, "maximum port input size in decimal MB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.portRecords, "port-input-record-limit", values.portRecords, "maximum nonblank port records. 0 disables this limit and can exhaust memory")
}

func registerSnapshotLimitFlags(fs *flag.FlagSet, values *resourceLimitFlagValues) {
	fs.Int64Var(&values.snapshotSizeGB, "snapshot-size-limit-gb", values.snapshotSizeGB, "maximum snapshot size in decimal GB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.snapshotChunks, "snapshot-chunk-limit", values.snapshotChunks, "maximum snapshot chunks. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.snapshotPorts, "snapshot-port-entry-limit", values.snapshotPorts, "maximum snapshot port entries. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.snapshotUnreachable, "snapshot-unreachable-ip-limit", values.snapshotUnreachable, "maximum snapshot unreachable IPs. 0 disables this limit and can exhaust memory")
}

func registerPressureLimitFlags(fs *flag.FlagSet, values *resourceLimitFlagValues) {
	fs.Int64Var(&values.pressureSizeMB, "pressure-response-size-limit-mb", values.pressureSizeMB, "maximum size of each pressure response in decimal MB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.pressureEntries, "pressure-response-entry-limit", values.pressureEntries, "maximum entries in each OAuth data response. 0 disables this limit and can exhaust memory")
}

func (values resourceLimitFlagValues) resolve() (ResourceLimitValues, error) {
	checks := []struct {
		flag  string
		value int64
	}{
		{"-cidr-input-size-limit-gb", values.cidrSizeGB},
		{"-cidr-input-record-limit", values.cidrRecords},
		{"-port-input-size-limit-mb", values.portSizeMB},
		{"-port-input-record-limit", values.portRecords},
		{"-snapshot-size-limit-gb", values.snapshotSizeGB},
		{"-snapshot-chunk-limit", values.snapshotChunks},
		{"-snapshot-port-entry-limit", values.snapshotPorts},
		{"-snapshot-unreachable-ip-limit", values.snapshotUnreachable},
		{"-pressure-response-size-limit-mb", values.pressureSizeMB},
		{"-pressure-response-entry-limit", values.pressureEntries},
	}
	for _, check := range checks {
		if check.value < 0 {
			return ResourceLimitValues{}, fmt.Errorf("%s must be >= 0", check.flag)
		}
	}
	cidrBytes, err := multiplyLimit(values.cidrSizeGB, decimalGB, "-cidr-input-size-limit-gb")
	if err != nil {
		return ResourceLimitValues{}, err
	}
	portBytes, err := multiplyLimit(values.portSizeMB, decimalMB, "-port-input-size-limit-mb")
	if err != nil {
		return ResourceLimitValues{}, err
	}
	snapshotBytes, err := multiplyLimit(values.snapshotSizeGB, decimalGB, "-snapshot-size-limit-gb")
	if err != nil {
		return ResourceLimitValues{}, err
	}
	pressureBytes, err := multiplyLimit(values.pressureSizeMB, decimalMB, "-pressure-response-size-limit-mb")
	if err != nil {
		return ResourceLimitValues{}, err
	}
	return ResourceLimitValues{
		CIDR: input.CIDRLimits{MaxBytes: cidrBytes, MaxRecords: uint64(values.cidrRecords)},
		Port: input.PortLimits{MaxBytes: portBytes, MaxRecords: uint64(values.portRecords)},
		Snapshot: state.SnapshotLimits{
			MaxBytes:          snapshotBytes,
			MaxChunks:         uint64(values.snapshotChunks),
			MaxPortEntries:    uint64(values.snapshotPorts),
			MaxUnreachableIPs: uint64(values.snapshotUnreachable),
		},
		Pressure: pressure.ResponseLimits{MaxBytes: pressureBytes, MaxEntries: uint64(values.pressureEntries)},
	}, nil
}

func multiplyLimit(value, unit int64, flagName string) (uint64, error) {
	if value == 0 {
		return 0, nil
	}
	if value > math.MaxInt64/unit {
		return 0, fmt.Errorf("%s value %d overflows bytes", flagName, value)
	}
	return uint64(value * unit), nil
}
