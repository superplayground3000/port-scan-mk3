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
)

const (
	defaultPressureLimit = 60
)

// errScanRequiresResume is returned by Run when no bucket file is supplied.
// Decision B makes scan a pure scanner: it consumes a resume snapshot produced
// by generate-buckets and never builds fresh chunks or pings. There is
// deliberately no fresh-build fallback.
var errScanRequiresResume = errors.New("scan requires -resume <bucket file>; run generate-buckets first")

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

// RunOptions customizes runtime behaviors that the CLI does not expose as flags.
type RunOptions struct {
	Dial DialFunc
	// TaskObserver receives tasks in dispatcher order.
	// The observer must return quickly because it runs in the dispatcher.
	TaskObserver func(ip string, port int)
	// ResumeObserver receives completed resume chunks during runtime rebuild.
	ResumeObserver func(completed, total int)
	// ResultObserver receives the count after each persisted result row.
	ResultObserver      func(completed uint64)
	PressureLimit       int
	DisableKeyboard     bool
	PressureSource      PressureSource
	ProgressInterval    int
	ReachabilityChecker ReachabilityChecker

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
	pressureValues, err := values.Pressure.Resolve()
	if err != nil {
		return fmt.Errorf("resolve pressure policy: %w", err)
	}

	pressureSource := opts.PressureSource
	if pressureSource == nil && pressureValues.Kind != config.PressureKindDisabled {
		pressureSource, err = newPressureSource(values.Pressure)
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
	if openOutputs == nil {
		openOutputs = openBatchOutputs
	}

	logger := newLoggerWithQuiet(values.LogLevel, values.Format == "json", stderr, values.Quiet)
	runtime := newScanRuntime(scanRuntimeInput{
		values:                   values,
		targetExpansion:          targetExpansion,
		pressure:                 pressureValues,
		stdout:                   stdout,
		stderr:                   stderr,
		pressureLimit:            opts.PressureLimit,
		disableKeyboard:          opts.DisableKeyboard,
		progressInterval:         opts.ProgressInterval,
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
	})
	if err := runtime.execute(ctx); err != nil {
		return fmt.Errorf("execute scan runtime: %w", err)
	}
	return nil
}
