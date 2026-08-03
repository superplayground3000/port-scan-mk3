//go:build windows

package scanapp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// TestResolveBatchOutputPaths_WindowsRelativeOutput_IsAnchoredToTheDrive is the
// Windows half of the #61 acceptance criteria: a relative -output must be
// resolved to a fully qualified path that names a volume before it is opened or
// persisted. Without that, a snapshot resumed from another PowerShell working
// directory - or, worse, another drive - re-resolves the same string somewhere
// else and splits the results.
//
// The expectations are deliberately expressed as "has the cwd's volume" and
// "sits in the cwd", not as a filepath.Join of the same helpers production uses,
// so the assertion can actually disagree with the code (docs/MAINTENANCE.md
// section 6).
func TestResolveBatchOutputPaths_WindowsRelativeOutput_IsAnchoredToTheDrive(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	vol := filepath.VolumeName(tmp)
	if vol == "" {
		t.Fatalf("expected the Windows temp dir %q to carry a volume name", tmp)
	}
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	paths, err := resolveBatchOutputPaths("scan_results.csv", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for name, got := range map[string]string{
		"scanPath":        paths.scanPath,
		"openPath":        paths.openPath,
		"unreachablePath": paths.unreachablePath,
	} {
		if !filepath.IsAbs(got) {
			t.Fatalf("%s must be fully qualified on Windows, got %q", name, got)
		}
		if filepath.VolumeName(got) != vol {
			t.Fatalf("%s must name the cwd volume %s, got %q", name, vol, got)
		}
		if filepath.Dir(got) != filepath.Clean(tmp) {
			t.Fatalf("%s must sit in the cwd %s, got %q", name, tmp, got)
		}
	}
	if base := filepath.Base(paths.scanPath); base != "scan_results-20260302T013045Z.csv" {
		t.Fatalf("unexpected scan file name %q", base)
	}
}

// TestResolvePersistedOutputPaths_WindowsPathShapes covers the snapshot
// compatibility rule against the Windows path shapes that a Linux-only test can
// never discriminate: a drive-rooted path is already unambiguous and passes
// through; a UNC path passes through; a path that is rooted but carries NO
// volume (`\out\scan.csv`) and a drive-relative path (`C:scan.csv`) are NOT
// absolute on Windows and must be completed against the process working
// directory. This helper performs no filesystem writes, so no case here can
// create directories outside the test's temp dir.
func TestResolvePersistedOutputPaths_WindowsPathShapes(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	cwd := filepath.Clean(tmp)
	vol := filepath.VolumeName(cwd)
	if vol == "" {
		t.Fatalf("expected the Windows temp dir %q to carry a volume name", tmp)
	}
	// The cwd without its volume, e.g. `\Users\runneradmin\AppData\Local\Temp\Txx`.
	rootedNoVolume := strings.TrimPrefix(cwd, vol)

	t.Run("drive-rooted paths pass through unchanged", func(t *testing.T) {
		in := state.OutputState{
			ScanPath: filepath.Join(cwd, "scan_results-x.csv"),
			OpenPath: filepath.Join(cwd, "opened_results-x.csv"),
		}
		got, err := resolvePersistedOutputPaths(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != in {
			t.Fatalf("expected %+v unchanged, got %+v", in, got)
		}
	})

	t.Run("UNC paths pass through unchanged", func(t *testing.T) {
		in := state.OutputState{
			ScanPath: `\\server\share\out\scan_results-x.csv`,
			OpenPath: `\\server\share\out\opened_results-x.csv`,
		}
		got, err := resolvePersistedOutputPaths(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != in {
			t.Fatalf("expected %+v unchanged, got %+v", in, got)
		}
	})

	t.Run("rooted without a volume is completed with the cwd drive", func(t *testing.T) {
		got, err := resolvePersistedOutputPaths(state.OutputState{
			ScanPath: rootedNoVolume + `\scan_results-x.csv`,
			OpenPath: rootedNoVolume + `\opened_results-x.csv`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ScanPath != cwd+`\scan_results-x.csv` {
			t.Fatalf("scan_path: got %q want %q", got.ScanPath, cwd+`\scan_results-x.csv`)
		}
		if got.OpenPath != cwd+`\opened_results-x.csv` {
			t.Fatalf("open_path: got %q want %q", got.OpenPath, cwd+`\opened_results-x.csv`)
		}
	})

	t.Run("drive-relative is completed against the working directory", func(t *testing.T) {
		got, err := resolvePersistedOutputPaths(state.OutputState{
			ScanPath: vol + "scan_results-x.csv",
			OpenPath: vol + "opened_results-x.csv",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for name, path := range map[string]string{"scan_path": got.ScanPath, "open_path": got.OpenPath} {
			if !filepath.IsAbs(path) {
				t.Fatalf("%s must be fully qualified, got %q", name, path)
			}
			if filepath.VolumeName(path) != vol {
				t.Fatalf("%s must stay on volume %s, got %q", name, vol, path)
			}
			if !strings.HasPrefix(path, vol+`\`) {
				t.Fatalf("%s must be drive-rooted, got %q", name, path)
			}
		}
	})

	t.Run("bare relative names anchor to the working directory", func(t *testing.T) {
		got, err := resolvePersistedOutputPaths(state.OutputState{
			ScanPath: "scan_results-x.csv",
			OpenPath: "opened_results-x.csv",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ScanPath != cwd+`\scan_results-x.csv` || got.OpenPath != cwd+`\opened_results-x.csv` {
			t.Fatalf("expected cwd-anchored paths under %s, got %+v", cwd, got)
		}
	})
}
