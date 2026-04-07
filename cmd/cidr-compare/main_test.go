package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRequiredFlags(t *testing.T) {
	// Build the binary first
	cmd := exec.Command("go", "build", "-o", "cidr-compare-test", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build: %s", string(output))
	}
	defer os.Remove("cidr-compare-test")

	// Run with no args - should exit with code 1 and show usage
	cmd = exec.Command("./cidr-compare-test")
	output, err = cmd.CombinedOutput()
	if err == nil {
		t.Error("expected error when running without flags")
	}
	if !strings.Contains(string(output), "deny-file") {
		t.Error("usage should mention deny-file flag")
	}
	if !strings.Contains(string(output), "open-file") {
		t.Error("usage should mention open-file flag")
	}
}

func TestEnvVarFallback(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", "cidr-compare-test", ".")
	output, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build: %s", string(output))
	}
	defer os.Remove("cidr-compare-test")

	// Create temp files
	denyContent := "dst_network_segment,decision\n10.0.0.0/8,deny\n"
	openContent := "segment,status\n10.1.2.3/32,open\n"

	denyFile := filepath.Join(t.TempDir(), "deny.csv")
	openFile := filepath.Join(t.TempDir(), "open.csv")

	os.WriteFile(denyFile, []byte(denyContent), 0644)
	os.WriteFile(openFile, []byte(openContent), 0644)

	// Set env vars
	os.Setenv("CIDR_COMPARE_DENY_FILE", denyFile)
	os.Setenv("CIDR_COMPARE_OPEN_FILE", openFile)
	defer func() {
		os.Unsetenv("CIDR_COMPARE_DENY_FILE")
		os.Unsetenv("CIDR_COMPARE_OPEN_FILE")
	}()

	// Run with no flags but env vars set
	runCmd := exec.Command("./cidr-compare-test")
	output, _ = runCmd.CombinedOutput()

	if !strings.Contains(string(output), "10.0.0.0/8,10.1.2.3/32") {
		t.Errorf("expected match output, got: %s", string(output))
	}
}
