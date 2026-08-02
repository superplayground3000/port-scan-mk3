// Package ciguard holds the repository-level contract tests that keep the CI
// configuration honest. It is a TEST-ONLY package: it contains no production
// code and ships nothing in any binary.
//
// The gates this repository relies on live outside the Go build: a bash
// verification script, a GitHub Actions workflow, and — since issue #63 — a
// PowerShell script that performs the native-Windows validation
// (`scripts/windows_gate.ps1`). Nothing in `go build` or `go vet` reads those
// files, so they can silently drift: a sixth command added under `cmd/` would
// never be launched by the Windows gate, and a `continue-on-error:` line
// re-added to the Windows job would turn a red run green without any test
// noticing.
//
// The tests here close that hole by asserting the parts of those files that
// MUST stay true, on every platform, inside `make verify`. The few helpers they
// need (repo-root discovery and file reading) live in this file rather than in a
// production source file, so issue #63 adds no new production surface.
package ciguard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errRepoRootNotFound is returned by repoRoot when no ancestor directory of the
// starting point contains a go.mod file, i.e. the caller is not inside this
// module's checkout.
var errRepoRootNotFound = errors.New("ciguard: repository root not found (no go.mod in any parent directory)")

// repoRoot walks up from start until it finds the directory holding go.mod and
// returns that directory.
//
// # Parameters
//
//	start: any path inside the checkout; usually the test's working directory.
//
// # Returns
//
//	The absolute path of the module root, or an error wrapping
//	errRepoRootNotFound when start is outside a Go module.
func repoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("ciguard: resolve %q: %w", start, err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("ciguard: from %q: %w", start, errRepoRootNotFound)
		}
		dir = parent
	}
}

// readRepoFile reads a file addressed by its slash-separated path relative to
// the repository root, so callers never hardcode an OS-specific path.
//
// Line endings are normalized to "\n". A Windows checkout gets CRLF in text
// files (git's core.autocrlf is true on the GitHub windows-latest runner), so
// without this every line-exact assertion in this package would pass on Linux
// and fail on Windows — which is exactly what happened the first time the
// native Windows gate ran.
//
// # Parameters
//
//	start: any path inside the checkout (see repoRoot).
//	relSlashPath: the file's path relative to the repo root, using '/'.
//
// # Returns
//
//	The file contents with LF line endings, or a wrapped error if the root
//	cannot be located or the file cannot be read.
func readRepoFile(start, relSlashPath string) (string, error) {
	root, err := repoRoot(start)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(relSlashPath))
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("ciguard: read %s: %w", relSlashPath, err)
	}
	return normalizeNewlines(string(data)), nil
}

// normalizeNewlines converts CRLF and lone CR line endings to LF.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func TestRepoRoot_FindsTheDirectoryHoldingGoMod(t *testing.T) {
	root, err := repoRoot(".")
	if err != nil {
		t.Fatalf("repoRoot(.): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("repoRoot returned %q, which holds no go.mod: %v", root, statErr)
	}
	// The package lives two levels below the root; anything else means the walk
	// stopped in the wrong place.
	if got := filepath.Base(root); got != "port-scan-mk3" && !strings.Contains(root, string(filepath.Separator)) {
		t.Fatalf("repoRoot returned an implausible root: %q", root)
	}
}

func TestRepoRoot_WhenOutsideAModule_ReturnsErrRepoRootNotFound(t *testing.T) {
	// t.TempDir() is created under the OS temp root, which has no go.mod above
	// it in any supported CI environment.
	_, err := repoRoot(t.TempDir())
	if !errors.Is(err, errRepoRootNotFound) {
		t.Fatalf("expected errRepoRootNotFound outside a module, got: %v", err)
	}
}

func TestReadRepoFile_ReadsAPathRelativeToTheRepoRoot(t *testing.T) {
	got, err := readRepoFile(".", "go.mod")
	if err != nil {
		t.Fatalf("readRepoFile(go.mod): %v", err)
	}
	if !strings.Contains(got, "module github.com/xuxiping/port-scan-mk3") {
		t.Fatalf("go.mod does not look like this module's go.mod:\n%s", got)
	}
}

func TestReadRepoFile_WhenFileIsMissing_ReturnsAWrappedNotExistError(t *testing.T) {
	_, err := readRepoFile(".", "no/such/file-for-ciguard.txt")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a wrapped os.ErrNotExist, got: %v", err)
	}
}

func TestNormalizeNewlines_MakesAWindowsCheckoutReadLikeALinuxOne(t *testing.T) {
	cases := map[string]string{
		"  windows-build-test:\r\n    runs-on: windows-latest\r\n": "  windows-build-test:\n    runs-on: windows-latest\n",
		"already\nlf\n": "already\nlf\n",
		"old\rmac\r":    "old\nmac\n",
		"":              "",
	}
	for in, want := range cases {
		if got := normalizeNewlines(in); got != want {
			t.Fatalf("normalizeNewlines(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestReadRepoFile_OnThisHostsCheckout_ReturnsNoCarriageReturns is a cheap smoke
// check over the file the gate actually reads. It is deliberately NOT the proof
// that the normalization works: on a Linux checkout ci.yml already holds LF, so
// this test passes with or without it. The discriminating proof is
// TestReadRepoFile_WhenTheCheckoutHasCRLF_ReturnsLFOnly below, which forces the
// CRLF bytes itself. This one still earns its place on windows-latest, where a
// regression in the normalization makes it fail against the real checkout.
func TestReadRepoFile_OnThisHostsCheckout_ReturnsNoCarriageReturns(t *testing.T) {
	got, err := readRepoFile(".", ciWorkflow)
	if err != nil {
		t.Fatalf("readRepoFile(ci.yml): %v", err)
	}
	if strings.Contains(got, "\r") {
		t.Fatal("readRepoFile returned carriage returns; line-exact assertions would be platform-dependent")
	}
}

// crlfFixtureRepo materializes a throwaway module root whose files are written
// with real CRLF bytes, so readRepoFile is exercised against a genuine
// Windows-style checkout no matter how git checked THIS repository out.
//
// Reading a tracked repo file cannot prove the normalization works: on a Linux
// checkout those files already hold LF, so the assertion passes with or without
// the fix. The bytes therefore have to be forced here.
func crlfFixtureRepo(t *testing.T, relSlashPath, lfBody string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module ciguard.fixture\n"), 0o600); err != nil {
		t.Fatalf("write fixture go.mod: %v", err)
	}
	full := filepath.Join(root, filepath.FromSlash(relSlashPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	crlf := strings.ReplaceAll(lfBody, "\n", "\r\n")
	if !strings.Contains(crlf, "\r\n") {
		t.Fatalf("fixture body %q contains no line break; the CRLF test would be vacuous", lfBody)
	}
	if err := os.WriteFile(full, []byte(crlf), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", relSlashPath, err)
	}
	// Guard the guard: prove the bytes really landed as CRLF before asserting
	// anything about what readRepoFile does with them.
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read back fixture %s: %v", relSlashPath, err)
	}
	if !bytes.Contains(raw, []byte("\r\n")) {
		t.Fatalf("fixture %s was not written with CRLF bytes; the test would be vacuous", relSlashPath)
	}
	return root
}

// TestReadRepoFile_WhenTheCheckoutHasCRLF_ReturnsLFOnly is the discriminating
// regression test for the first red run of the native Windows gate. On
// windows-latest git checks text files out with CRLF, so ci.yml reads
// "  windows-build-test:\r" and every line-exact assertion in this package
// stops matching. Removing the normalization in readRepoFile must fail here.
func TestReadRepoFile_WhenTheCheckoutHasCRLF_ReturnsLFOnly(t *testing.T) {
	const lf = "  windows-build-test:\n    runs-on: windows-latest\n"
	root := crlfFixtureRepo(t, ciWorkflow, lf)

	got, err := readRepoFile(root, ciWorkflow)
	if err != nil {
		t.Fatalf("readRepoFile on a CRLF checkout: %v", err)
	}
	if strings.Contains(got, "\r") {
		t.Fatalf("readRepoFile returned carriage returns on a CRLF checkout: %q", got)
	}
	if got != lf {
		t.Fatalf("readRepoFile(CRLF checkout) = %q, want %q", got, lf)
	}
}

// TestReadRepoFile_WhenTheCheckoutHasLoneCR_ReturnsLFOnly covers the other
// legacy line ending, so the normalization cannot regress to a CRLF-only
// strings.ReplaceAll.
func TestReadRepoFile_WhenTheCheckoutHasLoneCR_ReturnsLFOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module ciguard.fixture\n"), 0o600); err != nil {
		t.Fatalf("write fixture go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("old\rmac\r"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got, err := readRepoFile(root, "old.txt")
	if err != nil {
		t.Fatalf("readRepoFile: %v", err)
	}
	if got != "old\nmac\n" {
		t.Fatalf("readRepoFile(lone CR) = %q, want %q", got, "old\nmac\n")
	}
}
