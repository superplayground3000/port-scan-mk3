package ciguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoRoot_FindsTheDirectoryHoldingGoMod(t *testing.T) {
	root, err := RepoRoot(".")
	if err != nil {
		t.Fatalf("RepoRoot(.): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("RepoRoot returned %q, which holds no go.mod: %v", root, statErr)
	}
	// The package lives two levels below the root; anything else means the walk
	// stopped in the wrong place.
	if got := filepath.Base(root); got != "port-scan-mk3" && !strings.Contains(root, string(filepath.Separator)) {
		t.Fatalf("RepoRoot returned an implausible root: %q", root)
	}
}

func TestRepoRoot_WhenOutsideAModule_ReturnsErrRepoRootNotFound(t *testing.T) {
	// t.TempDir() is created under the OS temp root, which has no go.mod above
	// it in any supported CI environment.
	_, err := RepoRoot(t.TempDir())
	if !errors.Is(err, ErrRepoRootNotFound) {
		t.Fatalf("expected ErrRepoRootNotFound outside a module, got: %v", err)
	}
}

func TestReadRepoFile_ReadsAPathRelativeToTheRepoRoot(t *testing.T) {
	got, err := ReadRepoFile(".", "go.mod")
	if err != nil {
		t.Fatalf("ReadRepoFile(go.mod): %v", err)
	}
	if !strings.Contains(got, "module github.com/xuxiping/port-scan-mk3") {
		t.Fatalf("go.mod does not look like this module's go.mod:\n%s", got)
	}
}

// TestNormalizeNewlines_MakesAWindowsCheckoutReadLikeALinuxOne guards the fix
// for the first red run of the native Windows gate: git checks these files out
// with CRLF on windows-latest, so every line-exact assertion below (the job-key
// match in particular) silently stopped matching there.
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

func TestReadRepoFile_ReturnsNoCarriageReturns(t *testing.T) {
	got, err := ReadRepoFile(".", ".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("ReadRepoFile(ci.yml): %v", err)
	}
	if strings.Contains(got, "\r") {
		t.Fatal("ReadRepoFile returned carriage returns; line-exact assertions would be platform-dependent")
	}
}

func TestReadRepoFile_WhenFileIsMissing_ReturnsAWrappedNotExistError(t *testing.T) {
	_, err := ReadRepoFile(".", "no/such/file-for-ciguard.txt")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a wrapped os.ErrNotExist, got: %v", err)
	}
}
