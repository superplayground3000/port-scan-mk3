package scanapp

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// PrePingConfiguration supplies validated values to the pre-ping workflow.
type PrePingConfiguration interface {
	Resolve() (config.PrePingValues, error)
}

// RunPrePing runs the standalone pre-ping phase. It resolves the configuration
// before file, process, or network work. It loads the target input and pings
// each unique IP. It writes the fixed unreachable-results CSV schema to the
// output directory. It writes the resolved path to stdout for the next stage.
//
// RunPrePing builds no chunk, writes no snapshot, and scans no port. Its
// caller can supply a ReachabilityChecker through opts.ReachabilityChecker.
func RunPrePing(ctx context.Context, configuration PrePingConfiguration, stdout, stderr io.Writer, opts RunOptions) error {
	values, err := configuration.Resolve()
	if err != nil {
		return fmt.Errorf("resolve pre-ping configuration: %w", err)
	}
	expansion, err := resolveTargetExpansion(configuration)
	if err != nil {
		return err
	}
	resourceLimits, err := resolvePrePingLimits(configuration)
	if err != nil {
		return err
	}
	deps := defaultRunDependencies()

	if err := ctx.Err(); err != nil {
		return err
	}

	inputs, err := loadPrePingInputsContext(ctx, values.CIDRFile, values.CIDRIPCol, values.CIDRIPCidrCol, resourceLimits.CIDR, deps)
	if err != nil {
		return err
	}
	if _, err := task.EstimateAuthorizedCIDRRecords(inputs.cidrRecords, expansion.Limits, nil); err != nil {
		return err
	}

	now := time.Now()
	outputPaths, err := resolveRunOutputPaths(values.Output, deps, now)
	if err != nil {
		return err
	}

	plan, err := buildAuthorizedPreScanPlan(inputs)
	if err != nil {
		return err
	}
	uniqueIPs, err := plan.collectUniqueIPs()
	if err != nil {
		return err
	}

	timeout := values.PingTimeout
	reason := fmt.Sprintf("ping failed within %s", timeout)

	reporter := progress.New("pre-ping", len(uniqueIPs), values.ProgressInterval, stderr)
	unreachable, err := runReachabilityChecksWithProgress(
		ctx,
		resolvePrePingChecker(opts),
		uniqueIPs,
		values.Workers,
		timeout,
		reporter.Inc,
	)
	if err != nil {
		return err
	}
	reporter.Done()

	if err := ctx.Err(); err != nil {
		return err
	}

	if len(unreachable) == 0 {
		if err := finalizeUnreachableResults(outputPaths.unreachablePath, nil); err != nil {
			return err
		}
	} else if err := finalizeUnreachableResultsFromPlan(
		outputPaths.unreachablePath,
		plan,
		reachablePredicate(unreachable),
		reason,
	); err != nil {
		return err
	}

	fmt.Fprintln(stdout, outputPaths.unreachablePath)
	return nil
}

// resolvePrePingChecker selects the reachability checker for pre-ping. The
// pre-ping step exists to ping, so it always returns a checker: the injected one
// when present (tests) and otherwise the OS ping-command checker, which
// preserves the Windows deadline-kill classification and
// pingProcessStartupAllowance behavior. (Scan, by contrast, wires in no checker
// at all under decision B, so its "never pings" guarantee is structural.)
func resolvePrePingChecker(opts RunOptions) ReachabilityChecker {
	if opts.ReachabilityChecker != nil {
		return opts.ReachabilityChecker
	}
	return &commandReachabilityChecker{}
}

// finalizeUnreachableResults writes rows to a fresh unreachable_results CSV at
// finalPath and finalizes it (header-only when rows is empty). RunPrePing is its
// sole caller; it lives here (not scan.go) so the pre-ping path owns its writer.
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

func finalizeUnreachableResultsFromPlan(finalPath string, plan authorizedPreScanPlan, reachable func(string) bool, reason string) error {
	output, err := openUnreachableOutput(finalPath)
	if err != nil {
		return err
	}
	if err := plan.visitUnreachableRows(reachable, reason, output.writer.Write); err != nil {
		_ = output.Finalize(false)
		return err
	}
	return output.Finalize(true)
}
