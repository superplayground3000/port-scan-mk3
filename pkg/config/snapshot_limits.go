package config

import (
	"flag"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

type snapshotLimitFlagValues struct {
	sizeGB      int64
	chunks      int64
	ports       int64
	unreachable int64
}

func defaultSnapshotLimitFlags() snapshotLimitFlagValues {
	return snapshotLimitFlagValues{
		sizeGB: 2, chunks: int64(state.DefaultSnapshotChunkLimit), ports: int64(state.DefaultSnapshotPortEntryLimit), unreachable: int64(state.DefaultSnapshotUnreachableIPLimit),
	}
}

func registerSnapshotLimitFlags(fs *flag.FlagSet, values *snapshotLimitFlagValues) {
	fs.Int64Var(&values.sizeGB, "snapshot-size-limit-gb", values.sizeGB, "maximum snapshot size in decimal GB. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.chunks, "snapshot-chunk-limit", values.chunks, "maximum snapshot chunks. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.ports, "snapshot-port-entry-limit", values.ports, "maximum snapshot port entries. 0 disables this limit and can exhaust memory")
	fs.Int64Var(&values.unreachable, "snapshot-unreachable-ip-limit", values.unreachable, "maximum snapshot unreachable IPs. 0 disables this limit and can exhaust memory")
}

func (values snapshotLimitFlagValues) resolve() (state.SnapshotLimits, error) {
	checks := []struct {
		flag  string
		value int64
	}{{"-snapshot-size-limit-gb", values.sizeGB}, {"-snapshot-chunk-limit", values.chunks}, {"-snapshot-port-entry-limit", values.ports}, {"-snapshot-unreachable-ip-limit", values.unreachable}}
	for _, check := range checks {
		if check.value < 0 {
			return state.SnapshotLimits{}, fmt.Errorf("%s must be >= 0", check.flag)
		}
	}
	bytes, err := multiplyLimit(values.sizeGB, decimalGB, "-snapshot-size-limit-gb")
	if err != nil {
		return state.SnapshotLimits{}, err
	}
	return state.SnapshotLimits{MaxBytes: bytes, MaxChunks: uint64(values.chunks), MaxPortEntries: uint64(values.ports), MaxUnreachableIPs: uint64(values.unreachable)}, nil
}
