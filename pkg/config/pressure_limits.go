package config

import (
	"flag"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

type pressureLimitFlagValues struct {
	sizeMB  int64
	entries int64
}

func defaultPressureLimitFlags() pressureLimitFlagValues {
	return pressureLimitFlagValues{sizeMB: 1, entries: int64(pressure.DefaultResponseEntryLimit)}
}

func registerPressureLimitFlags(fs *flag.FlagSet, values *pressureLimitFlagValues) {
	fs.Int64Var(&values.sizeMB, "pressure-response-size-limit-mb", values.sizeMB, "maximum size of each pressure response in decimal MB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.entries, "pressure-response-entry-limit", values.entries, "maximum entries in each OAuth data response. 0 disables this limit and can exhaust memory")
}

func (values pressureLimitFlagValues) resolve() (pressure.ResponseLimits, error) {
	if values.sizeMB < 0 {
		return pressure.ResponseLimits{}, fmt.Errorf("-pressure-response-size-limit-mb must be >= 0")
	}
	if values.entries < 0 {
		return pressure.ResponseLimits{}, fmt.Errorf("-pressure-response-entry-limit must be >= 0")
	}
	bytes, err := multiplyLimit(values.sizeMB, decimalMB, "-pressure-response-size-limit-mb")
	if err != nil {
		return pressure.ResponseLimits{}, err
	}
	return pressure.ResponseLimits{MaxBytes: bytes, MaxEntries: uint64(values.entries)}, nil
}
