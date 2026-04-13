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
	"encoding/json"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// Save serializes a slice of task.Chunk to a JSON file at the given path.
// The file is written with indentation for human readability.
//
// # Parameters
//
//	path:   Destination file path.
//	chunks: Chunk state to persist.
//
// # Returns
//
//	nil on success; error if marshaling or file writing fails.
//
// # Example
//
//	chunks := []task.Chunk{{CIDR: "10.0.0.0/8", NextIndex: 42, TotalCount: 100, Status: "scanning"}}
//	if err := state.Save("resume_state.json", chunks); err != nil {
//	    log.Fatalf("save failed: %v", err)
//	}
func Save(path string, chunks []task.Chunk) error {
	data, err := json.MarshalIndent(chunks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads and deserializes a resume-state JSON file into a slice of task.Chunk.
//
// # Parameters
//
//	path: Source file path (written by Save).
//
// # Returns
//
//	[]task.Chunk on success; error if the file cannot be read or the JSON is malformed.
//
// # Example
//
//	chunks, err := state.Load("resume_state.json")
//	if err != nil {
//	    log.Fatalf("load failed: %v", err)
//	}
func Load(path string) ([]task.Chunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var chunks []task.Chunk
	if err := json.Unmarshal(data, &chunks); err != nil {
		return nil, err
	}
	return chunks, nil
}
