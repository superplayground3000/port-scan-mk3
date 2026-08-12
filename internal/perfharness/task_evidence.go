package perfharness

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"sync"
)

const (
	taskEvidenceFullLimit   = 256
	taskEvidencePrefixLimit = 8
	taskEvidenceSuffixLimit = 8
)

type taskEvidenceSnapshot struct {
	Count  uint64
	Digest string
	Full   []string
	Prefix []string
	Suffix []string
}

type orderedTaskEvidence struct {
	mu         sync.Mutex
	digest     hash.Hash
	count      uint64
	full       []string
	prefix     []string
	suffix     [taskEvidenceSuffixLimit]string
	suffixNext int
	suffixUsed int
	length     [8]byte
}

func newOrderedTaskEvidence() *orderedTaskEvidence {
	return &orderedTaskEvidence{
		digest: sha256.New(),
		full:   make([]string, 0, taskEvidenceFullLimit),
		prefix: make([]string, 0, taskEvidencePrefixLimit),
	}
}

func (evidence *orderedTaskEvidence) Observe(task string) {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()

	binary.BigEndian.PutUint64(evidence.length[:], uint64(len(task)))
	_, _ = evidence.digest.Write(evidence.length[:])
	_, _ = evidence.digest.Write([]byte(task))
	evidence.count++
	if len(evidence.prefix) < taskEvidencePrefixLimit {
		evidence.prefix = append(evidence.prefix, task)
	}
	if evidence.full != nil {
		if len(evidence.full) < taskEvidenceFullLimit {
			evidence.full = append(evidence.full, task)
		} else {
			evidence.full = nil
		}
	}
	evidence.suffix[evidence.suffixNext] = task
	evidence.suffixNext = (evidence.suffixNext + 1) % taskEvidenceSuffixLimit
	evidence.suffixUsed = min(evidence.suffixUsed+1, taskEvidenceSuffixLimit)
}

func (evidence *orderedTaskEvidence) Snapshot() taskEvidenceSnapshot {
	evidence.mu.Lock()
	defer evidence.mu.Unlock()

	suffix := make([]string, evidence.suffixUsed)
	start := 0
	if evidence.suffixUsed == taskEvidenceSuffixLimit {
		start = evidence.suffixNext
	}
	for index := range suffix {
		suffix[index] = evidence.suffix[(start+index)%taskEvidenceSuffixLimit]
	}
	return taskEvidenceSnapshot{
		Count:  evidence.count,
		Digest: fmt.Sprintf("%x", evidence.digest.Sum(nil)),
		Full:   append([]string(nil), evidence.full...),
		Prefix: append([]string(nil), evidence.prefix...),
		Suffix: suffix,
	}
}
