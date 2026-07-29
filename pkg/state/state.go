// Package state manages chunk resume state persistence for the port scanner.
//
// Resume state captures the progress of each CIDR chunk (scanned count, next index,
// status) so that an interrupted scan can be restarted from where it left off.
// State is stored as JSON and read/written via Save and Load.
//
// # Function Flow
//
//	Scan Interrupted
//	  |
//	  v
//	collectChunkStates  ── []task.Chunk
//	  |
//	  v
//	state.Save(resume_path)  → JSON file
//	  |
//	  v (on restart)
//	state.Load(resume_path)  ← JSON file
//	  |
//	  v
//	loadOrBuildChunks  ── resumes with existing chunks
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// PreScanPingState stores the pre-scan ping metadata persisted in resume state.
type PreScanPingState struct {
	Enabled            bool     `json:"enabled"`
	TimeoutMS          int      `json:"timeout_ms"`
	UnreachableIPv4U32 []uint32 `json:"unreachable_ipv4_u32,omitempty"`
}

// OutputState records the scan/open result files a run wrote so that a
// subsequent -resume appends to the same files instead of minting new
// timestamped paths (design §3.7). Snapshots written before this field existed
// decode with a nil Output; callers treat that as "no recorded path".
type OutputState struct {
	ScanPath string `json:"scan_path"`
	OpenPath string `json:"open_path"`
}

// Snapshot is the current resume envelope persisted by the state package.
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
	return os.WriteFile(path, data, 0o644)
}

// LoadSnapshot reads resume state from either the current envelope or legacy chunk array JSON.
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

// Save serializes chunk resume state to a JSON file at the given path via the
// current snapshot envelope. The file is written with indentation for
// readability.
//
// # Parameters
//
//	path:   Destination file path.
//	chunks: Chunk state to persist.
//
// # Returns
//
//	nil on success; error if marshaling or file writing fails.
func Save(path string, chunks []task.Chunk) error {
	return SaveSnapshot(path, Snapshot{Chunks: chunks})
}

// Load reads and deserializes a resume-state JSON file into a slice of task.Chunk.
// It accepts both the current snapshot envelope and the legacy chunk-array format.
//
// # Parameters
//
//	path: Source file path (written by Save).
//
// # Returns
//
//	[]task.Chunk on success; error if the file cannot be read or the JSON is malformed.
func Load(path string) ([]task.Chunk, error) {
	snap, err := LoadSnapshot(path)
	if err != nil {
		return nil, err
	}
	return snap.Chunks, nil
}
