// Package ciguard holds the repository-level contract tests that keep the CI
// configuration honest.
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
// The tests in this package close that hole by asserting the parts of those
// files that MUST stay true, on every platform, inside `make verify`. The
// package itself only provides the small amount of file access those tests
// need; it deliberately contains no product logic.
package ciguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrRepoRootNotFound is returned by RepoRoot when no ancestor directory of the
// starting point contains a go.mod file, i.e. the caller is not inside this
// module's checkout.
var ErrRepoRootNotFound = errors.New("ciguard: repository root not found (no go.mod in any parent directory)")

// RepoRoot walks up from start until it finds the directory holding go.mod and
// returns that directory.
//
// # Parameters
//
//	start: any path inside the checkout; usually the test's working directory.
//
// # Returns
//
//	The absolute path of the module root, or an error wrapping
//	ErrRepoRootNotFound when start is outside a Go module.
func RepoRoot(start string) (string, error) {
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
			return "", fmt.Errorf("ciguard: from %q: %w", start, ErrRepoRootNotFound)
		}
		dir = parent
	}
}

// ReadRepoFile reads a file addressed by its slash-separated path relative to
// the repository root, so callers never hardcode an OS-specific path.
//
// # Parameters
//
//	start: any path inside the checkout (see RepoRoot).
//	relSlashPath: the file's path relative to the repo root, using '/'.
//
// # Returns
//
//	The file contents, or a wrapped error if the root cannot be located or the
//	file cannot be read.
func ReadRepoFile(start, relSlashPath string) (string, error) {
	root, err := RepoRoot(start)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(relSlashPath))
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("ciguard: read %s: %w", relSlashPath, err)
	}
	return string(data), nil
}
