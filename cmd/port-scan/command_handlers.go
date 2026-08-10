package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/xuxiping/port-scan-mk3/pkg/cli"
	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/validate"
)

func handleHelpCommand(stdout io.Writer) int {
	usage(stdout)
	return 0
}

func handleValidateCommand(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	result := validate.Inputs(cfg)
	if err := cli.WriteValidation(stdout, cfg.Format, result.Valid, result.Detail); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if !result.Valid {
		return 1
	}
	return 0
}

func handleScanCommand(args []string, stdout, stderr io.Writer) int {
	return runScan(args, stdout, stderr)
}

// handlePrePingCommand runs the standalone pre-ping step. ParsePrePing rejects
// flags from other commands. RunPrePing writes the unreachable-results CSV and
// writes its resolved path to stdout. A parse error returns 2. Cancellation
// returns 130. All other errors return 1.
func handlePrePingCommand(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.ParsePrePing(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := state.WithSIGINTCancel(context.Background())
	defer cancel()

	if err := scanapp.RunPrePing(ctx, cfg, stdout, stderr, scanapp.RunOptions{}); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "pre-ping canceled")
			return 130
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// handleGenerateBucketsCommand builds a resume bucket snapshot from the target
// inputs (minus an optional unreachable blocklist) and writes it to -buckets-out.
// It performs no network I/O. Exit codes: parse error (including missing
// -buckets-out) → 2, SIGINT/cancel → 130, any other error → 1.
func handleGenerateBucketsCommand(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.ParseGenerateBuckets(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := state.WithSIGINTCancel(context.Background())
	defer cancel()

	if err := scanapp.GenerateBuckets(ctx, cfg, stderr, scanapp.GenerateBucketsOptions{}); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "generate-buckets canceled")
			return 130
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runScan(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.ParseScan(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := state.WithSIGINTCancel(context.Background())
	defer cancel()

	err = scanapp.Run(ctx, cfg, stdout, stderr, scanapp.RunOptions{})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "scan canceled")
			return 130
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "port-scan is a three-step pipeline: pre-ping -> generate-buckets -> scan.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "port-scan pre-ping -cidr-file <file> [-pre-scan-ping-timeout 100ms] [-workers N] [-output <file>] [-progress-interval N] [-cidr-ip-col ip] [-cidr-ip-cidr-col ip_cidr] [-log-level info] [-format human|json] [-quiet]")
	fmt.Fprintln(w, "    Ping unique targets and write unreachable_results-<ts>.csv; prints its path for chaining. No -port-file and no ping-toggle flag (skip pinging by skipping this step).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "port-scan generate-buckets -cidr-file <file> -buckets-out <file> [-port-file <file>] [-unreachable-file <file>] [-workers N] [-progress-interval N] [-cidr-ip-col ip] [-cidr-ip-cidr-col ip_cidr] [-log-level info] [-format human|json] [-quiet]")
	fmt.Fprintln(w, "    Build a resume bucket snapshot over targets minus the optional -unreachable-file blocklist. No network I/O.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "port-scan scan -cidr-file <file> -resume <bucket-file> [-output <file>] [-timeout 100ms] [-workers N] [-delay 10ms] [-bucket-rate N] [-bucket-capacity N] [-disable-api] [-pressure-api URL] [-pressure-interval 5s] [-pressure-auth-url URL] [-pressure-data-url URLs] [-pressure-client-id ID] [-pressure-client-secret SECRET] [-pressure-use-auth] [-progress-interval N] [-cidr-ip-col ip] [-cidr-ip-cidr-col ip_cidr] [-log-level info] [-format human|json] [-quiet]")
	fmt.Fprintln(w, "    Scan the buckets in -resume. Requires -resume; has NO ping flags (scan never pings). -port-file is a fallback normally ignored (chunks carry ports).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "port-scan validate -cidr-file <file> [-port-file <file>] [-format human|json]")
	fmt.Fprintln(w, "    Validate CIDR/port inputs without scanning.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "port-scan version | --version | -version")
	fmt.Fprintln(w, "    Print the release version, commit, build time and toolchain of this binary.")
}
