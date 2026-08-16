package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIRequiredFlags(t *testing.T) {
	// Build the binary first
	bin := buildTestBinary(t)

	// Run with no args - should exit with code 1 and show usage
	cmd := exec.Command(bin)
	output, err := cmd.CombinedOutput()
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
	bin := buildTestBinary(t)

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
	runCmd := exec.Command(bin)
	output, err := runCmd.CombinedOutput()
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

// TestRunMain_InputDiagnosticReportsPathVerbatim proves that an input
// diagnostic carries the path the operator gave, character for character.
// A Windows path contains backslashes. A quoted rendering doubles them, so the
// reported path no longer matches the real file. On the other operating
// systems the test puts a backslash in the directory name. This runs the same
// case everywhere.
func TestRunMain_InputDiagnosticReportsPathVerbatim(t *testing.T) {
	const (
		validDeny   = "dst_network_segment,decision\n10.0.0.0/8,deny\n"
		validOpen   = "segment,status\n10.0.0.1/32,open\n"
		invalidDeny = "dst_network_segment,decision\nnot-a-cidr,deny\n"
		invalidOpen = "segment,status\nnot-a-cidr,open\n"
	)
	// The wrapped OS error repeats the path, so a missing-file case passes on a
	// bare path check even when the outer copy is quoted. Each case therefore
	// asserts the role and the path together, which only the outer copy gives.
	tests := []struct {
		name        string
		denyContent string
		openContent string
		removeDeny  bool
		removeOpen  bool
		wantPath    string
		wantPrefix  string
	}{
		{name: "open deny CSV", denyContent: validDeny, openContent: validOpen, removeDeny: true, wantPath: "deny", wantPrefix: "open deny CSV "},
		{name: "read deny CSV", denyContent: invalidDeny, openContent: validOpen, wantPath: "deny", wantPrefix: "read deny CSV "},
		{name: "open open CSV", denyContent: validDeny, openContent: validOpen, removeOpen: true, wantPath: "open", wantPrefix: "open open CSV "},
		{name: "read open CSV", denyContent: validDeny, openContent: invalidOpen, wantPath: "open", wantPrefix: "read open CSV "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if runtime.GOOS != "windows" {
				dir = filepath.Join(dir, `back\slash`)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("create backslash directory: %v", err)
				}
			}
			denyFile := filepath.Join(dir, "deny.csv")
			openFile := filepath.Join(dir, "open.csv")
			if err := os.WriteFile(denyFile, []byte(tt.denyContent), 0o644); err != nil {
				t.Fatalf("write deny input: %v", err)
			}
			if err := os.WriteFile(openFile, []byte(tt.openContent), 0o644); err != nil {
				t.Fatalf("write open input: %v", err)
			}
			if tt.removeDeny {
				if err := os.Remove(denyFile); err != nil {
					t.Fatalf("remove deny input: %v", err)
				}
			}
			if tt.removeOpen {
				if err := os.Remove(openFile); err != nil {
					t.Fatalf("remove open input: %v", err)
				}
			}

			err := runMain([]string{"-deny-file", denyFile, "-open-file", openFile}, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil {
				t.Fatal("expected an input error")
			}
			wantPath := denyFile
			if tt.wantPath == "open" {
				wantPath = openFile
			}
			if !strings.Contains(err.Error(), tt.wantPrefix+wantPath) {
				t.Errorf("diagnostic %q does not contain %q", err.Error(), tt.wantPrefix+wantPath)
			}
		})
	}
}

func TestCLIInvalidInputHasSingleDiagnostic(t *testing.T) {
	bin := buildTestBinary(t)
	tests := []struct {
		name        string
		denyContent string
		openContent string
		wantRole    string
	}{
		{
			name:        "deny input",
			denyContent: "dst_network_segment,decision\nnot-a-cidr,deny\n",
			openContent: "segment,status\n10.0.0.1/32,open\n",
			wantRole:    "deny",
		},
		{
			name:        "open input",
			denyContent: "dst_network_segment,decision\n10.0.0.0/8,deny\n",
			openContent: "segment,status\nnot-a-cidr,open\n",
			wantRole:    "open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			denyFile := filepath.Join(dir, "deny.csv")
			openFile := filepath.Join(dir, "open.csv")
			if err := os.WriteFile(denyFile, []byte(tt.denyContent), 0o644); err != nil {
				t.Fatalf("write deny input: %v", err)
			}
			if err := os.WriteFile(openFile, []byte(tt.openContent), 0o644); err != nil {
				t.Fatalf("write open input: %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd := exec.Command(bin, "-deny-file", denyFile, "-open-file", openFile)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			if err == nil {
				t.Fatal("expected exit status 1")
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("exit error = %v, want status 1", err)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty output", stdout.String())
			}

			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			if len(lines) != 1 || lines[0] == "" {
				t.Fatalf("stderr = %q, want one diagnostic line", stderr.String())
			}
			wantPath := denyFile
			if tt.wantRole == "open" {
				wantPath = openFile
			}
			for _, detail := range []string{tt.wantRole, wantPath, "record 2", "line 2", "column 1", "invalid CIDR"} {
				if !strings.Contains(lines[0], detail) {
					t.Errorf("stderr line %q does not contain %q", lines[0], detail)
				}
			}
		})
	}
}
