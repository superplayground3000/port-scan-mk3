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

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := runner.Run(runCtx, name, args...); err != nil {
		result.FailureText = err.Error()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
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
