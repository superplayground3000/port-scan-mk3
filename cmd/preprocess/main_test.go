package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
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

// entryNames lists the immediate children of dir, so a test can prove a failed
// run created nothing new outside the directory it was pointed at.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestRunMain_UnsafeFabName_RejectedBeforeWritingAnything is the containment
// test for #67. A fab name carrying a traversal or a Windows-hostile shape must
// be refused with an error naming the flag, and — the part that actually
// matters — must not create anything beside the output directory.
//
// The assertion is deliberately not "the output path is where I computed it
// should be": it snapshots the parent of --output-dir before the run and
// requires the listing to be unchanged afterwards. That cannot pass by
// construction, because it never recomputes the path the way production does.
func TestRunMain_UnsafeFabName_RejectedBeforeWritingAnything(t *testing.T) {
	unsafeNames := []struct {
		name string
		fab  string
	}{
		{name: "parent traversal", fab: filepath.Join("..", "escape")},
		{name: "parent traversal slash", fab: "../escape"},
		{name: "nested traversal", fab: "sub/../../escape"},
		{name: "reserved device", fab: "con"},
		{name: "reserved device with extension", fab: "NUL.csv"},
		{name: "reserved device with padded stem", fab: "con .txt"},
		{name: "invalid character", fab: "fab?1"},
		{name: "trailing dot", fab: "fab."},
		{name: "trailing space", fab: "fab "},
		{name: "separator", fab: "fab/sub"},
		{name: "absolute", fab: "/escape"},
	}

	for _, tt := range unsafeNames {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := writeFile(t, dir, "input.csv", strings.Join(preprocesscfg.RichHeader(), ",")+"\n")
			cidrsPath := writeFile(t, dir, "cidrs.csv", "fab,segment,status\ndc-east,10.0.0.0/8,close\n")
			outDir := filepath.Join(dir, "output")

			before := entryNames(t, dir)

			var stdout, stderr bytes.Buffer
			err := runMain([]string{
				"--input", inputPath,
				"--cleaned-cidrs", cidrsPath,
				"--fab-name", tt.fab,
				"--output-dir", outDir,
			}, &stdout, &stderr, time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC))

			if err == nil {
				t.Fatalf("runMain with --fab-name %q returned nil, want a rejection", tt.fab)
			}
			if !strings.Contains(err.Error(), "--fab-name") {
				t.Errorf("error %q does not name the offending flag --fab-name", err)
			}

			after := entryNames(t, dir)
			if !slices.Equal(before, after) {
				t.Errorf("run with --fab-name %q changed the contents of %s: before %v, after %v",
					tt.fab, dir, before, after)
			}
		})
	}
}

func TestRunMain_ReservedFabNameRejectedBeforeInputOrOutputIO(t *testing.T) {
	reservedNames := []string{
		"COM¹",
		"lPt².csv",
		"LPT³ .log",
		"CONIN$",
		"cOnOuT$.txt",
		"CONIN$ .csv",
	}

	for _, fab := range reservedNames {
		t.Run(fab, func(t *testing.T) {
			dir := t.TempDir()
			before := entryNames(t, dir)

			var stdout, stderr bytes.Buffer
			err := runMain([]string{
				"--input", filepath.Join(dir, "missing-input.csv"),
				"--cleaned-cidrs", filepath.Join(dir, "missing-cidrs.csv"),
				"--fab-name", fab,
				"--output-dir", filepath.Join(dir, "output"),
			}, &stdout, &stderr, time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC))

			if err == nil {
				t.Fatalf("runMain with --fab-name %q returned nil, want a rejection", fab)
			}
			if !strings.HasPrefix(err.Error(), "--fab-name:") {
				t.Errorf("error %q does not identify --fab-name before an input error", err)
			}

			after := entryNames(t, dir)
			if !slices.Equal(before, after) {
				t.Errorf("run with --fab-name %q changed %s: before %v, after %v", fab, dir, before, after)
			}
		})
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
