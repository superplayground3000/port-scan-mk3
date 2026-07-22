package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunMain_Help(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"--help"}, out, errOut)
	if code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
	if !strings.Contains(out.String(), "port-scan scan") {
		t.Fatalf("unexpected help: %s", out.String())
	}
}

func TestRunMain_UnknownCommand(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"badcmd"}, out, errOut)
	if code != 2 {
		t.Fatalf("expected 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestRunMain_ScanParseError(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"scan"}, out, errOut)
	if code != 2 {
		t.Fatalf("expected 2, got %d", code)
	}
}

func TestCLIHelp_IncludesRequiredFlags(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := runMain([]string{"--help"}, out, errOut)
	if code != 0 {
		t.Fatalf("expected zero exit, got %d", code)
	}
	// Post-split help lists all three pipeline subcommands and their flags.
	// -disable-pre-scan-ping is intentionally gone (skip pinging by skipping the
	// preping step); the relocated flags -pre-scan-ping-timeout, -buckets-out,
	// and -unreachable-file appear on their owning subcommands.
	for _, want := range []string{
		"preping", "generate-buckets", "scan",
		"-cidr-file", "-port-file", "-cidr-ip-col", "-cidr-ip-cidr-col",
		"-resume", "-buckets-out", "-unreachable-file", "-pre-scan-ping-timeout",
		"-disable-api", "-format",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing help flag %s", want)
		}
	}
	if strings.Contains(out.String(), "-disable-pre-scan-ping") {
		t.Fatalf("help must not advertise removed -disable-pre-scan-ping flag: %s", out.String())
	}
}
