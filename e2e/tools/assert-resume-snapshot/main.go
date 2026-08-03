// Command assert-resume-snapshot checks that a resume snapshot left behind by
// an aborted scan is actually resumable.
//
// The isolated e2e suite uses it to turn "the scan exited non-zero and some
// file exists" into a contract-level assertion: the snapshot must decode
// through pkg/state, every chunk must be internally consistent, and the two
// halves of "the abort left resumable work behind" must both hold:
//
//	-require-progress   at least one chunk advanced past next_index=0, which is
//	                    what proves the aborted run wrote its cursor back. The
//	                    snapshot path doubles as the scan's -resume INPUT, so
//	                    "the file exists" alone proves nothing.
//	-require-remaining  at least one chunk still has undispatched work. A fully
//	                    dispatched snapshot means the scan finished instead of
//	                    being aborted — the exact failure mode issue #71 rules out.
//
// Usage:
//
//	assert-resume-snapshot -file <path> [-require-progress] [-require-remaining]
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

// checks selects the optional assertions on top of the structural ones.
type checks struct {
	requireProgress  bool
	requireRemaining bool
}

func main() {
	file := flag.String("file", "", "path to the resume snapshot JSON to check")
	requireProgress := flag.Bool("require-progress", false,
		"also require that the aborted run advanced at least one chunk past next_index=0")
	requireRemaining := flag.Bool("require-remaining", false,
		"also require that at least one chunk still has undispatched work")
	flag.Parse()

	if err := run(*file, checks{requireProgress: *requireProgress, requireRemaining: *requireRemaining}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, want checks) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("-file is required")
	}

	snap, err := state.LoadSnapshot(trimmed)
	if err != nil {
		return fmt.Errorf("load resume snapshot %s: %w", trimmed, err)
	}
	if err := assertSnapshot(snap, want); err != nil {
		return fmt.Errorf("resume snapshot %s is not resumable: %w", trimmed, err)
	}
	return nil
}

func assertSnapshot(snap state.Snapshot, want checks) error {
	if len(snap.Chunks) == 0 {
		return errors.New("snapshot holds no chunk at all")
	}

	remaining := 0
	progressed := 0
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
		progressed += chunk.NextIndex
	}

	if want.requireProgress && progressed == 0 {
		return fmt.Errorf("no chunk advanced past next_index=0 across %d chunk(s), so the aborted run persisted no progress into the snapshot", len(snap.Chunks))
	}
	if want.requireRemaining && remaining == 0 {
		return fmt.Errorf("every chunk is fully dispatched across %d chunk(s), so the scan finished instead of being aborted with work remaining", len(snap.Chunks))
	}
	return nil
}
