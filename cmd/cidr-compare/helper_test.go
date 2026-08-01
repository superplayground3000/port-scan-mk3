package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// testBinaryName returns the file name to build the cidr-compare test binary
// as, for the given GOOS. Windows decides executability by file extension
// (PATHEXT), so an extension-less file cannot be launched there at all.
func testBinaryName(goos string) string {
	name := "cidr-compare-test"
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// buildTestBinary compiles the cidr-compare command into a per-test temporary
// directory and returns the absolute path to the resulting executable.
//
// Callers invoke it as exec.Command(bin, args...) with no "./" prefix: the
// path is already absolute, and "./" is not a Windows convention. Building
// into t.TempDir() keeps the package directory clean and removes the race
// where concurrent tests overwrote a single shared binary file name.
func buildTestBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), testBinaryName(runtime.GOOS))
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build test binary: %v\n%s", err, out)
	}
	return bin
}

func TestTestBinaryName(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want string
	}{
		{name: "windows gets .exe suffix", goos: "windows", want: "cidr-compare-test.exe"},
		{name: "linux has no suffix", goos: "linux", want: "cidr-compare-test"},
		{name: "darwin has no suffix", goos: "darwin", want: "cidr-compare-test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testBinaryName(tt.goos); got != tt.want {
				t.Errorf("testBinaryName(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}
