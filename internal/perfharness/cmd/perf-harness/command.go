package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

type commandOptions struct {
	profile            string
	outputDir          string
	evidenceLabel      string
	cpu                string
	powerMode          string
	filesystem         string
	disk               string
	constraints        string
	commit             string
	ramBytes           uint64
	freeDiskBytes      uint64
	physicalCores      int
	logicalCores       int
	smokeItems         uint64
	smokeSnapshotBytes uint64
	compareLeft        string
	compareRight       string
	regressionBeforeNS float64
	regressionBeforeB  float64
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	options, code := parseCommandOptions(args, stderr)
	if code != 0 {
		return code
	}
	harness := perfharness.New()
	if options.compareLeft != "" {
		return compareReportFiles(options.compareLeft, options.compareRight, harness, stdout, stderr)
	}
	if err := validateGoVersion(runtime.Version()); err != nil {
		_ = writeStatus(stderr, "%v\n", err)
		return 1
	}
	runner, err := newMatrixRunner(options, harness, stdout, stderr)
	if err != nil {
		_ = writeStatus(stderr, "%v\n", err)
		return 1
	}
	return runner.run()
}

func parseCommandOptions(args []string, stderr io.Writer) (commandOptions, int) {
	options := commandOptions{}
	flags := flag.NewFlagSet("perf-harness", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.profile, "profile", "full", "matrix profile: full or smoke")
	flags.StringVar(&options.outputDir, "output", "", "new output directory")
	flags.StringVar(&options.evidenceLabel, "evidence-label", string(perfharness.EvidenceHardwareQualified), "evidence label")
	flags.StringVar(&options.cpu, "cpu", "unknown", "CPU model")
	flags.IntVar(&options.physicalCores, "physical-cores", 0, "physical core count")
	flags.IntVar(&options.logicalCores, "logical-cores", runtime.NumCPU(), "logical core count")
	flags.StringVar(&options.powerMode, "power-mode", "unknown", "power mode")
	flags.Uint64Var(&options.ramBytes, "ram-bytes", 0, "RAM bytes")
	flags.StringVar(&options.filesystem, "filesystem", "unknown", "filesystem")
	flags.StringVar(&options.disk, "disk", "unknown", "disk model")
	flags.Uint64Var(&options.freeDiskBytes, "free-disk-bytes", 0, "free disk bytes")
	flags.StringVar(&options.constraints, "constraints", "none recorded", "resource constraints")
	flags.StringVar(&options.commit, "commit", "unknown", "git commit")
	flags.Uint64Var(&options.smokeItems, "smoke-items", perfharness.SmokeItemCount, "bounded smoke item count")
	flags.Uint64Var(&options.smokeSnapshotBytes, "smoke-snapshot-bytes", perfharness.SmokeSnapshotBytes, "bounded smoke snapshot bytes")
	flags.StringVar(&options.compareLeft, "compare-left", "", "Linux or Windows JSON report")
	flags.StringVar(&options.compareRight, "compare-right", "", "other OS JSON report")
	flags.Float64Var(&options.regressionBeforeNS, "regression-before-ns", 0, "recorded before median in ns/op")
	flags.Float64Var(&options.regressionBeforeB, "regression-before-bytes", 0, "recorded before median in B/op")
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, 2
	}
	if code := validateCommandOptions(options, stderr); code != 0 {
		return commandOptions{}, code
	}
	return options, 0
}

func validateCommandOptions(options commandOptions, stderr io.Writer) int {
	if options.compareLeft != "" || options.compareRight != "" {
		if options.compareLeft == "" || options.compareRight == "" {
			if err := writeStatus(stderr, "-compare-left and -compare-right must be used together\n"); err != nil {
				return 1
			}
			return 2
		}
		return 0
	}
	if options.outputDir == "" {
		if err := writeStatus(stderr, "-output is required\n"); err != nil {
			return 1
		}
		return 2
	}
	if options.profile != "full" && options.profile != "smoke" {
		if err := writeStatus(stderr, "-profile must be full or smoke\n"); err != nil {
			return 1
		}
		return 2
	}
	label := perfharness.EvidenceLabel(options.evidenceLabel)
	if label != perfharness.EvidenceHardwareQualified && label != perfharness.EvidenceMinimumCertified {
		if err := writeStatus(stderr, "-evidence-label must be hardware-qualified or minimum-profile certified\n"); err != nil {
			return 1
		}
		return 2
	}
	return 0
}

func validateGoVersion(version string) error {
	value := strings.TrimPrefix(version, "devel ")
	const required = "go1.24"
	if !strings.HasPrefix(value, required) {
		return fmt.Errorf("unsupported Go version %q: require Go 1.24.x", version)
	}
	if len(value) == len(required) {
		return nil
	}
	switch value[len(required)] {
	case '.', '-', ' ':
		return nil
	default:
		return fmt.Errorf("unsupported Go version %q: require Go 1.24.x", version)
	}
}
