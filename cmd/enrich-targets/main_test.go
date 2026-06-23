package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	err := runMain([]string{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing flags")
	}
}

func TestRunMain_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	inputPath := writeFile(t, dir, "input.csv", "host,port\n10.1.2.3,22\n10.5.6.7,80\n")
	cidrPath := writeFile(t, dir, "cidrs.csv", "cidr\n10.0.0.0/8\n10.1.0.0/16\n")
	svcPath := writeFile(t, dir, "services.csv", "port,service_label\n22,SSH\n80,HTTP\n")
	outPath := filepath.Join(dir, "output.csv")

	var stdout, stderr bytes.Buffer
	err := runMain([]string{
		"--input", inputPath,
		"--cidr-list", cidrPath,
		"--service-map", svcPath,
		"--output", outPath,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	output := string(out)

	// Should contain rich header
	if !strings.Contains(output, "src_ip") {
		t.Error("expected rich CSV header")
	}
	// 10.1.2.3 should get most specific CIDR 10.1.0.0/16
	if !strings.Contains(output, "10.1.0.0/16") {
		t.Error("expected most specific CIDR for 10.1.2.3")
	}
	// Should have enriched 2 rows
	if !strings.Contains(stderr.String(), "Enriched 2 rows") {
		t.Errorf("expected summary, got: %s", stderr.String())
	}
}
