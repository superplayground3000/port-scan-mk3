package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

const (
	gracefulInterruptProcessHelper = "PORT_SCAN_GRACEFUL_INTERRUPT_PROCESS_HELPER"
	gracefulInterruptProcessDir    = "PORT_SCAN_GRACEFUL_INTERRUPT_PROCESS_DIR"
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

func gracefulInterruptScanArgs(dir string) []string {
	return []string{
		"-cidr-file", filepath.Join(dir, "cidr.csv"),
		"-port-file", filepath.Join(dir, "ports.csv"),
		"-resume", filepath.Join(dir, "buckets.json"),
		"-output", filepath.Join(dir, "scan_results.csv"),
		"-output-flush-results", "1",
		"-workers", "1",
		"-delay", "20ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}
}

func waitForScanResultRow(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		matches, globErr := filepath.Glob(filepath.Join(dir, "scan_results-*.csv"))
		if globErr != nil {
			t.Fatalf("find the scan output: %v", globErr)
		}
		for _, match := range matches {
			data, readErr := os.ReadFile(match)
			if readErr != nil {
				continue
			}
			if len(bytes.Split(bytes.TrimSpace(data), []byte("\n"))) >= 2 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the scan child wrote no result row")
}
