package config

import (
	"errors"
	"strconv"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

func TestNewValidate_WhenValuesAreValid_ReturnsResolvableConfiguration(t *testing.T) {
	cfg, err := NewValidate(ValidateValues{
		CIDRFile:      "targets.csv",
		CIDRIPCol:     "target_ip",
		CIDRIPCidrCol: "target_cidr",
		PortFile:      "ports.csv",
		Format:        "json",
	})
	if err != nil {
		t.Fatalf("NewValidate() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if values.CIDRFile != "targets.csv" || values.PortFile != "ports.csv" {
		t.Fatalf("unexpected input paths: %+v", values)
	}
	if values.CIDRIPCol != "target_ip" || values.CIDRIPCidrCol != "target_cidr" {
		t.Fatalf("unexpected column names: %+v", values)
	}
	if values.Format != "json" {
		t.Fatalf("Format = %q, want json", values.Format)
	}
}

func TestValidateConfig_WhenZeroValueResolved_ReturnsUninitializedError(t *testing.T) {
	var cfg ValidateConfig
	if _, err := cfg.Resolve(); !errors.Is(err, ErrUninitializedConfiguration) {
		t.Fatalf("Resolve() error = %v, want ErrUninitializedConfiguration", err)
	}
}

func TestNewValidate_WhenValuesAreInvalid_ReturnsError(t *testing.T) {
	valid := ValidateValues{
		CIDRFile:      "targets.csv",
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
		Format:        "human",
	}
	tests := []struct {
		name   string
		change func(*ValidateValues)
	}{
		{name: "missing cidr", change: func(v *ValidateValues) { v.CIDRFile = "" }},
		{name: "empty ip column", change: func(v *ValidateValues) { v.CIDRIPCol = " " }},
		{name: "empty cidr column", change: func(v *ValidateValues) { v.CIDRIPCidrCol = "\t" }},
		{name: "invalid format", change: func(v *ValidateValues) { v.Format = "xml" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := valid
			test.change(&values)
			if _, err := NewValidate(values); err == nil {
				t.Fatal("NewValidate() error = nil, want rejection")
			}
		})
	}
}

func TestParseValidate_WhenOnlyRequiredFlagProvided_AppliesValidateDefaults(t *testing.T) {
	cfg, err := ParseValidate([]string{"-cidr-file", "targets.csv"})
	if err != nil {
		t.Fatalf("ParseValidate() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := ValidateValues{
		CIDRFile:      "targets.csv",
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
		Format:        "human",
	}
	if values != want {
		t.Fatalf("Resolve() = %+v, want %+v", values, want)
	}
}

func TestParseValidate_WhenCompleteLegacyFlagSurfaceProvided_AcceptsAndDiscardsUnusedValues(t *testing.T) {
	cfg, err := ParseValidate([]string{
		"-cidr-file", "targets.csv",
		"-port-file", "ports.csv",
		"-output", "results.csv",
		"-timeout", "250ms",
		"-delay", "20ms",
		"-bucket-rate", "200",
		"-bucket-capacity", "300",
		"-workers", "20",
		"-pressure-api", "https://pressure.example.test",
		"-pressure-interval", "7",
		"-disable-api",
		"-pressure-auth-url", "https://auth.example.test",
		"-pressure-data-url", "https://one.example.test, https://two.example.test",
		"-pressure-client-id", "client",
		"-pressure-client-secret", "secret",
		"-pressure-use-auth",
		"-disable-pre-scan-ping",
		"-pre-scan-ping-timeout", "300ms",
		"-resume", "resume.json",
		"-log-level", "debug",
		"-format", "json",
		"-quiet",
		"-cidr-ip-col", "target_ip",
		"-cidr-ip-cidr-col", "target_cidr",
	})
	if err != nil {
		t.Fatalf("ParseValidate() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := ValidateValues{
		CIDRFile:      "targets.csv",
		PortFile:      "ports.csv",
		CIDRIPCol:     "target_ip",
		CIDRIPCidrCol: "target_cidr",
		Format:        "json",
	}
	if values != want {
		t.Fatalf("Resolve() = %+v, want %+v", values, want)
	}
}

func TestParseValidate_WhenLegacyValuesAreInvalid_ReturnsError(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing cidr", args: []string{"-port-file", "ports.csv"}},
		{name: "empty column", args: []string{"-cidr-file", "targets.csv", "-cidr-ip-col", " "}},
		{name: "format", args: []string{"-cidr-file", "targets.csv", "-format", "xml"}},
		{name: "workers", args: []string{"-cidr-file", "targets.csv", "-workers", "0"}},
		{name: "too many workers", args: []string{"-cidr-file", "targets.csv", "-workers", strconv.Itoa(MaxWorkers + 1)}},
		{name: "bucket rate", args: []string{"-cidr-file", "targets.csv", "-bucket-rate", "0"}},
		{name: "bucket rate above limit", args: []string{"-cidr-file", "targets.csv", "-bucket-rate", strconv.Itoa(ratelimit.MaxRate + 1)}},
		{name: "bucket capacity", args: []string{"-cidr-file", "targets.csv", "-bucket-capacity", "0"}},
		{name: "bucket capacity above limit", args: []string{"-cidr-file", "targets.csv", "-bucket-capacity", strconv.Itoa(ratelimit.MaxCapacity + 1)}},
		{name: "pressure interval", args: []string{"-cidr-file", "targets.csv", "-pressure-interval", "0s"}},
		{name: "pressure interval syntax", args: []string{"-cidr-file", "targets.csv", "-pressure-interval", "later"}},
		{name: "empty pressure data URLs", args: []string{"-cidr-file", "targets.csv", "-pressure-data-url", " , "}},
		{name: "ping timeout", args: []string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", "0s"}},
		{name: "timeout syntax", args: []string{"-cidr-file", "targets.csv", "-timeout", "later"}},
		{name: "delay syntax", args: []string{"-cidr-file", "targets.csv", "-delay", "later"}},
		{name: "ping timeout syntax", args: []string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", "later"}},
		{name: "oauth auth URL", args: []string{"-cidr-file", "targets.csv", "-pressure-use-auth"}},
		{name: "oauth data URL", args: []string{"-cidr-file", "targets.csv", "-pressure-use-auth", "-pressure-auth-url", "auth"}},
		{name: "oauth client ID", args: []string{"-cidr-file", "targets.csv", "-pressure-use-auth", "-pressure-auth-url", "auth", "-pressure-data-url", "data"}},
		{name: "oauth client secret", args: []string{"-cidr-file", "targets.csv", "-pressure-use-auth", "-pressure-auth-url", "auth", "-pressure-data-url", "data", "-pressure-client-id", "client"}},
		{name: "unknown flag", args: []string{"-cidr-file", "targets.csv", "-unknown"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseValidate(test.args); err == nil {
				t.Fatal("ParseValidate() error = nil, want rejection")
			}
		})
	}
}
