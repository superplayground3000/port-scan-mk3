package scanapp

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ReachabilityResult struct {
	IP          string
	Reachable   bool
	FailureText string
}

type ReachabilityChecker interface {
	Check(ctx context.Context, ip string, timeout time.Duration) ReachabilityResult
}

type detailedReachabilityChecker interface {
	CheckDetailed(ctx context.Context, ip string, timeout time.Duration) (ReachabilityResult, error)
}

type commandReachabilityChecker struct {
	goos   string
	runner commandRunner
}

type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func (c *commandReachabilityChecker) Check(ctx context.Context, ip string, timeout time.Duration) ReachabilityResult {
	result, _ := c.CheckDetailed(ctx, ip, timeout)
	return result
}

func (c *commandReachabilityChecker) CheckDetailed(ctx context.Context, ip string, timeout time.Duration) (ReachabilityResult, error) {
	result := ReachabilityResult{IP: ip}
	if strings.TrimSpace(ip) == "" {
		err := errors.New("ip is required")
		result.FailureText = err.Error()
		return result, err
	}

	name, args, err := buildPingCommand(c.goos, ip, timeout)
	if err != nil {
		result.FailureText = err.Error()
		return result, err
	}

	runner := c.runner
	if runner == nil {
		runner = execCommandRunner{}
	}

	runCtx, cancel := context.WithTimeout(ctx, pingProcessTimeout(c.goos, timeout))
	defer cancel()

	if err := runner.Run(runCtx, name, args...); err != nil {
		result.FailureText = err.Error()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}

		// The per-process ceiling (runCtx) fired: the host simply did not reply
		// in time, so it is unreachable, not a fatal error. Classify off runCtx
		// rather than the returned error, because on Windows the deadline kill
		// races the ping's own exit and surfaces as
		// "TerminateProcess: Access is denied" instead of context.DeadlineExceeded.
		if runCtx.Err() != nil {
			return result, nil
		}

		var exitErr *exec.ExitError
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &exitErr) {
			return result, nil
		}
		return result, err
	}

	result.Reachable = true
	return result, nil
}

func checkReachability(ctx context.Context, checker ReachabilityChecker, ip string, timeout time.Duration) (ReachabilityResult, error) {
	if checker == nil {
		return ReachabilityResult{IP: ip}, errors.New("reachability checker is required")
	}
	if detailed, ok := checker.(detailedReachabilityChecker); ok {
		return detailed.CheckDetailed(ctx, ip, timeout)
	}
	return checker.Check(ctx, ip, timeout), nil
}

// pingProcessStartupAllowance is extra wall-clock granted to the ping subprocess
// beyond its reply-wait timeout, to cover process creation, output, and teardown.
// It matters on Windows, where ping self-terminates its reply wait via -w, so the
// process still needs time to launch and exit after a fast reply arrives.
// The allowance is generous (10s) because under high fan-out (many workers,
// zero dispatch delay) Windows process creation is heavily contended, and a
// ping that is slow to *start* must not be killed and misreported as
// unreachable. The kill itself is now non-fatal (see CheckDetailed), so a large
// allowance only trades a bounded worst-case wait for fewer false negatives.
const pingProcessStartupAllowance = 10 * time.Second

// pingProcessTimeout returns the wall-clock ceiling for the whole ping process.
// On platforms whose ping command self-bounds the reply wait (Windows -w), the
// ceiling adds a startup allowance so a fast reply is not killed during process
// launch. On the -c path (Linux/others) the context is the only reply-wait bound,
// so the ceiling equals the reply-wait timeout to keep unreachable hosts fast.
func pingProcessTimeout(goos string, timeout time.Duration) time.Duration {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		if timeout < 0 {
			timeout = 0
		}
		return timeout + pingProcessStartupAllowance
	}
	return timeout
}

func buildPingCommand(goos, ip string, timeout time.Duration) (string, []string, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "", nil, errors.New("ip is required")
	}

	if goos == "" {
		goos = runtime.GOOS
	}

	if goos == "windows" {
		timeoutMS := timeout.Milliseconds()
		if timeoutMS < 0 {
			timeoutMS = 0
		}
		return "ping", []string{"-n", "1", "-w", strconv.FormatInt(timeoutMS, 10), ip}, nil
	}

	return "ping", []string{"-c", "1", ip}, nil
}
