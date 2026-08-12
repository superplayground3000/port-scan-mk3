package config

import (
	"flag"
	"fmt"
	"math"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

const decimalGB int64 = 1_000_000_000

type cidrLimitFlagValues struct {
	sizeGB  int64
	records int64
}

func defaultCIDRLimitFlags() cidrLimitFlagValues {
	return cidrLimitFlagValues{sizeGB: 1, records: int64(input.DefaultCIDRRecordLimit)}
}

func registerCIDRLimitFlags(fs *flag.FlagSet, values *cidrLimitFlagValues) {
	fs.Int64Var(&values.sizeGB, "cidr-input-size-limit-gb", values.sizeGB, "maximum CIDR input size in decimal GB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.records, "cidr-input-record-limit", values.records, "maximum CIDR data records. 0 disables this limit and can exhaust memory")
}

func (values cidrLimitFlagValues) resolve() (input.CIDRLimits, error) {
	if values.sizeGB < 0 {
		return input.CIDRLimits{}, fmt.Errorf("-cidr-input-size-limit-gb must be >= 0")
	}
	if values.records < 0 {
		return input.CIDRLimits{}, fmt.Errorf("-cidr-input-record-limit must be >= 0")
	}
	bytes, err := multiplyLimit(values.sizeGB, decimalGB, "-cidr-input-size-limit-gb")
	if err != nil {
		return input.CIDRLimits{}, err
	}
	return input.CIDRLimits{MaxBytes: bytes, MaxRecords: uint64(values.records)}, nil
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
