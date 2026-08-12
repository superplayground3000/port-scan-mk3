package scanapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

const (
	defaultPressureLimit = 60
)

// errScanRequiresResume is returned by Run when no bucket file is supplied.
// Decision B makes scan a pure scanner: it consumes a resume snapshot produced
// by generate-buckets and never builds fresh chunks or pings. There is
// deliberately no fresh-build fallback.
var errScanRequiresResume = errors.New("scan requires -resume <bucket file>; run generate-buckets first")

// ErrInjectedOutputFailure identifies a requested result-writer failure.
var ErrInjectedOutputFailure = errors.New("injected output write failure")

// DialFunc abstracts TCP dialing for tests and runtime customization.
type DialFunc func(context.Context, string, string) (net.Conn, error)

// PressureSource samples router pressure for the scan monitor.
type PressureSource interface {
	// Sample returns one aggregate sample and its source results.
	// It returns an error when the sample is not complete.
	Sample(context.Context) (pressure.Sample, error)
}

// ScanConfiguration supplies validated values for one scan run.
type ScanConfiguration interface {
	// Resolve returns the validated values for one scan run.
	Resolve() (config.ScanValues, error)
}

// ProbeTelemetry records probes accepted by the executor around a stop.
type ProbeTelemetry struct {
	TotalStarted    uint64
	StartsAfterStop uint64
}

// SnapshotTelemetry records the corrected chunks saved after a stopped run.
type SnapshotTelemetry struct {
	RewoundChunks uint64
}

// OutputFailureInjection makes the real scan-results writer fail on one result.
// FailOnResult starts at one. Run rejects zero before it opens output files.
type OutputFailureInjection struct {
	FailOnResult uint64
}

// RunOptions customizes runtime behaviors that the CLI does not expose as flags.
type RunOptions struct {
	Dial DialFunc
	// TaskObserver receives tasks in dispatcher order.
	// The observer must return quickly because it runs in the dispatcher.
	TaskObserver func(ip string, port int)
	// ResumeObserver receives completed resume chunks during runtime rebuild.
	ResumeObserver func(completed, total int)
	// ResultObserver receives the count after each committed result.
	ResultObserver func(completed uint64)
	// ProbeTelemetryObserver receives the final accepted-probe counts. The
	// executor records the stop and the accepted count under one lock.
	ProbeTelemetryObserver    func(ProbeTelemetry)
	SnapshotTelemetryObserver func(SnapshotTelemetry)
	PressureLimit             int
	DisableKeyboard           bool
	PressureSource            PressureSource
	ProgressInterval          int
	ReachabilityChecker       ReachabilityChecker
	OutputFailure             *OutputFailureInjection
	SnapshotFailure           *state.SaveFailureInjection

	dashboardTerminalDetector func(io.Writer) bool
	dashboardRefreshInterval  time.Duration
	dashboardRenderer         dashboardRenderLoop
	pressureObserver          pressureTelemetryObserver
	controllerObserver        controllerTelemetryObserver
	// batchOutputsOpener constructs the scan/open-only result writers. It is a
	// test seam only (like dashboardRenderer above), letting a test wrap the real
	// writers with one that fails on the Nth record so the output-write failure
	// path is exercisable end-to-end. nil selects the production opener,
	// openBatchOutputs. It stays unexported so the package/CLI contract is
	// unchanged (constitution II).
	batchOutputsOpener batchOutputsOpenFunc
}

// batchOutputsOpenFunc has the signature of openBatchOutputs; see
// RunOptions.batchOutputsOpener.
type batchOutputsOpenFunc func(scanPath, openPath string, appendMode bool) (*batchOutputs, error)

// Run resolves the configuration and constructs runtime adapters before file
// or network work. The private runtime owns the scan lifecycle.
func Run(ctx context.Context, configuration ScanConfiguration, stdout, stderr io.Writer, opts RunOptions) error {
	values, err := configuration.Resolve()
	if err != nil {
		return fmt.Errorf("resolve scan configuration: %w", err)
	}
	targetExpansion, err := resolveTargetExpansion(configuration)
	if err != nil {
		return err
	}
	resourceLimits, err := resolveScanLimits(configuration)
	if err != nil {
		return err
	}
	pressureValues, err := values.Pressure.Resolve()
	if err != nil {
		return fmt.Errorf("resolve pressure policy: %w", err)
	}

	pressureSource := opts.PressureSource
	if pressureSource == nil && pressureValues.Kind != config.PressureKindDisabled {
		pressureSource, err = newPressureSource(values.Pressure, resourceLimits.Pressure)
		if err != nil {
			return fmt.Errorf("create pressure source: %w", err)
		}
	}

	dial := opts.Dial
	if dial == nil {
		dialer := &net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}
		dial = dialer.DialContext
	}

	openOutputs := opts.batchOutputsOpener
	if opts.OutputFailure != nil {
		if opts.OutputFailure.FailOnResult == 0 {
			return fmt.Errorf("output failure result must be positive")
		}
		if openOutputs != nil {
			return fmt.Errorf("output failure cannot be combined with a custom output opener")
		}
		openOutputs = outputFailureBatchOpener(opts.OutputFailure.FailOnResult)
	} else if openOutputs == nil {
		openOutputs = openBufferedBatchOutputs
	}

	logger := newLoggerWithQuiet(values.LogLevel, values.Format == "json", stderr, values.Quiet)
	saveSnapshot := state.SaveSnapshotWithLimits
	if opts.SnapshotFailure != nil {
		injection := *opts.SnapshotFailure
		saveSnapshot = func(path string, snapshot state.Snapshot, limits state.SnapshotLimits) error {
			return state.SaveSnapshotWithFailureInjection(path, snapshot, limits, injection)
		}
	}
	runtime := newScanRuntime(scanRuntimeInput{
		values:                   values,
		targetExpansion:          targetExpansion,
		resourceLimits:           resourceLimits,
		pressure:                 pressureValues,
		stdout:                   stdout,
		stderr:                   stderr,
		pressureLimit:            opts.PressureLimit,
		disableKeyboard:          opts.DisableKeyboard,
		progressInterval:         opts.ProgressInterval,
		outputFlushResults:       values.OutputFlushResults,
		dashboardRefreshInterval: opts.dashboardRefreshInterval,
	}, scanRuntimeAdapters{
		deps:                      defaultRunDependencies(),
		logger:                    logger,
		dial:                      dial,
		pressureSource:            pressureSource,
		dashboardTerminalDetector: opts.dashboardTerminalDetector,
		dashboardRenderer:         opts.dashboardRenderer,
		pressureObserver:          opts.pressureObserver,
		controllerObserver:        opts.controllerObserver,
		batchOutputsOpener:        openOutputs,
		taskObserver:              opts.TaskObserver,
		resumeObserver:            opts.ResumeObserver,
		resultObserver:            opts.ResultObserver,
		probeTelemetryObserver:    opts.ProbeTelemetryObserver,
		snapshotTelemetryObserver: opts.SnapshotTelemetryObserver,
		saveSnapshot:              saveSnapshot,
	})
	if err := runtime.execute(ctx); err != nil {
		return fmt.Errorf("execute scan runtime: %w", err)
	}
	return nil
}
