package main

import (
	"bytes"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
)

// csv-transform's runMain takes the FULL argv (it strips argv[0] itself), so
// the version token is the second element here, not the first.
func TestRunMain_VersionRequest_PrintsBuildInfoToStdout(t *testing.T) {
	for _, arg := range []string{"version", "-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runMain([]string{"csv-transform", arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("runMain(%q) exit code = %d, want 0 (stderr: %s)", arg, code, stderr.String())
			}
			testkit.AssertUnstampedVersionReport(t, "csv-transform", stdout.String())
			if stderr.Len() != 0 {
				t.Errorf("version request wrote to stderr: %s", stderr.String())
			}
		})
	}
}
