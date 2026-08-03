package scanapp

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

type batchOutputPaths struct {
	scanPath        string
	openPath        string
	unreachablePath string
}

// resolveBatchOutputPaths mints the timestamped scan/open/unreachable paths for
// a fresh batch under the -output anchor's directory.
//
// The returned paths are ALWAYS absolute and cleaned, even when outputPath is
// relative (the shipped default is the bare name `scan_results.csv`). That is
// the chosen rule for issue #61: output locations are made unambiguous at their
// single point of origin, BEFORE any file is opened and before they are recorded
// in a resume snapshot. A relative path recorded in a snapshot is only meaningful
// together with the working directory that produced it, which is not persisted;
// resuming from another directory - or, on Windows, another drive - re-resolved
// the same string somewhere else and silently produced a second set of CSVs.
//
// The alternative considered was persisting paths relative to the snapshot file
// and always resolving them from the snapshot's directory. It was rejected
// because the snapshot location (`-buckets-out`/`-resume`) and the output anchor
// (`-output`) are independent flags, so the snapshot directory is not a base the
// user chose for outputs.
func resolveBatchOutputPaths(outputPath string, now time.Time) (batchOutputPaths, error) {
	baseDir, err := absOutputDir(outputPath)
	if err != nil {
		return batchOutputPaths{}, err
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return batchOutputPaths{}, err
	}

	ts := now.UTC().Format("20060102T150405Z")
	for seq := 0; seq < 100; seq++ {
		suffix := ""
		if seq > 0 {
			suffix = fmt.Sprintf("-%d", seq)
		}
		scanPath := filepath.Join(baseDir, fmt.Sprintf("scan_results-%s%s.csv", ts, suffix))
		openPath := filepath.Join(baseDir, fmt.Sprintf("opened_results-%s%s.csv", ts, suffix))
		unreachablePath := filepath.Join(baseDir, fmt.Sprintf("unreachable_results-%s%s.csv", ts, suffix))
		if !fileExists(scanPath) && !fileExists(openPath) && !fileExists(unreachablePath) {
			return batchOutputPaths{
				scanPath:        scanPath,
				openPath:        openPath,
				unreachablePath: unreachablePath,
			}, nil
		}
	}
	return batchOutputPaths{}, fmt.Errorf("failed to allocate unique batch output paths")
}

// absOutputDir returns the absolute, cleaned directory that the batch output
// files derived from outputPath are written into. filepath.Abs completes every
// non-absolute shape against the process working directory, which on Windows
// covers the two that are easy to mistake for absolute: a rooted path with no
// volume (`\out\scan.csv`) and a drive-relative path (`C:scan.csv`).
func absOutputDir(outputPath string) (string, error) {
	dir := filepath.Dir(outputPath)
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve output directory %q: %w", dir, err)
	}
	return abs, nil
}

// resolvePersistedOutputPaths makes the output paths recorded in a resume
// snapshot unambiguous before they are reopened (issue #61).
//
// Compatibility rule for existing snapshots:
//   - An absolute recorded path (every snapshot written by this build, and any
//     older one produced from an absolute -output) is used as-is, only cleaned.
//     Explicit snapshot output paths therefore keep working unchanged.
//   - A relative recorded path can only come from a build older than this fix.
//     It is completed against the process working directory, which is exactly
//     what the old build did implicitly when it reopened the file - so resuming
//     such a snapshot from its original directory behaves as before. Note this
//     recovers no information the old build failed to persist: the directory
//     that produced a legacy relative path was never recorded, so a legacy
//     snapshot resumed from a DIFFERENT directory still resolves against that
//     new directory. Only paths minted by this build are anchored.
//
// The upgrade of a legacy path is written back only in a snapshot the run
// actually saves - i.e. one that is interrupted or leaves work incomplete, which
// is the case where a later -resume still needs the path. A run that finishes
// cleanly saves no snapshot at all (persistResumeSnapshot returns early when
// nothing is resumable), so the legacy string stays on disk; that is harmless
// because the work is done. The upgrade is pinned by
// TestRun_WhenSnapshotHasRelativeOutputPaths_ResumesInPlaceAndRecordsAbsolute
// (same directory) and
// TestRun_WhenLegacyRelativeSnapshotResumedFromAnotherDirectory_UpgradeAnchorsToTheResumeDirectory
// (which shows the anchor is the resume directory, the documented limit above);
// the "clean completion saves nothing" half is pinned by
// TestRun_WhenScanCompletes_DoesNotWriteResumeState and
// TestPersistResumeState_WhenRunCompletesCleanly_SkipsWrite.
//
// It performs no filesystem access, so it never creates directories.
func resolvePersistedOutputPaths(recorded state.OutputState) (state.OutputState, error) {
	scanPath, err := absOutputFile(recorded.ScanPath)
	if err != nil {
		return state.OutputState{}, fmt.Errorf("resolve recorded scan output path: %w", err)
	}
	openPath, err := absOutputFile(recorded.OpenPath)
	if err != nil {
		return state.OutputState{}, fmt.Errorf("resolve recorded open-only output path: %w", err)
	}
	return state.OutputState{ScanPath: scanPath, OpenPath: openPath}, nil
}

// absOutputFile completes a single recorded output file path. An empty path is
// left empty: it carries no location to anchor, and the open path below reports
// the resulting failure with the original (empty) name.
func absOutputFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%q: %w", path, err)
	}
	return abs, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
