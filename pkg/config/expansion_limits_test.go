package config_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type expansionLimitConfig interface {
	ResolveTargetExpansion() (config.TargetExpansionValues, error)
}

func parseExpansionCommand(command string, extra ...string) (expansionLimitConfig, error) {
	args := []string{"-cidr-file", "targets.csv"}
	switch command {
	case "validate":
		args = append(args, extra...)
		return config.ParseValidate(args)
	case "pre-ping":
		args = append(args, extra...)
		return config.ParsePrePing(args)
	case "generate-buckets":
		args = append(args, "-buckets-out", "buckets.json")
		args = append(args, extra...)
		return config.ParseGenerateBuckets(args)
	case "scan":
		args = append(args, "-resume", "buckets.json")
		args = append(args, extra...)
		return config.ParseScan(args)
	default:
		panic("unknown command")
	}
}

func TestExpansionLimitFlags_AllCommandsUseDefaultsAndAcceptOverrides(t *testing.T) {
	for _, command := range []string{"validate", "pre-ping", "generate-buckets", "scan"} {
		t.Run(command+" defaults", func(t *testing.T) {
			cfg, err := parseExpansionCommand(command)
			if err != nil {
				t.Fatalf("parse command error = %v", err)
			}
			values, err := cfg.ResolveTargetExpansion()
			if err != nil {
				t.Fatalf("ResolveTargetExpansion() error = %v", err)
			}
			if values.Limits.CandidateLimit() != 10_000_000 || values.Limits.MemoryLimitGB() != 16 {
				t.Fatalf("limits = (%d, %d), want (10000000, 16)", values.Limits.CandidateLimit(), values.Limits.MemoryLimitGB())
			}
			if command == "scan" && (values.CountSet || values.MemorySet) {
				t.Fatalf("scan default presence = (%t, %t), want both false", values.CountSet, values.MemorySet)
			}
		})

		t.Run(command+" override", func(t *testing.T) {
			cfg, err := parseExpansionCommand(command,
				"-target-count-limit", "20000000",
				"-target-memory-limit-gb", "32",
			)
			if err != nil {
				t.Fatalf("parse command error = %v", err)
			}
			values, err := cfg.ResolveTargetExpansion()
			if err != nil {
				t.Fatalf("ResolveTargetExpansion() error = %v", err)
			}
			if values.Limits.CandidateLimit() != 20_000_000 || values.Limits.MemoryLimitGB() != 32 {
				t.Fatalf("limits = (%d, %d), want (20000000, 32)", values.Limits.CandidateLimit(), values.Limits.MemoryLimitGB())
			}
			if !values.CountSet || !values.MemorySet {
				t.Fatalf("override presence = (%t, %t), want both true", values.CountSet, values.MemorySet)
			}
		})

		t.Run(command+" bypass", func(t *testing.T) {
			cfg, err := parseExpansionCommand(command,
				"-target-count-limit", "0",
				"-target-memory-limit-gb", "0",
			)
			if err != nil {
				t.Fatalf("parse command error = %v", err)
			}
			values, err := cfg.ResolveTargetExpansion()
			if err != nil {
				t.Fatalf("ResolveTargetExpansion() error = %v", err)
			}
			if values.Limits.CandidateLimit() != 0 || values.Limits.MemoryLimitGB() != 0 {
				t.Fatalf("limits = (%d, %d), want bypass", values.Limits.CandidateLimit(), values.Limits.MemoryLimitGB())
			}
		})
	}
}

func TestExpansionLimitFlags_RejectNegativeAndOverflowBeforeWorkflow(t *testing.T) {
	for _, command := range []string{"validate", "pre-ping", "generate-buckets", "scan"} {
		for _, tc := range []struct {
			flag string
			want string
		}{
			{flag: "-target-count-limit", want: "-target-count-limit must be >= 0"},
			{flag: "-target-memory-limit-gb", want: "-target-memory-limit-gb must be >= 0"},
		} {
			_, err := parseExpansionCommand(command, tc.flag, "-1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %s error = %v, want %q", command, tc.flag, err, tc.want)
			}
		}
	}

	max := strconv.FormatInt(math.MaxInt64, 10)
	cfg, err := parseExpansionCommand("validate", "-target-count-limit", max, "-target-memory-limit-gb", "0")
	if err != nil {
		t.Fatalf("maximum count flag error = %v", err)
	}
	values, err := cfg.ResolveTargetExpansion()
	if err != nil || values.Limits.CandidateLimit() != math.MaxInt64 {
		t.Fatalf("maximum count resolve = (%+v, %v)", values, err)
	}
	if _, err := parseExpansionCommand("validate", "-target-memory-limit-gb", max); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("maximum memory flag error = %v, want overflow error", err)
	}
}

func TestScanExpansionLimitFlags_RecordIndependentPresence(t *testing.T) {
	cfg, err := parseExpansionCommand("scan", "-target-count-limit", "0")
	if err != nil {
		t.Fatal(err)
	}
	values, err := cfg.ResolveTargetExpansion()
	if err != nil {
		t.Fatal(err)
	}
	if !values.CountSet || values.MemorySet {
		t.Fatalf("presence = (%t, %t), want (true, false)", values.CountSet, values.MemorySet)
	}
}
