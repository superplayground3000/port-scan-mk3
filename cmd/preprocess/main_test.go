package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return p
}

func TestRunMain_MissingFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runMain([]string{}, &stdout, &stderr, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for missing flags")
	}
}

func TestRunMain_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	inputCSV := fmt.Sprintf("%s\n%s\n%s\n",
		strings.Join(preprocesscfg.RichHeader(), ","),
		"10.59.42.39,10.59.42.39/32,10.1.2.3,10.1.0.0/16,SSH,tcp,22,accept,enriched,MATCH_POLICY_ACCEPT",
		"10.59.42.39,10.59.42.39/32,192.168.1.1,192.168.1.0/24,HTTP,tcp,80,accept,enriched,MATCH_POLICY_ACCEPT",
	)
	inputPath := writeFile(t, dir, "input.csv", inputCSV)
	cidrsPath := writeFile(t, dir, "cidrs.csv", "fab,segment,status\ndc-east,10.0.0.0/8,close\ndc-east,192.168.0.0/16,open\n")
	outDir := filepath.Join(dir, "output")

	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	var stdout, stderr bytes.Buffer
	err := runMain([]string{
		"--input", inputPath,
		"--cleaned-cidrs", cidrsPath,
		"--fab-name", "dc-east",
		"--output-dir", outDir,
	}, &stdout, &stderr, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outPath := filepath.Join(outDir, "dc-east", "20260416T153000Z", "input.csv")
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(out)

	// 10.1.2.3 in 10.1.0.0/16 inside closed 10.0.0.0/8 → dropped
	if strings.Contains(output, "10.1.2.3") {
		t.Error("10.1.2.3 should have been filtered out")
	}
	// 192.168.1.1 in open 192.168.0.0/16 → kept
	if !strings.Contains(output, "192.168.1.1") {
		t.Error("192.168.1.1 should have been kept")
	}
	// Summary should show 1 kept, 1 dropped
	if !strings.Contains(stderr.String(), "Rows kept:         1") {
		t.Errorf("expected 1 kept in summary, got: %s", stderr.String())
	}
}
