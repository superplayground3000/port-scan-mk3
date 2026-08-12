package perfharness

import (
	"slices"
	"testing"
)

func TestOrderedTaskEvidenceMatchesSmallFullCollection(t *testing.T) {
	t.Parallel()

	tasks := []string{"192.0.2.1:80/tcp", "192.0.2.2:443/tcp", "192.0.2.3:22/tcp"}
	evidence := newOrderedTaskEvidence()
	for _, task := range tasks {
		evidence.Observe(task)
	}
	got := evidence.Snapshot()
	if got.Count != uint64(len(tasks)) || got.Digest == "" {
		t.Fatalf("task evidence = %+v", got)
	}
	if !slices.Equal(got.Full, tasks) || !slices.Equal(got.Prefix, tasks) || !slices.Equal(got.Suffix, tasks) {
		t.Fatalf("small task evidence = %+v, want full input", got)
	}

	replayed := newOrderedTaskEvidence()
	for _, task := range got.Full {
		replayed.Observe(task)
	}
	if replayed.Snapshot().Digest != got.Digest {
		t.Fatal("online digest differs from the full collection digest")
	}
}

func TestOrderedTaskEvidenceStaysBoundedAndDetectsMiddleOrder(t *testing.T) {
	t.Parallel()

	left := newOrderedTaskEvidence()
	right := newOrderedTaskEvidence()
	for index := 0; index < taskEvidenceFullLimit+2; index++ {
		value := string(rune('a' + index%26))
		left.Observe(value)
		if index == taskEvidencePrefixLimit {
			right.Observe("different-middle")
		} else {
			right.Observe(value)
		}
	}
	leftSnapshot := left.Snapshot()
	rightSnapshot := right.Snapshot()
	if leftSnapshot.Full != nil || rightSnapshot.Full != nil {
		t.Fatal("large task evidence retained the full task sequence")
	}
	if len(leftSnapshot.Prefix) > taskEvidencePrefixLimit || len(leftSnapshot.Suffix) > taskEvidenceSuffixLimit {
		t.Fatalf("task evidence is not bounded: %+v", leftSnapshot)
	}
	if leftSnapshot.Count != rightSnapshot.Count || leftSnapshot.Digest == rightSnapshot.Digest {
		t.Fatalf("ordered evidence did not detect a middle difference: left=%+v right=%+v", leftSnapshot, rightSnapshot)
	}
}
