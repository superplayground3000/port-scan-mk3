package scanapp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// RunPreping runs the standalone pre-scan ping phase. It loads the target
// inputs. It pings every unique target IP concurrently (cfg.Workers,
// cfg.PreScanPingTimeout). It reports percentage progress over the unique-IP
// count to stderr. It writes the fixed-schema unreachable_results-<ts>.csv into
// the -output directory. It then prints the resolved output path to stdout, so
// a caller can chain the file into generate-buckets.
//
// RunPreping builds no chunk, writes no snapshot, and scans no port. Its
// signature is the same as the signature of Run, so the CLI wiring can dispatch
// either entry point in the same way. A test injects a ReachabilityChecker
// through opts.ReachabilityChecker.
func RunPreping(ctx context.Context, cfg config.Config, stdout, stderr io.Writer, opts RunOptions) error {
	deps := defaultRunDependencies()
	if strings.TrimSpace(cfg.CIDRIPCol) == "" {
		cfg.CIDRIPCol = "ip"
	}
	if strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
		cfg.CIDRIPCidrCol = "ip_cidr"
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	inputs, err := loadPrepingInputs(cfg, deps)
	if err != nil {
		return err
	}

	now := time.Now()
	outputPaths, err := resolveRunOutputPaths(cfg, deps, now)
	if err != nil {
		return err
	}

	uniqueIPs, err := collectUniquePreScanIPs(inputs)
	if err != nil {
		return err
	}

	timeout := cfg.PreScanPingTimeout
	reason := fmt.Sprintf("ping failed within %s", timeout)

	reporter := progress.New("preping", len(uniqueIPs), cfg.ProgressInterval, stderr)
	unreachable, err := runReachabilityChecksWithProgress(
		ctx,
		resolvePrepingChecker(opts),
		uniqueIPs,
		cfg.Workers,
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

	rows, err := collectUnreachableRows(inputs, reachablePredicate(unreachable), reason)
	if err != nil {
		return err
	}

	if err := finalizeUnreachableResults(outputPaths.unreachablePath, rows); err != nil {
		return err
	}

	fmt.Fprintln(stdout, outputPaths.unreachablePath)
	return nil
}

// resolvePrepingChecker selects the reachability checker for preping. Preping's
// whole purpose is to ping, so it always returns a checker: the injected one
// when present (tests) and otherwise the OS ping-command checker, which
// preserves the Windows deadline-kill classification and
// pingProcessStartupAllowance behavior. (Scan, by contrast, wires in no checker
// at all under decision B, so its "never pings" guarantee is structural.)
func resolvePrepingChecker(opts RunOptions) ReachabilityChecker {
	if opts.ReachabilityChecker != nil {
		return opts.ReachabilityChecker
	}
	return &commandReachabilityChecker{}
}

// finalizeUnreachableResults writes rows to a fresh unreachable_results CSV at
// finalPath and finalizes it (header-only when rows is empty). RunPreping is its
// sole caller; it lives here (not scan.go) so the preping path owns its writer.
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
