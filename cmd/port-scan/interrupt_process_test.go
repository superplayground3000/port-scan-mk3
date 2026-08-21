package main

import (
	"os/exec"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func waitForForcedInterruptExit(t *testing.T, child *exec.Cmd) error {
	t.Helper()
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- child.Wait()
	}()

	limit := perfharness.DefaultContract().ForceWithin
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-waitResult:
		return err
	case <-timer.C:
		t.Fatalf("child did not exit within %s after the second interrupt", limit)
		return nil
	}
}

// waitForGracefulInterruptExit waits for a child that stops on its own after one
// interrupt. The limit is a hang guard, not a performance bound: the graceful
// stop budget is measured by the performance gate on certified hardware, so this
// case must not enforce a duration on a shared runner.
func waitForGracefulInterruptExit(t *testing.T, child *exec.Cmd) error {
	t.Helper()
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- child.Wait()
	}()

	const limit = 60 * time.Second
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case err := <-waitResult:
		return err
	case <-timer.C:
		t.Fatalf("child did not stop within %s after one interrupt", limit)
		return nil
	}
}
