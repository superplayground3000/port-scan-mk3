//go:build !windows

package scanapp

import "testing"

// On unix, ensureFDLimit compares the requested worker count against
// RLIMIT_NOFILE, so an absurd worker count must be rejected.
func TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError(t *testing.T) {
	err := ensureFDLimit(1_000_000_000)
	if err == nil {
		t.Fatal("expected fd limit error for huge workers")
	}
}
