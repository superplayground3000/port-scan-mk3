package config

import (
	"testing"
	"time"
)

func TestParseConfig_WhenRequiredFlagsProvided_UsesDefaultValues(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 100*time.Millisecond {
		t.Fatalf("timeout mismatch: %v", cfg.Timeout)
	}
	if cfg.Format != "human" {
		t.Fatalf("format mismatch: %s", cfg.Format)
	}
}

func TestParseConfig_WhenRequiredFlagsProvided_PreScanPingIsEnabledByDefault(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DisablePreScanPing {
		t.Fatal("expected pre-scan ping enabled by default")
	}
}

func TestParseConfig_WhenDisablePreScanPingFlagProvided_TurnsFeatureOff(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv", "-disable-pre-scan-ping=true"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DisablePreScanPing {
		t.Fatal("expected disable-pre-scan-ping to be true")
	}
}

func TestParseConfig_WhenPortFileOmitted_StillParses(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "cidr.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PortFile != "" {
		t.Fatalf("expected empty port file, got %q", cfg.PortFile)
	}
}

func TestParseConfig_WhenCIDRFileMissing_ReturnsError(t *testing.T) {
	_, err := Parse([]string{"-port-file", "ports.csv"})
	if err == nil {
		t.Fatal("expected error for missing cidr-file")
	}
}

func TestParseConfig_WhenFormatIsInvalid_ReturnsError(t *testing.T) {
	_, err := Parse([]string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv", "-format", "xml"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestParseConfig_WhenPressureIntervalProvidedInSeconds_ParsesDuration(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-interval", "7",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PressureInterval != 7*time.Second {
		t.Fatalf("expected 7s, got %v", cfg.PressureInterval)
	}
}

func TestParseConfig_WhenCIDRColumnFlagsProvided_SetsColumnNames(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-cidr-ip-col", "source_ip",
		"-cidr-ip-cidr-col", "source_cidr",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.CIDRIPCol != "source_ip" || cfg.CIDRIPCidrCol != "source_cidr" {
		t.Fatalf("unexpected cols: %#v", cfg)
	}
}

func TestParseConfig_WhenCIDRIPColumnIsEmpty_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-cidr-ip-col", " ",
		"-cidr-ip-cidr-col", "source_cidr",
	})
	if err == nil {
		t.Fatal("expected error for empty cidr-ip-col")
	}
}

func TestParseConfig_WhenOutputAndResumeFlagsProvided_SetsPaths(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-output", "/tmp/out.csv",
		"-resume", "/tmp/resume_state.json",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Output != "/tmp/out.csv" {
		t.Fatalf("unexpected output: %s", cfg.Output)
	}
	if cfg.Resume != "/tmp/resume_state.json" {
		t.Fatalf("unexpected resume path: %s", cfg.Resume)
	}
}

func TestParseConfig_WhenPressureIntervalIsInvalidString_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-interval", "not-a-duration",
	})
	if err == nil {
		t.Fatal("expected error for invalid pressure-interval string")
	}
}

func TestParseConfig_WhenPressureIntervalIsZero_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-interval", "0s",
	})
	if err == nil {
		t.Fatal("expected error for zero pressure-interval")
	}
}

func TestParseConfig_WhenPressureIntervalIsNegative_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-interval", "-5s",
	})
	if err == nil {
		t.Fatal("expected error for negative pressure-interval")
	}
}

func TestParseConfig_WhenPressureUseAuthSetWithoutAuthURL_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-use-auth",
		"-pressure-data-url", "http://data",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err == nil {
		t.Fatal("expected error when pressure-use-auth without auth-url")
	}
}

func TestParseConfig_WhenPressureUseAuthSetWithoutDataURL_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err == nil {
		t.Fatal("expected error when pressure-use-auth without data-url")
	}
}

func TestParseConfig_WhenPressureUseAuthSetWithoutClientID_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "http://data",
		"-pressure-client-secret", "secret",
	})
	if err == nil {
		t.Fatal("expected error when pressure-use-auth without client-id")
	}
}

func TestParseConfig_WhenPressureUseAuthSetWithoutClientSecret_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "http://data",
		"-pressure-client-id", "id",
	})
	if err == nil {
		t.Fatal("expected error when pressure-use-auth without client-secret")
	}
}

func TestParseConfig_WhenPressureUseAuthSetWithAllRequiredFields_Succeeds(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "http://data",
		"-pressure-client-id", "my-client-id",
		"-pressure-client-secret", "my-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.PressureUseAuth {
		t.Fatal("expected PressureUseAuth to be true")
	}
	if cfg.PressureAuthURL != "http://auth" {
		t.Fatalf("unexpected auth url: %s", cfg.PressureAuthURL)
	}
	if len(cfg.PressureDataURLs) != 1 || cfg.PressureDataURLs[0] != "http://data" {
		t.Fatalf("unexpected data urls: %v", cfg.PressureDataURLs)
	}
}

func TestParseConfig_WhenPressureDataURLHasMultipleURLs_ParsesAll(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "http://data1,http://data2, http://data3",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.PressureDataURLs) != 3 {
		t.Fatalf("expected 3 data urls, got %d: %v", len(cfg.PressureDataURLs), cfg.PressureDataURLs)
	}
	if cfg.PressureDataURLs[0] != "http://data1" || cfg.PressureDataURLs[1] != "http://data2" || cfg.PressureDataURLs[2] != "http://data3" {
		t.Fatalf("unexpected data urls: %v", cfg.PressureDataURLs)
	}
}

func TestParseConfig_WhenPressureDataURLIsWhitespaceOnly_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "  ,  ",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err == nil {
		t.Fatal("expected error when pressure-data-url contains only whitespace")
	}
}

func TestParseConfig_WhenCIDRIPCidrColumnIsEmpty_ReturnsError(t *testing.T) {
	_, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-cidr-ip-col", "source_ip",
		"-cidr-ip-cidr-col", " ",
	})
	if err == nil {
		t.Fatal("expected error for empty cidr-ip-cidr-col")
	}
}

func TestParseConfig_WhenPressureIntervalProvidedAsDuration_ParsesCorrectly(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-pressure-interval", "10s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PressureInterval != 10*time.Second {
		t.Fatalf("expected 10s, got %v", cfg.PressureInterval)
	}
}

func TestParseConfig_WithWorkerAndBucketFlags_SetsValues(t *testing.T) {
	cfg, err := Parse([]string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-workers", "50",
		"-bucket-rate", "200",
		"-bucket-capacity", "500",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Workers != 50 {
		t.Fatalf("expected 50 workers, got %d", cfg.Workers)
	}
	if cfg.BucketRate != 200 {
		t.Fatalf("expected bucket-rate 200, got %d", cfg.BucketRate)
	}
	if cfg.BucketCapacity != 500 {
		t.Fatalf("expected bucket-capacity 500, got %d", cfg.BucketCapacity)
	}
}

func TestParse_PreScanPingTimeout_DefaultsTo100ms(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "targets.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PreScanPingTimeout != 100*time.Millisecond {
		t.Fatalf("expected default 100ms, got %v", cfg.PreScanPingTimeout)
	}
}

func TestParse_PreScanPingTimeout_AcceptsDurationString(t *testing.T) {
	cfg, err := Parse([]string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", "250ms"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PreScanPingTimeout != 250*time.Millisecond {
		t.Fatalf("expected 250ms, got %v", cfg.PreScanPingTimeout)
	}
}

func TestParse_PreScanPingTimeout_RejectsNonPositive(t *testing.T) {
	for _, val := range []string{"0", "0s", "-5ms"} {
		if _, err := Parse([]string{"-cidr-file", "targets.csv", "-pre-scan-ping-timeout", val}); err == nil {
			t.Fatalf("expected error for -pre-scan-ping-timeout=%s", val)
		}
	}
}
