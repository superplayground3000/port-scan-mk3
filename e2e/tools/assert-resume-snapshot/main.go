// Command assert-resume-snapshot checks that a resume snapshot left behind by
// an aborted scan is actually resumable.
//
// The isolated e2e suite uses it to turn "the scan exited non-zero and some
// file exists" into a contract-level assertion: the snapshot must decode
// through pkg/state, every chunk must be internally consistent, and — with
// -require-remaining — at least one chunk must still have undispatched work.
// A fully dispatched snapshot means the scan actually finished, which is the
// exact failure mode issue #71 exists to rule out.
//
// Usage:
//
//	assert-resume-snapshot -file <path> [-require-remaining]
//
// It exits 0 when the snapshot is resumable and 1 with a diagnostic on stderr
// otherwise.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// validChunkStatuses mirrors the statuses pkg/task documents on Chunk.Status.
var validChunkStatuses = map[string]bool{
	"pending":   true,
	"scanning":  true,
	"completed": true,
}

func main() {
	file := flag.String("file", "", "path to the resume snapshot JSON to check")
	requireRemaining := flag.Bool("require-remaining", false,
		"also require that at least one chunk still has undispatched work")
	flag.Parse()

	if err := run(*file, *requireRemaining); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, requireRemaining bool) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("-file is required")
	}

	snap, err := state.LoadSnapshot(trimmed)
	if err != nil {
		return fmt.Errorf("load resume snapshot %s: %w", trimmed, err)
	}
	if err := assertSnapshot(snap, requireRemaining); err != nil {
		return fmt.Errorf("resume snapshot %s is not resumable: %w", trimmed, err)
	}
	return nil
}

func assertSnapshot(snap state.Snapshot, requireRemaining bool) error {
	if len(snap.Chunks) == 0 {
		return errors.New("snapshot holds no chunk at all")
	}

	remaining := 0
	for i, chunk := range snap.Chunks {
		where := fmt.Sprintf("chunk %d (cidr=%s)", i, chunk.CIDR)

		if chunk.TotalCount <= 0 {
			return fmt.Errorf("%s: total_count=%d, want > 0", where, chunk.TotalCount)
		}
		if chunk.NextIndex < 0 || chunk.NextIndex > chunk.TotalCount {
			return fmt.Errorf("%s: next_index=%d out of range [0,%d]", where, chunk.NextIndex, chunk.TotalCount)
		}
		if chunk.ScannedCount < 0 || chunk.ScannedCount > chunk.TotalCount {
			return fmt.Errorf("%s: scanned_count=%d out of range [0,%d]", where, chunk.ScannedCount, chunk.TotalCount)
		}
		if !validChunkStatuses[chunk.Status] {
			return fmt.Errorf("%s: status=%q is not one of pending/scanning/completed", where, chunk.Status)
		}

		remaining += chunk.Remaining()
	}

	if requireRemaining && remaining == 0 {
		return fmt.Errorf("every chunk is fully dispatched across %d chunk(s), so the scan finished instead of being aborted with work remaining", len(snap.Chunks))
	}
	return nil
}
