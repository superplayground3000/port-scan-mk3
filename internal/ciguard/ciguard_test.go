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

func TestReadRepoFile_WhenFileIsMissing_ReturnsAWrappedNotExistError(t *testing.T) {
	_, err := ReadRepoFile(".", "no/such/file-for-ciguard.txt")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a wrapped os.ErrNotExist, got: %v", err)
	}
}
