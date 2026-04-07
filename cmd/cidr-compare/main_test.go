package main

import (
	"bytes"
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
	output, err = runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v, output: %s", err, string(output))
	}

	if !strings.Contains(string(output), "10.0.0.0/8,10.1.2.3/32") {
		t.Errorf("expected match output, got: %s", string(output))
	}
}

func TestRunMain(t *testing.T) {
	// Create temp CSV files
	denyDir := t.TempDir()
	denyFile := filepath.Join(denyDir, "deny.csv")
	openDir := t.TempDir()
	openFile := filepath.Join(openDir, "open.csv")

	// Write deny CSV - uses correct headers
	denyContent := "dst_network_segment,decision\n10.0.0.0/8,deny\n192.168.1.0/24,deny\n"
	if err := os.WriteFile(denyFile, []byte(denyContent), 0644); err != nil {
		t.Fatalf("failed to write deny file: %v", err)
	}

	// Write open CSV - uses correct headers
	openContent := "segment,status\n10.1.2.3/32,open\n192.168.1.100/32,open\n172.16.0.1/32,open\n"
	if err := os.WriteFile(openFile, []byte(openContent), 0644); err != nil {
		t.Fatalf("failed to write open file: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runMain([]string{"-deny-file", denyFile, "-open-file", openFile}, stdout, stderr)
	if err != nil {
		t.Fatalf("runMain() returned error: %v", err)
	}

	output := stdout.String()
	if output == "" {
		t.Fatal("expected output, got empty string")
	}

	// Verify header
	if !strings.Contains(output, "deny_cidr,open_cidr") {
		t.Errorf("output missing header, got: %s", output)
	}

	// Verify matches are present
	if !strings.Contains(output, "10.0.0.0/8,10.1.2.3/32") {
		t.Errorf("expected 10.0.0.0/8 match for 10.1.2.3/32, got: %s", output)
	}
	if !strings.Contains(output, "192.168.1.0/24,192.168.1.100/32") {
		t.Errorf("expected 192.168.1.0/24 match for 192.168.1.100/32, got: %s", output)
	}
}

func TestRunMain_MissingFlags(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runMain([]string{}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for missing flags")
	}
}

func TestRunMain_InvalidDenyFile(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := runMain([]string{"-deny-file", "/nonexistent/deny.csv", "-open-file", "/nonexistent/open.csv"}, stdout, stderr)
	if err == nil {
		t.Fatal("expected error for invalid deny file")
	}
}
