package perfharness

import (
	"fmt"
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
	TaskCount    uint64        `json:"task_count"`
	TaskDigest   string        `json:"task_digest"`
	TaskPrefix   []string      `json:"task_prefix"`
	TaskSuffix   []string      `json:"task_suffix"`
	RowCount     uint64        `json:"row_count"`
	Status       string        `json:"status"`
	Cursor       uint64        `json:"cursor"`
	OutputDigest string        `json:"output_digest"`
}

// CompareReports checks portable Linux and Windows case evidence.
func (suite Suite) CompareReports(left, right Report) []string {
	differences := make([]string, 0)
	if len(left.Cases) != len(right.Cases) {
		differences = append(differences, "case_count")
	}
	count := min(len(left.Cases), len(right.Cases))
	for index := 0; index < count; index++ {
		leftCase := left.Cases[index]
		rightCase := right.Cases[index]
		name := leftCase.Name
		if name != rightCase.Name {
			differences = append(differences, fmt.Sprintf("case_%d:name", index))
			continue
		}
		if leftCase.Verdict.Passed != rightCase.Verdict.Passed {
			differences = append(differences, name+":verdict")
		}
		if leftCase.Correctness != rightCase.Correctness {
			differences = append(differences, name+":correctness")
		}
		if manifestDifferences(leftCase.Manifest, rightCase.Manifest) {
			differences = append(differences, name+":manifest")
		}
		if cancellationDifferences(leftCase.Cancellation, rightCase.Cancellation) {
			differences = append(differences, name+":cancellation")
		}
		if failureDifferences(leftCase.Failure, rightCase.Failure) {
			differences = append(differences, name+":failure")
		}
		switch {
		case leftCase.Semantic == nil && rightCase.Semantic == nil:
		case leftCase.Semantic == nil || rightCase.Semantic == nil:
			differences = append(differences, name+":semantic")
		default:
			for _, difference := range suite.CompareSemantic(*leftCase.Semantic, *rightCase.Semantic) {
				differences = append(differences, name+":"+difference)
			}
		}
	}
	return differences
}

func failureDifferences(left, right *FailureCaseEvidence) bool {
	if left == nil || right == nil {
		return left != right
	}
	if left.SchemaVersion != right.SchemaVersion || len(left.Runs) != len(right.Runs) {
		return true
	}
	for index := range left.Runs {
		a, b := left.Runs[index], right.Runs[index]
		if a.Scenario != b.Scenario || a.Observed != b.Observed || a.ErrorClass != b.ErrorClass ||
			a.Operation != b.Operation || a.TotalItems != b.TotalItems || failureOutputDifferences(a.Output, b.Output) {
			return true
		}
	}
	return false
}

func failureOutputDifferences(left, right *FailureOutputEvidence) bool {
	if left == nil || right == nil {
		return left != right
	}
	return left.FailureAtResult != right.FailureAtResult || left.RewoundChunks != right.RewoundChunks ||
		left.ProbeStartsAfterFailure != right.ProbeStartsAfterFailure || left.SavedCursor != right.SavedCursor ||
		left.Remaining != right.Remaining || left.RowsBeforeRecovery != right.RowsBeforeRecovery ||
		left.OpenRowsBeforeRecovery != right.OpenRowsBeforeRecovery || left.HandlesReleased != right.HandlesReleased ||
		left.RecoveryCompleted != right.RecoveryCompleted || left.RecoveryTaskCount != right.RecoveryTaskCount ||
		left.RecoveryTaskDigest != right.RecoveryTaskDigest || left.ReferenceTaskDigest != right.ReferenceTaskDigest ||
		left.FinalScanRows != right.FinalScanRows || left.FinalOpenRows != right.FinalOpenRows || left.FinalCursor != right.FinalCursor
}

func cancellationDifferences(left, right *CancellationCaseEvidence) bool {
	if left == nil || right == nil {
		return left != right
	}
	if left.SchemaVersion != right.SchemaVersion || len(left.Runs) != len(right.Runs) {
		return true
	}
	for index := range left.Runs {
		a, b := left.Runs[index], right.Runs[index]
		if a.Stage != b.Stage || a.Percent != b.Percent || a.TotalItems != b.TotalItems ||
			a.InjectionThreshold != b.InjectionThreshold || a.ProgressUnit != b.ProgressUnit ||
			a.ContextCanceled != b.ContextCanceled || a.ProbeStartsAfterCancel != b.ProbeStartsAfterCancel ||
			(a.Recovery == nil) != (b.Recovery == nil) {
			return true
		}
		if a.Recovery != nil && (a.Recovery.RecoveryCompleted != b.Recovery.RecoveryCompleted ||
			a.Recovery.FinalScanRows != b.Recovery.FinalScanRows || a.Recovery.FinalOpenRows != b.Recovery.FinalOpenRows ||
			a.Recovery.FinalCursor != b.Recovery.FinalCursor) {
			return true
		}
	}
	return false
}

func manifestDifferences(left, right *Manifest) bool {
	if left == nil || right == nil {
		return left != right
	}
	return left.SchemaVersion != right.SchemaVersion ||
		left.Family != right.Family ||
		left.Shape != right.Shape ||
		left.Seed != right.Seed ||
		left.InputRecords != right.InputRecords ||
		left.CandidateAddresses != right.CandidateAddresses ||
		left.ProbeTasks != right.ProbeTasks ||
		left.ExpectedOutputs != right.ExpectedOutputs ||
		left.ActualBytes != right.ActualBytes ||
		left.SHA256 != right.SHA256 ||
		left.ArtifactName != right.ArtifactName
}

// CompareSemantic compares portable behavior after permitted normalization.
func (Suite) CompareSemantic(left, right SemanticArtifact) []string {
	leftPath := normalizePath(left.Root, left.Path)
	rightPath := normalizePath(right.Root, right.Path)
	differences := make([]string, 0)
	if leftPath != rightPath {
		differences = append(differences, "path")
	}
	if left.TaskCount != right.TaskCount || left.TaskDigest != right.TaskDigest ||
		!slices.Equal(left.TaskPrefix, right.TaskPrefix) || !slices.Equal(left.TaskSuffix, right.TaskSuffix) ||
		!slices.Equal(left.TaskOrder, right.TaskOrder) {
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
