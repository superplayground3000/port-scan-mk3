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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

const (
	// CurrentTargetSemanticsVersion identifies snapshots that apply basic row ports.
	CurrentTargetSemanticsVersion = 1
	// DefaultSnapshotSizeLimitBytes is the default serialized snapshot size.
	DefaultSnapshotSizeLimitBytes uint64 = 2_000_000_000
	// DefaultSnapshotChunkLimit is the default number of chunks in a snapshot.
	DefaultSnapshotChunkLimit uint64 = 10_000_000
	// DefaultSnapshotPortEntryLimit is the default port-entry count across chunks.
	DefaultSnapshotPortEntryLimit uint64 = 10_000_000
	// DefaultSnapshotUnreachableIPLimit is the default unreachable-IP count.
	DefaultSnapshotUnreachableIPLimit uint64 = 10_000_000
)

// SnapshotLimits controls serialized bytes and decoded object counts for one snapshot.
// A zero maximum disables only that limit.
type SnapshotLimits struct {
	MaxBytes          uint64
	MaxChunks         uint64
	MaxPortEntries    uint64
	MaxUnreachableIPs uint64
}

// DefaultSnapshotLimits returns the default byte and object-count limits.
// It does not access a snapshot and cannot return an error.
func DefaultSnapshotLimits() SnapshotLimits {
	return SnapshotLimits{
		MaxBytes:          DefaultSnapshotSizeLimitBytes,
		MaxChunks:         DefaultSnapshotChunkLimit,
		MaxPortEntries:    DefaultSnapshotPortEntryLimit,
		MaxUnreachableIPs: DefaultSnapshotUnreachableIPLimit,
	}
}

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

// TargetExpansionState records the effective limits and original candidate count.
type TargetExpansionState struct {
	CandidateCount uint64 `json:"candidate_count"`
	CandidateLimit int64  `json:"candidate_limit"`
	MemoryLimitGB  int64  `json:"memory_limit_gb"`
}

// Snapshot is the current resume envelope that the state package persists.
type Snapshot struct {
	Chunks      []task.Chunk     `json:"chunks"`
	PreScanPing PreScanPingState `json:"pre_scan_ping,omitempty"`
	Output      *OutputState     `json:"output,omitempty"`
	// RichDenyExcluded is true when the target builder excluded rich deny rows.
	RichDenyExcluded bool `json:"rich_deny_excluded,omitempty"`
	// TargetExpansion is nil for snapshots that predate expansion limits.
	TargetExpansion *TargetExpansionState `json:"target_expansion,omitempty"`
	// TargetSemanticsVersion identifies the rules that produced the target set.
	TargetSemanticsVersion int `json:"target_semantics_version,omitempty"`
	// BasicPortFallback records the port-file values for blank basic row ports.
	BasicPortFallback []string `json:"basic_port_fallback,omitempty"`
}

type snapshotEnvelope struct {
	Chunks                 *[]task.Chunk         `json:"chunks"`
	PreScanPing            *preScanPingEnvelope  `json:"pre_scan_ping,omitempty"`
	Output                 *OutputState          `json:"output,omitempty"`
	RichDenyExcluded       bool                  `json:"rich_deny_excluded,omitempty"`
	TargetExpansion        *TargetExpansionState `json:"target_expansion,omitempty"`
	TargetSemanticsVersion int                   `json:"target_semantics_version,omitempty"`
	BasicPortFallback      []string              `json:"basic_port_fallback,omitempty"`
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

type snapshotLoadFileOps struct {
	stat func(string) (os.FileInfo, error)
	open func(string) (*os.File, error)
}

var loadFileOps = snapshotLoadFileOps{stat: os.Stat, open: os.Open}

// SaveSnapshot writes resume state as the current JSON envelope.
func SaveSnapshot(path string, snap Snapshot) error {
	return SaveSnapshotWithLimits(path, snap, DefaultSnapshotLimits())
}

// SaveSnapshotWithLimits writes snap to path when its bytes and object counts fit limits.
// It returns a serialization, limit, or filesystem error.
func SaveSnapshotWithLimits(path string, snap Snapshot, limits SnapshotLimits) error {
	_, previousStatErr := os.Stat(path)
	if err := validateSnapshotLimits(path, snap, limits); err != nil {
		return err
	}
	env := snapshotEnvelopeFromSnapshot(snap)

	if err := writeSnapshotViaTempRename(path, env, limits, snapshotFileMode); err != nil {
		switch {
		case previousStatErr == nil:
			return fmt.Errorf("save snapshot failed: previous snapshot remains usable: %w", err)
		case os.IsNotExist(previousStatErr):
			return fmt.Errorf("save snapshot failed: no previous snapshot is available: %w", err)
		default:
			return fmt.Errorf("save snapshot failed: previous snapshot usability is unknown: %w", err)
		}
	}
	return nil
}

func snapshotEnvelopeFromSnapshot(snap Snapshot) snapshotEnvelope {
	env := snapshotEnvelope{
		Chunks:           &snap.Chunks,
		RichDenyExcluded: snap.RichDenyExcluded,
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
	if snap.TargetExpansion != nil {
		expansion := *snap.TargetExpansion
		env.TargetExpansion = &expansion
	}
	env.TargetSemanticsVersion = snap.TargetSemanticsVersion
	env.BasicPortFallback = append([]string(nil), snap.BasicPortFallback...)

	return env
}

// snapshotFileMode is the permission the snapshot file ends up with, matching
// the mode the previous direct-write implementation passed to os.WriteFile.
const snapshotFileMode os.FileMode = 0o644

// writeSnapshotViaTempRename writes a snapshot to a temporary file in the
// destination directory. It renames that file over path after a successful write.
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
func writeSnapshotViaTempRename(path string, env snapshotEnvelope, limits SnapshotLimits, perm os.FileMode) (err error) {
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

	writer := io.Writer(snapshotFileWriter{file: f})
	if limits.MaxBytes > 0 {
		writer = &snapshotLimitWriter{writer: writer, path: path, limit: limits.MaxBytes}
	}
	buffered := bufio.NewWriterSize(writer, 256*1024)
	if writeErr := encodeSnapshot(buffered, env); writeErr != nil {
		return fmt.Errorf("write temp snapshot file %s: %w", tmpPath, writeErr)
	}
	if flushErr := buffered.Flush(); flushErr != nil {
		return fmt.Errorf("write buffered temp snapshot file %s: %w", tmpPath, flushErr)
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

type snapshotFileWriter struct {
	file *os.File
}

func (writer snapshotFileWriter) Write(data []byte) (int, error) {
	return fileOps.write(writer.file, data)
}

type snapshotLimitWriter struct {
	writer io.Writer
	path   string
	limit  uint64
	count  uint64
}

func (writer *snapshotLimitWriter) Write(data []byte) (int, error) {
	remaining := writer.limit - writer.count
	if uint64(len(data)) > remaining {
		data = data[:remaining+1]
	}
	count, err := writer.writer.Write(data)
	writer.count += uint64(count)
	if err != nil {
		return count, err
	}
	if writer.count > writer.limit {
		return count, snapshotLimitError(writer.path, "serialized bytes", writer.count, writer.limit, "-snapshot-size-limit-gb")
	}
	return count, nil
}

// destinationMode reports the permissions a replacement file should carry:
// those of the file it replaces when one exists, so a save never loosens
// permissions an operator tightened, and fallback for a first save.
func destinationMode(path string, fallback os.FileMode) os.FileMode {
	info, err := loadFileOps.stat(path)
	if err != nil {
		return fallback
	}
	return info.Mode().Perm()
}

// LoadSnapshot reads the resume state from the current envelope or from the
// legacy JSON array of chunks.
func LoadSnapshot(path string) (Snapshot, error) {
	return LoadSnapshotWithLimits(path, DefaultSnapshotLimits())
}

// LoadSnapshotWithLimits reads path and returns a snapshot that fits limits.
// It accepts current and legacy JSON. It returns a path, read, decode, or limit error.
func LoadSnapshotWithLimits(path string, limits SnapshotLimits) (Snapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat snapshot %s: %w", path, err)
	}
	if info.Size() < 0 || uint64(info.Size()) > uint64(math.MaxInt) {
		return Snapshot{}, fmt.Errorf("snapshot %s size %d cannot fit in memory", path, info.Size())
	}
	if limits.MaxBytes > 0 && uint64(info.Size()) > limits.MaxBytes {
		return Snapshot{}, snapshotLimitError(path, "input bytes", uint64(info.Size()), limits.MaxBytes, "-snapshot-size-limit-gb")
	}
	f, err := loadFileOps.open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open snapshot %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(f, data); err != nil {
		return Snapshot{}, fmt.Errorf("read snapshot %s: file size changed after stat: %w", path, err)
	}
	var extra [1]byte
	if count, readErr := f.Read(extra[:]); count != 0 || readErr != io.EOF {
		if readErr == nil {
			readErr = errors.New("file grew after stat")
		}
		return Snapshot{}, fmt.Errorf("read snapshot %s: file size changed after stat: %w", path, readErr)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return Snapshot{}, errors.New("unexpected end of JSON input")
	}

	switch trimmed[0] {
	case '[':
		var chunks []task.Chunk
		if err := json.Unmarshal(trimmed, &chunks); err != nil {
			return Snapshot{}, err
		}
		snap := Snapshot{Chunks: chunks}
		if err := validateSnapshotLimits(path, snap, limits); err != nil {
			return Snapshot{}, err
		}
		return snap, nil
	case '{':
		preScanPresent, err := validateSnapshotSchema(trimmed)
		if err != nil {
			return Snapshot{}, err
		}
		env := snapshotEnvelope{PreScanPing: &preScanPingEnvelope{
			UnreachableIPv4U32: make([]uint32, 0, unreachableCapacityHint(uint64(len(trimmed)), limits.MaxUnreachableIPs)),
		}}
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return Snapshot{}, err
		}
		if !preScanPresent {
			env.PreScanPing = nil
		}
		if env.Chunks == nil {
			return Snapshot{}, errors.New("resume snapshot missing required chunks field")
		}
		snap := Snapshot{
			Chunks:                 *env.Chunks,
			RichDenyExcluded:       env.RichDenyExcluded,
			TargetSemanticsVersion: env.TargetSemanticsVersion,
			BasicPortFallback:      append([]string(nil), env.BasicPortFallback...),
		}
		if env.TargetExpansion != nil {
			expansion := *env.TargetExpansion
			snap.TargetExpansion = &expansion
		}
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
		if err := validateSnapshotLimits(path, snap, limits); err != nil {
			return Snapshot{}, err
		}
		return snap, nil
	default:
		return Snapshot{}, fmt.Errorf("invalid resume snapshot root token %q", trimmed[0])
	}
}

func validateSnapshotLimits(path string, snap Snapshot, limits SnapshotLimits) error {
	chunkCount := uint64(len(snap.Chunks))
	if limits.MaxChunks > 0 && chunkCount > limits.MaxChunks {
		return snapshotLimitError(path, "chunks", chunkCount, limits.MaxChunks, "-snapshot-chunk-limit")
	}
	var portCount uint64
	for _, chunk := range snap.Chunks {
		entryCount := uint64(len(chunk.Ports))
		if portCount > math.MaxUint64-entryCount {
			return fmt.Errorf("snapshot %s port entries overflow the supported count; use -snapshot-port-entry-limit only for representable counts", path)
		}
		portCount += entryCount
		if limits.MaxPortEntries > 0 && portCount > limits.MaxPortEntries {
			return snapshotLimitError(path, "port entries", portCount, limits.MaxPortEntries, "-snapshot-port-entry-limit")
		}
	}
	unreachableCount := uint64(len(snap.PreScanPing.UnreachableIPv4U32))
	if limits.MaxUnreachableIPs > 0 && unreachableCount > limits.MaxUnreachableIPs {
		return snapshotLimitError(path, "unreachable IPs", unreachableCount, limits.MaxUnreachableIPs, "-snapshot-unreachable-ip-limit")
	}
	return nil
}

func snapshotLimitError(path, objectType string, count, limit uint64, flagName string) error {
	return fmt.Errorf("snapshot %s object type %s has known count %d above limit %d; use %s to override it", path, objectType, count, limit, flagName)
}

func hasPreScanPingState(state PreScanPingState) bool {
	return state.Enabled || state.TimeoutMS != 0 || len(state.UnreachableIPv4U32) > 0
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
