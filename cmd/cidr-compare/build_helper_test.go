package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildTestBinary compiles the cidr-compare command into a temporary directory
// and returns the path to the resulting executable. It appends ".exe" on
// Windows so the binary can be executed by name on every platform, and relies
// on t.TempDir() for automatic cleanup.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	name := "cidr-compare-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build: %s", string(out))
	}
	return bin
}
