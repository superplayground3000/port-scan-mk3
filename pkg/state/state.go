// Package state manages the persistence of the chunk resume state for the port
// scanner.
//
// The resume state holds the progress of each CIDR chunk: the scanned count,
// the next index, and the status. Therefore an interrupted scan can start again
// from the point where it stopped. The package stores the state as JSON. Save
// writes the state and Load reads it.
//
// # Function Flow
//
//	Scan Interrupted
//	  |
//	  v
//	collectChunkStates  ── []task.Chunk
//	  |
//	  v
//	state.SaveSnapshot(resume_path)  → JSON file
//	  |
//	  v (on restart)
//	state.LoadSnapshot(resume_path)  ← JSON file
//	  |
//	  v
//	scanRuntime  ── rebuilds incomplete chunks
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// PreScanPingState stores the pre-scan ping metadata persisted in resume state.
type PreScanPingState struct {
	Enabled            bool     `json:"enabled"`
	TimeoutMS          int      `json:"timeout_ms"`
	UnreachableIPv4U32 []uint32 `json:"unreachable_ipv4_u32,omitempty"`
}

// OutputState records the scan result file and the open result file that a run
// wrote. A later -resume then appends to the same files and does not create new
// timestamped paths (design §3.7). A snapshot written before this field existed
// decodes with a nil Output. A caller treats a nil Output as "no recorded
// path".
type OutputState struct {
	ScanPath string `json:"scan_path"`
	OpenPath string `json:"open_path"`
}

// Snapshot is the current resume envelope that the state package persists.
type Snapshot struct {
	Chunks      []task.Chunk     `json:"chunks"`
	PreScanPing PreScanPingState `json:"pre_scan_ping,omitempty"`
	Output      *OutputState     `json:"output,omitempty"`
}

type snapshotEnvelope struct {
	Chunks      *[]task.Chunk        `json:"chunks"`
	PreScanPing *preScanPingEnvelope `json:"pre_scan_ping,omitempty"`
	Output      *OutputState         `json:"output,omitempty"`
}

type preScanPingEnvelope struct {
	Enabled            *bool    `json:"enabled"`
	TimeoutMS          *int     `json:"timeout_ms"`
	UnreachableIPv4U32 []uint32 `json:"unreachable_ipv4_u32,omitempty"`
}

// snapshotFileOps holds the individual filesystem steps a snapshot write
// performs. Production always uses defaultSnapshotFileOps; the indirection
// exists so tests can inject a failure at one specific stage and assert the
// previous snapshot survives it.
type snapshotFileOps struct {
	createTemp func(dir, pattern string) (*os.File, error)
	write      func(f *os.File, data []byte) (int, error)
	sync       func(f *os.File) error
	closeFile  func(f *os.File) error
	chmod      func(name string, mode os.FileMode) error
	replace    func(oldPath, newPath string) error
	remove     func(name string) error
}

func defaultSnapshotFileOps() snapshotFileOps {
	return snapshotFileOps{
		createTemp: os.CreateTemp,
		write:      func(f *os.File, data []byte) (int, error) { return f.Write(data) },
		sync:       func(f *os.File) error { return f.Sync() },
		closeFile:  func(f *os.File) error { return f.Close() },
		chmod:      os.Chmod,
		replace:    os.Rename,
		remove:     os.Remove,
	}
}

var fileOps = defaultSnapshotFileOps()

// SaveSnapshot writes resume state as the current JSON envelope.
func SaveSnapshot(path string, snap Snapshot) error {
	env := snapshotEnvelope{
		Chunks: &snap.Chunks,
	}
	if hasPreScanPingState(snap.PreScanPing) {
		enabled := snap.PreScanPing.Enabled
		timeoutMS := snap.PreScanPing.TimeoutMS
		env.PreScanPing = &preScanPingEnvelope{
			Enabled:            &enabled,
			TimeoutMS:          &timeoutMS,
			UnreachableIPv4U32: snap.PreScanPing.UnreachableIPv4U32,
		}
	}
	if snap.Output != nil {
		out := *snap.Output
		env.Output = &out
	}

	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return writeFileViaTempRename(path, data, snapshotFileMode)
}

// snapshotFileMode is the permission the snapshot file ends up with, matching
// the mode the previous direct-write implementation passed to os.WriteFile.
const snapshotFileMode os.FileMode = 0o644

// writeFileViaTempRename writes data to a temp file in the destination's own
// directory and only then renames that file over path.
//
// The guarantee is deliberately narrower than "atomic write":
//
//   - the destination is never truncated or written in place;
//   - any failure before the rename leaves an existing file at path
//     byte-for-byte intact, and is reported with the stage that failed;
//   - the rename either replaces the destination or fails loudly, with the
//     previous file still in place.
//
// The rename step itself is atomic only on POSIX. Go explicitly does not
// promise that on Windows — os.Rename's documentation says "even within the
// same directory, on non-Unix platforms Rename is not an atomic operation" —
// so a crash during the rename is out of scope there; what holds on every
// platform is the three points above. The temp file must be a sibling of the
// destination: a rename across filesystems is atomic nowhere and may fail
// outright.
func writeFileViaTempRename(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := fileOps.createTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp snapshot file in %s: %w", dir, err)
	}
	tmpPath := f.Name()

	// Until the rename succeeds the temp file is garbage: drop it so a failed
	// save never leaves debris beside the snapshot it did not replace. If the
	// removal fails too, report it alongside the original failure rather than
	// leaving an unexplained file next to the snapshot.
	defer func() {
		if err == nil {
			return
		}
		_ = fileOps.closeFile(f)
		if removeErr := fileOps.remove(tmpPath); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove temp snapshot file %s: %w", tmpPath, removeErr))
		}
	}()

	// os.CreateTemp opens at 0600; give the snapshot the permissions it would
	// have had without the temp file in the way.
	if chmodErr := fileOps.chmod(tmpPath, destinationMode(path, perm)); chmodErr != nil {
		return fmt.Errorf("set mode on temp snapshot file %s: %w", tmpPath, chmodErr)
	}

	if _, writeErr := fileOps.write(f, data); writeErr != nil {
		return fmt.Errorf("write temp snapshot file %s: %w", tmpPath, writeErr)
	}
	if syncErr := fileOps.sync(f); syncErr != nil {
		return fmt.Errorf("sync temp snapshot file %s: %w", tmpPath, syncErr)
	}
	// Close before the rename: Windows refuses to rename a file that is still
	// open without FILE_SHARE_DELETE. A close error can also be the first
	// report of a failed flush, so it must not be discarded.
	if closeErr := fileOps.closeFile(f); closeErr != nil {
		return fmt.Errorf("close temp snapshot file %s: %w", tmpPath, closeErr)
	}
	if replaceErr := fileOps.replace(tmpPath, path); replaceErr != nil {
		return fmt.Errorf("replace snapshot %s with temp file %s: %w", path, tmpPath, replaceErr)
	}
	return nil
}

// destinationMode reports the permissions a replacement file should carry:
// those of the file it replaces when one exists, so a save never loosens
// permissions an operator tightened, and fallback for a first save.
func destinationMode(path string, fallback os.FileMode) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return fallback
	}
	return info.Mode().Perm()
}

// LoadSnapshot reads the resume state from the current envelope or from the
// legacy JSON array of chunks.
func LoadSnapshot(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Snapshot{}, errors.New("unexpected end of JSON input")
	}

	switch trimmed[0] {
	case '[':
		var chunks []task.Chunk
		if err := decodeStrictJSON(trimmed, &chunks); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{Chunks: chunks}, nil
	case '{':
		var env snapshotEnvelope
		if err := decodeStrictJSON(trimmed, &env); err != nil {
			return Snapshot{}, err
		}
		if env.Chunks == nil {
			return Snapshot{}, errors.New("resume snapshot missing required chunks field")
		}
		snap := Snapshot{Chunks: *env.Chunks}
		if env.PreScanPing != nil {
			if env.PreScanPing.Enabled == nil {
				return Snapshot{}, errors.New("resume snapshot pre_scan_ping missing required enabled field")
			}
			if env.PreScanPing.TimeoutMS == nil {
				return Snapshot{}, errors.New("resume snapshot pre_scan_ping missing required timeout_ms field")
			}
			snap.PreScanPing = PreScanPingState{
				Enabled:            *env.PreScanPing.Enabled,
				TimeoutMS:          *env.PreScanPing.TimeoutMS,
				UnreachableIPv4U32: env.PreScanPing.UnreachableIPv4U32,
			}
		}
		if env.Output != nil {
			out := *env.Output
			snap.Output = &out
		}
		return snap, nil
	default:
		return Snapshot{}, fmt.Errorf("invalid resume snapshot root token %q", trimmed[0])
	}
}

func hasPreScanPingState(state PreScanPingState) bool {
	return state.Enabled || state.TimeoutMS != 0 || len(state.UnreachableIPv4U32) > 0
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON content")
		}
		return err
	}
	return nil
}

// Save serializes the chunk resume state to a JSON file at path. Save uses the
// current snapshot envelope. Save writes the file with indentation, so a person
// can read it.
//
// # Parameters
//
//	path:   Destination file path.
//	chunks: Chunk state to persist.
//
// # Returns
//
//	nil on success. An error if the marshal step or the file write fails.
func Save(path string, chunks []task.Chunk) error {
	return SaveSnapshot(path, Snapshot{Chunks: chunks})
}

// Load reads a resume-state JSON file and deserializes it into a slice of
// task.Chunk. Load accepts the current snapshot envelope and the legacy
// chunk-array format.
//
// # Parameters
//
//	path: Source file path that Save wrote.
//
// # Returns
//
//	[]task.Chunk on success. An error if Load cannot read the file or the JSON
//	is malformed.
func Load(path string) ([]task.Chunk, error) {
	snap, err := LoadSnapshot(path)
	if err != nil {
		return nil, err
	}
	return snap.Chunks, nil
}
