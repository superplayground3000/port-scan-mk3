package scanapp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

// RunOptions customizes runtime behaviors that are not exposed as CLI flags.
type RunOptions struct {
	Dial                DialFunc
	ResumeStatePath     string
	PressureLimit       int
	DisableKeyboard     bool
	PressureHTTP        *http.Client
	PressureFetcher     PressureFetcher
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

// Run executes a full scan flow: load inputs, dispatch scan tasks, write batch
// outputs, and persist resume state on interruption/failure.
func Run(ctx context.Context, cfg config.Config, stdout, stderr io.Writer, opts RunOptions) error {
	deps := defaultRunDependencies()
	logger := newLoggerWithQuiet(cfg.LogLevel, cfg.Format == "json", stderr, cfg.Quiet)
	if strings.TrimSpace(cfg.CIDRIPCol) == "" {
		cfg.CIDRIPCol = "ip"
	}
	if strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
		cfg.CIDRIPCidrCol = "ip_cidr"
	}

	// Decision B: scan is a pure scanner. It requires a bucket file via -resume,
	// constructs no reachability checker, never pings, and never builds fresh
	// chunks. The "never pings" guarantee is structural — no checker is wired in.
	if strings.TrimSpace(cfg.Resume) == "" {
		return errScanRequiresResume
	}

	if err := ensureFDLimit(cfg.Workers); err != nil {
		return err
	}

	inputs, err := loadRunInputs(cfg, deps)
	if err != nil {
		return err
	}

	snapshot, err := loadResumeSnapshot(cfg)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Resolve output paths AFTER the snapshot: a snapshot that already recorded an
	// output path (a prior interrupted run) makes this run APPEND to the same
	// files (design §3.7); otherwise this is the first scan of the bucket and we
	// mint fresh timestamped paths and record them so the next -resume appends.
	var (
		scanPath   string
		openPath   string
		appendMode bool
	)
	if snapshot.Output != nil {
		scanPath = snapshot.Output.ScanPath
		openPath = snapshot.Output.OpenPath
		appendMode = true
	} else {
		outputPaths, resolveErr := resolveRunOutputPaths(cfg, deps, time.Now())
		if resolveErr != nil {
			return resolveErr
		}
		scanPath = outputPaths.scanPath
		openPath = outputPaths.openPath
	}
	outputState := &state.OutputState{ScanPath: scanPath, OpenPath: openPath}

	// The reachable predicate is derived directly from the snapshot blocklist the
	// generate-buckets step recorded; scan does not re-derive reachability by
	// pinging. An empty blocklist yields an all-reachable predicate.
	reachable := reachablePredicate(snapshot.PreScanPing.UnreachableIPv4U32)

	progressStep := opts.ProgressInterval
	if progressStep <= 0 {
		progressStep = 100
	}

	// Phase 5 (design §3.8): log the pre-scan runtime rebuild. bucket_parse_start
	// announces how many incomplete chunks will be expanded; a pkg/progress
	// reporter emits throttled bucket_parse_progress ticks (one per incomplete
	// chunk built, throttled by ProgressInterval); bucket_parse_complete reports
	// the totals and elapsed time. The per-result scan progress
	// (emitScanResultEvents) is untouched.
	incompleteChunks := countIncompleteChunks(snapshot.Chunks)
	logger.eventf("bucket_parse_start", "", 0, "bucket_parse_start", LogEventNone, map[string]any{
		"incomplete_chunks": incompleteChunks,
	})
	parseStart := time.Now()
	var parseReporter chunkExpandReporter
	var bucketProgress progress.Reporter
	if !cfg.Quiet {
		bucketProgress = progress.New("bucket_parse_progress", incompleteChunks, progressStep, stderr)
		parseReporter = bucketProgress.Inc
	}

	plan, err := prepareRuntimePlan(cfg, inputs, deps, reachable, snapshot.Chunks, true, parseReporter)
	if err != nil {
		return err
	}
	plan.scanOutputPath = scanPath
	plan.openOnlyPath = openPath

	if bucketProgress != nil {
		bucketProgress.Done()
	}
	targetsGenerated := 0
	for _, rt := range plan.runtimes {
		targetsGenerated += len(rt.targets)
	}
	logger.eventf("bucket_parse_complete", "", 0, "bucket_parse_complete", LogEventNone, map[string]any{
		"chunks_parsed":     incompleteChunks,
		"targets_generated": targetsGenerated,
		"elapsed_ms":        time.Since(parseStart).Milliseconds(),
	})

	openOutputs := opts.batchOutputsOpener
	if openOutputs == nil {
		openOutputs = openBatchOutputs
	}
	outputs, err := openOutputs(scanPath, openPath, appendMode)
	if err != nil {
		return err
	}
	defer func() {
		_ = outputs.Finalize()
	}()

	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	queueSize := workers * 2

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	runOpts := opts
	var (
		dashboardState *dashboardState
		resultObserver resultTelemetryObserver
	)
	if shouldEnableDashboard(cfg, stderr, opts) {
		dashboardState = newDashboardState(dashboardTotalTasks(plan.runtimes), time.Now)
		dashboardState.SetScannedTasks(dashboardScannedTasks(plan.runtimes))
		resultObserver = dashboardState
		dashboard := newDashboardRuntime(dashboardState, stderr, dashboardRuntimeOptions{
			refreshInterval: opts.dashboardRefreshInterval,
			renderer:        opts.dashboardRenderer,
			logger:          logger,
		})
		dashboard.Start(runCtx)
		defer dashboard.Stop()

		runOpts.pressureObserver = appendPressureTelemetryObservers(runOpts.pressureObserver, dashboardState)
		runOpts.controllerObserver = appendControllerTelemetryObservers(runOpts.controllerObserver, dashboardState)
	}

	ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(!cfg.DisableAPI))
	startControllerTelemetrySync(runCtx, ctrl, controllerTelemetryInterval(runOpts.dashboardRefreshInterval), runOpts.controllerObserver)
	if !opts.DisableKeyboard {
		if err := speedctrl.StartKeyboardLoop(runCtx, ctrl); err != nil {
			logger.errorf("failed to start keyboard loop: %v", err)
		}
	}
	startManualPauseMonitor(runCtx, ctrl, logger)

	apiErrCh := make(chan error, 1)
	if !cfg.DisableAPI {
		go pollPressureAPI(runCtx, cfg, runOpts, ctrl, logger, apiErrCh)
	}

	taskCh := make(chan scanTask, queueSize)

	dial := opts.Dial
	if dial == nil {
		dialer := &net.Dialer{LocalAddr: &net.TCPAddr{Port: 0}}
		dial = dialer.DialContext
	}
	resultCh, executorErrCh := startScanExecutor(workers, cfg.Timeout, dial, logger, taskCh)

	dispatchPolicy := dispatchPolicyFromConfig(cfg)
	if dashboardState != nil {
		dispatchPolicy.observer = newDashboardDispatchObserver(dashboardState)
	}

	// Phase 5: a single line marking the transition from the (now-complete)
	// pre-scan rebuild to dialing. Everything above was preparation; from here
	// the dispatcher enqueues tasks and workers dial.
	logger.eventf("scan_start", "", 0, "scan_start", LogEventNone, map[string]any{
		"workers": workers,
		"chunks":  len(plan.runtimes),
	})

	dispatchErrCh := make(chan error, 1)
	go func() {
		dispatchErrCh <- dispatchTasks(runCtx, dispatchPolicy, ctrl, logger, plan.runtimes, taskCh)
		close(taskCh)
	}()

	startedAt := time.Now()
	summary, dispatchErr, runErr := runResultLoop(cancel, false, resultLoopChannels{
		apiErrCh:      apiErrCh,
		executorErrCh: executorErrCh,
		dispatchErrCh: dispatchErrCh,
		resultCh:      resultCh,
	}, resultLoopDeps{
		outputs:        outputs,
		runtimes:       plan.runtimes,
		resultObserver: resultObserver,
		stdout:         stdout,
		logger:         logger,
		ctrl:           ctrl,
		progressStep:   progressStep,
		quiet:          cfg.Quiet,
	})

	for _, rt := range plan.runtimes {
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}

	// A snapshot failure (including the deliberate refusal to save after an
	// output-write failure) is the run's outcome, so it still gets a completion
	// summary — constitution VI requires every long-running scan to emit one.
	if err := persistResumeSnapshot(cfg, opts, logger, plan.runtimes, snapshot.PreScanPing, outputState, dispatchErr, runErr); err != nil {
		emitCompletionSummary(logger, summary, startedAt, err)
		return err
	}

	if runErr != nil {
		emitCompletionSummary(logger, summary, startedAt, runErr)
		return runErr
	}
	if dispatchErr != nil {
		emitCompletionSummary(logger, summary, startedAt, dispatchErr)
		return dispatchErr
	}
	emitCompletionSummary(logger, summary, startedAt, nil)
	return nil
}
