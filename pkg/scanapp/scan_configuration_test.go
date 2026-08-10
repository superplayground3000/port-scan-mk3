package scanapp

import (
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type testScanConfiguration struct {
	values config.ScanValues
}

// scanConfigFixture is a package-local builder input for workflow tests. It
// keeps test setup concise while production callers use config.ScanValues.
type scanConfigFixture struct {
	CIDRFile             string
	CIDRIPCol            string
	CIDRIPCidrCol        string
	PortFile             string
	Output               string
	Timeout              time.Duration
	Delay                time.Duration
	BucketRate           int
	BucketCapacity       int
	Workers              int
	PressureAPI          string
	PressureInterval     time.Duration
	DisableAPI           bool
	PressureAuthURL      string
	PressureDataURLs     []string
	PressureClientID     string
	PressureClientSecret string
	PressureUseAuth      bool
	PreScanPingTimeout   time.Duration
	Resume               string
	LogLevel             string
	Format               string
	Quiet                bool
	BucketsOut           string
	UnreachableFile      string
	ProgressInterval     int
}

func (c testScanConfiguration) Resolve() (config.ScanValues, error) {
	return c.values, nil
}

func scanConfigurationFromFixture(t *testing.T, fixture scanConfigFixture) testScanConfiguration {
	t.Helper()

	interval := fixture.PressureInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	var (
		policy config.PressurePolicy
		err    error
	)
	switch {
	case fixture.DisableAPI:
		policy = config.PressureDisabled()
	case fixture.PressureUseAuth:
		policy, err = config.AuthenticatedPressure(
			fixture.PressureAuthURL,
			fixture.PressureDataURLs,
			fixture.PressureClientID,
			fixture.PressureClientSecret,
			interval,
		)
	default:
		endpoint := fixture.PressureAPI
		if endpoint == "" {
			endpoint = "http://localhost:8080/api/pressure"
		}
		policy, err = config.SimplePressure(endpoint, interval)
	}
	if err != nil {
		t.Fatalf("create test pressure policy: %v", err)
	}

	ipColumn := fixture.CIDRIPCol
	if ipColumn == "" {
		ipColumn = "ip"
	}
	cidrColumn := fixture.CIDRIPCidrCol
	if cidrColumn == "" {
		cidrColumn = "ip_cidr"
	}
	format := fixture.Format
	if format == "" {
		format = "human"
	}

	return testScanConfiguration{values: config.ScanValues{
		CIDRFile:       fixture.CIDRFile,
		CIDRIPCol:      ipColumn,
		CIDRIPCidrCol:  cidrColumn,
		PortFile:       fixture.PortFile,
		ResumeInput:    fixture.Resume,
		Output:         fixture.Output,
		Workers:        fixture.Workers,
		DialTimeout:    fixture.Timeout,
		DispatchDelay:  fixture.Delay,
		BucketRate:     fixture.BucketRate,
		BucketCapacity: fixture.BucketCapacity,
		LogLevel:       fixture.LogLevel,
		Format:         format,
		Quiet:          fixture.Quiet,
		Pressure:       policy,
	}}
}
