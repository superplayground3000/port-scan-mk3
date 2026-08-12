package scanapp

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

type prePingLimitConfiguration interface {
	ResolveResourceLimits() (config.PrePingResourceLimits, error)
}

func resolvePrePingLimits(configuration any) (config.PrePingResourceLimits, error) {
	resolver, ok := configuration.(prePingLimitConfiguration)
	if !ok {
		return config.PrePingResourceLimits{CIDR: input.DefaultCIDRLimits("")}, nil
	}
	limits, err := resolver.ResolveResourceLimits()
	if err != nil {
		return config.PrePingResourceLimits{}, fmt.Errorf("resolve resource limits: %w", err)
	}
	return limits, nil
}

type generateBucketsLimitConfiguration interface {
	ResolveResourceLimits() (config.GenerateBucketsResourceLimits, error)
}

func resolveGenerateBucketsLimits(configuration any) (config.GenerateBucketsResourceLimits, error) {
	resolver, ok := configuration.(generateBucketsLimitConfiguration)
	if !ok {
		return config.GenerateBucketsResourceLimits{CIDR: input.DefaultCIDRLimits(""), Port: input.DefaultPortLimits(""), Snapshot: state.DefaultSnapshotLimits()}, nil
	}
	limits, err := resolver.ResolveResourceLimits()
	if err != nil {
		return config.GenerateBucketsResourceLimits{}, fmt.Errorf("resolve resource limits: %w", err)
	}
	return limits, nil
}

type scanLimitConfiguration interface {
	ResolveResourceLimits() (config.ScanResourceLimits, error)
}

func resolveScanLimits(configuration any) (config.ScanResourceLimits, error) {
	resolver, ok := configuration.(scanLimitConfiguration)
	if !ok {
		return config.ScanResourceLimits{CIDR: input.DefaultCIDRLimits(""), Port: input.DefaultPortLimits(""), Snapshot: state.DefaultSnapshotLimits(), Pressure: pressure.DefaultResponseLimits()}, nil
	}
	limits, err := resolver.ResolveResourceLimits()
	if err != nil {
		return config.ScanResourceLimits{}, fmt.Errorf("resolve resource limits: %w", err)
	}
	return limits, nil
}
