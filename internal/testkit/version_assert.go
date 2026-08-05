package testkit

import (
	"strings"
	"testing"
)

// AssertUnstampedVersionReport checks that out is the version report of a
// binary built WITHOUT release ldflags — which is how `go test` builds every
// command — for the command named cmdName.
//
// It deliberately asserts the placeholder values ("dev", "unknown"): those are
// the documented fallbacks, and asserting them here is what keeps the in-process
// tests honest about what they can and cannot prove. They cannot prove the
// linker stamps anything, because there is no linker stamp in a `go test`
// binary. That half of the contract is covered by the stamped-binary tests in
// tests/release, which build real binaries with -ldflags and assert the stamped
// values come back out.
func AssertUnstampedVersionReport(t *testing.T, cmdName, out string) {
	t.Helper()
	for _, want := range []string{
		cmdName + " version dev",
		"commit:  unknown",
		"built:   unknown",
		"go:      go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("version report for %s is missing %q:\n%s", cmdName, want, out)
		}
	}
}
