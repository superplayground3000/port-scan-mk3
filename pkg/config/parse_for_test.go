package config

import (
	"testing"
	"time"
)

// --- Ticket T0 required red tests ---

func TestParseFor_Preping_RejectsPingScanFlags(t *testing.T) {
	// preping does not own -disable-pre-scan-ping.
	if _, err := ParseFor("preping", []string{"-cidr-file", "cidr.csv", "-disable-pre-scan-ping"}); err == nil {
		t.Fatal("expected unknown-flag error for -disable-pre-scan-ping on preping")
	}
	// preping does not own -port-file (ping is per-IP).
	if _, err := ParseFor("preping", []string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"}); err == nil {
		t.Fatal("expected unknown-flag error for -port-file on preping")
	}
}

func TestParseFor_GenerateBuckets_RequiresBucketsOut(t *testing.T) {
	// Missing -buckets-out must error.
	if _, err := ParseFor("generate-buckets", []string{"-cidr-file", "cidr.csv"}); err == nil {
		t.Fatal("expected error when -buckets-out is missing")
	}
	// -unreachable-file is optional: absent is fine when -buckets-out is present.
	cfg, err := ParseFor("generate-buckets", []string{"-cidr-file", "cidr.csv", "-buckets-out", "buckets.json"})
	if err != nil {
		t.Fatalf("unexpected error with buckets-out and no unreachable-file: %v", err)
	}
	if cfg.BucketsOut != "buckets.json" {
		t.Fatalf("expected BucketsOut buckets.json, got %q", cfg.BucketsOut)
	}
	if cfg.UnreachableFile != "" {
		t.Fatalf("expected empty UnreachableFile, got %q", cfg.UnreachableFile)
	}
}

func TestParseFor_Scan_RequiresResume(t *testing.T) {
	if _, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv"}); err == nil {
		t.Fatal("expected error when -resume is missing for scan")
	}
	cfg, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv", "-resume", "buckets.json"})
	if err != nil {
		t.Fatalf("unexpected error with resume: %v", err)
	}
	if cfg.Resume != "buckets.json" {
		t.Fatalf("expected Resume buckets.json, got %q", cfg.Resume)
	}
}

func TestParseFor_Scan_RejectsPingFlag(t *testing.T) {
	if _, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv", "-resume", "b.json", "-pre-scan-ping-timeout", "1s"}); err == nil {
		t.Fatal("expected unknown-flag error for -pre-scan-ping-timeout on scan")
	}
	if _, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv", "-resume", "b.json", "-disable-pre-scan-ping"}); err == nil {
		t.Fatal("expected unknown-flag error for -disable-pre-scan-ping on scan")
	}
}

func TestParseFor_ProgressInterval_Default(t *testing.T) {
	commands := map[string][]string{
		"preping":          {"-cidr-file", "cidr.csv"},
		"generate-buckets": {"-cidr-file", "cidr.csv", "-buckets-out", "b.json"},
		"scan":             {"-cidr-file", "cidr.csv", "-resume", "b.json"},
	}
	for cmd, args := range commands {
		cfg, err := ParseFor(cmd, args)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", cmd, err)
		}
		if cfg.ProgressInterval != 100 {
			t.Fatalf("%s: expected default ProgressInterval 100, got %d", cmd, cfg.ProgressInterval)
		}
		// Overridable.
		overArgs := append(append([]string{}, args...), "-progress-interval", "250")
		cfg, err = ParseFor(cmd, overArgs)
		if err != nil {
			t.Fatalf("%s: unexpected error overriding: %v", cmd, err)
		}
		if cfg.ProgressInterval != 250 {
			t.Fatalf("%s: expected ProgressInterval 250, got %d", cmd, cfg.ProgressInterval)
		}
	}
}

// --- Additional coverage for per-command flag surfaces ---

func TestParseFor_Preping_OwnsPingTimeoutAndOutput(t *testing.T) {
	cfg, err := ParseFor("preping", []string{
		"-cidr-file", "cidr.csv",
		"-pre-scan-ping-timeout", "250ms",
		"-output", "out/",
		"-workers", "8",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PreScanPingTimeout != 250*time.Millisecond {
		t.Fatalf("expected 250ms timeout, got %v", cfg.PreScanPingTimeout)
	}
	if cfg.Output != "out/" {
		t.Fatalf("expected output out/, got %q", cfg.Output)
	}
	if cfg.Workers != 8 {
		t.Fatalf("expected 8 workers, got %d", cfg.Workers)
	}
}

func TestParseFor_Preping_RejectsNonPositivePingTimeout(t *testing.T) {
	if _, err := ParseFor("preping", []string{"-cidr-file", "cidr.csv", "-pre-scan-ping-timeout", "0s"}); err == nil {
		t.Fatal("expected error for non-positive ping timeout")
	}
}

func TestParseFor_GenerateBuckets_OwnsPortAndUnreachableFile(t *testing.T) {
	cfg, err := ParseFor("generate-buckets", []string{
		"-cidr-file", "cidr.csv",
		"-port-file", "ports.csv",
		"-buckets-out", "b.json",
		"-unreachable-file", "unreachable.csv",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PortFile != "ports.csv" {
		t.Fatalf("expected PortFile ports.csv, got %q", cfg.PortFile)
	}
	if cfg.UnreachableFile != "unreachable.csv" {
		t.Fatalf("expected UnreachableFile unreachable.csv, got %q", cfg.UnreachableFile)
	}
}

func TestParseFor_GenerateBuckets_RejectsPingAndScanFlags(t *testing.T) {
	for _, bad := range []string{"-pre-scan-ping-timeout", "-timeout", "-resume"} {
		args := []string{"-cidr-file", "cidr.csv", "-buckets-out", "b.json", bad, "x"}
		if _, err := ParseFor("generate-buckets", args); err == nil {
			t.Fatalf("expected unknown-flag error for %s on generate-buckets", bad)
		}
	}
}

func TestParseFor_Scan_OwnsScanAndPressureFlags(t *testing.T) {
	cfg, err := ParseFor("scan", []string{
		"-cidr-file", "cidr.csv",
		"-resume", "b.json",
		"-timeout", "200ms",
		"-delay", "5ms",
		"-bucket-rate", "200",
		"-bucket-capacity", "500",
		"-pressure-interval", "7",
		"-port-file", "ports.csv",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 200*time.Millisecond {
		t.Fatalf("expected timeout 200ms, got %v", cfg.Timeout)
	}
	if cfg.Delay != 5*time.Millisecond {
		t.Fatalf("expected delay 5ms, got %v", cfg.Delay)
	}
	if cfg.BucketRate != 200 || cfg.BucketCapacity != 500 {
		t.Fatalf("unexpected bucket flags: rate=%d cap=%d", cfg.BucketRate, cfg.BucketCapacity)
	}
	if cfg.PressureInterval != 7*time.Second {
		t.Fatalf("expected pressure interval 7s, got %v", cfg.PressureInterval)
	}
	if cfg.PortFile != "ports.csv" {
		t.Fatalf("expected PortFile fallback ports.csv, got %q", cfg.PortFile)
	}
}

func TestParseFor_Scan_PressureUseAuthRequiresAuthFields(t *testing.T) {
	_, err := ParseFor("scan", []string{
		"-cidr-file", "cidr.csv",
		"-resume", "b.json",
		"-pressure-use-auth",
		"-pressure-data-url", "http://data",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err == nil {
		t.Fatal("expected error when -pressure-use-auth is set without -pressure-auth-url")
	}
}

func TestParseFor_Scan_PressureUseAuthSucceedsWithAllFields(t *testing.T) {
	cfg, err := ParseFor("scan", []string{
		"-cidr-file", "cidr.csv",
		"-resume", "b.json",
		"-pressure-use-auth",
		"-pressure-auth-url", "http://auth",
		"-pressure-data-url", "http://data1,http://data2",
		"-pressure-client-id", "id",
		"-pressure-client-secret", "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.PressureDataURLs) != 2 {
		t.Fatalf("expected 2 data urls, got %d", len(cfg.PressureDataURLs))
	}
}

func TestParseFor_Scan_RejectsInvalidPressureInterval(t *testing.T) {
	if _, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv", "-resume", "b.json", "-pressure-interval", "nope"}); err == nil {
		t.Fatal("expected error for invalid pressure-interval")
	}
	if _, err := ParseFor("scan", []string{"-cidr-file", "cidr.csv", "-resume", "b.json", "-pressure-interval", "0s"}); err == nil {
		t.Fatal("expected error for zero pressure-interval")
	}
}

func TestParseFor_CIDRFileRequiredForAllCommands(t *testing.T) {
	cases := map[string][]string{
		"preping":          {},
		"generate-buckets": {"-buckets-out", "b.json"},
		"scan":             {"-resume", "b.json"},
		"validate":         {},
	}
	for cmd, args := range cases {
		if _, err := ParseFor(cmd, args); err == nil {
			t.Fatalf("%s: expected error when -cidr-file is missing", cmd)
		}
	}
}

func TestParseFor_FormatValidatedForAllCommands(t *testing.T) {
	if _, err := ParseFor("preping", []string{"-cidr-file", "cidr.csv", "-format", "xml"}); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestParseFor_EmptyColumnNamesRejected(t *testing.T) {
	if _, err := ParseFor("preping", []string{"-cidr-file", "cidr.csv", "-cidr-ip-col", " "}); err == nil {
		t.Fatal("expected error for empty cidr-ip-col")
	}
}

func TestParseFor_Validate_ParsesLegacyInputFlags(t *testing.T) {
	cfg, err := ParseFor("validate", []string{"-cidr-file", "cidr.csv", "-port-file", "ports.csv"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CIDRFile != "cidr.csv" || cfg.PortFile != "ports.csv" {
		t.Fatalf("unexpected validate cfg: %#v", cfg)
	}
}

func TestParseFor_UnknownCommand_ReturnsError(t *testing.T) {
	if _, err := ParseFor("frobnicate", []string{"-cidr-file", "cidr.csv"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestParseFor_HelpRequest_ReturnsFlagErrHelp(t *testing.T) {
	if _, err := ParseFor("preping", []string{"-h"}); err == nil {
		t.Fatal("expected error (flag.ErrHelp) for -h")
	}
}
