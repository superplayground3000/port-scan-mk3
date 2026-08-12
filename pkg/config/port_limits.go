package config

import (
	"flag"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

const decimalMB int64 = 1_000_000

type portLimitFlagValues struct {
	sizeMB  int64
	records int64
}

func defaultPortLimitFlags() portLimitFlagValues {
	return portLimitFlagValues{sizeMB: 1, records: int64(input.DefaultPortRecordLimit)}
}

func registerPortLimitFlags(fs *flag.FlagSet, values *portLimitFlagValues) {
	fs.Int64Var(&values.sizeMB, "port-input-size-limit-mb", values.sizeMB, "maximum port input size in decimal MB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.records, "port-input-record-limit", values.records, "maximum nonblank port records. 0 disables this limit and can exhaust memory")
}

func (values portLimitFlagValues) resolve() (input.PortLimits, error) {
	if values.sizeMB < 0 {
		return input.PortLimits{}, fmt.Errorf("-port-input-size-limit-mb must be >= 0")
	}
	if values.records < 0 {
		return input.PortLimits{}, fmt.Errorf("-port-input-record-limit must be >= 0")
	}
	bytes, err := multiplyLimit(values.sizeMB, decimalMB, "-port-input-size-limit-mb")
	if err != nil {
		return input.PortLimits{}, err
	}
	return input.PortLimits{MaxBytes: bytes, MaxRecords: uint64(values.records)}, nil
}
