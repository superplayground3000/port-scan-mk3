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

func runScan(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, cancel := state.WithSIGINTCancel(context.Background())
	defer cancel()

	// Build RunOptions with appropriate PressureFetcher
	opts := scanapp.RunOptions{}
	if !cfg.DisableAPI {
		if cfg.PressureUseAuth {
			opts.PressureFetcher = scanapp.NewMultiSourcePressureFetcher(
				cfg.PressureAuthURL,
				cfg.PressureDataURLs,
				cfg.PressureClientID,
				cfg.PressureClientSecret,
				nil,
			)
		} else {
			opts.PressureFetcher = scanapp.NewSimplePressureFetcher(
				cfg.PressureAPI,
				nil,
			)
		}
	}

	err = scanapp.Run(ctx, cfg, stdout, stderr, opts)
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
	fmt.Fprintln(w, "port-scan scan -cidr-file <file> [-port-file <file>] [flags]")
	fmt.Fprintln(w, "port-scan validate -cidr-file <file> [-port-file <file>] [-format human|json]")
	fmt.Fprintln(w, "Flags: -cidr-ip-col -cidr-ip-cidr-col -resume -disable-pre-scan-ping -pre-scan-ping-timeout -disable-api -pressure-api -pressure-interval -pressure-auth-url -pressure-data-url -pressure-client-id -pressure-client-secret -pressure-use-auth -quiet -bucket-rate -bucket-capacity -workers -timeout -delay -log-level -format")
}
