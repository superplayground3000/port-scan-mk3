package scanapp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type scanRuntimeInput struct {
	values                   config.ScanValues
	targetExpansion          config.TargetExpansionValues
	resourceLimits           config.ScanResourceLimits
	pressure                 config.PressureValues
	stdout                   io.Writer
	stderr                   io.Writer
	pressureLimit            int
	disableKeyboard          bool
	progressInterval         int
	outputFlushResults       int
	dashboardRefreshInterval time.Duration
}

type scanRuntimeAdapters struct {
	deps                      runDependencies
	logger                    *scanLogger
	dial                      DialFunc
	pressureSource            PressureSource
	dashboardTerminalDetector func(io.Writer) bool
	dashboardRenderer         dashboardRenderLoop
	pressureObserver          pressureTelemetryObserver
	controllerObserver        controllerTelemetryObserver
	batchOutputsOpener        batchOutputsOpenFunc
	taskObserver              func(ip string, port int)
	resumeObserver            func(completed, total int)
	resultObserver            func(completed uint64)
}

type scanRuntime struct {
	input    scanRuntimeInput
	adapters scanRuntimeAdapters
}

func newScanRuntime(input scanRuntimeInput, adapters scanRuntimeAdapters) *scanRuntime {
	return &scanRuntime{input: input, adapters: adapters}
}

func (r *scanRuntime) execute(ctx context.Context) error {
	cfg := r.input.values
	pressureValues := r.input.pressure
	logger := r.adapters.logger

	// Decision B: scan is a pure scanner. It requires a bucket file via -resume,
	// constructs no reachability checker, never pings, and never builds fresh
	// chunks. The "never pings" guarantee is structural — no checker is wired in.
	if strings.TrimSpace(cfg.ResumeInput) == "" {
		return errScanRequiresResume
	}

	if err := ensureFDLimit(cfg.Workers); err != nil {
		return err
	}

	inputs, err := loadRunInputsContext(ctx, inputConfiguration{
		cidrFile:         cfg.CIDRFile,
		cidrIPCol:        cfg.CIDRIPCol,
		cidrIPCidrCol:    cfg.CIDRIPCidrCol,
		portFile:         cfg.PortFile,
		allowMissingPort: true,
		cidrLimits:       r.input.resourceLimits.CIDR,
		portLimits:       r.input.resourceLimits.Port,
	}, r.adapters.deps)
	if err != nil {
		return err
	}

	snapshot, err := state.LoadSnapshotWithLimits(cfg.ResumeInput, r.input.resourceLimits.Snapshot)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateSnapshotAuthorization(snapshot, inputs.cidrRecords); err != nil {
		return err
	}
	if err := validateSnapshotTargetSemantics(snapshot, inputs.cidrRecords); err != nil {
		return err
	}
	if snapshot.TargetSemanticsVersion == state.CurrentTargetSemanticsVersion && !hasRichRecords(inputs.cidrRecords) {
		inputs.portSpecs, err = inputPortSpecsFromRows(snapshot.BasicPortFallback)
		if err != nil {
			return fmt.Errorf("load snapshot basic port fallback: %w", err)
		}
	}
	// A successful legacy check proves that the snapshot contains no denied
	// work. A later progress save can record this fact and skip the check.
	snapshot.RichDenyExcluded = true
	effectiveLimits, err := effectiveScanExpansionLimits(snapshot.TargetExpansion, r.input.targetExpansion)
	if err != nil {
		return err
	}
	expansionEstimate, err := task.EstimateAuthorizedCIDRRecords(inputs.cidrRecords, effectiveLimits, incompleteChunkKeys(snapshot.Chunks))
	if err != nil {
		return err
	}
	candidateCount := expansionEstimate.CandidateCount
	if snapshot.TargetExpansion != nil {
		candidateCount = snapshot.TargetExpansion.CandidateCount
	}
	snapshot.TargetExpansion = &state.TargetExpansionState{
		CandidateCount: candidateCount,
		CandidateLimit: int64(effectiveLimits.CandidateLimit()),
		MemoryLimitGB:  int64(effectiveLimits.MemoryLimitGB()),
	}
	// The snapshot blocklist supplies the reachable predicate. The scan does not
	// use ping to calculate reachability. An empty blocklist makes all targets
	// reachable.
	reachable := reachablePredicate(snapshot.PreScanPing.UnreachableIPv4U32)

	// Resolve output paths after the snapshot. A snapshot from an interrupted run
	// makes this run append to the same files (design §3.7). A new snapshot gets
	// new timestamped paths for a later resume.
	//
	// Resolve recorded paths to absolute paths first (issue #61). An older
	// snapshot can contain a path relative to the original working directory.
	// Another working directory can select a second set of files.
	// resolvePersistedOutputPaths defines the compatibility rule. The runtime
	// records resolved paths only when it saves a snapshot. A clean run does not
	// save a snapshot because no work remains.
	var (
		scanPath   string
		openPath   string
		appendMode bool
	)
	if snapshot.Output != nil {
		recorded, resolveErr := resolvePersistedOutputPaths(*snapshot.Output)
		if resolveErr != nil {
			return resolveErr
		}
		scanPath = recorded.ScanPath
		openPath = recorded.OpenPath
		appendMode = true
	} else {
		outputPaths, resolveErr := resolveRunOutputPaths(cfg.Output, r.adapters.deps, time.Now())
		if resolveErr != nil {
			return resolveErr
		}
		scanPath = outputPaths.scanPath
		openPath = outputPaths.openPath
	}
	outputState := &state.OutputState{ScanPath: scanPath, OpenPath: openPath}

	progressStep := r.input.progressInterval
	if progressStep <= 0 {
		progressStep = 100
	}

	// Phase 5 (design §3.8) logs the runtime rebuild. bucket_parse_start reports
	// the number of incomplete chunks. A pkg/progress reporter emits a throttled
	// bucket_parse_progress tick for each chunk. bucket_parse_complete reports
	// the totals and elapsed time. emitScanResultEvents keeps the result progress.
	incompleteChunks := countIncompleteChunks(snapshot.Chunks)
	logger.eventf("bucket_parse_start", "", 0, "bucket_parse_start", LogEventNone, map[string]any{
		"incomplete_chunks": incompleteChunks,
	})
	parseStart := time.Now()
	var parseReporter chunkExpandReporter
	var bucketProgress progress.Reporter
	if !cfg.Quiet || r.adapters.resumeObserver != nil {
		completed := 0
		bucketProgress = progress.New("bucket_parse_progress", incompleteChunks, progressStep, r.input.stderr)
		parseReporter = func() {
			completed++
			if !cfg.Quiet {
				bucketProgress.Inc()
			}
			if r.adapters.resumeObserver != nil {
				r.adapters.resumeObserver(completed, incompleteChunks)
			}
		}
	}

	plan, err := prepareRuntimePlanContext(ctx, runtimePolicy{
		bucketRate:     cfg.BucketRate,
		bucketCapacity: cfg.BucketCapacity,
	}, inputs, reachable, snapshot.Chunks, parseReporter)
	if err != nil {
		return err
	}

	if bucketProgress != nil && !cfg.Quiet {
		bucketProgress.Done()
	}
	targetsGenerated := 0
	for _, rt := range plan.runtimes {
		targetsGenerated += rt.targetCount()
	}
	logger.eventf("bucket_parse_complete", "", 0, "bucket_parse_complete", LogEventNone, map[string]any{
		"chunks_parsed":     incompleteChunks,
		"targets_generated": targetsGenerated,
		"elapsed_ms":        time.Since(parseStart).Milliseconds(),
	})

	outputs, err := r.adapters.batchOutputsOpener(scanPath, openPath, appendMode)
	if err != nil {
		return err
	}
	defer func() {
		_ = outputs.Finalize()
	}()

	workers := effectiveWorkerCount(cfg.Workers)
	queueSize := queueCapacityFor(workers)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		dashboardState *dashboardState
		resultObserver resultTelemetryObserver
	)
	if shouldEnableDashboard(cfg.Format, r.input.stderr, RunOptions{
		dashboardTerminalDetector: r.adapters.dashboardTerminalDetector,
	}) {
		dashboardState = newDashboardState(dashboardTotalTasks(plan.runtimes), time.Now)
		dashboardState.SetScannedTasks(dashboardScannedTasks(plan.runtimes))
		resultObserver = dashboardState
		dashboard := newDashboardRuntime(dashboardState, r.input.stderr, dashboardRuntimeOptions{
			refreshInterval: r.input.dashboardRefreshInterval,
			renderer:        r.adapters.dashboardRenderer,
			logger:          logger,
		})
		dashboard.Start(runCtx)
		defer dashboard.Stop()

		r.adapters.pressureObserver = appendPressureTelemetryObservers(r.adapters.pressureObserver, dashboardState)
		r.adapters.controllerObserver = appendControllerTelemetryObservers(r.adapters.controllerObserver, dashboardState)
	}

	pressureEnabled := pressureValues.Kind != config.PressureKindDisabled
	ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(pressureEnabled))
	startControllerTelemetrySync(runCtx, ctrl, controllerTelemetryInterval(r.input.dashboardRefreshInterval), r.adapters.controllerObserver)
	if !r.input.disableKeyboard {
		if err := speedctrl.StartKeyboardLoop(runCtx, ctrl); err != nil {
			logger.errorf("failed to start keyboard loop: %v", err)
		}
	}
	startManualPauseMonitor(runCtx, ctrl, logger)

	apiErrCh := make(chan error, 1)
	if pressureEnabled {
		go pollPressureAPI(runCtx, pressureValues.Interval, r.adapters.pressureSource, RunOptions{
			PressureLimit:    r.input.pressureLimit,
			pressureObserver: r.adapters.pressureObserver,
		}, ctrl, logger, apiErrCh)
	}

	taskCh := make(chan scanTask, queueSize)
	resultCh, executorErrCh, abandonedCh, executorTelemetry := startCancellableScanExecutor(runCtx, workers, cfg.DialTimeout, r.adapters.dial, logger, taskCh)

	dispatchPolicy := dispatchPolicy{delay: cfg.DispatchDelay, observer: noopDispatchObserver{}, taskObserver: r.adapters.taskObserver}
	if dashboardState != nil {
		dispatchPolicy.observer = newDashboardDispatchObserver(dashboardState)
	}

	// Phase 5 marks the transition from the runtime rebuild to dialing. The
	// dispatcher now puts tasks in the queue, and the workers dial the targets.
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
		abandonedCh:   abandonedCh,
	}, resultLoopDeps{
		outputs:                outputs,
		runtimes:               plan.runtimes,
		resultObserver:         resultObserver,
		stdout:                 r.input.stdout,
		logger:                 logger,
		ctrl:                   ctrl,
		progressStep:           progressStep,
		quiet:                  cfg.Quiet,
		outputFlushResults:     r.input.outputFlushResults,
		resultObserverCallback: r.adapters.resultObserver,
	})
	if inFlight, abandoned, stopStarted := executorTelemetry.snapshot(); !stopStarted.IsZero() {
		logger.eventf("probe_drain_complete", "", 0, "probe_drain_complete", LogEventNone, map[string]any{
			"duration_ms":            time.Since(stopStarted).Milliseconds(),
			"in_flight_probes":       inFlight,
			"abandoned_queued_tasks": abandoned,
		})
	}

	for _, rt := range plan.runtimes {
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}

	// A failure to save the snapshot is the run outcome. Therefore, it gets a
	// completion summary, as constitution VI requires.
	snapshotStartedAt := time.Now()
	rewoundChunks, snapshotErr := persistResumeSnapshotWithLimits(cfg.ResumeInput, logger, plan.runtimes, snapshot.PreScanPing, outputState, snapshot.RichDenyExcluded, snapshot.TargetExpansion, snapshot.TargetSemanticsVersion, snapshot.BasicPortFallback, r.input.resourceLimits.Snapshot, dispatchErr, runErr)
	logger.eventf("snapshot_save_complete", "", 0, "snapshot_save_complete", errorCause(snapshotErr), map[string]any{
		"duration_ms":    time.Since(snapshotStartedAt).Milliseconds(),
		"rewound_chunks": rewoundChunks,
	})
	if snapshotErr != nil {
		emitCompletionSummary(logger, summary, startedAt, snapshotErr)
		return snapshotErr
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
