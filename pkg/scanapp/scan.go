package scanapp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

const (
	defaultResumeStateFile = "resume_state.json"
	defaultPressureLimit   = 60
)

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
}

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

	if err := ensureFDLimit(cfg.Workers); err != nil {
		return err
	}

	inputs, err := loadRunInputs(cfg, deps)
	if err != nil {
		return err
	}

	now := time.Now()
	outputPaths, err := resolveRunOutputPaths(cfg, deps, now)
	if err != nil {
		return err
	}

	resumeSnapshot, err := loadResumeSnapshot(cfg)
	if err != nil {
		return err
	}

	preScan, err := runPreScanPing(ctx, inputs, cfg, resolveReachabilityChecker(cfg, opts), resumeSnapshot.PreScanPing)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := finalizeUnreachableResults(outputPaths.unreachablePath, preScan.UnreachableRows); err != nil {
		return err
	}

	plan, err := prepareRuntimePlan(
		cfg,
		inputs,
		deps,
		runReachablePredicate(cfg, preScan),
		resumeSnapshot.Chunks,
		shouldUseResumeChunks(cfg, resumeSnapshot.PreScanPing, preScan),
	)
	if err != nil {
		return err
	}
	plan.outputPaths = outputPaths
	plan.scanOutputPath = outputPaths.scanPath
	plan.openOnlyPath = outputPaths.openPath

	outputs, err := openBatchOutputs(plan.scanOutputPath, plan.openOnlyPath)
	if err != nil {
		return err
	}
	var scanSuccess bool
	defer func() {
		_ = outputs.Finalize(scanSuccess)
	}()

	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	queueSize := workers * 2
	progressStep := opts.ProgressInterval
	if progressStep <= 0 {
		progressStep = 100
	}

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

	dispatchErrCh := make(chan error, 1)
	go func() {
		dispatchErrCh <- dispatchTasks(runCtx, dispatchPolicy, ctrl, logger, plan.runtimes, taskCh)
		close(taskCh)
	}()

	var (
		dispatchDone bool
		dispatchErr  error
		runErr       error
		summary      resultSummary
	)
	startedAt := time.Now()
	for !dispatchDone || resultCh != nil {
		select {
		case apiErr := <-apiErrCh:
			if apiErr != nil && runErr == nil {
				runErr = apiErr
				cancel()
			}
		case executorErr, ok := <-executorErrCh:
			if !ok {
				executorErrCh = nil
				continue
			}
			if executorErr != nil && runErr == nil {
				runErr = executorErr
				cancel()
			}
		case err := <-dispatchErrCh:
			dispatchDone = true
			dispatchErr = err
			dispatchErrCh = nil
		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			if runErr == nil {
				if err := writeScanRecord(outputs.scanWriter, outputs.openOnlyWriter, res.record.AsWriterRecord()); err != nil {
					runErr = err
					cancel()
				}
			}
			applyScanResult(plan.runtimes, res, &summary, resultObserver)
			if runErr == nil {
				emitScanResultEvents(stdout, logger, ctrl, progressStep, plan.runtimes, res, &summary, cfg.Quiet)
			}
		}
	}

	for _, rt := range plan.runtimes {
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}

	if err := persistResumeSnapshot(cfg, opts, logger, plan.runtimes, preScan.State, dispatchErr, runErr); err != nil {
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
	scanSuccess = true
	return nil
}

func resolveReachabilityChecker(cfg config.Config, opts RunOptions) ReachabilityChecker {
	if cfg.DisablePreScanPing {
		return nil
	}
	if opts.ReachabilityChecker != nil {
		return opts.ReachabilityChecker
	}
	return &commandReachabilityChecker{}
}

func finalizeUnreachableResults(finalPath string, rows []writer.UnreachableRecord) error {
	output, err := openUnreachableOutput(finalPath)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := output.writer.Write(row); err != nil {
			_ = output.Finalize(false)
			return err
		}
	}
	return output.Finalize(true)
}

func runReachablePredicate(cfg config.Config, preScan preScanOutcome) func(string) bool {
	if cfg.DisablePreScanPing {
		return nil
	}
	return reachablePredicate(preScan.UnreachableIPv4U32)
}

func shouldUseResumeChunks(cfg config.Config, saved state.PreScanPingState, preScan preScanOutcome) bool {
	if cfg.Resume == "" {
		return false
	}
	if cfg.DisablePreScanPing {
		return true
	}
	if hasSavedPreScanPingState(saved) {
		return true
	}
	return len(preScan.UnreachableIPv4U32) == 0
}
