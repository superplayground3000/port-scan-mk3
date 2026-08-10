package scanapp

import (
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type testScanConfiguration struct {
	values config.ScanValues
}

// pressureConfigFixture contains only the pressure policy for a scan test.
type pressureConfigFixture struct {
	API          string
	Interval     time.Duration
	Disabled     bool
	AuthURL      string
	DataURLs     []string
	ClientID     string
	ClientSecret string
	UseAuth      bool
}

// scanConfigFixture contains only values for the scan workflow.
type scanConfigFixture struct {
	CIDRFile       string
	CIDRIPCol      string
	CIDRIPCidrCol  string
	PortFile       string
	Output         string
	Timeout        time.Duration
	Delay          time.Duration
	BucketRate     int
	BucketCapacity int
	Workers        int
	Pressure       pressureConfigFixture
	Resume         string
	LogLevel       string
	Format         string
	Quiet          bool
}

func (c testScanConfiguration) Resolve() (config.ScanValues, error) {
	return c.values, nil
}

func scanConfigurationFromFixture(t *testing.T, fixture scanConfigFixture) testScanConfiguration {
	t.Helper()

	interval := fixture.Pressure.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	var (
		policy config.PressurePolicy
		err    error
	)
	switch {
	case fixture.Pressure.Disabled:
		policy = config.PressureDisabled()
	case fixture.Pressure.UseAuth:
		policy, err = config.AuthenticatedPressure(
			fixture.Pressure.AuthURL,
			fixture.Pressure.DataURLs,
			fixture.Pressure.ClientID,
			fixture.Pressure.ClientSecret,
			interval,
		)
	default:
		endpoint := fixture.Pressure.API
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
