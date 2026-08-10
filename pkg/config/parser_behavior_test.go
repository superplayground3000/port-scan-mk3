package config_test

import (
	"errors"
	"flag"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestCommandParsersRejectMissingRequiredValues(t *testing.T) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{name: "pre-ping CIDR", parse: func() error {
			_, err := config.ParsePrePing(nil)
			return err
		}},
		{name: "bucket output", parse: func() error {
			_, err := config.ParseGenerateBuckets([]string{"-cidr-file", "targets.csv"})
			return err
		}},
		{name: "scan resume", parse: func() error {
			_, err := config.ParseScan([]string{"-cidr-file", "targets.csv"})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.parse(); err == nil {
				t.Fatal("parser error = nil, want missing value error")
			}
		})
	}
}

func TestCommandParsersRejectInvalidSharedValues(t *testing.T) {
	tests := []struct {
		name  string
		parse func() error
	}{
		{name: "pre-ping format", parse: func() error {
			_, err := config.ParsePrePing([]string{"-cidr-file", "targets.csv", "-format", "yaml"})
			return err
		}},
		{name: "bucket IP column", parse: func() error {
			_, err := config.ParseGenerateBuckets([]string{"-cidr-file", "targets.csv", "-buckets-out", "buckets.json", "-cidr-ip-col", " "})
			return err
		}},
		{name: "scan CIDR column", parse: func() error {
			_, err := config.ParseScan([]string{"-cidr-file", "targets.csv", "-resume", "buckets.json", "-cidr-ip-cidr-col", " "})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.parse(); err == nil {
				t.Fatal("parser error = nil, want shared value error")
			}
		})
	}
}

func TestCommandParsersPreserveHelpErrorClass(t *testing.T) {
	parsers := []func([]string) error{
		func(args []string) error { _, err := config.ParsePrePing(args); return err },
		func(args []string) error { _, err := config.ParseGenerateBuckets(args); return err },
		func(args []string) error { _, err := config.ParseScan(args); return err },
	}
	for i, parse := range parsers {
		if err := parse([]string{"-help"}); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("parser %d help error = %v, want flag.ErrHelp", i, err)
		}
	}
}

func TestParseScanRejectsInvalidPressureValues(t *testing.T) {
	base := []string{"-cidr-file", "targets.csv", "-resume", "buckets.json"}
	tests := [][]string{
		{"-pressure-interval", "later"},
		{"-pressure-interval", "0s"},
		{"-pressure-data-url", " , "},
		{"-pressure-use-auth"},
		{"-pressure-use-auth", "-pressure-auth-url", "auth"},
		{"-pressure-use-auth", "-pressure-auth-url", "auth", "-pressure-data-url", "data"},
		{"-pressure-use-auth", "-pressure-auth-url", "auth", "-pressure-data-url", "data", "-pressure-client-id", "client"},
	}
	for _, extra := range tests {
		args := append(append([]string(nil), base...), extra...)
		if _, err := config.ParseScan(args); err == nil {
			t.Fatalf("ParseScan(%v) error = nil, want rejection", extra)
		}
	}
}

func TestParseScanAcceptsIntegerPressureInterval(t *testing.T) {
	cfg, err := config.ParseScan([]string{
		"-cidr-file", "targets.csv",
		"-resume", "buckets.json",
		"-pressure-interval", "7",
	})
	if err != nil {
		t.Fatalf("ParseScan() error = %v", err)
	}
	values, err := cfg.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	pressureValues, err := values.Pressure.Resolve()
	if err != nil {
		t.Fatalf("Pressure.Resolve() error = %v", err)
	}
	if pressureValues.Interval != 7*time.Second {
		t.Fatalf("pressure interval = %s, want 7s", pressureValues.Interval)
	}
}
