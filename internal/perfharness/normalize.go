package perfharness

import (
	"slices"
	"strings"
	"time"
)

// SemanticArtifact contains portable results and declared volatile fields.
type SemanticArtifact struct {
	Root         string        `json:"root"`
	Path         string        `json:"path"`
	Timestamp    time.Time     `json:"timestamp"`
	Duration     time.Duration `json:"duration_ns"`
	OSError      string        `json:"os_error"`
	TaskOrder    []string      `json:"task_order"`
	RowCount     uint64        `json:"row_count"`
	Status       string        `json:"status"`
	Cursor       uint64        `json:"cursor"`
	OutputDigest string        `json:"output_digest"`
}

// CompareSemantic compares portable behavior after permitted normalization.
func (Suite) CompareSemantic(left, right SemanticArtifact) []string {
	leftPath := normalizePath(left.Root, left.Path)
	rightPath := normalizePath(right.Root, right.Path)
	differences := make([]string, 0)
	if leftPath != rightPath {
		differences = append(differences, "path")
	}
	if !slices.Equal(left.TaskOrder, right.TaskOrder) {
		differences = append(differences, "task_order")
	}
	if left.RowCount != right.RowCount {
		differences = append(differences, "row_count")
	}
	if left.Status != right.Status {
		differences = append(differences, "status")
	}
	if left.Cursor != right.Cursor {
		differences = append(differences, "cursor")
	}
	if left.OutputDigest != right.OutputDigest {
		differences = append(differences, "output_digest")
	}
	return differences
}

func normalizePath(root, path string) string {
	normalizedRoot := strings.TrimSuffix(strings.ReplaceAll(root, `\`, "/"), "/")
	normalizedPath := strings.ReplaceAll(path, `\`, "/")
	if normalizedRoot != "" {
		normalizedPath = strings.TrimPrefix(normalizedPath, normalizedRoot)
	}
	return strings.TrimPrefix(normalizedPath, "/")
}
