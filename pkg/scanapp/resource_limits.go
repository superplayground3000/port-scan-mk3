package scanapp

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type resourceLimitConfiguration interface {
	ResolveResourceLimits() (config.ResourceLimitValues, error)
}

func resolveResourceLimits(configuration any) (config.ResourceLimitValues, error) {
	resolver, ok := configuration.(resourceLimitConfiguration)
	if !ok {
		return config.DefaultResourceLimitValues(), nil
	}
	limits, err := resolver.ResolveResourceLimits()
	if err != nil {
		return config.ResourceLimitValues{}, fmt.Errorf("resolve resource limits: %w", err)
	}
	return limits, nil
}
