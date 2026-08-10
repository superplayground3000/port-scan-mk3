package scanapp

import (
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type testScanConfiguration struct {
	values config.ScanValues
}

func (c testScanConfiguration) Resolve() (config.ScanValues, error) {
	return c.values, nil
}

func testScanConfigurationFromLegacy(t *testing.T, legacy config.Config) testScanConfiguration {
	t.Helper()

	interval := legacy.PressureInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	var (
		policy config.PressurePolicy
		err    error
	)
	switch {
	case legacy.DisableAPI:
		policy = config.PressureDisabled()
	case legacy.PressureUseAuth:
		policy, err = config.AuthenticatedPressure(
			legacy.PressureAuthURL,
			legacy.PressureDataURLs,
			legacy.PressureClientID,
			legacy.PressureClientSecret,
			interval,
		)
	default:
		endpoint := legacy.PressureAPI
		if endpoint == "" {
			endpoint = "http://localhost:8080/api/pressure"
		}
		policy, err = config.SimplePressure(endpoint, interval)
	}
	if err != nil {
		t.Fatalf("create test pressure policy: %v", err)
	}

	ipColumn := legacy.CIDRIPCol
	if ipColumn == "" {
		ipColumn = "ip"
	}
	cidrColumn := legacy.CIDRIPCidrCol
	if cidrColumn == "" {
		cidrColumn = "ip_cidr"
	}
	format := legacy.Format
	if format == "" {
		format = "human"
	}

	return testScanConfiguration{values: config.ScanValues{
		CIDRFile:       legacy.CIDRFile,
		CIDRIPCol:      ipColumn,
		CIDRIPCidrCol:  cidrColumn,
		PortFile:       legacy.PortFile,
		ResumeInput:    legacy.Resume,
		Output:         legacy.Output,
		Workers:        legacy.Workers,
		DialTimeout:    legacy.Timeout,
		DispatchDelay:  legacy.Delay,
		BucketRate:     legacy.BucketRate,
		BucketCapacity: legacy.BucketCapacity,
		LogLevel:       legacy.LogLevel,
		Format:         format,
		Quiet:          legacy.Quiet,
		Pressure:       policy,
	}}
}
