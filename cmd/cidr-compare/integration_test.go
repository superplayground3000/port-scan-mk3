package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEndToEnd(t *testing.T) {
	// Build the binary first
	cmd := exec.Command("go", "build", "-o", "cidr-compare-test", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build: %s", string(output))
	}
	defer os.Remove("cidr-compare-test")

	// Create temp deny file
	denyContent := "dst_network_segment,decision\n10.0.0.0/8,deny\n192.168.0.0/16,deny\n"
	denyFile := filepath.Join(t.TempDir(), "deny.csv")
	if err := os.WriteFile(denyFile, []byte(denyContent), 0644); err != nil {
		t.Fatalf("failed to write deny file: %v", err)
	}

	// Create temp open file
	openContent := "segment,status\n10.1.2.3/32,open\n192.168.1.1/32,open\n172.16.0.1/32,open\n"
	openFile := filepath.Join(t.TempDir(), "open.csv")
	if err := os.WriteFile(openFile, []byte(openContent), 0644); err != nil {
		t.Fatalf("failed to write open file: %v", err)
	}

	cmd = exec.Command("./cidr-compare-test", "-deny-file="+denyFile, "-open-file="+openFile)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v, output: %s", err, string(output))
	}

	// Should find 10.1.2.3/32 inside 10.0.0.0/8 and 192.168.1.1/32 inside 192.168.0.0/16
	// 172.16.0.1/32 should not match
	outputStr := string(output)
	if !strings.Contains(outputStr, "10.0.0.0/8,10.1.2.3/32") {
		t.Error("missing match for 10.0.0.0/8 containing 10.1.2.3/32")
	}
	if !strings.Contains(outputStr, "192.168.0.0/16,192.168.1.1/32") {
		t.Error("missing match for 192.168.0.0/16 containing 192.168.1.1/32")
	}
	if strings.Contains(outputStr, "172.16.0.1/32") {
		t.Error("172.16.0.1/32 should not match any deny range")
	}
}
