package scanapp

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

const (
	defaultResumeStateFile = "resume_state.json"
	defaultPressureLimit   = 60
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
	Dial                DialFunc
	ResumeStatePath     string
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
		return err
	}
	pressureValues, err := values.Pressure.Resolve()
	if err != nil {
		return err
	}

	pressureSource := opts.PressureSource
	if pressureSource == nil && pressureValues.Kind != config.PressureKindDisabled {
		pressureSource, err = newPressureSource(values.Pressure)
		if err != nil {
			return err
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

	cfg := legacyScanConfig(values, pressureValues)
	logger := newLoggerWithQuiet(cfg.LogLevel, cfg.Format == "json", stderr, cfg.Quiet)
	runtime := newScanRuntime(scanRuntimeInput{
		cfg:                      cfg,
		stdout:                   stdout,
		stderr:                   stderr,
		resumeStatePath:          opts.ResumeStatePath,
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
	})
	return runtime.execute(ctx)
}

func legacyScanConfig(values config.ScanValues, pressureValues config.PressureValues) config.Config {
	return config.Config{
		CIDRFile:             values.CIDRFile,
		CIDRIPCol:            values.CIDRIPCol,
		CIDRIPCidrCol:        values.CIDRIPCidrCol,
		PortFile:             values.PortFile,
		Resume:               values.ResumeInput,
		Output:               values.Output,
		Workers:              values.Workers,
		Timeout:              values.DialTimeout,
		Delay:                values.DispatchDelay,
		BucketRate:           values.BucketRate,
		BucketCapacity:       values.BucketCapacity,
		LogLevel:             values.LogLevel,
		Format:               values.Format,
		Quiet:                values.Quiet,
		DisableAPI:           pressureValues.Kind == config.PressureKindDisabled,
		PressureAPI:          pressureValues.Endpoint,
		PressureInterval:     pressureValues.Interval,
		PressureAuthURL:      pressureValues.AuthEndpoint,
		PressureDataURLs:     append([]string(nil), pressureValues.DataEndpoints...),
		PressureClientID:     pressureValues.ClientID,
		PressureClientSecret: pressureValues.ClientSecret,
		PressureUseAuth:      pressureValues.Kind == config.PressureKindAuthenticated,
	}
}
