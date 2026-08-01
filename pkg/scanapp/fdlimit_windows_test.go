//go:build windows

package scanapp

import "testing"

// Windows has no POSIX RLIMIT_NOFILE, so ensureFDLimit is a documented no-op
// there (see fdlimit_windows.go). This asserts that contract explicitly rather
// than skipping the unix expectation: every worker count, including an absurd
// one, must return nil and must not panic.
func TestEnsureFDLimit_OnWindows_IsNoOpForAnyWorkerCount(t *testing.T) {
	for _, workers := range []int{-1, 0, 1, 1024, 1_000_000_000} {
		if err := ensureFDLimit(workers); err != nil {
			t.Fatalf("ensureFDLimit(%d) = %v, want nil (Windows no-op contract)", workers, err)
		}
	}
}
