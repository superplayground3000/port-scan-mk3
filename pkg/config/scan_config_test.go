package config_test

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestParseScanReturnsDefaults(t *testing.T) {
	cfg, err := config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
	})
	if err != nil {
		t.Fatalf("ParseScan() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	gotPressure, err := got.Pressure.Resolve()
	if err != nil {
		t.Fatalf("Pressure.Resolve() error = %v", err)
	}

	want := config.ScanValues{
		CIDRFile:           "targets.csv",
		CIDRIPCol:          "ip",
		CIDRIPCidrCol:      "ip_cidr",
		ResumeInput:        "buckets.json",
		Output:             "scan_results.csv",
		OutputFlushResults: 1000,
		Workers:            10,
		DialTimeout:        100 * time.Millisecond,
		DispatchDelay:      10 * time.Millisecond,
		BucketRate:         100,
		BucketCapacity:     100,
		LogLevel:           "info",
		Format:             "human",
		ProgressInterval:   100,
	}
	wantPressure := config.PressureValues{
		Kind:     config.PressureKindSimple,
		Endpoint: "http://localhost:8080/api/pressure",
		Interval: 5 * time.Second,
	}
	got.Pressure = config.PressurePolicy{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(gotPressure, wantPressure) {
		t.Fatalf("Pressure.Resolve() = %#v, want %#v", gotPressure, wantPressure)
	}
}

func TestParseScanAcceptsEveryOutputFlushMode(t *testing.T) {
	for _, interval := range []string{"0", "1", "1000", "9223372036854775807"} {
		cfg, err := config.ParseScan([]string{
			"-cidr-file", "targets.csv",
			"-resume", "buckets.json",
			"-output-flush-results", interval,
		})
		if err != nil {
			t.Fatalf("interval=%s ParseScan() error = %v", interval, err)
		}
		values, err := cfg.Resolve()
		if err != nil {
			t.Fatalf("interval=%s Resolve() error = %v", interval, err)
		}
		if got := values.OutputFlushResults; fmt.Sprint(got) != interval {
			t.Fatalf("OutputFlushResults = %d, want %s", got, interval)
		}
	}
}

func TestParseScanRejectsNegativeOutputFlushBeforeIO(t *testing.T) {
	_, err := config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
		"-output-flush-results", "-1",
	})
	if err == nil || !strings.Contains(err.Error(), "-output-flush-results must be >= 0") {
		t.Fatalf("ParseScan() error = %v", err)
	}
}

func TestScanAndPressureResolveRejectZeroValues(t *testing.T) {
	var scanConfig config.ScanConfig
	if _, err := scanConfig.Resolve(); !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("ScanConfig.Resolve() error = %v, want ErrUninitializedConfiguration", err)
	}

	var policy config.PressurePolicy
	if _, err := policy.Resolve(); !errors.Is(err, config.ErrUninitializedConfiguration) {
		t.Fatalf("PressurePolicy.Resolve() error = %v, want ErrUninitializedConfiguration", err)
	}
}

func TestPressurePoliciesRejectInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "simple zero interval", run: func() error {
			_, err := config.SimplePressure("https://router.example/pressure", 0)
			return err
		}},
		{name: "authenticated missing data URL", run: func() error {
			_, err := config.AuthenticatedPressure("https://auth.example/token", nil, "id", "secret", time.Second)
			return err
		}},
		{name: "authenticated blank client ID", run: func() error {
			_, err := config.AuthenticatedPressure("https://auth.example/token", []string{"https://router.example/pressure"}, "", "secret", time.Second)
			return err
		}},
		{name: "authenticated blank secret", run: func() error {
			_, err := config.AuthenticatedPressure("https://auth.example/token", []string{"https://router.example/pressure"}, "id", "", time.Second)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Fatal("pressure constructor error = nil")
			}
		})
	}
}

func TestPressurePolicyCopiesDataEndpoints(t *testing.T) {
	dataURLs := []string{"https://one.example/pressure"}
	policy, err := config.AuthenticatedPressure(
		"https://auth.example/token",
		dataURLs,
		"id",
		"secret",
		time.Second,
	)
	if err != nil {
		t.Fatalf("AuthenticatedPressure() error = %v", err)
	}
	dataURLs[0] = "https://changed.example/pressure"

	first, err := policy.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	first.DataEndpoints[0] = "https://also-changed.example/pressure"
	second, err := policy.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := second.DataEndpoints[0]; got != "https://one.example/pressure" {
		t.Fatalf("second Resolve() data URL = %q", got)
	}
}

func TestParseScanReturnsDisabledPressurePolicy(t *testing.T) {
	cfg, err := config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
		"-disable-api",
	})
	if err != nil {
		t.Fatalf("ParseScan() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	policy, err := values.Pressure.Resolve()
	if err != nil {
		t.Fatalf("Pressure.Resolve() error = %v", err)
	}
	if policy.Kind != config.PressureKindDisabled {
		t.Fatalf("pressure kind = %v, want disabled", policy.Kind)
	}
}

func TestParseScanPreservesLegacyDurationAndURLValues(t *testing.T) {
	cfg, err := config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
		"-timeout", "0s",
		"-delay", "-1ms",
		"-pressure-api", "relative",
	})
	if err != nil {
		t.Fatalf("ParseScan() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	policy, err := values.Pressure.Resolve()
	if err != nil {
		t.Fatalf("Pressure.Resolve() error = %v", err)
	}
	if values.DialTimeout != 0 || values.DispatchDelay != -time.Millisecond || policy.Endpoint != "relative" {
		t.Fatalf("Resolve() = %#v with pressure %#v", values, policy)
	}

	_, err = config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
		"-pressure-use-auth",
		"-pressure-auth-url", "relative-auth",
		"-pressure-data-url", "relative-data",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err != nil {
		t.Fatalf("ParseScan() with legacy relative OAuth URLs error = %v", err)
	}
}

func TestPressurePoliciesResolveVariants(t *testing.T) {
	disabled, err := config.PressureDisabled().Resolve()
	if err != nil {
		t.Fatalf("PressureDisabled().Resolve() error = %v", err)
	}
	if disabled.Kind != config.PressureKindDisabled {
		t.Fatalf("PressureDisabled().Resolve().Kind = %v, want disabled", disabled.Kind)
	}

	authenticated, err := config.AuthenticatedPressure(
		"https://auth.example/token",
		[]string{"https://one.example/pressure", "https://two.example/pressure"},
		"client-id",
		"client-secret",
		7*time.Second,
	)
	if err != nil {
		t.Fatalf("AuthenticatedPressure() error = %v", err)
	}
	got, err := authenticated.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := config.PressureValues{
		Kind:          config.PressureKindAuthenticated,
		AuthEndpoint:  "https://auth.example/token",
		DataEndpoints: []string{"https://one.example/pressure", "https://two.example/pressure"},
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Interval:      7 * time.Second,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestParseScanReturnsAcceptedFlagsAndAuthenticatedPolicy(t *testing.T) {
	cfg, err := config.ParseScan([]string{
		"-cidr-file", "custom.csv",
		"-cidr-ip-col", "address",
		"-cidr-ip-cidr-col", "network",
		"-port-file", "ports.csv",
		"-resume", "custom.json",
		"-output", "reports",
		"-workers", "24",
		"-timeout", "2s",
		"-delay", "20ms",
		"-bucket-rate", "80",
		"-bucket-capacity", "120",
		"-log-level", "debug",
		"-format", "json",
		"-quiet",
		"-progress-interval", "17",
		"-pressure-use-auth",
		"-pressure-auth-url", "https://auth.example/token",
		"-pressure-data-url", "https://one.example/pressure, https://two.example/pressure",
		"-pressure-client-id", "client-id",
		"-pressure-client-secret", "client-secret",
		"-pressure-interval", "7s",
	})
	if err != nil {
		t.Fatalf("ParseScan() error = %v", err)
	}

	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	gotPressure, err := got.Pressure.Resolve()
	if err != nil {
		t.Fatalf("Pressure.Resolve() error = %v", err)
	}
	want := config.ScanValues{
		CIDRFile:           "custom.csv",
		CIDRIPCol:          "address",
		CIDRIPCidrCol:      "network",
		PortFile:           "ports.csv",
		ResumeInput:        "custom.json",
		Output:             "reports",
		OutputFlushResults: 1000,
		Workers:            24,
		DialTimeout:        2 * time.Second,
		DispatchDelay:      20 * time.Millisecond,
		BucketRate:         80,
		BucketCapacity:     120,
		LogLevel:           "debug",
		Format:             "json",
		Quiet:              true,
		ProgressInterval:   17,
	}
	wantPressure := config.PressureValues{
		Kind:          config.PressureKindAuthenticated,
		AuthEndpoint:  "https://auth.example/token",
		DataEndpoints: []string{"https://one.example/pressure", "https://two.example/pressure"},
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Interval:      7 * time.Second,
	}
	got.Pressure = config.PressurePolicy{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(gotPressure, wantPressure) {
		t.Fatalf("Pressure.Resolve() = %#v, want %#v", gotPressure, wantPressure)
	}
}

// TestParseScanAcceptsNonPositiveProgressInterval pins the agreed contract: the
// scan parser does not reject a progress interval that is not positive, the
// same as the pre-ping and generate-buckets parsers. The scan runtime replaces
// such a value with the built-in cadence (see scanapp.Run).
func TestParseScanAcceptsNonPositiveProgressInterval(t *testing.T) {
	for _, raw := range []string{"0", "-5"} {
		cfg, err := config.ParseScan([]string{
			"-cidr-file", "targets.csv",
			"-resume", "buckets.json",
			"-progress-interval", raw,
		})
		if err != nil {
			t.Fatalf("ParseScan() with -progress-interval %s error = %v", raw, err)
		}
		got, err := cfg.Resolve()
		if err != nil {
			t.Fatalf("Resolve() with -progress-interval %s error = %v", raw, err)
		}
		want, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("Atoi(%s) error = %v", raw, err)
		}
		if got.ProgressInterval != want {
			t.Fatalf("ProgressInterval = %d, want %d", got.ProgressInterval, want)
		}
	}
}

func TestNewScanReturnsValidatedValues(t *testing.T) {
	policy, err := config.SimplePressure("https://router.example/pressure", 3*time.Second)
	if err != nil {
		t.Fatalf("SimplePressure() error = %v", err)
	}
	want := config.ScanValues{
		CIDRFile:       "targets.csv",
		CIDRIPCol:      "address",
		CIDRIPCidrCol:  "network",
		PortFile:       "ports.csv",
		ResumeInput:    "buckets.json",
		Output:         "reports",
		Workers:        12,
		DialTimeout:    250 * time.Millisecond,
		DispatchDelay:  time.Millisecond,
		BucketRate:     50,
		BucketCapacity: 75,
		LogLevel:       "debug",
		Format:         "json",
		Quiet:          true,
		Pressure:       policy,
	}

	cfg, err := config.NewScan(want)
	if err != nil {
		t.Fatalf("NewScan() error = %v", err)
	}
	got, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestNewScanRejectsInvalidValues(t *testing.T) {
	policy, err := config.SimplePressure("https://router.example/pressure", 3*time.Second)
	if err != nil {
		t.Fatalf("SimplePressure() error = %v", err)
	}
	valid := config.ScanValues{
		CIDRFile:       "targets.csv",
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		ResumeInput:    "buckets.json",
		Workers:        10,
		DialTimeout:    100 * time.Millisecond,
		DispatchDelay:  10 * time.Millisecond,
		BucketRate:     100,
		BucketCapacity: 100,
		Format:         "human",
		Pressure:       policy,
	}

	tests := []struct {
		name   string
		change func(*config.ScanValues)
	}{
		{name: "missing CIDR file", change: func(v *config.ScanValues) { v.CIDRFile = "" }},
		{name: "blank IP column", change: func(v *config.ScanValues) { v.CIDRIPCol = " " }},
		{name: "blank CIDR column", change: func(v *config.ScanValues) { v.CIDRIPCidrCol = " " }},
		{name: "missing resume input", change: func(v *config.ScanValues) { v.ResumeInput = "" }},
		{name: "negative output flush", change: func(v *config.ScanValues) { v.OutputFlushResults = -1 }},
		{name: "zero workers", change: func(v *config.ScanValues) { v.Workers = 0 }},
		{name: "too many workers", change: func(v *config.ScanValues) { v.Workers = config.MaxWorkers + 1 }},
		{name: "zero bucket rate", change: func(v *config.ScanValues) { v.BucketRate = 0 }},
		{name: "zero bucket capacity", change: func(v *config.ScanValues) { v.BucketCapacity = 0 }},
		{name: "invalid format", change: func(v *config.ScanValues) { v.Format = "yaml" }},
		{name: "uninitialized pressure", change: func(v *config.ScanValues) { v.Pressure = config.PressurePolicy{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := valid
			tt.change(&values)
			if _, err := config.NewScan(values); err == nil {
				t.Fatal("NewScan() error = nil")
			}
		})
	}
}
