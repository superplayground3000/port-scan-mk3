package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// errInjected is the sentinel every fault-injection test wraps, so the
// assertions can prove SaveSnapshot wraps the underlying cause instead of
// replacing it with a message of its own.
var errInjected = errors.New("injected filesystem failure")

// withFileOps swaps the package-level filesystem seam for the duration of one
// test. Tests in this package never run in parallel, so a package-level swap is
// safe; the cleanup restores production behavior even if the test fails.
func withFileOps(t *testing.T, mutate func(ops *snapshotFileOps)) {
	t.Helper()
	original := fileOps
	t.Cleanup(func() { fileOps = original })
	ops := defaultSnapshotFileOps()
	mutate(&ops)
	fileOps = ops
}

// writeInitialSnapshot persists a first snapshot through the production code
// path (never os.WriteFile from the test) and returns the bytes on disk, so a
// later assertion can compare the file byte-for-byte.
func writeInitialSnapshot(t *testing.T, path string) []byte {
	t.Helper()
	first := Snapshot{Chunks: []task.Chunk{{
		CIDR:         "10.0.0.0/24",
		NextIndex:    7,
		TotalCount:   256,
		ScannedCount: 7,
		Status:       "scanning",
	}}}
	if err := SaveSnapshot(path, first); err != nil {
		t.Fatalf("writing the initial snapshot failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the initial snapshot failed: %v", err)
	}
	return data
}

// replacementSnapshot is the snapshot every fault-injection test tries (and
// fails) to save over the initial one.
func replacementSnapshot() Snapshot {
	return Snapshot{Chunks: []task.Chunk{{
		CIDR:         "10.0.0.0/24",
		NextIndex:    250,
		TotalCount:   256,
		ScannedCount: 250,
		Status:       "scanning",
	}}}
}

// TestSaveSnapshot_WhenWriteFails_PreservesPreviousSnapshot is the core
// guarantee of issue #66: a failure before the file is replaced must leave the
// previous resume state byte-for-byte readable, because it is the only copy.
func TestSaveSnapshot_WhenWriteFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		ops.write = func(*os.File, []byte) (int, error) { return 0, errInjected }
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "write")
}

// TestSaveSnapshot_WhenSyncFails_PreservesPreviousSnapshot proves the temp file
// is flushed to disk before it is promoted: a sync failure means the temp file
// contents are not durable, so the previous snapshot must stay in place.
func TestSaveSnapshot_WhenSyncFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		ops.sync = func(*os.File) error { return errInjected }
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "sync")
}

// TestSaveSnapshot_WhenCloseFails_PreservesPreviousSnapshot covers the stage
// that is easiest to ignore: on network filesystems a deferred flush surfaces
// only at close, so a close error means the temp file may be incomplete and
// must never be promoted over a good snapshot.
func TestSaveSnapshot_WhenCloseFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		realClose := ops.closeFile
		ops.closeFile = func(f *os.File) error {
			// Still release the descriptor so the cleanup can remove the temp
			// file on Windows, but report the failure to the caller.
			_ = realClose(f)
			return errInjected
		}
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "close")
}

// TestSaveSnapshot_WhenReplaceFails_PreservesPreviousSnapshot covers the last
// stage: if the rename itself fails, the destination must still hold the old
// snapshot rather than a half-installed new one.
func TestSaveSnapshot_WhenReplaceFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		ops.replace = func(string, string) error { return errInjected }
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "replace")
}

// TestSaveSnapshot_WhenTempCreateFails_PreservesPreviousSnapshot covers the
// stage before any file exists — a read-only or full directory.
func TestSaveSnapshot_WhenTempCreateFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		ops.createTemp = func(string, string) (*os.File, error) { return nil, errInjected }
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "create")
}

// TestSaveSnapshot_WhenSettingModeFails_PreservesPreviousSnapshot keeps the
// permission step inside the same all-or-nothing contract as the other stages.
func TestSaveSnapshot_WhenSettingModeFails_PreservesPreviousSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	before := writeInitialSnapshot(t, path)

	withFileOps(t, func(ops *snapshotFileOps) {
		ops.chmod = func(string, os.FileMode) error { return errInjected }
	})

	err := SaveSnapshot(path, replacementSnapshot())
	assertFailedSaveLeftSnapshotIntact(t, path, before, err, "mode")
}

// TestSaveSnapshot_WhenReplacingExistingFile_LeavesValidJSONAndNoTempFile is
// the success half of the contract, and the case native Windows CI exercises:
// renaming over an existing file must succeed and leave nothing behind.
func TestSaveSnapshot_WhenReplacingExistingFile_LeavesValidJSONAndNoTempFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")
	writeInitialSnapshot(t, path)

	want := replacementSnapshot()
	if err := SaveSnapshot(path, want); err != nil {
		t.Fatalf("replacing an existing snapshot failed: %v", err)
	}

	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("the replaced snapshot is not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot mismatch after replacement:\ngot  %+v\nwant %+v", got, want)
	}

	assertNoTempFilesBesideSnapshot(t, path)
}

// TestSaveSnapshot_WhenDestinationIsNew_CreatesItWithNoTempFileLeftBehind keeps
// the first-save path covered now that it goes through a temp file too.
func TestSaveSnapshot_WhenDestinationIsNew_CreatesItWithNoTempFileLeftBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")

	if err := SaveSnapshot(path, replacementSnapshot()); err != nil {
		t.Fatalf("creating a new snapshot failed: %v", err)
	}
	if _, err := LoadSnapshot(path); err != nil {
		t.Fatalf("the new snapshot is not valid JSON: %v", err)
	}

	assertNoTempFilesBesideSnapshot(t, path)
}

// TestSaveSnapshot_CreatesTempFileInDestinationDirectory pins the property the
// atomicity depends on: a rename is only atomic within one filesystem, so the
// temp file must be a sibling of the destination, never in the system temp dir.
func TestSaveSnapshot_CreatesTempFileInDestinationDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume_state.json")

	var gotDirs []string
	withFileOps(t, func(ops *snapshotFileOps) {
		realCreate := ops.createTemp
		ops.createTemp = func(dir, pattern string) (*os.File, error) {
			gotDirs = append(gotDirs, dir)
			return realCreate(dir, pattern)
		}
	})

	if err := SaveSnapshot(path, replacementSnapshot()); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if want := []string{filepath.Dir(path)}; !reflect.DeepEqual(gotDirs, want) {
		t.Fatalf("temp file directories: got %v, want %v", gotDirs, want)
	}
}

// TestSaveSnapshot_WhenDestinationIsNew_UsesTheSameModeAsBefore guards against
// the temp file's private 0600 default leaking into the snapshot's permissions.
func TestSaveSnapshot_WhenDestinationIsNew_UsesTheSameModeAsBefore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not model POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "resume_state.json")

	if err := SaveSnapshot(path, replacementSnapshot()); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("snapshot mode: got %O, want %O", got, 0o644)
	}
}

// TestSaveSnapshot_WhenDestinationExists_KeepsItsMode makes sure replacing a
// snapshot does not loosen permissions an operator tightened on a file that
// lists internal scan targets.
func TestSaveSnapshot_WhenDestinationExists_KeepsItsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not model POSIX permission bits")
	}
	path := filepath.Join(t.TempDir(), "resume_state.json")
	writeInitialSnapshot(t, path)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}

	if err := SaveSnapshot(path, replacementSnapshot()); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("snapshot mode after replacement: got %O, want %O", got, 0o640)
	}
}

// assertFailedSaveLeftSnapshotIntact checks the full contract of a failed save:
// a wrapped error naming the failed stage, the previous snapshot untouched, and
// no temp file abandoned next to it. It uses Errorf (not Fatalf) for the error
// checks so a run that wrongly reports success still shows whether the previous
// snapshot survived.
func assertFailedSaveLeftSnapshotIntact(t *testing.T, path string, before []byte, err error, stage string) {
	t.Helper()

	if err == nil {
		t.Errorf("expected SaveSnapshot to fail at the %s stage, got nil", stage)
	} else {
		if !errors.Is(err, errInjected) {
			t.Errorf("expected the error to wrap the injected cause, got %v", err)
		}
		if !strings.Contains(err.Error(), stage) {
			t.Errorf("expected the error to identify the %q stage, got %v", stage, err)
		}
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("previous snapshot is no longer readable: %v", readErr)
	}
	if string(after) != string(before) {
		t.Errorf("previous snapshot was modified.\nbefore:\n%s\nafter:\n%s", before, after)
	}

	assertNoTempFilesBesideSnapshot(t, path)
}

// assertNoTempFilesBesideSnapshot globs the destination directory — the same
// directory the implementation must create its temp file in — for anything that
// is not the snapshot itself.
func assertNoTempFilesBesideSnapshot(t *testing.T, path string) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reading the snapshot directory failed: %v", err)
	}
	var leftovers []string
	for _, entry := range entries {
		if filepath.Join(filepath.Dir(path), entry.Name()) != path {
			leftovers = append(leftovers, entry.Name())
		}
	}
	if len(leftovers) > 0 {
		t.Errorf("expected no files beside %s, found %v", filepath.Base(path), leftovers)
	}
}
